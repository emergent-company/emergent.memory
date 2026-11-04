package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// RetrievalTraceCleanupTask deletes kb.retrieval_traces rows older than the
// configured retention period. Traces are written best-effort per search and
// are only used for debugging/reconstruction, so they are pruned on a schedule.
type RetrievalTraceCleanupTask struct {
	db        *bun.DB
	log       *slog.Logger
	retention time.Duration
}

// NewRetrievalTraceCleanupTask creates a new retrieval trace cleanup task.
// retention is how long traces are kept; defaults to 720h (30 days) when <= 0.
func NewRetrievalTraceCleanupTask(db *bun.DB, log *slog.Logger, retention time.Duration) *RetrievalTraceCleanupTask {
	if retention <= 0 {
		retention = 720 * time.Hour
	}
	return &RetrievalTraceCleanupTask{
		db:        db,
		log:       log.With(logger.Scope("scheduler.retrieval_trace_cleanup")),
		retention: retention,
	}
}

// Run deletes expired retrieval traces and logs the deleted count.
func (t *RetrievalTraceCleanupTask) Run(ctx context.Context) error {
	// Express the retention as whole seconds so it parses as a Postgres interval
	// text literal (e.g. "2592000 seconds").
	interval := fmt.Sprintf("%d seconds", int64(t.retention/time.Second))

	res, err := t.db.ExecContext(ctx, `
		DELETE FROM kb.retrieval_traces
		WHERE created_at < now() - ?::interval
	`, interval)
	if err != nil {
		t.log.Error("failed to clean up retrieval traces", slog.String("error", err.Error()))
		return err
	}

	if n, _ := res.RowsAffected(); n > 0 {
		t.log.Info("deleted expired retrieval traces", slog.Int64("count", n), slog.Duration("retention", t.retention))
	} else {
		t.log.Debug("no expired retrieval traces to delete", slog.Duration("retention", t.retention))
	}

	return nil
}
