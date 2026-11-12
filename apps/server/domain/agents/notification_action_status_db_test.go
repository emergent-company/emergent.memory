package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestUpdateNotificationActionStatusRecordsNonOwnerResponder is the regression
// test for the agent-question writer's scope.
//
// The respond route admits any project member, so the person answering is often
// not the notification's owner. An owner-scoped write would refuse them and
// leave the question in the owner's Action-required tab (requires_action AND
// action_status IS NULL) after it had already been answered and the run resumed.
// The write is therefore identified by the row's id alone, and the outcome is
// recorded without an attribution it cannot honestly make.
func TestUpdateNotificationActionStatusRecordsNonOwnerResponder(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agent_notif_verb_nonowner")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	ownerID := insertProfile(t, ctx, db, "Owner")
	notificationID := insertQuestionNotification(t, ctx, db, ownerID)

	// The responder is a different project member; the repository is not given
	// a user id at all, because it has no comparable one to scope or attribute
	// the write to.
	repo := NewRepository(db)
	require.NoError(t, repo.UpdateNotificationActionStatus(ctx, notificationID, "completed"),
		"a responder who is not the owner must still record the outcome")

	var status, attributedTo *string
	require.NoError(t, db.NewRaw(
		`SELECT action_status, action_status_by FROM kb.notifications WHERE id = ?`,
		notificationID).Scan(ctx, &status, &attributedTo))
	require.NotNil(t, status)
	require.Equal(t, "completed", *status)
	require.Nil(t, attributedTo, "there is no owner id to attribute the outcome to")
}

// TestUpdateNotificationActionStatusKeepsTheFirstVerb is the other half: the
// inbox records the resolve verb first-write-wins, and before this change
// answering the same agent question from chat updated the row unconditionally,
// so the verb and its attribution silently changed hands.
func TestUpdateNotificationActionStatusKeepsTheFirstVerb(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agent_notif_verb")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	ownerID := insertProfile(t, ctx, db, "Owner")
	notificationID := insertQuestionNotification(t, ctx, db, ownerID)

	// The owner resolves it from the inbox first.
	require.NoError(t, notifications.ResolveActionStatus(ctx, db, ownerID, notificationID, "accepted"))

	repo := NewRepository(db)
	err := repo.UpdateNotificationActionStatus(ctx, notificationID, "completed")
	require.ErrorIs(t, err, notifications.ErrActionAlreadyResolved,
		"a differing verb must lose the first-write-wins race, not overwrite it")

	var status, attributedTo *string
	require.NoError(t, db.NewRaw(
		`SELECT action_status, action_status_by FROM kb.notifications WHERE id = ?`,
		notificationID).Scan(ctx, &status, &attributedTo))
	require.NotNil(t, status)
	require.Equal(t, "accepted", *status, "the verb the inbox recorded must survive")
	require.NotNil(t, attributedTo)
	require.Equal(t, ownerID, *attributedTo, "attribution must not change hands")

	// A replay of the verb that already landed stays idempotent on this surface.
	require.NoError(t, repo.UpdateNotificationActionStatus(ctx, notificationID, "accepted"))
}

func insertProfile(t *testing.T, ctx context.Context, db bun.IDB, label string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name, created_at, updated_at)
		VALUES (?, ?, ?, now(), now())`, id, "zitadel-"+id, label).Exec(ctx)
	require.NoError(t, err)
	return id
}

func insertQuestionNotification(t *testing.T, ctx context.Context, db bun.IDB, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.notifications (id, user_id, title, message)
		VALUES (?, ?, 'Question from agent', 'The agent needs an answer')`, id, ownerID).Exec(ctx)
	require.NoError(t, err)
	return id
}
