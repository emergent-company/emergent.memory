package blueprints

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
)

// ErrCatalogEmbeddingsUnavailable is returned (wrapped) by the semantic resolver
// when no embedding service is wired or the project has no embedding model
// configured. Callers fall back to lexical resolution.
var ErrCatalogEmbeddingsUnavailable = errors.New("catalog embeddings unavailable")

// TextEmbedder is the slice of the embeddings service the catalog needs. It is
// satisfied by *pkg/embeddings.Service. Kept local so blueprints does not depend
// on the concrete package.
type TextEmbedder interface {
	IsEnabled() bool
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error)
}

// ResolveCatalogSemantic ranks catalog types by embedding cosine similarity.
// The catalog is small (~28 types), so it embeds every type's descriptor and
// the query per call and scores linearly; an ANN index is deliberately deferred
// until the catalog grows. Returns ErrCatalogEmbeddingsUnavailable when no
// embedder is configured or enabled.
func (s *Service) ResolveCatalogSemantic(ctx context.Context, query string, limit int) ([]CatalogTypeMatch, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if s.catalogEmbedder == nil || !s.catalogEmbedder.IsEnabled() {
		return nil, ErrCatalogEmbeddingsUnavailable
	}
	cat, err := ListBuiltinTaxonomy()
	if err != nil {
		return nil, err
	}
	if len(cat.Types) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultResolveLimit
	}
	if limit > maxResolveLimit {
		limit = maxResolveLimit
	}

	docs := make([]string, len(cat.Types))
	for i, t := range cat.Types {
		docs[i] = catalogTypeEmbeddingText(t)
	}
	docVecs, err := s.catalogEmbedder.EmbedDocuments(ctx, docs)
	if err != nil {
		return nil, err
	}
	if len(docVecs) != len(cat.Types) {
		return nil, errors.New("catalog embeddings: document vector count mismatch")
	}
	qVec, err := s.catalogEmbedder.EmbedQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	for i := range docVecs {
		if len(docVecs[i]) != len(qVec) {
			return nil, errors.New("catalog embeddings: query and document dimensions differ; the embedding model changed between calls")
		}
	}

	matches := make([]CatalogTypeMatch, 0, len(cat.Types))
	for i, t := range cat.Types {
		sim := cosineSimilarity(qVec, docVecs[i])
		if sim <= 0 {
			continue
		}
		matches = append(matches, CatalogTypeMatch{
			Name:     t.Name,
			Label:    t.Label,
			Category: t.Category,
			Parent:   t.Parent,
			Score:    round3(sim),
			Matched:  []string{"semantic"},
		})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Name < matches[j].Name
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

// ResolveCatalogHybrid fuses the lexical ranking with the semantic ranking.
// Lexical hits keep their order; semantic-only types are appended. Any semantic
// failure (no embedder, no project model) degrades to lexical without surfacing
// an error, because resolution must keep working.
func (s *Service) ResolveCatalogHybrid(ctx context.Context, query string, limit int) ([]CatalogTypeMatch, error) {
	cat, err := ListBuiltinTaxonomy()
	if err != nil {
		return nil, err
	}
	lexical := ResolveCatalogTypes(cat, query, maxResolveLimit)
	semantic, serr := s.ResolveCatalogSemantic(ctx, query, maxResolveLimit)
	if serr != nil {
		semantic = nil
	}
	return mergeCatalogMatches(lexical, semantic, limit), nil
}

// mergeCatalogMatches keeps lexical order, appends semantic-only matches, and
// trims to limit. Lexical is authoritative where both agree.
func mergeCatalogMatches(lexical, semantic []CatalogTypeMatch, limit int) []CatalogTypeMatch {
	if limit <= 0 {
		limit = defaultResolveLimit
	}
	if limit > maxResolveLimit {
		limit = maxResolveLimit
	}
	seen := make(map[string]bool, len(lexical))
	out := make([]CatalogTypeMatch, 0, len(lexical)+len(semantic))
	for _, m := range lexical {
		if seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		out = append(out, m)
	}
	for _, m := range semantic {
		if seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		out = append(out, m)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// catalogTypeEmbeddingText is the text embedded for a catalog type: the
// human-facing descriptors only, so a query phrased in domain language can hit
// it. Stable ordering keeps embeddings reproducible.
func catalogTypeEmbeddingText(t ObjectTypeDef) string {
	parts := []string{t.Name, t.Label, t.Category, t.Parent}
	parts = append(parts, t.Labels...)
	if t.Description != "" {
		parts = append(parts, t.Description)
	}
	return strings.Join(nonEmpty(parts), ". ")
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func cosineSimilarity(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
