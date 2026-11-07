package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// TestResolveBranchIDHonoursBranchScope covers the choke point every tool that
// takes a `branch` argument goes through: a run carrying a scope may name only a
// branch inside that scope, by name or by id, and a run with no scope keeps the
// old behaviour (#265).
func TestResolveBranchIDHonoursBranchScope(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "mcpbranchref")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB
	svc := &Service{db: db}

	orgID := uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "branch-ref-org")
	require.NoError(t, err)
	projectID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "branch-ref-project")
	require.NoError(t, err)

	newBranch := func(name, origin string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := db.ExecContext(ctx,
			`INSERT INTO kb.branches (id, project_id, name, origin) VALUES (?, ?, ?, ?)`,
			id, projectID, name, origin)
		require.NoError(t, err)
		return id
	}
	extractionID := newBranch("extraction/Invoice.pdf/1", "extraction")
	manualID := newBranch("plan/q3", "manual")

	scoped, err := branchscope.New(branchscope.Config{Unmerged: true, Origins: []string{"extraction"}})
	require.NoError(t, err)
	scopedCtx := branchscope.ContextWith(ctx, scoped)

	// In scope, by name and by id.
	got, err := svc.resolveBranchID(scopedCtx, projectID.String(), "extraction/Invoice.pdf/1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, extractionID, *got)
	got, err = svc.resolveBranchID(scopedCtx, projectID.String(), extractionID.String())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, extractionID, *got)

	// Out of scope, by name and by id: refused before any query reads it.
	_, err = svc.resolveBranchID(scopedCtx, projectID.String(), "plan/q3")
	assert.ErrorContains(t, err, "outside this agent's branch scope")
	_, err = svc.resolveBranchID(scopedCtx, projectID.String(), manualID.String())
	assert.ErrorContains(t, err, "outside this agent's branch scope")

	// A human client / non-agent caller carries no scope: unchanged behaviour.
	for _, ref := range []string{"plan/q3", manualID.String()} {
		got, err := svc.resolveBranchID(ctx, projectID.String(), ref)
		require.NoError(t, err, "unscoped callers keep branch access")
		require.NotNil(t, got)
		assert.Equal(t, manualID, *got)
	}

	// An unknown branch is still the resolver's own error, not a scope error.
	_, err = svc.resolveBranchID(scopedCtx, projectID.String(), "no/such-branch")
	assert.ErrorContains(t, err, "not found")
}
