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

// TestMapToOutputSchema_NilOrEmpty pins that an absent/empty outputSchema map
// (the persisted form of a tool that declared no outputSchema) maps back to nil,
// so pool reconstruction leaves OutputSchema unset.
func TestMapToOutputSchema_NilOrEmpty(t *testing.T) {
	assert.Nil(t, mapToOutputSchema(nil))
	assert.Nil(t, mapToOutputSchema(map[string]any{}))
}

// TestMapToOutputSchema_PreservesAbsentType covers Fix 2 end-to-end on the pool
// side: a valid JSON Schema may omit the top-level "type" yet still carry
// properties/required. mapToOutputSchema must NOT force Type:"object".
func TestMapToOutputSchema_PreservesAbsentType(t *testing.T) {
	m := map[string]any{
		"properties": map[string]any{
			"result": map[string]any{"type": "string"},
		},
		"required": []any{"result"},
	}
	schema := mapToOutputSchema(m)
	require.NotNil(t, schema)
	assert.Equal(t, "", schema.Type, "absent top-level type must be preserved, not forced to object")
	require.Contains(t, schema.Properties, "result")
	assert.Equal(t, "string", schema.Properties["result"].Type)
	assert.Equal(t, []string{"result"}, schema.Required)
}

// TestMapToOutputSchema_WithType verifies the normal typed path round-trips.
func TestMapToOutputSchema_WithType(t *testing.T) {
	m := map[string]any{"type": "object"}
	schema := mapToOutputSchema(m)
	require.NotNil(t, schema)
	assert.Equal(t, "object", schema.Type)
}

// TestExtractRelayToolDefs_PreservesOutputSchema covers Fix 2 on the relay
// parsing path: a type-less but declared outputSchema must survive, and a
// genuinely absent one must stay nil.
func TestExtractRelayToolDefs_PreservesOutputSchema(t *testing.T) {
	toolsMap := map[string]any{
		"tools": []any{
			map[string]any{
				"name":        "typed_tool",
				"description": "has output schema",
				"outputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
				},
			},
			map[string]any{
				"name":        "typeless_tool",
				"description": "type-less but declared",
				"outputSchema": map[string]any{
					"properties": map[string]any{"result": map[string]any{"type": "string"}},
					"required":   []any{"result"},
				},
			},
			map[string]any{
				"name":        "no_schema_tool",
				"description": "no output schema",
			},
		},
	}

	defs, err := extractRelayToolDefs(toolsMap)
	require.NoError(t, err)
	require.Len(t, defs, 3)

	typed := defs[0]
	require.NotNil(t, typed.OutputSchema)
	assert.Equal(t, "object", typed.OutputSchema.Type)

	typeless := defs[1]
	require.NotNil(t, typeless.OutputSchema, "type-less outputSchema must be preserved")
	assert.Equal(t, "", typeless.OutputSchema.Type)
	assert.Contains(t, typeless.OutputSchema.Properties, "result")

	assert.Nil(t, defs[2].OutputSchema, "absent outputSchema must stay nil")
}

// TestBuildCache_RelayToolCopiesOutputSchema pins Fix 1/Fix 2 on the relay pool
// path: extractRelayToolDefs parses outputSchema and the relay pool
// reconstruction copies it onto the pooled ToolDefinition.
func TestBuildCache_RelayToolCopiesOutputSchema(t *testing.T) {
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
					"outputSchema": map[string]any{
						"properties": map[string]any{"reminders": map[string]any{"type": "array"}},
					},
				},
			},
		},
	})

	tp := NewToolPool(ToolPoolConfig{
		MCPService:   &mcp.Service{},
		RelayService: relay,
		Logger:       logger,
	})

	cache := tp.getOrBuildCache(relayTestProjectID)
	require.NotNil(t, cache)

	const prefixed = "mcj-mini-connector_reminders_list"
	td, ok := cache.toolDefs[prefixed]
	require.True(t, ok)
	require.NotNil(t, td.OutputSchema, "relay pool reconstruction must copy outputSchema")
	assert.Equal(t, "", td.OutputSchema.Type, "type-less relay outputSchema must be preserved")
	assert.Contains(t, td.OutputSchema.Properties, "reminders")
}
