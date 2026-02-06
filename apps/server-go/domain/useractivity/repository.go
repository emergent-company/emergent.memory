package useractivity

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Repository handles database operations for user activity
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new user activity repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("useractivity.repo")),
	}
}

// Record records a user activity, upserting if the same resource was accessed
func (r *Repository) Record(ctx context.Context, item *UserRecentItem) error {
	// Upsert: if user accessed same resource, update accessed_at
	_, err := r.db.NewInsert().
		Model(item).
		On("CONFLICT (user_id, project_id, resource_type, resource_id) DO UPDATE").
		Set("accessed_at = EXCLUDED.accessed_at").
		Set("resource_name = EXCLUDED.resource_name").
		Set("resource_subtype = EXCLUDED.resource_subtype").
		Set("action_type = EXCLUDED.action_type").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to record user activity", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// GetRecent retrieves recent activity items for a user
func (r *Repository) GetRecent(ctx context.Context, userID string, limit int) ([]UserRecentItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	items := []UserRecentItem{}
	err := r.db.NewSelect().
		Model(&items).
		Where("user_id = ?", userID).
		Order("accessed_at DESC").
		Limit(limit).
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get recent items", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return items, nil
}

// GetRecentByType retrieves recent activity items for a user filtered by resource type
func (r *Repository) GetRecentByType(ctx context.Context, userID, resourceType string, limit int) ([]UserRecentItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	items := []UserRecentItem{}
	err := r.db.NewSelect().
		Model(&items).
		Where("user_id = ?", userID).
		Where("resource_type = ?", resourceType).
		Order("accessed_at DESC").
		Limit(limit).
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get recent items by type", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return items, nil
}

// DeleteAll deletes all recent activity for a user
func (r *Repository) DeleteAll(ctx context.Context, userID string) error {
	_, err := r.db.NewDelete().
		Model((*UserRecentItem)(nil)).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete all user activity", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// DeleteByResource deletes a specific resource from user's recent activity
func (r *Repository) DeleteByResource(ctx context.Context, userID, resourceType, resourceID string) error {
	_, err := r.db.NewDelete().
		Model((*UserRecentItem)(nil)).
		Where("user_id = ?", userID).
		Where("resource_type = ?", resourceType).
		Where("resource_id = ?", resourceID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete user activity by resource", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// CleanupOld removes activity items older than the specified duration
func (r *Repository) CleanupOld(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	result, err := r.db.NewDelete().
		Model((*UserRecentItem)(nil)).
		Where("accessed_at < ?", cutoff).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to cleanup old activity", logger.Error(err))
		return 0, apperror.ErrDatabase.WithInternal(err)
	}

	count, _ := result.RowsAffected()
	return count, nil
}
