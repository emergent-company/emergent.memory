// Package llm provides interfaces for language model providers.
package llm

import (
	"context"
)

// Provider is an interface for LLM providers
type Provider interface {
	// Complete generates a completion for the given prompt
	Complete(ctx context.Context, prompt string) (string, error)

	// IsConfigured returns true if the provider is properly configured
	IsConfigured() bool
}
