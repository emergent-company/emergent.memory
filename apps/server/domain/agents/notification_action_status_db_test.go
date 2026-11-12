package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestUpdateNotificationActionStatusKeepsTheFirstVerb is the regression test for
// the second writer of a notification's action_status.
//
// The inbox records the resolve verb first-write-wins. Before this, answering
// the same agent question from chat updated the row unconditionally, so the verb
// and its attribution silently changed hands: the row showed "completed" after
// the user had accepted it, and action_status_by moved to the other responder.
// Both surfaces now go through one conditional write, so the second write loses
// and says so instead of overwriting.
func TestUpdateNotificationActionStatusKeepsTheFirstVerb(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agent_notif_verb")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	userID := uuid.NewString()
	notificationID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name, created_at, updated_at)
		VALUES (?, ?, 'Responder', now(), now())`, userID, "zitadel-"+userID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.notifications (id, user_id, title, message)
		VALUES (?, ?, 'Question from agent', 'The agent needs an answer')`,
		notificationID, userID).Exec(ctx)
	require.NoError(t, err)

	// The user resolves it from the inbox first.
	require.NoError(t, notifications.ResolveActionStatus(ctx, db, userID, notificationID, "accepted"))

	repo := NewRepository(db)
	err = repo.UpdateNotificationActionStatus(ctx, notificationID, "completed", userID)
	require.ErrorIs(t, err, notifications.ErrActionAlreadyResolved,
		"a differing verb must lose the first-write-wins race, not overwrite it")

	var status, attributedTo *string
	require.NoError(t, db.NewRaw(
		`SELECT action_status, action_status_by FROM kb.notifications WHERE id = ?`,
		notificationID).Scan(ctx, &status, &attributedTo))
	require.NotNil(t, status)
	require.Equal(t, "accepted", *status, "the verb the inbox recorded must survive")
	require.NotNil(t, attributedTo)
	require.Equal(t, userID, *attributedTo, "attribution must not change hands")

	// A replay of the verb that already landed stays idempotent on this surface.
	require.NoError(t, repo.UpdateNotificationActionStatus(ctx, notificationID, "accepted", userID))
}
