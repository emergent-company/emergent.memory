package agents

import (
	"context"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"

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
		require.True(t, isBudgetFailure(err))
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

	t.Run("deduped work enqueue is blocked without claims", func(t *testing.T) {
		// Own agent: the earlier subtests insert runs for agentID, so absolute
		// counts would be polluted by ordering.
		dedupAgentID := uuid.NewString()
		_, err := db.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?,?,?,?,?)`, dedupAgentID, "budget-dedup-agent", "graph", "", projectID)
		require.NoError(t, err)

		repo.SetBudgetGate(&fakeBudgetChecker{exceeded: true}, true)
		logEntry := &AgentProcessingLog{
			AgentID:       dedupAgentID,
			GraphObjectID: uuid.NewString(),
			ObjectVersion: 1,
			EventType:     ReactionEventType("created"),
		}
		run, claimed, err := repo.EnqueueWorkRunDeduped(ctx, logEntry, dedupAgentID, 1, CreateRunQueuedOptions{})
		require.True(t, isBudgetFailure(err))
		require.Nil(t, run)
		require.False(t, claimed)
		var runs, jobs, logs int
		require.NoError(t, db.NewRaw(`SELECT COUNT(*) FROM kb.agent_runs WHERE agent_id = ?`, dedupAgentID).Scan(ctx, &runs))
		require.NoError(t, db.NewRaw(`SELECT COUNT(*) FROM kb.agent_run_jobs arj JOIN kb.agent_runs ar ON ar.id = arj.run_id WHERE ar.agent_id = ?`, dedupAgentID).Scan(ctx, &jobs))
		require.NoError(t, db.NewRaw(`SELECT COUNT(*) FROM kb.agent_processing_log WHERE agent_id = ?`, dedupAgentID).Scan(ctx, &logs))
		require.Zero(t, runs)
		require.Zero(t, jobs)
		require.Zero(t, logs)
	})

	t.Run("under budget proceeds", func(t *testing.T) {
		repo.SetBudgetGate(&fakeBudgetChecker{exceeded: false}, true)
		run, err := repo.CreateRunQueued(ctx, agentID, 1)
		require.NoError(t, err)
		require.NotNil(t, run)
	})
}

// TestExecuteWithRunBudgetBlockFinalizesRun covers the A2A/session-trigger
// surface: the run row already exists, so the gate must terminalize it instead
// of leaving it running until the reaper.
func TestExecuteWithRunBudgetBlockFinalizesRun(t *testing.T) {
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_budget_finalize")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB

	orgID := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "budget-fin-org")
	require.NoError(t, err)
	projectID := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "budget-fin-project")
	require.NoError(t, err)
	agentID := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?,?,?,?,?)`, agentID, "budget-fin-agent", "graph", "", projectID)
	require.NoError(t, err)

	repo := NewRepository(db)
	run, err := repo.CreateRun(ctx, agentID)
	require.NoError(t, err)

	ae := &AgentExecutor{
		repo:         repo,
		usageService: &fakeBudgetChecker{exceeded: true},
		safeguards:   config.AgentSafeguardsConfig{ExecutionEnabled: true, BudgetEnforcementEnabled: true},
		log:          testAgentLogger(),
	}
	_, err = ae.ExecuteWithRun(ctx, run, ExecuteRequest{ProjectID: projectID})
	require.True(t, isBudgetFailure(err))

	var row struct {
		Status       string  `bun:"status"`
		ErrorMessage *string `bun:"error_message"`
	}
	require.NoError(t, db.NewRaw(`SELECT status, error_message FROM kb.agent_runs WHERE id = ?`, run.ID).Scan(ctx, &row))
	require.Equal(t, string(RunStatusError), row.Status)
	require.NotNil(t, row.ErrorMessage)
	require.Contains(t, *row.ErrorMessage, "exceeded its spending budget")
}
