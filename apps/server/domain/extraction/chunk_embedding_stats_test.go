package extraction

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/jobs"
)

// seedChunkEmbeddingJobStatus inserts a chunk embedding job with the given
// status and optional last_error, referencing chunkID.
func seedChunkEmbeddingJobStatus(t *testing.T, ctx context.Context, db bun.IDB, chunkID, status string, lastError *string) {
	t.Helper()
	_, err := db.NewRaw(`INSERT INTO kb.chunk_embedding_jobs
		(id, chunk_id, status, attempt_count, last_error, scheduled_at, created_at, updated_at)
		VALUES (gen_random_uuid(), ?, ?, 0, ?, now(), now(), now())`, chunkID, status, lastError).Exec(ctx)
	require.NoError(t, err)
}

// TestChunkEmbeddingJobsStatsByProject pins the project-scoped chunk-queue
// counts used by GET /api/projects/:id/embeddings/progress: the status groups
// are scoped through the chunks -> documents join and genuine failures are
// split from stale-sweep reaps. The queue has no dead_letter bucket. Foreign-
// project jobs must not leak into the scoped counts.
func TestChunkEmbeddingJobsStatsByProject(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	foreignProjectID := seedEmbeddingProject(t, ctx, db)

	stale := jobs.StaleJobMessage
	genuine := "real embedding failure"

	seed := func(project string, status string, lastError *string) {
		docID := seedChunkDocument(t, ctx, db, project)
		chunkID := seedChunk(t, ctx, db, docID, 0, uuid.NewString())
		seedChunkEmbeddingJobStatus(t, ctx, db, chunkID, status, lastError)
	}

	// 2 pending, 1 processing, 3 completed, 1 genuine failed, 2 stale failed.
	for range 2 {
		seed(projectID, "pending", nil)
	}
	seed(projectID, "processing", nil)
	for range 3 {
		seed(projectID, "completed", nil)
	}
	seed(projectID, "failed", &genuine)
	seed(projectID, "failed", &stale)
	seed(projectID, "failed", &stale)

	// Foreign-project jobs must not leak into the scoped counts.
	for range 4 {
		seed(foreignProjectID, "pending", nil)
	}

	svc := NewChunkEmbeddingJobsService(db, quietLogger(), nil)

	got, err := svc.StatsByProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, int64(2), got.Pending)
	require.Equal(t, int64(1), got.Processing)
	require.Equal(t, int64(3), got.Completed)
	require.Equal(t, int64(1), got.Failed)      // genuine only
	require.Equal(t, int64(2), got.StaleFailed) // stale reaps split out
}
