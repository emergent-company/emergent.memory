package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// --- Official MCP registry browse/install client methods ---
//
// These cover the MemoryClient proxies for memory's registry admin routes
// (GET /api/admin/mcp-registry/search, POST /api/admin/mcp-registry/install),
// mirroring the mcp-servers client tests in mcp_servers_client_test.go.

// mcpRegistrySearchFixture is one memory RegistrySearchResultDTO sample.
func mcpRegistrySearchFixture() string {
	return `{
		"servers": [
			{
				"name": "io.github.github/github-mcp-server",
				"title": "GitHub",
				"description": "Official GitHub MCP server",
				"version": "1.0.0",
				"repository": {"url": "https://github.com/github/github-mcp-server", "source": "github"},
				"hasRemotes": true,
				"hasPackages": true,
				"packages": [{"registryType": "npm", "identifier": "@modelcontextprotocol/server-github", "transport": "stdio"}],
				"remotes": [{"type": "streamable-http", "url": "https://api.githubcopilot.com/mcp/"}],
				"envVars": [{"name": "GITHUB_PERSONAL_ACCESS_TOKEN", "isRequired": true, "isSecret": true}]
			},
			{
				"name": "io.github.ChromeDevTools/chrome-devtools-mcp",
				"title": "Chrome DevTools",
				"description": "Chrome DevTools automation",
				"version": "0.11.0",
				"hasRemotes": false,
				"hasPackages": true,
				"packages": [{"registryType": "npm", "identifier": "chrome-devtools-mcp", "transport": "stdio"}],
				"remotes": [],
				"envVars": []
			}
		],
		"nextCursor": "abc123",
		"count": 2
	}`
}

func TestSearchRegistryForwardsParams(t *testing.T) {
	var gotPath, gotRawQuery, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotRawQuery, gotMethod = r.URL.Path, r.URL.RawQuery, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"servers":[],"count":0}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.SearchRegistry(context.Background(), "chrome-devtools", 10, "cursor-1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-registry/search" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/admin/mcp-registry/search", gotMethod, gotPath)
	}
	q, err := url.ParseQuery(gotRawQuery)
	if err != nil {
		t.Fatalf("parse query %q: %v", gotRawQuery, err)
	}
	if q.Get("q") != "chrome-devtools" || q.Get("limit") != "10" || q.Get("cursor") != "cursor-1" {
		t.Errorf("query = %q, want q=chrome-devtools limit=10 cursor=cursor-1", gotRawQuery)
	}
}

func TestSearchRegistryEmptyParamsOmitted(t *testing.T) {
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"servers":[],"count":0}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.SearchRegistry(context.Background(), "", 0, ""); err != nil {
		t.Fatal(err)
	}
	if gotRawQuery != "" {
		t.Errorf("raw query = %q, want empty (no params)", gotRawQuery)
	}
}

func TestSearchRegistryDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":`+mcpRegistrySearchFixture()+`}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.SearchRegistry(context.Background(), "github", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 2 || res.NextCursor != "abc123" {
		t.Errorf("envelope = count %d next %q, want 2 abc123", res.Count, res.NextCursor)
	}
	if len(res.Servers) != 2 {
		t.Fatalf("servers = %d, want 2", len(res.Servers))
	}
	gh := res.Servers[0]
	if gh.Name != "io.github.github/github-mcp-server" || gh.Title != "GitHub" || !gh.HasRemotes || !gh.HasPackages {
		t.Errorf("github entry = %+v", gh)
	}
	if gh.Repository == nil || gh.Repository.Source != "github" {
		t.Errorf("github repository = %+v", gh.Repository)
	}
	if len(gh.Remotes) != 1 || gh.Remotes[0].Type != "streamable-http" {
		t.Errorf("github remotes = %+v", gh.Remotes)
	}
	if len(gh.EnvVars) != 1 || gh.EnvVars[0].Name != "GITHUB_PERSONAL_ACCESS_TOKEN" || !gh.EnvVars[0].IsRequired || !gh.EnvVars[0].IsSecret {
		t.Errorf("github envVars = %+v", gh.EnvVars)
	}
	dev := res.Servers[1]
	if dev.HasRemotes || !dev.HasPackages {
		t.Errorf("chrome-devtools entry must be stdio-only: %+v", dev)
	}
}

func TestSearchRegistryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"failed to search MCP registry"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.SearchRegistry(context.Background(), "x", 0, ""); err == nil ||
		!strings.Contains(err.Error(), "memory 500 internal") {
		t.Fatalf("error = %v, want memory 500 internal", err)
	}
}

func TestSearchRegistryDoesNotRetry502(t *testing.T) {
	// A slow/hanging registry makes memory answer 502 registry_unavailable
	// after its full timeout. That 502 is not a transient blip, so the browse
	// proxy must not replay it: one upstream 502 must cost exactly one attempt,
	// not maxMemoryAttempts × the upstream timeout.
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"registry_unavailable","message":"upstream registry timed out"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.SearchRegistry(context.Background(), "github", 0, ""); err == nil {
		t.Fatal("SearchRegistry() error = nil, want non-nil")
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 (registry 502 must not retry)", got)
	}
}

func TestInstallRegistryServer(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{
			"server":{"id":"srv-1","name":"github-mcp-server","type":"http","enabled":true},
			"requiredEnvVars":[{"name":"GITHUB_PERSONAL_ACCESS_TOKEN","isRequired":true,"isSecret":true}],
			"message":"Server installed."
		}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.InstallRegistryServer(context.Background(), "io.github.github/github-mcp-server", "1.0.0", "gh")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-registry/install" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/admin/mcp-registry/install", gotMethod, gotPath)
	}
	if gotBody["registryName"] != "io.github.github/github-mcp-server" ||
		gotBody["version"] != "1.0.0" || gotBody["name"] != "gh" {
		t.Errorf("body = %v, want registryName/version/name", gotBody)
	}
	if len(res.RequiredEnvVars) != 1 || res.RequiredEnvVars[0].Name != "GITHUB_PERSONAL_ACCESS_TOKEN" {
		t.Errorf("requiredEnvVars = %+v", res.RequiredEnvVars)
	}
	if res.Server == nil || res.Server.Name != "github-mcp-server" {
		t.Errorf("server = %+v", res.Server)
	}
}

func TestInstallRegistryServerOmitsOptionalFields(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"server":{"id":"srv-1"},"requiredEnvVars":[]}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.InstallRegistryServer(context.Background(), "io.github.x/y", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotBody["version"]; ok {
		t.Errorf("body must omit empty version, got %v", gotBody)
	}
	if _, ok := gotBody["name"]; ok {
		t.Errorf("body must omit empty name, got %v", gotBody)
	}
	if gotBody["registryName"] != "io.github.x/y" {
		t.Errorf("body.registryName = %v", gotBody["registryName"])
	}
}

// TestInstallRegistryServerToDevice asserts the device install posts the target
// fields, so memory resolves the entry's local package and pushes it over the
// relay rather than installing an outbound server.
func TestInstallRegistryServerToDevice(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"server":{"id":"srv-2","name":"chrome-devtools-mcp","type":"stdio"}}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.InstallRegistryServerToDevice(context.Background(),
		"io.github.ChromeDevTools/chrome-devtools-mcp", "0.11.0", "chrome-devtools-mcp", "dev-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-registry/install" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/admin/mcp-registry/install", gotMethod, gotPath)
	}
	if gotBody["target"] != "device" || gotBody["targetInstanceId"] != "dev-1" {
		t.Errorf("body = %v, want target=device targetInstanceId=dev-1", gotBody)
	}
	if gotBody["registryName"] != "io.github.ChromeDevTools/chrome-devtools-mcp" {
		t.Errorf("body.registryName = %v", gotBody["registryName"])
	}
	if res.Server == nil || res.Server.ID != "srv-2" {
		t.Errorf("server = %+v", res.Server)
	}
}

func TestInstallRegistryServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"bad_request","message":"only offers stdio-based packages"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.InstallRegistryServer(context.Background(), "io.github.x/y", "", ""); err == nil ||
		!strings.Contains(err.Error(), "only offers stdio-based packages") {
		t.Fatalf("error = %v, want the stdio-only rejection", err)
	}
}

// TestInstallRegistryServerRequiresAuthorization asserts the install result
// decodes the requiresAuthorization flag (an OAuth-required server installs
// with no env/config, so the UI must point the admin at the Connect action).
func TestInstallRegistryServerRequiresAuthorization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{
			"server":{"id":"srv-1","name":"github-mcp-server","type":"http","enabled":true},
			"requiredEnvVars":[],
			"requiresAuthorization":true,
			"message":"Server installed."
		}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.InstallRegistryServer(context.Background(), "io.github.github/github-mcp-server", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.RequiresAuthorization {
		t.Errorf("requiresAuthorization = false, want true")
	}
	if len(res.RequiredEnvVars) != 0 {
		t.Errorf("requiredEnvVars = %+v, want none for an OAuth-required server", res.RequiredEnvVars)
	}
	if res.Server == nil || res.Server.Name != "github-mcp-server" {
		t.Errorf("server = %+v", res.Server)
	}
}
