// Package modelref defines the canonical structured model reference used across
// the server: a provider instance (slug) plus a bare model name.
//
// The implementation lives in the shared SDK package
// apps/server/pkg/sdk/modelref, which the CLI also imports; this package
// re-exports it so internal server callers keep their import path and the
// accept/reject rule cannot drift between server and CLI (#1489).
//
// It stays dependency-neutral (the SDK package imports only the standard
// library) so that both pkg/adk and domain/provider can import it without
// creating an import cycle (domain/provider already imports pkg/adk via its ADK
// adapters).
package modelref

import (
	sdkmodelref "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/modelref"
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
type Ref = sdkmodelref.Ref

// Parse parses the edge form "provider/model" into a Ref.
//
// It splits on the FIRST '/', so a model name that itself contains slashes is
// preserved intact. The provider segment and the model must both be non-empty.
// Delegates to the canonical shared implementation.
func Parse(s string) (Ref, error) {
	return sdkmodelref.Parse(s)
}

// StripRoutingPrefix removes a leading routing-provider prefix from s when the
// first '/' segment is a recognised routing provider (as reported by
// isRoutingProvider), returning the bare model name. Otherwise s is returned
// unchanged.
//
// This is the canonical home for what used to be an ad-hoc "exactly one slash"
// prefix parser in domain/provider (stripModelPrefix). Only a name whose first
// segment is a recognised routing provider is treated as prefixed:
//
//   - "deepseek/deepseek-v4-flash"          → "deepseek-v4-flash"
//   - "google-vertex/publishers/.../model"  → "publishers/.../model" (model may
//     itself contain slashes, preserved by Parse's split-on-first-slash)
//   - "publishers/google/models/gemini-..."  → unchanged (first segment is not a
//     routing provider, so the slashes are part of the model id)
//   - "gpt-4o"                               → unchanged (no prefix)
//
// The predicate keeps modelref free of any dialect registry: the caller decides
// what counts as a routing provider, so this package stays dependency-neutral.
func StripRoutingPrefix(s string, isRoutingProvider func(string) bool) string {
	ref, err := Parse(s)
	if err != nil {
		return s // bare name — nothing to strip
	}
	if !isRoutingProvider(ref.Provider) {
		return s // multi-segment resource path, not a routing prefix
	}
	return ref.Model
}

// Backfill note: legacy-value normalization (resolving a bare or
// dialect-prefixed stored value to an instance) is implemented directly in the
// SQL backfill migrations (kb.provider configs and kb.project_model_config),
// not as a Go helper. There is no Go-side backfill path, so no NormalizeLegacy
// helper is defined here; the runtime edge uses only Parse (strict). See
// design D2/D6 and the migrations for the deterministic resolution rules.
