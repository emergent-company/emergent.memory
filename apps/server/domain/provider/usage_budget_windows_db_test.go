package provider

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/budgetwindow"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// seedWindowProject creates a project with both windows configured (a nil
// weekly/monthly cap leaves that window uncapped) and one project_admin member
// with a verified email address, so email-eligible admin lookups resolve.
func seedWindowProject(t *testing.T, ctx context.Context, db bun.IDB, monthlyCap *float64, monthlyThreshold float64, weeklyCap *float64, weeklyThreshold float64) (projectID, userID string) {
	t.Helper()
	userID = uuid.NewString()
	orgID := uuid.NewString()
	projectID = uuid.NewString()

	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Window Admin")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Org "+orgID)
	require.NoError(t, err)
	// Both thresholds are NOT NULL in the schema; passing 0 would silently disable
	// that window's alerts (the #1343 shape), so the tests always pass a real one.
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name, budget_usd, budget_alert_threshold,
		                          budget_weekly_usd, budget_weekly_alert_threshold)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		projectID, orgID, "Project "+projectID,
		monthlyCap, monthlyThreshold, weeklyCap, weeklyThreshold)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, 'project_admin')`,
		projectID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO core.user_emails (id, user_id, email, verified) VALUES (?, ?, ?, true)`,
		uuid.NewString(), userID, "admin-"+userID[:8]+"@example.com")
	require.NoError(t, err)
	return projectID, userID
}

// insertSpendAt records one usage event with an explicit timestamp.
func insertSpendAt(t *testing.T, ctx context.Context, db bun.IDB, projectID string, amount float64, at time.Time) {
	t.Helper()
	var orgID string
	require.NoError(t, db.NewSelect().TableExpr("kb.projects").Column("organization_id").
		Where("id = ?", projectID).Scan(ctx, &orgID))
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.llm_usage_events (project_id, org_id, provider, model, estimated_cost_usd, created_at)
		 VALUES (?, ?, 'openai', 'test-model', ?, ?)`,
		projectID, orgID, amount, at.UTC())
	require.NoError(t, err)
}

// budgetAlertGroupKeys returns the group_keys of a user's budget notifications,
// sorted for a stable assertion.
func budgetAlertGroupKeys(t *testing.T, ctx context.Context, db bun.IDB, userID string) []string {
	t.Helper()
	var keys []string
	err := db.NewSelect().
		TableExpr("kb.notifications").
		ColumnExpr("COALESCE(group_key, '')").
		Where("user_id = ?", userID).
		Where("event_key = ?", "account.usage.budget_alert").
		OrderExpr("group_key").
		Scan(ctx, &keys)
	require.NoError(t, err)
	return keys
}

// TestCheckBudget_WeeklyWindowAlertsIndependently proves the weekly window has
// its own alert level and its own deduplication key: crossing the weekly
// threshold while the month is untouched emits one weekly alert, and the key
// carries the week (not the month) so the two windows never mask each other.
func TestCheckBudget_WeeklyWindowAlertsIndependently(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetweek")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := newBudgetAlertService(db, log)

	// Monthly cap far away; weekly cap $10 at 80% => $8 threshold.
	projectID, userID := seedWindowProject(t, ctx, db, f64(1000), 0.80, f64(10), 0.80)
	insertSpendAt(t, ctx, db, projectID, 9, time.Now())

	svc.checkBudget(ctx, projectID)
	svc.checkBudget(ctx, projectID) // coalesced

	keys := budgetAlertGroupKeys(t, ctx, db, userID)
	require.Len(t, keys, 1, "only the weekly window crossed a level: %v", keys)
	require.True(t, strings.HasPrefix(keys[0], "budget-week-alert-"+projectID+"-"),
		"weekly warning key = %q", keys[0])
}

// TestCheckBudget_BothWindowsExhaustedEmitOneAlertEach proves the two windows
// produce distinct exhaustion alerts (a weekly and a monthly key), so the
// notification list shows which window blocked the run rather than a single
// ambiguous "budget exceeded".
func TestCheckBudget_BothWindowsExhaustedEmitOneAlertEach(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetboth")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := newBudgetAlertService(db, log)

	projectID, userID := seedWindowProject(t, ctx, db, f64(5), 0.80, f64(5), 0.80)
	insertSpendAt(t, ctx, db, projectID, 6, time.Now())

	svc.checkBudget(ctx, projectID)
	svc.checkBudget(ctx, projectID)

	keys := budgetAlertGroupKeys(t, ctx, db, userID)
	require.Len(t, keys, 2, "one alert per exhausted window: %v", keys)
	require.True(t, strings.HasPrefix(keys[0], "budget-exceeded-"+projectID+"-"), "monthly key = %q", keys[0])
	require.True(t, strings.HasPrefix(keys[1], "budget-week-exceeded-"+projectID+"-"), "weekly key = %q", keys[1])
}

// TestCheckBudget_ThresholdOneCollapsesToExhaustion proves a threshold of 1.0
// does not send two identical letters at the cap: at 100% the window is
// exhausted, and the warning level is skipped because it is the same instant
// and the same figures.
func TestCheckBudget_ThresholdOneCollapsesToExhaustion(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetone")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := newBudgetAlertService(db, log)

	// Monthly inactive (huge cap); weekly cap $5 at 100% with $5 spent.
	projectID, userID := seedWindowProject(t, ctx, db, f64(1000), 0.80, f64(5), 1.0)
	insertSpendAt(t, ctx, db, projectID, 5, time.Now())

	svc.checkBudget(ctx, projectID)

	keys := budgetAlertGroupKeys(t, ctx, db, userID)
	require.Equal(t, []string{"budget-week-exceeded-" + projectID + "-" + budgetwindow.PeriodFor(time.Now(), nil, budgetwindow.Week).Key()}, keys)
}

// TestCheckBudget_MonthlyKeyKeepsLegacyFormat is the rollout guard: existing
// production notifications use budget-alert-{project}-{YYYY-MM}, so an
// unanchored (calendar) month must keep that exact key or every project already
// over its threshold is re-alerted on deploy.
func TestCheckBudget_MonthlyKeyKeepsLegacyFormat(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetlegacy")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := newBudgetAlertService(db, log)

	// Weekly uncapped; monthly $10 at 80% with $9 spent.
	projectID, userID := seedWindowProject(t, ctx, db, f64(10), 0.80, nil, 0.80)
	insertSpendAt(t, ctx, db, projectID, 9, time.Now())

	svc.checkBudget(ctx, projectID)

	now := time.Now().UTC()
	keys := budgetAlertGroupKeys(t, ctx, db, userID)
	require.Equal(t, []string{"budget-alert-" + projectID + "-" + now.Format("2006-01")}, keys)
}

// TestGetBudgetState_ComputesBothWindows covers the read model: percent,
// remaining, status and period boundaries for both windows, plus the "no cap"
// shape (no remaining, status ok, period still reported so the UI can say when
// it would roll).
func TestGetBudgetState_ComputesBothWindows(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetstate")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := newBudgetAlertService(db, log)

	projectID, _ := seedWindowProject(t, ctx, db, f64(10), 0.80, nil, 0.80)
	insertSpendAt(t, ctx, db, projectID, 9, time.Now())

	state, err := svc.GetBudgetState(ctx, projectID)
	require.NoError(t, err)

	require.NotNil(t, state.Month)
	require.NotNil(t, state.Month.CapUSD)
	require.Equal(t, 10.0, *state.Month.CapUSD)
	require.InDelta(t, 9.0, state.Month.SpendUSD, 1e-9)
	require.InDelta(t, 1.0, *state.Month.RemainingUSD, 1e-9)
	require.InDelta(t, 90.0, state.Month.PercentUsed, 1e-9)
	require.Equal(t, budgetStatusWarning, state.Month.Status)

	// The week is uncapped: it keeps its period but reports no cap/remaining.
	require.NotNil(t, state.Week)
	require.Nil(t, state.Week.CapUSD)
	require.Nil(t, state.Week.RemainingUSD)
	require.Equal(t, budgetStatusOK, state.Week.Status)
	require.False(t, state.Week.PeriodEnd.IsZero())

	exceeded, err := svc.CheckBudgetExceeded(ctx, projectID)
	require.NoError(t, err)
	require.False(t, exceeded, "a warning is not exhaustion")

	// Push the month over its cap.
	insertSpendAt(t, ctx, db, projectID, 2, time.Now())
	exceeded, err = svc.CheckBudgetExceeded(ctx, projectID)
	require.NoError(t, err)
	require.True(t, exceeded)
}

// TestGetBudgetState_RemainingFloorsAtZero proves an over-spend reports
// "$0 left" rather than a negative figure, while the percentage keeps growing
// (which is the useful signal).
func TestGetBudgetState_RemainingFloorsAtZero(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetover")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := newBudgetAlertService(db, log)

	projectID, _ := seedWindowProject(t, ctx, db, f64(5), 0.80, nil, 0.80)
	insertSpendAt(t, ctx, db, projectID, 12.5, time.Now())

	state, err := svc.GetBudgetState(ctx, projectID)
	require.NoError(t, err)
	require.Zero(t, *state.Month.RemainingUSD)
	require.InDelta(t, 250.0, state.Month.PercentUsed, 1e-9)
	require.Equal(t, budgetStatusExhausted, state.Month.Status)
}

// TestGetProjectWindowSpend_HonoursWindowStarts proves the single-round-trip
// aggregate buckets by the periods it is given, not by NOW(): a spend before
// the (anchored) week start counts toward the month and not the week.
func TestGetProjectWindowSpend_HonoursWindowStarts(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetspend")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(db, log)

	projectID, _ := seedWindowProject(t, ctx, db, f64(10), 0.80, nil, 0.80)

	now := time.Now().UTC().Truncate(time.Second)
	weekStart := now // pretend the week was reset a moment ago
	monthStart := budgetwindow.PeriodFor(now, nil, budgetwindow.Month).Start
	// Before the anchored week start but inside the month: counted in the month
	// only. (Skipped on the 1st of a month, when the two starts coincide.)
	if weekStart.After(monthStart.Add(time.Minute)) {
		insertSpendAt(t, ctx, db, projectID, 4, weekStart.Add(-time.Hour))
	}
	insertSpendAt(t, ctx, db, projectID, 2, now)

	week, month, err := repo.GetProjectWindowSpend(ctx, projectID, weekStart, monthStart)
	require.NoError(t, err)
	require.InDelta(t, 2.0, week, 1e-9, "spend before the anchored week start is not this week's")
	if weekStart.After(monthStart.Add(time.Minute)) {
		require.InDelta(t, 6.0, month, 1e-9, "the month keeps the earlier spend")
	} else {
		require.InDelta(t, 2.0, month, 1e-9)
	}
}

// TestBudgetAlertGroupKey_Shapes pins the deduplication keys, including the
// legacy monthly format and the anchored-month variant.
func TestBudgetAlertGroupKey_Shapes(t *testing.T) {
	projectID := "p1"
	weekStart := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	monthStart := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	week := &BudgetWindowState{Window: budgetwindow.Week, PeriodStart: weekStart, PeriodEnd: weekStart.AddDate(0, 0, 7)}
	month := &BudgetWindowState{Window: budgetwindow.Month, PeriodStart: monthStart, PeriodEnd: monthStart.AddDate(0, 1, 0)}

	require.Equal(t, "budget-week-alert-p1-2026-11-09", budgetAlertGroupKey(projectID, &budgetConfig{}, week, false))
	require.Equal(t, "budget-week-exceeded-p1-2026-11-09", budgetAlertGroupKey(projectID, &budgetConfig{}, week, true))
	// Calendar month keeps the pre-existing YYYY-MM key.
	require.Equal(t, "budget-alert-p1-2026-11", budgetAlertGroupKey(projectID, &budgetConfig{}, month, false))
	require.Equal(t, "budget-exceeded-p1-2026-11", budgetAlertGroupKey(projectID, &budgetConfig{}, month, true))
	// An anchored month uses the anchor date, so a reset can alert again in the
	// same calendar month.
	anchor := monthStart
	require.Equal(t, "budget-alert-p1-2026-11-01", budgetAlertGroupKey(projectID, &budgetConfig{MonthAnchor: &anchor}, month, false))
}
