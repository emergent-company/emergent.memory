// Package auth provides authentication mechanisms for the Emergent API SDK.
package auth

import (
	"context"
	"net/http"
)

// Provider defines the interface for authentication providers.
type Provider interface {
	// Authenticate adds authentication headers to the HTTP request.
	Authenticate(req *http.Request) error

	// Refresh refreshes authentication credentials if applicable.
	// For API key auth, this is a no-op.
	// For OAuth, this triggers token refresh if needed.
	Refresh(ctx context.Context) error
}
