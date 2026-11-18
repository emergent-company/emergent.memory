package agents

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestToWorkItemRunDTOCarriesAgentAndSteps pins the drawer projection's new
// fields: the agent name comes from the preloaded relation, and the step usage
// comes from the run row. Without these the preview cannot say who ran or how
// far the run got.
func TestToWorkItemRunDTOCarriesAgentAndSteps(t *testing.T) {
	started := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	completed := started.Add(2 * time.Minute)
	duration := 120000
	maxSteps := 120
	run := &AgentRun{
		ID:          "run-1",
		Status:      RunStatusPaused,
		StartedAt:   started,
		CompletedAt: &completed,
		DurationMs:  &duration,
		StepCount:   120,
		MaxSteps:    &maxSteps,
		Summary:     map[string]any{"reason": "step_limit_reached"},
		Agent:       &Agent{Name: "coding"},
	}

	dto := toWorkItemRunDTO(run)
	require.Equal(t, "run-1", dto.ID)
	require.Equal(t, "coding", dto.AgentName)
	require.Equal(t, string(RunStatusPaused), dto.Status)
	require.Equal(t, 120, dto.StepCount)
	require.NotNil(t, dto.MaxSteps)
	require.Equal(t, 120, *dto.MaxSteps)
	require.NotNil(t, dto.DurationMs)
	require.Equal(t, 120000, *dto.DurationMs)
	require.Equal(t, "step_limit_reached", dto.Summary["reason"])

	// An unbound runtime agent must not panic or invent a name.
	bare := toWorkItemRunDTO(&AgentRun{ID: "run-2", Status: RunStatusQueued})
	require.Empty(t, bare.AgentName)
}

// TestListRunsForSubjectPreloadsAgentAndSteps pins the query change behind the
// preview: the run history is ordered newest first and carries the Agent
// relation plus the step usage the drawer renders. It is skipped without a
// database (testing.Short), like the sibling dispatch tests.
func TestListRunsForSubjectPreloadsAgentAndSteps(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "k-runs-history", "ready")
	objType := "ResearchRequest"
	run, err := repo.CreateRunQueued(ctx, agentID, 1, CreateRunQueuedOptions{
		SubjectObjectID:   &canonicalID,
		SubjectObjectType: &objType,
	})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`UPDATE kb.agent_runs SET status = ?, step_count = ?, max_steps = ? WHERE id = ?`,
		string(RunStatusPaused), 120, 120, run.ID)
	require.NoError(t, err)

	runs, err := repo.ListRunsForSubject(ctx, canonicalID, 20)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.NotNil(t, runs[0].Agent, "the Agent relation must be preloaded")
	require.Equal(t, "researcher", runs[0].Agent.Name)
	require.Equal(t, 120, runs[0].StepCount)
	require.NotNil(t, runs[0].MaxSteps)
	require.Equal(t, 120, *runs[0].MaxSteps)
}
