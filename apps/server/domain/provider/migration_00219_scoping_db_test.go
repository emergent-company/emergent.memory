package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/migrations"
)

// migration00219Up reads the real embedded 00219 migration and returns just the
// Up SQL with goose annotations stripped, so this test exercises the exact
// shipped statement rather than a paraphrase.
func migration00219Up(t *testing.T) string {
	t.Helper()
	b, err := migrations.FS.ReadFile("00219_clear_stale_budget_alerts.sql")
	require.NoError(t, err)
	up := strings.SplitN(string(b), "-- +goose Down", 2)[0]
	up = strings.ReplaceAll(up, "-- +goose StatementBegin", "")
	up = strings.ReplaceAll(up, "-- +goose StatementEnd", "")
	up = strings.ReplaceAll(up, "-- +goose Up", "")
	return up
}

func seedNotifOrgProject(t *testing.T, ctx context.Context, db *testdb.TestDB, name string, budget *float64, threshold float64) string {
	t.Helper()
	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Org "+orgID)
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name, budget_usd, budget_alert_threshold) VALUES (?, ?, ?, ?, ?)`,
		projectID, orgID, name, budget, threshold)
	require.NoError(t, err)
	return projectID
}

type notifRow struct {
	ClearedAt *time.Time `bun:"cleared_at"`
	Read      bool       `bun:"read"`
	ReadAt    *time.Time `bun:"read_at"`
}

func insertNotif(t *testing.T, ctx context.Context, db *testdb.TestDB, userID, projectID, message, category, eventKey, groupKey string, cleared *time.Time) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.DB.ExecContext(ctx,
		`INSERT INTO kb.notifications (id, user_id, project_id, title, message, category, event_key, group_key, cleared_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, projectID, "Budget alert", message, category, eventKey, groupKey, cleared)
	require.NoError(t, err)
	return id
}

func loadNotif(t *testing.T, ctx context.Context, db *testdb.TestDB, id string) notifRow {
	t.Helper()
	var r notifRow
	err := db.DB.NewRaw(`SELECT cleared_at, read, read_at FROM kb.notifications WHERE id = ?`, id).Scan(ctx, &r)
	require.NoError(t, err)
	return r
}

// TestMigration00219_Scoping is the mutation test for the 00219 scoping
// predicate: only stale non-breach budget alerts may be cleared; genuine
// breaches, non-budget notifications, malformed messages, and configs that
// cannot establish "non-breach" must be left untouched. Idempotent on rerun.
func TestMigration00219_Scoping(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mig00219_scoping")
	t.Cleanup(tdb.Close)

	userID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Notif User")
	require.NoError(t, err)

	budget10 := 10.0
	projA := seedNotifOrgProject(t, ctx, tdb, "A-budget10-thr80", &budget10, 0.80)
	projNoBudget := seedNotifOrgProject(t, ctx, tdb, "B-nobudget", nil, 0.80)
	projZeroThr := seedNotifOrgProject(t, ctx, tdb, "C-thr0", &budget10, 0.0)

	const eventKey = "account.usage.budget_alert"
	staleMsg := "Project has used $0.00 of its $10.00 monthly budget (0%)."
	breachMsg := "Project has used $8.00 of its $10.00 monthly budget (80%)."
	boundaryMsg := "Project has used $7.99 of its $10.00 monthly budget (80%)."

	past := time.Now().Add(-72 * time.Hour).UTC().Truncate(time.Second)

	// Expected cleartrue/false per row.
	nStale := insertNotif(t, ctx, tdb, userID, projA, staleMsg, "budget", eventKey, "gk-stale", nil)
	nBreach := insertNotif(t, ctx, tdb, userID, projA, breachMsg, "budget", eventKey, "gk-breach", nil)
	nBoundary := insertNotif(t, ctx, tdb, userID, projA, boundaryMsg, "budget", eventKey, "gk-boundary", nil)
	nBadFormat := insertNotif(t, ctx, tdb, userID, projA, "Budget exceeded for project", "budget", eventKey, "gk-badformat", nil)
	nNonBudget := insertNotif(t, ctx, tdb, userID, projA, staleMsg, "membership", "project.member.added", "gk-nonbudget", nil)
	nNoBudget := insertNotif(t, ctx, tdb, userID, projNoBudget, staleMsg, "budget", eventKey, "gk-nobudget", nil)
	nZeroThr := insertNotif(t, ctx, tdb, userID, projZeroThr, staleMsg, "budget", eventKey, "gk-zerothr", nil)
	nAlready := insertNotif(t, ctx, tdb, userID, projA, staleMsg, "budget", eventKey, "gk-already", &past)

	up := migration00219Up(t)
	_, err = tdb.DB.ExecContext(ctx, up)
	require.NoError(t, err)

	// Rerun: idempotency; counts and timestamps must not move.
	_, err = tdb.DB.ExecContext(ctx, up)
	require.NoError(t, err)

	after := func(id string) notifRow { return loadNotif(t, ctx, tdb, id) }

	for _, id := range []string{nStale, nBoundary} {
		r := after(id)
		require.NotNil(t, r.ClearedAt, "row %s should be cleared (non-breach)", id)
		require.True(t, r.Read, "cleared row %s should be marked read", id)
		require.NotNil(t, r.ReadAt, "cleared row %s should set read_at", id)
	}
	for _, id := range []string{nBreach, nBadFormat, nNonBudget, nNoBudget, nZeroThr} {
		r := after(id)
		require.Nil(t, r.ClearedAt, "row %s must be left untouched", id)
		require.False(t, r.Read, "row %s must remain unread", id)
	}
	// Pre-cleared row: stays cleared, read_at untouched (idempotent no-op).
	r := after(nAlready)
	require.NotNil(t, r.ClearedAt, "already-cleared row stays cleared")
	require.WithinDuration(t, past, *r.ClearedAt, time.Second, "already-cleared timestamp must not change")
}
