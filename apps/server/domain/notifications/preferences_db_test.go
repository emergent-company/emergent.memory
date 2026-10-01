package notifications

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestUpsertPreference_UpdatesNotDuplicates is the DB-backed regression for the
// preferences unique constraint. The app writes preferences via
// `INSERT … ON CONFLICT (user_id, project_id, event_key, channel) DO UPDATE`;
// with a plain UNIQUE constraint and a NULL project_id (the shape the gateway
// used to write) Postgres treats NULLs as distinct, the conflict never fires,
// and every save inserts a duplicate. This test proves a second save updates the
// existing row — for the project-scoped shape the app writes now, and for the
// legacy NULL-project shape the constraint must still cover.
func TestUpsertPreference_UpdatesNotDuplicates(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "notif_prefs")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	userID := uuid.NewString()
	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Org")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "Project")
	require.NoError(t, err)

	repo := NewRepository(db, slog.Default())

	t.Run("project-scoped save updates in place", func(t *testing.T) {
		first, err := repo.UpsertPreference(ctx, &NotificationPreference{
			UserID: userID, ProjectID: &projectID, EventKey: "comment.reply", Channel: "in_app", Enabled: true,
		})
		require.NoError(t, err)
		require.NotEmpty(t, first.ID)

		second, err := repo.UpsertPreference(ctx, &NotificationPreference{
			UserID: userID, ProjectID: &projectID, EventKey: "comment.reply", Channel: "in_app", Enabled: false,
		})
		require.NoError(t, err)
		require.Equal(t, first.ID, second.ID, "second save must update the existing row, not insert a new one")
		require.False(t, second.Enabled)

		require.Equal(t, 1, countPreferences(t, db, `user_id = ? AND project_id = ? AND event_key = ? AND channel = ?`,
			userID, projectID, "comment.reply", "in_app"))
	})

	t.Run("NULL project_id save updates in place", func(t *testing.T) {
		first, err := repo.UpsertPreference(ctx, &NotificationPreference{
			UserID: userID, ProjectID: nil, EventKey: "project.agent_config.changed", Channel: "in_app", Enabled: true,
		})
		require.NoError(t, err)
		require.NotEmpty(t, first.ID)

		second, err := repo.UpsertPreference(ctx, &NotificationPreference{
			UserID: userID, ProjectID: nil, EventKey: "project.agent_config.changed", Channel: "in_app", Enabled: false,
		})
		require.NoError(t, err)
		require.Equal(t, first.ID, second.ID,
			"NULL project_id rows must conflict too; plain UNIQUE treats NULLs as distinct and duplicates every save")

		require.Equal(t, 1, countPreferences(t, db,
			`user_id = ? AND project_id IS NULL AND event_key = ? AND channel = ?`,
			userID, "project.agent_config.changed", "in_app"))
	})
}

// countPreferences returns the number of kb.notification_preferences rows
// matching a WHERE fragment.
func countPreferences(t *testing.T, db bun.IDB, where string, args ...any) int {
	t.Helper()
	var n int
	err := db.NewRaw("SELECT count(*) FROM kb.notification_preferences WHERE "+where, args...).Scan(context.Background(), &n)
	require.NoError(t, err)
	return n
}
