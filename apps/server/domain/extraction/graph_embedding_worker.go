package extraction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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

// GraphEmbeddingWorker processes graph embedding jobs from the queue.
// Worker pattern:
// - Polling-based with configurable interval
// - Graceful shutdown waiting for current batch
// - Stale job recovery on startup
// - Metrics tracking
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
	w.mu.Unlock()

	// Recover stale jobs on startup
	go w.recoverStaleJobsOnStartup(ctx)

	w.log.Info("graph embedding worker starting",
		slog.Duration("poll_interval", w.cfg.WorkerInterval()),
		slog.Int("batch_size", w.cfg.WorkerBatchSize))

	w.wg.Add(1)
	go w.run(ctx)

	return nil
}

// Stop gracefully stops the worker, waiting for current batch to complete
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

	// Wait for worker to stop or context to be cancelled
	select {
	case <-w.stoppedCh:
		w.log.Info("graph embedding worker stopped gracefully")
	case <-ctx.Done():
		w.log.Warn("graph embedding worker stop timeout, forcing shutdown")
	}

	return nil
}

// recoverStaleJobsOnStartup recovers stale jobs on startup
func (w *GraphEmbeddingWorker) recoverStaleJobsOnStartup(ctx context.Context) {
	recovered, err := w.jobs.RecoverStaleJobs(ctx, 10)
	if err != nil {
		w.log.Warn("failed to recover stale jobs on startup",
			slog.String("error", err.Error()))
		return
	}
	if recovered > 0 {
		w.log.Info("recovered stale graph embedding jobs on startup",
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
		}
	}
}

// processBatch processes a batch of graph embedding jobs
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

	jobs, err := w.jobs.Dequeue(ctx, w.cfg.WorkerBatchSize)
	if err != nil {
		return err
	}

	if len(jobs) == 0 {
		return nil
	}

	concurrency := w.cfg.WorkerConcurrency
	if w.scaler != nil {
		concurrency = w.scaler.GetConcurrency(w.cfg.WorkerConcurrency)
	}
	if concurrency <= 0 {
		concurrency = 10
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, job := range jobs {
		sem <- struct{}{}
		wg.Add(1)
		go func(j *GraphEmbeddingJob) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := w.processJob(ctx, j); err != nil {
				w.log.Warn("process job failed",
					slog.String("job_id", j.ID),
					slog.String("error", err.Error()))
			}
		}(job)
	}
	wg.Wait()

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

// processJob processes a single graph embedding job
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
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
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
	if obj.ProjectID != "" {
		ctx = auth.ContextWithProjectID(ctx, obj.ProjectID)
	}

	// Budget pre-flight check — skip embedding when project has exceeded its monthly budget.
	if w.budget != nil && obj.ProjectID != "" {
		exceeded, err := w.budget.CheckBudgetExceeded(ctx, obj.ProjectID)
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
				WHERE id = ?`, job.ID).Exec(ctx); reschedErr != nil {
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
	result, err := w.embeds.EmbedQueryWithUsage(ctx, text)
	embeddingDurationMs := time.Since(embeddingStartTime).Milliseconds()

	if err != nil {
		// Embedding failed — distinguish permanent (bad model/creds) from transient (network, quota)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if isPermanentEmbeddingError(err) {
			w.log.Warn("graph embedding permanently failed (non-retryable error)",
				slog.String("job_id", job.ID),
				slog.String("object_id", job.ObjectID),
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
	orgID := w.orgCache.resolve(ctx, obj.ProjectID)
	recordEmbeddingUsage(w.usage, obj.ProjectID, orgID, result)

	// Update the graph object with the embedding
	// Note: embedding_v2 is vector(768), we need to use raw SQL for pgvector
	now := time.Now()
	_, err = w.db.NewRaw(`UPDATE kb.graph_objects
		SET embedding_v2 = ?::vector,
			embedding_updated_at = ?,
			updated_at = ?
		WHERE id = ?`,
		vectorToString(result.Embedding), now, now, job.ObjectID).Exec(ctx)

	if err != nil {
		// Update failed
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return fmt.Errorf("update embedding: %w", err)
	}

	// Mark job as completed
	if err := w.jobs.MarkCompleted(ctx, job.ID); err != nil {
		w.log.Error("failed to mark job as completed",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()))
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

	w.log.Debug("generated embedding for graph object",
		slog.String("object_id", obj.ID),
		slog.String("object_type", obj.Type),
		slog.Int("embedding_dims", len(result.Embedding)),
		slog.Int("text_length", textLength),
		slog.Int64("embedding_duration_ms", embeddingDurationMs),
		slog.Int64("total_duration_ms", totalDurationMs))

	w.incrementSuccess()
	span.SetStatus(codes.Ok, "")
	return nil
}

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
// Includes: type, stripped key (after last ':'), and meaningful string property values.
// Skips: IDs, UUIDs, URLs, internal metadata keys, and numeric-only values.
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

	// Walk properties with key-aware filtering
	var walk func(key string, v interface{})
	walk = func(key string, v interface{}) {
		if v == nil {
			return
		}
		if skipEmbeddingKey[strings.ToLower(key)] {
			return
		}
		switch val := v.(type) {
		case string:
			if val == "" || isUUID(val) {
				return
			}
			// Skip bare URLs
			if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
				return
			}
			tokens = append(tokens, val)
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

	return strings.Join(tokens, " ")
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
	// API error codes surfaced by the vertex client as "API error NNN: ..."
	for _, code := range []string{"API error 400", "API error 401", "API error 403", "API error 404"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}
