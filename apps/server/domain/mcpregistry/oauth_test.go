package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// errorIs is errors.Is, kept local so the test reads the same as the assertions.
func errorIs(err, target error) bool { return errors.Is(err, target) }

// --- test seams ---

// withTestOAuthHTTP swaps the OAuth client's SSRF guard and client factories so
// tests can drive a local httptest authorization server. The production paths
// (validateOutboundFetchURL + guarded clients) are restored on cleanup, and
// these tests must not run in parallel with each other.
func withTestOAuthHTTP(t *testing.T) {
	t.Helper()
	origValidate, origMeta, origProbe := oauthValidateURL, oauthMetadataClient, oauthProbeHTTPClient
	oauthValidateURL = func(context.Context, string) error { return nil }
	oauthMetadataClient = func(string) *http.Client { return &http.Client{} }
	oauthProbeHTTPClient = func(string) *http.Client { return &http.Client{} }
	t.Cleanup(func() {
		oauthValidateURL = origValidate
		oauthMetadataClient = origMeta
		oauthProbeHTTPClient = origProbe
	})
}

func strPtr(s string) *string { return &s }

// --- transport seam invariant ---

// TestOAuthMetadataClientIsUnwrapped is the regression guard for the
// 403-registration defect: the OAuth protocol client must observe 401/403 as
// ordinary status codes so registerClient and postToken reach their non-2xx
// handling (which build the typed oauthRegistrationRejected / token errors).
//
// authStatusTransport turns any 401/403 into *ErrMCPAuthRejected and closes the
// body, so a wrapped OAuth protocol client would never reach that handling — a
// 403 from dynamic client registration would surface as a generic internal
// error instead of ErrOAuthClientRequired. It reads the default seam directly
// (not via withTestOAuthHTTP, which overrides it) so a revert of the factory to
// outboundHTTPClient fails this test.
func TestOAuthMetadataClientIsUnwrapped(t *testing.T) {
	client := oauthMetadataClient("https://example.com")
	if client == nil {
		t.Fatal("oauthMetadataClient returned a nil client")
	}
	if _, ok := client.Transport.(authStatusTransport); ok {
		t.Fatal("oauthMetadataClient must not wrap its transport in authStatusTransport: a 401/403 response would be turned into ErrMCPAuthRejected before the OAuth protocol's non-2xx handling runs")
	}
	if client.Transport == nil {
		t.Fatal("oauthMetadataClient returned a client with no transport: the SSRF guard must still be installed")
	}
}

// --- WWW-Authenticate parsing ---

// --- well-known URL helpers ---

// --- discovery ---

// oauthTestServer serves the full MCP OAuth surface: a protected MCP endpoint,
// protected-resource metadata, and authorization-server metadata.
type oauthTestServer struct {
	*httptest.Server
	mcpMethod string
	// failPRM makes the protected-resource metadata endpoint return 500.
	failPRM bool
	// noChallengeScope drops the scope parameter from the Bearer challenge,
	// modelling a provider that only advertises scopes_supported.
	noChallengeScope bool
}

func newOAuthTestServer(t *testing.T) *oauthTestServer {
	t.Helper()
	ts := &oauthTestServer{}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	ts.Server = srv

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		ts.mcpMethod = r.Method
		if r.Header.Get("Authorization") != "" {
			// A credentialed caller is not what discovery probes.
			t.Errorf("discovery probe sent an Authorization header: %q", r.Header.Get("Authorization"))
		}
		challenge := `Bearer resource_metadata="` + srv.URL + `/.well-known/oauth-protected-resource"`
		if !ts.noChallengeScope {
			challenge += `,scope="mcp:connect"`
		}
		w.Header().Set("WWW-Authenticate", challenge)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		if ts.failPRM {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
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
	return ts
}

func TestDiscoverServerAuthRequirement_EndToEnd(t *testing.T) {
	withTestOAuthHTTP(t)
	ts := newOAuthTestServer(t)
	defer ts.Close()

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(ts.URL + "/mcp")}

	req, err := discoverServerAuthRequirement(context.Background(), server)
	if err != nil {
		t.Fatalf("discoverServerAuthRequirement: %v", err)
	}
	if req.AuthorizationServer != ts.URL {
		t.Errorf("AuthorizationServer = %q, want %q", req.AuthorizationServer, ts.URL)
	}
	if req.AuthorizationEndpoint != ts.URL+"/authorize" {
		t.Errorf("AuthorizationEndpoint = %q", req.AuthorizationEndpoint)
	}
	if req.TokenEndpoint != ts.URL+"/token" {
		t.Errorf("TokenEndpoint = %q", req.TokenEndpoint)
	}
	if req.RegistrationEndpoint != ts.URL+"/register" || !req.RegistrationSupported {
		t.Errorf("registration not discovered: endpoint=%q supported=%v", req.RegistrationEndpoint, req.RegistrationSupported)
	}
	if got := strings.Join(req.Scopes, " "); got != "mcp:connect" {
		t.Errorf("Scopes = %q, want mcp:connect", got)
	}
	if req.Resource != ts.URL+"/mcp" {
		t.Errorf("Resource = %q, want the MCP endpoint", req.Resource)
	}
	if req.ResourceMetadataURL == "" {
		t.Error("ResourceMetadataURL is empty")
	}
	if ts.mcpMethod != http.MethodPost {
		t.Errorf("probe method = %q, want POST", ts.mcpMethod)
	}
}

func TestDiscoverServerAuthRequirement_NoChallenge(t *testing.T) {
	withTestOAuthHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(srv.URL)}
	_, err := discoverServerAuthRequirement(context.Background(), server)
	if err == nil {
		t.Fatal("expected an error for a server with no OAuth challenge")
	}
	if !errorIs(err, ErrOAuthNotRequired) {
		t.Errorf("err = %v, want ErrOAuthNotRequired", err)
	}
}

func TestDiscoverServerAuthRequirement_401WithoutChallengeIsNotAuthRequired(t *testing.T) {
	withTestOAuthHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="static-token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(srv.URL)}
	_, err := discoverServerAuthRequirement(context.Background(), server)
	if !errorIs(err, ErrOAuthNotRequired) {
		t.Errorf("err = %v, want ErrOAuthNotRequired (no OAuth challenge)", err)
	}
}

func TestDiscoverServerAuthRequirement_MetadataFailure(t *testing.T) {
	withTestOAuthHTTP(t)
	ts := newOAuthTestServer(t)
	ts.failPRM = true
	defer ts.Close()

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(ts.URL + "/mcp")}
	_, err := discoverServerAuthRequirement(context.Background(), server)
	if !errorIs(err, ErrOAuthDiscoveryFailed) {
		t.Errorf("err = %v, want ErrOAuthDiscoveryFailed", err)
	}
}

func TestDiscoverServerAuthRequirement_NonRemoteTransport(t *testing.T) {
	server := &MCPServer{ID: "s1", Type: ServerTypeStdio}
	if _, err := discoverServerAuthRequirement(context.Background(), server); !errorIs(err, ErrOAuthNotSupported) {
		t.Errorf("err = %v, want ErrOAuthNotSupported", err)
	}
}

// --- dynamic client registration ---

func TestRegisterClient(t *testing.T) {
	withTestOAuthHTTP(t)

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding registration body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "cid-1", "client_secret": "sec-1"})
	}))
	defer srv.Close()

	identity, err := registerClient(context.Background(), srv.URL, "Memory", "https://api.example.test/cb", []string{"mcp:connect"})
	if err != nil {
		t.Fatalf("registerClient: %v", err)
	}
	if identity.ClientID != "cid-1" || identity.ClientSecret != "sec-1" {
		t.Errorf("identity = %+v", identity)
	}
	if gotBody["token_endpoint_auth_method"] != "none" {
		t.Errorf("token_endpoint_auth_method = %v, want none", gotBody["token_endpoint_auth_method"])
	}
	uris, _ := gotBody["redirect_uris"].([]any)
	if len(uris) != 1 || uris[0] != "https://api.example.test/cb" {
		t.Errorf("redirect_uris = %v", gotBody["redirect_uris"])
	}
	if gotBody["scope"] != "mcp:connect" {
		t.Errorf("scope = %v", gotBody["scope"])
	}
}

func TestRegisterClient_Rejected(t *testing.T) {
	withTestOAuthHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_redirect_uri"}`))
	}))
	defer srv.Close()

	if _, err := registerClient(context.Background(), srv.URL, "n", "https://cb.test", nil); err == nil {
		t.Fatal("expected an error for a rejected registration")
	}
}

func TestRegisterClient_RejectedSurfacesStatusAndSentinel(t *testing.T) {
	withTestOAuthHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"client not in the provider catalog"}`))
	}))
	defer srv.Close()

	_, err := registerClient(context.Background(), srv.URL, "n", "https://cb.test", nil)
	if err == nil {
		t.Fatal("expected an error for a rejected registration")
	}

	var rej *oauthRegistrationRejected
	if !errors.As(err, &rej) {
		t.Fatalf("err = %v, want *oauthRegistrationRejected", err)
	}
	if rej.Status != http.StatusForbidden {
		t.Errorf("Status = %d, want 403", rej.Status)
	}
	if !strings.Contains(rej.Error(), "403") {
		t.Errorf("err = %q, want it to name the HTTP status", rej.Error())
	}
	if !errorIs(err, ErrOAuthClientRequired) {
		t.Errorf("err = %v, want it to wrap ErrOAuthClientRequired", err)
	}
}

func TestRegisterClient_RejectedExcerptIsCappedAndSanitized(t *testing.T) {
	withTestOAuthHTTP(t)
	// A long body with embedded control characters and whitespace must collapse
	// to a short, single-line excerpt.
	body := strings.Repeat("x", 5000) + "\n\t\x00" + `{"error":"access_denied"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, err := registerClient(context.Background(), srv.URL, "n", "https://cb.test", nil)
	if err == nil {
		t.Fatal("expected an error for a rejected registration")
	}
	if len(err.Error()) > 400 {
		t.Errorf("error message too long (%d bytes), want a capped excerpt", len(err.Error()))
	}
	if strings.ContainsAny(err.Error(), "\n\t\x00") {
		t.Errorf("error message must not contain control characters: %q", err.Error())
	}
}

// TestDiscoverAuthRequirement_RejectsBlockedMetadataURL asserts a challenge
// cannot steer the server at internal infrastructure: a protected-resource
// metadata URL in a blocked range is rejected before any request, and the
// rejection surfaces through the registry's ErrOutboundURLNotAllowed sentinel
// (mapped to a 400 by the handler).
func TestDiscoverAuthRequirement_RejectsBlockedMetadataURL(t *testing.T) {
	// The resource is a public IP literal, so it passes the guard without a DNS
	// lookup; only the challenge-supplied URL is rejected.
	ch := authChallenge{ResourceMetadataURL: "http://169.254.169.254/latest/meta-data"}
	_, err := discoverAuthRequirement(context.Background(), "https://1.1.1.1/mcp", ch)
	if err == nil {
		t.Fatal("expected the blocked metadata URL to be rejected")
	}
	if !errors.Is(err, ErrOutboundURLNotAllowed) {
		t.Errorf("err = %v, want ErrOutboundURLNotAllowed", err)
	}
}

// --- authorize URL ---

func TestBuildAuthorizeURL_CarriesResourceAndPKCE(t *testing.T) {
	req := &authRequirement{
		AuthorizationEndpoint: "https://auth.test/authorize",
		Resource:              "https://mcp.test/mcp",
		Scopes:                []string{"mcp:connect", "offline_access"},
	}
	got, err := buildAuthorizeURL(req, &oauthClientIdentity{ClientID: "cid"}, "https://api.test/cb", "state-1", "challenge-1")
	if err != nil {
		t.Fatalf("buildAuthorizeURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("not a URL: %v", err)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"response_type":         "code",
		"client_id":             "cid",
		"redirect_uri":          "https://api.test/cb",
		"state":                 "state-1",
		"code_challenge":        "challenge-1",
		"code_challenge_method": "S256",
		"scope":                 "mcp:connect offline_access",
		// RFC 8707: without this the authorization server may mint a token for
		// a different resource.
		"resource": "https://mcp.test/mcp",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}
}

// --- token endpoint ---

func TestExchangeCode_PublicClient(t *testing.T) {
	withTestOAuthHTTP(t)

	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parsing form: %v", err)
		}
		gotForm = r.PostForm
		if _, _, ok := r.BasicAuth(); ok {
			t.Error("public client sent basic auth")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-1",
			"refresh_token": "rt-1",
			"token_type":    "Bearer",
			"scope":         "mcp:connect",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	req := &authRequirement{TokenEndpoint: srv.URL, Resource: "https://mcp.test/mcp"}
	tokens, err := exchangeCode(context.Background(), req, &oauthClientIdentity{ClientID: "cid"}, "code-1", "verifier-1", "https://api.test/cb")
	if err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}

	assertForm(t, gotForm, map[string]string{
		"grant_type":    "authorization_code",
		"code":          "code-1",
		"code_verifier": "verifier-1",
		"redirect_uri":  "https://api.test/cb",
		"client_id":     "cid",
		"resource":      "https://mcp.test/mcp",
	})
	if tokens.AccessToken != "at-1" || tokens.RefreshToken != "rt-1" || tokens.Scope != "mcp:connect" {
		t.Errorf("tokens = %+v", tokens)
	}
	if d := time.Until(tokens.ExpiresAt); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("ExpiresAt is %v from now, want ~1h", d)
	}
}

func TestExchangeCode_ConfidentialClientUsesBasicAuth(t *testing.T) {
	withTestOAuthHTTP(t)

	var gotForm url.Values
	var basicUser, basicPass string
	var basicOK bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		basicUser, basicPass, basicOK = r.BasicAuth()
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-1", "expires_in": 60})
	}))
	defer srv.Close()

	req := &authRequirement{TokenEndpoint: srv.URL}
	identity := &oauthClientIdentity{ClientID: "cid", ClientSecret: "sec"}
	if _, err := exchangeCode(context.Background(), req, identity, "code-1", "v", "https://api.test/cb"); err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}
	if !basicOK || basicUser != "cid" || basicPass != "sec" {
		t.Errorf("basic auth = (%q,%q,%v), want (cid,sec,true)", basicUser, basicPass, basicOK)
	}
	if gotForm.Get("client_id") != "" {
		t.Errorf("confidential client also sent client_id in the body: %q", gotForm.Get("client_id"))
	}
}

func TestExchangeCode_ProviderError(t *testing.T) {
	withTestOAuthHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"code expired"}`))
	}))
	defer srv.Close()

	req := &authRequirement{TokenEndpoint: srv.URL}
	_, err := exchangeCode(context.Background(), req, &oauthClientIdentity{ClientID: "cid"}, "code-1", "v", "https://api.test/cb")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("err = %v, want it to name invalid_grant", err)
	}
}

func TestRefreshAccessToken_CarriesResource(t *testing.T) {
	withTestOAuthHTTP(t)

	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-2", "expires_in": 900})
	}))
	defer srv.Close()

	tokens, err := refreshAccessToken(context.Background(), srv.URL, &oauthClientIdentity{ClientID: "cid"}, "rt-1", "https://mcp.test/mcp")
	if err != nil {
		t.Fatalf("refreshAccessToken: %v", err)
	}
	assertForm(t, gotForm, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": "rt-1",
		"resource":      "https://mcp.test/mcp",
		"client_id":     "cid",
	})
	if tokens.AccessToken != "at-2" {
		t.Errorf("AccessToken = %q", tokens.AccessToken)
	}
}

// --- PKCE ---

func TestCodeChallengeS256_MatchesRFC7636Example(t *testing.T) {
	// RFC 7636 appendix B.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := codeChallengeS256(verifier); got != want {
		t.Errorf("codeChallengeS256 = %q, want %q", got, want)
	}
}

func TestNewOAuthStateAndVerifierAreUnpredictable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		s, err := newOAuthState()
		if err != nil {
			t.Fatalf("newOAuthState: %v", err)
		}
		v, err := newCodeVerifier()
		if err != nil {
			t.Fatalf("newCodeVerifier: %v", err)
		}
		if len(s) < 40 || len(v) < 40 {
			t.Fatalf("state/verifier too short: %d/%d", len(s), len(v))
		}
		if seen[s] || seen[v] {
			t.Fatal("random value repeated")
		}
		seen[s], seen[v] = true, true
	}
}

// --- expiry ---

func TestOAuthTokenExpired(t *testing.T) {
	if oauthTokenExpired(nil) {
		t.Error("nil expiry must never be treated as expired")
	}
	future := time.Now().UTC().Add(time.Hour)
	if oauthTokenExpired(&future) {
		t.Error("an hour-away expiry must not be expired")
	}
	// Inside the refresh skew: treat as expired so a token is never used past
	// its lifetime mid-call.
	soon := time.Now().UTC().Add(5 * time.Second)
	if !oauthTokenExpired(&soon) {
		t.Error("an expiry inside the skew must be treated as expired")
	}
	past := time.Now().UTC().Add(-time.Minute)
	if !oauthTokenExpired(&past) {
		t.Error("a past expiry must be expired")
	}
}

// --- device config ---

func TestWithDeviceAuthorization(t *testing.T) {
	// A statically configured Authorization header must not survive alongside
	// the OAuth bearer the server resolved.
	got := withDeviceAuthorization(map[string]any{
		"Authorization": "Bearer stale-static",
		"X-Extra":       "1",
	}, "Bearer fresh-oauth")

	if got["Authorization"] != "Bearer fresh-oauth" {
		t.Errorf("Authorization = %v, want the resolved OAuth bearer", got["Authorization"])
	}
	if got["X-Extra"] != "1" {
		t.Errorf("unrelated header dropped: %v", got)
	}

	// A nil header set still yields an explicit one-entry map.
	only := withDeviceAuthorization(nil, "Bearer t")
	if len(only) != 1 || only["Authorization"] != "Bearer t" {
		t.Errorf("withDeviceAuthorization(nil) = %v", only)
	}
}

// --- helpers ---

func assertForm(t *testing.T, form url.Values, want map[string]string) {
	t.Helper()
	for key, expected := range want {
		if got := form.Get(key); got != expected {
			t.Errorf("form %s = %q, want %q", key, got, expected)
		}
	}
}
