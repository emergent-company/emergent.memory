package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestValidateSchemaDefinitionsRelationshipTypeShape pins the POST /api/schemas
// contract for relationship_type_schemas: the *request* shape is a JSON array
// of {name, sourceType, targetType} objects. The map of rel type → schema is
// the legacy stored shape and must not be accepted silently at the HTTP
// boundary — accepting both would let a broken client keep working while the
// documented contract rots.
func TestValidateSchemaDefinitionsRelationshipTypeShape(t *testing.T) {
	objectTypes := json.RawMessage(`[{"name":"Note"}]`)

	t.Run("array of relationship type objects is accepted", func(t *testing.T) {
		relTypes := json.RawMessage(`[{"name":"annotates","sourceType":"Note","targetType":"Note"}]`)
		if errs := validateSchemaDefinitions(objectTypes, relTypes); len(errs) != 0 {
			t.Fatalf("expected no validation errors, got %v", errs)
		}
	})

	t.Run("map form is rejected with the expected shape named", func(t *testing.T) {
		relTypes := json.RawMessage(`{"annotates":{"sourceTypes":["Note"],"targetTypes":["Note"]}}`)
		errs := validateSchemaDefinitions(objectTypes, relTypes)
		if len(errs) != 1 {
			t.Fatalf("expected exactly one validation error, got %v", errs)
		}
		msg := errs[0]
		t.Logf("map rejection: %s", msg)
		for _, want := range []string{
			"must be a JSON array",
			`"sourceType"`,
			`"targetType"`,
			`[{"name"`, // the example payload
			"JSON object",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("error message does not mention %q: %s", want, msg)
			}
		}
		// The old message leaked a Go type, which a test author cannot act on.
		for _, unwanted := range []string{"Go value", "struct {"} {
			if strings.Contains(msg, unwanted) {
				t.Errorf("error message still leaks the decoder internals (%q): %s", unwanted, msg)
			}
		}
	})

	t.Run("array entry missing targetType is still named by field", func(t *testing.T) {
		relTypes := json.RawMessage(`[{"name":"annotates","sourceType":"Note"}]`)
		errs := validateSchemaDefinitions(objectTypes, relTypes)
		if len(errs) != 1 || !strings.Contains(errs[0], "missing required field 'targetType'") {
			t.Fatalf("expected a missing-targetType error, got %v", errs)
		}
	})
}
