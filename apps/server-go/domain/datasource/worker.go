package datasource

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/emergent-company/emergent/pkg/encryption"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Worker processes data source sync jobs from the queue.
// It follows the same pattern as other workers in the extraction domain.
type Worker struct {
	jobs       *JobsService
	registry   *ProviderRegistry
	encryption *encryption.Service
	cfg        *Config
	log        *slog.Logger
	stopCh     chan struct{}
	stopped    chan struct{}
	running    bool
	mu         sync.Mutex
	wg         sync.WaitGroup

	// Metrics
	processedCount   int64
	successCount     int64
	failureCount     int64
	deadLetterCount  int64
	metricsMu        sync.RWMutex
}

// NewWorker creates a new data source sync worker
func NewWorker(jobs *JobsService, registry *ProviderRegistry, enc *encryption.Service, cfg *Config, log *slog.Logger) *Worker {
	return &Worker{
		jobs:       jobs,
		registry:   registry,
		encryption: enc,
		cfg:        cfg,
		log:        log.With(logger.Scope("datasource.worker")),
	}
}

// Start begins the worker's polling loop
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	if !w.cfg.Enabled {
		w.log.Info("data source sync worker not started (disabled)")
		w.mu.Unlock()
		return nil
	}

	w.running = true
	w.stopCh = make(chan struct{})
	w.stopped = make(chan struct{})
	w.mu.Unlock()

	// Recover stale jobs on startup
	go w.recoverStaleJobsOnStartup(ctx)

	w.log.Info("data source sync worker starting",
		slog.Duration("poll_interval", w.cfg.WorkerInterval()),
		slog.Int("batch_size", w.cfg.WorkerBatchSize))

	w.wg.Add(1)
	go w.run(ctx)

	return nil
}

// Stop gracefully stops the worker
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	w.log.Debug("waiting for data source sync worker to stop...")

	select {
	case <-w.stopped:
		w.log.Info("data source sync worker stopped gracefully")
	case <-ctx.Done():
		w.log.Warn("data source sync worker stop timeout")
	}

	return nil
}

// recoverStaleJobsOnStartup recovers jobs stuck in running state
func (w *Worker) recoverStaleJobsOnStartup(ctx context.Context) {
	recovered, err := w.jobs.RecoverStaleJobs(ctx, w.cfg.StaleJobMinutes)
	if err != nil {
		w.log.Warn("failed to recover stale jobs",
			slog.String("error", err.Error()))
		return
	}
	if recovered > 0 {
		w.log.Info("recovered stale sync jobs on startup",
			slog.Int("count", recovered))
	}
}

// run is the main worker loop
func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()
	defer close(w.stopped)

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
				w.log.Warn("process batch failed",
					slog.String("error", err.Error()))
			}
		}
	}
}

// processBatch processes a batch of sync jobs
func (w *Worker) processBatch(ctx context.Context) error {
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

	for _, job := range jobs {
		if err := w.processJob(ctx, job); err != nil {
			w.log.Warn("process job failed",
				slog.String("job_id", job.ID),
				slog.String("error", err.Error()))
		}
	}

	return nil
}

// processJob processes a single sync job
func (w *Worker) processJob(ctx context.Context, job *DataSourceSyncJob) error {
	startTime := time.Now()
	w.log.Info("processing sync job",
		slog.String("job_id", job.ID),
		slog.String("integration_id", job.IntegrationID))

	// Get the integration
	integration, err := w.jobs.GetIntegration(ctx, job.IntegrationID)
	if err != nil {
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return err
	}

	// Get the provider
	provider, ok := w.registry.Get(integration.ProviderType)
	if !ok {
		err := &syncError{message: "provider not found: " + integration.ProviderType}
		if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}
		w.incrementFailure()
		return err
	}

	// Update phase to syncing
	if err := w.jobs.MarkRunning(ctx, job.ID, "syncing"); err != nil {
		w.log.Warn("failed to update job phase",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()))
	}

	// Decrypt the integration config
	var config map[string]interface{}
	if integration.ConfigEncrypted != nil && *integration.ConfigEncrypted != "" {
		w.log.Debug("decrypting integration configuration",
			slog.String("integration_id", integration.ID))
		var err error
		config, err = w.encryption.Decrypt(ctx, *integration.ConfigEncrypted)
		if err != nil {
			w.log.Error("failed to decrypt integration config",
				slog.String("integration_id", integration.ID),
				slog.String("error", err.Error()))
			if markErr := w.jobs.MarkFailed(ctx, job.ID, err); markErr != nil {
				w.log.Error("failed to mark job as failed",
					slog.String("job_id", job.ID),
					slog.String("error", markErr.Error()))
			}
			w.incrementFailure()
			return err
		}
		w.log.Debug("configuration decrypted successfully")
	} else {
		config = make(map[string]interface{})
	}

	// Build provider config
	providerConfig := ProviderConfig{
		IntegrationID: integration.ID,
		ProjectID:     integration.ProjectID,
		Config:        config,
		Metadata:      integration.Metadata,
	}

	// Build sync options
	syncOptions := SyncOptions{
		Custom: job.SyncOptions,
	}
	if job.ConfigurationID != nil {
		syncOptions.ConfigurationID = *job.ConfigurationID
	}

	// Progress callback
	progressCallback := func(p Progress) {
		// Update job progress
		if err := w.jobs.UpdateProgress(ctx, job.ID,
			p.TotalItems,
			p.ProcessedItems,
			p.SuccessfulItems,
			p.FailedItems,
			p.SkippedItems,
			p.Phase,
			p.Message,
		); err != nil {
			w.log.Warn("failed to update job progress",
				slog.String("job_id", job.ID),
				slog.String("error", err.Error()))
		}
	}

	// Run the sync
	result, err := provider.Sync(ctx, providerConfig, syncOptions, progressCallback)
	if err != nil {
		// Update integration status
		errMsg := err.Error()
		if updateErr := w.jobs.UpdateIntegrationSyncStatus(ctx,
			integration.ID, time.Now(), nil, IntegrationStatusError, &errMsg); updateErr != nil {
			w.log.Warn("failed to update integration status",
				slog.String("integration_id", integration.ID),
				slog.String("error", updateErr.Error()))
		}

		// Use retry logic with dead letter handling
		willRetry, markErr := w.jobs.MarkFailedWithRetry(ctx, job.ID, err, job.RetryCount, job.MaxRetries)
		if markErr != nil {
			w.log.Error("failed to mark job as failed",
				slog.String("job_id", job.ID),
				slog.String("error", markErr.Error()))
		}

		if !willRetry {
			w.incrementDeadLetter()
		} else {
			w.incrementFailure()
		}
		return err
	}

	// Update final progress
	if err := w.jobs.UpdateProgress(ctx, job.ID,
		result.TotalItems,
		result.ProcessedItems,
		result.SuccessfulItems,
		result.FailedItems,
		result.SkippedItems,
		"completed",
		"Sync completed successfully",
	); err != nil {
		w.log.Warn("failed to update final progress",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()))
	}

	// Mark job completed
	if err := w.jobs.MarkCompleted(ctx, job.ID); err != nil {
		w.log.Error("failed to mark job as completed",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()))
		return err
	}

	// Update integration status
	var nextSync *time.Time
	if integration.SyncMode == SyncModeRecurring && integration.SyncIntervalMinutes != nil {
		next := time.Now().Add(time.Duration(*integration.SyncIntervalMinutes) * time.Minute)
		nextSync = &next
	}
	if err := w.jobs.UpdateIntegrationSyncStatus(ctx,
		integration.ID, time.Now(), nextSync, IntegrationStatusActive, nil); err != nil {
		w.log.Warn("failed to update integration status",
			slog.String("integration_id", integration.ID),
			slog.String("error", err.Error()))
	}

	durationMs := time.Since(startTime).Milliseconds()
	w.log.Info("sync job completed",
		slog.String("job_id", job.ID),
		slog.String("integration_id", job.IntegrationID),
		slog.Int("total", result.TotalItems),
		slog.Int("successful", result.SuccessfulItems),
		slog.Int("failed", result.FailedItems),
		slog.Int64("duration_ms", durationMs))

	w.incrementSuccess()
	return nil
}

// incrementSuccess increments success metrics
func (w *Worker) incrementSuccess() {
	w.metricsMu.Lock()
	w.processedCount++
	w.successCount++
	w.metricsMu.Unlock()
}

// incrementFailure increments failure metrics
func (w *Worker) incrementFailure() {
	w.metricsMu.Lock()
	w.processedCount++
	w.failureCount++
	w.metricsMu.Unlock()
}

// incrementDeadLetter increments dead letter metrics
func (w *Worker) incrementDeadLetter() {
	w.metricsMu.Lock()
	w.processedCount++
	w.deadLetterCount++
	w.metricsMu.Unlock()
}

// Metrics returns current worker metrics
func (w *Worker) Metrics() WorkerMetrics {
	w.metricsMu.RLock()
	defer w.metricsMu.RUnlock()

	return WorkerMetrics{
		Processed:  w.processedCount,
		Succeeded:  w.successCount,
		Failed:     w.failureCount,
		DeadLetter: w.deadLetterCount,
	}
}

// WorkerMetrics contains worker metrics
type WorkerMetrics struct {
	Processed  int64 `json:"processed"`
	Succeeded  int64 `json:"succeeded"`
	Failed     int64 `json:"failed"`
	DeadLetter int64 `json:"deadLetter"`
}

// IsRunning returns whether the worker is running
func (w *Worker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// syncError is a simple error type for sync failures
type syncError struct {
	message string
}

func (e *syncError) Error() string {
	return e.message
}
