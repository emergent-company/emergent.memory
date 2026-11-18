package agents

import (
	"context"
	"encoding/json"
	"strings"
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

// TestStaleRunReaper_ReapsAbandonedPausedWorkRun pins issue #993.
//
// A work run paused at its step limit is non-terminal, so it counts as a live
// run for its subject and the dispatch reconciler skips the item for ever while
// the item still reads `ready`. The reaper must cancel such a run once it has
// been idle past the park threshold and block the item with a reason, so the
// claim is released and the board can say why it stopped.
//
// A pause that is genuinely waiting on a human or a child must be spared: those
// pauses have an owner, and reclaiming them would run the subject twice.
func TestStaleRunReaper_ReapsAbandonedPausedWorkRun(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_paused_work_reaper")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB
	repo := NewRepository(db)
	projectID, agentID := seedReaperAgent(t, ctx, tdb, "paused-work")

	now := time.Now()
	insertPaused := func(subject *string, reason string, completedAt time.Time, stepCount int, maxSteps *int) string {
		t.Helper()
		id := uuid.NewString()
		var suspend any
		if reason != "" {
			raw, err := json.Marshal(map[string]string{"reason": reason})
			require.NoError(t, err)
			suspend = string(raw)
		}
		_, err := db.ExecContext(ctx, `
			INSERT INTO kb.agent_runs
				(id, agent_id, status, started_at, completed_at, step_count, max_steps, subject_object_id, suspend_context)
			VALUES (?,?,?,?,?,?,?,?,?::jsonb)`,
			id, agentID, string(RunStatusPaused), completedAt.Add(-time.Hour), completedAt,
			stepCount, maxSteps, subject, suspend)
		require.NoError(t, err)
		return id
	}
	subject := func() *string { s := uuid.NewString(); return &s }
	old := now.Add(-2 * time.Hour)
	fresh := now.Add(-time.Minute)

	stepLimitSubject := subject()
	maxSteps := 120
	stepLimitID := insertPaused(stepLimitSubject, string(SuspendReasonStepLimit), old, 120, &maxSteps)

	freshSubject := subject()
	freshID := insertPaused(freshSubject, string(SuspendReasonStepLimit), fresh, 120, &maxSteps)

	pendingQuestionSubject := subject()
	pendingQuestionID := insertPaused(pendingQuestionSubject, string(SuspendReasonAwaitingHuman), old, 4, nil)
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agent_questions (run_id, agent_id, project_id, question) VALUES (?,?,?,?)`,
		pendingQuestionID, agentID, projectID, "Which branch should I use?")
	require.NoError(t, err)

	toolConfirmSubject := subject()
	toolConfirmID := insertPaused(toolConfirmSubject, string(SuspendReasonAwaitingToolConfirm), old, 4, nil)

	awaitingChildSubject := subject()
	awaitingChildID := insertPaused(awaitingChildSubject, string(SuspendReasonAwaitingChild), old, 4, nil)

	noMarkerSubject := subject()
	noMarkerID := insertPaused(noMarkerSubject, "", old, 2, nil)

	// A paused run with no subject is not work-owned (share/chat/session); it is
	// out of scope for this reaper and must be left to its own timeout path.
	nonWorkID := insertPaused(nil, string(SuspendReasonStepLimit), old, 120, &maxSteps)

	NewStaleRunReaper(repo, testAgentLogger()).reapAbandonedPausedWorkRuns(ctx)

	statusOf := func(id string) string {
		t.Helper()
		var s string
		require.NoError(t, db.NewRaw(`SELECT status FROM kb.agent_runs WHERE id = ?`, id).Scan(ctx, &s))
		return s
	}
	blockReasonOf := func(subject string) (int, string) {
		t.Helper()
		var count int
		require.NoError(t, db.NewRaw(
			`SELECT COUNT(*) FROM kb.work_blocks WHERE canonical_id = ?`, subject,
		).Scan(ctx, &count))
		if count == 0 {
			return 0, ""
		}
		var reason string
		require.NoError(t, db.NewRaw(
			`SELECT reason FROM kb.work_blocks WHERE canonical_id = ?`, subject,
		).Scan(ctx, &reason))
		return count, reason
	}

	require.Equal(t, string(RunStatusCancelled), statusOf(stepLimitID),
		"an abandoned step-limit pause must be cancelled to release the claim")
	count, reason := blockReasonOf(*stepLimitSubject)
	require.Equal(t, 1, count, "the item must be blocked so the reconciler does not immediately re-dispatch it")
	require.Contains(t, reason, "step limit")
	require.Contains(t, reason, "120/120", "the block reason must report the budget condition")
	live, err := repo.HasLiveRunForSubject(ctx, *stepLimitSubject)
	require.NoError(t, err)
	require.False(t, live, "the reaped run must no longer count as live")

	require.Equal(t, string(RunStatusPaused), statusOf(freshID),
		"a pause inside the threshold must be left for a human")
	if n, _ := blockReasonOf(*freshSubject); n != 0 {
		t.Fatalf("a fresh pause must not block its item")
	}

	require.Equal(t, string(RunStatusPaused), statusOf(pendingQuestionID),
		"an ask_user pause has a pending question and must be spared")
	if n, _ := blockReasonOf(*pendingQuestionSubject); n != 0 {
		t.Fatalf("an ask_user pause must not block its item")
	}

	require.Equal(t, string(RunStatusPaused), statusOf(toolConfirmID),
		"a tool-confirmation pause expects a resume and must be spared")
	if n, _ := blockReasonOf(*toolConfirmSubject); n != 0 {
		t.Fatalf("a tool-confirmation pause must not block its item")
	}

	require.Equal(t, string(RunStatusPaused), statusOf(awaitingChildID),
		"a pause waiting on a child run must be spared")
	if n, _ := blockReasonOf(*awaitingChildSubject); n != 0 {
		t.Fatalf("a child-wait pause must not block its item")
	}

	require.Equal(t, string(RunStatusCancelled), statusOf(noMarkerID),
		"a pause with no marker and nothing pending can never be woken and must be reaped")
	if n, reason := blockReasonOf(*noMarkerSubject); n != 1 || !strings.Contains(reason, "nothing pending") {
		t.Fatalf("a markerless abandoned pause must be blocked with a reason, got count=%d reason=%q", n, reason)
	}

	require.Equal(t, string(RunStatusPaused), statusOf(nonWorkID),
		"a paused run with no work subject is not owned by this reaper")
}
