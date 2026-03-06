package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// WorkerConfig contains configuration for a background worker
type WorkerConfig struct {
	// Name is a descriptive name for the worker (for logging)
	Name string
	// PollInterval is how often to poll for new jobs (default: 5s)
	PollInterval time.Duration
	// BatchSize is the number of jobs to dequeue per poll (default: 10)
	BatchSize int
	// StaleThresholdMinutes is how long a job can be in 'processing' before
	// being considered stale and recovered (default: 10)
	StaleThresholdMinutes int
	// RecoverStaleOnStart determines if stale jobs should be recovered on startup
	RecoverStaleOnStart bool
}

// DefaultWorkerConfig returns a WorkerConfig with sensible defaults
func DefaultWorkerConfig(name string) WorkerConfig {
	return WorkerConfig{
		Name:                  name,
		PollInterval:          5 * time.Second,
		BatchSize:             10,
		StaleThresholdMinutes: 10,
		RecoverStaleOnStart:   true,
	}
}

// Worker is a background worker that processes jobs from a queue.
// It follows the same pattern as NestJS workers:
// - Polling-based with configurable interval
// - Graceful shutdown waiting for current batch
// - Stale job recovery on startup
// - Metrics tracking
type Worker struct {
	config     WorkerConfig
	log        *slog.Logger
	process    func(ctx context.Context) error
	stopCh     chan struct{}
	stoppedCh  chan struct{}
	running    bool
	mu         sync.Mutex
	wg         sync.WaitGroup

	// Metrics
	processedCount int64
	successCount   int64
	failureCount   int64
	metricsMu      sync.RWMutex
}

// NewWorker creates a new background worker
func NewWorker(config WorkerConfig, log *slog.Logger, process func(ctx context.Context) error) *Worker {
	// Apply defaults
	if config.PollInterval == 0 {
		config.PollInterval = 5 * time.Second
	}
	if config.BatchSize == 0 {
		config.BatchSize = 10
	}
	if config.StaleThresholdMinutes == 0 {
		config.StaleThresholdMinutes = 10
	}

	return &Worker{
		config:    config,
		log:       log.With(slog.String("worker", config.Name)),
		process:   process,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

// Start begins the worker's polling loop
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.stoppedCh = make(chan struct{})
	w.mu.Unlock()

	w.log.Info("worker starting",
		slog.Duration("poll_interval", w.config.PollInterval),
		slog.Int("batch_size", w.config.BatchSize))

	w.wg.Add(1)
	go w.run(ctx)

	return nil
}

// Stop gracefully stops the worker, waiting for current batch to complete
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	w.log.Debug("waiting for worker to stop...")

	// Wait for worker to stop or context to be cancelled
	select {
	case <-w.stoppedCh:
		w.log.Info("worker stopped gracefully")
	case <-ctx.Done():
		w.log.Warn("worker stop timeout, forcing shutdown")
	}

	return nil
}

// run is the main worker loop
func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()
	defer close(w.stoppedCh)

	ticker := time.NewTicker(w.config.PollInterval)
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

// processBatch processes a single batch of jobs
func (w *Worker) processBatch(ctx context.Context) error {
	// Check if we should stop
	select {
	case <-w.stopCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return w.process(ctx)
}

// Metrics returns current worker metrics
func (w *Worker) Metrics() WorkerMetrics {
	w.metricsMu.RLock()
	defer w.metricsMu.RUnlock()

	return WorkerMetrics{
		Processed: w.processedCount,
		Succeeded: w.successCount,
		Failed:    w.failureCount,
	}
}

// IncrementProcessed increments the processed counter
func (w *Worker) IncrementProcessed() {
	w.metricsMu.Lock()
	w.processedCount++
	w.metricsMu.Unlock()
}

// IncrementSuccess increments both processed and success counters
func (w *Worker) IncrementSuccess() {
	w.metricsMu.Lock()
	w.processedCount++
	w.successCount++
	w.metricsMu.Unlock()
}

// IncrementFailure increments both processed and failure counters
func (w *Worker) IncrementFailure() {
	w.metricsMu.Lock()
	w.processedCount++
	w.failureCount++
	w.metricsMu.Unlock()
}

// IsRunning returns whether the worker is currently running
func (w *Worker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// WorkerMetrics contains worker metrics
type WorkerMetrics struct {
	Processed int64 `json:"processed"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
}
