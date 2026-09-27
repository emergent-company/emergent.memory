package extraction

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/jobs"
)

// seedEmbeddingRelationship inserts a graph relationship owned by projectID and
// returns its ID.
func seedEmbeddingRelationship(t *testing.T, ctx context.Context, db bun.IDB, projectID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.graph_relationships
		(id, project_id, type, src_id, dst_id, canonical_id)
		VALUES (?, ?, 'RELATED_TO', gen_random_uuid(), gen_random_uuid(), ?)`,
		id, projectID, id).Exec(ctx)
	require.NoError(t, err)
	return id
}

// seedRelationshipEmbeddingJobStatus inserts a relationship embedding job with
// the given status and optional last_error, referencing relationshipID.
func seedRelationshipEmbeddingJobStatus(t *testing.T, ctx context.Context, db bun.IDB, relationshipID, status string, lastError *string) {
	t.Helper()
	_, err := db.NewRaw(`INSERT INTO kb.graph_relationship_embedding_jobs
		(id, relationship_id, status, attempt_count, last_error, scheduled_at, created_at, updated_at)
		VALUES (gen_random_uuid(), ?, ?, 0, ?, now(), now(), now())`, relationshipID, status, lastError).Exec(ctx)
	require.NoError(t, err)
}

// TestGraphRelationshipEmbeddingJobsStatsByProject pins the project-scoped
// relationship-queue counts used by GET /api/embeddings/progress: the
// status groups are scoped through the graph_relationships join and genuine
// failures are split from stale-sweep reaps. The queue's status CHECK does not
// admit dead_letter, so that bucket stays zero here.
func TestGraphRelationshipEmbeddingJobsStatsByProject(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	foreignProjectID := seedEmbeddingProject(t, ctx, db)

	stale := jobs.StaleJobMessage
	genuine := "real embedding failure"

	seed := func(project string, status string, lastError *string) {
		relID := seedEmbeddingRelationship(t, ctx, db, project)
		seedRelationshipEmbeddingJobStatus(t, ctx, db, relID, status, lastError)
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

	svc := NewGraphRelationshipEmbeddingJobsService(db, quietLogger(), nil)

	got, err := svc.StatsByProject(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, int64(2), got.Pending)
	require.Equal(t, int64(1), got.Processing)
	require.Equal(t, int64(3), got.Completed)
	require.Equal(t, int64(1), got.Failed)      // genuine only
	require.Equal(t, int64(2), got.StaleFailed) // stale reaps split out
	require.Equal(t, int64(0), got.DeadLetter)
}
