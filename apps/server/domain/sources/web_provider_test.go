package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/outbound"
)

func TestWebProvider_Catalog(t *testing.T) {
	p := newWebProvider(outbound.DefaultPolicy())
	c := p.Catalog()
	if c.Type != ProviderTypeWeb {
		t.Errorf("Catalog().Type = %q, want %q", c.Type, ProviderTypeWeb)
	}
	if c.SourceType != SourceTypeWeb {
		t.Errorf("Catalog().SourceType = %q, want %q", c.SourceType, SourceTypeWeb)
	}
	if c.Name != "Web" {
		t.Errorf("Catalog().Name = %q, want Web", c.Name)
	}
	if !c.Available {
		t.Error("Catalog().Available = false, want true")
	}
	if c.Vendor != "web" || c.VendorName != "Web" {
		t.Errorf("Catalog() vendor = %q/%q, want web/Web", c.Vendor, c.VendorName)
	}
}

func TestWebProvider_SchemaShape(t *testing.T) {
	p := newWebProvider(outbound.DefaultPolicy())
	s := p.ConfigSchema()
	if s.Type != ProviderTypeWeb {
		t.Errorf("ConfigSchema().Type = %q, want %q", s.Type, ProviderTypeWeb)
	}
	if len(s.Required) != 1 || s.Required[0] != "start_urls" {
		t.Errorf("ConfigSchema().Required = %v, want [start_urls]", s.Required)
	}
	for _, key := range []string{"start_urls", "include_globs", "exclude_globs", "max_pages", "max_depth"} {
		if _, ok := s.Properties[key]; !ok {
			t.Errorf("ConfigSchema().Properties missing %q", key)
		}
	}
	// No boolean fields: the gateway form omits unchecked checkboxes, so a
	// default-true boolean could never be turned off.
	for key, prop := range s.Properties {
		if m, ok := prop.(map[string]any); ok && m["type"] == "boolean" {
			t.Errorf("ConfigSchema().Properties[%q] is a boolean field", key)
		}
	}
}

func TestWebProvider_AuthIsConfigOnly(t *testing.T) {
	p := newWebProvider(outbound.DefaultPolicy())
	got := p.AuthMethods()
	if len(got) != 1 || got[0] != AuthMethodConfig {
		t.Errorf("AuthMethods() = %v, want [config]", got)
	}
	if _, ok := p.OAuth(); ok {
		t.Error("OAuth() ok = true, want false for a config-only provider")
	}
	if fields := p.CredentialFields(); len(fields) != 0 {
		t.Errorf("CredentialFields() = %v, want empty", fields)
	}
	if keys := p.CredentialKeys(); len(keys) != 0 {
		t.Errorf("CredentialKeys() = %v, want empty", keys)
	}
	creds, err := p.ExtractCredentials(map[string]any{"start_urls": []any{"https://example.com"}})
	if err != nil {
		t.Fatalf("ExtractCredentials() error = %v", err)
	}
	if len(creds) != 0 {
		t.Errorf("ExtractCredentials() = %v, want empty map", creds)
	}
}

func TestWebProvider_ListResourcesEmpty(t *testing.T) {
	p := newWebProvider(outbound.DefaultPolicy())
	got, truncated, err := p.ListResources(context.Background(), nil, nil, "")
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if truncated {
		t.Error("ListResources() truncated = true, want false")
	}
	if got == nil {
		t.Fatal("ListResources() = nil, want a non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("ListResources() = %v, want empty", got)
	}
}

func TestWebProvider_SyncIntervalBounds(t *testing.T) {
	p := newWebProvider(outbound.DefaultPolicy())
	minMin, defMin := p.SyncIntervalBounds()
	if minMin != 15 || defMin != 60 {
		t.Errorf("SyncIntervalBounds() = (%d, %d), want (15, 60)", minMin, defMin)
	}
}

func TestWebProvider_ValidateConfig(t *testing.T) {
	p := newWebProvider(outbound.DefaultPolicy())
	cases := []struct {
		name    string
		cfg     map[string]any
		wantErr bool
	}{
		{"missing start_urls", map[string]any{}, true},
		{"nil start_urls", map[string]any{"start_urls": nil}, true},
		{"empty start_urls", map[string]any{"start_urls": []any{}}, true},
		{"non-array start_urls", map[string]any{"start_urls": "https://example.com"}, true},
		{"non-string entry", map[string]any{"start_urls": []any{"https://example.com", 42}}, true},
		{"valid minimal", map[string]any{"start_urls": []any{"https://example.com"}}, false},
		{"valid string slice", map[string]any{"start_urls": []string{"https://example.com"}}, false},
		{
			"valid with optionals",
			map[string]any{
				"start_urls":    []string{"https://example.com"},
				"include_globs": []string{"/docs/**"},
				"exclude_globs": []string{"/admin/**"},
				"max_pages":     50,
				"max_depth":     1,
			},
			false,
		},
		{"relative url", map[string]any{"start_urls": []any{"/relative"}}, true},
		{"ftp scheme", map[string]any{"start_urls": []any{"ftp://example.com"}}, true},
		{"userinfo", map[string]any{"start_urls": []any{"https://user:pass@example.com"}}, true},
		{"no host", map[string]any{"start_urls": []any{"https:///path"}}, true},
		{"over 50 urls", map[string]any{"start_urls": fiftyOneWebURLs()}, true},
		{"max_pages 0", map[string]any{"start_urls": []any{"https://example.com"}, "max_pages": 0}, true},
		{"max_pages 201", map[string]any{"start_urls": []any{"https://example.com"}, "max_pages": 201}, true},
		{"max_depth 4", map[string]any{"start_urls": []any{"https://example.com"}, "max_depth": 4}, true},
		{"bad include_globs", map[string]any{"start_urls": []any{"https://example.com"}, "include_globs": []any{1}}, true},
		{"unknown key ignored", map[string]any{"start_urls": []any{"https://example.com"}, "bogus": 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := p.ValidateConfig(tc.cfg)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateConfig() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateConfig() error = %v", err)
			}
		})
	}
}

func fiftyOneWebURLs() []any {
	out := make([]any, 51)
	for i := range out {
		out[i] = fmt.Sprintf("https://example.com/p%d", i)
	}
	return out
}

func TestWebRegistry_ContainsWeb(t *testing.T) {
	reg := NewConfiguredRegistry(nil)
	if _, ok := reg.Get(ProviderTypeWeb); !ok {
		t.Fatalf("NewConfiguredRegistry(nil) has no %q provider", ProviderTypeWeb)
	}
	if _, ok := NewDefaultRegistry().Get(ProviderTypeWeb); !ok {
		t.Fatalf("NewDefaultRegistry() has no %q provider", ProviderTypeWeb)
	}
}

// TestWebRegistry_ConfiguredPolicyReachesConnector asserts the deployment's
// WebSource config drives the provider's outbound policy: AllowPrivate is what
// lets a loopback connection test succeed, and the default policy blocks it.
func TestWebRegistry_ConfiguredPolicyReachesConnector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer srv.Close()

	cfg := map[string]any{"start_urls": []any{srv.URL}}

	// Default policy (AllowPrivate false): loopback is blocked.
	def := NewConfiguredRegistry(nil)
	p, ok := def.Get(ProviderTypeWeb)
	if !ok {
		t.Fatal("web provider not registered")
	}
	conn, err := p.NewConnector(context.Background(), nil, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	if err := conn.TestConnection(context.Background(), cfg); err == nil {
		t.Error("default policy allowed a loopback connection test, want blocked")
	}

	// Configured policy (AllowPrivate true): loopback is reachable.
	cfg2 := &config.Config{WebSource: config.WebSourceConfig{AllowPrivate: true}}
	p2, ok := NewConfiguredRegistry(cfg2).Get(ProviderTypeWeb)
	if !ok {
		t.Fatal("web provider not registered")
	}
	conn2, err := p2.NewConnector(context.Background(), nil, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	if err := conn2.TestConnection(context.Background(), cfg); err != nil {
		t.Errorf("AllowPrivate policy blocked a loopback connection test: %v", err)
	}
}
