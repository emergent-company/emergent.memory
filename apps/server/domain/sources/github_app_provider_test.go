package sources

import (
	"context"
	"testing"
)

func TestGitHubAppProviderCatalogAndValidation(t *testing.T) {
	p := newGitHubAppProvider()
	cat := p.Catalog()
	if cat.Type != ProviderTypeGitHubApp || cat.SourceType != SourceTypeApp || !cat.Available {
		t.Fatalf("catalog = %#v", cat)
	}
	if err := p.ValidateConfig(map[string]any{
		"app_id":          "11111111-1111-1111-1111-111111111111",
		"installation_id": "42",
		"repos":           []any{"owner/repo"},
	}); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	for name, cfg := range map[string]map[string]any{
		"no app":         {"installation_id": "42", "repos": []any{"owner/repo"}},
		"bad app":        {"app_id": "nope", "installation_id": "42", "repos": []any{"owner/repo"}},
		"no install":     {"app_id": "11111111-1111-1111-1111-111111111111", "repos": []any{"owner/repo"}},
		"no repos":       {"app_id": "11111111-1111-1111-1111-111111111111", "installation_id": "42"},
		"bad repo shape": {"app_id": "11111111-1111-1111-1111-111111111111", "installation_id": "42", "repos": []any{"not-a-repo"}},
	} {
		if err := p.ValidateConfig(cfg); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}

	conn, err := p.NewConnector(context.Background(), nil, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if _, err := conn.Enumerate(context.Background(), nil); err == nil {
		t.Fatal("a trigger-only source must refuse sync")
	}
}
