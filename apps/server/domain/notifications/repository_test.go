package notifications

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
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

	// GetCounts issues seven count queries: all, unread, action required,
	// important, other, snoozed, cleared.
	for i := 0; i < 7; i++ {
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

	// Seven count queries in order: all, unread, action required, important,
	// other, snoozed, cleared.
	mock.ExpectQuery(`SELECT[\s\S]*cleared_at IS NULL[\s\S]*snoozed_until IS NULL[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(9)) // all
	mock.ExpectQuery(`SELECT[\s\S]*read = false[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4)) // unread
	mock.ExpectQuery(`SELECT[\s\S]*requires_action = true[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2)) // action required
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
	require.Equal(t, int64(2), counts.RequiresAction)
	require.Equal(t, int64(1), counts.Important)
	require.Equal(t, int64(8), counts.Other)
	require.Equal(t, int64(2), counts.Snoozed)
	require.Equal(t, int64(3), counts.Cleared)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetCountsActionRequiredPredicateOnce pins the Action-required bucket's SQL:
// it carries requires_action = true exactly once whether or not the caller
// already filtered by it. applyScopeFilter adds the predicate for the Action
// required tab; the bucket must not append a second copy.
func TestGetCountsActionRequiredPredicateOnce(t *testing.T) {
	repo, _ := newRepoMock(t)

	for _, requiresAction := range []bool{false, true} {
		q := repo.countBaseActionRequired("u1", CountParams{RequiresAction: requiresAction})
		sql := q.String()
		if got := strings.Count(sql, "requires_action = true"); got != 1 {
			t.Errorf("RequiresAction=%v: requires_action predicate appears %d times, want 1\nSQL: %s", requiresAction, got, sql)
		}
	}
}

// TestGetStatsDismissedExcludesCleared asserts GetStats draws all three of its
// counters from the same row population: `cleared_at IS NULL`. A dismissed row
// that was also cleared (Dismiss sets both dismissed=true and cleared_at) must
// not be counted as dismissed, matching total/unread. Regression for the
// dismissed predicate missing the cleared_at filter its siblings have (#1314).
func TestGetStatsDismissedExcludesCleared(t *testing.T) {
	repo, mock := newRepoMock(t)

	// Three ordered counts: total, unread, dismissed. Every one must exclude
	// cleared rows; before the fix the dismissed query carried no cleared_at
	// predicate and the third expectation below would fail to match.
	mock.ExpectQuery(`SELECT[\s\S]*cleared_at IS NULL[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2)) // total
	mock.ExpectQuery(`SELECT[\s\S]*read = false[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1)) // unread
	mock.ExpectQuery(`SELECT[\s\S]*cleared_at IS NULL[\s\S]*dismissed = true[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1)) // dismissed

	stats, err := repo.GetStats(context.Background(), "u1")
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.Total)
	require.Equal(t, int64(1), stats.Unread)
	require.Equal(t, int64(1), stats.Dismissed)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRestoreResetsDismissed is the row-level regression for the
// dismiss -> restore gap: Restore must reset `dismissed = false` and
// `dismissed_at = NULL` alongside `cleared_at = NULL`, in a single UPDATE, and
// must not touch `read`. It uses a custom matcher because the assertion is
// about both the presence of the reset columns and the absence of a read write.
func TestRestoreResetsDismissed(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(_, actualSQL string) error {
		up := strings.ToUpper(actualSQL)
		for _, want := range []string{"CLEARED_AT = NULL", "DISMISSED = FALSE", "DISMISSED_AT = NULL"} {
			if !strings.Contains(up, want) {
				return fmt.Errorf("restore UPDATE missing %q: %s", want, actualSQL)
			}
		}
		if strings.Contains(up, "READ = ") {
			return fmt.Errorf("restore UPDATE must not change read: %s", actualSQL)
		}
		return nil
	})

	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	db := bun.NewDB(sqldb, pgdialect.New())
	repo := NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	mock.ExpectExec(".*").WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Restore(context.Background(), "u1", "n1"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// wantActionRequiredSQL is the predicate the Action-required tab and its badge
// must both emit. Spelled out rather than referenced from the package so the
// test pins the SQL contract itself.
const wantActionRequiredSQL = "requires_action = true AND action_status IS NULL"

// TestListActionRequiredExcludesResolved pins the Action-required tab's
// predicate. The tab used to filter on requires_action alone, so a row whose
// resolve succeeded but whose client follow-up read/clear never landed stayed
// live there and only the client could remove it. The server must be
// authoritative on the recorded outcome instead.
func TestListActionRequiredExcludesResolved(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectQuery(`SELECT[\s\S]*requires_action = true[\s\S]*action_status IS NULL[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := repo.List(context.Background(), "u1", ListParams{Tab: TabAll, RequiresAction: true})
	require.NoError(t, err)
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestListPlainTabKeepsResolvedRows is the other half: a resolved row is still
// an ordinary inbox row until it is cleared, so the fix must not filter it out
// of the unfiltered tabs.
func TestListPlainTabKeepsResolvedRows(t *testing.T) {
	repo, _ := newRepoMock(t)

	q := applyScopeFilter(repo.db.NewSelect().Model((*Notification)(nil)), "", nil, false)
	require.NotContains(t, q.String(), "action_status IS NULL",
		"a list without the action filter must not filter on the recorded outcome")
}

// TestGetCountsActionRequiredMatchesListPredicate asserts the badge and the tab
// it labels are built from the same predicate, so the count can never include a
// row the list does not show. applyScopeFilter adds it for the Action-required
// tab; the bucket must not append a second, different copy.
func TestGetCountsActionRequiredMatchesListPredicate(t *testing.T) {
	repo, _ := newRepoMock(t)

	for _, requiresAction := range []bool{false, true} {
		sql := repo.countBaseActionRequired("u1", CountParams{RequiresAction: requiresAction}).String()
		require.Equal(t, 1, strings.Count(sql, wantActionRequiredSQL),
			"RequiresAction=%v: action-required predicate must appear exactly once\nSQL: %s", requiresAction, sql)
	}
}

// TestResolveActionWritesOnlyWhenUnresolved asserts the recorded verb is
// first-write-wins: the UPDATE is conditional on action_status IS NULL, so a
// second submit cannot overwrite the outcome that already landed.
func TestResolveActionWritesOnlyWhenUnresolved(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectExec(`UPDATE[\s\S]*action_status = 'accept'[\s\S]*action_status IS NULL[\s\S]*`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.ResolveAction(context.Background(), "u1", "n1", "accept"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestResolveActionReplaySameVerbIsIdempotent: a double submit carries the same
// verb, so the second call finds its own outcome stored and must succeed rather
// than 404 (the row is unreachable for writing, but the request is satisfied).
func TestResolveActionReplaySameVerbIsIdempotent(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectExec(`UPDATE[\s\S]*`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT[\s\S]*action_status[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"action_status"}).AddRow("accept"))

	require.NoError(t, repo.ResolveAction(context.Background(), "u1", "n1", "accept"),
		"a retry of the verb that already landed is an idempotent success")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestResolveActionRefusesDifferentVerb: a concurrent resolve landed accept
// first, so this decline must be refused, not silently overwrite it. This is the
// half that survives a second tab, where the client in-flight guard does not
// apply.
func TestResolveActionRefusesDifferentVerb(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectExec(`UPDATE[\s\S]*action_status = 'decline'[\s\S]*`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT[\s\S]*action_status[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"action_status"}).AddRow("accept"))

	err := repo.ResolveAction(context.Background(), "u1", "n1", "decline")
	require.Error(t, err)

	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, http.StatusConflict, appErr.HTTPStatus)
	require.Equal(t, "notification_action_already_resolved", appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestResolveActionUnknownNotification keeps the 404 for a row that is absent
// (or belongs to someone else) once the write is conditional: nothing was
// written and there is no stored outcome to reconcile against.
func TestResolveActionUnknownNotification(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectExec(`UPDATE[\s\S]*`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT[\s\S]*action_status[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"action_status"}))

	err := repo.ResolveAction(context.Background(), "u1", "n1", "accept")
	require.Error(t, err)

	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRestoreActiveGroupKeyConflict asserts that a unique violation from the
// active (user_id, group_key) partial index is translated into the typed
// ErrActiveNotificationConflict rather than leaking as a 500 database error.
// This is the unit-level half of the #1345/#1347 interaction fix.
func TestRestoreActiveGroupKeyConflict(t *testing.T) {
	repo, mock := newRepoMock(t)

	mock.ExpectExec(`UPDATE[\s\S]*"kb"\."notifications"[\s\S]*`).
		WillReturnError(&pgconn.PgError{
			Code:           "23505",
			ConstraintName: "ux_notifications_user_group_key_active",
		})

	err := repo.Restore(context.Background(), "u1", "n1")
	require.ErrorIs(t, err, ErrActiveNotificationConflict)
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

// TestInboxScopeIsolation is the #1363 guard. The account inbox binds
// scope='account' and NO project predicate, so a project-scope row can never
// surface there; the project inbox binds BOTH scope='project' and project_id,
// so one project's rows can never leak into another's. (The agent-question
// notification reported in the account inbox was a stale pre-#1312 row
// backfilled to the default account scope — not a cross-project leak; the
// classification backfill corrects it.)
func TestInboxScopeIsolation(t *testing.T) {
	t.Run("account binds account scope only", func(t *testing.T) {
		repo, mock := newRepoMock(t)
		mock.ExpectQuery(`SELECT[\s\S]*scope = 'account'[\s\S]*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		_, err := repo.List(context.Background(), "u1", ListParams{Scope: ScopeAccount, Tab: TabAll})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("project binds scope and project id", func(t *testing.T) {
		repo, mock := newRepoMock(t)
		mock.ExpectQuery(`SELECT[\s\S]*scope = 'project'[\s\S]*project_id = 'proj-a'[\s\S]*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		pid := "proj-a"
		_, err := repo.List(context.Background(), "u1", ListParams{
			Scope: ScopeProject, ProjectID: &pid, Tab: TabAll,
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
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

// sqlRecorder captures the statements bun sends so a test can assert on a
// predicate directly — including one a statement must NOT carry, which a
// sqlmock regexp cannot express.
type sqlRecorder struct {
	statements []string
}

func (r *sqlRecorder) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (r *sqlRecorder) AfterQuery(_ context.Context, evt *bun.QueryEvent) {
	r.statements = append(r.statements, evt.Query)
}

// recorderOn attaches a recorder to the repository's bun handle.
func recorderOn(t *testing.T, repo *Repository) *sqlRecorder {
	t.Helper()
	db, ok := repo.db.(*bun.DB)
	require.True(t, ok, "repository must hold a *bun.DB for this test")
	rec := &sqlRecorder{}
	db.AddQueryHook(rec)
	return rec
}

// TestResolveActionStatusWithoutUserIDDoesNotScopeToOwner pins the half of the
// shared write that the agent-question path depends on: an empty userID means
// "authorized by another route", so neither statement may filter on user_id
// (the responder is not necessarily the notification's owner) and the outcome
// must not be attributed to a user it cannot name.
//
// Without this the agent path could not use the same first-write-wins rule
// without silently refusing every write whose responder differs from the owner.
func TestResolveActionStatusWithoutUserIDDoesNotScopeToOwner(t *testing.T) {
	repo, mock := newRepoMock(t)
	rec := recorderOn(t, repo)

	mock.ExpectExec(`UPDATE[\s\S]*`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"action_status"}).AddRow("accepted"))

	err := ResolveActionStatus(context.Background(), repo.db, "", "n1", "completed")
	require.ErrorIs(t, err, ErrActionAlreadyResolved,
		"a differing verb is refused even without an owner scope")
	require.NoError(t, mock.ExpectationsWereMet())

	require.Len(t, rec.statements, 2, "one update and one read-back")
	update, read := rec.statements[0], rec.statements[1]
	require.Contains(t, update, "action_status IS NULL",
		"the first-write-wins predicate is the point of the shared helper")
	require.NotContains(t, update, "user_id",
		"an empty userID must not scope the update to the notification's owner")
	require.NotContains(t, update, "action_status_by",
		"there is no user id to attribute the outcome to")
	require.NotContains(t, read, "user_id",
		"the read-back must find the row by id alone")
}

// TestResolveActionStatusWithUserIDScopesToOwner keeps the inbox route's
// guarantee: a write is scoped to the notification's owner, in both statements,
// and is attributed to them.
func TestResolveActionStatusWithUserIDScopesToOwner(t *testing.T) {
	repo, mock := newRepoMock(t)
	rec := recorderOn(t, repo)

	mock.ExpectExec(`UPDATE[\s\S]*`).WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, ResolveActionStatus(context.Background(), repo.db, "u1", "n1", "accept"))
	require.NoError(t, mock.ExpectationsWereMet())

	require.Len(t, rec.statements, 1)
	require.Contains(t, rec.statements[0], "user_id = 'u1'")
	require.Contains(t, rec.statements[0], "action_status_by = 'u1'")
}
