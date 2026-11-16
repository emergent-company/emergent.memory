package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestRunListingsLoadAgentRelationForDTOFallback pins issue #873.
//
// #871 taught AgentRun.ToDTO to fall back to the runtime agent's
// agent_definition_id when the run's own column is NULL (work/board dispatch,
// scheduler and reaction runs never set it). The fallback only produces a value
// where the loader joined the Agent relation, so every run listing that feeds a
// DTO must load it — otherwise the same run reports `agentDefinitionId` on the
// run-history endpoint and nothing at all in the project/session listings, and
// a UI grouping runs by agent silently drops them.
//
// The relation is `rel:belongs-to` → LEFT JOIN, so a run is never filtered out
// by a missing runtime agent. (kb.agent_runs.agent_id is ON DELETE CASCADE, so a
// deleted agent takes its runs with it regardless; the LEFT JOIN matters for the
// projection, not for row survival.)
func TestRunListingsLoadAgentRelationForDTOFallback(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "run_listing_agent_relation")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)

	defID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility) VALUES (?, ?, ?, 'project')`,
		defID, projectID, "coding-873")
	require.NoError(t, err)

	// A runtime agent bound to the definition: the shape every object-driven
	// run is created through.
	agentID := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id, agent_definition_id)
		 VALUES (?, ?, 'definition', '', ?, ?)`,
		agentID, "coding-873", projectID, defID)
	require.NoError(t, err)

	parent, err := repo.CreateRun(ctx, agentID)
	require.NoError(t, err)

	// A child run, linked to the parent so FindChildRuns returns it.
	child, err := repo.CreateRun(ctx, agentID)
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_runs SET parent_run_id = ? WHERE id = ?`, parent.ID, child.ID)
	require.NoError(t, err)

	// A root run (resumed_from NULL) with a session, the A2A task shape.
	root, err := repo.CreateRun(ctx, agentID)
	require.NoError(t, err)
	sessionID := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.sessions (id, project_id) VALUES (?, ?)`, sessionID, projectID)
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_runs SET session_id = ? WHERE id = ?`, sessionID, root.ID)
	require.NoError(t, err)

	// Guard: the column really is NULL, so a pass cannot come from the column
	// itself instead of the relation fallback.
	var columnDef *string
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT agent_definition_id FROM kb.agent_runs WHERE id = ?`, root.ID,
	).Scan(ctx, &columnDef))
	require.Nil(t, columnDef, "runtime-agent runs must start with a NULL agent_definition_id")

	assertAttributed := func(t *testing.T, label string, run *AgentRun) {
		t.Helper()
		require.NotNil(t, run, "%s: no run returned", label)
		require.NotNil(t, run.Agent, "%s: loader did not join the Agent relation", label)
		dto := run.ToDTO()
		require.NotNil(t, dto.AgentDefinitionID, "%s: DTO reported no agentDefinitionId", label)
		require.Equal(t, defID, *dto.AgentDefinitionID, "%s: wrong agentDefinitionId", label)
		require.Equal(t, "coding-873", dto.AgentName, "%s: wrong agentName", label)
	}

	t.Run("GetRecentRuns", func(t *testing.T) {
		runs, err := repo.GetRecentRuns(ctx, agentID, 10)
		require.NoError(t, err)
		require.NotEmpty(t, runs)
		assertAttributed(t, "GetRecentRuns", runs[0])
	})

	t.Run("FindChildRuns", func(t *testing.T) {
		runs, err := repo.FindChildRuns(ctx, parent.ID)
		require.NoError(t, err)
		require.Len(t, runs, 1)
		assertAttributed(t, "FindChildRuns", runs[0])
	})

	t.Run("FindRunsByProjectPaginated", func(t *testing.T) {
		runs, total, err := repo.FindRunsByProjectPaginated(ctx, projectID, RunFilters{}, 50, 0)
		require.NoError(t, err)
		require.Equal(t, 3, total)
		require.Len(t, runs, 3)
		for _, run := range runs {
			assertAttributed(t, "FindRunsByProjectPaginated", run)
		}
	})

	// A page smaller than the row count: a belongs-to relation must not multiply
	// rows (an erroneous has-many join would return duplicates and skew `total`
	// against the page), and every page must keep the attribution.
	t.Run("FindRunsByProjectPaginatedWindow", func(t *testing.T) {
		runs, total, err := repo.FindRunsByProjectPaginated(ctx, projectID, RunFilters{}, 1, 1)
		require.NoError(t, err)
		require.Equal(t, 3, total)
		require.Len(t, runs, 1, "one row per run, never a duplicated join row")
		assertAttributed(t, "FindRunsByProjectPaginatedWindow", runs[0])
	})

	t.Run("ListA2ARuns", func(t *testing.T) {
		runs, total, err := repo.ListA2ARuns(ctx, projectID, "", nil, 50, 0)
		require.NoError(t, err)
		require.NotZero(t, total)
		var found bool
		for _, run := range runs {
			if run.ID != root.ID {
				continue
			}
			found = true
			assertAttributed(t, "ListA2ARuns", run)
		}
		require.True(t, found, "ListA2ARuns did not return the root task")
	})
}

// TestRunListingsSurviveUnboundRuntimeAgent is the negative half: a runtime
// agent with no definition must not claim one, and the listing must still return
// the run rather than dropping it.
func TestRunListingsSurviveUnboundRuntimeAgent(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "run_listing_unbound")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, "unbound-873", projectID)
	require.NoError(t, err)

	_, err = repo.CreateRun(ctx, agentID)
	require.NoError(t, err)

	runs, err := repo.GetRecentRuns(ctx, agentID, 10)
	require.NoError(t, err, "an unbound runtime agent must not remove the run from the listing")
	require.Len(t, runs, 1)
	require.Nil(t, runs[0].ToDTO().AgentDefinitionID)
	require.Equal(t, "unbound-873", runs[0].ToDTO().AgentName)
}
