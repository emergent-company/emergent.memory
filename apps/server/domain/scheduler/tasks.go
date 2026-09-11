package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// RevisionCountRefreshTask refreshes the materialized view for revision counts
type RevisionCountRefreshTask struct {
	db  *bun.DB
	log *slog.Logger
}

// NewRevisionCountRefreshTask creates a new revision count refresh task
func NewRevisionCountRefreshTask(db *bun.DB, log *slog.Logger) *RevisionCountRefreshTask {
	return &RevisionCountRefreshTask{
		db:  db,
		log: log.With(logger.Scope("scheduler.revision_count")),
	}
}

// Run executes the revision count refresh
func (t *RevisionCountRefreshTask) Run(ctx context.Context) error {
	start := time.Now()
	t.log.Debug("refreshing revision counts")

	// Call the PostgreSQL function to refresh counts
	_, err := t.db.ExecContext(ctx, "SELECT kb.refresh_revision_counts()")
	if err != nil {
		t.log.Error("failed to refresh revision counts",
			slog.String("error", err.Error()))
		return err
	}

	t.log.Debug("revision counts refreshed",
		slog.Duration("duration", time.Since(start)))
	return nil
}

// TagCleanupTask removes unused tags from the database
type TagCleanupTask struct {
	db  *bun.DB
	log *slog.Logger
}

// NewTagCleanupTask creates a new tag cleanup task
func NewTagCleanupTask(db *bun.DB, log *slog.Logger) *TagCleanupTask {
	return &TagCleanupTask{
		db:  db,
		log: log.With(logger.Scope("scheduler.tag_cleanup")),
	}
}

// Run executes the tag cleanup
func (t *TagCleanupTask) Run(ctx context.Context) error {
	start := time.Now()
	t.log.Debug("cleaning up unused tags")

	// Delete tags that are not referenced by any graph objects
	result, err := t.db.ExecContext(ctx, `
		DELETE FROM kb.tags t
		WHERE NOT EXISTS (
			SELECT 1 FROM kb.graph_objects go
			WHERE go.properties->'tags' @> to_jsonb(t.name)
		)
	`)
	if err != nil {
		t.log.Error("failed to clean up tags",
			slog.String("error", err.Error()))
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		t.log.Info("cleaned up unused tags",
			slog.Int64("count", rowsAffected),
			slog.Duration("duration", time.Since(start)))
	} else {
		t.log.Debug("no unused tags to clean up",
			slog.Duration("duration", time.Since(start)))
	}

	return nil
}

// CacheCleanupTask removes expired cache entries
type CacheCleanupTask struct {
	db  *bun.DB
	log *slog.Logger
}

// NewCacheCleanupTask creates a new cache cleanup task
func NewCacheCleanupTask(db *bun.DB, log *slog.Logger) *CacheCleanupTask {
	return &CacheCleanupTask{
		db:  db,
		log: log.With(logger.Scope("scheduler.cache_cleanup")),
	}
}

// Run executes the cache cleanup
func (t *CacheCleanupTask) Run(ctx context.Context) error {
	start := time.Now()
	t.log.Debug("cleaning up expired cache entries")

	// Delete expired introspection cache entries
	result, err := t.db.ExecContext(ctx, `
		DELETE FROM kb.auth_introspection_cache
		WHERE expires_at < NOW()
	`)
	if err != nil {
		t.log.Error("failed to clean up cache",
			slog.String("error", err.Error()))
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		t.log.Info("cleaned up expired cache entries",
			slog.Int64("count", rowsAffected),
			slog.Duration("duration", time.Since(start)))
	} else {
		t.log.Debug("no expired cache entries to clean up",
			slog.Duration("duration", time.Since(start)))
	}

	return nil
}

// StaleJobCleanupTask marks stale jobs as failed across all job queues
type StaleJobCleanupTask struct {
	db                          *bun.DB
	log                         *slog.Logger
	staleMinutes                int
	documentParsingStaleMinutes int
	mu                          sync.RWMutex
}

// NewStaleJobCleanupTask creates a new stale job cleanup task.
// documentParsingStaleMinutes sets a longer stale threshold for document parsing jobs
// (audio transcription can take hours); 0 falls back to staleMinutes.
func NewStaleJobCleanupTask(db *bun.DB, log *slog.Logger, staleMinutes, documentParsingStaleMinutes int) *StaleJobCleanupTask {
	if staleMinutes <= 0 {
		staleMinutes = 30
	}
	if documentParsingStaleMinutes <= 0 {
		documentParsingStaleMinutes = staleMinutes
	}
	return &StaleJobCleanupTask{
		db:                          db,
		log:                         log.With(logger.Scope("scheduler.stale_job_cleanup")),
		staleMinutes:                staleMinutes,
		documentParsingStaleMinutes: documentParsingStaleMinutes,
	}
}

// SetStaleMinutes updates the stale threshold at runtime.
func (t *StaleJobCleanupTask) SetStaleMinutes(minutes int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.staleMinutes = minutes
}

// GetStaleMinutes returns the current stale threshold.
func (t *StaleJobCleanupTask) GetStaleMinutes() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.staleMinutes
}

// jobTableConfig holds the configuration for cleaning up a specific job table
type jobTableConfig struct {
	table                string
	hasStartedAt         bool
	hasCompletedAt       bool
	errorColumn          string
	staleMinutesOverride int // when non-zero, overrides the global stale threshold for this table
}

// Run executes the stale job cleanup
func (t *StaleJobCleanupTask) Run(ctx context.Context) error {
	start := time.Now()
	t.log.Debug("cleaning up stale jobs")

	t.mu.RLock()
	staleMinutes := t.staleMinutes
	documentParsingStaleMinutes := t.documentParsingStaleMinutes
	t.mu.RUnlock()

	totalCleaned := int64(0)

	// Clean up stale extraction jobs (multiple tables with different schemas)
	tables := []jobTableConfig{
		{table: "kb.document_parsing_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "error_message", staleMinutesOverride: documentParsingStaleMinutes},
		{table: "kb.chunk_embedding_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "last_error"},
		{table: "kb.graph_embedding_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "last_error"},
		{table: "kb.object_extraction_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "error_message"},
		{table: "kb.email_jobs", hasStartedAt: false, hasCompletedAt: false, errorColumn: "last_error"},
	}

	for _, cfg := range tables {
		count, err := t.cleanupTable(ctx, cfg, staleMinutes)
		if err != nil {
			t.log.Warn("failed to clean up stale jobs in table",
				slog.String("table", cfg.table),
				slog.String("error", err.Error()))
			continue
		}
		if count > 0 {
			t.log.Info("cleaned up stale jobs",
				slog.String("table", cfg.table),
				slog.Int64("count", count))
			totalCleaned += count
		}
	}

	t.log.Debug("stale job cleanup completed",
		slog.Int64("total_cleaned", totalCleaned),
		slog.Duration("duration", time.Since(start)))

	return nil
}

// cleanupTable cleans up stale jobs in a specific table.
// globalStaleMinutes is the default threshold; cfg.staleMinutesOverride takes precedence when non-zero.
func (t *StaleJobCleanupTask) cleanupTable(ctx context.Context, cfg jobTableConfig, globalStaleMinutes int) (int64, error) {
	effectiveMinutes := globalStaleMinutes
	if cfg.staleMinutesOverride > 0 {
		effectiveMinutes = cfg.staleMinutesOverride
	}
	cutoff := time.Now().Add(-time.Duration(effectiveMinutes) * time.Minute)

	var (
		query string
		args  []any
	)

	if cfg.hasStartedAt && cfg.hasCompletedAt {
		// Tables with started_at and completed_at columns. Mark any non-terminal
		// job stale when it has an old started_at, or when it never started
		// (started_at IS NULL) but has an old created_at.
		query = `
			UPDATE ` + cfg.table + `
			SET status = 'failed',
				` + cfg.errorColumn + ` = 'Job marked as stale during cleanup',
				completed_at = NOW(),
				updated_at = NOW()
		WHERE status IN ('pending', 'processing', 'running')
		AND (
			(started_at IS NOT NULL AND started_at < ?)
			OR (started_at IS NULL AND created_at < ?)
		)
		`
		args = []any{cutoff, cutoff}
	} else {
		// Tables without started_at (like email_jobs) - use created_at only
		query = `
			UPDATE ` + cfg.table + `
			SET status = 'failed',
				` + cfg.errorColumn + ` = 'Job marked as stale during cleanup'
			WHERE status IN ('pending', 'processing', 'running')
			AND created_at < ?
		`
		args = []any{cutoff}
	}

	result, err := t.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// projectDeletionBatchSize is the maximum number of projects selected per batch.
const projectDeletionBatchSize = 50

// projectDeletionMaxBatches caps how many batches a single sweep processes so a
// large backlog drains gradually without exceeding the scheduler task timeout.
const projectDeletionMaxBatches = 20

// ProjectDeletionTask hard-purges projects whose deletion grace period elapsed.
// It runs as a durable scheduler task so pending deletions survive restarts
// (unlike the previous in-process goroutine).
//
// Each due project is deleted by its own guarded statement rather than one
// bulk DELETE. This isolates poison pills: if a single project's cascade always
// errors or times out, only that project is skipped — the rest of the batch
// still makes progress instead of rolling back and being retried forever.
type ProjectDeletionTask struct {
	db  *bun.DB
	log *slog.Logger
}

// NewProjectDeletionTask creates a new project deletion sweep task.
func NewProjectDeletionTask(db *bun.DB, log *slog.Logger) *ProjectDeletionTask {
	return &ProjectDeletionTask{
		db:  db,
		log: log.With(logger.Scope("scheduler.project_deletion")),
	}
}

// Run executes the project deletion sweep. Zero rows is a successful no-op.
func (t *ProjectDeletionTask) Run(ctx context.Context) error {
	start := time.Now()
	total := 0

	for i := 0; i < projectDeletionMaxBatches; i++ {
		// Select a bounded FIFO batch of due project IDs. No row locking here:
		// the guarded DELETE below re-verifies each row, so a concurrent
		// restore/reschedule is handled safely without a held lock.
		var ids []string
		err := t.db.NewSelect().
			TableExpr("kb.projects").
			Column("id").
			Where("deletion_scheduled_for IS NOT NULL").
			Where("deletion_scheduled_for <= now()").
			Order("deletion_scheduled_for ASC").
			Limit(projectDeletionBatchSize).
			Scan(ctx, &ids)
		if err != nil {
			t.log.Error("failed to select projects due for purge",
				slog.String("error", err.Error()))
			return err
		}
		if len(ids) == 0 {
			break
		}

		deletedInBatch := 0
		for _, id := range ids {
			// Guarded delete: re-check the schedule at delete time. A project
			// whose deletion was cancelled (restored) or rescheduled between the
			// SELECT and this DELETE no longer matches — 0 rows affected — so it is
			// never hard-deleted by a stale list, closing the TOCTOU window.
			result, derr := t.db.NewRaw(`
				DELETE FROM kb.projects
				WHERE id = ?
				  AND deletion_scheduled_for IS NOT NULL
				  AND deletion_scheduled_for <= now()`, id).Exec(ctx)
			if derr != nil {
				// Poison-pill isolation: log and continue so one bad cascade does
				// not abort the whole run.
				t.log.Error("failed to purge project, skipping",
					slog.String("projectID", id),
					slog.String("error", derr.Error()))
				continue
			}
			if n, _ := result.RowsAffected(); n > 0 {
				deletedInBatch++
			}
		}
		total += deletedInBatch

		if len(ids) < projectDeletionBatchSize {
			break
		}
		// When a full batch yields no deletions, the remaining due rows are all
		// poison pills; stop looping over the same batch this run.
		if deletedInBatch == 0 {
			break
		}
	}

	if total > 0 {
		t.log.Info("purged expired projects",
			slog.Int("count", total),
			slog.Duration("duration", time.Since(start)))
	} else {
		t.log.Debug("no expired projects to purge",
			slog.Duration("duration", time.Since(start)))
	}

	return nil
}
