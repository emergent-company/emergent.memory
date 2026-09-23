package extraction

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// These tests pin the #795 admission design for the chunk and relationship
// embedding workers: batch admission is decoupled from per-job completion (no
// whole-batch wg.Wait), in-flight work is bounded by the configured concurrency,
// and a slow request never gates the rest of the queue. They reuse the helpers
// from graph_embedding_worker_throughput_test.go (same package) and run against
// a throwaway database; they skip cleanly when no database is available.

func newTestChunkWorkerConfig() *ChunkEmbeddingConfig {
	cfg := DefaultChunkEmbeddingConfig()
	cfg.WorkerIntervalMs = 5000
	cfg.WorkerBatchSize = 50
	cfg.WorkerConcurrency = 2
	cfg.EnableAdaptiveScaling = false
	return cfg
}

func newTestRelWorkerConfig() *GraphEmbeddingConfig {
	cfg := DefaultGraphEmbeddingConfig()
	cfg.WorkerIntervalMs = 5000
	cfg.WorkerBatchSize = 50
	cfg.WorkerConcurrency = 2
	cfg.EnableAdaptiveScaling = false
	return cfg
}

// seedChunkDocument inserts a document and returns its ID.
func seedChunkDocument(t *testing.T, ctx context.Context, db bun.IDB, projectID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.documents (id, project_id, created_at, updated_at)
		VALUES (?, ?, now(), now())`, id, projectID).Exec(ctx)
	require.NoError(t, err)
	return id
}

// seedChunk inserts a chunk for a document and returns its ID.
func seedChunk(t *testing.T, ctx context.Context, db bun.IDB, documentID string, chunkIndex int, text string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.chunks (id, document_id, chunk_index, text, created_at, updated_at)
		VALUES (?, ?, ?, ?, now(), now())`, id, documentID, chunkIndex, text).Exec(ctx)
	require.NoError(t, err)
	return id
}

// seedPendingChunkJob inserts a pending chunk embedding job and returns its ID.
func seedPendingChunkJob(t *testing.T, ctx context.Context, db bun.IDB, chunkID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.chunk_embedding_jobs
		(id, chunk_id, status, attempt_count, scheduled_at, created_at, updated_at)
		VALUES (?, ?, 'pending', 0, now(), now(), now())`, id, chunkID).Exec(ctx)
	require.NoError(t, err)
	return id
}

// chunkJobStatusCounts returns chunk embedding job counts keyed by status.
func chunkJobStatusCounts(t *testing.T, ctx context.Context, db bun.IDB) map[string]int {
	t.Helper()
	var rows []struct {
		Status string `bun:"status"`
		Count  int    `bun:"count"`
	}
	err := db.NewRaw(`SELECT status, count(*) AS count FROM kb.chunk_embedding_jobs GROUP BY status`).
		Scan(ctx, &rows)
	require.NoError(t, err)
	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[r.Status] = r.Count
	}
	return counts
}

// seedRelObject inserts a graph object for a relationship endpoint and returns
// its ID. It reuses seedEmbeddingObject, which sets canonical_id = id and leaves
// supersedes_id NULL so the relationship worker's join resolves it.
func seedRelObject(t *testing.T, ctx context.Context, db bun.IDB, projectID, name string) string {
	t.Helper()
	return seedEmbeddingObject(t, ctx, db, projectID, name)
}

// seedRelPair inserts a src/dst object pair plus the relationship between them,
// and returns the relationship ID.
func seedRelPair(t *testing.T, ctx context.Context, db bun.IDB, projectID, srcName, dstName string) string {
	t.Helper()
	srcID := seedRelObject(t, ctx, db, projectID, srcName)
	dstID := seedRelObject(t, ctx, db, projectID, dstName)
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.graph_relationships
		(id, project_id, type, src_id, dst_id, properties, canonical_id, version, created_at)
		VALUES (?, ?, 'relates_to', ?, ?, '{}'::jsonb, ?, 1, now())`,
		id, projectID, srcID, dstID, id).Exec(ctx)
	require.NoError(t, err)
	return id
}

// seedPendingRelJob inserts a pending relationship embedding job and returns its ID.
func seedPendingRelJob(t *testing.T, ctx context.Context, db bun.IDB, relationshipID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.graph_relationship_embedding_jobs
		(id, relationship_id, status, priority, attempt_count, scheduled_at, created_at, updated_at)
		VALUES (?, ?, 'pending', 0, 0, now(), now(), now())`, id, relationshipID).Exec(ctx)
	require.NoError(t, err)
	return id
}

// relJobStatusCounts returns relationship embedding job counts keyed by status.
func relJobStatusCounts(t *testing.T, ctx context.Context, db bun.IDB) map[string]int {
	t.Helper()
	var rows []struct {
		Status string `bun:"status"`
		Count  int    `bun:"count"`
	}
	err := db.NewRaw(`SELECT status, count(*) AS count FROM kb.graph_relationship_embedding_jobs GROUP BY status`).
		Scan(ctx, &rows)
	require.NoError(t, err)
	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[r.Status] = r.Count
	}
	return counts
}

// TestChunkEmbeddingWorker_StragglerDoesNotGateBatch proves one slow chunk does
// not gate the rest of the batch nor the poll loop (processBatch returns
// immediately).
func TestChunkEmbeddingWorker_StragglerDoesNotGateBatch(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	docID := seedChunkDocument(t, ctx, db, projectID)

	for i, text := range []string{"alpha text", "beta text", "gamma text"} {
		seedPendingChunkJob(t, ctx, db, seedChunk(t, ctx, db, docID, i, text))
	}
	slowChunk := seedChunk(t, ctx, db, docID, 3, "straggler chunk text")
	seedPendingChunkJob(t, ctx, db, slowChunk)

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "straggler", started: make(chan struct{})}
	cfg := newTestChunkWorkerConfig()
	cfg.WorkerConcurrency = 10
	jobs := NewChunkEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewChunkEmbeddingWorker(jobs, emb, db, cfg, quietLogger(), nil, nil, nil, false)

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

	require.Eventually(t, func() bool {
		return chunkJobStatusCounts(t, ctx, db)["completed"] == 3
	}, 3*time.Second, 20*time.Millisecond,
		"the straggler must not gate the other jobs' completion")

	counts := chunkJobStatusCounts(t, ctx, db)
	assert.Equal(t, 1, counts["processing"], "the straggler is still processing")
	assert.Equal(t, 0, counts["failed"], "no job may be failed by the straggler")

	// Release the straggler: it must still complete (no job lost).
	emb.release()
	require.Eventually(t, func() bool {
		return chunkJobStatusCounts(t, ctx, db)["completed"] == 4
	}, 5*time.Second, 50*time.Millisecond, "the straggler must eventually complete")

	var unembedded int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.chunks
		WHERE document_id = ? AND embedding IS NULL`, docID).Scan(ctx, &unembedded))
	assert.Zero(t, unembedded, "every chunk must have an embedding")

	var processing int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.chunk_embedding_jobs
		WHERE status = 'processing'`).Scan(ctx, &processing))
	assert.Zero(t, processing, "no job may be left processing")
}

// TestChunkEmbeddingWorker_BoundsInFlight proves admission is capped at the
// configured concurrency: claiming more jobs than can run would leave them
// 'processing' but idle, exposed to the stale sweep.
func TestChunkEmbeddingWorker_BoundsInFlight(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	docID := seedChunkDocument(t, ctx, db, projectID)

	for i := 0; i < 5; i++ {
		seedPendingChunkJob(t, ctx, db, seedChunk(t, ctx, db, docID, i, "chunk text"))
	}

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "chunk", started: make(chan struct{})}
	cfg := newTestChunkWorkerConfig() // concurrency 2
	jobs := NewChunkEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewChunkEmbeddingWorker(jobs, emb, db, cfg, quietLogger(), nil, nil, nil, false)

	require.NoError(t, worker.processBatch(ctx))
	// Second call must not claim anything: capacity is exhausted.
	require.NoError(t, worker.processBatch(ctx))

	counts := chunkJobStatusCounts(t, ctx, db)
	assert.Equal(t, 2, counts["processing"], "in-flight jobs must be bounded by concurrency")
	assert.Equal(t, 3, counts["pending"], "remaining jobs must stay queued, not claimed")

	emb.release()
	require.Eventually(t, func() bool {
		return chunkJobStatusCounts(t, ctx, db)["completed"] == 2
	}, 5*time.Second, 20*time.Millisecond)
}

// TestGraphRelationshipEmbeddingWorker_StragglerDoesNotGateBatch mirrors the
// chunk straggler test for relationship jobs.
func TestGraphRelationshipEmbeddingWorker_StragglerDoesNotGateBatch(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	for _, pair := range [][2]string{{"alpha", "x"}, {"beta", "y"}, {"gamma", "z"}} {
		seedPendingRelJob(t, ctx, db, seedRelPair(t, ctx, db, projectID, pair[0], pair[1]))
	}
	stragglerRel := seedRelPair(t, ctx, db, projectID, "straggler-one", "dst-straggler")
	seedPendingRelJob(t, ctx, db, stragglerRel)

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "straggler", started: make(chan struct{})}
	cfg := newTestRelWorkerConfig()
	cfg.WorkerConcurrency = 10
	jobs := NewGraphRelationshipEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewGraphRelationshipEmbeddingWorker(jobs, emb, db, cfg, nil, quietLogger(), nil, nil, false)

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

	require.Eventually(t, func() bool {
		return relJobStatusCounts(t, ctx, db)["completed"] == 3
	}, 3*time.Second, 20*time.Millisecond,
		"the straggler must not gate the other jobs' completion")

	counts := relJobStatusCounts(t, ctx, db)
	assert.Equal(t, 1, counts["processing"], "the straggler is still processing")
	assert.Equal(t, 0, counts["failed"], "no job may be failed by the straggler")

	// Release the straggler: it must still complete (no job lost).
	emb.release()
	require.Eventually(t, func() bool {
		return relJobStatusCounts(t, ctx, db)["completed"] == 4
	}, 5*time.Second, 50*time.Millisecond, "the straggler must eventually complete")

	var unembedded int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_relationships
		WHERE project_id = ? AND embedding IS NULL`, projectID).Scan(ctx, &unembedded))
	assert.Zero(t, unembedded, "every relationship must have an embedding")

	var processing int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_relationship_embedding_jobs
		WHERE status = 'processing'`).Scan(ctx, &processing))
	assert.Zero(t, processing, "no job may be left processing")
}

// TestGraphRelationshipEmbeddingWorker_BoundsInFlight proves relationship
// admission is capped at the configured concurrency.
func TestGraphRelationshipEmbeddingWorker_BoundsInFlight(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	for i := 0; i < 5; i++ {
		seedPendingRelJob(t, ctx, db, seedRelPair(t, ctx, db, projectID, "src", "dst"))
	}

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "src", started: make(chan struct{})}
	cfg := newTestRelWorkerConfig() // concurrency 2
	jobs := NewGraphRelationshipEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewGraphRelationshipEmbeddingWorker(jobs, emb, db, cfg, nil, quietLogger(), nil, nil, false)

	require.NoError(t, worker.processBatch(ctx))
	// Second call must not claim anything: capacity is exhausted.
	require.NoError(t, worker.processBatch(ctx))

	counts := relJobStatusCounts(t, ctx, db)
	assert.Equal(t, 2, counts["processing"], "in-flight jobs must be bounded by concurrency")
	assert.Equal(t, 3, counts["pending"], "remaining jobs must stay queued, not claimed")

	emb.release()
	require.Eventually(t, func() bool {
		return relJobStatusCounts(t, ctx, db)["completed"] == 2
	}, 5*time.Second, 20*time.Millisecond)
}

// TestChunkEmbeddingWorker_StopDrains proves Stop waits for in-flight jobs and
// leaves none 'processing' after it returns.
func TestChunkEmbeddingWorker_StopDrains(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	docID := seedChunkDocument(t, ctx, db, projectID)
	seedPendingChunkJob(t, ctx, db, seedChunk(t, ctx, db, docID, 0, "slow chunk text"))

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "slow", started: make(chan struct{})}
	cfg := newTestChunkWorkerConfig()
	cfg.WorkerIntervalMs = 30000 // ticker never fires during the test
	cfg.WorkerConcurrency = 2
	jobs := NewChunkEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewChunkEmbeddingWorker(jobs, emb, db, cfg, quietLogger(), nil, nil, nil, false)

	startCtx := context.Background()
	require.NoError(t, worker.Start(startCtx))
	// let one-shot startup recovery finish
	time.Sleep(500 * time.Millisecond)

	require.NoError(t, worker.processBatch(startCtx))

	select {
	case <-emb.started:
	case <-time.After(3 * time.Second):
		t.Fatal("slow job never started")
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		emb.release()
	}()

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, worker.Stop(stopCtx))

	counts := chunkJobStatusCounts(t, ctx, db)
	assert.Equal(t, 1, counts["completed"], "drained job must complete")
	assert.Equal(t, 0, counts["processing"], "no job left processing after Stop")
	assert.Equal(t, 0, counts["failed"], "no job failed")

	var unembedded int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.chunks
		WHERE document_id = ? AND embedding IS NULL`, docID).Scan(ctx, &unembedded))
	assert.Zero(t, unembedded, "chunk must have an embedding")
}

// TestGraphRelationshipEmbeddingWorker_StopDrains mirrors the chunk StopDrains
// test for relationship jobs.
func TestGraphRelationshipEmbeddingWorker_StopDrains(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	seedPendingRelJob(t, ctx, db, seedRelPair(t, ctx, db, projectID, "src-slow", "dst-slow"))

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "slow", started: make(chan struct{})}
	cfg := newTestRelWorkerConfig()
	cfg.WorkerIntervalMs = 30000 // ticker never fires during the test
	cfg.WorkerConcurrency = 2
	jobs := NewGraphRelationshipEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewGraphRelationshipEmbeddingWorker(jobs, emb, db, cfg, nil, quietLogger(), nil, nil, false)

	startCtx := context.Background()
	require.NoError(t, worker.Start(startCtx))
	// let one-shot startup recovery finish
	time.Sleep(500 * time.Millisecond)

	require.NoError(t, worker.processBatch(startCtx))

	select {
	case <-emb.started:
	case <-time.After(3 * time.Second):
		t.Fatal("slow job never started")
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		emb.release()
	}()

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, worker.Stop(stopCtx))

	counts := relJobStatusCounts(t, ctx, db)
	assert.Equal(t, 1, counts["completed"], "drained job must complete")
	assert.Equal(t, 0, counts["processing"], "no job left processing after Stop")
	assert.Equal(t, 0, counts["failed"], "no job failed")

	var unembedded int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_relationships
		WHERE project_id = ? AND embedding IS NULL`, projectID).Scan(ctx, &unembedded))
	assert.Zero(t, unembedded, "relationship must have an embedding")
}

// TestChunkEmbeddingWorker_WakeRefillClaimsBeforeNextTick proves a finishing job
// frees capacity that is reclaimed via wakeCh, not the poll tick.
func TestChunkEmbeddingWorker_WakeRefillClaimsBeforeNextTick(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	docID := seedChunkDocument(t, ctx, db, projectID)

	for i, text := range []string{"slow chunk one", "slow chunk two"} {
		seedPendingChunkJob(t, ctx, db, seedChunk(t, ctx, db, docID, i, text))
	}

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "slow", started: make(chan struct{})}
	cfg := newTestChunkWorkerConfig()
	cfg.WorkerIntervalMs = 30000 // ticker never fires during the test
	cfg.WorkerConcurrency = 1    // second job only claimable after the first frees capacity
	jobs := NewChunkEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewChunkEmbeddingWorker(jobs, emb, db, cfg, quietLogger(), nil, nil, nil, false)

	startCtx := context.Background()
	require.NoError(t, worker.Start(startCtx))
	// let one-shot startup recovery finish
	time.Sleep(500 * time.Millisecond)

	require.NoError(t, worker.processBatch(startCtx))

	select {
	case <-emb.started:
	case <-time.After(3 * time.Second):
		t.Fatal("slow job never started")
	}

	counts := chunkJobStatusCounts(t, ctx, db)
	assert.Equal(t, 1, counts["processing"], "only one job in flight at concurrency 1")
	assert.Equal(t, 1, counts["pending"], "second job must remain queued")

	emb.release()
	require.Eventually(t, func() bool {
		return chunkJobStatusCounts(t, ctx, db)["completed"] == 2
	}, 2*time.Second, 20*time.Millisecond, "wakeCh must claim the second job without waiting for the tick")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, worker.Stop(stopCtx))

	counts = chunkJobStatusCounts(t, ctx, db)
	assert.Equal(t, 0, counts["processing"], "no job left processing")
	assert.Equal(t, 0, counts["failed"], "no job failed")
}

// TestGraphRelationshipEmbeddingWorker_WakeRefillClaimsBeforeNextTick mirrors the
// chunk wake-refill test for relationship jobs.
func TestGraphRelationshipEmbeddingWorker_WakeRefillClaimsBeforeNextTick(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	seedPendingRelJob(t, ctx, db, seedRelPair(t, ctx, db, projectID, "src-one", "dst-one"))
	seedPendingRelJob(t, ctx, db, seedRelPair(t, ctx, db, projectID, "src-two", "dst-two"))

	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "src", started: make(chan struct{})}
	cfg := newTestRelWorkerConfig()
	cfg.WorkerIntervalMs = 30000 // ticker never fires during the test
	cfg.WorkerConcurrency = 1    // second job only claimable after the first frees capacity
	jobs := NewGraphRelationshipEmbeddingJobsService(db, quietLogger(), cfg)
	worker := NewGraphRelationshipEmbeddingWorker(jobs, emb, db, cfg, nil, quietLogger(), nil, nil, false)

	startCtx := context.Background()
	require.NoError(t, worker.Start(startCtx))
	// let one-shot startup recovery finish
	time.Sleep(500 * time.Millisecond)

	require.NoError(t, worker.processBatch(startCtx))

	select {
	case <-emb.started:
	case <-time.After(3 * time.Second):
		t.Fatal("slow job never started")
	}

	counts := relJobStatusCounts(t, ctx, db)
	assert.Equal(t, 1, counts["processing"], "only one job in flight at concurrency 1")
	assert.Equal(t, 1, counts["pending"], "second job must remain queued")

	emb.release()
	require.Eventually(t, func() bool {
		return relJobStatusCounts(t, ctx, db)["completed"] == 2
	}, 2*time.Second, 20*time.Millisecond, "wakeCh must claim the second job without waiting for the tick")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, worker.Stop(stopCtx))

	counts = relJobStatusCounts(t, ctx, db)
	assert.Equal(t, 0, counts["processing"], "no job left processing")
	assert.Equal(t, 0, counts["failed"], "no job failed")
}
