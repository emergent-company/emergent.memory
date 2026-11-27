package discoveryjobs

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/blueprints"
)

func TestRelationshipPromptIncludesCatalogVocabulary(t *testing.T) {
	svc := &Service{log: quietLogger()}
	svc.SetTaxonomy(nil, []blueprints.RelationshipTypeDef{
		{Name: "works_for", Label: "Works for", SourceType: "Person", TargetType: "Organization"},
		{Name: "home_location", Label: "Home location", SourceType: "Person", TargetType: "Place"},
	})
	prompt := svc.buildRelationshipDiscoveryPrompt([]DiscoveredType{{TypeName: "Person"}}, "test")
	for _, want := range []string{"works_for", "home_location", "Person -> Organization", "reuse the exact name"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("relationship prompt missing %q", want)
		}
	}
}

func TestRelationshipVocabularyEmptyWithoutTaxonomy(t *testing.T) {
	svc := &Service{log: quietLogger()}
	if got := svc.buildRelationshipVocabulary(); got != "" {
		t.Fatalf("empty taxonomy must yield empty vocabulary, got %q", got)
	}
}

// TestNormalizeRelationshipTypes pins the deterministic alias pass: invented
// edge names snap to the catalog vocabulary, an already-canonical name is left
// alone, and an unknown name is untouched.
func TestNormalizeRelationshipTypes(t *testing.T) {
	svc := &Service{log: quietLogger()}
	svc.SetTaxonomy(nil, []blueprints.RelationshipTypeDef{
		{Name: "works_for"}, {Name: "located_at"}, {Name: "home_location"}, {Name: "featured_actor"},
	})
	rels := []DiscoveredRelationship{
		{RelationType: "employed_by"},
		{RelationType: "located_in"},
		{RelationType: "resides_at"},
		{RelationType: "appears_in"},
		{RelationType: "works_for"},
		{RelationType: "random_thing"},
	}
	out := svc.normalizeRelationshipTypes(rels)
	want := []string{"works_for", "located_at", "home_location", "featured_actor", "works_for", "random_thing"}
	for i := range want {
		if out[i].RelationType != want[i] {
			t.Fatalf("rel[%d] = %q, want %q", i, out[i].RelationType, want[i])
		}
	}
}

// TestNormalizeRelationshipTypesSkipsUnknownTarget pins that an alias whose
// canonical target is not in the loaded vocabulary is never applied.
func TestNormalizeRelationshipTypesSkipsUnknownTarget(t *testing.T) {
	svc := &Service{log: quietLogger()}
	svc.SetTaxonomy(nil, []blueprints.RelationshipTypeDef{{Name: "works_for"}})
	out := svc.normalizeRelationshipTypes([]DiscoveredRelationship{{RelationType: "located_in"}})
	if out[0].RelationType != "located_in" {
		t.Fatalf("alias to a missing canonical target must not apply, got %q", out[0].RelationType)
	}
}
