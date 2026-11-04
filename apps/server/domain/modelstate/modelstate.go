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

// SourceNone is the source value that means nothing resolved.
const SourceNone = "none"

// Kind selects which model family a failure is about. Generative and embedding
// models resolve through different chains (a project can have a generative
// model selected and no embedding model at all), so copy that says "no default
// model is selected" has to know which one it is talking about.
type Kind string

const (
	// KindGenerative is the chat/completion model an extraction or agent run uses.
	KindGenerative Kind = "generative"
	// KindEmbedding is the vector model chunk and graph embeddings use.
	KindEmbedding Kind = "embedding"
)

// State is a project's model configuration as a run would resolve it.
type State struct {
	// GenerativeModel is the routed "instance/model" name a run would use.
	// Empty when nothing resolves.
	GenerativeModel string
	// GenerativeSource is where that model came from: "project", "provider" or
	// "none".
	GenerativeSource string
	// EmbeddingModel is the routed "instance/model" embedding name a run would
	// use. Empty when nothing resolves.
	EmbeddingModel string
	// EmbeddingSource is where that model came from.
	EmbeddingSource string
	// ProviderCount is how many provider instances the project has configured.
	// Metadata only: no credential is decrypted.
	ProviderCount int
}

// Model returns the routed model name for the kind, or "" when nothing
// resolves for it.
func (s State) Model(kind Kind) string {
	if kind == KindEmbedding {
		return s.EmbeddingModel
	}
	return s.GenerativeModel
}

// Source returns where the kind's model came from, or SourceNone.
func (s State) Source(kind Kind) string {
	source := s.GenerativeSource
	if kind == KindEmbedding {
		source = s.EmbeddingSource
	}
	if source == "" {
		return SourceNone
	}
	return source
}

// Configured reports whether a run of the kind would find a model.
func (s State) Configured(kind Kind) bool { return s.Model(kind) != "" }

// Reader resolves a project's model state.
type Reader interface {
	State(ctx context.Context, projectID string) (State, error)
}
