package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevokeToken_Success(t *testing.T) {
	var receivedToken, receivedHint, receivedClientID string

	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/v2/revoke": func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "POST", r.Method)
			require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
			require.NoError(t, r.ParseForm())
			receivedToken = r.FormValue("token")
			receivedHint = r.FormValue("token_type_hint")
			receivedClientID = r.FormValue("client_id")
			w.WriteHeader(http.StatusOK)
		},
	})
	defer server.Close()

	err := RevokeToken(server.URL+"/oauth/v2/revoke", "test-client", "my-token", "access_token")
	require.NoError(t, err)

	assert.Equal(t, "my-token", receivedToken)
	assert.Equal(t, "access_token", receivedHint)
	assert.Equal(t, "test-client", receivedClientID)
}

func TestRevokeToken_EmptyEndpoint(t *testing.T) {
	err := RevokeToken("", "test-client", "my-token", "access_token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no revocation endpoint")
}

func TestRevokeToken_EmptyToken(t *testing.T) {
	err := RevokeToken("http://example.com/revoke", "test-client", "", "access_token")
	assert.NoError(t, err) // nothing to revoke
}

func TestRevokeToken_Non2xxResponse(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/revoke": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		},
	})
	defer server.Close()

	err := RevokeToken(server.URL+"/revoke", "test-client", "my-token", "access_token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
}

func TestRevokeToken_NetworkError(t *testing.T) {
	err := RevokeToken("http://invalid.test.local.nonexistent/revoke", "test-client", "my-token", "access_token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "revocation request failed")
}

// newRevokeTestServer creates a test server that serves both OIDC discovery
// (with a revocation_endpoint pointing back to itself) and a revocation handler.
func newRevokeTestServer(t *testing.T, revokedTokens *[]string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		testutil.WithJSONResponse(200, map[string]interface{}{
			"issuer":                        "https://auth.example.com",
			"device_authorization_endpoint": "https://auth.example.com/oauth/device_authorization",
			"token_endpoint":                "https://auth.example.com/oauth/token",
			"userinfo_endpoint":             "https://auth.example.com/oauth/userinfo",
			"revocation_endpoint":           ts.URL + "/oauth/v2/revoke",
		})(w, r)
	})

	mux.HandleFunc("/oauth/v2/revoke", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		*revokedTokens = append(*revokedTokens, r.FormValue("token_type_hint")+":"+r.FormValue("token"))
		w.WriteHeader(http.StatusOK)
	})

	return ts
}

func TestRevokeCredentials_Success(t *testing.T) {
	var revokedTokens []string
	server := newRevokeTestServer(t, &revokedTokens)
	defer server.Close()

	creds := &Credentials{
		AccessToken:  "my-access-token",
		RefreshToken: "my-refresh-token",
		IssuerURL:    server.URL,
	}

	warnings := RevokeCredentials(creds, "test-client")
	assert.Empty(t, warnings)
	assert.Len(t, revokedTokens, 2)
	assert.Equal(t, "refresh_token:my-refresh-token", revokedTokens[0])
	assert.Equal(t, "access_token:my-access-token", revokedTokens[1])
}

func TestRevokeCredentials_NoIssuerURL(t *testing.T) {
	creds := &Credentials{
		AccessToken:  "my-access-token",
		RefreshToken: "my-refresh-token",
	}

	warnings := RevokeCredentials(creds, "test-client")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "no issuer URL")
}

func TestRevokeCredentials_NoRevocationEndpoint(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/.well-known/openid-configuration": testutil.WithJSONResponse(200, map[string]interface{}{
			"issuer":                        "https://auth.example.com",
			"device_authorization_endpoint": "https://auth.example.com/oauth/device_authorization",
			"token_endpoint":                "https://auth.example.com/oauth/token",
			"userinfo_endpoint":             "https://auth.example.com/oauth/userinfo",
		}),
	})
	defer server.Close()

	creds := &Credentials{
		AccessToken: "my-access-token",
		IssuerURL:   server.URL,
	}

	warnings := RevokeCredentials(creds, "test-client")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "does not expose a revocation endpoint")
}

func TestRevokeCredentials_DiscoveryFails(t *testing.T) {
	creds := &Credentials{
		AccessToken: "my-access-token",
		IssuerURL:   "http://invalid.test.local.nonexistent",
	}

	warnings := RevokeCredentials(creds, "test-client")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "failed to discover OIDC config")
}
