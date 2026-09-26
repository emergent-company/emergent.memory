package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAgentOnlyToolClassification pins which tools are gated on agent-only
// visibility (issue #994, mechanism 7): they are reachable in-process ONLY from
// a trusted/internal run, never via an external surface, matching the HTTP
// transports which hide them entirely.
func TestAgentOnlyToolClassification(t *testing.T) {
	agentOnly := []string{
		"web-search-brave",
		"web-fetch",
		"web-search-reddit",
	}
	for _, name := range agentOnly {
		tool := toolByName(t, name)
		if !tool.AgentOnly {
			t.Errorf("tool %q must be AgentOnly, got false", name)
		}
	}

	// Ordinary tools must not be agent-only (they remain reachable over HTTP).
	ordinary := []string{"project-get", "entity-query", "schema-list"}
	for _, name := range ordinary {
		tool := toolByName(t, name)
		if tool.AgentOnly {
			t.Errorf("tool %q must NOT be AgentOnly", name)
		}
	}
}

// TestExecuteToolAgentOnlyGate proves the in-process dispatch path — the one the
// ADK ToolPool reaches during an agent run — enforces the AgentOnly boundary
// directly in Service.ExecuteTool. This is the bypass the HTTP-only gate missed:
// ExecuteTool is the single dispatch point for every transport AND the
// in-process agent ToolPool, so the AgentOnly check must live here (issue #994,
// mechanism 7). The test is hermetic: the agent-only gate reads no DB.
func TestExecuteToolAgentOnlyGate(t *testing.T) {
	svc := &Service{}
	projectID := "00000000-0000-0000-0000-000000000000"

	// Untrusted run context: no trust marker. This is the fail-closed state that
	// mirrors a webhook / A2A / agentcompat / public-share run, whose persisted
	// kb.agent_runs.trusted_internal is false (and whose ctx carries no marker).
	untrustedCtx := context.Background()

	// Trusted run context: the marker is set true for session UI / scheduler /
	// MCP-triggered agent runs.
	trustedCtx := ContextWithTrustedInternal(context.Background(), true)

	t.Run("untrusted run is refused on web-fetch (in-process)", func(t *testing.T) {
		_, err := svc.ExecuteTool(untrustedCtx, projectID, "web-fetch", map[string]any{"url": ""})
		require.Error(t, err, "in-process web-fetch by an untrusted run must be refused")
	})

	t.Run("untrusted run is refused on web-search-brave (in-process)", func(t *testing.T) {
		_, err := svc.ExecuteTool(untrustedCtx, projectID, "web-search-brave", map[string]any{"query": "x"})
		require.Error(t, err, "in-process web-search-brave by an untrusted run must be refused")
	})

	t.Run("untrusted run is refused on web-search-reddit (in-process)", func(t *testing.T) {
		_, err := svc.ExecuteTool(untrustedCtx, projectID, "web-search-reddit", map[string]any{"query": "x"})
		require.Error(t, err, "in-process web-search-reddit by an untrusted run must be refused")
	})

	t.Run("trusted run reaches web-fetch dispatch (no over-correction)", func(t *testing.T) {
		// web-fetch with an empty URL returns a structured tool error (IsError,
		// nil Go error), proving the gate passed and dispatch reached the tool
		// logic rather than being refused.
		result, err := svc.ExecuteTool(trustedCtx, projectID, "web-fetch", map[string]any{"url": ""})
		require.NoError(t, err, "a trusted run must reach web-fetch dispatch, not be refused")
		require.NotNil(t, result)
		require.True(t, result.IsError, "empty URL must yield a structured tool error, not a Go error")
	})
}
