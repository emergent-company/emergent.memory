package extraction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/logger"
)

// ObjectExtractionConfig holds configuration for object extraction jobs
type ObjectExtractionConfig struct {
	// DefaultMaxRetries is the maximum number of retry attempts (default: 3)
	DefaultMaxRetries int
	// WorkerIntervalMs is the polling interval for the worker (default: 5000)
	WorkerIntervalMs int
	// WorkerBatchSize is the number of jobs to process in each batch (default: 5)
	WorkerBatchSize int
	// WorkerConcurrency is the static concurrency level, defaults to 5
	WorkerConcurrency int
	// StaleThresholdMinutes is how long a job can be 'processing' before recovery (default: 30)
	StaleThresholdMinutes int
	// EnableAdaptiveScaling enables dynamic concurrency adjustment based on system health
	EnableAdaptiveScaling bool
	// MinConcurrency is the minimum concurrency when adaptive scaling is enabled (default: 1)
	MinConcurrency int
	// MaxConcurrency is the maximum concurrency when adaptive scaling is enabled (default: 5)
	MaxConcurrency int
}

// DefaultObjectExtractionConfig returns default configuration
func DefaultObjectExtractionConfig() *ObjectExtractionConfig {
	return &ObjectExtractionConfig{
		DefaultMaxRetries:     3,
		WorkerIntervalMs:      5000,
		WorkerBatchSize:       5,
		WorkerConcurrency:     5,
		StaleThresholdMinutes: 30,
		EnableAdaptiveScaling: true,
		MinConcurrency:        2,
		MaxConcurrency:        10,
	}
}

// ObjectExtractionJobsService manages object extraction jobs
type ObjectExtractionJobsService struct {
	db     bun.IDB
	log    *slog.Logger
	config *ObjectExtractionConfig
}

// NewObjectExtractionJobsService creates a new object extraction jobs service
func NewObjectExtractionJobsService(db bun.IDB, log *slog.Logger, config *ObjectExtractionConfig) *ObjectExtractionJobsService {
	if config == nil {
		config = DefaultObjectExtractionConfig()
	}
	return &ObjectExtractionJobsService{
		db:     db,
		log:    log.With(logger.Scope("object-extraction-jobs")),
		config: config,
	}
}

// CreateJobOptions contains options for creating an object extraction job
type CreateObjectExtractionJobOptions struct {
	ProjectID        string
	DocumentID       *string  // Optional: specific document
	ChunkID          *string  // Optional: specific chunk
	JobType          JobType  // full_extraction, reextraction, incremental
	EnabledTypes     []string // Entity types to extract
	ExtractionConfig JSON     // LLM/extraction settings
	SourceType       *string  // 'document', 'chunk', 'manual'
	SourceID         *string
	SourceMetadata   JSON
	CreatedBy        *string // User who created the job
	ReprocessingOf   *string // ID of job being reprocessed
}

// CreateJob creates a new object extraction job
func (s *ObjectExtractionJobsService) CreateJob(ctx context.Context, opts CreateObjectExtractionJobOptions) (*ObjectExtractionJob, error) {
	now := time.Now().UTC()

	// Defaults
	jobType := opts.JobType
	if jobType == "" {
		jobType = JobTypeFullExtraction
	}

	enabledTypes := opts.EnabledTypes
	if enabledTypes == nil {
		enabledTypes = []string{}
	}

	extractionConfig := opts.ExtractionConfig
	if extractionConfig == nil {
		extractionConfig = JSON{}
	}

	sourceMetadata := opts.SourceMetadata
	if sourceMetadata == nil {
		sourceMetadata = JSON{}
	}

	job := &ObjectExtractionJob{
		ProjectID:        opts.ProjectID,
		DocumentID:       opts.DocumentID,
		ChunkID:          opts.ChunkID,
		JobType:          jobType,
		Status:           JobStatusPending,
		EnabledTypes:     enabledTypes,
		ExtractionConfig: extractionConfig,
		SourceType:       opts.SourceType,
		SourceID:         opts.SourceID,
		SourceMetadata:   sourceMetadata,
		CreatedBy:        opts.CreatedBy,
		ReprocessingOf:   opts.ReprocessingOf,
		MaxRetries:       s.config.DefaultMaxRetries,
		Logs:             JSONArray{},
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	_, err := s.db.NewInsert().
		Model(job).
		Returning("*").
		Exec(ctx)
	if err != nil {
		s.log.Error("failed to create object extraction job",
			slog.String("projectId", opts.ProjectID),
			logger.Error(err))
		return nil, fmt.Errorf("create object extraction job: %w", err)
	}

	s.log.Info("created object extraction job",
		slog.String("id", job.ID),
		slog.String("projectId", opts.ProjectID),
		slog.String("jobType", string(jobType)))

	return job, nil
}

// Dequeue claims the next pending job for processing using FOR UPDATE SKIP LOCKED
// Deprecated: use DequeueBatch instead
func (s *ObjectExtractionJobsService) Dequeue(ctx context.Context) (*ObjectExtractionJob, error) {
	jobs, err := s.DequeueBatch(ctx, 1)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return jobs[0], nil
}

// DequeueBatch claims multiple pending jobs for processing using FOR UPDATE SKIP LOCKED
func (s *ObjectExtractionJobsService) DequeueBatch(ctx context.Context, batchSize int) ([]*ObjectExtractionJob, error) {
	if batchSize <= 0 {
		batchSize = s.config.WorkerBatchSize
	}

	var jobs []*ObjectExtractionJob

	// Use a transaction to ensure atomic claiming
	err := s.db.RunInTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(ctx context.Context, tx bun.Tx) error {
		// Find up to N pending jobs
		err := tx.NewSelect().
			Model(&jobs).
			Where("status = ?", JobStatusPending).
			Order("created_at ASC").
			Limit(batchSize).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)

		if err != nil {
			return err
		}

		if len(jobs) == 0 {
			return nil // No jobs available
		}

		now := time.Now().UTC()
		var jobIDs []string
		for _, job := range jobs {
			job.Status = JobStatusProcessing
			job.StartedAt = &now
			job.UpdatedAt = now
			jobIDs = append(jobIDs, job.ID)
		}

		// Update the claimed jobs individually (Bun requires CTE+VALUES for bulk updates)
		_, err = tx.NewUpdate().
			Model((*ObjectExtractionJob)(nil)).
			Set("status = ?", JobStatusProcessing).
			Set("started_at = ?", now).
			Set("updated_at = ?", now).
			Where("id IN (?)", bun.In(jobIDs)).
			Exec(ctx)

		return err
	})

	if err == sql.ErrNoRows {
		return nil, nil // No jobs available
	}
	if err != nil {
		return nil, fmt.Errorf("dequeue batch: %w", err)
	}

	return jobs, nil
}

// MarkCompleted marks a job as completed with results
func (s *ObjectExtractionJobsService) MarkCompleted(ctx context.Context, jobID string, results ObjectExtractionResults) error {
	now := time.Now().UTC()

	_, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("status = ?", JobStatusCompleted).
		Set("completed_at = ?", now).
		Set("updated_at = ?", now).
		Set("objects_created = ?", results.ObjectsCreated).
		Set("relationships_created = ?", results.RelationshipsCreated).
		Set("suggestions_created = ?", results.SuggestionsCreated).
		Set("total_items = ?", results.TotalItems).
		Set("processed_items = ?", results.ProcessedItems).
		Set("successful_items = ?", results.SuccessfulItems).
		Set("failed_items = ?", results.FailedItems).
		Set("discovered_types = ?", results.DiscoveredTypes).
		Set("created_objects = ?", results.CreatedObjects).
		Set("debug_info = ?", results.DebugInfo).
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		s.log.Error("failed to mark job completed",
			slog.String("id", jobID),
			logger.Error(err))
		return fmt.Errorf("mark completed: %w", err)
	}

	s.log.Info("object extraction job completed",
		slog.String("id", jobID),
		slog.Int("objectsCreated", results.ObjectsCreated),
		slog.Int("successfulItems", results.SuccessfulItems))

	return nil
}

// ObjectExtractionResults contains the results of an object extraction job
type ObjectExtractionResults struct {
	ObjectsCreated       int
	RelationshipsCreated int
	SuggestionsCreated   int
	TotalItems           int
	ProcessedItems       int
	SuccessfulItems      int
	FailedItems          int
	DiscoveredTypes      JSONArray
	CreatedObjects       JSONArray
	DebugInfo            JSON
}

// MarkFailed marks a job as failed with an error message
// If retries are available, schedules a retry; otherwise marks as permanently failed
func (s *ObjectExtractionJobsService) MarkFailed(ctx context.Context, jobID string, errorMessage string, errorDetails JSON) error {
	now := time.Now().UTC()

	// Get current retry count
	job := new(ObjectExtractionJob)
	err := s.db.NewSelect().
		Model(job).
		Column("retry_count", "max_retries").
		Where("id = ?", jobID).
		Scan(ctx)
	if err != nil {
		return fmt.Errorf("get job for retry check: %w", err)
	}

	newRetryCount := job.RetryCount + 1

	if newRetryCount >= job.MaxRetries {
		// Move to dead letter queue
		_, err = s.db.NewUpdate().
			Model((*ObjectExtractionJob)(nil)).
			Set("status = ?", JobStatusDeadLetter).
			Set("completed_at = ?", now).
			Set("updated_at = ?", now).
			Set("error_message = ?", errorMessage).
			Set("error_details = ?", errorDetails).
			Set("retry_count = ?", newRetryCount).
			Where("id = ?", jobID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("mark as dead letter: %w", err)
		}

		s.log.Warn("object extraction job moved to dead letter queue",
			slog.String("id", jobID),
			slog.String("error", errorMessage),
			slog.Int("retryCount", newRetryCount))
	} else {
		// Schedule retry - reset to pending
		_, err = s.db.NewUpdate().
			Model((*ObjectExtractionJob)(nil)).
			Set("status = ?", JobStatusPending).
			Set("updated_at = ?", now).
			Set("error_message = ?", errorMessage).
			Set("error_details = ?", errorDetails).
			Set("retry_count = ?", newRetryCount).
			Set("started_at = NULL").
			Where("id = ?", jobID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("schedule retry: %w", err)
		}

		s.log.Info("object extraction job scheduled for retry",
			slog.String("id", jobID),
			slog.Int("retryCount", newRetryCount),
			slog.Int("maxRetries", job.MaxRetries))
	}

	return nil
}

// UpdateProgress updates the progress of a running job
func (s *ObjectExtractionJobsService) UpdateProgress(ctx context.Context, jobID string, processed, total int) error {
	now := time.Now().UTC()

	_, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("processed_items = ?", processed).
		Set("total_items = ?", total).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update progress: %w", err)
	}

	return nil
}

// CancelJob cancels a pending or processing job
func (s *ObjectExtractionJobsService) CancelJob(ctx context.Context, jobID string) error {
	now := time.Now().UTC()

	res, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("status = ?", JobStatusCancelled).
		Set("completed_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Where("status IN (?)", bun.In([]JobStatus{JobStatusPending, JobStatusProcessing})).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("job not found or already completed")
	}

	s.log.Info("object extraction job cancelled", slog.String("id", jobID))
	return nil
}

// RecoverStaleJobs resets jobs stuck in 'processing' status back to 'pending'
func (s *ObjectExtractionJobsService) RecoverStaleJobs(ctx context.Context) (int, error) {
	threshold := time.Now().UTC().Add(-time.Duration(s.config.StaleThresholdMinutes) * time.Minute)
	now := time.Now().UTC()

	res, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("status = ?", JobStatusPending).
		Set("started_at = NULL").
		Set("updated_at = ?", now).
		Where("status = ?", JobStatusProcessing).
		Where("started_at < ?", threshold).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("recover stale jobs: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected > 0 {
		s.log.Warn("recovered stale object extraction jobs",
			slog.Int64("count", rowsAffected),
			slog.Int("thresholdMinutes", s.config.StaleThresholdMinutes))
	}

	return int(rowsAffected), nil
}

// FindByID finds a job by ID
func (s *ObjectExtractionJobsService) FindByID(ctx context.Context, jobID string) (*ObjectExtractionJob, error) {
	job := new(ObjectExtractionJob)
	err := s.db.NewSelect().
		Model(job).
		Where("id = ?", jobID).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find by id: %w", err)
	}
	return job, nil
}

// FindByProject finds all jobs for a project with optional status filter
func (s *ObjectExtractionJobsService) FindByProject(ctx context.Context, projectID string, status *JobStatus, limit int) ([]*ObjectExtractionJob, error) {
	if limit <= 0 {
		limit = 100
	}

	query := s.db.NewSelect().
		Model((*ObjectExtractionJob)(nil)).
		Where("project_id = ?", projectID).
		OrderExpr("created_at DESC").
		Limit(limit)

	if status != nil {
		query = query.Where("status = ?", *status)
	}

	var jobs []*ObjectExtractionJob
	err := query.Scan(ctx, &jobs)
	if err != nil {
		return nil, fmt.Errorf("find by project: %w", err)
	}

	return jobs, nil
}

// FindByDocument finds all jobs for a specific document
func (s *ObjectExtractionJobsService) FindByDocument(ctx context.Context, documentID string) ([]*ObjectExtractionJob, error) {
	var jobs []*ObjectExtractionJob
	err := s.db.NewSelect().
		Model(&jobs).
		Where("document_id = ?", documentID).
		OrderExpr("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find by document: %w", err)
	}
	return jobs, nil
}

// ObjectExtractionStats contains statistics about object extraction jobs
type ObjectExtractionStats struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Completed  int64 `json:"completed"`
	Failed     int64 `json:"failed"`
	Cancelled  int64 `json:"cancelled"`
	Total      int64 `json:"total"`
}

// Stats returns statistics about object extraction jobs
func (s *ObjectExtractionJobsService) Stats(ctx context.Context, projectID *string) (*ObjectExtractionStats, error) {
	query := s.db.NewSelect().
		Model((*ObjectExtractionJob)(nil)).
		Column("status").
		ColumnExpr("COUNT(*) as count")

	if projectID != nil {
		query = query.Where("project_id = ?", *projectID)
	}

	query = query.Group("status")

	var results []struct {
		Status JobStatus `bun:"status"`
		Count  int64     `bun:"count"`
	}

	err := query.Scan(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	stats := &ObjectExtractionStats{}
	for _, r := range results {
		stats.Total += r.Count
		switch r.Status {
		case JobStatusPending:
			stats.Pending = r.Count
		case JobStatusProcessing:
			stats.Processing = r.Count
		case JobStatusCompleted:
			stats.Completed = r.Count
		case JobStatusFailed:
			stats.Failed = r.Count
		case JobStatusCancelled:
			stats.Cancelled = r.Count
		}
	}

	return stats, nil
}

// BulkCancelByProject cancels all pending/processing jobs for a project
func (s *ObjectExtractionJobsService) BulkCancelByProject(ctx context.Context, projectID string) (int, error) {
	now := time.Now().UTC()

	res, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("status = ?", JobStatusCancelled).
		Set("completed_at = ?", now).
		Set("updated_at = ?", now).
		Where("project_id = ?", projectID).
		Where("status IN (?)", bun.In([]JobStatus{JobStatusPending, JobStatusProcessing})).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("bulk cancel: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	s.log.Info("bulk cancelled object extraction jobs",
		slog.String("projectId", projectID),
		slog.Int64("count", rowsAffected))

	return int(rowsAffected), nil
}

// BulkRetryFailed resets all failed jobs for a project back to pending
func (s *ObjectExtractionJobsService) BulkRetryFailed(ctx context.Context, projectID string) (int, error) {
	now := time.Now().UTC()

	res, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("status = ?", JobStatusPending).
		Set("error_message = NULL").
		Set("error_details = NULL").
		Set("started_at = NULL").
		Set("completed_at = NULL").
		Set("updated_at = ?", now).
		Where("project_id = ?", projectID).
		Where("status = ?", JobStatusFailed).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("bulk retry: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	s.log.Info("bulk retried failed object extraction jobs",
		slog.String("projectId", projectID),
		slog.Int64("count", rowsAffected))

	return int(rowsAffected), nil
}

// DeleteCompleted deletes all completed/failed/cancelled jobs for a project
func (s *ObjectExtractionJobsService) DeleteCompleted(ctx context.Context, projectID string) (int, error) {
	res, err := s.db.NewDelete().
		Model((*ObjectExtractionJob)(nil)).
		Where("project_id = ?", projectID).
		Where("status IN (?)", bun.In([]JobStatus{JobStatusCompleted, JobStatusFailed, JobStatusCancelled})).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("delete completed: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	s.log.Info("deleted completed object extraction jobs",
		slog.String("projectId", projectID),
		slog.Int64("count", rowsAffected))

	return int(rowsAffected), nil
}

// FindByProjectPaginated finds jobs for a project with pagination
func (s *ObjectExtractionJobsService) FindByProjectPaginated(
	ctx context.Context,
	projectID string,
	status *JobStatus,
	sourceType *string,
	sourceID *string,
	page int,
	limit int,
) ([]*ObjectExtractionJob, int, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	// Build query
	query := s.db.NewSelect().
		Model((*ObjectExtractionJob)(nil)).
		Where("project_id = ?", projectID)

	if status != nil {
		query = query.Where("status = ?", *status)
	}
	if sourceType != nil {
		query = query.Where("source_type = ?", *sourceType)
	}
	if sourceID != nil {
		query = query.Where("source_id = ?", *sourceID)
	}

	// Get total count
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}

	// Get paginated results
	var jobs []*ObjectExtractionJob
	err = query.
		OrderExpr("created_at DESC").
		Limit(limit).
		Offset(offset).
		Scan(ctx, &jobs)
	if err != nil {
		return nil, 0, fmt.Errorf("find by project paginated: %w", err)
	}

	return jobs, total, nil
}

// UpdateJob updates a job with the given fields
func (s *ObjectExtractionJobsService) UpdateJob(ctx context.Context, job *ObjectExtractionJob) error {
	now := time.Now().UTC()
	job.UpdatedAt = now

	_, err := s.db.NewUpdate().
		Model(job).
		WherePK().
		Exec(ctx)
	if err != nil {
		s.log.Error("failed to update object extraction job",
			slog.String("id", job.ID),
			logger.Error(err))
		return fmt.Errorf("update job: %w", err)
	}

	s.log.Debug("updated object extraction job",
		slog.String("id", job.ID))

	return nil
}

// DeleteJob deletes a single job by ID
// Only completed/failed/cancelled jobs can be deleted
func (s *ObjectExtractionJobsService) DeleteJob(ctx context.Context, jobID string, projectID string) error {
	res, err := s.db.NewDelete().
		Model((*ObjectExtractionJob)(nil)).
		Where("id = ?", jobID).
		Where("project_id = ?", projectID).
		Where("status IN (?)", bun.In([]JobStatus{JobStatusCompleted, JobStatusFailed, JobStatusCancelled, JobStatusDeadLetter})).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete job: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("job not found or cannot be deleted (only completed/failed/cancelled jobs can be deleted)")
	}

	s.log.Info("deleted object extraction job", slog.String("id", jobID))
	return nil
}

// RetryJob resets a failed or stuck job back to pending for retry
func (s *ObjectExtractionJobsService) RetryJob(ctx context.Context, jobID string, projectID string) (*ObjectExtractionJob, error) {
	now := time.Now().UTC()

	// Get current job
	job, err := s.FindByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("find job: %w", err)
	}
	if job == nil {
		return nil, fmt.Errorf("job not found")
	}

	// Verify project
	if job.ProjectID != projectID {
		return nil, fmt.Errorf("job not found in project")
	}

	// Can only retry failed, dead_letter, or processing (stuck) jobs
	if job.Status != JobStatusFailed && job.Status != JobStatusDeadLetter && job.Status != JobStatusProcessing {
		return nil, fmt.Errorf("cannot retry job with status %s (only failed/dead_letter/processing jobs can be retried)", job.Status)
	}

	// Reset job to pending
	_, err = s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("status = ?", JobStatusPending).
		Set("error_message = NULL").
		Set("error_details = NULL").
		Set("started_at = NULL").
		Set("completed_at = NULL").
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("retry job: %w", err)
	}

	s.log.Info("retried object extraction job",
		slog.String("id", jobID),
		slog.String("previousStatus", string(job.Status)))

	// Fetch updated job
	return s.FindByID(ctx, jobID)
}

// GetStatistics returns detailed statistics for a project
func (s *ObjectExtractionJobsService) GetStatistics(ctx context.Context, projectID string) (*ExtendedStats, error) {
	now := time.Now().UTC()
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, -1, 0)

	// Basic stats by status
	basicStats, err := s.Stats(ctx, &projectID)
	if err != nil {
		return nil, fmt.Errorf("get basic stats: %w", err)
	}

	// Jobs this week
	var weekCount int
	err = s.db.NewSelect().
		Model((*ObjectExtractionJob)(nil)).
		Where("project_id = ?", projectID).
		Where("created_at >= ?", weekAgo).
		ColumnExpr("COUNT(*)").
		Scan(ctx, &weekCount)
	if err != nil {
		return nil, fmt.Errorf("count week jobs: %w", err)
	}

	// Jobs this month
	var monthCount int
	err = s.db.NewSelect().
		Model((*ObjectExtractionJob)(nil)).
		Where("project_id = ?", projectID).
		Where("created_at >= ?", monthAgo).
		ColumnExpr("COUNT(*)").
		Scan(ctx, &monthCount)
	if err != nil {
		return nil, fmt.Errorf("count month jobs: %w", err)
	}

	// Average processing time for completed jobs
	var avgDurationMs sql.NullFloat64
	err = s.db.NewSelect().
		Model((*ObjectExtractionJob)(nil)).
		Where("project_id = ?", projectID).
		Where("status = ?", JobStatusCompleted).
		Where("started_at IS NOT NULL").
		Where("completed_at IS NOT NULL").
		ColumnExpr("AVG(EXTRACT(EPOCH FROM (completed_at - started_at)) * 1000)").
		Scan(ctx, &avgDurationMs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get avg duration: %w", err)
	}

	// Success rate
	var successRate float64
	if basicStats.Total > 0 {
		successRate = float64(basicStats.Completed) / float64(basicStats.Total) * 100
	}

	stats := &ExtendedStats{
		TotalJobs:     basicStats.Total,
		JobsByStatus:  make(map[string]int64),
		SuccessRate:   successRate,
		JobsThisWeek:  int64(weekCount),
		JobsThisMonth: int64(monthCount),
	}

	stats.JobsByStatus["pending"] = basicStats.Pending
	stats.JobsByStatus["processing"] = basicStats.Processing
	stats.JobsByStatus["completed"] = basicStats.Completed
	stats.JobsByStatus["failed"] = basicStats.Failed
	stats.JobsByStatus["cancelled"] = basicStats.Cancelled

	if avgDurationMs.Valid {
		avgMs := int64(avgDurationMs.Float64)
		stats.AverageProcessingTimeMs = &avgMs
	}

	return stats, nil
}

// ExtendedStats contains extended statistics
type ExtendedStats struct {
	TotalJobs               int64            `json:"total_jobs"`
	JobsByStatus            map[string]int64 `json:"jobs_by_status"`
	SuccessRate             float64          `json:"success_rate"`
	AverageProcessingTimeMs *int64           `json:"average_processing_time_ms,omitempty"`
	JobsThisWeek            int64            `json:"jobs_this_week"`
	JobsThisMonth           int64            `json:"jobs_this_month"`
}

// ------------------------------------------------------------------
// Extraction Log Methods
// ------------------------------------------------------------------

// GetJobLogs retrieves all logs for an extraction job, ordered by step_index and started_at
func (s *ObjectExtractionJobsService) GetJobLogs(ctx context.Context, jobID string) ([]*ObjectExtractionLog, error) {
	var logs []*ObjectExtractionLog
	err := s.db.NewSelect().
		Model(&logs).
		Where("extraction_job_id = ?", jobID).
		OrderExpr("step_index ASC, started_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get job logs: %w", err)
	}
	return logs, nil
}

// GetJobLogSummary calculates summary statistics for extraction logs
func (s *ObjectExtractionJobsService) GetJobLogSummary(ctx context.Context, jobID string) (*ExtractionLogSummaryDTO, error) {
	logs, err := s.GetJobLogs(ctx, jobID)
	if err != nil {
		return nil, err
	}

	// Count operations by type
	operationCounts := make(map[string]int)
	totalDurationMs := 0
	totalTokensUsed := 0
	successSteps := 0
	errorSteps := 0
	warningSteps := 0

	for _, log := range logs {
		// Count by operation type
		operationCounts[log.OperationType]++

		// Sum duration
		if log.DurationMs != nil {
			totalDurationMs += *log.DurationMs
		}

		// Sum tokens
		if log.TokensUsed != nil {
			totalTokensUsed += *log.TokensUsed
		}

		// Count by status
		switch log.Status {
		case "completed", "success":
			successSteps++
		case "failed", "error":
			errorSteps++
		case "warning":
			warningSteps++
		}
	}

	return &ExtractionLogSummaryDTO{
		TotalSteps:      len(logs),
		SuccessSteps:    successSteps,
		ErrorSteps:      errorSteps,
		WarningSteps:    warningSteps,
		TotalDurationMs: totalDurationMs,
		TotalTokensUsed: totalTokensUsed,
		OperationCounts: operationCounts,
	}, nil
}
