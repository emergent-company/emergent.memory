// Package extraction provides object extraction job processing.
package extraction

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/syshealth"
	"github.com/emergent-company/emergent.memory/pkg/textsplitter"
	"github.com/emergent-company/emergent.memory/pkg/tracing"
)

// ObjectExtractionWorkerConfig holds configuration for the extraction worker.
type ObjectExtractionWorkerConfig struct {
	// PollInterval is how often to check for new jobs. Default: 5 seconds.
	PollInterval time.Duration

	// Concurrency is the maximum number of jobs to process concurrently
	Concurrency int

	// OrphanThreshold is the max acceptable orphan rate (0.0-1.0). Default: 0.3.
	OrphanThreshold float64

	// MaxRetries is the max number of relationship extraction retries. Default: 3.
	MaxRetries uint
}

// DefaultObjectExtractionWorkerConfig returns default worker configuration.
func DefaultObjectExtractionWorkerConfig() *ObjectExtractionWorkerConfig {
	return &ObjectExtractionWorkerConfig{
		PollInterval:    5 * time.Second,
		Concurrency:     5,
		OrphanThreshold: 0.3,
		MaxRetries:      3,
	}
}

// SchemaProvider provides object and relationship schemas for extraction.
// This interface allows different implementations (e.g., from database, config, etc.).
type SchemaProvider interface {
	// GetProjectSchemas returns object and relationship schemas for a project.
	GetProjectSchemas(ctx context.Context, projectID string) (*ExtractionSchemas, error)
}

// ExtractionSchemas holds the schemas needed for extraction.
type ExtractionSchemas struct {
	ObjectSchemas       map[string]agents.ObjectSchema
	RelationshipSchemas map[string]agents.RelationshipSchema
}

// ObjectExtractionWorker processes object extraction jobs.
type ObjectExtractionWorker struct {
	jobsService    *ObjectExtractionJobsService
	graphService   *graph.Service
	docService     *documents.Service
	schemaProvider SchemaProvider
	modelFactory   *adk.ModelFactory
	limitResolver  adk.ModelLimitResolver // optional; nil → no truncation
	config         *ObjectExtractionWorkerConfig
	log            *slog.Logger
	scaler         *syshealth.ConcurrencyScaler

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewObjectExtractionWorker creates a new extraction worker.
func NewObjectExtractionWorker(
	jobsService *ObjectExtractionJobsService,
	graphService *graph.Service,
	docService *documents.Service,
	schemaProvider SchemaProvider,
	modelFactory *adk.ModelFactory,
	config *ObjectExtractionWorkerConfig,
	log *slog.Logger,
	scaler *syshealth.ConcurrencyScaler,
) *ObjectExtractionWorker {
	if config == nil {
		config = DefaultObjectExtractionWorkerConfig()
	}
	return &ObjectExtractionWorker{
		jobsService:    jobsService,
		graphService:   graphService,
		docService:     docService,
		schemaProvider: schemaProvider,
		modelFactory:   modelFactory,
		config:         config,
		log:            log.With(logger.Scope("object-extraction-worker")),
		scaler:         scaler,
		stopCh:         make(chan struct{}),
	}
}

// WithLimitResolver sets an optional ModelLimitResolver on the worker.
// When set, document text is truncated to the model's max_input_tokens before
// being sent to the extraction pipeline.
func (w *ObjectExtractionWorker) WithLimitResolver(r adk.ModelLimitResolver) *ObjectExtractionWorker {
	w.limitResolver = r
	return w
}

// Start begins processing jobs in the background.
func (w *ObjectExtractionWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.run(ctx)
	w.log.Info("object extraction worker started",
		slog.Duration("poll_interval", w.config.PollInterval))
}

// Stop gracefully stops the worker.
func (w *ObjectExtractionWorker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.log.Info("object extraction worker stopped")
}

// run is the main worker loop.
func (w *ObjectExtractionWorker) run(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			// Just fetch jobs and launch goroutines up to concurrency
			jobs, err := w.jobsService.DequeueBatch(ctx, w.config.Concurrency)
			if err != nil {
				w.log.Error("error dequeuing jobs", logger.Error(err))
				continue
			}
			if len(jobs) == 0 {
				continue
			}

			w.log.Info("processing extraction jobs", slog.Int("count", len(jobs)))

			concurrency := w.config.Concurrency
			if w.scaler != nil {
				concurrency = w.scaler.GetConcurrency(w.config.Concurrency)
			}
			if concurrency <= 0 {
				concurrency = 5
			}
			sem := make(chan struct{}, concurrency)
			var batchWg sync.WaitGroup

			for _, job := range jobs {
				sem <- struct{}{}
				batchWg.Add(1)
				go func(j *ObjectExtractionJob) {
					defer batchWg.Done()
					defer func() { <-sem }()
					if err := w.processSingleJob(ctx, j); err != nil {
						w.log.Error("job failed",
							slog.String("job_id", j.ID),
							logger.Error(err))
					}
				}(job)
			}
			batchWg.Wait()
		}
	}
}

// processSingleJob processes a single job after it's dequeued
func (w *ObjectExtractionWorker) processSingleJob(ctx context.Context, job *ObjectExtractionJob) error {
	docID := ""
	if job.DocumentID != nil {
		docID = *job.DocumentID
	}
	ctx, span := tracing.Start(ctx, "extraction.object_extraction",
		attribute.String("memory.job.id", job.ID),
		attribute.String("memory.project.id", job.ProjectID),
		attribute.String("memory.document.id", docID),
	)
	defer span.End()

	// Inject project ID into context so the credential resolver can look up
	// per-project LLM provider configuration (e.g. Vertex AI credentials).
	if job.ProjectID != "" {
		ctx = auth.ContextWithProjectID(ctx, job.ProjectID)
	}

	w.log.Info("processing extraction job",
		slog.String("job_id", job.ID),
		slog.String("project_id", job.ProjectID),
		slog.String("job_type", string(job.JobType)))

	// Process the job
	result, err := w.processJob(ctx, job)
	if err != nil {
		w.log.Error("job failed",
			slog.String("job_id", job.ID),
			logger.Error(err))

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return w.jobsService.MarkFailed(ctx, job.ID, err.Error(), JSON{
			"error_type": "processing_error",
		})
	}

	span.SetAttributes(
		attribute.Int("memory.extraction.entity_count", result.ObjectsCreated),
		attribute.Int("memory.extraction.relationship_count", result.RelationshipsCreated),
	)
	span.SetStatus(codes.Ok, "")

	// Mark completed
	return w.jobsService.MarkCompleted(ctx, job.ID, *result)
}

// processJob processes a single extraction job.
func (w *ObjectExtractionWorker) processJob(ctx context.Context, job *ObjectExtractionJob) (*ObjectExtractionResults, error) {
	// Load document text
	documentText, err := w.loadDocumentText(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("load document text: %w", err)
	}

	if documentText == "" {
		return nil, fmt.Errorf("no document text to extract from")
	}

	// Split into extraction-sized batches that fit the model context window.
	// Each batch is a semantically-split window (paragraph → sentence → word
	// boundaries) large enough for good relationship detection (~4000 tokens)
	// with 20% overlap to capture entities that span chunk boundaries.
	batches := w.splitIntoExtractionBatches(ctx, documentText)

	// Load schemas
	schemas, err := w.loadSchemas(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("load schemas: %w", err)
	}

	var traceLogger agents.TraceLogger
	documentID := ""
	if job.DocumentID != nil {
		documentID = *job.DocumentID
	}
	if tl, err := agents.NewExtractionTraceLogger(agents.TraceLoggerConfig{
		JobID:      job.ID,
		DocumentID: documentID,
		ProjectID:  job.ProjectID,
	}); err != nil {
		w.log.Warn("failed to create trace logger, continuing without tracing",
			slog.String("error", err.Error()),
		)
		traceLogger = &agents.NullTraceLogger{}
	} else {
		traceLogger = tl
		defer tl.Close()
	}

	// Create extraction pipeline (shared across all batches).
	pipeline, err := agents.NewExtractionPipeline(agents.ExtractionPipelineConfig{
		ModelFactory:        w.modelFactory,
		ObjectSchemas:       schemas.ObjectSchemas,
		RelationshipSchemas: schemas.RelationshipSchemas,
		OrphanThreshold:     w.config.OrphanThreshold,
		MaxRetries:          w.config.MaxRetries,
		Logger:              w.log,
		TraceLogger:         traceLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("create pipeline: %w", err)
	}

	// Run extraction over each batch, accumulating results.
	// persistResults upserts by (project_id, type, name) so duplicate entities
	// across overlapping batches are merged naturally.
	var totalResult *ObjectExtractionResults
	for i, batch := range batches {
		if len(batches) > 1 {
			w.log.Info("processing extraction batch",
				slog.Int("batch", i+1),
				slog.Int("total_batches", len(batches)),
				slog.Int("chars", len(batch)),
			)
		}

		pipelineOutput, err := pipeline.Run(ctx, agents.ExtractionPipelineInput{
			DocumentText:        batch,
			ObjectSchemas:       schemas.ObjectSchemas,
			RelationshipSchemas: schemas.RelationshipSchemas,
			AllowedTypes:        job.EnabledTypes,
		})
		if err != nil {
			return nil, fmt.Errorf("run pipeline (batch %d/%d): %w", i+1, len(batches), err)
		}

		result, err := w.persistResults(ctx, job, pipelineOutput)
		if err != nil {
			return nil, fmt.Errorf("persist results (batch %d/%d): %w", i+1, len(batches), err)
		}

		if totalResult == nil {
			totalResult = result
		} else {
			totalResult.ObjectsCreated += result.ObjectsCreated
			totalResult.RelationshipsCreated += result.RelationshipsCreated
		}
	}

	if totalResult == nil {
		totalResult = &ObjectExtractionResults{}
	}
	return totalResult, nil
}

// truncateToInputLimit caps documentText to the model's max_input_tokens.
// Uses a 4 chars-per-token estimate and reserves 20% for system prompts and
// schema descriptions. If the limit is unknown (0) or the resolver is nil,
// the text is returned unchanged.
// splitIntoExtractionBatches splits documentText into context-window-sized
// batches ready to pass to the extraction pipeline.
//
// Algorithm:
//  1. Split text using extraction-tuned config (16K chars / 3.2K overlap) —
//     respects paragraph → sentence → word boundaries, never cuts mid-word.
//  2. Compute maxBatchChars from the model's input limit (80% of token budget
//     at 4 chars/token to leave headroom for system prompt + schemas).
//  3. Greedily pack consecutive extraction chunks (joined with "\n\n") into
//     batches until the next chunk would exceed maxBatchChars.
//
// If limitResolver is nil or returns 0, all chunks are packed into one batch
// (equivalent to the previous single-shot behaviour).
func (w *ObjectExtractionWorker) splitIntoExtractionBatches(ctx context.Context, text string) []string {
	// Step 1: split into extraction-sized semantic chunks.
	chunks := textsplitter.Split(text, textsplitter.ExtractionConfig())
	if len(chunks) == 0 {
		return []string{text}
	}

	// Step 2: determine per-batch character budget.
	const (
		charsPerToken  = 4
		promptOverhead = 0.20
	)
	maxBatchChars := 0
	if w.limitResolver != nil {
		inputLimit, err := w.limitResolver.GetInputLimit(ctx)
		if err != nil {
			w.log.Warn("could not resolve model input limit, using single batch", logger.Error(err))
		} else if inputLimit > 0 {
			maxBatchChars = int(float64(inputLimit) * (1 - promptOverhead) * charsPerToken)
		}
	}

	// Step 3: greedy packing.
	// If no limit known, one batch = all chunks.
	if maxBatchChars <= 0 {
		return []string{strings.Join(chunks, "\n\n")}
	}

	var batches []string
	var current strings.Builder
	for _, chunk := range chunks {
		// +2 for the "\n\n" separator we'd prepend
		needed := len(chunk)
		if current.Len() > 0 {
			needed += 2
		}
		if current.Len() > 0 && current.Len()+needed > maxBatchChars {
			batches = append(batches, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(chunk)
	}
	if current.Len() > 0 {
		batches = append(batches, current.String())
	}

	if len(batches) > 1 {
		w.log.Info("document split into extraction batches",
			slog.Int("batches", len(batches)),
			slog.Int("total_chars", len(text)),
			slog.Int("max_batch_chars", maxBatchChars),
		)
	}
	return batches
}

// loadDocumentText loads the text content for extraction.
func (w *ObjectExtractionWorker) loadDocumentText(ctx context.Context, job *ObjectExtractionJob) (string, error) {
	// Check source type
	sourceType := ""
	if job.SourceType != nil {
		sourceType = *job.SourceType
	}

	switch sourceType {
	case "manual":
		// Get text from source metadata
		if job.SourceMetadata != nil {
			if text, ok := job.SourceMetadata["text"].(string); ok {
				return text, nil
			}
		}
		return "", fmt.Errorf("manual source type requires text in source_metadata")

	case "document", "":
		// Load from document
		if job.DocumentID == nil {
			return "", fmt.Errorf("document source type requires document_id")
		}
		doc, err := w.docService.GetByID(ctx, job.ProjectID, *job.DocumentID)
		if err != nil {
			return "", fmt.Errorf("get document: %w", err)
		}
		if doc.Content == nil || *doc.Content == "" {
			return "", fmt.Errorf("document has no content")
		}
		return *doc.Content, nil

	default:
		return "", fmt.Errorf("unsupported source type: %s", sourceType)
	}
}

// loadSchemas loads object and relationship schemas for the project.
func (w *ObjectExtractionWorker) loadSchemas(ctx context.Context, job *ObjectExtractionJob) (*ExtractionSchemas, error) {
	if w.schemaProvider != nil {
		return w.schemaProvider.GetProjectSchemas(ctx, job.ProjectID)
	}

	// Fall back to extraction config if no schema provider
	schemas := &ExtractionSchemas{
		ObjectSchemas:       make(map[string]agents.ObjectSchema),
		RelationshipSchemas: make(map[string]agents.RelationshipSchema),
	}

	// Try to get schemas from job's extraction config
	if job.ExtractionConfig != nil {
		if objSchemas, ok := job.ExtractionConfig["object_schemas"].(map[string]any); ok {
			for name, schema := range objSchemas {
				if schemaMap, ok := schema.(map[string]any); ok {
					schemas.ObjectSchemas[name] = convertToObjectSchema(schemaMap)
				}
			}
		}
		if relSchemas, ok := job.ExtractionConfig["relationship_schemas"].(map[string]any); ok {
			for name, schema := range relSchemas {
				if schemaMap, ok := schema.(map[string]any); ok {
					schemas.RelationshipSchemas[name] = convertToRelationshipSchema(schemaMap)
				}
			}
		}
	}

	return schemas, nil
}

// persistResults creates graph objects and relationships from extraction results.
func (w *ObjectExtractionWorker) persistResults(
	ctx context.Context,
	job *ObjectExtractionJob,
	output *agents.ExtractionPipelineOutput,
) (*ObjectExtractionResults, error) {
	projectID, err := uuid.Parse(job.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project_id: %w", err)
	}

	// Map temp_id -> created object ID
	tempIDToObjectID := make(map[string]uuid.UUID)

	// Create graph objects
	objectsCreated := 0
	for _, entity := range output.Entities {
		properties := map[string]any{
			"name":        entity.Name,
			"description": entity.Description,
		}
		for k, v := range entity.Properties {
			properties[k] = v
		}

		// Add extraction metadata
		properties["_extraction_job_id"] = job.ID
		if job.SourceType != nil {
			properties["_extraction_source"] = *job.SourceType
		}

		graphObj, err := w.graphService.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type:       entity.Type,
			Properties: properties,
			Status:     stringPtr("suggested"),
		}, nil)
		if err != nil {
			w.log.Warn("failed to create graph object",
				slog.String("name", entity.Name),
				slog.String("type", entity.Type),
				logger.Error(err))
			continue
		}

		tempIDToObjectID[entity.TempID] = graphObj.ID
		objectsCreated++
	}

	// Create relationships
	relationshipsCreated := 0
	for _, rel := range output.Relationships {
		srcID, srcOK := tempIDToObjectID[rel.SourceRef]
		dstID, dstOK := tempIDToObjectID[rel.TargetRef]

		if !srcOK || !dstOK {
			w.log.Warn("relationship references unknown temp_id",
				slog.String("source_ref", rel.SourceRef),
				slog.String("target_ref", rel.TargetRef),
				slog.Bool("src_found", srcOK),
				slog.Bool("dst_found", dstOK))
			continue
		}

		properties := map[string]any{
			"description":        rel.Description,
			"_extraction_job_id": job.ID,
		}

		_, err := w.graphService.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
			Type:       rel.Type,
			SrcID:      srcID,
			DstID:      dstID,
			Properties: properties,
		})
		if err != nil {
			w.log.Warn("failed to create relationship",
				slog.String("type", rel.Type),
				logger.Error(err))
			continue
		}
		relationshipsCreated++
	}

	// Calculate discovered types
	discoveredTypes := make([]any, 0)
	typeSet := make(map[string]bool)
	for _, entity := range output.Entities {
		if !typeSet[entity.Type] {
			typeSet[entity.Type] = true
			discoveredTypes = append(discoveredTypes, entity.Type)
		}
	}

	return &ObjectExtractionResults{
		ObjectsCreated:       objectsCreated,
		RelationshipsCreated: relationshipsCreated,
		TotalItems:           len(output.Entities),
		ProcessedItems:       len(output.Entities),
		SuccessfulItems:      objectsCreated,
		FailedItems:          len(output.Entities) - objectsCreated,
		DiscoveredTypes:      discoveredTypes,
		DebugInfo: JSON{
			"entity_count":       len(output.Entities),
			"relationship_count": len(output.Relationships),
			"orphan_rate":        agents.CalculateOrphanRate(output.Entities, output.Relationships),
		},
	}, nil
}

// convertToObjectSchema converts a generic map to ObjectSchema.
func convertToObjectSchema(m map[string]any) agents.ObjectSchema {
	schema := agents.ObjectSchema{}

	if name, ok := m["name"].(string); ok {
		schema.Name = name
	}
	if desc, ok := m["description"].(string); ok {
		schema.Description = desc
	}
	if props, ok := m["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]agents.PropertyDef)
		for k, v := range props {
			if propMap, ok := v.(map[string]any); ok {
				propDef := agents.PropertyDef{}
				if t, ok := propMap["type"].(string); ok {
					propDef.Type = t
				}
				if d, ok := propMap["description"].(string); ok {
					propDef.Description = d
				}
				schema.Properties[k] = propDef
			}
		}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	return schema
}

// convertToRelationshipSchema converts a generic map to RelationshipSchema.
func convertToRelationshipSchema(m map[string]any) agents.RelationshipSchema {
	schema := agents.RelationshipSchema{}

	if name, ok := m["name"].(string); ok {
		schema.Name = name
	}
	if desc, ok := m["description"].(string); ok {
		schema.Description = desc
	}
	if st, ok := m["source_types"].([]any); ok {
		for _, t := range st {
			if s, ok := t.(string); ok {
				schema.SourceTypes = append(schema.SourceTypes, s)
			}
		}
	}
	if tt, ok := m["target_types"].([]any); ok {
		for _, t := range tt {
			if s, ok := t.(string); ok {
				schema.TargetTypes = append(schema.TargetTypes, s)
			}
		}
	}
	if g, ok := m["extraction_guidelines"].(string); ok {
		schema.ExtractionGuidelines = g
	}

	return schema
}

// stringPtr returns a pointer to a string.
func stringPtr(s string) *string {
	return &s
}
