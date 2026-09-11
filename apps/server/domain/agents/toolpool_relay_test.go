package agents

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcprelay"
)

const relayTestProjectID = "00000000-0000-0000-0000-000000000001"

// buildRelayTestPool creates a real ToolPool backed by a real relay service
// with a single registered relay instance exposing one tool. Tools are supplied
// in the same tools/list shape the relay WSS registration persists.
func buildRelayTestPool(t *testing.T) (*ToolPool, *mcprelay.Service) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	relay := mcprelay.NewService(logger)
	relay.Register(&mcprelay.Session{
		ProjectID:  relayTestProjectID,
		InstanceID: "mcj-mini-connector",
		Tools: map[string]any{
			"tools": []any{
				map[string]any{
					"name":        "reminders_list",
					"description": "List reminders",
				},
			},
		},
	})

	tp := NewToolPool(ToolPoolConfig{
		MCPService:   &mcp.Service{},
		RelayService: relay,
		Logger:       logger,
	})
	return tp, relay
}

// TestBuildCache_RelayToolRegisteredWithInstancePrefix pins the fix for the
// regression where relay tools were pooled under their bare MCP name
// ("reminders_list") while agent whitelists and relay routing both use the
// instance-prefixed form ("mcj-mini-connector_reminders_list").
func TestBuildCache_RelayToolRegisteredWithInstancePrefix(t *testing.T) {
	tp, _ := buildRelayTestPool(t)

	cache := tp.getOrBuildCache(relayTestProjectID)
	require.NotNil(t, cache)

	const prefixed = "mcj-mini-connector_reminders_list"

	td, ok := cache.toolDefs[prefixed]
	require.True(t, ok, "relay tool must be pooled under the instance-prefixed name")
	assert.Equal(t, prefixed, td.Name)
	assert.Equal(t, "List reminders", td.Description)

	assert.Contains(t, cache.toolNames, prefixed,
		"prefixed relay tool must appear in the ordered pool names")
	assert.Equal(t, "mcj-mini-connector", cache.relayToolInstance[prefixed],
		"prefixed relay tool must map back to its instance for routing")

	assert.NotContains(t, cache.toolDefs, "reminders_list",
		"bare relay tool name must not be registered as a pool key")
	assert.NotContains(t, cache.toolNames, "reminders_list",
		"bare relay tool name must not leak into the ordered pool names")
}

// TestResolveTools_RelayToolPrefixedWhitelist_Selected verifies that the
// prefixed whitelist entry stored by the gateway agent picker resolves to a
// wrapped ADK tool. MCPService is non-nil (required for wrapping), so the
// hidden set_session_title builtin is also injected.
func TestResolveTools_RelayToolPrefixedWhitelist_Selected(t *testing.T) {
	tp, _ := buildRelayTestPool(t)

	agentDef := &AgentDefinition{
		Name:  "reminders-agent",
		Tools: []string{"mcj-mini-connector_reminders_list"},
	}

	tools, err := tp.ResolveTools(relayTestProjectID, agentDef, 0, DefaultMaxDepth)
	require.NoError(t, err)

	var names []string
	for _, tl := range tools {
		if tl == nil {
			continue
		}
		names = append(names, tl.Name())
	}

	assert.Contains(t, names, "mcj-mini-connector_reminders_list",
		"prefixed relay whitelist entry must resolve to the wrapped ADK tool")
	assert.NotContains(t, names, "reminders_list",
		"bare relay name must never be offered to the model")
	assert.Contains(t, names, "set_session_title",
		"hidden builtin is injected when MCPService is non-nil")
}
