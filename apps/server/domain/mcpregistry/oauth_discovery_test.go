package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// withGuardAllowingTestServer keeps the production outbound guard for every
// host except the httptest origin (which the guard refuses, as it should), and
// installs plain clients so the local test server is reachable without the
// SSRF-guarded transport's loopback refusal. A provider-advertised URL that
// names a blocked host is then rejected by the real guard, exactly as in
// production.
func withGuardAllowingTestServer(t *testing.T, allowedOrigin string) {
	t.Helper()
	origValidate, origMeta, origProbe := oauthValidateURL, oauthMetadataClient, oauthProbeHTTPClient
	oauthValidateURL = func(ctx context.Context, raw string) error {
		if strings.HasPrefix(raw, allowedOrigin) {
			return nil
		}
		return validateOutboundFetchURL(ctx, raw)
	}
	oauthMetadataClient = func(string) *http.Client { return &http.Client{} }
	oauthProbeHTTPClient = func(string) *http.Client { return &http.Client{} }
	t.Cleanup(func() {
		oauthValidateURL, oauthMetadataClient, oauthProbeHTTPClient = origValidate, origMeta, origProbe
	})
}

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

// assertProviderSideDiscoveryError pins the registry contract for a blocked
// URL the provider advertised: the error is the provider-side
// ErrOAuthDiscoveryFailed class (503), never the caller-facing
// ErrOutboundURLNotAllowed (400), and the caller copy is absent from the
// message the caller would see.
func assertProviderSideDiscoveryError(t *testing.T, err error, wantDetail string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a discovery error")
	}
	if errors.Is(err, ErrOutboundURLNotAllowed) {
		t.Errorf("err = %v, must not blame the caller's own URL", err)
	}
	if !errors.Is(err, ErrOAuthDiscoveryFailed) {
		t.Errorf("err = %v, want the provider-side ErrOAuthDiscoveryFailed class", err)
	}
	if !strings.Contains(err.Error(), wantDetail) {
		t.Errorf("err = %v, want the provider-advertised detail %q", err, wantDetail)
	}
	status, body := apperror.ToHTTPError(mcpOAuthError(err))
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", status)
	}
	msg, _ := body["error"].(map[string]any)["message"].(string)
	if strings.Contains(msg, ErrOutboundURLNotAllowed.Error()) {
		t.Errorf("message = %q, must not carry the caller-facing URL copy", msg)
	}
}

// TestDiscoverServerAuthRequirement_ProviderBlockedIssuerIsServiceUnavailable
// covers the all-blocked case from issue #248: the protected-resource document
// names an issuer on the link-local metadata address, so every authorization-
// server metadata candidate is rejected. That value is provider-controlled, so
// the caller must see a retryable 503, not a 400 blaming a URL it never wrote.
func TestDiscoverServerAuthRequirement_ProviderBlockedIssuerIsServiceUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	withGuardAllowingTestServer(t, srv.URL)

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+srv.URL+`/.well-known/oauth-protected-resource"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              srv.URL + "/mcp",
			"authorization_servers": []string{"http://169.254.169.254"},
		})
	})

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(srv.URL + "/mcp")}
	_, err := discoverServerAuthRequirement(context.Background(), server)
	assertProviderSideDiscoveryError(t, err, "169.254.169.254")
}

// TestDiscoverServerAuthRequirement_MixedBlockedCandidateIsServiceUnavailable
// covers the genuinely mixed case from issue #248: one provider-derived
// candidate is blocked while the rest answer 404. The pre-#204 aggregate dropped
// sentinels and reported a plain discovery failure; #204 exposed them, which
// must not flip the whole walk to a caller-facing 400. A blocked candidate among
// transient/absent ones is still provider-side.
func TestDiscoverServerAuthRequirement_MixedBlockedCandidateIsServiceUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	withGuardAllowingTestServer(t, srv.URL)

	blocked := "http://169.254.169.254/.well-known/oauth-authorization-server"
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+srv.URL+`/.well-known/oauth-protected-resource", authorization_uri="`+blocked+`"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	// A legitimate, allowed issuer whose own well-known candidates all 404.
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              srv.URL + "/mcp",
			"authorization_servers": []string{srv.URL + "/tenant"},
		})
	})

	server := &MCPServer{ID: "s1", Type: ServerTypeHTTP, URL: strPtr(srv.URL + "/mcp")}
	_, err := discoverServerAuthRequirement(context.Background(), server)
	assertProviderSideDiscoveryError(t, err, "169.254.169.254")
	// The walk really was mixed: the blocked candidate failed the guard while
	// the allowed issuer's candidates answered 404.
	if !strings.Contains(err.Error(), "-> 404") {
		t.Errorf("err = %v, want at least one 404 candidate alongside the blocked one", err)
	}
}
