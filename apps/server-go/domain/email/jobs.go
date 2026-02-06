package email

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/logger"
)

// JobsService manages the email job queue.
// It provides methods to enqueue, dequeue, and manage email jobs.
type JobsService struct {
	db  bun.IDB
	log *slog.Logger
	cfg *Config
}

// NewJobsService creates a new email jobs service
func NewJobsService(db bun.IDB, log *slog.Logger, cfg *Config) *JobsService {
	return &JobsService{
		db:  db,
		log: log.With(logger.Scope("email.jobs")),
		cfg: cfg,
	}
}

// EnqueueOptions contains options for enqueuing an email job
type EnqueueOptions struct {
	TemplateName string
	ToEmail      string
	ToName       *string
	Subject      string
	TemplateData map[string]interface{}
	SourceType   *string
	SourceID     *string
	MaxAttempts  *int
}

// Enqueue creates a new email job ready for immediate processing.
//
// Uses PostgreSQL now() for next_retry_at to ensure clock consistency
// with the dequeue() query.
func (s *JobsService) Enqueue(ctx context.Context, opts EnqueueOptions) (*EmailJob, error) {
	maxAttempts := s.cfg.MaxRetries
	if opts.MaxAttempts != nil {
		maxAttempts = *opts.MaxAttempts
	}

	templateData := opts.TemplateData
	if templateData == nil {
		templateData = make(map[string]interface{})
	}

	// Serialize template data to JSON
	templateDataJSON, err := json.Marshal(templateData)
	if err != nil {
		return nil, fmt.Errorf("marshal template data: %w", err)
	}

	job := &EmailJob{}

	// Use raw SQL for now() to ensure clock consistency
	// Bun's NewRaw uses ? placeholders
	err = s.db.NewRaw(`INSERT INTO kb.email_jobs (
		template_name, to_email, to_name, subject, template_data,
		status, attempts, max_attempts, source_type, source_id, next_retry_at
	) VALUES (?, ?, ?, ?, ?, 'pending', 0, ?, ?, ?, now())
	RETURNING *`,
		opts.TemplateName,
		opts.ToEmail,
		opts.ToName,
		opts.Subject,
		string(templateDataJSON),
		maxAttempts,
		opts.SourceType,
		opts.SourceID,
	).Scan(ctx, job)

	if err != nil {
		return nil, fmt.Errorf("enqueue email job: %w", err)
	}

	s.log.Debug("enqueued email job",
		slog.String("job_id", job.ID),
		slog.String("to_email", job.ToEmail),
		slog.String("template", job.TemplateName))

	return job, nil
}

// Dequeue atomically claims jobs for processing.
//
// Uses PostgreSQL's FOR UPDATE SKIP LOCKED for concurrent workers.
// This pattern allows multiple workers to safely process jobs without conflicts.
func (s *JobsService) Dequeue(ctx context.Context, batchSize int) ([]*EmailJob, error) {
	if batchSize <= 0 {
		batchSize = s.cfg.WorkerBatchSize
	}

	var jobs []*EmailJob

	// Strategic SQL: FOR UPDATE SKIP LOCKED for concurrent workers
	// Bun's NewRaw uses ? placeholders
	err := s.db.NewRaw(`WITH cte AS (
		SELECT id FROM kb.email_jobs
		WHERE status='pending' 
			AND (next_retry_at IS NULL OR next_retry_at <= now())
		ORDER BY created_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT ?
	)
	UPDATE kb.email_jobs j 
	SET status='processing', 
		attempts=attempts+1
	FROM cte WHERE j.id = cte.id
	RETURNING j.*`, batchSize).Scan(ctx, &jobs)
	if err != nil {
		return nil, fmt.Errorf("dequeue email jobs: %w", err)
	}

	return jobs, nil
}

// MarkSent marks a job as sent successfully
func (s *JobsService) MarkSent(ctx context.Context, id string, messageID string) error {
	now := time.Now()
	_, err := s.db.NewUpdate().
		Model((*EmailJob)(nil)).
		Set("status = ?", JobStatusSent).
		Set("mailgun_message_id = ?", messageID).
		Set("processed_at = ?", now).
		Set("last_error = NULL").
		Where("id = ?", id).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("mark sent: %w", err)
	}

	s.log.Debug("email job marked as sent",
		slog.String("job_id", id),
		slog.String("message_id", messageID))

	return nil
}

// MarkFailed marks a job as failed.
// If attempts < maxAttempts, requeue with exponential backoff.
// Otherwise, mark as permanently failed.
func (s *JobsService) MarkFailed(ctx context.Context, id string, jobErr error) error {
	job := &EmailJob{}
	selectErr := s.db.NewSelect().
		Model(job).
		Column("id", "attempts", "max_attempts").
		Where("id = ?", id).
		Scan(ctx)

	if selectErr != nil {
		if selectErr == sql.ErrNoRows {
			s.log.Warn("email job not found when marking as failed", slog.String("job_id", id))
			return nil
		}
		return fmt.Errorf("get job for mark failed: %w", selectErr)
	}

	errorMessage := truncateError(jobErr.Error())

	if job.Attempts < job.MaxAttempts {
		// Calculate exponential backoff: base * attempt^2, capped at 1 hour
		delaySeconds := int(math.Min(
			3600,
			float64(s.cfg.RetryDelaySec)*float64(job.Attempts)*float64(job.Attempts),
		))

		// Requeue for retry using Bun's NewRaw with ? placeholders
		_, updateErr := s.db.NewRaw(`UPDATE kb.email_jobs 
			SET status='pending', 
				last_error=?, 
				next_retry_at=now() + (? || ' seconds')::interval
			WHERE id=?`,
			errorMessage, fmt.Sprintf("%d", delaySeconds), id).Exec(ctx)
		if updateErr != nil {
			return fmt.Errorf("requeue failed job: %w", updateErr)
		}

		s.log.Warn("email job failed, retrying",
			slog.String("job_id", id),
			slog.Int("attempt", job.Attempts),
			slog.Int("max_attempts", job.MaxAttempts),
			slog.Duration("retry_delay", time.Duration(delaySeconds)*time.Second),
			slog.String("error", errorMessage))
	} else {
		// Max retries exceeded - move to dead letter queue
		now := time.Now()
		_, updateErr := s.db.NewUpdate().
			Model((*EmailJob)(nil)).
			Set("status = ?", JobStatusDeadLetter).
			Set("last_error = ?", errorMessage).
			Set("processed_at = ?", now).
			Where("id = ?", id).
			Exec(ctx)

		if updateErr != nil {
			return fmt.Errorf("mark as dead letter: %w", updateErr)
		}

		s.log.Error("email job moved to dead letter queue",
			slog.String("job_id", id),
			slog.Int("attempts", job.Attempts),
			slog.String("error", errorMessage))
	}

	return nil
}

// RecoverStaleJobs recovers jobs stuck in 'processing' status.
// This can happen when the server restarts while jobs are being processed.
func (s *JobsService) RecoverStaleJobs(ctx context.Context, staleThresholdMinutes int) (int, error) {
	if staleThresholdMinutes <= 0 {
		staleThresholdMinutes = 10
	}

	// Use Bun's NewRaw with ? placeholders
	result, err := s.db.NewRaw(`UPDATE kb.email_jobs 
		SET status = 'pending', 
			next_retry_at = now()
		WHERE status = 'processing' 
			AND created_at < now() - (? || ' minutes')::interval`,
		fmt.Sprintf("%d", staleThresholdMinutes)).Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("recover stale jobs: %w", err)
	}

	count, _ := result.RowsAffected()

	if count > 0 {
		s.log.Warn("recovered stale email jobs",
			slog.Int64("count", count),
			slog.Int("threshold_minutes", staleThresholdMinutes))
	}

	return int(count), nil
}

// GetJob retrieves a job by ID
func (s *JobsService) GetJob(ctx context.Context, id string) (*EmailJob, error) {
	job := &EmailJob{}
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

// GetJobsBySource retrieves jobs by source type and ID
func (s *JobsService) GetJobsBySource(ctx context.Context, sourceType, sourceID string) ([]*EmailJob, error) {
	var jobs []*EmailJob
	err := s.db.NewSelect().
		Model(&jobs).
		Where("source_type = ?", sourceType).
		Where("source_id = ?", sourceID).
		Order("created_at DESC").
		Scan(ctx)

	if err != nil {
		return nil, fmt.Errorf("get jobs by source: %w", err)
	}

	return jobs, nil
}

// Stats returns queue statistics
func (s *JobsService) Stats(ctx context.Context) (*QueueStats, error) {
	stats := &QueueStats{}

	err := s.db.NewRaw(`SELECT 
		COUNT(*) FILTER (WHERE status = 'pending') as pending,
		COUNT(*) FILTER (WHERE status = 'processing') as processing,
		COUNT(*) FILTER (WHERE status = 'sent') as sent,
		COUNT(*) FILTER (WHERE status = 'failed') as failed
	FROM kb.email_jobs`).Scan(ctx, &stats.Pending, &stats.Processing, &stats.Sent, &stats.Failed)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	return stats, nil
}

// QueueStats contains queue statistics
type QueueStats struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Sent       int64 `json:"sent"`
	Failed     int64 `json:"failed"`
}

// truncateError truncates an error message to 1000 characters
func truncateError(msg string) string {
	if len(msg) > 1000 {
		return msg[:1000]
	}
	return msg
}
