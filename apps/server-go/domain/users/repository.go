package users

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Repository handles database operations for users
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new users repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("users.repo")),
	}
}

// SearchByEmail searches for users by email (partial match)
// Returns up to 10 results, optionally excluding a specific user
func (r *Repository) SearchByEmail(ctx context.Context, emailQuery string, excludeUserID *string) ([]UserSearchResult, error) {
	// Normalize and prepare the search pattern
	emailQuery = strings.TrimSpace(strings.ToLower(emailQuery))
	if len(emailQuery) < 2 {
		return []UserSearchResult{}, nil
	}

	// Use ILIKE for case-insensitive partial match
	pattern := "%" + emailQuery + "%"

	// Query to find users by email with their profile info
	// Join user_profiles with user_emails to get email and profile data
	query := r.db.NewSelect().
		TableExpr("core.user_emails AS ue").
		ColumnExpr("up.id").
		ColumnExpr("ue.email").
		ColumnExpr("up.display_name").
		ColumnExpr("up.first_name").
		ColumnExpr("up.last_name").
		ColumnExpr("up.avatar_object_key").
		Join("INNER JOIN core.user_profiles AS up ON up.id = ue.user_id").
		Where("ue.email ILIKE ?", pattern).
		Where("ue.verified = true").
		Where("up.deleted_at IS NULL").
		OrderExpr("ue.email ASC").
		Limit(10)

	// Optionally exclude a user (e.g., the current user)
	if excludeUserID != nil && *excludeUserID != "" {
		query = query.Where("up.id != ?", *excludeUserID)
	}

	var results []UserSearchResult
	err := query.Scan(ctx, &results)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []UserSearchResult{}, nil
		}
		r.log.Error("failed to search users by email", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return results, nil
}

// FindByEmail finds a user by exact email match
func (r *Repository) FindByEmail(ctx context.Context, email string) (*UserSearchResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))

	var result UserSearchResult
	err := r.db.NewSelect().
		TableExpr("core.user_emails AS ue").
		ColumnExpr("up.id").
		ColumnExpr("ue.email").
		ColumnExpr("up.display_name").
		ColumnExpr("up.first_name").
		ColumnExpr("up.last_name").
		ColumnExpr("up.avatar_object_key").
		Join("INNER JOIN core.user_profiles AS up ON up.id = ue.user_id").
		Where("ue.email = ?", email).
		Where("ue.verified = true").
		Where("up.deleted_at IS NULL").
		Limit(1).
		Scan(ctx, &result)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Not found, not an error
		}
		r.log.Error("failed to find user by email", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &result, nil
}
