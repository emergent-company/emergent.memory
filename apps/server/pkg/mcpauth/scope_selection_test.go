package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// scopeDiscoveryServer serves a protected-resource metadata document and an
// authorization-server document. Either scope list may be nil, modelling a
// provider that advertises scopes only in one of the two documents.
type scopeDiscoveryServer struct {
	*httptest.Server
	prmScopes []string
	asScopes  []string
}

func newScopeDiscoveryServer(t *testing.T, prmScopes, asScopes []string) *scopeDiscoveryServer {
	t.Helper()
	fs := &scopeDiscoveryServer{prmScopes: prmScopes, asScopes: asScopes}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	fs.Server = srv

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"resource":              srv.URL + "/mcp",
			"authorization_servers": []string{srv.URL},
		}
		if len(fs.prmScopes) > 0 {
			body["scopes_supported"] = fs.prmScopes
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
		}
		if len(fs.asScopes) > 0 {
			body["scopes_supported"] = fs.asScopes
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
	t.Cleanup(srv.Close)
	return fs
}

func discoverFrom(t *testing.T, fs *scopeDiscoveryServer, challenge Challenge, explicit []string) *Requirement {
	t.Helper()
	req, err := Discover(context.Background(), DiscoveryConfig{
		Resource:  fs.URL + "/mcp",
		Challenge: challenge,
		Scopes:    explicit,
		ClientFor: func(string) *http.Client { return fs.Client() },
		Validate:  func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return req
}

// TestDiscover_AdvertisedScopesAreNotRequested is the RFC 9728 §7.2 contract:
// scopes_supported is a capability list, so a challenge without a scope must
// not turn the whole advertised set into the requested set.
func TestDiscover_AdvertisedScopesAreNotRequested(t *testing.T) {
	fs := newScopeDiscoveryServer(t, []string{"repo", "write:packages", "project"}, nil)

	req := discoverFrom(t, fs, Challenge{ResourceMetadataURL: fs.URL + "/.well-known/oauth-protected-resource"}, nil)

	if len(req.Scopes) != 0 {
		t.Errorf("Scopes = %v, want none requested", req.Scopes)
	}
	want := []string{"repo", "write:packages", "project"}
	if len(req.ScopesAvailable) != len(want) {
		t.Fatalf("ScopesAvailable = %v, want %v", req.ScopesAvailable, want)
	}
	for i := range want {
		if req.ScopesAvailable[i] != want[i] {
			t.Fatalf("ScopesAvailable = %v, want %v", req.ScopesAvailable, want)
		}
	}
}

// TestDiscover_ChallengeScopeIsRequested keeps the authoritative signal intact:
// a challenge that names a scope still requests exactly that scope.
func TestDiscover_ChallengeScopeIsRequested(t *testing.T) {
	fs := newScopeDiscoveryServer(t, []string{"repo", "project"}, nil)

	req := discoverFrom(t, fs, Challenge{
		ResourceMetadataURL: fs.URL + "/.well-known/oauth-protected-resource",
		Scope:               "mcp:connect offline_access",
	}, nil)

	if got := len(req.Scopes); got != 2 || req.Scopes[0] != "mcp:connect" || req.Scopes[1] != "offline_access" {
		t.Errorf("Scopes = %v, want [mcp:connect offline_access]", req.Scopes)
	}
	if len(req.ScopesAvailable) != 2 {
		t.Errorf("ScopesAvailable = %v, want the advertised list", req.ScopesAvailable)
	}
}

// TestDiscover_ExplicitScopesWinOverChallenge pins the operator-override path:
// configured scopes replace the challenge scope rather than merging with it.
func TestDiscover_ExplicitScopesWinOverChallenge(t *testing.T) {
	fs := newScopeDiscoveryServer(t, []string{"repo", "project"}, nil)

	req := discoverFrom(t, fs, Challenge{
		ResourceMetadataURL: fs.URL + "/.well-known/oauth-protected-resource",
		Scope:               "mcp:connect",
	}, []string{"repo", "read:org"})

	if got := len(req.Scopes); got != 2 || req.Scopes[0] != "repo" || req.Scopes[1] != "read:org" {
		t.Errorf("Scopes = %v, want the explicit [repo read:org]", req.Scopes)
	}
}

// TestDiscover_ScopesAvailableFallsBackToAuthServerMetadata covers providers
// that advertise scopes only on the authorization server, not the resource.
func TestDiscover_ScopesAvailableFallsBackToAuthServerMetadata(t *testing.T) {
	fs := newScopeDiscoveryServer(t, nil, []string{"openid", "email"})

	req := discoverFrom(t, fs, Challenge{ResourceMetadataURL: fs.URL + "/.well-known/oauth-protected-resource"}, nil)

	if len(req.Scopes) != 0 {
		t.Errorf("Scopes = %v, want none requested", req.Scopes)
	}
	if got := len(req.ScopesAvailable); got != 2 || req.ScopesAvailable[0] != "openid" || req.ScopesAvailable[1] != "email" {
		t.Errorf("ScopesAvailable = %v, want [openid email]", req.ScopesAvailable)
	}
}
