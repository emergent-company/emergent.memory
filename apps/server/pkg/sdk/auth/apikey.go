package auth

import (
	"context"
	"net/http"
)

// APIKeyProvider implements Provider for API key authentication.
type APIKeyProvider struct {
	apiKey string
}

// NewAPIKeyProvider creates a new API key authentication provider.
func NewAPIKeyProvider(apiKey string) *APIKeyProvider {
	return &APIKeyProvider{apiKey: apiKey}
}

// Authenticate adds the X-API-Key header to the request.
func (p *APIKeyProvider) Authenticate(req *http.Request) error {
	req.Header.Set("X-API-Key", p.apiKey)
	return nil
}

// Refresh is a no-op for API key authentication.
func (p *APIKeyProvider) Refresh(ctx context.Context) error {
	return nil
}
