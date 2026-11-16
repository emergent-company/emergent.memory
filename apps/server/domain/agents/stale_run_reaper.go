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

	// defaultRunHeartbeatInterval is how often a live executor refreshes its
	// run's last_step_at via StartRunHeartbeat. Kept well below
	// staleRunThreshold (15 ticks per window) so a run whose executor goroutine
	// is alive is never reaped, even across long blocking phases such as
	// workspace provisioning/build or a slow in-flight model/tool call.
	defaultRunHeartbeatInterval = 2 * time.Minute

	// queuedRunDispatchThreshold is how long a queued ("submitted") run may sit
	// with no dispatch job any worker can claim before the reaper fails it
	// (issue #880). It matches the stale-run window: a run that has been idle
	// this long was never dispatched at all, not merely waiting in line.
	queuedRunDispatchThreshold = 30 * time.Minute
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

// reap finds and marks stale runs. Each sweep is independent: a failure in one
// must not starve the others, because a database error in the running-run sweep
// has nothing to do with whether queued or cancelling runs can be finalized.
func (r *StaleRunReaper) reap(ctx context.Context) {
	if n, err := r.repo.MarkStaleRunsAsError(ctx, staleRunThreshold); err != nil {
		r.log.Warn("failed to mark stale runs",
			slog.String("error", err.Error()),
		)
	} else if n > 0 {
		r.log.Info("marked stale agent runs as error",
			slog.Int("count", n),
		)
	}

	// A queued ("submitted") run whose dispatch job no worker can ever claim has
	// nothing left to drive it: a job whose queue row is missing, or with a NULL
	// project_id, never matches a claim. Neither the running-run sweep above nor
	// work maintenance (which counts "submitted" as live) will ever terminate it,
	// so no error would surface and the run would sit non-terminal forever
	// (issue #880).
	//
	// Heal first, then fail: a queued run that merely lost its job row is
	// re-enqueued — the same recovery the startup hook performs — and only a run
	// that still has no claimable job after being idle past the threshold is
	// failed. This covers fan-out children and session/A2A turns too, because
	// their shape is reachable by the same defect; the repository declines to act
	// on one a worker already claimed and reports it as spared instead (issue
	// #907). A run whose queue row exists but is disabled is spared: disabling is
	// reversible operator intent and re-enabling must bring the backlog back.
	if k, err := r.repo.RequeueOrphanedQueuedRuns(ctx); err != nil {
		r.log.Warn("failed to requeue orphaned queued runs",
			slog.String("error", err.Error()),
		)
	} else if k > 0 {
		r.log.Info("re-enqueued orphaned queued agent runs",
			slog.Int("count", k),
		)
	}
	if res, err := r.repo.ReapUndispatchableQueuedRuns(ctx, queuedRunDispatchThreshold); err != nil {
		r.log.Warn("failed to reap undispatchable queued runs",
			slog.String("error", err.Error()),
		)
	} else {
		if res.Failed > 0 {
			r.log.Info("marked undispatchable queued agent runs as error",
				slog.Int("count", res.Failed),
			)
		}
		if res.SparedDisabled > 0 {
			r.log.Info("spared queued agent runs parked on disabled queues",
				slog.Int("count", res.SparedDisabled),
			)
		}
		if res.SparedExecuted > 0 {
			// Residue, not a silent spare: a run whose lifecycle a parent or a
			// session owns and that a worker already claimed cannot be
			// re-enqueued without risking a second turn (issue #907).
			r.log.Info("spared queued agent runs already claimed by a worker",
				slog.Int("count", res.SparedExecuted),
			)
		}
	}

	// A durable cancel whose executing instance never observed it (the process
	// died, or a queued run was cancelled before it was claimed) must not sit in
	// the intermediate "cancelling" state forever. Finalize it as cancelled once
	// it has been idle past the same threshold; the sweep is heartbeat-aware, so
	// a run still owned by a live executor is spared. A committed cancel always
	// resolves to cancelled, never to a failure (issue #1166).
	if m, err := r.repo.FinalizeOrphanedCancellingRuns(ctx); err != nil {
		r.log.Warn("failed to finalize stale cancelling runs",
			slog.String("error", err.Error()),
		)
	} else if m > 0 {
		r.log.Info("finalized stale cancelling agent runs as cancelled",
			slog.Int("count", m),
		)
	}
}
