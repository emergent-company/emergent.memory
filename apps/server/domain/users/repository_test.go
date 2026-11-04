package users

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// connectTestDB opens a throwaway test database owned by this test (see the
// skills domain's helper for the same pattern). Each test runs against a
// uniquely-named database dropped on cleanup.
func connectTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "users")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

// seedUser creates a user profile plus a verified email and returns the user id.
func seedUser(t *testing.T, db bun.IDB, email string) string {
	t.Helper()
	userID := uuid.NewString()
	_, err := db.ExecContext(t.Context(),
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "User "+email)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(),
		`INSERT INTO core.user_emails (id, user_id, email, verified) VALUES (?, ?, ?, true)`,
		uuid.NewString(), userID, email)
	require.NoError(t, err)
	return userID
}

// seedOrg creates an org row and returns its id.
func seedOrg(t *testing.T, db bun.IDB, name string) string {
	t.Helper()
	orgID := uuid.NewString()
	_, err := db.ExecContext(t.Context(), `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, name)
	require.NoError(t, err)
	return orgID
}

// seedMembership adds an org_admin membership row for user in org.
func seedMembership(t *testing.T, db bun.IDB, orgID, userID string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(),
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`,
		orgID, userID)
	require.NoError(t, err)
}

// TestRepository_SearchByEmail_SameOrgOnly is the two-org regression for issue
// #1022: a caller in org A searching an email prefix that matches a user in org
// A and a user in org B sees only the org A user. The cross-org user is omitted
// from results entirely (its profile fields are never returned).
func TestRepository_SearchByEmail_SameOrgOnly(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())

	orgA := seedOrg(t, db, "org-a")
	orgB := seedOrg(t, db, "org-b")

	caller := seedUser(t, db, "caller@example.com")
	seedMembership(t, db, orgA, caller)

	// Two target users sharing the same email prefix, in different orgs.
	alice := seedUser(t, db, "alice@example.com") // org A — visible
	seedMembership(t, db, orgA, alice)

	bob := seedUser(t, db, "alice.bob@example.com") // org B only — hidden
	seedMembership(t, db, orgB, bob)

	results, err := repo.SearchByEmail(t.Context(), "alice", caller)
	require.NoError(t, err)

	ids := make(map[string]bool, len(results))
	for _, r := range results {
		ids[r.ID] = true
	}

	assert.True(t, ids[alice], "same-org user must be returned")
	assert.False(t, ids[bob], "cross-org user must be omitted from results")
}

// TestRepository_SearchByEmail_CallerWithoutOrgSeesNothing asserts a caller
// with no organization membership cannot enumerate anyone (fail closed).
func TestRepository_SearchByEmail_CallerWithoutOrgSeesNothing(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())

	orgA := seedOrg(t, db, "org-a")
	alice := seedUser(t, db, "alice@example.com")
	seedMembership(t, db, orgA, alice)

	// Caller belongs to no org at all.
	stranger := seedUser(t, db, "stranger@example.com")

	results, err := repo.SearchByEmail(t.Context(), "alice", stranger)
	require.NoError(t, err)
	assert.Empty(t, results, "a caller with no org membership must not enumerate anyone")
}

// TestRepository_SearchByEmail_ExcludesCaller asserts the caller is never
// returned in their own search, even when their email matches the query.
func TestRepository_SearchByEmail_ExcludesCaller(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())

	orgA := seedOrg(t, db, "org-a")
	caller := seedUser(t, db, "caller@example.com")
	seedMembership(t, db, orgA, caller)

	// A second same-org user sharing the caller's email prefix.
	alice := seedUser(t, db, "caller.alice@example.com")
	seedMembership(t, db, orgA, alice)

	results, err := repo.SearchByEmail(t.Context(), "caller", caller)
	require.NoError(t, err)

	for _, r := range results {
		assert.NotEqual(t, caller, r.ID, "the caller must be excluded from their own search")
	}
	assert.Contains(t, resultIDs(results), alice, "same-org user matching the query must still be returned")
}

// resultIDs maps search results to their IDs for assertion helpers.
func resultIDs(results []UserSearchResult) []string {
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	return ids
}
