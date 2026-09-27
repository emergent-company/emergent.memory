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
}
