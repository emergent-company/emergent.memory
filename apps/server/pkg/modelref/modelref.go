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

// Backfill note: legacy-value normalization (resolving a bare or
// dialect-prefixed stored value to an instance) is implemented directly in the
// SQL backfill migrations (kb.provider configs and kb.project_model_config),
// not as a Go helper. There is no Go-side backfill path, so no NormalizeLegacy
// helper is defined here; the runtime edge uses only Parse (strict). See
// design D2/D6 and the migrations for the deterministic resolution rules.
