package apitoken

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Repository handles data access for API tokens
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new API token repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("apitoken.repo")),
	}
}

// Create creates a new API token
func (r *Repository) Create(ctx context.Context, token *ApiToken) error {
	_, err := r.db.NewInsert().
		Model(token).
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// FindByProjectAndName finds a token by project ID and name (for uniqueness check)
// Includes ALL tokens (even revoked) since the database constraint is project-wide
func (r *Repository) FindByProjectAndName(ctx context.Context, projectID, name string) (*ApiToken, error) {
	var token ApiToken
	err := r.db.NewSelect().
		Model(&token).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found is not an error
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &token, nil
}

// ListByProject returns all tokens for a project
func (r *Repository) ListByProject(ctx context.Context, projectID string) ([]ApiToken, error) {
	var tokens []ApiToken
	err := r.db.NewSelect().
		Model(&tokens).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Scan(ctx)

	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return tokens, nil
}

// GetByID returns a token by ID and project ID
func (r *Repository) GetByID(ctx context.Context, tokenID, projectID string) (*ApiToken, error) {
	var token ApiToken
	err := r.db.NewSelect().
		Model(&token).
		Where("id = ?", tokenID).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &token, nil
}

// Revoke sets the revoked_at timestamp on a token
func (r *Repository) Revoke(ctx context.Context, tokenID, projectID string) (bool, error) {
	result, err := r.db.NewUpdate().
		Model((*ApiToken)(nil)).
		Set("revoked_at = NOW()").
		Where("id = ?", tokenID).
		Where("project_id = ?", projectID).
		Where("revoked_at IS NULL").
		Exec(ctx)

	if err != nil {
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	return rows > 0, nil
}
