package standalone

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestBootstrapCreateProject_DefaultsWeeklyBudget covers the second creation
// path that bypasses projects.Service.Create: the standalone bootstrap writes
// its project with raw SQL. If it forgot the weekly column, a standalone
// install would start life weekly-uncapped while every project created through
// the API had a cap — the runaway week would go uncaught exactly where nobody is
// watching a budget page.
func TestBootstrapCreateProject_DefaultsWeeklyBudget(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "standalone_bootstrap_budget")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	userID := uuid.NewString()
	_, err := db.NewRaw(
		`INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`,
		userID, "standalone-"+userID,
	).Exec(ctx)
	require.NoError(t, err)

	orgID := uuid.NewString()
	_, err = db.NewRaw(`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Standalone Org").Exec(ctx)
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.Standalone.ProjectName = "Standalone"
	bs := NewBootstrapService(db, cfg, slog.Default())

	var projectID string
	err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var txErr error
		projectID, txErr = bs.createProject(ctx, tx, orgID, userID)
		return txErr
	})
	require.NoError(t, err)

	var row struct {
		BudgetUSD                  float64 `bun:"budget_usd"`
		BudgetWeeklyUSD            float64 `bun:"budget_weekly_usd"`
		BudgetAlertThreshold       float64 `bun:"budget_alert_threshold"`
		BudgetWeeklyAlertThreshold float64 `bun:"budget_weekly_alert_threshold"`
	}
	err = db.NewRaw(
		`SELECT budget_usd, budget_weekly_usd, budget_alert_threshold, budget_weekly_alert_threshold
		 FROM kb.projects WHERE id = ?`,
		projectID,
	).Scan(ctx, &row)
	require.NoError(t, err)

	require.InDelta(t, projects.DefaultMonthlyBudgetUSD, row.BudgetUSD, 1e-9)
	require.InDelta(t, projects.DefaultWeeklyBudgetUSD, row.BudgetWeeklyUSD, 1e-9)
	require.InDelta(t, projects.DefaultBudgetAlertThreshold, row.BudgetAlertThreshold, 1e-9)
	require.InDelta(t, projects.DefaultBudgetAlertThreshold, row.BudgetWeeklyAlertThreshold, 1e-9)
}
