package useraccess

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestGetAccessTreeExcludesSoftDeletedRows is the regression guard for #44: a
// project scheduled for purge (deleted_at set) or an org pending deletion must
// not appear in the caller's access tree. Because the tree is ordered
// created_at DESC and its first project is the default-selection target, the
// deleted project is made the newest row — without the filter it would win the
// default pick.
func TestGetAccessTreeExcludesSoftDeletedRows(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "useraccess_tree")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	userID := uuid.NewString()
	activeOrgID := uuid.NewString()
	deletedOrgID := uuid.NewString()
	activeProjectID := uuid.NewString()
	deletedProjectID := uuid.NewString()
	projectInDeletedOrgID := uuid.NewString()

	// Membership rows reference core.user_profiles, so the caller must exist.
	_, err := db.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name, created_at, updated_at)
		VALUES (?, ?, 'Tree User', now(), now())`, userID, "zitadel-"+userID).Exec(ctx)
	require.NoError(t, err)

	_, err = db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Active org', now() - interval '1 hour', now())`, activeOrgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at, deleted_at)
		VALUES (?, 'Deleted org', now() - interval '1 hour', now(), now())`, deletedOrgID).Exec(ctx)
	require.NoError(t, err)

	// Active project is older; the soft-deleted project is newer, so
	// created_at DESC would put it first (the default-selection slot) if the
	// deleted_at filter were missing.
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Active project', ?, now() - interval '1 hour', now())`, activeProjectID, activeOrgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at, deleted_at, deletion_scheduled_for)
		VALUES (?, 'Project awaiting purge', ?, now(), now(), now(), now() + interval '24 hours')`, deletedProjectID, activeOrgID).Exec(ctx)
	require.NoError(t, err)
	// A live project under a soft-deleted org must not leak in either.
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Project in deleted org', ?, now(), now())`, projectInDeletedOrgID, deletedOrgID).Exec(ctx)
	require.NoError(t, err)

	_, err = db.NewRaw(`INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
		VALUES (?, ?, 'org_admin', now()), (?, ?, 'org_admin', now())`,
		activeOrgID, userID, deletedOrgID, userID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
		VALUES (?, ?, 'project_admin', now()), (?, ?, 'project_admin', now()), (?, ?, 'project_admin', now())`,
		activeProjectID, userID, deletedProjectID, userID, projectInDeletedOrgID, userID).Exec(ctx)
	require.NoError(t, err)

	tree, err := NewService(db).GetAccessTree(ctx, userID)
	require.NoError(t, err)
	require.Len(t, tree, 1, "the soft-deleted org must be absent")
	require.Equal(t, activeOrgID, tree[0].ID)
	require.Len(t, tree[0].Projects, 1, "only the live project under the live org may remain")
	require.Equal(t, activeProjectID, tree[0].Projects[0].ID)

	// Default-project selection consumes the first project in tree order; it
	// must be the live project, not the newer pending-deletion row.
	require.Equal(t, activeProjectID, tree[0].Projects[0].ID,
		"default selection must not land on a project awaiting deletion")

	for _, org := range tree {
		require.NotEqual(t, deletedOrgID, org.ID)
		for _, project := range org.Projects {
			require.NotEqual(t, deletedProjectID, project.ID)
			require.NotEqual(t, projectInDeletedOrgID, project.ID)
		}
	}
}
