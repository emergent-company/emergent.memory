// Package modelref defines the canonical structured model reference used across
// the server: a provider instance (slug) plus a bare model name.
//
// It lives in a dependency-neutral package so that both pkg/adk and
// domain/provider can import it without creating an import cycle
// (domain/provider already imports pkg/adk via its ADK adapters).
package modelref

import (
	"fmt"
	"strings"
)

// Ref identifies a model served by a specific provider instance.
//
// Provider is the project-scoped instance slug. It may also hold a legacy
// dialect name for pre-migration references, which resolution maps to the
// dialect's default instance.
//
// Model is the bare model name as the provider API expects it. It may itself
// contain '/' (for example a Vertex resource path such as
// "publishers/google/models/gemini-2.5-flash").
type Ref struct {
	Provider string
	Model    string
}

// String renders the display/edge form "provider/model".
func (r Ref) String() string {
	switch {
	case r.Provider == "":
		return r.Model
	case r.Model == "":
		return r.Provider
	default:
		return r.Provider + "/" + r.Model
	}
}

// IsZero reports whether the reference does not identify both a provider and a
// model.
func (r Ref) IsZero() bool { return r.Provider == "" || r.Model == "" }

// Parse parses the edge form "provider/model" into a Ref.
//
// It splits on the FIRST '/', so a model name that itself contains slashes is
// preserved intact. The provider segment and the model must both be non-empty.
func Parse(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Ref{}, fmt.Errorf("empty model reference")
	}
	provider, model, found := strings.Cut(s, "/")
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if !found || provider == "" {
		return Ref{}, fmt.Errorf("invalid model reference %q: expected 'provider/model'", s)
	}
	if model == "" {
		return Ref{}, fmt.Errorf("invalid model reference %q: missing model name", s)
	}
	return Ref{Provider: provider, Model: model}, nil
}

// Resolver supplies the context NormalizeLegacy needs to resolve a stored
// legacy value. Implementations are backed by the provider repository.
type Resolver interface {
	// IsInstance reports whether slug is a provider instance in scope.
	IsInstance(slug string) bool
	// IsDialect reports whether dialect is a supported provider dialect.
	IsDialect(dialect string) bool
	// DefaultInstanceForDialect returns the default instance slug for a
	// dialect (the instance whose slug equals the dialect, else the smallest
	// slug), if the dialect has any instance in scope.
	DefaultInstanceForDialect(dialect string) (slug string, ok bool)
	// InferDialectForModel returns the dialect of the unique instance whose
	// configured models include model, if exactly one such instance exists.
	InferDialectForModel(model string) (dialect string, ok bool)
}

// NormalizeLegacy resolves a pre-migration stored value into a Ref.
//
// It is a context-aware backfill/inference helper, NOT the runtime edge parser
// (that is Parse, which is strict). Resolution order:
//
//  1. A value whose first '/' segment is a known instance slug uses it.
//  2. A value whose first '/' segment is a known dialect uses that dialect's
//     default instance.
//  3. A bare value (no '/', or a slash-containing model id that matches a
//     configured model) is attributed to a default instance inferred from the
//     model name, or to the single in-scope default dialect.
//  4. Otherwise it is unresolved (ok=false) and the caller flags the row.
//
// A slash-containing value is never split unless its prefix is a recognised
// slug or dialect, so unqualified multi-segment model ids are preserved.
func NormalizeLegacy(s string, r Resolver) (Ref, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Ref{}, false
	}

	// (1)/(2): a recognised prefix is an instance slug or a dialect.
	if prefix, rest, found := strings.Cut(s, "/"); found {
		prefix = strings.TrimSpace(prefix)
		stripRest := strings.TrimSpace(rest)
		if rest != "" && r.IsInstance(prefix) {
			return Ref{Provider: prefix, Model: stripRest}, true
		}
		if rest != "" && r.IsDialect(prefix) {
			if slug, ok := r.DefaultInstanceForDialect(prefix); ok {
				return Ref{Provider: slug, Model: stripRest}, true
			}
		}
		// Not a recognised prefix: fall through and try model-name inference
		// so unqualified multi-segment model ids are not split.
	}

	// (3): bare or unqualified multi-segment value — infer from the model name.
	if dialect, ok := r.InferDialectForModel(s); ok {
		if slug, ok := r.DefaultInstanceForDialect(dialect); ok {
			return Ref{Provider: slug, Model: s}, true
		}
	}

	return Ref{}, false
}
