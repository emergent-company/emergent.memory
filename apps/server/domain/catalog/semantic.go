package catalog

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
)

// ErrEmbeddingsUnavailable is returned by Semantic when no embedder is wired or
// the project has no embedding model configured. Callers fall back to lexical.
var ErrEmbeddingsUnavailable = errors.New("catalog embeddings unavailable")

// TextEmbedder is the slice of the embeddings service the catalog needs.
// Satisfied by *pkg/embeddings.Service.
type TextEmbedder interface {
	IsEnabled() bool
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error)
}

// Semantic ranks catalog types by embedding cosine similarity. The catalog is
// small, so it embeds each type descriptor and the query per call and scores
// linearly; an ANN index is deliberately deferred until the catalog grows.
func Semantic(ctx context.Context, cat *Catalog, embedder TextEmbedder, query string, limit int) ([]TypeMatch, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if embedder == nil || !embedder.IsEnabled() {
		return nil, ErrEmbeddingsUnavailable
	}
	if cat == nil || len(cat.Types) == 0 {
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
		docs[i] = embeddingText(t)
	}
	docVecs, err := embedder.EmbedDocuments(ctx, docs)
	if err != nil {
		return nil, err
	}
	if len(docVecs) != len(cat.Types) {
		return nil, errors.New("catalog embeddings: document vector count mismatch")
	}
	qVec, err := embedder.EmbedQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	for i := range docVecs {
		if len(docVecs[i]) != len(qVec) {
			return nil, errors.New("catalog embeddings: query and document dimensions differ; the embedding model changed between calls")
		}
	}

	matches := make([]TypeMatch, 0, len(cat.Types))
	for i, t := range cat.Types {
		sim := cosineSimilarity(qVec, docVecs[i])
		if sim <= 0 {
			continue
		}
		matches = append(matches, TypeMatch{
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

// Hybrid fuses lexical order with semantic-only additions. Any semantic failure
// degrades to lexical; resolution must keep working.
func Hybrid(ctx context.Context, cat *Catalog, embedder TextEmbedder, query string, limit int) ([]TypeMatch, error) {
	lexical := ResolveTypes(cat, query, maxResolveLimit)
	semantic, err := Semantic(ctx, cat, embedder, query, maxResolveLimit)
	if err != nil {
		semantic = nil
	}
	return mergeMatches(lexical, semantic, limit), nil
}

func mergeMatches(lexical, semantic []TypeMatch, limit int) []TypeMatch {
	if limit <= 0 {
		limit = defaultResolveLimit
	}
	if limit > maxResolveLimit {
		limit = maxResolveLimit
	}
	seen := make(map[string]bool, len(lexical))
	out := make([]TypeMatch, 0, len(lexical)+len(semantic))
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

func embeddingText(t Type) string {
	parts := []string{t.Name, t.Label, t.Category, t.Parent}
	parts = append(parts, t.Labels...)
	if t.Description != "" {
		parts = append(parts, t.Description)
	}
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, ". ")
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
