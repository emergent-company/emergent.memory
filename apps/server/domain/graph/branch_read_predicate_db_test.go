package graph

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// TestBranchReadPredicate covers the relationship-read boundary: a caller with no
// branch boundary keeps the previous unbounded query, an agent run reads the main
// graph only, and a scoped run reads main plus exactly the branches its scope
// admits — including not reading an extraction branch whose source was deleted
// (#265).
func TestBranchReadPredicate(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "graphbranchpred")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	repo := &Repository{db: tdb.DB}

	orgID := uuid.New()
	_, err := tdb.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "pred-org")
	require.NoError(t, err)
	projectID := uuid.New()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "pred-project")
	require.NoError(t, err)

	newBranch := func(name, origin string, merged bool, originID *uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := tdb.DB.ExecContext(ctx,
			`INSERT INTO kb.branches (id, project_id, name, origin, origin_id, merged_at)
			 VALUES (?, ?, ?, ?, ?, CASE WHEN ? THEN now() ELSE NULL END)`,
			id, projectID, name, origin, originID, merged)
		require.NoError(t, err)
		return id
	}
	extractionBranchID := newBranch("extraction/Invoice.pdf/1", "extraction", false, nil)
	manualBranchID := newBranch("plan/q3", "manual", false, nil)
	newBranch("extraction/Old.pdf/1", "extraction", true, nil)
	// Deleted source: deleting the document removed its extraction job, leaving
	// the branch's origin_id dangling (there is no foreign key to follow it).
	deletedSourceBranchID := newBranch("extraction/Deleted.pdf/1", "extraction", false,
		func() *uuid.UUID { id := uuid.New(); return &id }())

	// No boundary installed: a human API client, unbounded as before.
	predicate, args, err := repo.branchReadPredicate(ctx, projectID)
	require.NoError(t, err)
	assert.Empty(t, predicate, "a caller with no boundary must stay unbounded")
	assert.Empty(t, args)

	// An installed main-only boundary: main graph only.
	predicate, args, err = repo.branchReadPredicate(branchscope.ContextWith(ctx, nil), projectID)
	require.NoError(t, err)
	assert.Equal(t, "branch_id IS NULL", predicate)
	assert.Empty(t, args)

	// A scoped run: main plus the branches in scope, never the manual one and
	// never the branch whose extraction source was deleted.
	extraction, err := branchscope.New(branchscope.Config{Unmerged: true, Origins: []string{"extraction"}})
	require.NoError(t, err)
	extractionCtx := branchscope.ContextWith(ctx, extraction)
	predicate, args, err = repo.branchReadPredicate(extractionCtx, projectID)
	require.NoError(t, err)
	assert.Equal(t, "(branch_id IS NULL OR branch_id IN (?))", predicate)
	require.Len(t, args, 1, "the arg is the bun IN-list of the in-scope branch ids")

	// Rows to read: one on main, one on the in-scope extraction branch, one on the
	// out-of-scope manual branch, and one on the deleted-source branch.
	canonicalID := uuid.New()
	insertEdge := func(branchID *uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := tdb.DB.ExecContext(ctx,
			`INSERT INTO kb.graph_relationships (id, project_id, type, src_id, dst_id, canonical_id, version, branch_id)
			 VALUES (?, ?, 'RELATES_TO', ?, ?, ?, 1, ?)`,
			id, projectID, uuid.New(), canonicalID, id, branchID)
		require.NoError(t, err)
		return id
	}
	mainEdgeID := insertEdge(nil)
	extractionEdgeID := insertEdge(&extractionBranchID)
	insertEdge(&manualBranchID)
	insertEdge(&deletedSourceBranchID)

	branchOf := func(edgeID uuid.UUID, edges []*GraphRelationship) bool {
		for _, e := range edges {
			if e.ID == edgeID {
				return true
			}
		}
		return false
	}

	// A scoped run sees main and its own branch, and nothing else.
	incoming, outgoing, err := repo.GetEdges(extractionCtx, projectID, canonicalID, GetEdgesParams{Direction: "both"})
	require.NoError(t, err, "the scoped edge query must execute")
	edges := append(incoming, outgoing...)
	assert.True(t, branchOf(mainEdgeID, edges), "the main edge is readable")
	assert.True(t, branchOf(extractionEdgeID, edges), "the in-scope branch edge is readable")
	assert.Len(t, edges, 2, "the manual and deleted-source edges must not be readable")

	// A main-only run sees the main edge only.
	incoming, outgoing, err = repo.GetEdges(branchscope.ContextWith(ctx, nil), projectID, canonicalID,
		GetEdgesParams{Direction: "both"})
	require.NoError(t, err)
	edges = append(incoming, outgoing...)
	require.Len(t, edges, 1)
	assert.Equal(t, mainEdgeID, edges[0].ID)

	// A caller with no boundary sees all four, as before.
	incoming, outgoing, err = repo.GetEdges(ctx, projectID, canonicalID, GetEdgesParams{Direction: "both"})
	require.NoError(t, err)
	assert.Len(t, append(incoming, outgoing...), 4)

	// An internal cascade asks for every branch explicitly, so a delete removes
	// the relationships a read boundary would have hidden (#265).
	incoming, outgoing, err = repo.GetEdges(branchscope.ContextWith(ctx, nil), projectID, canonicalID,
		GetEdgesParams{Direction: "both", IncludeAllBranches: true})
	require.NoError(t, err)
	assert.Len(t, append(incoming, outgoing...), 4)

	// A scope that matches no branch reads main only rather than everything.
	agentOrigin, err := branchscope.New(branchscope.Config{Unmerged: true, Origins: []string{"agent"}})
	require.NoError(t, err)
	predicate, args, err = repo.branchReadPredicate(branchscope.ContextWith(ctx, agentOrigin), projectID)
	require.NoError(t, err)
	assert.Equal(t, "branch_id IS NULL", predicate)
	assert.Empty(t, args)
}
