package catalog

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func loadCat(t *testing.T) *Catalog {
	t.Helper()
	cat, err := Load()
	require.NoError(t, err)
	require.NotEmpty(t, cat.Types)
	require.NotEmpty(t, cat.Relationships)
	return cat
}

func hasType(types []Type, name string) bool {
	for _, t := range types {
		if t.Name == name {
			return true
		}
	}
	return false
}

func hasPair(rels []Relationship, name, source, target string) bool {
	for _, r := range rels {
		if r.Name == name && r.SourceType == source && r.TargetType == target {
			return true
		}
	}
	return false
}

func TestLoadIncludesRelationships(t *testing.T) {
	cat := loadCat(t)
	require.True(t, hasType(cat.Types, "Movie"))
	require.True(t, hasPair(cat.Relationships, "directed_by", "Movie", "Person"))

	seen := map[string]bool{}
	for _, r := range cat.Relationships {
		key := relationshipKey(r)
		require.False(t, seen[key], "duplicate relationship %q", key)
		seen[key] = true
	}
}

func TestLoadExpandsPluralRelationships(t *testing.T) {
	fsys := fstest.MapFS{
		"data/x.yaml": &fstest.MapFile{Data: []byte(`
name: x
version: 1.0.0
objectTypeSchemas:
  - name: A
  - name: B
relationshipTypeSchemas:
  - name: links
    sourceTypes: [A, B]
    targetTypes: [B]
`)},
	}
	cat, err := load(fsys, "data")
	require.NoError(t, err)
	require.Len(t, cat.Relationships, 2)
	require.True(t, hasPair(cat.Relationships, "links", "A", "B"))
	require.True(t, hasPair(cat.Relationships, "links", "B", "B"))
}

func TestProjectGatesByEndpoints(t *testing.T) {
	cat := loadCat(t)

	onlyMovie := Project(cat, []string{"Movie"})
	require.True(t, hasType(onlyMovie.Types, "Movie"))
	require.False(t, hasType(onlyMovie.Types, "Person"))
	require.False(t, hasType(onlyMovie.Types, "CreativeWork"))
	require.Empty(t, onlyMovie.Relationships)
	require.True(t, hasPair(onlyMovie.Deferred, "directed_by", "Movie", "Person"))

	withPerson := Project(cat, []string{"Movie", "Person"})
	require.True(t, hasPair(withPerson.Relationships, "directed_by", "Movie", "Person"))
	require.False(t, hasPair(withPerson.Deferred, "directed_by", "Movie", "Person"))
	require.True(t, hasPair(withPerson.Deferred, "produced_by", "Movie", "Organization"))
}

func TestProjectUnknownAndProblems(t *testing.T) {
	cat := &Catalog{
		Types: []Type{{Name: "A"}, {Name: "B"}},
		Relationships: []Relationship{
			{Name: "ok", SourceType: "A", TargetType: "B"},
			{Name: "dangling", SourceType: "A", TargetType: "Ghost"},
		},
	}
	proj := Project(cat, []string{"A", "Nope"})
	require.Equal(t, []string{"Nope"}, proj.Unknown)
	require.Len(t, proj.Problems, 1)
	require.Contains(t, proj.Problems[0], "Ghost")
}

func TestDetail(t *testing.T) {
	cat := loadCat(t)

	movie, ok := Detail(cat, "Movie")
	require.True(t, ok)
	require.Equal(t, []string{"CreativeWork", "Thing"}, movie.Ancestors)
	require.Empty(t, movie.Children)
	require.True(t, hasPair(movie.RelatedRelationships, "directed_by", "Movie", "Person"))

	creative, ok := Detail(cat, "CreativeWork")
	require.True(t, ok)
	for _, want := range []string{"Movie", "Article", "Book", "TVSeries", "SoftwareApplication"} {
		require.Contains(t, creative.Children, want)
	}

	root, ok := Detail(cat, "Thing")
	require.True(t, ok)
	require.Empty(t, root.Ancestors)
	for _, want := range []string{"Person", "Organization", "Event", "CreativeWork", "Product"} {
		require.Contains(t, root.Children, want)
	}

	_, ok = Detail(cat, "NotAType")
	require.False(t, ok)
}

func TestDetailCycleGuard(t *testing.T) {
	cat := &Catalog{Types: []Type{{Name: "A", Parent: "B"}, {Name: "B", Parent: "A"}}}
	d, ok := Detail(cat, "A")
	require.True(t, ok)
	require.Equal(t, []string{"B"}, d.Ancestors)
}

func TestResolveTypes(t *testing.T) {
	cat := loadCat(t)

	exact := ResolveTypes(cat, "Movie", 5)
	require.NotEmpty(t, exact)
	require.Equal(t, "Movie", exact[0].Name)
	require.Equal(t, 1.0, exact[0].Score)

	alias := ResolveTypes(cat, "film", 5)
	require.NotEmpty(t, alias)
	require.Equal(t, "Movie", alias[0].Name)
	require.Contains(t, alias[0].Matched, "alias")

	multi := ResolveTypes(cat, "tv series", 5)
	require.NotEmpty(t, multi)
	require.Equal(t, "TVSeries", multi[0].Name)

	require.Empty(t, ResolveTypes(cat, "zzzznotatype", 5))
	require.Empty(t, ResolveTypes(cat, "  ", 5))
	require.Empty(t, ResolveTypes(nil, "Movie", 5))
}

type keywordEmbedder struct{ enabled bool }

var embedKeys = []string{"film", "movie", "motion", "picture", "commerce", "product", "person", "organization", "event", "book", "series"}

func keywordVec(s string) []float32 {
	v := make([]float32, len(embedKeys))
	tokens := tokenize(s)
	for _, tok := range tokens {
		for i, k := range embedKeys {
			if tok == k {
				v[i] = 1
			}
		}
	}
	return v
}

func (e keywordEmbedder) IsEnabled() bool { return e.enabled }
func (e keywordEmbedder) EmbedQuery(_ context.Context, s string) ([]float32, error) {
	return keywordVec(s), nil
}
func (e keywordEmbedder) EmbedDocuments(_ context.Context, docs []string) ([][]float32, error) {
	out := make([][]float32, len(docs))
	for i, d := range docs {
		out[i] = keywordVec(d)
	}
	return out, nil
}

func TestSemantic(t *testing.T) {
	cat := loadCat(t)
	got, err := Semantic(context.Background(), cat, keywordEmbedder{enabled: true}, "motion", 5)
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Equal(t, "Movie", got[0].Name)
	require.Contains(t, got[0].Matched, "semantic")

	_, err = Semantic(context.Background(), cat, nil, "film", 5)
	require.True(t, errors.Is(err, ErrEmbeddingsUnavailable))
	_, err = Semantic(context.Background(), cat, keywordEmbedder{enabled: false}, "film", 5)
	require.True(t, errors.Is(err, ErrEmbeddingsUnavailable))
}

func TestHybridFallsBackToLexical(t *testing.T) {
	cat := loadCat(t)
	got, err := Hybrid(context.Background(), cat, nil, "film", 5)
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Equal(t, "Movie", got[0].Name)
}

func TestMergeMatches(t *testing.T) {
	lexical := []TypeMatch{{Name: "Movie"}, {Name: "CreativeWork"}}
	semantic := []TypeMatch{{Name: "CreativeWork"}, {Name: "Brand"}}
	require.Equal(t, []string{"Movie", "CreativeWork", "Brand"}, matchNames(mergeMatches(lexical, semantic, 10)))
	require.Equal(t, []string{"Movie", "CreativeWork"}, matchNames(mergeMatches(lexical, semantic, 2)))
}

func matchNames(ms []TypeMatch) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

func TestCosineSimilarity(t *testing.T) {
	require.InDelta(t, 1.0, cosineSimilarity([]float32{1, 0}, []float32{2, 0}), 1e-9)
	require.InDelta(t, 0.0, cosineSimilarity([]float32{1, 0}, []float32{0, 1}), 1e-9)
	require.Equal(t, 0.0, cosineSimilarity(nil, []float32{1}))
}

func TestListFiltersAndFacets(t *testing.T) {
	cat := loadCat(t)

	all := List(cat, ListFilter{})
	require.Equal(t, len(cat.Types), all.Total)
	require.Len(t, all.Types, len(cat.Types))
	require.NotEmpty(t, all.Categories)
	require.Equal(t, cat.Version, all.Version)

	media := List(cat, ListFilter{Category: "media"})
	require.NotEmpty(t, media.Types)
	for _, s := range media.Types {
		require.Equal(t, "media", s.Category)
	}

	film := List(cat, ListFilter{Query: "film"})
	require.True(t, hasSummary(film.Types, "Movie"))

	subtypes := List(cat, ListFilter{Parent: "CreativeWork"})
	require.True(t, hasSummary(subtypes.Types, "Movie"))
	require.False(t, hasSummary(subtypes.Types, "Person"))

	page := List(cat, ListFilter{Limit: 2, Offset: 1})
	require.Equal(t, len(cat.Types), page.Total)
	require.Len(t, page.Types, 2)
	require.Equal(t, all.Types[1].Name, page.Types[0].Name)
}

func hasSummary(list []Summary, name string) bool {
	for _, s := range list {
		if s.Name == name {
			return true
		}
	}
	return false
}
