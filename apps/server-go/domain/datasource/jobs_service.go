package datasource

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Common errors
var (
	ErrJobNotFound      = errors.New("sync job not found")
	ErrJobAlreadyExists = errors.New("sync job already exists for this integration")
)

// JobsService handles data source sync job queue operations.
// Follows the same pattern as other job services in the extraction domain.
type JobsService struct {
	db  *bun.DB
	log *slog.Logger
	cfg *config.Config
}

// NewJobsService creates a new JobsService
func NewJobsService(db *bun.DB, log *slog.Logger, cfg *config.Config) *JobsService {
	return &JobsService{
		db:  db,
		log: log.With(logger.Scope("datasource.jobs")),
		cfg: cfg,
	}
}

// Create creates a new sync job for an integration
func (s *JobsService) Create(ctx context.Context, job *DataSourceSyncJob) error {
	_, err := s.db.NewInsert().Model(job).Exec(ctx)
	if err != nil {
		return err
	}
	s.log.Debug("created sync job",
		slog.String("job_id", job.ID),
		slog.String("integration_id", job.IntegrationID))
	return nil
}

// GetByID retrieves a sync job by ID
func (s *JobsService) GetByID(ctx context.Context, id string) (*DataSourceSyncJob, error) {
	job := &DataSourceSyncJob{}
	err := s.db.NewSelect().
		Model(job).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	return job, nil
}

// GetActiveJobForIntegration returns any pending/running job for an integration
func (s *JobsService) GetActiveJobForIntegration(ctx context.Context, integrationID string) (*DataSourceSyncJob, error) {
	job := &DataSourceSyncJob{}
	err := s.db.NewSelect().
		Model(job).
		Where("integration_id = ?", integrationID).
		Where("status IN (?)", bun.In([]JobStatus{JobStatusPending, JobStatusRunning})).
		OrderExpr("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // No active job
		}
		return nil, err
	}
	return job, nil
}

// ListByIntegration returns sync jobs for an integration
func (s *JobsService) ListByIntegration(ctx context.Context, integrationID string, limit int) ([]*DataSourceSyncJob, error) {
	var jobs []*DataSourceSyncJob
	err := s.db.NewSelect().
		Model(&jobs).
		Where("integration_id = ?", integrationID).
		OrderExpr("created_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// Dequeue claims pending jobs for processing using FOR UPDATE SKIP LOCKED
func (s *JobsService) Dequeue(ctx context.Context, batchSize int) ([]*DataSourceSyncJob, error) {
	var jobs []*DataSourceSyncJob

	err := s.db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		// Select pending jobs with lock
		err := tx.NewSelect().
			Model(&jobs).
			Where("status = ?", JobStatusPending).
			OrderExpr("created_at ASC").
			Limit(batchSize).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return err
		}

		if len(jobs) == 0 {
			return nil
		}

		// Mark as running
		now := time.Now()
		ids := make([]string, len(jobs))
		for i, job := range jobs {
			ids[i] = job.ID
			job.Status = JobStatusRunning
			job.StartedAt = &now
		}

		_, err = tx.NewUpdate().
			Model((*DataSourceSyncJob)(nil)).
			Set("status = ?", JobStatusRunning).
			Set("started_at = ?", now).
			Set("current_phase = ?", "initializing").
			Set("updated_at = ?", now).
			Where("id IN (?)", bun.In(ids)).
			Exec(ctx)
		return err
	})

	if err != nil {
		return nil, err
	}

	return jobs, nil
}

// MarkRunning marks a job as running
func (s *JobsService) MarkRunning(ctx context.Context, jobID string, phase string) error {
	now := time.Now()
	_, err := s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("status = ?", JobStatusRunning).
		Set("started_at = COALESCE(started_at, ?)", now).
		Set("current_phase = ?", phase).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	return err
}

// UpdateProgress updates job progress counters
func (s *JobsService) UpdateProgress(ctx context.Context, jobID string, total, processed, successful, failed, skipped int, phase string, message string) error {
	now := time.Now()
	_, err := s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("total_items = ?", total).
		Set("processed_items = ?", processed).
		Set("successful_items = ?", successful).
		Set("failed_items = ?", failed).
		Set("skipped_items = ?", skipped).
		Set("current_phase = ?", phase).
		Set("status_message = ?", message).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	return err
}

// AddDocumentID adds a document ID to the job's document list
func (s *JobsService) AddDocumentID(ctx context.Context, jobID string, documentID string) error {
	now := time.Now()
	_, err := s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("document_ids = document_ids || ?::jsonb", `"`+documentID+`"`).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	return err
}

// AppendLog adds a log entry to the job
func (s *JobsService) AppendLog(ctx context.Context, jobID string, entry SyncJobLogEntry) error {
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	now := time.Now()
	_, err = s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("logs = logs || ?::jsonb", string(entryJSON)).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	return err
}

// MarkCompleted marks a job as completed
func (s *JobsService) MarkCompleted(ctx context.Context, jobID string) error {
	now := time.Now()
	_, err := s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("status = ?", JobStatusCompleted).
		Set("completed_at = ?", now).
		Set("current_phase = ?", "completed").
		Set("status_message = ?", "Sync completed successfully").
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		return err
	}

	s.log.Info("sync job completed", slog.String("job_id", jobID))
	return nil
}

// MarkFailed marks a job as failed with an error
func (s *JobsService) MarkFailed(ctx context.Context, jobID string, err error) error {
	now := time.Now()
	errMsg := truncateError(err.Error())
	_, dbErr := s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("status = ?", JobStatusFailed).
		Set("completed_at = ?", now).
		Set("current_phase = ?", "failed").
		Set("error_message = ?", errMsg).
		Set("status_message = ?", "Sync failed: "+errMsg).
		Set("retry_count = retry_count + 1").
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	if dbErr != nil {
		return dbErr
	}

	s.log.Warn("sync job failed",
		slog.String("job_id", jobID),
		slog.String("error", errMsg))
	return nil
}

// MarkFailedWithRetry marks a job as failed and schedules a retry if under max retries
// Returns true if the job was scheduled for retry, false if it was moved to dead letter
func (s *JobsService) MarkFailedWithRetry(ctx context.Context, jobID string, err error, retryCount, maxRetries int) (bool, error) {
	now := time.Now()
	errMsg := truncateError(err.Error())

	// Check if we should move to dead letter
	if retryCount >= maxRetries {
		// Move to dead letter - permanently failed
		_, dbErr := s.db.NewUpdate().
			Model((*DataSourceSyncJob)(nil)).
			Set("status = ?", JobStatusDeadLetter).
			Set("completed_at = ?", now).
			Set("current_phase = ?", "dead_letter").
			Set("error_message = ?", errMsg).
			Set("status_message = ?", "Permanently failed after "+string(rune(maxRetries+'0'))+" retries: "+errMsg).
			Set("retry_count = ?", retryCount).
			Set("updated_at = ?", now).
			Where("id = ?", jobID).
			Exec(ctx)
		if dbErr != nil {
			return false, dbErr
		}

		s.log.Error("sync job moved to dead letter",
			slog.String("job_id", jobID),
			slog.Int("retry_count", retryCount),
			slog.Int("max_retries", maxRetries),
			slog.String("error", errMsg))
		return false, nil
	}

	// Schedule retry with exponential backoff
	backoffMinutes := 1 << retryCount // 1, 2, 4, 8, 16... minutes
	if backoffMinutes > 60 {
		backoffMinutes = 60 // Cap at 1 hour
	}
	nextRetry := now.Add(time.Duration(backoffMinutes) * time.Minute)

	_, dbErr := s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("status = ?", JobStatusPending).
		Set("error_message = ?", errMsg).
		Set("status_message = ?", "Scheduled for retry").
		Set("retry_count = ?", retryCount+1).
		Set("next_retry_at = ?", nextRetry).
		Set("started_at = NULL").
		Set("completed_at = NULL").
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	if dbErr != nil {
		return false, dbErr
	}

	s.log.Info("sync job scheduled for retry",
		slog.String("job_id", jobID),
		slog.Int("retry_count", retryCount+1),
		slog.Time("next_retry_at", nextRetry))
	return true, nil
}

// MarkCancelled marks a job as cancelled
func (s *JobsService) MarkCancelled(ctx context.Context, jobID string) error {
	now := time.Now()
	_, err := s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("status = ?", JobStatusCancelled).
		Set("completed_at = ?", now).
		Set("current_phase = ?", "cancelled").
		Set("status_message = ?", "Sync cancelled by user").
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Where("status IN (?)", bun.In([]JobStatus{JobStatusPending, JobStatusRunning})).
		Exec(ctx)
	if err != nil {
		return err
	}

	s.log.Info("sync job cancelled", slog.String("job_id", jobID))
	return nil
}

// RecoverStaleJobs marks jobs stuck in running state as failed
// This is called on worker startup to handle jobs that were interrupted
func (s *JobsService) RecoverStaleJobs(ctx context.Context, staleMinutes int) (int, error) {
	cutoff := time.Now().Add(-time.Duration(staleMinutes) * time.Minute)

	res, err := s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("status = ?", JobStatusFailed).
		Set("error_message = ?", "Job interrupted - marked as failed during recovery").
		Set("completed_at = ?", time.Now()).
		Set("updated_at = ?", time.Now()).
		Where("status = ?", JobStatusRunning).
		Where("started_at < ?", cutoff).
		Exec(ctx)
	if err != nil {
		return 0, err
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected > 0 {
		s.log.Info("recovered stale sync jobs", slog.Int64("count", rowsAffected))
	}
	return int(rowsAffected), nil
}

// GetIntegration retrieves an integration by ID
func (s *JobsService) GetIntegration(ctx context.Context, id string) (*DataSourceIntegration, error) {
	integration := &DataSourceIntegration{}
	err := s.db.NewSelect().
		Model(integration).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("integration not found")
		}
		return nil, err
	}
	return integration, nil
}

// UpdateIntegrationSyncStatus updates the sync-related fields on an integration
func (s *JobsService) UpdateIntegrationSyncStatus(ctx context.Context, integrationID string, lastSynced time.Time, nextSync *time.Time, status IntegrationStatus, errMsg *string) error {
	now := time.Now()
	q := s.db.NewUpdate().
		Model((*DataSourceIntegration)(nil)).
		Set("last_synced_at = ?", lastSynced).
		Set("status = ?", status).
		Set("updated_at = ?", now).
		Where("id = ?", integrationID)

	if nextSync != nil {
		q = q.Set("next_sync_at = ?", *nextSync)
	}
	if errMsg != nil {
		q = q.Set("error_message = ?", *errMsg)
		q = q.Set("error_count = error_count + 1")
	} else {
		q = q.Set("error_message = NULL")
		q = q.Set("error_count = 0")
	}

	_, err := q.Exec(ctx)
	return err
}

// truncateError truncates error messages to a reasonable length
func truncateError(msg string) string {
	const maxLen = 1000
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen]
}

// ------------------------------------------------------------------
// Dead Letter Queue Methods
// ------------------------------------------------------------------

// ListDeadLetterJobs returns jobs that have permanently failed
func (s *JobsService) ListDeadLetterJobs(ctx context.Context, projectID string, limit, offset int) ([]*DataSourceSyncJob, int, error) {
	var jobs []*DataSourceSyncJob

	q := s.db.NewSelect().
		Model(&jobs).
		Where("status = ?", JobStatusDeadLetter)

	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}

	count, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	err = q.OrderExpr("updated_at DESC").
		Limit(limit).
		Offset(offset).
		Scan(ctx)
	if err != nil {
		return nil, 0, err
	}

	return jobs, count, nil
}

// RetryDeadLetterJob moves a dead letter job back to pending for retry
func (s *JobsService) RetryDeadLetterJob(ctx context.Context, jobID string) error {
	now := time.Now()
	res, err := s.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("status = ?", JobStatusPending).
		Set("retry_count = 0").
		Set("error_message = NULL").
		Set("error_details = NULL").
		Set("status_message = ?", "Manually retried from dead letter queue").
		Set("current_phase = NULL").
		Set("started_at = NULL").
		Set("completed_at = NULL").
		Set("next_retry_at = NULL").
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Where("status = ?", JobStatusDeadLetter).
		Exec(ctx)
	if err != nil {
		return err
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrJobNotFound
	}

	s.log.Info("dead letter job retried",
		slog.String("job_id", jobID))
	return nil
}

// DeleteDeadLetterJob permanently deletes a dead letter job
func (s *JobsService) DeleteDeadLetterJob(ctx context.Context, jobID string) error {
	res, err := s.db.NewDelete().
		Model((*DataSourceSyncJob)(nil)).
		Where("id = ?", jobID).
		Where("status = ?", JobStatusDeadLetter).
		Exec(ctx)
	if err != nil {
		return err
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return ErrJobNotFound
	}

	s.log.Info("dead letter job deleted",
		slog.String("job_id", jobID))
	return nil
}

// PurgeDeadLetterJobs deletes all dead letter jobs older than the specified duration
func (s *JobsService) PurgeDeadLetterJobs(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	res, err := s.db.NewDelete().
		Model((*DataSourceSyncJob)(nil)).
		Where("status = ?", JobStatusDeadLetter).
		Where("updated_at < ?", cutoff).
		Exec(ctx)
	if err != nil {
		return 0, err
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected > 0 {
		s.log.Info("purged dead letter jobs",
			slog.Int64("count", rowsAffected),
			slog.Duration("older_than", olderThan))
	}
	return int(rowsAffected), nil
}

// GetDeadLetterStats returns statistics about dead letter jobs
func (s *JobsService) GetDeadLetterStats(ctx context.Context, projectID string) (*DeadLetterStats, error) {
	stats := &DeadLetterStats{}

	q := s.db.NewSelect().
		Model((*DataSourceSyncJob)(nil)).
		Where("status = ?", JobStatusDeadLetter)

	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}

	// Get total count
	count, err := q.Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.TotalCount = count

	// Get oldest job timestamp
	if count > 0 {
		var oldest time.Time
		err = s.db.NewSelect().
			Model((*DataSourceSyncJob)(nil)).
			Column("updated_at").
			Where("status = ?", JobStatusDeadLetter).
			OrderExpr("updated_at ASC").
			Limit(1).
			Scan(ctx, &oldest)
		if err == nil {
			stats.OldestJobAt = &oldest
		}
	}

	return stats, nil
}

// DeadLetterStats contains statistics about dead letter jobs
type DeadLetterStats struct {
	TotalCount  int        `json:"totalCount"`
	OldestJobAt *time.Time `json:"oldestJobAt,omitempty"`
}
