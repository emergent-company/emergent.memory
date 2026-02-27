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

	"github.com/emergent-company/emergent/pkg/logger"
	"github.com/emergent-company/emergent/pkg/syshealth"
	"github.com/emergent-company/emergent/pkg/tracing"
)

// graphRelationshipRow holds the minimal fields needed to generate a relationship embedding.
type graphRelationshipRow struct {
	ID      string `bun:"id,type:uuid"`
	Type    string `bun:"type"`
	SrcName string `bun:"src_name"`
	DstName string `bun:"dst_name"`
	SrcType string `bun:"src_type"`
	DstType string `bun:"dst_type"`
}

// GraphRelationshipEmbeddingWorker processes relationship embedding jobs from the queue.
// Follows the same pattern as GraphEmbeddingWorker.
type GraphRelationshipEmbeddingWorker struct {
	jobs      *GraphRelationshipEmbeddingJobsService
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

	// Metrics
	processedCount int64
	successCount   int64
	failureCount   int64
	metricsMu      sync.RWMutex
}

// NewGraphRelationshipEmbeddingWorker creates a new worker.
func NewGraphRelationshipEmbeddingWorker(
	jobs *GraphRelationshipEmbeddingJobsService,
	embeds EmbeddingService,
	db bun.IDB,
	cfg *GraphEmbeddingConfig,
	monitor syshealth.Monitor,
	log *slog.Logger,
) *GraphRelationshipEmbeddingWorker {
	scaler := syshealth.NewConcurrencyScaler(
		monitor,
		"graph_relationship_embedding",
		cfg.EnableAdaptiveScaling,
		cfg.MinConcurrency,
		cfg.MaxConcurrency,
	)

	return &GraphRelationshipEmbeddingWorker{
		jobs:   jobs,
		embeds: embeds,
		db:     db,
		cfg:    cfg,
		scaler: scaler,
		log:    log.With(logger.Scope("graph.rel.embedding.worker")),
	}
}

// Start begins the worker's polling loop.
func (w *GraphRelationshipEmbeddingWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	if !w.embeds.IsEnabled() {
		w.log.Info("graph relationship embedding worker not started (embeddings not enabled)")
		w.mu.Unlock()
		return nil
	}

	w.running = true
	w.stopCh = make(chan struct{})
	w.stoppedCh = make(chan struct{})
	w.mu.Unlock()

	go w.recoverStaleJobsOnStartup(ctx)

	w.log.Info("graph relationship embedding worker starting",
		slog.Duration("poll_interval", w.cfg.WorkerInterval()),
		slog.Int("batch_size", w.cfg.WorkerBatchSize))

	w.wg.Add(1)
	go w.run(ctx)

	return nil
}

// Stop gracefully stops the worker.
func (w *GraphRelationshipEmbeddingWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	select {
	case <-w.stoppedCh:
		w.log.Info("graph relationship embedding worker stopped gracefully")
	case <-ctx.Done():
		w.log.Warn("graph relationship embedding worker stop timeout, forcing shutdown")
	}
	return nil
}

func (w *GraphRelationshipEmbeddingWorker) recoverStaleJobsOnStartup(ctx context.Context) {
	recovered, err := w.jobs.RecoverStaleJobs(ctx, 10)
	if err != nil {
		w.log.Warn("failed to recover stale rel embedding jobs", slog.String("error", err.Error()))
		return
	}
	if recovered > 0 {
		w.log.Info("recovered stale relationship embedding jobs on startup", slog.Int("count", recovered))
	}
}

func (w *GraphRelationshipEmbeddingWorker) run(ctx context.Context) {
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
				w.log.Warn("rel embedding process batch failed", slog.String("error", err.Error()))
			}
		}
	}
}

func (w *GraphRelationshipEmbeddingWorker) processBatch(ctx context.Context) error {
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

	// Use adaptive scaler to determine concurrency based on system health
	concurrency := w.scaler.GetConcurrency(w.cfg.WorkerConcurrency)
	if concurrency <= 0 {
		concurrency = 10
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, job := range jobs {
		sem <- struct{}{}
		wg.Add(1)
		go func(j *GraphRelationshipEmbeddingJob) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := w.processJob(ctx, j); err != nil {
				w.log.Warn("rel embedding process job failed",
					slog.String("job_id", j.ID),
					slog.String("error", err.Error()))
			}
		}(job)
	}
	wg.Wait()
	return nil
}

// processJob generates and stores the embedding for a single relationship job.
func (w *GraphRelationshipEmbeddingWorker) processJob(ctx context.Context, job *GraphRelationshipEmbeddingJob) error {
	ctx, span := tracing.Start(ctx, "extraction.relationship_embedding",
		attribute.String("emergent.job.id", job.ID),
	)
	defer span.End()

	startTime := time.Now()

	// Fetch the relationship along with endpoint names for triplet text generation.
	// We join graph_objects twice to get src/dst display names (key or type).
	rel := &graphRelationshipRow{}
	err := w.db.NewRaw(`
		SELECT
			gr.id,
			gr.type,
			COALESCE(src.key, src.type) AS src_name,
			src.type                    AS src_type,
			COALESCE(dst.key, dst.type) AS dst_name,
			dst.type                    AS dst_type
		FROM kb.graph_relationships gr
		JOIN kb.graph_objects src ON src.canonical_id = gr.src_id AND src.supersedes_id IS NULL
		JOIN kb.graph_objects dst ON dst.canonical_id = gr.dst_id AND dst.supersedes_id IS NULL
		WHERE gr.id = ?`,
		job.RelationshipID,
	).Scan(ctx, rel)

	if err == sql.ErrNoRows {
		relErr := fmt.Errorf("relationship not found: %s", job.RelationshipID)
		span.RecordError(relErr)
		span.SetStatus(codes.Error, relErr.Error())
		markErr := w.jobs.MarkFailed(ctx, job.ID, fmt.Errorf("relationship_missing"))
		if markErr != nil {
			w.log.Error("failed to mark rel embedding job as failed", slog.String("job_id", job.ID), slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return relErr
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		markErr := w.jobs.MarkFailed(ctx, job.ID, err)
		if markErr != nil {
			w.log.Error("failed to mark rel embedding job as failed", slog.String("job_id", job.ID), slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return fmt.Errorf("fetch relationship: %w", err)
	}

	// Build triplet text: "SrcName REL_TYPE DstName"
	text := rel.SrcName + " " + rel.Type + " " + rel.DstName

	embeddingStartTime := time.Now()
	result, err := w.embeds.EmbedQueryWithUsage(ctx, text)
	embeddingDurationMs := time.Since(embeddingStartTime).Milliseconds()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		markErr := w.jobs.MarkFailed(ctx, job.ID, err)
		if markErr != nil {
			w.log.Error("failed to mark rel embedding job as failed", slog.String("job_id", job.ID), slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return fmt.Errorf("generate rel embedding: %w", err)
	}

	if result == nil || len(result.Embedding) == 0 {
		jobErr := fmt.Errorf("no embedding returned")
		span.RecordError(jobErr)
		span.SetStatus(codes.Error, jobErr.Error())
		markErr := w.jobs.MarkFailed(ctx, job.ID, jobErr)
		if markErr != nil {
			w.log.Error("failed to mark rel embedding job as failed", slog.String("job_id", job.ID), slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return jobErr
	}

	// Store the embedding on the relationship row.
	now := time.Now()
	_, err = w.db.NewRaw(`
		UPDATE kb.graph_relationships
		SET embedding = ?::vector,
		    embedding_updated_at = ?
		WHERE id = ?`,
		vectorToString(result.Embedding), now, job.RelationshipID,
	).Exec(ctx)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		markErr := w.jobs.MarkFailed(ctx, job.ID, err)
		if markErr != nil {
			w.log.Error("failed to mark rel embedding job as failed", slog.String("job_id", job.ID), slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return fmt.Errorf("update rel embedding: %w", err)
	}

	if err := w.jobs.MarkCompleted(ctx, job.ID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		w.log.Error("failed to mark rel embedding job as completed", slog.String("job_id", job.ID), slog.String("error", err.Error()))
		return err
	}

	totalDurationMs := time.Since(startTime).Milliseconds()
	w.log.Debug("generated embedding for graph relationship",
		slog.String("relationship_id", rel.ID),
		slog.String("type", rel.Type),
		slog.Int("embedding_dims", len(result.Embedding)),
		slog.Int64("embedding_duration_ms", embeddingDurationMs),
		slog.Int64("total_duration_ms", totalDurationMs))

	w.incrementSuccess()
	span.SetStatus(codes.Ok, "")
	return nil
}

func (w *GraphRelationshipEmbeddingWorker) incrementSuccess() {
	w.metricsMu.Lock()
	w.processedCount++
	w.successCount++
	w.metricsMu.Unlock()
}

func (w *GraphRelationshipEmbeddingWorker) incrementFailure() {
	w.metricsMu.Lock()
	w.processedCount++
	w.failureCount++
	w.metricsMu.Unlock()
}

// IsRunning returns whether the worker is currently running.
func (w *GraphRelationshipEmbeddingWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// Pause suspends job processing without stopping the worker goroutine.
func (w *GraphRelationshipEmbeddingWorker) Pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused = true
	w.log.Info("graph relationship embedding worker paused")
}

// Resume resumes job processing after a Pause.
func (w *GraphRelationshipEmbeddingWorker) Resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused = false
	w.log.Info("graph relationship embedding worker resumed")
}

// IsPaused returns whether the worker is currently paused.
func (w *GraphRelationshipEmbeddingWorker) IsPaused() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.paused
}

// GetConfig returns a copy of the current worker configuration.
func (w *GraphRelationshipEmbeddingWorker) GetConfig() GraphEmbeddingConfig {
	w.mu.Lock()
	defer w.mu.Unlock()
	return *w.cfg
}

// SetConfig updates the worker configuration at runtime.
// Changes take effect on the next poll cycle.
func (w *GraphRelationshipEmbeddingWorker) SetConfig(cfg GraphEmbeddingConfig) {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.cfg = cfg

	if w.scaler != nil {
		w.scaler.UpdateConfig(cfg.EnableAdaptiveScaling, cfg.MinConcurrency, cfg.MaxConcurrency)
	}

	w.log.Info("graph relationship embedding worker config updated",
		slog.Int("batch_size", cfg.WorkerBatchSize),
		slog.Int("concurrency", cfg.WorkerConcurrency),
		slog.Int("interval_ms", cfg.WorkerIntervalMs),
	)
}
