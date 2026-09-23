package superadmin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// connectTestDB opens a throwaway test database owned by this test, so each
// test runs against a uniquely-named database it drops on cleanup. Skips when
// unavailable or in short mode; fails when REQUIRE_DB is set.
func connectTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "superadmin")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

// seedSuperadmin inserts a core.user_profiles row (FK target of
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

// IsSuperadminFull must admit exactly the superadmin_full role: a
// superadmin_readonly grant is a platform-observation role and must not mint or
// mutate platform-global state (issues #810/#839).
func TestRepository_IsSuperadminFull_AdmitsOnlySuperadminFull(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db)
	ctx := context.Background()

	t.Run("superadmin_full admitted", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadmin(t, db, userID, auth.RoleSuperadminFull)

		ok, err := repo.IsSuperadminFull(ctx, userID)
		require.NoError(t, err)
		require.True(t, ok, "superadmin_full must be admitted")
	})

	t.Run("superadmin_readonly refused", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadmin(t, db, userID, auth.RoleSuperadminReadonly)

		ok, err := repo.IsSuperadminFull(ctx, userID)
		require.NoError(t, err)
		require.False(t, ok, "superadmin_readonly must be refused")
	})

	t.Run("revoked superadmin_full refused", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadmin(t, db, userID, auth.RoleSuperadminFull)
		_, err := db.ExecContext(ctx,
			`UPDATE core.superadmins SET revoked_at = NOW() WHERE user_id = ?`, userID)
		require.NoError(t, err)

		ok, err := repo.IsSuperadminFull(ctx, userID)
		require.NoError(t, err)
		require.False(t, ok, "revoked superadmin_full must be refused")
	})

	t.Run("non-superadmin refused", func(t *testing.T) {
		ok, err := repo.IsSuperadminFull(ctx, uuid.NewString())
		require.NoError(t, err)
		require.False(t, ok, "a user with no superadmin grant must be refused")
	})
}
