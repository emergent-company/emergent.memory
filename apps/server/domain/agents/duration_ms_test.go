package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestDurationMsPopulatedOnAllTerminalTransitions locks the fix for #1071:
// every terminal agent-run transition must persist a non-NULL duration_ms
// derived from completed_at - started_at, and tool-call records must carry a
// non-NULL duration_ms. Before the fix, duration_ms was written only on the
// success path (CompleteRunWithSteps) and never on tool calls, leaving NULL for
// exactly the runs an operator wants to inspect.
func TestDurationMsPopulatedOnAllTerminalTransitions(t *testing.T) {
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

	repo := NewRepository(db)

	insertRun := func(status AgentRunStatus, startedAt time.Time) string {
		id := uuid.NewString()
		_, insErr := db.ExecContext(ctx,
			`INSERT INTO kb.agent_runs (id, agent_id, status, started_at) VALUES (?,?,?,?)`,
			id, agentID, string(status), startedAt)
		require.NoError(t, insErr)
		return id
	}
	insertJob := func(runID string) string {
		id := uuid.NewString()
		_, insErr := db.ExecContext(ctx,
			`INSERT INTO kb.agent_run_jobs (id, run_id, status) VALUES (?,?,?)`,
			id, runID, "processing")
		require.NoError(t, insErr)
		return id
	}
	durationOf := func(id string) *int {
		var d *int
		require.NoError(t, db.NewRaw(`SELECT duration_ms FROM kb.agent_runs WHERE id = ?`, id).Scan(ctx, &d))
		return d
	}

	// Each case seeds a run (and optional job), drives it to a terminal state
	// through a real repository method, and returns the run ID to assert on.
	cases := []struct {
		name string
		run  func() string
	}{
		{
			name: "CompleteRunWithSteps",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
				require.NoError(t, repo.CompleteRunWithSteps(ctx, id, map[string]any{}, 1, 120000))
				return id
			},
		},
		{
			name: "CompleteRun",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
				require.NoError(t, repo.CompleteRun(ctx, id, map[string]any{}))
				return id
			},
		},
		{
			name: "CompleteJob",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
				jobID := insertJob(id)
				require.NoError(t, repo.CompleteJob(ctx, jobID, id))
				return id
			},
		},
		{
			name: "FailRun",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
				require.NoError(t, repo.FailRun(ctx, id, "boom"))
				return id
			},
		},
		{
			name: "FailRunWithSteps",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
				require.NoError(t, repo.FailRunWithSteps(ctx, id, "boom steps", 3))
				return id
			},
		},
		{
			name: "FailJob",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
				jobID := insertJob(id)
				require.NoError(t, repo.FailJob(ctx, jobID, id, "boom job", false, time.Time{}))
				return id
			},
		},
		{
			name: "SkipRun",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
				require.NoError(t, repo.SkipRun(ctx, id, "skipped by policy"))
				return id
			},
		},
		{
			name: "CancelRun",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
				require.NoError(t, repo.CancelRun(ctx, id))
				return id
			},
		},
		{
			name: "CancelRunIfPaused",
			run: func() string {
				id := insertRun(RunStatusPaused, time.Now().Add(-2*time.Minute))
				ok, err := repo.CancelRunIfPaused(ctx, id)
				require.NoError(t, err)
				require.True(t, ok)
				return id
			},
		},
		{
			name: "MarkStaleRunsAsError",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-40*time.Minute))
				n, err := repo.MarkStaleRunsAsError(ctx, 30*time.Minute)
				require.NoError(t, err)
				require.Equal(t, 1, n)
				return id
			},
		},
		{
			name: "MarkOrphanedRunsAsError",
			run: func() string {
				id := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
				n, err := repo.MarkOrphanedRunsAsError(ctx)
				require.NoError(t, err)
				require.Equal(t, 1, n)
				return id
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runID := tc.run()
			d := durationOf(runID)
			require.NotNil(t, d, "duration_ms must be populated on %s", tc.name)
			require.GreaterOrEqual(t, *d, 0)
		})
	}

	// Tool-call records must carry a non-NULL duration_ms.
	t.Run("CreateToolCall", func(t *testing.T) {
		runID := insertRun(RunStatusRunning, time.Now().Add(-2*time.Minute))
		tc := &AgentRunToolCall{
			RunID:      runID,
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
	})
}
