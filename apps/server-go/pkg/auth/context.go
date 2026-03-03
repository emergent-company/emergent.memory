package auth

import "context"

// Context keys for storing auth data in standard context.Context.
// These allow downstream service layers to access auth information
// without requiring an echo.Context dependency.
type (
	userCtxKey      struct{}
	projectIDCtxKey struct{}
	orgIDCtxKey     struct{}
)

// ContextWithUser returns a new context with the AuthUser embedded.
func ContextWithUser(ctx context.Context, user *AuthUser) context.Context {
	return context.WithValue(ctx, userCtxKey{}, user)
}

// UserFromContext extracts the AuthUser from a standard context.Context.
// Returns nil if no user is present.
func UserFromContext(ctx context.Context) *AuthUser {
	if user, ok := ctx.Value(userCtxKey{}).(*AuthUser); ok {
		return user
	}
	return nil
}

// ContextWithProjectID returns a new context with the project ID embedded.
func ContextWithProjectID(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, projectIDCtxKey{}, projectID)
}

// ProjectIDFromContext extracts the project ID from a standard context.Context.
// Returns empty string if no project ID is present.
func ProjectIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(projectIDCtxKey{}).(string); ok {
		return id
	}
	return ""
}

// ContextWithOrgID returns a new context with the organization ID embedded.
func ContextWithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgIDCtxKey{}, orgID)
}

// OrgIDFromContext extracts the organization ID from a standard context.Context.
// Returns empty string if no organization ID is present.
func OrgIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(orgIDCtxKey{}).(string); ok {
		return id
	}
	return ""
}

// InjectAuthContext enriches the given context with user, project ID, and org ID
// from the provided AuthUser. This is a convenience function for injecting all
// auth-related values at once.
func InjectAuthContext(ctx context.Context, user *AuthUser) context.Context {
	ctx = ContextWithUser(ctx, user)
	if user.ProjectID != "" {
		ctx = ContextWithProjectID(ctx, user.ProjectID)
	}
	if user.OrgID != "" {
		ctx = ContextWithOrgID(ctx, user.OrgID)
	}
	return ctx
}
