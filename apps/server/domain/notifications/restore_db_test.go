package notifications

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// newRestoreTestRepo provisions an isolated DB-backed repository plus a user
// profile row (kb.notifications.user_id references core.user_profiles).
func newRestoreTestRepo(t *testing.T) (*Repository, *testdb.TestDB, string) {
	t.Helper()
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "notif_restore_unique")
	t.Cleanup(tdb.Close)

	userID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User")
	require.NoError(t, err)

	return NewRepository(tdb.DB, slog.Default()), tdb, userID
}

// activeRowsForKey returns the ids of active (not-cleared) rows for a
// (user_id, group_key) pair. The partial unique index
// ux_notifications_user_group_key_active guarantees this never exceeds one.
func activeRowsForKey(t *testing.T, repo *Repository, userID, groupKey string) []Notification {
	t.Helper()
	var rows []Notification
	err := repo.db.NewSelect().
		Model(&rows).
		Where("user_id = ?", userID).
		Where("group_key = ?", groupKey).
		Where("cleared_at IS NULL").
		Scan(context.Background())
	require.NoError(t, err)
	return rows
}

// TestRestoreRefusesActiveGroupKeyConflict is the DB-backed regression for the
// #1345/#1347 interaction. Dismiss a budget alert (which frees the
// (user_id, group_key) key), let a newer active row be produced for the same
// key, then restore the old one.
//
// Before the fix Restore blindly set cleared_at = NULL and the partial unique
// index ux_notifications_user_group_key_active rejected the UPDATE, surfacing a
// database error. The chosen behaviour is to refuse: the newer active row is
// left untouched, the old row stays cleared/dismissed, and Restore returns the
// typed ErrActiveNotificationConflict. No data is lost, and at most one active
// row for the key still exists.
func TestRestoreRefusesActiveGroupKeyConflict(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	repo, _, userID := newRestoreTestRepo(t)

	groupKey := "budget-alert-" + uuid.NewString() + "-2026-01"

	old, err := repo.Create(ctx, &Notification{
		UserID:   userID,
		Title:    "Budget alert (stale)",
		Message:  "old",
		GroupKey: &groupKey,
	})
	require.NoError(t, err)

	// Dismiss frees the key (cleared_at is set), so a newer producer row can be
	// inserted for the same (user_id, group_key).
	require.NoError(t, repo.Dismiss(ctx, userID, old.ID))

	newer, err := repo.Create(ctx, &Notification{
		UserID:   userID,
		Title:    "Budget alert (current)",
		Message:  "new",
		GroupKey: &groupKey,
	})
	require.NoError(t, err)
	require.NotEqual(t, old.ID, newer.ID)

	active := activeRowsForKey(t, repo, userID, groupKey)
	require.Len(t, active, 1, "only the newer row is active before the restore")
	require.Equal(t, newer.ID, active[0].ID)

	// The restore must be refused, not error with a raw unique violation or 500.
	err = repo.Restore(ctx, userID, old.ID)
	require.ErrorIs(t, err, ErrActiveNotificationConflict,
		"restoring an older row while a newer same-key row is active must return the typed conflict")

	// The newer row is untouched and remains the single active row.
	active = activeRowsForKey(t, repo, userID, groupKey)
	require.Len(t, active, 1, "the conflict must not leave two active rows")
	require.Equal(t, newer.ID, active[0].ID, "the newer active row must be preserved")

	// The old row is left exactly as it was: still cleared and dismissed.
	var gotOld Notification
	require.NoError(t, repo.db.NewSelect().Model(&gotOld).Where("id = ?", old.ID).Scan(ctx))
	require.NotNil(t, gotOld.ClearedAt, "a refused restore must not un-clear the old row")
	require.True(t, gotOld.Dismissed, "a refused restore must not un-dismiss the old row")
	require.NotNil(t, gotOld.DismissedAt)
}

// TestRestoreGroupKeyNoConflict is the non-conflicting counterpart: a
// group-keyed notification can be restored normally while no active same-key
// row exists, preserving the #1345 semantics.
func TestRestoreGroupKeyNoConflict(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	repo, _, userID := newRestoreTestRepo(t)

	groupKey := "budget-alert-" + uuid.NewString() + "-2026-01"

	n, err := repo.Create(ctx, &Notification{
		UserID:   userID,
		Title:    "Budget alert",
		Message:  "m",
		GroupKey: &groupKey,
	})
	require.NoError(t, err)

	// Dismiss frees the key; nothing else holds it, so restore must succeed.
	require.NoError(t, repo.Dismiss(ctx, userID, n.ID))
	require.Empty(t, activeRowsForKey(t, repo, userID, groupKey))

	require.NoError(t, repo.Restore(ctx, userID, n.ID))

	var got Notification
	require.NoError(t, repo.db.NewSelect().Model(&got).Where("id = ?", n.ID).Scan(ctx))
	require.Nil(t, got.ClearedAt, "restore un-clears the row")
	require.False(t, got.Dismissed, "restore un-dismisses the row")
	require.Nil(t, got.DismissedAt)

	active := activeRowsForKey(t, repo, userID, groupKey)
	require.Len(t, active, 1)
	require.Equal(t, n.ID, active[0].ID)
}
