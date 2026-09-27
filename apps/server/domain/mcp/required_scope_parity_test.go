package mcp_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// fakeAgentHandler is a safe stand-in for the agents-domain MCP tool handler:
// GetAgentToolDefinitions returns the real (production) definitions, while every
// Execute/Run method returns an error rather than touching a DB/service. The
// parity test never reaches dispatch (the authority gate fires first), so these
// stubs exist only to make a reach-through impossible rather than a panic.
type fakeAgentHandler struct{}

func (fakeAgentHandler) GetAgentToolDefinitions() []mcp.ToolDefinition {
	return (&agents.MCPToolHandler{}).GetAgentToolDefinitions()
}
func (fakeAgentHandler) GetAgentToolDefinitionsForProject(context.Context, string) []mcp.ToolDefinition {
	return (&agents.MCPToolHandler{}).GetAgentToolDefinitions()
}
func (fakeAgentHandler) ExecuteListAgentDefinitions(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteGetAgentDefinition(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteCreateAgentDefinition(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteUpdateAgentDefinition(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteDeleteAgentDefinition(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteListAgents(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteGetAgent(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteCreateAgent(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteUpdateAgent(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteDeleteAgent(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteTriggerAgent(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteListAgentRuns(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteGetAgentRun(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteGetAgentRunMessages(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteGetAgentRunToolCalls(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteGetRunStatus(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteRememberStatus(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteListAvailableAgents(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteListAgentQuestions(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteListProjectAgentQuestions(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteRespondToAgentQuestion(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteListAgentHooks(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteCreateAgentHook(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteDeleteAgentHook(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteListADKSessions(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) ExecuteGetADKSession(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return nil, errNotDispatched
}
func (fakeAgentHandler) RunAgentOnce(context.Context, string, string, string, mcp.AgentRunBudget) (string, string, error) {
	return "", "", errNotDispatched
}
func (fakeAgentHandler) RunAgentInSession(context.Context, string, string, string, string, mcp.AgentRunBudget) (string, string, int, error) {
	return "", "", 0, errNotDispatched
}

var errNotDispatched = errors.New("tool dispatch must not be reached in the parity test")

// TestRequiredScopeInProcessParity is the mechanism-7 regression guard: it
// asserts, for every tool in the runtime catalog that declares a RequiredScope,
// that an untrusted in-process caller is refused on the ExecuteTool path — the
// same authority the three HTTP transports enforce before dispatch. It goes RED
// when a tool's RequiredScope is not enforced in-process (fail-first).
func TestRequiredScopeInProcessParity(t *testing.T) {
	svc := &mcp.Service{}
	svc.RegisterAgentToolHandler(fakeAgentHandler{})
	svc.RegisterMCPRegistryToolHandler(fakeRegistryHandler{})

	defs := svc.GetToolDefinitions()
	require.NotEmpty(t, defs, "tool catalog must be non-empty")

	var scoped, agentOnly, superadmin []string
	scopedByScope := map[string]string{}
	for _, def := range defs {
		switch {
		case def.SuperadminOnly:
			superadmin = append(superadmin, def.Name)
		case def.AgentOnly:
			agentOnly = append(agentOnly, def.Name)
		case def.RequiredScope != "":
			scoped = append(scoped, def.Name)
			scopedByScope[def.Name] = def.RequiredScope
		}
	}

	// Coverage guard: the catalog must include handler-provided and dynamic
	// scoped tools, so a missing handler cannot silently shrink the parity set.
	require.Contains(t, scoped, "agent-create", "agent handler must contribute scoped agent tools")
	require.Contains(t, agentOnly, "mcp-server-create", "registry handler must contribute agent-only registry tools")
	require.Contains(t, scoped, "blueprint-create", "blueprint tools must be in the scoped set")
	require.Contains(t, scoped, "entity-query", "core graph tools must be in the scoped set")

	projectID := "00000000-0000-0000-0000-000000000000"
	untrustedCtx := context.Background()

	for _, name := range scoped {
		name, scope := name, scopedByScope[name]
		t.Run("untrusted refused on scoped "+name, func(t *testing.T) {
			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process %s (RequiredScope %q) by an untrusted run must be refused", name, scope)
			// The gate denial is one of two shapes: the seam's untrusted-surface
			// refusal (plain + admin-scoped) or the superadmin escalation for the
			// sensitive admin tools (token-*, provider-configure-project,
			// project-create). Either proves the tool was gated before dispatch.
			msg := err.Error()
			require.True(t, strings.Contains(msg, "untrusted surface") || strings.Contains(msg, "superadmin"),
				"%s refusal must come from the authority gate, got: %v", name, err)
		})
	}

	for _, name := range agentOnly {
		t.Run("untrusted refused on agent-only "+name, func(t *testing.T) {
			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process agent-only %s by an untrusted run must be refused", name)
			require.Contains(t, err.Error(), "agent-only", "%s refusal must name the agent-only boundary", name)
		})
	}

	for _, name := range superadmin {
		t.Run("untrusted refused on superadmin "+name, func(t *testing.T) {
			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process superadmin-only %s by an untrusted run must be refused", name)
			require.Contains(t, err.Error(), "superadmin", "%s refusal must name the superadmin boundary", name)
		})
	}
}

// TestRequiredScopeInProcessTrustedNoOverCorrection pins the positive direction:
// a trusted in-process run (session UI / scheduler) is NOT refused at the
// authority gate on a scoped tool — the gate must allow the trusted principal to
// reach dispatch. It exercises a scoped tool whose dispatch fails on its own
// argument validation (not the gate), proving the gate passed.
func TestRequiredScopeInProcessTrustedNoOverCorrection(t *testing.T) {
	svc := &mcp.Service{}
	svc.RegisterAgentToolHandler(fakeAgentHandler{})
	svc.RegisterMCPRegistryToolHandler(fakeRegistryHandler{})
	_ = svc.GetToolDefinitions()

	projectID := "00000000-0000-0000-0000-000000000000"
	trustedCtx := mcp.ContextWithTrustedInternal(context.Background(), true)

	// provider-models-list is admin-scoped but read-only (not in the sensitive
	// subset), so a trusted run must pass the gate and reach the tool's own
	// argument validation rather than be gate-refused.
	_, err := svc.ExecuteTool(trustedCtx, projectID, "provider-models-list", map[string]any{})
	require.Error(t, err, "provider-models-list with empty args must error on validation")
	require.NotContains(t, err.Error(), "untrusted surface", "trusted run must not be gate-refused")
	require.True(t, strings.Contains(err.Error(), "provider_name") || strings.Contains(err.Error(), "provider"),
		"must reach the tool's own argument validation, got: %v", err)
}
