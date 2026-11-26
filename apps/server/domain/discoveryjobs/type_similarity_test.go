package discoveryjobs

import (
	"context"
	"testing"
)

func fakeComplete(resp string) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) { return resp, nil }
}

func TestClassifierTypeSimilarityResolve(t *testing.T) {
	m := NewClassifierTypeSimilarity(fakeComplete(
		`{"matches":[{"type":"Character","same_as":"Person","confidence":0.95},{"type":"Location","same_as":null,"confidence":1.0}]}`),
		0.90, quietLogger())

	matches, err := m.Resolve(context.Background(),
		[]DiscoveredType{{TypeName: "Character"}, {TypeName: "Location"}},
		[]ExistingType{{Name: "Person"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if matches["Character"] != "Person" {
		t.Fatalf("Character match = %q, want Person", matches["Character"])
	}
	if _, ok := matches["Location"]; ok {
		t.Fatalf("Location must not match (same_as null)")
	}
}

func TestClassifierTypeSimilarityBelowThreshold(t *testing.T) {
	m := NewClassifierTypeSimilarity(fakeComplete(
		`{"matches":[{"type":"Character","same_as":"Person","confidence":0.7}]}`),
		0.90, quietLogger())

	matches, err := m.Resolve(context.Background(),
		[]DiscoveredType{{TypeName: "Character"}}, []ExistingType{{Name: "Person"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("low confidence must not match, got %v", matches)
	}
}

func TestClassifierTypeSimilarityRejectsHallucinatedExisting(t *testing.T) {
	// The model returns an existing name that is not in the comparison set.
	m := NewClassifierTypeSimilarity(fakeComplete(
		`{"matches":[{"type":"Character","same_as":"Zorg","confidence":0.99}]}`),
		0.90, quietLogger())

	matches, err := m.Resolve(context.Background(),
		[]DiscoveredType{{TypeName: "Character"}}, []ExistingType{{Name: "Person"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("hallucinated existing name must be ignored, got %v", matches)
	}
}

func TestBuildTypeClassificationPromptDeterministic(t *testing.T) {
	discovered := []DiscoveredType{
		{TypeName: "Location", Description: "a place"},
		{TypeName: "Character", Description: "a character"},
	}
	existing := []ExistingType{{Name: "Person", Description: "a person"}}
	a := buildTypeClassificationPrompt(discovered, existing)
	b := buildTypeClassificationPrompt(discovered, existing)
	if a != b {
		t.Fatal("prompt must be deterministic for the same inputs")
	}
}

type fakeTypeSimilarity struct{ mapping map[string]string }

func (f *fakeTypeSimilarity) Resolve(_ context.Context, discovered []DiscoveredType, _ []ExistingType) (map[string]string, error) {
	out := map[string]string{}
	for _, d := range discovered {
		if m, ok := f.mapping[d.TypeName]; ok {
			out[d.TypeName] = m
		}
	}
	return out, nil
}

func TestMergeSemanticallySimilarTypes(t *testing.T) {
	svc := &Service{typeSimilarity: &fakeTypeSimilarity{mapping: map[string]string{"Character": "Person"}}, log: quietLogger()}
	types := []DiscoveredType{
		{TypeName: "Character", Description: "a character in a story"},
		{TypeName: "Location", Description: "a place"},
	}
	out := svc.mergeSemanticallySimilarTypes(context.Background(), types, []string{"Person"}, nil)
	if out[0].TypeName != "Person" {
		t.Fatalf("Character was not merged onto Person, got %q", out[0].TypeName)
	}
	if out[1].TypeName != "Location" {
		t.Fatalf("Location should stay, got %q", out[1].TypeName)
	}
}

func TestMergeSemanticallySimilarTypesSpellingWins(t *testing.T) {
	svc := &Service{typeSimilarity: &fakeTypeSimilarity{mapping: map[string]string{"Person": "Organization"}}, log: quietLogger()}
	out := svc.mergeSemanticallySimilarTypes(context.Background(),
		[]DiscoveredType{{TypeName: "Person"}}, []string{"Person"}, nil)
	if out[0].TypeName != "Person" {
		t.Fatalf("spelling match must stay, got %q", out[0].TypeName)
	}
}

func TestMergeSemanticallySimilarTypesNilResolver(t *testing.T) {
	svc := &Service{log: quietLogger()} // typeSimilarity nil
	out := svc.mergeSemanticallySimilarTypes(context.Background(),
		[]DiscoveredType{{TypeName: "Character"}}, []string{"Person"}, nil)
	if out[0].TypeName != "Character" {
		t.Fatalf("nil resolver must be a no-op, got %q", out[0].TypeName)
	}
}

func TestMergeSemanticallySimilarTypesAdoptsTaxonomySchema(t *testing.T) {
	svc := &Service{
		typeSimilarity: &fakeTypeSimilarity{mapping: map[string]string{"Person": "Character"}},
		taxonomy: []ExistingType{{
			Name:        "Character",
			Description: "A fictional character in a TV show",
			Tags:        []string{"character", "persona"},
			Properties:  map[string]any{"name": map[string]any{"type": "string"}},
			Required:    []string{"name"},
		}},
		log: quietLogger(),
	}

	out := svc.mergeSemanticallySimilarTypes(context.Background(),
		[]DiscoveredType{{TypeName: "Person", Description: "a person", Properties: map[string]any{"foo": "bar"}}},
		nil, nil)

	got := out[0]
	if got.TypeName != "Character" {
		t.Fatalf("renamed to %q, want Character", got.TypeName)
	}
	if got.Description != "A fictional character in a TV show" {
		t.Fatalf("description not adopted: %q", got.Description)
	}
	if _, ok := got.Properties["name"]; !ok {
		t.Fatalf("properties not adopted: %v", got.Properties)
	}
	if len(got.RequiredProperties) != 1 || got.RequiredProperties[0] != "name" {
		t.Fatalf("required not adopted: %v", got.RequiredProperties)
	}
}
