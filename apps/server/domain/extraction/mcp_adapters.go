package extraction

import (
	"context"
	"fmt"
	"strings"

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
		classifier:     NewDocumentClassifier(modelFactory, nil, log),
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
	// Provide a short excerpt so the agent can determine document format/type
	// when naming a new schema pack (without needing to fetch the document separately).
	if content != "" {
		excerpt := content
		if len(excerpt) > 500 {
			excerpt = excerpt[:500]
		}
		snap.DocumentExcerpt = excerpt
	}
	// When no schema matched, suggest a pack_name derived from the first line of content.
	// This gives the agent a concrete starting point and reduces generic "new_domain" responses.
	if stage == "new_domain" && content != "" {
		snap.SuggestedPackName = suggestPackName(content)
	}
	return snap, nil
}

// suggestPackName derives a short descriptive name from the first meaningful line of content.
func suggestPackName(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		// Strip bracketed title like "[AI Assistant Session - 2026-05-10 14:32 UTC]"
		if strings.HasPrefix(line, "[") {
			if end := strings.Index(line, "]"); end > 0 {
				inner := strings.TrimSpace(line[1:end])
				// Remove trailing date/time like " - 2026-05-10 14:32 UTC"
				if idx := strings.Index(inner, " - 20"); idx > 0 {
					inner = strings.TrimSpace(inner[:idx])
				}
				if len(inner) >= 3 {
					return inner
				}
			}
		}
		// Strip markdown/list prefixes
		line = strings.TrimLeft(line, "#-*")
		line = strings.TrimSpace(line)
		// Strip trailing date suffix like " - May 2026" or " - 2026-05-10"
		for _, sep := range []string{" - 20", " – 20", " - May ", " - Jan ", " - Feb ", " - Mar ", " - Apr ", " - Jun ", " - Jul ", " - Aug ", " - Sep ", " - Oct ", " - Nov ", " - Dec "} {
			if idx := strings.Index(line, sep); idx > 0 {
				line = strings.TrimSpace(line[:idx])
				break
			}
		}
		if len(line) >= 3 && len(line) <= 60 {
			return line
		}
	}
	return ""
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
