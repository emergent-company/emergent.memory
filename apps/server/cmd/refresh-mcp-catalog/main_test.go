package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// githubResponse wraps a server object in the GitHub MCP-registry
// `?format=json` envelope, mirroring the fixed
// payload.mcpDetailsRoute.server_data.raw_data.server path.
func githubResponse(server map[string]any) map[string]any {
	return map[string]any{
		"payload": map[string]any{
			"mcpDetailsRoute": map[string]any{
				"server_data": map[string]any{
					"raw_data": map[string]any{
						"server": server,
					},
				},
			},
		},
	}
}

// officialResponse wraps a server object in the official MCP registry
// envelope: {"server": {...}, "_meta": {...}}.
func officialResponse(server map[string]any) map[string]any {
	return map[string]any{
		"server": server,
		"_meta":  map[string]any{"sidecar": true},
	}
}

func TestParseSources(t *testing.T) {
	input := strings.Join([]string{
		"# curated list",
		"",
		"github/github-mcp-server",
		"  microsoft/playwright-mcp  ",
		"\tChromeDevTools/chrome-devtools-mcp",
		"   # indented comment",
		"com.supabase/mcp",
		"",
	}, "\n")

	got, err := parseSources(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseSources: %v", err)
	}
	want := []string{
		"github/github-mcp-server",
		"microsoft/playwright-mcp",
		"ChromeDevTools/chrome-devtools-mcp",
		"com.supabase/mcp",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseSourcesKeepsOfficialPrefix(t *testing.T) {
	input := strings.Join([]string{
		"# curated list",
		"",
		"official:io.github.homeassistant-ai/ha-mcp",
		"github/github-mcp-server",
		"",
	}, "\n")

	got, err := parseSources(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseSources: %v", err)
	}
	want := []string{
		"official:io.github.homeassistant-ai/ha-mcp",
		"github/github-mcp-server",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractServerStripsMeta(t *testing.T) {
	body, err := json.Marshal(githubResponse(map[string]any{
		"name":    "io.test.x/y",
		"$schema": "https://example/schema.json",
		"_meta":   map[string]any{"secret": true},
	}))
	if err != nil {
		t.Fatal(err)
	}

	srv, err := extractServer(body)
	if err != nil {
		t.Fatalf("extractServer: %v", err)
	}
	if _, ok := srv["_meta"]; ok {
		t.Errorf("_meta should be stripped")
	}
	if srv["name"] != "io.test.x/y" {
		t.Errorf("name = %v", srv["name"])
	}
	if srv["$schema"] != "https://example/schema.json" {
		t.Errorf("$schema should be preserved, got %v", srv["$schema"])
	}
}

func TestExtractServerMissingName(t *testing.T) {
	body, err := json.Marshal(githubResponse(map[string]any{"description": "no name"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extractServer(body); err == nil {
		t.Fatal("expected error for a server missing a name")
	}
}

func TestExtractServerMissingServerData(t *testing.T) {
	if _, err := extractServer([]byte(`{"payload":{"mcpDetailsRoute":{"server_data":{"raw_data":{}}}}}`)); err == nil {
		t.Fatal("expected error when payload has no server data")
	}
}

func TestExtractOfficialServerStripsMeta(t *testing.T) {
	body, err := json.Marshal(officialResponse(map[string]any{
		"name":    "io.github.homeassistant-ai/ha-mcp",
		"$schema": "https://example/schema.json",
		"_meta":   map[string]any{"secret": true},
	}))
	if err != nil {
		t.Fatal(err)
	}

	srv, err := extractOfficialServer(body)
	if err != nil {
		t.Fatalf("extractOfficialServer: %v", err)
	}
	if _, ok := srv["_meta"]; ok {
		t.Errorf("_meta should be stripped")
	}
	if srv["name"] != "io.github.homeassistant-ai/ha-mcp" {
		t.Errorf("name = %v", srv["name"])
	}
	if srv["$schema"] != "https://example/schema.json" {
		t.Errorf("$schema should be preserved, got %v", srv["$schema"])
	}
}

func TestExtractOfficialServerMissingName(t *testing.T) {
	body, err := json.Marshal(officialResponse(map[string]any{"description": "no name"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extractOfficialServer(body); err == nil {
		t.Fatal("expected error for an official server missing a name")
	}
}

func TestExtractOfficialServerMissingServerData(t *testing.T) {
	if _, err := extractOfficialServer([]byte(`{}`)); err == nil {
		t.Fatal("expected error when payload has no server data")
	}
}

func TestRefreshCatalogEndToEnd(t *testing.T) {
	servers := map[string]map[string]any{
		"a/one": {
			"name":        "io.test.a/one",
			"description": "first",
			"_meta":       map[string]any{"secret": "strip-me"},
			"$schema":     "https://example/schema.json",
		},
		"b/two": {
			"name":        "io.test.b/two",
			"description": "second",
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("missing format=json query param")
		}
		slug := strings.TrimPrefix(r.URL.Path, "/")
		srv, ok := servers[slug]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(githubResponse(srv)); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer ts.Close()

	dir := t.TempDir()
	src := filepath.Join(dir, "sources.txt")
	if err := os.WriteFile(src, []byte("# header\na/one\n\nb/two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "servers.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-sources", src, "-out", out, "-base-url", ts.URL}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}

	var result struct {
		Servers []map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Servers) != 2 {
		t.Fatalf("len(servers) = %d, want 2", len(result.Servers))
	}
	if result.Servers[0]["name"] != "io.test.a/one" {
		t.Errorf("servers[0].name = %v (order must match sources.txt)", result.Servers[0]["name"])
	}
	if _, ok := result.Servers[0]["_meta"]; ok {
		t.Errorf("_meta not stripped from output")
	}
	if result.Servers[0]["$schema"] != "https://example/schema.json" {
		t.Errorf("$schema lost: %v", result.Servers[0]["$schema"])
	}
	if !strings.Contains(stdout.String(), "wrote 2 servers") {
		t.Errorf("stdout = %q, want summary", stdout.String())
	}
}

func TestRefreshCatalogFailureDoesNotWrite(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	dir := t.TempDir()
	src := filepath.Join(dir, "sources.txt")
	if err := os.WriteFile(src, []byte("a/one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "servers.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-sources", src, "-out", out, "-base-url", ts.URL}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero; stderr = %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "404") {
		t.Errorf("stderr should mention 404, got %q", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("out file must not be written on failure, stat err = %v", err)
	}
}

func TestRefreshCatalogMixedSourcesEndToEnd(t *testing.T) {
	ghServers := map[string]map[string]any{
		"a/one": {"name": "io.test.a/one", "description": "first"},
		"b/two": {"name": "io.test.b/two", "description": "second"},
	}
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("missing format=json query param")
		}
		slug := strings.TrimPrefix(r.URL.Path, "/")
		srv, ok := ghServers[slug]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(githubResponse(srv)); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer gh.Close()

	const haMCPName = "io.github.homeassistant-ai/ha-mcp"
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		esc := r.URL.EscapedPath()
		if !strings.Contains(esc, "%2F") {
			t.Errorf("registry path must escape the slash: %q", esc)
		}
		if !strings.HasSuffix(esc, "/versions/latest") {
			t.Errorf("registry path must end in /versions/latest: %q", esc)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(officialResponse(map[string]any{
			"name":        haMCPName,
			"description": "Home Assistant",
			"_meta":       map[string]any{"strip-me": true},
		})); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	defer reg.Close()

	dir := t.TempDir()
	src := filepath.Join(dir, "sources.txt")
	if err := os.WriteFile(src, []byte("# header\na/one\nofficial:io.github.homeassistant-ai/ha-mcp\nb/two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "servers.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"-sources", src,
		"-out", out,
		"-base-url", gh.URL,
		"-registry-base-url", reg.URL,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}

	var result struct {
		Servers []map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Servers) != 3 {
		t.Fatalf("len(servers) = %d, want 3", len(result.Servers))
	}
	wantOrder := []string{"io.test.a/one", haMCPName, "io.test.b/two"}
	for i, want := range wantOrder {
		if result.Servers[i]["name"] != want {
			t.Errorf("servers[%d].name = %v, want %q (order must match sources.txt)", i, result.Servers[i]["name"], want)
		}
	}
	if _, ok := result.Servers[1]["_meta"]; ok {
		t.Errorf("_meta not stripped from official entry")
	}
	if !strings.Contains(stdout.String(), "wrote 3 servers") {
		t.Errorf("stdout = %q, want summary", stdout.String())
	}
}

func TestRefreshCatalogOfficialFailureDoesNotWrite(t *testing.T) {
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer reg.Close()

	dir := t.TempDir()
	src := filepath.Join(dir, "sources.txt")
	if err := os.WriteFile(src, []byte("official:io.github.homeassistant-ai/ha-mcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "servers.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"-sources", src,
		"-out", out,
		"-base-url", reg.URL,
		"-registry-base-url", reg.URL,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero; stderr = %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "404") {
		t.Errorf("stderr should mention 404, got %q", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("out file must not be written on failure, stat err = %v", err)
	}
}
