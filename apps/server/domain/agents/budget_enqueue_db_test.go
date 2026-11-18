package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestCreateRunQueuedBudgetGate locks the enqueue-side gate: an over-budget
// project must not accumulate run or job rows, and enforcement-disabled (or
// no gate) must keep today's behaviour.
func TestCreateRunQueuedBudgetGate(t *testing.T) {
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_budget_enqueue")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB

	orgID := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "budget-org")
	require.NoError(t, err)
	projectID := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "budget-project")
	require.NoError(t, err)
	agentID := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?,?,?,?,?)`, agentID, "budget-agent", "graph", "", projectID)
	require.NoError(t, err)

	repo := NewRepository(db)
	countRows := func() (runs, jobs int) {
		require.NoError(t, db.NewRaw(`SELECT COUNT(*) FROM kb.agent_runs WHERE agent_id = ?`, agentID).Scan(ctx, &runs))
		require.NoError(t, db.NewRaw(`SELECT COUNT(*) FROM kb.agent_run_jobs arj JOIN kb.agent_runs ar ON ar.id = arj.run_id WHERE ar.agent_id = ?`, agentID).Scan(ctx, &jobs))
		return runs, jobs
	}

	t.Run("enforced gate blocks without inserting rows", func(t *testing.T) {
		repo.SetBudgetGate(&fakeBudgetChecker{exceeded: true}, true)
		_, err := repo.CreateRunQueued(ctx, agentID, 1)
		var budgetErr *BudgetExceededError
		require.True(t, errors.As(err, &budgetErr))
		runs, jobs := countRows()
		require.Zero(t, runs)
		require.Zero(t, jobs)
	})

	t.Run("disabled enforcement proceeds", func(t *testing.T) {
		repo.SetBudgetGate(&fakeBudgetChecker{exceeded: true}, false)
		run, err := repo.CreateRunQueued(ctx, agentID, 1)
		require.NoError(t, err)
		require.NotNil(t, run)
	})

	t.Run("under budget proceeds", func(t *testing.T) {
		repo.SetBudgetGate(&fakeBudgetChecker{exceeded: false}, true)
		run, err := repo.CreateRunQueued(ctx, agentID, 1)
		require.NoError(t, err)
		require.NotNil(t, run)
	})
}
