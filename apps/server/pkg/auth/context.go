package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Context keys for storing auth data in standard context.Context.
// These allow downstream service layers to access auth information
// without requiring an echo.Context dependency.
type (
	userCtxKey      struct{}
	projectIDCtxKey struct{}
	orgIDCtxKey     struct{}
	rawTokenCtxKey  struct{}
	namespaceCtxKey struct{}
	actorCtxKey     struct{}
)

// actorValue carries the acting actor for provenance attribution. It is stamped
// on the context at run/endpoint boundaries (agent run, per-agent MCP endpoint,
// extraction worker) and read by downstream graph mutations to attribute writes
// to the actual writer rather than the HTTP principal.
type actorValue struct {
	ActorType string
	ActorID   *uuid.UUID
}

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

// RequireUser returns the authenticated user embedded in ctx, or
// ErrUnauthorized when no user is present or the user ID is empty. It is the
// error-returning counterpart to MustGetUser for authorization checks that run
// in the service layer (which receives a context.Context, not an echo.Context).
// Callers that need a domain-specific message should re-wrap the returned error.
func RequireUser(ctx context.Context) (*AuthUser, error) {
	user := UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return nil, apperror.ErrUnauthorized
	}
	return user, nil
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

// ContextWithNamespace returns a new context with the graph namespace embedded.
func ContextWithNamespace(ctx context.Context, namespace string) context.Context {
	return context.WithValue(ctx, namespaceCtxKey{}, namespace)
}

// NamespaceFromContext extracts the graph namespace from a standard context.Context.
// Returns empty string if no namespace is present.
func NamespaceFromContext(ctx context.Context) string {
	if ns, ok := ctx.Value(namespaceCtxKey{}).(string); ok {
		return ns
	}
	return ""
}

// WithActor stamps the acting actor on the context. Downstream graph mutations
// read this (via ActorFromContext) to attribute writes to the actual writer —
// `agent` for an agent run's tool writes, `system` for background/extraction —
// overriding the fallback user actor that HTTP handlers pass explicitly.
func WithActor(ctx context.Context, actorType string, actorID *uuid.UUID) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, actorValue{ActorType: actorType, ActorID: actorID})
}

// ActorFromContext extracts the stamped actor from a standard context.Context.
// The bool is false when no actor was stamped.
func ActorFromContext(ctx context.Context) (actorType string, actorID *uuid.UUID, ok bool) {
	if a, ok := ctx.Value(actorCtxKey{}).(actorValue); ok {
		return a.ActorType, a.ActorID, true
	}
	return "", nil, false
}

// ContextWithRawToken returns a new context with the raw bearer/API token embedded.
// This allows downstream code (e.g. internal HTTP loopback calls) to forward the
// original credential without re-extracting it from HTTP headers.
func ContextWithRawToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, rawTokenCtxKey{}, token)
}

// RawTokenFromContext extracts the raw bearer/API token from a standard context.Context.
// Returns empty string if no token is present.
func RawTokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(rawTokenCtxKey{}).(string); ok {
		return token
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
