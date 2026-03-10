package provider

import "context"

// runIDKey is the context key for the agent run ID injected by the executor.
type runIDKey struct{}

// ContextWithRunID returns a new context carrying the given agent run ID.
func ContextWithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDKey{}, runID)
}

// RunIDFromContext extracts the agent run ID from the context.
// Returns an empty string if no run ID is present.
func RunIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(runIDKey{}).(string); ok {
		return v
	}
	return ""
}
