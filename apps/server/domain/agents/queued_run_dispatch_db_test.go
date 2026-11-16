package agents

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// queuedDispatchFixture is one seeded queued run plus everything a case needs to
// reach the rows it wants to perturb.
type queuedDispatchFixture struct {
	tdb       *testdb.TestDB
	repo      *Repository
	projectID string
	agentID   string
	runID     string
	jobID     string
}

// TestReaperTerminatesUndispatchableQueuedRuns pins issue #880.
//
// A queued ("submitted") run is inserted together with its dispatch job, and a
// worker can only claim that job by polling its exact (project_id, queue) pair:
// ClaimNextJobInQueue matches on both, and the supervisor only sizes workers for
// the enabled rows of kb.agent_queues. So a job whose queue row is missing, or
// whose project_id is NULL (resolveAgentRouting returns "" when it cannot load
// the agent, and strPtrIfNotEmpty stores that as NULL), is invisible to every
// worker for good. The run then sits "submitted" forever: the stale-run reaper
// only looks at "working", and work maintenance counts "submitted" as live, so
// nothing ever terminates it and no error surfaces.
//
// A disabled queue is NOT in that class — re-enabling reverses it — so those
// runs are spared (see TestSweepSparesDisabledQueueUntilReEnabled).
//
// The reaper now heals first (RequeueOrphanedQueuedRuns, for a run that lost its
// job row) and then fails what still has no claimable job after the threshold,
// retiring the dispatch job in the same transaction. Every case here drives
// StaleRunReaper.reap, so the wiring is part of what is tested, against the real
// schema and the real claim predicate.
func TestReaperTerminatesUndispatchableQueuedRuns(t *testing.T) {
	ctx := context.Background()

	ageRun := func(t *testing.T, tdb *testdb.TestDB, runID string) {
		t.Helper()
		_, err := tdb.DB.ExecContext(ctx,
			`UPDATE kb.agent_runs
			    SET started_at = now() - interval '2 hours', last_step_at = NULL
			  WHERE id = ?`, runID)
		require.NoError(t, err)
	}

	cases := []struct {
		name string
		// prepare runs after the queued run and its dispatch job exist.
		prepare func(t *testing.T, f queuedDispatchFixture)
		// wantFailed is the verdict for the failing sweep after reap.
		wantFailed bool
	}{
		{
			// The seeded project has no kb.agent_queues row at all: workers
			// resolve their queues from that table, so nothing ever polls this
			// job.
			name:       "missing_queue_row_is_failed",
			wantFailed: true,
		},
		{
			// A disabled queue is reversible operator intent: the supervisor
			// stops polling it, but re-enabling brings the backlog back, so the
			// run must survive a deploy window instead of being failed.
			name: "disabled_queue_is_spared",
			prepare: func(t *testing.T, f queuedDispatchFixture) {
				t.Helper()
				require.NoError(t, f.repo.EnsureDefaultQueue(ctx, f.projectID))
				_, err := f.tdb.DB.ExecContext(ctx,
					`UPDATE kb.agent_queues SET enabled = false WHERE project_id = ? AND name = ?`,
					f.projectID, DefaultQueueName)
				require.NoError(t, err)
			},
			wantFailed: false,
		},
		{
			// The enqueue-time defect: routing resolved no project, so the job
			// carries project_id = NULL and no claim can ever match it.
			name: "null_project_job_is_failed",
			prepare: func(t *testing.T, f queuedDispatchFixture) {
				t.Helper()
				require.NoError(t, f.repo.EnsureDefaultQueue(ctx, f.projectID))
				_, err := f.tdb.DB.ExecContext(ctx,
					`UPDATE kb.agent_run_jobs SET project_id = NULL WHERE id = ?`, f.jobID)
				require.NoError(t, err)
			},
			wantFailed: true,
		},
		{
			// A claimable job is a run legitimately waiting in line, however
			// long the line is: it must never be failed for being slow.
			name: "enabled_queue_is_spared",
			prepare: func(t *testing.T, f queuedDispatchFixture) {
				t.Helper()
				require.NoError(t, f.repo.EnsureDefaultQueue(ctx, f.projectID))
			},
			wantFailed: false,
		},
		{
			// Fan-out children are driven by their parent chain; the sweep
			// deliberately leaves that lifecycle alone.
			name: "root_run_is_spared",
			prepare: func(t *testing.T, f queuedDispatchFixture) {
				t.Helper()
				// root_run_id is an FK to agent_runs, so the root has to exist.
				root, err := f.repo.CreateRun(ctx, f.agentID)
				require.NoError(t, err)
				_, err = f.tdb.DB.ExecContext(ctx,
					`UPDATE kb.agent_runs SET root_run_id = ? WHERE id = ?`, root.ID, f.runID)
				require.NoError(t, err)
			},
			wantFailed: false,
		},
		{
			// A session (A2A task) run's completion is owned by the session
			// turn, not by the dispatch queue.
			name: "session_run_is_spared",
			prepare: func(t *testing.T, f queuedDispatchFixture) {
				t.Helper()
				sessionID := uuid.NewString()
				_, err := f.tdb.DB.ExecContext(ctx,
					`INSERT INTO kb.sessions (id, project_id) VALUES (?, ?)`, sessionID, f.projectID)
				require.NoError(t, err)
				_, err = f.tdb.DB.ExecContext(ctx,
					`UPDATE kb.agent_runs SET session_id = ? WHERE id = ?`, sessionID, f.runID)
				require.NoError(t, err)
			},
			wantFailed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tdb, projectID := seedAgentDeleteProject(t, tc.name)
			repo := NewRepository(tdb.DB)
			agentID := uuid.NewString()
			_, err := tdb.DB.ExecContext(ctx,
				`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
				 VALUES (?, ?, 'graph', '', ?)`,
				agentID, "queued-dispatch-"+uuid.NewString()[:8], projectID)
			require.NoError(t, err)

			run, err := repo.CreateRunQueued(ctx, agentID, 1)
			require.NoError(t, err)
			require.Equal(t, RunStatusQueued, run.Status)

			var jobID string
			require.NoError(t, tdb.DB.NewRaw(
				`SELECT id::text FROM kb.agent_run_jobs WHERE run_id = ?`, run.ID,
			).Scan(ctx, &jobID))

			f := queuedDispatchFixture{
				tdb: tdb, repo: repo, projectID: projectID,
				agentID: agentID, runID: run.ID, jobID: jobID,
			}
			ageRun(t, tdb, run.ID)
			if tc.prepare != nil {
				tc.prepare(t, f)
			}

			NewStaleRunReaper(repo, testAgentLogger()).reap(ctx)

			after, err := repo.FindRunByID(ctx, run.ID)
			require.NoError(t, err)
			if !tc.wantFailed {
				require.NotEqual(t, RunStatusError, after.Status,
					"a run that can still be dispatched (or whose lifecycle someone else owns) must not be failed")
				return
			}

			require.Equal(t, RunStatusError, after.Status,
				"a queued run no worker can ever claim must not stay non-terminal")
			require.NotNil(t, after.CompletedAt)
			require.NotNil(t, after.ErrorMessage)
			require.Contains(t, *after.ErrorMessage, "never dispatched")

			// A terminal run must not leave a live dispatch job behind: an
			// unclaimable pending row would count against MaxPendingJobs forever
			// (issue #880 review).
			var liveJobs int
			require.NoError(t, tdb.DB.NewRaw(
				`SELECT COUNT(*) FROM kb.agent_run_jobs
				  WHERE run_id = ? AND status IN ('pending', 'processing')`, run.ID,
			).Scan(ctx, &liveJobs))
			require.Zero(t, liveJobs, "a failed queued run must retire its dispatch jobs")
		})
	}
}

// TestReaperSparingKeepsQueuedRunClaimable is the other half of the
// enabled-queue case: "spared" must mean "a worker can actually take it", not
// merely "not yet failed". After the reaper the job is still claimable and the
// claim moves the run to working.
func TestReaperSparingKeepsQueuedRunClaimable(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "queued_dispatch_claimable")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)
	require.NoError(t, repo.EnsureDefaultQueue(ctx, projectID))

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, "queued-claimable-880", projectID)
	require.NoError(t, err)

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_runs
		    SET started_at = now() - interval '2 hours', last_step_at = NULL
		  WHERE id = ?`, run.ID)
	require.NoError(t, err)

	NewStaleRunReaper(repo, testAgentLogger()).reap(ctx)

	after, err := repo.FindRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusQueued, after.Status, "a claimable queued run is spared")

	job, err := repo.ClaimNextJobInQueue(ctx, projectID, DefaultQueueName)
	require.NoError(t, err)
	require.NotNil(t, job, "the spared run's job must still be claimable")
	require.Equal(t, run.ID, job.RunID)

	claimed, err := repo.FindRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusRunning, claimed.Status)
}

// TestReaperHealsQueuedRunThatLostItsJob pins the order inside the reaper: a
// run that merely lost its dispatch job row is recoverable, so the heal runs
// before the fail sweep and the run is dispatched instead of being failed.
func TestReaperHealsQueuedRunThatLostItsJob(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "queued_dispatch_lost_job")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)
	require.NoError(t, repo.EnsureDefaultQueue(ctx, projectID))

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, "queued-lost-job-880", projectID)
	require.NoError(t, err)

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx, `DELETE FROM kb.agent_run_jobs WHERE run_id = ?`, run.ID)
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_runs
		    SET started_at = now() - interval '2 hours', last_step_at = NULL
		  WHERE id = ?`, run.ID)
	require.NoError(t, err)

	NewStaleRunReaper(repo, testAgentLogger()).reap(ctx)

	after, err := repo.FindRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusQueued, after.Status,
		"a run that only lost its job row must be re-enqueued, not failed")

	var jobs int
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_run_jobs
		  WHERE run_id = ? AND status = 'pending' AND project_id = ?`,
		run.ID, projectID,
	).Scan(ctx, &jobs))
	require.Equal(t, 1, jobs, "the heal must restore exactly one claimable job")

	job, err := repo.ClaimNextJobInQueue(ctx, projectID, DefaultQueueName)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, run.ID, job.RunID)
}

// TestUndispatchableSweepNeedsIdleThreshold guards the age gate: a freshly
// queued run with no queue row yet is normal (the supervisor reconciles
// EnsureDefaultQueues on its own interval), so the sweep must not fail it
// before the threshold.
func TestUndispatchableSweepNeedsIdleThreshold(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "queued_dispatch_threshold")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, fmt.Sprintf("queued-threshold-880-%s", uuid.NewString()[:8]), projectID)
	require.NoError(t, err)

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)

	res, err := repo.ReapUndispatchableQueuedRuns(ctx, queuedRunDispatchThreshold)
	require.NoError(t, err)
	require.Zero(t, res.Failed, "a young queued run must be left alone")

	after, err := repo.FindRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusQueued, after.Status)
}

// TestSweepSparesDisabledQueueUntilReEnabled pins the difference between a
// disabled queue and an unroutable one. `enabled` is PATCHable
// (handler_queues.go) and EnsureDefaultQueue is ON CONFLICT DO NOTHING, so a
// disabled queue is stable but reversible: an operator may park `default` for a
// deploy window. Failing the backlog would destroy work that re-enabling brings
// straight back, so the sweep reports the runs as spared and a re-enabled queue
// dispatches them normally.
func TestSweepSparesDisabledQueueUntilReEnabled(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "queued_dispatch_disabled")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)
	require.NoError(t, repo.EnsureDefaultQueue(ctx, projectID))
	_, err := tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_queues SET enabled = false WHERE project_id = ? AND name = ?`,
		projectID, DefaultQueueName)
	require.NoError(t, err)

	agentID := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, "queued-disabled-880", projectID)
	require.NoError(t, err)

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_runs
		    SET started_at = now() - interval '2 hours', last_step_at = NULL
		  WHERE id = ?`, run.ID)
	require.NoError(t, err)

	res, err := repo.ReapUndispatchableQueuedRuns(ctx, queuedRunDispatchThreshold)
	require.NoError(t, err)
	require.Zero(t, res.Failed, "a parked queue must not cost the operator its backlog")
	require.Equal(t, 1, res.SparedDisabled)

	still, err := repo.FindRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusQueued, still.Status)
	require.Nil(t, still.ErrorMessage)

	// Re-enabling is the whole point: the backlog is still there and runs.
	_, err = tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_queues SET enabled = true WHERE project_id = ? AND name = ?`,
		projectID, DefaultQueueName)
	require.NoError(t, err)

	res, err = repo.ReapUndispatchableQueuedRuns(ctx, queuedRunDispatchThreshold)
	require.NoError(t, err)
	require.Zero(t, res.Failed)

	job, err := repo.ClaimNextJobInQueue(ctx, projectID, DefaultQueueName)
	require.NoError(t, err)
	require.NotNil(t, job, "the spared backlog must be claimable once the queue is re-enabled")
	require.Equal(t, run.ID, job.RunID)

	claimed, err := repo.FindRunByID(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusRunning, claimed.Status)
}
