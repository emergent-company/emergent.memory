package scheduler

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/jobs"
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

const (
	// staleJobMessage is stamped on every job terminal-failed by the sweep. The
	// canonical value lives in internal/jobs so the reporting queries that split
	// stale-sweep rows out of "failed" use the exact same string.
	staleJobMessage = jobs.StaleJobMessage

	// staleJobMassReapAlert is the stable log/alert marker emitted when a single
	// sweep terminal-fails more jobs in one table than massReapThreshold.
	staleJobMassReapAlert = "mass_stale_reap"

	// defaultStaleJobMassReapThreshold is the per-table reap count above which a
	// single sweep emits a mass-reap alert. A bulk enqueue draining a backlog can
	// legitimately reap a handful of genuinely stuck in-flight jobs; hundreds or
	// thousands in one sweep is the signal that the sweep is mis-classifying
	// queued work as stale (issue #705).
	defaultStaleJobMassReapThreshold = 1000
)

// StaleJobCleanupTask marks stale jobs as failed across all job queues
type StaleJobCleanupTask struct {
	db                          *bun.DB
	log                         *slog.Logger
	staleMinutes                int
	documentParsingStaleMinutes int
	massReapThreshold           int
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
		massReapThreshold:           defaultStaleJobMassReapThreshold,
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

// SetMassReapThreshold sets the per-table reap count above which a single sweep
// emits a mass-reap alert. Values <= 0 restore the default.
func (t *StaleJobCleanupTask) SetMassReapThreshold(threshold int) {
	if threshold <= 0 {
		threshold = defaultStaleJobMassReapThreshold
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.massReapThreshold = threshold
}

// GetMassReapThreshold returns the current mass-reap alert threshold.
func (t *StaleJobCleanupTask) GetMassReapThreshold() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.massReapThreshold
}

// jobTableConfig holds the configuration for cleaning up a specific job table
type jobTableConfig struct {
	table                string
	hasStartedAt         bool
	hasCompletedAt       bool
	errorColumn          string
	staleMinutesOverride int // when non-zero, overrides the global stale threshold for this table
}

// effectiveStaleMinutes resolves the stale threshold for this table.
func (cfg jobTableConfig) effectiveStaleMinutes(global int) int {
	if cfg.staleMinutesOverride > 0 {
		return cfg.staleMinutesOverride
	}
	return global
}

// Run executes the stale job cleanup
func (t *StaleJobCleanupTask) Run(ctx context.Context) error {
	start := time.Now()
	t.log.Debug("cleaning up stale jobs")

	t.mu.RLock()
	staleMinutes := t.staleMinutes
	documentParsingStaleMinutes := t.documentParsingStaleMinutes
	massReapThreshold := t.massReapThreshold
	t.mu.RUnlock()

	totalCleaned := int64(0)

	// Clean up stale extraction jobs (multiple tables with different schemas)
	tables := []jobTableConfig{
		{table: "kb.document_parsing_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "error_message", staleMinutesOverride: documentParsingStaleMinutes},
		{table: "kb.chunk_embedding_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "last_error"},
		{table: "kb.graph_embedding_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "last_error"},
		{table: "kb.object_extraction_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "error_message"},
		{table: "kb.email_jobs", hasStartedAt: true, hasCompletedAt: false, errorColumn: "last_error"},
	}

	for _, cfg := range tables {
		count, err := t.cleanupTable(ctx, cfg, staleMinutes)
		if err != nil {
			// A failed sweep is not a soft miss: jobs stuck in
			// processing/running would accumulate indefinitely. Log at error
			// level (not warn) so the failure is visible operationally, matching
			// the mass-reap alert already emitted at error level.
			t.log.Error("failed to clean up stale jobs in table",
				slog.String("table", cfg.table),
				slog.String("error", err.Error()))
			continue
		}
		if count <= 0 {
			continue
		}

		totalCleaned += count
		staleJobsReapedTotal.WithLabelValues(cfg.table).Add(float64(count))
		t.log.Info("cleaned up stale jobs",
			slog.String("table", cfg.table),
			slog.Int64("count", count))

		// A single sweep reaping an unexpectedly large batch means the sweep is
		// probably mis-classifying queued work as stale. Make it loud rather than
		// letting a mass-fail pass as routine cleanup (issue #705).
		if massReapThreshold > 0 && count > int64(massReapThreshold) {
			t.log.Error("mass stale-job reap detected",
				slog.String("alert", staleJobMassReapAlert),
				slog.String("table", cfg.table),
				slog.Int64("count", count),
				slog.Int("threshold", massReapThreshold),
				slog.Int("stale_minutes", cfg.effectiveStaleMinutes(staleMinutes)))
		}
	}

	if massReapThreshold > 0 && totalCleaned > int64(massReapThreshold) {
		t.log.Error("mass stale-job reap detected across tables",
			slog.String("alert", staleJobMassReapAlert),
			slog.Int64("total_cleaned", totalCleaned),
			slog.Int("threshold", massReapThreshold))
	}

	t.log.Debug("stale job cleanup completed",
		slog.Int64("total_cleaned", totalCleaned),
		slog.Duration("duration", time.Since(start)))

	return nil
}

// cleanupStaleJobsQuery builds the UPDATE statement that terminal-fails stale
// jobs in cfg.table, plus its bind args. It is pure so the predicate selection
// can be unit-tested without a live database.
//
// Only jobs that actually started are reaped: rows in processing/running whose
// in-flight timestamp (started_at when the table has one, else created_at) is
// older than the cutoff. 'pending' rows are deliberately never terminal-failed:
// a never-started job is queued behind a backlog, not stale, and failing it
// drops work. This applies to every swept table.
func cleanupStaleJobsQuery(cfg jobTableConfig, cutoff time.Time) (string, []any) {
	startedAt := "created_at"
	notNull := ""
	if cfg.hasStartedAt {
		startedAt = "started_at"
		notNull = "\n\t\t\tAND started_at IS NOT NULL"
	}

	// Build the SET list as a slice of assignments and join them, so a missing
	// separator between fragments can never silently corrupt the statement
	// (issue #893: the bookkeeping fragment was concatenated without a comma,
	// yielding a 42601 syntax error for every hasCompletedAt table).
	setClauses := []string{
		"status = 'failed'",
		cfg.errorColumn + " = '" + staleJobMessage + "'",
	}
	if cfg.hasCompletedAt {
		setClauses = append(setClauses,
			"completed_at = NOW()",
			"updated_at = NOW()",
		)
	}

	query := `
		UPDATE ` + cfg.table + `
		SET ` + strings.Join(setClauses, ",\n\t\t\t") + `
		WHERE status IN ('processing', 'running')` + notNull + `
		AND ` + startedAt + ` < ?`
	return query, []any{cutoff}
}

// cleanupTable cleans up stale jobs in a specific table.
// globalStaleMinutes is the default threshold; cfg.staleMinutesOverride takes precedence when non-zero.
func (t *StaleJobCleanupTask) cleanupTable(ctx context.Context, cfg jobTableConfig, globalStaleMinutes int) (int64, error) {
	cutoff := time.Now().Add(-time.Duration(cfg.effectiveStaleMinutes(globalStaleMinutes)) * time.Minute)

	query, args := cleanupStaleJobsQuery(cfg, cutoff)

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
