package blueprints_test

import (
	"encoding/json"
	"strings"
	"testing"

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
