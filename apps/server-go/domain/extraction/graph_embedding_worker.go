package extraction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/embeddings/vertex"
	"github.com/emergent-company/emergent/pkg/logger"
)

// EmbeddingService is the interface for embedding services used by the worker.
// This allows for dependency injection and testing.
type EmbeddingService interface {
	IsEnabled() bool
	EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error)
}

// GraphEmbeddingWorker processes graph embedding jobs from the queue.
// It follows the same pattern as NestJS workers:
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
	stopCh    chan struct{}
	stoppedCh chan struct{}
	running   bool
	paused    bool
	mu        sync.Mutex
	wg        sync.WaitGroup

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
) *GraphEmbeddingWorker {
	return &GraphEmbeddingWorker{
		jobs:   jobs,
		embeds: embeds,
		db:     db,
		cfg:    cfg,
		log:    log.With(logger.Scope("graph.embedding.worker")),
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

	for _, job := range jobs {
		if err := w.processJob(ctx, job); err != nil {
			w.log.Warn("process job failed",
				slog.String("job_id", job.ID),
				slog.String("error", err.Error()))
		}
	}

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
	startTime := time.Now()

	// Fetch the graph object
	obj := &graphObjectRow{}
	err := w.db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id", "type", "key", "properties", "project_id").
		Where("id = ?", job.ObjectID).
		Scan(ctx, obj)

	if err == sql.ErrNoRows {
		// Object doesn't exist, mark as failed with a short error
		if markErr := w.jobs.MarkFailed(ctx, job.ID, fmt.Errorf("object_missing")); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return fmt.Errorf("object not found: %s", job.ObjectID)
	}
	if err != nil {
		// Database error
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return fmt.Errorf("fetch object: %w", err)
	}

	// Extract text for embedding
	text := w.extractText(obj)
	textLength := len(text)

	// Generate embedding
	embeddingStartTime := time.Now()
	result, err := w.embeds.EmbedQueryWithUsage(ctx, text)
	embeddingDurationMs := time.Since(embeddingStartTime).Milliseconds()

	if err != nil {
		// Embedding failed
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return fmt.Errorf("generate embedding: %w", err)
	}

	if result == nil || len(result.Embedding) == 0 {
		// No embedding returned (likely noop client)
		err := fmt.Errorf("no embedding returned")
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return err
	}

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
		return err
	}

	totalDurationMs := time.Since(startTime).Milliseconds()

	w.log.Debug("generated embedding for graph object",
		slog.String("object_id", obj.ID),
		slog.String("object_type", obj.Type),
		slog.Int("embedding_dims", len(result.Embedding)),
		slog.Int("text_length", textLength),
		slog.Int64("embedding_duration_ms", embeddingDurationMs),
		slog.Int64("total_duration_ms", totalDurationMs))

	w.incrementSuccess()
	return nil
}

// extractText extracts text from a graph object for embedding.
// Follows the same heuristic as NestJS: join type, key, and all primitive leaf values.
func (w *GraphEmbeddingWorker) extractText(obj *graphObjectRow) string {
	tokens := []string{obj.Type}
	if obj.Key != nil {
		tokens = append(tokens, *obj.Key)
	}

	// Walk properties recursively
	var walk func(v interface{})
	walk = func(v interface{}) {
		if v == nil {
			return
		}
		switch val := v.(type) {
		case string:
			tokens = append(tokens, val)
		case float64:
			tokens = append(tokens, fmt.Sprintf("%v", val))
		case bool:
			tokens = append(tokens, fmt.Sprintf("%v", val))
		case []interface{}:
			for _, x := range val {
				walk(x)
			}
		case map[string]interface{}:
			for _, x := range val {
				walk(x)
			}
		}
	}
	walk(obj.Properties)

	// Join with spaces
	result := ""
	for i, token := range tokens {
		if i > 0 {
			result += " "
		}
		result += token
	}
	return result
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
