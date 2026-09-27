package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/session"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// These tests cover the queued / worker-pool lifecycle of durable cancellation.
// The original #1166 coverage exercised only the in-flight executor path; a
// cancel that lands while a run is queued (or between claim and completion) is
// equally reachable, and the worker's own terminal writes must honour it.

func jobStatus(t *testing.T, tdb *testdb.TestDB, runID string) string {
	t.Helper()
	var status string
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT status FROM kb.agent_run_jobs WHERE run_id = ? ORDER BY created_at DESC LIMIT 1`,
		runID,
	).Scan(context.Background(), &status))
	return status
}

// TestQueuedCancel_ClaimNextJobDoesNotResurrect is the fail-first regression for
// the claim path: a queued run cancelled before a worker claims it must not be
// resurrected to "working" by ClaimNextJob, which would discard the durable
// cancel and execute a run the endpoint already reported cancelled. RED against
// the previous unconditional `SET status='working'`.
func TestQueuedCancel_ClaimNextJobDoesNotResurrect(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_queued_cancel_claim")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "queued-agent")
	repo := NewRepository(tdb.DB)

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)
	require.Equal(t, string(RunStatusQueued), runStatus(t, tdb, run.ID))

	accepted, err := repo.RequestRunCancellation(ctx, run.ID)
	require.NoError(t, err)
	require.True(t, accepted)
	require.Equal(t, string(RunStatusCancelling), runStatus(t, tdb, run.ID))

	job, err := repo.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.Nil(t, job, "a queued run with a committed cancel must not be claimable")
	require.Equal(t, string(RunStatusCancelling), runStatus(t, tdb, run.ID),
		"the claim must not resurrect a cancelled run to working")
	require.Equal(t, string(JobStatusCompleted), jobStatus(t, tdb, run.ID),
		"the retired job must not stay pending/processing")
}

// TestQueuedCancel_CompleteJobDoesNotOverwriteCancel is the fail-first
// regression for the worker's success write: Claim -> Cancel -> Complete must
// leave the run cancelled, never flip it to completed. RED against CompleteJob's
// previous unguarded `SET status='completed'`.
func TestQueuedCancel_CompleteJobDoesNotOverwriteCancel(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_queued_cancel_complete")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "queued-agent")
	repo := NewRepository(tdb.DB)

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)
	job, err := repo.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, string(RunStatusRunning), runStatus(t, tdb, run.ID))

	accepted, err := repo.RequestRunCancellation(ctx, run.ID)
	require.NoError(t, err)
	require.True(t, accepted)

	require.NoError(t, repo.CompleteJob(ctx, job.ID, run.ID))
	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, run.ID),
		"CompleteJob must not overwrite a committed cancel with completed")
}

// TestQueuedCancel_FailJobDoesNotRequeueCancelledRun is the fail-first
// regression for the worker's failure write: a durable cancel must survive a
// requeue request (the run is not reset to queued for another attempt) and must
// not be overwritten to failed. RED against FailJob's previous unguarded writes.
func TestQueuedCancel_FailJobDoesNotRequeueCancelledRun(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_queued_cancel_fail")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "queued-agent")
	repo := NewRepository(tdb.DB)

	// Requeue requested while the run is cancelling: must not reset to queued.
	run, err := repo.CreateRunQueued(ctx, agentID, 3)
	require.NoError(t, err)
	job, err := repo.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, job)
	accepted, err := repo.RequestRunCancellation(ctx, run.ID)
	require.NoError(t, err)
	require.True(t, accepted)

	require.NoError(t, repo.FailJob(ctx, job.ID, run.ID, "boom", true, time.Now().Add(time.Minute)))
	require.Equal(t, string(RunStatusCancelling), runStatus(t, tdb, run.ID),
		"a cancelled run must not be requeued to submitted")
	require.Equal(t, string(JobStatusCompleted), jobStatus(t, tdb, run.ID),
		"a cancelled run's job must not be requeued to pending")
	none, err := repo.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.Nil(t, none, "the retired job must not be claimable again")

	// Final failure (no requeue) while cancelling: must resolve cancelled, not failed.
	run2, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)
	job2, err := repo.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, job2)
	accepted, err = repo.RequestRunCancellation(ctx, run2.ID)
	require.NoError(t, err)
	require.True(t, accepted)
	require.NoError(t, repo.FailJob(ctx, job2.ID, run2.ID, "boom", false, time.Time{}))
	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, run2.ID),
		"a final failure must not overwrite a committed cancel with failed")
}

// TestDurableCancel_HonoredWhenContextCancelled is the fail-first regression for
// the post-loop race: a timeout/request cancellation that arrives together with
// a durable cancel must be reported as cancelled, not failed. RED against the
// previous `ctx.Err() == nil` gate on the post-loop durable read.
func TestDurableCancel_HonoredWhenContextCancelled(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_durable_cancel_ctx_cancelled")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "ctx-cancel-agent")

	repoB := NewRepository(tdb.DB)
	aeB := transportEnforcedExecutor(repoB, newGateToolPool(), session.InMemoryService())
	gate := aeB.toolPool.(*gateToolPool)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	done := startGateRun(t, aeB, gate, requestCtx, agentID, projectID)

	runID := newestRunningRunID(t, tdb, agentID)
	require.NotEmpty(t, runID)

	repoA := NewRepository(tdb.DB)
	accepted, err := repoA.RequestRunCancellation(ctx, runID)
	require.NoError(t, err)
	require.True(t, accepted)

	// The request context is cancelled (timeout / disconnect) after the durable
	// cancel was committed. The persisted cancel must win the classification.
	cancelRequest()
	close(gate.release)

	o := waitOutcome(t, done)
	require.NoError(t, o.err)
	require.NotNil(t, o.res)
	require.Equal(t, RunStatusCancelled, o.res.Status,
		"a durable cancel must be honoured even when the context is cancelled")
	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, runID))
}

// TestStartupFinalize_SparesLiveCancellingRun is the fail-first regression for
// the startup overreach: an unconditional startup sweep would finalize a live
// run owned by another instance during a rolling restart. The startup sweep must
// be age/heartbeat-aware, sparing a run whose last_step_at is fresh. RED against
// the previous `FinalizeCancellingRuns(ctx, 0)`.
func TestStartupFinalize_SparesLiveCancellingRun(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_startup_finalize_live")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "startup-agent")
	repo := NewRepository(tdb.DB)

	liveID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusCancelling), true)
	_, err := tdb.DB.NewRaw(
		`UPDATE kb.agent_runs SET last_step_at = now() WHERE id = ?`, liveID,
	).Exec(ctx)
	require.NoError(t, err)

	staleID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusCancelling), true)
	_, err = tdb.DB.NewRaw(
		`UPDATE kb.agent_runs SET started_at = now() - interval '1 hour', last_step_at = now() - interval '1 hour' WHERE id = ?`,
		staleID,
	).Exec(ctx)
	require.NoError(t, err)

	n, err := repo.FinalizeOrphanedCancellingRuns(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "only the abandoned cancelling run should be finalized")
	require.Equal(t, string(RunStatusCancelling), runStatus(t, tdb, liveID),
		"startup must not finalize a live cancelling run owned by another instance")
	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, staleID))
}

// TestPauseRun_DoesNotClobberCancelling is the fail-first regression for the
// after-tool pause paths: PauseRun must not move a "cancelling" row to
// "input-required" and drop the durable cancel. RED against the previous
// unguarded PauseRun.
func TestPauseRun_DoesNotClobberCancelling(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_pause_does_not_clobber_cancel")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "pause-agent")
	repo := NewRepository(tdb.DB)

	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)
	accepted, err := repo.RequestRunCancellation(ctx, runID)
	require.NoError(t, err)
	require.True(t, accepted)

	require.NoError(t, repo.PauseRun(ctx, runID, 3))
	require.Equal(t, string(RunStatusCancelling), runStatus(t, tdb, runID),
		"a pause must not clobber a committed cancel")
}
