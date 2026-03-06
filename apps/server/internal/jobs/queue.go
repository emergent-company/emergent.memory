// Package jobs provides a PostgreSQL-backed job queue implementation.
//
// This follows the same pattern as the NestJS BaseJobQueueService:
// - Idempotent enqueue (won't create duplicate active jobs)
// - Atomic dequeue with FOR UPDATE SKIP LOCKED
// - Exponential backoff for retries
// - Stale job recovery
// - Queue statistics
package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/uptrace/bun"
)

// JobStatus represents the state of a job
type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
	StatusSent       JobStatus = "sent" // For email jobs specifically
)

// QueueConfig contains configuration for a job queue
type QueueConfig struct {
	// TableName is the fully qualified table name (e.g., "kb.email_jobs")
	TableName string
	// EntityIDColumn is the column name for the entity ID (e.g., "object_id")
	EntityIDColumn string
	// MaxAttempts is the maximum number of retry attempts (0 = unlimited)
	MaxAttempts int
	// BaseRetryDelaySec is the base delay in seconds for retries (default: 60)
	BaseRetryDelaySec int
	// MaxRetryDelaySec is the maximum retry delay in seconds (default: 3600)
	MaxRetryDelaySec int
	// BatchSize is the default number of jobs to dequeue at once (default: 10)
	BatchSize int
}

// DefaultQueueConfig returns a QueueConfig with sensible defaults
func DefaultQueueConfig(tableName, entityIDColumn string) QueueConfig {
	return QueueConfig{
		TableName:         tableName,
		EntityIDColumn:    entityIDColumn,
		MaxAttempts:       0, // unlimited
		BaseRetryDelaySec: 60,
		MaxRetryDelaySec:  3600,
		BatchSize:         10,
	}
}

// Queue provides base job queue operations using PostgreSQL.
// It uses FOR UPDATE SKIP LOCKED for concurrent worker safety.
type Queue struct {
	db     bun.IDB
	config QueueConfig
	log    *slog.Logger
}

// NewQueue creates a new job queue with the given configuration
func NewQueue(db bun.IDB, config QueueConfig, log *slog.Logger) *Queue {
	// Apply defaults
	if config.BaseRetryDelaySec == 0 {
		config.BaseRetryDelaySec = 60
	}
	if config.MaxRetryDelaySec == 0 {
		config.MaxRetryDelaySec = 3600
	}
	if config.BatchSize == 0 {
		config.BatchSize = 10
	}

	return &Queue{
		db:     db,
		config: config,
		log:    log,
	}
}

// DequeueResult contains the IDs of dequeued jobs
type DequeueResult struct {
	IDs []string
}

// Dequeue atomically claims jobs for processing.
//
// Uses PostgreSQL's FOR UPDATE SKIP LOCKED for concurrent workers.
// This is the key pattern that allows multiple workers to safely process jobs
// without conflicts.
//
// SQL Pattern:
//
//	WITH cte AS (
//	  SELECT id FROM table
//	  WHERE status='pending' AND scheduled_at <= now()
//	  ORDER BY priority DESC, scheduled_at ASC
//	  FOR UPDATE SKIP LOCKED
//	  LIMIT $1
//	)
//	UPDATE table SET status='processing', started_at=now()
//	FROM cte WHERE table.id = cte.id
//	RETURNING id
func (q *Queue) Dequeue(ctx context.Context, batchSize int) ([]string, error) {
	if batchSize <= 0 {
		batchSize = q.config.BatchSize
	}

	// This is strategic SQL that cannot be expressed with Bun's query builder
	query := fmt.Sprintf(`
		WITH cte AS (
			SELECT id FROM %s
			WHERE status='pending' AND (scheduled_at IS NULL OR scheduled_at <= now())
			ORDER BY priority DESC, scheduled_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE %s j 
		SET status='processing', started_at=now(), updated_at=now()
		FROM cte WHERE j.id = cte.id
		RETURNING j.id`,
		q.config.TableName, q.config.TableName)

	var ids []string
	_, err := q.db.NewRaw(query, batchSize).Exec(ctx, &ids)
	if err != nil {
		return nil, fmt.Errorf("dequeue failed: %w", err)
	}

	return ids, nil
}

// MarkCompleted marks a job as completed
func (q *Queue) MarkCompleted(ctx context.Context, id string) error {
	query := fmt.Sprintf(`
		UPDATE %s 
		SET status = 'completed', 
			completed_at = now(), 
			updated_at = now()
		WHERE id = $1`,
		q.config.TableName)

	_, err := q.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("mark completed failed: %w", err)
	}

	return nil
}

// MarkSent marks a job as sent (for email jobs)
func (q *Queue) MarkSent(ctx context.Context, id string) error {
	query := fmt.Sprintf(`
		UPDATE %s 
		SET status = 'sent', 
			processed_at = now(), 
			updated_at = now()
		WHERE id = $1`,
		q.config.TableName)

	_, err := q.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("mark sent failed: %w", err)
	}

	return nil
}

// MarkFailed marks a job as failed and schedules for retry with exponential backoff.
// If maxAttempts is configured and reached, the job is permanently marked as failed.
func (q *Queue) MarkFailed(ctx context.Context, id string, attemptCount int, errMsg string) error {
	attempt := attemptCount + 1

	// Check if we've exceeded max attempts
	if q.config.MaxAttempts > 0 && attempt >= q.config.MaxAttempts {
		query := fmt.Sprintf(`
			UPDATE %s 
			SET status = 'failed',
				attempt_count = $2,
				last_error = $3,
				updated_at = now()
			WHERE id = $1`,
			q.config.TableName)

		_, err := q.db.ExecContext(ctx, query, id, attempt, truncateError(errMsg))
		if err != nil {
			return fmt.Errorf("mark failed (permanent) failed: %w", err)
		}

		q.log.Warn("job permanently failed after max attempts",
			slog.String("job_id", id),
			slog.Int("attempts", attempt),
			slog.String("error", errMsg))

		return nil
	}

	// Calculate exponential backoff: baseDelay * attempt^2, capped at maxRetryDelaySec
	delay := math.Min(
		float64(q.config.MaxRetryDelaySec),
		float64(q.config.BaseRetryDelaySec)*float64(attempt)*float64(attempt),
	)

	query := fmt.Sprintf(`
		UPDATE %s 
		SET status = 'pending',
			attempt_count = $2,
			last_error = $3,
			scheduled_at = now() + ($4 || ' seconds')::interval,
			updated_at = now()
		WHERE id = $1`,
		q.config.TableName)

	_, err := q.db.ExecContext(ctx, query, id, attempt, truncateError(errMsg), fmt.Sprintf("%d", int(delay)))
	if err != nil {
		return fmt.Errorf("mark failed (retry) failed: %w", err)
	}

	q.log.Debug("job scheduled for retry",
		slog.String("job_id", id),
		slog.Int("attempt", attempt),
		slog.Duration("delay", time.Duration(delay)*time.Second))

	return nil
}

// RecoverStaleJobs recovers jobs stuck in 'processing' status.
// This can happen when the server restarts while jobs are being processed.
// Returns the number of jobs recovered.
func (q *Queue) RecoverStaleJobs(ctx context.Context, staleThresholdMinutes int) (int, error) {
	if staleThresholdMinutes <= 0 {
		staleThresholdMinutes = 10
	}

	query := fmt.Sprintf(`
		UPDATE %s 
		SET status = 'pending',
			started_at = NULL,
			scheduled_at = now(),
			updated_at = now()
		WHERE status = 'processing'
			AND started_at < now() - ($1 || ' minutes')::interval`,
		q.config.TableName)

	result, err := q.db.ExecContext(ctx, query, fmt.Sprintf("%d", staleThresholdMinutes))
	if err != nil {
		return 0, fmt.Errorf("recover stale jobs failed: %w", err)
	}

	count, _ := result.RowsAffected()

	if count > 0 {
		q.log.Warn("recovered stale jobs",
			slog.Int64("count", count),
			slog.Int("threshold_minutes", staleThresholdMinutes))
	}

	return int(count), nil
}

// Stats represents queue statistics
type Stats struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Completed  int64 `json:"completed"`
	Failed     int64 `json:"failed"`
}

// GetStats returns queue statistics
func (q *Queue) GetStats(ctx context.Context) (*Stats, error) {
	query := fmt.Sprintf(`
		SELECT 
			COUNT(*) FILTER (WHERE status = 'pending') as pending,
			COUNT(*) FILTER (WHERE status = 'processing') as processing,
			COUNT(*) FILTER (WHERE status = 'completed' OR status = 'sent') as completed,
			COUNT(*) FILTER (WHERE status = 'failed') as failed
		FROM %s`,
		q.config.TableName)

	stats := &Stats{}
	err := q.db.QueryRowContext(ctx, query).Scan(&stats.Pending, &stats.Processing, &stats.Completed, &stats.Failed)
	if err != nil {
		return nil, fmt.Errorf("get stats failed: %w", err)
	}

	return stats, nil
}

// GetJobByID retrieves a job by its ID. Returns nil if not found.
// The result is scanned into the provided destination.
func (q *Queue) GetJobByID(ctx context.Context, id string, dest interface{}) error {
	query := fmt.Sprintf(`SELECT * FROM %s WHERE id = $1`, q.config.TableName)
	err := q.db.NewRaw(query, id).Scan(ctx, dest)
	if err == sql.ErrNoRows {
		return nil
	}
	return err
}

// truncateError truncates an error message to 500 characters
func truncateError(msg string) string {
	if len(msg) > 500 {
		return msg[:500]
	}
	return msg
}
