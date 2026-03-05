package provider

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
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

// --- Organization Provider Configs ---

// GetOrgProviderConfig returns the config for a specific provider and org.
func (r *Repository) GetOrgProviderConfig(ctx context.Context, orgID string, provider ProviderType) (*OrgProviderConfig, error) {
	var cfg OrgProviderConfig
	err := r.db.NewSelect().
		Model(&cfg).
		Where("org_id = ?", orgID).
		Where("provider = ?", provider).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // not found is not an error here
		}
		r.log.Error("failed to get org provider config",
			logger.Error(err),
			slog.String("orgID", orgID),
			slog.String("provider", string(provider)),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &cfg, nil
}

// UpsertOrgProviderConfig inserts or updates an organization's provider config.
func (r *Repository) UpsertOrgProviderConfig(ctx context.Context, cfg *OrgProviderConfig) error {
	_, err := r.db.NewInsert().
		Model(cfg).
		On("CONFLICT (org_id, provider) DO UPDATE").
		Set("encrypted_credential = EXCLUDED.encrypted_credential").
		Set("encryption_nonce = EXCLUDED.encryption_nonce").
		Set("gcp_project = EXCLUDED.gcp_project").
		Set("location = EXCLUDED.location").
		Set("generative_model = EXCLUDED.generative_model").
		Set("embedding_model = EXCLUDED.embedding_model").
		Set("updated_at = NOW()").
		Returning("*").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to upsert org provider config",
			logger.Error(err),
			slog.String("orgID", cfg.OrgID),
			slog.String("provider", string(cfg.Provider)),
		)
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// DeleteOrgProviderConfig removes an organization's provider config.
func (r *Repository) DeleteOrgProviderConfig(ctx context.Context, orgID string, provider ProviderType) error {
	_, err := r.db.NewDelete().
		Model((*OrgProviderConfig)(nil)).
		Where("org_id = ?", orgID).
		Where("provider = ?", provider).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete org provider config",
			logger.Error(err),
			slog.String("orgID", orgID),
			slog.String("provider", string(provider)),
		)
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// ListOrgProviderConfigs lists all configs for an organization (metadata only, no secrets).
func (r *Repository) ListOrgProviderConfigs(ctx context.Context, orgID string) ([]OrgProviderConfig, error) {
	var cfgs []OrgProviderConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Column("id", "org_id", "provider", "gcp_project", "location", "generative_model", "embedding_model", "created_at", "updated_at").
		Where("org_id = ?", orgID).
		Order("provider ASC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to list org provider configs",
			logger.Error(err),
			slog.String("orgID", orgID),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return cfgs, nil
}

// --- Project Provider Configs ---

// GetProjectProviderConfig returns the config for a specific provider and project.
func (r *Repository) GetProjectProviderConfig(ctx context.Context, projectID string, provider ProviderType) (*ProjectProviderConfig, error) {
	var cfg ProjectProviderConfig
	err := r.db.NewSelect().
		Model(&cfg).
		Where("project_id = ?", projectID).
		Where("provider = ?", provider).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to get project provider config",
			logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("provider", string(provider)),
		)
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &cfg, nil
}

// UpsertProjectProviderConfig inserts or updates a project's provider config.
func (r *Repository) UpsertProjectProviderConfig(ctx context.Context, cfg *ProjectProviderConfig) error {
	_, err := r.db.NewInsert().
		Model(cfg).
		On("CONFLICT (project_id, provider) DO UPDATE").
		Set("encrypted_credential = EXCLUDED.encrypted_credential").
		Set("encryption_nonce = EXCLUDED.encryption_nonce").
		Set("gcp_project = EXCLUDED.gcp_project").
		Set("location = EXCLUDED.location").
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
		)
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// DeleteProjectProviderConfig removes a project's provider config.
func (r *Repository) DeleteProjectProviderConfig(ctx context.Context, projectID string, provider ProviderType) error {
	_, err := r.db.NewDelete().
		Model((*ProjectProviderConfig)(nil)).
		Where("project_id = ?", projectID).
		Where("provider = ?", provider).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete project provider config",
			logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("provider", string(provider)),
		)
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
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

// UsageSummaryRow is a single row from an aggregated usage query.
type UsageSummaryRow struct {
	Provider         ProviderType `bun:"provider"`
	Model            string       `bun:"model"`
	TotalText        int64        `bun:"total_text"`
	TotalImage       int64        `bun:"total_image"`
	TotalVideo       int64        `bun:"total_video"`
	TotalAudio       int64        `bun:"total_audio"`
	TotalOutput      int64        `bun:"total_output"`
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
