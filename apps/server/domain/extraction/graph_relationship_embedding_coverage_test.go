package extraction

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// seedEmbeddedRelationship seeds a live graph relationship and sets its embedding
// vector, returning the relationship ID.
func seedEmbeddedRelationship(t *testing.T, ctx context.Context, db bun.IDB, projectID string) string {
	t.Helper()
	id := seedEmbeddingRelationship(t, ctx, db, projectID)
	_, err := db.NewRaw(`UPDATE kb.graph_relationships SET embedding = ?::vector WHERE id = ?`, vec768(), id).Exec(ctx)
	require.NoError(t, err)
	return id
}

// TestGraphRelationshipEmbeddingJobsCoverage pins the relationship coverage
// counts used by GET /api/embeddings/coverage (mirror of
// TestGraphEmbeddingJobsCoverage, on the embedding column).
func TestGraphRelationshipEmbeddingJobsCoverage(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	foreignProjectID := seedEmbeddingProject(t, ctx, db)

	for range 3 {
		seedEmbeddedRelationship(t, ctx, db, projectID)
	}
	for range 2 {
		seedEmbeddingRelationship(t, ctx, db, projectID)
	}
	for range 4 {
		seedEmbeddedRelationship(t, ctx, db, foreignProjectID)
	}

	svc := NewGraphRelationshipEmbeddingJobsService(db, quietLogger(), nil)

	t.Run("project CoverageByProject", func(t *testing.T) {
		got, err := svc.CoverageByProject(ctx, projectID)
		require.NoError(t, err)
		require.Equal(t, int64(3), got.Embedded)
		require.Equal(t, int64(2), got.Awaiting)
		require.Equal(t, int64(5), got.Total)
	})

	t.Run("project scoping excludes foreign rows", func(t *testing.T) {
		got, err := svc.CoverageByProject(ctx, foreignProjectID)
		require.NoError(t, err)
		require.Equal(t, int64(4), got.Embedded)
		require.Equal(t, int64(0), got.Awaiting)
		require.Equal(t, int64(4), got.Total)
	})

	t.Run("global Coverage", func(t *testing.T) {
		got, err := svc.Coverage(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(7), got.Embedded)
		require.Equal(t, int64(2), got.Awaiting)
		require.Equal(t, int64(9), got.Total)
	})
}
