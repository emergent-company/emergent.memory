package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// EmbeddingJobPurgeTask deletes terminal jobs (completed, failed, dead_letter)
// older than a retention period across every job table the stale-job sweep
// touches. Terminal rows accumulate because each re-enqueue creates a new row,
// bloating the queue tables and slowing status queries; a bulk stale-sweep
// mis-classification (#705) is reclaimed by the same policy.
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

// purgeTable describes retention for one terminal job table. terminal lists the
// status values that are safe to delete; ageExpr is the timestamp column (or
// expression) that determines a row's retention age.
//
// kb.email_jobs has no updated_at column (see migration 00173) and its terminal
// success status is 'sent', not 'completed' — hence the per-table config.
type purgeTable struct {
	table    string
	terminal []string
	ageExpr  string
}

// purgeTables is every table the stale-job sweep terminal-fails, so a bulk
// mis-classification (#705) is reclaimed by the same retention policy.
var purgeTables = []purgeTable{
	{table: "kb.graph_embedding_jobs", terminal: []string{"completed", "failed", "dead_letter"}, ageExpr: "updated_at"},
	{table: "kb.graph_relationship_embedding_jobs", terminal: []string{"completed", "failed", "dead_letter"}, ageExpr: "updated_at"},
	{table: "kb.chunk_embedding_jobs", terminal: []string{"completed", "failed", "dead_letter"}, ageExpr: "updated_at"},
	{table: "kb.document_parsing_jobs", terminal: []string{"completed", "failed"}, ageExpr: "updated_at"},
	{table: "kb.object_extraction_jobs", terminal: []string{"completed", "failed"}, ageExpr: "updated_at"},
	{table: "kb.email_jobs", terminal: []string{"sent", "failed", "dead_letter"}, ageExpr: "COALESCE(processed_at, created_at)"},
}

// Run deletes terminal jobs older than the retention cutoff from every job
// queue. Best-effort: a failure on one table does not stop the others.
func (t *EmbeddingJobPurgeTask) Run(ctx context.Context) error {
	start := time.Now()
	cutoff := start.AddDate(0, 0, -t.retentionDays)

	totalDeleted := int64(0)
	for _, pt := range purgeTables {
		res, err := t.db.NewDelete().
			TableExpr(pt.table).
			Where("status IN (?)", bun.In(pt.terminal)).
			Where(pt.ageExpr+" < ?", cutoff).
			Exec(ctx)
		if err != nil {
			t.log.Warn("embedding job purge: delete failed",
				slog.String("table", pt.table), slog.String("error", err.Error()))
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			t.log.Info("embedding job purge: deleted terminal jobs",
				slog.String("table", pt.table), slog.Int64("count", n))
		}
		totalDeleted += n
	}

	t.log.Info("embedding job purge: completed",
		slog.Int64("total_deleted", totalDeleted),
		slog.Duration("duration", time.Since(start)))
	return nil
}
