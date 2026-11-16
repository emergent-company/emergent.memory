package agents

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// seedQueuedRunWithLostJob seeds a project + agent + queued run and then deletes
// the run's dispatch job, which is the shape RequeueOrphanedQueuedRuns heals.
func seedQueuedRunWithLostJob(t *testing.T, suffix string) (*Repository, string, string) {
	t.Helper()
	ctx := context.Background()

	tdb, projectID := seedAgentDeleteProject(t, suffix)
	repo := NewRepository(tdb.DB)

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, "queued-requeue-"+uuid.NewString()[:8], projectID)
	require.NoError(t, err)

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)
	require.Equal(t, RunStatusQueued, run.Status)

	_, err = tdb.DB.ExecContext(ctx,
		`DELETE FROM kb.agent_run_jobs WHERE run_id = ?`, run.ID)
	require.NoError(t, err)

	return repo, projectID, run.ID
}

func countLiveJobs(t *testing.T, repo *Repository, runID string) int {
	t.Helper()
	var n int
	require.NoError(t, repo.db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_run_jobs
		  WHERE run_id = ? AND status IN ('pending', 'processing')`, runID,
	).Scan(context.Background(), &n))
	return n
}

// TestRequeueOrphanedQueuedRunsIsRaceSafe pins issue #905.
//
// RequeueOrphanedQueuedRuns is a read-then-insert: it selects queued runs with
// no live dispatch job and inserts one. It runs on every process boot and every
// five minutes per process, so several instances run it concurrently for the
// same run. Before the partial unique index
// uq_agent_run_jobs_one_live_per_run (migration 00312) and the requeue's
// ON CONFLICT DO NOTHING, each caller inserted its own row: six concurrent
// calls left two or more pending jobs for one run, inflating the row count
// CountPendingJobsForAgent feeds to the AGENT_MAX_PENDING_JOBS guard.
//
// The heal must converge on exactly one live job, and only one caller may report
// an insert, however the calls interleave.
func TestRequeueOrphanedQueuedRunsIsRaceSafe(t *testing.T) {
	ctx := context.Background()
	repo, projectID, runID := seedQueuedRunWithLostJob(t, "queued_requeue_race")

	const callers = 6
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, callers)
	counts := make([]int, callers)
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // maximize the chance every caller observes "no live job"
			counts[i], errs[i] = repo.RequeueOrphanedQueuedRuns(ctx)
		}(i)
	}
	close(start)
	wg.Wait()

	insertedTotal := 0
	for i := range callers {
		require.NoErrorf(t, errs[i], "concurrent requeue caller %d", i)
		insertedTotal += counts[i]
	}

	require.Equal(t, 1, countLiveJobs(t, repo, runID),
		"concurrent requeues must leave exactly one live job for the run")
	require.Equal(t, 1, insertedTotal,
		"exactly one caller may report an insert; the losers are ON CONFLICT no-ops")

	// The surviving row must be the claimable one, and the duplicate-free
	// recovery must still dispatch normally.
	job, err := repo.ClaimNextJobInQueue(ctx, projectID, DefaultQueueName)
	require.NoError(t, err)
	require.NotNil(t, job, "the requeued job must be claimable")
	require.Equal(t, runID, job.RunID)

	claimed, err := repo.FindRunByID(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, RunStatusRunning, claimed.Status)
}

// TestCreateRunJobIsIdempotentPerLiveJob pins the database-level invariant the
// requeue relies on: at most one pending/processing row per run, enforced by the
// partial unique index rather than by a check-then-act, while terminal rows stay
// unconstrained so a run can legitimately be dispatched again after its job
// completed.
func TestCreateRunJobIsIdempotentPerLiveJob(t *testing.T) {
	ctx := context.Background()
	repo, _, runID := seedQueuedRunWithLostJob(t, "queued_requeue_idempotent")

	inserted, err := repo.CreateRunJob(ctx, runID, 1, "", 0)
	require.NoError(t, err)
	require.True(t, inserted, "the orphaned run's job must be restored")
	require.Equal(t, 1, countLiveJobs(t, repo, runID))

	// A second insert while a live job exists is a no-op, not a second row.
	inserted, err = repo.CreateRunJob(ctx, runID, 1, "", 0)
	require.NoError(t, err)
	require.False(t, inserted, "a live job already exists; the insert must be a no-op")
	require.Equal(t, 1, countLiveJobs(t, repo, runID))

	// The index itself rejects a duplicate written around the repository, so
	// every insert path — including one added later — is covered.
	_, err = repo.db.ExecContext(ctx,
		`INSERT INTO kb.agent_run_jobs (run_id, status) VALUES (?, 'pending')`, runID)
	require.ErrorContains(t, err, "uq_agent_run_jobs_one_live_per_run",
		"the partial unique index must reject a second live job for the run")

	// The predicate is limited to live rows: once the run's job is terminal a
	// fresh dispatch row can be inserted.
	_, err = repo.db.ExecContext(ctx,
		`UPDATE kb.agent_run_jobs
		    SET status = 'completed', completed_at = now()
		  WHERE run_id = ? AND status IN ('pending', 'processing')`, runID)
	require.NoError(t, err)
	inserted, err = repo.CreateRunJob(ctx, runID, 1, "", 0)
	require.NoError(t, err)
	require.True(t, inserted, "a terminal job must not block the next dispatch row")
	require.Equal(t, 1, countLiveJobs(t, repo, runID))
}

// TestCreateRunJobEmitsOnConflictDoNothing keeps the requeue's conflict handling
// honest without a database: the insert must carry ON CONFLICT DO NOTHING (so a
// concurrent caller's row is a no-op rather than a unique violation), and the
// returned bool must follow the affected-row count so the requeue reports only
// rows it actually inserted.
func TestCreateRunJobEmitsOnConflictDoNothing(t *testing.T) {
	ctx := context.Background()
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	repo := NewRepository(bun.NewDB(sqldb, pgdialect.New()))

	// The routing lookup is best-effort: an unloadable run falls back to the
	// default queue and the insert still runs.
	const runLookup = `(?s)SELECT.*FROM "kb"."agent_runs"`
	// bun inserts with RETURNING (the table has defaulted columns), so the
	// affected-row count is the number of rows it scans back: none when the
	// ON CONFLICT DO NOTHING absorbed the insert, one otherwise.
	const jobInsert = `(?s)INSERT INTO "kb"."agent_run_jobs".*ON CONFLICT DO NOTHING`
	returning := []string{"id", "attempt_count", "project_id", "created_at", "completed_at"}

	mock.ExpectQuery(runLookup).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(jobInsert).WillReturnRows(sqlmock.NewRows(returning))
	inserted, err := repo.CreateRunJob(ctx, uuid.NewString(), 1, "", 0)
	require.NoError(t, err)
	require.False(t, inserted, "a conflict-absorbed insert must report no row")

	mock.ExpectQuery(runLookup).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(jobInsert).WillReturnRows(
		sqlmock.NewRows(returning).AddRow(uuid.NewString(), 0, nil, time.Now(), nil))
	inserted, err = repo.CreateRunJob(ctx, uuid.NewString(), 1, "", 0)
	require.NoError(t, err)
	require.True(t, inserted, "an inserted row must be reported")

	require.NoError(t, mock.ExpectationsWereMet())
}
