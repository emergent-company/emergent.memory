package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IsAPIToken
// =============================================================================

func TestIsAPIToken(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{"valid emt_ token", "emt_abc123def456", true},
		{"valid emt_ token with 64 hex chars", "emt_" + "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", true},
		{"just emt_ prefix", "emt_x", true},
		{"empty string", "", false},
		{"short string", "emt", false},
		{"just prefix no content", "emt_", false},
		{"standalone API key", "sk-abc123", false},
		{"random string", "some-random-key", false},
		{"bearer token", "Bearer emt_abc123", false},
		{"uppercase EMT_", "EMT_abc123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsAPIToken(tt.key))
		})
	}
}

// =============================================================================
// APITokenProvider
// =============================================================================

func TestAPITokenProvider_Authenticate(t *testing.T) {
	token := "emt_abc123def456"
	provider := NewAPITokenProvider(token)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	err := provider.Authenticate(req)

	require.NoError(t, err)
	assert.Equal(t, "Bearer "+token, req.Header.Get("Authorization"))
	// Should NOT set X-API-Key
	assert.Empty(t, req.Header.Get("X-API-Key"))
}

func TestAPITokenProvider_Refresh(t *testing.T) {
	provider := NewAPITokenProvider("emt_abc123")

	// Refresh should be a no-op
	err := provider.Refresh(t.Context())
	assert.NoError(t, err)
}

// =============================================================================
// APIKeyProvider (existing, for comparison)
// =============================================================================

func TestAPIKeyProvider_Authenticate(t *testing.T) {
	key := "sk-standalone-key"
	provider := NewAPIKeyProvider(key)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	err := provider.Authenticate(req)

	require.NoError(t, err)
	assert.Equal(t, key, req.Header.Get("X-API-Key"))
	// Should NOT set Authorization
	assert.Empty(t, req.Header.Get("Authorization"))
}

func TestAPIKeyProvider_Refresh(t *testing.T) {
	provider := NewAPIKeyProvider("sk-key")

	err := provider.Refresh(t.Context())
	assert.NoError(t, err)
}

// =============================================================================
// Provider interface compliance
// =============================================================================

func TestAPITokenProvider_ImplementsProvider(t *testing.T) {
	var _ Provider = (*APITokenProvider)(nil)
}

func TestAPIKeyProvider_ImplementsProvider(t *testing.T) {
	var _ Provider = (*APIKeyProvider)(nil)
}
