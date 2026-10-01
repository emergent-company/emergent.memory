package notifications

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
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

// applyScopeFilter adds scope / project / requires_action predicates to a
// select query. Empty scope and nil project leave the query unchanged so the
// existing unscoped call sites keep their semantics.
func applyScopeFilter(q *bun.SelectQuery, scope Scope, projectID *string, requiresAction bool) *bun.SelectQuery {
	if scope != "" {
		q = q.Where("scope = ?", scope)
	}
	if projectID != nil {
		q = q.Where("project_id = ?", *projectID)
	}
	if requiresAction {
		q = q.Where("requires_action = true")
	}
	return q
}

// Create inserts a notification and returns the populated row (including the
// database-generated id and timestamps).
func (r *Repository) Create(ctx context.Context, n *Notification) (*Notification, error) {
	if _, err := r.db.NewInsert().Model(n).Returning("*").Exec(ctx); err != nil {
		r.log.Error("failed to create notification", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return n, nil
}

// CreateCoalescing inserts n unless an active notification already exists for
// the same (user_id, group_key). It relies on the partial unique index
// ux_notifications_user_group_key_active (WHERE group_key IS NOT NULL AND
// cleared_at IS NULL) and INSERT ... ON CONFLICT DO NOTHING, so concurrent
// producers cannot both win the way a read-then-insert check allowed.
//
// Returns (nil, nil) when the insert was suppressed by an existing row.
func (r *Repository) CreateCoalescing(ctx context.Context, n *Notification) (*Notification, error) {
	res, err := r.db.NewInsert().
		Model(n).
		On("CONFLICT DO NOTHING").
		Returning("*").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to create coalesced notification", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		r.log.Error("failed to read coalesced insert result", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	if affected == 0 {
		// An active row already exists for this (user_id, group_key).
		return nil, nil
	}
	return n, nil
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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	stats.Unread = int64(unread)

	// Count dismissed (uncleared only, so all three GetStats counters share the
	// same base population: Dismiss and Clear both set cleared_at).
	dismissed, err := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID).
		Where("cleared_at IS NULL").
		Where("dismissed = true").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count dismissed notifications", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	stats.Dismissed = int64(dismissed)

	return stats, nil
}

// countBase builds a scoped count query against kb.notifications.
func (r *Repository) countBase(userID string, params CountParams) *bun.SelectQuery {
	q := r.db.NewSelect().
		Model((*Notification)(nil)).
		Where("user_id = ?", userID)
	return applyScopeFilter(q, params.Scope, params.ProjectID, params.RequiresAction)
}

// GetCounts returns notification counts by tab for a user, honouring the scope
// / project / requires_action filters.
func (r *Repository) GetCounts(ctx context.Context, userID string, params CountParams) (*NotificationCounts, error) {
	counts := &NotificationCounts{}
	now := time.Now()

	// Count all (not cleared, not snoozed)
	all, err := r.countBase(userID, params).
		Where("cleared_at IS NULL").
		Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count all notifications", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	counts.All = int64(all)

	// Count unread (not cleared, not snoozed, read=false) — the bell badge.
	unread, err := r.countBase(userID, params).
		Where("cleared_at IS NULL").
		Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
		Where("read = false").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count unread notifications", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	counts.Unread = int64(unread)

	// Count important (not cleared, not snoozed, importance = 'important')
	important, err := r.countBase(userID, params).
		Where("cleared_at IS NULL").
		Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
		Where("importance = ?", "important").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count important notifications", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	counts.Important = int64(important)

	// Count other (not cleared, not snoozed, importance = 'other')
	other, err := r.countBase(userID, params).
		Where("cleared_at IS NULL").
		Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
		Where("importance = ?", "other").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count other notifications", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	counts.Other = int64(other)

	// Count snoozed (not cleared, snoozed_until > now)
	snoozed, err := r.countBase(userID, params).
		Where("cleared_at IS NULL").
		Where("snoozed_until > ?", now).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count snoozed notifications", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	counts.Snoozed = int64(snoozed)

	// Count cleared
	cleared, err := r.countBase(userID, params).
		Where("cleared_at IS NOT NULL").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count cleared notifications", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
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

	q = applyScopeFilter(q, params.Scope, params.ProjectID, params.RequiresAction)

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
		return nil, apperror.NewDatabase("Database operation failed", err)
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
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.NewNotFound("notification", notificationID)
	}

	return nil
}

// MarkUnread marks a notification as unread
func (r *Repository) MarkUnread(ctx context.Context, userID, notificationID string) error {
	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("read = ?", false).
		Set("read_at = NULL").
		Set("updated_at = ?", time.Now()).
		Where("id = ?", notificationID).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to mark notification as unread", logger.Error(err))
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.NewNotFound("notification", notificationID)
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
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.NewNotFound("notification", notificationID)
	}

	return nil
}

// Snooze sets a notification's snoozed_until timestamp.
func (r *Repository) Snooze(ctx context.Context, userID, notificationID string, until time.Time) error {
	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("snoozed_until = ?", until).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", notificationID).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to snooze notification", logger.Error(err))
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.NewNotFound("notification", notificationID)
	}

	return nil
}

// Unsnooze clears a notification's snoozed_until timestamp.
func (r *Repository) Unsnooze(ctx context.Context, userID, notificationID string) error {
	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("snoozed_until = NULL").
		Set("updated_at = ?", time.Now()).
		Where("id = ?", notificationID).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to unsnooze notification", logger.Error(err))
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.NewNotFound("notification", notificationID)
	}

	return nil
}

// Clear moves a notification to the cleared tab.
func (r *Repository) Clear(ctx context.Context, userID, notificationID string) error {
	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("cleared_at = ?", time.Now()).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", notificationID).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to clear notification", logger.Error(err))
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.NewNotFound("notification", notificationID)
	}

	return nil
}

// Restore un-clears and un-dismisses a notification, returning it to the inbox.
// It resets both the dismissed flag and dismissed_at alongside cleared_at in a
// single statement so a dismissed-then-restored row is neither cleared nor
// dismissed. `read` is deliberately left untouched: a restored notification
// keeps whatever read/unread state it had before being dismissed.
func (r *Repository) Restore(ctx context.Context, userID, notificationID string) error {
	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("cleared_at = NULL").
		Set("dismissed = ?", false).
		Set("dismissed_at = NULL").
		Set("updated_at = ?", time.Now()).
		Where("id = ?", notificationID).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to restore notification", logger.Error(err))
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.NewNotFound("notification", notificationID)
	}

	return nil
}

// ResolveAction records the outcome of an actionable notification.
func (r *Repository) ResolveAction(ctx context.Context, userID, notificationID, status string) error {
	now := time.Now()

	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("action_status = ?", status).
		Set("action_status_at = ?", now).
		Set("action_status_by = ?", userID).
		Set("updated_at = ?", now).
		Where("id = ?", notificationID).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to resolve notification action", logger.Error(err))
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.NewNotFound("notification", notificationID)
	}

	return nil
}

// MarkAllRead marks all notifications as read for a user within the given
// scope (and optional project).
func (r *Repository) MarkAllRead(ctx context.Context, userID string, scope Scope, projectID *string) (int64, error) {
	now := time.Now()

	q := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("read = ?", true).
		Set("read_at = ?", now).
		Set("updated_at = ?", now).
		Where("user_id = ?", userID).
		Where("read = ?", false).
		Where("cleared_at IS NULL")

	if scope != "" {
		q = q.Where("scope = ?", scope)
	}
	if projectID != nil {
		q = q.Where("project_id = ?", *projectID)
	}

	result, err := q.Exec(ctx)
	if err != nil {
		r.log.Error("failed to mark all notifications as read", logger.Error(err))
		return 0, apperror.NewDatabase("Database operation failed", err)
	}

	count, _ := result.RowsAffected()
	return count, nil
}

// GetPreferences returns the stored notification preferences for a user,
// optionally scoped to a project (nil projectID returns account-scope rows).
func (r *Repository) GetPreferences(ctx context.Context, userID string, projectID *string) ([]NotificationPreference, error) {
	prefs := []NotificationPreference{}

	q := r.db.NewSelect().
		Model(&prefs).
		Where("user_id = ?", userID)

	if projectID == nil {
		q = q.Where("project_id IS NULL")
	} else {
		q = q.Where("project_id = ?", *projectID)
	}

	if err := q.Scan(ctx); err != nil {
		r.log.Error("failed to list notification preferences", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}

	return prefs, nil
}

// GetPreference returns a single stored preference row, or nil when none
// exists for the (user, project, event key, channel) tuple.
func (r *Repository) GetPreference(ctx context.Context, userID string, projectID *string, eventKey, channel string) (*NotificationPreference, error) {
	p := &NotificationPreference{}

	q := r.db.NewSelect().
		Model(p).
		Where("user_id = ?", userID).
		Where("event_key = ?", eventKey).
		Where("channel = ?", channel)
	if projectID == nil {
		q = q.Where("project_id IS NULL")
	} else {
		q = q.Where("project_id = ?", *projectID)
	}

	if err := q.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		r.log.Error("failed to get notification preference", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}

	return p, nil
}

// UpsertPreference inserts or updates a preference row keyed by the
// (user_id, project_id, event_key, channel) unique constraint.
func (r *Repository) UpsertPreference(ctx context.Context, p *NotificationPreference) (*NotificationPreference, error) {
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	_, err := r.db.NewInsert().
		Model(p).
		On("CONFLICT (user_id, project_id, event_key, channel) DO UPDATE").
		Set("enabled = EXCLUDED.enabled").
		Set("updated_at = EXCLUDED.updated_at").
		Returning("*").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to upsert notification preference", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}

	return p, nil
}
