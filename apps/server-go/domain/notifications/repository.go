package notifications

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Repository handles database operations for notifications
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new notifications repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("notifications.repo")),
	}
}

// GetStats returns notification statistics for a user
func (r *Repository) GetStats(ctx context.Context, userID string) (*NotificationStats, error) {
	stats := &NotificationStats{}

	// Count total
	total, err := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID).
		Where("cleared_at IS NULL").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count total notifications", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	stats.Total = int64(total)

	// Count unread
	unread, err := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID).
		Where("cleared_at IS NULL").
		Where("read = false").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count unread notifications", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	stats.Unread = int64(unread)

	// Count dismissed
	dismissed, err := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID).
		Where("dismissed = true").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count dismissed notifications", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	stats.Dismissed = int64(dismissed)

	return stats, nil
}

// GetCounts returns notification counts by tab for a user
func (r *Repository) GetCounts(ctx context.Context, userID string) (*NotificationCounts, error) {
	counts := &NotificationCounts{}
	now := time.Now()

	// Count all (not cleared, not snoozed)
	all, err := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID).
		Where("cleared_at IS NULL").
		Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count all notifications", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.All = int64(all)

	// Count important (not cleared, not snoozed, importance = 'important')
	important, err := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID).
		Where("cleared_at IS NULL").
		Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
		Where("importance = ?", "important").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count important notifications", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Important = int64(important)

	// Count other (not cleared, not snoozed, importance = 'other')
	other, err := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID).
		Where("cleared_at IS NULL").
		Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
		Where("importance = ?", "other").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count other notifications", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Other = int64(other)

	// Count snoozed (not cleared, snoozed_until > now)
	snoozed, err := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID).
		Where("cleared_at IS NULL").
		Where("snoozed_until > ?", now).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count snoozed notifications", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Snoozed = int64(snoozed)

	// Count cleared
	cleared, err := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID).
		Where("cleared_at IS NOT NULL").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count cleared notifications", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Cleared = int64(cleared)

	return counts, nil
}

// List returns notifications for a user with filters
func (r *Repository) List(ctx context.Context, userID string, params ListParams) ([]Notification, error) {
	notifications := []Notification{} // Initialize as empty slice, not nil
	now := time.Now()

	q := r.db.NewSelect().
		Model(&notifications).
		Where("user_id = ?", userID)

	// Apply tab filter
	switch params.Tab {
	case TabAll:
		q = q.Where("cleared_at IS NULL").
			Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now)
	case TabImportant:
		q = q.Where("cleared_at IS NULL").
			Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
			Where("importance = ?", "important")
	case TabOther:
		q = q.Where("cleared_at IS NULL").
			Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
			Where("importance = ?", "other")
	case TabSnoozed:
		q = q.Where("cleared_at IS NULL").
			Where("snoozed_until > ?", now)
	case TabCleared:
		q = q.Where("cleared_at IS NOT NULL")
	}

	// Apply category filter
	if params.Category != "" && params.Category != "all" {
		q = q.Where("category = ?", params.Category)
	}

	// Apply unread filter
	if params.UnreadOnly {
		q = q.Where("read = false")
	}

	// Apply search filter
	if params.Search != "" {
		searchPattern := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.Where("title ILIKE ?", searchPattern).
				WhereOr("message ILIKE ?", searchPattern)
		})
	}

	// Order by created_at descending
	q = q.Order("created_at DESC")

	if err := q.Scan(ctx); err != nil {
		r.log.Error("failed to list notifications", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return notifications, nil
}

// MarkRead marks a notification as read
func (r *Repository) MarkRead(ctx context.Context, userID, notificationID string) error {
	now := time.Now()

	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("read = ?", true).
		Set("read_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ?", notificationID).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to mark notification as read", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("Notification not found")
	}

	return nil
}

// Dismiss dismisses (clears) a notification
func (r *Repository) Dismiss(ctx context.Context, userID, notificationID string) error {
	now := time.Now()

	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("dismissed = ?", true).
		Set("dismissed_at = ?", now).
		Set("cleared_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ?", notificationID).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to dismiss notification", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("Notification not found")
	}

	return nil
}

// MarkAllRead marks all notifications as read for a user
func (r *Repository) MarkAllRead(ctx context.Context, userID string) (int64, error) {
	now := time.Now()

	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("read = ?", true).
		Set("read_at = ?", now).
		Set("updated_at = ?", now).
		Where("user_id = ?", userID).
		Where("read = ?", false).
		Where("cleared_at IS NULL").
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to mark all notifications as read", logger.Error(err))
		return 0, apperror.ErrDatabase.WithInternal(err)
	}

	count, _ := result.RowsAffected()
	return count, nil
}
