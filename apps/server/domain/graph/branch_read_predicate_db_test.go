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
// admits (#265).
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

	newBranch := func(name, origin string, merged bool) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := tdb.DB.ExecContext(ctx,
			`INSERT INTO kb.branches (id, project_id, name, origin, merged_at) VALUES (?, ?, ?, ?, CASE WHEN ? THEN now() ELSE NULL END)`,
			id, projectID, name, origin, merged)
		require.NoError(t, err)
		return id
	}
	newBranch("extraction/Invoice.pdf/1", "extraction", false)
	newBranch("plan/q3", "manual", false)
	newBranch("extraction/Old.pdf/1", "extraction", true)

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

	// A scoped run: main plus the branches in scope, never the manual one.
	extraction, err := branchscope.New(branchscope.Config{Unmerged: true, Origins: []string{"extraction"}})
	require.NoError(t, err)
	predicate, args, err = repo.branchReadPredicate(branchscope.ContextWith(ctx, extraction), projectID)
	require.NoError(t, err)
	assert.Equal(t, "(branch_id IS NULL OR branch_id IN (?))", predicate)
	require.Len(t, args, 1, "the arg is the bun IN-list of the in-scope branch ids")

	// The predicate must actually run: a bun IN-list passed through a raw
	// placeholder is the part that can fail at query time, not at construction.
	incoming, outgoing, err := repo.GetEdges(
		branchscope.ContextWith(ctx, extraction), projectID, uuid.New(), GetEdgesParams{Direction: "both"})
	require.NoError(t, err, "the scoped edge query must execute")
	assert.Empty(t, incoming)
	assert.Empty(t, outgoing)

	// A scope that matches no branch reads main only rather than everything.
	agentOrigin, err := branchscope.New(branchscope.Config{Unmerged: true, Origins: []string{"agent"}})
	require.NoError(t, err)
	predicate, args, err = repo.branchReadPredicate(branchscope.ContextWith(ctx, agentOrigin), projectID)
	require.NoError(t, err)
	assert.Equal(t, "branch_id IS NULL", predicate)
	assert.Empty(t, args)
}
