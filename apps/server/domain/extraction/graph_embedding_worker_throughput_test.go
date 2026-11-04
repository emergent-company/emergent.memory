package extraction

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
)

// These tests pin the #743 throughput fix: batch admission is decoupled from
// per-job completion (no whole-batch wg.Wait), in-flight work is bounded by the
// configured concurrency, and objects are embedded with multi-object requests
// grouped per project. They run against a throwaway database created by
// internal/testdb (TEST_DATABASE_URL + REQUIRE_DB); they skip cleanly when no
// database is available.

func openEmbeddingWorkerTestDB(t *testing.T) (context.Context, bun.IDB) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "embworker")
	t.Cleanup(tdb.Close)
	return ctx, tdb.GetDB()
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedEmbeddingProject inserts an org + project and returns the project ID.
func seedEmbeddingProject(t *testing.T, ctx context.Context, db bun.IDB) string {
	t.Helper()
	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Embedding Throughput Org', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Embedding Throughput Project', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)
	return projectID
}

// seedEmbeddingObject inserts a graph object whose extracted embedding text
// contains name, and returns the object ID.
func seedEmbeddingObject(t *testing.T, ctx context.Context, db bun.IDB, projectID, name string) string {
	t.Helper()
	id := uuid.NewString()
	props := `{"name":"` + name + `"}`
	_, err := db.NewRaw(`INSERT INTO kb.graph_objects
		(id, project_id, canonical_id, version, type, status, properties, labels, created_at, updated_at)
		VALUES (?, ?, ?, 1, 'Thing', 'active', ?::jsonb, '{}'::text[], now(), now())`,
		id, projectID, id, props).Exec(ctx)
	require.NoError(t, err)
	return id
}

// seedPendingEmbeddingJob inserts a pending graph embedding job for objectID.
func seedPendingEmbeddingJob(t *testing.T, ctx context.Context, db bun.IDB, objectID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.graph_embedding_jobs
		(id, object_id, status, attempt_count, scheduled_at, created_at, updated_at)
		VALUES (?, ?, 'pending', 0, now(), now(), now())`, id, objectID).Exec(ctx)
	require.NoError(t, err)
	return id
}

// jobStatusCounts returns job counts keyed by status.
func jobStatusCounts(t *testing.T, ctx context.Context, db bun.IDB) map[string]int {
	t.Helper()
	var rows []struct {
		Status string `bun:"status"`
		Count  int    `bun:"count"`
	}
	err := db.NewRaw(`SELECT status, count(*) AS count FROM kb.graph_embedding_jobs GROUP BY status`).
		Scan(ctx, &rows)
	require.NoError(t, err)
	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[r.Status] = r.Count
	}
	return counts
}

func newTestEmbeddingConfig() *GraphEmbeddingConfig {
	cfg := DefaultGraphEmbeddingConfig()
	cfg.WorkerIntervalMs = 5000
	cfg.WorkerBatchSize = 50
	cfg.WorkerConcurrency = 10
	cfg.EmbeddingRequestBatchSize = 100
	cfg.EnableAdaptiveScaling = false
	return cfg
}

// gatedEmbedder is a single-object-only EmbeddingService (no
// EmbedDocumentsWithUsage) whose requests for text containing slowMarker block
// until gate is closed. It proves the straggler-decoupling behaviour: with no
// batch method each job runs in its own chunk.
type gatedEmbedder struct {
	gate        chan struct{}
	releaseOnce sync.Once
	slowMarker  string
	started     chan struct{}
	startOnce   sync.Once
}

func (e *gatedEmbedder) IsEnabled() bool { return true }

// release unblocks every gated request exactly once.
func (e *gatedEmbedder) release() {
	e.releaseOnce.Do(func() { close(e.gate) })
}

func (e *gatedEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	result, err := e.EmbedQueryWithUsage(ctx, query)
	if err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

func (e *gatedEmbedder) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	if strings.Contains(query, e.slowMarker) {
		e.startOnce.Do(func() { close(e.started) })
		select {
		case <-e.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &vertex.EmbedResult{Embedding: make([]float32, 768), Model: "test", Provider: "vertex"}, nil
}

// batchRecordingEmbedder implements both EmbeddingService and
// batchEmbeddingService, recording the document groups of every multi-object
// request.
type batchRecordingEmbedder struct {
	mu      sync.Mutex
	batches [][]string
	singles []string
}

func (e *batchRecordingEmbedder) IsEnabled() bool { return true }

func (e *batchRecordingEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	result, err := e.EmbedQueryWithUsage(ctx, query)
	if err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

func (e *batchRecordingEmbedder) EmbedQueryWithUsage(_ context.Context, query string) (*vertex.EmbedResult, error) {
	e.mu.Lock()
	e.singles = append(e.singles, query)
	e.mu.Unlock()
	return &vertex.EmbedResult{Embedding: make([]float32, 768), Model: "test", Provider: "vertex"}, nil
}

func (e *batchRecordingEmbedder) EmbedDocumentsWithUsage(_ context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	docs := append([]string(nil), documents...)
	e.mu.Lock()
	e.batches = append(e.batches, docs)
	e.mu.Unlock()

	vectors := make([][]float32, len(docs))
	for i := range docs {
		vectors[i] = make([]float32, 768)
	}
	return &vertex.BatchEmbedResult{
		Embeddings: vectors,
		Usage:      &vertex.Usage{PromptTokens: len(docs) * 8, TotalTokens: len(docs) * 8},
		Model:      "test",
		Provider:   "vertex",
	}, nil
}

func (e *batchRecordingEmbedder) batchSizes() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([][]string, len(e.batches))
	copy(out, e.batches)
	return out
}

// TestGraphEmbeddingWorker_StragglerDoesNotGateBatch is the core regression
// test: one slow embedding request must not delay the completion of the other
// jobs, and must not block the poll loop (processBatch returns immediately).
func TestGraphEmbeddingWorker_StragglerDoesNotGateBatch(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		seedPendingEmbeddingJob(t, ctx, db, seedEmbeddingObject(t, ctx, db, projectID, name))
	}
	stragglerJob := seedPendingEmbeddingJob(t, ctx, db, seedEmbeddingObject(t, ctx, db, projectID, "straggler-one"))

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "straggler", started: make(chan struct{})}
	cfg := newTestEmbeddingConfig()
	jobs := NewGraphEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewGraphEmbeddingWorker(jobs, emb, db, cfg, quietLogger(), nil, nil, nil, false)

	// Safety valve: never let a regression hang the suite.
	go func() {
		select {
		case <-emb.started:
		case <-time.After(5 * time.Second):
		}
		time.Sleep(4 * time.Second)
		emb.release()
	}()

	start := time.Now()
	require.NoError(t, worker.processBatch(ctx))
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 500*time.Millisecond,
		"processBatch must return without waiting for the slow job (took %s)", elapsed)

	// The three fast jobs complete while the straggler is still blocked.
	require.Eventually(t, func() bool {
		return jobStatusCounts(t, ctx, db)["completed"] == 3
	}, 3*time.Second, 20*time.Millisecond,
		"the straggler must not gate the other jobs' completion")

	counts := jobStatusCounts(t, ctx, db)
	assert.Equal(t, 1, counts["processing"], "the straggler is still processing")
	assert.Equal(t, 0, counts["failed"], "no job may be failed by the straggler")

	// Release the straggler: it must still complete (no job lost).
	emb.release()
	require.Eventually(t, func() bool {
		return jobStatusCounts(t, ctx, db)["completed"] == 4
	}, 5*time.Second, 50*time.Millisecond, "the straggler must eventually complete")

	var unembedded int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_objects
		WHERE project_id = ? AND embedding_v2 IS NULL`, projectID).Scan(ctx, &unembedded))
	assert.Zero(t, unembedded, "every object must have an embedding")

	var stragglerStatus string
	require.NoError(t, db.NewRaw(`SELECT status FROM kb.graph_embedding_jobs WHERE id = ?`, stragglerJob).
		Scan(ctx, &stragglerStatus))
	assert.Equal(t, string(JobStatusCompleted), stragglerStatus)
}

// TestGraphEmbeddingWorker_EmbedsInBatchesPerProject proves objects are sent in
// multi-object requests and that requests are grouped per project (credentials
// and budgets are resolved per project).
func TestGraphEmbeddingWorker_EmbedsInBatchesPerProject(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectA := seedEmbeddingProject(t, ctx, db)
	projectB := seedEmbeddingProject(t, ctx, db)

	for _, name := range []string{"a-one", "a-two"} {
		seedPendingEmbeddingJob(t, ctx, db, seedEmbeddingObject(t, ctx, db, projectA, name))
	}
	for _, name := range []string{"b-one", "b-two"} {
		seedPendingEmbeddingJob(t, ctx, db, seedEmbeddingObject(t, ctx, db, projectB, name))
	}

	emb := &batchRecordingEmbedder{}
	cfg := newTestEmbeddingConfig()
	jobs := NewGraphEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewGraphEmbeddingWorker(jobs, emb, db, cfg, quietLogger(), nil, nil, nil, false)

	require.NoError(t, worker.processBatch(ctx))
	require.Eventually(t, func() bool {
		return jobStatusCounts(t, ctx, db)["completed"] == 4
	}, 5*time.Second, 20*time.Millisecond, "all four jobs must complete")

	batches := emb.batchSizes()
	require.Len(t, batches, 2, "one multi-object request per project")
	for _, batch := range batches {
		assert.Len(t, batch, 2, "each project's two objects must share one request")
	}
	assert.Empty(t, emb.singles, "no single-object request should be issued")
}

// TestGraphEmbeddingWorker_BoundsInFlight proves admission is capped at the
// configured concurrency: claiming more jobs than can run would leave them
// 'processing' but idle, exposed to the stale sweep.
func TestGraphEmbeddingWorker_BoundsInFlight(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	for i := 0; i < 5; i++ {
		seedPendingEmbeddingJob(t, ctx, db, seedEmbeddingObject(t, ctx, db, projectID, "obj"))
	}

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "Thing", started: make(chan struct{})}
	cfg := newTestEmbeddingConfig()
	cfg.WorkerConcurrency = 2
	jobs := NewGraphEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewGraphEmbeddingWorker(jobs, emb, db, cfg, quietLogger(), nil, nil, nil, false)

	require.NoError(t, worker.processBatch(ctx))
	// Second call must not claim anything: capacity is exhausted.
	require.NoError(t, worker.processBatch(ctx))

	counts := jobStatusCounts(t, ctx, db)
	assert.Equal(t, 2, counts["processing"], "in-flight jobs must be bounded by concurrency")
	assert.Equal(t, 3, counts["pending"], "remaining jobs must stay queued, not claimed")

	emb.release()
	require.Eventually(t, func() bool {
		return jobStatusCounts(t, ctx, db)["completed"] == 2
	}, 5*time.Second, 20*time.Millisecond)
}

// TestChunkJobs pins the pure chunking helper.
func TestChunkJobs(t *testing.T) {
	mk := func(n int) []*GraphEmbeddingJob {
		jobs := make([]*GraphEmbeddingJob, n)
		for i := range jobs {
			jobs[i] = &GraphEmbeddingJob{ID: uuid.NewString()}
		}
		return jobs
	}

	assert.Nil(t, chunkJobs(nil, 10))

	single := chunkJobs(mk(3), 10)
	require.Len(t, single, 1)
	assert.Len(t, single[0], 3)

	exact := chunkJobs(mk(4), 2)
	require.Len(t, exact, 2)
	assert.Len(t, exact[0], 2)
	assert.Len(t, exact[1], 2)

	ragged := chunkJobs(mk(5), 2)
	require.Len(t, ragged, 3)
	assert.Len(t, ragged[0], 2)
	assert.Len(t, ragged[1], 2)
	assert.Len(t, ragged[2], 1)

	perJob := chunkJobs(mk(3), 1)
	require.Len(t, perJob, 3)
}
