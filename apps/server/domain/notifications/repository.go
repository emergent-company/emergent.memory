package notifications

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
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

// actionRequiredPredicate is the server-side definition of "this row still
// needs the user's action": it was produced as actionable and no outcome has
// been recorded yet.
//
// Both the Action-required tab (List) and its badge (GetCounts) apply this exact
// predicate, so the count and the rows it labels cannot disagree, and a row
// leaves the tab the moment ResolveAction writes action_status — independently
// of whether the client's follow-up read/clear requests land. A resolved but
// uncleared row is still an ordinary inbox row: it stays in All and Unread, it
// just no longer demands action.
const actionRequiredPredicate = "requires_action = true AND action_status IS NULL"

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
		q = q.Where(actionRequiredPredicate)
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

// notificationReopenRow carries the row a create-or-reopen write returned and
// whether the statement inserted it rather than reopening an existing row.
type notificationReopenRow struct {
	Notification `bun:",extend"`
	Inserted     bool `bun:"inserted"`
}

// CreateOrReopen inserts n, or — when an unresolved row already occupies the
// same (user_id, group_key) — reopens that row instead of coalescing the new
// episode away (#2414).
//
// It is the write for a producer whose event has to be actionable on every
// occurrence. ResolveResource sets action_status, not cleared_at, so a resolved
// row still occupies the partial unique index
// ux_notifications_user_group_key_active, and a plain coalescing insert would be
// silently swallowed on the next occurrence. Snooze likewise leaves the row on
// the index, and every inbox tab filters a live snooze, so the reopen has to
// reset it or the item is unresolved in the database and invisible in the
// inbox.
//
// On conflict the conflict target restates the index predicate in full: the
// index is partial on `group_key IS NOT NULL AND cleared_at IS NULL`, and an
// inference that omits the group_key clause matches nothing (SQLSTATE 42P10).
// The update
// refreshes the whole actionable payload, clears action_status, snoozed_until
// and the read mark, drops a stale expiry, and leaves id, created_at and
// cleared_at (identity and age) alone. A cleared row is off the index, so a
// dismissed or cleared item is not resurrected: the insert creates a new one.
//
// Returns the stored row and whether it was inserted (true) or reopened (false).
func (r *Repository) CreateOrReopen(ctx context.Context, n *Notification) (*Notification, bool, error) {
	row := &notificationReopenRow{}
	if err := r.db.NewInsert().
		Model(n).
		On("CONFLICT (user_id, group_key) WHERE group_key IS NOT NULL AND cleared_at IS NULL DO UPDATE SET "+
			"title = EXCLUDED.title, "+
			"message = EXCLUDED.message, "+
			"details = EXCLUDED.details, "+
			"severity = EXCLUDED.severity, "+
			"importance = EXCLUDED.importance, "+
			"category = EXCLUDED.category, "+
			"action_url = EXCLUDED.action_url, "+
			"action_label = EXCLUDED.action_label, "+
			"actions = EXCLUDED.actions, "+
			"related_resource_type = EXCLUDED.related_resource_type, "+
			"related_resource_id = EXCLUDED.related_resource_id, "+
			"source_type = EXCLUDED.source_type, "+
			"source_id = EXCLUDED.source_id, "+
			"requires_action = EXCLUDED.requires_action, "+
			"event_key = EXCLUDED.event_key, "+
			"action_status = NULL, action_status_at = NULL, action_status_by = NULL, "+
			"snoozed_until = NULL, read = false, read_at = NULL, expires_at = NULL, "+
			"updated_at = now()").
		Returning("*, (xmax = 0) AS inserted").
		Scan(ctx, row); err != nil {
		r.log.Error("failed to create or reopen notification", logger.Error(err))
		return nil, false, apperror.NewDatabase("Database operation failed", err)
	}
	return &row.Notification, row.Inserted, nil
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

// countBaseActionRequired builds the base query for the Action-required bucket.
// That bucket always needs the action-required predicate, but applyScopeFilter
// already adds it when the caller asked for it (the Action required tab), so add
// it here only when it is absent. Appending it unconditionally produced the same
// clause twice and made the SQL depend on the caller in a way that was easy to
// misread. It comes from actionRequiredPredicate in both paths, so the badge and
// the list it counts are filtered identically.
func (r *Repository) countBaseActionRequired(userID string, params CountParams) *bun.SelectQuery {
	q := r.countBase(userID, params)
	if !params.RequiresAction {
		q = q.Where(actionRequiredPredicate)
	}
	return q
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

	// Count action-required (not cleared, not snoozed, requires_action = true) —
	// the "Action required" tab's badge. It mirrors that tab's list filter, so
	// the badge and the rows it labels can never disagree.
	actionRequired, err := r.countBaseActionRequired(userID, params).
		Where("cleared_at IS NULL").
		Where("(snoozed_until IS NULL OR snoozed_until <= ?)", now).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count action-required notifications", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	counts.RequiresAction = int64(actionRequired)

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

// ErrActiveNotificationConflict is returned by Restore when re-activating a
// group-keyed notification would collide with another notification that is
// already active for the same (user_id, group_key). Restoring in that situation
// is refused rather than silently superseding the newer alert: the later row is
// at least as fresh as the one being restored, so clearing it would discard the
// current alert to surface a stale duplicate. Callers can map this to a 409 and
// tell the user "a newer notification for this key is already active".
var ErrActiveNotificationConflict = apperror.New(
	http.StatusConflict,
	"notification_active_conflict",
	"a newer notification for this key is already active",
)

// Restore un-clears and un-dismisses a notification, returning it to the inbox.
// It resets both the dismissed flag and dismissed_at alongside cleared_at in a
// single statement so a dismissed-then-restored row is neither cleared nor
// dismissed. `read` is deliberately left untouched: a restored notification
// keeps whatever read/unread state it had before being dismissed.
//
// Setting cleared_at = NULL re-activates the row, which is governed by the
// partial unique index ux_notifications_user_group_key_active (WHERE group_key
// IS NOT NULL AND cleared_at IS NULL). If a newer active notification already
// owns this (user_id, group_key) — e.g. dismiss an old budget alert, let the
// producer emit the next one, then restore the old one — the UPDATE violates
// that index. We translate the violation into the typed
// ErrActiveNotificationConflict instead of surfacing a 500. The UPDATE is a
// single atomic statement, so a rejected restore leaves the row untouched:
// there is no half-updated state to roll back.
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
		// The only unique constraint this UPDATE can violate is the active
		// (user_id, group_key) partial index: the target's primary key is not
		// modified, and no other uniqueness covers the row being re-activated.
		if pgutils.IsUniqueViolation(err) {
			return ErrActiveNotificationConflict
		}
		r.log.Error("failed to restore notification", logger.Error(err))
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.NewNotFound("notification", notificationID)
	}

	return nil
}

// ErrActionAlreadyResolved is returned by ResolveActionStatus when the
// notification already carries a different action_status.
//
// The recorded verb is first-write-wins: a repeat of the verb that landed is an
// idempotent success, a different verb is refused instead of silently
// overwriting it. A double click, an Accept followed by a Decline, or a second
// tab therefore cannot leave the verb the user did not choose — the invariant
// does not depend on the client's in-flight guard.
var ErrActionAlreadyResolved = apperror.New(
	http.StatusConflict,
	"notification_action_already_resolved",
	"this notification's action was already resolved with a different outcome",
)

// ResolveActionStatus records the outcome of an actionable notification.
//
// It is the one write every surface that resolves an action goes through — the
// inbox route and an agent question answered from chat — because two writers
// with two rules is how a recorded verb silently changes hands: the second
// surface overwrites the first, and action_status_by changes owner with it.
//
// The UPDATE is conditional on action_status IS NULL, so the first statement to
// land wins and concurrent submits cannot overwrite each other. When nothing was
// written the stored value decides the response: absent (or, when a userID is
// given, someone else's) row is a 404, the same verb again is nil (idempotent),
// a different verb is ErrActionAlreadyResolved.
//
// A non-empty userID scopes both statements to the notification's owner. An
// empty userID means the caller authorized the write by another route and is not
// necessarily the row's user — an agent question is answered by a run
// participant while the notification belongs to the run's user — so the id
// alone identifies the row, and action_status_by stays NULL because there is no
// comparable user id to attribute the outcome to. The outcome is still
// first-write-wins.
//
// It is a package function taking the database handle rather than a Repository
// method so another domain can reuse it without owning this repository.
func ResolveActionStatus(ctx context.Context, db bun.IDB, userID, notificationID, status string) error {
	now := time.Now()

	update := db.NewUpdate().
		Model((*Notification)(nil)).
		Set("action_status = ?", status).
		Set("action_status_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ?", notificationID).
		Where("action_status IS NULL")
	if userID != "" {
		update = update.
			Set("action_status_by = ?", userID).
			Where("user_id = ?", userID)
	}

	result, err := update.Exec(ctx)
	if err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return apperror.NewDatabase("Database operation failed", err)
	}
	if rowsAffected > 0 {
		return nil
	}

	// Nothing was written: the row is missing, not owned by this caller, or it
	// already carries an outcome. Read the recorded verb to say which.
	existing := new(Notification)
	read := db.NewSelect().
		Model(existing).
		Column("action_status").
		Where("id = ?", notificationID)
	if userID != "" {
		read = read.Where("user_id = ?", userID)
	}

	if err := read.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperror.NewNotFound("notification", notificationID)
		}
		return apperror.NewDatabase("Database operation failed", err)
	}

	if existing.ActionStatus != nil && *existing.ActionStatus == status {
		// A retry of the submit that already landed: report success.
		return nil
	}

	return ErrActionAlreadyResolved
}

// ResolveAction records the outcome of an actionable notification for the
// notification's owner. It delegates to ResolveActionStatus, which owns the
// first-write-wins rule, so this route and the agent-question writer cannot
// drift apart.
func (r *Repository) ResolveAction(ctx context.Context, userID, notificationID, status string) error {
	err := ResolveActionStatus(ctx, r.db, userID, notificationID, status)
	// A conflict or an absent row is the client's answer, not a server fault:
	// the handler turns it into a 409/404. Only an internal failure is worth an
	// error log, so an expected refusal does not look like an outage in the log.
	var appErr *apperror.Error
	if errors.As(err, &appErr) && appErr.HTTPStatus >= http.StatusInternalServerError {
		r.log.Error("failed to resolve notification action", logger.Error(err))
	}
	return err
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

// ListProjectMemberUserIDs returns the user ids of every member of a project
// (kb.project_memberships). Producers use it to fan a single project event out
// to one notification per member.
func (r *Repository) ListProjectMemberUserIDs(ctx context.Context, projectID string) ([]string, error) {
	var ids []string
	err := r.db.NewSelect().
		TableExpr("kb.project_memberships").
		Column("user_id").
		Where("project_id = ?", projectID).
		Scan(ctx, &ids)
	if err != nil && err != sql.ErrNoRows {
		r.log.Error("failed to list project member user ids", logger.Error(err))
		return nil, apperror.NewDatabase("Database operation failed", err)
	}
	return ids, nil
}

// ResolveByBranch marks every actionable, unresolved notification referencing a
// staging branch as resolved. Called after a merge so review-ready alerts stop
// demanding action once their branch is merged.
func (r *Repository) ResolveByBranch(ctx context.Context, projectID, branchID string) error {
	_, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("action_status = ?", "resolved").
		Set("action_status_at = ?", time.Now()).
		Set("updated_at = ?", time.Now()).
		Where("project_id = ?", projectID).
		Where("related_resource_type = ?", "graph_branch").
		Where("related_resource_id = ?", branchID).
		Where("requires_action = true").
		Where("action_status IS NULL").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to resolve branch notifications", logger.Error(err))
		return apperror.NewDatabase("Database operation failed", err)
	}
	return nil
}

// ResolveResource marks every actionable, unresolved notification referencing a
// resource as resolved, scoped to the given project. Returns the number of rows
// affected. This backs the resolve-resource endpoint.
func (r *Repository) ResolveResource(ctx context.Context, projectID, resourceType, resourceID string) (int64, error) {
	result, err := r.db.NewUpdate().
		Model((*Notification)(nil)).
		Set("action_status = ?", "resolved").
		Set("action_status_at = ?", time.Now()).
		Set("updated_at = ?", time.Now()).
		Where("project_id = ?", projectID).
		Where("related_resource_type = ?", resourceType).
		Where("related_resource_id = ?", resourceID).
		Where("requires_action = true").
		Where("action_status IS NULL").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to resolve resource notifications", logger.Error(err))
		return 0, apperror.NewDatabase("Database operation failed", err)
	}
	count, _ := result.RowsAffected()
	return count, nil
}
