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

	t.Run("copies enum values", func(t *testing.T) {
		def := PropertyDefFromMap(map[string]any{
			"type": "string",
			"enum": []any{"draft", "published"},
		})
		assert.Equal(t, []any{"draft", "published"}, def.Enum)
	})

	t.Run("accepts a string-slice enum", func(t *testing.T) {
		def := PropertyDefFromMap(map[string]any{
			"type": "string",
			"enum": []string{"low", "high"},
		})
		assert.Equal(t, []any{"low", "high"}, def.Enum)
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

// A schema pack decoded directly into PropertyDef must carry the same enum the
// map-based decoder does, including numeric and boolean values.
func TestPropertyDefJSONEnum(t *testing.T) {
	var def PropertyDef
	require.NoError(t, json.Unmarshal([]byte(`{"type":"string","enum":["draft","published"]}`), &def))
	assert.Equal(t, []any{"draft", "published"}, def.Enum)

	var numeric PropertyDef
	require.NoError(t, json.Unmarshal([]byte(`{"type":"integer","enum":[1,2,3]}`), &numeric))
	assert.Equal(t, []any{float64(1), float64(2), float64(3)}, numeric.Enum)
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

// A declared format and bounds are enforced at write time, so the prompt must
// render them or it asks the model for a value it cannot know is invalid.
func TestPropertyDefPromptTypeLabel_Constraints(t *testing.T) {
	zero, ten := 0.0, 10.0

	assert.Equal(t, "string, format email", PropertyDef{Type: "string", Format: "email"}.promptTypeLabel())
	assert.Equal(t, "number, min 0", PropertyDef{Type: "number", Minimum: &zero}.promptTypeLabel())
	assert.Equal(t, "integer, max 10", PropertyDef{Type: "integer", Maximum: &ten}.promptTypeLabel())
	assert.Equal(t, "number, min 0, max 10", PropertyDef{Type: "number", Minimum: &zero, Maximum: &ten}.promptTypeLabel())
	assert.Equal(t, "string, format email, min 0", PropertyDef{Type: "string", Format: "email", Minimum: &zero}.promptTypeLabel())
	assert.Equal(t, "array of string, min 0, max 10", PropertyDef{
		Type:    "array",
		Items:   &PropertyDef{Type: "string"},
		Minimum: &zero,
		Maximum: &ten,
	}.promptTypeLabel(), "bounds declared on the array render after its element type")
}

// Enum values are enforced at write time too, so the prompt must name them or
// it asks the model for a value it cannot know is invalid.
func TestPropertyDefPromptTypeLabel_Enum(t *testing.T) {
	assert.Equal(t, "string, one of: draft, published", PropertyDef{
		Type: "string",
		Enum: []any{"draft", "published"},
	}.promptTypeLabel())
	assert.Equal(t, "integer, one of: 1, 2", PropertyDef{
		Type: "integer",
		Enum: []any{float64(1), float64(2)},
	}.promptTypeLabel())
	assert.Equal(t, "array of string, elements one of: a, b", PropertyDef{
		Type:  "array",
		Items: &PropertyDef{Type: "string", Enum: []any{"a", "b"}},
	}.promptTypeLabel())
	assert.Equal(t, "array of string, elements one of: \"a,b\", c", PropertyDef{
		Type:  "array",
		Items: &PropertyDef{Type: "string", Enum: []any{"a,b", "c"}},
	}.promptTypeLabel(), "ambiguous string values are quoted")
	assert.Equal(t, "array of string, elements one of: a, b", PropertyDef{
		Type:  "array",
		Enum:  []any{"whole-array-value"},
		Items: &PropertyDef{Type: "string", Enum: []any{"a", "b"}},
	}.promptTypeLabel(), "array-level enum is documentation-only and does not hide the item enum")
}
