package graph_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// TestBranchScopeObjectReads is the #308 regression: the branch scope that
// search already applies must now also bound object detail (GetByID), list/count
// (List/Count) and traversal (ExpandGraph). A main-only run reads main only; a
// scoped run reads main plus its in-scope unmerged branches, deduplicated
// main-first; a caller with no boundary keeps the pre-existing main-only default
// for list/count and the pre-existing main-preferred resolution for detail.
func TestBranchScopeObjectReads(t *testing.T) {
	ctx, db, repo, _, projectID := setupProvenanceTest(t)

	branchID := uuid.New()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.branches (id, project_id, name, origin) VALUES (?, ?, ?, 'extraction')`,
		branchID, projectID, "extraction/Invoice.pdf/1")
	require.NoError(t, err)

	insertObject := func(id, canonical uuid.UUID, branch *uuid.UUID, typ, key string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO kb.graph_objects
				(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
				 properties, labels, content_hash, created_at, updated_at)
			VALUES (?, ?, ?, ?, NULL, 1, ?, ?, 'active', '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())`,
			id, projectID, branch, canonical, typ, key, []byte("scope-ext-hash"))
		require.NoError(t, err)
	}

	// A fork: canonical C on main AND on the branch.
	mainCanonical := uuid.New()
	forkBranchID := branchID
	insertObject(uuid.New(), mainCanonical, nil, "Thing", "forked")
	insertObject(uuid.New(), mainCanonical, &forkBranchID, "Thing", "forked")
	// A branch-only object: canonical D only on the branch.
	branchOnlyCanonical := uuid.New()
	insertObject(uuid.New(), branchOnlyCanonical, &branchID, "Thing", "branch-only")

	scoped, err := branchscope.New(branchscope.Config{Unmerged: true, Origins: []string{"extraction"}})
	require.NoError(t, err)
	scopedCtx := branchscope.ContextWith(ctx, scoped)
	mainOnlyCtx := branchscope.ContextWith(ctx, nil)

	// ── Object detail (GetByID) ──────────────────────────────────────────────
	// A scoped run can read a branch-only object by canonical id...
	got, err := repo.GetByID(scopedCtx, projectID, branchOnlyCanonical)
	require.NoError(t, err)
	assert.Equal(t, branchOnlyCanonical, got.CanonicalID, "scoped detail must read a branch-only object")
	assert.Equal(t, &branchID, got.BranchID)

	// ...while a main-only run cannot.
	_, err = repo.GetByID(mainOnlyCtx, projectID, branchOnlyCanonical)
	assert.Error(t, err, "a main-only run must not read a branch-only object")

	// A fork resolves to main for both, since main is preferred.
	got, err = repo.GetByID(scopedCtx, projectID, mainCanonical)
	require.NoError(t, err)
	assert.Nil(t, got.BranchID, "a forked canonical id resolves to main")

	// A caller with no boundary keeps the pre-existing main-preferred detail read.
	got, err = repo.GetByID(ctx, projectID, branchOnlyCanonical)
	require.NoError(t, err)
	assert.Equal(t, branchOnlyCanonical, got.CanonicalID)

	// ── List / Count ─────────────────────────────────────────────────────────
	canonicalIDs := func(objects []*graph.GraphObject) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(objects))
		for _, o := range objects {
			out = append(out, o.CanonicalID)
		}
		return out
	}

	// Main-only lists main only (the fork's main head).
	mainOnlyList, err := repo.List(mainOnlyCtx, graph.ListParams{ProjectID: projectID})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{mainCanonical}, canonicalIDs(mainOnlyList),
		"a main-only list reads main only")

	// A scoped run reads main plus the branch-only object, deduplicated: the fork
	// appears once (its main head), and the branch-only object is included.
	scopedList, err := repo.List(scopedCtx, graph.ListParams{ProjectID: projectID})
	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{mainCanonical, branchOnlyCanonical}, canonicalIDs(scopedList),
		"a scoped list reads main + in-scope branches, deduplicated main-first")

	scopedCount, err := repo.Count(scopedCtx, graph.ListParams{ProjectID: projectID})
	require.NoError(t, err)
	assert.Equal(t, 2, scopedCount, "a scoped count dedups the fork and includes the branch-only object")

	// ── Traversal (ExpandGraph) ──────────────────────────────────────────────
	// Link the branch-only object to a main neighbour via a branch relationship.
	mainNeighbour := uuid.New()
	insertObject(uuid.New(), mainNeighbour, nil, "Thing", "main-neighbour")
	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.graph_relationships
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, src_id, dst_id, properties, content_hash, created_at)
		VALUES (?, ?, ?, ?, NULL, 1, 'relates', ?, ?, '{}'::jsonb, ?, NOW())`,
		uuid.New(), projectID, branchID, uuid.New(), branchOnlyCanonical, mainNeighbour, []byte("rel-hash"))
	require.NoError(t, err)

	scopedExpand, err := repo.ExpandGraph(scopedCtx, graph.ExpandParams{
		ProjectID: projectID,
		RootIDs:   []uuid.UUID{branchOnlyCanonical},
		MaxDepth:  1,
		MaxNodes:  20,
		MaxEdges:  20,
	})
	require.NoError(t, err)
	_, reached := scopedExpand.Nodes[mainNeighbour]
	assert.True(t, reached, "a scoped traversal must cross an in-scope branch relationship")

	mainOnlyExpand, err := repo.ExpandGraph(mainOnlyCtx, graph.ExpandParams{
		ProjectID: projectID,
		RootIDs:   []uuid.UUID{branchOnlyCanonical},
		MaxDepth:  1,
		MaxNodes:  20,
		MaxEdges:  20,
	})
	require.NoError(t, err)
	_, reached = mainOnlyExpand.Nodes[mainNeighbour]
	assert.False(t, reached, "a main-only traversal must not cross a branch relationship")
}
