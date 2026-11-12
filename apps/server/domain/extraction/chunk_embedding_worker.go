package extraction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uptrace/bun"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/syshealth"
	"github.com/emergent-company/emergent.memory/pkg/tracing"
)

// ChunkEmbeddingWorker processes chunk embedding jobs from the queue.
// It follows the same pattern as GraphEmbeddingWorker:
// - Polling-based with configurable interval
// - Graceful shutdown waiting for current batch
// - Stale job recovery on startup
// - Metrics tracking
type ChunkEmbeddingWorker struct {
	jobs      *ChunkEmbeddingJobsService
	embeds    EmbeddingService
	db        bun.IDB
	cfg       *ChunkEmbeddingConfig
	log       *slog.Logger
	scaler    *syshealth.ConcurrencyScaler
	stopCh    chan struct{}
	stoppedCh chan struct{}
	running   bool
	mu        sync.Mutex
	wg        sync.WaitGroup

	// jobsWg tracks in-flight chunk goroutines so Stop can drain them;
	// inFlight counts the jobs they hold, bounding batch admission.
	jobsWg   sync.WaitGroup
	inFlight atomic.Int64
	// wakeCh nudges the poll loop when a chunk frees capacity, so the next
	// tranche is claimed immediately instead of waiting for the next tick.
	wakeCh chan struct{}

	// Usage tracking & budget enforcement
	usage                    EmbeddingUsageRecorder
	budget                   BudgetChecker
	budgetEnforcementEnabled bool
	orgCache                 *orgIDCache

	// Metrics
	processedCount int64
	successCount   int64
	failureCount   int64
	metricsMu      sync.RWMutex
}

// NewChunkEmbeddingWorker creates a new chunk embedding worker
func NewChunkEmbeddingWorker(
	jobs *ChunkEmbeddingJobsService,
	embeds EmbeddingService,
	db bun.IDB,
	cfg *ChunkEmbeddingConfig,
	log *slog.Logger,
	scaler *syshealth.ConcurrencyScaler,
	usage EmbeddingUsageRecorder,
	budget BudgetChecker,
	budgetEnforcementEnabled bool,
) *ChunkEmbeddingWorker {
	return &ChunkEmbeddingWorker{
		jobs:                     jobs,
		embeds:                   embeds,
		db:                       db,
		cfg:                      cfg,
		log:                      log.With(logger.Scope("chunk.embedding.worker")),
		scaler:                   scaler,
		usage:                    usage,
		budget:                   budget,
		budgetEnforcementEnabled: budgetEnforcementEnabled,
		orgCache:                 newOrgIDCache(db, log),
	}
}

// Start begins the worker's polling loop
func (w *ChunkEmbeddingWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	// Check if embeddings are enabled
	if !w.embeds.IsEnabled() {
		w.log.Info("chunk embedding worker not started (embeddings not enabled)")
		w.mu.Unlock()
		return nil
	}

	w.running = true
	w.stopCh = make(chan struct{})
	w.stoppedCh = make(chan struct{})
	w.wakeCh = make(chan struct{}, 1)
	w.mu.Unlock()

	// Recover stale jobs on startup
	go w.recoverStaleJobsOnStartup(ctx)

	w.log.Info("chunk embedding worker starting",
		slog.Duration("poll_interval", w.cfg.WorkerInterval()),
		slog.Int("batch_size", w.cfg.WorkerBatchSize))

	w.wg.Add(1)
	go w.run(ctx)

	return nil
}

// Stop gracefully stops the worker, waiting for current batch to complete
func (w *ChunkEmbeddingWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	w.log.Debug("waiting for chunk embedding worker to stop...")

	// Wait for worker to stop or context to be cancelled
	select {
	case <-w.stoppedCh:
		w.log.Info("chunk embedding worker stopped gracefully")
	case <-ctx.Done():
		w.log.Warn("chunk embedding worker stop timeout, forcing shutdown")
		return nil
	}

	// The poll loop no longer waits for a whole batch, so drain the chunk
	// goroutines that are still holding claimed jobs.
	drained := make(chan struct{})
	go func() {
		w.jobsWg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		w.log.Info("chunk embedding worker drained in-flight jobs")
	case <-ctx.Done():
		w.log.Warn("chunk embedding worker drain timeout, forcing shutdown")
	}

	return nil
}

// recoverStaleJobsOnStartup recovers orphaned processing jobs on startup
func (w *ChunkEmbeddingWorker) recoverStaleJobsOnStartup(ctx context.Context) {
	recovered, err := w.jobs.RecoverOrphanedProcessingJobs(ctx)
	if err != nil {
		w.log.Warn("failed to recover orphaned jobs on startup",
			slog.String("error", err.Error()))
		return
	}
	if recovered > 0 {
		w.log.Info("recovered orphaned chunk embedding jobs on startup",
			slog.Int("count", recovered))
	}
}

// run is the main worker loop
func (w *ChunkEmbeddingWorker) run(ctx context.Context) {
	defer w.wg.Done()
	defer close(w.stoppedCh)

	ticker := time.NewTicker(w.cfg.WorkerInterval())
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.processBatch(ctx); err != nil {
				w.log.Warn("process batch failed", slog.String("error", err.Error()))
			}
		case <-w.wakeCh:
			// Capacity freed up mid-interval; claim the next tranche now.
			if err := w.processBatch(ctx); err != nil {
				w.log.Warn("process batch failed", slog.String("error", err.Error()))
			}
		}
	}
}

// concurrency returns the effective number of jobs that may be in flight.
func (w *ChunkEmbeddingWorker) concurrency() int {
	concurrency := w.cfg.WorkerConcurrency
	if w.scaler != nil {
		concurrency = w.scaler.GetConcurrency(w.cfg.WorkerConcurrency)
	}
	if concurrency <= 0 {
		concurrency = 10
	}
	return concurrency
}

// processBatch claims jobs for the free in-flight capacity and dispatches them
// asynchronously. It never waits for the claimed jobs to finish: a slow
// embedding request holds only its own slot, so it cannot gate the rest of the
// queue. The next tranche is claimed as capacity frees up.
func (w *ChunkEmbeddingWorker) processBatch(ctx context.Context) error {
	// Check if we should stop
	select {
	case <-w.stopCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	free := int64(w.concurrency()) - w.inFlight.Load()
	if free <= 0 {
		return nil
	}

	claim := w.cfg.WorkerBatchSize
	if claim <= 0 || int64(claim) > free {
		claim = int(free)
	}
	if claim <= 0 {
		return nil
	}

	jobs, err := w.jobs.Dequeue(ctx, claim)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	// Reserve capacity before spawning so admission never overshoots the
	// concurrency budget.
	w.inFlight.Add(int64(len(jobs)))

	for _, job := range jobs {
		w.jobsWg.Add(1)
		go w.processTrackedJob(ctx, job)
	}

	return nil
}

// processTrackedJob runs one claimed chunk job and always releases the reserved
// capacity exactly once.
func (w *ChunkEmbeddingWorker) processTrackedJob(ctx context.Context, job *ChunkEmbeddingJob) {
	defer w.jobsWg.Done()
	defer func() {
		w.inFlight.Add(-1)
		if w.wakeCh != nil {
			select {
			case w.wakeCh <- struct{}{}:
			default:
			}
		}
	}()
	if err := w.processJob(ctx, job); err != nil {
		w.log.Warn("process job failed",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()))
	}
}

// chunkRow represents the minimal data needed from a chunk for embedding
type chunkRow struct {
	ID              string `bun:"id,type:uuid"`
	DocumentID      string `bun:"document_id,type:uuid"`
	ChunkIndex      int    `bun:"chunk_index"`
	Text            string `bun:"text"`
	CurrentRevision int    `bun:"current_revision"`
	ProjectID       string `bun:"project_id,type:uuid"`
	IsParent        bool   `bun:"is_parent"`
	Oversized       bool   `bun:"oversized"`
}

// processJob processes a single chunk embedding job
func (w *ChunkEmbeddingWorker) processJob(ctx context.Context, job *ChunkEmbeddingJob) error {
	ctx, span := tracing.Start(ctx, "extraction.chunk_embedding",
		attribute.String("memory.job.id", job.ID),
	)
	defer span.End()

	startTime := time.Now()

	// Fetch the chunk along with the project ID from its parent document.
	chunk := &chunkRow{}
	err := w.db.NewRaw(`
		SELECT c.id, c.document_id, c.chunk_index, c.text, c.current_revision, d.project_id,
		       COALESCE((c.metadata->>'isParent')::boolean, false) AS is_parent,
		       COALESCE((c.metadata->>'oversized')::boolean, false) AS oversized
		FROM kb.chunks c
		JOIN kb.documents d ON d.id = c.document_id
		WHERE c.id = ?`, job.ChunkID).Scan(ctx, chunk)

	if err == sql.ErrNoRows {
		// Chunk doesn't exist, mark as failed
		chunkErr := fmt.Errorf("chunk not found: %s", job.ChunkID)
		span.RecordError(chunkErr)
		span.SetStatus(codes.Error, chunkErr.Error())
		if markErr := w.jobs.MarkFailed(ctx, job.ID, fmt.Errorf("chunk_missing")); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return chunkErr
	}
	if err != nil {
		// Database error
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return fmt.Errorf("fetch chunk: %w", err)
	}

	textLength := len(chunk.Text)

	// Inject project ID into context so the credential resolver can look up
	// per-project LLM provider configuration (e.g. Vertex AI credentials).
	if chunk.ProjectID != "" {
		ctx = auth.ContextWithProjectID(ctx, chunk.ProjectID)
		span.SetAttributes(attribute.String("memory.project.id", chunk.ProjectID))
	}

	// Structural parents and oversized atomic segments beyond the embedding
	// context are not embedded; mark the job complete so it is not retried.
	if reason := skipEmbeddingReason(chunk.IsParent, chunk.Oversized, textLength); reason != "" {
		span.SetAttributes(attribute.String("memory.embedding.skipped", reason))
		w.log.Info("chunk embedding skipped",
			slog.String("job_id", job.ID),
			slog.String("chunk_id", chunk.ID),
			slog.String("reason", reason),
		)
		// A skipped chunk must not stay searchable: clear any previous vector so
		// stale content is never served. If the clear fails, mark the job failed
		// (retry) rather than silently keeping the stale vector.
		if _, clearErr := w.db.NewRaw(`UPDATE kb.chunks
			SET embedding = NULL,
				updated_at = now()
			WHERE id = ?`, chunk.ID).Exec(ctx); clearErr != nil {
			span.RecordError(clearErr)
			span.SetStatus(codes.Error, clearErr.Error())
			if markErr := w.jobs.MarkFailed(ctx, job.ID, clearErr); markErr != nil {
				w.log.Error("failed to mark job failed after skip clear failure",
					slog.String("job_id", job.ID),
					slog.String("error", markErr.Error()))
			}
			w.incrementFailure()
			return fmt.Errorf("clear skipped chunk embedding: %w", clearErr)
		}
		if markErr := w.jobs.MarkCompleted(ctx, job.ID); markErr != nil {
			w.log.Error("failed to mark skipped job completed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
			return fmt.Errorf("mark skipped job completed: %w", markErr)
		}
		return nil
	}

	// Budget pre-flight check — skip embedding when project has exceeded its monthly budget.
	if w.budget != nil && chunk.ProjectID != "" {
		exceeded, err := w.budget.CheckBudgetExceeded(ctx, chunk.ProjectID)
		if err != nil {
			w.log.Warn("embedding budget check failed, proceeding",
				slog.String("job_id", job.ID),
				slog.String("project_id", chunk.ProjectID),
				slog.String("error", err.Error()),
			)
		} else if exceeded && w.budgetEnforcementEnabled {
			w.log.Warn("chunk embedding skipped: project budget exceeded, rescheduling",
				slog.String("job_id", job.ID),
				slog.String("project_id", chunk.ProjectID),
			)
			if _, reschedErr := w.db.NewRaw(`UPDATE kb.chunk_embedding_jobs
				SET status = 'pending',
					last_error = 'budget_exceeded',
					scheduled_at = now() + interval '5 minutes',
					updated_at = now()
				WHERE id = ?`, job.ID).Exec(ctx); reschedErr != nil {
				w.log.Error("failed to reschedule budget-exceeded job",
					slog.String("job_id", job.ID),
					slog.String("error", reschedErr.Error()))
			}
			return &EmbeddingBudgetExceededError{ProjectID: chunk.ProjectID}
		}
	}

	// Generate embedding
	embeddingStartTime := time.Now()
	result, err := w.embeds.EmbedDocumentWithUsage(ctx, chunk.Text)
	embeddingDurationMs := time.Since(embeddingStartTime).Milliseconds()

	if err != nil {
		// Embedding failed — distinguish permanent (bad model/creds) from transient (network, quota)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if isPermanentEmbeddingError(err) {
			w.log.Warn("chunk embedding permanently failed (non-retryable error)",
				slog.String("job_id", job.ID),
				slog.String("error", err.Error()))
			if markErr := w.jobs.MarkPermanentlyFailed(ctx, job.ID, err); markErr != nil {
				w.log.Error("failed to mark job as permanently failed",
					slog.String("job_id", job.ID),
					slog.String("error", markErr.Error()))
			}
		} else {
			if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
				w.log.Error("failed to mark job as failed",
					slog.String("job_id", job.ID),
					slog.String("error", markErr.Error()))
			}
		}
		w.incrementFailure()
		return fmt.Errorf("generate embedding: %w", err)
	}

	if result == nil || len(result.Embedding) == 0 {
		// No embedding returned (likely noop client)
		err := fmt.Errorf("no embedding returned")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return err
	}

	// Record embedding usage for cost tracking
	orgID := w.orgCache.resolve(ctx, chunk.ProjectID)
	recordEmbeddingUsage(w.usage, chunk.ProjectID, orgID, result)

	// Update the chunk with the embedding and complete the job atomically.
	//
	// Note: embedding is vector(768), we need to use raw SQL for pgvector.
	// Two things must hold together:
	//   - The vector write is guarded by current_revision so a stale embedding
	//     produced from text that was edited/rolled back/re-chunked while the
	//     request was in flight is never stored.
	//   - The job's completion is serialized with concurrent edits on the chunk
	//     row lock. A single transaction holds that lock from the guarded
	//     UPDATE until the job is marked completed. An edit therefore either
	//     commits before the guard (0 rows → the job is requeued in the same tx)
	//     or blocks until completion commits (its own enqueue then sees the job
	//     as terminal and schedules a fresh job). Without this, an edit landing
	//     between the vector write and MarkCompleted left the stale vector with
	//     no job to regenerate it.
	now := time.Now()
	requeued := false
	err = w.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewRaw(`UPDATE kb.chunks
			SET embedding = ?::vector,
				updated_at = ?
			WHERE id = ? AND current_revision = ?`,
			vectorToString(result.Embedding), now, job.ChunkID, chunk.CurrentRevision).Exec(ctx)
		if err != nil {
			return fmt.Errorf("update embedding: %w", err)
		}

		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("check embedding update affected rows: %w", err)
		}

		if affected == 0 {
			// The chunk changed (edited/rolled back/re-chunked) or vanished while
			// the embedding was in flight. Discard the stale vector and make the
			// SAME job run again against the fresh text; it is still 'processing'
			// inside this tx, so the partial unique index does not block it. If the
			// chunk was deleted its job was cascade-deleted and this matches nothing.
			if _, err := tx.NewRaw(`UPDATE kb.chunk_embedding_jobs
				SET status = 'pending',
					started_at = NULL,
					scheduled_at = now(),
					last_error = 'stale_chunk_revision',
					updated_at = now()
				WHERE id = ?`, job.ID).Exec(ctx); err != nil {
				return fmt.Errorf("requeue stale embedding job: %w", err)
			}
			requeued = true
			return nil
		}

		if _, err := tx.NewUpdate().
			Model((*ChunkEmbeddingJob)(nil)).
			Set("status = ?", JobStatusCompleted).
			Set("completed_at = ?", now).
			Set("last_error = NULL").
			Set("updated_at = ?", now).
			Where("id = ?", job.ID).
			Exec(ctx); err != nil {
			return fmt.Errorf("mark chunk embedding job completed: %w", err)
		}
		return nil
	})

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return err
	}

	if requeued {
		w.log.Warn("chunk changed while embedding in flight; discarding stale vector",
			slog.String("job_id", job.ID),
			slog.String("chunk_id", chunk.ID))
		return nil
	}
	span.SetStatus(codes.Ok, "")

	totalDurationMs := time.Since(startTime).Milliseconds()

	w.log.Debug("generated embedding for chunk",
		slog.String("chunk_id", chunk.ID),
		slog.String("document_id", chunk.DocumentID),
		slog.Int("chunk_index", chunk.ChunkIndex),
		slog.Int("embedding_dims", len(result.Embedding)),
		slog.Int("text_length", textLength),
		slog.Int64("embedding_duration_ms", embeddingDurationMs),
		slog.Int64("total_duration_ms", totalDurationMs))

	w.incrementSuccess()
	return nil
}

// incrementSuccess increments both processed and success counters
func (w *ChunkEmbeddingWorker) incrementSuccess() {
	w.metricsMu.Lock()
	w.processedCount++
	w.successCount++
	w.metricsMu.Unlock()
}

// incrementFailure increments both processed and failure counters
func (w *ChunkEmbeddingWorker) incrementFailure() {
	w.metricsMu.Lock()
	w.processedCount++
	w.failureCount++
	w.metricsMu.Unlock()
}

// Metrics returns current worker metrics
func (w *ChunkEmbeddingWorker) Metrics() ChunkEmbeddingWorkerMetrics {
	w.metricsMu.RLock()
	defer w.metricsMu.RUnlock()

	return ChunkEmbeddingWorkerMetrics{
		Processed: w.processedCount,
		Succeeded: w.successCount,
		Failed:    w.failureCount,
	}
}

// ChunkEmbeddingWorkerMetrics contains worker metrics
type ChunkEmbeddingWorkerMetrics struct {
	Processed int64 `json:"processed"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
}

// IsRunning returns whether the worker is currently running
func (w *ChunkEmbeddingWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}
