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

	// An INSTALLED main-only boundary — every agent run that declares no scope —
	// refuses branch reads outright: otherwise a delegated child would read more
	// than its scoped parent, which is the widening this boundary exists to stop.
	mainOnlyCtx := branchscope.ContextWith(ctx, nil)
	for _, ref := range []string{"plan/q3", manualID.String()} {
		_, err := svc.resolveReadableBranchID(mainOnlyCtx, projectID.String(), ref)
		assert.ErrorContains(t, err, "main graph only", "a main-only run must not read a branch")
	}
	// In-scope branches are still readable through a read tool under the scope.
	got, err = svc.resolveReadableBranchID(scopedCtx, projectID.String(), "extraction/Invoice.pdf/1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, extractionID, *got)

	// Branch MANAGEMENT is deliberately not taken away: an agent could always
	// create a branch and write to it, and merging can surface a branch's content
	// in main regardless — that is a write-permission question, not a read scope.
	got, err = svc.resolveBranchID(mainOnlyCtx, projectID.String(), "plan/q3")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, manualID, *got)

	// No boundary at all is a human MCP client: unchanged, read included.
	got, err = svc.resolveReadableBranchID(ctx, projectID.String(), "plan/q3")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, manualID, *got)
}

// TestReadableBranchRefusesDeletedExtractionSource covers the source half of the
// read boundary: an extraction branch whose source document was deleted must not
// be readable through a branch scope, however it was linked to its job (#265,
// #246). The search fan-out applies the same predicate in SQL, so naming the
// branch must not reach what a scoped search would not return.
func TestReadableBranchRefusesDeletedExtractionSource(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "mcpbranchsource")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB
	svc := &Service{db: db}

	orgID := uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "branch-source-org")
	require.NoError(t, err)
	projectID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "branch-source-project")
	require.NoError(t, err)

	// A live extraction branch: the document and its job still exist.
	liveDocID, liveJobID := uuid.New(), uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.documents (id, project_id, filename) VALUES (?, ?, ?)`,
		liveDocID, projectID, "Invoice.pdf")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.object_extraction_jobs (id, project_id, document_id) VALUES (?, ?, ?)`,
		liveJobID, projectID, liveDocID)
	require.NoError(t, err)

	liveBranchID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.branches (id, project_id, name, origin, origin_id) VALUES (?, ?, ?, 'extraction', ?)`,
		liveBranchID, projectID, "extraction/Invoice.pdf/1", liveJobID)
	require.NoError(t, err)

	// The source was deleted: deleting the document took the job with it, leaving
	// the branch's origin_id dangling (there is no foreign key from a branch).
	danglingBranchID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.branches (id, project_id, name, origin, origin_id) VALUES (?, ?, ?, 'extraction', ?)`,
		danglingBranchID, projectID, "extraction/Deleted.pdf/1", uuid.New())
	require.NoError(t, err)

	// The older delete path left the job behind with a null document_id and no
	// origin_id on the branch; the staging link still proves the source is gone.
	stagingBranchID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.branches (id, project_id, name, origin) VALUES (?, ?, ?, 'extraction')`,
		stagingBranchID, projectID, "extraction/staging-only/1")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.object_extraction_jobs (id, project_id, document_id, staging_branch_id) VALUES (?, ?, NULL, ?)`,
		uuid.New(), projectID, stagingBranchID)
	require.NoError(t, err)

	scoped, err := branchscope.New(branchscope.Config{Unmerged: true, Origins: []string{"extraction"}})
	require.NoError(t, err)
	scopedCtx := branchscope.ContextWith(ctx, scoped)

	// The live branch is readable; the two deleted-source shapes are not.
	got, err := svc.resolveReadableBranchID(scopedCtx, projectID.String(), liveBranchID.String())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, liveBranchID, *got)

	for _, ref := range []string{danglingBranchID.String(), stagingBranchID.String()} {
		_, err := svc.resolveReadableBranchID(scopedCtx, projectID.String(), ref)
		assert.ErrorContains(t, err, "extraction source document was deleted",
			"a deleted source's staged objects must not be readable through a scope")
	}

	// A human client with no boundary keeps its pre-existing access: the exclusion
	// is part of the scope's own guarantee, not a new global rule.
	got, err = svc.resolveReadableBranchID(ctx, projectID.String(), danglingBranchID.String())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, danglingBranchID, *got)
}
