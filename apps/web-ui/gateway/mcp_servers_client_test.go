package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- MCP server sync / inspect / tool-management client methods ---
//
// These cover the MemoryClient proxies for memory's mcpregistry admin routes
// (POST /:id/sync, POST /:id/inspect, GET /:id/tools, PATCH
// /:id/tools/:toolId). Like the CRUD methods in extras.go they hit
// /api/admin/mcp-servers with a plain context — the static credentials apply.

// inspectFixture is one memory inspect DTO sample (MCPServerInspectDTO) shared
// by the inspect tests.
func mcpInspectFixture() string {
	return `{
		"serverId":"srv-1",
		"serverName":"github",
		"serverType":"http",
		"status":"ok",
		"latencyMs":42,
		"serverInfo":{"name":"github-mcp-server","version":"1.2.3","protocolVersion":"2025-03-26"},
		"capabilities":{"tools":true,"prompts":false,"resources":false,"logging":false,"completions":false},
		"tools":[{"name":"create_issue","description":"Create a GitHub issue","inputSchema":{"type":"object"}}],
		"prompts":[],
		"resources":[],
		"resourceTemplates":[]
	}`
}

func TestSyncMCPServer(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"message":"synced 3 tools successfully"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.SyncMCPServer(context.Background(), "srv-1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/sync" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/admin/mcp-servers/srv-1/sync", gotMethod, gotPath)
	}
}

func TestSyncMCPServerSendsNoBody(t *testing.T) {
	bodySeen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodySeen = len(raw) > 0 // auto-discover mode must send an empty body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"message":"synced 3 tools successfully"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.SyncMCPServer(context.Background(), "srv-1"); err != nil {
		t.Fatal(err)
	}
	if bodySeen {
		t.Error("SyncMCPServer sent a body; auto-discover requires an empty body")
	}
}

func TestSyncMCPServerNotFound404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"mcp_server not found"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.SyncMCPServer(context.Background(), "missing"); err == nil ||
		!strings.Contains(err.Error(), "memory 404 not_found") {
		t.Fatalf("error = %v, want memory 404 not_found", err)
	}
}

func TestInspectMCPServer(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":`+mcpInspectFixture()+`}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	res, err := m.InspectMCPServer(context.Background(), "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/inspect" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/admin/mcp-servers/srv-1/inspect", gotMethod, gotPath)
	}
	if res.ServerID != "srv-1" || res.ServerName != "github" || res.ServerType != "http" {
		t.Errorf("identity = %+v", res)
	}
	if res.Status != "ok" || res.LatencyMs != 42 {
		t.Errorf("connection = status %q latency %d, want ok 42", res.Status, res.LatencyMs)
	}
	if res.ServerInfo == nil || res.ServerInfo.Name != "github-mcp-server" || res.ServerInfo.Version != "1.2.3" {
		t.Errorf("serverInfo = %+v", res.ServerInfo)
	}
	if res.Capabilities == nil || !res.Capabilities.Tools || res.Capabilities.Prompts {
		t.Errorf("capabilities = %+v", res.Capabilities)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "create_issue" || res.Tools[0].Description != "Create a GitHub issue" {
		t.Errorf("tools = %+v", res.Tools)
	}
	if res.Tools[0].InputSchema == nil {
		t.Error("tool inputSchema should decode")
	}
	if res.Error != nil {
		t.Errorf("error = %v, want nil on a successful inspect", *res.Error)
	}
}

// TestInspectMCPServerConnectionError asserts inspect surfaces a failed
// connection through the body — memory answers 200 with status "error" and the
// error text, not an HTTP error status.
func TestInspectMCPServerConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{
			"serverId":"srv-1","serverName":"dead","serverType":"http",
			"status":"error","error":"dial tcp: connection refused","latencyMs":5,
			"tools":[],"prompts":[],"resources":[],"resourceTemplates":[]
		}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	res, err := m.InspectMCPServer(context.Background(), "srv-1")
	if err != nil {
		t.Fatalf("inspect must not error on 200 even when the connection failed: %v", err)
	}
	if res.Status != "error" {
		t.Errorf("status = %q, want error", res.Status)
	}
	if res.Error == nil || !strings.Contains(*res.Error, "connection refused") {
		t.Errorf("error = %v, want connection refused", res.Error)
	}
}

func TestInspectMCPServerInternal500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"failed to inspect MCP server"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.InspectMCPServer(context.Background(), "srv-1"); err == nil ||
		!strings.Contains(err.Error(), "memory 500 internal") {
		t.Fatalf("error = %v, want memory 500 internal", err)
	}
}

func TestListMCPServerTools(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		// Memory's MCPServerToolDTO carries extra fields beyond MCPTool
		// (inputSchema, config, timestamps) — the gateway subset must ignore them.
		_, _ = io.WriteString(w, `{"success":true,"data":[
			{"id":"t1","serverId":"srv-1","toolName":"web_search","description":"Search the web","enabled":true,"inputSchema":{"type":"object"},"createdAt":"2026-09-01T10:00:00Z"},
			{"id":"t2","serverId":"srv-1","toolName":"fetch","enabled":false,"config":{"maxDepth":2},"createdAt":"2026-09-01T10:00:00Z"}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	tools, err := m.ListMCPServerTools(context.Background(), "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/tools" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/admin/mcp-servers/srv-1/tools", gotMethod, gotPath)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2: %+v", len(tools), tools)
	}
	if tools[0].ID != "t1" || tools[0].ServerID != "srv-1" || tools[0].ToolName != "web_search" ||
		tools[0].Description != "Search the web" || !tools[0].Enabled {
		t.Errorf("tools[0] = %+v", tools[0])
	}
	if tools[1].ToolName != "fetch" || tools[1].Enabled {
		t.Errorf("tools[1] = %+v", tools[1])
	}
}

func TestListMCPServerToolsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":[]}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	tools, err := m.ListMCPServerTools(context.Background(), "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("want 0 tools, got %+v", tools)
	}
}

func TestListMCPServerToolsNotFound404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"mcp_server not found"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.ListMCPServerTools(context.Background(), "missing"); err == nil ||
		!strings.Contains(err.Error(), "memory 404 not_found") {
		t.Fatalf("error = %v, want memory 404 not_found", err)
	}
}

func TestSetMCPServerToolEnabled(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"message":"tool updated"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.SetMCPServerToolEnabled(context.Background(), "srv-1", "t1", false); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/tools/t1" || gotMethod != http.MethodPatch {
		t.Errorf("request = %s %s, want PATCH /api/admin/mcp-servers/srv-1/tools/t1", gotMethod, gotPath)
	}
	if len(gotBody) != 1 {
		t.Errorf("body = %v, want only enabled", gotBody)
	}
	if enabled, ok := gotBody["enabled"].(bool); !ok || enabled {
		t.Errorf("body.enabled = %v, want false", gotBody["enabled"])
	}
}

func TestSetMCPServerToolEnabledBadRequest400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"bad_request","message":"at least one of enabled or config must be provided"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.SetMCPServerToolEnabled(context.Background(), "srv-1", "t1", true); err == nil ||
		!strings.Contains(err.Error(), "memory 400 bad_request") {
		t.Fatalf("error = %v, want memory 400 bad_request", err)
	}
}

func TestMCPServerToolsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	m := NewMemoryClient(url, "tok", "proj")
	if err := m.SyncMCPServer(context.Background(), "srv-1"); err == nil {
		t.Error("SyncMCPServer: want transport error, got nil")
	}
	if _, err := m.InspectMCPServer(context.Background(), "srv-1"); err == nil {
		t.Error("InspectMCPServer: want transport error, got nil")
	}
	if _, err := m.ListMCPServerTools(context.Background(), "srv-1"); err == nil {
		t.Error("ListMCPServerTools: want transport error, got nil")
	}
	if err := m.SetMCPServerToolEnabled(context.Background(), "srv-1", "t1", true); err == nil {
		t.Error("SetMCPServerToolEnabled: want transport error, got nil")
	}
}
