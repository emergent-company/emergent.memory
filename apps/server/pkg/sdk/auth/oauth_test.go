package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Device flow scopes
// =============================================================================

func TestInitiateDeviceFlowRequestsOfflineAccess(t *testing.T) {
	var gotScope, gotClientID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		gotScope = r.Form.Get("scope")
		gotClientID = r.Form.Get("client_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"uc","verification_uri":"https://example.test/device","expires_in":600,"interval":5}`))
	}))
	defer srv.Close()

	credsPath := filepath.Join(t.TempDir(), "credentials.json")
	p := NewOAuthProvider(testOIDCConfig(srv.URL), "client-1", credsPath)

	_, err := p.InitiateDeviceFlow(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "client-1", gotClientID)
	assert.Contains(t, gotScope, "openid")
	assert.Contains(t, gotScope, "offline_access")
}

func TestInitiateDeviceFlowCustomScopes(t *testing.T) {
	var gotScope string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		gotScope = r.Form.Get("scope")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"uc","verification_uri":"https://example.test/device","expires_in":600,"interval":5}`))
	}))
	defer srv.Close()

	credsPath := filepath.Join(t.TempDir(), "credentials.json")
	p := NewOAuthProviderWithScopes(testOIDCConfig(srv.URL), "client-1", credsPath, []string{"openid", "custom"})

	_, err := p.InitiateDeviceFlow(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "openid custom", gotScope)
}

func TestNewOAuthProviderWithScopesEmptyUsesDefault(t *testing.T) {
	var gotScope string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		gotScope = r.Form.Get("scope")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"uc","verification_uri":"https://example.test/device","expires_in":600,"interval":5}`))
	}))
	defer srv.Close()

	credsPath := filepath.Join(t.TempDir(), "credentials.json")
	p := NewOAuthProviderWithScopes(testOIDCConfig(srv.URL), "client-1", credsPath, nil)

	_, err := p.InitiateDeviceFlow(context.Background())
	require.NoError(t, err)

	assert.Equal(t, strings.Join(DefaultDeviceFlowScopes, " "), gotScope)
	assert.Contains(t, gotScope, "offline_access")
}

// =============================================================================
// Device flow token persistence
// =============================================================================

func TestPollForTokenPersistsRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.Form.Get("grant_type"))
		assert.Equal(t, "device-code-1", r.Form.Get("device_code"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-token-1","refresh_token":"refresh-token-1","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	credsPath := filepath.Join(t.TempDir(), "credentials.json")
	p := NewOAuthProvider(testOIDCConfig(srv.URL), "client-1", credsPath)

	err := p.PollForToken(context.Background(), "device-code-1", 1, 5)
	require.NoError(t, err)

	creds, err := LoadCredentials(credsPath)
	require.NoError(t, err)
	assert.Equal(t, "access-token-1", creds.AccessToken)
	assert.Equal(t, "refresh-token-1", creds.RefreshToken)

	info, err := os.Stat(credsPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func testOIDCConfig(endpoint string) *OIDCConfig {
	return &OIDCConfig{
		Issuer:                      "https://issuer.test",
		DeviceAuthorizationEndpoint: endpoint,
		TokenEndpoint:               endpoint,
		UserinfoEndpoint:            endpoint,
	}
}

func refreshableProvider(t *testing.T, endpoint string) *OAuthProvider {
	t.Helper()
	credsPath := filepath.Join(t.TempDir(), "credentials.json")
	require.NoError(t, SaveCredentials(&Credentials{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(time.Hour),
	}, credsPath))
	return NewOAuthProvider(testOIDCConfig(endpoint), "client-1", credsPath)
}

func TestRefreshNon200ReturnsRefreshError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token expired"}`))
	}))
	defer srv.Close()

	p := refreshableProvider(t, srv.URL)

	err := p.Refresh(context.Background())
	var re *RefreshError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, http.StatusBadRequest, re.StatusCode)
	assert.Equal(t, "invalid_grant", re.Code)
}

func TestRefreshTransportFailureIsNotRefreshError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	srv.Close() // now unreachable

	p := refreshableProvider(t, srv.URL)

	err := p.Refresh(context.Background())
	require.Error(t, err)
	var re *RefreshError
	assert.False(t, errors.As(err, &re), "transport failure must not be a *RefreshError")
}
