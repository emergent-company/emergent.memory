package extraction

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// vec768 returns a 768-dimension pgvector literal.
func vec768() string {
	parts := make([]string, 768)
	for i := range parts {
		parts[i] = "0.0"
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// seedEmbeddedObject seeds a live graph object and sets its embedding_v2 vector,
// returning the object ID.
func seedEmbeddedObject(t *testing.T, ctx context.Context, db bun.IDB, projectID, name string) string {
	t.Helper()
	id := seedEmbeddingObject(t, ctx, db, projectID, name)
	_, err := db.NewRaw(`UPDATE kb.graph_objects SET embedding_v2 = ?::vector WHERE id = ?`, vec768(), id).Exec(ctx)
	require.NoError(t, err)
	return id
}

// TestGraphEmbeddingJobsCoverage pins the object coverage counts used by
// GET /api/embeddings/coverage: embedded (vector present), awaiting (vector
// missing), total (live rows = embedded+awaiting), scoped to a project and
// excluding foreign-project and deleted rows.
func TestGraphEmbeddingJobsCoverage(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	foreignProjectID := seedEmbeddingProject(t, ctx, db)

	for i := range 3 {
		seedEmbeddedObject(t, ctx, db, projectID, fmt.Sprintf("obj-%d", i))
	}
	for i := range 2 {
		seedEmbeddingObject(t, ctx, db, projectID, fmt.Sprintf("await-%d", i))
	}
	for range 4 {
		seedEmbeddedObject(t, ctx, db, foreignProjectID, "foreign")
	}

	svc := NewGraphEmbeddingJobsService(db, quietLogger(), nil)

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
