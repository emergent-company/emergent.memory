package discoveryjobs

import (
	"context"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/blueprints"
)

func TestRelationshipPromptIncludesCatalogVocabulary(t *testing.T) {
	svc := &Service{log: quietLogger()}
	svc.SetRelationshipTaxonomy([]blueprints.RelationshipTypeDef{
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

// TestAlignRelationshipTypes pins the classifier safety net: an invented edge
// name is renamed onto the catalog vocabulary when the classifier is confident,
// while an already-canonical name is left untouched.
func TestAlignRelationshipTypes(t *testing.T) {
	svc := &Service{
		typeSimilarity: &fakeTypeSimilarity{mapping: map[string]string{"employed by": "works_for"}},
		log:            quietLogger(),
	}
	svc.SetRelationshipTaxonomy([]blueprints.RelationshipTypeDef{
		{Name: "works_for", Description: "The person is employed by the organization."},
		{Name: "located_at", Description: "The event is located at a place."},
	})
	rels := []DiscoveredRelationship{
		{RelationType: "employed by", SourceType: "Character", TargetType: "Organization"},
		{RelationType: "located_at", SourceType: "Event", TargetType: "Place"},
	}
	out := svc.alignRelationshipTypes(context.Background(), rels)
	if out[0].RelationType != "works_for" {
		t.Fatalf("employed by -> %q, want works_for", out[0].RelationType)
	}
	if out[1].RelationType != "located_at" {
		t.Fatalf("already-canonical edge must stay, got %q", out[1].RelationType)
	}
}

func TestAlignRelationshipTypesNilClassifier(t *testing.T) {
	svc := &Service{log: quietLogger()} // typeSimilarity nil
	out := svc.alignRelationshipTypes(context.Background(), []DiscoveredRelationship{{RelationType: "employed by"}})
	if out[0].RelationType != "employed by" {
		t.Fatalf("nil classifier must be a no-op, got %q", out[0].RelationType)
	}
}
