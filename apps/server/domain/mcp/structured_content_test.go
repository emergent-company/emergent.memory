package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnvelopeResultSetsStructuredContent pins that the structuredContent of an
// envelope result is the SAME JSON object serialized into the text content block
// (issue #586): programmatic consumers can read structuredContent and get an
// object byte-identical to the human-readable text.
func TestEnvelopeResultSetsStructuredContent(t *testing.T) {
	data := map[string]any{
		"results": []any{
			map[string]any{"ok": true, "index": 0, "entity": map[string]any{"id": "e1", "type": "Note"}},
		},
	}
	meta := map[string]any{"created": 1, "failed": 0, "total": 1}

	res, err := envelopeResult(true, data, meta, "")
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Content, 1)

	// structuredContent must be present and match the text JSON exactly.
	require.NotNil(t, res.StructuredContent, "structuredContent must be set for envelope results")
	structured, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	assert.JSONEq(t, res.Content[0].Text, string(structured),
		"structuredContent must serialize to the same JSON as the text content block")

	// The envelope shape is preserved in structured content.
	assert.Equal(t, true, res.StructuredContent["ok"])
	assert.NotNil(t, res.StructuredContent["data"])
	assert.NotNil(t, res.StructuredContent["meta"])
}

// TestEnvelopeOutputSchemaShape pins the shared envelope output schema:
// a root object with ok/data required and optional error/meta properties.
func TestEnvelopeOutputSchemaShape(t *testing.T) {
	schema := envelopeOutputSchema()
	require.NotNil(t, schema)
	assert.Equal(t, "object", schema.Type)
	assert.Contains(t, schema.Required, "ok")
	assert.Contains(t, schema.Required, "data")
	require.Contains(t, schema.Properties, "ok")
	assert.Equal(t, "boolean", schema.Properties["ok"].Type)
	require.Contains(t, schema.Properties, "data")
	assert.Equal(t, "object", schema.Properties["data"].Type)
	require.Contains(t, schema.Properties, "error")
	assert.Equal(t, "string", schema.Properties["error"].Type)
	require.Contains(t, schema.Properties, "meta")
	assert.Equal(t, "object", schema.Properties["meta"].Type)
}

// TestToolsListDeclaresOutputSchema checks that envelope-producing tools expose
// an `outputSchema` field in the serialized tools/list result (not just on the
// Go struct), matching the MCP 2025-06-18 shape.
func TestToolsListDeclaresOutputSchema(t *testing.T) {
	svc := &Service{}
	toolMap := make(map[string]ToolDefinition)
	for _, tool := range svc.GetToolDefinitions() {
		toolMap[tool.Name] = tool
	}

	for _, name := range phase1ResultTools {
		tool, ok := toolMap[name]
		require.Truef(t, ok, "envelope tool %q must be registered", name)
		require.NotNilf(t, tool.OutputSchema, "envelope tool %q must declare outputSchema", name)

		// Serialize the tool definition as it would appear in tools/list.
		raw, err := json.Marshal(tool)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m))

		os, ok := m["outputSchema"].(map[string]any)
		require.Truef(t, ok, "tool %q must serialize an outputSchema object", name)
		assert.Equal(t, "object", os["type"])
		props, _ := os["properties"].(map[string]any)
		require.Contains(t, props, "ok", "outputSchema.properties.ok")
		require.Contains(t, props, "data", "outputSchema.properties.data")
		required, _ := os["required"].([]any)
		assert.Contains(t, required, "ok")
		assert.Contains(t, required, "data")
	}
}

// TestToolsCallSerializesStructuredContent checks that a tool result with
// structuredContent serializes it alongside the required content block.
func TestToolsCallSerializesStructuredContent(t *testing.T) {
	result := &ToolResult{
		Content:           []ContentBlock{{Type: "text", Text: `{"ok":true,"data":{"entity_id":"e1"}}`}},
		StructuredContent: map[string]any{"ok": true, "data": map[string]any{"entity_id": "e1"}},
	}

	raw, err := json.Marshal(result)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	// content remains present (backward compat)...
	require.Contains(t, m, "content", "content is REQUIRED by MCP")
	// ...and structuredContent is emitted alongside it.
	sc, ok := m["structuredContent"].(map[string]any)
	require.True(t, ok, "structuredContent must be a JSON object")
	assert.Equal(t, true, sc["ok"])

	// isError must not be set for a successful call.
	_, hasIsError := m["isError"]
	assert.False(t, hasIsError, "isError must be omitted on success")
}

// TestWrapResultStructuredContentIsObjectOnly checks that wrapResult sets
// structuredContent for object payloads and leaves it nil for arrays/primitives
// (MCP 2025-06-18 requires a root object; non-object shapes keep the text block).
func TestWrapResultStructuredContentIsObjectOnly(t *testing.T) {
	svc := &Service{}

	obj, err := svc.wrapResult(map[string]any{"success": true, "count": 3})
	require.NoError(t, err)
	require.NotNil(t, obj.StructuredContent, "object payload must produce structuredContent")
	assert.Equal(t, true, obj.StructuredContent["success"])

	arr, err := svc.wrapResult([]any{1, 2, 3})
	require.NoError(t, err)
	assert.Nil(t, arr.StructuredContent, "array payload must not produce structuredContent")

	prim, err := svc.wrapResultCompact("plain string")
	require.NoError(t, err)
	assert.Nil(t, prim.StructuredContent, "primitive payload must not produce structuredContent")
}
