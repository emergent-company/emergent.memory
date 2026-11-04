package extraction

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/jobs"
)

// seedEmbeddingJobStatus inserts a graph embedding job with the given status and
// optional last_error, referencing objectID.
func seedEmbeddingJobStatus(t *testing.T, ctx context.Context, db bun.IDB, objectID, status string, lastError *string) {
	t.Helper()
	_, err := db.NewRaw(`INSERT INTO kb.graph_embedding_jobs
		(id, object_id, status, attempt_count, last_error, scheduled_at, created_at, updated_at)
		VALUES (gen_random_uuid(), ?, ?, 0, ?, now(), now(), now())`, objectID, status, lastError).Exec(ctx)
	require.NoError(t, err)
}

// TestGraphEmbeddingJobsStatsShape pins the index-friendly Stats rewrite
// (issue #1098): the queue counts are computed from a single `GROUP BY status`
// aggregate (satisfied by the status b-tree index) rather than six
// `COUNT(*) FILTER (WHERE status = ...)` full scans, and genuine failures are
// correctly split from stale-sweep reaps.
func TestGraphEmbeddingJobsStatsShape(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	stale := jobs.StaleJobMessage
	genuine := "real embedding failure"

	seed := func(status string, lastError *string) {
		objectID := seedEmbeddingObject(t, ctx, db, projectID, "obj")
		seedEmbeddingJobStatus(t, ctx, db, objectID, status, lastError)
	}

	// 2 pending, 1 processing, 3 completed, 1 genuine failed, 2 stale failed, 1 dead_letter.
	for range 2 {
		seed("pending", nil)
	}
	seed("processing", nil)
	for range 3 {
		seed("completed", nil)
	}
	seed("failed", &genuine)
	seed("failed", &stale)
	seed("failed", &stale)
	seed("dead_letter", nil)

	svc := NewGraphEmbeddingJobsService(db, quietLogger(), nil)

	t.Run("global Stats", func(t *testing.T) {
		got, err := svc.Stats(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(2), got.Pending)
		require.Equal(t, int64(1), got.Processing)
		require.Equal(t, int64(3), got.Completed)
		require.Equal(t, int64(1), got.Failed)      // genuine only
		require.Equal(t, int64(2), got.StaleFailed) // stale reaps split out
		require.Equal(t, int64(1), got.DeadLetter)
	})

	t.Run("project Stats", func(t *testing.T) {
		got, err := svc.StatsByProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, int64(2), got.Pending)
		require.Equal(t, int64(1), got.Processing)
		require.Equal(t, int64(3), got.Completed)
		require.Equal(t, int64(1), got.Failed)
		require.Equal(t, int64(2), got.StaleFailed)
		require.Equal(t, int64(1), got.DeadLetter)
	})
}
