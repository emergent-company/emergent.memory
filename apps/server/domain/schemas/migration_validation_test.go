package schemas

import (
	"encoding/json"
	"testing"
)

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// newSchemaRaw builds a personal-memory-style object schema: a type whose name
// is the NEW (CamelCase) name, with a property that exists under its new name.
func newSchemaRaw(t *testing.T) json.RawMessage {
	return mustRaw(t, []map[string]any{
		{
			"name": "Person",
			"properties": map[string]any{
				"first_name": map[string]any{"type": "string"},
			},
		},
	})
}

func TestValidateMigrationHintsTypeRenamesTo(t *testing.T) {
	// A rename old -> new must validate the NEW name against the current schema.
	hints := &SchemaMigrationHints{
		FromVersion: "1.0.0",
		TypeRenames: []TypeRename{{From: "person", To: "Person"}},
	}
	if errs := validateMigrationHints(hints, newSchemaRaw(t), nil); len(errs) != 0 {
		t.Fatalf("expected valid, got: %v", errs)
	}
}

func TestValidateMigrationHintsTypeRenamesToMissing(t *testing.T) {
	hints := &SchemaMigrationHints{
		FromVersion: "1.0.0",
		TypeRenames: []TypeRename{{From: "person", To: "Missing"}},
	}
	errs := validateMigrationHints(hints, newSchemaRaw(t), nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateMigrationHintsPropertyRenamesTo(t *testing.T) {
	// Property rename old -> new must validate the NEW property name.
	hints := &SchemaMigrationHints{
		FromVersion: "1.0.0",
		PropertyRenames: []PropertyRename{
			{TypeName: "Person", From: "given_name", To: "first_name"},
		},
	}
	if errs := validateMigrationHints(hints, newSchemaRaw(t), nil); len(errs) != 0 {
		t.Fatalf("expected valid, got: %v", errs)
	}
}

func TestValidateMigrationHintsPropertyRenamesToMissing(t *testing.T) {
	hints := &SchemaMigrationHints{
		FromVersion: "1.0.0",
		PropertyRenames: []PropertyRename{
			{TypeName: "Person", From: "given_name", To: "missing_prop"},
		},
	}
	errs := validateMigrationHints(hints, newSchemaRaw(t), nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateMigrationHintsRemovedProperties(t *testing.T) {
	// A removed property is intentionally absent from the new schema.
	hints := &SchemaMigrationHints{
		FromVersion: "1.0.0",
		RemovedProperties: []RemovedProperty{
			{TypeName: "Person", Name: "legacy_field"},
		},
	}
	if errs := validateMigrationHints(hints, newSchemaRaw(t), nil); len(errs) != 0 {
		t.Fatalf("expected valid (removed property is absent from new schema), got: %v", errs)
	}
}

func TestValidateMigrationHintsRemovedTypeMissing(t *testing.T) {
	hints := &SchemaMigrationHints{
		FromVersion: "1.0.0",
		RemovedProperties: []RemovedProperty{
			{TypeName: "Missing", Name: "legacy_field"},
		},
	}
	errs := validateMigrationHints(hints, newSchemaRaw(t), nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}
