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

// rootRunIDKey is the context key for the top-level orchestration run ID.
// This is the run ID of the root agent in a spawn tree — identical across all
// sub-agents in a single orchestration, enabling cross-tree cost aggregation.
type rootRunIDKey struct{}

// ContextWithRootRunID returns a new context carrying the given root run ID.
func ContextWithRootRunID(ctx context.Context, rootRunID string) context.Context {
	return context.WithValue(ctx, rootRunIDKey{}, rootRunID)
}

// RootRunIDFromContext extracts the top-level orchestration run ID from context.
// Returns an empty string if no root run ID is present.
func RootRunIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(rootRunIDKey{}).(string); ok {
		return v
	}
	return ""
}
