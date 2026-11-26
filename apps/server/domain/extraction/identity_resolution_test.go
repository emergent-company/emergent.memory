package extraction

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
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
// invented or that fell outside the prompt context.
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

// TestResolveExtractionIdentityRequiresExactType covers case-differing type
// names: schema type names are identifiers ("Person" and "person" are different
// types) and the merge's identity key compares them exactly, so a case-only
// match must fall back instead of persisting under a type the schema does not
// have (#1123).
func TestResolveExtractionIdentityRequiresExactType(t *testing.T) {
	index := indexExistingEntities([]agents.ExistingEntityContext{{
		ID:       "canonical-1",
		Name:     "Katarzyna Nowak",
		TypeName: "Person",
		Key:      "katarzyna-nowak",
	}})

	key, name := resolveExtractionIdentity(agents.InternalEntity{
		Name:             "Katarzyna Nowack",
		Type:             "person",
		ExistingEntityID: "canonical-1",
	}, index)

	if key != "Katarzyna Nowack" || name != "Katarzyna Nowack" {
		t.Errorf("key/name = %q/%q, want the extracted name on a case-differing type", key, name)
	}
}
