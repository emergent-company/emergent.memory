package main

import "context"

// sessionContext carries the per-request Memory credentials resolved from a
// signed-in web session (design D3). A single MemoryClient serves many
// sessions by reading these from the request context instead of process-global
// struct fields.
type sessionContext struct {
	Token        string // Memory bearer token (Zitadel access token for web sessions)
	ProjectID    string // active project for the session
	OrgID        string // active org when known (may be empty)
	RefreshToken string // refresh token, for re-issuing the session cookie
	ExpiresAt    int64  // unix seconds; session expiry carried for re-issue
	Name         string // signed-in user identity (from the ID token)
	Email        string
	Picture      string
	Sub          string // stable account id (Zitadel subject)

	// Device marks a scoped per-device credential context (not a web session).
	// Token then holds the device credential verbatim and ProjectID/OrgID are
	// derived from token introspection, never from raw client headers.
	Device bool

	// AvatarOverrideURL is the gateway-relative memory avatar URL
	// (/api/user/avatar?v=<key>) when the user uploaded a profile photo; it
	// wins over the IdP picture when both are present.
	AvatarOverrideURL string
}

type sessionContextKey struct{}

// withSessionContext returns a context carrying the session credentials scoped
// to one request.
func withSessionContext(ctx context.Context, sc *sessionContext) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, sc)
}

// sessionContextFrom returns the request's session credentials. The bool is
// false when no session context was attached (the API-key / programmatic
// path).
func sessionContextFrom(ctx context.Context) (*sessionContext, bool) {
	sc, ok := ctx.Value(sessionContextKey{}).(*sessionContext)
	return sc, ok
}

// sessionlessTokenKey carries a session-less bearer token (a scoped programmatic
// credential, e.g. the GitHub webhook trigger token) through a request context.
// A session-less token carries no project/org identity: unlike a web session, the
// MemoryClient leaves project scope to its static project id (the deployment
// default, which equals the token's own bound project) and sends no
// X-Project-ID/X-Org-ID headers — the server derives the project from the
// token's binding, never from a client header (#1998).
type sessionlessTokenKey struct{}

// withSessionlessToken returns a context carrying a session-less bearer token.
func withSessionlessToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, sessionlessTokenKey{}, token)
}

// sessionlessTokenFrom returns the session-less bearer token, or "" when none
// is attached.
func sessionlessTokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(sessionlessTokenKey{}).(string)
	return t
}
