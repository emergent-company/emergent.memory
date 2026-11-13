package extraction

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// These tests pin the budget-blocked ingestion fix: budget-exceeded jobs are
// parked with bounded exponential backoff (durable last_error='budget_exceeded')
// instead of being reheated on a fixed 5-minute loop, and the budget is checked
// once per project per cache window instead of once per job. They reuse the
// helpers from graph_embedding_worker_throughput_test.go and
// embedding_worker_admission_test.go (same package) and skip cleanly when no
// database is available.

func TestBudgetBlockDelaySecBounded(t *testing.T) {
	const base = 60
	const max = 3600

	assert.Equal(t, base, BudgetBlockDelaySec(1, base, max), "attempt 1 must equal base")
	assert.Equal(t, base, BudgetBlockDelaySec(0, base, max), "attempt below 1 is treated as 1")
	assert.Equal(t, base, BudgetBlockDelaySec(-5, base, max), "negative attempt is treated as 1")

	prev := 0
	for attempt := 1; attempt <= 30; attempt++ {
		delay := BudgetBlockDelaySec(attempt, base, max)
		assert.GreaterOrEqual(t, delay, prev, "delay must be monotonic non-decreasing (attempt %d)", attempt)
		assert.LessOrEqual(t, delay, max, "delay must never exceed max (attempt %d)", attempt)
		prev = delay
	}

	assert.Equal(t, max, BudgetBlockDelaySec(30, base, max), "large attempts must clamp to max")
	assert.Equal(t, base, BudgetBlockDelaySec(1, 0, 0), "non-positive bounds fall back to defaults")
}

func TestRescheduleBudgetExceededForProjectParksJobs(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	docID := seedChunkDocument(t, ctx, db, projectID)

	attempts := []int{0, 1, 5}
	jobIDs := make([]string, len(attempts))
	for i, attempt := range attempts {
		chunkID := seedChunk(t, ctx, db, docID, i, "budget blocked chunk text")
		id := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.chunk_embedding_jobs
			(id, chunk_id, status, attempt_count, scheduled_at, created_at, updated_at)
			VALUES (?, ?, 'pending', ?, now(), now(), now())`, id, chunkID, attempt).Exec(ctx)
		require.NoError(t, err)
		jobIDs[i] = id
	}

	cfg := DefaultChunkEmbeddingConfig()
	svc := NewChunkEmbeddingJobsService(db, quietLogger(), cfg)

	n, err := svc.RescheduleBudgetExceededForProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, int64(3), n)

	type parkedRow struct {
		Status    string  `bun:"status"`
		LastError *string `bun:"last_error"`
		Attempt   int     `bun:"attempt_count"`
		DelaySecs float64 `bun:"delay_secs"`
	}
	delays := make(map[int]float64, len(jobIDs))
	for i, id := range jobIDs {
		row := &parkedRow{}
		err := db.NewRaw(`SELECT status, last_error, attempt_count,
			EXTRACT(EPOCH FROM (scheduled_at - now())) AS delay_secs
			FROM kb.chunk_embedding_jobs WHERE id = ?`, id).Scan(ctx, row)
		require.NoError(t, err)

		assert.Equal(t, "pending", row.Status)
		require.NotNil(t, row.LastError)
		assert.Equal(t, "budget_exceeded", *row.LastError)
		assert.Greater(t, row.DelaySecs, 30.0, "job must be parked well into the future, not retried immediately")
		assert.LessOrEqual(t, row.DelaySecs, float64(cfg.MaxRetryDelaySec)+5, "delay must stay bounded")
		delays[attempts[i]] = row.DelaySecs
	}

	// attempt_count 0 and 1 both map to base (60s); 5 maps to 2^4*60 = 960s.
	assert.InDelta(t, 60, delays[0], 5, "attempt 0 parks at base delay")
	assert.InDelta(t, 60, delays[1], 5, "attempt 1 parks at base delay")
	assert.InDelta(t, 960, delays[5], 30, "attempt 5 parks at 2^4 * base")
	assert.Greater(t, delays[5], delays[1], "higher attempt must back off longer")
}

// countingBudgetChecker is a fake BudgetChecker that records how often it is
// consulted, so tests can prove the per-project cache avoids per-job queries.
type countingBudgetChecker struct {
	mu       sync.Mutex
	calls    int
	exceeded bool
}

func (c *countingBudgetChecker) CheckBudgetExceeded(context.Context, string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.exceeded, nil
}

func (c *countingBudgetChecker) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestChunkEmbeddingWorkerBudgetParksWithoutPerJobBudgetChecks(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	docID := seedChunkDocument(t, ctx, db, projectID)

	// job1 is due now at attempt 0. job2 carries a prior attempt and is not yet
	// due: the first budget hit parks job1 but must not touch job2, so when job2
	// is later reheated its attempt grows and its backoff is observably larger.
	job1Chunk := seedChunk(t, ctx, db, docID, 0, "budget blocked chunk one")
	job2Chunk := seedChunk(t, ctx, db, docID, 1, "budget blocked chunk two")
	job1ID := uuid.NewString()
	job2ID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.chunk_embedding_jobs
		(id, chunk_id, status, attempt_count, scheduled_at, created_at, updated_at)
		VALUES (?, ?, 'pending', 0, now(), now(), now())`, job1ID, job1Chunk).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.chunk_embedding_jobs
		(id, chunk_id, status, attempt_count, scheduled_at, created_at, updated_at)
		VALUES (?, ?, 'pending', 1, now() + interval '1 hour', now(), now())`, job2ID, job2Chunk).Exec(ctx)
	require.NoError(t, err)

	cfg := DefaultChunkEmbeddingConfig()
	cfg.WorkerBatchSize = 10
	cfg.WorkerConcurrency = 2
	cfg.EnableAdaptiveScaling = false
	jobsSvc := NewChunkEmbeddingJobsService(db, quietLogger(), cfg)

	checker := &countingBudgetChecker{exceeded: true}
	// nil embed service is fine: the budget path returns before embedding.
	worker := NewChunkEmbeddingWorker(jobsSvc, nil, db, cfg, quietLogger(), nil, nil, checker, true)

	claimed, err := jobsSvc.Dequeue(ctx, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, job1ID, claimed[0].ID)

	err = worker.processJob(ctx, claimed[0])
	var budgetErr *EmbeddingBudgetExceededError
	require.ErrorAs(t, err, &budgetErr)
	assert.Equal(t, projectID, budgetErr.ProjectID)
	assert.Equal(t, 1, checker.callCount(), "first job must perform exactly one budget check")

	firstDelay := chunkJobDelaySecs(t, ctx, db, job1ID)
	assert.Greater(t, firstDelay, 30.0, "job1 must be parked, not retried immediately")
	assert.LessOrEqual(t, firstDelay, float64(cfg.MaxRetryDelaySec)+5)

	// Reheat the second job and process it. The project is now cached as over
	// budget, so no further budget query may happen.
	_, err = db.NewRaw(`UPDATE kb.chunk_embedding_jobs
		SET scheduled_at = now(), updated_at = now() WHERE id = ?`, job2ID).Exec(ctx)
	require.NoError(t, err)

	claimed, err = jobsSvc.Dequeue(ctx, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, job2ID, claimed[0].ID)

	err = worker.processJob(ctx, claimed[0])
	require.ErrorAs(t, err, &budgetErr)
	assert.Equal(t, 1, checker.callCount(), "cached over-budget project must not be re-queried per job")

	secondDelay := chunkJobDelaySecs(t, ctx, db, job2ID)
	assert.Greater(t, secondDelay, firstDelay,
		"backoff must grow with the attempt count, not be a fixed 5-minute delay")
	assert.LessOrEqual(t, secondDelay, float64(cfg.MaxRetryDelaySec)+5, "delay must stay bounded")

	for _, id := range []string{job1ID, job2ID} {
		var row struct {
			Status    string  `bun:"status"`
			LastError *string `bun:"last_error"`
			DelaySecs float64 `bun:"delay_secs"`
		}
		err := db.NewRaw(`SELECT status, last_error,
			EXTRACT(EPOCH FROM (scheduled_at - now())) AS delay_secs
			FROM kb.chunk_embedding_jobs WHERE id = ?`, id).Scan(ctx, &row)
		require.NoError(t, err)
		assert.Equal(t, "pending", row.Status)
		require.NotNil(t, row.LastError)
		assert.Equal(t, "budget_exceeded", *row.LastError)
		assert.Greater(t, row.DelaySecs, 30.0, "parked job must not be due immediately")
	}
}

// chunkJobDelaySecs returns how many seconds into the future a job is scheduled.
func chunkJobDelaySecs(t *testing.T, ctx context.Context, db bun.IDB, jobID string) float64 {
	t.Helper()
	var delay float64
	err := db.NewRaw(`SELECT EXTRACT(EPOCH FROM (scheduled_at - now())) AS delay_secs
		FROM kb.chunk_embedding_jobs WHERE id = ?`, jobID).Scan(ctx, &delay)
	require.NoError(t, err)
	return delay
}
