package provider

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Repository handles database operations for the provider domain.
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new provider repository.
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("provider.repo")),
	}
}

// --- Project Provider Configs ---

// GetProjectProviderConfigBySlug returns the config for a specific provider
// instance (slug) in a project. Returns nil, nil when absent.
func (r *Repository) GetProjectProviderConfigBySlug(ctx context.Context, projectID string, slug ProviderSlug) (*ProjectProviderConfig, error) {
	var cfg ProjectProviderConfig
	err := r.db.NewSelect().
		Model(&cfg).
		Where("project_id = ?", projectID).
		Where("slug = ?", slug).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to get project provider config by slug",
			logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("slug", string(slug)),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &cfg, nil
}

// GetProjectProviderConfig returns the default instance for a dialect in a
// project: the instance whose slug equals the dialect, otherwise the smallest
// slug of that dialect. Returns nil, nil when the project has no such instance.
//
// This preserves the dialect-addressed semantics of the previous
// (project, provider) lookup for callers that do not yet handle slugs.
func (r *Repository) GetProjectProviderConfig(ctx context.Context, projectID string, provider ProviderType) (*ProjectProviderConfig, error) {
	if cfg, err := r.GetProjectProviderConfigBySlug(ctx, projectID, ProviderSlug(provider)); err != nil {
		return nil, err
	} else if cfg != nil {
		return cfg, nil
	}

	var cfg ProjectProviderConfig
	err := r.db.NewSelect().
		Model(&cfg).
		Where("project_id = ?", projectID).
		Where("provider = ?", provider).
		Order("slug ASC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to get default project provider config",
			logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("provider", string(provider)),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &cfg, nil
}

// ListProjectProviderConfigsByDialect returns every instance of a dialect in a
// project, ordered by slug (default instance first when slug == dialect).
func (r *Repository) ListProjectProviderConfigsByDialect(ctx context.Context, projectID string, dialect ProviderDialect) ([]ProjectProviderConfig, error) {
	var cfgs []ProjectProviderConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Where("project_id = ?", projectID).
		Where("provider = ?", dialect).
		Order("slug ASC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list project provider configs by dialect",
			logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("dialect", string(dialect)),
		)
		return nil, err
	}
	return cfgs, nil
}

// ProjectProviderSlugs returns the set of instance slugs configured for a
// project. Used for slug validation and auto-suffix allocation.
func (r *Repository) ProjectProviderSlugs(ctx context.Context, projectID string) (map[ProviderSlug]bool, error) {
	var slugs []string
	err := r.db.NewSelect().
		TableExpr("kb.project_provider_configs").
		Column("slug").
		Where("project_id = ?", projectID).
		Scan(ctx, &slugs)
	if err != nil {
		r.log.Error("failed to list project provider slugs",
			logger.Error(err),
			slog.String("projectID", projectID),
		)
		return nil, err
	}
	out := make(map[ProviderSlug]bool, len(slugs))
	for _, s := range slugs {
		out[ProviderSlug(s)] = true
	}
	return out, nil
}

// UpsertProjectProviderConfig inserts or updates a project's provider instance.
// The identity is (project_id, slug).
func (r *Repository) UpsertProjectProviderConfig(ctx context.Context, cfg *ProjectProviderConfig) error {
	_, err := r.db.NewInsert().
		Model(cfg).
		On("CONFLICT (project_id, slug) DO UPDATE").
		Set("provider = EXCLUDED.provider").
		Set("encrypted_credential = EXCLUDED.encrypted_credential").
		Set("encryption_nonce = EXCLUDED.encryption_nonce").
		Set("gcp_project = EXCLUDED.gcp_project").
		Set("location = EXCLUDED.location").
		Set("base_url = EXCLUDED.base_url").
		Set("generative_model = EXCLUDED.generative_model").
		Set("embedding_model = EXCLUDED.embedding_model").
		Set("updated_at = NOW()").
		Returning("*").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to upsert project provider config",
			logger.Error(err),
			slog.String("projectID", cfg.ProjectID),
			slog.String("provider", string(cfg.Provider)),
			slog.String("slug", string(cfg.Slug)),
		)
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// InsertProjectProviderConfig inserts a new provider instance. Unlike
// UpsertProjectProviderConfig it does not merge on conflict, so a unique-slug
// violation surfaces to the caller (used for concurrency-safe auto-suffix
// allocation).
func (r *Repository) InsertProjectProviderConfig(ctx context.Context, cfg *ProjectProviderConfig) error {
	_, err := r.db.NewInsert().
		Model(cfg).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return err
	}
	return nil
}

// DeleteProjectProviderConfigBySlug removes a project's provider instance.
func (r *Repository) DeleteProjectProviderConfigBySlug(ctx context.Context, projectID string, slug ProviderSlug) error {
	_, err := r.db.NewDelete().
		Model((*ProjectProviderConfig)(nil)).
		Where("project_id = ?", projectID).
		Where("slug = ?", slug).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete project provider config by slug",
			logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("slug", string(slug)),
		)
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// DeleteProjectProviderConfig removes a project's default instance for a dialect.
func (r *Repository) DeleteProjectProviderConfig(ctx context.Context, projectID string, provider ProviderType) error {
	cfg, err := r.GetProjectProviderConfig(ctx, projectID, provider)
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	return r.DeleteProjectProviderConfigBySlug(ctx, projectID, cfg.Slug)
}

// ListProjectProviderConfigs lists all provider configs for a specific project (metadata only, no secrets).
func (r *Repository) ListProjectProviderConfigs(ctx context.Context, projectID string) ([]ProjectProviderConfig, error) {
	var cfgs []ProjectProviderConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Where("project_id = ?", projectID).
		Order("provider ASC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to list project provider configs",
			logger.Error(err),
			slog.String("projectID", projectID),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return cfgs, nil
}

// ListProjectProviderConfigsByProvider lists every project provider config of
// the given provider across all projects. Includes the encrypted credential
// columns (server-internal use — e.g. the catalog resync job that re-resolves
// credentials). Callers must never serialize these configs to clients.
func (r *Repository) ListProjectProviderConfigsByProvider(ctx context.Context, provider ProviderType) ([]ProjectProviderConfig, error) {
	var cfgs []ProjectProviderConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Where("provider = ?", provider).
		Order("project_id ASC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to list project provider configs by provider",
			logger.Error(err),
			slog.String("provider", string(provider)),
		)
		return nil, fmt.Errorf("list project provider configs by provider %s: %w", provider, err)
	}
	return cfgs, nil
}

// ListProjectProviderConfigsByOrg lists all project-level provider configs for
// projects belonging to the given organization (metadata only, no secrets).
func (r *Repository) ListProjectProviderConfigsByOrg(ctx context.Context, orgID string) ([]ProjectProviderConfig, error) {
	var cfgs []ProjectProviderConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Column("ppc.id", "ppc.project_id", "ppc.provider", "ppc.gcp_project", "ppc.location", "ppc.generative_model", "ppc.embedding_model", "ppc.created_at", "ppc.updated_at").
		Join("JOIN kb.projects AS p ON p.id = ppc.project_id").
		Where("p.organization_id = ?", orgID).
		Order("ppc.provider ASC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to list project provider configs by org",
			logger.Error(err),
			slog.String("orgID", orgID),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return cfgs, nil
}

// --- Provider Supported Models ---

// ListSupportedModels returns all cached supported models for a provider, optionally filtered by type.
func (r *Repository) ListSupportedModels(ctx context.Context, provider ProviderType, modelType *ModelType) ([]ProviderSupportedModel, error) {
	var models []ProviderSupportedModel
	q := r.db.NewSelect().
		Model(&models).
		Where("provider = ?", provider).
		Order("model_name ASC")

	if modelType != nil {
		q = q.Where("model_type = ?", *modelType)
	}

	if err := q.Scan(ctx); err != nil {
		r.log.Error("failed to list supported models",
			logger.Error(err),
			slog.String("provider", string(provider)),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return models, nil
}

// ListAllSupportedModels returns all cached supported models across all providers, optionally filtered by type.
func (r *Repository) ListAllSupportedModels(ctx context.Context, modelType *ModelType) ([]ProviderSupportedModel, error) {
	var models []ProviderSupportedModel
	q := r.db.NewSelect().
		Model(&models).
		Order("provider ASC", "model_name ASC")

	if modelType != nil {
		q = q.Where("model_type = ?", *modelType)
	}

	if err := q.Scan(ctx); err != nil {
		r.log.Error("failed to list all supported models", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return models, nil
}

// UpsertSupportedModels bulk upserts supported models for a provider.
func (r *Repository) UpsertSupportedModels(ctx context.Context, models []ProviderSupportedModel) error {
	if len(models) == 0 {
		return nil
	}

	_, err := r.db.NewInsert().
		Model(&models).
		On("CONFLICT (provider, model_name) DO UPDATE").
		Set("model_type = EXCLUDED.model_type").
		Set("display_name = EXCLUDED.display_name").
		Set("max_input_tokens = COALESCE(EXCLUDED.max_input_tokens, psm.max_input_tokens)").
		Set("max_output_tokens = COALESCE(EXCLUDED.max_output_tokens, psm.max_output_tokens)").
		Set("last_synced = NOW()").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to upsert supported models", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// DeleteSupportedModelsNotIn removes rows for a provider whose model_name is
// not in the provided list. Used after a successful sync to prune stale entries
// (retired models, renamed models, leftover static-fallback rows).
func (r *Repository) DeleteSupportedModelsNotIn(ctx context.Context, provider ProviderType, modelNames []string) error {
	if len(modelNames) == 0 {
		return nil
	}

	_, err := r.db.NewDelete().
		TableExpr("kb.provider_supported_models").
		Where("provider = ?", string(provider)).
		Where("model_name NOT IN (?)", bun.In(modelNames)).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete stale supported models", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// UpdateModelLimits bulk-updates max_output_tokens and max_input_tokens in
// provider_supported_models by model name (provider-agnostic).
func (r *Repository) UpdateModelLimits(ctx context.Context, limits map[string]modelLimits) error {
	if len(limits) == 0 {
		return nil
	}

	for modelName, lim := range limits {
		out := lim.MaxOutputTokens
		in := lim.MaxInputTokens
		_, err := r.db.NewUpdate().
			TableExpr("kb.provider_supported_models").
			Set("max_output_tokens = ?", out).
			Set("max_input_tokens = ?", in).
			Where("model_name = ?", modelName).
			Exec(ctx)
		if err != nil {
			r.log.Error("failed to update model limits",
				logger.Error(err),
				slog.String("model", modelName),
			)
			return apperror.ErrDatabase.WithInternal(err)
		}
	}
	return nil
}

// GetModelOutputLimit returns the max_output_tokens for a given model name,
// or 0 if not found / not set.
func (r *Repository) GetModelOutputLimit(ctx context.Context, modelName string) (int, error) {
	var limit int
	err := r.db.NewSelect().
		TableExpr("kb.provider_supported_models").
		ColumnExpr("max_output_tokens").
		Where("model_name = ?", modelName).
		Where("max_output_tokens IS NOT NULL").
		OrderExpr("max_output_tokens DESC").
		Limit(1).
		Scan(ctx, &limit)

	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		r.log.Error("failed to get model output limit",
			logger.Error(err),
			slog.String("model", modelName),
		)
		return 0, apperror.ErrDatabase.WithInternal(err)
	}
	return limit, nil
}

// GetModelInputLimit returns the max_input_tokens (context window) for a given
// model name, or 0 if not found / not set.
func (r *Repository) GetModelInputLimit(ctx context.Context, modelName string) (int, error) {
	var limit int
	err := r.db.NewSelect().
		TableExpr("kb.provider_supported_models").
		ColumnExpr("max_input_tokens").
		Where("model_name = ?", modelName).
		Where("max_input_tokens IS NOT NULL").
		OrderExpr("max_input_tokens DESC").
		Limit(1).
		Scan(ctx, &limit)

	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		r.log.Error("failed to get model input limit",
			logger.Error(err),
			slog.String("model", modelName),
		)
		return 0, apperror.ErrDatabase.WithInternal(err)
	}
	return limit, nil
}

// --- LLM Usage Events ---

// InsertUsageEvent records a single LLM usage event.
func (r *Repository) InsertUsageEvent(ctx context.Context, event *LLMUsageEvent) error {
	_, err := r.db.NewInsert().
		Model(event).
		Returning("id").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to insert usage event", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// --- Provider Pricing ---

// GetPricing returns the pricing for a specific provider and model.
func (r *Repository) GetPricing(ctx context.Context, provider ProviderType, model string) (*ProviderPricing, error) {
	var pricing ProviderPricing
	err := r.db.NewSelect().
		Model(&pricing).
		Where("provider = ?", provider).
		Where("model = ?", model).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to get pricing",
			logger.Error(err),
			slog.String("provider", string(provider)),
			slog.String("model", model),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &pricing, nil
}

// GetPricingByModel returns the global pricing for a model name, searching
// across all providers. When multiple providers carry the same model name the
// first match (ordered by provider) is returned. Returns nil when not found.
func (r *Repository) GetPricingByModel(ctx context.Context, model string) (*ProviderPricing, error) {
	var pricing ProviderPricing
	err := r.db.NewSelect().
		Model(&pricing).
		Where("model = ?", model).
		OrderExpr("provider ASC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to get pricing by model",
			logger.Error(err),
			slog.String("model", model),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &pricing, nil
}

// ListPricing returns all global retail pricing rows, ordered by provider then
// model. Returns an empty (non-nil) slice when no rows exist.
func (r *Repository) ListPricing(ctx context.Context) ([]ProviderPricing, error) {
	var rows []ProviderPricing
	err := r.db.NewSelect().
		Model(&rows).
		OrderExpr("provider ASC, model ASC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to list pricing", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if rows == nil {
		rows = []ProviderPricing{}
	}
	return rows, nil
}

// UpsertPricing bulk upserts global pricing entries.
func (r *Repository) UpsertPricing(ctx context.Context, entries []ProviderPricing) error {
	if len(entries) == 0 {
		return nil
	}

	_, err := r.db.NewInsert().
		Model(&entries).
		On("CONFLICT (provider, model) DO UPDATE").
		Set("text_input_price = EXCLUDED.text_input_price").
		Set("image_input_price = EXCLUDED.image_input_price").
		Set("video_input_price = EXCLUDED.video_input_price").
		Set("audio_input_price = EXCLUDED.audio_input_price").
		Set("output_price = EXCLUDED.output_price").
		Set("last_synced = NOW()").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to upsert pricing", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// --- Organization Custom Pricing ---

// GetOrgCustomPricing returns the custom pricing for a specific org, provider, and model.
func (r *Repository) GetOrgCustomPricing(ctx context.Context, orgID string, provider ProviderType, model string) (*OrganizationCustomPricing, error) {
	var pricing OrganizationCustomPricing
	err := r.db.NewSelect().
		Model(&pricing).
		Where("org_id = ?", orgID).
		Where("provider = ?", provider).
		Where("model = ?", model).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to get org custom pricing",
			logger.Error(err),
			slog.String("orgID", orgID),
			slog.String("provider", string(provider)),
			slog.String("model", model),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &pricing, nil
}

// --- Project Custom Pricing ---

// GetProjectCustomPricing returns the custom pricing for a specific project,
// provider instance (slug), and model. Returns (nil, nil) when no override exists.
func (r *Repository) GetProjectCustomPricing(ctx context.Context, projectID string, providerSlug string, model string) (*ProjectCustomPricing, error) {
	var pricing ProjectCustomPricing
	err := r.db.NewSelect().
		Model(&pricing).
		Where("project_id = ?", projectID).
		Where("provider_slug = ?", providerSlug).
		Where("model = ?", model).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to get project custom pricing",
			logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("providerSlug", providerSlug),
			slog.String("model", model),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &pricing, nil
}

// ListProjectCustomPricing lists all custom pricing overrides for a project,
// ordered by provider then model.
func (r *Repository) ListProjectCustomPricing(ctx context.Context, projectID string) ([]ProjectCustomPricing, error) {
	var entries []ProjectCustomPricing
	err := r.db.NewSelect().
		Model(&entries).
		Where("project_id = ?", projectID).
		Order("provider_slug ASC", "model ASC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to list project custom pricing",
			logger.Error(err),
			slog.String("projectID", projectID),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return entries, nil
}

// UpsertProjectCustomPricing inserts or updates a project's pricing override.
// The override is keyed by (project_id, provider_slug, model).
func (r *Repository) UpsertProjectCustomPricing(ctx context.Context, entry *ProjectCustomPricing) error {
	// Default the instance slug to the dialect when the caller only supplied a
	// dialect — the dialect's default instance is the pre-instance identity.
	if entry.ProviderSlug == "" {
		entry.ProviderSlug = ProviderSlug(entry.Provider)
	}
	_, err := r.db.NewInsert().
		Model(entry).
		On("CONFLICT (project_id, provider_slug, model) DO UPDATE").
		Set("text_input_price = EXCLUDED.text_input_price").
		Set("image_input_price = EXCLUDED.image_input_price").
		Set("video_input_price = EXCLUDED.video_input_price").
		Set("audio_input_price = EXCLUDED.audio_input_price").
		Set("output_price = EXCLUDED.output_price").
		Set("updated_at = NOW()").
		Returning("*").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to upsert project custom pricing",
			logger.Error(err),
			slog.String("projectID", entry.ProjectID),
			slog.String("provider", string(entry.Provider)),
			slog.String("model", entry.Model),
		)
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// DeleteProjectCustomPricing removes a project's pricing override for the given
// provider instance (slug) and model. Deleting a non-existent override is a no-op.
func (r *Repository) DeleteProjectCustomPricing(ctx context.Context, projectID string, providerSlug string, model string) error {
	_, err := r.db.NewDelete().
		Model((*ProjectCustomPricing)(nil)).
		Where("project_id = ?", projectID).
		Where("provider_slug = ?", providerSlug).
		Where("model = ?", model).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete project custom pricing",
			logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("providerSlug", providerSlug),
			slog.String("model", model),
		)
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// UsageSummaryRow is a single row from an aggregated usage query.
type UsageSummaryRow struct {
	Provider         ProviderType `bun:"provider"`
	Model            string       `bun:"model"`
	TotalText        int64        `bun:"total_text"`
	TotalImage       int64        `bun:"total_image"`
	TotalVideo       int64        `bun:"total_video"`
	TotalAudio       int64        `bun:"total_audio"`
	TotalOutput      int64        `bun:"total_output"`
	TotalCached      int64        `bun:"total_cached"`
	EstimatedCostUSD float64      `bun:"estimated_cost_usd"`
}

// GetProjectUsageSummary returns aggregated usage for a project grouped by provider + model.
func (r *Repository) GetProjectUsageSummary(ctx context.Context, projectID string, since, until *time.Time) ([]UsageSummaryRow, error) {
	var rows []UsageSummaryRow
	q := r.db.NewSelect().
		TableExpr("kb.llm_usage_events").
		ColumnExpr("provider, model").
		ColumnExpr("SUM(text_input_tokens) AS total_text").
		ColumnExpr("SUM(image_input_tokens) AS total_image").
		ColumnExpr("SUM(video_input_tokens) AS total_video").
		ColumnExpr("SUM(audio_input_tokens) AS total_audio").
		ColumnExpr("SUM(output_tokens) AS total_output").
		ColumnExpr("SUM(cached_tokens) AS total_cached").
		ColumnExpr("SUM(estimated_cost_usd) AS estimated_cost_usd").
		Where("project_id = ?", projectID).
		GroupExpr("provider, model").
		OrderExpr("provider, model")

	if since != nil {
		q = q.Where("created_at >= ?", *since)
	}
	if until != nil {
		q = q.Where("created_at <= ?", *until)
	}

	if err := q.Scan(ctx, &rows); err != nil {
		r.log.Error("failed to get project usage summary", logger.Error(err), slog.String("projectID", projectID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return rows, nil
}

// GetOrgUsageSummary returns aggregated usage across all projects in an org, grouped by provider + model.
func (r *Repository) GetOrgUsageSummary(ctx context.Context, orgID string, since, until *time.Time) ([]UsageSummaryRow, error) {
	var rows []UsageSummaryRow
	q := r.db.NewSelect().
		TableExpr("kb.llm_usage_events").
		ColumnExpr("provider, model").
		ColumnExpr("SUM(text_input_tokens) AS total_text").
		ColumnExpr("SUM(image_input_tokens) AS total_image").
		ColumnExpr("SUM(video_input_tokens) AS total_video").
		ColumnExpr("SUM(audio_input_tokens) AS total_audio").
		ColumnExpr("SUM(output_tokens) AS total_output").
		ColumnExpr("SUM(cached_tokens) AS total_cached").
		ColumnExpr("SUM(estimated_cost_usd) AS estimated_cost_usd").
		Where("org_id = ?", orgID).
		GroupExpr("provider, model").
		OrderExpr("provider, model")

	if since != nil {
		q = q.Where("created_at >= ?", *since)
	}
	if until != nil {
		q = q.Where("created_at <= ?", *until)
	}

	if err := q.Scan(ctx, &rows); err != nil {
		r.log.Error("failed to get org usage summary", logger.Error(err), slog.String("orgID", orgID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return rows, nil
}

// UsageTimeSeriesRow is a single row from a time-bucketed usage query.
type UsageTimeSeriesRow struct {
	Period           time.Time    `bun:"period"`
	Provider         ProviderType `bun:"provider"`
	Model            string       `bun:"model"`
	TotalText        int64        `bun:"total_text"`
	TotalImage       int64        `bun:"total_image"`
	TotalVideo       int64        `bun:"total_video"`
	TotalAudio       int64        `bun:"total_audio"`
	TotalOutput      int64        `bun:"total_output"`
	TotalCached      int64        `bun:"total_cached"`
	EstimatedCostUSD float64      `bun:"estimated_cost_usd"`
}

// OrgUsageByProjectRow is a single row from an org-wide usage query grouped by project.
type OrgUsageByProjectRow struct {
	ProjectID        string  `bun:"project_id"`
	ProjectName      string  `bun:"project_name"`
	TotalText        int64   `bun:"total_text"`
	TotalImage       int64   `bun:"total_image"`
	TotalVideo       int64   `bun:"total_video"`
	TotalAudio       int64   `bun:"total_audio"`
	TotalOutput      int64   `bun:"total_output"`
	TotalCached      int64   `bun:"total_cached"`
	EstimatedCostUSD float64 `bun:"estimated_cost_usd"`
}

// GetProjectUsageTimeSeries returns time-bucketed usage for a project.
// granularity must be "day", "week", or "month".
func (r *Repository) GetProjectUsageTimeSeries(ctx context.Context, projectID, granularity string, since, until *time.Time) ([]UsageTimeSeriesRow, error) {
	g := sanitizeGranularity(granularity)
	var rows []UsageTimeSeriesRow
	q := r.db.NewSelect().
		TableExpr("kb.llm_usage_events").
		ColumnExpr("DATE_TRUNC(?, created_at AT TIME ZONE 'UTC') AS period", g).
		ColumnExpr("provider, model").
		ColumnExpr("SUM(text_input_tokens) AS total_text").
		ColumnExpr("SUM(image_input_tokens) AS total_image").
		ColumnExpr("SUM(video_input_tokens) AS total_video").
		ColumnExpr("SUM(audio_input_tokens) AS total_audio").
		ColumnExpr("SUM(output_tokens) AS total_output").
		ColumnExpr("SUM(cached_tokens) AS total_cached").
		ColumnExpr("SUM(estimated_cost_usd) AS estimated_cost_usd").
		Where("project_id = ?", projectID).
		GroupExpr("period, provider, model").
		OrderExpr("period ASC, provider, model")

	if since != nil {
		q = q.Where("created_at >= ?", *since)
	}
	if until != nil {
		q = q.Where("created_at <= ?", *until)
	}

	if err := q.Scan(ctx, &rows); err != nil {
		r.log.Error("failed to get project usage timeseries", logger.Error(err), slog.String("projectID", projectID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return rows, nil
}

// GetOrgUsageTimeSeries returns time-bucketed usage for all projects in an org.
// granularity must be "day", "week", or "month".
func (r *Repository) GetOrgUsageTimeSeries(ctx context.Context, orgID, granularity string, since, until *time.Time) ([]UsageTimeSeriesRow, error) {
	g := sanitizeGranularity(granularity)
	var rows []UsageTimeSeriesRow
	q := r.db.NewSelect().
		TableExpr("kb.llm_usage_events").
		ColumnExpr("DATE_TRUNC(?, created_at AT TIME ZONE 'UTC') AS period", g).
		ColumnExpr("provider, model").
		ColumnExpr("SUM(text_input_tokens) AS total_text").
		ColumnExpr("SUM(image_input_tokens) AS total_image").
		ColumnExpr("SUM(video_input_tokens) AS total_video").
		ColumnExpr("SUM(audio_input_tokens) AS total_audio").
		ColumnExpr("SUM(output_tokens) AS total_output").
		ColumnExpr("SUM(cached_tokens) AS total_cached").
		ColumnExpr("SUM(estimated_cost_usd) AS estimated_cost_usd").
		Where("org_id = ?", orgID).
		GroupExpr("period, provider, model").
		OrderExpr("period ASC, provider, model")

	if since != nil {
		q = q.Where("created_at >= ?", *since)
	}
	if until != nil {
		q = q.Where("created_at <= ?", *until)
	}

	if err := q.Scan(ctx, &rows); err != nil {
		r.log.Error("failed to get org usage timeseries", logger.Error(err), slog.String("orgID", orgID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return rows, nil
}

// GetOrgUsageByProject returns aggregated usage for an org grouped by project.
func (r *Repository) GetOrgUsageByProject(ctx context.Context, orgID string, since, until *time.Time) ([]OrgUsageByProjectRow, error) {
	var rows []OrgUsageByProjectRow
	q := r.db.NewSelect().
		TableExpr("kb.llm_usage_events AS e").
		Join("JOIN kb.projects AS p ON p.id = e.project_id").
		ColumnExpr("e.project_id").
		ColumnExpr("p.name AS project_name").
		ColumnExpr("SUM(e.text_input_tokens) AS total_text").
		ColumnExpr("SUM(e.image_input_tokens) AS total_image").
		ColumnExpr("SUM(e.video_input_tokens) AS total_video").
		ColumnExpr("SUM(e.audio_input_tokens) AS total_audio").
		ColumnExpr("SUM(e.output_tokens) AS total_output").
		ColumnExpr("SUM(e.cached_tokens) AS total_cached").
		ColumnExpr("SUM(e.estimated_cost_usd) AS estimated_cost_usd").
		Where("e.org_id = ?", orgID).
		GroupExpr("e.project_id, p.name").
		OrderExpr("estimated_cost_usd DESC")

	if since != nil {
		q = q.Where("e.created_at >= ?", *since)
	}
	if until != nil {
		q = q.Where("e.created_at <= ?", *until)
	}

	if err := q.Scan(ctx, &rows); err != nil {
		r.log.Error("failed to get org usage by project", logger.Error(err), slog.String("orgID", orgID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return rows, nil
}

// GetProjectCurrentMonthSpend returns the total estimated cost for a project
// for the current calendar month (UTC).
func (r *Repository) GetProjectCurrentMonthSpend(ctx context.Context, projectID string) (float64, error) {
	var total float64
	err := r.db.NewSelect().
		TableExpr("kb.llm_usage_events").
		ColumnExpr("COALESCE(SUM(estimated_cost_usd), 0)").
		Where("project_id = ?", projectID).
		Where("DATE_TRUNC('month', created_at AT TIME ZONE 'UTC') = DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')").
		Scan(ctx, &total)

	if err != nil {
		r.log.Error("failed to get project current month spend", logger.Error(err), slog.String("projectID", projectID))
		return 0, apperror.ErrDatabase.WithInternal(err)
	}
	return total, nil
}

// sanitizeGranularity validates and normalises a DATE_TRUNC granularity string.
// Falls back to "day" for unrecognised values to prevent SQL injection.
func sanitizeGranularity(g string) string {
	switch g {
	case "week", "month":
		return g
	default:
		return "day"
	}
}

// IsUserOrgMember reports whether the given user is a member of the given
// organization, consulting kb.organization_memberships. It is the authoritative
// source for org-scoped authorization (issue #841): the caller's org must be
// derived from real membership, never from a request-controlled path parameter.
func (r *Repository) IsUserOrgMember(ctx context.Context, orgID, userID string) (bool, error) {
	exists, err := r.db.NewSelect().
		TableExpr("kb.organization_memberships").
		Where("organization_id = ?", orgID).
		Where("user_id = ?", userID).
		Exists(ctx)

	if err != nil {
		r.log.Error("failed to check org membership",
			logger.Error(err),
			slog.String("orgID", orgID),
			slog.String("userID", userID))
		return false, apperror.ErrDatabase.WithInternal(err)
	}
	return exists, nil
}

// GetOrgIDForProject looks up the organization ID for a given project.
func (r *Repository) GetOrgIDForProject(ctx context.Context, projectID string) (string, error) {
	var orgID string
	err := r.db.NewSelect().
		TableExpr("kb.projects").
		Column("organization_id").
		Where("id = ?", projectID).
		Scan(ctx, &orgID)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", apperror.ErrNotFound.WithMessage("project not found")
		}
		r.log.Error("failed to get org ID for project",
			logger.Error(err),
			slog.String("projectID", projectID),
		)
		return "", apperror.ErrDatabase.WithInternal(err)
	}
	return orgID, nil
}

// GetUserUsageSummary returns aggregated usage for a specific user grouped by provider + model.
func (r *Repository) GetUserUsageSummary(ctx context.Context, userID string, since, until *time.Time) ([]UsageSummaryRow, error) {
	var rows []UsageSummaryRow
	q := r.db.NewSelect().
		TableExpr("kb.llm_usage_events").
		ColumnExpr("provider, model").
		ColumnExpr("SUM(text_input_tokens) AS total_text").
		ColumnExpr("SUM(image_input_tokens) AS total_image").
		ColumnExpr("SUM(video_input_tokens) AS total_video").
		ColumnExpr("SUM(audio_input_tokens) AS total_audio").
		ColumnExpr("SUM(output_tokens) AS total_output").
		ColumnExpr("SUM(cached_tokens) AS total_cached").
		ColumnExpr("SUM(estimated_cost_usd) AS estimated_cost_usd").
		Where("user_id = ?", userID).
		GroupExpr("provider, model").
		OrderExpr("provider, model")

	if since != nil {
		q = q.Where("created_at >= ?", *since)
	}
	if until != nil {
		q = q.Where("created_at <= ?", *until)
	}

	if err := q.Scan(ctx, &rows); err != nil {
		r.log.Error("failed to get user usage summary", logger.Error(err), slog.String("userID", userID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return rows, nil
}

// GetUserUsageTimeSeries returns time-bucketed usage for a specific user.
// granularity must be "day", "week", or "month".
func (r *Repository) GetUserUsageTimeSeries(ctx context.Context, userID, granularity string, since, until *time.Time) ([]UsageTimeSeriesRow, error) {
	g := sanitizeGranularity(granularity)
	var rows []UsageTimeSeriesRow
	q := r.db.NewSelect().
		TableExpr("kb.llm_usage_events").
		ColumnExpr("DATE_TRUNC(?, created_at AT TIME ZONE 'UTC') AS period", g).
		ColumnExpr("provider, model").
		ColumnExpr("SUM(text_input_tokens) AS total_text").
		ColumnExpr("SUM(image_input_tokens) AS total_image").
		ColumnExpr("SUM(video_input_tokens) AS total_video").
		ColumnExpr("SUM(audio_input_tokens) AS total_audio").
		ColumnExpr("SUM(output_tokens) AS total_output").
		ColumnExpr("SUM(cached_tokens) AS total_cached").
		ColumnExpr("SUM(estimated_cost_usd) AS estimated_cost_usd").
		Where("user_id = ?", userID).
		GroupExpr("period, provider, model").
		OrderExpr("period ASC, provider, model")

	if since != nil {
		q = q.Where("created_at >= ?", *since)
	}
	if until != nil {
		q = q.Where("created_at <= ?", *until)
	}

	if err := q.Scan(ctx, &rows); err != nil {
		r.log.Error("failed to get user usage timeseries", logger.Error(err), slog.String("userID", userID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return rows, nil
}
