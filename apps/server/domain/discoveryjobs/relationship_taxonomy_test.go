package discoveryjobs

import (
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
