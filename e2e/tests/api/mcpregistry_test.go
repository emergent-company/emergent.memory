// Package api_test — mcpregistry_test.go
package api_test

import (
	"net/http"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func createMCPServer(t *testing.T, projectID, name, serverType, url string) string {
	t.Helper()
	token := e2eTestToken()
	body := map[string]any{
		"name": name,
		"type": serverType,
	}
	if url != "" {
		body["url"] = url
	}
	resp := doAPI(t, "POST", "/api/admin/mcp-servers", token, projectID, jsonBody(body))
	respBody := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, respBody, &result)
	data := result["data"].(map[string]any)
	id := data["id"].(string)
	t.Cleanup(func() {
		doAPI(t, "DELETE", "/api/admin/mcp-servers/"+id, token, projectID, nil)
	})
	return id
}

// ─────────────────────────────────────────────────────────────────────────────
// Auth tests
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPRegistry_ListServers_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestMCPRegistry_CreateServer_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers", "", "", jsonBody(map[string]any{
		"name": "test-server",
		"type": "http",
		"url":  "https://example.com/mcp",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

// ─────────────────────────────────────────────────────────────────────────────
// CRUD tests
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPRegistry_CreateServer_HTTPSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": uniqueName("test-http-server"),
		"type": "http",
		"url":  "https://example.com/mcp",
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].(map[string]any)
	if data["id"] == "" {
		t.Error("expected non-empty id")
	}
	if data["type"] != "http" {
		t.Errorf("expected type 'http', got %v", data["type"])
	}
	if data["enabled"] != true {
		t.Errorf("expected enabled true, got %v", data["enabled"])
	}
}

func TestMCPRegistry_CreateServer_StdioType(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name":    uniqueName("test-stdio-server"),
		"type":    "stdio",
		"command": "npx",
		"args":    []string{"-y", "@modelcontextprotocol/server-filesystem"},
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	data := result["data"].(map[string]any)
	if data["type"] != "stdio" {
		t.Errorf("expected type 'stdio', got %v", data["type"])
	}
}

func TestMCPRegistry_CreateServer_DuplicateName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	name := uniqueName("duplicate-mcp-server")
	createMCPServer(t, projectID, name, "http", "https://example.com/mcp")

	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": name,
		"type": "http",
		"url":  "https://other.com/mcp",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestMCPRegistry_CreateServer_BuiltinTypeRejected(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": uniqueName("sneaky-builtin"),
		"type": "builtin",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestMCPRegistry_CreateServer_HTTPMissingURL(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": uniqueName("missing-url"),
		"type": "http",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestMCPRegistry_CreateServer_StdioMissingCommand(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": uniqueName("missing-cmd"),
		"type": "stdio",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestMCPRegistry_ListServers_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
}

func TestMCPRegistry_ListServers_WithServers(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	createMCPServer(t, projectID, uniqueName("server-a"), "http", "https://a.example.com/mcp")
	createMCPServer(t, projectID, uniqueName("server-b"), "http", "https://b.example.com/mcp")

	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	data := result["data"].([]any)
	if len(data) < 2 {
		t.Errorf("expected at least 2 servers, got %d", len(data))
	}
}

func TestMCPRegistry_GetServer_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	serverID := createMCPServer(t, projectID, uniqueName("get-me"), "http", "https://example.com/mcp")

	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers/"+serverID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	data := result["data"].(map[string]any)
	if data["id"] != serverID {
		t.Errorf("expected id %q, got %v", serverID, data["id"])
	}
}

func TestMCPRegistry_GetServer_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers/00000000-0000-0000-0000-000000000000", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestMCPRegistry_UpdateServer_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	serverID := createMCPServer(t, projectID, uniqueName("update-me"), "http", "https://example.com/mcp")

	resp := doAPILogged(t, rl, "PATCH", "/api/admin/mcp-servers/"+serverID, e2eTestToken(), projectID, jsonBody(map[string]any{
		"enabled": false,
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	data := result["data"].(map[string]any)
	if data["enabled"] != false {
		t.Errorf("expected enabled false, got %v", data["enabled"])
	}
}

func TestMCPRegistry_DeleteServer_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers", token, projectID, jsonBody(map[string]any{
		"name": uniqueName("delete-me"),
		"type": "http",
		"url":  "https://example.com/mcp",
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, body, &created)
	serverID := created["data"].(map[string]any)["id"].(string)

	delResp := doAPILogged(t, rl, "DELETE", "/api/admin/mcp-servers/"+serverID, token, projectID, nil)
	mustStatus(t, delResp, http.StatusOK)

	getResp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers/"+serverID, token, projectID, nil)
	mustStatus(t, getResp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tool sync and toggle tests
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPRegistry_SyncTools_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	serverID := createMCPServer(t, projectID, uniqueName("tool-server"), "http", "https://example.com/mcp")

	tools := []map[string]any{
		{
			"name":        "read_file",
			"description": "Read a file from disk",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path to read",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			"name":        "write_file",
			"description": "Write content to a file",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				"required": []string{"path", "content"},
			},
		},
	}

	syncResp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers/"+serverID+"/sync", e2eTestToken(), projectID, jsonBody(tools))
	mustStatus(t, syncResp, http.StatusOK)

	listResp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers/"+serverID+"/tools", e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, listBody, &result)
	data := result["data"].([]any)
	if len(data) != 2 {
		t.Errorf("expected 2 tools, got %d", len(data))
	}
}

func TestMCPRegistry_SyncTools_RemovesStale(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	serverID := createMCPServer(t, projectID, uniqueName("stale-server"), "http", "https://example.com/mcp")
	token := e2eTestToken()

	tools3 := []map[string]any{
		{"name": "tool_a", "description": "Tool A"},
		{"name": "tool_b", "description": "Tool B"},
		{"name": "tool_c", "description": "Tool C"},
	}
	doAPILogged(t, rl, "POST", "/api/admin/mcp-servers/"+serverID+"/sync", token, projectID, jsonBody(tools3))

	tools2 := []map[string]any{
		{"name": "tool_a", "description": "Tool A updated"},
		{"name": "tool_b", "description": "Tool B"},
	}
	doAPILogged(t, rl, "POST", "/api/admin/mcp-servers/"+serverID+"/sync", token, projectID, jsonBody(tools2))

	listResp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers/"+serverID+"/tools", token, projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, listBody, &result)
	data := result["data"].([]any)
	if len(data) != 2 {
		t.Errorf("expected 2 tools after stale removal, got %d", len(data))
	}
}

func TestMCPRegistry_ToggleTool_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	serverID := createMCPServer(t, projectID, uniqueName("toggle-server"), "http", "https://example.com/mcp")
	token := e2eTestToken()

	tools := []map[string]any{
		{"name": "my_tool", "description": "A test tool"},
	}
	doAPILogged(t, rl, "POST", "/api/admin/mcp-servers/"+serverID+"/sync", token, projectID, jsonBody(tools))

	listResp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers/"+serverID+"/tools", token, projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)
	var listResult map[string]any
	parseBodyJSON(t, listBody, &listResult)
	data := listResult["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(data))
	}
	toolData := data[0].(map[string]any)
	toolID := toolData["id"].(string)

	patchResp := doAPILogged(t, rl, "PATCH", "/api/admin/mcp-servers/"+serverID+"/tools/"+toolID, token, projectID, jsonBody(map[string]any{
		"enabled": false,
	}))
	mustStatus(t, patchResp, http.StatusOK)

	listResp2 := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers/"+serverID+"/tools", token, projectID, nil)
	listBody2 := mustStatus(t, listResp2, http.StatusOK)
	var listResult2 map[string]any
	parseBodyJSON(t, listBody2, &listResult2)
	data2 := listResult2["data"].([]any)
	tool2 := data2[0].(map[string]any)
	if tool2["enabled"] != false {
		t.Errorf("expected tool enabled=false, got %v", tool2["enabled"])
	}
}

func TestMCPRegistry_DeleteServer_CascadesTools(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	// Create server without cleanup registration (we'll delete it manually)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers", token, projectID, jsonBody(map[string]any{
		"name": uniqueName("cascade-server"),
		"type": "http",
		"url":  "https://example.com/mcp",
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, body, &created)
	serverID := created["data"].(map[string]any)["id"].(string)

	tools := []map[string]any{
		{"name": "tool_x", "description": "Tool X"},
	}
	doAPILogged(t, rl, "POST", "/api/admin/mcp-servers/"+serverID+"/sync", token, projectID, jsonBody(tools))

	delResp := doAPILogged(t, rl, "DELETE", "/api/admin/mcp-servers/"+serverID, token, projectID, nil)
	mustStatus(t, delResp, http.StatusOK)

	toolsResp := doAPILogged(t, rl, "GET", "/api/admin/mcp-servers/"+serverID+"/tools", token, projectID, nil)
	mustStatus(t, toolsResp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// MCP Registry Browse/Install tests
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPRegistry_SearchRegistry_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-registry/search?q=github", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestMCPRegistry_SearchRegistry_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-registry/search?q=github&limit=5", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].(map[string]any)
	servers := data["servers"].([]any)
	if len(servers) == 0 {
		t.Error("expected at least one server matching 'github'")
	}
	first := servers[0].(map[string]any)
	if first["name"] == "" {
		t.Error("expected non-empty server name")
	}
}

func TestMCPRegistry_SearchRegistry_EmptyQuery(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-registry/search?limit=3", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].(map[string]any)
	servers := data["servers"].([]any)
	if len(servers) == 0 {
		t.Error("expected at least one server in registry")
	}
}

func TestMCPRegistry_GetRegistryServer_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-registry/servers/io.github.github%2Fgithub-mcp-server", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].(map[string]any)
	if data["name"] == "" {
		t.Error("expected non-empty name")
	}
}

func TestMCPRegistry_GetRegistryServer_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/admin/mcp-registry/servers/io.nonexistent.server%2Fdoes-not-exist", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestMCPRegistry_InstallFromRegistry_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-registry/install", "", "", jsonBody(map[string]any{
		"registryName": "io.github.github/github-mcp-server",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestMCPRegistry_InstallFromRegistry_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-registry/install", e2eTestToken(), projectID, jsonBody(map[string]any{
		"registryName": "io.github.github/github-mcp-server",
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].(map[string]any)
	server := data["server"].(map[string]any)
	if server["id"] == "" {
		t.Error("expected non-empty server id")
	}
	if server["type"] != "http" {
		t.Errorf("expected type 'http', got %v", server["type"])
	}
}

func TestMCPRegistry_InstallFromRegistry_CustomName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	customName := uniqueName("my-github")
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-registry/install", e2eTestToken(), projectID, jsonBody(map[string]any{
		"registryName": "io.github.github/github-mcp-server",
		"name":         customName,
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	data := result["data"].(map[string]any)
	server := data["server"].(map[string]any)
	if server["name"] != customName {
		t.Errorf("expected name %q, got %v", customName, server["name"])
	}
}

func TestMCPRegistry_InstallFromRegistry_MissingRegistryName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-registry/install", e2eTestToken(), projectID, jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestMCPRegistry_InstallFromRegistry_StdioOnlyBlocked(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-registry/install", e2eTestToken(), projectID, jsonBody(map[string]any{
		"registryName": "io.github.upstash/context7",
	}))
	body := mustStatus(t, resp, http.StatusBadRequest)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}
	msg, _ := errObj["message"].(string)
	assertContains(t, msg, "stdio")
}

// ─────────────────────────────────────────────────────────────────────────────
// Inspect/Test-Connection tests
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPRegistry_InspectServer_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers/00000000-0000-0000-0000-000000000000/inspect", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestMCPRegistry_InspectServer_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers/00000000-0000-0000-0000-000000000000/inspect", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestMCPRegistry_InspectServer_UnreachableHTTP(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	serverID := createMCPServer(t, projectID, uniqueName("unreachable-inspect"), "http", "http://192.0.2.1:9999/mcp")

	resp := doAPILogged(t, rl, "POST", "/api/admin/mcp-servers/"+serverID+"/inspect", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	inspect := result["data"].(map[string]any)
	if inspect["status"] != "error" {
		t.Errorf("expected status 'error', got %v", inspect["status"])
	}
	if inspect["error"] == nil {
		t.Error("expected error message for unreachable server")
	}
}
