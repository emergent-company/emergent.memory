package memoryapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcpregistry"
)

func TestGetRegistryServerMapsArgsAndHeaders(t *testing.T) {
	body := `{"success":true,"data":{
		"name":"io.github.x/y",
		"title":"X/Y",
		"description":"desc",
		"version":"0.1.0",
		"hasRemotes":true,
		"hasPackages":true,
		"packages":[{"registryType":"npm","name":"y","identifier":"y","version":"2.3.4","transport":"stdio","runtimeArguments":["--log-level","debug"],"packageArguments":["serve"]}],
		"remotes":[{"type":"streamable-http","url":"https://mcp.example.com/mcp"}],
		"envVars":[{"name":"API_KEY","isRequired":true,"isSecret":true}],
		"headers":[{"name":"Authorization","description":"Bearer","isRequired":true,"isSecret":true,"value":"Bearer {api_key}"}]
	}}`
	srv, cap := newTestServer(t, http.StatusOK, body)
	client := newClientFor(t, srv.URL)

	entry, err := client.GetRegistryServer(context.Background(), "gh")
	if err != nil {
		t.Fatalf("GetRegistryServer: %v", err)
	}
	assertRequest(t, cap, http.MethodGet, "/api/admin/mcp-registry/servers/gh")

	if len(entry.Packages) != 1 {
		t.Fatalf("packages = %d, want 1", len(entry.Packages))
	}
	pkg := entry.Packages[0]
	if pkg.Version != "2.3.4" {
		t.Errorf("Version = %q, want 2.3.4", pkg.Version)
	}
	if !equalStrings(pkg.RuntimeArguments, []string{"--log-level", "debug"}) {
		t.Errorf("RuntimeArguments = %v", pkg.RuntimeArguments)
	}
	if !equalStrings(pkg.PackageArguments, []string{"serve"}) {
		t.Errorf("PackageArguments = %v", pkg.PackageArguments)
	}
	if len(entry.Headers) != 1 {
		t.Fatalf("headers = %d, want 1", len(entry.Headers))
	}
	if entry.Headers[0].Name != "Authorization" || !entry.Headers[0].IsRequired || !entry.Headers[0].IsSecret {
		t.Errorf("header = %+v", entry.Headers[0])
	}
}

func TestGetRegistryServerNotFoundMapsToErrNotFound(t *testing.T) {
	srv, _ := newTestServer(t, http.StatusNotFound, `{"error":{"code":"not_found","message":"no"}}`)
	client := newClientFor(t, srv.URL)

	_, err := client.GetRegistryServer(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != mcpregistry.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
