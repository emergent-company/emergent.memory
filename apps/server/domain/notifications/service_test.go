package notifications

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
)

func newTestService(t *testing.T) (*Service, sqlmock.Sqlmock, *events.Service) {
	t.Helper()
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	db := bun.NewDB(sqldb, pgdialect.New())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(db, log)
	ev := events.NewService(log)
	return NewService(repo, ev, log), mock, ev
}

// expectInsert mocks the bun INSERT (with RETURNING of default columns) and
// returns the given id.
func expectInsert(mock sqlmock.Sqlmock, id string) {
	mock.ExpectQuery(regexp.QuoteMeta("INSERT") + `[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "read", "dismissed", "actions", "created_at", "updated_at", "requires_action"}).
			AddRow(id, false, false, "[]", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z", false))
}

// expectPreference mocks a GetPreference lookup. When enabled is nil, no row
// is returned (no stored preference).
func expectPreference(mock sqlmock.Sqlmock, enabled *bool) {
	rows := sqlmock.NewRows([]string{"id", "user_id", "project_id", "event_key", "channel", "enabled", "created_at", "updated_at"})
	if enabled != nil {
		rows.AddRow("p1", "u1", "prj", "comment.reply", "in_app", *enabled, "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT") + `[\s\S]*notification_preferences[\s\S]*`).
		WillReturnRows(rows)
}

// expectGroupKeyExists mocks the group coalescing existence check.
func expectGroupKeyExists(mock sqlmock.Sqlmock, exists bool) {
	mock.ExpectQuery(`SELECT EXISTS[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(exists))
}

func TestCreate_AccountDelivered(t *testing.T) {
	svc, mock, ev := newTestService(t)
	expectInsert(mock, "n1")

	got := make(chan events.EntityEvent, 1)
	unsub := ev.Subscribe("*", func(e events.EntityEvent) { got <- e })
	defer unsub()

	n, err := svc.Create(context.Background(), CreateInput{
		UserID:   "u1",
		EventKey: "project.member.added",
		Title:    "You were added",
		Message:  "Added to project",
	})
	require.NoError(t, err)
	require.NotNil(t, n)
	require.Equal(t, "n1", n.ID)
	require.Equal(t, ScopeAccount, n.Scope)
	require.Equal(t, "project.member.added", *n.EventKey)

	require.NoError(t, mock.ExpectationsWereMet())

	select {
	case e := <-got:
		require.Equal(t, events.EntityNotification, e.Entity)
		require.Equal(t, "n1", *e.ID)
		require.Equal(t, "", e.ProjectID)
	case <-time.After(time.Second):
		t.Fatal("expected notification entity event to be emitted")
	}
}

func TestCreate_ProjectOptInOff_Suppressed(t *testing.T) {
	svc, mock, _ := newTestService(t)
	expectPreference(mock, nil) // no stored preference -> opt-in off

	n, err := svc.Create(context.Background(), CreateInput{
		UserID:    "u1",
		ProjectID: strPtr("prj"),
		Scope:     ScopeProject,
		EventKey:  "comment.reply",
		Title:     "Reply",
		Message:   "Someone replied",
	})
	require.NoError(t, err)
	require.Nil(t, n)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreate_ProjectOptInOn_Delivered(t *testing.T) {
	svc, mock, _ := newTestService(t)
	expectPreference(mock, boolPtr(true))
	expectInsert(mock, "n2")

	n, err := svc.Create(context.Background(), CreateInput{
		UserID:    "u1",
		ProjectID: strPtr("prj"),
		Scope:     ScopeProject,
		EventKey:  "comment.reply",
		Title:     "Reply",
		Message:   "Someone replied",
	})
	require.NoError(t, err)
	require.NotNil(t, n)
	require.Equal(t, "n2", n.ID)
	require.Equal(t, ScopeProject, n.Scope)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreate_RequiredProject_DeliveredDespitePrefs(t *testing.T) {
	// task.assigned is project-scope + required: no preference query is issued.
	svc, mock, _ := newTestService(t)
	expectInsert(mock, "n3")

	n, err := svc.Create(context.Background(), CreateInput{
		UserID:    "u1",
		ProjectID: strPtr("prj"),
		Scope:     ScopeProject,
		EventKey:  "task.assigned",
		Title:     "Task assigned",
		Message:   "You have a task",
	})
	require.NoError(t, err)
	require.NotNil(t, n)
	require.Equal(t, "n3", n.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreate_GroupCoalescing(t *testing.T) {
	svc, mock, _ := newTestService(t)
	expectGroupKeyExists(mock, true)

	gk := "budget-alert-prj-2026-01"
	n, err := svc.Create(context.Background(), CreateInput{
		UserID:   "u1",
		EventKey: "project.member.added",
		Title:    "t",
		Message:  "m",
		GroupKey: &gk,
	})
	require.NoError(t, err)
	require.Nil(t, n)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreate_UnknownEventKey(t *testing.T) {
	svc, _, _ := newTestService(t)

	n, err := svc.Create(context.Background(), CreateInput{
		UserID:   "u1",
		EventKey: "does.not.exist",
		Title:    "t",
		Message:  "m",
	})
	require.Error(t, err)
	require.Nil(t, n)
}

func TestSavePreference_AccountNonSuppressible(t *testing.T) {
	svc, mock, _ := newTestService(t)

	// Account key: no-op, no DB write.
	pref, err := svc.SavePreference(context.Background(), "u1", nil, "project.member.added", "in_app", true)
	require.NoError(t, err)
	require.Nil(t, pref)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSavePreference_Upsert(t *testing.T) {
	svc, mock, _ := newTestService(t)

	mock.ExpectQuery(regexp.QuoteMeta("INSERT") + `[\s\S]*notification_preferences[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "project_id", "event_key", "channel", "enabled", "created_at", "updated_at"}).
			AddRow("p1", "u1", "prj", "comment.reply", "in_app", true, "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z"))

	pref, err := svc.SavePreference(context.Background(), "u1", strPtr("prj"), "comment.reply", "in_app", true)
	require.NoError(t, err)
	require.NotNil(t, pref)
	require.Equal(t, "comment.reply", pref.EventKey)
	require.True(t, pref.Enabled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListEffectivePreferences_MaterializesDefaults(t *testing.T) {
	svc, mock, _ := newTestService(t)

	// Two stored prefs (comment.reply enabled, mention disabled).
	mock.ExpectQuery(regexp.QuoteMeta("SELECT") + `[\s\S]*notification_preferences[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "project_id", "event_key", "channel", "enabled", "created_at", "updated_at"}).
			AddRow("p1", "u1", "prj", "comment.reply", "in_app", true, "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z").
			AddRow("p2", "u1", "prj", "mention", "in_app", false, "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z"))

	entries, err := svc.ListEffectivePreferences(context.Background(), "u1", strPtr("prj"))
	require.NoError(t, err)

	// Every project event key is materialised.
	projKeys := map[string]bool{}
	for _, k := range taxonomy.Keys(taxonomy.ScopeProject) {
		projKeys[k] = true
	}
	require.Len(t, entries, len(projKeys))

	byKey := map[string]PreferenceEntry{}
	for _, e := range entries {
		byKey[e.EventKey] = e
		if _, ok := projKeys[e.EventKey]; !ok {
			t.Fatalf("unexpected key %q in effective prefs", e.EventKey)
		}
		if e.Scope != ScopeProject {
			t.Fatalf("key %q has scope %q, want project", e.EventKey, e.Scope)
		}
	}

	require.True(t, byKey["comment.reply"].Enabled)
	require.False(t, byKey["comment.reply"].Default)
	require.False(t, byKey["mention"].Enabled)
	// A key with no stored row is defaulted to disabled.
	require.False(t, byKey["project.agent_config.changed"].Enabled)
	require.True(t, byKey["project.agent_config.changed"].Default)

	require.NoError(t, mock.ExpectationsWereMet())
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
