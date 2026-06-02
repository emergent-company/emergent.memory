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

// UpsertProjectModelConfig creates or updates the model config for a project.
func (s *Store) UpsertProjectModelConfig(ctx context.Context, cfg *ProjectModelConfig) error {
	_, err := s.db.NewInsert().
		Model(cfg).
		On("CONFLICT (project_id) DO UPDATE").
		Set("generative_model = EXCLUDED.generative_model").
		Set("embedding_model = EXCLUDED.embedding_model").
		Set("updated_at = NOW()").
		Exec(ctx)
	return err
}

// DeleteProjectModelConfig removes the project model config.
func (s *Store) DeleteProjectModelConfig(ctx context.Context, projectID uuid.UUID) error {
	_, err := s.db.NewDelete().
		Model((*ProjectModelConfig)(nil)).
		Where("project_id = ?", projectID).
		Exec(ctx)
	return err
}
