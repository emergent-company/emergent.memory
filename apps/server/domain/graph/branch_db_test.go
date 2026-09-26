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
