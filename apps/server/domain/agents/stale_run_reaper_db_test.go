package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestMarkStaleRunsAsError_HeartbeatSparesActiveRun exercises the idle-timeout
// reaper against a real schema built from the embedded migrations (applied to
// head by internal/testdb):
//
//   - a running run with a fresh heartbeat survives the sweep,
//   - a running run whose start AND heartbeat are stale is reaped,
//   - a running run that never heartbeated falls back to started_at via
//     COALESCE and is reaped,
//   - a never-started queued run is never touched (regression guard for the
//     "queued, not stale" class of false failures).
func TestMarkStaleRunsAsError_HeartbeatSparesActiveRun(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_stale_reaper")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB

	orgID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`,
		orgID, "reaper-test-org")
	require.NoError(t, err)

	projectID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "reaper-test-project")
	require.NoError(t, err)

	agentID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?,?,?,?,?)`,
		agentID, "reaper-test", "graph", "", projectID)
	require.NoError(t, err)

	now := time.Now()
	insertRun := func(status AgentRunStatus, startedAt time.Time, lastStepAt *time.Time) string {
		id := uuid.NewString()
		_, insErr := db.ExecContext(ctx,
			`INSERT INTO kb.agent_runs (id, agent_id, status, started_at, last_step_at) VALUES (?,?,?,?,?)`,
			id, agentID, string(status), startedAt, lastStepAt)
		require.NoError(t, insErr)
		return id
	}
	freshHeartbeat := now.Add(-1 * time.Minute)
	staleHeartbeat := now.Add(-40 * time.Minute)
	longAgo := now.Add(-40 * time.Minute)

	activeID := insertRun(RunStatusRunning, longAgo, &freshHeartbeat)
	stoppedHeartbeatID := insertRun(RunStatusRunning, longAgo, &staleHeartbeat)
	neverHeartbeatedID := insertRun(RunStatusRunning, longAgo, nil)
	queuedID := insertRun(RunStatusQueued, longAgo, nil)

	n, err := NewRepository(db).MarkStaleRunsAsError(ctx, 30*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, n, "only the two runs with no recent activity should be reaped")

	statusOf := func(id string) string {
		var s string
		require.NoError(t, db.NewRaw(`SELECT status FROM kb.agent_runs WHERE id = ?`, id).Scan(ctx, &s))
		return s
	}

	require.Equal(t, string(RunStatusRunning), statusOf(activeID),
		"a run heartbeating within the threshold must survive the sweep")
	require.Equal(t, string(RunStatusError), statusOf(stoppedHeartbeatID),
		"a run whose heartbeat stopped must still be reaped (dead-writer case)")
	require.Equal(t, string(RunStatusError), statusOf(neverHeartbeatedID),
		"a run that never heartbeated must fall back to started_at and be reaped")
	require.Equal(t, string(RunStatusQueued), statusOf(queuedID),
		"a never-started queued run must never be terminal-failed")
}

// seedReaperAgent inserts the org/project/agent trio the reaper DB tests need
// and returns the project and agent IDs.
func seedReaperAgent(t *testing.T, ctx context.Context, tdb *testdb.TestDB, name string) (projectID, agentID string) {
	t.Helper()

	orgID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`,
		orgID, "reaper-"+name+"-org")
	require.NoError(t, err)

	projectID = uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "reaper-"+name+"-project")
	require.NoError(t, err)

	agentID = uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?,?,?,?,?)`,
		agentID, "reaper-"+name, "graph", "", projectID)
	require.NoError(t, err)
	return projectID, agentID
}

// jobState reads a dispatch job's status and completion stamp.
func jobState(t *testing.T, ctx context.Context, tdb *testdb.TestDB, jobID string) (status string, completedAt *time.Time) {
	t.Helper()
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT status, completed_at FROM kb.agent_run_jobs WHERE id = ?`, jobID,
	).Scan(ctx, &status, &completedAt))
	return status, completedAt
}

// TestMarkStaleRunsAsError_RetiresProcessingJob pins issue #906.
//
// The worker-claim path moves the run to "running" and its dispatch job to
// "processing". When that worker dies, MarkStaleRunsAsError fails the run — but
// nothing used to move the job row out of "processing". The row was then
// unclaimable for ever (ClaimNextJobInQueue only selects "pending") while still
// being counted by CountPendingJobsForAgent, i.e. by the AGENT_MAX_PENDING_JOBS
// guard in CreateRunQueued. Enough such rows and the agent returns
// QueueFullError while doing no work at all.
//
// The sweep must therefore retire the job with the run: after the reap the
// queue depth for the agent must drop back to zero.
func TestMarkStaleRunsAsError_RetiresProcessingJob(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_stale_reaper_jobs")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	repo := NewRepository(tdb.DB)
	projectID, agentID := seedReaperAgent(t, ctx, tdb, "stale-job")
	require.NoError(t, repo.EnsureDefaultQueue(ctx, projectID))

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)

	// Claim it, exactly as a worker does: run running, job processing.
	job, err := repo.ClaimNextJobInQueue(ctx, projectID, DefaultQueueName)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, run.ID, job.RunID)
	require.Equal(t, JobStatusProcessing, job.Status)

	depth, err := repo.CountPendingJobsForAgent(ctx, agentID)
	require.NoError(t, err)
	require.Equal(t, 1, depth)

	// The worker dies: no heartbeat for longer than the threshold.
	_, err = tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_runs
		    SET started_at = now() - interval '1 hour',
		        last_step_at = now() - interval '45 minutes'
		  WHERE id = ?`, run.ID)
	require.NoError(t, err)

	n, err := repo.MarkStaleRunsAsError(ctx, 30*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	after, err := repo.FindRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusError, after.Status)

	status, completedAt := jobState(t, ctx, tdb, job.ID)
	require.Equal(t, string(JobStatusCompleted), status,
		"a failed run must not leave its dispatch job non-terminal (issue #906)")
	require.NotNil(t, completedAt, "the retired job must be stamped with completed_at")

	depth, err = repo.CountPendingJobsForAgent(ctx, agentID)
	require.NoError(t, err)
	require.Zero(t, depth,
		"the retired job must not keep counting against AGENT_MAX_PENDING_JOBS")
}

// TestMarkOrphanedRunsAsError_RetiresProcessingJob is the startup half of
// issue #906: a crash leaves the run "running" and its job "processing", and
// startup recovery fails the run. RequeueOrphanedQueuedRuns only covers queued
// runs, so without retiring the job here the stranded processing row would
// count against the agent for ever after every unclean shutdown.
func TestMarkOrphanedRunsAsError_RetiresProcessingJob(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_orphan_reaper_jobs")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	repo := NewRepository(tdb.DB)
	projectID, agentID := seedReaperAgent(t, ctx, tdb, "orphan-job")
	require.NoError(t, repo.EnsureDefaultQueue(ctx, projectID))

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)

	job, err := repo.ClaimNextJobInQueue(ctx, projectID, DefaultQueueName)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, JobStatusProcessing, job.Status)

	n, err := repo.MarkOrphanedRunsAsError(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	after, err := repo.FindRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusError, after.Status)

	status, completedAt := jobState(t, ctx, tdb, job.ID)
	require.Equal(t, string(JobStatusCompleted), status,
		"startup recovery must retire the dead worker's job row (issue #906)")
	require.NotNil(t, completedAt)

	depth, err := repo.CountPendingJobsForAgent(ctx, agentID)
	require.NoError(t, err)
	require.Zero(t, depth)
}
