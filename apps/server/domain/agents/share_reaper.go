package agents

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	// defaultShareApprovalTimeout is the fallback TTL for cancelling suspended
	// (input-required) share runs when a link has no explicit config.
	defaultShareApprovalTimeout = 600 * time.Second

	// shareRunReaperInterval is how often the reaper scans for stale share runs.
	shareRunReaperInterval = time.Minute
)

// ShareRunReaper periodically cancels suspended (input-required) share runs
// whose approval window has lapsed, reusing CancelPendingQuestionsForRun +
// CancelRun so questions and run status are transitioned consistently.
type ShareRunReaper struct {
	repo    *Repository
	log     *slog.Logger
	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
	running bool
}

// NewShareRunReaper creates a ShareRunReaper.
func NewShareRunReaper(repo *Repository, log *slog.Logger) *ShareRunReaper {
	return &ShareRunReaper{
		repo:   repo,
		log:    log.With("component", "share-run-reaper"),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start begins the periodic reaper goroutine.
func (r *ShareRunReaper) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	go func() {
		defer close(r.doneCh)
		ticker := time.NewTicker(shareRunReaperInterval)
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
func (r *ShareRunReaper) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	close(r.stopCh)
	<-r.doneCh
}

// reap finds and cancels stale suspended share runs.
func (r *ShareRunReaper) reap(ctx context.Context) {
	candidates, err := r.repo.ListPausedShareRuns(ctx)
	if err != nil {
		r.log.Warn("failed to list paused share runs", slog.String("error", err.Error()))
		return
	}

	linkTimeout := make(map[string]time.Duration)
	for _, c := range candidates {
		timeout, ok := linkTimeout[c.ShareLinkID]
		if !ok {
			timeout = defaultShareApprovalTimeout
			if link, lerr := r.repo.GetShareLinkByID(ctx, c.ShareLinkID, nil); lerr == nil && link != nil {
				if t := link.EffectiveConfig().ApprovalTimeoutSeconds; t > 0 {
					timeout = time.Duration(t) * time.Second
				}
			}
			linkTimeout[c.ShareLinkID] = timeout
		}
		if c.LastActivity == nil || time.Since(*c.LastActivity) < timeout {
			continue
		}

		// Cancel the run only if it is STILL paused (input-required). A visitor may
		// have approved in between; the conditional update skips resumed runs.
		cancelled, err := r.repo.CancelRunIfPaused(ctx, c.RunID)
		if err != nil {
			r.log.Warn("failed to cancel stale share run",
				slog.String("run_id", c.RunID),
				slog.String("error", err.Error()),
			)
			continue
		}
		if !cancelled {
			// Run was resumed (or already cancelled) — skip questions too.
			continue
		}
		_ = r.repo.CancelPendingQuestionsForRun(ctx, c.RunID)
		r.log.Info("cancelled stale share run (approval timeout)",
			slog.String("run_id", c.RunID),
			slog.String("share_link_id", c.ShareLinkID),
		)
	}
}
