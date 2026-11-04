package agents

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// TestCreateShareSessionIfUnderCap_CappedInsertsNothing pins #1911: when the
// per-(link, end user) active-session cap is reached, the transaction inserts
// NEITHER the backing kb.sessions row NOR the share-session join row. sqlmock
// fails the test on any unexpected query/exec, so leaving the kb.sessions insert
// before the cap check — the old orphan bug — regresses this test.
func TestCreateShareSessionIfUnderCap_CappedInsertsNothing(t *testing.T) {
	repo, mock := newShareStoreRepo(t)

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM kb\.agent_share_sessions[\s\S]*share_link_id = [\s\S]*end_user_ref = `).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectCommit()

	created, err := repo.CreateShareSessionIfUnderCap(
		context.Background(),
		&Session{ProjectID: "proj-a", AgentName: testStrPtr("Agent A")},
		&AgentShareSession{ShareLinkID: "link-a", EndUserRef: "eu-1"},
		5,
	)
	require.NoError(t, err)
	require.False(t, created)
	require.NoError(t, mock.ExpectationsWereMet(),
		"a capped request must insert neither kb.sessions nor kb.agent_share_sessions")
}

// TestCreateShareSessionIfUnderCap_CreatesBothRowsWhenUnderCap pins the happy
// path: under the cap, the backing session is created (RETURNING the generated
// id) and the join row links it, in the same transaction.
func TestCreateShareSessionIfUnderCap_CreatesBothRowsWhenUnderCap(t *testing.T) {
	repo, mock := newShareStoreRepo(t)

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM kb\.agent_share_sessions`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`INSERT INTO "kb"\."sessions"[\s\S]*RETURNING id, project_id, agent_name, title, created_at, updated_at`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "agent_name", "title", "created_at", "updated_at"}).
			AddRow("sess-1", "proj-a", "Agent A", nil, nil, nil))
	mock.ExpectQuery(`INSERT INTO "kb"\."agent_share_sessions"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "last_activity_at", "is_archived", "created_at"}).
			AddRow("share-1", nil, nil, false, nil))
	mock.ExpectCommit()

	session := &Session{ProjectID: "proj-a", AgentName: testStrPtr("Agent A")}
	sess := &AgentShareSession{ShareLinkID: "link-a", EndUserRef: "eu-1"}
	created, err := repo.CreateShareSessionIfUnderCap(context.Background(), session, sess, 5)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "sess-1", sess.SessionID, "the join row must link the created session")
	require.NoError(t, mock.ExpectationsWereMet())
}
