package sources

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TriggerSync admits a manual sync job for a source through the single-active-job
// admission rule. When a job is already active, it returns the existing job in a
// non-success TriggerSyncResponse (no error) — the SDK reads success=false and
// the jobId rather than an HTTP error.
func (s *Service) TriggerSync(ctx context.Context, projectID, sourceID uuid.UUID) (*TriggerSyncResponse, error) {
	if _, err := s.repo.GetByID(ctx, sourceID, projectID); err != nil {
		return nil, err
	}

	// Fast path: return the active job without attempting an insert. This keeps
	// the common re-trigger case off the unique-violation path (which, inside a
	// caller-held transaction, would abort the transaction).
	if active, err := s.repo.GetActiveSyncJob(ctx, sourceID); err != nil {
		return nil, err
	} else if active != nil {
		id := active.ID.String()
		return &TriggerSyncResponse{Success: false, Message: "sync already in progress", JobID: &id}, nil
	}

	now := s.now()
	job := &SyncJob{
		ID:          uuid.New(),
		Status:      SyncStatusPending,
		ScheduledAt: now,
		ProjectID:   projectID,
		SourceID:    sourceID,
		TriggerType: TriggerTypeManual,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	created, err := s.repo.CreateSyncJob(ctx, job)
	if err != nil {
		if errors.Is(err, ErrSyncAlreadyInProgress) {
			// Concurrent-trigger fallback: the unique index caught a race the
			// pre-check missed. Return the now-active job.
			active, aerr := s.repo.GetActiveSyncJob(ctx, sourceID)
			if aerr != nil {
				return nil, aerr
			}
			resp := &TriggerSyncResponse{Success: false, Message: "sync already in progress"}
			if active != nil {
				id := active.ID.String()
				resp.JobID = &id
			}
			return resp, nil
		}
		return nil, err
	}

	id := created.ID.String()
	return &TriggerSyncResponse{Success: true, Message: "sync started", JobID: &id}, nil
}

// ListSyncJobs returns a source's sync jobs (newest first) as SDK DTOs.
func (s *Service) ListSyncJobs(ctx context.Context, projectID, sourceID uuid.UUID, status *string) (*ListSyncJobsResponse, error) {
	if _, err := s.repo.GetByID(ctx, sourceID, projectID); err != nil {
		return nil, err
	}
	jobs, err := s.repo.ListSyncJobs(ctx, projectID, sourceID, status)
	if err != nil {
		return nil, err
	}
	items := make([]SyncJobDTO, len(jobs))
	for i, j := range jobs {
		items[i] = toSyncJobDTO(j)
	}
	return &ListSyncJobsResponse{Items: items, Total: len(items)}, nil
}

// GetLatestSyncJob returns the most recent sync job for a source, or nil.
func (s *Service) GetLatestSyncJob(ctx context.Context, projectID, sourceID uuid.UUID) (*SyncJobDTO, error) {
	if _, err := s.repo.GetByID(ctx, sourceID, projectID); err != nil {
		return nil, err
	}
	job, err := s.repo.GetLatestSyncJob(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	dto := toSyncJobDTO(job)
	return &dto, nil
}

// GetSyncJob returns a single sync job scoped to source + project (cross-project
// → not-found).
func (s *Service) GetSyncJob(ctx context.Context, projectID, sourceID, jobID uuid.UUID) (*SyncJobDTO, error) {
	job, err := s.repo.GetSyncJob(ctx, projectID, sourceID, jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, apperror.ErrNotFound
	}
	dto := toSyncJobDTO(job)
	return &dto, nil
}

// CancelSyncJob cancels an active (pending/processing) sync job scoped to source
// + project.
func (s *Service) CancelSyncJob(ctx context.Context, projectID, sourceID, jobID uuid.UUID) (*CancelSyncJobResponse, error) {
	if _, err := s.repo.GetByID(ctx, sourceID, projectID); err != nil {
		return nil, err
	}
	job, err := s.repo.CancelSyncJob(ctx, projectID, sourceID, jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		// No active job matched (already terminal or wrong scope). Resolve as
		// not-found so the SDK can distinguish "nothing to cancel".
		return nil, apperror.ErrNotFound
	}
	return &CancelSyncJobResponse{Success: true, Message: "sync cancelled"}, nil
}

// AdmitDueSources admits a scheduled sync job for every source whose sync
// interval has elapsed (or that has never synced), through the same
// single-active-job admission rule as manual triggers. It is best-effort: an
// active job suppresses the duplicate, and a transient failure on one source
// does not stop the rest.
func (s *Service) AdmitDueSources(ctx context.Context) error {
	due, err := s.repo.ListDueSources(ctx, s.now())
	if err != nil {
		return err
	}
	now := s.now()
	for _, src := range due {
		// Fast path: skip sources that already have an active job, keeping the
		// common re-admission case off the unique-violation path (which aborts a
		// caller-held transaction).
		if active, err := s.repo.GetActiveSyncJob(ctx, src.ID); err != nil {
			s.log.Warn("failed to check active sync job", "source_id", src.ID, "error", err)
			continue
		} else if active != nil {
			continue
		}

		job := &SyncJob{
			ID:          uuid.New(),
			Status:      SyncStatusPending,
			ScheduledAt: now,
			ProjectID:   src.ProjectID,
			SourceID:    src.ID,
			TriggerType: TriggerTypeScheduled,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if _, err := s.repo.CreateSyncJob(ctx, job); err != nil {
			if errors.Is(err, ErrSyncAlreadyInProgress) {
				continue // active job already running for this source
			}
			s.log.Warn("failed to admit scheduled sync", "source_id", src.ID, "error", err)
			continue
		}
	}
	return nil
}

// toSyncJobDTO projects a persisted SyncJob row onto the SDK sync-job shape.
func toSyncJobDTO(j *SyncJob) SyncJobDTO {
	return SyncJobDTO{
		ID:              j.ID.String(),
		IntegrationID:   j.SourceID.String(),
		Status:          j.Status,
		TotalItems:      j.TotalItems,
		ProcessedItems:  j.ProcessedItems,
		SuccessfulItems: j.SuccessfulItems,
		FailedItems:     j.FailedItems,
		SkippedItems:    j.SkippedItems,
		CurrentPhase:    j.CurrentPhase,
		StatusMessage:   j.StatusMessage,
		ErrorMessage:    j.LastError,
		TriggerType:     j.TriggerType,
		RetryCount:      j.AttemptCount,
		MaxRetries:      maxSyncRetries,
		CreatedAt:       j.CreatedAt,
		StartedAt:       j.StartedAt,
		CompletedAt:     j.CompletedAt,
	}
}
