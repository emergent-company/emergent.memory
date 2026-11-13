package graph_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestReindexEmbeddingsChunks covers the chunk arm of ReindexEmbeddings
// (issue #624): chunks had no bulk re-embed path at all, so a project that
// changed embedding model could not move its chunk corpus.
//
// It pins the three properties the SQL has to get right: parent chunks are
// never enqueued, a non-force pass only targets chunks with a NULL embedding,
// and a second pass does not stack jobs on chunks that already have an active
// one (the partial unique index is what makes that airtight).
func TestReindexEmbeddingsChunks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode (#624)")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "reindexchunks")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Reindex Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Reindex Project",
	}, testutil.AdminUser.ID))

	docID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.documents
		   (id, project_id, filename, mime_type, content, source_type, conversion_status, source_status, file_hash, created_at)
		 VALUES (?, ?, 'doc.md', 'text/markdown', 'body', 'upload', 'completed', 'active', ?, now())`,
		docID, projectID, uuid.NewString()).Exec(ctx)
	require.NoError(t, err)

	// kb.chunks is unique on (document_id, chunk_index), so each fixture chunk
	// needs its own index.
	nextIndex := 0
	insertChunk := func(metadata string, embedded bool) string {
		t.Helper()
		id := uuid.NewString()
		vec := "NULL"
		if embedded {
			vec = "array_fill(0.01::real, ARRAY[768])::vector"
		}
		_, err := db.NewRaw(`INSERT INTO kb.chunks (id, document_id, chunk_index, text, embedding, metadata, created_at, updated_at)
			 VALUES (?, ?, ?, 'text', `+vec+`, ?::jsonb, now(), now())`,
			id, docID, nextIndex, metadata).Exec(ctx)
		require.NoError(t, err)
		nextIndex++
		return id
	}

	parentID := insertChunk(`{"isParent":true}`, false)
	nullEmbeddingID := insertChunk(`{}`, false)
	embeddedID := insertChunk(`{}`, true)

	repo := graph.NewRepository(db, slog.Default(), testDB.Config)
	svc := graph.NewService(repo, slog.Default(), nil, nil, nil, nil, nil, nil, nil, nil)

	jobCount := func(chunkID string) int {
		t.Helper()
		var n int
		require.NoError(t, db.NewRaw(
			`SELECT count(*) FROM kb.chunk_embedding_jobs WHERE chunk_id = ?`, chunkID).Scan(ctx, &n))
		return n
	}

	// First pass: only the chunk with a NULL embedding is queued.
	resp, err := svc.ReindexEmbeddings(ctx, uuid.MustParse(projectID), graph.ReindexEmbeddingsRequest{Chunks: true})
	require.NoError(t, err)
	require.Equal(t, 1, resp.ChunksEnqueued)
	require.Equal(t, 0, jobCount(parentID), "parent chunks are structural context and must never be enqueued")
	require.Equal(t, 1, jobCount(nullEmbeddingID))
	require.Equal(t, 0, jobCount(embeddedID), "a non-force pass must not re-embed an existing vector")

	// Second pass is a no-op: the first pass already left an active job, and
	// stacking a second one would double the work when the worker drains.
	resp, err = svc.ReindexEmbeddings(ctx, uuid.MustParse(projectID), graph.ReindexEmbeddingsRequest{Chunks: true})
	require.NoError(t, err)
	require.Equal(t, 0, resp.ChunksEnqueued)
	require.Equal(t, 1, jobCount(nullEmbeddingID), "an active job must not be duplicated")

	// Force queues the already-embedded chunk, but still skips the chunk whose
	// active job has not been drained yet.
	resp, err = svc.ReindexEmbeddings(ctx, uuid.MustParse(projectID), graph.ReindexEmbeddingsRequest{Chunks: true, Force: true})
	require.NoError(t, err)
	require.Equal(t, 1, resp.ChunksEnqueued)
	require.Equal(t, 1, jobCount(embeddedID))
	require.Equal(t, 1, jobCount(nullEmbeddingID))
	require.Equal(t, 0, jobCount(parentID))

	// Without Chunks the chunk corpus is untouched.
	resp, err = svc.ReindexEmbeddings(ctx, uuid.MustParse(projectID), graph.ReindexEmbeddingsRequest{})
	require.NoError(t, err)
	require.Zero(t, resp.ChunksEnqueued)
}
