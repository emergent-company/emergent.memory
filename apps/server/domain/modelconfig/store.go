package modelconfig

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Store handles database operations for model config.
type Store struct {
	db  bun.IDB
	log *slog.Logger
}

// NewStore creates a new Store.
func NewStore(db bun.IDB, log *slog.Logger) *Store {
	return &Store{db: db, log: log}
}

// GetProjectModelConfig returns the model config for a project, or nil if not set.
func (s *Store) GetProjectModelConfig(ctx context.Context, projectID uuid.UUID) (*ProjectModelConfig, error) {
	cfg := &ProjectModelConfig{}
	err := s.db.NewSelect().
		Model(cfg).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return cfg, nil
}

// UpsertProjectModelConfig applies a partial update to a project's model config
// and returns the stored row.
//
// A field the patch does not name is never written, so two concurrent partial
// updates to different fields cannot resurrect each other's stale copy: the
// INSERT only creates a missing row, and the UPDATE names only the supplied
// columns (#708). Both statements run in one transaction so the returned row is
// the one this call left behind.
func (s *Store) UpsertProjectModelConfig(ctx context.Context, projectID uuid.UUID, patch ProjectModelConfigPatch) (*ProjectModelConfig, error) {
	cfg := &ProjectModelConfig{}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Create the row if it does not exist, leaning on the column defaults
		// ('' models, NOW() timestamps) and leaving an existing row untouched.
		if _, err := tx.NewRaw(
			`INSERT INTO kb.project_model_config (project_id) VALUES (?)
			 ON CONFLICT (project_id) DO NOTHING`,
			projectID,
		).Exec(ctx); err != nil {
			return err
		}

		// An empty patch is a no-op: it must not move updated_at.
		if patch.GenerativeModel == nil && patch.GenerativeProviderSlug == nil &&
			patch.EmbeddingModel == nil && patch.EmbeddingProviderSlug == nil {
			return tx.NewSelect().Model(cfg).Where("project_id = ?", projectID).Scan(ctx)
		}

		update := tx.NewUpdate().
			Model((*ProjectModelConfig)(nil)).
			Where("project_id = ?", projectID)
		if patch.GenerativeModel != nil {
			update = update.Set("generative_model = ?", *patch.GenerativeModel)
		}
		if patch.GenerativeProviderSlug != nil {
			update = update.Set("generative_provider_slug = ?", *patch.GenerativeProviderSlug)
		}
		if patch.EmbeddingModel != nil {
			update = update.Set("embedding_model = ?", *patch.EmbeddingModel)
		}
		if patch.EmbeddingProviderSlug != nil {
			update = update.Set("embedding_provider_slug = ?", *patch.EmbeddingProviderSlug)
		}
		if _, err := update.Set("updated_at = NOW()").Exec(ctx); err != nil {
			return err
		}

		return tx.NewSelect().Model(cfg).Where("project_id = ?", projectID).Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// DeleteProjectModelConfig removes the project model config.
func (s *Store) DeleteProjectModelConfig(ctx context.Context, projectID uuid.UUID) error {
	_, err := s.db.NewDelete().
		Model((*ProjectModelConfig)(nil)).
		Where("project_id = ?", projectID).
		Exec(ctx)
	return err
}
