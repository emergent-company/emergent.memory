package agents

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// These tests exercise the run-lifetime teardown binding used by Execute,
// ExecuteWithRun, and Resume: each creates one *runCleanup, defers
// Cleanup(), and hands the same idempotent function to callers via
// ExecuteResult.Cleanup. The binding is what makes teardown guaranteed on
// every exit path, including a panic.

// A caller which never invokes Cleanup still gets teardown exactly once,
// because the executor defers Cleanup on the run lifetime.
func TestRunCleanup_DeferredRunsWithoutCallerInvocation(t *testing.T) {
	var calls atomic.Int64

	// Mirrors the executor pattern: create the binding, defer it, and return
	// without any caller invoking Cleanup.
	func() {
		cleanup := newRunCleanup(func() { calls.Add(1) })
		defer cleanup.Cleanup()
	}()

	assert.Equal(t, int64(1), calls.Load(), "deferred teardown must run once")
}

// Teardown survives a panic mid-run: the deferred binding still runs while the
// stack unwinds, before the surrounding handler recovers.
func TestRunCleanup_DeferredSurvivesPanic(t *testing.T) {
	var calls atomic.Int64

	func() {
		defer func() {
			_ = recover()
		}()
		cleanup := newRunCleanup(func() { calls.Add(1) })
		defer cleanup.Cleanup()

		panic("run exploded before returning a result")
	}()

	assert.Equal(t, int64(1), calls.Load(), "teardown must run even when the run panics")
}

// Teardown runs when the run returns early because its context was cancelled.
func TestRunCleanup_DeferredRunsOnContextCancellation(t *testing.T) {
	var calls atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the run body, forcing an early return

	func() {
		cleanup := newRunCleanup(func() { calls.Add(1) })
		defer cleanup.Cleanup()

		if ctx.Err() != nil {
			return // early return on a cancelled context
		}
	}()

	assert.Equal(t, int64(1), calls.Load(), "teardown must run on a cancelled-context early return")
}

// A caller that also invokes Cleanup after the executor's deferred call must
// not trigger a second destroy: the existing sync.Once semantics are retained.
func TestRunCleanup_ExactlyOnceAcrossRepeatedCalls(t *testing.T) {
	var calls atomic.Int64
	cleanup := newRunCleanup(func() { calls.Add(1) })

	for i := 0; i < 5; i++ {
		cleanup.Cleanup()
	}

	assert.Equal(t, int64(1), calls.Load(), "teardown body must run at most once")
}

// Teardown failure is not surfaced to the caller: Cleanup returns nothing, so a
// failed destroy cannot mask the run's own result.
func TestRunCleanup_DoesNotSurfaceTeardownError(t *testing.T) {
	cleanup := newRunCleanup(func() { /* best-effort teardown, error logged inside */ })

	assert.NotPanics(t, func() { cleanup.Cleanup() })
}

// Cleanup must be safe on a nil binding or when no teardown body is bound, so
// error paths that never provisioned a sandbox cannot panic.
func TestRunCleanup_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		var c *runCleanup
		c.Cleanup()
		newRunCleanup(nil).Cleanup()
	})
}
