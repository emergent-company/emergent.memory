package sources

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
)

func TestOAuthClient_Unconfigured(t *testing.T) {
	svc := NewService(nil, &config.Config{}, discardLog(), NewDefaultRegistry())
	if _, err := svc.resolveOAuthClient(context.Background(), uuid.Nil, ProviderTypeGoogleDrive); err == nil {
		t.Fatal("resolveOAuthClient(google_drive) with no client = nil, want actionable error")
	}
}

func TestOAuthClient_ConfiguredFromDeploymentDefault(t *testing.T) {
	svc := NewService(nil, &config.Config{SourceOAuth: config.SourceOAuthConfig{
		GoogleClientID:     "cid",
		GoogleClientSecret: "cs",
	}}, discardLog(), NewDefaultRegistry())
	c, err := svc.resolveOAuthClient(context.Background(), uuid.Nil, ProviderTypeGoogleDrive)
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
	if got := svc.OAuthLandingURL("?connected=abc"); got != "https://api.example/data-sources?connected=abc" {
		t.Fatalf("OAuthLandingURL() = %q", got)
	}

	// No base URL → relative fallback.
	svc2 := NewService(nil, &config.Config{}, discardLog(), NewDefaultRegistry())
	if got := svc2.OAuthLandingURL("?oauth_error=connect_failed"); got != "/data-sources?oauth_error=connect_failed" {
		t.Fatalf("OAuthLandingURL() fallback = %q", got)
	}
}
