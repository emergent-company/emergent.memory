package agents

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	// staleRunThreshold is how long a run can be in "running" status with no
	// step activity before the reaper marks it as errored.
	staleRunThreshold = 30 * time.Minute

	// staleRunReaperInterval is how often the reaper checks for stale runs.
	staleRunReaperInterval = 5 * time.Minute
)

// StaleRunReaper periodically scans for agent runs stuck in "running" status
// and marks them as errored. This handles cases where the CLI connection drops
// without a graceful close, leaving runs in "running" forever.
type StaleRunReaper struct {
	repo    *Repository
	log     *slog.Logger
	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
	running bool
}

// NewStaleRunReaper creates a new reaper.
func NewStaleRunReaper(repo *Repository, log *slog.Logger) *StaleRunReaper {
	return &StaleRunReaper{
		repo:   repo,
		log:    log.With("component", "stale-run-reaper"),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start begins the periodic reaper goroutine.
func (r *StaleRunReaper) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	go func() {
		defer close(r.doneCh)

		ticker := time.NewTicker(staleRunReaperInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				r.reap(ctx)
			case <-r.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the reaper to stop and waits for it to finish.
func (r *StaleRunReaper) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	close(r.stopCh)
	<-r.doneCh
}

// reap finds and marks stale runs.
func (r *StaleRunReaper) reap(ctx context.Context) {
	n, err := r.repo.MarkStaleRunsAsError(ctx, staleRunThreshold)
	if err != nil {
		r.log.Warn("failed to mark stale runs",
			slog.String("error", err.Error()),
		)
		return
	}
	if n > 0 {
		r.log.Info("marked stale agent runs as error",
			slog.Int("count", n),
		)
	}
}
