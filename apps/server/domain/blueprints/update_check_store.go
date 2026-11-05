package blueprints

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// appliedBlueprintRef identifies one applied, GitHub-imported blueprint in a
// project (the (project_id, blueprint_name) the update-check job queue is keyed
// on).
type appliedBlueprintRef struct {
	ProjectID     string `bun:"project_id"`
	BlueprintName string `bun:"blueprint_name"`
}

// appliedBlueprintSource is the source provenance plus the applied manifest
// snapshot a check compares against.
type appliedBlueprintSource struct {
	ProjectID       string          `bun:"project_id"`
	BlueprintName   string          `bun:"blueprint_name"`
	SourceURL       string          `bun:"source_url"`
	SourceRef       string          `bun:"source_ref"`
	AppliedVersion  string          `bun:"version"`
	AppliedChecksum string          `bun:"checksum"`
	AppliedManifest json.RawMessage `bun:"manifest"`
	// AppliedSourceHash is the canonical hash of the raw upstream manifest at
	// import time; empty for imports predating migration 00225, in which case
	// the applied manifest is used as the content baseline.
	AppliedSourceHash string `bun:"source_manifest_checksum"`
}

// ListAppliedGithubBlueprintNames returns the distinct (project_id,
// blueprint_name) pairs that have an applied blueprint whose source_kind is
// 'github'. This is the set the always-on sweep enqueues one check job per.
func (r *Repository) ListAppliedGithubBlueprintNames(ctx context.Context) ([]appliedBlueprintRef, error) {
	var refs []appliedBlueprintRef
	err := r.db.NewRaw(`
		SELECT DISTINCT bpa.project_id, bp.name AS blueprint_name
		FROM kb.blueprint_applications bpa
		JOIN kb.blueprints bp ON bp.id = bpa.blueprint_id
		WHERE bpa.status = 'applied'
		  AND bp.source_kind = 'github'
	`).Scan(ctx, &refs)
	if err != nil {
		r.log.Error("failed to list applied github blueprint names", logger.Error(err))
		return nil, apperror.NewDatabase("blueprint update-check database error", err)
	}
	return refs, nil
}

// GetAppliedBlueprintSource returns the source provenance and applied snapshot
// for one applied GitHub-imported blueprint in a project, or ErrNotFound.
func (r *Repository) GetAppliedBlueprintSource(ctx context.Context, projectID, blueprintName string) (*appliedBlueprintSource, error) {
	var src appliedBlueprintSource
	err := r.db.NewRaw(`
		SELECT bpa.project_id, bp.name AS blueprint_name, bp.source_url, bp.source_ref,
		       bpa.version, bpa.checksum, bp.manifest, bp.source_manifest_checksum
		FROM kb.blueprint_applications bpa
		JOIN kb.blueprints bp ON bp.id = bpa.blueprint_id
		WHERE bpa.project_id = ?
		  AND bp.name = ?
		  AND bpa.status = 'applied'
		  AND bp.source_kind = 'github'
	`, projectID, blueprintName).Scan(ctx, &src)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NewNotFound("applied github blueprint", blueprintName)
		}
		r.log.Error("failed to get applied blueprint source", logger.Error(err))
		return nil, apperror.NewDatabase("blueprint update-check database error", err)
	}
	return &src, nil
}

// UpsertUpdateCheck inserts or updates the latest check result for a
// (project_id, blueprint_name) pair.
func (r *Repository) UpsertUpdateCheck(ctx context.Context, check *BlueprintUpdateCheck) error {
	_, err := r.db.NewInsert().
		Model(check).
		On("CONFLICT (project_id, blueprint_name) DO UPDATE").
		Set("source_url = EXCLUDED.source_url").
		Set("source_ref = EXCLUDED.source_ref").
		Set("latest_version = EXCLUDED.latest_version").
		Set("latest_checksum = EXCLUDED.latest_checksum").
		Set("update_available = EXCLUDED.update_available").
		Set("last_checked_at = EXCLUDED.last_checked_at").
		Set("last_check_error = EXCLUDED.last_check_error").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to upsert blueprint update check", logger.Error(err))
		return apperror.NewDatabase("blueprint update-check database error", err)
	}
	return nil
}

// MarkUpdateCheckError records a failed check for a (project_id,
// blueprint_name) pair WITHOUT clobbering the last known-good result: on
// conflict only the error timestamp/fields and provenance are refreshed, so a
// transient failure (network hiccup, deleted source) never clears a previously
// detected `update_available` or `latest_version`. The first-ever check failure
// inserts a row with no result (update_available = false).
func (r *Repository) MarkUpdateCheckError(ctx context.Context, check *BlueprintUpdateCheck) error {
	_, err := r.db.NewInsert().
		Model(check).
		On("CONFLICT (project_id, blueprint_name) DO UPDATE").
		Set("source_url = EXCLUDED.source_url").
		Set("source_ref = EXCLUDED.source_ref").
		Set("last_checked_at = EXCLUDED.last_checked_at").
		Set("last_check_error = EXCLUDED.last_check_error").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to record blueprint update-check error", logger.Error(err))
		return apperror.NewDatabase("blueprint update-check database error", err)
	}
	return nil
}

// GetUpdateCheck returns the latest check result for a (project_id,
// blueprint_name) pair, or ErrNotFound when no check has run yet.
func (r *Repository) GetUpdateCheck(ctx context.Context, projectID, blueprintName string) (*BlueprintUpdateCheck, error) {
	var check BlueprintUpdateCheck
	err := r.db.NewSelect().
		Model(&check).
		Where("project_id = ?", projectID).
		Where("blueprint_name = ?", blueprintName).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NewNotFound("update check", blueprintName)
		}
		r.log.Error("failed to get blueprint update check", logger.Error(err))
		return nil, apperror.NewDatabase("blueprint update-check database error", err)
	}
	return &check, nil
}

// findPendingUpdateJob returns the pending/running job for (project, name,
// action) if one exists, else nil.
func (r *Repository) findPendingUpdateJob(ctx context.Context, projectID, blueprintName, action string) (*BlueprintUpdateJob, error) {
	var job BlueprintUpdateJob
	err := r.db.NewSelect().
		Model(&job).
		Where("project_id = ?", projectID).
		Where("blueprint_name = ?", blueprintName).
		Where("action = ?", action).
		Where("status IN (?, ?)", UpdateJobStatusPending, UpdateJobStatusRunning).
		Order("created_at ASC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		r.log.Error("failed to find pending update job", logger.Error(err))
		return nil, apperror.NewDatabase("blueprint update-check database error", err)
	}
	return &job, nil
}

// UpdateJobOptions carries per-job policy for a queued update job.
type UpdateJobOptions struct {
	// MergeTypeConflicts allows the async apply to proceed through object-type
	// name collisions. Check jobs always store false.
	MergeTypeConflicts bool
}

// EnqueueUpdateJob inserts a pending job for (project, name, action), unless one
// is already pending/running, in which case it returns that existing job.
func (r *Repository) EnqueueUpdateJob(ctx context.Context, projectID, blueprintName, action string) (*BlueprintUpdateJob, error) {
	return r.EnqueueUpdateJobWithOptions(ctx, projectID, blueprintName, action, UpdateJobOptions{})
}

// EnqueueUpdateJobWithOptions is EnqueueUpdateJob with explicit job policy.
func (r *Repository) EnqueueUpdateJobWithOptions(ctx context.Context, projectID, blueprintName, action string, opts UpdateJobOptions) (*BlueprintUpdateJob, error) {
	if existing, err := r.findPendingUpdateJob(ctx, projectID, blueprintName, action); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	job := &BlueprintUpdateJob{
		ProjectID:          projectID,
		BlueprintName:      blueprintName,
		Action:             action,
		Status:             UpdateJobStatusPending,
		MergeTypeConflicts: action == UpdateActionUpdate && opts.MergeTypeConflicts,
		Result:             json.RawMessage(`{}`),
	}
	if _, err := r.db.NewInsert().Model(job).Exec(ctx); err != nil {
		r.log.Error("failed to enqueue update job", logger.Error(err))
		return nil, apperror.NewDatabase("blueprint update-check database error", err)
	}
	return job, nil
}

// ClaimUpdateJobs atomically claims up to `limit` eligible pending jobs and
// marks them running in a single statement (CTE + FOR UPDATE SKIP LOCKED),
// so multiple instances can run concurrently without double-claiming. Backoff
// is derived from the job's attempt count: 1m → 5m → 30m → 2h (capped). A
// `running` job whose lease (started_at) is older than the reclaim window is
// treated as orphaned (crashed/restart) and claimed again, so one stuck row
// cannot permanently starve a blueprint's checks.
func (r *Repository) ClaimUpdateJobs(ctx context.Context, limit int) ([]BlueprintUpdateJob, error) {
	var jobs []BlueprintUpdateJob
	err := r.db.NewRaw(`
		WITH claimed AS (
			SELECT id FROM kb.blueprint_update_jobs
			WHERE (
				status = 'pending'
				AND (finished_at IS NULL OR finished_at <= now() - (
				  CASE attempts
					WHEN 0 THEN interval '0 seconds'
					WHEN 1 THEN interval '1 minute'
					WHEN 2 THEN interval '5 minutes'
					WHEN 3 THEN interval '30 minutes'
					WHEN 4 THEN interval '2 hours'
					ELSE interval '0 seconds'
				  END))
			) OR (
				status = 'running'
				AND started_at IS NOT NULL
				AND started_at <= now() - interval '30 minutes'
			)
			ORDER BY created_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		UPDATE kb.blueprint_update_jobs j
		SET status = 'running', attempts = attempts + 1, started_at = now(), last_error = NULL
		FROM claimed
		WHERE j.id = claimed.id
		RETURNING j.id, j.project_id, j.blueprint_name, j.action, j.status, j.attempts,
		          j.merge_type_conflicts, j.last_error, j.result, j.created_at,
		          j.started_at, j.finished_at
	`, limit).Scan(ctx, &jobs)
	if err != nil {
		r.log.Error("failed to claim update jobs", logger.Error(err))
		return nil, apperror.NewDatabase("blueprint update-check database error", err)
	}
	return jobs, nil
}

// CompleteUpdateJob marks a claimed job succeeded with a result payload.
func (r *Repository) CompleteUpdateJob(ctx context.Context, jobID string, result json.RawMessage) error {
	now := time.Now()
	if _, err := r.db.NewUpdate().
		Model((*BlueprintUpdateJob)(nil)).
		Where("id = ?", jobID).
		Set("status = ?", UpdateJobStatusSucceeded).
		Set("result = ?", result).
		Set("last_error = NULL").
		Set("finished_at = ?", now).
		Exec(ctx); err != nil {
		r.log.Error("failed to complete update job", logger.Error(err))
		return apperror.NewDatabase("blueprint update-check database error", err)
	}
	return nil
}

// FailUpdateJob marks a claimed job terminally failed (no further attempts).
func (r *Repository) FailUpdateJob(ctx context.Context, jobID, errMsg string) error {
	now := time.Now()
	if _, err := r.db.NewUpdate().
		Model((*BlueprintUpdateJob)(nil)).
		Where("id = ?", jobID).
		Set("status = ?", UpdateJobStatusFailed).
		Set("last_error = ?", errMsg).
		Set("finished_at = ?", now).
		Exec(ctx); err != nil {
		r.log.Error("failed to fail update job", logger.Error(err))
		return apperror.NewDatabase("blueprint update-check database error", err)
	}
	return nil
}

// FailUpdateJobWithResult marks a claimed job terminally failed and persists a
// result payload (e.g. object-type conflict details) alongside the error.
func (r *Repository) FailUpdateJobWithResult(ctx context.Context, jobID, errMsg string, result json.RawMessage) error {
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	now := time.Now()
	if _, err := r.db.NewUpdate().
		Model((*BlueprintUpdateJob)(nil)).
		Where("id = ?", jobID).
		Set("status = ?", UpdateJobStatusFailed).
		Set("last_error = ?", errMsg).
		Set("result = ?", result).
		Set("finished_at = ?", now).
		Exec(ctx); err != nil {
		r.log.Error("failed to fail update job with result", logger.Error(err))
		return apperror.NewDatabase("blueprint update-check database error", err)
	}
	return nil
}

// RequeueUpdateJob returns a failed job to pending for a later retry (backoff
// enforced on claim by the attempt count), recording the error and failure time.
func (r *Repository) RequeueUpdateJob(ctx context.Context, jobID, errMsg string) error {
	now := time.Now()
	if _, err := r.db.NewUpdate().
		Model((*BlueprintUpdateJob)(nil)).
		Where("id = ?", jobID).
		Set("status = ?", UpdateJobStatusPending).
		Set("last_error = ?", errMsg).
		Set("finished_at = ?", now).
		Exec(ctx); err != nil {
		r.log.Error("failed to requeue update job", logger.Error(err))
		return apperror.NewDatabase("blueprint update-check database error", err)
	}
	return nil
}

// UpsertGithubCredential stores (or replaces) a project's encrypted GitHub PAT.
func (r *Repository) UpsertGithubCredential(ctx context.Context, cred *GithubCredential) error {
	_, err := r.db.NewInsert().
		Model(cred).
		On("CONFLICT (project_id) DO UPDATE").
		Set("encrypted_token = EXCLUDED.encrypted_token").
		Set("nonce = EXCLUDED.nonce").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to upsert github credential", logger.Error(err))
		return apperror.NewDatabase("blueprint update-check database error", err)
	}
	return nil
}

// GetGithubCredential returns a project's encrypted GitHub PAT, or ErrNotFound.
func (r *Repository) GetGithubCredential(ctx context.Context, projectID string) (*GithubCredential, error) {
	var cred GithubCredential
	err := r.db.NewSelect().
		Model(&cred).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NewNotFound("github credential", projectID)
		}
		r.log.Error("failed to get github credential", logger.Error(err))
		return nil, apperror.NewDatabase("blueprint update-check database error", err)
	}
	return &cred, nil
}

// GetUpdateJob returns an update job by ID, or ErrNotFound.
func (r *Repository) GetUpdateJob(ctx context.Context, jobID string) (*BlueprintUpdateJob, error) {
	var job BlueprintUpdateJob
	err := r.db.NewSelect().
		Model(&job).
		Where("id = ?", jobID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NewNotFound("update job", jobID)
		}
		r.log.Error("failed to get update job", logger.Error(err))
		return nil, apperror.NewDatabase("blueprint update-check database error", err)
	}
	return &job, nil
}

// GetLatestUpdateJob returns the most recently-created update job (any action)
// for a (project, blueprint_name) pair, or ErrNotFound when none has been
// enqueued.
func (r *Repository) GetLatestUpdateJob(ctx context.Context, projectID, blueprintName string) (*BlueprintUpdateJob, error) {
	var job BlueprintUpdateJob
	err := r.db.NewSelect().
		Model(&job).
		Where("project_id = ?", projectID).
		Where("blueprint_name = ?", blueprintName).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NewNotFound("update job", blueprintName)
		}
		r.log.Error("failed to get latest update job", logger.Error(err))
		return nil, apperror.NewDatabase("blueprint update-check database error", err)
	}
	return &job, nil
}
