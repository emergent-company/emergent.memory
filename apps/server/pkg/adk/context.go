package adk

import (
	"context"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

type contextKey string

const projectIDKey contextKey = "adk_project_id"

// WithProjectID stores the project ID in the context for model resolution.
// Called by request handlers (or middleware) that know the current project scope.
func WithProjectID(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, projectIDKey, projectID)
}

// ProjectIDFromContext retrieves the project ID from context.
// Checks the adk-specific key first, then falls back to the auth package key
// (set by auth.ContextWithProjectID in request handlers).
// Returns empty string if not set.
func ProjectIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(projectIDKey).(string); ok && v != "" {
		return v
	}
	// Fall back to the auth package context key used by request handlers.
	return auth.ProjectIDFromContext(ctx)
}
