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

	m := NewMemoryClient(srv.URL, "proj")
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

	m := NewMemoryClient(srv.URL, "proj")
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
	m := NewMemoryClient(srv.URL, "proj")
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

	m := NewMemoryClient(srv.URL, "proj")
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

	m := NewMemoryClient(srv.URL, "proj")
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
	m := NewMemoryClient(srv.URL, "proj")
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

	m := NewMemoryClient(srv.URL, "proj")
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
	m := NewMemoryClient(srv.URL, "proj")
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
	m := NewMemoryClient(srv.URL, "proj")
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

	m := NewMemoryClient(srv.URL, "proj")
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
	m := NewMemoryClient(srv.URL, "proj")
	if err := m.SetMCPServerToolEnabled(context.Background(), "srv-1", "t1", true); err == nil ||
		!strings.Contains(err.Error(), "memory 400 bad_request") {
		t.Fatalf("error = %v, want memory 400 bad_request", err)
	}
}

func TestMCPServerToolsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	m := NewMemoryClient(url, "proj")
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

func TestListBuiltinToolGroups(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":[
			{"id":"graph","label":"Graph","description":"Entity + relation tools","policy":"","enabled":true,"tools":["graph_write","memory_lookup"]}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	groups, err := m.ListBuiltinToolGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/builtin-tool-groups" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/admin/builtin-tool-groups", gotMethod, gotPath)
	}
	if len(groups) != 1 || groups[0].ID != "graph" || groups[0].Label != "Graph" ||
		groups[0].Description != "Entity + relation tools" || !groups[0].Enabled {
		t.Errorf("groups = %+v", groups)
	}
	if len(groups[0].Tools) != 2 || groups[0].Tools[0] != "graph_write" {
		t.Errorf("group tools = %v", groups[0].Tools)
	}
}

func TestCallMCPServerTool(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{
			"content":[{"type":"text","text":"{\"ok\":true}"}],
			"isError":false,
			"structuredContent":{"ok":true}
		}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.CallMCPServerTool(context.Background(), "srv-1", "memory_lookup", map[string]any{"query": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/tools/memory_lookup/call" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/admin/mcp-servers/srv-1/tools/memory_lookup/call", gotMethod, gotPath)
	}
	args, _ := gotBody["arguments"].(map[string]any)
	if len(gotBody) != 1 || args["query"] != "hello" {
		t.Errorf("body = %v, want {arguments:{query:hello}}", gotBody)
	}
	if len(res.Content) != 1 || res.Content[0].Type != "text" || res.Content[0].Text != `{"ok":true}` || res.IsError {
		t.Errorf("result = %+v", res)
	}
	if len(res.StructuredContent) == 0 {
		t.Error("structuredContent dropped from the result")
	}
}

func TestCallMCPServerToolNilArgsSendsEmptyObject(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"content":[],"isError":false}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.CallMCPServerTool(context.Background(), "srv-1", "ping", nil); err != nil {
		t.Fatal(err)
	}
	args, ok := gotBody["arguments"].(map[string]any)
	if !ok || len(args) != 0 {
		t.Errorf("arguments = %v, want an empty object", gotBody["arguments"])
	}
}

// --- MCP OAuth connect client methods ---
//
// These cover the MemoryClient proxies for memory's per-server OAuth routes
// (GET /:id/oauth/status, POST /:id/oauth/authorize, DELETE /:id/oauth). Like
// the CRUD methods in extras.go they hit /api/admin/mcp-servers with a plain
// context — the static project-bound credentials apply.

func TestGetMCPServerOAuthStatus(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{
			"serverId":"srv-1","configured":true,"connected":true,"clientConfigured":true,
			"authorizationServer":"https://auth.example.com","scope":"read:repo","requestedScope":"repo read:org",
			"scopes":["read:repo","write:repo"],"registrationSupported":true,
			"expiresAt":"2026-12-31T00:00:00Z"
		}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.GetMCPServerOAuthStatus(context.Background(), "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/oauth/status" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/admin/mcp-servers/srv-1/oauth/status", gotMethod, gotPath)
	}
	if res.ServerID != "srv-1" || !res.Configured || !res.Connected || !res.ClientConfigured {
		t.Errorf("status = %+v", res)
	}
	if res.AuthorizationServer == nil || *res.AuthorizationServer != "https://auth.example.com" {
		t.Errorf("authorizationServer = %v", res.AuthorizationServer)
	}
	if res.Scope == nil || *res.Scope != "read:repo" {
		t.Errorf("scope = %v", res.Scope)
	}
	if res.RequestedScope == nil || *res.RequestedScope != "repo read:org" {
		t.Errorf("requestedScope = %v, want the configured set", res.RequestedScope)
	}
	if len(res.Scopes) != 2 || res.Scopes[0] != "read:repo" {
		t.Errorf("scopes = %v", res.Scopes)
	}
	if !res.RegistrationSupported {
		t.Error("registrationSupported = false, want true")
	}
	if res.ExpiresAt == nil || *res.ExpiresAt != "2026-12-31T00:00:00Z" {
		t.Errorf("expiresAt = %v", res.ExpiresAt)
	}
}

func TestAuthorizeMCPServer(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"authorizeUrl":"https://auth.example.com/authorize?client_id=abc","state":"opaque-state"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.AuthorizeMCPServer(context.Background(), "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/oauth/authorize" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/admin/mcp-servers/srv-1/oauth/authorize", gotMethod, gotPath)
	}
	if res.AuthorizeURL != "https://auth.example.com/authorize?client_id=abc" || res.State != "opaque-state" {
		t.Errorf("authorize = %+v", res)
	}
}

func TestAuthorizeMCPServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"authorization is not available for this server"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.AuthorizeMCPServer(context.Background(), "srv-1"); err == nil ||
		!strings.Contains(err.Error(), "memory 400") ||
		!strings.Contains(err.Error(), "authorization is not available") {
		t.Fatalf("error = %v, want memory 400 with the message", err)
	}
}

func TestAuthorizeMCPServerNotFound404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"mcp_server not found"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.AuthorizeMCPServer(context.Background(), "missing"); err == nil ||
		!strings.Contains(err.Error(), "memory 404 not_found") {
		t.Fatalf("error = %v, want memory 404 not_found", err)
	}
}

func TestDisconnectMCPServer(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if err := m.DisconnectMCPServer(context.Background(), "srv-1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/oauth" || gotMethod != http.MethodDelete {
		t.Errorf("request = %s %s, want DELETE /api/admin/mcp-servers/srv-1/oauth", gotMethod, gotPath)
	}
}

func TestDisconnectMCPServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"failed to clear tokens"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if err := m.DisconnectMCPServer(context.Background(), "srv-1"); err == nil ||
		!strings.Contains(err.Error(), "memory 500 internal") {
		t.Fatalf("error = %v, want memory 500 internal", err)
	}
}

func TestSetMCPServerOAuthClient(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"serverId":"srv-1","configured":true,"connected":false,"clientConfigured":true}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.SetMCPServerOAuthClient(context.Background(), "srv-1", "client-abc", "secret-xyz", "repo read:org")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/oauth/client" || gotMethod != http.MethodPut {
		t.Errorf("request = %s %s, want PUT /api/admin/mcp-servers/srv-1/oauth/client", gotMethod, gotPath)
	}
	if gotBody["clientId"] != "client-abc" || gotBody["clientSecret"] != "secret-xyz" {
		t.Errorf("body = %v, want clientId client-abc + clientSecret secret-xyz", gotBody)
	}
	if gotBody["scope"] != "repo read:org" {
		t.Errorf("body.scope = %q, want the configured scope set", gotBody["scope"])
	}
	if !res.ClientConfigured {
		t.Errorf("status = %+v, want clientConfigured true", res)
	}
}

func TestSetMCPServerOAuthClientOmitsBlankSecret(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"serverId":"srv-1","clientConfigured":true}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.SetMCPServerOAuthClient(context.Background(), "srv-1", "client-abc", "", "repo"); err != nil {
		t.Fatal(err)
	}
	if gotBody["clientId"] != "client-abc" {
		t.Errorf("body.clientId = %v, want client-abc", gotBody["clientId"])
	}
	if _, ok := gotBody["clientSecret"]; ok {
		t.Errorf("body = %v, want clientSecret omitted when blank", gotBody)
	}
	if gotBody["scope"] != "repo" {
		t.Errorf("body.scope = %q, want the configured scope set", gotBody["scope"])
	}
}

func TestSetMCPServerOAuthClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"bad_request","message":"client id is required"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.SetMCPServerOAuthClient(context.Background(), "srv-1", "", "", ""); err == nil ||
		!strings.Contains(err.Error(), "memory 400 bad_request") {
		t.Fatalf("error = %v, want memory 400 bad_request", err)
	}
}

func TestClearMCPServerOAuthClient(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if err := m.ClearMCPServerOAuthClient(context.Background(), "srv-1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/admin/mcp-servers/srv-1/oauth/client" || gotMethod != http.MethodDelete {
		t.Errorf("request = %s %s, want DELETE /api/admin/mcp-servers/srv-1/oauth/client", gotMethod, gotPath)
	}
}

func TestClearMCPServerOAuthClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"failed to clear client"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	if err := m.ClearMCPServerOAuthClient(context.Background(), "srv-1"); err == nil ||
		!strings.Contains(err.Error(), "memory 500 internal") {
		t.Fatalf("error = %v, want memory 500 internal", err)
	}
}

// TestInspectMCPServerAuthRequired asserts inspect decodes the auth_required
// state and its auth block (authorization server, scopes, registration support).
func TestInspectMCPServerAuthRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{
			"serverId":"srv-1","serverName":"github","serverType":"http",
			"status":"auth_required","latencyMs":0,
			"auth":{"resourceMetadataUrl":"https://api.githubcopilot.com/mcp/.well-known/oauth-protected-resource","authorizationServer":"https://github.com/login/oauth/authorize","scopes":["read:org"],"scopesAvailable":["repo","write:packages","project"],"registrationSupported":false},
			"tools":[],"prompts":[],"resources":[],"resourceTemplates":[]
		}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.InspectMCPServer(context.Background(), "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "auth_required" {
		t.Fatalf("status = %q, want auth_required", res.Status)
	}
	if res.Auth == nil {
		t.Fatal("auth = nil, want the oauth requirement")
	}
	if res.Auth.AuthorizationServer != "https://github.com/login/oauth/authorize" ||
		res.Auth.ResourceMetadataURL != "https://api.githubcopilot.com/mcp/.well-known/oauth-protected-resource" {
		t.Errorf("auth = %+v", res.Auth)
	}
	if len(res.Auth.Scopes) != 1 || res.Auth.Scopes[0] != "read:org" {
		t.Errorf("scopes = %v", res.Auth.Scopes)
	}
	if len(res.Auth.ScopesAvailable) != 3 || res.Auth.ScopesAvailable[1] != "write:packages" {
		t.Errorf("scopesAvailable = %v, want the advertised list passed through", res.Auth.ScopesAvailable)
	}
	if res.Auth.RegistrationSupported {
		t.Error("registrationSupported = true, want false")
	}
}

// TestInspectMCPServerAuthUnavailable asserts inspect decodes the auth_required
// state whose provider publishes no discoverable OAuth metadata: the auth block
// carries unavailable + remedy so the UI can disable Connect.
func TestInspectMCPServerAuthUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{
			"serverId":"srv-1","serverName":"github","serverType":"http",
			"status":"auth_required","latencyMs":0,
			"auth":{"unavailable":true,"remedy":"configure a token header for this server instead (e.g. Authorization: Bearer <token>)"},
			"tools":[],"prompts":[],"resources":[],"resourceTemplates":[]
		}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.InspectMCPServer(context.Background(), "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "auth_required" {
		t.Fatalf("status = %q, want auth_required", res.Status)
	}
	if res.Auth == nil {
		t.Fatal("auth = nil, want the oauth requirement")
	}
	if !res.Auth.Unavailable {
		t.Error("unavailable = false, want true")
	}
	if !strings.Contains(res.Auth.Remedy, "token header") {
		t.Errorf("remedy = %q, want it to name the token-header remedy", res.Auth.Remedy)
	}
}
