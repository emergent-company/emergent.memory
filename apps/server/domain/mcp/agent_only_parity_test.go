package mcp_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
)

// fakeRegistryHandler is a safe stand-in for the mcpregistry handler: it returns
// the real tool definitions (so AgentOnly/admin flags are the production ones)
// but never touches a DB/service on Execute*.
type fakeRegistryHandler struct{}

func (fakeRegistryHandler) GetMCPRegistryToolDefinitions() []mcp.ToolDefinition {
	return (&mcpregistry.MCPRegistryToolHandler{}).GetMCPRegistryToolDefinitions()
}
func (fakeRegistryHandler) ExecuteListMCPServers(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteGetMCPServer(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteCreateMCPServer(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteUpdateMCPServer(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteDeleteMCPServer(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteToggleMCPServerTool(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteSyncMCPServerTools(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteSearchMCPRegistry(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteGetMCPRegistryServer(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteInstallMCPFromRegistry(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ExecuteInspectMCPServer(context.Context, string, map[string]any) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{}, nil
}
func (fakeRegistryHandler) ResolveBuiltinToolSettings(context.Context, string, string) (bool, map[string]any, string, error) {
	return false, nil, "", nil
}
func (fakeRegistryHandler) ResolveBuiltinToolConfig(context.Context, string, string) (map[string]any, string, error) {
	return nil, "", nil
}

// TestAgentOnlyParityHTTPInProcess asserts the in-process agent-only gate
// protects EXACTLY the same set the HTTP transports hide. Both read AgentOnly
// from mcp.Service.GetToolByName, so this pins that the two cannot drift —
// including the handler-provided mcpregistry admin tools that a parallel static
// set would miss (issue #994, mechanism 7).
func TestAgentOnlyParityHTTPInProcess(t *testing.T) {
	svc := &mcp.Service{}
	svc.RegisterMCPRegistryToolHandler(fakeRegistryHandler{})

	// Enumerate the agent-only set from the SAME catalog the HTTP transports
	// read (GetToolByName, backed by GetToolDefinitions which includes the
	// handler-provided mcpregistry tools).
	var agentOnly []string
	for _, def := range svc.GetToolDefinitions() {
		if !def.AgentOnly {
			continue
		}
		agentOnly = append(agentOnly, def.Name)
		got := svc.GetToolByName(def.Name)
		require.NotNil(t, got, "agent-only %q must resolve via GetToolByName", def.Name)
		require.True(t, got.AgentOnly, "agent-only %q: GetToolByName disagrees", def.Name)
	}

	// The full agent-only set: 3 web tools + 7 mcpregistry admin tools.
	require.ElementsMatch(t, []string{
		"web-fetch", "web-search-brave", "web-search-reddit",
		"mcp-server-list", "mcp-server-get", "mcp-server-create",
		"update_mcp_server", "mcp-server-delete", "toggle_mcp_server_tool",
		"sync_mcp_server_tools",
	}, agentOnly, "agent-only set must be exactly the web + mcpregistry tools")
}

// TestExecuteToolMCPRegistryAgentOnlyGate proves the handler-provided mcpregistry
// admin tools (mcp-server-create = SSRF, sync_mcp_server_tools = config
// manipulation) are refused for untrusted in-process runs and reachable for
// trusted runs (no over-correction).
func TestExecuteToolMCPRegistryAgentOnlyGate(t *testing.T) {
	svc := &mcp.Service{}
	svc.RegisterMCPRegistryToolHandler(fakeRegistryHandler{})
	_ = svc.GetToolDefinitions()

	projectID := "00000000-0000-0000-0000-000000000000"
	untrustedCtx := context.Background()
	trustedCtx := mcp.ContextWithTrustedInternal(context.Background(), true)

	for _, name := range []string{"mcp-server-create", "sync_mcp_server_tools", "mcp-server-list"} {
		t.Run("untrusted refused on "+name, func(t *testing.T) {
			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process %s by an untrusted run must be refused", name)
		})
	}

	t.Run("trusted reaches mcp-server-create dispatch (no over-correction)", func(t *testing.T) {
		_, err := svc.ExecuteTool(trustedCtx, projectID, "mcp-server-create", map[string]any{})
		require.NoError(t, err, "a trusted run must reach mcp-server-create dispatch, not be refused")
	})
}
