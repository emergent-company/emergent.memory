package adk

import "context"

type contextKey string

const projectIDKey contextKey = "adk_project_id"

// WithProjectID stores the project ID in the context for model resolution.
// Called by request handlers (or middleware) that know the current project scope.
func WithProjectID(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, projectIDKey, projectID)
}

// ProjectIDFromContext retrieves the project ID from context.
// Returns empty string if not set.
func ProjectIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(projectIDKey).(string); ok {
		return v
	}
	return ""
}
