package graph

import (
	"encoding/json"
	"testing"
)

// TestParseObjectTypeSchemasToMap verifies that parseObjectTypeSchemasToMap
// handles both the array format (user YAML files) and map format (epf-engine v3 /
// blueprint seeds). This mirrors the same fix applied to schemas/repository.go —
// the graph service reads object_type_schemas via this helper so that both
// compiled-types and the object_type_not_allowed type-check use the same logic.
func TestParseObjectTypeSchemasToMap(t *testing.T) {
	t.Run("array format", func(t *testing.T) {
		data := json.RawMessage(`[
			{"name":"Belief","label":"Belief","description":"A belief","properties":{"text":{"type":"string"}}},
			{"name":"Person","label":"Person"}
		]`)

		got := parseObjectTypeSchemasToMap(data)
		if got == nil {
			t.Fatal("expected non-nil result for array format")
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 types, got %d", len(got))
		}
		if _, ok := got["Belief"]; !ok {
			t.Error("expected key 'Belief'")
		}
		if _, ok := got["Person"]; !ok {
			t.Error("expected key 'Person'")
		}
	})

	t.Run("map format (epf-engine v3 / blueprint)", func(t *testing.T) {
		data := json.RawMessage(`{
			"Belief": {"label":"Belief","description":"A belief","properties":{"text":{"type":"string"}}},
			"Person": {"label":"Person"}
		}`)

		got := parseObjectTypeSchemasToMap(data)
		if got == nil {
			t.Fatal("expected non-nil result for map format")
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 types, got %d", len(got))
		}
		if _, ok := got["Belief"]; !ok {
			t.Error("expected key 'Belief'")
		}
	})

	t.Run("nil on empty", func(t *testing.T) {
		if parseObjectTypeSchemasToMap(nil) != nil {
			t.Error("expected nil for empty input")
		}
	})

	t.Run("nil on invalid json", func(t *testing.T) {
		if parseObjectTypeSchemasToMap(json.RawMessage(`not-json`)) != nil {
			t.Error("expected nil for invalid JSON")
		}
	})
}
