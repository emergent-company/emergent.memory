package blueprints_test

import (
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
)

// TestValidateUIConfigViewPlan pins offline validation of a type's `ui.view`
// block. The renderer treats a view it cannot parse as "no view" — a typo
// therefore costs the author the whole layout without any error anywhere, so
// the validator has to be the place that says so.
func TestValidateUIConfigViewPlan(t *testing.T) {
	withView := func(view map[string]any) blueprints.PackFile {
		p := validMetadataPack()
		p.ObjectTypes[0].UI["view"] = view
		return p
	}

	t.Run("a well-formed plan validates clean", func(t *testing.T) {
		p := withView(map[string]any{
			"inline": []any{"status", "source"},
			"more":   false,
			"sections": []any{
				map[string]any{"title": "Affects", "relation": "affects", "dir": "out", "limit": 4},
				map[string]any{"relation": []any{"fixes", "spawned_by"}},
			},
		})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if len(report.Issues) != 0 {
			t.Fatalf("expected a valid view to validate clean, got: %v", report.Issues)
		}
	})

	t.Run("typo in a top-level key is reported", func(t *testing.T) {
		p := withView(map[string]any{"sectons": []any{}})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "ui.view.sectons", "unknown key") {
			t.Fatalf("expected an unknown-key finding, got: %v", report.Issues)
		}
	})

	t.Run("typo in a section key is reported", func(t *testing.T) {
		p := withView(map[string]any{"sections": []any{
			map[string]any{"relation": "affects", "limt": 4},
		}})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "sections[0].limt", "unknown key") {
			t.Fatalf("expected a section unknown-key finding, got: %v", report.Issues)
		}
	})

	t.Run("a section without a relation is reported", func(t *testing.T) {
		p := withView(map[string]any{"sections": []any{map[string]any{"title": "Affects"}}})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "sections[0].relation", "required") {
			t.Fatalf("expected a missing-relation finding, got: %v", report.Issues)
		}
	})

	t.Run("an unknown direction is reported", func(t *testing.T) {
		p := withView(map[string]any{"sections": []any{
			map[string]any{"relation": "affects", "dir": "sideways"},
		}})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "sections[0].dir", "must be in, out or both") {
			t.Fatalf("expected a direction finding, got: %v", report.Issues)
		}
	})

	t.Run("an out-of-range limit is reported", func(t *testing.T) {
		p := withView(map[string]any{"sections": []any{
			map[string]any{"relation": "affects", "limit": 500},
		}})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "sections[0].limit", "between 0 and") {
			t.Fatalf("expected a limit finding, got: %v", report.Issues)
		}
	})

	t.Run("a non-integer limit is reported", func(t *testing.T) {
		p := withView(map[string]any{"sections": []any{
			map[string]any{"relation": "affects", "limit": "four"},
		}})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "sections[0].limit", "whole number") {
			t.Fatalf("expected a limit-type finding, got: %v", report.Issues)
		}
	})

	t.Run("sections must be a list", func(t *testing.T) {
		p := withView(map[string]any{"sections": map[string]any{"relation": "affects"}})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "ui.view.sections", "must be a list") {
			t.Fatalf("expected a sections-shape finding, got: %v", report.Issues)
		}
	})

	t.Run("a non-boolean more is reported", func(t *testing.T) {
		p := withView(map[string]any{"more": "yes"})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "ui.view.more", "true or false") {
			t.Fatalf("expected a more-type finding, got: %v", report.Issues)
		}
	})

	t.Run("a relation no pack declares is reported", func(t *testing.T) {
		p := withView(map[string]any{"sections": []any{
			map[string]any{"relation": "affects"},
		}})
		p.RelationshipTypes = []blueprints.RelationshipTypeDef{{Name: "fixes", Label: "Fixes"}}
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "sections[0].relation", `"affects" is not a relationshipType`) {
			t.Fatalf("expected an undeclared-relation finding, got: %v", report.Issues)
		}
	})

	t.Run("a relation a sibling pack declares validates clean", func(t *testing.T) {
		p := withView(map[string]any{"sections": []any{
			map[string]any{"relation": []any{"affects", "fixes"}},
		}})
		p.RelationshipTypes = []blueprints.RelationshipTypeDef{{Name: "affects"}}
		// "fixes" arrives from a second pack in the same blueprint: the known
		// set is blueprint-wide, or a shared relationship type would be
		// reported for every pack that did not happen to declare it.
		other := validMetadataPack()
		other.Name = "other"
		other.SourceFile = "schemas/other.yaml"
		other.RelationshipTypes = []blueprints.RelationshipTypeDef{{Name: "fixes"}}
		report := blueprints.Validate(nil, []blueprints.PackFile{p, other}, nil, nil, nil, nil, nil)
		if hasIssue(report.Issues, "sections[0].relation", "not a relationshipType") {
			t.Fatalf("a declared relation must validate clean, got: %v", report.Issues)
		}
	})

	t.Run("a non-string title is reported", func(t *testing.T) {
		// Unquoted YAML year: the renderer decodes title as a string and drops
		// the whole view, so the validator has to name it.
		p := withView(map[string]any{"sections": []any{
			map[string]any{"title": 2024, "relation": "affects"},
		}})
		report := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
		if !hasIssue(report.Issues, "sections[0].title", "must be a string") {
			t.Fatalf("expected a title-type finding, got: %v", report.Issues)
		}
	})
}

// TestValidateUIConfigProjectRelationshipTypes pins #667.6: a view may present a
// relationship type a companion pack installed earlier owns, so the install gate
// (which reads the project's compiled types) must accept it, while the offline
// validator — which only knows the blueprint — still points the author at the
// missing declaration.
func TestValidateUIConfigProjectRelationshipTypes(t *testing.T) {
	p := validMetadataPack()
	p.ObjectTypes[0].UI["view"] = map[string]any{"sections": []any{
		map[string]any{"relation": "serves"},
	}}
	p.RelationshipTypes = []blueprints.RelationshipTypeDef{{Name: "fixes"}}

	offline := blueprints.Validate(nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil)
	if !hasIssue(offline.Issues, "sections[0].relation", "not a relationshipType declared by this blueprint") {
		t.Fatalf("offline validation must still warn about an undeclared relation, got: %v", offline.Issues)
	}

	gated := blueprints.PreflightWithProjectTypes(
		blueprints.KnownProjectTypes{RelationshipTypes: []string{"serves"}},
		nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil,
	)
	if hasIssue(gated.Issues, "sections[0].relation", "not a relationshipType") {
		t.Fatalf("a project-owned relationship type must satisfy the view check, got: %v", gated.Issues)
	}

	// An unknown name is still reported when the project set does not contain
	// it, and the message no longer promises "another installed blueprint" —
	// the gate already checked the project.
	unknown := blueprints.PreflightWithProjectTypes(
		blueprints.KnownProjectTypes{RelationshipTypes: []string{"something-else"}},
		nil, []blueprints.PackFile{p}, nil, nil, nil, nil, nil,
	)
	if !hasIssue(unknown.Issues, "sections[0].relation", "not a relationshipType declared by this blueprint or the project") {
		t.Fatalf("an unknown relation must still be reported by the gate, got: %v", unknown.Issues)
	}
}

// TestHasUnknownViewRelationshipTypes pins the install path's lazy pre-check: the
// project's relationship types are read only when the blueprint's own packs
// cannot answer, so a blueprint that fails offline validation still aborts with
// zero requests.
func TestHasUnknownViewRelationshipTypes(t *testing.T) {
	withView := func(relation any) blueprints.PackFile {
		p := validMetadataPack()
		p.ObjectTypes[0].UI["view"] = map[string]any{"sections": []any{
			map[string]any{"relation": relation},
		}}
		return p
	}

	unknown := withView([]any{"serves", "fixes"})
	unknown.RelationshipTypes = []blueprints.RelationshipTypeDef{{Name: "fixes"}}
	if !blueprints.HasUnknownViewRelationshipTypes([]blueprints.PackFile{unknown}) {
		t.Fatal("a view naming a relation no pack declares must ask for the project's relationship types")
	}

	known := withView("fixes")
	known.RelationshipTypes = []blueprints.RelationshipTypeDef{{Name: "fixes"}}
	if blueprints.HasUnknownViewRelationshipTypes([]blueprints.PackFile{known}) {
		t.Fatal("a view whose relations every pack declares must not trigger a project read")
	}

	// Pack-level uiConfigs can carry the view instead of the inline ui block.
	packLevel := validMetadataPack()
	packLevel.UIConfigs = blueprints.JSONBlob(`{"Doc":{"view":{"sections":[{"relation":"serves"}]}}}`)
	if !blueprints.HasUnknownViewRelationshipTypes([]blueprints.PackFile{packLevel}) {
		t.Fatal("a pack-level uiConfigs view naming an unknown relation must ask for the project's relationship types")
	}

	// A malformed view is a validation finding, not a reason to read the
	// project: there is no resolvable name to look up.
	malformed := validMetadataPack()
	malformed.ObjectTypes[0].UI["view"] = "not-a-map"
	if blueprints.HasUnknownViewRelationshipTypes([]blueprints.PackFile{malformed}) {
		t.Fatal("a malformed view must not trigger a project read")
	}
}
