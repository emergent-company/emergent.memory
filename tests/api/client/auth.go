package client

// TestTokens provides server-aware test token selection.
// Both servers now support the same token mappings after alignment.
type TestTokens struct {
	serverType ServerType
}

// NewTestTokens creates a TestTokens helper for the given server type.
func NewTestTokens(serverType ServerType) *TestTokens {
	return &TestTokens{serverType: serverType}
}

// Tokens returns the TestTokens helper from the client.
func (c *Client) Tokens() *TestTokens {
	return NewTestTokens(c.serverType)
}

// Admin returns the admin token with full access.
// Maps to test-admin-user on both servers (after NestJS alignment).
func (t *TestTokens) Admin() string {
	return "e2e-test-user"
}

// AllScopes returns a token with all scopes.
// Note: Go uses "all-scopes", NestJS uses "e2e-all" - both give full access.
func (t *TestTokens) AllScopes() string {
	if t.serverType == ServerGo {
		return "all-scopes"
	}
	return "e2e-all"
}

// NoScope returns a token with no scopes.
func (t *TestTokens) NoScope() string {
	return "no-scope"
}

// WithScope returns a token with limited scopes.
// Note: Scope sets differ between servers:
// - Go: documents:read, documents:write, project:read
// - NestJS: org:read
func (t *TestTokens) WithScope() string {
	return "with-scope"
}

// GraphRead returns a token with graph read permissions.
func (t *TestTokens) GraphRead() string {
	return "graph-read"
}

// ReadOnly returns a token with read-only permissions (Go only).
// For NestJS, falls back to with-scope.
func (t *TestTokens) ReadOnly() string {
	if t.serverType == ServerGo {
		return "read-only"
	}
	// NestJS doesn't have read-only token, use with-scope
	return "with-scope"
}

// Dynamic returns a dynamic e2e token that creates an ad-hoc user.
// The token format is e2e-{suffix} which creates user test-user-e2e-{suffix} (NestJS)
// or uses {suffix} directly as zitadel_user_id (Go for e2e-* pattern).
func (t *TestTokens) Dynamic(suffix string) string {
	return "e2e-" + suffix
}
