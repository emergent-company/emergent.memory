package notifications

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

func newRepoMock(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	db := bun.NewDB(sqldb, pgdialect.New())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRepository(db, log), mock
}

func TestListAppliesScopeFilter(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectQuery(`SELECT[\s\S]*scope = 'account'[\s\S]*project_id = 'prj'[\s\S]*requires_action = true[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	pid := "prj"
	got, err := repo.List(context.Background(), "u1", ListParams{
		Scope:          ScopeAccount,
		ProjectID:      &pid,
		RequiresAction: true,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListUnscoped(t *testing.T) {
	repo, mock := newRepoMock(t)

	// No scope predicate: the query must not contain "scope =".
	mock.ExpectQuery(`SELECT[\s\S]*FROM "kb"\."notifications"[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err := repo.List(context.Background(), "u1", ListParams{})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCountsAppliesScopeFilter(t *testing.T) {
	repo, mock := newRepoMock(t)

	// GetCounts issues six count queries: all, unread, important, other,
	// snoozed, cleared.
	for i := 0; i < 6; i++ {
		mock.ExpectQuery(`SELECT[\s\S]*scope = 'project'[\s\S]*project_id = 'prj'[\s\S]*`).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	}

	pid := "prj"
	counts, err := repo.GetCounts(context.Background(), "u1", CountParams{
		Scope:     ScopeProject,
		ProjectID: &pid,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), counts.All)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetCountsUnread asserts the unread bucket is a distinct count: unread AND
// not cleared AND not currently snoozed (the bell badge semantics).
func TestGetCountsUnread(t *testing.T) {
	repo, mock := newRepoMock(t)

	// Six count queries in order: all, unread, important, other, snoozed, cleared.
	mock.ExpectQuery(`SELECT[\s\S]*cleared_at IS NULL[\s\S]*snoozed_until IS NULL[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(9)) // all
	mock.ExpectQuery(`SELECT[\s\S]*read = false[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4)) // unread
	mock.ExpectQuery(`SELECT[\s\S]*importance = 'important'[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT[\s\S]*importance = 'other'[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(8))
	mock.ExpectQuery(`SELECT[\s\S]*snoozed_until > [\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT[\s\S]*cleared_at IS NOT NULL[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	counts, err := repo.GetCounts(context.Background(), "u1", CountParams{})
	require.NoError(t, err)
	require.Equal(t, int64(9), counts.All)
	require.Equal(t, int64(4), counts.Unread)
	require.Equal(t, int64(1), counts.Important)
	require.Equal(t, int64(8), counts.Other)
	require.Equal(t, int64(2), counts.Snoozed)
	require.Equal(t, int64(3), counts.Cleared)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkAllRead_AccountScope(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectExec(`UPDATE[\s\S]*scope = 'account'[\s\S]*`).
		WillReturnResult(sqlmock.NewResult(0, 2))

	n, err := repo.MarkAllRead(context.Background(), "u1", ScopeAccount, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), n)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkAllRead_ProjectScope(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectExec(`UPDATE[\s\S]*scope = 'project'[\s\S]*project_id = 'prj'[\s\S]*`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	pid := "prj"
	n, err := repo.MarkAllRead(context.Background(), "u1", ScopeProject, &pid)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpsertPreference_UniqueConflict(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectQuery(`INSERT[\s\S]*notification_preferences[\s\S]*ON CONFLICT[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "project_id", "event_key", "channel", "enabled", "created_at", "updated_at"}).
			AddRow("p1", "u1", "prj", "comment.reply", "in_app", true, "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z"))

	pid := "prj"
	p, err := repo.UpsertPreference(context.Background(), &NotificationPreference{
		UserID:    "u1",
		ProjectID: &pid,
		EventKey:  "comment.reply",
		Channel:   "in_app",
		Enabled:   true,
	})
	require.NoError(t, err)
	require.Equal(t, "p1", p.ID)
	require.True(t, p.Enabled)
	require.NoError(t, mock.ExpectationsWereMet())
}
