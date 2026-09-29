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
func (fakeAgentHandler) ResolveAgentDefinitionID(context.Context, string, string) string { return "" }

var errNotDispatched = errors.New("tool dispatch must not be reached in the parity test")

// #1135 scope hygiene: the registry browse/install tools previously declared no
// RequiredScope (implicitly open in-process), and seven tools declared the
// deprecated bare `admin` scope. This table pins the corrected authority so a
// regression to an unscoped or over-broad declaration fails here. It is
// deliberately exhaustive for the changed tools — the generic parity loop below
// only asserts *some* scope is enforced, not *which*.
var changedToolScopes = map[string]string{
	// Registry browse/install tools: explicit least-privilege scopes.
	"search_mcp_registry":  "projects:read",
	"mcp-registry-get":     "projects:read",
	"mcp-registry-install": "admin",
	"mcp-server-inspect":   "admin",
	// Re-keyed away from the deprecated bare `admin`: project-tier authority.
	"token-list":                 "projects:write",
	"token-create":               "projects:write",
	"token-get":                  "projects:write",
	"token-revoke":               "projects:write",
	"provider-configure-project": "projects:write",
	"provider-models-list":       "projects:read",
	"project-create":             "projects:write",
}

// retiredBareAdminAgentOnlyTools are the seven agent-only registry-management
// tools whose bare `admin` RequiredScope was retired by #1143. They are
// reachable only in-process: every HTTP transport refuses an AgentOnly tool
// before it consults RequiredScope (handler.go / sse_handler.go /
// streamable_http_handler.go), and authz.AuthorizeTool checks AgentOnly ahead of
// RequiredScope, so a RequiredScope on them is inert on every entrypoint.
// AgentOnly alone is the authority; the scope carried no reachability and is
// removed rather than replaced with an equally-inert narrower value.
var retiredBareAdminAgentOnlyTools = []string{
	"mcp-server-list",
	"mcp-server-get",
	"mcp-server-create",
	"update_mcp_server",
	"mcp-server-delete",
	"toggle_mcp_server_tool",
	"sync_mcp_server_tools",
}

// TestRetiredBareAdminAgentOnlyTools pins the #1143 retirement. For each of the
// seven named tools it asserts the declared scope is empty (a reintroduced bare
// `admin` — or any other scope — fails here) and the AgentOnly marker survives.
// It then asserts the invariant across the whole catalog: no AgentOnly tool may
// declare a RequiredScope. Finally it runs the negative case — an untrusted
// in-process caller must still be refused by the agent-only boundary — so that
// dropping the scope does not silently open the tool.
func TestRetiredBareAdminAgentOnlyTools(t *testing.T) {
	svc := &mcp.Service{}
	svc.RegisterAgentToolHandler(fakeAgentHandler{})
	svc.RegisterMCPRegistryToolHandler(fakeRegistryHandler{})
	defs := svc.GetToolDefinitions()
	require.NotEmpty(t, defs)

	projectID := "00000000-0000-0000-0000-000000000000"
	untrustedCtx := context.Background()

	for _, name := range retiredBareAdminAgentOnlyTools {
		name := name
		t.Run(name, func(t *testing.T) {
			def := svc.GetToolByName(name)
			require.NotNil(t, def, "retired tool %q must be in the catalog", name)
			require.Empty(t, def.RequiredScope,
				"tool %q must declare no RequiredScope — AgentOnly is its authority (#1143 retired bare admin)", name)
			require.True(t, def.AgentOnly,
				"tool %q must remain explicitly agent-only; dropping the scope without the marker would open it", name)

			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process %s by an untrusted run must be refused", name)
			require.Contains(t, err.Error(), "agent-only",
				"%s refusal must come from the agent-only boundary, got: %v", name, err)
		})
	}

	// Invariant: AgentOnly is a complete authority on its own; a scope declared
	// alongside it is unreachable on every entrypoint and would be a finding.
	for _, def := range defs {
		if !def.AgentOnly {
			continue
		}
		require.Empty(t, def.RequiredScope,
			"agent-only tool %q must not declare RequiredScope %q", def.Name, def.RequiredScope)
	}
}

// unscopeableToolNames are the only tools allowed to declare no RequiredScope
// and no SuperadminOnly/AgentOnly marker. Both are session-scoped: their
// ownership is enforced at the data-access layer by the shared
// sessiontodos.SessionAccessible predicate, not by a project scope. Any other
// unscoped tool is a finding — the #1135 hole was exactly an unscoped,
// implicitly-open registry tool.
var unscopeableToolNames = map[string]bool{
	"session-todo-list":   true,
	"session-todo-update": true,
}

// TestChangedToolScopesPinned asserts the declared scope of every tool changed
// by #1135 is exactly the corrected value, and that an untrusted in-process run
// is refused on it (the negative case). It goes RED if a changed tool reverts to
// bare `admin` or loses its scope.
func TestChangedToolScopesPinned(t *testing.T) {
	svc := &mcp.Service{}
	svc.RegisterAgentToolHandler(fakeAgentHandler{})
	svc.RegisterMCPRegistryToolHandler(fakeRegistryHandler{})
	_ = svc.GetToolDefinitions()

	projectID := "00000000-0000-0000-0000-000000000000"
	untrustedCtx := context.Background()

	for name, want := range changedToolScopes {
		name, want := name, want
		t.Run(name, func(t *testing.T) {
			def := svc.GetToolByName(name)
			require.NotNil(t, def, "changed tool %q must be in the catalog", name)
			require.Equal(t, want, def.RequiredScope,
				"tool %q declared scope drifted (want %q)", name, want)

			// Negative: a caller lacking the scope (untrusted in-process) is denied.
			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process %s (%q) by an untrusted run must be refused", name, want)
			msg := err.Error()
			require.True(t, strings.Contains(msg, "untrusted surface") || strings.Contains(msg, "superadmin"),
				"%s refusal must come from the authority gate, got: %v", name, err)
		})
	}
}

// TestNoUnexpectedUnscopedTools is the coverage guard for the #1135 hole: every
// tool in the runtime catalog must declare a RequiredScope, be AgentOnly, be
// SuperadminOnly, or be one of the session-scoped tools whose ownership is
// enforced at the data-access layer. An unscoped, non-agent tool is implicitly
// open to any caller — the exact defect this change closes.
func TestNoUnexpectedUnscopedTools(t *testing.T) {
	svc := &mcp.Service{}
	svc.RegisterAgentToolHandler(fakeAgentHandler{})
	svc.RegisterMCPRegistryToolHandler(fakeRegistryHandler{})
	defs := svc.GetToolDefinitions()
	require.NotEmpty(t, defs)

	for _, def := range defs {
		if def.RequiredScope != "" || def.AgentOnly || def.SuperadminOnly {
			continue
		}
		if unscopeableToolNames[def.Name] {
			continue
		}
		t.Errorf("tool %q declares no RequiredScope and no AgentOnly/SuperadminOnly marker", def.Name)
	}
}

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
