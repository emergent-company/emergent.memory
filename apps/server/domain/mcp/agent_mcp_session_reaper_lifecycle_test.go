package mcp

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

// captureLifecycle records the hooks appended by the register* wiring function
// so a test can invoke the OnStart hook exactly as fx would: with a lifecycle
// context whose 15s fx.StartTimeout deadline (or post-OnStart cancel) elapses
// well before the reaper's 5-minute interval.
type captureLifecycle struct {
	hooks []fx.Hook
}

func (c *captureLifecycle) Append(h fx.Hook) {
	c.hooks = append(c.hooks, h)
}

// TestAgentMCPSessionReaperSurvivesCancelledLifecycleContext is the regression
// test for the fx StartTimeout bug: registerAgentMCPSessionReaper used to pass
// the fx lifecycle ctx straight into the reaper goroutine, whose loop selects
// on <-ctx.Done(). Because fx derives that ctx with a 15s deadline, the reaper
// died ~15s after boot — before its first 5-minute tick — so expired MCP agent
// sessions were never reaped.
//
// A full fx-app harness is impractical, so this pins the invariant directly on
// the wiring: invoke the real registerAgentMCPSessionReaper hook with an
// already-cancelled lifecycle ctx (modelling the deadline elapsing before the
// first tick) and require the goroutine to stay alive — it must terminate only
// via Stop().
func TestAgentMCPSessionReaperSurvivesCancelledLifecycleContext(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reaper := NewAgentMCPSessionReaper(nil, log)

	lc := &captureLifecycle{}
	registerAgentMCPSessionReaper(lc, reaper)
	require.Len(t, lc.hooks, 1, "register hook must append exactly one hook")

	// fx lifecycle ctx: cancelled before the 5-minute interval elapses.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, lc.hooks[0].OnStart(ctx))

	// Pre-fix, Start(ctx) selected on <-ctx.Done() and the goroutine exited
	// immediately, closing doneCh. Post-fix, Start(context.Background()) keeps
	// it alive until Stop().
	require.False(t, closedWithin(reaper.doneCh, 500*time.Millisecond),
		"reaper goroutine must stay alive after the fx lifecycle ctx is cancelled")

	reaper.Stop()
	require.True(t, closedWithin(reaper.doneCh, time.Second),
		"reaper goroutine must terminate on Stop()")
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
