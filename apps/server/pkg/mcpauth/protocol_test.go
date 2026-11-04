package mcpauth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// These tests cover the protocol helpers that used to live in
// domain/mcpregistry and moved into this package (#2090). They are kept
// verbatim where they pin behaviour that a real provider forced (Figma's
// challenge, GitHub's path-bearing issuer, Supabase's issuer-as-identifier).

func TestParseBearerChallenge_Figma(t *testing.T) {
	// Verbatim from a live probe of https://mcp.figma.com/mcp.
	header := `Bearer resource_metadata="https://mcp.figma.com/.well-known/oauth-protected-resource",scope="mcp:connect",authorization_uri="https://api.figma.com/.well-known/oauth-authorization-server"`

	ch, ok := ParseBearerChallenge(header)
	if !ok {
		t.Fatalf("ParseBearerChallenge returned ok=false")
	}
	if ch.ResourceMetadataURL != "https://mcp.figma.com/.well-known/oauth-protected-resource" {
		t.Errorf("ResourceMetadataURL = %q", ch.ResourceMetadataURL)
	}
	if ch.AuthorizationURI != "https://api.figma.com/.well-known/oauth-authorization-server" {
		t.Errorf("AuthorizationURI = %q", ch.AuthorizationURI)
	}
	if ch.Scope != "mcp:connect" {
		t.Errorf("Scope = %q", ch.Scope)
	}
}

func TestParseBearerChallenge_TokenFormAndUnquoted(t *testing.T) {
	ch, ok := ParseBearerChallenge(`Bearer resource_metadata=https://example.test/prm`)
	if !ok {
		t.Fatalf("ok=false, want true")
	}
	if ch.ResourceMetadataURL != "https://example.test/prm" {
		t.Errorf("ResourceMetadataURL = %q", ch.ResourceMetadataURL)
	}
}

func TestParseBearerChallenge_Rejects(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"not bearer":       `Basic realm="x"`,
		"bearer no params": "Bearer",
		// A challenge with neither metadata URL nor authorization_uri cannot
		// drive discovery, so it must not be treated as an OAuth requirement.
		"bearer only scope": `Bearer scope="read"`,
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := ParseBearerChallenge(header); ok {
				t.Errorf("ParseBearerChallenge(%q) = ok, want false", header)
			}
		})
	}
}

func TestIssuerFromMetadataURL(t *testing.T) {
	cases := map[string]string{
		"https://api.figma.com/.well-known/oauth-authorization-server":  "https://api.figma.com",
		"https://issuer.test/.well-known/oauth-authorization-server/t1": "https://issuer.test",
		"https://issuer.test/oauth/token":                               "",
		"not a url":                                                     "",
	}
	for in, want := range cases {
		if got := issuerFromMetadataURL(in); got != want {
			t.Errorf("issuerFromMetadataURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWellKnownInsert(t *testing.T) {
	cases := map[string]string{
		// Path-bearing issuer: the well-known segment is inserted between host
		// and path (RFC 8414 §3.1).
		"https://example.com/tenant1":       "https://example.com/.well-known/oauth-authorization-server/tenant1",
		"https://example.com/tenant1/":      "https://example.com/.well-known/oauth-authorization-server/tenant1",
		"https://example.com/a/b":           "https://example.com/.well-known/oauth-authorization-server/a/b",
		"https://api.figma.com":             "https://api.figma.com/.well-known/oauth-authorization-server",
		"https://api.figma.com/":            "https://api.figma.com/.well-known/oauth-authorization-server",
		"https://github.com/login/oauth":    "https://github.com/.well-known/oauth-authorization-server/login/oauth",
		"https://github.com/login/oauth/":   "https://github.com/.well-known/oauth-authorization-server/login/oauth",
		"https://issuer.test/some/path/x/y": "https://issuer.test/.well-known/oauth-authorization-server/some/path/x/y",
	}
	for in, want := range cases {
		if got := wellKnownInsert(in, wellKnownOAuthASPath); got != want {
			t.Errorf("wellKnownInsert(%q, oauth) = %q, want %q", in, got, want)
		}
	}
}

func TestWellKnownAppend(t *testing.T) {
	if got, want := wellKnownAppend("https://github.com/login/oauth", wellKnownOIDCPath), "https://github.com/login/oauth/.well-known/openid-configuration"; got != want {
		t.Errorf("wellKnownAppend = %q, want %q", got, want)
	}
	if got, want := wellKnownInsert("https://github.com/login/oauth", wellKnownOIDCPath), "https://github.com/.well-known/openid-configuration/login/oauth"; got != want {
		t.Errorf("wellKnownInsert(oidc) = %q, want %q", got, want)
	}
}

func TestAuthMetadataCandidates_AuthorizationURIFirst(t *testing.T) {
	// A challenge may point at a metadata document on a different host than the
	// issuer the resource advertises (Figma: resource on mcp.figma.com, AS
	// documents on api.figma.com). That URL is authoritative and leads the list,
	// and must not be duplicated later.
	authorizationURI := "https://auth.example.com/.well-known/oauth-authorization-server/tenant9"
	got := authMetadataCandidates("https://issuer.example.com/tenant1", authorizationURI)
	if got[0] != authorizationURI {
		t.Fatalf("candidates[0] = %q, want the authorization_uri %q (full: %v)", got[0], authorizationURI, got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] == got[0] {
			t.Errorf("candidate %q duplicated at index %d", got[0], i)
		}
	}
}

func TestAuthMetadataCandidates_FigmaHostOnlyDedupe(t *testing.T) {
	// Host-only issuer: insert and append collapse for each document family.
	got := authMetadataCandidates("https://api.figma.com", "")
	want := []string{
		"https://api.figma.com/.well-known/oauth-authorization-server",
		"https://api.figma.com/.well-known/openid-configuration",
	}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAuthorizeURL_CarriesResourceAndPKCE(t *testing.T) {
	req := &Requirement{
		AuthorizationEndpoint: "https://auth.test/authorize",
		Resource:              "https://mcp.test/mcp",
		Scopes:                []string{"mcp:connect", "offline_access"},
	}
	got, err := AuthorizeURL(req, &Identity{ClientID: "cid"}, "https://api.test/cb", "state-1", "challenge-1")
	if err != nil {
		t.Fatalf("AuthorizeURL: %v", err)
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
		"resource":              "https://mcp.test/mcp",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestCodeChallengeS256_MatchesRFC7636Example(t *testing.T) {
	// RFC 7636 Appendix B.
	if got, want := CodeChallengeS256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"), "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"; got != want {
		t.Errorf("CodeChallengeS256 = %q, want %q", got, want)
	}
}

func TestNewStateAndVerifierAreUnpredictable(t *testing.T) {
	state, err := NewState()
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	verifier, err := NewCodeVerifier()
	if err != nil {
		t.Fatalf("NewCodeVerifier: %v", err)
	}
	if len(state) < 32 || len(verifier) < 43 {
		t.Errorf("state/verifier too short: %d/%d", len(state), len(verifier))
	}
	if state == verifier {
		t.Error("state and verifier must not be derived from the same draw")
	}
}

// TestIsCredentialRejection pins the one predicate both refresh paths use to
// decide between "the credential is dead, ask the user to reconnect" and "this
// clears on its own, retry". Getting it wrong in either direction is visible to
// the user: a transient failure reported as dead sends them through consent for a
// working grant, and a dead grant reported as transient retries forever.
func TestIsCredentialRejection(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error is not a rejection", errors.New("token request failed: dial tcp: i/o timeout"), false},
		{"invalid_grant", &Rejection{StatusCode: http.StatusBadRequest, Code: "invalid_grant", Description: "Token has been expired or revoked."}, true},
		{"gotrue invalid_credentials", &Rejection{StatusCode: http.StatusBadRequest, Code: "invalid_credentials", Description: "Invalid login credentials"}, true},
		{"invalid_client", &Rejection{StatusCode: http.StatusUnauthorized, Code: "invalid_client"}, true},
		{"unauthorized_client", &Rejection{StatusCode: http.StatusBadRequest, Code: "unauthorized_client"}, true},
		{"code-less 4xx", &Rejection{StatusCode: http.StatusForbidden}, true},
		{"wrapped rejection", fmt.Errorf("refresh: %w", &Rejection{StatusCode: http.StatusBadRequest, Code: "invalid_grant"}), true},
		{"rate limit", &Rejection{StatusCode: http.StatusTooManyRequests, Code: "over_request_rate_limit", Description: "Request rate limit reached"}, false},
		{"server-side timeout", &Rejection{StatusCode: http.StatusRequestTimeout, Code: "slow_down"}, false},
		{"server error", &Rejection{StatusCode: http.StatusServiceUnavailable, Code: "temporarily_unavailable"}, false},
		{"malformed request", &Rejection{StatusCode: http.StatusBadRequest, Code: "invalid_request"}, false},
		{"unsupported grant type", &Rejection{StatusCode: http.StatusBadRequest, Code: "unsupported_grant_type"}, false},
		{"invalid target", &Rejection{StatusCode: http.StatusBadRequest, Code: "invalid_target"}, false},
		{"invalid scope", &Rejection{StatusCode: http.StatusBadRequest, Code: "invalid_scope"}, false},
		{"case-insensitive code", &Rejection{StatusCode: http.StatusBadRequest, Code: "INVALID_REQUEST"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCredentialRejection(tc.err); got != tc.want {
				t.Errorf("IsCredentialRejection(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
