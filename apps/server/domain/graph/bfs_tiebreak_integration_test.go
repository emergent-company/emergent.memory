package graph

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpandGraph_BFSTieBreakDeterminism verifies that when many edges share the
// same similarity score (here: all relationships lack embeddings, so every edge
// collapses to 0.0 / no similarity row), MaxEdges truncation selects a stable,
// id-ascending subset on every run.
func TestExpandGraph_BFSTieBreakDeterminism(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)

	// Root object — its canonical_id is the BFS root.
	rootCanonicalID := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, content_hash, created_at, updated_at)
		VALUES
			(?, ?, NULL, ?, NULL, 1, ?, ?,
			 '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())
	`, rootCanonicalID, projectID, rootCanonicalID, "TieBreakRoot", "active", "root-content-hash")
	require.NoError(t, err)

	// >=6 outgoing relationships, all lacking embeddings (embedding column left NULL).
	const numRels = 6
	for i := 0; i < numRels; i++ {
		relID := uuid.New()
		dstID := uuid.New()
		_, err := db.ExecContext(ctx, `
			INSERT INTO kb.graph_relationships
				(id, project_id, branch_id, canonical_id, supersedes_id, version, type, src_id, dst_id,
				 properties, content_hash, created_at)
			VALUES
				(?, ?, NULL, ?, NULL, 1, ?, ?, ?,
				 '{}'::jsonb, ?, NOW())
		`, relID, projectID, relID, "links", rootCanonicalID, dstID, "rel-content-hash")
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_relationships WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	params := ExpandParams{
		ProjectID:   projectID,
		RootIDs:     []uuid.UUID{rootCanonicalID},
		Direction:   "out",
		MaxDepth:    1,
		MaxNodes:    1000,
		MaxEdges:    3,
		QueryVector: []float32{0.1, 0.2, 0.3}, // all-missing-embedding → maximal ties
	}

	var reference []uuid.UUID
	for run := 0; run < 20; run++ {
		res, err := repo.ExpandGraph(ctx, params)
		require.NoError(t, err)
		require.Len(t, res.Edges, 3, "MaxEdges=3 should truncate to exactly 3 edges")

		ids := make([]uuid.UUID, len(res.Edges))
		for i, e := range res.Edges {
			ids[i] = e.ID
		}

		if run == 0 {
			reference = append([]uuid.UUID(nil), ids...)
		}

		assert.Equal(t, reference, ids, "run %d: edge id sequence must be identical every run", run)
		assert.True(t, isUUIDsSorted(ids), "run %d: edges must be ascending by id", run)
	}
}
