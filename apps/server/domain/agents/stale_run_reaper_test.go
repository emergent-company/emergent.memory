package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests pin the two distinct "running run" recovery paths on the
// Repository:
//
//   - MarkStaleRunsAsError (idle-timeout reaper): must key off
//     COALESCE(last_step_at, started_at) so a heartbeating long run survives.
//   - MarkOrphanedRunsAsError (startup recovery): must keep failing ALL running
//     rows with no activity predicate.
//
// They reuse the captureExecDriver/newCaptureExecRepository helpers from
// ask_user_tool_test.go, which record the SQL of the last ExecContext call.

func resetCaptureDriver() {
	sharedCaptureDriver.mu.Lock()
	sharedCaptureDriver.query = ""
	sharedCaptureDriver.args = nil
	sharedCaptureDriver.mu.Unlock()
}

// TestMarkStaleRunsAsError_KeysOffLastStepAt asserts the idle-timeout reaper
// keys off COALESCE(last_step_at, started_at) rather than started_at alone.
func TestMarkStaleRunsAsError_KeysOffLastStepAt(t *testing.T) {
	resetCaptureDriver()
	repo := newCaptureExecRepository(t)

	_, err := repo.MarkStaleRunsAsError(context.Background(), 30*time.Minute)
	require.NoError(t, err)

	q := sharedCaptureDriver.lastQuery()
	require.Contains(t, q, "COALESCE(last_step_at, started_at)",
		"stale-run reaper must key off last activity, not start time; got: %s", q)
	require.Contains(t, q, "status", "reaper must still restrict to running runs")
	require.NotContains(t, q, "AND (started_at <",
		"reaper must not key off started_at alone; got: %s", q)
}

// TestMarkOrphanedRunsAsError_StillFailsAllRunning guards the startup recovery
// path: it must keep failing ALL running rows with no last_step_at / started_at
// predicate, independent of the idle-timeout path change.
func TestMarkOrphanedRunsAsError_StillFailsAllRunning(t *testing.T) {
	resetCaptureDriver()
	repo := newCaptureExecRepository(t)

	_, err := repo.MarkOrphanedRunsAsError(context.Background())
	require.NoError(t, err)

	q := sharedCaptureDriver.lastQuery()
	require.Contains(t, q, "status", "orphan reaper must target running runs")
	require.NotContains(t, q, "COALESCE", "startup recovery must not key off last_step_at")
	require.NotContains(t, q, "started_at <", "startup recovery must not key off started_at")
}

// TestTouchRun_SetsLastStepAt asserts the heartbeat writes last_step_at on the
// target run.
func TestTouchRun_SetsLastStepAt(t *testing.T) {
	resetCaptureDriver()
	repo := newCaptureExecRepository(t)

	err := repo.TouchRun(context.Background(), "run-1")
	require.NoError(t, err)

	q := sharedCaptureDriver.lastQuery()
	require.Contains(t, q, "last_step_at", "TouchRun must set last_step_at; got: %s", q)
}

// TestTouchRun_OnlyTouchesRunningRuns pins the status guard: a heartbeat that
// races a terminal transition must not resurrect liveness on a finished run.
func TestTouchRun_OnlyTouchesRunningRuns(t *testing.T) {
	resetCaptureDriver()
	repo := newCaptureExecRepository(t)

	err := repo.TouchRun(context.Background(), "run-1")
	require.NoError(t, err)

	q := sharedCaptureDriver.lastQuery()
	require.Contains(t, q, "last_step_at", "TouchRun must set last_step_at; got: %s", q)
	require.Contains(t, q, "status", "TouchRun must restrict to running rows; got: %s", q)
}

// TestStartRunHeartbeat_TouchesPeriodicallyThenStops covers the run-lifetime
// heartbeat: it must keep refreshing last_step_at while the executor goroutine
// is alive (so provisioning and long blocking phases count as activity), and it
// must stop cleanly when the executor returns — a dead goroutine stops ticking,
// so its run is still reaped.
func TestStartRunHeartbeat_TouchesPeriodicallyThenStops(t *testing.T) {
	resetCaptureDriver()
	repo := newCaptureExecRepository(t)

	stop := repo.StartRunHeartbeat("run-heartbeat", 20*time.Millisecond)

	deadline := time.Now().Add(3 * time.Second)
	for sharedCaptureDriver.lastQuery() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	require.Contains(t, sharedCaptureDriver.lastQuery(), "last_step_at",
		"heartbeat must write last_step_at")

	stop()
	// Give any in-flight tick a chance to observe the closed channel, then prove
	// that no further writes arrive over several heartbeat intervals.
	time.Sleep(50 * time.Millisecond)
	resetCaptureDriver()
	time.Sleep(60 * time.Millisecond)
	require.Empty(t, sharedCaptureDriver.lastQuery(),
		"heartbeat must stop writing once stop() is called")

	// stop must be idempotent.
	require.NotPanics(t, stop)
}
