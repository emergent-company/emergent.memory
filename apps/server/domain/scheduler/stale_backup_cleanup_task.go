package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// StaleBackupCleanupTask marks project backups stuck in "creating" as failed.
// Backups are created asynchronously in a fire-and-forget goroutine; if the
// server restarts mid-backup (or the failure update was lost) the row can stay
// "creating" forever. This task reaps those after staleMinutes.
type StaleBackupCleanupTask struct {
	db           *bun.DB
	log          *slog.Logger
	staleMinutes int
}

// NewStaleBackupCleanupTask creates a new stale backup cleanup task.
func NewStaleBackupCleanupTask(db *bun.DB, log *slog.Logger, staleMinutes int) *StaleBackupCleanupTask {
	if staleMinutes <= 0 {
		staleMinutes = 60
	}
	return &StaleBackupCleanupTask{
		db:           db,
		log:          log.With(logger.Scope("scheduler.stale_backup_cleanup")),
		staleMinutes: staleMinutes,
	}
}

// Run marks backups that have been "creating" longer than the threshold as failed.
func (t *StaleBackupCleanupTask) Run(ctx context.Context) error {
	cutoff := time.Now().Add(-time.Duration(t.staleMinutes) * time.Minute)

	res, err := t.db.ExecContext(ctx, `
		UPDATE kb.backups
		SET status = 'failed',
		    error_message = 'Backup marked as stale during cleanup',
		    completed_at = NOW()
		WHERE status = 'creating'
		AND created_at < ?
	`, cutoff)
	if err != nil {
		t.log.Error("failed to clean up stale backups", slog.String("error", err.Error()))
		return err
	}

	if n, _ := res.RowsAffected(); n > 0 {
		t.log.Info("marked stale backups as failed", slog.Int64("count", n))
	}

	return nil
}
