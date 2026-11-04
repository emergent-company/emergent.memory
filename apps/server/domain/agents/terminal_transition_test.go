package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// These tests lock the terminal-transition race fix for issue #1149. A run's
// cancel and its completion are two competing terminal writers: whichever
// reaches the row first must win, and the loser must be a no-op. Without the
// `status IN (non-terminal)` guard the later writer silently overwrites the
// earlier, so the run row can end up `completed` while the cancel endpoint
// already answered "cancelled" (or vice-versa).

// TestTerminalTransition_CancelThenComplete_KeepsCancelled reproduces the
// blocking race: a cancel lands after the executor's progress/terminal check
// but before its success write. The guarded cancel applies first; the late
// completion must then be a no-op, leaving the row cancelled so it matches what
// the cancel endpoint reported. RED against the unguarded completion.
func TestTerminalTransition_CancelThenComplete_KeepsCancelled(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_terminal_cancel_then_complete")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "terminal-agent")
	repo := NewRepository(tdb.DB)

	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

	cancelled, err := repo.CancelRun(ctx, runID)
	require.NoError(t, err)
	require.True(t, cancelled, "the cancel must win the terminal transition")

	// The executor's success write lands after the check; it must not clobber.
	require.NoError(t, repo.CompleteRunWithSteps(ctx, runID, map[string]any{}, 5, 1000))

	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, runID),
		"a late completion must not overwrite a run the cancel endpoint already reported cancelled")
}

// TestTerminalTransition_CompleteThenCancel_KeepsSuccess is the other ordering:
// the run completes before the cancel arrives. The guarded cancel must report
// that it changed nothing and leave the row completed, so a cancel that arrives
// too late cannot turn a finished run into "cancelled". RED against the
// unguarded CancelRun (BLOCKING 2).
func TestTerminalTransition_CompleteThenCancel_KeepsSuccess(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_terminal_complete_then_cancel")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "terminal-agent")
	repo := NewRepository(tdb.DB)

	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

	require.NoError(t, repo.CompleteRunWithSteps(ctx, runID, map[string]any{}, 5, 1000))

	cancelled, err := repo.CancelRun(ctx, runID)
	require.NoError(t, err)
	require.False(t, cancelled, "a cancel after a terminal write must report that it changed nothing")

	require.Equal(t, string(RunStatusSuccess), runStatus(t, tdb, runID),
		"a late cancel must not overwrite a completed run")
}

// TestTerminalTransition_CancelThenFail_KeepsCancelled covers the same race
// against the failure writer: a late failure must not clobber a cancel, so the
// terminal status stays honest.
func TestTerminalTransition_CancelThenFail_KeepsCancelled(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_terminal_cancel_then_fail")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "terminal-agent")
	repo := NewRepository(tdb.DB)

	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

	cancelled, err := repo.CancelRunWithSteps(ctx, runID, userCancelReason, 3)
	require.NoError(t, err)
	require.True(t, cancelled)

	require.NoError(t, repo.FailRunWithSteps(ctx, runID, "agent stopped: context canceled", 3))

	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, runID))
	var errMsg string
	require.NoError(t, tdb.DB.NewRaw(`SELECT COALESCE(error_message, '') FROM kb.agent_runs WHERE id = ?`, runID).Scan(ctx, &errMsg))
	require.Equal(t, userCancelReason, errMsg, "the cancel reason must survive a late failure write")
}

// TestTerminalTransition_CancelAfterQueuedRunNotOverwritten ensures the guard
// is not so strict that it blocks a legitimate terminal transition of a
// non-running (queued) run.
func TestTerminalTransition_CancelAfterQueuedRunNotOverwritten(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_terminal_queued_cancel")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "terminal-agent")
	repo := NewRepository(tdb.DB)

	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusQueued), true)

	cancelled, err := repo.CancelRun(ctx, runID)
	require.NoError(t, err)
	require.True(t, cancelled, "a queued run is non-terminal and must remain cancellable")
	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, runID))
}

func runStatus(t *testing.T, tdb *testdb.TestDB, runID string) string {
	t.Helper()
	var status string
	require.NoError(t, tdb.DB.NewRaw(`SELECT status FROM kb.agent_runs WHERE id = ?`, runID).Scan(context.Background(), &status))
	return status
}

// TestTerminalTransition_ConcurrentCancelAndComplete asserts the invariant under
// true concurrency: run a cancel and a completion against the same run at the
// same time. Exactly one wins; if the cancel reports it transitioned the row,
// the final status must be cancelled, otherwise it must be completed. The
// guarded UPDATE makes the two mutually exclusive via Postgres row locking.
func TestTerminalTransition_ConcurrentCancelAndComplete(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_terminal_concurrent")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "terminal-agent")
	repo := NewRepository(tdb.DB)

	for i := 0; i < 25; i++ {
		runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

		cancelDone := make(chan bool, 1)
		completeDone := make(chan error, 1)
		go func() {
			ok, err := repo.CancelRun(ctx, runID)
			require.NoError(t, err)
			cancelDone <- ok
		}()
		go func() {
			completeDone <- repo.CompleteRunWithSteps(ctx, runID, map[string]any{}, 1, 10)
		}()
		cancelled := <-cancelDone
		require.NoError(t, <-completeDone)

		final := runStatus(t, tdb, runID)
		if cancelled {
			require.Equal(t, string(RunStatusCancelled), final,
				"iteration %d: cancel won but final status is %q", i, final)
		} else {
			require.Equal(t, string(RunStatusSuccess), final,
				"iteration %d: cancel lost but final status is %q", i, final)
		}
		time.Sleep(0)
	}
}
