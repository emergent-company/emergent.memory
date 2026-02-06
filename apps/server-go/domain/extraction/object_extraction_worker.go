// Package extraction provides object extraction job processing.
package extraction

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/emergent/emergent-core/domain/documents"
	"github.com/emergent/emergent-core/domain/extraction/agents"
	"github.com/emergent/emergent-core/domain/graph"
	"github.com/emergent/emergent-core/pkg/adk"
	"github.com/emergent/emergent-core/pkg/logger"
)

// ObjectExtractionWorkerConfig holds configuration for the extraction worker.
type ObjectExtractionWorkerConfig struct {
	// PollInterval is how often to check for new jobs. Default: 5 seconds.
	PollInterval time.Duration

	// OrphanThreshold is the max acceptable orphan rate (0.0-1.0). Default: 0.3.
	OrphanThreshold float64

	// MaxRetries is the max number of relationship extraction retries. Default: 3.
	MaxRetries uint
}

// DefaultObjectExtractionWorkerConfig returns default worker configuration.
func DefaultObjectExtractionWorkerConfig() *ObjectExtractionWorkerConfig {
	return &ObjectExtractionWorkerConfig{
		PollInterval:    5 * time.Second,
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
	config         *ObjectExtractionWorkerConfig
	log            *slog.Logger

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
		stopCh:         make(chan struct{}),
	}
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
			if err := w.processNextJob(ctx); err != nil {
				w.log.Error("error processing job", logger.Error(err))
			}
		}
	}
}

// processNextJob dequeues and processes the next available job.
func (w *ObjectExtractionWorker) processNextJob(ctx context.Context) error {
	job, err := w.jobsService.Dequeue(ctx)
	if err != nil {
		return fmt.Errorf("dequeue job: %w", err)
	}
	if job == nil {
		return nil // No jobs available
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

		return w.jobsService.MarkFailed(ctx, job.ID, err.Error(), JSON{
			"error_type": "processing_error",
		})
	}

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

	// Create and run the extraction pipeline
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

	// Run extraction
	pipelineOutput, err := pipeline.Run(ctx, agents.ExtractionPipelineInput{
		DocumentText:        documentText,
		ObjectSchemas:       schemas.ObjectSchemas,
		RelationshipSchemas: schemas.RelationshipSchemas,
		AllowedTypes:        job.EnabledTypes,
	})
	if err != nil {
		return nil, fmt.Errorf("run pipeline: %w", err)
	}

	// Create graph objects and relationships
	result, err := w.persistResults(ctx, job, pipelineOutput)
	if err != nil {
		return nil, fmt.Errorf("persist results: %w", err)
	}

	return result, nil
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
