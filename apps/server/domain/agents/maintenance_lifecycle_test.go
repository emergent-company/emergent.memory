package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

// captureLifecycle records the hooks appended by the register* wiring functions
// so a test can invoke the OnStart hook exactly as fx would: with a lifecycle
// context whose 15s fx.StartTimeout deadline (or post-OnStart cancel) elapses
// well before the 1m/5m/30s maintenance interval.
type captureLifecycle struct {
	hooks []fx.Hook
}

func (c *captureLifecycle) Append(h fx.Hook) {
	c.hooks = append(c.hooks, h)
}

// TestConfigurableMaintenanceLoopsKeepTickingAfterLifecycleContextCancelled is
// the regression test for the fx StartTimeout bug: the register* OnStart hooks
// used to pass the fx lifecycle ctx straight into the maintenance goroutine,
// whose loop selects on <-ctx.Done(). Because fx derives that ctx with a 15s
// deadline (fx.StartTimeout), every maintenance goroutine died ~15s after boot
// — before its first 1m/5m tick — so object-driven work never dispatched.
//
// A full fx-app harness is impractical here (the agents Module carries dozens
// of transitive dependencies), so this pins the invariant directly on the
// wiring: invoke the real register* hook with an already-cancelled lifecycle
// ctx (modelling the deadline elapsing before the first tick) and require the
// loop to keep ticking regardless.
func TestConfigurableMaintenanceLoopsKeepTickingAfterLifecycleContextCancelled(t *testing.T) {
	tests := []struct {
		name  string
		wired func(lc *captureLifecycle, repo *Repository) (stop func())
	}{
		{
			name: "WorkReconciler",
			wired: func(lc *captureLifecycle, repo *Repository) func() {
				rc := NewWorkReconciler(repo, testAgentLogger(), 10*time.Millisecond)
				registerWorkReconciler(lc, rc, &fakeListStore{})
				return rc.Stop
			},
		},
		{
			name: "WorkStatusReaper",
			wired: func(lc *captureLifecycle, repo *Repository) func() {
				r := NewWorkStatusReaper(repo, testAgentLogger(), 10*time.Millisecond, time.Minute)
				registerWorkStatusReaper(lc, r, &fakeListStore{})
				return r.Stop
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCaptureDriver()
			repo := newCaptureExecRepository(t)
			lc := &captureLifecycle{}
			stop := tt.wired(lc, repo)
			require.Len(t, lc.hooks, 1, "register hook must append exactly one hook")

			// fx lifecycle ctx: cancelled before the maintenance interval elapses.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			require.NoError(t, lc.hooks[0].OnStart(ctx))

			// The loop must keep ticking: each tick calls
			// repo.listWorkAgentStatuses, which issues a query the capture driver
			// records. Pre-fix, the goroutine selected on <-ctx.Done() and exited
			// before its first tick, so no query was ever recorded.
			require.Eventually(t, func() bool {
				return sharedCaptureDriver.lastQuery() != ""
			}, 2*time.Second, 5*time.Millisecond,
				"%s must keep ticking after the fx lifecycle ctx is cancelled", tt.name)

			stop()
		})
	}
}

// TestFixedIntervalReapersSurviveCancelledLifecycleContext pins the same
// invariant for the fixed-interval reapers (StaleRunReaper at 5m, ShareRunReaper
// at 1m), whose tick is too far out to observe in a unit test. It asserts the
// loop goroutine stays alive (doneCh remains open) after the fx lifecycle ctx is
// cancelled; only Stop() should terminate it.
func TestFixedIntervalReapersSurviveCancelledLifecycleContext(t *testing.T) {
	repo := newCaptureExecRepository(t)
	log := testAgentLogger()

	tests := []struct {
		name  string
		wired func(lc *captureLifecycle) (<-chan struct{}, func())
	}{
		{
			name: "StaleRunReaper",
			wired: func(lc *captureLifecycle) (<-chan struct{}, func()) {
				r := NewStaleRunReaper(repo, log)
				registerStaleRunReaper(lc, r)
				return r.doneCh, r.Stop
			},
		},
		{
			name: "ShareRunReaper",
			wired: func(lc *captureLifecycle) (<-chan struct{}, func()) {
				r := NewShareRunReaper(repo, log)
				registerShareRunReaper(lc, r)
				return r.doneCh, r.Stop
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := &captureLifecycle{}
			done, stop := tt.wired(lc)
			require.Len(t, lc.hooks, 1, "register hook must append exactly one hook")

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			require.NoError(t, lc.hooks[0].OnStart(ctx))

			// Pre-fix, Start(ctx) selected on <-ctx.Done() and the goroutine
			// exited immediately, closing doneCh. Post-fix, Start(context.Background())
			// keeps it alive until Stop().
			require.False(t, closedWithin(done, 500*time.Millisecond),
				"%s goroutine must stay alive after the fx lifecycle ctx is cancelled", tt.name)

			stop()
			require.True(t, closedWithin(done, time.Second),
				"%s goroutine must terminate on Stop()", tt.name)
		})
	}
}

// closedWithin reports whether ch is closed within d.
func closedWithin(ch <-chan struct{}, d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case <-ch:
			return true
		case <-deadline:
			return false
		}
	}
}
