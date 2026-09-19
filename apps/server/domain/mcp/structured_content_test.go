package mcp

import (
	"encoding/json"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/sessiontodos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnvelopeResultSetsStructuredContent pins that the structuredContent of an
// envelope result is the SAME JSON object serialized into the text content block
// (issue #586): programmatic consumers can read structuredContent and get the
// same JSON object as the human-readable text.
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

	// structuredContent must be present and encode the same JSON object as the
	// text block. The contract is semantic equivalence (assert.JSONEq), not
	// exact string/byte equality.
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

// TestToolsListDeclaresOutputSchema checks that every registered tool declares
// an `outputSchema` (MCP 2025-06-18) unless it is on the prose-only allowlist,
// and that envelope-producing tools keep the envelope output schema shape.
func TestToolsListDeclaresOutputSchema(t *testing.T) {
	svc := &Service{}
	tools := svc.GetToolDefinitions()
	toolMap := make(map[string]ToolDefinition, len(tools))
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	proseOnly := make(map[string]bool, len(proseOnlyTools))
	for _, name := range proseOnlyTools {
		proseOnly[name] = true
	}

	// Every non-allowlisted tool must declare a root-object output schema.
	for _, tool := range tools {
		if proseOnly[tool.Name] {
			assert.Nilf(t, tool.OutputSchema, "prose-only tool %q must NOT declare outputSchema", tool.Name)
			continue
		}
		require.NotNilf(t, tool.OutputSchema, "tool %q must declare outputSchema", tool.Name)
		assert.Equalf(t, "object", tool.OutputSchema.Type, "tool %q outputSchema must be an object", tool.Name)
	}

	// Per-tool envelope assertions (unchanged contract) for phase1ResultTools.
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

// TestWrapResultStructuredContentIsObjectOnly checks that wrapResult and
// wrapResultCompact always produce a root-object structuredContent (MCP
// 2025-06-18): objects map to themselves, arrays to {"results":[...]}, and
// primitives to {"value":...}.
func TestWrapResultStructuredContentIsObjectOnly(t *testing.T) {
	svc := &Service{}

	obj, err := svc.wrapResult(map[string]any{"success": true, "count": 3})
	require.NoError(t, err)
	require.NotNil(t, obj.StructuredContent, "object payload must produce structuredContent")
	assert.Equal(t, true, obj.StructuredContent["success"])
	assert.Equal(t, float64(3), obj.StructuredContent["count"])

	arr, err := svc.wrapResult([]any{1, 2, 3})
	require.NoError(t, err)
	require.NotNil(t, arr.StructuredContent, "array payload must produce a root-object structuredContent")
	assert.Equal(t, []any{float64(1), float64(2), float64(3)}, arr.StructuredContent["results"])

	prim, err := svc.wrapResultCompact("plain string")
	require.NoError(t, err)
	require.NotNil(t, prim.StructuredContent, "primitive payload must produce a root-object structuredContent")
	assert.Equal(t, "plain string", prim.StructuredContent["value"])
}

// TestStructuredContentFromJSON pins the truthfulness mapping of the shared
// marshalled-result → structuredContent converter (issue #586).
func TestStructuredContentFromJSON(t *testing.T) {
	t.Run("object maps to itself", func(t *testing.T) {
		sc := StructuredContentFromJSON([]byte(`{"a":1,"b":"x"}`))
		require.NotNil(t, sc)
		assert.Equal(t, float64(1), sc["a"])
		assert.Equal(t, "x", sc["b"])
	})

	t.Run("array wraps as results", func(t *testing.T) {
		sc := StructuredContentFromJSON([]byte(`[1,2,3]`))
		require.NotNil(t, sc)
		assert.Equal(t, []any{float64(1), float64(2), float64(3)}, sc["results"])
	})

	t.Run("primitive wraps as value", func(t *testing.T) {
		assert.Equal(t, "hi", StructuredContentFromJSON([]byte(`"hi"`))["value"])
		assert.Equal(t, float64(42), StructuredContentFromJSON([]byte(`42`))["value"])
		assert.Equal(t, true, StructuredContentFromJSON([]byte(`true`))["value"])
	})

	t.Run("nil and invalid map to nil", func(t *testing.T) {
		assert.Nil(t, StructuredContentFromJSON([]byte(`null`)))
		assert.Nil(t, StructuredContentFromJSON([]byte(`not json`)))
	})
}

// TestObjectOutputSchemaSerialization pins that objectOutputSchema emits
// additionalProperties:true and that an input schema without it does NOT emit
// the key (omitempty).
func TestObjectOutputSchemaSerialization(t *testing.T) {
	// objectOutputSchema must serialize additionalProperties:true.
	raw, err := json.Marshal(objectOutputSchema())
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, true, m["additionalProperties"])

	// A plain input schema (AdditionalProperties nil) must NOT emit the key.
	input := InputSchema{Type: "object", Properties: map[string]PropertySchema{}}
	rawInput, err := json.Marshal(input)
	require.NoError(t, err)
	assert.NotContains(t, string(rawInput), "additionalProperties")
}

// TestSessionTodoListStructuredContent pins that session-todo-list keeps the
// raw array as its text block while structuredContent wraps it as
// {"todos":[...]}.
func TestSessionTodoListStructuredContent(t *testing.T) {
	todos := []*sessiontodos.SessionTodo{
		{ID: "t1", Content: "first", Status: sessiontodos.StatusDraft},
		{ID: "t2", Content: "second", Status: sessiontodos.StatusInProgress},
	}
	res := sessionTodoListResult(todos)

	require.Len(t, res.Content, 1)
	// Text block stays the raw array.
	var arr []any
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].Text), &arr))
	require.Len(t, arr, 2)

	// structuredContent wraps the array as {"todos": [...]}.
	require.NotNil(t, res.StructuredContent)
	scTodos, ok := res.StructuredContent["todos"]
	require.True(t, ok, "structuredContent must carry a todos key")
	require.NotNil(t, scTodos)

	// Serializing structuredContent must equal {"todos": <text array>}.
	structuredJSON, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	assert.JSONEq(t, `{"todos":`+res.Content[0].Text+`}`, string(structuredJSON))
}

// TestAgentEndpointToolDefinitionsDeclareOutputSchema pins Fix 4: the four
// session tools return envelope-shaped results (via sessionEnvelope →
// envelopeResult), so they must declare the shared envelope output schema.
// call_agent returns a bare text reply (not an envelope), so it stays nil.
func TestAgentEndpointToolDefinitionsDeclareOutputSchema(t *testing.T) {
	defs := agentEndpointToolDefinitions("")

	var byName = make(map[string]ToolDefinition, len(defs))
	for _, d := range defs {
		byName[d.Name] = d
	}

	for _, name := range []string{
		agentStartSessionToolName,
		agentContinueSessionToolName,
		agentGetSessionToolName,
		agentListSessionsToolName,
	} {
		td, ok := byName[name]
		require.Truef(t, ok, "session tool %q must be present", name)
		require.NotNilf(t, td.OutputSchema, "session tool %q must declare outputSchema", name)
		assert.Equal(t, "object", td.OutputSchema.Type)
		assert.Contains(t, td.OutputSchema.Properties, "ok")
		assert.Contains(t, td.OutputSchema.Properties, "data")
	}

	// call_agent produces a bare text reply / agentRunErrorResult, not an
	// envelope, so it must NOT declare an outputSchema.
	callAgent, ok := byName[agentCallToolName]
	require.True(t, ok)
	assert.Nil(t, callAgent.OutputSchema, "call_agent must not declare an outputSchema (non-envelope result)")
}
