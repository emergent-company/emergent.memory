package sources

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
)

func TestOAuthClient_Unconfigured(t *testing.T) {
	svc := NewService(nil, &config.Config{}, discardLog(), NewDefaultRegistry())
	if _, err := svc.resolveOAuthClient(context.Background(), uuid.Nil, ProviderTypeGoogleDrive, nil); err == nil {
		t.Fatal("resolveOAuthClient(google_drive) with no client = nil, want actionable error")
	}
}

func TestOAuthClient_ConfiguredFromDeploymentDefault(t *testing.T) {
	svc := NewService(nil, &config.Config{SourceOAuth: config.SourceOAuthConfig{
		GoogleClientID:     "cid",
		GoogleClientSecret: "cs",
	}}, discardLog(), NewDefaultRegistry())
	c, err := svc.resolveOAuthClient(context.Background(), uuid.Nil, ProviderTypeGoogleDrive, nil)
	if err != nil {
		t.Fatalf("resolveOAuthClient() error = %v", err)
	}
	if c.ClientID != "cid" || c.ClientSecret != "cs" {
		t.Fatalf("resolveOAuthClient() = %+v, want cid/cs", c)
	}
}

func TestOAuthRedirectURI_RequiresBase(t *testing.T) {
	svc := NewService(nil, &config.Config{}, discardLog(), NewDefaultRegistry())
	if _, err := svc.oauthRedirectURI(ProviderTypeGoogleDrive); err == nil {
		t.Fatal("oauthRedirectURI() with no base URL = nil, want error")
	}

	svc2 := NewService(nil, &config.Config{SourceOAuth: config.SourceOAuthConfig{
		RedirectBaseURL: "https://api.example/",
	}}, discardLog(), NewDefaultRegistry())
	got, err := svc2.oauthRedirectURI(ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("oauthRedirectURI() error = %v", err)
	}
	want := "https://api.example/api/data-source-integrations/oauth/google_drive/callback"
	if got != want {
		t.Fatalf("oauthRedirectURI() = %q, want %q", got, want)
	}
}

func TestOAuthLandingURL(t *testing.T) {
	svc := NewService(nil, &config.Config{SourceOAuth: config.SourceOAuthConfig{
		RedirectBaseURL: "https://api.example",
	}}, discardLog(), NewDefaultRegistry())
	if got := svc.OAuthLandingURL("?connected=abc"); got != "https://api.example/oauth/landing/data-sources?connected=abc" {
		t.Fatalf("OAuthLandingURL() = %q", got)
	}

	// No base URL → relative fallback.
	svc2 := NewService(nil, &config.Config{}, discardLog(), NewDefaultRegistry())
	if got := svc2.OAuthLandingURL("?oauth_error=connect_failed"); got != "/oauth/landing/data-sources?oauth_error=connect_failed" {
		t.Fatalf("OAuthLandingURL() fallback = %q", got)
	}

	// Explicit landing URL wins over the redirect-base derivation.
	svc3 := NewService(nil, &config.Config{SourceOAuth: config.SourceOAuthConfig{
		RedirectBaseURL: "https://api.example",
		LandingURL:      "https://ui.example/oauth/landing/data-sources",
	}}, discardLog(), NewDefaultRegistry())
	if got := svc3.OAuthLandingURL("?connected=abc"); got != "https://ui.example/oauth/landing/data-sources?connected=abc" {
		t.Fatalf("OAuthLandingURL() with LandingURL = %q", got)
	}
	if got := svc3.oauthJavascriptOrigin(); got != "https://ui.example" {
		t.Fatalf("oauthJavascriptOrigin() = %q, want https://ui.example", got)
	}

	// A configured landing URL's own query/fragment is dropped: the callback
	// query is appended, so keeping it would corrupt the outcome parameter. A
	// trailing slash is normalized away for the same reason (it would match no
	// route once the query follows).
	svc4 := NewService(nil, &config.Config{SourceOAuth: config.SourceOAuthConfig{
		LandingURL: "https://ui.example/oauth/landing/data-sources/?debug=1#frag",
	}}, discardLog(), NewDefaultRegistry())
	if got := svc4.OAuthLandingURL("?connected=abc"); got != "https://ui.example/oauth/landing/data-sources?connected=abc" {
		t.Fatalf("OAuthLandingURL() with query in LandingURL = %q", got)
	}
}

// TestOAuthLandingQuery verifies the callback landing query: the one-shot
// outcome plus the originating project when the flow knows it (the gateway
// validates that hint against the caller's access tree before using it).
func TestOAuthLandingQuery(t *testing.T) {
	projectID := "0fac5a81-b340-481e-9097-b978c7da0b71"

	got := oauthLandingQuery("connected", "src-1", projectID)
	want := "?connected=src-1&scope_project=" + projectID
	if got != want {
		t.Fatalf("oauthLandingQuery(connected) = %q, want %q", got, want)
	}

	// Unknown project (state never read) → no hint.
	if got := oauthLandingQuery("oauth_error", "connect_failed", ""); got != "?oauth_error=connect_failed" {
		t.Fatalf("oauthLandingQuery(no project) = %q", got)
	}

	// Outcomes are escaped, never concatenated raw.
	if got := oauthLandingQuery("oauth_error", "access denied&x=1", ""); got != "?oauth_error=access+denied%26x%3D1" {
		t.Fatalf("oauthLandingQuery(escaping) = %q", got)
	}
}

// TestGrantedScopes pins the grant-scope capture used for providers that declare
// no selectable scope strategies: the provider's own words, in order, deduped.
func TestGrantedScopes(t *testing.T) {
	cases := []struct {
		name  string
		creds Credentials
		want  []string
	}{
		{name: "missing", creds: Credentials{}, want: nil},
		{name: "empty", creds: Credentials{"scope": "  "}, want: nil},
		{name: "non-string", creds: Credentials{"scope": 42}, want: nil},
		{name: "space delimited", creds: Credentials{"scope": "openid email profile"}, want: []string{"openid", "email", "profile"}},
		{name: "dedupes and trims", creds: Credentials{"scope": " openid  openid\temail "}, want: []string{"openid", "email"}},
		// GitHub answers with the comma-delimited form, which must not become one
		// compound entry on the credentials card.
		{name: "comma delimited", creds: Credentials{"scope": "repo,read:org,gist"}, want: []string{"repo", "read:org", "gist"}},
		{name: "mixed separators", creds: Credentials{"scope": "repo, read:org\tuser"}, want: []string{"repo", "read:org", "user"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := grantedScopes(tc.creds)
			if len(got) != len(tc.want) {
				t.Fatalf("grantedScopes(%v) = %#v, want %#v", tc.creds, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("grantedScopes(%v) = %#v, want %#v", tc.creds, got, tc.want)
				}
			}
		})
	}
}
