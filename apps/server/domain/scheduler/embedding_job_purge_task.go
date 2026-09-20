package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// EmbeddingJobPurgeTask deletes terminal embedding jobs (completed, failed,
// dead_letter) older than a retention period. Terminal rows accumulate because
// each re-enqueue creates a new row, bloating the queue tables and slowing
// status queries.
type EmbeddingJobPurgeTask struct {
	db            *bun.DB
	log           *slog.Logger
	retentionDays int
}

// NewEmbeddingJobPurgeTask creates a new EmbeddingJobPurgeTask.
// retentionDays is the minimum age (in days) of a terminal job before deletion.
// Defaults to 7 if <= 0.
func NewEmbeddingJobPurgeTask(db *bun.DB, log *slog.Logger, retentionDays int) *EmbeddingJobPurgeTask {
	if retentionDays <= 0 {
		retentionDays = 7
	}
	return &EmbeddingJobPurgeTask{
		db:            db,
		log:           log.With(logger.Scope("scheduler.embedding_job_purge")),
		retentionDays: retentionDays,
	}
}

// Run deletes terminal embedding jobs older than the retention cutoff from all
// three embedding job queues. Best-effort: a failure on one table does not stop
// the others.
func (t *EmbeddingJobPurgeTask) Run(ctx context.Context) error {
	start := time.Now()
	cutoff := start.AddDate(0, 0, -t.retentionDays)

	tables := []string{
		"kb.graph_embedding_jobs",
		"kb.graph_relationship_embedding_jobs",
		"kb.chunk_embedding_jobs",
	}

	totalDeleted := int64(0)
	for _, table := range tables {
		res, err := t.db.NewDelete().
			TableExpr(table).
			Where("status IN ('completed', 'failed', 'dead_letter')").
			Where("updated_at < ?", cutoff).
			Exec(ctx)
		if err != nil {
			t.log.Warn("embedding job purge: delete failed",
				slog.String("table", table), slog.String("error", err.Error()))
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			t.log.Info("embedding job purge: deleted terminal jobs",
				slog.String("table", table), slog.Int64("count", n))
		}
		totalDeleted += n
	}

	t.log.Info("embedding job purge: completed",
		slog.Int64("total_deleted", totalDeleted),
		slog.Duration("duration", time.Since(start)))
	return nil
}
