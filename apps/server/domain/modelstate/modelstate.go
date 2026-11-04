// Package modelstate reports the model a project would actually run with, and
// whether it has any provider configured at all.
//
// Read models use it so that a configuration failure can name the element that
// is actually missing instead of restating the raw error text: "no provider is
// connected", "a provider is connected but no default model is selected", or "a
// model is configured and the run failed anyway". Those are three different
// fixes, and only the first two are configuration work.
package modelstate

import "context"

// SourceNone is the GenerativeSource value that means nothing resolved.
const SourceNone = "none"

// State is a project's model configuration as a run would resolve it.
type State struct {
	// GenerativeModel is the routed "instance/model" name a run would use.
	// Empty when nothing resolves.
	GenerativeModel string
	// GenerativeSource is where that model came from: "project", "provider" or
	// "none".
	GenerativeSource string
	// ProviderCount is how many provider instances the project has configured.
	// Metadata only: no credential is decrypted.
	ProviderCount int
}

// Configured reports whether a run would find a generative model.
func (s State) Configured() bool { return s.GenerativeModel != "" }

// Reader resolves a project's model state.
type Reader interface {
	State(ctx context.Context, projectID string) (State, error)
}
