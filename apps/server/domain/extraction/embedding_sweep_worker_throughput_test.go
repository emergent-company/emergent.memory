package extraction

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
)

// These tests pin the #796 sweep ceilings fix: object admission is keyed off
// available queue depth (not a constant per-sweep batch), and relationships are
// embedded in multi-object requests grouped per project instead of one request
// per relationship. They run against a throwaway database created by
// internal/testdb (TEST_DATABASE_URL + REQUIRE_DB); they skip cleanly when no
// database is available.

// seedSweepModelConfig inserts a project embedding-model config so the sweep's
// model gate admits objects/relationships for the project.
func seedSweepModelConfig(t *testing.T, ctx context.Context, db bun.IDB, projectID string) {
	t.Helper()
	_, err := db.NewRaw(`INSERT INTO kb.project_model_config (project_id, embedding_model)
		VALUES (?, 'text-embedding-004')`, projectID).Exec(ctx)
	require.NoError(t, err)
}

// seedSweepRelationshipObject inserts a graph object (with canonical_id) and
// returns its id.
func seedSweepRelationshipObject(t *testing.T, ctx context.Context, db bun.IDB, projectID, name string) string {
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

// seedSweepRelationship inserts a graph relationship with a NULL embedding
// between srcID and dstID and returns its id.
func seedSweepRelationship(t *testing.T, ctx context.Context, db bun.IDB, projectID, srcID, dstID, relType string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.graph_relationships
		(id, project_id, type, src_id, dst_id, properties, canonical_id, version, created_at)
		VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, 1, now())`,
		id, projectID, relType, srcID, dstID, id).Exec(ctx)
	require.NoError(t, err)
	return id
}

// newSweepWorker builds an EmbeddingSweepWorker wired to the given embedding
// service for direct sweepObjects/sweepRelationships calls.
func newSweepWorker(db bun.IDB, emb EmbeddingService, cfg *EmbeddingSweepConfig) *EmbeddingSweepWorker {
	jobs := NewGraphEmbeddingJobsService(db, quietLogger(), DefaultGraphEmbeddingConfig())
	return NewEmbeddingSweepWorker(jobs, emb, db, cfg, quietLogger(), nil, nil, false)
}

// TestEmbeddingSweepWorker_ObjectAdmissionKeyedToQueueDepth proves admission is
// no longer capped by the constant per-sweep BatchSize: it admits up to the
// target queue depth, then stops once the queue is full.
func TestEmbeddingSweepWorker_ObjectAdmissionKeyedToQueueDepth(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	seedSweepModelConfig(t, ctx, db, projectID)

	for i := 0; i < 600; i++ {
		seedEmbeddingObject(t, ctx, db, projectID, fmt.Sprintf("obj-%d", i))
	}

	cfg := DefaultEmbeddingSweepConfig()
	cfg.ObjectQueueTargetDepth = 500
	cfg.BatchSize = 200 // deliberately the old cap — must be ignored for admission

	worker := newSweepWorker(db, &batchRecordingEmbedder{}, cfg)

	enqueued := worker.sweepObjects(ctx)
	assert.Equal(t, 500, enqueued, "admission must key off queue depth, not the constant 200 cap")

	// Queue is now at target depth; a second sweep must admit nothing.
	assert.Equal(t, 0, worker.sweepObjects(ctx), "depth-keyed admission must not flood")
}

// TestEmbeddingSweepWorker_ObjectAdmissionLeavesRoomForActiveJobs proves
// admission is target-active: existing pending jobs reduce the admission.
func TestEmbeddingSweepWorker_ObjectAdmissionLeavesRoomForActiveJobs(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	seedSweepModelConfig(t, ctx, db, projectID)

	for i := 0; i < 6; i++ {
		objID := seedEmbeddingObject(t, ctx, db, projectID, fmt.Sprintf("job-obj-%d", i))
		seedPendingEmbeddingJob(t, ctx, db, objID)
	}
	for i := 0; i < 100; i++ {
		seedEmbeddingObject(t, ctx, db, projectID, fmt.Sprintf("obj-%d", i))
	}

	cfg := DefaultEmbeddingSweepConfig()
	cfg.ObjectQueueTargetDepth = 10

	worker := newSweepWorker(db, &batchRecordingEmbedder{}, cfg)
	assert.Equal(t, 4, worker.sweepObjects(ctx), "admission must leave room for the 6 active jobs")
}

// TestEmbeddingSweepWorker_RelationshipBatchingPerProject proves relationships
// are embedded in multi-object requests grouped per project.
func TestEmbeddingSweepWorker_RelationshipBatchingPerProject(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)

	seedProject := func() string {
		p := seedEmbeddingProject(t, ctx, db)
		seedSweepModelConfig(t, ctx, db, p)
		return p
	}
	seedRelationship := func(projectID string) {
		src := seedSweepRelationshipObject(t, ctx, db, projectID, "src")
		dst := seedSweepRelationshipObject(t, ctx, db, projectID, "dst")
		seedSweepRelationship(t, ctx, db, projectID, src, dst, "KNOWS")
	}

	projectA := seedProject()
	seedRelationship(projectA)
	seedRelationship(projectA)

	projectB := seedProject()
	seedRelationship(projectB)
	seedRelationship(projectB)

	emb := &batchRecordingEmbedder{}
	cfg := DefaultEmbeddingSweepConfig()
	cfg.RelationshipRequestBatchSize = 100

	worker := newSweepWorker(db, emb, cfg)
	embedded, errs := worker.sweepRelationships(ctx)
	assert.Equal(t, 4, embedded)
	assert.Equal(t, 0, errs)

	batches := emb.batchSizes()
	require.Len(t, batches, 2, "one multi-object request per project")
	for _, batch := range batches {
		assert.Len(t, batch, 2, "each project's two relationships must share one request")
	}
	assert.Empty(t, emb.singles, "no single-object request should be issued")

	var nullCount int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_relationships
		WHERE project_id IN (?, ?) AND embedding IS NULL`, projectA, projectB).Scan(ctx, &nullCount))
	assert.Zero(t, nullCount, "every relationship must have an embedding")
}

// TestEmbeddingSweepWorker_RelationshipSerialFallback proves the serial
// per-row path still works when the embedding service has no multi-object
// method (single-object-only embedder → batchSizes stays empty by construction).
func TestEmbeddingSweepWorker_RelationshipSerialFallback(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	seedSweepModelConfig(t, ctx, db, projectID)

	for i := 0; i < 2; i++ {
		src := seedSweepRelationshipObject(t, ctx, db, projectID, fmt.Sprintf("src-%d", i))
		dst := seedSweepRelationshipObject(t, ctx, db, projectID, fmt.Sprintf("dst-%d", i))
		seedSweepRelationship(t, ctx, db, projectID, src, dst, "KNOWS")
	}

	// slowMarker never matches a triplet, so no request blocks.
	emb := &gatedEmbedder{gate: make(chan struct{}), slowMarker: "zzz-never-match", started: make(chan struct{})}
	cfg := DefaultEmbeddingSweepConfig()

	worker := newSweepWorker(db, emb, cfg)
	embedded, errs := worker.sweepRelationships(ctx)
	assert.Equal(t, 2, embedded)
	assert.Equal(t, 0, errs)

	// Single-object-only embedder: no multi-object request is ever issued.
	if _, ok := any(emb).(batchEmbeddingService); ok {
		t.Fatal("gatedEmbedder must not implement batchEmbeddingService")
	}

	var nullCount int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_relationships
		WHERE project_id = ? AND embedding IS NULL`, projectID).Scan(ctx, &nullCount))
	assert.Zero(t, nullCount, "every relationship must have an embedding")
}

// sweepFailingBatchEmbedder implements both EmbeddingService and
// batchEmbeddingService but every call fails, so the sweep must count every row
// as an error and leave embeddings NULL.
type sweepFailingBatchEmbedder struct{}

func (e *sweepFailingBatchEmbedder) IsEnabled() bool { return true }

func (e *sweepFailingBatchEmbedder) EmbedQuery(context.Context, string) ([]float32, error) {
	return nil, errors.New("sweep failing embedder")
}

func (e *sweepFailingBatchEmbedder) EmbedQueryWithUsage(context.Context, string) (*vertex.EmbedResult, error) {
	return nil, errors.New("sweep failing embedder")
}

func (e *sweepFailingBatchEmbedder) EmbedDocumentsWithUsage(context.Context, []string) (*vertex.BatchEmbedResult, error) {
	return nil, errors.New("sweep failing embedder")
}

// TestEmbeddingSweepWorker_RelationshipBatchErrorCountsEveryRow proves a failed
// batch request counts every relationship in the batch as an error and never
// writes a vector.
func TestEmbeddingSweepWorker_RelationshipBatchErrorCountsEveryRow(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	seedSweepModelConfig(t, ctx, db, projectID)

	for i := 0; i < 3; i++ {
		src := seedSweepRelationshipObject(t, ctx, db, projectID, fmt.Sprintf("src-%d", i))
		dst := seedSweepRelationshipObject(t, ctx, db, projectID, fmt.Sprintf("dst-%d", i))
		seedSweepRelationship(t, ctx, db, projectID, src, dst, "KNOWS")
	}

	cfg := DefaultEmbeddingSweepConfig()
	worker := newSweepWorker(db, &sweepFailingBatchEmbedder{}, cfg)

	embedded, errs := worker.sweepRelationships(ctx)
	assert.Equal(t, 0, embedded)
	assert.Equal(t, 3, errs)

	var nullCount int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_relationships
		WHERE project_id = ? AND embedding IS NULL`, projectID).Scan(ctx, &nullCount))
	assert.Equal(t, 3, nullCount, "all embeddings must remain NULL for a later retry")
}
