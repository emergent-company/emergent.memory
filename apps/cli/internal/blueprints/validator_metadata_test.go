package blueprints_test

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// validMetadataPack returns a pack whose only object type satisfies every
// metadata check, so a test can mutate one field and attribute any finding to
// that mutation.
func validMetadataPack() blueprints.PackFile {
	return blueprints.PackFile{
		Name:       "p",
		Version:    "1.0.0",
		SourceFile: "schemas/p.yaml",
		ObjectTypes: []blueprints.ObjectTypeDef{
			{
				Name:        "Doc",
				Label:       "Document",
				Labels:      []string{"doc"},
				Description: "d",
				Properties: map[string]any{
					"title": map[string]any{"type": "string"},
				},
				Required: []string{"title"},
				UI:       map[string]any{"icon": "file-text", "color": "#3B82F6"},
			},
		},
	}
}

func hasIssue(issues []blueprints.ValidationIssue, field, msgSubstr string) bool {
	for _, i := range issues {
		if strings.Contains(i.Field, field) && strings.Contains(i.Message, msgSubstr) {
			return true
		}
	}
	return false
}

func TestValidateObjectMetadata_Clean(t *testing.T) {
	report := blueprints.Validate(nil, []blueprints.PackFile{validMetadataPack()}, nil, nil, nil, nil, nil)
	if len(report.Issues) != 0 {
		t.Fatalf("expected a fully-populated pack to validate clean, got: %v", report.Issues)
	}
}

func TestValidateObjectMetadata_CanonicalExtendedTypes(t *testing.T) {
	p := validMetadataPack()
	p.ObjectTypes[0].Properties["homepage"] = map[string]any{"type": "url"}
	p.ObjectTypes[0].Properties["digest"] = map[string]any{"type": "sha256"}
	report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
	if len(report.Issues) != 0 {
		t.Fatalf("expected canonical url/sha256 types to validate clean, got: %v", report.Issues)
	}
}

func TestValidateObjectMetadata_StyleWarnings(t *testing.T) {
	t.Run("missing labels is a warning", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].Labels = nil
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "objectTypes[0].labels", "no labels") {
			t.Fatalf("expected a labels warning, got: %v", report.Issues)
		}
		if report.HasErrors() {
			t.Fatalf("missing labels must not be an error: %v", report.Errors())
		}
	})

	t.Run("missing inline ui is a warning", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].UI = nil
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "objectTypes[0].ui", "no ui metadata") {
			t.Fatalf("expected a missing-ui warning, got: %v", report.Issues)
		}
	})

	t.Run("missing icon and color are warnings", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].UI = map[string]any{"category": "docs"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		warns := report.Warnings()
		if !hasIssue(warns, "objectTypes[0].ui.icon", "no ui.icon") {
			t.Fatalf("expected an icon warning, got: %v", warns)
		}
		if !hasIssue(warns, "objectTypes[0].ui.color", "no ui.color") {
			t.Fatalf("expected a color warning, got: %v", warns)
		}
	})

	t.Run("unrenderable icon is a warning", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].UI = map[string]any{"icon": "not-a-real-icon", "color": "#3B82F6"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "objectTypes[0].ui.icon", "not in the supported icon catalog") {
			t.Fatalf("expected an icon catalog warning, got: %v", report.Warnings())
		}
	})

	t.Run("PascalCase icon normalizes as renderable", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].UI = map[string]any{"icon": "FileText", "color": "#3B82F6"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if len(report.Issues) != 0 {
			t.Fatalf("expected PascalCase FileText to be renderable, got: %v", report.Issues)
		}
	})

	t.Run("emoji icon is accepted as text", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].UI = map[string]any{"icon": "🚀", "color": "#3B82F6"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if len(report.Issues) != 0 {
			t.Fatalf("expected an emoji icon to be accepted, got: %v", report.Issues)
		}
	})

	t.Run("invalid color is a warning", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].UI = map[string]any{"icon": "file-text", "color": "blue"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "objectTypes[0].ui.color", "not a hex color") {
			t.Fatalf("expected a color warning, got: %v", report.Warnings())
		}
	})

	t.Run("valid short and alpha hex colors pass", func(t *testing.T) {
		for _, color := range []string{"#fff", "#3B82F6", "#3B82F6FF"} {
			p := validMetadataPack()
			p.ObjectTypes[0].UI = map[string]any{"icon": "file-text", "color": color}
			report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
			if len(report.Issues) != 0 {
				t.Fatalf("expected %s to be a valid hex color, got: %v", color, report.Issues)
			}
		}
	})
}

func TestValidateObjectMetadata_StructuralErrors(t *testing.T) {
	t.Run("required names undeclared property", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].Required = []string{"title", "missing"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "required[1]", `required property "missing" is not declared`) {
			t.Fatalf("expected a required-property warning, got: %v", report.Warnings())
		}
	})

	t.Run("property definition must be a mapping", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].Properties["bad"] = "string"
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "properties.bad", "must be a mapping") {
			t.Fatalf("expected a mapping warning, got: %v", report.Warnings())
		}
	})

	t.Run("property type is required and constrained", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].Properties["noType"] = map[string]any{}
		p.ObjectTypes[0].Properties["badType"] = map[string]any{"type": "currency"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "properties.noType.type", "type is not declared") {
			t.Fatalf("expected a missing-type warning, got: %v", report.Warnings())
		}
		if !hasIssue(report.Warnings(), "properties.badType.type", `unknown type "currency"`) {
			t.Fatalf("expected an unknown-type warning, got: %v", report.Warnings())
		}
		if report.HasErrors() {
			t.Fatalf("type vocabulary findings must not be errors, got: %v", report.Errors())
		}
	})

	t.Run("enum must be a non-empty list matching the type", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].Properties["emptyEnum"] = map[string]any{"type": "string", "enum": []any{}}
		p.ObjectTypes[0].Properties["mismatch"] = map[string]any{"type": "string", "enum": []any{"ok", 2}}
		p.ObjectTypes[0].Properties["nonList"] = map[string]any{"type": "string", "enum": "ok"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		warns := report.Warnings()
		if !hasIssue(warns, "properties.emptyEnum.enum", "non-empty list") {
			t.Fatalf("expected empty-enum warning, got: %v", warns)
		}
		if !hasIssue(warns, "properties.mismatch.enum[1]", "does not match property type") {
			t.Fatalf("expected enum value mismatch warning, got: %v", warns)
		}
		if !hasIssue(warns, "properties.nonList.enum", "must be a non-empty list") {
			t.Fatalf("expected non-list enum warning, got: %v", warns)
		}
		if report.HasErrors() {
			t.Fatalf("enum findings must not be errors, got: %v", report.Errors())
		}
	})

	t.Run("minimum and maximum only on numeric properties", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].Properties["label"] = map[string]any{"type": "string", "minimum": 0}
		p.ObjectTypes[0].Properties["count"] = map[string]any{"type": "integer", "maximum": "ten"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "properties.label.minimum", "only valid on number/integer") {
			t.Fatalf("expected minimum-on-string warning, got: %v", report.Warnings())
		}
		if !hasIssue(report.Warnings(), "properties.count.maximum", "must be numeric") {
			t.Fatalf("expected non-numeric maximum warning, got: %v", report.Warnings())
		}
	})

	t.Run("x-embedding-target must be true and on a string property", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].Properties["badFlag"] = map[string]any{"type": "string", "x-embedding-target": "yes"}
		p.ObjectTypes[0].Properties["wrongType"] = map[string]any{"type": "integer", "x-embedding-target": true}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "properties.badFlag.x-embedding-target", "must be boolean true") {
			t.Fatalf("expected embedding-flag warning, got: %v", report.Warnings())
		}
		if !hasIssue(report.Warnings(), "properties.wrongType.x-embedding-target", "only meaningful on string") {
			t.Fatalf("expected non-string embedding-target warning, got: %v", report.Warnings())
		}
	})

	t.Run("array without items is a warning", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].Properties["tags"] = map[string]any{"type": "array"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "properties.tags.items", "array without items") {
			t.Fatalf("expected array-items warning, got: %v", report.Warnings())
		}
	})

	t.Run("embedding and extraction known keys have fixed kinds", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].Embedding = map[string]any{"mode": 1, "field": true, "enabled": "yes"}
		p.ObjectTypes[0].Extraction = map[string]any{"enabled": "on"}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		warns := report.Warnings()
		if !hasIssue(warns, "objectTypes[0].embedding.mode", "must be a string") {
			t.Fatalf("expected embedding.mode warning, got: %v", warns)
		}
		if !hasIssue(warns, "objectTypes[0].embedding.field", "must be a string") {
			t.Fatalf("expected embedding.field warning, got: %v", warns)
		}
		if !hasIssue(warns, "objectTypes[0].embedding.enabled", "must be a boolean") {
			t.Fatalf("expected embedding.enabled warning, got: %v", warns)
		}
		if !hasIssue(warns, "objectTypes[0].extraction.enabled", "must be a boolean") {
			t.Fatalf("expected extraction.enabled warning, got: %v", warns)
		}
		if report.HasErrors() {
			t.Fatalf("behavior-block findings must not be errors, got: %v", report.Errors())
		}
	})
}

func TestValidatePackUIConfigs(t *testing.T) {
	t.Run("entry satisfies effective ui", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes[0].UI = nil
		p.UIConfigs = blueprints.JSONBlob(`{"Doc":{"icon":"user","color":"#fff"}}`)
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if len(report.Issues) != 0 {
			t.Fatalf("expected pack uiConfigs to satisfy the ui check, got: %v", report.Issues)
		}
	})

	t.Run("entry with invalid icon warns under its uiConfigs field", func(t *testing.T) {
		p := validMetadataPack()
		p.UIConfigs = blueprints.JSONBlob(`{"Doc":{"icon":"nope","color":"#fff"}}`)
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "uiConfigs.Doc.icon", "not in the supported icon catalog") {
			t.Fatalf("expected uiConfigs icon warning, got: %v", report.Warnings())
		}
	})

	t.Run("undeclared object type key warns", func(t *testing.T) {
		p := validMetadataPack()
		p.UIConfigs = blueprints.JSONBlob(`{"Ghost":{"icon":"user","color":"#fff"}}`)
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "uiConfigs.Ghost", "not an objectType declared by this pack") {
			t.Fatalf("expected undeclared uiConfigs key warning, got: %v", report.Warnings())
		}
	})

	t.Run("non-object entry is a warning", func(t *testing.T) {
		p := validMetadataPack()
		p.UIConfigs = blueprints.JSONBlob(`{"Doc":"blue"}`)
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "uiConfigs", "must be an object") {
			t.Fatalf("expected uiConfigs warning, got: %v", report.Warnings())
		}
		if report.HasErrors() {
			t.Fatalf("malformed uiConfigs must not be an error, got: %v", report.Errors())
		}
	})

	t.Run("non-object uiConfigs is a warning", func(t *testing.T) {
		p := validMetadataPack()
		p.UIConfigs = blueprints.JSONBlob(`[]`)
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "uiConfigs", "must be an object") {
			t.Fatalf("expected uiConfigs warning, got: %v", report.Warnings())
		}
		if report.HasErrors() {
			t.Fatalf("malformed uiConfigs must not be an error, got: %v", report.Errors())
		}
	})
}

func TestValidateRelationshipMetadata(t *testing.T) {
	t.Run("missing label is a warning", func(t *testing.T) {
		p := validMetadataPack()
		p.ObjectTypes = append(p.ObjectTypes, blueprints.ObjectTypeDef{
			Name: "Note", Label: "Note", Labels: []string{"note"},
			UI: map[string]any{"icon": "file", "color": "#fff"},
		})
		p.RelationshipTypes = []blueprints.RelationshipTypeDef{
			{Name: "links", SourceType: "Doc", TargetType: "Note"},
		}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "relationshipTypes[0].label", "no label") {
			t.Fatalf("expected relationship label warning, got: %v", report.Warnings())
		}
	})

	t.Run("relationship properties use the same property checks", func(t *testing.T) {
		p := validMetadataPack()
		p.RelationshipTypes = []blueprints.RelationshipTypeDef{
			{
				Name: "links", Label: "Links", SourceType: "Doc", TargetType: "Doc",
				Properties: map[string]any{
					"weight": map[string]any{"type": "number", "x-embedding-target": true},
					"broken": "not-a-mapping",
				},
			},
		}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Warnings(), "relationshipTypes[0].properties.broken", "must be a mapping") {
			t.Fatalf("expected relationship property mapping warning, got: %v", report.Warnings())
		}
		if !hasIssue(report.Warnings(), "relationshipTypes[0].properties.weight.x-embedding-target", "only meaningful on string") {
			t.Fatalf("expected relationship property embedding warning, got: %v", report.Warnings())
		}
	})
}
