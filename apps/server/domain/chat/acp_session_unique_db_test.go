package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestACPSessionIDUniqueEnforcesOneToOneConversation is the regression test for
// migration 00183: kb.chat_conversations.acp_session_id is now UNIQUE, so a
// second conversation cannot point at the same ACP session (the 1:1 invariant
// becomes the database's job), while multiple NULL acp_session_id conversations
// remain permitted (non-agent conversations have no session).
func TestACPSessionIDUniqueEnforcesOneToOneConversation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "acpsession_unique")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "ACP Session Unique Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "ACP Session Unique Project",
	}, testutil.AdminUser.ID))

	sessionID := uuid.New()

	// Seed one ACP session and link one conversation to it (the normal 1:1 flow).
	_, err := db.NewRaw(`
		INSERT INTO kb.acp_sessions (id, project_id, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
	`, sessionID, projectID).Exec(ctx)
	require.NoError(t, err)

	convA := uuid.New()
	_, err = db.NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, acp_session_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, NOW(), NOW())
	`, convA, "conv-a", projectID, sessionID).Exec(ctx)
	require.NoError(t, err, "first conversation must link to the session (normal 1:1 flow)")

	// AFTER 00183: a second conversation reusing the same session is rejected.
	convB := uuid.New()
	_, err = db.NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, acp_session_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, NOW(), NOW())
	`, convB, "conv-b", projectID, sessionID).Exec(ctx)
	require.Error(t, err, "second conversation must not reuse an existing ACP session (unique violation expected)")

	// Multiple NULL acp_session_id conversations remain permitted: the column is
	// nullable and Postgres treats NULLs as distinct in a unique index.
	for _, title := range []string{"null-1", "null-2"} {
		_, err = db.NewRaw(`
			INSERT INTO kb.chat_conversations (id, title, project_id, acp_session_id, created_at, updated_at)
			VALUES (?, ?, ?, NULL, NOW(), NOW())
		`, uuid.New(), title, projectID).Exec(ctx)
		require.NoError(t, err, "NULL acp_session_id conversations must remain allowed")
	}
}
