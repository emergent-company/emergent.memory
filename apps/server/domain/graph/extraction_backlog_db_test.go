package graph_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestExtractionBacklogCountsOnlyPendingExtractionBranches is the regression
// test for #496: a project whose extraction ran with auto-merge off has all of
// its objects on unmerged extraction branches, so default graph reads (main
// graph only) report nothing. The backlog must count those objects, ignore
// merged/agent/manual branches, and expose the stranded state.
func TestExtractionBacklogCountsOnlyPendingExtractionBranches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode (#496)")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "extractionbacklog")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Backlog Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Backlog Project",
	}, testutil.AdminUser.ID))

	pid := uuid.MustParse(projectID)
	repo := graph.NewRepository(db, slog.Default(), testDB.Config)
	svc := graph.NewService(repo, slog.Default(), nil, nil, nil, nil, nil, nil, nil, nil)

	makeBranch := func(name, origin string, merged bool) *graph.Branch {
		b := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: name, Origin: origin, CreatedAt: time.Now().UTC()}
		if merged {
			at := time.Now().UTC().Add(-time.Hour)
			b.MergedAt = &at
		}
		require.NoError(t, repo.CreateBranch(ctx, b))
		return b
	}

	pendingBranch := makeBranch("extraction/doc-a/job-1", "extraction", false)
	mergedBranch := makeBranch("extraction/doc-b/job-2", "extraction", true)
	agentBranch := makeBranch("agent/run-3", "agent", false)

	createOn := func(key string, branchID *uuid.UUID) *graph.GraphObjectResponse {
		k := key
		obj, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type:     "BenchEntity",
			Key:      &k,
			BranchID: branchID,
		}, nil)
		require.NoError(t, err)
		return obj
	}

	// Two live objects on the pending branch; one on the already-merged branch
	// and one on an agent branch (neither is extraction backlog); one on main so
	// default reads are not empty.
	objA := createOn("pending-a", &pendingBranch.ID)
	objB := createOn("pending-b", &pendingBranch.ID)
	createOn("merged", &mergedBranch.ID)
	createOn("agent-owned", &agentBranch.ID)
	createOn("main-visible", nil)

	// One live relationship on the pending branch.
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_relationships
			(id, project_id, type, src_id, dst_id, properties, canonical_id, version, branch_id)
		VALUES (?, ?, 'RelatedTo', ?, ?, '{}'::jsonb, ?, 1, ?)
	`, uuid.New(), pid, objA.CanonicalID, objB.CanonicalID, uuid.New(), pendingBranch.ID)
	require.NoError(t, err)

	backlog, err := svc.ExtractionBacklog(ctx, pid)
	require.NoError(t, err)

	require.Equal(t, 1, backlog.BranchCount, "only the unmerged extraction branch is pending")
	require.Equal(t, 2, backlog.ObjectCount, "both live objects on the pending branch count")
	require.Equal(t, 1, backlog.RelationshipCount, "the relationship on the pending branch counts")
	require.Equal(t, 1, backlog.MainGraphObjectCount, "the main-graph object is what default reads see")
	require.False(t, backlog.Stranded, "main graph is not empty")
	require.Len(t, backlog.Branches, 1)
	require.Equal(t, pendingBranch.ID, backlog.Branches[0].ID)
}

// TestMergeExtractionBacklogDryRunTouchesNothing pins the safety default: a
// request without execute=true reports what would be merged and leaves the
// branch and its objects untouched. This is the "one-click preview" half of the
// acceptance criteria — merging must be an explicit second step.
func TestMergeExtractionBacklogDryRunTouchesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode (#496)")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "extractionbacklogmerge")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Backlog Merge Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Backlog Merge Project",
	}, testutil.AdminUser.ID))

	pid := uuid.MustParse(projectID)
	repo := graph.NewRepository(db, slog.Default(), testDB.Config)
	svc := graph.NewService(repo, slog.Default(), nil, nil, nil, nil, nil, nil, nil, nil)

	branch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "extraction/doc/job", Origin: "extraction", CreatedAt: time.Now().UTC()}
	require.NoError(t, repo.CreateBranch(ctx, branch))
	k := "dry-run-object"
	_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "BenchEntity", Key: &k, BranchID: &branch.ID,
	}, nil)
	require.NoError(t, err)

	resp, err := svc.MergeExtractionBacklog(ctx, pid, &graph.ExtractionBacklogMergeRequest{})
	require.NoError(t, err)

	require.True(t, resp.DryRun, "an unset execute must be a dry run")
	require.Equal(t, 1, resp.BranchesTotal)
	require.Equal(t, 1, resp.BranchesProcessed)
	require.Equal(t, 0, resp.BranchesRemaining)
	require.Len(t, resp.Results, 1)
	require.Empty(t, resp.Results[0].Error)
	require.NotNil(t, resp.Results[0].Result)

	// The branch is still open and still pending.
	after, err := svc.ExtractionBacklog(ctx, pid)
	require.NoError(t, err)
	require.Equal(t, 1, after.BranchCount)
	require.Equal(t, 1, after.ObjectCount)
}
