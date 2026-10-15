package mcpregistry

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// These tests exercise the full MCP OAuth connect flow — discovery, dynamic
// client registration, state persistence, and code exchange — against a real
// database and a local httptest authorization server. They are the end-to-end
// counterpart to the unit tests in oauth_test.go, which exercise the individual
// client operations over the same test seam (withTestOAuthHTTP).
//
// They are database-backed: when no database is reachable (and REQUIRE_DB is
// unset) they skip, matching the repo-wide database-required contract.

// oauthFlowServer serves the complete MCP OAuth connect surface: a protected MCP
// endpoint, protected-resource metadata, authorization-server metadata, dynamic
// client registration, and the token endpoint.
type oauthFlowServer struct {
	*httptest.Server
	registerCalls      int
	tokenCalls         int
	registrationStatus int
}

// newOAuthFlowServer builds the flow server. When noChallenge is true the MCP
// endpoint answers 200 without a Bearer challenge, so discovery reports "no
// authorization required".
func newOAuthFlowServer(t *testing.T, noChallenge bool) *oauthFlowServer {
	t.Helper()
	fs := &oauthFlowServer{registrationStatus: http.StatusCreated}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	fs.Server = srv

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		if noChallenge {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
			return
		}
		w.Header().Set("WWW-Authenticate",
			`Bearer resource_metadata="`+srv.URL+`/.well-known/oauth-protected-resource",scope="mcp:connect"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":                 srv.URL + "/mcp",
			"authorization_servers":    []string{srv.URL},
			"scopes_supported":         []string{"mcp:connect"},
			"bearer_methods_supported": []string{"header"},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"registration_endpoint":                 srv.URL + "/register",
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, _ *http.Request) {
		fs.registerCalls++
		if fs.registrationStatus != http.StatusCreated && fs.registrationStatus != http.StatusOK {
			w.WriteHeader(fs.registrationStatus)
			_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"client not in the provider catalog"}`))
			return
		}
		w.WriteHeader(fs.registrationStatus)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "cid-flow", "client_secret": "sec-flow"})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		fs.tokenCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-flow",
			"refresh_token": "rt-flow",
			"token_type":    "Bearer",
			"scope":         "mcp:connect",
			"expires_in":    3600,
		})
	})
	return fs
}

// newOAuthFlowService provisions a throwaway database, encryptor, repository,
// and an OAuth-configured service. The returned database is dropped on cleanup.
func newOAuthFlowService(t *testing.T) (tdb *testdb.TestDB, repo *Repository, enc *crypto.Encryptor, svc *Service) {
	t.Helper()
	tdb = testdb.SetupTestDBOrFail(t, context.Background(), "mcpregistry_oauth_flow")
	t.Cleanup(tdb.Close)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	enc, err = crypto.NewEncryptor(key)
	require.NoError(t, err)

	repo = NewRepository(tdb.DB)

	cfg := OAuthConfig{
		RedirectBaseURL: "https://api.test",
		LandingURL:      "https://ui.test/settings/mcp-servers",
		ClientName:      "Test Client",
	}
	svc, err = NewServiceWithOAuth(repo, nil, enc, cfg, slog.Default())
	require.NoError(t, err)
	return tdb, repo, enc, svc
}

// insertFlowServer writes a remote HTTP server row straight through the
// repository, bypassing the service's SSRF guard so the URL can point at the
// local httptest authorization server.
func insertFlowServer(t *testing.T, repo *Repository, projectID, url string) *MCPServer {
	t.Helper()
	server := &MCPServer{
		ProjectID: projectID,
		Name:      "flow-server",
		Type:      ServerTypeHTTP,
		URL:       strPtr(url),
		Target:    TargetServer,
		Enabled:   true,
	}
	require.NoError(t, repo.CreateServer(context.Background(), server))
	require.NotEmpty(t, server.ID, "Returning(*) must populate the generated server id")
	return server
}

func TestStartMCPOAuth_Success(t *testing.T) {
	withTestOAuthHTTP(t)
	_, repo, _, svc := newOAuthFlowService(t)
	fs := newOAuthFlowServer(t, false)
	defer fs.Close()

	projectID := uuid.NewString()
	server := insertFlowServer(t, repo, projectID, fs.URL+"/mcp")

	resp, err := svc.StartMCPOAuth(context.Background(), projectID, server.ID, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.AuthorizeURL)
	require.NotEmpty(t, resp.State)

	u, err := url.Parse(resp.AuthorizeURL)
	require.NoError(t, err)
	require.Equal(t, resp.State, u.Query().Get("state"), "authorize URL must carry the returned state")
	require.NotEmpty(t, u.Query().Get("code_challenge"), "authorize URL must carry the PKCE challenge")

	require.Equal(t, 1, fs.registerCalls, "a client must be registered exactly once")

	row, err := repo.FindOAuthForUser(context.Background(), server.ID, "")
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, "cid-flow", row.ClientID)
	require.Nil(t, row.AccessToken, "authorize must persist a registration but never a token")
}

func TestStartMCPOAuth_RegistrationRejected(t *testing.T) {
	withTestOAuthHTTP(t)
	_, repo, _, svc := newOAuthFlowService(t)
	fs := newOAuthFlowServer(t, false)
	defer fs.Close()
	fs.registrationStatus = http.StatusForbidden

	projectID := uuid.NewString()
	server := insertFlowServer(t, repo, projectID, fs.URL+"/mcp")

	_, err := svc.StartMCPOAuth(context.Background(), projectID, server.ID, "")
	require.ErrorIs(t, err, ErrOAuthClientRequired,
		"a rejected dynamic client registration must surface as ErrOAuthClientRequired, not an opaque failure")

	var rej *oauthRegistrationRejected
	require.ErrorAs(t, err, &rej)
	require.Equal(t, http.StatusForbidden, rej.Status)
}

func TestStartMCPOAuth_UsesProvisionedClient(t *testing.T) {
	withTestOAuthHTTP(t)
	_, repo, _, svc := newOAuthFlowService(t)
	fs := newOAuthFlowServer(t, false)
	defer fs.Close()

	projectID := uuid.NewString()
	server := insertFlowServer(t, repo, projectID, fs.URL+"/mcp")

	// Provision a pre-registered client first; the authorize flow must reuse it
	// and never hit the dynamic registration endpoint.
	status, err := svc.ProvisionOAuthClient(context.Background(), projectID, server.ID, "", "provisioned-client", "provisioned-secret")
	require.NoError(t, err)
	require.True(t, status.ClientConfigured)

	resp, err := svc.StartMCPOAuth(context.Background(), projectID, server.ID, "")
	require.NoError(t, err)
	require.Zero(t, fs.registerCalls, "a provisioned client must not trigger dynamic client registration")

	u, err := url.Parse(resp.AuthorizeURL)
	require.NoError(t, err)
	require.Equal(t, "provisioned-client", u.Query().Get("client_id"), "authorize URL must carry the provisioned client_id")
}

func TestStartMCPOAuth_NoAuthorizationRequired(t *testing.T) {
	withTestOAuthHTTP(t)
	_, repo, _, svc := newOAuthFlowService(t)
	fs := newOAuthFlowServer(t, true)
	defer fs.Close()

	projectID := uuid.NewString()
	server := insertFlowServer(t, repo, projectID, fs.URL+"/mcp")

	_, err := svc.StartMCPOAuth(context.Background(), projectID, server.ID, "")
	require.ErrorIs(t, err, ErrOAuthNotRequired)
}

func TestCompleteMCPOAuth_Success(t *testing.T) {
	withTestOAuthHTTP(t)
	_, repo, enc, svc := newOAuthFlowService(t)
	fs := newOAuthFlowServer(t, false)
	defer fs.Close()

	projectID := uuid.NewString()
	server := insertFlowServer(t, repo, projectID, fs.URL+"/mcp")

	start, err := svc.StartMCPOAuth(context.Background(), projectID, server.ID, "")
	require.NoError(t, err)

	gotServerID, gotProjectID, err := svc.CompleteMCPOAuth(context.Background(), start.State, "code-1")
	require.NoError(t, err)
	require.Equal(t, server.ID, gotServerID)
	require.Equal(t, projectID, gotProjectID)
	require.Equal(t, 1, fs.tokenCalls, "the authorization code must be exchanged exactly once")

	row, err := repo.FindOAuthForUser(context.Background(), server.ID, "")
	require.NoError(t, err)
	require.NotNil(t, row)
	require.NotNil(t, row.AccessToken)
	require.NotNil(t, row.RefreshToken)
	require.NotNil(t, row.ExpiresAt)

	access, err := enc.Decrypt(row.AccessToken.Ciphertext, row.AccessToken.Nonce)
	require.NoError(t, err)
	require.Equal(t, "at-flow", string(access), "stored access token must be the one the token endpoint issued")

	refresh, err := enc.Decrypt(row.RefreshToken.Ciphertext, row.RefreshToken.Nonce)
	require.NoError(t, err)
	require.Equal(t, "rt-flow", string(refresh))
}

func TestCompleteMCPOAuth_ReplayedState(t *testing.T) {
	withTestOAuthHTTP(t)
	_, repo, _, svc := newOAuthFlowService(t)
	fs := newOAuthFlowServer(t, false)
	defer fs.Close()

	projectID := uuid.NewString()
	server := insertFlowServer(t, repo, projectID, fs.URL+"/mcp")

	start, err := svc.StartMCPOAuth(context.Background(), projectID, server.ID, "")
	require.NoError(t, err)

	_, _, err = svc.CompleteMCPOAuth(context.Background(), start.State, "code-1")
	require.NoError(t, err)

	// The state is single-use: replaying the same state is rejected.
	_, _, err = svc.CompleteMCPOAuth(context.Background(), start.State, "code-2")
	require.ErrorIs(t, err, ErrOAuthStateInvalid)
}

func TestCompleteMCPOAuth_ExpiredState(t *testing.T) {
	_, repo, _, svc := newOAuthFlowService(t)

	// A pending state whose TTL has elapsed must be rejected before any server
	// lookup or token exchange, so no network seam is needed.
	expired := &mcpOAuthPendingState{
		State:        "expired-state",
		ProjectID:    uuid.NewString(),
		Provider:     oauthProviderMCP,
		CodeVerifier: "verifier",
		RedirectURI:  "https://api.test/api/mcp-servers/oauth/callback",
		Config:       map[string]any{"serverId": "11111111-1111-1111-1111-111111111111"},
		Scopes:       []string{"mcp:connect"},
		ExpiresAt:    time.Now().UTC().Add(-time.Minute),
	}
	require.NoError(t, repo.CreateOAuthPendingState(context.Background(), expired))

	_, _, err := svc.CompleteMCPOAuth(context.Background(), "expired-state", "code-1")
	require.ErrorIs(t, err, ErrOAuthStateInvalid)
}
