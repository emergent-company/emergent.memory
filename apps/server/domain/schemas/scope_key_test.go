package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseScopeKey(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		decl, err := ParseScopeKey(json.RawMessage(`{"properties":{"a":{"type":"string"}}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if decl != nil {
			t.Fatalf("expected nil declaration, got %+v", decl)
		}
	})

	t.Run("camelCase", func(t *testing.T) {
		decl, err := ParseScopeKey(json.RawMessage(`{
			"properties":{"law_ref_id":{},"section_id":{}},
			"scopeKey":{"property":"law_ref_id","referencesType":"Law","referencesProperty":"ref_id","identityProperty":"section_id"}
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if decl == nil || decl.Property != "law_ref_id" || decl.ReferencesType != "Law" ||
			decl.ReferencesProperty != "ref_id" || decl.IdentityProperty != "section_id" {
			t.Fatalf("unexpected declaration: %+v", decl)
		}
	})

	t.Run("snake_case aliases", func(t *testing.T) {
		decl, err := ParseScopeKey(json.RawMessage(`{
			"properties":{"law_ref_id":{}},
			"scope_key":{"property":"law_ref_id","references_type":"Law","references_property":"ref_id"}
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if decl == nil || decl.ReferencesType != "Law" || decl.ReferencesProperty != "ref_id" {
			t.Fatalf("unexpected declaration: %+v", decl)
		}
	})
}

func TestValidateScopeKey(t *testing.T) {
	props := map[string]struct{}{"law_ref_id": {}, "section_id": {}}
	known := map[string]map[string]struct{}{
		"Law": {"ref_id": {}},
	}

	t.Run("valid", func(t *testing.T) {
		errs := ValidateScopeKey("LegalParagraph", &ScopeKeyDeclaration{
			Property: "law_ref_id", ReferencesType: "Law", ReferencesProperty: "ref_id", IdentityProperty: "section_id",
		}, props, known)
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
	})

	t.Run("property not present on type", func(t *testing.T) {
		errs := ValidateScopeKey("LegalParagraph", &ScopeKeyDeclaration{Property: "nope"}, props, known)
		if len(errs) != 1 || !strings.Contains(errs[0], `scopeKey.property "nope" is not a declared property`) {
			t.Fatalf("expected missing-property error, got %v", errs)
		}
	})

	t.Run("identity property not present", func(t *testing.T) {
		errs := ValidateScopeKey("LegalParagraph", &ScopeKeyDeclaration{Property: "law_ref_id", IdentityProperty: "nope"}, props, known)
		if len(errs) != 1 || !strings.Contains(errs[0], "scopeKey.identityProperty") {
			t.Fatalf("expected identity error, got %v", errs)
		}
	})

	t.Run("unresolvable reference target", func(t *testing.T) {
		errs := ValidateScopeKey("LegalParagraph", &ScopeKeyDeclaration{
			Property: "law_ref_id", ReferencesType: "MissingType", ReferencesProperty: "ref_id",
		}, props, known)
		if len(errs) != 1 || !strings.Contains(errs[0], `scopeKey.referencesType "MissingType" does not resolve`) {
			t.Fatalf("expected unresolvable-target error, got %v", errs)
		}
	})

	t.Run("reference property not on target", func(t *testing.T) {
		errs := ValidateScopeKey("LegalParagraph", &ScopeKeyDeclaration{
			Property: "law_ref_id", ReferencesType: "Law", ReferencesProperty: "missing",
		}, props, known)
		if len(errs) != 1 || !strings.Contains(errs[0], `scopeKey.referencesProperty "missing" is not a declared property of type Law`) {
			t.Fatalf("expected missing-target-property error, got %v", errs)
		}
	})

	t.Run("half-specified reference", func(t *testing.T) {
		errs := ValidateScopeKey("LegalParagraph", &ScopeKeyDeclaration{
			Property: "law_ref_id", ReferencesType: "Law",
		}, props, known)
		if len(errs) != 1 || !strings.Contains(errs[0], "must be set together") {
			t.Fatalf("expected half-reference error, got %v", errs)
		}
	})
}

func TestValidateSchemaDefinitions_ScopeKey(t *testing.T) {
	t.Run("valid declaration passes", func(t *testing.T) {
		raw := json.RawMessage(`[
			{"name":"Law","properties":{"ref_id":{"type":"string"}}},
			{"name":"LegalParagraph","properties":{"law_ref_id":{"type":"string"},"chapter_id":{"type":"string"}},
			 "scopeKey":{"property":"law_ref_id","referencesType":"Law","referencesProperty":"ref_id"}}
		]`)
		if errs := validateSchemaDefinitions(raw, nil); len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
	})

	t.Run("property not present on type is rejected", func(t *testing.T) {
		raw := json.RawMessage(`[
			{"name":"Law","properties":{"ref_id":{"type":"string"}}},
			{"name":"LegalParagraph","properties":{"chapter_id":{"type":"string"}},
			 "scopeKey":{"property":"law_ref_id","referencesType":"Law","referencesProperty":"ref_id"}}
		]`)
		errs := validateSchemaDefinitions(raw, nil)
		if len(errs) != 1 || !strings.Contains(errs[0], `scopeKey.property "law_ref_id" is not a declared property`) {
			t.Fatalf("expected scope-key property error, got %v", errs)
		}
	})

	t.Run("unresolvable reference target is rejected", func(t *testing.T) {
		raw := json.RawMessage(`[
			{"name":"LegalParagraph","properties":{"law_ref_id":{"type":"string"}},
			 "scopeKey":{"property":"law_ref_id","referencesType":"Law","referencesProperty":"ref_id"}}
		]`)
		errs := validateSchemaDefinitions(raw, nil)
		if len(errs) != 1 || !strings.Contains(errs[0], `scopeKey.referencesType "Law" does not resolve`) {
			t.Fatalf("expected unresolvable-target error, got %v", errs)
		}
	})

	t.Run("no declaration leaves existing validation unchanged", func(t *testing.T) {
		raw := json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}}}]`)
		if errs := validateSchemaDefinitions(raw, nil); len(errs) != 0 {
			t.Fatalf("expected no errors, got %v", errs)
		}
	})

	// Regression for the CLI blueprint path: the CLI marshals its pack schemas
	// with json.Marshal, and its ObjectTypeDef historically serialised
	// `"scopeKey":null` for every type WITHOUT a declaration. JSON null must be
	// treated as absent (like ParseScopeKey / isNullJSON already do), not as a
	// present-but-empty declaration that fails `scopeKey.property is required`.
	//
	// The payload below is the exact wire shape produced by
	// apps/cli/internal/blueprints.ObjectTypeDef (same field tags, no
	// omitempty) — mirrored here because the CLI is a separate Go module the
	// server cannot import. The CLI-side assertion that the real struct now
	// omits the field lives in
	// apps/cli/internal/blueprints/types_scope_key_test.go.
	t.Run("CLI payload with scopeKey:null is accepted", func(t *testing.T) {
		type cliObjectTypePayload struct {
			Name        string         `json:"name"`
			Label       string         `json:"label"`
			Description string         `json:"description"`
			Properties  map[string]any `json:"properties"`
			ScopeKey    map[string]any `json:"scopeKey"` // no omitempty: emits null
		}
		payload, err := json.Marshal([]cliObjectTypePayload{{
			Name:        "Person",
			Label:       "Person",
			Description: "A human individual",
			Properties:  map[string]any{"name": map[string]any{"type": "string"}},
		}})
		if err != nil {
			t.Fatalf("marshal CLI payload: %v", err)
		}
		if !strings.Contains(string(payload), `"scopeKey":null`) {
			t.Fatalf("test payload must reproduce the CLI null shape, got %s", payload)
		}
		if errs := validateSchemaDefinitions(payload, nil); len(errs) != 0 {
			t.Fatalf("expected null scopeKey to be treated as absent, got %v", errs)
		}
	})

	t.Run("snake_case scope_key:null is accepted", func(t *testing.T) {
		raw := json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}},"scope_key":null}]`)
		if errs := validateSchemaDefinitions(raw, nil); len(errs) != 0 {
			t.Fatalf("expected null scope_key to be treated as absent, got %v", errs)
		}
	})
}
