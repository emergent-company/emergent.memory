package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestTriggerAgent_UntrustedCallerCannotReachInternal is the fail-first
// regression for the trigger_agent trust misclassification. trigger_agent is
// reachable from ANY agent run via the ToolPool and from authenticated MCP
// clients, so an untrusted run (webhook / A2A / agentcompat / share) whose agent
// opts into trigger_agent must not be able to reach an internal-visible agent
// nor force the child trusted. Pre-fix, ExecuteTriggerAgent set
// TrustedInternal=true unconditionally and applied no visibility check, so the
// untrusted caller reached the internal target and produced a trusted child.
func TestTriggerAgent_UntrustedCallerCannotReachInternal(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_trigger_trust")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	insertAgentDefinition(t, tdb.DB, ctx, projectID, "internal-child", string(VisibilityInternal))
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "project-child", string(VisibilityProject))
	internalAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "internal-child")
	projectAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "project-child")

	// An untrusted parent run (e.g. started via the public webhook receiver).
	parentAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "parent-agent")
	parentRunID := insertAgentRun(t, tdb.DB, ctx, parentAgentID, "", false)

	repo := NewRepository(tdb.DB)
	ae := trustTestExecutor(repo)
	h := &MCPToolHandler{repo: repo, executor: ae}

	// The tool call carries the invoking run's ID, as runPipeline injects it.
	callCtx := contextWithCallerRunID(context.Background(), parentRunID)

	// 1) An untrusted caller must be blocked from triggering an internal target.
	res, err := h.ExecuteTriggerAgent(callCtx, projectID, map[string]any{"agent_name": "internal-child"})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Contains(t, res.Content[0].Text, "internal",
		"trigger_agent must reject an internal-visible target for an untrusted caller")
	require.Equal(t, 0, runCountByAgentID(t, tdb.DB, ctx, internalAgentID),
		"no child run must be started for a blocked internal trigger")

	// 2) A non-internal target is still allowed, but the child must be untrusted
	// (it inherits the parent's trust marker, so it cannot reach internal agents).
	res, err = h.ExecuteTriggerAgent(callCtx, projectID, map[string]any{"agent_name": "project-child"})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, 1, runCountByAgentID(t, tdb.DB, ctx, projectAgentID))
	require.False(t, runTrustedByAgentID(t, tdb.DB, ctx, projectAgentID),
		"a trigger_agent child of an untrusted parent must be persisted untrusted (trusted_internal=false)")
}

// TestCallAgent_ProducesUntrustedRun pins the call_agent trust inheritance: the
// per-agent MCP endpoint's call_agent is a direct authenticated MCP client (no
// parent run), so the run it starts must be untrusted, leaving the called agent
// unable to reach internal-visible agents through its own coordination tools.
func TestCallAgent_ProducesUntrustedRun(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_call_trust")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "called-agent", string(VisibilityProject))
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "called-agent")

	repo := NewRepository(tdb.DB)
	ae := trustTestExecutor(repo)
	h := &MCPToolHandler{repo: repo, executor: ae}

	// No caller run in context — a direct authenticated MCP client.
	_, _, _ = h.RunAgentOnce(context.Background(), projectID, agentID, "hello", mcp.AgentRunBudget{MaxSteps: 4})

	require.Equal(t, 1, runCountByAgentID(t, tdb.DB, ctx, agentID))
	require.False(t, runTrustedByAgentID(t, tdb.DB, ctx, agentID),
		"a call_agent run must be persisted untrusted (trusted_internal=false)")
}

func runCountByAgentID(t *testing.T, db *bun.DB, ctx context.Context, agentID string) int {
	t.Helper()
	var n int
	err := db.NewRaw(`SELECT COUNT(*) FROM kb.agent_runs WHERE agent_id = ?`, agentID).Scan(ctx, &n)
	require.NoError(t, err)
	return n
}

func runTrustedByAgentID(t *testing.T, db *bun.DB, ctx context.Context, agentID string) bool {
	t.Helper()
	var trusted bool
	err := db.NewRaw(`SELECT trusted_internal FROM kb.agent_runs WHERE agent_id = ? ORDER BY created_at DESC LIMIT 1`, agentID).Scan(ctx, &trusted)
	require.NoError(t, err)
	return trusted
}
