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

	// Sensitive admin-scoped tools are refused for untrusted runs via the
	// superadmin_full gate (issue #1018): their `admin` scope is token-only (no
	// project role maps to it), so no trusted session run can hold it, and the
	// in-process bar is raised to the identity-based superadmin_full grant.
	sensitiveAdminScoped := []string{
		"token-list", "token-create", "token-get", "token-revoke",
		"provider-configure-project",
		"project-create",
	}
	for _, name := range sensitiveAdminScoped {
		t.Run("untrusted refused on sensitive admin "+name, func(t *testing.T) {
			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process %s by an untrusted run must be refused", name)
			require.Contains(t, err.Error(), "superadmin", "sensitive admin %s refusal must name the superadmin boundary", name)
		})
	}

	// The read-only admin-scoped tool is not in the sensitive subset and stays at
	// the trusted-internal bar.
	t.Run("untrusted refused on admin-scoped provider-models-list", func(t *testing.T) {
		_, err := svc.ExecuteTool(untrustedCtx, projectID, "provider-models-list", map[string]any{})
		require.Error(t, err, "in-process provider-models-list by an untrusted run must be refused")
		require.Contains(t, err.Error(), "admin authority", "refusal must name the admin boundary")
	})

	// Superadmin-only tools (trace-*) refuse via the superadmin gate.
	for _, name := range []string{"trace-list", "trace-get"} {
		t.Run("untrusted refused on superadmin "+name, func(t *testing.T) {
			_, err := svc.ExecuteTool(untrustedCtx, projectID, name, map[string]any{})
			require.Error(t, err, "in-process %s by an untrusted run must be refused", name)
			require.Contains(t, err.Error(), "superadmin", "superadmin %s refusal must name the superadmin boundary", name)
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

	// No over-correction on the admin path: a trusted run passes the gate on the
	// NON-sensitive admin tool (provider-models-list), whose own argument
	// validation fires (not the gate). It is read-only and left at the
	// trusted-internal bar.
	t.Run("trusted run passes gate on provider-models-list", func(t *testing.T) {
		_, err := svc.ExecuteTool(trustedCtx, projectID, "provider-models-list", map[string]any{})
		require.Error(t, err, "provider-models-list with empty args must error on validation")
		require.NotContains(t, err.Error(), "admin authority", "trusted run must not be gate-refused")
		require.Contains(t, err.Error(), "provider_name", "must reach the tool's own argument validation")
	})

	// A trusted run still cannot reach the sensitive admin tools without a
	// superadmin grant: the `admin` scope is token-only, so trusted-internal is
	// the wrong axis for token minting / provider config / project creation
	// (issue #1018).
	t.Run("trusted non-superadmin run is refused on token-create", func(t *testing.T) {
		_, err := svc.ExecuteTool(trustedCtx, projectID, "token-create", map[string]any{})
		require.Error(t, err, "a trusted non-superadmin run must be refused on token-create")
		require.Contains(t, err.Error(), "superadmin", "refusal must come from the superadmin gate")
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
