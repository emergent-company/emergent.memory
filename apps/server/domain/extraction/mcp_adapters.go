package extraction

import (
	"context"
	"fmt"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"log/slog"
)

// ============================================================================
// DomainClassifierMCPAdapter
// Satisfies mcp.DomainClassifierHandler
// ============================================================================

// DomainClassifierMCPAdapter wraps DocumentClassifier + dependencies to satisfy
// the mcp.DomainClassifierHandler interface.
type DomainClassifierMCPAdapter struct {
	classifier     *DocumentClassifier
	schemaProvider *MemorySchemaProvider
	docService     *documents.Service
	log            *slog.Logger
}

// NewDomainClassifierMCPAdapter creates a new adapter.
func NewDomainClassifierMCPAdapter(
	modelFactory *adk.ModelFactory,
	schemaProvider *MemorySchemaProvider,
	docService *documents.Service,
	log *slog.Logger,
) *DomainClassifierMCPAdapter {
	return &DomainClassifierMCPAdapter{
		classifier:     NewDocumentClassifier(modelFactory, log),
		schemaProvider: schemaProvider,
		docService:     docService,
		log:            log,
	}
}

// ClassifyDocument fetches the document content, loads installed schemas, and
// runs the two-stage classifier, returning an mcp.ClassificationSnapshot.
func (a *DomainClassifierMCPAdapter) ClassifyDocument(ctx context.Context, projectID, documentID string) (mcp.ClassificationSnapshot, error) {
	doc, err := a.docService.GetByID(ctx, projectID, documentID)
	if err != nil {
		return mcp.ClassificationSnapshot{}, fmt.Errorf("fetch document: %w", err)
	}
	content := ""
	if doc.Content != nil {
		content = *doc.Content
	}

	schemas, err := a.schemaProvider.GetInstalledSchemaSummaries(ctx, projectID)
	if err != nil {
		return mcp.ClassificationSnapshot{}, fmt.Errorf("load schemas: %w", err)
	}

	result, err := a.classifier.Classify(ctx, content, schemas)
	if err != nil {
		return mcp.ClassificationSnapshot{}, fmt.Errorf("classify: %w", err)
	}

	stage := "heuristic"
	if len(result.Signals.HeuristicKeywords) == 0 {
		stage = "llm"
	}
	// No domain matched — signal new_domain so the agent triggers discovery.
	if result.DomainName == "" {
		stage = "new_domain"
	}

	snap := mcp.ClassificationSnapshot{
		SchemaID:   result.MatchedSchemaID,
		Label:      result.DomainName,
		Confidence: result.Confidence,
		Stage:      stage,
		LLMReason:  result.Signals.LLMReason,
	}
	return snap, nil
}

// ============================================================================
// SchemaIndexMCPAdapter
// Satisfies mcp.SchemaIndexHandler
// ============================================================================

// SchemaIndexMCPAdapter wraps MemorySchemaProvider to satisfy mcp.SchemaIndexHandler.
type SchemaIndexMCPAdapter struct {
	provider *MemorySchemaProvider
}

// NewSchemaIndexMCPAdapter creates a new adapter.
func NewSchemaIndexMCPAdapter(provider *MemorySchemaProvider) *SchemaIndexMCPAdapter {
	return &SchemaIndexMCPAdapter{provider: provider}
}

// ListInstalledSchemas returns lightweight schema summaries for a project.
func (a *SchemaIndexMCPAdapter) ListInstalledSchemas(ctx context.Context, projectID string) ([]mcp.SchemaIndexEntry, error) {
	summaries, err := a.provider.GetInstalledSchemaSummaries(ctx, projectID)
	if err != nil {
		return nil, err
	}

	entries := make([]mcp.SchemaIndexEntry, len(summaries))
	for i, s := range summaries {
		entries[i] = mcp.SchemaIndexEntry{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Keywords:    s.Keywords,
		}
	}
	return entries, nil
}

// ============================================================================
// ReextractionQueuerMCPAdapter
// Satisfies mcp.ReextractionQueuer
// ============================================================================

// ReextractionQueuerMCPAdapter wraps ObjectExtractionJobsService.
type ReextractionQueuerMCPAdapter struct {
	jobsSvc *ObjectExtractionJobsService
}

// NewReextractionQueuerMCPAdapter creates a new adapter.
func NewReextractionQueuerMCPAdapter(jobsSvc *ObjectExtractionJobsService) *ReextractionQueuerMCPAdapter {
	return &ReextractionQueuerMCPAdapter{jobsSvc: jobsSvc}
}

// QueueReextraction creates a reextraction job for the given document.
func (a *ReextractionQueuerMCPAdapter) QueueReextraction(ctx context.Context, projectID, documentID, _ string) (string, error) {
	job, err := a.jobsSvc.CreateJob(ctx, CreateObjectExtractionJobOptions{
		ProjectID:  projectID,
		DocumentID: &documentID,
		JobType:    JobTypeReextraction,
		SourceType: strPtr("document"),
		SourceID:   &documentID,
	})
	if err != nil {
		return "", fmt.Errorf("create reextraction job: %w", err)
	}
	return job.ID, nil
}

func strPtr(s string) *string { return &s }
