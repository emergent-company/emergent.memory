package mgmtapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
)

// --- harness -----------------------------------------------------------------

// fakeReloader records Reload calls and returns deterministic per-server
// statuses derived from the config it is handed. No real MCP server is touched.
type fakeReloader struct {
	mu    sync.Mutex
	calls int
	last  []mcphost.ServerConfig
	err   error
}

func (f *fakeReloader) Reload(_ context.Context, cfg *config.Config) ([]mcphost.ServerStatus, error) {
	f.mu.Lock()
	f.calls++
	f.last = append([]mcphost.ServerConfig(nil), cfg.MCPServers...)
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return statusesFor(cfg.MCPServers), nil
}

func (f *fakeReloader) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeReloader) lastConfig() []mcphost.ServerConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mcphost.ServerConfig(nil), f.last...)
}

func (f *fakeReloader) setError(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

// statusesFor builds one enabled, connected status with a single fake tool for
// each enabled server, mirroring what mcphost.Manager.Status would report.
func statusesFor(servers []mcphost.ServerConfig) []mcphost.ServerStatus {
	out := make([]mcphost.ServerStatus, 0, len(servers))
	for _, sc := range servers {
		tools := []mcphost.ToolStatus{}
		if sc.IsEnabled() {
			tools = append(tools, mcphost.ToolStatus{
				Name:        sc.Name + "_tool1",
				Description: "tool one for " + sc.Name,
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			})
		}
		out = append(out, mcphost.ServerStatus{
			Name:      sc.Name,
			Transport: sc.Transport,
			Enabled:   sc.IsEnabled(),
			Connected: sc.IsEnabled(),
			ToolCount: len(tools),
			Tools:     tools,
		})
	}
	return out
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const baseConfigYAML = "server_url: https://memory.example.test\n" +
	"token: emt_projecttoken\n" +
	"instance_id: test-connector\n"

type harness struct {
	server   *Server
	ts       *httptest.Server
	config   string
	reloader *fakeReloader
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(baseConfigYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	rl := &fakeReloader{}
	s := NewServer(path, cfg, quietLogger(), rl.Reload)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &harness{server: s, ts: ts, config: path, reloader: rl}
}

// do issues a request with an optional JSON body and returns the status code
// and body, draining and closing the response body.
func (h *harness) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, data
}

func (h *harness) requireStatus(t *testing.T, code, want int) {
	t.Helper()
	if code != want {
		t.Fatalf("status = %d, want %d", code, want)
	}
}

func decodeInto[T any](t *testing.T, data []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	return out
}

// rawConfig reads the config file off disk.
func (h *harness) rawConfig(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(h.config)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return b
}

// --- tests -------------------------------------------------------------------

func TestCRUDLifecyclePersistsAndReloads(t *testing.T) {
	h := newHarness(t)

	// Empty list starts as an empty JSON array, not null.
	code, data := h.do(t, http.MethodGet, "/api/mcp-servers", nil)
	h.requireStatus(t, code, http.StatusOK)
	if got := strings.TrimSpace(string(data)); got != "[]" {
		t.Fatalf("empty list body = %q, want []", got)
	}

	// Create.
	createBody := map[string]any{
		"name":           "gh",
		"transport":      "stdio",
		"command":        "gh-mcp",
		"args":           []string{"--stdio"},
		"env":            map[string]string{"GH_TOKEN": "secret"},
		"disabled_tools": []string{"delete_repo"},
	}
	code, data = h.do(t, http.MethodPost, "/api/mcp-servers", createBody)
	h.requireStatus(t, code, http.StatusCreated)
	created := decodeInto[serverDetail](t, data)
	if created.Name != "gh" || created.Transport != mcphost.TransportStdio || !created.Enabled {
		t.Fatalf("created detail = %+v", created)
	}
	if created.Command != "gh-mcp" || created.Env["GH_TOKEN"] != "secret" {
		t.Fatalf("created config not echoed: %+v", created)
	}
	if h.reloader.callCount() != 1 {
		t.Fatalf("reload calls = %d, want 1", h.reloader.callCount())
	}
	last := h.reloader.lastConfig()
	if len(last) != 1 || last[0].Name != "gh" || last[0].Command != "gh-mcp" {
		t.Fatalf("reloader last config = %+v", last)
	}

	// Persisted to disk.
	onDisk := loadConfig(t, h.config)
	if len(onDisk.MCPServers) != 1 || onDisk.MCPServers[0].Name != "gh" {
		t.Fatalf("on-disk servers = %+v", onDisk.MCPServers)
	}

	// List reflects status (connected + tool count from the reloader).
	code, data = h.do(t, http.MethodGet, "/api/mcp-servers", nil)
	h.requireStatus(t, code, http.StatusOK)
	list := decodeInto[[]serverSummary](t, data)
	if len(list) != 1 || list[0].Name != "gh" || !list[0].Connected || list[0].ToolCount != 1 {
		t.Fatalf("list = %+v", list)
	}

	// Detail includes config + status.tools.
	code, data = h.do(t, http.MethodGet, "/api/mcp-servers/gh", nil)
	h.requireStatus(t, code, http.StatusOK)
	detail := decodeInto[serverDetail](t, data)
	if detail.Command != "gh-mcp" || len(detail.Status.Tools) != 1 || detail.Status.Tools[0].Name != "gh_tool1" {
		t.Fatalf("detail = %+v", detail)
	}

	// Tools endpoint serves the status-provided tools.
	_, data = h.do(t, http.MethodGet, "/api/mcp-servers/gh/tools", nil)
	tools := decodeInto[[]mcphost.ToolStatus](t, data)
	if len(tools) != 1 || tools[0].Name != "gh_tool1" || tools[0].Description == "" {
		t.Fatalf("tools = %+v", tools)
	}

	// Replace config.
	code, data = h.do(t, http.MethodPut, "/api/mcp-servers/gh/config", map[string]any{
		"transport": "http",
		"url":       "https://gh.example.test/mcp",
		"headers":   map[string]string{"Authorization": "Bearer x"},
	})
	h.requireStatus(t, code, http.StatusOK)
	replaced := decodeInto[serverDetail](t, data)
	if replaced.Transport != mcphost.TransportHTTP || replaced.URL != "https://gh.example.test/mcp" {
		t.Fatalf("replaced detail = %+v", replaced)
	}
	if replaced.Command != "" {
		t.Fatalf("replace should drop stdio command, got %q", replaced.Command)
	}
	if h.reloader.callCount() != 2 {
		t.Fatalf("reload calls after replace = %d, want 2", h.reloader.callCount())
	}
	onDisk = loadConfig(t, h.config)
	if len(onDisk.MCPServers) != 1 || onDisk.MCPServers[0].Transport != mcphost.TransportHTTP {
		t.Fatalf("on-disk after replace = %+v", onDisk.MCPServers)
	}

	// Toggle enabled.
	code, data = h.do(t, http.MethodPut, "/api/mcp-servers/gh/enabled", map[string]any{"enabled": false})
	h.requireStatus(t, code, http.StatusOK)
	toggled := decodeInto[serverDetail](t, data)
	if toggled.Enabled || toggled.Status.Connected {
		t.Fatalf("disabled detail = %+v", toggled)
	}
	if h.reloader.callCount() != 3 {
		t.Fatalf("reload calls after toggle = %d, want 3", h.reloader.callCount())
	}

	// Runtime status is surfaced by /api/status.
	h.server.SetRuntimeStatus(func() RuntimeStatus {
		return RuntimeStatus{Connected: true, ToolCount: 42}
	})
	code, data = h.do(t, http.MethodGet, "/api/status", nil)
	h.requireStatus(t, code, http.StatusOK)
	st := decodeInto[statusResponse](t, data)
	if st.InstanceID != "test-connector" || st.ServerURL != "https://memory.example.test" {
		t.Fatalf("status identity = %+v", st)
	}
	if !st.Connected || st.ToolCount != 42 {
		t.Fatalf("status runtime = %+v", st)
	}
	if len(st.Servers) != 1 || st.Servers[0].Enabled {
		t.Fatalf("status servers = %+v", st.Servers)
	}

	// Delete.
	code, data = h.do(t, http.MethodDelete, "/api/mcp-servers/gh", nil)
	h.requireStatus(t, code, http.StatusNoContent)
	if len(data) != 0 {
		t.Fatalf("delete body = %q, want empty", data)
	}
	code, data = h.do(t, http.MethodGet, "/api/mcp-servers", nil)
	h.requireStatus(t, code, http.StatusOK)
	if got := strings.TrimSpace(string(data)); got != "[]" {
		t.Fatalf("list after delete = %q, want []", got)
	}
	if h.reloader.callCount() != 4 {
		t.Fatalf("reload calls after delete = %d, want 4", h.reloader.callCount())
	}
	onDisk = loadConfig(t, h.config)
	if len(onDisk.MCPServers) != 0 {
		t.Fatalf("on-disk after delete = %+v", onDisk.MCPServers)
	}
}

// loadConfig reads and validates the config file at path.
func loadConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config %s: %v", path, err)
	}
	return cfg
}

func TestValidationRejectedWithoutSideEffects(t *testing.T) {
	h := newHarness(t)
	before := h.rawConfig(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   map[string]any
		want   int
	}{
		{
			name:   "create unknown transport",
			method: http.MethodPost,
			path:   "/api/mcp-servers",
			body:   map[string]any{"name": "x", "transport": "carrier-pigeon"},
			want:   http.StatusBadRequest,
		},
		{
			name:   "create stdio missing command",
			method: http.MethodPost,
			path:   "/api/mcp-servers",
			body:   map[string]any{"name": "x", "transport": "stdio"},
			want:   http.StatusBadRequest,
		},
		{
			name:   "create http missing url",
			method: http.MethodPost,
			path:   "/api/mcp-servers",
			body:   map[string]any{"name": "x", "transport": "http"},
			want:   http.StatusBadRequest,
		},
		{
			name:   "create missing transport",
			method: http.MethodPost,
			path:   "/api/mcp-servers",
			body:   map[string]any{"name": "x", "command": "c"},
			want:   http.StatusBadRequest,
		},
		{
			name:   "create missing name",
			method: http.MethodPost,
			path:   "/api/mcp-servers",
			body:   map[string]any{"transport": "stdio", "command": "c"},
			want:   http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, data := h.do(t, tc.method, tc.path, tc.body)
			h.requireStatus(t, code, tc.want)
			var errBody map[string]string
			if err := json.Unmarshal(data, &errBody); err != nil || errBody["error"] == "" {
				t.Fatalf("error body = %s, want JSON {\"error\":...}", data)
			}
			if got := h.rawConfig(t); !bytes.Equal(got, before) {
				t.Fatalf("invalid request changed config file:\nbefore=%s\nafter=%s", before, got)
			}
			if h.reloader.callCount() != 0 {
				t.Fatalf("reloader called %d times for invalid config, want 0", h.reloader.callCount())
			}
		})
	}

	// In-memory config is unchanged too.
	code, data := h.do(t, http.MethodGet, "/api/mcp-servers", nil)
	h.requireStatus(t, code, http.StatusOK)
	if got := strings.TrimSpace(string(data)); got != "[]" {
		t.Fatalf("list after invalid creates = %q, want []", got)
	}

	// Duplicate name -> 409 and no disk change.
	code, _ = h.do(t, http.MethodPost, "/api/mcp-servers", map[string]any{
		"name": "dup", "transport": "stdio", "command": "c",
	})
	h.requireStatus(t, code, http.StatusCreated)
	afterCreate := h.rawConfig(t)
	code, _ = h.do(t, http.MethodPost, "/api/mcp-servers", map[string]any{
		"name": "dup", "transport": "stdio", "command": "other",
	})
	h.requireStatus(t, code, http.StatusConflict)
	if !bytes.Equal(h.rawConfig(t), afterCreate) {
		t.Fatal("duplicate create changed config file")
	}
	if h.reloader.callCount() != 1 {
		t.Fatalf("reload calls after duplicate = %d, want 1", h.reloader.callCount())
	}

	// Invalid replace -> 400 and no disk change.
	code, _ = h.do(t, http.MethodPut, "/api/mcp-servers/dup/config", map[string]any{
		"transport": "nope",
	})
	h.requireStatus(t, code, http.StatusBadRequest)
	if !bytes.Equal(h.rawConfig(t), afterCreate) {
		t.Fatal("invalid replace changed config file")
	}
	if h.reloader.callCount() != 1 {
		t.Fatalf("reload calls after invalid replace = %d, want 1", h.reloader.callCount())
	}
}

func TestNotFound(t *testing.T) {
	h := newHarness(t)
	// Seed one server so "missing" is genuinely absent.
	code, _ := h.do(t, http.MethodPost, "/api/mcp-servers", map[string]any{
		"name": "present", "transport": "stdio", "command": "c",
	})
	h.requireStatus(t, code, http.StatusCreated)
	before := h.rawConfig(t)

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/mcp-servers/missing", nil},
		{http.MethodPut, "/api/mcp-servers/missing/config", map[string]any{"transport": "stdio", "command": "c"}},
		{http.MethodPut, "/api/mcp-servers/missing/enabled", map[string]any{"enabled": true}},
		{http.MethodDelete, "/api/mcp-servers/missing", nil},
		{http.MethodGet, "/api/mcp-servers/missing/tools", nil},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			code, data := h.do(t, tc.method, tc.path, tc.body)
			h.requireStatus(t, code, http.StatusNotFound)
			var errBody map[string]string
			if err := json.Unmarshal(data, &errBody); err != nil || errBody["error"] == "" {
				t.Fatalf("error body = %s, want JSON error", data)
			}
		})
	}
	if !bytes.Equal(h.rawConfig(t), before) {
		t.Fatal("404 requests changed the config file")
	}
}

func TestMethodAndRouteBehavior(t *testing.T) {
	h := newHarness(t)

	cases := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"delete collection", http.MethodDelete, "/api/mcp-servers", http.StatusMethodNotAllowed},
		{"post item", http.MethodPost, "/api/mcp-servers/x", http.StatusMethodNotAllowed},
		{"get config subresource", http.MethodGet, "/api/mcp-servers/x/config", http.StatusMethodNotAllowed},
		{"post enabled subresource", http.MethodPost, "/api/mcp-servers/x/enabled", http.StatusMethodNotAllowed},
		{"delete tools subresource", http.MethodDelete, "/api/mcp-servers/x/tools", http.StatusMethodNotAllowed},
		{"unknown route", http.MethodGet, "/api/nope", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := h.do(t, tc.method, tc.path, nil)
			h.requireStatus(t, code, tc.want)
		})
	}
}

func TestStatusesAndRuntimeReflected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	cfg := &config.Config{
		ServerURL:  "https://memory.example.test",
		Token:      "emt_x",
		InstanceID: "conn",
		MCPServers: []mcphost.ServerConfig{
			{Name: "off", Transport: mcphost.TransportStdio, Command: "c"},
		},
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save config: %v", err)
	}
	s := NewServer(path, cfg, quietLogger(), func(context.Context, *config.Config) ([]mcphost.ServerStatus, error) {
		return nil, nil
	})
	s.SetStatuses([]mcphost.ServerStatus{{
		Name:      "off",
		Transport: mcphost.TransportStdio,
		Enabled:   true,
		Connected: false,
		Error:     "connection refused",
		ToolCount: 2,
		Tools: []mcphost.ToolStatus{
			{Name: "off_a", Description: "a", InputSchema: map[string]any{"type": "object"}},
			{Name: "off_b", Description: "b", InputSchema: map[string]any{"type": "object"}},
		},
	}})
	s.SetRuntimeStatus(func() RuntimeStatus { return RuntimeStatus{Connected: true, ToolCount: 9} })
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	get := func(path string) (int, []byte) {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}

	code, data := get("/api/mcp-servers")
	if code != http.StatusOK {
		t.Fatalf("list code = %d", code)
	}
	list := decodeInto[[]serverSummary](t, data)
	if len(list) != 1 || list[0].Error != "connection refused" || list[0].ToolCount != 2 || list[0].Connected {
		t.Fatalf("list = %+v", list)
	}

	code, data = get("/api/mcp-servers/off/tools")
	if code != http.StatusOK {
		t.Fatalf("tools code = %d", code)
	}
	tools := decodeInto[[]mcphost.ToolStatus](t, data)
	if len(tools) != 2 || tools[0].Name != "off_a" {
		t.Fatalf("tools = %+v", tools)
	}

	code, data = get("/api/status")
	if code != http.StatusOK {
		t.Fatalf("status code = %d", code)
	}
	st := decodeInto[statusResponse](t, data)
	if !st.Connected || st.ToolCount != 9 || len(st.Servers) != 1 || st.Servers[0].Error != "connection refused" {
		t.Fatalf("status = %+v", st)
	}
}

func TestReloadErrorReturns500(t *testing.T) {
	h := newHarness(t)
	h.reloader.setError(fmt.Errorf("boom"))
	code, data := h.do(t, http.MethodPost, "/api/mcp-servers", map[string]any{
		"name": "x", "transport": "stdio", "command": "c",
	})
	h.requireStatus(t, code, http.StatusInternalServerError)
	var errBody map[string]string
	if err := json.Unmarshal(data, &errBody); err != nil || !strings.Contains(errBody["error"], "boom") {
		t.Fatalf("error body = %s, want apply error", data)
	}
}

// TestConcurrentReadsWhileMutating exercises the Server's locking under the
// race detector: many readers hammer the read endpoints while CRUD mutations
// commit. No sleeps; readers stop when the writer closes the stop channel.
func TestConcurrentReadsWhileMutating(t *testing.T) {
	h := newHarness(t)
	code, _ := h.do(t, http.MethodPost, "/api/mcp-servers", map[string]any{
		"name": "seed", "transport": "stdio", "command": "c",
	})
	h.requireStatus(t, code, http.StatusCreated)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	paths := []string{"/api/mcp-servers", "/api/mcp-servers/seed", "/api/status"}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := h.ts.Client()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, p := range paths {
					resp, err := client.Get(h.ts.URL + p)
					if err != nil {
						t.Errorf("concurrent GET %s: %v", p, err)
						return
					}
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}
		}()
	}

	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("s%02d", i)
		code, _ := h.do(t, http.MethodPost, "/api/mcp-servers", map[string]any{
			"name": name, "transport": "stdio", "command": "c",
		})
		if code != http.StatusCreated {
			t.Fatalf("create %s code = %d", name, code)
		}
		code, _ = h.do(t, http.MethodPut, "/api/mcp-servers/"+name+"/enabled", map[string]any{"enabled": false})
		if code != http.StatusOK {
			t.Fatalf("disable %s code = %d", name, code)
		}
		code, _ = h.do(t, http.MethodDelete, "/api/mcp-servers/"+name, nil)
		if code != http.StatusNoContent {
			t.Fatalf("delete %s code = %d", name, code)
		}
	}
	close(stop)
	wg.Wait()
}

// TestRegisterRoutesOnInjectedMux verifies the routes are installed on a
// caller-owned mux (the documented integration point) and answer JSON.
func TestRegisterRoutesOnInjectedMux(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	cfg := &config.Config{ServerURL: "https://x.test", Token: "t", InstanceID: "i"}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	s := NewServer(path, cfg, quietLogger(), func(context.Context, *config.Config) ([]mcphost.ServerStatus, error) {
		return nil, nil
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/mcp-servers")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("code = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}
