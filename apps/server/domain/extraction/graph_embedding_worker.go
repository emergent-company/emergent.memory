package extraction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uptrace/bun"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/syshealth"
	"github.com/emergent-company/emergent.memory/pkg/tracing"
)

// EmbeddingService is the interface for embedding services used by the worker.
// This allows for dependency injection and testing.
type EmbeddingService interface {
	IsEnabled() bool
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
	EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error)
}

// batchEmbeddingService is implemented by embedding services that can embed
// several documents in one request. *embeddings.Service satisfies it. A worker
// wired with a single-object-only service falls back to one request per job.
type batchEmbeddingService interface {
	EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*vertex.BatchEmbedResult, error)
}

// defaultEmbeddingRequestBatchSize is the number of objects sent in one
// multi-object embedding request when
// GraphEmbeddingConfig.EmbeddingRequestBatchSize is unset. It matches the
// embedding clients' own per-request split point (vertex.DefaultBatchSize), so
// a chunk of this size becomes a single HTTP request.
const defaultEmbeddingRequestBatchSize = 100

// GraphEmbeddingWorker processes graph embedding jobs from the queue.
// Worker pattern:
//   - Polling-based with configurable interval
//   - Batch admission is decoupled from per-job completion: the poll loop claims
//     only as many jobs as there is free in-flight capacity and dispatches them
//     asynchronously, so a slow request can never gate the whole batch.
//   - Multi-object embedding requests, grouped per project
//   - Graceful shutdown draining in-flight jobs
//   - Stale job recovery on startup
//   - Metrics tracking
type GraphEmbeddingWorker struct {
	jobs      *GraphEmbeddingJobsService
	embeds    EmbeddingService
	db        bun.IDB
	cfg       *GraphEmbeddingConfig
	log       *slog.Logger
	scaler    *syshealth.ConcurrencyScaler
	stopCh    chan struct{}
	stoppedCh chan struct{}
	running   bool
	paused    bool
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

// NewGraphEmbeddingWorker creates a new graph embedding worker
func NewGraphEmbeddingWorker(
	jobs *GraphEmbeddingJobsService,
	embeds EmbeddingService,
	db bun.IDB,
	cfg *GraphEmbeddingConfig,
	log *slog.Logger,
	scaler *syshealth.ConcurrencyScaler,
	usage EmbeddingUsageRecorder,
	budget BudgetChecker,
	budgetEnforcementEnabled bool,
) *GraphEmbeddingWorker {
	return &GraphEmbeddingWorker{
		jobs:                     jobs,
		embeds:                   embeds,
		db:                       db,
		cfg:                      cfg,
		log:                      log.With(logger.Scope("graph.embedding.worker")),
		scaler:                   scaler,
		usage:                    usage,
		budget:                   budget,
		budgetEnforcementEnabled: budgetEnforcementEnabled,
		orgCache:                 newOrgIDCache(db, log),
	}
}

// Start begins the worker's polling loop
func (w *GraphEmbeddingWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	// Check if embeddings are enabled
	if !w.embeds.IsEnabled() {
		w.log.Info("graph embedding worker not started (embeddings not enabled)")
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

	w.log.Info("graph embedding worker starting",
		slog.Duration("poll_interval", w.cfg.WorkerInterval()),
		slog.Int("batch_size", w.cfg.WorkerBatchSize),
		slog.Int("request_batch_size", w.requestBatchSize()))

	w.wg.Add(1)
	go w.run(ctx)

	return nil
}

// Stop gracefully stops the worker, then drains in-flight jobs so shutdown
// does not abandon jobs that were already claimed ('processing').
func (w *GraphEmbeddingWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	w.log.Debug("waiting for graph embedding worker to stop...")

	// Wait for the poll loop to stop or context to be cancelled
	select {
	case <-w.stoppedCh:
		w.log.Info("graph embedding worker stopped gracefully")
	case <-ctx.Done():
		w.log.Warn("graph embedding worker stop timeout, forcing shutdown")
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
		w.log.Info("graph embedding worker drained in-flight jobs")
	case <-ctx.Done():
		w.log.Warn("graph embedding worker drain timeout, forcing shutdown")
	}

	return nil
}

// recoverStaleJobsOnStartup recovers orphaned processing jobs on startup
func (w *GraphEmbeddingWorker) recoverStaleJobsOnStartup(ctx context.Context) {
	recovered, err := w.jobs.RecoverOrphanedProcessingJobs(ctx)
	if err != nil {
		w.log.Warn("failed to recover orphaned jobs on startup",
			slog.String("error", err.Error()))
		return
	}
	if recovered > 0 {
		w.log.Info("recovered orphaned graph embedding jobs on startup",
			slog.Int("count", recovered))
	}
}

// run is the main worker loop
func (w *GraphEmbeddingWorker) run(ctx context.Context) {
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
func (w *GraphEmbeddingWorker) concurrency() int {
	concurrency := w.cfg.WorkerConcurrency
	if w.scaler != nil {
		concurrency = w.scaler.GetConcurrency(w.cfg.WorkerConcurrency)
	}
	if concurrency <= 0 {
		concurrency = 10
	}
	return concurrency
}

// requestBatchSize returns the effective number of objects per embedding
// request. A non-positive configuration disables multi-object requests.
func (w *GraphEmbeddingWorker) requestBatchSize() int {
	size := w.cfg.EmbeddingRequestBatchSize
	if size == 0 {
		return defaultEmbeddingRequestBatchSize
	}
	return size
}

// chunkSize returns how many jobs to group into one work chunk. Chunks are
// embedded with a single multi-object request only when the embedding service
// supports it; otherwise each job gets its own chunk so jobs never serialize
// behind one another.
func (w *GraphEmbeddingWorker) chunkSize() int {
	if _, ok := w.embeds.(batchEmbeddingService); !ok {
		return 1
	}
	size := w.requestBatchSize()
	if size <= 0 {
		return 1
	}
	return size
}

// chunkJobs splits jobs into consecutive slices of at most size. size <= 0 or
// size >= len(jobs) yields a single chunk.
func chunkJobs(jobs []*GraphEmbeddingJob, size int) [][]*GraphEmbeddingJob {
	if len(jobs) == 0 {
		return nil
	}
	if size <= 0 || size >= len(jobs) {
		return [][]*GraphEmbeddingJob{jobs}
	}
	chunks := make([][]*GraphEmbeddingJob, 0, (len(jobs)+size-1)/size)
	for i := 0; i < len(jobs); i += size {
		end := i + size
		if end > len(jobs) {
			end = len(jobs)
		}
		chunks = append(chunks, jobs[i:end])
	}
	return chunks
}

// processBatch claims jobs for the free in-flight capacity and dispatches them
// asynchronously. It never waits for the claimed jobs to finish: a slow
// embedding request holds only its own chunk's slots, so it cannot gate the
// rest of the queue. The next tranche is claimed as capacity frees up.
func (w *GraphEmbeddingWorker) processBatch(ctx context.Context) error {
	// Check if we should stop
	select {
	case <-w.stopCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Check if paused
	w.mu.Lock()
	paused := w.paused
	w.mu.Unlock()
	if paused {
		return nil
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

	for _, chunk := range chunkJobs(jobs, w.chunkSize()) {
		w.jobsWg.Add(1)
		go w.processChunk(ctx, chunk)
	}

	return nil
}

// processChunk embeds one chunk of jobs and completes/fails each job
// independently. It must always release the reserved capacity.
func (w *GraphEmbeddingWorker) processChunk(ctx context.Context, jobs []*GraphEmbeddingJob) {
	defer w.jobsWg.Done()
	defer func() {
		w.inFlight.Add(-int64(len(jobs)))
		if w.wakeCh != nil {
			select {
			case w.wakeCh <- struct{}{}:
			default:
			}
		}
	}()

	// A single job keeps the original per-job path (also the fallback when the
	// embedding service has no multi-object method).
	if len(jobs) == 1 {
		if err := w.processJob(ctx, jobs[0]); err != nil {
			w.log.Warn("process job failed",
				slog.String("job_id", jobs[0].ID),
				slog.String("error", err.Error()))
		}
		return
	}

	objects, err := w.fetchObjects(ctx, jobs)
	if err != nil {
		w.log.Warn("failed to fetch objects for embedding chunk",
			slog.Int("jobs", len(jobs)),
			slog.String("error", err.Error()))
		for _, j := range jobs {
			w.markJobFailed(ctx, j, err)
		}
		return
	}

	// Group by project: embedding credentials and budgets are per project.
	groups := make(map[string][]*embeddingWorkItem, 4)
	for _, j := range jobs {
		obj, ok := objects[j.ObjectID]
		if !ok {
			w.dropMissingObjectJob(ctx, j)
			continue
		}
		groups[obj.ProjectID] = append(groups[obj.ProjectID], &embeddingWorkItem{job: j, obj: obj})
	}

	for projectID, items := range groups {
		w.processProjectGroup(ctx, projectID, items)
	}
}

// embeddingWorkItem pairs a claimed job with the graph object it embeds.
type embeddingWorkItem struct {
	job *GraphEmbeddingJob
	obj *graphObjectRow
}

// fetchObjects loads every object referenced by jobs in one query.
func (w *GraphEmbeddingWorker) fetchObjects(ctx context.Context, jobs []*GraphEmbeddingJob) (map[string]*graphObjectRow, error) {
	ids := make([]string, 0, len(jobs))
	for _, j := range jobs {
		ids = append(ids, j.ObjectID)
	}

	var rows []graphObjectRow
	err := w.db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id", "type", "key", "properties", "project_id").
		Where("id IN (?)", bun.In(ids)).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("fetch embedding objects: %w", err)
	}

	objects := make(map[string]*graphObjectRow, len(rows))
	for i := range rows {
		objects[rows[i].ID] = &rows[i]
	}
	return objects, nil
}

// processProjectGroup embeds a project's objects with a single multi-object
// request and completes each job independently. Errors fail only the jobs in
// this group; other groups are unaffected.
func (w *GraphEmbeddingWorker) processProjectGroup(ctx context.Context, projectID string, items []*embeddingWorkItem) {
	gctx := ctx
	if projectID != "" {
		gctx = auth.ContextWithProjectID(ctx, projectID)
	}

	// Budget pre-flight check — one check per project group.
	if w.budget != nil && projectID != "" {
		exceeded, err := w.budget.CheckBudgetExceeded(gctx, projectID)
		if err != nil {
			// Fail-open: log warning but proceed so a broken budget query never halts embeddings.
			w.log.Warn("embedding budget check failed, proceeding",
				slog.String("project_id", projectID),
				slog.String("error", err.Error()),
			)
		} else if exceeded && w.budgetEnforcementEnabled {
			for _, it := range items {
				w.log.Warn("graph embedding skipped: project budget exceeded, rescheduling",
					slog.String("job_id", it.job.ID),
					slog.String("project_id", projectID),
				)
				// Reschedule with a 5-minute delay without incrementing attempt count
				if _, reschedErr := w.db.NewRaw(`UPDATE kb.graph_embedding_jobs
					SET status = 'pending',
						last_error = 'budget_exceeded',
						scheduled_at = now() + interval '5 minutes',
						updated_at = now()
					WHERE id = ?`, it.job.ID).Exec(gctx); reschedErr != nil {
					w.log.Error("failed to reschedule budget-exceeded job",
						slog.String("job_id", it.job.ID),
						slog.String("error", reschedErr.Error()))
				}
			}
			return
		}
	}

	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = w.extractText(it.obj)
	}

	batcher, ok := w.embeds.(batchEmbeddingService)
	if !ok {
		// Should not happen (chunkSize() == 1 for single-object services), but
		// keep the fallback correct rather than embedding the wrong text.
		for _, it := range items {
			if err := w.processJob(gctx, it.job); err != nil {
				w.log.Warn("process job failed",
					slog.String("job_id", it.job.ID),
					slog.String("error", err.Error()))
			}
		}
		return
	}

	batchCtx, span := tracing.Start(gctx, "extraction.graph_embedding.batch",
		attribute.String("memory.project.id", projectID),
		attribute.Int("memory.embedding.batch_size", len(texts)),
	)
	defer span.End()

	embeddingStartTime := time.Now()
	result, err := batcher.EmbedDocumentsWithUsage(batchCtx, texts)
	embeddingDurationMs := time.Since(embeddingStartTime).Milliseconds()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		for _, it := range items {
			w.markJobFailed(batchCtx, it.job, fmt.Errorf("generate embedding: %w", err))
		}
		return
	}
	if result == nil || len(result.Embeddings) != len(texts) {
		batchErr := fmt.Errorf("embedding batch returned %d vectors for %d documents", embeddingVectorCount(result), len(texts))
		span.RecordError(batchErr)
		span.SetStatus(codes.Error, batchErr.Error())
		for _, it := range items {
			w.markJobFailed(batchCtx, it.job, batchErr)
		}
		return
	}

	span.SetAttributes(attribute.Int64("memory.embedding.duration_ms", embeddingDurationMs))
	span.SetStatus(codes.Ok, "")

	// One usage event per project group: the batch API reports aggregate tokens.
	if w.usage != nil && projectID != "" {
		orgID := w.orgCache.resolve(batchCtx, projectID)
		recordEmbeddingUsage(w.usage, projectID, orgID, &vertex.EmbedResult{
			Usage:    result.Usage,
			Model:    result.Model,
			Provider: result.Provider,
		})
	}

	for i, it := range items {
		if err := w.storeEmbedding(batchCtx, it, result.Embeddings[i], embeddingDurationMs); err != nil {
			w.log.Warn("store embedding failed",
				slog.String("job_id", it.job.ID),
				slog.String("error", err.Error()))
		}
	}
}

// embeddingVectorCount is a nil-safe length helper.
func embeddingVectorCount(result *vertex.BatchEmbedResult) int {
	if result == nil {
		return 0
	}
	return len(result.Embeddings)
}

// dropMissingObjectJob removes a job whose object no longer exists, matching
// the per-job path: the job is deleted rather than retried.
func (w *GraphEmbeddingWorker) dropMissingObjectJob(ctx context.Context, job *GraphEmbeddingJob) {
	w.log.Warn("graph embedding job dropped, object not found",
		slog.String("job_id", job.ID),
		slog.String("object_id", job.ObjectID))
	if delErr := w.jobs.DeleteJob(ctx, job.ID); delErr != nil {
		w.log.Error("failed to delete job for missing object",
			slog.String("job_id", job.ID),
			slog.String("error", delErr.Error()))
	}
	w.incrementFailure()
}

// markJobFailed records a failed embedding attempt for one job, distinguishing
// permanent (bad model/credentials) from transient errors so retry semantics
// are unchanged.
func (w *GraphEmbeddingWorker) markJobFailed(ctx context.Context, job *GraphEmbeddingJob, err error) {
	if isPermanentEmbeddingError(err) {
		w.log.Warn("graph embedding permanently failed (non-retryable error)",
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
}

// storeEmbedding writes the vector to the graph object and completes the job.
func (w *GraphEmbeddingWorker) storeEmbedding(ctx context.Context, it *embeddingWorkItem, embedding []float32, embeddingDurationMs int64) error {
	now := time.Now()
	_, err := w.db.NewRaw(`UPDATE kb.graph_objects
		SET embedding_v2 = ?::vector,
			embedding_updated_at = ?,
			updated_at = ?
		WHERE id = ?`,
		vectorToString(embedding), now, now, it.job.ObjectID).Exec(ctx)
	if err != nil {
		if markErr := w.jobs.MarkFailed(ctx, it.job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", it.job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		w.log.Warn("failed to store graph object embedding",
			slog.String("job_id", it.job.ID),
			slog.String("object_id", it.job.ObjectID),
			slog.String("error", err.Error()))
		return fmt.Errorf("update embedding: %w", err)
	}

	if err := w.jobs.MarkCompleted(ctx, it.job.ID); err != nil {
		w.log.Error("failed to mark job as completed",
			slog.String("job_id", it.job.ID),
			slog.String("error", err.Error()))
		return err
	}

	w.incrementSuccess()
	w.log.Debug("generated embedding for graph object",
		slog.String("object_id", it.job.ObjectID),
		slog.String("object_type", it.obj.Type),
		slog.Int("embedding_dims", len(embedding)),
		slog.Int64("embedding_duration_ms", embeddingDurationMs))
	return nil
}

// graphObjectRow represents the minimal data needed from a graph object for embedding
type graphObjectRow struct {
	ID         string                 `bun:"id,type:uuid"`
	Type       string                 `bun:"type"`
	Key        *string                `bun:"key"`
	Properties map[string]interface{} `bun:"properties,type:jsonb"`
	ProjectID  string                 `bun:"project_id,type:uuid"`
}

// processJob processes a single graph embedding job. It is used for single-job
// chunks and as the fallback when the embedding service has no multi-object
// method.
func (w *GraphEmbeddingWorker) processJob(ctx context.Context, job *GraphEmbeddingJob) error {
	ctx, span := tracing.Start(ctx, "extraction.graph_embedding",
		attribute.String("memory.job.id", job.ID),
		attribute.String("memory.object.id", job.ObjectID),
	)
	defer span.End()

	startTime := time.Now()

	// Fetch the graph object
	obj := &graphObjectRow{}
	err := w.db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id", "type", "key", "properties", "project_id").
		Where("id = ?", job.ObjectID).
		Scan(ctx, obj)

	if err == sql.ErrNoRows {
		// Object doesn't exist — remove job from queue, no retry
		objErr := fmt.Errorf("object not found: %s", job.ObjectID)
		span.RecordError(objErr)
		span.SetStatus(codes.Error, objErr.Error())
		if delErr := w.jobs.DeleteJob(ctx, job.ID); delErr != nil {
			w.log.Error("failed to delete job for missing object",
				slog.String("job_id", job.ID),
				slog.String("error", delErr.Error()))
		}
		w.incrementFailure()
		return objErr
	}
	if err != nil {
		// Database error
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		w.markJobFailed(ctx, job, err)
		return fmt.Errorf("fetch object: %w", err)
	}

	// Now we have the full object — set remaining span attributes
	keyVal := ""
	if obj.Key != nil {
		keyVal = *obj.Key
	}
	span.SetAttributes(
		attribute.String("memory.project.id", obj.ProjectID),
		attribute.String("memory.object.type", obj.Type),
		attribute.String("memory.object.key", keyVal),
	)

	// Inject project ID into context so the credential resolver can look up
	// per-project LLM provider configuration (e.g. Vertex AI credentials).
	projCtx := ctx
	if obj.ProjectID != "" {
		projCtx = auth.ContextWithProjectID(ctx, obj.ProjectID)
	}

	// Budget pre-flight check — skip embedding when project has exceeded its monthly budget.
	if w.budget != nil && obj.ProjectID != "" {
		exceeded, err := w.budget.CheckBudgetExceeded(projCtx, obj.ProjectID)
		if err != nil {
			// Fail-open: log warning but proceed so a broken budget query never halts embeddings.
			w.log.Warn("embedding budget check failed, proceeding",
				slog.String("job_id", job.ID),
				slog.String("project_id", obj.ProjectID),
				slog.String("error", err.Error()),
			)
		} else if exceeded && w.budgetEnforcementEnabled {
			w.log.Warn("graph embedding skipped: project budget exceeded, rescheduling",
				slog.String("job_id", job.ID),
				slog.String("project_id", obj.ProjectID),
			)
			// Reschedule with a 5-minute delay without incrementing attempt count
			if _, reschedErr := w.db.NewRaw(`UPDATE kb.graph_embedding_jobs
				SET status = 'pending',
					last_error = 'budget_exceeded',
					scheduled_at = now() + interval '5 minutes',
					updated_at = now()
				WHERE id = ?`, job.ID).Exec(projCtx); reschedErr != nil {
				w.log.Error("failed to reschedule budget-exceeded job",
					slog.String("job_id", job.ID),
					slog.String("error", reschedErr.Error()))
			}
			return &EmbeddingBudgetExceededError{ProjectID: obj.ProjectID}
		}
	}

	// Extract text for embedding
	text := w.extractText(obj)
	textLength := len(text)

	// Generate embedding
	embeddingStartTime := time.Now()
	result, err := w.embeds.EmbedQueryWithUsage(projCtx, text)
	embeddingDurationMs := time.Since(embeddingStartTime).Milliseconds()

	if err != nil {
		// Embedding failed — distinguish permanent (bad model/creds) from transient (network, quota)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		w.markJobFailed(projCtx, job, err)
		return fmt.Errorf("generate embedding: %w", err)
	}

	if result == nil || len(result.Embedding) == 0 {
		// No embedding returned (likely noop client)
		err := fmt.Errorf("no embedding returned")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		w.markJobFailed(projCtx, job, err)
		return err
	}

	// Record embedding usage for cost tracking
	orgID := w.orgCache.resolve(projCtx, obj.ProjectID)
	recordEmbeddingUsage(w.usage, obj.ProjectID, orgID, result)

	// Update the graph object with the embedding and complete the job.
	if err := w.storeEmbedding(projCtx, &embeddingWorkItem{job: job, obj: obj}, result.Embedding, embeddingDurationMs); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	totalDurationMs := time.Since(startTime).Milliseconds()

	span.SetAttributes(
		attribute.Int("memory.embedding.text_length", textLength),
		attribute.Int("memory.embedding.dims", len(result.Embedding)),
		attribute.Int64("memory.embedding.duration_ms", embeddingDurationMs),
		attribute.Int64("memory.total_duration_ms", totalDurationMs),
	)
	span.SetStatus(codes.Ok, "")
	return nil
}

// maxEmbeddingChars is the character budget for embedding text.
// Gemini embedding models accept ~3 000 tokens; at ~5 chars/word this gives
// plenty of headroom while preventing runaway concatenation on large objects.
const maxEmbeddingChars = 8000

// skipEmbeddingKey lists property keys that carry no semantic value for embedding.
var skipEmbeddingKey = map[string]bool{
	"id":         true,
	"url":        true,
	"uri":        true,
	"href":       true,
	"license":    true,
	"version":    true,
	"citations":  true,
	"references": true,
	"hash":       true,
	"checksum":   true,
	"created_at": true,
	"updated_at": true,
	"deleted_at": true,
}

// boostedEmbeddingKeys are semantic fields prepended (and repeated ×2) to
// increase their weight in the embedding vector. Order is significant: more
// important fields first so they are never truncated by the char budget.
var boostedEmbeddingKeys = []string{"name", "title", "description", "summary", "label"}

// isUUID returns true if s looks like a UUID (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// extractText extracts semantic text from a graph object for embedding.
//
// Token order (most important → least important):
//  1. obj.Type
//  2. Stripped key (namespace prefix removed)
//  3. Boosted fields (name/title/description/summary/label) — each repeated ×2
//     so their cosine-distance weight dominates over incidental properties.
//  4. Remaining string/numeric/bool property values (boosted keys skipped here).
//
// Skips: IDs, UUIDs, URLs, internal metadata keys (skipEmbeddingKey), large numerics.
// Output is capped at maxEmbeddingChars characters (word-boundary truncation).
func (w *GraphEmbeddingWorker) extractText(obj *graphObjectRow) string {
	var tokens []string

	tokens = append(tokens, obj.Type)

	// Strip namespace prefix from key (e.g. "ns:sarah" → "sarah")
	if obj.Key != nil && *obj.Key != "" {
		k := *obj.Key
		if i := strings.LastIndex(k, ":"); i >= 0 {
			k = k[i+1:]
		}
		if k != "" && !isUUID(k) {
			tokens = append(tokens, k)
		}
	}

	// isEmbeddableString returns true for non-empty, non-UUID, non-URL strings.
	isEmbeddableString := func(s string) bool {
		if s == "" || isUUID(s) {
			return false
		}
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			return false
		}
		return true
	}

	// Phase 1: boosted keys — prepend each value twice to increase embedding weight.
	for _, bk := range boostedEmbeddingKeys {
		v, ok := obj.Properties[bk]
		if !ok {
			continue
		}
		if s, ok := v.(string); ok && isEmbeddableString(s) {
			tokens = append(tokens, s, s) // ×2 repetition
		}
	}

	// Phase 2: remaining properties (boosted keys skipped to avoid triple-counting).
	boostedSet := make(map[string]bool, len(boostedEmbeddingKeys))
	for _, bk := range boostedEmbeddingKeys {
		boostedSet[bk] = true
	}

	var walk func(key string, v interface{})
	walk = func(key string, v interface{}) {
		if v == nil {
			return
		}
		lk := strings.ToLower(key)
		if skipEmbeddingKey[lk] || boostedSet[lk] {
			return
		}
		switch val := v.(type) {
		case string:
			if isEmbeddableString(val) {
				tokens = append(tokens, val)
			}
		case float64:
			// Skip large numeric IDs (>= 1e9); include small counts/years
			if val < 1e9 {
				tokens = append(tokens, fmt.Sprintf("%v", val))
			}
		case bool:
			tokens = append(tokens, fmt.Sprintf("%v", val))
		case []interface{}:
			for _, x := range val {
				walk(key, x)
			}
		case map[string]interface{}:
			for k, x := range val {
				walk(k, x)
			}
		}
	}

	for k, v := range obj.Properties {
		walk(k, v)
	}

	full := strings.Join(tokens, " ")

	// Apply character budget — truncate at word boundary.
	if len(full) <= maxEmbeddingChars {
		return full
	}
	truncated := full[:maxEmbeddingChars]
	if i := strings.LastIndex(truncated, " "); i > 0 {
		truncated = truncated[:i]
	}
	return truncated + "..."
}

// vectorToString converts a float32 slice to PostgreSQL vector string format
func vectorToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	result := "["
	for i, val := range v {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%f", val)
	}
	result += "]"
	return result
}

// incrementSuccess increments both processed and success counters
func (w *GraphEmbeddingWorker) incrementSuccess() {
	w.metricsMu.Lock()
	w.processedCount++
	w.successCount++
	w.metricsMu.Unlock()
}

// incrementFailure increments both processed and failure counters
func (w *GraphEmbeddingWorker) incrementFailure() {
	w.metricsMu.Lock()
	w.processedCount++
	w.failureCount++
	w.metricsMu.Unlock()
}

// Metrics returns current worker metrics
func (w *GraphEmbeddingWorker) Metrics() GraphEmbeddingWorkerMetrics {
	w.metricsMu.RLock()
	defer w.metricsMu.RUnlock()

	return GraphEmbeddingWorkerMetrics{
		Processed: w.processedCount,
		Succeeded: w.successCount,
		Failed:    w.failureCount,
	}
}

// GraphEmbeddingWorkerMetrics contains worker metrics
type GraphEmbeddingWorkerMetrics struct {
	Processed int64 `json:"processed"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
}

// IsRunning returns whether the worker is currently running
func (w *GraphEmbeddingWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// Pause suspends job processing without stopping the worker goroutine.
func (w *GraphEmbeddingWorker) Pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused = true
	w.log.Info("graph embedding worker paused")
}

// Resume resumes job processing after a Pause.
func (w *GraphEmbeddingWorker) Resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused = false
	w.log.Info("graph embedding worker resumed")
}

// IsPaused returns whether the worker is currently paused.
func (w *GraphEmbeddingWorker) IsPaused() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.paused
}

// GetConfig returns a copy of the current worker configuration.
func (w *GraphEmbeddingWorker) GetConfig() GraphEmbeddingConfig {
	w.mu.Lock()
	defer w.mu.Unlock()
	return *w.cfg
}

// SetConfig updates the worker configuration at runtime.
// Changes take effect on the next poll cycle.
func (w *GraphEmbeddingWorker) SetConfig(cfg GraphEmbeddingConfig) {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.cfg = cfg

	if w.scaler != nil {
		w.scaler.UpdateConfig(cfg.EnableAdaptiveScaling, cfg.MinConcurrency, cfg.MaxConcurrency)
	}

	w.log.Info("graph embedding worker config updated",
		slog.Int("batch_size", cfg.WorkerBatchSize),
		slog.Int("concurrency", cfg.WorkerConcurrency),
		slog.Int("interval_ms", cfg.WorkerIntervalMs),
	)
}

// isPermanentEmbeddingError returns true for errors that will never succeed
// on retry regardless of how many times we try. These include:
//   - HTTP 400 Bad Request: invalid model name, malformed request
//   - HTTP 401 Unauthorized: invalid API key / service account credentials
//   - HTTP 403 Forbidden: credentials exist but lack required IAM permissions
//   - HTTP 404 Not Found: model or endpoint does not exist in this project/region
//
// Transient errors (rate limits 429, server errors 5xx, network timeouts)
// are NOT permanent and should continue retrying.
func isPermanentEmbeddingError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// HTTP client errors from the model provider (invalid model/creds) are
	// permanent — retrying will never succeed.
	for _, code := range []string{"API error 400", "API error 401", "API error 403", "API error 404"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	// Missing embedding model configuration is a permanent config error, not a
	// transient network/quota failure.
	if strings.Contains(msg, "no embedding model configured") {
		return true
	}
	return false
}
