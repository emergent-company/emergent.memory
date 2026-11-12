package graph_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// recordingEmbeddingEnqueuer records each batch an arm hands to the enqueuer,
// so a test can prove pagination actually happened and count duplicates.
type recordingEmbeddingEnqueuer struct {
	batches [][]string
}

func (r *recordingEmbeddingEnqueuer) EnqueueEmbedding(_ context.Context, objectID string) error {
	r.batches = append(r.batches, []string{objectID})
	return nil
}

func (r *recordingEmbeddingEnqueuer) EnqueueBatchEmbeddings(_ context.Context, objectIDs []string) (int, error) {
	r.batches = append(r.batches, append([]string(nil), objectIDs...))
	return len(objectIDs), nil
}

func (r *recordingEmbeddingEnqueuer) reset() { r.batches = nil }

func (r *recordingEmbeddingEnqueuer) ids() []string {
	var all []string
	for _, batch := range r.batches {
		all = append(all, batch...)
	}
	return all
}

// insertReindexTestObject inserts a graph object eligible for the non-force
// reindex arm unless embedded/deleted, at an explicit created_at so the keyset
// order is deterministic.
func insertReindexTestObject(t *testing.T, ctx context.Context, db bun.IDB, projectID uuid.UUID, key string, createdAt time.Time, embedded, deleted bool) uuid.UUID {
	t.Helper()
	vec := "NULL"
	if embedded {
		vec = "array_fill(0.01::real, ARRAY[768])::vector"
	}
	deletedAt := "NULL"
	if deleted {
		deletedAt = "now()"
	}
	id := uuid.New()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at, embedding_v2, deleted_at)
		VALUES (?, ?, NULL, ?, NULL, 1, 'Note', ?, 'active',
			'{}'::jsonb, '{}'::text[], ?, ?, `+vec+`, `+deletedAt+`)`,
		id, projectID, id, key, createdAt, createdAt).Exec(ctx)
	require.NoError(t, err)
	return id
}

// TestReindexEmbeddingsPagesObjectsAcrossPageBoundary covers the object arm of
// issue #677: a corpus larger than the page size must take several enqueue
// calls, enqueue every eligible id exactly once, and still honour the
// namespace, force, soft-delete and active-job predicates on every page.
func TestReindexEmbeddingsPagesObjectsAcrossPageBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "reindexpages")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Reindex Paging Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Reindex Paging Project",
	}, testutil.AdminUser.ID))
	pid := uuid.MustParse(projectID)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eligible := make([]uuid.UUID, 5)
	for i := range eligible {
		eligible[i] = insertReindexTestObject(t, ctx, db, pid,
			"ns:item-"+string(rune('a'+i)), base.Add(time.Duration(i)*time.Second), false, false)
	}
	otherNamespace := insertReindexTestObject(t, ctx, db, pid, "other:item", base.Add(time.Minute), false, false)
	embedded := insertReindexTestObject(t, ctx, db, pid, "ns:embedded", base.Add(2*time.Minute), true, false)
	deleted := insertReindexTestObject(t, ctx, db, pid, "ns:deleted", base.Add(3*time.Minute), false, true)
	pending := insertReindexTestObject(t, ctx, db, pid, "ns:pending", base.Add(4*time.Minute), false, false)

	_, err := db.NewRaw(`INSERT INTO kb.graph_embedding_jobs (object_id, status) VALUES (?, 'pending')`, pending).Exec(ctx)
	require.NoError(t, err)

	repo := graph.NewRepository(db, slog.Default(), testDB.Config)
	enqueuer := &recordingEmbeddingEnqueuer{}
	svc := graph.NewService(repo, slog.Default(), nil, nil, nil, enqueuer, nil, nil, nil, nil).
		WithReindexPageSize(2) // 5 eligible rows => pages of 2, 2, 1.

	resp, err := svc.ReindexEmbeddings(ctx, pid, graph.ReindexEmbeddingsRequest{Namespace: "ns:"})
	require.NoError(t, err)
	require.Equal(t, 5, resp.ObjectsEnqueued)
	require.Len(t, enqueuer.batches, 3, "page size 2 over 5 eligible rows must take three enqueue calls")

	seen := map[string]int{}
	for _, id := range enqueuer.ids() {
		seen[id]++
	}
	require.Len(t, seen, 5)
	for _, id := range eligible {
		require.Equal(t, 1, seen[id.String()], "eligible object %s must be enqueued exactly once", id)
	}
	for name, id := range map[string]uuid.UUID{
		"namespace mismatch": otherNamespace,
		"already embedded":   embedded,
		"soft-deleted":       deleted,
		"active job":         pending,
	} {
		require.NotContains(t, seen, id.String(), "%s must not be enqueued", name)
	}
	require.Equal(t, 5, len(enqueuer.ids()), "no id may be enqueued twice")

	// Force still honours the soft-delete and active-job predicates, and now
	// includes the embedded object — proving the force predicate survives
	// pagination.
	enqueuer.reset()
	resp, err = svc.ReindexEmbeddings(ctx, pid, graph.ReindexEmbeddingsRequest{Namespace: "ns:", Force: true})
	require.NoError(t, err)
	require.Equal(t, 6, resp.ObjectsEnqueued)
	require.Len(t, enqueuer.batches, 3, "6 eligible rows at page size 2 must take three pages")

	forced := map[string]int{}
	for _, id := range enqueuer.ids() {
		forced[id]++
	}
	require.Equal(t, 1, forced[embedded.String()], "force must include an already-embedded object")
	require.NotContains(t, forced, deleted.String())
	require.NotContains(t, forced, pending.String())
	require.NotContains(t, forced, otherNamespace.String())
}

// TestReindexEmbeddingsChunksAcrossPageBoundary exercises the chunk arm's
// keyset CTE: with a page size smaller than the corpus every chunk must get
// exactly one job and the summed count must cover all pages.
func TestReindexEmbeddingsChunksAcrossPageBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "reindexchunkpages")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Reindex Chunk Paging Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Reindex Chunk Paging Project",
	}, testutil.AdminUser.ID))
	pid := uuid.MustParse(projectID)

	docID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.documents
		   (id, project_id, filename, mime_type, content, source_type, conversion_status, source_status, file_hash, created_at)
		 VALUES (?, ?, 'doc.md', 'text/markdown', 'body', 'upload', 'completed', 'active', ?, now())`,
		docID, projectID, uuid.NewString()).Exec(ctx)
	require.NoError(t, err)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	chunkIDs := make([]uuid.UUID, 5)
	for i := range chunkIDs {
		chunkIDs[i] = uuid.New()
		metadata := `{}`
		if i == len(chunkIDs)-1 {
			metadata = `{"isParent":true}` // parent chunks are never enqueued
		}
		_, err := db.NewRaw(`INSERT INTO kb.chunks (id, document_id, chunk_index, text, metadata, created_at, updated_at)
			 VALUES (?, ?, ?, 'text', ?::jsonb, ?, ?)`,
			chunkIDs[i], docID, i, metadata, base.Add(time.Duration(i)*time.Second), base.Add(time.Duration(i)*time.Second)).Exec(ctx)
		require.NoError(t, err)
	}

	repo := graph.NewRepository(db, slog.Default(), testDB.Config)
	svc := graph.NewService(repo, slog.Default(), nil, nil, nil, nil, nil, nil, nil, nil).
		WithReindexPageSize(2) // 4 eligible chunks => pages of 2, 2.

	resp, err := svc.ReindexEmbeddings(ctx, pid, graph.ReindexEmbeddingsRequest{Chunks: true})
	require.NoError(t, err)
	require.Equal(t, 4, resp.ChunksEnqueued)

	for i, chunkID := range chunkIDs {
		var jobs int
		require.NoError(t, db.NewRaw(
			`SELECT count(*) FROM kb.chunk_embedding_jobs WHERE chunk_id = ?`, chunkID).Scan(ctx, &jobs))
		if i == len(chunkIDs)-1 {
			require.Zero(t, jobs, "parent chunks must never be enqueued")
		} else {
			require.Equal(t, 1, jobs, "chunk %s must get exactly one job", chunkID)
		}
	}

	// A second pass must add nothing: every eligible chunk has an active job.
	resp, err = svc.ReindexEmbeddings(ctx, pid, graph.ReindexEmbeddingsRequest{Chunks: true})
	require.NoError(t, err)
	require.Zero(t, resp.ChunksEnqueued)
}
