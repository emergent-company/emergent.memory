package extraction

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// EmbeddingUsageRecorder records embedding usage events asynchronously.
// Implemented by provider.UsageService.
type EmbeddingUsageRecorder interface {
	RecordAsync(event *provider.LLMUsageEvent)
}

// BudgetChecker checks whether a project has exceeded its monthly budget.
// Implemented by provider.UsageService.
type BudgetChecker interface {
	CheckBudgetExceeded(ctx context.Context, projectID string) (bool, error)
}

// BudgetExceededError is returned when a project has exceeded its budget.
type EmbeddingBudgetExceededError struct {
	ProjectID string
}

func (e *EmbeddingBudgetExceededError) Error() string {
	return fmt.Sprintf("project %s has exceeded its monthly spending budget", e.ProjectID)
}

// orgIDCache provides a simple in-memory cache for projectID → orgID lookups
// to avoid hitting the database for every embedding job.
type orgIDCache struct {
	mu    sync.RWMutex
	cache map[string]string // projectID → orgID
	db    bun.IDB
	log   *slog.Logger
}

func newOrgIDCache(db bun.IDB, log *slog.Logger) *orgIDCache {
	return &orgIDCache{
		cache: make(map[string]string),
		db:    db,
		log:   log.With(logger.Scope("org_id_cache")),
	}
}

// resolve returns the orgID for the given projectID. Results are cached.
// Returns "" if the project is not found or query fails.
func (c *orgIDCache) resolve(ctx context.Context, projectID string) string {
	if projectID == "" {
		return ""
	}

	c.mu.RLock()
	if orgID, ok := c.cache[projectID]; ok {
		c.mu.RUnlock()
		return orgID
	}
	c.mu.RUnlock()

	// Look up in database
	var result struct {
		OrgID string `bun:"organization_id"`
	}
	err := c.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("organization_id").
		Where("id = ?", projectID).
		Scan(ctx, &result)
	if err != nil {
		c.log.Warn("failed to resolve org_id for project",
			slog.String("project_id", projectID),
			slog.String("error", err.Error()),
		)
		return ""
	}

	c.mu.Lock()
	c.cache[projectID] = result.OrgID
	c.mu.Unlock()

	return result.OrgID
}

// recordEmbeddingUsage is a helper that creates and records an embedding usage event.
// It is safe to call with nil recorder (no-op).
func recordEmbeddingUsage(
	recorder EmbeddingUsageRecorder,
	projectID string,
	orgID string,
	result *vertex.EmbedResult,
) {
	if recorder == nil || projectID == "" || orgID == "" {
		return
	}
	if result == nil || result.Usage == nil {
		return
	}

	providerType := provider.ProviderType(result.Provider)
	// Map our provider strings to the ProviderType enum
	switch result.Provider {
	case "vertex":
		providerType = provider.ProviderVertexAI
	case "googleai":
		providerType = provider.ProviderGoogleAI
	default:
		providerType = provider.ProviderGoogleAI // safe default
	}

	recorder.RecordAsync(&provider.LLMUsageEvent{
		ProjectID:       projectID,
		OrgID:           orgID,
		Provider:        providerType,
		Model:           result.Model,
		Operation:       provider.OperationEmbed,
		TextInputTokens: int64(result.Usage.PromptTokens),
	})
}
