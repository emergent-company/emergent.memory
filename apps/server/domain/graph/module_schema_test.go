package graph

import (
	"encoding/json"
	"slices"
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

// TestGraphPathArraySchemaPreservesBoardWorkConfig pins the graph-path contract
// for the array storage format used by the flagship task-board blueprint:
// parseObjectTypeSchemasToMap → objectSchemaFromMap must carry
// boardEnabled/allowedStatuses (and the operational skip flags) through to
// agents.ObjectSchema, otherwise runtime work-status enforcement
// (validateTypeStatus / rejectAgentWorkStatusWrite) is silently inert.
func TestGraphPathArraySchemaPreservesBoardWorkConfig(t *testing.T) {
	// Verbatim array shape of blueprints/task-board/schemas/task-board.yaml.
	data := json.RawMessage(`[
		{
			"name":"Task",
			"label":"Task",
			"description":"A single unit of work tracked on the Kanban board.",
			"boardEnabled":true,
			"allowedStatuses":["ready","in_progress","review","revision","blocked","done"],
			"skipEmbeddings":true,
			"skipExtraction":true,
			"excludeFromSearch":true,
			"properties":{"title":{"type":"string"}}
		}
	]`)

	raws := parseObjectTypeSchemasToMap(data)
	raw, ok := raws["Task"]
	if !ok {
		t.Fatal("expected Task type from array-form schema")
	}
	var schemaMap map[string]any
	if err := json.Unmarshal(raw, &schemaMap); err != nil {
		t.Fatalf("unmarshal Task schema: %v", err)
	}
	task := objectSchemaFromMap("Task", "1.0.0", schemaMap)

	if !task.BoardEnabled {
		t.Error("graph path dropped boardEnabled from array-form schema; runtime status enforcement is inert")
	}
	want := []string{"ready", "in_progress", "review", "revision", "blocked", "done"}
	if !slices.Equal(task.AllowedStatuses, want) {
		t.Errorf("graph path AllowedStatuses = %v, want %v", task.AllowedStatuses, want)
	}
	if !task.SkipEmbeddings || !task.SkipExtraction || !task.ExcludeFromSearch {
		t.Errorf("graph path dropped skip flags: skipEmbeddings=%v skipExtraction=%v excludeFromSearch=%v",
			task.SkipEmbeddings, task.SkipExtraction, task.ExcludeFromSearch)
	}

	// End-to-end runtime enforcement: a status outside the declared set is
	// rejected, one inside is accepted.
	invalid := "shipped"
	if err := validateTypeStatus(&task.ObjectTypeWorkConfig, &invalid, nil); err == nil {
		t.Errorf("validateTypeStatus accepted out-of-set status %q; enforcement is inert", invalid)
	}
	valid := "ready"
	if err := validateTypeStatus(&task.ObjectTypeWorkConfig, &valid, nil); err != nil {
		t.Errorf("validateTypeStatus rejected allowed status %q: %v", valid, err)
	}
	// Marking the type board-enabled must be visible to the guard.
	if !isBoardEnabledConfig(&task.ObjectTypeWorkConfig) {
		t.Error("isBoardEnabledConfig = false for array-form board-enabled type")
	}
	// A direct agent write that sets work status is rejected on a board-enabled type.
	if err := rejectAgentWorkStatusWrite(ActorAgent, isBoardEnabledConfig(&task.ObjectTypeWorkConfig), writesStatus(&valid, nil)); err == nil {
		t.Error("rejectAgentWorkStatusWrite allowed an agent status write on a board-enabled type")
	}
}

// TestParseRelationshipTypeSchemasToMap verifies that parseRelationshipTypeSchemasToMap
// handles both storage formats and correctly merges multiple entries sharing the same
// relationship name (issue #115: same-name relationships with different source types
// were being overwritten — only the last entry was active).
func TestParseRelationshipTypeSchemasToMap(t *testing.T) {
	t.Run("array format merges same-name entries", func(t *testing.T) {
		// Reproduces the exact scenario from issue #115:
		// belongs_to from 4 different source types → all should be registered.
		data := json.RawMessage(`[
			{"name":"belongs_to","sourceType":"Scenario","targetType":"Domain"},
			{"name":"belongs_to","sourceType":"Service","targetType":"Domain"},
			{"name":"belongs_to","sourceType":"Job","targetType":"Domain"},
			{"name":"belongs_to","sourceType":"APIEndpoint","targetType":"Domain"}
		]`)

		got := parseRelationshipTypeSchemasToMap(data)
		if got == nil {
			t.Fatal("expected non-nil result for array format")
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 relationship type (merged), got %d", len(got))
		}

		rel, ok := got["belongs_to"]
		if !ok {
			t.Fatal("expected 'belongs_to' key")
		}
		if len(rel.SourceTypes) != 4 {
			t.Errorf("expected 4 source types merged, got %d: %v", len(rel.SourceTypes), rel.SourceTypes)
		}
	})

	t.Run("array format distinct names stay separate", func(t *testing.T) {
		data := json.RawMessage(`[
			{"name":"belongs_to","sourceType":"Service","targetType":"Domain"},
			{"name":"uses","sourceType":"Service","targetType":"APIEndpoint"}
		]`)

		got := parseRelationshipTypeSchemasToMap(data)
		if len(got) != 2 {
			t.Fatalf("expected 2 distinct relationship types, got %d", len(got))
		}
		if _, ok := got["belongs_to"]; !ok {
			t.Error("missing 'belongs_to'")
		}
		if _, ok := got["uses"]; !ok {
			t.Error("missing 'uses'")
		}
	})

	t.Run("map format (epf-engine v3)", func(t *testing.T) {
		data := json.RawMessage(`{
			"belongs_to": {"sourceTypes":["Service","Job"],"targetTypes":["Domain"]},
			"uses":       {"sourceType":"Service","targetType":"APIEndpoint"}
		}`)

		got := parseRelationshipTypeSchemasToMap(data)
		if len(got) != 2 {
			t.Fatalf("expected 2 relationship types, got %d", len(got))
		}
		rel := got["belongs_to"]
		if len(rel.SourceTypes) != 2 {
			t.Errorf("expected 2 source types, got %d: %v", len(rel.SourceTypes), rel.SourceTypes)
		}
	})

	t.Run("nil on empty", func(t *testing.T) {
		if parseRelationshipTypeSchemasToMap(nil) != nil {
			t.Error("expected nil for empty input")
		}
	})
}
