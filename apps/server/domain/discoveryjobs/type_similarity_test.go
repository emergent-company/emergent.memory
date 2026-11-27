package discoveryjobs

import (
	"context"
	"strings"
	"testing"
)

func fakeComplete(resp string) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) { return resp, nil }
}

func TestClassifierTypeSimilarityResolve(t *testing.T) {
	m := NewClassifierTypeSimilarity(fakeComplete(
		`{"matches":[{"type":"Company","same_as":"Organization","confidence":0.95},{"type":"Location","same_as":null,"confidence":1.0}]}`),
		0.90, quietLogger())

	matches, err := m.Resolve(context.Background(),
		[]DiscoveredType{{TypeName: "Company"}, {TypeName: "Location"}},
		[]ExistingType{{Name: "Organization"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if matches["Company"] != "Organization" {
		t.Fatalf("Company match = %q, want Organization", matches["Company"])
	}
	if _, ok := matches["Location"]; ok {
		t.Fatalf("Location must not match (same_as null)")
	}
}

func TestClassifierTypeSimilarityBelowThreshold(t *testing.T) {
	m := NewClassifierTypeSimilarity(fakeComplete(
		`{"matches":[{"type":"Company","same_as":"Organization","confidence":0.7}]}`),
		0.90, quietLogger())

	matches, err := m.Resolve(context.Background(),
		[]DiscoveredType{{TypeName: "Company"}}, []ExistingType{{Name: "Organization"}})
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
		`{"matches":[{"type":"Company","same_as":"Zorg","confidence":0.99}]}`),
		0.90, quietLogger())

	matches, err := m.Resolve(context.Background(),
		[]DiscoveredType{{TypeName: "Company"}}, []ExistingType{{Name: "Organization"}})
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

// TestBuildTypeClassificationPromptKeepsCharacterAndPersonDistinct pins that
// the prompt keeps Character (fictional) and Person (real) distinct: the
// same-concept example must not cite them and the prompt must call the
// distinction out. A match adopts the canonical schema, so a bad example would
// collapse real people into fictional characters.
func TestBuildTypeClassificationPromptKeepsCharacterAndPersonDistinct(t *testing.T) {
	prompt := buildTypeClassificationPrompt(
		[]DiscoveredType{{TypeName: "Character", Description: "a fictional persona"}},
		[]ExistingType{{Name: "Person", Description: "a real human being"}})

	if strings.Contains(prompt, `"Character" and "Person" for a human`) {
		t.Fatal("prompt must not cite Character/Person as an example of the same concept")
	}
	if !strings.Contains(prompt, `"Company" and "Organization"`) {
		t.Fatal("prompt must keep a taxonomy-aligned same-concept example")
	}
	if !strings.Contains(prompt, "A fictional character is NOT a real human being") {
		t.Fatal("prompt must state that a fictional character is not a real human being")
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
	svc := &Service{typeSimilarity: &fakeTypeSimilarity{mapping: map[string]string{"Company": "Organization"}}, log: quietLogger()}
	types := []DiscoveredType{
		{TypeName: "Company", Description: "a company in the story"},
		{TypeName: "Location", Description: "a place"},
	}
	out := svc.mergeSemanticallySimilarTypes(context.Background(), types, []string{"Organization"}, nil)
	if out[0].TypeName != "Organization" {
		t.Fatalf("Company was not merged onto Organization, got %q", out[0].TypeName)
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
		typeSimilarity: &fakeTypeSimilarity{mapping: map[string]string{"Company": "Organization"}},
		taxonomy: []ExistingType{{
			Name:        "Organization",
			Description: "A company, institution or other organized group named in the content",
			Tags:        []string{"organization", "company"},
			Properties:  map[string]any{"name": map[string]any{"type": "string"}},
			Required:    []string{"name"},
		}},
		log: quietLogger(),
	}

	out := svc.mergeSemanticallySimilarTypes(context.Background(),
		[]DiscoveredType{{TypeName: "Company", Description: "a company", Properties: map[string]any{"foo": "bar"}}},
		nil, nil)

	got := out[0]
	if got.TypeName != "Organization" {
		t.Fatalf("renamed to %q, want Organization", got.TypeName)
	}
	if got.Description != "A company, institution or other organized group named in the content" {
		t.Fatalf("description not adopted: %q", got.Description)
	}
	if _, ok := got.Properties["name"]; !ok {
		t.Fatalf("properties not adopted: %v", got.Properties)
	}
	if len(got.RequiredProperties) != 1 || got.RequiredProperties[0] != "name" {
		t.Fatalf("required not adopted: %v", got.RequiredProperties)
	}
}

// TestMergeSemanticallySimilarTypesInstalledTypeKeepsOwnSchema pins that a
// project type that is already installed under a taxonomy name is authoritative:
// a match onto it must not replace its description/properties/required with the
// taxonomy schema.
func TestMergeSemanticallySimilarTypesInstalledTypeKeepsOwnSchema(t *testing.T) {
	svc := &Service{
		typeSimilarity: &fakeTypeSimilarity{mapping: map[string]string{"Studio": "Organization"}},
		taxonomy: []ExistingType{{
			Name:        "Organization",
			Description: "taxonomy description",
			Properties:  map[string]any{"name": map[string]any{"type": "string"}},
			Required:    []string{"name"},
		}},
		log: quietLogger(),
	}

	out := svc.mergeSemanticallySimilarTypes(context.Background(),
		[]DiscoveredType{{
			TypeName:    "Studio",
			Description: "the project's own description",
			Properties:  map[string]any{"founded": map[string]any{"type": "number"}},
		}},
		[]string{"Organization"}, nil)

	got := out[0]
	if got.TypeName != "Organization" {
		t.Fatalf("renamed to %q, want Organization", got.TypeName)
	}
	if got.Description != "the project's own description" {
		t.Fatalf("installed type must keep its description, got %q", got.Description)
	}
	if _, ok := got.Properties["name"]; ok {
		t.Fatalf("installed type must not inherit taxonomy properties: %v", got.Properties)
	}
	if _, ok := got.Properties["founded"]; !ok {
		t.Fatalf("installed type must keep its own properties: %v", got.Properties)
	}
	if len(got.RequiredProperties) != 0 {
		t.Fatalf("installed type must not inherit taxonomy required, got %v", got.RequiredProperties)
	}
}

// TestMergeSemanticallySimilarTypesDeepCopiesTaxonomySchema pins that adopting a
// taxonomy schema copies it: mutating the per-run discovered type must never
// corrupt the process-lived canonical taxonomy for later runs.
func TestMergeSemanticallySimilarTypesDeepCopiesTaxonomySchema(t *testing.T) {
	taxonomyProps := map[string]any{
		"name": map[string]any{"type": "string"},
		"tags": []any{"organization", "company"},
	}
	svc := &Service{
		typeSimilarity: &fakeTypeSimilarity{mapping: map[string]string{"Studio": "Organization"}},
		taxonomy: []ExistingType{{
			Name:       "Organization",
			Properties: taxonomyProps,
			Required:   []string{"name"},
		}},
		log: quietLogger(),
	}

	out := svc.mergeSemanticallySimilarTypes(context.Background(),
		[]DiscoveredType{{TypeName: "Studio"}}, nil, nil)

	out[0].Properties["name"].(map[string]any)["type"] = "number"
	out[0].Properties["tags"].([]any)[0] = "mutated"
	out[0].Properties["extra"] = true
	out[0].RequiredProperties[0] = "mutated"

	if got := taxonomyProps["name"].(map[string]any)["type"]; got != "string" {
		t.Fatalf("nested taxonomy property was aliased, got %v", got)
	}
	if got := taxonomyProps["tags"].([]any)[0]; got != "organization" {
		t.Fatalf("nested taxonomy slice was aliased, got %v", got)
	}
	if _, ok := taxonomyProps["extra"]; ok {
		t.Fatal("taxonomy map gained a key through the adopted copy")
	}
	if got := svc.taxonomy[0].Required[0]; got != "name" {
		t.Fatalf("taxonomy required slice was aliased, got %q", got)
	}
}

// TestTaxonomyUIStyle pins the curated taxonomy UI accent lookup used when a
// discovered type adopts a canonical taxonomy type.
func TestTaxonomyUIStyle(t *testing.T) {
	s := &Service{taxonomy: []ExistingType{
		{Name: "Movie", Icon: "tabler--movie", Color: "#DB2777"},
	}}
	icon, color, ok := s.taxonomyUIStyle("Movie")
	if !ok || icon != "tabler--movie" || color != "#DB2777" {
		t.Fatalf("taxonomyUIStyle(Movie) = %q, %q, %v", icon, color, ok)
	}
	if _, _, ok := s.taxonomyUIStyle("Nope"); ok {
		t.Fatal("taxonomyUIStyle(Nope) = ok, want not ok")
	}
}

// recordingTypeSimilarity records the candidate set the classifier is offered,
// so a test can assert the catalog resolution narrowed it.
type recordingTypeSimilarity struct {
	mapping     map[string]string
	gotExisting []string
}

func (f *recordingTypeSimilarity) Resolve(_ context.Context, discovered []DiscoveredType, existing []ExistingType) (map[string]string, error) {
	f.gotExisting = f.gotExisting[:0]
	for _, e := range existing {
		f.gotExisting = append(f.gotExisting, e.Name)
	}
	out := map[string]string{}
	for _, d := range discovered {
		if m, ok := f.mapping[d.TypeName]; ok {
			out[d.TypeName] = m
		}
	}
	return out, nil
}

// TestMergeSemanticallySimilarTypesNarrowsCatalogCandidates pins that the
// catalog's lexical resolution narrows the taxonomy offered to the classifier to
// the types that actually resemble a discovered name, instead of the whole
// catalog.
func TestMergeSemanticallySimilarTypesNarrowsCatalogCandidates(t *testing.T) {
	rec := &recordingTypeSimilarity{mapping: map[string]string{"Company": "Organization"}}
	svc := &Service{typeSimilarity: rec, log: quietLogger()}
	svc.SetTaxonomy([]ExistingType{
		{Name: "Organization", Tags: []string{"company", "business"}},
		{Name: "Person", Tags: []string{"human"}},
		{Name: "Recipe", Tags: []string{"food"}},
	})

	out := svc.mergeSemanticallySimilarTypes(context.Background(),
		[]DiscoveredType{{TypeName: "Company"}}, nil, nil)

	if out[0].TypeName != "Organization" {
		t.Fatalf("Company -> %q, want Organization", out[0].TypeName)
	}
	if len(rec.gotExisting) != 1 || rec.gotExisting[0] != "Organization" {
		t.Fatalf("classifier candidates = %v, want only [Organization]", rec.gotExisting)
	}
}

// TestMergeSemanticallySimilarTypesFallsBackToFullTaxonomy pins that a discovered
// type with no lexical catalog match still sees the full taxonomy, so narrowing
// never costs recall for names the lexical resolver cannot place.
func TestMergeSemanticallySimilarTypesFallsBackToFullTaxonomy(t *testing.T) {
	rec := &recordingTypeSimilarity{}
	svc := &Service{typeSimilarity: rec, log: quietLogger()}
	svc.SetTaxonomy([]ExistingType{
		{Name: "Organization", Tags: []string{"company"}},
		{Name: "Person", Tags: []string{"human"}},
		{Name: "Recipe", Tags: []string{"food"}},
	})

	svc.mergeSemanticallySimilarTypes(context.Background(),
		[]DiscoveredType{{TypeName: "Zzz"}}, nil, nil)

	if len(rec.gotExisting) != 3 {
		t.Fatalf("classifier candidates = %v, want all 3 taxonomy types (fallback)", rec.gotExisting)
	}
}
