package extraction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/emergent-company/emergent/pkg/syshealth"
	"github.com/emergent-company/emergent/pkg/logger"
	"github.com/emergent-company/emergent/pkg/tracing"
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
) *ChunkEmbeddingWorker {
	return &ChunkEmbeddingWorker{
		jobs:   jobs,
		embeds: embeds,
		db:     db,
		cfg:    cfg,
		log:    log.With(logger.Scope("chunk.embedding.worker")),
		scaler: scaler,
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
	}

	return nil
}

// recoverStaleJobsOnStartup recovers stale jobs on startup
func (w *ChunkEmbeddingWorker) recoverStaleJobsOnStartup(ctx context.Context) {
	recovered, err := w.jobs.RecoverStaleJobs(ctx, 10)
	if err != nil {
		w.log.Warn("failed to recover stale jobs on startup",
			slog.String("error", err.Error()))
		return
	}
	if recovered > 0 {
		w.log.Info("recovered stale chunk embedding jobs on startup",
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
		}
	}
}

// processBatch processes a batch of chunk embedding jobs
func (w *ChunkEmbeddingWorker) processBatch(ctx context.Context) error {
	// Check if we should stop
	select {
	case <-w.stopCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
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
		go func(j *ChunkEmbeddingJob) {
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

// chunkRow represents the minimal data needed from a chunk for embedding
type chunkRow struct {
	ID         string `bun:"id,type:uuid"`
	DocumentID string `bun:"document_id,type:uuid"`
	ChunkIndex int    `bun:"chunk_index"`
	Text       string `bun:"text"`
}

// processJob processes a single chunk embedding job
func (w *ChunkEmbeddingWorker) processJob(ctx context.Context, job *ChunkEmbeddingJob) error {
	ctx, span := tracing.Start(ctx, "extraction.chunk_embedding",
		attribute.String("emergent.job.id", job.ID),
	)
	defer span.End()

	startTime := time.Now()

	// Fetch the chunk
	chunk := &chunkRow{}
	err := w.db.NewSelect().
		TableExpr("kb.chunks").
		Column("id", "document_id", "chunk_index", "text").
		Where("id = ?", job.ChunkID).
		Scan(ctx, chunk)

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

	// Generate embedding
	embeddingStartTime := time.Now()
	result, err := w.embeds.EmbedQueryWithUsage(ctx, chunk.Text)
	embeddingDurationMs := time.Since(embeddingStartTime).Milliseconds()

	if err != nil {
		// Embedding failed
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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

	// Update the chunk with the embedding
	// Note: embedding is vector(768), we need to use raw SQL for pgvector
	now := time.Now()
	_, err = w.db.NewRaw(`UPDATE kb.chunks
		SET embedding = ?::vector,
			updated_at = ?
		WHERE id = ?`,
		vectorToString(result.Embedding), now, job.ChunkID).Exec(ctx)

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
