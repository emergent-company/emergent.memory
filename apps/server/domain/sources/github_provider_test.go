package sources

import (
	"context"
	"slices"
	"testing"
)

func TestGitHubProvider_Catalog(t *testing.T) {
	p := newGitHubProvider()
	c := p.Catalog()
	if c.Type != ProviderTypeGitHub {
		t.Errorf("Catalog().Type = %q, want %q", c.Type, ProviderTypeGitHub)
	}
	if c.Name != "GitHub" {
		t.Errorf("Catalog().Name = %q, want GitHub", c.Name)
	}
	if !c.Available {
		t.Error("Catalog().Available = false, want true")
	}
	if c.SourceType != SourceTypeRepo {
		t.Errorf("Catalog().SourceType = %q, want %q", c.SourceType, SourceTypeRepo)
	}
}

func TestGitHubProvider_ValidateConfig_MissingRepo(t *testing.T) {
	p := newGitHubProvider()
	if err := p.ValidateConfig(map[string]any{}); err == nil {
		t.Fatal("ValidateConfig({}) = nil, want error")
	}
	if err := p.ValidateConfig(map[string]any{"repo": "owner/name"}); err != nil {
		t.Fatalf("ValidateConfig(repo) = %v, want nil", err)
	}
}

func TestGitHubProvider_ExtractCredentials(t *testing.T) {
	p := newGitHubProvider()
	if _, err := p.ExtractCredentials(map[string]any{}); err == nil {
		t.Fatal("ExtractCredentials({}) = nil, want error")
	}
	creds, err := p.ExtractCredentials(map[string]any{"access_token": "at", "client_id": "cid", "client_secret": "cs"})
	if err != nil {
		t.Fatalf("ExtractCredentials() error = %v", err)
	}
	if creds["access_token"] != "at" {
		t.Errorf("creds[access_token] = %v, want at", creds["access_token"])
	}
	if creds["client_id"] != "cid" || creds["client_secret"] != "cs" {
		t.Errorf("creds = %v, want client_id/client_secret carried", creds)
	}
}

func TestGitHubProvider_CredentialKeys(t *testing.T) {
	p := newGitHubProvider()
	keys := p.CredentialKeys()
	if !slices.Contains(keys, "access_token") {
		t.Errorf("CredentialKeys() = %v, want access_token", keys)
	}
}

func TestGitHubProvider_AuthMethods(t *testing.T) {
	p := newGitHubProvider()
	methods := p.AuthMethods()
	if !slices.Contains(methods, AuthMethodOAuth) {
		t.Errorf("AuthMethods() = %v, want oauth", methods)
	}
	if !slices.Contains(methods, AuthMethodCredentials) {
		t.Errorf("AuthMethods() = %v, want credentials", methods)
	}
}

func TestGitHubProvider_ListResources_NotImplemented(t *testing.T) {
	p := newGitHubProvider()
	if _, err := p.ListResources(context.Background(), Credentials{"access_token": "at"}, nil); err == nil {
		t.Fatal("ListResources() = nil, want not-implemented error")
	}
}
