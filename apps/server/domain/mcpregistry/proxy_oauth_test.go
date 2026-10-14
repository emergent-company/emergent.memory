package mcpregistry

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- helpers ---

// oauthTestProxy builds a ProxyManager with a real encryptor so stored token
// material round-trips through AES-GCM exactly as it does in production.
func oauthTestProxy(t *testing.T, repo *fakeProxyRepo) *ProxyManager {
	t.Helper()
	return &ProxyManager{
		repo:      repo,
		encryptor: testEncryptor(t),
		log:       slog.Default(),
		conns:     make(map[string]*mcpConnection),
	}
}

// oauthRow builds a stored credential row for a server, encrypting the token
// values with the manager's encryptor.
func oauthRow(t *testing.T, pm *ProxyManager, serverID, access, refresh, tokenEndpoint, resource string, expiresAt *time.Time) *MCPServerOAuth {
	t.Helper()
	row := &MCPServerOAuth{
		ServerID:      serverID,
		ClientID:      "cid",
		TokenEndpoint: tokenEndpoint,
		Resource:      resource,
		Scope:         "mcp:connect",
		ExpiresAt:     expiresAt,
	}
	var err error
	if access != "" {
		row.AccessToken, err = encryptSecretValue(pm.encryptor, access)
		require.NoError(t, err)
	}
	if refresh != "" {
		row.RefreshToken, err = encryptSecretValue(pm.encryptor, refresh)
		require.NoError(t, err)
	}
	return row
}

func remoteServer(id, projectID, name, url string) *MCPServer {
	return &MCPServer{ID: id, ProjectID: projectID, Name: name, Enabled: true, Type: ServerTypeHTTP, URL: strPtr(url)}
}

// --- header resolution ---

func TestRequestHeaders_AttachesOAuthBearer(t *testing.T) {
	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	expiry := time.Now().UTC().Add(time.Hour)
	row := oauthRow(t, pm, "s1", "oauth-access", "rt", "https://auth.test/token", "https://mcp.test/mcp", &expiry)
	repo.oauth = map[string]*MCPServerOAuth{"s1": row}

	server := remoteServer("s1", "p1", "figma", "https://mcp.test/mcp")
	server.Headers = map[string]any{"X-Extra": "1"}

	headers, err := pm.requestHeaders(context.Background(), server)
	require.NoError(t, err)
	if got := headers["Authorization"]; got != "Bearer oauth-access" {
		t.Errorf("Authorization = %q, want the OAuth bearer", got)
	}
	if headers["X-Extra"] != "1" {
		t.Errorf("plaintext headers were dropped: %v", headers)
	}
}

func TestRequestHeaders_OAuthBearerBeatsStaticAuthorization(t *testing.T) {
	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	expiry := time.Now().UTC().Add(time.Hour)
	repo.oauth = map[string]*MCPServerOAuth{
		"s1": oauthRow(t, pm, "s1", "oauth-access", "rt", "https://auth.test/token", "https://mcp.test/mcp", &expiry),
	}

	server := remoteServer("s1", "p1", "figma", "https://mcp.test/mcp")
	// A statically configured bearer must not be sent alongside the credential
	// the OAuth flow owns.
	server.Headers = map[string]any{"Authorization": "Bearer static-token"}

	headers, err := pm.requestHeaders(context.Background(), server)
	require.NoError(t, err)
	if got := headers["Authorization"]; got != "Bearer oauth-access" {
		t.Errorf("Authorization = %q, want only the OAuth bearer", got)
	}
}

func TestRequestHeaders_NoCredentialLeavesHeadersAlone(t *testing.T) {
	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)

	server := remoteServer("s1", "p1", "figma", "https://mcp.test/mcp")
	server.Headers = map[string]any{"Authorization": "Bearer static-token"}

	headers, err := pm.requestHeaders(context.Background(), server)
	require.NoError(t, err)
	if got := headers["Authorization"]; got != "Bearer static-token" {
		t.Errorf("Authorization = %q, want the configured header untouched", got)
	}
}

// --- refresh ---

func TestOAuthAuthorizationHeader_RefreshesExpiredTokenSingleFlight(t *testing.T) {
	withTestOAuthHTTP(t)

	var tokenCalls int32
	var gotForm struct {
		grantType string
		resource  string
	}
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		_ = r.ParseForm()
		gotForm.grantType = r.PostForm.Get("grant_type")
		gotForm.resource = r.PostForm.Get("resource")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-new", "expires_in": 3600})
	}))
	defer tokenSrv.Close()

	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	expired := time.Now().UTC().Add(-time.Minute)
	repo.oauth = map[string]*MCPServerOAuth{
		"s1": oauthRow(t, pm, "s1", "at-old", "rt-1", tokenSrv.URL, "https://mcp.test/mcp", &expired),
	}
	server := remoteServer("s1", "p1", "figma", "https://mcp.test/mcp")

	const goroutines = 8
	var wg sync.WaitGroup
	results := make([]string, goroutines)
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = pm.oauthAuthorizationHeader(context.Background(), server)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "goroutine %d", i)
	}
	for i, got := range results {
		if got != "Bearer at-new" {
			t.Errorf("goroutine %d got %q, want Bearer at-new", i, got)
		}
	}
	if got := atomic.LoadInt32(&tokenCalls); got != 1 {
		t.Errorf("token endpoint called %d times, want exactly 1 (single-flight)", got)
	}
	if gotForm.grantType != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", gotForm.grantType)
	}
	if gotForm.resource != "https://mcp.test/mcp" {
		t.Errorf("resource = %q, want the protected resource", gotForm.resource)
	}

	// The refreshed token set is persisted for the next call.
	row, err := repo.FindOAuthByServerID(context.Background(), "s1")
	require.NoError(t, err)
	stored, err := decryptSecretValue(pm.encryptor, row.AccessToken)
	require.NoError(t, err)
	if stored != "at-new" {
		t.Errorf("stored access token = %q, want at-new", stored)
	}
	if row.ExpiresAt == nil || !row.ExpiresAt.After(time.Now().UTC()) {
		t.Errorf("stored expiry = %v, want a future time", row.ExpiresAt)
	}
}

func TestOAuthAuthorizationHeader_RefreshRetainsRefreshTokenWhenNotReissued(t *testing.T) {
	withTestOAuthHTTP(t)
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A refresh response without a new refresh_token must keep the old one.
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-new", "expires_in": 60})
	}))
	defer tokenSrv.Close()

	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	expired := time.Now().UTC().Add(-time.Minute)
	repo.oauth = map[string]*MCPServerOAuth{
		"s1": oauthRow(t, pm, "s1", "at-old", "rt-keep", tokenSrv.URL, "https://mcp.test/mcp", &expired),
	}

	if _, err := pm.oauthAuthorizationHeader(context.Background(), remoteServer("s1", "p1", "figma", "https://mcp.test/mcp")); err != nil {
		t.Fatalf("oauthAuthorizationHeader: %v", err)
	}
	row, err := repo.FindOAuthByServerID(context.Background(), "s1")
	require.NoError(t, err)
	refresh, err := decryptSecretValue(pm.encryptor, row.RefreshToken)
	require.NoError(t, err)
	if refresh != "rt-keep" {
		t.Errorf("refresh token = %q, want the original rt-keep", refresh)
	}
}

func TestOAuthAuthorizationHeader_ExpiredWithoutRefreshTokenNeedsReconnect(t *testing.T) {
	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	expired := time.Now().UTC().Add(-time.Minute)
	repo.oauth = map[string]*MCPServerOAuth{
		"s1": oauthRow(t, pm, "s1", "at-old", "", "https://auth.test/token", "https://mcp.test/mcp", &expired),
	}

	_, err := pm.oauthAuthorizationHeader(context.Background(), remoteServer("s1", "p1", "figma", "https://mcp.test/mcp"))
	if !errorIs(err, ErrOAuthNeedsReconnect) {
		t.Errorf("err = %v, want ErrOAuthNeedsReconnect", err)
	}
}

func TestOAuthAuthorizationHeader_RefreshRejectedNeedsReconnect(t *testing.T) {
	withTestOAuthHTTP(t)
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenSrv.Close()

	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	expired := time.Now().UTC().Add(-time.Minute)
	repo.oauth = map[string]*MCPServerOAuth{
		"s1": oauthRow(t, pm, "s1", "at-old", "rt-bad", tokenSrv.URL, "https://mcp.test/mcp", &expired),
	}

	_, err := pm.oauthAuthorizationHeader(context.Background(), remoteServer("s1", "p1", "figma", "https://mcp.test/mcp"))
	if !errorIs(err, ErrOAuthNeedsReconnect) {
		t.Errorf("err = %v, want ErrOAuthNeedsReconnect", err)
	}
}

func TestOAuthAuthorizationHeader_NoCredential(t *testing.T) {
	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)

	got, err := pm.oauthAuthorizationHeader(context.Background(), remoteServer("s1", "p1", "figma", "https://mcp.test/mcp"))
	require.NoError(t, err)
	if got != "" {
		t.Errorf("header = %q, want empty for a server with no credential", got)
	}
}

func TestOAuthAuthorizationHeader_ConnectedWithoutExpiryUsesToken(t *testing.T) {
	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	repo.oauth = map[string]*MCPServerOAuth{
		"s1": oauthRow(t, pm, "s1", "at-stable", "", "https://auth.test/token", "https://mcp.test/mcp", nil),
	}

	got, err := pm.oauthAuthorizationHeader(context.Background(), remoteServer("s1", "p1", "figma", "https://mcp.test/mcp"))
	require.NoError(t, err)
	if got != "Bearer at-stable" {
		t.Errorf("header = %q, want Bearer at-stable (no expiry means no refresh)", got)
	}
}

// --- inspect classification ---

func TestClassifyAuthRequired_ChallengeBecomesRequirement(t *testing.T) {
	withTestOAuthHTTP(t)
	ts := newOAuthTestServer(t)
	defer ts.Close()

	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	server := remoteServer("s1", "p1", "figma", ts.URL+"/mcp")

	auth := classifyAuthRequired(context.Background(), pm, server, &ErrMCPAuthRejected{Status: http.StatusUnauthorized})
	if auth == nil {
		t.Fatal("expected an InspectAuthDTO")
	}
	if auth.AuthorizationServer != ts.URL {
		t.Errorf("AuthorizationServer = %q, want %q", auth.AuthorizationServer, ts.URL)
	}
	if len(auth.Scopes) != 1 || auth.Scopes[0] != "mcp:connect" {
		t.Errorf("Scopes = %v", auth.Scopes)
	}
	if !auth.RegistrationSupported {
		t.Error("RegistrationSupported = false, want true")
	}
}

func TestClassifyAuthRequired_NonAuthErrorIsNotARequirement(t *testing.T) {
	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	server := remoteServer("s1", "p1", "figma", "https://mcp.test/mcp")

	if auth := classifyAuthRequired(context.Background(), pm, server, context.DeadlineExceeded); auth != nil {
		t.Errorf("auth = %+v, want nil for a non-auth error", auth)
	}
}

func TestClassifyAuthRequired_401WithoutChallengeStaysAnError(t *testing.T) {
	withTestOAuthHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	server := remoteServer("s1", "p1", "figma", srv.URL)

	// A plain 401 (a wrong static header) must not grow a Connect affordance.
	if auth := classifyAuthRequired(context.Background(), pm, server, &ErrMCPAuthRejected{Status: http.StatusUnauthorized}); auth != nil {
		t.Errorf("auth = %+v, want nil", auth)
	}
}

func TestClassifyAuthRequired_MetadataUnreachableStaysAnError(t *testing.T) {
	withTestOAuthHTTP(t)
	ts := newOAuthTestServer(t)
	ts.failPRM = true
	defer ts.Close()

	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	server := remoteServer("s1", "p1", "figma", ts.URL+"/mcp")

	if auth := classifyAuthRequired(context.Background(), pm, server, &ErrMCPAuthRejected{Status: http.StatusUnauthorized}); auth != nil {
		t.Errorf("auth = %+v, want nil when metadata cannot be fetched", auth)
	}
}

func TestClassifyAuthRequired_NoMetadataReportsUnavailable(t *testing.T) {
	withTestOAuthHTTP(t)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	issuer := srv.URL + "/login/oauth"

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+srv.URL+`/.well-known/oauth-protected-resource"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              srv.URL + "/mcp",
			"authorization_servers": []string{issuer},
		})
	})
	// No well-known metadata anywhere.

	repo := &fakeProxyRepo{}
	pm := oauthTestProxy(t, repo)
	server := remoteServer("s1", "p1", "github", srv.URL+"/mcp")

	auth := classifyAuthRequired(context.Background(), pm, server, &ErrMCPAuthRejected{Status: http.StatusUnauthorized})
	if auth == nil {
		t.Fatal("expected an InspectAuthDTO (the server did reject us with an auth requirement)")
	}
	if !auth.Unavailable {
		t.Error("Unavailable = false, want true for a provider with no discoverable metadata")
	}
	if auth.Remedy == "" {
		t.Error("Remedy is empty, want the token-header remedy")
	}
	if auth.AuthorizationServer != "" {
		t.Errorf("AuthorizationServer = %q, want empty for an unavailable requirement", auth.AuthorizationServer)
	}
}

// --- install classification ---

func TestClassifyInstallAuth_ChallengeBecomesAuthorization(t *testing.T) {
	withTestOAuthHTTP(t)
	ts := newOAuthTestServer(t)
	defer ts.Close()

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(ts.URL + "/mcp")}

	requiresAuth, requiresTokenHeader := classifyInstallAuth(context.Background(), server, &ErrMCPAuthRejected{Status: http.StatusUnauthorized})
	if !requiresAuth {
		t.Error("requiresAuthorization = false, want true (discoverable metadata)")
	}
	if requiresTokenHeader {
		t.Error("requiresTokenHeader = true, want false when metadata is discoverable")
	}
}

func TestClassifyInstallAuth_NoMetadataRequiresTokenHeader(t *testing.T) {
	withTestOAuthHTTP(t)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	issuer := srv.URL + "/login/oauth"

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+srv.URL+`/.well-known/oauth-protected-resource"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              srv.URL + "/mcp",
			"authorization_servers": []string{issuer},
		})
	})
	// No well-known metadata anywhere: the connect flow cannot work.

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(srv.URL + "/mcp")}

	requiresAuth, requiresTokenHeader := classifyInstallAuth(context.Background(), server, &ErrMCPAuthRejected{Status: http.StatusUnauthorized})
	if requiresAuth {
		t.Error("requiresAuthorization = true, want false (no discoverable metadata)")
	}
	if !requiresTokenHeader {
		t.Error("requiresTokenHeader = false, want true (token header is the remedy)")
	}
}

func TestClassifyInstallAuth_NonAuthErrorIsNeither(t *testing.T) {
	withTestOAuthHTTP(t)
	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr("https://mcp.test/mcp")}

	requiresAuth, requiresTokenHeader := classifyInstallAuth(context.Background(), server, context.DeadlineExceeded)
	if requiresAuth {
		t.Error("requiresAuthorization = true, want false for a non-auth error")
	}
	if requiresTokenHeader {
		t.Error("requiresTokenHeader = true, want false for a non-auth error")
	}
}
