package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestCreateProject_DefaultsWeeklyBudget closes the one creation path the
// projects service does not own: the create_project MCP tool inserts the row
// itself, so it has to state the weekly cap the REST path derives. Leaving it
// to the schema default would make MCP-created projects the only new projects
// with no weekly limit — invisible until a runaway week went uncapped.
func TestCreateProject_DefaultsWeeklyBudget(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mcp_create_project_budget")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	orgID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Budget Org").Exec(ctx)
	require.NoError(t, err)

	userID := uuid.NewString()
	svc := &Service{
		db: db,
		projectOrgAdminAuthorizer: func(context.Context, string, string) error {
			return nil
		},
	}
	callerCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: userID})

	res, err := svc.executeCreateProject(callerCtx, map[string]any{
		"name":   "MCP Budget Project " + uuid.NewString()[:8],
		"org_id": orgID,
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	var row struct {
		BudgetUSD                  float64 `bun:"budget_usd"`
		BudgetWeeklyUSD            float64 `bun:"budget_weekly_usd"`
		BudgetAlertThreshold       float64 `bun:"budget_alert_threshold"`
		BudgetWeeklyAlertThreshold float64 `bun:"budget_weekly_alert_threshold"`
	}
	err = db.NewRaw(
		`SELECT budget_usd, budget_weekly_usd, budget_alert_threshold, budget_weekly_alert_threshold
		 FROM kb.projects WHERE id = ?`,
		res.StructuredContent["id"],
	).Scan(ctx, &row)
	require.NoError(t, err)

	require.InDelta(t, 10.0, row.BudgetUSD, 1e-9)
	require.InDelta(t, 2.5, row.BudgetWeeklyUSD, 1e-9)
	require.InDelta(t, 0.8, row.BudgetAlertThreshold, 1e-9)
	require.InDelta(t, 0.8, row.BudgetWeeklyAlertThreshold, 1e-9)
}
