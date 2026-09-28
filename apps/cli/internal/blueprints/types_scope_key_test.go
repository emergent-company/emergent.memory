package blueprints_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// TestObjectTypeDefScopeKeyMarshal pins the CLI side of the null-scopeKey
// regression: the applier marshals the typed ObjectTypes slice
// (apps/cli/internal/blueprints/applier.go), so a type WITHOUT a declaration
// must not emit `"scopeKey":null` — that null is a present-but-invalid
// declaration to the server validator and made first-apply of any pack fail
// with HTTP 400. A declared scope key must still be carried through.
func TestObjectTypeDefScopeKeyMarshal(t *testing.T) {
	t.Run("absent declaration is omitted", func(t *testing.T) {
		raw, err := json.Marshal([]blueprints.ObjectTypeDef{{
			Name:       "Person",
			Properties: map[string]any{"name": map[string]any{"type": "string"}},
		}})
		if err != nil {
			t.Fatalf("marshal object types: %v", err)
		}
		if strings.Contains(string(raw), "scopeKey") {
			t.Fatalf("a type without a declaration must not marshal scopeKey, got %s", raw)
		}
	})

	t.Run("present declaration is carried", func(t *testing.T) {
		raw, err := json.Marshal([]blueprints.ObjectTypeDef{{
			Name:       "LegalParagraph",
			Properties: map[string]any{"law_ref_id": map[string]any{"type": "string"}},
			ScopeKey: map[string]any{
				"property":           "law_ref_id",
				"referencesType":     "Law",
				"referencesProperty": "ref_id",
			},
		}})
		if err != nil {
			t.Fatalf("marshal object types: %v", err)
		}
		if !strings.Contains(string(raw), `"scopeKey":{"`) {
			t.Fatalf("expected scopeKey to be carried, got %s", raw)
		}
	})
}

// TestObjectTypeDefScopeKeySnakeAlias verifies the top-level snake_case alias
// (`scope_key`) is accepted and normalised into the canonical declaration. RED
// before the alias existed: a YAML/JSON pack authored with `scope_key` decoded
// to nil and re-marshalled without any declaration, silently disabling
// enforcement.
func TestObjectTypeDefScopeKeySnakeAlias(t *testing.T) {
	t.Run("yaml scope_key is promoted", func(t *testing.T) {
		src := []byte(`
- name: LegalParagraph
  properties:
    law_ref_id:
      type: string
  scope_key:
    property: law_ref_id
    references_type: Law
    references_property: ref_id
`)
		var types []blueprints.ObjectTypeDef
		if err := yaml.Unmarshal(src, &types); err != nil {
			t.Fatalf("yaml unmarshal: %v", err)
		}
		if len(types) != 1 || types[0].ScopeKey == nil {
			t.Fatalf("scope_key must be promoted to a declaration, got %+v", types)
		}
		if types[0].ScopeKey["property"] != "law_ref_id" {
			t.Fatalf("unexpected declaration: %+v", types[0].ScopeKey)
		}
		if types[0].ScopeKeySnake != nil {
			t.Fatalf("alias must be cleared after promotion, got %+v", types[0].ScopeKeySnake)
		}
		raw, err := json.Marshal(types)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(raw), `"scopeKey":{"property":"law_ref_id"`) {
			t.Fatalf("promoted declaration must be emitted as canonical scopeKey, got %s", raw)
		}
		if strings.Contains(string(raw), "scope_key") {
			t.Fatalf("the snake_case alias must not be re-emitted, got %s", raw)
		}
	})

	t.Run("json scope_key is promoted", func(t *testing.T) {
		src := []byte(`[{"name":"LegalParagraph","properties":{"law_ref_id":{"type":"string"}},"scope_key":{"property":"law_ref_id"}}]`)
		var types []blueprints.ObjectTypeDef
		if err := json.Unmarshal(src, &types); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if len(types) != 1 || types[0].ScopeKey == nil || types[0].ScopeKey["property"] != "law_ref_id" {
			t.Fatalf("scope_key must be promoted, got %+v", types)
		}
	})

	t.Run("canonical scopeKey wins when both are present", func(t *testing.T) {
		src := []byte(`[{"name":"T","scopeKey":{"property":"camel"},"scope_key":{"property":"snake"}}]`)
		var types []blueprints.ObjectTypeDef
		if err := json.Unmarshal(src, &types); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if len(types) != 1 || types[0].ScopeKey["property"] != "camel" {
			t.Fatalf("canonical scopeKey must win, got %+v", types)
		}
		if types[0].ScopeKeySnake != nil {
			t.Fatalf("alias must be cleared, got %+v", types[0].ScopeKeySnake)
		}
	})
}
