package agents

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPropertyDefFromMap(t *testing.T) {
	t.Run("copies type and description", func(t *testing.T) {
		def := PropertyDefFromMap(map[string]any{
			"type":        "string",
			"description": "Display name",
			"default":     "x",
		})
		assert.Equal(t, "string", def.Type)
		assert.Equal(t, "Display name", def.Description)
		assert.Nil(t, def.Default, "default is read by the caller that applies defaults")
		assert.Nil(t, def.Items)
	})

	t.Run("keeps a scalar items schema", func(t *testing.T) {
		def := PropertyDefFromMap(map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		})
		require.NotNil(t, def.Items)
		assert.Equal(t, "string", def.Items.Type)
	})

	t.Run("keeps a nested array items schema", func(t *testing.T) {
		def := PropertyDefFromMap(map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "number"},
			},
		})
		require.NotNil(t, def.Items)
		assert.Equal(t, "array", def.Items.Type)
		require.NotNil(t, def.Items.Items)
		assert.Equal(t, "number", def.Items.Items.Type)
	})

	t.Run("keeps an object items schema's type", func(t *testing.T) {
		def := PropertyDefFromMap(map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":       "object",
				"properties": map[string]any{"option": map[string]any{"type": "string"}},
			},
		})
		require.NotNil(t, def.Items)
		assert.Equal(t, "object", def.Items.Type)
	})

	t.Run("no items yields a nil element schema", func(t *testing.T) {
		def := PropertyDefFromMap(map[string]any{"type": "array"})
		assert.Nil(t, def.Items)
	})
}

// A schema pack decoded into PropertyDef directly must carry the same items
// schema as the map-based decoder, since both shapes reach the validator.
func TestPropertyDefJSONItems(t *testing.T) {
	var def PropertyDef
	require.NoError(t, json.Unmarshal([]byte(`{"type":"array","items":{"type":"integer"}}`), &def))
	require.NotNil(t, def.Items)
	assert.Equal(t, "integer", def.Items.Type)

	var nested PropertyDef
	require.NoError(t, json.Unmarshal([]byte(`{"type":"array","items":{"type":"array","items":{"type":"number"}}}`), &nested))
	require.NotNil(t, nested.Items)
	require.NotNil(t, nested.Items.Items)
	assert.Equal(t, "number", nested.Items.Items.Type)
}

// Non-object items forms are valid JSON Schema that this codebase does not
// interpret; they must not fail the decode of the property or its type.
func TestPropertyDefJSONTolerantItems(t *testing.T) {
	for _, raw := range []string{
		`{"type":"array","items":false}`,
		`{"type":"array","items":[{"type":"string"}]}`,
		`{"type":"array","items":null}`,
	} {
		var def PropertyDef
		require.NoError(t, json.Unmarshal([]byte(raw), &def), raw)
		assert.Equal(t, "array", def.Type, raw)
		// A non-object items (false, a tuple array) decodes to an empty
		// definition while null leaves the pointer nil; the invariant that
		// matters is "declares no element type".
		if def.Items != nil {
			assert.Empty(t, def.Items.Type, raw)
		}
	}
}

func TestPropertyDefPromptTypeLabel(t *testing.T) {
	assert.Equal(t, "string", PropertyDef{}.promptTypeLabel())
	assert.Equal(t, "number", PropertyDef{Type: "number"}.promptTypeLabel())
	assert.Equal(t, "array", PropertyDef{Type: "array"}.promptTypeLabel())
	assert.Equal(t, "array of string", PropertyDef{
		Type:  "array",
		Items: &PropertyDef{Type: "string"},
	}.promptTypeLabel())
	assert.Equal(t, "array", PropertyDef{
		Type:  "array",
		Items: &PropertyDef{Type: "array", Items: &PropertyDef{Type: "string"}},
	}.promptTypeLabel(), "nested arrays keep the plain type label")
}
