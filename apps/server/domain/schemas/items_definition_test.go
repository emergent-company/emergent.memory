package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

// A bare scalar `items` is not JSON Schema, but PropertyDefFromMap only reads an
// object, so before this guard the pack installed, stored the declaration and
// then validated no element type — silently (#726).
func TestValidateSchemaDefinitionsRejectsScalarItems(t *testing.T) {
	objectTypes := func(props string) json.RawMessage {
		return json.RawMessage(`[{"name":"UIPattern","properties":` + props + `}]`)
	}

	t.Run("bare scalar string is rejected with the property name", func(t *testing.T) {
		errs := validateSchemaDefinitions(objectTypes(`{"contract_aria":{"type":"array","items":"string"}}`), nil)
		if len(errs) != 1 {
			t.Fatalf("expected one error, got %v", errs)
		}
		msg := errs[0]
		for _, want := range []string{`objectTypeSchemas[0] ("UIPattern")`, `property "contract_aria".items`, "must be an object", `"string"`} {
			if !strings.Contains(msg, want) {
				t.Fatalf("expected error to contain %q, got %q", want, msg)
			}
		}
	})

	t.Run("bare scalar number is rejected", func(t *testing.T) {
		errs := validateSchemaDefinitions(objectTypes(`{"scores":{"type":"array","items":42}}`), nil)
		if len(errs) != 1 || !strings.Contains(errs[0], "got the bare scalar 42") {
			t.Fatalf("expected scalar-number error, got %v", errs)
		}
	})

	t.Run("nested array scalar items names the nested path", func(t *testing.T) {
		errs := validateSchemaDefinitions(objectTypes(`{"matrix":{"type":"array","items":{"type":"array","items":"number"}}}`), nil)
		if len(errs) != 1 || !strings.Contains(errs[0], `property "matrix".items.items`) {
			t.Fatalf("expected nested items error, got %v", errs)
		}
	})

	t.Run("relationship property scalar items is rejected too", func(t *testing.T) {
		relTypes := json.RawMessage(`[{"name":"MEMBER_OF","sourceType":"Person","targetType":"Org","properties":{"roles":{"type":"array","items":"string"}}}]`)
		errs := validateSchemaDefinitions(json.RawMessage(`[{"name":"Person"}]`), relTypes)
		if len(errs) != 1 ||
			!strings.Contains(errs[0], "relationshipTypeSchemas[0]") ||
			!strings.Contains(errs[0], `property "roles".items`) {
			t.Fatalf("expected relationship items error, got %v", errs)
		}
	})

	t.Run("valid and uninterpreted items forms stay accepted", func(t *testing.T) {
		props := `{
			"object_items":{"type":"array","items":{"type":"string","enum":["a","b"]}},
			"nested":{"type":"array","items":{"type":"array","items":{"type":"number"}}},
			"tuple":{"type":"array","items":[{"type":"string"}]},
			"boolean":{"type":"array","items":false},
			"absent_type":{"type":"array"},
			"null_items":{"type":"array","items":null}
		}`
		if errs := validateSchemaDefinitions(objectTypes(props), nil); len(errs) != 0 {
			t.Fatalf("expected no validation errors, got %v", errs)
		}
	})
}

// ValidateSchemaDefinitions is the exported seam the MCP schema-create tool
// calls (#811). It must be the same guard CreatePack runs — not a copy that can
// drift — so a declaration it rejects over REST is rejected over MCP too.
func TestValidateSchemaDefinitionsExportedSeam(t *testing.T) {
	objectTypes := func(props string) json.RawMessage {
		return json.RawMessage(`[{"name":"Contract","properties":` + props + `}]`)
	}

	errs := ValidateSchemaDefinitions(objectTypes(`{"tags":{"type":"array","items":"string"}}`), nil)
	if len(errs) != 1 || !strings.Contains(errs[0], `property "tags".items`) {
		t.Fatalf("expected the scalar-items guard through the exported seam, got %v", errs)
	}

	valid := ValidateSchemaDefinitions(
		objectTypes(`{"tags":{"type":"array","items":{"type":"string"}}}`), nil)
	if len(valid) != 0 {
		t.Fatalf("expected no validation errors for the object items form, got %v", valid)
	}
}
