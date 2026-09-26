package agents

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func watchdogTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestStepWatchdogKillsNoProgressStep verifies the core of issue #1072 (a): a
// step that makes no progress is terminated once the per-step budget elapses.
func TestStepWatchdogKillsNoProgressStep(t *testing.T) {
	var reason string
	w := newStepWatchdog(context.Background(), 50*time.Millisecond, &reason, watchdogTestLogger(), "run-1")
	defer w.stop()

	select {
	case <-w.ctx.Done():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not cancel a no-progress step within the budget")
	}

	if !strings.Contains(reason, "step watchdog") {
		t.Fatalf("cancellation reason %q does not identify the step watchdog", reason)
	}
}

// TestStepWatchdogProgressKeepsRunAlive verifies issue #1072 (b): a run that
// keeps making steady progress survives well past the old fixed wall-clock
// deadline (here simulated as 8x the per-step budget).
func TestStepWatchdogProgressKeepsRunAlive(t *testing.T) {
	var reason string
	stepBudget := 30 * time.Millisecond
	w := newStepWatchdog(context.Background(), stepBudget, &reason, watchdogTestLogger(), "run-2")
	defer w.stop()

	// Simulate a long multi-step run: one progress signal per step, for a total
	// duration well beyond any single budget (and beyond 4x the budget, the
	// stand-in for the old fixed deadline).
	deadline := time.After(8 * stepBudget)
	tick := time.NewTicker(stepBudget / 2)
	defer tick.Stop()

	steps := 0
	for {
		select {
		case <-deadline:
			if w.ctx.Err() != nil {
				t.Fatalf("progressing run was cancelled after %d steps: %v", steps, w.ctx.Err())
			}
			if steps < 4 {
				t.Fatalf("expected at least 4 progress signals, got %d", steps)
			}
			return
		case <-tick.C:
			w.progress()
			steps++
		case <-w.ctx.Done():
			t.Fatalf("progressing run was cancelled after %d steps despite steady progress", steps)
		}
	}
}

// TestStepWatchdogStopDisarmsTimer verifies Stop is idempotent and prevents a
// later fire from cancelling the context.
func TestStepWatchdogStopDisarmsTimer(t *testing.T) {
	var reason string
	w := newStepWatchdog(context.Background(), 20*time.Millisecond, &reason, watchdogTestLogger(), "run-3")

	w.stop()
	w.stop() // idempotent

	select {
	case <-w.ctx.Done():
		t.Fatal("stopped watchdog still cancelled the context")
	case <-time.After(80 * time.Millisecond):
		// expected: never fires
	}
}
