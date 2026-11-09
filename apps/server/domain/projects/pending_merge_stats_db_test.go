package projects_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestGetByIDStatsReportsPendingExtractionMerge executes the project-status SQL
// from #496 against a real database. The counts are correlated subqueries that
// are easy to get subtly wrong (merged ledger rows, agent branches, main-graph
// scope), so this test seeds each shape and asserts the aggregate rather than
// only the Go mapping (covered by pending_merge_stats_test.go).
func TestGetByIDStatsReportsPendingExtractionMerge(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "projectpendingmerge")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Pending Merge Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Pending Merge Project",
	}, testutil.AdminUser.ID))
	pid := uuid.MustParse(projectID)

	// Pending extraction branch (counts), already-merged extraction branch
	// (excluded), and an agent branch (excluded by origin).
	pendingBranch := uuid.New()
	mergedBranch := uuid.New()
	agentBranch := uuid.New()
	mergedAt := time.Now().UTC().Add(-time.Hour)
	for _, row := range []struct {
		id     uuid.UUID
		origin string
		merged *time.Time
	}{
		{pendingBranch, "extraction", nil},
		{mergedBranch, "extraction", &mergedAt},
		{agentBranch, "agent", nil},
	} {
		_, err := db.ExecContext(ctx, `
			INSERT INTO kb.branches (id, project_id, name, origin, merged_at)
			VALUES (?, ?, ?, ?, ?)
		`, row.id, pid, "branch-"+row.id.String(), row.origin, row.merged)
		require.NoError(t, err)
	}

	insertObject := func(branchID *uuid.UUID, key string) uuid.UUID {
		canonical := uuid.New()
		_, err := db.ExecContext(ctx, `
			INSERT INTO kb.graph_objects (id, project_id, canonical_id, type, key, version, branch_id)
			VALUES (?, ?, ?, 'BenchEntity', ?, 1, ?)
		`, uuid.New(), pid, canonical, key, branchID)
		require.NoError(t, err)
		return canonical
	}

	insertObject(&pendingBranch, "pending-1")
	insertObject(&pendingBranch, "pending-2")
	insertObject(&mergedBranch, "merged-1")
	insertObject(&agentBranch, "agent-1")
	insertObject(nil, "main-1")

	repo := projects.NewRepository(db, slog.Default())
	project, err := repo.GetByID(ctx, projectID, true)
	require.NoError(t, err)
	require.NotNil(t, project.Stats)
	require.NotNil(t, project.Stats.PendingExtractionMerge)

	got := project.Stats.PendingExtractionMerge
	require.Equal(t, 1, got.BranchCount, "only the unmerged extraction branch counts")
	require.Equal(t, 2, got.ObjectCount, "only live objects on the pending branch count")
	require.Equal(t, 1, got.MainGraphObjectCount, "main-graph objects are what default reads see")
	require.False(t, got.Stranded)

	// The list query uses a different SELECT shape (a membership join); it must
	// expose the same signal.
	require.NoError(t, testutil.CreateTestUser(ctx, db, testutil.AdminUser))
	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.project_memberships (id, project_id, user_id, role)
		VALUES (?, ?, ?, 'project_admin')
	`, uuid.New(), pid, testutil.AdminUser.ID)
	require.NoError(t, err)

	list, err := repo.List(ctx, projects.ListParams{
		UserID:       testutil.AdminUser.ID,
		ProjectID:    projectID,
		IncludeStats: true,
	})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NotNil(t, list[0].Stats)
	require.NotNil(t, list[0].Stats.PendingExtractionMerge)
	require.Equal(t, 2, list[0].Stats.PendingExtractionMerge.ObjectCount)
}
