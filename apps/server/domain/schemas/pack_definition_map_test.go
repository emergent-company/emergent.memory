package schemas

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// The MCP schema-create tool writes the registry rows directly in the stored map
// form (type name → schema object). ValidatePackDefinitionMap normalises that form
// onto the same validateSchemaDefinitions guard HTTP CreatePack/UpdatePack use, so
// a definition rejected on the REST path is rejected here too — the bypass #811
// records, where a bare scalar `items` (#726) installed silently through MCP.
func TestValidatePackDefinitionMapRunsTheSharedGuard(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }

	t.Run("bare scalar items is rejected with the REST message", func(t *testing.T) {
		err := ValidatePackDefinitionMap(map[string]json.RawMessage{
			"UIPattern": raw(`{"properties":{"contract_aria":{"type":"array","items":"string"}}}`),
		}, nil)
		if err == nil {
			t.Fatal("expected the scalar items declaration to be rejected")
		}
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusBadRequest {
			t.Fatalf("expected a 400 apperror, got %T %v", err, err)
		}
		for _, want := range []string{
			"invalid schema definitions:",
			`objectTypeSchemas[0] ("UIPattern")`,
			`property "contract_aria".items`,
			"must be an object",
			`"string"`,
		} {
			if !strings.Contains(appErr.Message, want) {
				t.Fatalf("expected error to contain %q, got %q", want, appErr.Message)
			}
		}
	})

	t.Run("scalar items in a relationship property is rejected too", func(t *testing.T) {
		err := ValidatePackDefinitionMap(
			map[string]json.RawMessage{"Person": raw(`{"properties":{"name":{"type":"string"}}}`)},
			map[string]json.RawMessage{
				"MEMBER_OF": raw(`{"type":"object","sourceTypes":["Person"],"targetTypes":["Org"],"properties":{"roles":{"type":"array","items":"string"}}}`),
			})
		if err == nil || !strings.Contains(err.Error(), `property "roles".items`) {
			t.Fatalf("expected a relationship items error, got %v", err)
		}
	})

	t.Run("documented map forms with plural endpoints stay accepted", func(t *testing.T) {
		err := ValidatePackDefinitionMap(
			map[string]json.RawMessage{
				"ResearchPaper": raw(`{"type":"object","properties":{"title":{"type":"string"},"authors":{"type":"array","items":{"type":"string"}}}}`),
				"Person":        raw(`{"type":"object","properties":{"name":{"type":"string"}}}`),
			},
			map[string]json.RawMessage{
				"authored_by": raw(`{"type":"object","sourceTypes":["ResearchPaper"],"targetTypes":["Person"],"properties":{"role":{"type":"string","enum":["primary","co-author"]}}}`),
				"cites":       raw(`{"type":"object","source_types":["ResearchPaper"],"target_types":["ResearchPaper"]}`),
			})
		if err != nil {
			t.Fatalf("expected the documented map forms to validate, got %v", err)
		}
	})

	t.Run("relationship without a resolvable endpoint is rejected", func(t *testing.T) {
		err := ValidatePackDefinitionMap(nil, map[string]json.RawMessage{
			"orphan": raw(`{"type":"object"}`),
		})
		if err == nil {
			t.Fatal("expected a relationship without endpoints to be rejected")
		}
		for _, want := range []string{`relationshipTypeSchemas[0] ("orphan")`, "sourceType", "targetType"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error to contain %q, got %q", want, err.Error())
			}
		}
	})

	t.Run("non-object definition is a 400 naming the type", func(t *testing.T) {
		err := ValidatePackDefinitionMap(map[string]json.RawMessage{"Broken": raw(`"not a schema"`)}, nil)
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusBadRequest {
			t.Fatalf("expected a 400 apperror, got %T %v", err, err)
		}
		if !strings.Contains(appErr.Message, `object type "Broken" must be a JSON object schema`) {
			t.Fatalf("unexpected message: %q", appErr.Message)
		}
	})

	t.Run("errors are deterministic across map iteration order", func(t *testing.T) {
		defs := map[string]json.RawMessage{
			"Zeta":  raw(`{"properties":{"a":{"type":"array","items":"string"}}}`),
			"Alpha": raw(`{"properties":{"a":{"type":"array","items":"string"}}}`),
		}
		first := ValidatePackDefinitionMap(defs, nil)
		for i := 0; i < 20; i++ {
			if err := ValidatePackDefinitionMap(defs, nil); err == nil || err.Error() != first.Error() {
				t.Fatalf("expected deterministic message, got %v then %v", first, err)
			}
		}
		alphaAt := strings.Index(first.Error(), `("Alpha")`)
		zetaAt := strings.Index(first.Error(), `("Zeta")`)
		if alphaAt < 0 || zetaAt < 0 || alphaAt > zetaAt {
			t.Fatalf("expected errors to be ordered by type name, got %q", first.Error())
		}
	})
}
