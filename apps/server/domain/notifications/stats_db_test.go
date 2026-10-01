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
// Dismiss sets dismissed=true *and* cleared_at; a row that is both dismissed and
// cleared must not count toward `dismissed`, because its siblings total/unread
// both filter `cleared_at IS NULL`. Before the fix the dismissed counter ignored
// cleared_at, so it could exceed total for a user with dismissed-and-cleared
// rows.
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

	insert := func(read, dismissed, cleared bool) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO kb.notifications (id, user_id, title, message, read, dismissed, cleared_at)
			VALUES (?, ?, 't', 'm', ?, ?, CASE WHEN ? THEN now() ELSE NULL END)`,
			uuid.NewString(), userID, read, dismissed, cleared)
		require.NoError(t, err)
	}

	insert(false, false, false) // active unread          -> total, unread
	insert(true, false, false)  // active read            -> total
	insert(false, true, false)  // dismissed, not cleared -> total, dismissed
	insert(false, true, true)   // dismissed AND cleared  -> neither
	insert(true, true, true)    // dismissed AND cleared  -> neither

	stats, err := NewRepository(db, slog.Default()).GetStats(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.Total, "total excludes cleared rows")
	require.Equal(t, int64(1), stats.Unread)
	require.Equal(t, int64(1), stats.Dismissed, "dismissed must exclude cleared rows")
	require.LessOrEqual(t, stats.Dismissed, stats.Total)
}
