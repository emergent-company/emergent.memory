package sandbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// captureLifecycle records the hooks appended by the startCleanupJob wiring
// function so a test can invoke the OnStart hook exactly as fx would: with a
// lifecycle context whose 15s fx.StartTimeout deadline (or post-OnStart cancel)
// elapses well before the cleanup job's interval (default 1h).
type captureLifecycle struct {
	hooks []fx.Hook
}

func (c *captureLifecycle) Append(h fx.Hook) {
	c.hooks = append(c.hooks, h)
}

// TestCleanupJobSurvivesCancelledLifecycleContext is the regression test for the
// fx StartTimeout bug: startCleanupJob used to pass the fx lifecycle ctx straight
// into the cleanup goroutine, whose loop selects on <-ctx.Done(). Because fx
// derives that ctx with a 15s deadline, the periodic cleanup loop died ~15s
// after boot (the initial cycle still ran, but the 1h periodic cycle never
// fired).
//
// A full fx-app harness is impractical, so this pins the invariant directly on
// the wiring: invoke the real startCleanupJob hook with an already-cancelled
// lifecycle ctx (modelling the deadline elapsing before the first interval tick)
// and require the goroutine to stay alive — it must terminate only via Stop().
func TestCleanupJobSurvivesCancelledLifecycleContext(t *testing.T) {
	cfg := &config.Config{Sandbox: config.SandboxConfig{Enabled: true}}

	job := NewCleanupJob(nil, nil, nil, testLogger(), CleanupConfig{
		Interval:       10 * time.Millisecond,
		MaxConcurrent:  10,
		AlertThreshold: 0.8,
	})

	lc := &captureLifecycle{}
	// rec == nil + ReconcileEnabled == false keeps reconciliation disabled.
	startCleanupJob(lc, job, nil, cfg, testLogger())
	require.Len(t, lc.hooks, 1, "startCleanupJob must append exactly one hook")

	// fx lifecycle ctx: cancelled before the interval elapses.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, lc.hooks[0].OnStart(ctx))

	// Pre-fix, Start(ctx) selected on <-ctx.Done() and the goroutine exited
	// immediately, closing doneCh. Post-fix, Start(context.Background()) keeps
	// it alive until Stop().
	require.False(t, closedWithin(job.doneCh, 500*time.Millisecond),
		"cleanup goroutine must stay alive after the fx lifecycle ctx is cancelled")

	job.Stop()
	require.True(t, closedWithin(job.doneCh, time.Second),
		"cleanup goroutine must terminate on Stop()")
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
