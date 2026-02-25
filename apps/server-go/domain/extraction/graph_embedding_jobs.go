package extraction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/logger"
)

// GraphEmbeddingJobsService manages the graph embedding job queue.
// It provides methods to enqueue, dequeue, and manage embedding jobs for graph objects.
//
// Key features:
// - Idempotent enqueue (won't create duplicate active jobs for same object)
// - Atomic dequeue with FOR UPDATE SKIP LOCKED
// - Exponential backoff for retries
// - Stale job recovery
// - Queue statistics
//
// Note: Jobs retry indefinitely until they succeed (no maxAttempts limit).
type GraphEmbeddingJobsService struct {
	db  bun.IDB
	log *slog.Logger
	cfg *GraphEmbeddingConfig
}

// GraphEmbeddingConfig contains configuration for graph embedding jobs
type GraphEmbeddingConfig struct {
	// BaseRetryDelaySec is the base delay in seconds for retries (default: 60)
	BaseRetryDelaySec int
	// MaxRetryDelaySec is the maximum delay in seconds (default: 3600)
	MaxRetryDelaySec int
	// WorkerIntervalMs is the polling interval in milliseconds (default: 5000)
	WorkerIntervalMs int
	// WorkerBatchSize is the number of jobs to dequeue per poll (default: 50)
	WorkerBatchSize int
	// WorkerConcurrency is the number of jobs processed concurrently per poll (default: 50)
	WorkerConcurrency int
	// EnableAdaptiveScaling enables dynamic concurrency adjustment based on system health
	EnableAdaptiveScaling bool
	// MinConcurrency is the minimum concurrency when adaptive scaling is enabled (default: 1)
	MinConcurrency int
	// MaxConcurrency is the maximum concurrency when adaptive scaling is enabled (default: 10)
	MaxConcurrency int
}

// DefaultGraphEmbeddingConfig returns default configuration
func DefaultGraphEmbeddingConfig() *GraphEmbeddingConfig {
	return &GraphEmbeddingConfig{
		BaseRetryDelaySec:     60,
		MaxRetryDelaySec:      3600,
		WorkerIntervalMs:      5000,
		WorkerBatchSize:       200,
		WorkerConcurrency:     200,
		EnableAdaptiveScaling: true,
		MinConcurrency:        50,
		MaxConcurrency:        500,
	}
}

// WorkerInterval returns the worker interval as a Duration
func (c *GraphEmbeddingConfig) WorkerInterval() time.Duration {
	return time.Duration(c.WorkerIntervalMs) * time.Millisecond
}

// NewGraphEmbeddingJobsService creates a new graph embedding jobs service
func NewGraphEmbeddingJobsService(db bun.IDB, log *slog.Logger, cfg *GraphEmbeddingConfig) *GraphEmbeddingJobsService {
	if cfg == nil {
		cfg = DefaultGraphEmbeddingConfig()
	}
	return &GraphEmbeddingJobsService{
		db:  db,
		log: log.With(logger.Scope("graph.embedding.jobs")),
		cfg: cfg,
	}
}

// EnqueueOptions contains options for enqueuing a graph embedding job
type EnqueueOptions struct {
	ObjectID   string     // Required: the graph object ID to generate embedding for
	Priority   int        // Optional: higher = more urgent (default: 0)
	ScheduleAt *time.Time // Optional: when to process (default: now)
}

// Enqueue creates a new graph embedding job or returns existing active job.
// Idempotent: if an active (pending|processing) job exists for the object, returns it.
func (s *GraphEmbeddingJobsService) Enqueue(ctx context.Context, opts EnqueueOptions) (*GraphEmbeddingJob, error) {
	// Check for existing active job
	existing := &GraphEmbeddingJob{}
	err := s.db.NewSelect().
		Model(existing).
		Where("object_id = ?", opts.ObjectID).
		Where("status IN ('pending', 'processing')").
		Limit(1).
		Scan(ctx)

	if err == nil {
		// Active job exists, return it
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("check existing job: %w", err)
	}

	// No active job, create new one
	scheduleAt := time.Now()
	if opts.ScheduleAt != nil {
		scheduleAt = *opts.ScheduleAt
	}

	job := &GraphEmbeddingJob{
		ObjectID:     opts.ObjectID,
		Status:       JobStatusPending,
		AttemptCount: 0,
		Priority:     opts.Priority,
		ScheduledAt:  scheduleAt,
	}

	_, err = s.db.NewInsert().
		Model(job).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("enqueue graph embedding job: %w", err)
	}

	s.log.Debug("enqueued graph embedding job",
		slog.String("job_id", job.ID),
		slog.String("object_id", job.ObjectID),
		slog.Int("priority", job.Priority))

	return job, nil
}

// EnqueueBatch creates multiple graph embedding jobs in a single transaction.
// Skips objects that already have active jobs.
func (s *GraphEmbeddingJobsService) EnqueueBatch(ctx context.Context, objectIDs []string, priority int) (int, error) {
	if len(objectIDs) == 0 {
		return 0, nil
	}

	// Get objects that already have active jobs
	var existingObjectIDs []string
	err := s.db.NewSelect().
		Model((*GraphEmbeddingJob)(nil)).
		Column("object_id").
		Where("object_id IN (?)", bun.In(objectIDs)).
		Where("status IN ('pending', 'processing')").
		Scan(ctx, &existingObjectIDs)
	if err != nil {
		return 0, fmt.Errorf("check existing jobs: %w", err)
	}

	// Filter out objects with existing jobs
	existingSet := make(map[string]bool, len(existingObjectIDs))
	for _, id := range existingObjectIDs {
		existingSet[id] = true
	}

	var toEnqueue []string
	for _, id := range objectIDs {
		if !existingSet[id] {
			toEnqueue = append(toEnqueue, id)
		}
	}

	if len(toEnqueue) == 0 {
		return 0, nil
	}

	// Create jobs
	now := time.Now()
	jobs := make([]*GraphEmbeddingJob, len(toEnqueue))
	for i, objectID := range toEnqueue {
		jobs[i] = &GraphEmbeddingJob{
			ObjectID:     objectID,
			Status:       JobStatusPending,
			AttemptCount: 0,
			Priority:     priority,
			ScheduledAt:  now,
		}
	}

	_, err = s.db.NewInsert().
		Model(&jobs).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("enqueue batch: %w", err)
	}

	s.log.Debug("enqueued graph embedding jobs batch",
		slog.Int("count", len(jobs)),
		slog.Int("skipped", len(existingObjectIDs)))

	return len(jobs), nil
}

// Dequeue atomically claims jobs for processing.
// Uses PostgreSQL's FOR UPDATE SKIP LOCKED for concurrent workers.
func (s *GraphEmbeddingJobsService) Dequeue(ctx context.Context, batchSize int) ([]*GraphEmbeddingJob, error) {
	if batchSize <= 0 {
		batchSize = s.cfg.WorkerBatchSize
	}

	var jobs []*GraphEmbeddingJob

	// Strategic SQL: FOR UPDATE SKIP LOCKED for concurrent workers
	// Order by priority DESC (higher = more urgent) then by scheduled_at ASC
	err := s.db.NewRaw(`WITH cte AS (
		SELECT id FROM kb.graph_embedding_jobs
		WHERE status = 'pending' 
			AND scheduled_at <= now()
		ORDER BY priority DESC, scheduled_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT ?
	)
	UPDATE kb.graph_embedding_jobs j 
	SET status = 'processing', 
		started_at = now(),
		attempt_count = attempt_count + 1,
		updated_at = now()
	FROM cte WHERE j.id = cte.id
	RETURNING j.*`, batchSize).Scan(ctx, &jobs)
	if err != nil {
		return nil, fmt.Errorf("dequeue graph embedding jobs: %w", err)
	}

	return jobs, nil
}

// MarkCompleted marks a job as successfully completed
func (s *GraphEmbeddingJobsService) MarkCompleted(ctx context.Context, id string) error {
	now := time.Now()
	_, err := s.db.NewUpdate().
		Model((*GraphEmbeddingJob)(nil)).
		Set("status = ?", JobStatusCompleted).
		Set("completed_at = ?", now).
		Set("last_error = NULL").
		Set("updated_at = ?", now).
		Where("id = ?", id).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}

	s.log.Debug("graph embedding job completed", slog.String("job_id", id))
	return nil
}

// MarkFailed marks a job as failed and schedules retry with exponential backoff.
// Unlike email jobs, graph embedding jobs retry indefinitely (no max attempts).
func (s *GraphEmbeddingJobsService) MarkFailed(ctx context.Context, id string, jobErr error) error {
	job := &GraphEmbeddingJob{}
	err := s.db.NewSelect().
		Model(job).
		Column("id", "attempt_count").
		Where("id = ?", id).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			s.log.Warn("graph embedding job not found when marking as failed", slog.String("job_id", id))
			return nil
		}
		return fmt.Errorf("get job for mark failed: %w", err)
	}

	errorMessage := truncateError(jobErr.Error())

	// Calculate exponential backoff: base * attempt^2, capped at max
	delaySeconds := int(math.Min(
		float64(s.cfg.MaxRetryDelaySec),
		float64(s.cfg.BaseRetryDelaySec)*float64(job.AttemptCount)*float64(job.AttemptCount),
	))
	if delaySeconds < s.cfg.BaseRetryDelaySec {
		delaySeconds = s.cfg.BaseRetryDelaySec
	}

	// Requeue for retry
	_, updateErr := s.db.NewRaw(`UPDATE kb.graph_embedding_jobs 
		SET status = 'pending', 
			last_error = ?, 
			scheduled_at = now() + (? || ' seconds')::interval,
			updated_at = now()
		WHERE id = ?`,
		errorMessage, fmt.Sprintf("%d", delaySeconds), id).Exec(ctx)
	if updateErr != nil {
		return fmt.Errorf("requeue failed job: %w", updateErr)
	}

	s.log.Warn("graph embedding job failed, retrying",
		slog.String("job_id", id),
		slog.Int("attempt", job.AttemptCount),
		slog.Duration("retry_delay", time.Duration(delaySeconds)*time.Second),
		slog.String("error", errorMessage))

	return nil
}

// RecoverStaleJobs recovers jobs stuck in 'processing' status.
// This can happen when the server restarts while jobs are being processed.
func (s *GraphEmbeddingJobsService) RecoverStaleJobs(ctx context.Context, staleThresholdMinutes int) (int, error) {
	if staleThresholdMinutes <= 0 {
		staleThresholdMinutes = 10
	}

	result, err := s.db.NewRaw(`UPDATE kb.graph_embedding_jobs 
		SET status = 'pending', 
			started_at = NULL,
			scheduled_at = now(),
			updated_at = now()
		WHERE status = 'processing' 
			AND started_at < now() - (? || ' minutes')::interval`,
		fmt.Sprintf("%d", staleThresholdMinutes)).Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("recover stale jobs: %w", err)
	}

	count, _ := result.RowsAffected()

	if count > 0 {
		s.log.Warn("recovered stale graph embedding jobs",
			slog.Int64("count", count),
			slog.Int("threshold_minutes", staleThresholdMinutes))
	}

	return int(count), nil
}

// GetJob retrieves a job by ID
func (s *GraphEmbeddingJobsService) GetJob(ctx context.Context, id string) (*GraphEmbeddingJob, error) {
	job := &GraphEmbeddingJob{}
	err := s.db.NewSelect().
		Model(job).
		Where("id = ?", id).
		Scan(ctx)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}

	return job, nil
}

// GetActiveJobForObject returns the active job for an object, if any.
func (s *GraphEmbeddingJobsService) GetActiveJobForObject(ctx context.Context, objectID string) (*GraphEmbeddingJob, error) {
	job := &GraphEmbeddingJob{}
	err := s.db.NewSelect().
		Model(job).
		Where("object_id = ?", objectID).
		Where("status IN ('pending', 'processing')").
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active job for object: %w", err)
	}

	return job, nil
}

// Stats returns queue statistics
func (s *GraphEmbeddingJobsService) Stats(ctx context.Context) (*GraphEmbeddingQueueStats, error) {
	stats := &GraphEmbeddingQueueStats{}

	err := s.db.NewRaw(`SELECT 
		COUNT(*) FILTER (WHERE status = 'pending') as pending,
		COUNT(*) FILTER (WHERE status = 'processing') as processing,
		COUNT(*) FILTER (WHERE status = 'completed') as completed,
		COUNT(*) FILTER (WHERE status = 'failed') as failed
	FROM kb.graph_embedding_jobs`).Scan(ctx, &stats.Pending, &stats.Processing, &stats.Completed, &stats.Failed)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	return stats, nil
}

// GraphEmbeddingQueueStats contains queue statistics
type GraphEmbeddingQueueStats struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Completed  int64 `json:"completed"`
	Failed     int64 `json:"failed"`
}

// truncateError truncates an error message to 1000 characters
func truncateError(msg string) string {
	if len(msg) > 1000 {
		return msg[:1000]
	}
	return msg
}
