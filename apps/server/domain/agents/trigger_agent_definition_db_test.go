package agents

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestTriggerAgentResolvesDefinitionOnlyAgent pins the #727 shape on the MCP
// surface: trigger_agent's advertised description lists the project's agent
// *definitions* by name, but both resolution paths queried kb.agents only, so an
// agent created in the UI was advertised and then answered "not found" — for
// agent_name and for the definition id alike.
func TestTriggerAgentResolvesDefinitionOnlyAgent(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "trigger_agent_definition_resolution")
	t.Cleanup(tdb.Close)

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "UI Agent", "project")
	repo := NewRepository(tdb.DB)
	h := NewMCPToolHandler(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)

	runtimeRow := func() (id, strategy string, definitionID *string) {
		require.NoError(t, tdb.DB.NewRaw(
			`SELECT id, strategy_type, agent_definition_id FROM kb.agents WHERE project_id = ?`,
			projectID).Scan(ctx, &id, &strategy, &definitionID))
		return id, strategy, definitionID
	}
	countRuntimeRows := func() int {
		var n int
		require.NoError(t, tdb.DB.NewRaw(
			`SELECT count(*) FROM kb.agents WHERE project_id = ?`, projectID).Scan(ctx, &n))
		return n
	}
	require.Zero(t, countRuntimeRows(), "fixture already has a kb.agents row")

	// By name, the way the injected catalog lists them.
	res, err := h.ExecuteTriggerAgent(ctx, projectID, map[string]any{"agent_name": "UI Agent"})
	require.NoError(t, err)
	require.NotContains(t, res.Content[0].Text, `"error"`, "trigger by catalog name failed: %s", res.Content[0].Text)
	require.True(t, strings.Contains(res.Content[0].Text, `"success": true`), "unexpected result: %s", res.Content[0].Text)

	id, strategy, definitionID := runtimeRow()
	require.Equal(t, "agent-def:"+defID, strategy)
	require.NotNil(t, definitionID)
	require.Equal(t, defID, *definitionID)

	// By defined id: the same row is reused, not duplicated.
	res, err = h.ExecuteTriggerAgent(ctx, projectID, map[string]any{"agent_id": defID})
	require.NoError(t, err)
	require.NotContains(t, res.Content[0].Text, `"error"`, "trigger by definition id failed: %s", res.Content[0].Text)
	require.Equal(t, 1, countRuntimeRows(), "the second trigger created a duplicate runtime agent")

	againID, _, _ := runtimeRow()
	require.Equal(t, id, againID)

	// An id in neither table still reports not found, and creates nothing.
	res, err = h.ExecuteTriggerAgent(ctx, projectID, map[string]any{"agent_id": uuid.NewString()})
	require.NoError(t, err)
	require.Contains(t, res.Content[0].Text, `"error"`)
	require.Equal(t, 1, countRuntimeRows(), "a failed trigger created a runtime agent")
}
