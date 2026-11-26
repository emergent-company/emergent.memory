package discoveryjobs

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/typehygiene"
)

// defaultTypeSimilarityThreshold is the cosine cutoff above which a discovered
// type's name+description is treated as the same entity as an existing type.
// It is intentionally conservative: a wrong merge silently collapses two real
// types, so the resolver only acts on a strong match.
const defaultTypeSimilarityThreshold = 0.85

// TypeSimilarity resolves whether a discovered type is semantically the same as
// an already-installed project type, so discovery converges across runs without
// a hardcoded synonym table. Implemented by *embeddingTypeSimilarity; nil (or a
// resolver that returns "") keeps the spelling-only reconcile path.
type TypeSimilarity interface {
	Resolve(ctx context.Context, discovered DiscoveredType, existing []ExistingType) (string, error)
}

// SetTypeSimilarity injects the semantic type merger. Nil-safe: without it,
// discovery converges only on exact spelling/plural/synonym matches.
func (s *Service) SetTypeSimilarity(t TypeSimilarity) {
	s.typeSimilarity = t
}

// mergeSemanticallySimilarTypes renames discovered types onto existing project
// types when the similarity resolver finds a strong match. It only runs for
// types the spelling resolver does not already cover, and is a no-op when no
// TypeSimilarity is injected or there is nothing to compare against.
func (s *Service) mergeSemanticallySimilarTypes(ctx context.Context, types []DiscoveredType, registered []string, current *DiscoveredSchemaSnapshot) []DiscoveredType {
	if s.typeSimilarity == nil || len(types) == 0 {
		return types
	}

	existing := make([]ExistingType, 0, len(registered))
	seen := make(map[string]bool, len(registered))
	for _, name := range registered {
		seen[name] = true
		existing = append(existing, ExistingType{Name: name})
	}
	if current != nil {
		for _, name := range sortedMapKeys(current.UIConfigs) {
			if seen[name] {
				continue
			}
			desc := ""
			if cfg, ok := current.UIConfigs[name].(map[string]any); ok {
				if d, ok := cfg["description"].(string); ok {
					desc = d
				}
			}
			existing = append(existing, ExistingType{Name: name, Description: desc})
			seen[name] = true
		}
	}
	if len(existing) == 0 {
		return types
	}

	resolver := typehygiene.NewResolver(registered...)
	out := make([]DiscoveredType, 0, len(types))
	for _, t := range types {
		// Spelling matches belong to the reconcile resolver — leave them alone.
		if _, ok := resolver.Resolve(t.TypeName); ok {
			out = append(out, t)
			continue
		}
		match, err := s.typeSimilarity.Resolve(ctx, t, existing)
		if err != nil {
			s.log.Warn("type similarity check failed; keeping discovered name",
				slog.String("type", t.TypeName), logger.Error(err))
			out = append(out, t)
			continue
		}
		if match != "" {
			t.TypeName = match
		}
		out = append(out, t)
	}
	return out
}

// ExistingType is one already-installed type offered for comparison.
type ExistingType struct {
	Name        string
	Description string
}

// Embedder is the slice of the embedding service the semantic merge needs. All
// texts are embedded with the document task so the vectors are comparable.
type Embedder interface {
	EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error)
}

// embeddingTypeSimilarity merges types whose name+description embed near each
// other in the document space.
type embeddingTypeSimilarity struct {
	embeds    Embedder
	threshold float32
	log       *slog.Logger
}

// NewEmbeddingTypeSimilarity builds the semantic type merger. A nil embeds
// yields a resolver that never matches (spelling-only converge).
func NewEmbeddingTypeSimilarity(embeds Embedder, threshold float32, log *slog.Logger) *embeddingTypeSimilarity {
	if threshold <= 0 || threshold > 1 {
		threshold = defaultTypeSimilarityThreshold
	}
	return &embeddingTypeSimilarity{embeds: embeds, threshold: threshold, log: log.With(logger.Scope("type-similarity"))}
}

// Resolve returns the existing type name most similar to the discovered type,
// or "" when none clears the threshold. All texts are embedded in one batch so
// they share the embedding space.
func (m *embeddingTypeSimilarity) Resolve(ctx context.Context, discovered DiscoveredType, existing []ExistingType) (string, error) {
	if m.embeds == nil || len(existing) == 0 {
		return "", nil
	}

	texts := make([]string, 1+len(existing))
	texts[0] = typeEmbeddingText(discovered.TypeName, discovered.Description)
	for i, e := range existing {
		texts[1+i] = typeEmbeddingText(e.Name, e.Description)
	}

	vecs, err := m.embeds.EmbedDocuments(ctx, texts)
	if err != nil {
		return "", fmt.Errorf("embed types: %w", err)
	}
	if len(vecs) != len(texts) {
		return "", fmt.Errorf("embed types: got %d vectors for %d texts", len(vecs), len(texts))
	}

	query := vecs[0]
	best := -1.0
	bestIdx := -1
	for i := 1; i < len(vecs); i++ {
		if s := cosineSimilarity(query, vecs[i]); s > best {
			best = s
			bestIdx = i - 1
		}
	}
	if bestIdx >= 0 && best >= float64(m.threshold) {
		m.log.Debug("merged discovered type onto existing type by similarity",
			slog.String("discovered", discovered.TypeName),
			slog.String("existing", existing[bestIdx].Name),
			slog.Float64("similarity", best))
		return existing[bestIdx].Name, nil
	}
	return "", nil
}

// typeEmbeddingText renders the text embedded for a type: its name plus, when
// present, its description. The description is what carries the semantic signal
// that a bare name ("Character" vs "Person") does not.
func typeEmbeddingText(name, description string) string {
	if description == "" {
		return name
	}
	return name + ": " + description
}

// cosineSimilarity returns the cosine similarity of two equal-length vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var dot, na, nb float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
