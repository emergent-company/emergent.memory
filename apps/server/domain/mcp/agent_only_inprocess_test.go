package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExecuteToolAuthorityGate proves the in-process dispatch path — the one the
// ADK ToolPool reaches during an agent run — enforces the same per-tool authority
// the three HTTP transports enforce before dispatch: AgentOnly (hidden) and the
// sensitive admin scope (RequiredScope:"admin"). This is the bypass the
// HTTP-only gate missed: ExecuteTool is the single dispatch point for every
// transport AND the in-process agent ToolPool (issue #994, mechanism 7).
//
// The test is hermetic: the authority gate reads the tool catalog (GetToolByName,
// populated from GetToolDefinitions) and the trust marker only — no DB.
func TestExecuteToolAuthorityGate(t *testing.T) {
	svc := &Service{}
	_ = svc.GetToolDefinitions() // populate the tool index GetToolByName reads

	projectID := "00000000-0000-0000-0000-000000000000"

	// Untrusted run context: no trust marker. This is the fail-closed state that
	// mirrors a webhook / A2A / agentcompat / public-share run, whose persisted
	// kb.agent_runs.trusted_internal is false (and whose ctx carries no marker).
	untrustedCtx := context.Background()

	// Trusted run context: the marker is set true for session UI / scheduler /
	// MCP-triggered agent runs.
	trustedCtx := ContextWithTrustedInternal(context.Background(), true)

	// Agent-only tools are refused for untrusted runs.
	agentOnly := []string{"web-fetch", "web-search-brave", "web-search-reddit"}
	for _, name := range agentOnly {
		t.Run("untrusted refused on agent-only "+name, func(t *testing.T) {
			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process %s by an untrusted run must be refused", name)
			require.Contains(t, err.Error(), "agent-only", "refusal must name the agent-only boundary")
		})
	}

	// Sensitive admin-scoped tools are refused for untrusted runs: token minting
	// (privilege escalation), provider config, project creation (issue #994,
	// RequiredScope:"admin" residual closed via TrustedInternal). Trace tools are
	// deliberately absent — they moved to SuperadminOnly (a stronger boundary) and
	// are covered by TestTraceToolsSuperadminGate, not this admin gate.
	adminScoped := []string{
		"token-list", "token-create", "token-get", "token-revoke",
		"provider-configure-project", "provider-models-list",
		"project-create",
	}
	for _, name := range adminScoped {
		t.Run("untrusted refused on admin-scoped "+name, func(t *testing.T) {
			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process %s by an untrusted run must be refused", name)
			require.Contains(t, err.Error(), "admin authority", "refusal must name the admin boundary")
		})
	}

	// No over-correction: a trusted run must pass the gate. web-fetch with an
	// empty URL returns a structured tool error (IsError, nil Go error), proving
	// the gate passed and dispatch reached the tool logic.
	t.Run("trusted run reaches web-fetch dispatch", func(t *testing.T) {
		result, err := svc.ExecuteTool(trustedCtx, projectID, "web-fetch", map[string]any{"url": ""})
		require.NoError(t, err, "a trusted run must reach web-fetch dispatch, not be refused")
		require.NotNil(t, result)
		require.True(t, result.IsError, "empty URL must yield a structured tool error, not a Go error")
	})

	// No over-correction on the admin path: a trusted run passes the gate and the
	// token tool fails on its own argument validation (not the gate). token-create
	// with empty args hits the name check before any token service call.
	t.Run("trusted run passes gate on token-create", func(t *testing.T) {
		_, err := svc.ExecuteTool(trustedCtx, projectID, "token-create", map[string]any{})
		require.Error(t, err, "token-create with empty args must error on validation")
		require.NotContains(t, err.Error(), "admin authority", "trusted run must not be gate-refused")
		require.Contains(t, err.Error(), "name", "must reach the tool's own argument validation")
	})

	// Ordinary project tools stay reachable for untrusted runs: they are neither
	// agent-only nor admin-scoped, so the gate must not flag them.
	t.Run("ordinary tools are not gated", func(t *testing.T) {
		for _, name := range []string{"project-get", "entity-query", "schema-list", "search-hybrid"} {
			def := svc.GetToolByName(name)
			require.NotNil(t, def, "%s must be in the catalog", name)
			require.False(t, def.AgentOnly, "%s must not be agent-only", name)
			require.NotEqual(t, "admin", def.RequiredScope, "%s must not be admin-scoped", name)
		}
	})
}
