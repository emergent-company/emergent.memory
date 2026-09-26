package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestDurationMsPopulatedOnTerminalPaths locks the fix for #1071: every run
// completion path (failure, timeout/stale-reap, cancellation) must persist a
// non-NULL duration_ms derived from completed_at - started_at, and tool-call
// records must carry a non-NULL duration_ms. Before the fix both columns were
// written only on the success path (runs) or never at all (tool calls), leaving
// NULL for exactly the runs an operator wants to inspect.
func TestDurationMsPopulatedOnTerminalPaths(t *testing.T) {
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_duration_ms")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB

	orgID := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "dur-org")
	require.NoError(t, err)
	projectID := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "dur-project")
	require.NoError(t, err)
	agentID := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?,?,?,?,?)`, agentID, "dur-agent", "graph", "", projectID)
	require.NoError(t, err)

	insertRun := func(status AgentRunStatus, startedAt time.Time) string {
		id := uuid.NewString()
		_, insErr := db.ExecContext(ctx,
			`INSERT INTO kb.agent_runs (id, agent_id, status, started_at) VALUES (?,?,?,?)`,
			id, agentID, string(status), startedAt)
		require.NoError(t, insErr)
		return id
	}
	durationOf := func(id string) *int {
		var d *int
		require.NoError(t, db.NewRaw(`SELECT duration_ms FROM kb.agent_runs WHERE id = ?`, id).Scan(ctx, &d))
		return d
	}

	repo := NewRepository(db)

	// 1) Stale-run reaper: an abandoned running run gets duration_ms on reap.
	staleID := insertRun(RunStatusRunning, time.Now().Add(-40*time.Minute))
	n, err := repo.MarkStaleRunsAsError(ctx, 30*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NotNil(t, durationOf(staleID), "stale-reaped run must have non-NULL duration_ms")
	require.GreaterOrEqual(t, *durationOf(staleID), 0)

	// 2) FailRun (single-row failure path).
	failID := insertRun(RunStatusRunning, time.Now().Add(-5*time.Minute))
	require.NoError(t, repo.FailRun(ctx, failID, "boom"))
	require.NotNil(t, durationOf(failID), "FailRun must set duration_ms")

	// 3) FailRunWithSteps (executor failure path).
	failStepsID := insertRun(RunStatusRunning, time.Now().Add(-5*time.Minute))
	require.NoError(t, repo.FailRunWithSteps(ctx, failStepsID, "boom steps", 3))
	require.NotNil(t, durationOf(failStepsID), "FailRunWithSteps must set duration_ms")

	// 4) CancelRun (explicit cancellation).
	cancelID := insertRun(RunStatusRunning, time.Now().Add(-5*time.Minute))
	require.NoError(t, repo.CancelRun(ctx, cancelID))
	require.NotNil(t, durationOf(cancelID), "CancelRun must set duration_ms")

	// 5) CancelRunIfPaused (paused → cancelled).
	pausedID := insertRun(RunStatusPaused, time.Now().Add(-5*time.Minute))
	ok, err := repo.CancelRunIfPaused(ctx, pausedID)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, durationOf(pausedID), "CancelRunIfPaused must set duration_ms")

	// 6) Tool calls: CreateToolCall persists a non-NULL duration_ms.
	tc := &AgentRunToolCall{
		RunID:      failID,
		ToolName:   "entity-search",
		Input:      map[string]any{"q": "x"},
		Output:     map[string]any{"r": "y"},
		Status:     "completed",
		StepNumber: 0,
		DurationMs: intPtr(42),
	}
	require.NoError(t, repo.CreateToolCall(ctx, tc))
	var tcDur *int
	require.NoError(t, db.NewRaw(`SELECT duration_ms FROM kb.agent_run_tool_calls WHERE id = ?`, tc.ID).Scan(ctx, &tcDur))
	require.NotNil(t, tcDur, "tool-call record must have non-NULL duration_ms")
	require.Equal(t, 42, *tcDur)
}
