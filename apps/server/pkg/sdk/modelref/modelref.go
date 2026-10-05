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
	return Ref{Provider: provider, Model: model}, nil
}
