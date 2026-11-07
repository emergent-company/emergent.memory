// Package extraction provides object extraction job processing.
package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/branchorigin"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/runstatus"
	"github.com/emergent-company/emergent.memory/pkg/syshealth"
	"github.com/emergent-company/emergent.memory/pkg/textsplitter"
	"github.com/emergent-company/emergent.memory/pkg/tracing"
)

// ObjectExtractionWorkerConfig holds configuration for the extraction worker.
type ObjectExtractionWorkerConfig struct {
	// PollInterval is how often to check for new jobs. Default: 5 seconds.
	PollInterval time.Duration

	// Concurrency is the maximum number of jobs to process concurrently
	Concurrency int

	// OrphanThreshold is the max acceptable orphan rate (0.0-1.0). Default: 0.3.
	OrphanThreshold float64

	// MaxRetries is the max number of relationship extraction retries. Default: 3.
	MaxRetries uint

	// SkipStagingBranch disables per-job staging branches.
	// When true, extracted objects are written directly to the main graph.
	SkipStagingBranch bool
}

// DefaultObjectExtractionWorkerConfig returns default worker configuration.
func DefaultObjectExtractionWorkerConfig() *ObjectExtractionWorkerConfig {
	return &ObjectExtractionWorkerConfig{
		PollInterval:    5 * time.Second,
		Concurrency:     5,
		OrphanThreshold: 0.3,
		MaxRetries:      3,
	}
}

// PipelineInputModifier is an optional hook called just before each batch is
// passed to the extraction pipeline. It receives the input and returns a
// (possibly modified) copy. Used in tests to inject TypeHints or NegativeExamples
// without changing production code paths.
type PipelineInputModifier func(agents.ExtractionPipelineInput) agents.ExtractionPipelineInput

// SchemaProvider provides object and relationship schemas for extraction.
// This interface allows different implementations (e.g., from database, config, etc.).
type SchemaProvider interface {
	// GetProjectSchemas returns object and relationship schemas for a project.
	GetProjectSchemas(ctx context.Context, projectID string) (*ExtractionSchemas, error)
	// GetInstalledSchemaSummaries returns lightweight schema-pack summaries for domain classification.
	// Implementations may return nil without error if classification is not supported.
	GetInstalledSchemaSummaries(ctx context.Context, projectID string) ([]InstalledSchemaSummary, error)
}

// ExtractionSchemas holds the schemas needed for extraction.
type ExtractionSchemas struct {
	ObjectSchemas       map[string]agents.ObjectSchema
	RelationshipSchemas map[string]agents.RelationshipSchema
}

// ObjectExtractionWorker processes object extraction jobs.
type ObjectExtractionWorker struct {
	jobsService           *ObjectExtractionJobsService
	graphService          *graph.Service
	branchService         *branches.Service
	docService            *documents.Service
	projectsRepo          *projects.Repository
	schemaProvider        SchemaProvider
	modelFactory          *adk.ModelFactory
	classifier            *DocumentClassifier
	limitResolver         adk.ModelLimitResolver // optional; nil → no truncation
	pipelineInputModifier PipelineInputModifier  // optional; nil → no modification
	notificationsSvc      projectNotifier        // optional; nil → no review-ready/lifecycle notifications
	runRecorder           *RunRecorder           // optional; nil → extraction transcripts are not recorded
	config                *ObjectExtractionWorkerConfig
	log                   *slog.Logger
	scaler                *syshealth.ConcurrencyScaler

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewObjectExtractionWorker creates a new extraction worker.
func NewObjectExtractionWorker(
	jobsService *ObjectExtractionJobsService,
	graphService *graph.Service,
	branchService *branches.Service,
	docService *documents.Service,
	projectsRepo *projects.Repository,
	schemaProvider SchemaProvider,
	modelFactory *adk.ModelFactory,
	embeddingService EmbeddingService,
	config *ObjectExtractionWorkerConfig,
	log *slog.Logger,
	scaler *syshealth.ConcurrencyScaler,
) *ObjectExtractionWorker {
	if config == nil {
		config = DefaultObjectExtractionWorkerConfig()
	}
	return &ObjectExtractionWorker{
		jobsService:    jobsService,
		graphService:   graphService,
		branchService:  branchService,
		docService:     docService,
		projectsRepo:   projectsRepo,
		schemaProvider: schemaProvider,
		modelFactory:   modelFactory,
		classifier:     NewDocumentClassifier(modelFactory, embeddingService, log),
		config:         config,
		log:            log.With(logger.Scope("object-extraction-worker")),
		scaler:         scaler,
		stopCh:         make(chan struct{}),
	}
}

// WithLimitResolver sets an optional ModelLimitResolver on the worker.
// When set, document text is truncated to the model's max_input_tokens before
// being sent to the extraction pipeline.
func (w *ObjectExtractionWorker) WithLimitResolver(r adk.ModelLimitResolver) *ObjectExtractionWorker {
	w.limitResolver = r
	return w
}

// SetPipelineInputModifier sets an optional hook that mutates ExtractionPipelineInput
// just before each batch is processed. Intended for testing only — allows injecting
// TypeHints, NegativeExamples, or other fields without wiring the full classifier.
func (w *ObjectExtractionWorker) SetPipelineInputModifier(m PipelineInputModifier) {
	w.pipelineInputModifier = m
}

// WithNotifications sets the notifications service used to emit review-ready
// alerts after extraction completes. Nil (the default) disables notifications,
// so callers that never wire it — including tests — are unaffected.
func (w *ObjectExtractionWorker) WithNotifications(svc *notifications.Service) *ObjectExtractionWorker {
	// Assign only a real service: storing a typed nil in the interface field
	// would make the `== nil` guard below pass and then panic on use.
	if svc == nil {
		w.notificationsSvc = nil
		return w
	}
	w.notificationsSvc = svc
	return w
}

// WithRunRecorder sets the recorder that persists each job's pipeline transcript
// as an agent run, so an extraction is debuggable as a chat session (the chat
// workspace opens the run and renders its messages). Nil (the default) disables
// recording, which every test and any host without the agents surface can do
// without behaviour change beyond the missing transcript.
func (w *ObjectExtractionWorker) WithRunRecorder(r *RunRecorder) *ObjectExtractionWorker {
	w.runRecorder = r
	return w
}

// startExtractionRun opens the job's transcript run and records the header
// message. Failures degrade to "no transcript": the job itself is unaffected.
func (w *ObjectExtractionWorker) startExtractionRun(ctx context.Context, job *ObjectExtractionJob) (string, string) {
	if w.runRecorder == nil {
		return "", ""
	}
	runID, err := w.runRecorder.StartRun(ctx, job)
	if err != nil {
		w.log.Warn("failed to open extraction transcript run, continuing without it",
			slog.String("job_id", job.ID),
			logger.Error(err))
		return "", ""
	}
	docID := "manual input"
	if job.DocumentID != nil && *job.DocumentID != "" {
		docID = "document " + *job.DocumentID
	}
	header := fmt.Sprintf("Extraction of %s · job `%s`", docID, job.ID)
	return runID, header
}

// finishExtractionRun closes the transcript run with the job's terminal status
// and, on failure, appends the error as a final message so an opened transcript
// explains itself. Uses a non-cancelled context: the worker's context may be
// dying at shutdown and the terminal status still has to be written.
func (w *ObjectExtractionWorker) finishExtractionRun(ctx context.Context, runID, errMessage string) {
	if w.runRecorder == nil || runID == "" {
		return
	}
	status := string(runstatus.Success)
	if errMessage != "" {
		status = string(runstatus.Error)
	}
	if err := w.runRecorder.FinishRun(ctx, runID, status, errMessage); err != nil {
		w.log.Warn("failed to close extraction transcript run",
			slog.String("run_id", runID),
			logger.Error(err))
	}
}

// Start begins processing jobs in the background.
func (w *ObjectExtractionWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.run(ctx)
	w.log.Info("object extraction worker started",
		slog.Duration("poll_interval", w.config.PollInterval))
}

// Stop gracefully stops the worker.
func (w *ObjectExtractionWorker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.log.Info("object extraction worker stopped")
}

// run is the main worker loop.
func (w *ObjectExtractionWorker) run(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			// Just fetch jobs and launch goroutines up to concurrency
			jobs, err := w.jobsService.DequeueBatch(ctx, w.config.Concurrency)
			if err != nil {
				w.log.Error("error dequeuing jobs", logger.Error(err))
				continue
			}
			if len(jobs) == 0 {
				continue
			}

			w.log.Info("processing extraction jobs", slog.Int("count", len(jobs)))

			concurrency := w.config.Concurrency
			if w.scaler != nil {
				concurrency = w.scaler.GetConcurrency(w.config.Concurrency)
			}
			if concurrency <= 0 {
				concurrency = 5
			}
			sem := make(chan struct{}, concurrency)
			var batchWg sync.WaitGroup

			for _, job := range jobs {
				sem <- struct{}{}
				batchWg.Add(1)
				go func(j *ObjectExtractionJob) {
					defer batchWg.Done()
					defer func() { <-sem }()
					if err := w.processSingleJob(ctx, j); err != nil {
						w.log.Error("job failed",
							slog.String("job_id", j.ID),
							logger.Error(err))
					}
				}(job)
			}
			batchWg.Wait()
		}
	}
}

// processSingleJob processes a single job after it's dequeued
// ProcessJobSync processes a single extraction job synchronously.
// Used by tests to run extraction in-process without the background poll loop.
func (w *ObjectExtractionWorker) ProcessJobSync(ctx context.Context, job *ObjectExtractionJob) (*ObjectExtractionResults, error) {
	return w.processJob(ctx, job)
}

func (w *ObjectExtractionWorker) processSingleJob(ctx context.Context, job *ObjectExtractionJob) error {
	docID := ""
	if job.DocumentID != nil {
		docID = *job.DocumentID
	}
	ctx, span := tracing.Start(ctx, "extraction.object_extraction",
		attribute.String("memory.job.id", job.ID),
		attribute.String("memory.project.id", job.ProjectID),
		attribute.String("memory.document.id", docID),
	)
	defer span.End()

	// Persist the trace id so the admin job-detail endpoint can fetch the span
	// tree from Tempo. With tracing disabled the span context is invalid and the
	// write is a no-op (NULL). Failure here must never fail the job.
	if sc := span.SpanContext(); sc.IsValid() && sc.TraceID().IsValid() {
		traceID := sc.TraceID().String()
		if err := w.jobsService.SetTraceID(ctx, job.ID, traceID); err != nil {
			w.log.Warn("failed to persist trace id on extraction job",
				slog.String("job_id", job.ID),
				logger.Error(err))
		} else if job.TraceID == nil {
			job.TraceID = &traceID
		}
	}

	// Inject project ID into context so the credential resolver can look up
	// per-project LLM provider configuration (e.g. Vertex AI credentials).
	if job.ProjectID != "" {
		ctx = auth.ContextWithProjectID(ctx, job.ProjectID)
		// Also inject org ID so usage tracking can record cost against the correct org.
		if orgID, err := w.jobsService.GetProjectOrgID(ctx, job.ProjectID); err == nil && orgID != "" {
			ctx = auth.ContextWithOrgID(ctx, orgID)
		}
	}

	w.log.Info("processing extraction job",
		slog.String("job_id", job.ID),
		slog.String("project_id", job.ProjectID),
		slog.String("job_type", string(job.JobType)))

	// Process the job
	result, err := w.processJob(ctx, job)
	if err != nil {
		w.log.Error("job failed",
			slog.String("job_id", job.ID),
			logger.Error(err))

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		details := JSON{"error_type": "processing_error"}
		if w.modelFactory != nil {
			if name := w.modelFactory.ModelName(); name != "" {
				details["model"] = name
			}
		}
		var batchErr *extractionBatchError
		if errors.As(err, &batchErr) {
			details["batch"] = batchErr.Batch
			details["total_batches"] = batchErr.Total
		}
		if job.TraceID != nil {
			details["trace_id"] = *job.TraceID
		}

		deadLettered, markErr := w.jobsService.MarkFailed(ctx, job.ID, err.Error(), details)
		if markErr != nil {
			return markErr
		}
		// Only the final failure is announced: a retry is an internal detail and
		// announcing each attempt would notify the same people up to
		// max_retries times for one run.
		if deadLettered {
			w.emitProcessingFailedNotification(ctx, job, err)
		}
		return nil
	}

	span.SetAttributes(
		attribute.Int("memory.extraction.entity_count", result.ObjectsCreated),
		attribute.Int("memory.extraction.relationship_count", result.RelationshipsCreated),
	)
	span.SetStatus(codes.Ok, "")

	// Mark completed
	return w.jobsService.MarkCompleted(ctx, job.ID, *result)
}

// extractionBatchError carries the failing batch position so the job's
// error_details can explain where extraction stopped.
type extractionBatchError struct {
	Batch int
	Total int
	Err   error
}

func (e *extractionBatchError) Error() string {
	return fmt.Sprintf("run pipeline (batch %d/%d): %v", e.Batch, e.Total, e.Err)
}

func (e *extractionBatchError) Unwrap() error { return e.Err }

// processJob processes a single extraction job.
func (w *ObjectExtractionWorker) processJob(ctx context.Context, job *ObjectExtractionJob) (result *ObjectExtractionResults, err error) {
	// Open the job's transcript run first, so even a job that fails while loading
	// its document leaves a run with a failure message rather than nothing.
	runID, transcriptHeader := w.startExtractionRun(ctx, job)
	if runID != "" {
		defer func() {
			finishCtx := context.WithoutCancel(ctx)
			errMessage := ""
			if err != nil {
				errMessage = err.Error()
				// A high step keeps the reason last even when the message shares a
				// timestamp with the last recorded stage (the timeline sorts by
				// time, then step).
				if recErr := w.runRecorder.RecordMessage(finishCtx, runID, RunMessage{
					Role:      "assistant",
					Step:      failureMessageStep,
					Text:      err.Error(),
					Stage:     extractionRunAgentName,
					StageKind: stageKindError,
				}); recErr != nil {
					w.log.Warn("failed to record extraction failure message",
						slog.String("run_id", runID),
						logger.Error(recErr))
				}
			}
			w.finishExtractionRun(finishCtx, runID, errMessage)
		}()
	}

	// Load document text
	documentText, err := w.loadDocumentText(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("load document text: %w", err)
	}

	if documentText == "" {
		return nil, fmt.Errorf("no document text to extract from")
	}

	// Split into extraction-sized batches that fit the model context window.
	// Each batch is a semantically-split window (paragraph → sentence → word
	// boundaries) large enough for good relationship detection (~4000 tokens)
	// with 20% overlap to capture entities that span chunk boundaries.
	batches := w.splitIntoExtractionBatches(ctx, documentText)

	// Load schemas
	schemas, err := w.loadSchemas(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("load schemas: %w", err)
	}

	// Classify document domain for schema-guided extraction.
	var classificationResult ClassificationResult
	if w.classifier != nil && w.schemaProvider != nil {
		summaries, sumErr := w.schemaProvider.GetInstalledSchemaSummaries(ctx, job.ProjectID)
		if sumErr != nil {
			w.log.Warn("failed to load schema summaries for classification, continuing without domain guidance",
				logger.Error(sumErr))
		} else if len(summaries) == 0 {
			// No schemas installed — do NOT write domain_name here; leave it NULL so
			// finalize-discovery (or a future classify-document call after schemas are installed)
			// can set the correct value without racing.
			w.log.Info("no schemas installed, skipping domain classification write")
		} else {
			cr, classErr := w.classifier.Classify(ctx, batches[0], summaries)
			if classErr != nil {
				w.log.Warn("document classification failed, continuing without domain guidance",
					logger.Error(classErr))
			} else {
				classificationResult = cr
				// Write domain fields back to document asynchronously (best-effort).
				// Only write when we have a real schema match (DomainName != "").
				// Do NOT write "new_domain" placeholder — leave domain_name NULL so
				// finalize-discovery owns that transition and there is no race condition.
				if job.DocumentID != nil && w.docService != nil {
					if cr.DomainName != "" {
						w.log.Info("document classified",
							slog.String("domain", cr.DomainName),
							slog.Float64("confidence", float64(cr.Confidence)),
						)
						go w.writeDomainClassification(ctx, *job.DocumentID, cr)
					} else {
						w.log.Info("document classified as new domain (no schema match), skipping domain_name write")
					}
				}
			}
		}
	}

	// Create a staging branch so extracted objects are isolated from the main
	// graph until a reviewer explicitly merges them.
	var stagingBranchID *uuid.UUID
	if w.branchService != nil && !w.config.SkipStagingBranch {
		branch, err := w.branchService.Create(ctx,
			extractionBranchRequest(job, w.stagingBranchName(job), w.stagingBranchLabel(ctx, job)))
		if err != nil {
			// Non-fatal: fall back to writing on main graph rather than blocking extraction.
			w.log.Warn("failed to create staging branch, writing to main graph",
				slog.String("job_id", job.ID),
				logger.Error(err))
		} else {
			id, parseErr := uuid.Parse(branch.ID)
			if parseErr == nil {
				stagingBranchID = &id
				// Persist branch reference on the job immediately so it's visible
				// even if a crash occurs mid-extraction.
				if updateErr := w.jobsService.UpdateStagingBranchID(ctx, job.ID, branch.ID); updateErr != nil {
					w.log.Warn("failed to record staging_branch_id on job",
						slog.String("job_id", job.ID),
						logger.Error(updateErr))
				}
			}
		}
	}

	var traceLogger agents.TraceLogger
	documentID := ""
	if job.DocumentID != nil {
		documentID = *job.DocumentID
	}
	if tl, err := agents.NewExtractionTraceLogger(agents.TraceLoggerConfig{
		JobID:      job.ID,
		DocumentID: documentID,
		ProjectID:  job.ProjectID,
	}); err != nil {
		w.log.Warn("failed to create trace logger, continuing without tracing",
			slog.String("error", err.Error()),
		)
		traceLogger = &agents.NullTraceLogger{}
	} else {
		traceLogger = tl
		defer tl.Close()
	}
	// Bridge the pipeline's stage callbacks to the job's run so the prompts and
	// model responses render as a chat transcript. The file logger above stays
	// the full-fidelity record; the transcript stores the prompt and answer
	// whole (only a runaway-output guard cuts one).
	if runID != "" {
		traceLogger = newRunTraceLogger(w.runRecorder, runID, traceLogger, transcriptHeader, w.log)
	}

	// Create extraction pipeline (shared across all batches).
	pipeline, err := agents.NewExtractionPipeline(agents.ExtractionPipelineConfig{
		ModelFactory:        w.modelFactory,
		ObjectSchemas:       schemas.ObjectSchemas,
		RelationshipSchemas: schemas.RelationshipSchemas,
		OrphanThreshold:     w.config.OrphanThreshold,
		MaxRetries:          w.config.MaxRetries,
		Logger:              w.log,
		TraceLogger:         traceLogger,
	})
	if err != nil {
		// Pipeline creation failed — delete the (empty) staging branch to avoid clutter.
		w.deleteStagingBranch(ctx, job, stagingBranchID)
		return nil, fmt.Errorf("create pipeline: %w", err)
	}

	// Run extraction over each batch, accumulating results.
	// persistResults upserts by (project_id, type, name) so duplicate entities
	// across overlapping batches are merged naturally.

	// Resolve the source document's version once per job (not per batch) so the
	// provenance stamp is consistent across all batches of this job.
	documentVersion := w.resolveDocumentVersion(ctx, job)

	var totalResult *ObjectExtractionResults
	for i, batch := range batches {
		if len(batches) > 1 {
			w.log.Info("processing extraction batch",
				slog.Int("batch", i+1),
				slog.Int("total_batches", len(batches)),
				slog.Int("chars", len(batch)),
			)
		}

		pipelineInput := agents.ExtractionPipelineInput{
			DocumentText:        batch,
			ObjectSchemas:       schemas.ObjectSchemas,
			RelationshipSchemas: schemas.RelationshipSchemas,
			AllowedTypes:        job.EnabledTypes,
			DomainGuidance:      classificationResult.DomainGuidance,
		}
		if w.pipelineInputModifier != nil {
			pipelineInput = w.pipelineInputModifier(pipelineInput)
		}
		pipelineOutput, err := pipeline.Run(ctx, pipelineInput)
		if err != nil {
			w.deleteStagingBranch(ctx, job, stagingBranchID)
			return nil, &extractionBatchError{Batch: i + 1, Total: len(batches), Err: err}
		}

		result, err := w.persistResults(ctx, job, pipelineOutput, stagingBranchID, documentVersion)
		if err != nil {
			w.deleteStagingBranch(ctx, job, stagingBranchID)
			return nil, fmt.Errorf("persist results (batch %d/%d): %w", i+1, len(batches), err)
		}

		if totalResult == nil {
			totalResult = result
		} else {
			totalResult.ObjectsCreated += result.ObjectsCreated
			totalResult.RelationshipsCreated += result.RelationshipsCreated
			totalResult.CreatedObjectIDs = append(totalResult.CreatedObjectIDs, result.CreatedObjectIDs...)
		}
	}

	if totalResult == nil {
		totalResult = &ObjectExtractionResults{}
	}

	// Auto-merge the staging branch into main when the project opts in.
	// Best-effort: failures are logged and the branch is left for manual review.
	if stagingBranchID != nil && w.autoMergeEnabled(ctx, job.ProjectID) {
		w.autoMergeExtractionBranch(ctx, job, *stagingBranchID)
	}

	w.emitExtractionOutcome(ctx, job, stagingBranchID, totalResult)

	return totalResult, nil
}

// autoMergeEnabled reports whether the project has auto-merge enabled.
func (w *ObjectExtractionWorker) autoMergeEnabled(ctx context.Context, projectID string) bool {
	if w.projectsRepo == nil {
		return false
	}
	project, err := w.projectsRepo.GetByID(ctx, projectID, false)
	if err != nil {
		w.log.Warn("could not read project auto-merge setting", logger.Error(err))
		return false
	}
	return project != nil && project.AutoMergeExtractionBranches
}

// autoMergeExtractionBranch waits for the staging branch to be fully embedded,
// then performs a partial merge into the main graph: clean additions and
// fast-forwards are applied, while conflicting/similar objects are left on the
// branch for human review. It is best-effort — failures never block extraction.
func (w *ObjectExtractionWorker) autoMergeExtractionBranch(ctx context.Context, job *ObjectExtractionJob, branchID uuid.UUID) {
	projectID, err := uuid.Parse(job.ProjectID)
	if err != nil {
		w.log.Warn("auto-merge skipped: invalid project id",
			slog.String("job_id", job.ID), logger.Error(err))
		return
	}

	if err := w.waitForEmbeddings(ctx, projectID, branchID); err != nil {
		w.log.Warn("auto-merge skipped: embeddings not settled",
			slog.String("job_id", job.ID),
			slog.String("branch_id", branchID.String()),
			logger.Error(err))
		return
	}

	req := &graph.BranchMergeRequest{
		SourceBranchID:    branchID,
		Execute:           true,
		Policy:            "partial",
		WaitForEmbeddings: true,
	}
	result, err := w.graphService.MergeBranch(ctx, projectID, nil, req)
	if err != nil {
		w.log.Warn("auto-merge failed, branch left for manual merge",
			slog.String("job_id", job.ID),
			slog.String("branch_id", branchID.String()),
			logger.Error(err))
		return
	}

	w.log.Info("extraction branch auto-merged",
		slog.String("job_id", job.ID),
		slog.String("branch_id", branchID.String()),
		slog.Int("added", result.AddedCount),
		slog.Int("fast_forward", result.FastForwardCount),
		slog.Int("conflicts", result.ConflictCount),
		slog.Int("similar", result.SimilarCount),
		slog.Int("left_for_review", result.LeftForReviewCount))
}

// waitForEmbeddings polls merge readiness until every object on the branch has
// an embedding (or a timeout elapses), so similarity classification has full
// coverage before the partial merge runs.
func (w *ObjectExtractionWorker) waitForEmbeddings(ctx context.Context, projectID, branchID uuid.UUID) error {
	const (
		timeout  = 2 * time.Minute
		interval = 5 * time.Second
	)
	deadline := time.Now().Add(timeout)
	for {
		_, pending, err := w.graphService.BranchMergeReadiness(ctx, projectID, branchID)
		if err != nil {
			return fmt.Errorf("readiness check: %w", err)
		}
		if pending == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %d embeddings", pending)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// truncateToInputLimit caps documentText to the model's max_input_tokens.
// Uses a 4 chars-per-token estimate and reserves 20% for system prompts and
// schema descriptions. If the limit is unknown (0) or the resolver is nil,
// the text is returned unchanged.
// splitIntoExtractionBatches splits documentText into context-window-sized
// batches ready to pass to the extraction pipeline.
//
// Algorithm:
//  1. Split text using extraction-tuned config (16K chars / 3.2K overlap) —
//     respects paragraph → sentence → word boundaries, never cuts mid-word.
//  2. Compute maxBatchChars from the model's input limit (80% of token budget
//     at 4 chars/token to leave headroom for system prompt + schemas).
//  3. Greedily pack consecutive extraction chunks (joined with "\n\n") into
//     batches until the next chunk would exceed maxBatchChars.
//
// If limitResolver is nil or returns 0, all chunks are packed into one batch
// (equivalent to the previous single-shot behaviour).
func (w *ObjectExtractionWorker) splitIntoExtractionBatches(ctx context.Context, text string) []string {
	// Step 1: split into extraction-sized semantic chunks.
	chunks := textsplitter.Split(text, textsplitter.ExtractionConfig())
	if len(chunks) == 0 {
		return []string{text}
	}

	// Step 2: determine per-batch character budget.
	const (
		charsPerToken  = 4
		promptOverhead = 0.20
	)
	maxBatchChars := 0
	if w.limitResolver != nil {
		inputLimit, err := w.limitResolver.GetInputLimit(ctx)
		if err != nil {
			w.log.Warn("could not resolve model input limit, using single batch", logger.Error(err))
		} else if inputLimit > 0 {
			maxBatchChars = int(float64(inputLimit) * (1 - promptOverhead) * charsPerToken)
		}
	}

	// Step 3: greedy packing.
	// If no limit known, one batch = all chunks.
	if maxBatchChars <= 0 {
		return []string{strings.Join(chunks, "\n\n")}
	}

	var batches []string
	var current strings.Builder
	for _, chunk := range chunks {
		// +2 for the "\n\n" separator we'd prepend
		needed := len(chunk)
		if current.Len() > 0 {
			needed += 2
		}
		if current.Len() > 0 && current.Len()+needed > maxBatchChars {
			batches = append(batches, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(chunk)
	}
	if current.Len() > 0 {
		batches = append(batches, current.String())
	}

	if len(batches) > 1 {
		w.log.Info("document split into extraction batches",
			slog.Int("batches", len(batches)),
			slog.Int("total_chars", len(text)),
			slog.Int("max_batch_chars", maxBatchChars),
		)
	}
	return batches
}

// loadDocumentText loads the text content for extraction.
func (w *ObjectExtractionWorker) loadDocumentText(ctx context.Context, job *ObjectExtractionJob) (string, error) {
	// Check source type
	sourceType := ""
	if job.SourceType != nil {
		sourceType = *job.SourceType
	}

	switch sourceType {
	case "manual":
		// Get text from source metadata
		if job.SourceMetadata != nil {
			if text, ok := job.SourceMetadata["text"].(string); ok {
				return text, nil
			}
		}
		return "", fmt.Errorf("manual source type requires text in source_metadata")

	case "document", "":
		// Load from document
		if job.DocumentID == nil {
			return "", fmt.Errorf("document source type requires document_id")
		}
		doc, err := w.docService.GetContentByID(ctx, job.ProjectID, *job.DocumentID)
		if err != nil {
			return "", fmt.Errorf("get document: %w", err)
		}
		if doc.Content == nil || *doc.Content == "" {
			return "", fmt.Errorf("document has no content")
		}
		return *doc.Content, nil

	default:
		return "", fmt.Errorf("unsupported source type: %s", sourceType)
	}
}

// loadSchemas loads object and relationship schemas for the project.
// Inline schemas from job.ExtractionConfig take precedence over installed schemas.
func (w *ObjectExtractionWorker) loadSchemas(ctx context.Context, job *ObjectExtractionJob) (*ExtractionSchemas, error) {
	schemas := &ExtractionSchemas{
		ObjectSchemas:       make(map[string]agents.ObjectSchema),
		RelationshipSchemas: make(map[string]agents.RelationshipSchema),
	}

	// Load installed project schemas first (base layer)
	if w.schemaProvider != nil {
		installed, err := w.schemaProvider.GetProjectSchemas(ctx, job.ProjectID)
		if err != nil {
			return nil, err
		}
		if installed != nil {
			for k, v := range installed.ObjectSchemas {
				schemas.ObjectSchemas[k] = v
			}
			for k, v := range installed.RelationshipSchemas {
				schemas.RelationshipSchemas[k] = v
			}
		}
	}

	// Merge inline schemas from extraction_config (override installed schemas)
	if job.ExtractionConfig != nil {
		if objSchemas, ok := job.ExtractionConfig["object_schemas"].(map[string]any); ok {
			for name, schema := range objSchemas {
				if schemaMap, ok := schema.(map[string]any); ok {
					schemas.ObjectSchemas[name] = convertToObjectSchema(schemaMap)
				}
			}
		}
		if relSchemas, ok := job.ExtractionConfig["relationship_schemas"].(map[string]any); ok {
			for name, schema := range relSchemas {
				if schemaMap, ok := schema.(map[string]any); ok {
					schemas.RelationshipSchemas[name] = convertToRelationshipSchema(schemaMap)
				}
			}
		}
	}

	// Per-type operational flag (P4.2): board-enabled / operational types marked
	// skipExtraction are never produced by the extraction pipeline. Explicit
	// inline schemas (converted above) carry no flag, so they are not skipped.
	for name, schema := range schemas.ObjectSchemas {
		if schema.SkipExtraction {
			delete(schemas.ObjectSchemas, name)
		}
	}

	return schemas, nil
}

// persistResults creates graph objects and relationships from extraction results.
// stagingBranchID, when non-nil, routes all created objects to the staging branch
// so they are invisible to main-graph searches until merged by a reviewer.
func (w *ObjectExtractionWorker) persistResults(
	ctx context.Context,
	job *ObjectExtractionJob,
	output *agents.ExtractionPipelineOutput,
	stagingBranchID *uuid.UUID,
	documentVersion int,
) (*ObjectExtractionResults, error) {
	projectID, err := uuid.Parse(job.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project_id: %w", err)
	}

	jobUUID, err := uuid.Parse(job.ID)
	if err != nil {
		return nil, fmt.Errorf("parse job id: %w", err)
	}

	// Extraction writes are system-originated: stamp the system actor so the
	// objects/relationships this job persists are attributed to `system` (NULL
	// actor_id) rather than any HTTP principal (issue #1193).
	ctx = auth.WithActor(ctx, graph.ActorSystem, nil)

	// Map temp_id -> created object ID
	tempIDToObjectID := make(map[string]uuid.UUID)
	createdObjectIDs := make([]string, 0)

	// Create graph objects, deduplicating by (project_id, type, name).
	// CreateOrUpdate upserts by key so that duplicate entities extracted from
	// overlapping batches (or repeated extraction jobs on the same document)
	// merge into a single graph object instead of creating duplicate rows.
	// objectsCreated counts only genuinely new graph rows (created=true).
	objectsCreated := 0
	objectsMerged := 0
	for _, entity := range output.Entities {
		properties := map[string]any{
			"name":        entity.Name,
			"description": entity.Description,
		}
		for k, v := range entity.Properties {
			properties[k] = v
		}

		// Add extraction metadata
		properties["_extraction_job_id"] = job.ID
		if isDocumentSource(job) {
			// Provenance is only meaningful for document-sourced extractions.
			// `_source_id` seeds the legacy scalar on merge, while `_sources`
			// stamps this job as the initial multi-source entry (with the source
			// document's version) so later merges union sources across documents
			// rather than overwrite a single scalar. All three keys are gated
			// together so a chunk/manual job never leaves a bogus scalar that a
			// later merge would backfill into `_sources`.
			properties["_source_id"] = *job.SourceID
			properties["_sources"] = buildSourcesArray(job, documentVersion)
			if documentVersion > 0 {
				properties["_document_version"] = documentVersion
			}
		}
		if job.SourceType != nil {
			properties["_extraction_source"] = *job.SourceType
		}

		key := strings.TrimSpace(entity.Name)
		if key != "" {
			// Upsert: merge into existing object when (project_id, type, key) matches.
			graphObj, created, err := w.graphService.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
				Type:            entity.Type,
				Key:             &key,
				Properties:      properties,
				Status:          stringPtr("suggested"),
				BranchID:        stagingBranchID,
				ExtractionJobID: &jobUUID,
			}, nil)
			if err != nil {
				w.log.Warn("failed to persist graph object",
					slog.String("name", entity.Name),
					slog.String("type", entity.Type),
					logger.Error(err))
				continue
			}
			tempIDToObjectID[entity.TempID] = graphObj.ID
			createdObjectIDs = append(createdObjectIDs, graphObj.ID.String())
			if created {
				objectsCreated++
			} else {
				objectsMerged++
			}
		} else {
			// No name → cannot key for dedup, fall back to plain Create.
			graphObj, err := w.graphService.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
				Type:            entity.Type,
				Properties:      properties,
				Status:          stringPtr("suggested"),
				BranchID:        stagingBranchID,
				ExtractionJobID: &jobUUID,
			}, nil)
			if err != nil {
				w.log.Warn("failed to create graph object (no name)",
					slog.String("type", entity.Type),
					logger.Error(err))
				continue
			}
			tempIDToObjectID[entity.TempID] = graphObj.ID
			createdObjectIDs = append(createdObjectIDs, graphObj.ID.String())
			objectsCreated++
		}
	}

	// Create relationships
	relationshipsCreated := 0
	for _, rel := range output.Relationships {
		srcID, srcOK := tempIDToObjectID[rel.SourceRef]
		dstID, dstOK := tempIDToObjectID[rel.TargetRef]

		if !srcOK || !dstOK {
			w.log.Warn("relationship references unknown temp_id",
				slog.String("source_ref", rel.SourceRef),
				slog.String("target_ref", rel.TargetRef),
				slog.Bool("src_found", srcOK),
				slog.Bool("dst_found", dstOK))
			continue
		}

		properties := map[string]any{
			"description":        rel.Description,
			"_extraction_job_id": job.ID,
		}

		_, err := w.graphService.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
			Type:       rel.Type,
			SrcID:      srcID,
			DstID:      dstID,
			Properties: properties,
			BranchID:   stagingBranchID,
		}, nil)
		if err != nil {
			w.log.Warn("failed to create relationship",
				slog.String("type", rel.Type),
				logger.Error(err))
			continue
		}
		relationshipsCreated++
	}

	// Calculate discovered types
	discoveredTypes := make([]any, 0)
	typeSet := make(map[string]bool)
	for _, entity := range output.Entities {
		if !typeSet[entity.Type] {
			typeSet[entity.Type] = true
			discoveredTypes = append(discoveredTypes, entity.Type)
		}
	}

	successfulItems := objectsCreated + objectsMerged
	return &ObjectExtractionResults{
		ObjectsCreated:       objectsCreated,
		ObjectsMerged:        objectsMerged,
		RelationshipsCreated: relationshipsCreated,
		TotalItems:           len(output.Entities),
		ProcessedItems:       len(output.Entities),
		SuccessfulItems:      successfulItems,
		FailedItems:          len(output.Entities) - successfulItems,
		DiscoveredTypes:      discoveredTypes,
		CreatedObjectIDs:     createdObjectIDs,
		DebugInfo: JSON{
			"entity_count":       len(output.Entities),
			"objects_created":    objectsCreated,
			"objects_merged":     objectsMerged,
			"relationship_count": len(output.Relationships),
			"orphan_rate":        agents.CalculateOrphanRate(output.Entities, output.Relationships),
		},
	}, nil
}

// extractionBranchRequest builds the staging-branch create request for a job.
// Isolated as a pure function so the attribution — the extraction origin, the
// job id and the source document's label — is testable without a database: the
// UI labels this branch with its source document rather than the opaque
// extraction/<doc>/<job> name (#242), and the id is what cleanup uses to find a
// deleted document's branches (#246).
func extractionBranchRequest(job *ObjectExtractionJob, name string, label *string) *branches.CreateBranchRequest {
	desc := fmt.Sprintf("Extraction job %s", job.ID)
	if job.DocumentID != nil {
		desc = fmt.Sprintf("Extraction job %s (doc %s)", job.ID, *job.DocumentID)
	}
	jobID := job.ID
	return &branches.CreateBranchRequest{
		ProjectID:   &job.ProjectID,
		Name:        name,
		Description: &desc,
		Attribution: &branchorigin.Attribution{
			Origin: branchorigin.Extraction,
			ID:     &jobID,
			Label:  label,
		},
	}
}

// stagingBranchName builds a human-readable branch name for an extraction job.
// Format: extraction/<doc-shortid>/<job-shortid>  (or extraction/<job-shortid> if no doc).
func (w *ObjectExtractionWorker) stagingBranchName(job *ObjectExtractionJob) string {
	jobShort := job.ID
	if len(jobShort) > 8 {
		jobShort = jobShort[:8]
	}
	if job.DocumentID != nil {
		docShort := *job.DocumentID
		if len(docShort) > 8 {
			docShort = docShort[:8]
		}
		return fmt.Sprintf("extraction/%s/%s", docShort, jobShort)
	}
	return fmt.Sprintf("extraction/%s", jobShort)
}

// stagingBranchLabel names the branch's source document for its origin label,
// so the UI can show "Invoice.pdf" instead of extraction/<doc>/<job>. Best
// effort: a missing document, filename or document service yields nil, and the
// UI falls back to the branch name.
func (w *ObjectExtractionWorker) stagingBranchLabel(ctx context.Context, job *ObjectExtractionJob) *string {
	if w.docService == nil || job.DocumentID == nil {
		return nil
	}
	doc, err := w.docService.GetByID(ctx, job.ProjectID, *job.DocumentID)
	if err != nil || doc == nil || doc.Filename == nil {
		return nil
	}
	name := strings.TrimSpace(*doc.Filename)
	if name == "" {
		return nil
	}
	return &name
}

// resolveDocumentVersion returns the source document's current version for
// provenance stamping. A nil SourceID (e.g. manual source), a nil docService,
// or a failed lookup yields 0, which omits the version field from provenance.
func (w *ObjectExtractionWorker) resolveDocumentVersion(ctx context.Context, job *ObjectExtractionJob) int {
	if w.docService == nil || job.SourceID == nil {
		return 0
	}
	doc, err := w.docService.GetByID(ctx, job.ProjectID, *job.SourceID)
	if err != nil {
		w.log.Warn("failed to resolve document version for provenance",
			slog.String("document_id", *job.SourceID),
			logger.Error(err))
		return 0
	}
	if doc == nil {
		return 0
	}
	return doc.Version
}

// isDocumentSource reports whether a job carries a document source: an explicit
// "document" source type plus a resolved source document ID. Only these jobs
// stamp provenance, since a chunk/manual job has no meaningful source document.
func isDocumentSource(job *ObjectExtractionJob) bool {
	return job != nil &&
		job.SourceType != nil && *job.SourceType == string(SourceTypeDocument) &&
		job.SourceID != nil
}

// buildSourcesArray builds the initial `_sources` provenance array for a graph
// object stamped by this extraction job. It mirrors the canonical entry shape
// read by graph.mergeProvenanceProps: {"document_id", "document_version",
// "job_id", "extracted_at"}. `extracted_at` is the job's creation time so the
// stamp is deterministic and monotonic rather than depending on when the worker
// happens to persist the object.
func buildSourcesArray(job *ObjectExtractionJob, documentVersion int) []any {
	if job.SourceID == nil {
		return nil
	}
	extractedAt := job.CreatedAt
	if extractedAt.IsZero() {
		extractedAt = time.Now().UTC()
	}
	entry := map[string]any{
		"document_id":  *job.SourceID,
		"job_id":       job.ID,
		"extracted_at": extractedAt.UTC().Format(time.RFC3339),
	}
	if documentVersion > 0 {
		entry["document_version"] = documentVersion
	}
	return []any{entry}
}

// emitReviewReadyNotification fans a review-ready notification out to project
// members when extraction left a staging branch open for manual review: objects
// were created and the branch was not auto-merged. It reports whether it emitted,
// which suppresses the completion notification for the same run.
//
// Unlike the informational lifecycle events, this one is NOT actor-excluded: it
// carries a required action (merging the branch), and in a solo project the
// triggerer is the only person who can perform it.
//
// It is best-effort — failures are logged and never fail the extraction job.
func (w *ObjectExtractionWorker) emitReviewReadyNotification(ctx context.Context, job *ObjectExtractionJob, stagingBranchID *uuid.UUID, result *ObjectExtractionResults) bool {
	if w.notificationsSvc == nil || stagingBranchID == nil || result == nil || result.ObjectsCreated == 0 {
		return false
	}

	branchID := stagingBranchID.String()

	// Skip only when the branch has actually been merged (e.g. auto-merge ran
	// before we emitted). An open — or even not-found — branch still needs a
	// human reviewer, so it proceeds to notify. The old `autoMergeEnabled` gate
	// is deliberately dropped: an opted-in project whose auto-merge failed still
	// leaves the branch open and must alert.
	if w.branchService != nil {
		branch, err := w.branchService.GetByID(ctx, branchID, job.ProjectID)
		if err == nil && branch != nil && branch.MergedAt != nil {
			return false
		}
	}

	// Resolve the source document once to derive the display filename, version,
	// and the review action URL. Only document-sourced jobs carry a document, so
	// a manual/chunk job notifies with no action URL and a job-id message.
	filename := ""
	documentVersion := 0
	var actionURL *string
	if job.DocumentID != nil {
		docID := *job.DocumentID
		u := fmt.Sprintf("/documents/%s/extractions", docID)
		actionURL = &u
		if w.docService != nil {
			if doc, err := w.docService.GetByID(ctx, job.ProjectID, docID); err == nil && doc != nil {
				if doc.Filename != nil {
					filename = *doc.Filename
				}
				documentVersion = doc.Version
			}
		}
	}

	groupKey := "extraction.review:" + job.ID
	actionLabel := "Review merge"

	detail := map[string]any{
		"job_id":                job.ID,
		"branch_id":             branchID,
		"objects":               result.ObjectsCreated,
		"relationships":         result.RelationshipsCreated,
		"objects_created":       result.ObjectsCreated,
		"relationships_created": result.RelationshipsCreated,
		"document_version":      documentVersion,
	}
	if job.DocumentID != nil {
		detail["document_id"] = *job.DocumentID
	}
	details, _ := json.Marshal(detail)

	relatedType := "graph_branch"
	relatedID := branchID
	sourceType := "extraction_job"

	title := fmt.Sprintf("Extraction ready for review (%d objects)", result.ObjectsCreated)
	message := fmt.Sprintf("Extraction job %s produced %d objects and %d relationships, staged on a branch awaiting your review.",
		job.ID, result.ObjectsCreated, result.RelationshipsCreated)
	if filename != "" {
		message = fmt.Sprintf("%s · %d objects, %d relationships", filename, result.ObjectsCreated, result.RelationshipsCreated)
	}

	if err := w.notificationsSvc.CreateForProjectMembers(ctx, job.ProjectID, notifications.CreateInput{
		EventKey:            "extraction.review_ready",
		Title:               title,
		Message:             message,
		Severity:            "info",
		Importance:          "important",
		SourceType:          &sourceType,
		SourceID:            &job.ID,
		RelatedResourceType: &relatedType,
		RelatedResourceID:   &relatedID,
		ActionURL:           actionURL,
		ActionLabel:         &actionLabel,
		GroupKey:            &groupKey,
		Details:             details,
	}); err != nil {
		w.log.Warn("failed to emit review-ready notification",
			slog.String("job_id", job.ID),
			slog.String("branch_id", branchID),
			logger.Error(err))
		return false
	}
	return true
}

// deleteStagingBranch removes a staging branch that was created for a job that
// subsequently failed. This keeps the branch list clean — partial extractions
// should not be left for reviewers to discover.
func (w *ObjectExtractionWorker) deleteStagingBranch(ctx context.Context, job *ObjectExtractionJob, branchID *uuid.UUID) {
	if w.branchService == nil || branchID == nil {
		return
	}
	if err := w.branchService.Delete(ctx, branchID.String(), job.ProjectID); err != nil {
		w.log.Warn("failed to delete staging branch after job failure",
			slog.String("job_id", job.ID),
			slog.String("branch_id", branchID.String()),
			logger.Error(err))
	}
}

// convertToObjectSchema converts a generic map to ObjectSchema.
func convertToObjectSchema(m map[string]any) agents.ObjectSchema {
	schema := agents.ObjectSchema{}

	if name, ok := m["name"].(string); ok {
		schema.Name = name
	}
	if desc, ok := m["description"].(string); ok {
		schema.Description = desc
	}
	if props, ok := m["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]agents.PropertyDef)
		for k, v := range props {
			if propMap, ok := v.(map[string]any); ok {
				schema.Properties[k] = agents.PropertyDefFromMap(propMap)
			}
		}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	return schema
}

// convertToRelationshipSchema converts a generic map to RelationshipSchema.
func convertToRelationshipSchema(m map[string]any) agents.RelationshipSchema {
	schema := agents.RelationshipSchema{}

	if name, ok := m["name"].(string); ok {
		schema.Name = name
	}
	if desc, ok := m["description"].(string); ok {
		schema.Description = desc
	}
	if st, ok := m["source_types"].([]any); ok {
		for _, t := range st {
			if s, ok := t.(string); ok {
				schema.SourceTypes = append(schema.SourceTypes, s)
			}
		}
	}
	if tt, ok := m["target_types"].([]any); ok {
		for _, t := range tt {
			if s, ok := t.(string); ok {
				schema.TargetTypes = append(schema.TargetTypes, s)
			}
		}
	}
	if g, ok := m["extraction_guidelines"].(string); ok {
		schema.ExtractionGuidelines = g
	}
	if props, ok := m["properties"].(map[string]any); ok && len(props) > 0 {
		schema.Properties = make(map[string]agents.PropertyDef, len(props))
		for propName, raw := range props {
			if pm, ok := raw.(map[string]any); ok {
				schema.Properties[propName] = agents.PropertyDefFromMap(pm)
			}
		}
	}
	if required, ok := m["required"].([]any); ok {
		for _, item := range required {
			if s, ok := item.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	return schema
}

// stringPtr returns a pointer to a string.
func stringPtr(s string) *string {
	return &s
}

// writeDomainClassification writes classification results to the document table (best-effort).
func (w *ObjectExtractionWorker) writeDomainClassification(ctx context.Context, documentID string, cr ClassificationResult) {
	var domainName *string
	var confidence *float32
	if cr.DomainName != "" {
		domainName = &cr.DomainName
		confidence = &cr.Confidence
	}
	signals := map[string]any{
		"matchedSchemaId":   cr.Signals.MatchedSchemaID,
		"matchedSchemaName": cr.Signals.MatchedSchemaName,
		"heuristicKeywords": cr.Signals.HeuristicKeywords,
		"llmReason":         cr.Signals.LLMReason,
		"classifiedAt":      cr.Signals.ClassifiedAt,
	}
	if err := w.docService.UpdateDomainClassification(ctx, documentID, domainName, confidence, signals, false); err != nil {
		w.log.Warn("failed to write domain classification to document",
			slog.String("document_id", documentID),
			logger.Error(err),
		)
	}
}
