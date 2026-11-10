package githubapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// staticReviewToolsHandler supplies the real review-tool definitions to an MCP
// service without a database. Every method the authority path needs is local;
// dispatch is recorded and never allowed to succeed.
type staticReviewToolsHandler struct {
	calls int
}

func (h *staticReviewToolsHandler) HandlesGitHubAppTool(name string) bool {
	return name == toolPRDiff || name == toolReviewSubmit
}

func (h *staticReviewToolsHandler) ExecuteGitHubAppTool(context.Context, string, string, map[string]any) (*mcp.ToolResult, error) {
	h.calls++
	return nil, errReviewDispatchReached
}

var errReviewDispatchReached = errors.New("review tool dispatch reached; the authority gate should have refused")

const testReviewProjectID = "11111111-1111-1111-1111-111111111111"

var reviewToolNames = []string{toolPRDiff, toolReviewSubmit}

// TestGitHubAppReviewToolsAreAgentOnlyInProjectCatalog pins the declared
// authority of the real definitions and proves they reach the project catalog
// (the catalog tools/call resolves) even with no agent handler registered.
func TestGitHubAppReviewToolsAreAgentOnlyInProjectCatalog(t *testing.T) {
	svc := &mcp.Service{}
	svc.RegisterGitHubAppToolHandler(&staticReviewToolsHandler{})

	defs := svc.GetToolDefinitionsForProject(context.Background(), testReviewProjectID)
	byName := make(map[string]mcp.ToolDefinition, len(defs))
	for _, def := range defs {
		byName[def.Name] = def
	}
	for _, name := range reviewToolNames {
		def, ok := byName[name]
		require.True(t, ok, "%s must be in the project catalog", name)
		require.True(t, def.AgentOnly, "%s must declare AgentOnly", name)
		require.Empty(t, def.RequiredScope,
			"%s is AgentOnly; a RequiredScope alongside it is unreachable on every entrypoint (#1143)", name)
	}
}

// TestGitHubAppReviewToolsRefuseUntrustedCallers is the blocking regression
// test: neither a plain untrusted caller nor a transport-enforced caller may
// reach either review tool. The transport-enforced case is the reviewer's
// reproduction — an HTTP transport that could not resolve the dynamic
// definition claims transport-enforced authority, and the seam must still refuse
// because AgentOnly outranks TransportEnforced.
func TestGitHubAppReviewToolsRefuseUntrustedCallers(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
	}{
		{name: "untrusted in-process run", ctx: context.Background()},
		{name: "transport-enforced but untrusted", ctx: mcp.ContextWithTransportEnforced(context.Background())},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &staticReviewToolsHandler{}
			svc := &mcp.Service{}
			svc.RegisterGitHubAppToolHandler(handler)

			for _, name := range reviewToolNames {
				args := map[string]any{
					"repo": "owner/repo", "number": float64(7), "head_sha": "abc1234",
					"verdict": "MERGE", "summary": "injected approval",
				}
				_, err := svc.ExecuteTool(tc.ctx, testReviewProjectID, name, args)
				require.Error(t, err, "%s must be refused", name)
				require.Contains(t, err.Error(), "agent-only", "%s refusal must name the agent-only boundary, got: %v", name, err)
			}
			require.Zero(t, handler.calls, "no review tool may reach dispatch without a trusted run")
		})
	}
}

// TestGitHubAppReviewToolsSurviveIndexEviction reproduces the reviewer's
// sequence: a tools/call for an unknown name rebuilds the process-global index
// from the static catalog and evicts the dynamic tools. The authority decision
// must resolve the project-scoped definition anyway (GetToolByNameForProject),
// so the review tools stay gated instead of falling through with toolDef == nil.
func TestGitHubAppReviewToolsSurviveIndexEviction(t *testing.T) {
	handler := &staticReviewToolsHandler{}
	svc := &mcp.Service{}
	svc.RegisterGitHubAppToolHandler(handler)

	// Evict: a miss rebuilds toolIndex without the handler-supplied tools.
	require.Nil(t, svc.GetToolByName("zzz-unknown-tool"))

	_, err := svc.ExecuteTool(context.Background(), testReviewProjectID, toolReviewSubmit, map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": "abc1234",
		"verdict": "MERGE", "summary": "injected approval",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "agent-only", "got: %v", err)
	require.Zero(t, handler.calls)
}

// TestGitHubAppReviewToolsReachDispatchForTrustedRun pins the positive
// direction: a trusted in-process run passes the authority gate and reaches
// dispatch, so the fix does not over-correct the tool out of existence.
func TestGitHubAppReviewToolsReachDispatchForTrustedRun(t *testing.T) {
	handler := &staticReviewToolsHandler{}
	svc := &mcp.Service{}
	svc.RegisterGitHubAppToolHandler(handler)

	ctx := mcp.ContextWithTrustedInternal(context.Background(), true)
	_, err := svc.ExecuteTool(ctx, testReviewProjectID, toolReviewSubmit, map[string]any{})
	require.ErrorIs(t, err, errReviewDispatchReached)
	require.Equal(t, 1, handler.calls)
}
