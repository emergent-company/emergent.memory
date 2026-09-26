package apitoken

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// connectTestDB opens a throwaway test database owned by this test, so each
// test runs against a uniquely-named database it drops on cleanup. Skips when
// unavailable or in short mode; fails when REQUIRE_DB is set.
func connectTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "apitoken")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

// seedSuperadmin creates a core.user_profiles row (FK target of
// core.superadmins) and an active superadmin grant with the given role.
func seedSuperadmin(t *testing.T, db bun.IDB, userID, role string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User "+userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO core.superadmins (user_id, role) VALUES (?, ?)`,
		userID, role)
	require.NoError(t, err)
}

// seedUser inserts a core.user_profiles row and returns nothing.
func seedUser(t *testing.T, db bun.IDB, userID string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User "+userID)
	require.NoError(t, err)
}

// seedOrgAdmin inserts a core.user_profiles row, an org, and an org_admin
// membership in that org.
func seedOrgAdmin(t *testing.T, db bun.IDB, userID string) {
	t.Helper()
	ctx := context.Background()
	seedUser(t, db, userID)
	orgID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Org "+orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`,
		orgID, userID)
	require.NoError(t, err)
}

// seedProjectAdmin inserts a core.user_profiles row, an org, a project in that
// org, and a project_admin membership. No superadmin grant and no org_admin
// membership is created — this is the "bare project_admin" principal.
func seedProjectAdmin(t *testing.T, db bun.IDB, userID string) {
	t.Helper()
	ctx := context.Background()
	seedUser(t, db, userID)
	orgID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Org "+orgID)
	require.NoError(t, err)
	projectID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "Project "+projectID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, 'project_admin')`,
		projectID, userID)
	require.NoError(t, err)
}

// CanGrantAdminAll must require the superadmin_full role: a superadmin_readonly
// grant is a non-minting platform-observation role and must not be able to mint
// an admin:all token (issue #810), and an org_admin membership no longer
// qualifies — org-scoped authority must not buy platform-scoped power
// (issue #949).
func TestRepository_CanGrantAdminAll_RequiresSuperadminFull(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())
	ctx := context.Background()

	t.Run("superadmin_readonly cannot mint admin:all", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadmin(t, db, userID, "superadmin_readonly")

		allowed, err := repo.CanGrantAdminAll(ctx, userID)
		require.NoError(t, err)
		require.False(t, allowed, "superadmin_readonly must not mint admin:all")
	})

	t.Run("superadmin_full can mint admin:all", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadmin(t, db, userID, "superadmin_full")

		allowed, err := repo.CanGrantAdminAll(ctx, userID)
		require.NoError(t, err)
		require.True(t, allowed, "superadmin_full must mint admin:all")
	})

	t.Run("revoked superadmin_full cannot mint admin:all", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadmin(t, db, userID, "superadmin_full")
		_, err := db.ExecContext(ctx,
			`UPDATE core.superadmins SET revoked_at = NOW() WHERE user_id = ?`, userID)
		require.NoError(t, err)

		allowed, err := repo.CanGrantAdminAll(ctx, userID)
		require.NoError(t, err)
		require.False(t, allowed, "revoked superadmin_full must not mint admin:all")
	})

	t.Run("non-superadmin non-org-admin cannot mint admin:all", func(t *testing.T) {
		allowed, err := repo.CanGrantAdminAll(ctx, uuid.NewString())
		require.NoError(t, err)
		require.False(t, allowed, "unprivileged user must not mint admin:all")
	})

	t.Run("org_admin cannot mint admin:all", func(t *testing.T) {
		userID := uuid.NewString()
		seedOrgAdmin(t, db, userID)

		allowed, err := repo.CanGrantAdminAll(ctx, userID)
		require.NoError(t, err)
		require.False(t, allowed, "org_admin must not mint admin:all (org-scoped authority does not buy platform power)")
	})

	t.Run("bare project_admin cannot mint admin:all", func(t *testing.T) {
		userID := uuid.NewString()
		seedProjectAdmin(t, db, userID)

		allowed, err := repo.CanGrantAdminAll(ctx, userID)
		require.NoError(t, err)
		require.False(t, allowed, "project tier must not mint admin:all")
	})
}
