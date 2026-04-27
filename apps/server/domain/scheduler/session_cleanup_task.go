package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// SessionCleanupTask deletes ADK session data (events, states, sessions) for
// agent runs that have been in a terminal state for longer than RetentionDays.
//
// Terminal run statuses: success, error, skipped, cancelled.
//
// Deletion order respects FK constraints:
//  1. kb.adk_events   (FK → kb.adk_sessions ON DELETE CASCADE, but we delete explicitly for logging)
//  2. kb.adk_states   (no FK, scoped by session_id)
//  3. kb.adk_sessions (session_id == run_id by convention)
//
// The run record itself is NOT deleted — only the ADK session payload.
type SessionCleanupTask struct {
	db            *bun.DB
	log           *slog.Logger
	retentionDays int
}

// NewSessionCleanupTask creates a new SessionCleanupTask.
// retentionDays is the minimum age (in days) of a completed run before its
// session data is eligible for deletion. Defaults to 90 if <= 0.
func NewSessionCleanupTask(db *bun.DB, log *slog.Logger, retentionDays int) *SessionCleanupTask {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	return &SessionCleanupTask{
		db:            db,
		log:           log.With(logger.Scope("scheduler.session_cleanup")),
		retentionDays: retentionDays,
	}
}

// Run executes the session cleanup.
func (t *SessionCleanupTask) Run(ctx context.Context) error {
	start := time.Now()
	cutoff := start.AddDate(0, 0, -t.retentionDays)

	t.log.Debug("starting session cleanup",
		slog.Int("retention_days", t.retentionDays),
		slog.Time("cutoff", cutoff),
	)

	// Collect session IDs for runs that completed before the cutoff.
	// session_id == run_id by ADK convention (set in executor.go buildSessionID).
	var sessionIDs []string
	err := t.db.NewSelect().
		TableExpr("kb.agent_runs").
		ColumnExpr("id::text").
		Where("status IN ('success', 'error', 'skipped', 'cancelled')").
		Where("completed_at IS NOT NULL").
		Where("completed_at < ?", cutoff).
		Scan(ctx, &sessionIDs)
	if err != nil {
		t.log.Error("session cleanup: failed to query eligible runs", slog.String("error", err.Error()))
		return err
	}

	if len(sessionIDs) == 0 {
		t.log.Debug("session cleanup: no sessions eligible for deletion",
			slog.Duration("duration", time.Since(start)))
		return nil
	}

	t.log.Info("session cleanup: found eligible sessions",
		slog.Int("count", len(sessionIDs)),
		slog.Time("cutoff", cutoff),
	)

	// Delete adk_events (cascade would handle this, but explicit for row count logging).
	eventsDeleted, err := t.deleteWhere(ctx, "kb.adk_events", "session_id", sessionIDs)
	if err != nil {
		t.log.Warn("session cleanup: failed to delete adk_events", slog.String("error", err.Error()))
		// best-effort: continue to attempt sessions/states deletion
	}

	// Delete adk_states scoped to these sessions.
	statesDeleted, err := t.deleteWhere(ctx, "kb.adk_states", "session_id", sessionIDs)
	if err != nil {
		t.log.Warn("session cleanup: failed to delete adk_states", slog.String("error", err.Error()))
	}

	// Delete adk_sessions rows (id column).
	sessionsDeleted, err := t.deleteWhere(ctx, "kb.adk_sessions", "id", sessionIDs)
	if err != nil {
		t.log.Warn("session cleanup: failed to delete adk_sessions", slog.String("error", err.Error()))
	}

	t.log.Info("session cleanup: completed",
		slog.Int64("events_deleted", eventsDeleted),
		slog.Int64("states_deleted", statesDeleted),
		slog.Int64("sessions_deleted", sessionsDeleted),
		slog.Duration("duration", time.Since(start)),
	)

	return nil
}

// deleteWhere deletes rows from table where column IN ids and returns the row count.
func (t *SessionCleanupTask) deleteWhere(ctx context.Context, table, column string, ids []string) (int64, error) {
	result, err := t.db.NewDelete().
		TableExpr(table).
		Where("? IN (?)", bun.Ident(column), bun.In(ids)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}
