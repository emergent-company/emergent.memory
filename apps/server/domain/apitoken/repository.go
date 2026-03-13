package apitoken

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
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
		if pgutils.IsUniqueViolation(err) {
			return apperror.New(409, "token_name_exists", "A token with this name already exists")
		}
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// FindByProjectAndName finds an active token by project ID and name (for uniqueness check)
func (r *Repository) FindByProjectAndName(ctx context.Context, projectID, name string) (*ApiToken, error) {
	var token ApiToken
	err := r.db.NewSelect().
		Model(&token).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Where("revoked_at IS NULL").
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

// ListByUser returns all account-level tokens for a user (project_id IS NULL)
func (r *Repository) ListByUser(ctx context.Context, userID string) ([]ApiToken, error) {
	var tokens []ApiToken
	err := r.db.NewSelect().
		Model(&tokens).
		Where("user_id = ?", userID).
		Where("project_id IS NULL").
		Order("created_at DESC").
		Scan(ctx)

	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return tokens, nil
}

// RevokeByUser revokes an account-level token owned by the user (project_id IS NULL)
func (r *Repository) RevokeByUser(ctx context.Context, tokenID, userID string) (bool, error) {
	result, err := r.db.NewUpdate().
		Model((*ApiToken)(nil)).
		Set("revoked_at = NOW()").
		Where("id = ?", tokenID).
		Where("user_id = ?", userID).
		Where("project_id IS NULL").
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

// CreateAccountToken creates a new account-level token (project_id = NULL)
func (r *Repository) CreateAccountToken(ctx context.Context, token *ApiToken) error {
	_, err := r.db.NewInsert().
		Model(token).
		Exec(ctx)
	if err != nil {
		if pgutils.IsUniqueViolation(err) {
			return apperror.New(409, "token_name_exists", "An account token with this name already exists")
		}
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// FindByUserAndName finds an account-level token by user ID and name (for uniqueness check)
func (r *Repository) FindByUserAndName(ctx context.Context, userID, name string) (*ApiToken, error) {
	var token ApiToken
	err := r.db.NewSelect().
		Model(&token).
		Where("user_id = ?", userID).
		Where("project_id IS NULL").
		Where("name = ?", name).
		Where("revoked_at IS NULL").
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &token, nil
}

// GetByIDAndUser returns an account-level token by ID and user ID (project_id IS NULL)
func (r *Repository) GetByIDAndUser(ctx context.Context, tokenID, userID string) (*ApiToken, error) {
	var token ApiToken
	err := r.db.NewSelect().
		Model(&token).
		Where("id = ?", tokenID).
		Where("user_id = ?", userID).
		Where("project_id IS NULL").
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
