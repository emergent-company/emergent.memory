package agents

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func watchdogTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// manualClock is a deterministic watchdogClock for tests. Logical time advances
// only when Advance is called, and timers fire exactly when that logical clock
// reaches their deadline. This removes the wall-clock race that made
// TestStepWatchdogProgressKeepsRunAlive flaky under a loaded test run: a
// progress signal that correctly resets the timer moves the deadline forward,
// so the next Advance cannot fire it, regardless of goroutine scheduling.
type manualClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*manualTimer
}

func newManualClock() *manualClock { return &manualClock{} }

func (c *manualClock) AfterFunc(d time.Duration, f func()) watchdogTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &manualTimer{clock: c, callback: f, deadline: c.now + d, active: true}
	c.timers = append(c.timers, t)
	return t
}

// Advance moves the logical clock forward by d and fires every timer whose
// deadline has been reached. Callbacks run synchronously and outside the lock.
func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	var due []*manualTimer
	for _, t := range c.timers {
		if t.active && t.deadline <= c.now {
			t.active = false
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.callback()
	}
}

type manualTimer struct {
	clock    *manualClock
	callback func()

	deadline time.Duration
	active   bool
}

func (t *manualTimer) Reset(d time.Duration) bool {
	c := t.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	wasActive := t.active
	t.deadline = c.now + d
	t.active = true
	return wasActive
}

func (t *manualTimer) Stop() bool {
	c := t.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	wasActive := t.active
	t.active = false
	return wasActive
}

// TestStepWatchdogKillsNoProgressStep verifies the core of issue #1072 (a): a
// step that makes no progress is terminated once the per-step budget elapses.
func TestStepWatchdogKillsNoProgressStep(t *testing.T) {
	var reason string
	clock := newManualClock()
	stepBudget := 30 * time.Second
	w := newStepWatchdogWithClock(context.Background(), stepBudget, &reason, watchdogTestLogger(), "run-1", clock)
	defer w.stop()

	if err := w.ctx.Err(); err != nil {
		t.Fatalf("watchdog cancelled before the budget elapsed: %v", err)
	}

	clock.Advance(stepBudget)

	if err := w.ctx.Err(); err == nil {
		t.Fatal("watchdog did not cancel a no-progress step once the budget elapsed")
	}
	if !strings.Contains(reason, "step watchdog") {
		t.Fatalf("cancellation reason %q does not identify the step watchdog", reason)
	}
}

// TestStepWatchdogProgressKeepsRunAlive verifies issue #1072 (b): a run that
// keeps making steady progress survives well past the old fixed wall-clock
// deadline (here simulated as 8x the per-step budget). Progress is delivered
// explicitly against a manual clock, so the assertion does not depend on
// scheduler timing.
func TestStepWatchdogProgressKeepsRunAlive(t *testing.T) {
	var reason string
	stepBudget := 30 * time.Second
	clock := newManualClock()
	w := newStepWatchdogWithClock(context.Background(), stepBudget, &reason, watchdogTestLogger(), "run-2", clock)
	defer w.stop()

	// Simulate a long multi-step run: advance the logical clock by less than the
	// budget, then report progress, for a total span beyond 8x the budget (and
	// well beyond 4x, the stand-in for the old fixed deadline). A watchdog that
	// honours progress resets its deadline each time, so it never fires.
	steps := 0
	for elapsed := time.Duration(0); elapsed < 8*stepBudget; elapsed += stepBudget / 2 {
		clock.Advance(stepBudget / 2)
		w.progress()
		steps++
	}

	if steps < 4 {
		t.Fatalf("expected at least 4 progress signals, got %d", steps)
	}
	if err := w.ctx.Err(); err != nil {
		t.Fatalf("progressing run was cancelled after %d steps: %v", steps, err)
	}

	// Once progress stops, the watchdog must still fire after one more budget.
	clock.Advance(stepBudget)

	if err := w.ctx.Err(); err == nil {
		t.Fatal("watchdog did not cancel after progress stopped")
	}
	if !strings.Contains(reason, "step watchdog") {
		t.Fatalf("cancellation reason %q does not identify the step watchdog", reason)
	}
}

// TestStepWatchdogStopDisarmsTimer verifies Stop is idempotent and prevents a
// later fire from cancelling the context.
func TestStepWatchdogStopDisarmsTimer(t *testing.T) {
	var reason string
	clock := newManualClock()
	w := newStepWatchdogWithClock(context.Background(), 20*time.Second, &reason, watchdogTestLogger(), "run-3", clock)

	w.stop()
	w.stop() // idempotent

	clock.Advance(time.Hour)

	if w.ctx.Err() != nil {
		t.Fatal("stopped watchdog still cancelled the context")
	}
}
