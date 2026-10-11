package sources

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// stubProvider is a minimal SourceProvider used to prove the core is
// provider-agnostic: registering one is enough to have it served by the
// catalog, schema, and source-type derivation.
type stubProvider struct {
	catalog Provider
}

func (p *stubProvider) Catalog() Provider                 { return p.catalog }
func (p *stubProvider) ConfigSchema() ProviderSchema      { return ProviderSchema{Type: p.catalog.Type} }
func (p *stubProvider) CredentialFields() []ProviderField { return nil }
func (p *stubProvider) AuthMethods() []AuthMethod         { return []AuthMethod{AuthMethodOAuth} }
func (p *stubProvider) OAuth() (OAuthStrategy, bool) {
	return nil, false
}
func (p *stubProvider) ValidateConfig(map[string]any) error {
	return nil
}
func (p *stubProvider) ExtractCredentials(map[string]any) (Credentials, error) {
	return Credentials{}, nil
}
func (p *stubProvider) CredentialKeys() []string { return nil }
func (p *stubProvider) ListResources(context.Context, Credentials, map[string]any) ([]Resource, error) {
	return nil, nil
}
func (p *stubProvider) NewConnector(context.Context, Credentials, ConnectorDeps) (Connector, error) {
	return nil, nil
}

func TestRegistry_RegisterResolveList(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubProvider{catalog: Provider{Type: "github", Name: "GitHub", SourceType: "repo", Available: true}})

	if got, ok := reg.Get("github"); !ok || got.Catalog().Type != "github" {
		t.Fatalf("Get(github) = %v, %v; want the registered provider", got, ok)
	}
	if _, ok := reg.Get("imap"); ok {
		t.Fatal("Get(imap) = ok, want not-found")
	}
	if list := reg.List(); len(list) != 1 || list[0].Catalog().Type != "github" {
		t.Fatalf("List() = %v, want [github]", list)
	}

	// Re-registering a type replaces it without duplicating the listing.
	reg.Register(&stubProvider{catalog: Provider{Type: "github", Name: "GitHub v2", SourceType: "repo", Available: true}})
	if list := reg.List(); len(list) != 1 || list[0].Catalog().Name != "GitHub v2" {
		t.Fatalf("List() after replace = %v, want single GitHub v2", list)
	}
}

func TestService_ProviderAgnosticCatalogAndSourceType(t *testing.T) {
	cfg := &config.Config{}
	svc := NewService(nil, cfg, discardLog(), NewRegistry(
		&stubProvider{catalog: Provider{Type: "github", Name: "GitHub", SourceType: "repo", Available: true}},
	))

	providers := svc.ListProviders(context.Background())
	if len(providers) != 1 || providers[0].Type != "github" {
		t.Fatalf("ListProviders() = %v, want the registered github provider", providers)
	}

	src := &Source{
		ID:         uuid.New(),
		ProjectID:  uuid.New(),
		Provider:   "github",
		SyncPolicy: "manual",
		Status:     "active",
	}
	if it := svc.toIntegration(src); it.SourceType != "repo" {
		t.Fatalf("toIntegration().SourceType = %q, want repo (derived from the provider)", it.SourceType)
	}
}
