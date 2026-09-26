package agents

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// stepWatchdog replaces the single fixed wall-clock run deadline with a
// resettable per-step budget (issue #1072). A run whose steps keep making
// progress is never killed by the aggregate deadline, while a step that makes
// no progress for longer than the budget is terminated.
//
// Progress is signalled from the executor callbacks (beforeModelCb / beforeToolCb
// / afterToolCb) and resets the timer. When the timer elapses without progress,
// the watchdog cancels the derived context and records a human-readable reason
// (via the shared reason pointer) before cancelling.
type stepWatchdog struct {
	ctx    context.Context
	cancel context.CancelFunc

	stepTimeout time.Duration
	reason      *string
	log         *slog.Logger
	runID       string

	mu    sync.Mutex
	timer *time.Timer
}

// newStepWatchdog derives a cancellable context from parent and arms a timer
// for stepTimeout. reason, when non-nil, receives the cancellation reason
// before the context is cancelled so the run can report a meaningful error.
func newStepWatchdog(parent context.Context, stepTimeout time.Duration, reason *string, log *slog.Logger, runID string) *stepWatchdog {
	ctx, cancel := context.WithCancel(parent)
	w := &stepWatchdog{
		ctx:         ctx,
		cancel:      cancel,
		stepTimeout: stepTimeout,
		reason:      reason,
		log:         log,
		runID:       runID,
	}
	w.timer = time.AfterFunc(stepTimeout, w.fire)
	return w
}

// progress resets the watchdog timer, signalling that the current step is
// still making headway.
func (w *stepWatchdog) progress() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Reset(w.stepTimeout)
	}
}

// stop disarms the watchdog. It is idempotent and safe to call after fire.
func (w *stepWatchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

// fire is the timer callback: record the reason, then cancel the run.
func (w *stepWatchdog) fire() {
	w.mu.Lock()
	if w.timer == nil {
		// Stop was called before the callback ran; nothing to do.
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	msg := fmt.Sprintf("agent stopped: step watchdog timeout — no progress within %s", w.stepTimeout)
	w.log.Warn("step watchdog fired: cancelling run",
		slog.String("run_id", w.runID),
		slog.Duration("step_timeout", w.stepTimeout),
	)
	if w.reason != nil {
		*w.reason = msg
	}
	w.cancel()
}
