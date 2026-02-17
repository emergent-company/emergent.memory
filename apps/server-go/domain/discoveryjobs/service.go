package discoveryjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/llm"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Service handles business logic for discovery jobs
type Service struct {
	repo *Repository
	cfg  *config.Config
	llm  llm.Provider
	log  *slog.Logger
}

// NewService creates a new discovery jobs service
func NewService(repo *Repository, cfg *config.Config, llmProvider llm.Provider, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		cfg:  cfg,
		llm:  llmProvider,
		log:  log.With(logger.Scope("discoveryjobs.svc")),
	}
}

// StartDiscovery starts a new discovery job
func (s *Service) StartDiscovery(ctx context.Context, projectID, orgID uuid.UUID, req *StartDiscoveryRequest) (*StartDiscoveryResponse, error) {
	// Set defaults
	if req.BatchSize <= 0 {
		req.BatchSize = 5
	}
	if req.MinConfidence <= 0 {
		req.MinConfidence = 0.5
	}
	if req.MaxIterations <= 0 {
		req.MaxIterations = 3
	}

	// Get KB purpose from project
	kbPurpose, err := s.repo.GetProjectKBPurpose(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// Convert document IDs to strings for config
	docIDStrings := make([]string, len(req.DocumentIDs))
	for i, id := range req.DocumentIDs {
		docIDStrings[i] = id.String()
	}

	// Calculate total steps for progress tracking
	numBatches := (len(req.DocumentIDs) + req.BatchSize - 1) / req.BatchSize
	totalSteps := numBatches + 2 // batches + refinement + pack creation

	// Create job
	job := &DiscoveryJob{
		ID:             uuid.New(),
		TenantID:       orgID, // Using orgID as tenantID for now
		OrganizationID: orgID,
		ProjectID:      projectID,
		Status:         StatusPending,
		Progress: JSONMap{
			"current_step": 0,
			"total_steps":  totalSteps,
			"message":      "Discovery job created, waiting to start...",
		},
		Config: JSONMap{
			"document_ids":          docIDStrings,
			"batch_size":            req.BatchSize,
			"min_confidence":        req.MinConfidence,
			"include_relationships": req.IncludeRelationships,
			"max_iterations":        req.MaxIterations,
		},
		KBPurpose:               kbPurpose,
		DiscoveredTypes:         JSONArray{},
		DiscoveredRelationships: JSONArray{},
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
	}

	if err := s.repo.Create(ctx, job); err != nil {
		return nil, err
	}

	// Start processing asynchronously
	go s.processDiscoveryJob(context.Background(), job.ID, projectID)

	return &StartDiscoveryResponse{JobID: job.ID}, nil
}

// GetJobStatus retrieves the status of a discovery job
func (s *Service) GetJobStatus(ctx context.Context, jobID uuid.UUID) (*JobStatusResponse, error) {
	job, err := s.repo.GetByID(ctx, jobID)
	if err != nil {
		return nil, err
	}

	return &JobStatusResponse{
		ID:                      job.ID,
		Status:                  job.Status,
		Progress:                job.Progress,
		CreatedAt:               job.CreatedAt,
		StartedAt:               job.StartedAt,
		CompletedAt:             job.CompletedAt,
		ErrorMessage:            job.ErrorMessage,
		DiscoveredTypes:         job.DiscoveredTypes,
		DiscoveredRelationships: job.DiscoveredRelationships,
		TemplatePackID:          job.TemplatePackID,
	}, nil
}

// ListJobsForProject retrieves discovery jobs for a project
func (s *Service) ListJobsForProject(ctx context.Context, projectID uuid.UUID) ([]*JobListItem, error) {
	jobs, err := s.repo.ListByProject(ctx, projectID, 20)
	if err != nil {
		return nil, err
	}

	result := make([]*JobListItem, len(jobs))
	for i, job := range jobs {
		result[i] = &JobListItem{
			ID:                      job.ID,
			Status:                  job.Status,
			Progress:                job.Progress,
			CreatedAt:               job.CreatedAt,
			CompletedAt:             job.CompletedAt,
			DiscoveredTypes:         job.DiscoveredTypes,
			DiscoveredRelationships: job.DiscoveredRelationships,
			TemplatePackID:          job.TemplatePackID,
		}
	}
	return result, nil
}

// CancelJob cancels a discovery job
func (s *Service) CancelJob(ctx context.Context, jobID uuid.UUID) error {
	return s.repo.CancelJob(ctx, jobID)
}

// FinalizeDiscovery finalizes discovery and creates/extends a template pack
func (s *Service) FinalizeDiscovery(ctx context.Context, jobID, projectID, orgID uuid.UUID, req *FinalizeDiscoveryRequest) (*FinalizeDiscoveryResponse, error) {
	s.log.Info("finalizing discovery",
		slog.String("job_id", jobID.String()),
		slog.String("mode", req.Mode),
		slog.Int("types_count", len(req.IncludedTypes)),
		slog.Int("relationships_count", len(req.IncludedRelationships)))

	// Build template pack schemas
	objectTypeSchemas := make(JSONMap)
	uiConfigs := make(JSONMap)

	for _, t := range req.IncludedTypes {
		objectTypeSchemas[t.TypeName] = map[string]any{
			"type":       "object",
			"required":   t.RequiredProperties,
			"properties": t.Properties,
		}
		uiConfigs[t.TypeName] = map[string]any{
			"icon":        s.suggestIconForType(t.TypeName),
			"color":       s.generateColorForType(t.TypeName),
			"displayName": t.TypeName,
			"description": t.Description,
		}
	}

	relationshipTypeSchemas := make(JSONMap)
	for _, rel := range req.IncludedRelationships {
		relationshipTypeSchemas[rel.RelationType] = map[string]any{
			"sourceTypes": []string{rel.SourceType},
			"targetTypes": []string{rel.TargetType},
			"cardinality": rel.Cardinality,
			"description": rel.Description,
		}
	}

	var templatePackID uuid.UUID
	var message string

	if req.Mode == "create" {
		// Create new template pack
		packID, err := s.repo.CreateTemplatePack(ctx, CreateTemplatePackParams{
			Name:                    req.PackName,
			Version:                 "1.0.0",
			Description:             fmt.Sprintf("Discovery pack with %d types and %d relationships", len(req.IncludedTypes), len(req.IncludedRelationships)),
			Author:                  "Auto-Discovery System",
			ObjectTypeSchemas:       objectTypeSchemas,
			RelationshipTypeSchemas: relationshipTypeSchemas,
			UIConfigs:               uiConfigs,
			Source:                  "discovered",
			DiscoveryJobID:          &jobID,
			PendingReview:           false,
		})
		if err != nil {
			return nil, err
		}
		templatePackID = packID
		message = fmt.Sprintf("Created new template pack \"%s\" with %d types", req.PackName, len(req.IncludedTypes))
		s.log.Info("created new template pack", slog.String("pack_id", packID.String()))
	} else {
		// Extend existing pack
		if req.ExistingPackID == nil {
			return nil, apperror.ErrBadRequest.WithMessage("existingPackId is required for extend mode")
		}

		existingPack, err := s.repo.GetTemplatePack(ctx, *req.ExistingPackID)
		if err != nil {
			return nil, err
		}

		// Merge schemas
		mergedObjectSchemas := existingPack.ObjectTypeSchemas
		for k, v := range objectTypeSchemas {
			mergedObjectSchemas[k] = v
		}

		mergedRelSchemas := existingPack.RelationshipTypeSchemas
		for k, v := range relationshipTypeSchemas {
			mergedRelSchemas[k] = v
		}

		mergedUIConfigs := existingPack.UIConfigs
		for k, v := range uiConfigs {
			mergedUIConfigs[k] = v
		}

		if err := s.repo.UpdateTemplatePack(ctx, *req.ExistingPackID, mergedObjectSchemas, mergedRelSchemas, mergedUIConfigs); err != nil {
			return nil, err
		}

		templatePackID = *req.ExistingPackID
		message = fmt.Sprintf("Extended template pack with %d additional types", len(req.IncludedTypes))
		s.log.Info("extended template pack", slog.String("pack_id", templatePackID.String()))
	}

	// Update discovery job
	if err := s.repo.SetJobTemplatePack(ctx, jobID, templatePackID); err != nil {
		return nil, err
	}

	return &FinalizeDiscoveryResponse{
		TemplatePackID: templatePackID,
		Message:        message,
	}, nil
}

// processDiscoveryJob runs the discovery job in the background
func (s *Service) processDiscoveryJob(ctx context.Context, jobID, projectID uuid.UUID) {
	s.log.Info("starting discovery job", slog.String("job_id", jobID.String()), slog.String("project_id", projectID.String()))

	// Update status to analyzing
	if err := s.repo.UpdateStatus(ctx, jobID, StatusAnalyzingDocuments, nil); err != nil {
		s.log.Error("failed to update job status", logger.Error(err))
		return
	}
	if err := s.repo.MarkStarted(ctx, jobID); err != nil {
		s.log.Error("failed to mark job started", logger.Error(err))
		return
	}

	// Get job to access config
	job, err := s.repo.GetByID(ctx, jobID)
	if err != nil {
		s.handleJobError(ctx, jobID, err)
		return
	}

	// Parse config
	configBytes, _ := json.Marshal(job.Config)
	var config JobConfig
	if err := json.Unmarshal(configBytes, &config); err != nil {
		s.handleJobError(ctx, jobID, fmt.Errorf("invalid job config: %w", err))
		return
	}

	// Parse document IDs
	documentIDs := make([]uuid.UUID, 0, len(config.DocumentIDs))
	for _, idStr := range config.DocumentIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			s.handleJobError(ctx, jobID, fmt.Errorf("invalid document ID: %s", idStr))
			return
		}
		documentIDs = append(documentIDs, id)
	}

	// Step 1: Batch documents
	batches := s.batchDocuments(documentIDs, config.BatchSize)
	s.log.Info("processing batches", slog.Int("batch_count", len(batches)))

	// Step 2: Extract types from each batch
	if err := s.repo.UpdateStatus(ctx, jobID, StatusExtractingTypes, nil); err != nil {
		s.handleJobError(ctx, jobID, err)
		return
	}

	successfulBatches := 0
	failedBatches := 0
	var batchErrors []string

	for i, batch := range batches {
		batchNum := i + 1
		s.updateProgress(ctx, jobID, batchNum, len(batches)+2, fmt.Sprintf("Analyzing batch %d/%d...", batchNum, len(batches)))

		if err := s.extractTypesFromBatch(ctx, jobID, batch, batchNum, job.KBPurpose); err != nil {
			failedBatches++
			batchErrors = append(batchErrors, fmt.Sprintf("Batch %d: %v", batchNum, err))
			s.log.Error("batch extraction failed", slog.Int("batch", batchNum), logger.Error(err))
			// Continue with other batches
		} else {
			successfulBatches++
		}
	}

	// If ALL batches failed, fail the entire job
	if failedBatches > 0 && successfulBatches == 0 {
		errorMsg := fmt.Sprintf("All %d batches failed. Errors: %s", failedBatches, strings.Join(batchErrors, "; "))
		s.handleJobError(ctx, jobID, fmt.Errorf("%s", errorMsg))
		return
	}

	// Step 3: Refine and merge types
	if err := s.repo.UpdateStatus(ctx, jobID, StatusRefiningSchemas, nil); err != nil {
		s.handleJobError(ctx, jobID, err)
		return
	}
	s.updateProgress(ctx, jobID, len(batches)+1, len(batches)+2, "Refining discovered types and merging duplicates...")

	refinedTypes, err := s.refineAndMergeTypes(ctx, jobID, config.MinConfidence)
	if err != nil {
		s.handleJobError(ctx, jobID, err)
		return
	}

	// Step 4: Discover relationships (if enabled and we have types)
	var relationships []DiscoveredRelationship
	if config.IncludeRelationships && len(refinedTypes) > 1 {
		relationships, err = s.discoverRelationships(ctx, jobID, refinedTypes, job.KBPurpose)
		if err != nil {
			s.log.Warn("relationship discovery failed, continuing without relationships", logger.Error(err))
			relationships = []DiscoveredRelationship{}
		}
	}

	// Step 5: Create template pack
	if len(refinedTypes) == 0 {
		s.handleJobError(ctx, jobID, fmt.Errorf("discovery completed but found no entity types"))
		return
	}

	if err := s.repo.UpdateStatus(ctx, jobID, StatusCreatingPack, nil); err != nil {
		s.handleJobError(ctx, jobID, err)
		return
	}
	s.updateProgress(ctx, jobID, len(batches)+2, len(batches)+2, "Creating template pack from discovered types...")

	// Convert types and relationships to JSONArray for storage
	typesArray := make(JSONArray, len(refinedTypes))
	for i, t := range refinedTypes {
		typesArray[i] = map[string]any{
			"type_name":           t.TypeName,
			"description":         t.Description,
			"confidence":          t.Confidence,
			"properties":          t.Properties,
			"required_properties": t.RequiredProperties,
			"example_instances":   t.ExampleInstances,
			"frequency":           t.Frequency,
		}
	}

	relsArray := make(JSONArray, len(relationships))
	for i, r := range relationships {
		relsArray[i] = map[string]any{
			"source_type":   r.SourceType,
			"target_type":   r.TargetType,
			"relation_type": r.RelationType,
			"description":   r.Description,
			"confidence":    r.Confidence,
			"cardinality":   r.Cardinality,
		}
	}

	// Create auto template pack
	timestamp := time.Now().Format("2006-01-02-15-04-05")
	packName := fmt.Sprintf("Discovered Types - %s", timestamp)

	templatePackID, err := s.repo.CreateTemplatePack(ctx, CreateTemplatePackParams{
		Name:                    packName,
		Version:                 "1.0.0",
		Description:             fmt.Sprintf("Auto-discovered types from %d entities", len(refinedTypes)),
		Author:                  "Auto-Discovery System",
		ObjectTypeSchemas:       s.typesToObjectSchemas(refinedTypes),
		RelationshipTypeSchemas: s.relationshipsToSchemas(relationships),
		UIConfigs:               s.typesToUIConfigs(refinedTypes),
		Source:                  "discovered",
		DiscoveryJobID:          &jobID,
		PendingReview:           true,
	})
	if err != nil {
		s.handleJobError(ctx, jobID, err)
		return
	}

	// Step 6: Complete
	if err := s.repo.MarkCompleted(ctx, jobID, &templatePackID, typesArray, relsArray); err != nil {
		s.handleJobError(ctx, jobID, err)
		return
	}

	s.log.Info("discovery job completed",
		slog.String("job_id", jobID.String()),
		slog.String("pack_id", templatePackID.String()),
		slog.Int("types_count", len(refinedTypes)),
		slog.Int("relationships_count", len(relationships)))
}

// batchDocuments splits document IDs into batches
func (s *Service) batchDocuments(documentIDs []uuid.UUID, batchSize int) [][]uuid.UUID {
	var batches [][]uuid.UUID
	for i := 0; i < len(documentIDs); i += batchSize {
		end := i + batchSize
		if end > len(documentIDs) {
			end = len(documentIDs)
		}
		batches = append(batches, documentIDs[i:end])
	}
	return batches
}

// extractTypesFromBatch extracts types from a batch of documents using LLM
func (s *Service) extractTypesFromBatch(ctx context.Context, jobID uuid.UUID, documentIDs []uuid.UUID, batchNum int, kbPurpose string) error {
	s.log.Info("extracting types from batch",
		slog.Int("batch", batchNum),
		slog.Int("doc_count", len(documentIDs)))

	// Get document contents
	docs, err := s.repo.GetDocumentContents(ctx, documentIDs)
	if err != nil {
		return err
	}

	if len(docs) == 0 {
		s.log.Warn("no documents found for batch", slog.Int("batch", batchNum))
		return nil
	}

	// Build prompt
	prompt := s.buildTypeDiscoveryPrompt(docs, kbPurpose)

	// Call LLM
	if s.llm == nil {
		return fmt.Errorf("LLM provider not configured")
	}

	response, err := s.llm.Complete(ctx, prompt)
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse response
	types, err := s.parseTypeDiscoveryResponse(response)
	if err != nil {
		return fmt.Errorf("failed to parse LLM response: %w", err)
	}

	s.log.Info("LLM discovered types",
		slog.Int("batch", batchNum),
		slog.Int("type_count", len(types)))

	// Store candidates
	for _, t := range types {
		candidate := &DiscoveryTypeCandidate{
			ID:                uuid.New(),
			JobID:             jobID,
			BatchNumber:       batchNum,
			TypeName:          t.TypeName,
			Description:       &t.Description,
			Confidence:        t.Confidence,
			InferredSchema:    t.Properties,
			ExampleInstances:  toJSONArray(t.ExampleInstances),
			Frequency:         len(t.ExampleInstances),
			SourceDocumentIDs: documentIDs,
			ExtractionContext: strPtr(fmt.Sprintf("Batch %d: Discovered from %d documents", batchNum, len(docs))),
			Status:            CandidateStatusCandidate,
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
		}
		if err := s.repo.CreateTypeCandidate(ctx, candidate); err != nil {
			s.log.Error("failed to store type candidate", logger.Error(err))
			// Continue with other candidates
		}
	}

	return nil
}

// buildTypeDiscoveryPrompt builds the LLM prompt for type discovery
func (s *Service) buildTypeDiscoveryPrompt(docs []DocumentContent, kbPurpose string) string {
	var combinedContent strings.Builder
	for _, doc := range docs {
		combinedContent.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", doc.Filename, doc.Content))
	}

	return fmt.Sprintf(`You are analyzing a knowledge base with the following purpose:

%s

Based on the documents provided, discover up to 20 important entity types that should be tracked in this knowledge base.

For each type, provide:
- type_name: A clear, singular name (e.g., "Customer", "Product", "Issue")
- description: What this type represents
- inferred_schema: A JSON schema describing the properties of this type
- example_instances: 2-3 example instances from the documents
- confidence: How confident you are about this type (0-1)
- occurrences: Estimate how many times this type appears

Documents:
%s

Return ONLY a JSON object with this structure (no markdown, no code blocks):
{
  "discovered_types": [
    {
      "type_name": "...",
      "description": "...",
      "inferred_schema": {...},
      "example_instances": [...],
      "confidence": 0.9,
      "occurrences": 5
    }
  ]
}`, kbPurpose, combinedContent.String())
}

// parseTypeDiscoveryResponse parses the LLM response for type discovery
func (s *Service) parseTypeDiscoveryResponse(response string) ([]DiscoveredType, error) {
	// Clean response (remove markdown code blocks if present)
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
		if idx := strings.LastIndex(response, "```"); idx >= 0 {
			response = response[:idx]
		}
	} else if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		if idx := strings.LastIndex(response, "```"); idx >= 0 {
			response = response[:idx]
		}
	}
	response = strings.TrimSpace(response)

	var result struct {
		DiscoveredTypes []struct {
			TypeName         string         `json:"type_name"`
			Description      string         `json:"description"`
			InferredSchema   map[string]any `json:"inferred_schema"`
			ExampleInstances []any          `json:"example_instances"`
			Confidence       float32        `json:"confidence"`
			Occurrences      int            `json:"occurrences"`
		} `json:"discovered_types"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, err
	}

	types := make([]DiscoveredType, len(result.DiscoveredTypes))
	for i, t := range result.DiscoveredTypes {
		types[i] = DiscoveredType{
			TypeName:           t.TypeName,
			Description:        t.Description,
			Confidence:         t.Confidence,
			Properties:         t.InferredSchema,
			RequiredProperties: []string{},
			ExampleInstances:   t.ExampleInstances,
			Frequency:          t.Occurrences,
		}
	}

	return types, nil
}

// refineAndMergeTypes refines and merges type candidates
func (s *Service) refineAndMergeTypes(ctx context.Context, jobID uuid.UUID, minConfidence float32) ([]DiscoveredType, error) {
	candidates, err := s.repo.GetCandidatesByJob(ctx, jobID, CandidateStatusCandidate)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		s.log.Warn("no type candidates found for job", slog.String("job_id", jobID.String()))
		return nil, nil
	}

	s.log.Info("refining type candidates", slog.Int("candidate_count", len(candidates)))

	// Group by similar names
	groups := s.groupSimilarTypes(candidates)

	var refinedTypes []DiscoveredType
	for _, group := range groups {
		merged := s.mergeTypeSchemas(group)
		if merged.Confidence >= minConfidence {
			refinedTypes = append(refinedTypes, merged)

			// Mark originals as merged
			for _, candidate := range group {
				_ = s.repo.UpdateCandidateStatus(ctx, candidate.ID, CandidateStatusMerged)
			}
		}
	}

	// Save refined types
	typesArray := make(JSONArray, len(refinedTypes))
	for i, t := range refinedTypes {
		typesArray[i] = map[string]any{
			"type_name":           t.TypeName,
			"description":         t.Description,
			"confidence":          t.Confidence,
			"properties":          t.Properties,
			"required_properties": t.RequiredProperties,
			"example_instances":   t.ExampleInstances,
			"frequency":           t.Frequency,
		}
	}
	if err := s.repo.SaveDiscoveredTypes(ctx, jobID, typesArray); err != nil {
		return nil, err
	}

	s.log.Info("refined types", slog.Int("count", len(refinedTypes)))
	return refinedTypes, nil
}

// groupSimilarTypes groups similar type candidates
func (s *Service) groupSimilarTypes(candidates []*DiscoveryTypeCandidate) [][]*DiscoveryTypeCandidate {
	var groups [][]*DiscoveryTypeCandidate
	used := make(map[uuid.UUID]bool)

	for _, candidate := range candidates {
		if used[candidate.ID] {
			continue
		}

		group := []*DiscoveryTypeCandidate{candidate}
		used[candidate.ID] = true

		for _, other := range candidates {
			if used[other.ID] {
				continue
			}

			similarity := s.calculateTypeSimilarity(candidate.TypeName, other.TypeName)
			if similarity > 0.8 {
				group = append(group, other)
				used[other.ID] = true
			}
		}

		groups = append(groups, group)
	}

	return groups
}

// calculateTypeSimilarity calculates similarity between two type names
func (s *Service) calculateTypeSimilarity(name1, name2 string) float64 {
	n1 := strings.ToLower(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		return -1
	}, name1))
	n2 := strings.ToLower(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		return -1
	}, name2))

	if n1 == n2 {
		return 1.0
	}

	maxLen := len(n1)
	if len(n2) > maxLen {
		maxLen = len(n2)
	}
	if maxLen == 0 {
		return 0.0
	}

	distance := levenshteinDistance(n1, n2)
	return 1 - float64(distance)/float64(maxLen)
}

// levenshteinDistance calculates the Levenshtein distance between two strings
func levenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	matrix := make([][]int, len(b)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(a)+1)
		matrix[i][0] = i
	}
	for j := 0; j <= len(a); j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= len(b); i++ {
		for j := 1; j <= len(a); j++ {
			if b[i-1] == a[j-1] {
				matrix[i][j] = matrix[i-1][j-1]
			} else {
				min := matrix[i-1][j-1]
				if matrix[i][j-1] < min {
					min = matrix[i][j-1]
				}
				if matrix[i-1][j] < min {
					min = matrix[i-1][j]
				}
				matrix[i][j] = min + 1
			}
		}
	}

	return matrix[len(b)][len(a)]
}

// mergeTypeSchemas merges multiple type candidates into one
func (s *Service) mergeTypeSchemas(candidates []*DiscoveryTypeCandidate) DiscoveredType {
	// Find best candidate
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Confidence > best.Confidence {
			best = c
		}
	}

	// Merge properties
	allProperties := make(map[string]any)
	allExamples := make([]any, 0)

	for _, c := range candidates {
		for k, v := range c.InferredSchema {
			allProperties[k] = v
		}
		allExamples = append(allExamples, c.ExampleInstances...)
	}

	// Calculate average confidence
	var totalConfidence float32
	var totalFrequency int
	for _, c := range candidates {
		totalConfidence += c.Confidence
		totalFrequency += c.Frequency
	}
	avgConfidence := totalConfidence / float32(len(candidates))

	// Limit examples
	if len(allExamples) > 5 {
		allExamples = allExamples[:5]
	}

	description := ""
	if best.Description != nil {
		description = *best.Description
	}

	return DiscoveredType{
		TypeName:           best.TypeName,
		Description:        description,
		Confidence:         avgConfidence,
		Properties:         allProperties,
		RequiredProperties: []string{},
		ExampleInstances:   allExamples,
		Frequency:          totalFrequency,
	}
}

// discoverRelationships discovers relationships between types using LLM
func (s *Service) discoverRelationships(ctx context.Context, jobID uuid.UUID, types []DiscoveredType, kbPurpose string) ([]DiscoveredRelationship, error) {
	s.log.Info("discovering relationships", slog.Int("type_count", len(types)))

	if s.llm == nil {
		return nil, fmt.Errorf("LLM provider not configured")
	}

	// Build types list
	var typesList strings.Builder
	for _, t := range types {
		typesList.WriteString(fmt.Sprintf("- %s: %s\n", t.TypeName, t.Description))
	}

	prompt := fmt.Sprintf(`You are analyzing a knowledge base with the following purpose:

%s

We have discovered the following entity types:
%s

Discover important relationships between these types.

For each relationship, provide:
- from_type: The source type (must be one of the types above)
- to_type: The target type (must be one of the types above)
- relationship_name: A clear name for the relationship
- description: What this relationship represents
- cardinality: one-to-one, one-to-many, or many-to-many
- confidence: How confident you are about this relationship (0-1)

Return ONLY a JSON object with this structure (no markdown, no code blocks):
{
  "discovered_relationships": [
    {
      "from_type": "...",
      "to_type": "...",
      "relationship_name": "...",
      "description": "...",
      "cardinality": "one-to-many",
      "confidence": 0.9
    }
  ]
}

Focus on the most important relationships.`, kbPurpose, typesList.String())

	response, err := s.llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse response
	relationships, err := s.parseRelationshipDiscoveryResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Save relationships
	relsArray := make(JSONArray, len(relationships))
	for i, r := range relationships {
		relsArray[i] = map[string]any{
			"source_type":   r.SourceType,
			"target_type":   r.TargetType,
			"relation_type": r.RelationType,
			"description":   r.Description,
			"confidence":    r.Confidence,
			"cardinality":   r.Cardinality,
		}
	}
	if err := s.repo.SaveDiscoveredRelationships(ctx, jobID, relsArray); err != nil {
		return nil, err
	}

	s.log.Info("discovered relationships", slog.Int("count", len(relationships)))
	return relationships, nil
}

// parseRelationshipDiscoveryResponse parses the LLM response for relationship discovery
func (s *Service) parseRelationshipDiscoveryResponse(response string) ([]DiscoveredRelationship, error) {
	// Clean response
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
		if idx := strings.LastIndex(response, "```"); idx >= 0 {
			response = response[:idx]
		}
	} else if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		if idx := strings.LastIndex(response, "```"); idx >= 0 {
			response = response[:idx]
		}
	}
	response = strings.TrimSpace(response)

	var result struct {
		DiscoveredRelationships []struct {
			FromType         string  `json:"from_type"`
			ToType           string  `json:"to_type"`
			RelationshipName string  `json:"relationship_name"`
			Description      string  `json:"description"`
			Cardinality      string  `json:"cardinality"`
			Confidence       float32 `json:"confidence"`
		} `json:"discovered_relationships"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, err
	}

	relationships := make([]DiscoveredRelationship, len(result.DiscoveredRelationships))
	for i, r := range result.DiscoveredRelationships {
		relationships[i] = DiscoveredRelationship{
			SourceType:   r.FromType,
			TargetType:   r.ToType,
			RelationType: r.RelationshipName,
			Description:  r.Description,
			Confidence:   r.Confidence,
			Cardinality:  r.Cardinality,
		}
	}

	return relationships, nil
}

// Helper functions

func (s *Service) handleJobError(ctx context.Context, jobID uuid.UUID, err error) {
	errMsg := err.Error()
	s.log.Error("discovery job failed", slog.String("job_id", jobID.String()), logger.Error(err))
	_ = s.repo.UpdateStatus(ctx, jobID, StatusFailed, &errMsg)
}

func (s *Service) updateProgress(ctx context.Context, jobID uuid.UUID, current, total int, message string) {
	progress := JSONMap{
		"current_step": current,
		"total_steps":  total,
		"message":      message,
	}
	_ = s.repo.UpdateProgress(ctx, jobID, progress)
}

func (s *Service) typesToObjectSchemas(types []DiscoveredType) JSONMap {
	schemas := make(JSONMap)
	for _, t := range types {
		schemas[t.TypeName] = map[string]any{
			"type":       "object",
			"required":   t.RequiredProperties,
			"properties": t.Properties,
		}
	}
	return schemas
}

func (s *Service) relationshipsToSchemas(relationships []DiscoveredRelationship) JSONMap {
	schemas := make(JSONMap)
	for _, r := range relationships {
		schemas[r.RelationType] = map[string]any{
			"sourceTypes": []string{r.SourceType},
			"targetTypes": []string{r.TargetType},
			"cardinality": r.Cardinality,
			"description": r.Description,
		}
	}
	return schemas
}

func (s *Service) typesToUIConfigs(types []DiscoveredType) JSONMap {
	configs := make(JSONMap)
	for _, t := range types {
		configs[t.TypeName] = map[string]any{
			"icon":        s.suggestIconForType(t.TypeName),
			"color":       s.generateColorForType(t.TypeName),
			"displayName": t.TypeName,
			"description": t.Description,
		}
	}
	return configs
}

func (s *Service) suggestIconForType(typeName string) string {
	lower := strings.ToLower(typeName)
	switch {
	case strings.Contains(lower, "decision"):
		return "check-circle"
	case strings.Contains(lower, "requirement"):
		return "file-text"
	case strings.Contains(lower, "task"):
		return "check-square"
	case strings.Contains(lower, "issue"):
		return "alert-circle"
	case strings.Contains(lower, "risk"):
		return "alert-triangle"
	case strings.Contains(lower, "person"), strings.Contains(lower, "user"):
		return "user"
	case strings.Contains(lower, "team"), strings.Contains(lower, "group"):
		return "users"
	case strings.Contains(lower, "component"), strings.Contains(lower, "system"):
		return "box"
	case strings.Contains(lower, "document"):
		return "file"
	case strings.Contains(lower, "meeting"):
		return "calendar"
	default:
		return "circle"
	}
}

func (s *Service) generateColorForType(typeName string) string {
	colors := []string{
		"#3B82F6", // Blue
		"#10B981", // Green
		"#F59E0B", // Amber
		"#EF4444", // Red
		"#8B5CF6", // Purple
		"#EC4899", // Pink
		"#06B6D4", // Cyan
		"#84CC16", // Lime
	}

	var hash int
	for _, c := range typeName {
		hash = int(c) + ((hash << 5) - hash)
	}
	if hash < 0 {
		hash = -hash
	}

	return colors[hash%len(colors)]
}

func strPtr(s string) *string {
	return &s
}

func toJSONArray(items []any) JSONArray {
	if items == nil {
		return JSONArray{}
	}
	return JSONArray(items)
}
