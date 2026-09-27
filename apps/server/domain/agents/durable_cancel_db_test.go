package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/session"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// These tests lock the durable, multi-instance cancellation fix for issue
// #1166. A cancel served by one server instance must stop a run executing on
// another: the cancel is persisted as the intermediate "cancelling" status in
// kb.agent_runs (visible to every instance), the executing instance observes
// that status at a step boundary and stops, and the terminal write finalizes
// the run as "cancelled". The per-process cancel registry is only a fast-path
// notification and must never be load-bearing for correctness.

// TestDurableCancel_CompletionHonorsCancel is the fail-first regression at the
// repository seam: a cancel committed by instance A (row moved to "cancelling")
// must not be turned into a success by instance B's completion write. RED
// against the previous guard, under which a completion was allowed to move a
// "cancelling" row straight to "completed".
func TestDurableCancel_CompletionHonorsCancel(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_durable_cancel_complete")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "durable-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

	// Two repositories model two instances sharing one database.
	repoA := NewRepository(tdb.DB)
	repoB := NewRepository(tdb.DB)

	accepted, err := repoA.RequestRunCancellation(ctx, runID)
	require.NoError(t, err)
	require.True(t, accepted, "instance A must commit the durable cancel intent")
	require.Equal(t, string(RunStatusCancelling), runStatus(t, tdb, runID))

	// Instance B finalizes the run it was executing. It must resolve the
	// committed cancel to "cancelled", not to "completed".
	require.NoError(t, repoB.CompleteRunWithSteps(ctx, runID, map[string]any{"ok": true}, 5, 1000))

	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, runID),
		"a completion on instance B must not overwrite a cancel committed on instance A")
	var errMsg string
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT COALESCE(error_message, '') FROM kb.agent_runs WHERE id = ?`, runID,
	).Scan(ctx, &errMsg))
	require.Equal(t, userCancelReason, errMsg, "the durable cancel reason must be recorded")
}

// TestDurableCancel_FailureHonorsCancel is the failure-writer counterpart: a
// durable cancel wins over a later failure write, so the endpoint's
// cancelled:true promise is never reported as a server fault.
func TestDurableCancel_FailureHonorsCancel(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_durable_cancel_fail")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "durable-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

	repoA := NewRepository(tdb.DB)
	repoB := NewRepository(tdb.DB)

	accepted, err := repoA.RequestRunCancellation(ctx, runID)
	require.NoError(t, err)
	require.True(t, accepted)

	require.NoError(t, repoB.FailRunWithSteps(ctx, runID, "agent stopped: context canceled", 3))

	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, runID),
		"a failure on instance B must not overwrite a cancel committed on instance A")
}

// TestDurableCancel_AlreadyTerminalNotClobbered proves the cancel intent is
// guarded: a cancel issued after the run reached a terminal state changes no
// rows, so a completed run stays completed and the caller is told the cancel
// did not apply.
func TestDurableCancel_AlreadyTerminalNotClobbered(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_durable_cancel_terminal")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "durable-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusSuccess), true)

	repo := NewRepository(tdb.DB)
	accepted, err := repo.RequestRunCancellation(ctx, runID)
	require.NoError(t, err)
	require.False(t, accepted, "a terminal run must not be cancellable")
	require.Equal(t, string(RunStatusSuccess), runStatus(t, tdb, runID),
		"a late cancel must leave a completed run untouched")
}

// TestDurableCancel_RequestIsIdempotent covers a repeated cancel: the row is
// already "cancelling", but the intent is still accepted, so the endpoint keeps
// answering cancelled:true for a cancel the first request already committed.
func TestDurableCancel_RequestIsIdempotent(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_durable_cancel_idempotent")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "durable-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

	repo := NewRepository(tdb.DB)
	first, err := repo.RequestRunCancellation(ctx, runID)
	require.NoError(t, err)
	require.True(t, first)
	second, err := repo.RequestRunCancellation(ctx, runID)
	require.NoError(t, err)
	require.True(t, second, "a repeated cancel of an already-cancelling run must stay accepted")
	require.Equal(t, string(RunStatusCancelling), runStatus(t, tdb, runID))
}

// TestRunIsCancelling_ObservesDurableIntent pins the observation primitive the
// executor reads at step boundaries: it reports the persisted cancel intent and
// stops reporting it once the run reaches a terminal state.
func TestRunIsCancelling_ObservesDurableIntent(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_durable_cancel_observe")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "durable-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

	repo := NewRepository(tdb.DB)
	cancelling, err := repo.RunIsCancelling(ctx, runID)
	require.NoError(t, err)
	require.False(t, cancelling, "a running run does not carry a cancel intent")

	_, err = repo.RequestRunCancellation(ctx, runID)
	require.NoError(t, err)

	cancelling, err = repo.RunIsCancelling(ctx, runID)
	require.NoError(t, err)
	require.True(t, cancelling, "the durable cancel intent must be observable")

	_, err = repo.CancelRunWithSteps(ctx, runID, userCancelReason, 1)
	require.NoError(t, err)
	cancelling, err = repo.RunIsCancelling(ctx, runID)
	require.NoError(t, err)
	require.False(t, cancelling, "a finalized run no longer carries the cancel intent")
}

// TestRunIsCancelling_MissingRunIsNotCancelling guards the observation helper
// against a deleted/unknown run: it must report false rather than error, so
// observation never blocks or aborts a run.
func TestRunIsCancelling_MissingRunIsNotCancelling(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_durable_cancel_missing")
	t.Cleanup(tdb.Close)

	repo := NewRepository(tdb.DB)
	cancelling, err := repo.RunIsCancelling(context.Background(), "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	require.False(t, cancelling)
}

// TestFinalizeCancellingRuns_ResolvesOrphans covers the safety net: a run stuck
// in the intermediate "cancelling" state because no live executor observed the
// cancel must eventually resolve to "cancelled" (never to a failure). Fresh
// cancelling runs are spared when a threshold is applied; startup recovery
// (threshold <= 0) finalizes every cancelling row.
func TestFinalizeCancellingRuns_ResolvesOrphans(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_durable_cancel_orphans")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "durable-agent")
	repo := NewRepository(tdb.DB)

	staleID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusCancelling), true)
	freshID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusCancelling), true)
	// Backdate the stale run's activity so only it is past the threshold.
	_, err := tdb.DB.NewRaw(
		`UPDATE kb.agent_runs SET started_at = now() - interval '1 hour', last_step_at = now() - interval '1 hour' WHERE id = ?`,
		staleID,
	).Exec(ctx)
	require.NoError(t, err)

	n, err := repo.FinalizeCancellingRuns(ctx, 30*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, n, "only the idle cancelling run should be finalized")
	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, staleID))
	require.Equal(t, string(RunStatusCancelling), runStatus(t, tdb, freshID),
		"a cancelling run still winding down must be spared by the threshold")

	// Startup recovery has no live executor for any cancelling row.
	n, err = repo.FinalizeCancellingRuns(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, freshID))
}

// TestDurableCancel_TwoInstances_CancelOnAStopsRunOnB is the end-to-end
// two-instance regression: a run executing on instance B is cancelled by a
// request served on instance A, whose local cancel registry does not know about
// the run. B must observe the persisted "cancelling" status at its step boundary
// and stop on its own; the terminal status must be "cancelled". No local
// notification is sent to B, so passing proves correctness does not depend on
// the per-process registry.
func TestDurableCancel_TwoInstances_CancelOnAStopsRunOnB(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_durable_cancel_two_instances")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "durable-cancel-agent")

	// Instance B owns the executor and its in-flight cancel registry.
	repoB := NewRepository(tdb.DB)
	aeB := transportEnforcedExecutor(repoB, newGateToolPool(), session.InMemoryService())
	gate := aeB.toolPool.(*gateToolPool)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	done := startGateRun(t, aeB, gate, DetachedRunContext(requestCtx), agentID, projectID)

	runID := newestRunningRunID(t, tdb, agentID)
	require.NotEmpty(t, runID)

	// Instance A serves the cancel. It shares only the database with B; its
	// repository/executor has no in-flight registry entry for this run.
	repoA := NewRepository(tdb.DB)
	accepted, err := repoA.RequestRunCancellation(ctx, runID)
	require.NoError(t, err)
	require.True(t, accepted, "instance A must commit the durable cancel intent")

	// Release the gate without cancelling B's context and without notifying B's
	// registry: B must discover the cancel by observing the persisted status.
	close(gate.release)

	o := waitOutcome(t, done)
	require.NoError(t, o.err)
	require.NotNil(t, o.res)
	require.Equal(t, RunStatusCancelled, o.res.Status,
		"instance B must stop the run after observing the cancel committed on instance A")
	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, runID),
		"the run executing on instance B must end as cancelled")
}
