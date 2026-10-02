package provider

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// newBudgetAlertService builds a UsageService wired to the given database and
// the real notifications producer, without fx lifecycle wiring. Used to invoke
// checkBudget directly in DB-backed tests.
func newBudgetAlertService(db bun.IDB, log *slog.Logger) *UsageService {
	repo := NewRepository(db, log)
	notifSvc := notifications.NewService(notifications.NewRepository(db, log), nil, log)
	return &UsageService{repo: repo, db: db, log: log, notificationsSvc: notifSvc}
}

// seedBudgetProject creates a project with the given budget, alert threshold,
// and current-month spend, plus one project_admin member. It returns the
// project and admin user IDs.
func seedBudgetProject(t *testing.T, ctx context.Context, db bun.IDB, budgetUSD, threshold, spend float64) (string, string) {
	t.Helper()
	userID := uuid.NewString()
	orgID := uuid.NewString()
	projectID := uuid.NewString()

	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Budget Admin")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Org "+orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name, budget_usd, budget_alert_threshold) VALUES (?, ?, ?, ?, ?)`,
		projectID, orgID, "Project "+projectID, budgetUSD, threshold)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, 'project_admin')`,
		projectID, userID)
	require.NoError(t, err)

	if spend > 0 {
		_, err = db.ExecContext(ctx,
			`INSERT INTO kb.llm_usage_events (project_id, org_id, provider, model, estimated_cost_usd, created_at)
			 VALUES (?, ?, 'openai', 'test-model', ?, now())`,
			projectID, orgID, spend)
		require.NoError(t, err)
	}
	return projectID, userID
}

// countBudgetAlerts returns the number of budget-alert notifications for a user.
func countBudgetAlerts(t *testing.T, ctx context.Context, db bun.IDB, userID string) int {
	t.Helper()
	n, err := db.NewSelect().
		TableExpr("kb.notifications").
		Where("user_id = ?", userID).
		Where("event_key = ?", "account.usage.budget_alert").
		Count(ctx)
	require.NoError(t, err)
	return n
}

// TestCheckBudget_ZeroUsageEmitsNoAlert is the regression for the spurious
// "$0 used of this $10 monthly budget" alert (issue #1343). A threshold of 0
// (the value the Bun insert path persisted over the column default) made
// `spend < budget*threshold` false even at zero spend.
func TestCheckBudget_ZeroUsageEmitsNoAlert(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetzero")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := newBudgetAlertService(db, log)

	t.Run("default threshold with zero spend", func(t *testing.T) {
		projectID, userID := seedBudgetProject(t, ctx, db, 10.0, 0.80, 0)
		svc.checkBudget(ctx, projectID)
		svc.checkBudget(ctx, projectID)
		require.Zero(t, countBudgetAlerts(t, ctx, db, userID), "0% usage must not produce an alert")
	})

	t.Run("legacy zero threshold with zero spend", func(t *testing.T) {
		// Pre-fix rows have threshold 0; the guard must suppress alerts for
		// them even before the backfill runs.
		projectID, userID := seedBudgetProject(t, ctx, db, 10.0, 0, 0)
		svc.checkBudget(ctx, projectID)
		require.Zero(t, countBudgetAlerts(t, ctx, db, userID), "a non-positive threshold is not a breach level")
	})
}

// TestCheckBudget_CoalescesRepeatedEvaluations is the regression for the four
// identical alerts emitted at the same second (issue #1343): concurrent
// read-then-insert coalescing let every evaluation insert a row.
func TestCheckBudget_CoalescesRepeatedEvaluations(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetdedup")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := newBudgetAlertService(db, log)

	// $10 budget at 50% threshold = $5; $6 spend is a genuine breach.
	projectID, userID := seedBudgetProject(t, ctx, db, 10.0, 0.50, 6.0)

	t.Run("sequential evaluations emit one", func(t *testing.T) {
		svc.checkBudget(ctx, projectID)
		svc.checkBudget(ctx, projectID)
		require.Equal(t, 1, countBudgetAlerts(t, ctx, db, userID))
	})

	t.Run("concurrent evaluations emit one", func(t *testing.T) {
		const workers = 8
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				svc.checkBudget(ctx, projectID)
			}()
		}
		wg.Wait()
		require.Equal(t, 1, countBudgetAlerts(t, ctx, db, userID),
			"the durable unique key must make coalescing race-free")
	})
}

// TestCheckBudget_BreachEmitsAlert proves genuine breaches still alert.
func TestCheckBudget_BreachEmitsAlert(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "budgetbreach")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := newBudgetAlertService(db, log)

	// $10 budget at 80% = $8 threshold; $9 spend breaches it.
	projectID, userID := seedBudgetProject(t, ctx, db, 10.0, 0.80, 9.0)
	svc.checkBudget(ctx, projectID)
	require.Equal(t, 1, countBudgetAlerts(t, ctx, db, userID))
}
