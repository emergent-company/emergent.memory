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

// TestBranchCreateBranchScansAllColumns is the regression test for issue #952.
// graph.Branch lacked `description` (migration 00073) and `merged_at` (00076).
// CreateBranch performs INSERT ... RETURNING *, and bun scans every returned
// column into the entity, so the insert failed with
// "bun: Branch does not have column \"description\"". This exercises that exact
// path (the insert ForkBranch -> CreateBranch performs) and asserts the columns
// round-trip.
func TestBranchCreateBranchScansAllColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "branchfork")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Branch Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Branch Project",
	}, testutil.AdminUser.ID))

	repo := graph.NewRepository(db, slog.Default(), testDB.Config)

	desc := "forked from main"
	mergedAt := time.Now().UTC().Truncate(time.Millisecond)
	branch := &graph.Branch{
		ID:          uuid.New(),
		ProjectID:   uuid.MustParse(projectID),
		Name:        "feature-branch",
		Description: &desc,
		MergedAt:    &mergedAt,
	}

	require.NoError(t, repo.CreateBranch(ctx, branch),
		"INSERT ... RETURNING * must scan every migrated column")

	require.NotNil(t, branch.Description)
	require.Equal(t, desc, *branch.Description)
	require.NotNil(t, branch.MergedAt)

	got, err := repo.GetBranchByID(ctx, uuid.MustParse(projectID), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Description)
	require.Equal(t, desc, *got.Description)
	require.NotNil(t, got.MergedAt)
}

// TestForkBranchPersistsDescription is the regression test for #2231 item 1.
// ForkBranchRequest carries a description, both surfaces (REST body bind and the
// MCP graph-branch-fork tool) forward it, and ForkBranch built the branch entity
// without it — so the field was silently dropped and the new branch had no
// description. It reads the value back through the repository, not the response.
func TestForkBranchPersistsDescription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode (#2231)")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "branchforkdesc")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Fork Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Fork Project",
	}, testutil.AdminUser.ID))

	repo := graph.NewRepository(db, slog.Default(), testDB.Config)
	svc := graph.NewService(repo, slog.Default(), nil, nil, nil, nil, nil, nil, nil, nil)

	desc := "spike: restructure the auth domain"
	resp, err := svc.ForkBranch(ctx, uuid.MustParse(projectID), nil, &graph.ForkBranchRequest{
		Name:        "spike-auth",
		Description: &desc,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.BranchID)

	got, err := repo.GetBranchByID(ctx, uuid.MustParse(projectID), uuid.MustParse(resp.BranchID))
	require.NoError(t, err)
	require.NotNil(t, got.Description, "ForkBranch dropped the requested description (#2231)")
	require.Equal(t, desc, *got.Description)

	// A fork that asks for no description must not invent one.
	plain, err := svc.ForkBranch(ctx, uuid.MustParse(projectID), nil, &graph.ForkBranchRequest{
		Name: "spike-no-desc",
	})
	require.NoError(t, err)
	gotPlain, err := repo.GetBranchByID(ctx, uuid.MustParse(projectID), uuid.MustParse(plain.BranchID))
	require.NoError(t, err)
	require.Nil(t, gotPlain.Description)
}

// TestMergeBranchRejectsUnknownPolicy pins the wiring, not just the helper: the
// validation has to happen before the merge reads or writes anything. Before
// #2231 a typo'd policy fell through to "enrich" and merged with semantics the
// caller never asked for.
func TestMergeBranchRejectsUnknownPolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode (#2231)")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "mergereject")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Merge Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Merge Project",
	}, testutil.AdminUser.ID))

	repo := graph.NewRepository(db, slog.Default(), testDB.Config)
	svc := graph.NewService(repo, slog.Default(), nil, nil, nil, nil, nil, nil, nil, nil)

	forked, err := svc.ForkBranch(ctx, uuid.MustParse(projectID), nil, &graph.ForkBranchRequest{Name: "typo-policy"})
	require.NoError(t, err)
	sourceID := uuid.MustParse(forked.BranchID)

	_, err = svc.MergeBranch(ctx, uuid.MustParse(projectID), nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceID,
		Execute:        true,
		Policy:         "enrichme",
	})
	require.Error(t, err, "a typo'd policy must not merge the branch")
	require.Contains(t, err.Error(), "unknown merge policy")
	require.Contains(t, err.Error(), "enrich_no_sim", "the error must list the valid policies")

	// The branch must still be open: the rejected call ran no part of the merge.
	branch, err := repo.GetBranchByID(ctx, uuid.MustParse(projectID), sourceID)
	require.NoError(t, err)
	require.Nil(t, branch.MergedAt)

	// A valid preset still merges (nothing to copy from an empty fork).
	_, err = svc.MergeBranch(ctx, uuid.MustParse(projectID), nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceID,
		Execute:        true,
		Policy:         "manual",
	})
	require.NoError(t, err)
}
