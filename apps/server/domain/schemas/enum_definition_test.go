package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateSchemaDefinitionsEnum(t *testing.T) {
	objectTypes := func(props string) json.RawMessage {
		return json.RawMessage(`[{"name":"Task","properties":` + props + `}]`)
	}

	t.Run("valid primitive enums are accepted", func(t *testing.T) {
		props := `{
			"status":{"type":"string","enum":["draft","published"]},
			"priority":{"type":"integer","enum":[1,2,3]},
			"score":{"type":"number","enum":[1.5,2.5]},
			"urgent":{"type":"boolean","enum":[true,false]}
		}`
		if errs := validateSchemaDefinitions(objectTypes(props), nil); len(errs) != 0 {
			t.Fatalf("expected no validation errors, got %v", errs)
		}
	})

	t.Run("empty enum is rejected", func(t *testing.T) {
		errs := validateSchemaDefinitions(objectTypes(`{"status":{"type":"string","enum":[]}}`), nil)
		if len(errs) != 1 || !strings.Contains(errs[0], "non-empty list") {
			t.Fatalf("expected empty-enum error, got %v", errs)
		}
	})

	t.Run("non-list enum is rejected", func(t *testing.T) {
		errs := validateSchemaDefinitions(objectTypes(`{"status":{"type":"string","enum":"draft"}}`), nil)
		if len(errs) != 1 || !strings.Contains(errs[0], "non-empty list") {
			t.Fatalf("expected non-list error, got %v", errs)
		}
	})

	t.Run("enum value mismatch names the index", func(t *testing.T) {
		errs := validateSchemaDefinitions(objectTypes(`{"status":{"type":"string","enum":["draft",2]}}`), nil)
		if len(errs) != 1 || !strings.Contains(errs[0], "enum[1]") || !strings.Contains(errs[0], `type "string"`) {
			t.Fatalf("expected index mismatch error, got %v", errs)
		}
	})

	t.Run("integer enum rejects a fraction", func(t *testing.T) {
		errs := validateSchemaDefinitions(objectTypes(`{"priority":{"type":"integer","enum":[1,2.5]}}`), nil)
		if len(errs) != 1 || !strings.Contains(errs[0], "enum[1]") {
			t.Fatalf("expected integer mismatch error, got %v", errs)
		}
	})

	t.Run("relationship property enums are validated too", func(t *testing.T) {
		relTypes := json.RawMessage(`[{"name":"MEMBER_OF","sourceType":"Person","targetType":"Org","properties":{"role":{"type":"string","enum":["owner",1]}}}]`)
		errs := validateSchemaDefinitions(json.RawMessage(`[{"name":"Person"}]`), relTypes)
		if len(errs) != 1 || !strings.Contains(errs[0], "relationshipTypeSchemas[0]") || !strings.Contains(errs[0], "enum[1]") {
			t.Fatalf("expected relationship enum error, got %v", errs)
		}
	})

	t.Run("enum on an unsupported type is left to the CLI warning", func(t *testing.T) {
		errs := validateSchemaDefinitions(objectTypes(`{"meta":{"type":"object","enum":["x"]}}`), nil)
		if len(errs) != 0 {
			t.Fatalf("expected non-primitive enum to be ignored, got %v", errs)
		}
	})
}
