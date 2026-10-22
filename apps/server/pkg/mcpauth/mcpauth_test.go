package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

// newTestAS spins up a fake protected-resource + authorization server that
// serves the full dynamic-client bootstrap: resource probe (401 Bearer
// challenge), protected-resource metadata, authorization-server metadata, DCR,
// and the token endpoint. The token handler is customizable via tokenBody.
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

	mux.HandleFunc("/prm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              serverBase(r) + "/api/mcp",
			"authorization_servers": []string{serverBase(r) + "/as"},
		})
	})

	mux.HandleFunc("/as", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 serverBase(r),
			"authorization_endpoint": serverBase(r) + "/auth",
			"token_endpoint":         serverBase(r) + "/token",
			"registration_endpoint":  serverBase(r) + "/register",
		})
	})

	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["token_endpoint_auth_method"] != "none" {
			t.Errorf("register token_endpoint_auth_method = %v, want none", req["token_endpoint_auth_method"])
		}
		if req["client_name"] != "Emergent Memory" {
			t.Errorf("register client_name = %v, want %q", req["client_name"], "Emergent Memory")
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

func TestDiscoverAndRegister_FullFlow(t *testing.T) {
	srv := newTestAS(t, http.StatusOK, func() map[string]any {
		return map[string]any{"access_token": "at", "expires_in": 3600}
	})
	defer srv.Close()

	c, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Emergent Memory",
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
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              serverBase(r) + "/api/mcp",
			"authorization_servers": []string{serverBase(r) + "/as"},
		})
	})
	mux.HandleFunc("/as", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authorization_endpoint": serverBase(r) + "/auth",
			"token_endpoint":         serverBase(r) + "/token",
			"registration_endpoint":  serverBase(r) + "/register",
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-wk"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Emergent Memory",
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
	mux.HandleFunc("/prm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              serverBase(r) + "/api/mcp",
			"authorization_servers": []string{serverBase(r) + "/as"},
		})
	})
	mux.HandleFunc("/as", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authorization_endpoint": serverBase(r) + "/auth",
			"token_endpoint":         serverBase(r) + "/token",
			"registration_endpoint":  serverBase(r) + "/register",
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_redirect_uri", "error_description": "uri not allowed"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Emergent Memory",
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

func TestDiscoverAndRegister_ASMetadataError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+serverBase(r)+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/prm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              serverBase(r) + "/api/mcp",
			"authorization_servers": []string{serverBase(r) + "/as"},
		})
	})
	mux.HandleFunc("/as", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := DiscoverAndRegister(context.Background(), Config{
		Resource:    srv.URL + "/api/mcp",
		ClientName:  "Emergent Memory",
		Scopes:      []string{"openid"},
		RedirectURI: "https://app.example/callback",
		HTTPClient:  srv.Client(),
	})
	if err == nil {
		t.Fatal("DiscoverAndRegister() error = nil, want AS metadata error")
	}
}

func TestDiscoverAndRegister_RejectsInternalResource(t *testing.T) {
	_, err := DiscoverAndRegister(context.Background(), Config{
		Resource:   "http://127.0.0.1/api/mcp",
		ClientName: "Emergent Memory",
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

func TestParseResourceMetadataURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`Bearer resource_metadata="https://example.com/prm"`, "https://example.com/prm"},
		{`Bearer resource_metadata="https://example.com/prm", error="invalid_token"`, "https://example.com/prm"},
		{`Bearer error="invalid_token"`, ""},
		{`Basic realm="x"`, ""},
	}
	for _, tc := range cases {
		if got := parseResourceMetadataURL(tc.in); got != tc.want {
			t.Errorf("parseResourceMetadataURL(%q) = %q, want %q", tc.in, got, tc.want)
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
