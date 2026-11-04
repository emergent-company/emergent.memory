package agents

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

func newShareStoreRepo(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return NewRepository(bun.NewDB(sqldb, pgdialect.New())), mock
}

func expectShareJoinRow(mock sqlmock.Sqlmock, sessionID string) {
	mock.ExpectQuery(`SELECT[\s\S]*FROM "kb"\."agent_share_sessions"[\s\S]*WHERE \(id = [\s\S]*share_link_id = [\s\S]*end_user_ref = `).
		WillReturnRows(sqlmock.NewRows([]string{"id", "share_link_id", "session_id", "end_user_ref", "is_archived"}).
			AddRow("share-1", "link-1", sessionID, "eu-1", false))
}

// expectReclaimLock pins the per-conversation advisory lock the reclaim takes
// before deciding: without it two concurrent deletes of the last references can
// each miss the other's uncommitted join row.
func expectReclaimLock(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
}

// TestDeleteShareSession_LastReferenceReclaimsConversation pins the retention
// decision from #1891: deleting the end user's only share reference to a
// conversation deletes the join row AND reclaims the backing session and its run
// history, rather than stranding them.
func TestDeleteShareSession_LastReferenceReclaimsConversation(t *testing.T) {
	repo, mock := newShareStoreRepo(t)

	mock.ExpectBegin()
	expectShareJoinRow(mock, "sess-1")
	expectReclaimLock(mock)
	mock.ExpectExec(`DELETE FROM "kb"\."agent_share_sessions"[\s\S]*WHERE \(id = [\s\S]*end_user_ref = `).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "kb"\."agent_share_sessions"[\s\S]*session_id = `).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`DELETE FROM "kb"\."agent_runs"[\s\S]*session_id = `).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(`DELETE FROM "kb"\."sessions"[\s\S]*WHERE \(id = `).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	deleted, err := repo.DeleteShareSession(context.Background(), "share-1", "link-1", "eu-1")
	require.NoError(t, err)
	require.True(t, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestDeleteShareSession_OtherReferenceKeepsConversation asserts the sharing
// guard: when the conversation is still referenced by another share link, only
// the caller's join row goes away — the session and its runs survive.
func TestDeleteShareSession_OtherReferenceKeepsConversation(t *testing.T) {
	repo, mock := newShareStoreRepo(t)

	mock.ExpectBegin()
	expectShareJoinRow(mock, "sess-1")
	expectReclaimLock(mock)
	mock.ExpectExec(`DELETE FROM "kb"\."agent_share_sessions"[\s\S]*WHERE \(id = [\s\S]*end_user_ref = `).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "kb"\."agent_share_sessions"[\s\S]*session_id = `).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectCommit()

	deleted, err := repo.DeleteShareSession(context.Background(), "share-1", "link-1", "eu-1")
	require.NoError(t, err)
	require.True(t, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestDeleteShareSession_NoOwnedRowIsNoOp asserts a caller who owns no such
// session deletes nothing and reports false (the handler maps it to 404).
func TestDeleteShareSession_NoOwnedRowIsNoOp(t *testing.T) {
	repo, mock := newShareStoreRepo(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT[\s\S]*FROM "kb"\."agent_share_sessions"[\s\S]*`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()

	deleted, err := repo.DeleteShareSession(context.Background(), "share-x", "link-1", "eu-1")
	require.NoError(t, err)
	require.False(t, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}
