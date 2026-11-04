package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// newTestAS spins up a fake protected-resource + authorization server that
// serves the full dynamic-client bootstrap: resource probe (401 Bearer
// challenge), protected-resource metadata, authorization-server metadata, DCR,
// and the token endpoint. The token handler is customizable via tokenBody.
//
// The fake AS models RFC 8414 / Supabase/Trale behaviour: the issuer is
// <base>/auth/v1, the bare issuer path answers 401, and the metadata document is
// served at the path-insertion URL <base>/.well-known/oauth-authorization-server/auth/v1.
func newTestAS(t *testing.T, tokenStatus int, tokenBody func() map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverBase(r)+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})

	mux.HandleFunc("/prm", writePRM)

	registerRFC8414AS(mux, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["token_endpoint_auth_method"] != "none" {
			t.Errorf("register token_endpoint_auth_method = %v, want none", req["token_endpoint_auth_method"])
		}
		if req["client_name"] != "Memory" {
			t.Errorf("register client_name = %v, want %q", req["client_name"], "Memory")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-123"})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(tokenStatus)
		_ = json.NewEncoder(w).Encode(tokenBody())
	})

	return httptest.NewServer(mux)
}

func serverBase(r *http.Request) string {
	return "http://" + r.Host
}

// writePRM encodes protected-resource metadata advertising the RFC 8414 issuer
// <base>/auth/v1 — exactly what Trale's live document advertises.
func writePRM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"resource":              serverBase(r) + "/api/mcp",
		"authorization_servers": []string{serverBase(r) + "/auth/v1"},
	})
}

// writeASMetadata serves the authorization-server metadata document naming the
// authorization/token/registration endpoints.
func writeASMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                 serverBase(r) + "/auth/v1",
		"authorization_endpoint": serverBase(r) + "/auth",
		"token_endpoint":         serverBase(r) + "/token",
		"registration_endpoint":  serverBase(r) + "/register",
	})
}

// registerRFC8414AS wires the fake authorization server onto mux the way
// Supabase/Trale actually behave: the bare issuer path <base>/auth/v1 answers
// 401 (the production bug), and the metadata document lives at the RFC 8414 §3.1
// path-insertion URL <base>/.well-known/oauth-authorization-server/auth/v1. reg
// handles the DCR registration request (each test supplies its own response).
func registerRFC8414AS(mux *http.ServeMux, reg http.HandlerFunc) {
	mux.HandleFunc("/auth/v1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server/auth/v1", writeASMetadata)
	mux.HandleFunc("/register", reg)
}

func TestDiscoverAndRegister_FullFlow(t *testing.T) {
	srv := newTestAS(t, http.StatusOK, func() map[string]any {
		return map[string]any{"access_token": "at", "expires_in": 3600}
	})
	defer srv.Close()

	c, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Memory",
		Scopes:      []string{"openid", "email", "profile"},
		RedirectURI: "https://app.example/callback",
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("DiscoverAndRegister() error = %v", err)
	}
	if c.ClientID != "client-123" {
		t.Errorf("ClientID = %q, want client-123", c.ClientID)
	}
	if c.AuthURL != srv.URL+"/auth" {
		t.Errorf("AuthURL = %q, want %q", c.AuthURL, srv.URL+"/auth")
	}
	if c.TokenURL != srv.URL+"/token" {
		t.Errorf("TokenURL = %q, want %q", c.TokenURL, srv.URL+"/token")
	}
	if c.Resource != srv.URL+"/api/mcp" {
		t.Errorf("Resource = %q, want %q", c.Resource, srv.URL+"/api/mcp")
	}
}

// TestDiscoverAndRegister_WellKnownFallback asserts the flow falls back to the
// well-known protected-resource metadata endpoint when the resource does not
// return a 401 Bearer challenge.
func TestDiscoverAndRegister_WellKnownFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // no challenge
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", writePRM)
	registerRFC8414AS(mux, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-wk"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Memory",
		Scopes:      []string{"openid"},
		RedirectURI: "https://app.example/callback",
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("DiscoverAndRegister() error = %v", err)
	}
	if c.ClientID != "client-wk" {
		t.Errorf("ClientID = %q, want client-wk", c.ClientID)
	}
}

func TestDiscoverAndRegister_DCRError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverBase(r)+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/prm", writePRM)
	registerRFC8414AS(mux, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_redirect_uri", "error_description": "uri not allowed"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Memory",
		Scopes:      []string{"openid"},
		RedirectURI: "https://app.example/callback",
		HTTPClient:  srv.Client(),
	})
	if err == nil {
		t.Fatal("DiscoverAndRegister() error = nil, want DCR error")
	}
	if !strings.Contains(err.Error(), "invalid_redirect_uri") {
		t.Errorf("DiscoverAndRegister() error = %v, want invalid_redirect_uri detail", err)
	}
}

// TestDiscoverAndRegister_AllMetadataEndpointsFail asserts that when every
// discovery candidate fails, the error names the attempted endpoints and their
// statuses, and no DCR attempt is made.
func TestDiscoverAndRegister_AllMetadataEndpointsFail(t *testing.T) {
	var registered atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverBase(r)+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/prm", writePRM)
	// The bare issuer 401s (Supabase); no metadata is served at any candidate.
	mux.HandleFunc("/auth/v1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		registered.Store(true)
		w.WriteHeader(http.StatusCreated)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Memory",
		Scopes:      []string{"openid"},
		RedirectURI: "https://app.example/callback",
		HTTPClient:  srv.Client(),
	})
	if err == nil {
		t.Fatal("DiscoverAndRegister() error = nil, want AS metadata error")
	}
	if !strings.Contains(err.Error(), "authorization-server metadata") {
		t.Errorf("DiscoverAndRegister() error = %v, want authorization-server metadata prefix", err)
	}
	// At least two candidate URLs and their statuses must be named.
	for _, want := range []string{srv.URL + "/.well-known/oauth-authorization-server/auth/v1", srv.URL + "/auth/v1/.well-known/openid-configuration", " -> 404"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("DiscoverAndRegister() error = %v, want it to name %q", err, want)
		}
	}
	if registered.Load() {
		t.Error("DCR was attempted despite all discovery endpoints failing")
	}
}

// TestDiscoverAndRegister_RFC8414PathInsertion is the regression test for the
// Trale production bug: the PRM's authorization_servers[0] is an ISSUER
// identifier (<base>/auth/v1), the bare issuer path answers 401 (Supabase), and
// the metadata document is served only at the RFC 8414 §3.1 path-insertion URL
// <base>/.well-known/oauth-authorization-server/auth/v1. Against the old code
// (which fetched the issuer directly) this fails with a 401.
func TestDiscoverAndRegister_RFC8414PathInsertion(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverBase(r)+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/prm", writePRM)
	registerRFC8414AS(mux, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-rfc8414"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Memory",
		Scopes:      []string{"openid"},
		RedirectURI: "https://app.example/callback",
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("DiscoverAndRegister() error = %v", err)
	}
	if c.ClientID != "client-rfc8414" {
		t.Errorf("ClientID = %q, want client-rfc8414", c.ClientID)
	}
	if c.AuthURL != srv.URL+"/auth" {
		t.Errorf("AuthURL = %q, want %q", c.AuthURL, srv.URL+"/auth")
	}
	if c.TokenURL != srv.URL+"/token" {
		t.Errorf("TokenURL = %q, want %q", c.TokenURL, srv.URL+"/token")
	}
}

// TestDiscoverAndRegister_OIDCFallback asserts discovery when the metadata is
// served only at the OpenID location <base>/<path>/.well-known/openid-configuration.
func TestDiscoverAndRegister_OIDCFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverBase(r)+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/prm", writePRM)
	mux.HandleFunc("/auth/v1/.well-known/openid-configuration", writeASMetadata)
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-oidc"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Memory",
		Scopes:      []string{"openid"},
		RedirectURI: "https://app.example/callback",
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("DiscoverAndRegister() error = %v", err)
	}
	if c.ClientID != "client-oidc" {
		t.Errorf("ClientID = %q, want client-oidc", c.ClientID)
	}
}

// TestDiscoverAndRegister_OIDCAlternate asserts discovery when the metadata is
// served only at the OpenID path-suffix variant <base>/.well-known/openid-configuration/<path>.
func TestDiscoverAndRegister_OIDCAlternate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverBase(r)+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/prm", writePRM)
	mux.HandleFunc("/.well-known/openid-configuration/auth/v1", writeASMetadata)
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-oidc-alt"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Memory",
		Scopes:      []string{"openid"},
		RedirectURI: "https://app.example/callback",
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("DiscoverAndRegister() error = %v", err)
	}
	if c.ClientID != "client-oidc-alt" {
		t.Errorf("ClientID = %q, want client-oidc-alt", c.ClientID)
	}
}

// TestDiscoverAndRegister_IssuerWithoutPath asserts discovery when the PRM
// advertises an origin-only issuer (authorization_servers: [base]): the
// metadata is discovered at <base>/.well-known/oauth-authorization-server.
func TestDiscoverAndRegister_IssuerWithoutPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverBase(r)+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/prm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              serverBase(r) + "/api/mcp",
			"authorization_servers": []string{serverBase(r)},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", writeASMetadata)
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-origin"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Memory",
		Scopes:      []string{"openid"},
		RedirectURI: "https://app.example/callback",
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("DiscoverAndRegister() error = %v", err)
	}
	if c.ClientID != "client-origin" {
		t.Errorf("ClientID = %q, want client-origin", c.ClientID)
	}
}

// TestAsMetadataCandidates pins the candidate derivation rules directly.
func TestAuthMetadataCandidates(t *testing.T) {
	issuerWithPath := "https://auth.example/auth/v1"
	// RFC 8414 §3.1 and OpenID Discovery §4 both define the path-INSERTED
	// location as canonical, so each document family's inserted form precedes
	// its appended form.
	pathCandidates := []string{
		"https://auth.example/.well-known/oauth-authorization-server/auth/v1",
		"https://auth.example/auth/v1/.well-known/oauth-authorization-server",
		"https://auth.example/.well-known/openid-configuration/auth/v1",
		"https://auth.example/auth/v1/.well-known/openid-configuration",
	}

	cases := []struct {
		name    string
		issuer  string
		want    []string
		notBare string
	}{
		{
			name:    "issuer with path order",
			issuer:  issuerWithPath,
			want:    pathCandidates,
			notBare: issuerWithPath,
		},
		{
			name:   "origin only",
			issuer: "https://auth.example",
			want: []string{
				"https://auth.example/.well-known/oauth-authorization-server",
				"https://auth.example/.well-known/openid-configuration",
			},
			notBare: "https://auth.example",
		},
		{
			name:   "already a metadata URL",
			issuer: "https://auth.example/.well-known/oauth-authorization-server/auth/v1",
			want:   []string{"https://auth.example/.well-known/oauth-authorization-server/auth/v1"},
		},
		{
			name:    "trailing slash on path",
			issuer:  "https://auth.example/auth/v1/",
			want:    pathCandidates,
			notBare: "https://auth.example/auth/v1/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := authMetadataCandidates(tc.issuer, "")
			if len(got) != len(tc.want) {
				t.Fatalf("authMetadataCandidates(%q) = %v, want %v", tc.issuer, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("authMetadataCandidates(%q)[%d] = %q, want %q (full: %v)", tc.issuer, i, got[i], tc.want[i], got)
				}
			}
			if tc.notBare != "" {
				for _, c := range got {
					if c == tc.notBare {
						t.Errorf("authMetadataCandidates(%q) returned the bare issuer %q", tc.issuer, c)
					}
				}
			}
		})
	}

	// The bare issuer must never be a candidate even when it has no path.
	if got := authMetadataCandidates("https://auth.example", ""); contains(got, "https://auth.example") {
		t.Errorf("authMetadataCandidates returned the bare origin as a candidate: %v", got)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestDiscoverAndRegister_RejectsInternalResource(t *testing.T) {
	_, err := DiscoverAndRegister(context.Background(), Config{
		Resource:   "http://127.0.0.1/api/mcp",
		ClientName: "Memory",
	})
	if err == nil {
		t.Fatal("DiscoverAndRegister() error = nil, want SSRF rejection")
	}
	if !strings.Contains(err.Error(), "resource URL rejected") {
		t.Errorf("DiscoverAndRegister() error = %v, want resource URL rejected", err)
	}
}

func TestClient_Exchange_NoSecretStoresResource(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at",
			"refresh_token": "rt",
			"expires_in":    3600,
			"scope":         "openid email profile",
			"token_type":    "Bearer",
		})
	}))
	defer srv.Close()

	c := NewClient("client-123", srv.URL, "https://app.trale.ai/api/mcp", srv.Client())
	tok, err := c.Exchange(context.Background(), "code-1", "verifier-1", "https://app.example/callback")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if tok.AccessToken != "at" || tok.RefreshToken != "rt" {
		t.Errorf("Exchange() tokens = %+v, want at/rt", tok)
	}
	if gotForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q, want authorization_code", gotForm.Get("grant_type"))
	}
	if gotForm.Get("client_id") != "client-123" {
		t.Errorf("client_id = %q, want client-123", gotForm.Get("client_id"))
	}
	if gotForm.Get("code_verifier") != "verifier-1" {
		t.Errorf("code_verifier = %q, want verifier-1", gotForm.Get("code_verifier"))
	}
	if gotForm.Get("resource") != "https://app.trale.ai/api/mcp" {
		t.Errorf("resource = %q, want the protected resource", gotForm.Get("resource"))
	}
	if gotForm.Get("client_secret") != "" {
		t.Errorf("client_secret = %q, want empty (public client)", gotForm.Get("client_secret"))
	}
}

func TestClient_Refresh_Rotates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", r.PostForm.Get("grant_type"))
		}
		if r.PostForm.Get("refresh_token") != "rt-old" {
			t.Errorf("refresh_token = %q, want rt-old", r.PostForm.Get("refresh_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-new",
			"refresh_token": "rt-new",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	c := NewClient("client-123", srv.URL, "https://app.trale.ai/api/mcp", srv.Client())
	tok, err := c.Refresh(context.Background(), "rt-old")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if tok.AccessToken != "at-new" || tok.RefreshToken != "rt-new" {
		t.Errorf("Refresh() = %+v, want at-new/rt-new", tok)
	}
}

func TestClient_Exchange_TokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "code already used"})
	}))
	defer srv.Close()

	c := NewClient("client-123", srv.URL, "https://app.trale.ai/api/mcp", srv.Client())
	_, err := c.Exchange(context.Background(), "code-1", "verifier-1", "https://app.example/callback")
	if err == nil {
		t.Fatal("Exchange() error = nil, want token error")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("Exchange() error = %v, want invalid_grant detail", err)
	}
}

func TestParseBearerChallenge(t *testing.T) {
	cases := []struct {
		in   string
		want Challenge
		ok   bool
	}{
		{`Bearer resource_metadata="https://example.com/prm"`, Challenge{ResourceMetadataURL: "https://example.com/prm"}, true},
		{`Bearer resource_metadata="https://example.com/prm", error="invalid_token"`, Challenge{ResourceMetadataURL: "https://example.com/prm"}, true},
		{`bearer resource_metadata=https://example.com/prm, scope="mcp:connect"`, Challenge{ResourceMetadataURL: "https://example.com/prm", Scope: "mcp:connect"}, true},
		{`Bearer authorization_uri="https://as.example/.well-known/oauth-authorization-server"`, Challenge{AuthorizationURI: "https://as.example/.well-known/oauth-authorization-server"}, true},
		{`Bearer error="invalid_token"`, Challenge{}, false},
		{`Basic realm="x"`, Challenge{}, false},
	}
	for _, tc := range cases {
		got, ok := ParseBearerChallenge(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseBearerChallenge(%q) = (%+v, %v), want (%+v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBlockedAddr(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.1.1", "0.0.0.0", "100.64.0.1", "240.0.0.1", "::1", "fc00::1"}
	allowed := []string{"8.8.8.8", "1.1.1.1", "142.250.72.14", "2606:4700:4700::1111"}
	for _, s := range blocked {
		if !blockedAddr(netip.MustParseAddr(s)) {
			t.Errorf("blockedAddr(%s) = false, want true", s)
		}
	}
	for _, s := range allowed {
		if blockedAddr(netip.MustParseAddr(s)) {
			t.Errorf("blockedAddr(%s) = true, want false", s)
		}
	}
}

func TestValidateURL(t *testing.T) {
	if err := validateURL("https://app.trale.ai/api/mcp"); err != nil {
		t.Errorf("validateURL(public https) = %v, want nil", err)
	}
	rejected := []string{
		"http://127.0.0.1/api/mcp",
		"http://10.0.0.5/x",
		"http://localhost/x",
		"http://metadata.google.internal/x",
		"ftp://example.com/x",
		"http://user:pass@example.com/x",
		"",
	}
	for _, u := range rejected {
		if err := validateURL(u); err == nil {
			t.Errorf("validateURL(%q) = nil, want error", u)
		}
	}
}
