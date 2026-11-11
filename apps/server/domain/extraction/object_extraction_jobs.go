package extraction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/modelconfig"
	"github.com/emergent-company/emergent.memory/internal/jobs"
	"github.com/emergent-company/emergent.memory/pkg/logger"
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
	// SkipStagingBranch disables the staging branch for extraction jobs.
	// When true, extracted objects are written directly to the main graph without review.
	SkipStagingBranch bool
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

// GenerativeModelResolver resolves a project's effective generative model.
// Implemented by *modelconfig.Service. It is an optional seam on the jobs
// service: when nil, job creation is not gated (fail-open) so callers and tests
// that construct the service without a resolver keep working unchanged.
type GenerativeModelResolver interface {
	ResolveGenerativeModel(ctx context.Context, projectID uuid.UUID) (string, modelconfig.ModelSource, error)
}

// noGenerativeModelMessage is the user-facing reason surfaced by callers (the
// admin API's 409 no_generative_model body). It is intentionally sentence-cased;
// the ErrNoGenerativeModel sentinel carries its own lower-case, punctuation-free
// text so staticcheck ST1005 stays clean.
const noGenerativeModelMessage = "No generative model is configured for this project. Add a provider and set a default model before creating extraction jobs."

// ErrNoGenerativeModel is returned by CreateJob when the addressed project has
// no resolvable generative model. The gate lives at the job-creation chokepoint
// so every creation path (admin API, MCP queue-reextraction, document
// auto-extract) is covered. Callers map it to their own surface: the admin API
// returns 409 no_generative_model; MCP returns a tool error; the document
// auto-extract path logs and skips without failing the upload.
var ErrNoGenerativeModel = errors.New("no generative model is configured for this project")

// ErrJobStillRunning is returned by RetryJob when the addressed job is
// 'processing' but younger than the stale-job threshold, so the worker that
// claimed it is probably still alive. Requeuing a live run would process the
// same unit twice and let the older run overwrite the newer attempt's terminal
// state. Callers map it to 409 Conflict. Genuinely stuck jobs are converted to
// 'failed' by the stale-job sweep and retried through the linked-run path.
var ErrJobStillRunning = errors.New("extraction job is still running")

// StaleMinutesResolver reports the threshold, in minutes, after which a
// processing job counts as stuck. It is wired to the scheduler's
// StaleJobCleanupTask so the retry path and the stale-job sweep share one
// definition of "stuck" (and a runtime threshold override applies to both).
type StaleMinutesResolver func() int

// ObjectExtractionJobsService manages object extraction jobs
type ObjectExtractionJobsService struct {
	db            bun.IDB
	log           *slog.Logger
	config        *ObjectExtractionConfig
	modelResolver GenerativeModelResolver
	staleMinutes  StaleMinutesResolver
}

// NewObjectExtractionJobsService creates a new object extraction jobs service.
// Job creation is not gated unless a GenerativeModelResolver is attached with
// WithGenerativeModelResolver.
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

// WithGenerativeModelResolver attaches the resolver used to reject job creation
// when the project has no effective generative model (issue #1555). Passing nil
// disables the gate (fail-open).
func (s *ObjectExtractionJobsService) WithGenerativeModelResolver(resolver GenerativeModelResolver) *ObjectExtractionJobsService {
	s.modelResolver = resolver
	return s
}

// WithStaleMinutesResolver attaches the threshold that defines a stuck
// 'processing' job. Production wires the scheduler's StaleJobCleanupTask here so
// the retry guard and the sweep cannot drift apart. Passing nil falls back to
// the extraction config's own threshold, which is the value RecoverStaleJobs
// already uses.
func (s *ObjectExtractionJobsService) WithStaleMinutesResolver(resolver StaleMinutesResolver) *ObjectExtractionJobsService {
	s.staleMinutes = resolver
	return s
}

// staleThresholdMinutes returns the stale-job threshold in minutes.
func (s *ObjectExtractionJobsService) staleThresholdMinutes() int {
	if s.staleMinutes != nil {
		if minutes := s.staleMinutes(); minutes > 0 {
			return minutes
		}
	}
	if s.config != nil && s.config.StaleThresholdMinutes > 0 {
		return s.config.StaleThresholdMinutes
	}
	return 30
}

// ensureGenerativeModel is the job-creation chokepoint gate. It fails open when
// no resolver is wired, when the project ID is not a UUID, or when resolution
// itself errors (a transient model-config outage must not disable extraction) —
// matching the gateway's extractionModelMissing convention (PR #1522). Only a
// definitive "no model resolves" (empty model name) is rejected.
func (s *ObjectExtractionJobsService) ensureGenerativeModel(ctx context.Context, projectID string) error {
	if s.modelResolver == nil {
		return nil
	}
	id, err := uuid.Parse(projectID)
	if err != nil {
		return nil
	}
	model, _, err := s.modelResolver.ResolveGenerativeModel(ctx, id)
	if err != nil {
		return nil
	}
	if model == "" {
		return ErrNoGenerativeModel
	}
	return nil
}

// DB returns the underlying database handle. The admin handler uses it to
// enforce project membership at the handler layer for routes whose addressed
// project is not the :projectId path param (issue #959).
func (s *ObjectExtractionJobsService) DB() bun.IDB { return s.db }

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

// CreateJob creates a new object extraction job.
//
// This is the single chokepoint for every extraction-job creation path (admin
// API, MCP queue-reextraction, document auto-extract). Before inserting, it
// rejects the request with ErrNoGenerativeModel when the project has no
// resolvable generative model, so a doomed job is never enqueued only to fail
// at run time (issue #1555). With no resolver attached the gate is a no-op.
func (s *ObjectExtractionJobsService) CreateJob(ctx context.Context, opts CreateObjectExtractionJobOptions) (*ObjectExtractionJob, error) {
	if err := s.ensureGenerativeModel(ctx, opts.ProjectID); err != nil {
		return nil, err
	}

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

// MarkCompleted marks a job as completed with results.
//
// The write only applies while the job is 'processing'. A job that was
// requeued, cancelled or swept while this run was in flight keeps its current
// state: writing completion onto it would overwrite the newer owner's state.
func (s *ObjectExtractionJobsService) MarkCompleted(ctx context.Context, jobID string, results ObjectExtractionResults) error {
	now := time.Now().UTC()

	// created_object_ids is NOT NULL with default '{}'. Writing pq.Array(nil)
	// for a zero-object completion emits SQL NULL and violates the constraint
	// (SQLSTATE 23502). Coerce to a non-nil empty slice so "no objects created"
	// is stored as an empty array (issue #894).
	createdObjectIDs := results.CreatedObjectIDs
	if createdObjectIDs == nil {
		createdObjectIDs = []string{}
	}

	res, err := s.db.NewUpdate().
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
		Set("created_object_ids = ?", pq.Array(createdObjectIDs)).
		Set("debug_info = ?", results.DebugInfo).
		Where("id = ?", jobID).
		Where("status = ?", JobStatusProcessing).
		Exec(ctx)
	if err != nil {
		s.log.Error("failed to mark job completed",
			slog.String("id", jobID),
			logger.Error(err))
		return fmt.Errorf("mark completed: %w", err)
	}

	if affected, err := res.RowsAffected(); err == nil && affected == 0 {
		// The job left 'processing' while this run was in flight (requeued,
		// cancelled or swept). Its terminal state belongs to whoever owns it
		// now; writing completion here would overwrite that state.
		s.log.Warn("ignored completion for a job that is no longer processing",
			slog.String("id", jobID))
		return nil
	}

	s.log.Info("object extraction job completed",
		slog.String("id", jobID),
		slog.Int("objectsCreated", results.ObjectsCreated),
		slog.Int("successfulItems", results.SuccessfulItems))

	return nil
}

// SchemaFingerprint identifies the schema configuration a job extracted with:
// the explicit enabled types (order-insensitive) plus the job's extraction
// config. A document-triggered run leaves both empty because the project's
// installed schema packs are resolved at run time; the fingerprint then matches
// any other document-triggered run, which is exactly the comparison issue #240
// describes. When a caller does pin types or config, a run that changed them is
// a different configuration and is not compared.
func (j *ObjectExtractionJob) SchemaFingerprint() string {
	if j == nil {
		return ""
	}
	types := append([]string(nil), j.EnabledTypes...)
	if types == nil {
		// Normalise the nil in-memory slice against the empty array the same
		// column scans back as, so a job compared before insert and one read
		// back from the database share a fingerprint.
		types = []string{}
	}
	sort.Strings(types)
	config := j.ExtractionConfig
	if config == nil {
		config = JSON{}
	}
	cfg, err := json.Marshal(config)
	if err != nil {
		cfg = []byte("null")
	}
	out, err := json.Marshal(struct {
		Types  []string `json:"types"`
		Config string   `json:"config"`
	}{types, string(cfg)})
	if err != nil {
		return ""
	}
	return string(out)
}

// RunCountSnapshot is the object count of one completed extraction run, used to
// decide whether a new run deviated strongly from the previous one.
type RunCountSnapshot struct {
	JobID          string
	ObjectsCreated int
	CompletedAt    *time.Time
}

// PreviousCompletedRunForDocument returns the most recent completed run over the
// same project + document whose schema fingerprint matches the job's, excluding
// the job itself. It returns nil (no error) when the job has no document or no
// earlier comparable run exists — the first run of a document never warns.
func (s *ObjectExtractionJobsService) PreviousCompletedRunForDocument(ctx context.Context, job *ObjectExtractionJob) (*RunCountSnapshot, error) {
	if s == nil || job == nil || job.DocumentID == nil || *job.DocumentID == "" {
		return nil, nil
	}
	// Take a few of the most recent completed runs rather than only the very
	// last one: an interleaved job created with a different explicit config (e.g.
	// an API run with pinned types) must not hide the comparable run behind it.
	const lookback = 5
	var candidates []ObjectExtractionJob
	if err := s.db.NewSelect().
		Model(&candidates).
		Where("project_id = ?", job.ProjectID).
		Where("document_id = ?", *job.DocumentID).
		Where("status = ?", JobStatusCompleted).
		Where("id <> ?", job.ID).
		Order("completed_at DESC NULLS LAST", "created_at DESC").
		Limit(lookback).
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("find previous completed extraction run: %w", err)
	}
	want := job.SchemaFingerprint()
	for i := range candidates {
		if candidates[i].SchemaFingerprint() != want {
			continue
		}
		return &RunCountSnapshot{
			JobID:          candidates[i].ID,
			ObjectsCreated: candidates[i].ObjectsCreated,
			CompletedAt:    candidates[i].CompletedAt,
		}, nil
	}
	return nil, nil
}

// ObjectExtractionResults contains the results of an object extraction job
type ObjectExtractionResults struct {
	ObjectsCreated       int
	ObjectsMerged        int // entities merged into existing objects (CreateOrUpdate, not new rows)
	RelationshipsCreated int
	SuggestionsCreated   int
	TotalItems           int
	ProcessedItems       int
	SuccessfulItems      int
	FailedItems          int
	DiscoveredTypes      JSONArray
	CreatedObjects       JSONArray
	CreatedObjectIDs     []string
	DebugInfo            JSON
	// VarianceWarning is set when this run's object count deviates strongly
	// from the previous completed run over the same document and schema
	// configuration. It rides into the job's debug_info and the run's outcome
	// notification so the operator sees it before merging (#240).
	VarianceWarning *RunVarianceWarning
	// SkippedTypes lists the object types omitted because their installed schema
	// sets skipExtraction (sorted). They are also written to DebugInfo.
	SkippedTypes []string
	// OutcomeWarning is set when the run's outcome needs naming: types skipped
	// by skipExtraction and/or a run that wrote no objects. It rides the same
	// debug_info/notification path as VarianceWarning (#233).
	OutcomeWarning *RunOutcomeWarning
}

// MarkFailed marks a job as failed with an error message
// If retries are available, schedules a retry; otherwise marks as permanently failed.
// Like MarkCompleted, it only writes while the job is 'processing', so a stale
// worker cannot clobber a job that was requeued or cancelled behind it.
// MarkFailed records a failed attempt and either schedules a retry or moves the
// job to dead letter. It reports whether this attempt was the final one: only a
// dead letter is worth a user-visible failure notification, because announcing
// each retry would notify the same people up to max_retries times for one run.
func (s *ObjectExtractionJobsService) MarkFailed(ctx context.Context, jobID string, errorMessage string, errorDetails JSON) (bool, error) {
	now := time.Now().UTC()

	// Get current retry count
	job := new(ObjectExtractionJob)
	err := s.db.NewSelect().
		Model(job).
		Column("retry_count", "max_retries").
		Where("id = ?", jobID).
		Scan(ctx)
	if err != nil {
		return false, fmt.Errorf("get job for retry check: %w", err)
	}

	newRetryCount := job.RetryCount + 1

	if newRetryCount >= job.MaxRetries {
		// Move to dead letter queue
		res, err := s.db.NewUpdate().
			Model((*ObjectExtractionJob)(nil)).
			Set("status = ?", JobStatusDeadLetter).
			Set("completed_at = ?", now).
			Set("updated_at = ?", now).
			Set("error_message = ?", errorMessage).
			Set("error_details = ?", errorDetails).
			Set("retry_count = ?", newRetryCount).
			Where("id = ?", jobID).
			Where("status = ?", JobStatusProcessing).
			Exec(ctx)
		if err != nil {
			return false, fmt.Errorf("mark as dead letter: %w", err)
		}
		if affected, err := res.RowsAffected(); err == nil && affected == 0 {
			s.log.Warn("ignored failure for a job that is no longer processing",
				slog.String("id", jobID))
			return false, nil
		}

		s.log.Warn("object extraction job moved to dead letter queue",
			slog.String("id", jobID),
			slog.String("error", errorMessage),
			slog.Int("retryCount", newRetryCount))
		return true, nil
	} else {
		// Schedule retry - reset to pending
		res, err := s.db.NewUpdate().
			Model((*ObjectExtractionJob)(nil)).
			Set("status = ?", JobStatusPending).
			Set("updated_at = ?", now).
			Set("error_message = ?", errorMessage).
			Set("error_details = ?", errorDetails).
			Set("retry_count = ?", newRetryCount).
			Set("started_at = NULL").
			Where("id = ?", jobID).
			Where("status = ?", JobStatusProcessing).
			Exec(ctx)
		if err != nil {
			return false, fmt.Errorf("schedule retry: %w", err)
		}
		if affected, err := res.RowsAffected(); err == nil && affected == 0 {
			s.log.Warn("ignored failure for a job that is no longer processing",
				slog.String("id", jobID))
			return false, nil
		}

		s.log.Info("object extraction job scheduled for retry",
			slog.String("id", jobID),
			slog.Int("retryCount", newRetryCount),
			slog.Int("maxRetries", job.MaxRetries))
		return false, nil
	}
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
	// Failed includes dead-lettered jobs; DeadLettered is the subset that
	// exhausted its retry budget.
	Failed       int64 `json:"failed"`
	DeadLettered int64 `json:"deadLettered"`
	StaleFailed  int64 `json:"staleFailed"`
	Cancelled    int64 `json:"cancelled"`
	Total        int64 `json:"total"`
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
		case JobStatusDeadLetter:
			// A dead letter is a failure that exhausted its retry budget. Keep
			// the subset separately so the UI can say "failed permanently".
			// It is folded into Failed after the loop: GROUP BY returns rows in
			// arbitrary order, so adding it here would be overwritten if the
			// failed group happened to come second.
			stats.DeadLettered = r.Count
		case JobStatusCancelled:
			stats.Cancelled = r.Count
		}
	}

	stats.Failed += stats.DeadLettered

	// Count rows terminal-failed by the stale-job sweep separately so genuine
	// failures and historical cleanup are not conflated.
	var staleCount int64
	staleQuery := s.db.NewSelect().
		Model((*ObjectExtractionJob)(nil)).
		ColumnExpr("COUNT(*)").
		Where("status = ?", JobStatusFailed).
		Where("error_message = ?", jobs.StaleJobMessage)
	if projectID != nil {
		staleQuery = staleQuery.Where("project_id = ?", *projectID)
	}
	if err := staleQuery.Scan(ctx, &staleCount); err != nil {
		return nil, fmt.Errorf("get stale stats: %w", err)
	}
	stats.StaleFailed = staleCount
	stats.Failed -= staleCount

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

// BulkRetryOptions controls which terminal failures a project-wide retry may
// re-run.
type BulkRetryOptions struct {
	// IncludeNonRetryable re-runs failures the classifier marks
	// retryable=false (a missing model, an unreadable document). It is set only
	// when a user explicitly confirms the cause is fixed — the panel's
	// "Retry all after fixing the cause" button — never by a plain sweep or a
	// scheduled retry, which would loop on a cause nobody addressed.
	IncludeNonRetryable bool
}

// BulkRetryFailed starts a new run for every retryable terminal failure in the
// project. It is BulkRetryFailedWithOptions with the default (safe) filter.
func (s *ObjectExtractionJobsService) BulkRetryFailed(ctx context.Context, projectID string) (int, error) {
	return s.BulkRetryFailedWithOptions(ctx, projectID, BulkRetryOptions{})
}

// BulkRetryFailedWithOptions starts a new run for the latest failed or
// dead-lettered job of every document/chunk in the project. A unit whose latest
// job completed or was cancelled is not retried, and units that already have an
// active job are skipped so a quick double click does not stack duplicate runs.
// Jobs without a document or chunk are retried individually, not by this
// project-wide action.
//
// Retryable-class failures only are re-run unless IncludeNonRetryable is set:
// re-driving a missing-model failure without the user asserting the fix just
// burns a fresh budget into the same dead letter.
func (s *ObjectExtractionJobsService) BulkRetryFailedWithOptions(ctx context.Context, projectID string, opts BulkRetryOptions) (int, error) {
	// One candidate per unit: only when the unit's most recent job is a
	// terminal failure.
	candidates := make([]*ObjectExtractionJob, 0)
	err := s.db.NewSelect().
		Model(&candidates).
		Where("project_id = ?", projectID).
		Where("status IN (?)", bun.In([]JobStatus{JobStatusFailed, JobStatusDeadLetter})).
		Where(`id IN (
			SELECT DISTINCT ON (COALESCE(document_id::text, chunk_id::text, id::text)) id
			FROM kb.object_extraction_jobs
			WHERE project_id = ? AND (document_id IS NOT NULL OR chunk_id IS NOT NULL)
			ORDER BY COALESCE(document_id::text, chunk_id::text, id::text), created_at DESC
		)`, projectID).
		Scan(ctx)
	if err != nil {
		return 0, fmt.Errorf("find retryable jobs: %w", err)
	}

	created := 0
	skipped := 0
	var firstErr error
	for _, job := range candidates {
		if !opts.IncludeNonRetryable && !retryableObjectExtraction(job) {
			skipped++
			continue
		}
		_, wasCreated, err := s.ReprocessJob(ctx, job.ID, projectID)
		if err != nil {
			// One unreprocessable job (e.g. the project has no model) must not
			// abort the rest of the batch; the caller reports the count that
			// did start.
			if firstErr == nil {
				firstErr = err
			}
			s.log.Warn("failed to reprocess extraction job",
				slog.String("jobId", job.ID),
				slog.String("projectId", projectID),
				logger.Error(err))
			continue
		}
		if wasCreated {
			created++
		}
	}

	s.log.Info("bulk reprocessed failed object extraction jobs",
		slog.String("projectId", projectID),
		slog.Int("count", created),
		slog.Int("skipped", skipped))

	// Every candidate was rejected: report the reason instead of letting the
	// UI claim a retry started. A partial success still returns the count and
	// leaves the failures visible in the panel.
	if created == 0 && firstErr != nil {
		return 0, fmt.Errorf("retry failed extractions: %w", firstErr)
	}
	return created, nil
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

// RetryJob retries a failed extraction job.
//
// A failed or dead-lettered job is terminal history: retrying it starts a new,
// linked run (reprocessing_of) with a fresh retry budget, so the failed attempt
// keeps its error message and trace. A stuck 'processing' job is requeued in
// place because there is no failed attempt worth preserving — but only once it
// is old enough to be stuck, which is the same threshold the stale-job sweep
// uses to terminal-fail it.
func (s *ObjectExtractionJobsService) RetryJob(ctx context.Context, jobID string, projectID string) (*ObjectExtractionJob, error) {
	job, err := s.FindByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("find job: %w", err)
	}
	if job == nil {
		return nil, fmt.Errorf("job not found")
	}
	if job.ProjectID != projectID {
		return nil, fmt.Errorf("job not found in project")
	}

	switch job.Status {
	case JobStatusProcessing:
		return s.requeueJob(ctx, job)
	case JobStatusFailed, JobStatusDeadLetter:
		next, _, err := s.ReprocessJob(ctx, jobID, projectID)
		return next, err
	default:
		return nil, fmt.Errorf("cannot retry job with status %s (only failed/dead_letter/processing jobs can be retried)", job.Status)
	}
}

// requeueJob resets a stuck processing job to pending with a fresh retry
// budget. It is only used for jobs that never reached a terminal state.
//
// A 'processing' job younger than the stale-job threshold is rejected with
// ErrJobStillRunning: its worker is probably still alive, and resetting it would
// run the unit twice. The staleness predicate is the sweep's
// (started_at IS NOT NULL AND started_at < now - threshold) so the two paths
// agree on what "stuck" means; the job is also guarded by that predicate in the
// UPDATE, so a run reclaimed between the read and the write cannot be requeued.
func (s *ObjectExtractionJobsService) requeueJob(ctx context.Context, job *ObjectExtractionJob) (*ObjectExtractionJob, error) {
	now := time.Now().UTC()
	staleMinutes := s.staleThresholdMinutes()
	cutoff := now.Add(-time.Duration(staleMinutes) * time.Minute)

	if job.StartedAt == nil {
		return nil, fmt.Errorf("%w: it has no start time recorded, and a run is only requeued after the %d-minute stale threshold", ErrJobStillRunning, staleMinutes)
	}
	if !job.StartedAt.Before(cutoff) {
		return nil, fmt.Errorf("%w: it started %s ago and is only requeued after the %d-minute stale threshold",
			ErrJobStillRunning, now.Sub(*job.StartedAt).Truncate(time.Minute), staleMinutes)
	}

	res, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("status = ?", JobStatusPending).
		Set("retry_count = 0").
		Set("error_message = NULL").
		Set("error_details = NULL").
		Set("started_at = NULL").
		Set("completed_at = NULL").
		Set("updated_at = ?", now).
		Where("id = ?", job.ID).
		Where("status = ?", JobStatusProcessing).
		Where("started_at IS NOT NULL AND started_at < ?", cutoff).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("retry job: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("retry job rows: %w", err)
	}
	if affected == 0 {
		// The worker finished the job between the read and this update. Do not
		// revive a completed/failed row: that would start a duplicate run and
		// erase its terminal state.
		return nil, fmt.Errorf("job is no longer processing; refresh to see its current state")
	}

	s.log.Info("requeued stuck object extraction job",
		slog.String("id", job.ID),
		slog.String("previousStatus", string(job.Status)),
		slog.Int("staleMinutes", staleMinutes))

	return s.FindByID(ctx, job.ID)
}

// ReprocessJob creates a new extraction job for a failed or dead-lettered one.
// The new job carries a full retry budget and points at the original through
// reprocessing_of, so the failure remains in the document's extraction history
// instead of being overwritten by the retry.
//
// The bool reports whether a new job was created. It is false when an active
// job already exists for the same unit; that job is returned so repeated
// retries are idempotent.
func (s *ObjectExtractionJobsService) ReprocessJob(ctx context.Context, jobID, projectID string) (*ObjectExtractionJob, bool, error) {
	old, err := s.FindByID(ctx, jobID)
	if err != nil {
		return nil, false, fmt.Errorf("find job: %w", err)
	}
	if old == nil || old.ProjectID != projectID {
		return nil, false, fmt.Errorf("job not found in project")
	}
	if old.Status != JobStatusFailed && old.Status != JobStatusDeadLetter {
		return nil, false, fmt.Errorf("cannot reprocess job with status %s (only failed/dead_letter jobs can be reprocessed)", old.Status)
	}

	if active, err := s.findActiveSibling(ctx, old); err != nil {
		return nil, false, err
	} else if active != nil {
		return active, false, nil
	}

	next, err := s.CreateJob(ctx, CreateObjectExtractionJobOptions{
		ProjectID:        old.ProjectID,
		DocumentID:       old.DocumentID,
		ChunkID:          old.ChunkID,
		JobType:          JobTypeReextraction,
		EnabledTypes:     old.EnabledTypes,
		ExtractionConfig: old.ExtractionConfig,
		SourceType:       old.SourceType,
		SourceID:         old.SourceID,
		SourceMetadata:   old.SourceMetadata,
		CreatedBy:        old.CreatedBy,
		ReprocessingOf:   &old.ID,
	})
	if err != nil {
		return nil, false, err
	}

	s.log.Info("reprocessed object extraction job",
		slog.String("previousJobId", old.ID),
		slog.String("newJobId", next.ID),
		slog.String("projectId", projectID))

	return next, true, nil
}

// findActiveSibling returns a pending or processing job for the same document
// or chunk, which is the guard against stacking duplicate runs on one unit.
func (s *ObjectExtractionJobsService) findActiveSibling(ctx context.Context, job *ObjectExtractionJob) (*ObjectExtractionJob, error) {
	if job.DocumentID == nil && job.ChunkID == nil {
		return nil, nil
	}

	active := new(ObjectExtractionJob)
	q := s.db.NewSelect().
		Model(active).
		Where("project_id = ?", job.ProjectID).
		Where("status IN (?)", bun.In([]JobStatus{JobStatusPending, JobStatusProcessing}))
	if job.DocumentID != nil {
		q = q.Where("document_id = ?", *job.DocumentID)
	}
	if job.ChunkID != nil {
		q = q.Where("chunk_id = ?", *job.ChunkID)
	}
	if err := q.Order("created_at DESC").Limit(1).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find active extraction job: %w", err)
	}
	return active, nil
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
	stats.JobsByStatus["dead_letter"] = basicStats.DeadLettered
	stats.JobsByStatus["staleFailed"] = basicStats.StaleFailed
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

// TriggerForDocument creates a full extraction job for the given document.
// This satisfies the documents.ExtractionJobCreator interface so the upload
// handler can trigger extraction without importing this package.
func (s *ObjectExtractionJobsService) TriggerForDocument(ctx context.Context, projectID, documentID string) error {
	_, err := s.CreateJob(ctx, CreateObjectExtractionJobOptions{
		ProjectID:  projectID,
		DocumentID: &documentID,
		SourceID:   &documentID,
		JobType:    JobTypeFullExtraction,
		SourceType: func() *string { v := "document"; return &v }(),
	})
	return err
}

// SetTraceID records the OpenTelemetry trace id on the job row. A write is
// skipped when traceID is empty (tracing disabled → NULL, no-op). Called by the
// worker right after the root span is started so the admin job-detail endpoint
// can later fetch the job's span tree from Tempo. Failures are non-fatal to the
// enclosing job (the caller logs and continues).
//
// The UPDATE has no "AND trace_id IS NULL" guard: on a retry the worker starts a
// fresh root span, so a later attempt overwrites the previous attempt's trace id.
// This is intentional — the detail view follows the latest attempt — not a bug.
func (s *ObjectExtractionJobsService) SetTraceID(ctx context.Context, jobID, traceID string) error {
	if traceID == "" {
		return nil
	}
	now := time.Now().UTC()
	_, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("trace_id = ?", traceID).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("set trace id: %w", err)
	}
	return nil
}

// UpdateStagingBranchID records the staging branch that was created for this
// extraction job. Called after the branch is successfully created so reviewers
// can locate pending extractions via the branch.
// SetRunID links the job to the agent run that executed it. Agent-run
// extraction gets a real executor run (not the synthetic pipeline transcript),
// so this is what makes the job debuggable as a chat session and lets the run
// page open the extraction it produced.
func (s *ObjectExtractionJobsService) SetRunID(ctx context.Context, jobID, runID string) error {
	_, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("run_id = ?", runID).
		Set("updated_at = now()").
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("link extraction run: %w", err)
	}
	return nil
}

func (s *ObjectExtractionJobsService) UpdateStagingBranchID(ctx context.Context, jobID string, branchID string) error {
	now := time.Now().UTC()
	_, err := s.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("staging_branch_id = ?", branchID).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update staging_branch_id: %w", err)
	}
	return nil
}

// GetProjectOrgID returns the organization ID for the given project.
// Used by background workers that need org context for usage tracking.
func (s *ObjectExtractionJobsService) GetProjectOrgID(ctx context.Context, projectID string) (string, error) {
	var orgID string
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		Column("organization_id").
		Where("id = ?", projectID).
		Scan(ctx, &orgID)
	if err != nil {
		return "", fmt.Errorf("get org id for project %s: %w", projectID, err)
	}
	return orgID, nil
}
