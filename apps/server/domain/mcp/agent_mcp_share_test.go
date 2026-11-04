package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustJSON marshals a value for a JSON-RPC request/param body.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// The endpoint credential scope set is minimized to the marker plus
// projects:read. Loopback tool execution bypasses credential scopes
// (ToolPool.CallTool -> Service.ExecuteTool), so the previous read-only grants
// were unnecessary yet still valid REST scopes. They must not be present.
func TestAgentShareScopes(t *testing.T) {
	assert.ElementsMatch(t, []string{AgentCallScope, "projects:read"}, agentShareScopes)

	for _, overGrant := range []string{"data:read", "schema:read", "agents:read", "chat:use"} {
		assert.NotContains(t, agentShareScopes, overGrant, "%s broadens REST access beyond the agent endpoint", overGrant)
	}
	for _, writeScope := range []string{"data:write", "agents:write", "schema:write", "admin", "admin:all"} {
		assert.NotContains(t, agentShareScopes, writeScope)
	}
}

func TestHasAgentCallScope(t *testing.T) {
	assert.True(t, hasAgentCallScope([]string{"data:read", AgentCallScope}))
	assert.False(t, hasAgentCallScope([]string{"data:read", "admin:all"}))
	assert.False(t, hasAgentCallScope(nil))
}

// The key token name carries the label and is truncated to fit
// core.api_tokens.name varchar(255).
func TestAgentMCPKeyTokenNameCapped(t *testing.T) {
	name := agentMCPKeyTokenName(strings.Repeat("x", 500))
	assert.True(t, strings.HasPrefix(name, "Agent MCP Key: "))
	assert.LessOrEqual(t, len(name), 255)

	assert.Equal(t, "Agent MCP Key: Team A", agentMCPKeyTokenName("Team A"))
}
