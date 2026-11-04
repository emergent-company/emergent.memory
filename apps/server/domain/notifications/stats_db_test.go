package notifications

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestGetStats_DismissedExcludesCleared is the DB-backed regression for #1314.
//
// All three GetStats counters share the base population `cleared_at IS NULL`.
// `Repository.Dismiss` sets `dismissed = true` AND `cleared_at = now()`, so a
// row dismissed through the API is cleared: it contributes to neither `total`,
// `unread`, nor the corrected `dismissed` counter. Before the fix the dismissed
// query omitted `cleared_at IS NULL`, so a dismissed-and-cleared row was
// excluded from `total` but still counted in `dismissed`, letting `dismissed`
// exceed `total`.
//
// The fixture only constructs states reachable through the repository's normal
// lifecycle (create, mark-read, dismiss, clear); it deliberately does not hand
// -craft a `dismissed = true AND cleared_at IS NULL` row, because `Dismiss`
// always clears. See TestGetStats_DismissedIsClearedInvariant for the focused
// invariant assertion.
func TestGetStats_DismissedExcludesCleared(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "notif_stats")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	userID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User")
	require.NoError(t, err)

	repo := NewRepository(db, slog.Default())

	// create inserts an active (uncleared, unread, non-dismissed) notification,
	// exactly the shape Repository.Create produces.
	create := func() string {
		t.Helper()
		n, err := repo.Create(ctx, &Notification{UserID: userID, Title: "t", Message: "m"})
		require.NoError(t, err)
		return n.ID
	}

	unreadID := create() // active, unread      -> total, unread
	readID := create()   // active, read        -> total
	require.NoError(t, repo.MarkRead(ctx, userID, readID))
	dismissedID := create() // dismissed (clears)  -> neither
	require.NoError(t, repo.Dismiss(ctx, userID, dismissedID))
	clearedID := create() // cleared only         -> neither
	require.NoError(t, repo.Clear(ctx, userID, clearedID))
	_ = unreadID

	stats, err := repo.GetStats(ctx, userID)
	require.NoError(t, err)

	// Expected numbers derived from the rows above: only the two active rows
	// have `cleared_at IS NULL`, so total = 2 and (of those) unread = 1. Both
	// the dismissed row (Dismiss also sets cleared_at) and the cleared-only row
	// are cleared, so the dismissed counter is 0.
	require.Equal(t, int64(2), stats.Total, "total counts only uncleared rows")
	require.Equal(t, int64(1), stats.Unread, "unread counts only uncleared, unread rows")
	require.Equal(t, int64(0), stats.Dismissed,
		"a dismissed row is also cleared (Dismiss sets cleared_at), so it is excluded from the dismissed counter too")
	require.LessOrEqual(t, stats.Dismissed, stats.Total)
}

// TestGetStats_DismissedIsClearedInvariant documents, on a real database, the
// invariant behind the dismissed counter: dismissing a notification clears it,
// so a dismissed notification is excluded from `total`/`unread` and the
// corrected `dismissed` counter reads 0 for it. `Dismiss` is the only API path
// that sets `dismissed = true`, and it sets `cleared_at` in the same statement,
// so the counter is 0 by construction for rows dismissed through the API. The
// counter is retained for schema/API compatibility; removing or repurposing it
// is a follow-up.
//
// `Repository.Restore` resets `dismissed`/`dismissed_at` together with
// `cleared_at`, so a dismissed-then-restored row re-enters `total`/`unread` as a
// live row and is not counted as dismissed; see
// TestRestoreUnDismisses_StatsAndRow.
func TestGetStats_DismissedIsClearedInvariant(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "notif_stats_invariant")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	userID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User")
	require.NoError(t, err)

	repo := NewRepository(db, slog.Default())
	n, err := repo.Create(ctx, &Notification{UserID: userID, Title: "t", Message: "m"})
	require.NoError(t, err)

	require.NoError(t, repo.Dismiss(ctx, userID, n.ID))

	stats, err := repo.GetStats(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(0), stats.Total, "a dismissed notification is cleared, so it is not in total")
	require.Equal(t, int64(0), stats.Unread, "a dismissed notification is cleared, so it is not unread")
	require.Equal(t, int64(0), stats.Dismissed,
		"the dismissed counter filters cleared_at IS NULL, so a dismissed row counts 0 by construction")
}

// TestRestoreUnDismisses_StatsAndRow is the DB-backed regression for the
// dismiss -> restore lifecycle gap. Before the fix Restore only reset
// `cleared_at`, so a dismissed-then-restored row re-entered `total`/`unread`
// while still carrying `dismissed = true` and was counted by the dismissed
// counter (total=1, unread=1, dismissed=1). A restored row must be neither
// cleared nor dismissed.
//
// It also pins the row-level state and the read semantics: Restore must leave
// `read` untouched, so a restored notification is `unread` only if it was
// unread when dismissed.
func TestRestoreUnDismisses_StatsAndRow(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "notif_restore")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	userID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User")
	require.NoError(t, err)

	repo := NewRepository(db, slog.Default())
	n, err := repo.Create(ctx, &Notification{UserID: userID, Title: "t", Message: "m"})
	require.NoError(t, err)

	// Dismiss then restore an unread notification.
	require.NoError(t, repo.Dismiss(ctx, userID, n.ID))
	require.NoError(t, repo.Restore(ctx, userID, n.ID))

	// Row level: neither cleared nor dismissed; read was false and is untouched.
	var got Notification
	require.NoError(t, db.NewSelect().Model(&got).Where("id = ?", n.ID).Scan(ctx))
	require.False(t, got.Dismissed, "Restore must reset dismissed to false")
	require.Nil(t, got.DismissedAt, "Restore must reset dismissed_at to NULL")
	require.Nil(t, got.ClearedAt, "Restore must clear cleared_at")
	require.False(t, got.Read, "Restore must not change read")

	// The restored notification is back in the inbox: total, and unread because
	// it was unread; it is not dismissed.
	stats, err := repo.GetStats(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Total, "a restored notification is back in total")
	require.Equal(t, int64(1), stats.Unread, "it was unread, so it is unread again")
	require.Equal(t, int64(0), stats.Dismissed, "a restored notification is not dismissed")

	list, err := repo.List(ctx, userID, ListParams{Tab: TabAll})
	require.NoError(t, err)
	require.Len(t, list, 1, "the restored notification is listed again")
	require.Equal(t, n.ID, list[0].ID)

	// A read notification stays read across dismiss -> restore.
	require.NoError(t, repo.MarkRead(ctx, userID, n.ID))
	require.NoError(t, repo.Dismiss(ctx, userID, n.ID))
	require.NoError(t, repo.Restore(ctx, userID, n.ID))

	require.NoError(t, db.NewSelect().Model(&got).Where("id = ?", n.ID).Scan(ctx))
	require.True(t, got.Read, "Restore must not change read")
	require.False(t, got.Dismissed)
	require.Nil(t, got.DismissedAt)

	stats, err = repo.GetStats(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Total)
	require.Equal(t, int64(0), stats.Unread, "it was read, so it is not unread after restore")
	require.Equal(t, int64(0), stats.Dismissed)
}
