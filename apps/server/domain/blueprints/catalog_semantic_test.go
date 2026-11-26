package blueprints

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// keywordEmbedder maps a fixed vocabulary onto one-hot-ish vectors so semantic
// ranking is deterministic in tests.
type keywordEmbedder struct {
	enabled bool
}

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

func TestResolveCatalogSemantic(t *testing.T) {
	s := &Service{catalogEmbedder: keywordEmbedder{enabled: true}}
	got, err := s.ResolveCatalogSemantic(context.Background(), "motion", 5)
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Equal(t, "Movie", got[0].Name)
	require.Contains(t, got[0].Matched, "semantic")
}

func TestResolveCatalogSemanticUnavailable(t *testing.T) {
	// nil embedder
	_, err := (&Service{}).ResolveCatalogSemantic(context.Background(), "film", 5)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCatalogEmbeddingsUnavailable))

	// disabled embedder
	_, err = (&Service{catalogEmbedder: keywordEmbedder{enabled: false}}).ResolveCatalogSemantic(context.Background(), "film", 5)
	require.True(t, errors.Is(err, ErrCatalogEmbeddingsUnavailable))
}

func TestResolveCatalogHybridFallsBackToLexical(t *testing.T) {
	s := &Service{} // no embedder
	got, err := s.ResolveCatalogHybrid(context.Background(), "film", 5)
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Equal(t, "Movie", got[0].Name, "lexical must still resolve film to Movie")
}

func TestMergeCatalogMatches(t *testing.T) {
	lexical := []CatalogTypeMatch{{Name: "Movie"}, {Name: "CreativeWork"}}
	semantic := []CatalogTypeMatch{{Name: "CreativeWork"}, {Name: "Brand"}}
	got := mergeCatalogMatches(lexical, semantic, 10)
	require.Equal(t, []string{"Movie", "CreativeWork", "Brand"}, names(got))

	trimmed := mergeCatalogMatches(lexical, semantic, 2)
	require.Equal(t, []string{"Movie", "CreativeWork"}, names(trimmed))
}

func names(ms []CatalogTypeMatch) []string {
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
	require.False(t, math.IsNaN(cosineSimilarity([]float32{0, 0}, []float32{1, 1})))
}
