// Package modelref defines the canonical structured model reference used across
// the platform: a provider instance (slug) plus a bare model name.
//
// It is the single source of truth for parsing the "provider/model" edge form.
// The server's pkg/modelref re-exports these types (so internal callers keep
// their import path), and the CLI blueprint validator imports this package
// directly, so the accept/reject rule cannot drift between the two (#1489).
//
// The package is dependency-neutral: it imports only the standard library, so
// it can be used from the SDK, the server, and the CLI alike.
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
	// A model segment that begins with '/' means the separator was doubled
	// (e.g. "openai//gpt-4o"): the reference round-trips but is malformed and
	// would only fail later at execution time. Reject it at the parse boundary.
	if strings.HasPrefix(model, "/") {
		return Ref{}, fmt.Errorf("invalid model reference %q: model segment must not start with '/'", s)
	}
	return Ref{Provider: provider, Model: model}, nil
}

// ParseWithProvider parses a model reference that may carry its provider either
// as the canonical "provider/model" prefix or as a separate structured provider
// slug. It is the single entry point for write paths that accept both the
// structured {name, provider} shape and the prefixed shape, so every caller
// agrees on what a valid reference is.
//
//	name / provider                     result
//	"openai/gpt-4o" / ""                {openai, gpt-4o}
//	"gpt-4o" / "openai"                 {openai, gpt-4o}
//	"openai/gpt-4o" / "openai"          {openai, gpt-4o} (redundant, allowed)
//	"gpt-4o" / ""                       error (no provider)
//	"openai/gpt-4o" / "google"          error (conflicting providers)
//	"" / "openai"                       error (provider without a model)
//	"" / ""                             zero Ref, nil (no override)
func ParseWithProvider(name, provider string) (Ref, error) {
	name = strings.TrimSpace(name)
	provider = strings.TrimSpace(provider)

	if name == "" {
		if provider != "" {
			return Ref{}, fmt.Errorf("invalid model reference: model.provider %q set without a model name", provider)
		}
		return Ref{}, nil
	}

	ref, err := Parse(name)
	if err == nil {
		if provider != "" && provider != ref.Provider {
			return Ref{}, fmt.Errorf("invalid model reference: provider %q in model.name conflicts with model.provider %q", ref.Provider, provider)
		}
		return ref, nil
	}

	// name is not a valid "provider/model" itself. A structured provider may
	// still qualify a bare model name, but a name that contains '/' was an
	// attempted (malformed) prefixed form and must not be reinterpreted.
	if strings.Contains(provider, "/") {
		return Ref{}, fmt.Errorf("invalid model reference: model.provider %q must be a bare provider slug", provider)
	}
	if provider == "" || strings.Contains(name, "/") {
		return Ref{}, err
	}
	return Parse(provider + "/" + name)
}
