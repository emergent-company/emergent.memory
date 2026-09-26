package agents

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestGetConversationFullHistoryRaw_ScopedToCaller proves the MCP
// session-get-messages data-access layer enforces the #1010 conversation
// ownership model. Before the fix (issue #1032) GetConversationFullHistoryRaw
// resolved a session by acp_session_id alone, so any caller could read another
// project's (or another member's private) session messages — including the
// composed system prompt persisted by #1006.
//
// The predicate is the shared sessiontodos.SessionAccessibleQuery: the session
// must be in the caller's project AND, when it is linked to a chat conversation
// (kb.chat_conversations.acp_session_id), that conversation must be owned by the
// caller or non-private. A foreign/unknown session fails closed to 404 so its
// existence does not leak.
func TestGetConversationFullHistoryRaw_ScopedToCaller(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_session_history_scoping")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB
	repo := NewRepository(db)

	orgID := uuid.NewString()
	require.NoError(t, rawExec(ctx, db, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "scoping-org"))

	projectA := uuid.NewString()
	require.NoError(t, rawExec(ctx, db, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectA, orgID, "Project A"))
	projectB := uuid.NewString()
	require.NoError(t, rawExec(ctx, db, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectB, orgID, "Project B"))

	// ownerB owns the private session in project B; attackerA is a different
	// project member (same org) who must not reach ownerB's private session.
	ownerB := uuid.NewString()
	attackerA := uuid.NewString()
	// chat_conversations.owner_user_id references core.user_profiles(id), so both
	// principals need a profile row for the FK to resolve.
	require.NoError(t, rawExec(ctx, db, `INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`, ownerB, "scoping-owner-b"))
	require.NoError(t, rawExec(ctx, db, `INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`, attackerA, "scoping-attacker-a"))

	seedSession := func(projectID, ownerID string, isPrivate bool) string {
		sessionID := uuid.NewString()
		require.NoError(t, rawExec(ctx, db, `INSERT INTO kb.acp_sessions (id, project_id, created_at, updated_at) VALUES (?, ?, NOW(), NOW())`, sessionID, projectID))

		convID := uuid.NewString()
		require.NoError(t, rawExec(ctx, db, `INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, acp_session_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())`, convID, "session-"+sessionID, projectID, isPrivate, ownerID, sessionID))

		agentID := uuid.NewString()
		require.NoError(t, rawExec(ctx, db, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, ?, ?, ?)`, agentID, "scoping-agent", "graph", "", projectID))

		runID := uuid.NewString()
		require.NoError(t, rawExec(ctx, db, `INSERT INTO kb.agent_runs (id, agent_id, status, started_at, acp_session_id) VALUES (?, ?, 'completed', NOW(), ?)`, runID, agentID, sessionID))

		// One message with a system-prompt-like payload to mirror what #1006 made
		// sensitive; the content is a literal so no bound JSONB casting is needed.
		require.NoError(t, rawExec(ctx, db, `INSERT INTO kb.agent_run_messages (id, run_id, role, content, step_number, created_at) VALUES (?, ?, 'assistant', '{"text":"secret reply"}'::jsonb, 0, NOW())`, uuid.NewString(), runID))

		return sessionID
	}

	// Owner (project B) reading their own private session must succeed.
	privateSession := seedSession(projectB, ownerB, true)
	items, err := repo.GetConversationFullHistoryRaw(ctx, projectB, ownerB, privateSession)
	require.NoError(t, err, "owner must read their own private session")
	require.NotEmpty(t, items, "owner must receive the session's messages")

	// Cross-project: a caller in project A must not read project B's session.
	_, err = repo.GetConversationFullHistoryRaw(ctx, projectA, ownerB, privateSession)
	requireSessionNotFound(t, err, "cross-project session read must be refused")

	// Cross-user within project B: a non-owner member must not read a private
	// session.
	_, err = repo.GetConversationFullHistoryRaw(ctx, projectB, attackerA, privateSession)
	requireSessionNotFound(t, err, "foreign user's private session must be refused")

	// Unknown session id must also fail closed (no existence oracle).
	_, err = repo.GetConversationFullHistoryRaw(ctx, projectB, ownerB, uuid.NewString())
	requireSessionNotFound(t, err, "unknown session id must be refused")

	// Non-private (project-shared) carve-out: a non-owner member of the same
	// project may read a shared conversation's session.
	sharedSession := seedSession(projectB, ownerB, false)
	items, err = repo.GetConversationFullHistoryRaw(ctx, projectB, attackerA, sharedSession)
	require.NoError(t, err, "non-private session must be readable by any project member")
	require.NotEmpty(t, items, "non-private session must return its messages")
}

// requireSessionNotFound asserts err is a 404 apperror, so a foreign/unknown
// session is indistinguishable from a missing one.
func requireSessionNotFound(t *testing.T, err error, msg string) {
	t.Helper()
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "%s: expected apperror, got: %v", msg, err)
	require.Equal(t, http.StatusNotFound, appErr.HTTPStatus, "%s: expected 404, got: %v", msg, err)
}

// rawExec runs a raw SQL statement against the hermetic test DB and fails the
// test on error.
func rawExec(ctx context.Context, db bun.IDB, query string, args ...any) error {
	_, err := db.NewRaw(query, args...).Exec(ctx)
	return err
}
