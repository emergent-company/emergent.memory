package sources

import (
	"context"
	"encoding/json"
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
	if err := conn.TestConnection(context.Background(), nil); err == nil {
		t.Fatal("a trigger-only source must not report a healthy connection: its credentials are verified per delivery")
	}
}

// TestNormalizeInstallationID covers the shape a browser form actually posts:
// GitHub's install redirect and JSON.stringify hand back a JSON number, which
// encoding/json decodes to float64. Stored verbatim, fmt.Sprint would write
// "1.2345678e+07" and the dispatcher's strconv.FormatInt comparison would never
// match, so the source would silently never dispatch.
func TestNormalizeInstallationID(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   any
		want string
	}{
		{"string", "12345678", "12345678"},
		{"padded string", " 12345678 ", "12345678"},
		{"json float64", float64(12345678), "12345678"},
		{"json number", json.Number("12345678"), "12345678"},
		{"int", 12345678, "12345678"},
		{"int64", int64(12345678), "12345678"},
	} {
		got, err := normalizeInstallationID(tc.in)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	for _, tc := range []struct {
		name string
		in   any
	}{
		{"missing", nil},
		{"empty", ""},
		{"nil string", "<nil>"},
		{"fractional", 1234.5},
		{"negative", -1},
		{"non-numeric", "install-42"},
		{"object", map[string]any{}},
	} {
		if got, err := normalizeInstallationID(tc.in); err == nil {
			t.Errorf("%s: expected an error, got %q", tc.name, got)
		}
	}

	// A JSON number survives NormalizeConfig and then ValidateConfig, and the
	// stored config carries digits rather than scientific notation.
	cfg := map[string]any{
		"app_id":          "11111111-1111-1111-1111-111111111111",
		"installation_id": float64(12345678),
		"repos":           []any{"owner/repo"},
	}
	p := newGitHubAppProvider()
	normalizer, ok := p.(ConfigNormalizer)
	if !ok {
		t.Fatal("github_app provider must normalize its config")
	}
	normalizer.NormalizeConfig(cfg)
	if got, _ := cfg["installation_id"].(string); got != "12345678" {
		t.Fatalf("normalized installation_id = %#v, want 12345678", cfg["installation_id"])
	}
	if err := p.ValidateConfig(cfg); err != nil {
		t.Fatalf("normalized config rejected: %v", err)
	}
}

// TestGitHubAppPackProvenanceAndPresentation pins the metadata the Schema UI
// depends on: the pack names its provider as its source, the catalog exposes the
// pack identity, and every emitted type carries a presentation block from the
// supported icon catalog with an accent colour.
func TestGitHubAppPackProvenanceAndPresentation(t *testing.T) {
	p := newGitHubAppProvider()
	typed, ok := p.(TypedObjectProvider)
	if !ok {
		t.Fatal("github app provider does not declare object types")
	}
	pack := typed.ObjectTypes()
	if pack == nil {
		t.Fatal("github app provider declares no pack")
	}
	if pack.Name != "github-app-workflow" || pack.Version != "1.4.0" {
		t.Errorf("pack identity = %s@%s", pack.Name, pack.Version)
	}
	if pack.Source != "provider:github_app" {
		t.Errorf("pack source = %q, want provider:github_app", pack.Source)
	}
	cat := p.Catalog()
	if cat.SchemaPack != pack.Name || cat.SchemaPackVersion != pack.Version {
		t.Errorf("catalog pack = %s@%s, want %s@%s", cat.SchemaPack, cat.SchemaPackVersion, pack.Name, pack.Version)
	}
	for _, def := range pack.ObjectTypes {
		if def.UI == nil {
			t.Errorf("%s: no ui block", def.Name)
			continue
		}
		if icon, _ := def.UI["icon"].(string); icon == "" {
			t.Errorf("%s: no icon", def.Name)
		}
		if color, _ := def.UI["color"].(string); color == "" {
			t.Errorf("%s: no color", def.Name)
		}
	}
}

func TestGitHubAppProviderNormalizeAssigneeLogin(t *testing.T) {
	p, ok := newGitHubAppProvider().(ConfigNormalizer)
	if !ok {
		t.Fatal("github_app provider must normalize config")
	}
	cfg := map[string]any{"assignee_login": nil}
	p.NormalizeConfig(cfg)
	if got := cfg["assignee_login"]; got != "" {
		t.Fatalf("nil assignee_login = %#v, want empty string", got)
	}
	cfg = map[string]any{"assignee_login": " mkucharz "}
	p.NormalizeConfig(cfg)
	if got := cfg["assignee_login"]; got != "mkucharz" {
		t.Fatalf("assignee_login = %#v, want mkucharz", got)
	}
}
