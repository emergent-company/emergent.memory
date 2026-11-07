package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newOIDCFallbackServer builds a GitHub-like discovery surface: protected
// resource metadata names a path-bearing authorization server (issuer
// <host>/login/oauth) whose RFC 8414 well-known paths all 404, while its OpenID
// Connect discovery document — at the append path only — returns 200. This is
// the exact shape of GitHub's remote MCP server (issue #1792).
func newOIDCFallbackServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

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
	// Only the OIDC append location serves a document; every other well-known
	// location falls through to the mux's default 404.
	mux.HandleFunc("/login/oauth/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 issuer,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
		})
	})

	return srv
}

func TestDiscoverAuthRequirement_OIDCFallback(t *testing.T) {
	withTestOAuthHTTP(t)
	srv := newOIDCFallbackServer(t)

	ch := authChallenge{ResourceMetadataURL: srv.URL + "/.well-known/oauth-protected-resource"}
	req, err := discoverAuthRequirement(context.Background(), srv.URL+"/mcp", ch)
	if err != nil {
		t.Fatalf("discoverAuthRequirement: %v", err)
	}
	if req.AuthorizationServer != srv.URL+"/login/oauth" {
		t.Errorf("AuthorizationServer = %q, want the path-bearing issuer", req.AuthorizationServer)
	}
	if req.AuthorizationEndpoint != srv.URL+"/authorize" {
		t.Errorf("AuthorizationEndpoint = %q, want %q (from the OIDC document)", req.AuthorizationEndpoint, srv.URL+"/authorize")
	}
	if req.TokenEndpoint != srv.URL+"/token" {
		t.Errorf("TokenEndpoint = %q, want %q (from the OIDC document)", req.TokenEndpoint, srv.URL+"/token")
	}
	if req.RegistrationSupported {
		t.Error("RegistrationSupported = true, want false (the OIDC document has no registration_endpoint)")
	}
}

func TestDiscoverServerAuthRequirement_NoMetadataAnywhere(t *testing.T) {
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
	// No well-known document is served: every candidate 404s.

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(srv.URL + "/mcp")}
	_, err := discoverServerAuthRequirement(context.Background(), server)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrOAuthMetadataUnavailable) {
		t.Errorf("err = %v, want ErrOAuthMetadataUnavailable", err)
	}
	if errors.Is(err, ErrOAuthDiscoveryFailed) {
		t.Errorf("err = %v, must NOT be ErrOAuthDiscoveryFailed (permanent absence, not transient)", err)
	}
}

func TestDiscoverServerAuthRequirement_TransientFailureWinsOverAbsence(t *testing.T) {
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
	// RFC 8414 candidates 404 (absent), but the OIDC append location is a 500:
	// a transient upstream failure must surface as such, not as "no metadata".
	mux.HandleFunc("/login/oauth/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(srv.URL + "/mcp")}
	_, err := discoverServerAuthRequirement(context.Background(), server)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrOAuthDiscoveryFailed) {
		t.Errorf("err = %v, want ErrOAuthDiscoveryFailed (transient 500)", err)
	}
	if errors.Is(err, ErrOAuthMetadataUnavailable) {
		t.Errorf("err = %v, must NOT be ErrOAuthMetadataUnavailable", err)
	}
}

// TestDiscoverServerAuthRequirement_PRMWellKnown5xxIsDiscoveryFailure pins the
// registry-visible consequence of the PRM 5xx rule. An authorization_uri-only
// challenge is the registry-reachable route into the well-known
// protected-resource fallback; a 5xx there must surface as a discovery failure
// (the 503 class via ErrOAuthDiscoveryFailed), never degrade silently into the
// authorization-server walk and report success.
func TestDiscoverServerAuthRequirement_PRMWellKnown5xxIsDiscoveryFailure(t *testing.T) {
	withTestOAuthHTTP(t)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer authorization_uri="`+srv.URL+`/.well-known/oauth-authorization-server"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	// The optional PRM document is an upstream outage, not absence. Pre-change
	// code swallowed this and fell through to the authorization-server walk.
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	// A usable authorization-server document exists behind the challenge, so
	// the pre-change silent fallthrough would otherwise complete successfully.
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
		})
	})

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(srv.URL + "/mcp")}
	req, err := discoverServerAuthRequirement(context.Background(), server)
	if err == nil {
		t.Fatalf("discoverServerAuthRequirement() = %+v, error = nil, want a discovery failure", req)
	}
	if !errors.Is(err, ErrOAuthDiscoveryFailed) {
		t.Errorf("err = %v, want ErrOAuthDiscoveryFailed (PRM 5xx is the 503 class)", err)
	}
	if errors.Is(err, ErrOAuthMetadataUnavailable) {
		t.Errorf("err = %v, must NOT be ErrOAuthMetadataUnavailable (a 5xx is not absence)", err)
	}
}

func TestDiscoverServerAuthRequirement_401MetadataIsNotAbsence(t *testing.T) {
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
	// The OIDC document answers 403 (not 404/410): a status code we can observe
	// because the metadata client is unwrapped. It must surface as a discovery
	// failure, never as a definitive absence.
	mux.HandleFunc("/login/oauth/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(srv.URL + "/mcp")}
	_, err := discoverServerAuthRequirement(context.Background(), server)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrOAuthMetadataUnavailable) {
		t.Errorf("err = %v, a 403 is not a definitive absence", err)
	}
	if !errors.Is(err, ErrOAuthDiscoveryFailed) {
		t.Errorf("err = %v, want ErrOAuthDiscoveryFailed for a 403", err)
	}
}
