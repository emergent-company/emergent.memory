package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// newTraleAS spins up a fake Trale protected-resource + authorization server on
// loopback: resource probe (401 Bearer challenge), PRM, AS metadata, DCR, and
// the token endpoint. It lets the dynamic OAuth flow run end-to-end without
// reaching Trale.
func newTraleAS(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://`+r.Host+`/prm"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/prm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              "http://" + r.Host + "/api/mcp",
			"authorization_servers": []string{"http://" + r.Host + "/auth/v1"},
		})
	})
	// The bare issuer path 401s (Supabase behaviour); the metadata document is
	// served at the RFC 8414 §3.1 path-insertion URL.
	mux.HandleFunc("/auth/v1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server/auth/v1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 "http://" + r.Host + "/auth/v1",
			"authorization_endpoint": "http://" + r.Host + "/auth",
			"token_endpoint":         "http://" + r.Host + "/token",
			"registration_endpoint":  "http://" + r.Host + "/register",
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-123"})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-dyn",
			"refresh_token": "rt-dyn",
			"expires_in":    3600,
			"scope":         "openid email profile",
			"token_type":    "Bearer",
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestTraleOAuth_DynamicFlow asserts the full self-registering OAuth flow: Start
// discovers + registers a public client and returns an authorize URL naming the
// registered client and resource; Complete exchanges the code, stores the token
// set and client material (no secret), and strips the reserved flow key.
func TestTraleOAuth_DynamicFlow(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "traleoauthflow")
	t.Cleanup(tdb.Close)

	srv := newTraleAS(t)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{
		LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)},
		SourceOAuth: config.SourceOAuthConfig{RedirectBaseURL: "https://app.example"},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(tdb.DB, log)

	p := NewTraleProvider(srv.Client()).(*traleProvider)
	p.resource = srv.URL + "/api/mcp"
	svc := NewService(repo, cfg, log, NewRegistry(p), WithOAuthHTTPClient(srv.Client()))

	projectID := uuid.New()
	uid := uuid.New()
	start, err := svc.StartOAuth(ctx, projectID, ProviderTypeTrale, StartOAuthRequest{
		UserID: &uid,
		Config: map[string]any{"tags": []string{"standup"}},
	})
	if err != nil {
		t.Fatalf("StartOAuth() error = %v", err)
	}

	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != "client-123" {
		t.Errorf("authorize client_id = %q, want client-123 (the registered client)", q.Get("client_id"))
	}
	if q.Get("resource") != srv.URL+"/api/mcp" {
		t.Errorf("authorize resource = %q, want %q", q.Get("resource"), srv.URL+"/api/mcp")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("authorize code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("authorize code_challenge is empty")
	}
	state := q.Get("state")
	if state == "" {
		t.Fatal("authorize state is empty")
	}

	completion, err := svc.CompleteOAuth(ctx, ProviderTypeTrale, state, "code-1")
	if err != nil {
		t.Fatalf("CompleteOAuth() error = %v", err)
	}
	integ := completion.Integration
	if completion.Reconnected {
		t.Error("a first connect must not report a reconnect")
	}

	// The returned integration carries the non-secret config: tags preserved,
	// reserved flow key stripped, no credential material.
	if _, ok := integ.Config["tags"]; !ok {
		t.Errorf("integration.Config missing tags: %+v", integ.Config)
	}
	if _, present := integ.Config[oauthDynamicKey]; present {
		t.Errorf("integration.Config still has reserved %q: %+v", oauthDynamicKey, integ.Config)
	}
	if _, present := integ.Config["client_secret"]; present {
		t.Errorf("integration.Config has client_secret: %+v", integ.Config)
	}

	// The granted scope set is recorded verbatim so the read can report the
	// grant: Trale declares no selectable scope strategies, so there is no
	// granted strategy to map and the scopes are the only grant detail there is.
	if got := stringSlice(integ.Config[oauthGrantedScopesKey]); len(got) != 3 || got[0] != "openid" || got[1] != "email" || got[2] != "profile" {
		t.Errorf("integration.Config[%s] = %#v, want [openid email profile]", oauthGrantedScopesKey, integ.Config[oauthGrantedScopesKey])
	}
	if _, present := integ.Config[oauthGrantedStrategyKey]; present {
		t.Errorf("integration.Config has %s for a strategy-less provider: %+v", oauthGrantedStrategyKey, integ.Config)
	}

	// Inspect the stored credential blob: token set + client material present,
	// no client secret (Trale registers a public client).
	src, err := repo.GetByIDWithCredentials(ctx, uuid.MustParse(integ.ID), projectID)
	if err != nil {
		t.Fatalf("GetByIDWithCredentials() error = %v", err)
	}
	creds, err := svc.decryptCredentials(src.EncryptedCredentials)
	if err != nil {
		t.Fatalf("decryptCredentials() error = %v", err)
	}
	for _, want := range []string{"access_token", "refresh_token", "client_id", "token_endpoint", "resource"} {
		if v, _ := creds[want].(string); v == "" {
			t.Errorf("stored credential %q empty: %+v", want, creds)
		}
	}
	if v, _ := creds["access_token"].(string); v != "at-dyn" {
		t.Errorf("stored access_token = %q, want at-dyn", v)
	}
	if _, present := creds["client_secret"]; present {
		t.Errorf("stored credential has client_secret: %+v", creds)
	}
}

// TestTraleOAuthStrategy_AuthorizeURL asserts the authorize URL names the
// client, the resource (RFC 8707), and PKCE S256.
func TestTraleOAuthStrategy_AuthorizeURL(t *testing.T) {
	s := newTraleOAuthStrategy()
	u := s.AuthorizeURL(OAuthClient{
		ClientID: "client-9",
		AuthURL:  "https://auth.example/authorize",
		Scopes:   []string{"openid"},
	}, OAuthAuthorizeRequest{
		State:         "st",
		CodeChallenge: "ch",
		RedirectURI:   "https://app.example/callback",
	})
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	q := parsed.Query()
	if q.Get("client_id") != "client-9" {
		t.Errorf("client_id = %q, want client-9", q.Get("client_id"))
	}
	if q.Get("resource") != traleResource {
		t.Errorf("resource = %q, want %q", q.Get("resource"), traleResource)
	}
	if q.Get("code_challenge") != "ch" || q.Get("code_challenge_method") != "S256" {
		t.Errorf("PKCE = %q/%q, want ch/S256", q.Get("code_challenge"), q.Get("code_challenge_method"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
}

// TestTraleOAuthStrategy_Exchange_NoSecret asserts the exchange posts no client
// secret and stores the token set plus client/resource material.
func TestTraleOAuthStrategy_Exchange_NoSecret(t *testing.T) {
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
		})
	}))
	defer srv.Close()

	s := newTraleOAuthStrategy()
	creds, err := s.Exchange(context.Background(), srv.Client(), OAuthClient{
		ClientID: "client-9",
		TokenURL: srv.URL,
		Resource: traleResource,
	}, "code-1", "verifier-1", "https://app.example/callback")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}

	if gotForm.Get("client_secret") != "" {
		t.Errorf("client_secret = %q, want empty (public client)", gotForm.Get("client_secret"))
	}
	if gotForm.Get("resource") != traleResource {
		t.Errorf("resource = %q, want %q", gotForm.Get("resource"), traleResource)
	}

	if creds["access_token"] != "at" || creds["refresh_token"] != "rt" {
		t.Errorf("creds tokens = %v, want at/rt", creds)
	}
	if creds["client_id"] != "client-9" {
		t.Errorf("creds client_id = %v, want client-9", creds["client_id"])
	}
	if creds["token_endpoint"] != srv.URL {
		t.Errorf("creds token_endpoint = %v, want %q", creds["token_endpoint"], srv.URL)
	}
	if creds["resource"] != traleResource {
		t.Errorf("creds resource = %v, want %q", creds["resource"], traleResource)
	}
	if _, ok := creds["expires_at"]; !ok {
		t.Errorf("creds missing expires_at: %v", creds)
	}
	if _, ok := creds["client_secret"]; ok {
		t.Errorf("creds has client_secret: %v", creds)
	}
}

// TestDynamicOAuthRoundTrip asserts the pending-state serialisation round-trips
// a discovered dynamic client.
func TestDynamicOAuthRoundTrip(t *testing.T) {
	dc := DynamicOAuthClient{
		ClientID: "client-9",
		AuthURL:  "https://auth.example/authorize",
		TokenURL: "https://auth.example/token",
		Resource: "https://app.trale.ai/api/mcp",
		Scopes:   []string{"openid", "email"},
	}
	cfg := dynamicOAuthToConfig(dc)
	got, ok := dynamicOAuthFromConfig(cfg)
	if !ok {
		t.Fatal("dynamicOAuthFromConfig() = ok=false, want true")
	}
	if got.ClientID != dc.ClientID || got.AuthURL != dc.AuthURL || got.TokenURL != dc.TokenURL || got.Resource != dc.Resource {
		t.Errorf("round-trip = %+v, want %+v", got, dc)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "openid" {
		t.Errorf("round-trip scopes = %v, want [openid email]", got.Scopes)
	}

	// A value without a client_id is invalid.
	if _, ok := dynamicOAuthFromConfig(map[string]any{"token_url": "https://tok"}); ok {
		t.Error("dynamicOAuthFromConfig(no client_id) = ok=true, want false")
	}
}
