package notifications

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// TestResolveResourceScopesToProject is the project-isolation guard for the
// resolve-resource endpoint (#2041 review). The update must bind project_id so a
// resolve call cannot resolve another project's notifications; before the fix
// the query had no project predicate at all.
func TestResolveResourceScopesToProject(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectExec(`UPDATE[\s\S]*project_id = 'prj'[\s\S]*related_resource_type = 'graph_branch'[\s\S]*related_resource_id = 'branch1'[\s\S]*requires_action = true[\s\S]*action_status IS NULL[\s\S]*`).
		WillReturnResult(sqlmock.NewResult(0, 2))

	count, err := repo.ResolveResource(context.Background(), "prj", "graph_branch", "branch1")
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	require.NoError(t, mock.ExpectationsWereMet())
}
