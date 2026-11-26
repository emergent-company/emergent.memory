package discoveryjobs

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/emergent-company/emergent.memory/pkg/llmjson"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/typehygiene"
)

// defaultTypeSimilarityThreshold is the confidence cutoff above which a
// discovered type is treated as the same concept as an existing type. It is
// deliberately conservative: a wrong merge silently collapses two real types,
// so only a confident classification acts and the rest is left to a human.
const defaultTypeSimilarityThreshold = 0.90

// TypeSimilarity resolves, for a set of discovered types, which existing
// project types they are semantically the same as. Implemented by
// *classifierTypeSimilarity; nil (or an empty result) keeps the spelling-only
// reconcile path. The result maps a discovered type's name to the existing type
// name it should adopt ("" entries are absent).
type TypeSimilarity interface {
	Resolve(ctx context.Context, discovered []DiscoveredType, existing []ExistingType) (map[string]string, error)
}

// ExistingType is one already-installed (or built-in taxonomy) type offered for
// comparison. Properties/Required/Tags are populated for built-in taxonomy
// types so a match can adopt the full canonical schema.
type ExistingType struct {
	Name        string
	Description string
	// Category is the optional browse/grouping lens ("media", "commerce", …).
	// It is a disambiguation hint, not a subtyping edge.
	Category string
	// Parent is the optional canonical supertype (schema.org subClassOf), e.g.
	// Movie -> CreativeWork. A hint for alignment, not a compiled edge.
	Parent     string
	Tags       []string
	Properties map[string]any
	Required   []string
	// Icon and Color are the taxonomy type's declared UI accent (e.g.
	// "tabler--movie", "#DB2777"). A discovered type that adopts this canonical
	// type inherits them, so curated taxonomy styling reaches the installed
	// schema instead of a name-guessed icon.
	Icon  string
	Color string
}

// SetTypeSimilarity injects the semantic type classifier. Nil-safe: without it,
// discovery converges only on exact spelling/plural/synonym matches.
func (s *Service) SetTypeSimilarity(t TypeSimilarity) {
	s.typeSimilarity = t
}

// SetTaxonomy injects the built-in common-entity taxonomy types that discovery
// aligns proposed types against before inventing new ones. Nil-safe: without it,
// the classifier matches only the project's installed types.
func (s *Service) SetTaxonomy(t []ExistingType) {
	s.taxonomy = t
}

// taxonomyUIStyle returns the declared UI icon and color for a canonical
// taxonomy type. Empty strings mean the taxonomy did not declare that facet.
func (s *Service) taxonomyUIStyle(name string) (icon, color string, ok bool) {
	for _, t := range s.taxonomy {
		if t.Name == name {
			return t.Icon, t.Color, true
		}
	}
	return "", "", false
}

// taxonomyCategory returns the browse/grouping category declared by a canonical
// taxonomy type, if any.
func (s *Service) taxonomyCategory(name string) (string, bool) {
	for _, t := range s.taxonomy {
		if t.Name == name {
			return t.Category, true
		}
	}
	return "", false
}

// mergeSemanticallySimilarTypes renames discovered types onto existing project
// types when the classifier is confident they are the same concept. It only
// runs for types the spelling resolver does not already cover, and is a no-op
// when no TypeSimilarity is injected or there is nothing to compare against.
func (s *Service) mergeSemanticallySimilarTypes(ctx context.Context, types []DiscoveredType, registered []string, current *DiscoveredSchemaSnapshot) []DiscoveredType {
	if s.typeSimilarity == nil || len(types) == 0 {
		return types
	}

	existing := buildExistingTypes(registered, current)
	candidates := append([]ExistingType(nil), existing...)
	installed := make(map[string]bool, len(existing))
	for _, e := range existing {
		installed[e.Name] = true
	}
	// A taxonomy type that is also already installed must not be offered twice;
	// the project's installed entry is the one the classifier compares against.
	for _, tx := range s.taxonomy {
		if installed[tx.Name] {
			continue
		}
		candidates = append(candidates, tx)
	}
	if len(candidates) == 0 {
		return types
	}

	// A built-in taxonomy name adopts its full canonical schema on a match, but
	// only when the project has not already installed that name: the project's
	// own schema is authoritative and must not be replaced by the taxonomy one.
	taxonomyByName := make(map[string]ExistingType, len(s.taxonomy))
	for _, tx := range s.taxonomy {
		if installed[tx.Name] {
			continue
		}
		taxonomyByName[tx.Name] = tx
	}

	resolver := typehygiene.NewResolver(registered...)
	var toResolve, keep []DiscoveredType
	for _, t := range types {
		if _, ok := resolver.Resolve(t.TypeName); ok {
			keep = append(keep, t) // spelling match — the reconcile resolver owns it
		} else {
			toResolve = append(toResolve, t)
		}
	}
	if len(toResolve) == 0 {
		return types
	}

	matches, err := s.typeSimilarity.Resolve(ctx, toResolve, candidates)
	if err != nil {
		s.log.Warn("type similarity classification failed; keeping discovered names", logger.Error(err))
		return types
	}

	out := make([]DiscoveredType, 0, len(types))
	out = append(out, keep...)
	for _, t := range toResolve {
		match, ok := matches[t.TypeName]
		if !ok || match == "" {
			out = append(out, t)
			continue
		}
		t.TypeName = match
		// Adopt the full canonical schema when the match is a built-in taxonomy
		// type, so the well-described properties/tags replace the ad-hoc
		// discovered ones. The taxonomy is process-lived, so copy rather than
		// alias its maps/slices: a later in-place mutation of the discovered
		// type must never corrupt the shared canonical taxonomy.
		if tx, ok := taxonomyByName[match]; ok {
			if tx.Description != "" {
				t.Description = tx.Description
			}
			if len(tx.Properties) > 0 {
				t.Properties = cloneProperties(tx.Properties)
			}
			if len(tx.Required) > 0 {
				t.RequiredProperties = append([]string(nil), tx.Required...)
			}
		}
		out = append(out, t)
	}
	return out
}

// cloneProperties deep-copies a taxonomy property map (including nested maps
// and slices) so a per-run discovered type never aliases the process-lived
// taxonomy schema.
func cloneProperties(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = clonePropertyValue(v)
	}
	return out
}

// clonePropertyValue recursively copies the container values a decoded
// taxonomy property can hold. Scalars are immutable and returned as-is.
func clonePropertyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return cloneProperties(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = clonePropertyValue(item)
		}
		return out
	case []string:
		return append([]string(nil), val...)
	default:
		return v
	}
}

// buildExistingTypes assembles the comparison set: registered names first, then
// the current discovered pack's names plus their descriptions.
func buildExistingTypes(registered []string, current *DiscoveredSchemaSnapshot) []ExistingType {
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
	return existing
}

// classifierTypeSimilarity judges type equivalence with a generative model: one
// batched classification per discovery run, returning a calibrated confidence
// the merge gate uses. It is the "same concept?" decision a pure embedding
// cosine cannot make reliably ("Character" vs "Person" vs "Episode").
type classifierTypeSimilarity struct {
	complete  func(ctx context.Context, prompt string) (string, error)
	threshold float32
	log       *slog.Logger
}

// NewClassifierTypeSimilarity builds the semantic type classifier. A nil
// complete yields a classifier that never matches (spelling-only converge).
func NewClassifierTypeSimilarity(complete func(ctx context.Context, prompt string) (string, error), threshold float32, log *slog.Logger) *classifierTypeSimilarity {
	if threshold <= 0 || threshold > 1 {
		threshold = defaultTypeSimilarityThreshold
	}
	return &classifierTypeSimilarity{complete: complete, threshold: threshold, log: log.With(logger.Scope("type-similarity"))}
}

// Resolve classifies every discovered type against the existing types in one
// call and returns the confident matches.
func (m *classifierTypeSimilarity) Resolve(ctx context.Context, discovered []DiscoveredType, existing []ExistingType) (map[string]string, error) {
	if m.complete == nil || len(discovered) == 0 || len(existing) == 0 {
		return map[string]string{}, nil
	}

	response, err := m.complete(ctx, buildTypeClassificationPrompt(discovered, existing))
	if err != nil {
		return nil, fmt.Errorf("classify types: %w", err)
	}

	clean := extractJSON(response, "matches")
	var result struct {
		Matches []struct {
			Type       string  `json:"type"`
			SameAs     *string `json:"same_as"`
			Confidence float32 `json:"confidence"`
		} `json:"matches"`
	}
	if err := llmjson.Decode(clean, &result); err != nil {
		return nil, fmt.Errorf("parse type classification: %w", err)
	}

	existingNames := make(map[string]bool, len(existing))
	for _, e := range existing {
		existingNames[e.Name] = true
	}

	out := map[string]string{}
	for _, mrow := range result.Matches {
		if mrow.SameAs == nil || *mrow.SameAs == "" || mrow.Confidence < m.threshold {
			continue
		}
		if !existingNames[*mrow.SameAs] {
			continue // model hallucinated an existing type name; ignore
		}
		out[mrow.Type] = *mrow.SameAs
		if mrow.Type != *mrow.SameAs {
			m.log.Debug("merged discovered type onto existing type by classification",
				slog.String("discovered", mrow.Type),
				slog.String("existing", *mrow.SameAs),
				slog.Float64("confidence", float64(mrow.Confidence)))
		}
	}
	return out, nil
}

// buildTypeClassificationPrompt renders the deterministic classification
// prompt: discovered types (with descriptions) against existing types, both
// sorted by name so the same inputs produce the same prompt.
func buildTypeClassificationPrompt(discovered []DiscoveredType, existing []ExistingType) string {
	ds := append([]DiscoveredType(nil), discovered...)
	sort.Slice(ds, func(i, j int) bool { return ds[i].TypeName < ds[j].TypeName })
	es := append([]ExistingType(nil), existing...)
	sort.Slice(es, func(i, j int) bool { return es[i].Name < es[j].Name })

	var b strings.Builder
	b.WriteString(`You are classifying whether discovered knowledge-graph types are the same concept as existing types. A "same concept" means the two types describe the same kind of entity (e.g. "Company" and "Organization"; "TV Show" and "Show"). They are NOT the same when they describe different kinds (e.g. a person vs a location, a show vs an episode). A fictional character is NOT a real human being: "Character" (a fictional persona in a story) and "Person" (a real, specific human) are deliberately DISTINCT types, as are "Character" and "Actor". Never match a fictional character onto a real person or vice versa. Use the description and tags of each existing type to decide.

Discovered types:
`)
	for _, t := range ds {
		fmt.Fprintf(&b, "- %s: %s\n", t.TypeName, t.Description)
	}
	b.WriteString("\nExisting types:\n")
	for _, e := range es {
		fmt.Fprintf(&b, "- %s: %s", e.Name, e.Description)
		if e.Category != "" {
			fmt.Fprintf(&b, " [category: %s]", e.Category)
		}
		if e.Parent != "" {
			fmt.Fprintf(&b, " [parent: %s]", e.Parent)
		}
		if len(e.Tags) > 0 {
			fmt.Fprintf(&b, " [tags: %s]", strings.Join(e.Tags, ", "))
		}
		b.WriteString("\n")
	}
	b.WriteString(`
For each discovered type, decide whether it is the same concept as an existing type. If none matches, same_as must be null. Return ONLY JSON:
{"matches": [{"type": "<discovered name>", "same_as": "<existing name or null>", "confidence": 0.0}]}

confidence is your confidence in the decision, from 0 to 1.`)
	return b.String()
}
