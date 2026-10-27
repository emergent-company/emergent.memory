package sources

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// ErrSyncAlreadyInProgress is the typed admission result returned when a source
// already has an active (pending/processing) sync job. The service maps it to a
// TriggerSyncResponse carrying the existing job rather than a second job.
var ErrSyncAlreadyInProgress = errors.New("sync already in progress")

// CreateSyncJob inserts a pending sync job. The partial unique admission index
// (idx_source_sync_jobs_active_source) rejects a second active job per source;
// that unique violation is mapped to ErrSyncAlreadyInProgress.
func (r *Repository) CreateSyncJob(ctx context.Context, job *SyncJob) (*SyncJob, error) {
	if _, err := r.db.NewInsert().Model(job).Exec(ctx); err != nil {
		if pgutils.IsUniqueViolation(err) {
			return nil, ErrSyncAlreadyInProgress
		}
		r.log.Error("failed to create sync job", "error", err)
		return nil, apperror.NewDatabase("failed to create sync job", err)
	}
	return job, nil
}

// GetActiveSyncJob returns the currently active (pending/processing) job for a
// source, or nil when none exists.
func (r *Repository) GetActiveSyncJob(ctx context.Context, sourceID uuid.UUID) (*SyncJob, error) {
	job := &SyncJob{}
	err := r.db.NewSelect().
		Model(job).
		Where("source_id = ?", sourceID).
		Where("status IN (?)", bun.In([]string{SyncStatusPending, SyncStatusProcessing})).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		r.log.Error("failed to get active sync job", "error", err)
		return nil, apperror.NewDatabase("failed to get active sync job", err)
	}
	return job, nil
}

// GetSyncJobByID loads a sync job by id with no project scope (worker use — the
// job carries its own project_id).
func (r *Repository) GetSyncJobByID(ctx context.Context, jobID uuid.UUID) (*SyncJob, error) {
	job := &SyncJob{}
	err := r.db.NewSelect().Model(job).Where("id = ?", jobID).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		r.log.Error("failed to get sync job by id", "error", err)
		return nil, apperror.NewDatabase("failed to get sync job", err)
	}
	return job, nil
}

// GetSyncJob loads a sync job scoped to source + project (cross-project → nil).
func (r *Repository) GetSyncJob(ctx context.Context, projectID, sourceID, jobID uuid.UUID) (*SyncJob, error) {
	job := &SyncJob{}
	err := r.db.NewSelect().
		Model(job).
		Where("id = ?", jobID).
		Where("source_id = ?", sourceID).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		r.log.Error("failed to get sync job", "error", err)
		return nil, apperror.NewDatabase("failed to get sync job", err)
	}
	return job, nil
}

// ListSyncJobs returns a source's sync jobs, newest first, optionally filtered
// by status.
func (r *Repository) ListSyncJobs(ctx context.Context, projectID, sourceID uuid.UUID, status *string) ([]*SyncJob, error) {
	q := r.db.NewSelect().
		Model(&[]*SyncJob{}).
		Where("source_id = ?", sourceID).
		Where("project_id = ?", projectID)
	if status != nil && *status != "" {
		q = q.Where("status = ?", *status)
	}
	var jobs []*SyncJob
	if err := q.Order("created_at DESC").Scan(ctx, &jobs); err != nil {
		r.log.Error("failed to list sync jobs", "error", err)
		return nil, apperror.NewDatabase("failed to list sync jobs", err)
	}
	if jobs == nil {
		jobs = []*SyncJob{}
	}
	return jobs, nil
}

// GetLatestSyncJob returns the most recent sync job for a source, or nil.
func (r *Repository) GetLatestSyncJob(ctx context.Context, projectID, sourceID uuid.UUID) (*SyncJob, error) {
	job := &SyncJob{}
	err := r.db.NewSelect().
		Model(job).
		Where("source_id = ?", sourceID).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		r.log.Error("failed to get latest sync job", "error", err)
		return nil, apperror.NewDatabase("failed to get latest sync job", err)
	}
	return job, nil
}

// DequeueSyncJob atomically claims pending sync jobs (pending → processing) with
// FOR UPDATE SKIP LOCKED, matching the internal/jobs.Queue contract (reads
// scheduled_at/priority, writes status/started_at/updated_at). It returns the
// claimed ids; callers load full rows via GetSyncJobByID.
func (r *Repository) DequeueSyncJob(ctx context.Context, batchSize int) ([]uuid.UUID, error) {
	if batchSize <= 0 {
		batchSize = 10
	}
	var ids []uuid.UUID
	_, err := r.db.NewRaw(`
		WITH cte AS (
			SELECT id FROM kb.source_sync_jobs
			WHERE status = 'pending' AND (scheduled_at IS NULL OR scheduled_at <= clock_timestamp())
			ORDER BY priority DESC, scheduled_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT ?
		)
		UPDATE kb.source_sync_jobs j
		SET status = 'processing', started_at = now(), updated_at = now()
		FROM cte WHERE j.id = cte.id
		RETURNING j.id`, batchSize).Exec(ctx, &ids)
	if err != nil {
		return nil, fmt.Errorf("dequeue sync jobs: %w", err)
	}
	return ids, nil
}

// SyncProgress is a durable progress snapshot written by the worker as it
// advances. Zero-valued fields are left unchanged.
type SyncProgress struct {
	TotalItems      int
	ProcessedItems  int
	SuccessfulItems int
	FailedItems     int
	SkippedItems    int
	Phase           *string
	Message         *string
}

// UpdateSyncJobProgress persists counters, phase, and message for a job.
func (r *Repository) UpdateSyncJobProgress(ctx context.Context, jobID uuid.UUID, p SyncProgress) error {
	q := r.db.NewUpdate().
		Model((*SyncJob)(nil)).
		Set("total_items = ?", p.TotalItems).
		Set("processed_items = ?", p.ProcessedItems).
		Set("successful_items = ?", p.SuccessfulItems).
		Set("failed_items = ?", p.FailedItems).
		Set("skipped_items = ?", p.SkippedItems).
		Set("updated_at = now()").
		Where("id = ?", jobID)
	if p.Phase != nil {
		q = q.Set("current_phase = ?", *p.Phase)
	}
	if p.Message != nil {
		q = q.Set("status_message = ?", *p.Message)
	}
	if _, err := q.Exec(ctx); err != nil {
		r.log.Error("failed to update sync job progress", "error", err)
		return apperror.NewDatabase("failed to update sync job progress", err)
	}
	return nil
}

// MarkSyncJobCompleted transitions a job to completed, stamping completed_at. Any
// per-item error detail previously persisted in the `error` column is retained
// so a completed-with-failures job still reports its failures.
func (r *Repository) MarkSyncJobCompleted(ctx context.Context, jobID uuid.UUID, message string) error {
	_, err := r.db.NewUpdate().
		Model((*SyncJob)(nil)).
		Set("status = ?", SyncStatusCompleted).
		Set("current_phase = NULL").
		Set("completed_at = now()").
		Set("updated_at = now()").
		Set("status_message = ?", message).
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to complete sync job", "error", err)
		return apperror.NewDatabase("failed to complete sync job", err)
	}
	return nil
}

// RecordSyncJobErrorDetail persists structured per-item (or top-level) error
// detail into the job's `error` jsonb column without changing status.
func (r *Repository) RecordSyncJobErrorDetail(ctx context.Context, jobID uuid.UUID, detail map[string]any) error {
	_, err := r.db.NewUpdate().
		Model((*SyncJob)(nil)).
		Set("error = ?", detail).
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to record sync job error detail", "error", err)
		return apperror.NewDatabase("failed to record sync job error detail", err)
	}
	return nil
}

// MarkSyncJobFailed transitions a job to terminal failed, recording the
// structured error (message + per-item errors) and incrementing attempt_count.
// Ingested items are left intact; the scheduler re-admits the source because
// last_synced_at was not advanced. The phase is cleared: a failed job is not
// still "enumerating", and a stale phase in the UI contradicts the status.
func (r *Repository) MarkSyncJobFailed(ctx context.Context, jobID uuid.UUID, errMsg string, errDetail map[string]any) error {
	q := r.db.NewUpdate().
		Model((*SyncJob)(nil)).
		Set("status = ?", SyncStatusFailed).
		Set("attempt_count = attempt_count + 1").
		Set("last_error = ?", errMsg).
		Set("current_phase = NULL").
		Set("completed_at = now()").
		Set("updated_at = now()").
		Where("id = ?", jobID)
	if errDetail != nil {
		q = q.Set("error = ?", errDetail)
	}
	if _, err := q.Exec(ctx); err != nil {
		r.log.Error("failed to fail sync job", "error", err)
		return apperror.NewDatabase("failed to fail sync job", err)
	}
	return nil
}

// MarkSyncJobCancelled transitions a job to cancelled (terminal).
func (r *Repository) MarkSyncJobCancelled(ctx context.Context, jobID uuid.UUID, message string) error {
	_, err := r.db.NewUpdate().
		Model((*SyncJob)(nil)).
		Set("status = ?", SyncStatusCancelled).
		Set("current_phase = NULL").
		Set("completed_at = now()").
		Set("updated_at = now()").
		Set("status_message = ?", message).
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to cancel sync job", "error", err)
		return apperror.NewDatabase("failed to cancel sync job", err)
	}
	return nil
}

// CancelSyncJob marks an active job cancelled, scoped to source + project.
// Returns the updated job, or (nil, nil) when no active job matches.
func (r *Repository) CancelSyncJob(ctx context.Context, projectID, sourceID, jobID uuid.UUID) (*SyncJob, error) {
	res, err := r.db.NewUpdate().
		Model((*SyncJob)(nil)).
		Set("status = ?", SyncStatusCancelled).
		Set("current_phase = NULL").
		Set("completed_at = now()").
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Where("source_id = ?", sourceID).
		Where("project_id = ?", projectID).
		Where("status IN (?)", bun.In([]string{SyncStatusPending, SyncStatusProcessing})).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to cancel sync job", "error", err)
		return nil, apperror.NewDatabase("failed to cancel sync job", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil
	}
	return r.GetSyncJob(ctx, projectID, sourceID, jobID)
}

// RecoverStaleJobs requeues processing jobs stuck past the threshold (started_at
// older than the threshold) back to pending, matching the internal/jobs
// RecoverStaleJobs contract.
func (r *Repository) RecoverStaleJobs(ctx context.Context, staleThresholdMinutes int) (int, error) {
	if staleThresholdMinutes <= 0 {
		staleThresholdMinutes = 10
	}
	res, err := r.db.NewRaw(`
		UPDATE kb.source_sync_jobs
		SET status = 'pending', started_at = NULL, scheduled_at = now(), updated_at = now()
		WHERE status = 'processing'
			AND started_at < clock_timestamp() - (? || ' minutes')::interval`,
		fmt.Sprintf("%d", staleThresholdMinutes)).Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("recover stale sync jobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AdvanceSourceCursor persists the source cursor + last_synced_at after a
// successful batch, and clears the last-error state.
func (r *Repository) AdvanceSourceCursor(ctx context.Context, projectID, sourceID uuid.UUID, cursor map[string]any, syncedAt time.Time) error {
	res, err := r.db.NewUpdate().
		Model((*Source)(nil)).
		Set("cursor = ?", cursor).
		Set("last_synced_at = ?", syncedAt).
		Set("last_error = NULL").
		Set("last_error_at = NULL").
		Set("error_count = 0").
		Set("updated_at = now()").
		Where("id = ?", sourceID).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to advance source cursor", "error", err)
		return apperror.NewDatabase("failed to advance source cursor", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

// RecordSourceError records a sync failure on the source row (error message +
// incrementing error count) without advancing last_synced_at.
func (r *Repository) RecordSourceError(ctx context.Context, projectID, sourceID uuid.UUID, errMsg string) error {
	_, err := r.db.NewUpdate().
		Model((*Source)(nil)).
		Set("last_error = ?", errMsg).
		Set("last_error_at = now()").
		Set("error_count = error_count + 1").
		Set("updated_at = now()").
		Where("id = ?", sourceID).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to record source error", "error", err)
		return apperror.NewDatabase("failed to record source error", err)
	}
	return nil
}

// ListDueSources returns sources whose sync_interval_minutes is set and whose
// last_synced_at is older than the interval (or never synced), i.e. the sources
// the scheduler should admit next.
func (r *Repository) ListDueSources(ctx context.Context, now time.Time) ([]*Source, error) {
	var srcs []*Source
	err := r.db.NewSelect().
		Model(&srcs).
		Where("status = ?", SourceStatusActive).
		Where("sync_interval_minutes IS NOT NULL").
		Where("sync_interval_minutes > 0").
		Where("last_synced_at IS NULL OR last_synced_at < now() - make_interval(mins => sync_interval_minutes)").
		ExcludeColumn("encrypted_credentials").
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list due sources", "error", err)
		return nil, apperror.NewDatabase("failed to list due sources", err)
	}
	if srcs == nil {
		srcs = []*Source{}
	}
	return srcs, nil
}

// ListHeadSourceRevisions returns externalID → headRevisionID for the HEAD
// documents (supersedes_id IS NULL) of a source. The keys are the currently
// ingested external ids (used for removals); the values are the cheap revision
// pre-filter for change detection.
func (r *Repository) ListHeadSourceRevisions(ctx context.Context, projectID, sourceID uuid.UUID) (map[string]string, error) {
	type row struct {
		ExternalID string  `bun:"external_id"`
		RevisionID *string `bun:"revision_id"`
	}
	var rows []row
	err := r.db.NewSelect().
		TableExpr("kb.documents AS d").
		ColumnExpr("d.source_external_id AS external_id, d.source_revision_id AS revision_id").
		Where("d.project_id = ?", projectID).
		Where("d.source_id = ?", sourceID).
		Where("d.supersedes_id IS NULL").
		Where("d.source_external_id IS NOT NULL").
		Scan(ctx, &rows)
	if err != nil {
		r.log.Error("failed to list head source revisions", "error", err)
		return nil, apperror.NewDatabase("failed to list head source revisions", err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		rev := ""
		if row.RevisionID != nil {
			rev = *row.RevisionID
		}
		out[row.ExternalID] = rev
	}
	return out, nil
}

// ListDeletedHeadSourceIDs returns the set of source_external_ids whose HEAD
// document is currently marked deleted_from_source. The worker uses this to
// bypass the revision pre-filter for re-appearing items: restoring an item from
// Drive trash does not bump its headRevisionId, so an unchanged revision must
// still be routed through ApplySourceItem to reactivate it.
func (r *Repository) ListDeletedHeadSourceIDs(ctx context.Context, projectID, sourceID uuid.UUID) (map[string]bool, error) {
	var externalIDs []string
	err := r.db.NewSelect().
		TableExpr("kb.documents AS d").
		ColumnExpr("d.source_external_id").
		Where("d.project_id = ?", projectID).
		Where("d.source_id = ?", sourceID).
		Where("d.supersedes_id IS NULL").
		Where("d.source_external_id IS NOT NULL").
		Where("d.source_status = ?", "deleted_from_source").
		Scan(ctx, &externalIDs)
	if err != nil {
		r.log.Error("failed to list deleted head source ids", "error", err)
		return nil, apperror.NewDatabase("failed to list deleted head source ids", err)
	}
	out := make(map[string]bool, len(externalIDs))
	for _, id := range externalIDs {
		out[id] = true
	}
	return out, nil
}
