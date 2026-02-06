package extraction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/logger"
)

// DocumentParsingJobsService manages the document parsing job queue.
// It provides methods to create, dequeue, and manage parsing jobs for documents.
//
// Key features:
// - Create jobs with organization/project scoping
// - Atomic dequeue with FOR UPDATE SKIP LOCKED
// - Retry with exponential backoff (limited by maxRetries)
// - Retry-pending status for scheduled retries
// - Stale job recovery
// - Queue statistics
//
// Unlike embedding jobs, parsing jobs use maxRetries to limit retry attempts.
type DocumentParsingJobsService struct {
	db  bun.IDB
	log *slog.Logger
	cfg *DocumentParsingConfig
}

// DocumentParsingConfig contains configuration for document parsing jobs
type DocumentParsingConfig struct {
	// BaseRetryDelayMs is the base delay in milliseconds for retries (default: 10000)
	BaseRetryDelayMs int
	// MaxRetryDelayMs is the maximum delay in milliseconds (default: 300000 = 5 minutes)
	MaxRetryDelayMs int
	// RetryMultiplier is the exponential backoff multiplier (default: 3)
	RetryMultiplier float64
	// DefaultMaxRetries is the default max retries for new jobs (default: 3)
	DefaultMaxRetries int
	// WorkerIntervalMs is the polling interval in milliseconds (default: 5000)
	WorkerIntervalMs int
	// WorkerBatchSize is the number of jobs to process per poll (default: 5)
	WorkerBatchSize int
	// StaleThresholdMinutes is the threshold for considering a job stale (default: 10)
	StaleThresholdMinutes int
}

// DefaultDocumentParsingConfig returns default configuration
func DefaultDocumentParsingConfig() *DocumentParsingConfig {
	return &DocumentParsingConfig{
		BaseRetryDelayMs:      10000,  // 10 seconds
		MaxRetryDelayMs:       300000, // 5 minutes
		RetryMultiplier:       3.0,
		DefaultMaxRetries:     3,
		WorkerIntervalMs:      5000,
		WorkerBatchSize:       5,
		StaleThresholdMinutes: 10,
	}
}

// WorkerInterval returns the worker interval as a Duration
func (c *DocumentParsingConfig) WorkerInterval() time.Duration {
	return time.Duration(c.WorkerIntervalMs) * time.Millisecond
}

// NewDocumentParsingJobsService creates a new document parsing jobs service
func NewDocumentParsingJobsService(db bun.IDB, log *slog.Logger, cfg *DocumentParsingConfig) *DocumentParsingJobsService {
	if cfg == nil {
		cfg = DefaultDocumentParsingConfig()
	}
	return &DocumentParsingJobsService{
		db:  db,
		log: log.With(logger.Scope("document.parsing.jobs")),
		cfg: cfg,
	}
}

// CreateJobOptions contains options for creating a document parsing job
type CreateJobOptions struct {
	OrganizationID  *string                // Optional: the organization ID (nil if user has no org)
	ProjectID       string                 // Required: the project ID
	SourceType      string                 // Required: 'file_upload', 'web_page', etc.
	SourceFilename  *string                // Optional: original filename
	MimeType        *string                // Optional: MIME type
	FileSizeBytes   *int64                 // Optional: file size in bytes
	StorageKey      *string                // Optional: S3/MinIO storage key
	DocumentID      *string                // Optional: existing document ID
	ExtractionJobID *string                // Optional: linked extraction job ID
	Metadata        map[string]interface{} // Optional: additional metadata
	MaxRetries      *int                   // Optional: override default max retries
}

// CreateJob creates a new document parsing job.
func (s *DocumentParsingJobsService) CreateJob(ctx context.Context, opts CreateJobOptions) (*DocumentParsingJob, error) {
	maxRetries := s.cfg.DefaultMaxRetries
	if opts.MaxRetries != nil {
		maxRetries = *opts.MaxRetries
	}

	metadata := opts.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	job := &DocumentParsingJob{
		OrganizationID:  opts.OrganizationID,
		ProjectID:       opts.ProjectID,
		Status:          JobStatusPending,
		SourceType:      opts.SourceType,
		SourceFilename:  opts.SourceFilename,
		MimeType:        opts.MimeType,
		FileSizeBytes:   opts.FileSizeBytes,
		StorageKey:      opts.StorageKey,
		DocumentID:      opts.DocumentID,
		ExtractionJobID: opts.ExtractionJobID,
		Metadata:        JSON(metadata),
		RetryCount:      0,
		MaxRetries:      maxRetries,
	}

	_, err := s.db.NewInsert().
		Model(job).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("create document parsing job: %w", err)
	}

	s.log.Debug("created document parsing job",
		slog.String("job_id", job.ID),
		slog.String("source_type", job.SourceType),
		slog.String("source_filename", ptrToString(job.SourceFilename)))

	return job, nil
}

// Dequeue atomically claims jobs for processing.
// Uses PostgreSQL's FOR UPDATE SKIP LOCKED for concurrent workers.
// Includes both 'pending' jobs and 'retry_pending' jobs whose nextRetryAt has passed.
func (s *DocumentParsingJobsService) Dequeue(ctx context.Context, batchSize int) ([]*DocumentParsingJob, error) {
	if batchSize <= 0 {
		batchSize = s.cfg.WorkerBatchSize
	}

	var jobs []*DocumentParsingJob

	// Strategic SQL: FOR UPDATE SKIP LOCKED for concurrent workers
	// Include retry_pending jobs whose nextRetryAt has passed
	err := s.db.NewRaw(`WITH cte AS (
		SELECT id FROM kb.document_parsing_jobs
		WHERE (status = 'pending')
		   OR (status = 'retry_pending' AND next_retry_at <= now())
		ORDER BY created_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT ?
	)
	UPDATE kb.document_parsing_jobs j 
	SET status = 'processing', 
		started_at = now(),
		updated_at = now()
	FROM cte WHERE j.id = cte.id
	RETURNING j.*`, batchSize).Scan(ctx, &jobs)
	if err != nil {
		return nil, fmt.Errorf("dequeue document parsing jobs: %w", err)
	}

	if len(jobs) > 0 {
		s.log.Debug("dequeued document parsing jobs", slog.Int("count", len(jobs)))
	}

	return jobs, nil
}

// MarkCompleted marks a job as successfully completed with parsed content
func (s *DocumentParsingJobsService) MarkCompleted(ctx context.Context, id string, result MarkCompletedResult) error {
	now := time.Now()

	// Build metadata update
	metadata := result.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["completedAt"] = now.Format(time.RFC3339)
	if result.ParsedContent != "" {
		metadata["characterCount"] = len(result.ParsedContent)
	}

	_, err := s.db.NewUpdate().
		Model((*DocumentParsingJob)(nil)).
		Set("status = ?", JobStatusCompleted).
		Set("parsed_content = ?", result.ParsedContent).
		Set("document_id = ?", result.DocumentID).
		Set("metadata = metadata || ?::jsonb", metadata).
		Set("completed_at = ?", now).
		Set("error_message = NULL").
		Set("updated_at = ?", now).
		Where("id = ?", id).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}

	s.log.Debug("document parsing job completed",
		slog.String("job_id", id),
		slog.Int("content_length", len(result.ParsedContent)))
	return nil
}

// MarkCompletedResult contains the result of a completed parsing job
type MarkCompletedResult struct {
	ParsedContent string                 // The extracted text content
	DocumentID    *string                // The created/updated document ID
	Metadata      map[string]interface{} // Additional metadata to store
}

// MarkFailed marks a job as failed and schedules retry if retries remain.
// Uses exponential backoff with the configured multiplier.
func (s *DocumentParsingJobsService) MarkFailed(ctx context.Context, id string, jobErr error) error {
	job := &DocumentParsingJob{}
	err := s.db.NewSelect().
		Model(job).
		Column("id", "retry_count", "max_retries", "metadata").
		Where("id = ?", id).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			s.log.Warn("document parsing job not found when marking as failed", slog.String("job_id", id))
			return nil
		}
		return fmt.Errorf("get job for mark failed: %w", err)
	}

	errorMessage := truncateError(jobErr.Error())
	shouldRetry := job.RetryCount < job.MaxRetries

	if shouldRetry {
		// Schedule retry with exponential backoff
		delayMs := s.calculateRetryDelay(job.RetryCount)
		nextRetryAt := time.Now().Add(time.Duration(delayMs) * time.Millisecond)

		// Update metadata with retry info
		metadata := map[string]interface{}{
			"lastError":    errorMessage,
			"lastFailedAt": time.Now().Format(time.RFC3339),
		}

		_, updateErr := s.db.NewUpdate().
			Model((*DocumentParsingJob)(nil)).
			Set("status = 'retry_pending'").
			Set("error_message = ?", errorMessage).
			Set("retry_count = retry_count + 1").
			Set("next_retry_at = ?", nextRetryAt).
			Set("metadata = metadata || ?::jsonb", metadata).
			Set("updated_at = now()").
			Where("id = ?", id).
			Exec(ctx)
		if updateErr != nil {
			return fmt.Errorf("schedule retry: %w", updateErr)
		}

		s.log.Warn("document parsing job failed, scheduled retry",
			slog.String("job_id", id),
			slog.Int("retry", job.RetryCount+1),
			slog.Int("max_retries", job.MaxRetries),
			slog.Time("next_retry_at", nextRetryAt),
			slog.String("error", errorMessage))
	} else {
		// No more retries - mark as dead letter
		metadata := map[string]interface{}{
			"lastError": errorMessage,
			"failedAt":  time.Now().Format(time.RFC3339),
		}

		_, updateErr := s.db.NewUpdate().
			Model((*DocumentParsingJob)(nil)).
			Set("status = ?", JobStatusDeadLetter).
			Set("error_message = ?", errorMessage).
			Set("completed_at = now()").
			Set("metadata = metadata || ?::jsonb", metadata).
			Set("updated_at = now()").
			Where("id = ?", id).
			Exec(ctx)
		if updateErr != nil {
			return fmt.Errorf("mark as dead letter: %w", updateErr)
		}

		s.log.Error("document parsing job moved to dead letter queue",
			slog.String("job_id", id),
			slog.Int("attempts", job.RetryCount+1),
			slog.String("error", errorMessage))
	}

	return nil
}

// RecoverStaleJobs recovers jobs stuck in 'processing' status.
// This can happen when the server restarts while jobs are being processed.
func (s *DocumentParsingJobsService) RecoverStaleJobs(ctx context.Context, staleThresholdMinutes int) (int, error) {
	if staleThresholdMinutes <= 0 {
		staleThresholdMinutes = s.cfg.StaleThresholdMinutes
	}

	result, err := s.db.NewRaw(`UPDATE kb.document_parsing_jobs 
		SET status = 'pending', 
			started_at = NULL,
			updated_at = now()
		WHERE status = 'processing' 
			AND started_at < now() - (? || ' minutes')::interval`,
		fmt.Sprintf("%d", staleThresholdMinutes)).Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("recover stale jobs: %w", err)
	}

	count, _ := result.RowsAffected()

	if count > 0 {
		s.log.Warn("recovered stale document parsing jobs",
			slog.Int64("count", count),
			slog.Int("threshold_minutes", staleThresholdMinutes))
	}

	return int(count), nil
}

// GetJob retrieves a job by ID
func (s *DocumentParsingJobsService) GetJob(ctx context.Context, id string) (*DocumentParsingJob, error) {
	job := &DocumentParsingJob{}
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

// FindByProject returns jobs for a project with pagination
func (s *DocumentParsingJobsService) FindByProject(ctx context.Context, projectID string, limit, offset int) ([]*DocumentParsingJob, error) {
	if limit <= 0 {
		limit = 20
	}

	var jobs []*DocumentParsingJob
	err := s.db.NewSelect().
		Model(&jobs).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Scan(ctx)

	if err != nil {
		return nil, fmt.Errorf("find by project: %w", err)
	}

	return jobs, nil
}

// FindByStatus returns jobs with a specific status
func (s *DocumentParsingJobsService) FindByStatus(ctx context.Context, status JobStatus, limit int) ([]*DocumentParsingJob, error) {
	if limit <= 0 {
		limit = 100
	}

	var jobs []*DocumentParsingJob
	err := s.db.NewSelect().
		Model(&jobs).
		Where("status = ?", status).
		Order("created_at ASC").
		Limit(limit).
		Scan(ctx)

	if err != nil {
		return nil, fmt.Errorf("find by status: %w", err)
	}

	return jobs, nil
}

// FindByDocumentID returns jobs associated with a document
func (s *DocumentParsingJobsService) FindByDocumentID(ctx context.Context, documentID string) ([]*DocumentParsingJob, error) {
	var jobs []*DocumentParsingJob
	err := s.db.NewSelect().
		Model(&jobs).
		Where("document_id = ?", documentID).
		Order("created_at DESC").
		Scan(ctx)

	if err != nil {
		return nil, fmt.Errorf("find by document: %w", err)
	}

	return jobs, nil
}

// CancelJobsForDocument cancels pending/processing jobs for a document
func (s *DocumentParsingJobsService) CancelJobsForDocument(ctx context.Context, documentID string) (int, error) {
	result, err := s.db.NewUpdate().
		Model((*DocumentParsingJob)(nil)).
		Set("status = ?", JobStatusFailed).
		Set("error_message = 'Cancelled by user'").
		Set("completed_at = now()").
		Set("updated_at = now()").
		Where("document_id = ?", documentID).
		Where("status IN ('pending', 'processing', 'retry_pending')").
		Exec(ctx)

	if err != nil {
		return 0, fmt.Errorf("cancel jobs for document: %w", err)
	}

	count, _ := result.RowsAffected()

	if count > 0 {
		s.log.Info("cancelled parsing jobs for document",
			slog.String("document_id", documentID),
			slog.Int64("count", count))
	}

	return int(count), nil
}

// UpdateStatus updates a job's status with optional field updates
func (s *DocumentParsingJobsService) UpdateStatus(ctx context.Context, id string, status JobStatus, updates *JobStatusUpdates) error {
	query := s.db.NewUpdate().
		Model((*DocumentParsingJob)(nil)).
		Set("status = ?", status).
		Set("updated_at = now()").
		Where("id = ?", id)

	if updates != nil {
		if updates.ParsedContent != nil {
			query = query.Set("parsed_content = ?", *updates.ParsedContent)
		}
		if updates.ErrorMessage != nil {
			query = query.Set("error_message = ?", *updates.ErrorMessage)
		}
		if updates.DocumentID != nil {
			query = query.Set("document_id = ?", *updates.DocumentID)
		}
		if updates.Metadata != nil {
			query = query.Set("metadata = metadata || ?::jsonb", updates.Metadata)
		}
	}

	_, err := query.Exec(ctx)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

// JobStatusUpdates contains optional updates when changing job status
type JobStatusUpdates struct {
	ParsedContent *string
	ErrorMessage  *string
	DocumentID    *string
	Metadata      map[string]interface{}
}

// Stats returns queue statistics
func (s *DocumentParsingJobsService) Stats(ctx context.Context) (*DocumentParsingQueueStats, error) {
	stats := &DocumentParsingQueueStats{}

	err := s.db.NewRaw(`SELECT 
		COUNT(*) FILTER (WHERE status = 'pending') as pending,
		COUNT(*) FILTER (WHERE status = 'processing') as processing,
		COUNT(*) FILTER (WHERE status = 'retry_pending') as retry_pending,
		COUNT(*) FILTER (WHERE status = 'completed') as completed,
		COUNT(*) FILTER (WHERE status = 'failed') as failed
	FROM kb.document_parsing_jobs`).Scan(ctx,
		&stats.Pending, &stats.Processing, &stats.RetryPending, &stats.Completed, &stats.Failed)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	return stats, nil
}

// DocumentParsingQueueStats contains queue statistics
type DocumentParsingQueueStats struct {
	Pending      int64 `json:"pending"`
	Processing   int64 `json:"processing"`
	RetryPending int64 `json:"retryPending"`
	Completed    int64 `json:"completed"`
	Failed       int64 `json:"failed"`
}

// calculateRetryDelay calculates the retry delay using exponential backoff.
// Formula: base * multiplier^retryCount, capped at max
func (s *DocumentParsingJobsService) calculateRetryDelay(retryCount int) int {
	delay := float64(s.cfg.BaseRetryDelayMs) * math.Pow(s.cfg.RetryMultiplier, float64(retryCount))
	if delay > float64(s.cfg.MaxRetryDelayMs) {
		delay = float64(s.cfg.MaxRetryDelayMs)
	}
	return int(delay)
}

// ptrToString safely converts a string pointer to string for logging
func ptrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RetryPendingStatus is the status value for jobs waiting for retry
const RetryPendingStatus JobStatus = "retry_pending"
