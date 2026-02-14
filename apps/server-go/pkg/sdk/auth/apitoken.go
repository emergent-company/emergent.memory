package auth

import (
	"context"
	"net/http"
)

// APITokenProvider implements Provider for project-scoped API token authentication.
// API tokens (emt_* prefix) are sent as Bearer tokens and carry an embedded project ID
// on the server side, so the caller does not need to set X-Project-ID.
type APITokenProvider struct {
	token string
}

// NewAPITokenProvider creates a new API token authentication provider.
// The token should be a project-scoped API token (e.g., "emt_abc123...").
func NewAPITokenProvider(token string) *APITokenProvider {
	return &APITokenProvider{token: token}
}

// Authenticate adds the Authorization: Bearer header with the API token.
func (p *APITokenProvider) Authenticate(req *http.Request) error {
	req.Header.Set("Authorization", "Bearer "+p.token)
	return nil
}

// Refresh is a no-op for API token authentication.
func (p *APITokenProvider) Refresh(ctx context.Context) error {
	return nil
}

// IsAPIToken returns true if the given key looks like a project API token (emt_ prefix).
func IsAPIToken(key string) bool {
	return len(key) > 4 && key[:4] == "emt_"
}
