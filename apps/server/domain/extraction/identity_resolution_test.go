package extraction

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/projects"
)

// TestResolveExtractionIdentityReusesExistingKey is the #1074 core: a re-mention
// the model tied to an existing object must persist under that object's stored
// key even when speech-to-text produced a different spelling, so the merge sees
// the same identity instead of a near-duplicate.
func TestResolveExtractionIdentityReusesExistingKey(t *testing.T) {
	existing := []agents.ExistingEntityContext{{
		ID:       "canonical-1",
		Name:     "Katarzyna Nowak",
		TypeName: "Person",
		Key:      "katarzyna-nowak",
	}}
	index := indexExistingEntities(existing)

	key, name := resolveExtractionIdentity(agents.InternalEntity{
		Name:             "Katarzyna Nowack",
		Type:             "Person",
		ExistingEntityID: "canonical-1",
	}, index)

	if key != "katarzyna-nowak" {
		t.Errorf("key = %q, want the existing object's stored key", key)
	}
	if name != "Katarzyna Nowak" {
		t.Errorf("name = %q, want the existing display name", name)
	}
}

// TestResolveExtractionIdentityWithoutReference covers the historical behaviour:
// no model reference means the trimmed extracted name is the identity.
func TestResolveExtractionIdentityWithoutReference(t *testing.T) {
	key, name := resolveExtractionIdentity(agents.InternalEntity{
		Name: "  Anna Kowalska  ",
		Type: "Person",
	}, nil)

	if key != "Anna Kowalska" || name != "Anna Kowalska" {
		t.Errorf("key/name = %q/%q, want the trimmed extracted name", key, name)
	}
}

// TestResolveExtractionIdentityIgnoresTypeMismatch is the guard against a
// hallucinated or stale ID: the context names a different kind of object, so
// falling back to the extracted name is safer than merging into it.
func TestResolveExtractionIdentityIgnoresTypeMismatch(t *testing.T) {
	index := indexExistingEntities([]agents.ExistingEntityContext{{
		ID:       "canonical-1",
		Name:     "Project Atlas",
		TypeName: "Project",
		Key:      "atlas",
	}})

	key, name := resolveExtractionIdentity(agents.InternalEntity{
		Name:             "Atlas",
		Type:             "Person",
		ExistingEntityID: "canonical-1",
	}, index)

	if key != "Atlas" || name != "Atlas" {
		t.Errorf("key/name = %q/%q, want the extracted name on a type mismatch", key, name)
	}
}

// TestResolveExtractionIdentityUnknownIDFallsBack covers an ID the model
// invented or that fell outside the top-k context.
func TestResolveExtractionIdentityUnknownIDFallsBack(t *testing.T) {
	key, name := resolveExtractionIdentity(agents.InternalEntity{
		Name:             "Marek",
		Type:             "Person",
		ExistingEntityID: "not-in-context",
	}, indexExistingEntities([]agents.ExistingEntityContext{{ID: "other", Name: "Other", TypeName: "Person"}}))

	if key != "Marek" || name != "Marek" {
		t.Errorf("key/name = %q/%q, want the extracted name", key, name)
	}
}

// TestExistingEntitiesFromSearch pins the prompt context conversion: vector
// search hits become IDs/names/types/descriptions, unusable hits are dropped,
// and the result is capped.
func TestExistingEntitiesFromSearch(t *testing.T) {
	name := "Katarzyna Nowak"
	key := "katarzyna-nowak"
	canonical := uuid.New()
	resp := &graph.SearchResponse{Data: []*graph.SearchResultItem{
		{
			Object: &graph.GraphObjectResponse{
				ID:          uuid.New(),
				CanonicalID: canonical,
				Type:        "Person",
				Name:        &name,
				Key:         &key,
				Properties:  map[string]any{"description": "Board chair"},
			},
			Score: 0.91,
		},
		{Object: nil},
		{
			Object: &graph.GraphObjectResponse{
				ID:         uuid.New(),
				Type:       "Person",
				Properties: map[string]any{},
			},
		},
	}}

	got := existingEntitiesFromSearch(resp, existingEntityContextLimit)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 usable hit", len(got))
	}
	if got[0].ID != canonical.String() || got[0].Name != name || got[0].TypeName != "Person" ||
		got[0].Key != key || got[0].Description != "Board chair" {
		t.Errorf("context = %+v, want the hit's id/name/type/key/description", got[0])
	}
	if got[0].Similarity < 0.909 || got[0].Similarity > 0.911 {
		t.Errorf("Similarity = %v, want 0.91", got[0].Similarity)
	}

	if capped := existingEntitiesFromSearch(resp, 0); len(capped) != 1 {
		t.Errorf("limit 0 should not drop usable hits, got %d", len(capped))
	}
}

// TestExistingEntityTypesUsesEnabledTypesFirst checks the retrieval scope:
// enabled types when the job set them (sorted/deduped), otherwise every schema
// type.
func TestExistingEntityTypesUsesEnabledTypesFirst(t *testing.T) {
	schemas := map[string]agents.ObjectSchema{"Person": {}, "Project": {}}
	if got := existingEntityTypes([]string{" Project ", "Person", "Project", ""}, schemas); !reflect.DeepEqual(got, []string{"Person", "Project"}) {
		t.Errorf("enabled types = %v, want sorted unique [Person Project]", got)
	}
	if got := existingEntityTypes(nil, schemas); !reflect.DeepEqual(got, []string{"Person", "Project"}) {
		t.Errorf("schema types = %v, want sorted [Person Project]", got)
	}
	if got := existingEntityTypes(nil, nil); len(got) != 0 {
		t.Errorf("no schemas should mean no query types, got %v", got)
	}
}

// TestAutoMergeConfigForProject pins the project setting plumbing (#1074):
// auto-merge stays off unless the project opted in, and the chosen policy is
// used verbatim ("enrich" must not be normalized to "partial").
func TestAutoMergeConfigForProject(t *testing.T) {
	if enabled, policy := autoMergeConfigForProject(nil); enabled || policy != projects.AutoMergePolicyPartial {
		t.Errorf("nil project = %v/%q, want disabled/partial", enabled, policy)
	}

	off := &projects.Project{AutoMergeExtractionPolicy: projects.AutoMergePolicyEnrich}
	if enabled, _ := autoMergeConfigForProject(off); enabled {
		t.Error("auto-merge enabled for a project that did not opt in")
	}

	on := &projects.Project{
		AutoMergeExtractionBranches: true,
		AutoMergeExtractionPolicy:   projects.AutoMergePolicyEnrich,
	}
	if enabled, policy := autoMergeConfigForProject(on); !enabled || policy != projects.AutoMergePolicyEnrich {
		t.Errorf("opted-in project = %v/%q, want enabled/enrich", enabled, policy)
	}

	unknown := &projects.Project{
		AutoMergeExtractionBranches: true,
		AutoMergeExtractionPolicy:   "overwrite",
	}
	if _, policy := autoMergeConfigForProject(unknown); policy != projects.AutoMergePolicyPartial {
		t.Errorf("unknown stored policy = %q, want the safe partial default", policy)
	}
}
