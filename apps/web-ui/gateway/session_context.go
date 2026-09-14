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
