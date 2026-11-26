package discoveryjobs

import (
	"context"
	"testing"
)

type fakeEmbedder struct {
	vecs [][]float32
}

func (f *fakeEmbedder) EmbedDocuments(_ context.Context, _ []string) ([][]float32, error) {
	return f.vecs, nil
}

func TestCosineSimilarity(t *testing.T) {
	if s := cosineSimilarity([]float32{1, 0}, []float32{1, 0}); s != 1.0 {
		t.Fatalf("identical = %v, want 1.0", s)
	}
	if s := cosineSimilarity([]float32{1, 0}, []float32{0, 1}); s != 0.0 {
		t.Fatalf("orthogonal = %v, want 0.0", s)
	}
	if s := cosineSimilarity(nil, []float32{1, 0}); s != 0.0 {
		t.Fatalf("empty = %v, want 0.0", s)
	}
}

func TestEmbeddingTypeSimilarityResolve(t *testing.T) {
	// query [1,0]; existing A [0,1] (sim 0), existing B [1,0] (sim 1) -> B wins.
	m := NewEmbeddingTypeSimilarity(&fakeEmbedder{vecs: [][]float32{{1, 0}, {0, 1}, {1, 0}}}, 0.85, quietLogger())
	match, err := m.Resolve(context.Background(), DiscoveredType{TypeName: "X"}, []ExistingType{{Name: "A"}, {Name: "B"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if match != "B" {
		t.Fatalf("match = %q, want B", match)
	}
}

func TestEmbeddingTypeSimilarityBelowThreshold(t *testing.T) {
	// query [1,0]; existing [0,1] (sim 0) -> no match.
	m := NewEmbeddingTypeSimilarity(&fakeEmbedder{vecs: [][]float32{{1, 0}, {0, 1}}}, 0.85, quietLogger())
	match, err := m.Resolve(context.Background(), DiscoveredType{TypeName: "X"}, []ExistingType{{Name: "A"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if match != "" {
		t.Fatalf("match = %q, want empty", match)
	}
}

type fakeTypeSimilarity struct{ mapping map[string]string }

func (f *fakeTypeSimilarity) Resolve(_ context.Context, d DiscoveredType, _ []ExistingType) (string, error) {
	return f.mapping[d.TypeName], nil
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
	// "Person" already matches a registered type by spelling; the resolver must
	// not be consulted (and would return "" anyway).
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
