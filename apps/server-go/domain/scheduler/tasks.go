package scheduler

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/logger"
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
	db           *bun.DB
	log          *slog.Logger
	staleMinutes int
	mu           sync.RWMutex
}

// NewStaleJobCleanupTask creates a new stale job cleanup task
func NewStaleJobCleanupTask(db *bun.DB, log *slog.Logger, staleMinutes int) *StaleJobCleanupTask {
	if staleMinutes <= 0 {
		staleMinutes = 30
	}
	return &StaleJobCleanupTask{
		db:           db,
		log:          log.With(logger.Scope("scheduler.stale_job_cleanup")),
		staleMinutes: staleMinutes,
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
	table          string
	hasStartedAt   bool
	hasCompletedAt bool
	errorColumn    string
}

// Run executes the stale job cleanup
func (t *StaleJobCleanupTask) Run(ctx context.Context) error {
	start := time.Now()
	t.log.Debug("cleaning up stale jobs")

	t.mu.RLock()
	staleMinutes := t.staleMinutes
	t.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(staleMinutes) * time.Minute)
	totalCleaned := int64(0)

	// Clean up stale extraction jobs (multiple tables with different schemas)
	tables := []jobTableConfig{
		{table: "kb.document_parsing_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "error_message"},
		{table: "kb.chunk_embedding_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "last_error"},
		{table: "kb.graph_embedding_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "last_error"},
		{table: "kb.object_extraction_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "error_message"},
		{table: "kb.data_source_sync_jobs", hasStartedAt: true, hasCompletedAt: true, errorColumn: "error_message"},
		{table: "kb.email_jobs", hasStartedAt: false, hasCompletedAt: false, errorColumn: "last_error"},
	}

	for _, cfg := range tables {
		count, err := t.cleanupTable(ctx, cfg, cutoff)
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

// cleanupTable cleans up stale jobs in a specific table
func (t *StaleJobCleanupTask) cleanupTable(ctx context.Context, cfg jobTableConfig, cutoff time.Time) (int64, error) {
	var query string

	if cfg.hasStartedAt && cfg.hasCompletedAt {
		// Tables with started_at and completed_at columns
		query = `
			UPDATE ` + cfg.table + `
			SET status = 'failed',
				` + cfg.errorColumn + ` = 'Job marked as stale during cleanup',
				completed_at = NOW(),
				updated_at = NOW()
		WHERE status IN ('processing', 'running')
		AND started_at < ?
		`
	} else {
		// Tables without started_at (like email_jobs) - use created_at only
		query = `
			UPDATE ` + cfg.table + `
			SET status = 'failed',
				` + cfg.errorColumn + ` = 'Job marked as stale during cleanup'
			WHERE status IN ('pending', 'processing', 'running')
			AND created_at < ?
		`
	}

	var result sql.Result
	var err error

	if cfg.hasStartedAt {
		result, err = t.db.ExecContext(ctx, query, cutoff)
	} else {
		result, err = t.db.ExecContext(ctx, query, cutoff)
	}

	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}
