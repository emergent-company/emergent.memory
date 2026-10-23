package sessiontodos_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sessiontodos"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestOwnerlessCallerSessionScope covers the device session log, mirroring
// TestOwnerlessCallerScope (#2112) and TestOwnerlessCreateConversation (#2136).
//
// A device credential is ownerless (core.api_tokens.user_id is NULL), so the
// request carries "". SessionAccessibleQuery used to bind that empty string
// against c.owner_user_id — a uuid column — unconditionally, and Postgres
// rejected the parameter cast with SQLSTATE 22P02 before any row was examined
// (the c.id IS NULL branch does not rescue a bad parameter). Every session-scoped
// entry point shares this predicate (session todos, the MCP session-get-messages
// tool), so an ownerless caller could read no session at all (#2141).
//
// The chosen semantics are the chat ones: an ownerless caller owns nothing, so it
// sees project-scoped sessions and sessions backed by a project-shared
// conversation, and never another member's private conversation.
func TestOwnerlessCallerSessionScope(t *testing.T) {
	ctx := context.Background()
	db := testutil.SetupTestDBOrFail(t, ctx, "sessiontodos_ownerless")
	t.Cleanup(func() { db.Close() })

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	otherProjectID := uuid.New().String()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db.DB, orgID, "ownerless-org"))
	require.NoError(t, testutil.CreateTestProject(ctx, db.DB, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "ownerless-project",
	}, testutil.AdminUser.ID))
	require.NoError(t, testutil.CreateTestProject(ctx, db.DB, testutil.TestProject{
		ID:    otherProjectID,
		OrgID: orgID,
		Name:  "ownerless-other-project",
	}, testutil.AdminUser.ID))
	require.NoError(t, testutil.CreateTestUser(ctx, db.DB, testutil.AdminUser))

	foreignID := "00000000-0000-0000-0000-00000000c0de"
	require.NoError(t, testutil.CreateTestUser(ctx, db.DB, testutil.TestUser{
		ID:            foreignID,
		ZitadelUserID: "ownerless-foreign",
		Email:         "ownerless-foreign@test.local",
	}))
	require.NoError(t, testutil.CreateTestOrgMembership(ctx, db.DB, orgID, foreignID, "member"))

	// seedSession inserts a session in project, optionally linking it to a chat
	// conversation owned by ownerID (private when isPrivate), and returns it.
	seedSession := func(project, ownerID string, isPrivate bool, linked bool) string {
		sessionID := uuid.New()
		_, err := db.DB.NewRaw(`
			INSERT INTO kb.sessions (id, project_id, created_at, updated_at)
			VALUES (?, ?, NOW(), NOW())
		`, sessionID, project).Exec(ctx)
		require.NoError(t, err)
		if linked {
			_, err = db.DB.NewRaw(`
				INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, session_id, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())
			`, uuid.New(), "conv", project, isPrivate, ownerID, sessionID).Exec(ctx)
			require.NoError(t, err)
		}
		return sessionID.String()
	}

	// The caller (ownerless) must be able to look at these two.
	sharedSession := seedSession(projectID, testutil.AdminUser.ID, false, true)
	unlinkedSession := seedSession(projectID, testutil.AdminUser.ID, false, false)
	// ...and must not be able to look at these.
	foreignPrivate := seedSession(projectID, foreignID, true, true)
	otherProjectShared := seedSession(otherProjectID, testutil.AdminUser.ID, false, true)
	// A session that does not exist at all must fail closed rather than error.
	unknown := uuid.New().String()

	repo := sessiontodos.NewRepository(db.DB, slog.Default())

	allowed := []struct {
		name      string
		sessionID string
	}{
		{"a session backed by a project-shared conversation", sharedSession},
		{"a session with no linked conversation (project scope)", unlinkedSession},
	}
	for _, tc := range allowed {
		ok, err := repo.SessionAccessible(ctx, tc.sessionID, projectID, "")
		require.NoErrorf(t, err, "an ownerless caller must not fail on a uuid comparison: %s", tc.name)
		require.Truef(t, ok, "ownerless caller must be allowed: %s", tc.name)
	}

	denied := []struct {
		name      string
		sessionID string
	}{
		{"another member's private conversation", foreignPrivate},
		{"a session in another project", otherProjectShared},
		{"an unknown session id", unknown},
	}
	for _, tc := range denied {
		ok, err := repo.SessionAccessible(ctx, tc.sessionID, projectID, "")
		require.NoErrorf(t, err, "an ownerless caller must not error, it must be denied: %s", tc.name)
		require.Falsef(t, ok, "ownerless caller must be denied: %s", tc.name)
	}

	// The predicate is shared, so the package-level entry point must agree with
	// the method (and with the owner case, which was never broken).
	ok, err := sessiontodos.SessionAccessibleQuery(ctx, db.DB, sharedSession, projectID, "")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = sessiontodos.SessionAccessibleQuery(ctx, db.DB, foreignPrivate, projectID, testutil.AdminUser.ID)
	require.NoError(t, err)
	require.False(t, ok, "unrelated: the owner of a different private conversation still gets nothing")

	ok, err = sessiontodos.SessionAccessibleQuery(ctx, db.DB, seedSession(projectID, testutil.AdminUser.ID, true, true), projectID, testutil.AdminUser.ID)
	require.NoError(t, err)
	require.True(t, ok, "a real owner must still reach its own private conversation")
}
