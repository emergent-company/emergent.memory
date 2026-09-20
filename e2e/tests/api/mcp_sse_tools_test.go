// Package api_test — mcp_sse_tools_test.go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// initAPIUnifiedMCPSession initializes an MCP session on /api/mcp and returns the session ID.
func initAPIUnifiedMCPSession(t *testing.T, projectID string) string {
	t.Helper()
	token := e2eTestToken()
	params := map[string]any{
		"protocolVersion": "2025-11-25",
		"clientInfo":      map[string]any{"name": "mcp-sse-test", "version": "1.0.0"},
	}
	if projectID != "" {
		params["project_id"] = projectID
	}
	resp := framework.DoMCPJSON(t, serverURL()+"/api/mcp", token, projectID, "", "2025-11-25",
		jsonBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params":  params,
		}))
	mustStatus(t, resp, http.StatusOK)
	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initAPIUnifiedMCPSession: expected Mcp-Session-Id header")
	}
	return sessionID
}

// callAPIMCPTool calls a tool on /api/mcp and returns the raw parsed JSON body.
func callAPIMCPTool(t *testing.T, projectID, sessionID, toolName string, args map[string]any) map[string]any {
	t.Helper()
	token := e2eTestToken()
	resp := framework.DoMCPJSON(t, serverURL()+"/api/mcp", token, projectID, sessionID, "2025-11-25",
		jsonBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      2,
			"params": map[string]any{
				"name":      toolName,
				"arguments": args,
			},
		}))
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	return result
}

// parseAPIMCPToolResultText extracts the text content block and parses it as JSON.
func parseAPIMCPToolResultText(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	if body["error"] != nil {
		t.Fatalf("tool returned error: %v", body["error"])
	}
	result, ok := body["result"].(map[string]any)
	if !ok {
		t.Fatal("expected result field")
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatal("expected non-empty content array")
	}
	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Fatalf("expected text block, got %v", block["type"])
	}
	text := block["text"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parseAPIMCPToolResultText: %v\ntext: %s", err, text)
	}
	return parsed
}

// doDeleteAPIMCP sends a DELETE /api/mcp request with Mcp-Session-Id header.
func doDeleteAPIMCP(t *testing.T, token, sessionID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("DELETE", serverURL()+"/api/mcp", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("doDeleteAPIMCP: create request: %v", err)
	}
	framework.SetAuthHeader(req, token)
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doDeleteAPIMCP: do request: %v", err)
	}
	return resp
}

// ─────────────────────────────────────────────────────────────────────────────
// Tool Discovery
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_ToolsList_DiscoverAll18Tools(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	sessionID := initAPIUnifiedMCPSession(t, "")

	token := e2eTestToken()
	resp := framework.DoMCPJSON(t, serverURL()+"/api/mcp", token, "", sessionID, "2025-11-25",
		jsonBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      2,
		}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	if rpc["error"] != nil {
		t.Fatalf("unexpected error: %v", rpc["error"])
	}
	result, ok := rpc["result"].(map[string]any)
	if !ok {
		t.Fatal("expected result field")
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("expected tools array")
	}
	if len(tools) < 9 {
		t.Errorf("expected at least 9 tools, got %d", len(tools))
	}
	toolNames := make(map[string]bool)
	for _, tt := range tools {
		tool := tt.(map[string]any)
		toolNames[tool["name"].(string)] = true
	}
	expected := []string{
		"schema-version", "entity-type-list", "entity-query", "entity-search",
		"schema-list", "schema-get", "schema-list-available", "schema-list-installed",
	}
	for _, name := range expected {
		if !toolNames[name] {
			t.Errorf("expected tool %q", name)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// schema_version
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_SchemaVersion(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	sessionID := initAPIUnifiedMCPSession(t, "")
	body := callAPIMCPTool(t, "", sessionID, "schema-version", map[string]any{})
	result := parseAPIMCPToolResultText(t, body)

	for _, key := range []string{"version", "timestamp", "pack_count", "cache_hint_ttl"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q in schema_version result", key)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// list_template_packs
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_ListTemplatePacks(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	sessionID := initAPIUnifiedMCPSession(t, "")
	body := callAPIMCPTool(t, "", sessionID, "schema-list", map[string]any{})
	result := parseAPIMCPToolResultText(t, body)

	for _, key := range []string{"schemas", "total", "limit", "offset"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q in schema-list result", key)
		}
	}
	if _, ok := result["schemas"].([]any); !ok {
		t.Error("expected schemas to be an array")
	}
}

func TestMCPSSE_Tool_ListTemplatePacks_WithPagination(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	sessionID := initAPIUnifiedMCPSession(t, "")
	body := callAPIMCPTool(t, "", sessionID, "schema-list", map[string]any{
		"limit": 5,
	})
	result := parseAPIMCPToolResultText(t, body)
	if result["limit"] != float64(5) {
		t.Errorf("expected limit 5, got %v", result["limit"])
	}
	if result["offset"] != float64(0) {
		t.Errorf("expected offset 0, got %v", result["offset"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// get_template_pack
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_GetTemplatePack_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	sessionID := initAPIUnifiedMCPSession(t, "")
	body := callAPIMCPTool(t, "", sessionID, "schema-get", map[string]any{
		"schema_id": "00000000-0000-0000-0000-000000000000",
	})
	// Either error or result with nil pack
	if errObj, hasError := body["error"].(map[string]any); hasError {
		assertContains(t, errObj["message"].(string), "not found")
	} else {
		result := parseAPIMCPToolResultText(t, body)
		if result["pack"] != nil {
			t.Error("expected nil pack for non-existent ID")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// list_entity_types
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_ListEntityTypes(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "entity-type-list", map[string]any{})
	result := parseAPIMCPToolResultText(t, body)

	for _, key := range []string{"projectId", "types", "total"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q in list_entity_types result", key)
		}
	}
	if result["projectId"] != projectID {
		t.Errorf("expected projectId %q, got %v", projectID, result["projectId"])
	}
	if _, ok := result["types"].([]any); !ok {
		t.Error("expected types to be an array")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// query_entities
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_QueryEntities_RequiresTypeName(t *testing.T) {
	t.Skip("entity-query no longer requires type_name — returns all entities when omitted")

	projectID, _ := setupProject(t)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "entity-query", map[string]any{})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing type_name")
	}
	assertContains(t, errObj["message"].(string), "type_name")
}

func TestMCPSSE_Tool_QueryEntities_EmptyResult(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "entity-query", map[string]any{
		"type_name": "NonExistentType",
	})
	result := parseAPIMCPToolResultText(t, body)
	entities, ok := result["entities"].([]any)
	if !ok {
		t.Fatal("expected entities array")
	}
	if len(entities) != 0 {
		t.Errorf("expected empty, got %d", len(entities))
	}
}

func TestMCPSSE_Tool_QueryEntities_WithPagination(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "entity-query", map[string]any{
		"type_name": "SomeType",
		"limit":     10,
		"offset":    0,
	})
	result := parseAPIMCPToolResultText(t, body)
	for _, key := range []string{"entities", "projectId"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q", key)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// search_entities
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_SearchEntities_RequiresQuery(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "entity-search", map[string]any{})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing query")
	}
	assertContains(t, errObj["message"].(string), "query")
}

func TestMCPSSE_Tool_SearchEntities_EmptyResult(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "entity-search", map[string]any{
		"query": "xyznonexistent123abc",
	})
	result := parseAPIMCPToolResultText(t, body)
	for _, key := range []string{"entities", "query", "count"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q", key)
		}
	}
	entities, ok := result["entities"].([]any)
	if !ok {
		t.Fatal("expected entities array")
	}
	if len(entities) != 0 {
		t.Errorf("expected empty entities, got %d", len(entities))
	}
}

func TestMCPSSE_Tool_SearchEntities_WithTypeFilter(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "entity-search", map[string]any{
		"query":     "test",
		"type_name": "Document",
		"limit":     5,
	})
	result := parseAPIMCPToolResultText(t, body)
	for _, key := range []string{"entities", "query"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q", key)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// get_entity_edges
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_GetEntityEdges_RequiresEntityID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "entity-edges-get", map[string]any{})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing entity_id")
	}
	assertContains(t, errObj["message"].(string), "entity_id")
}

func TestMCPSSE_Tool_GetEntityEdges_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "entity-edges-get", map[string]any{
		"entity_id": "00000000-0000-0000-0000-000000000000",
	})
	if errObj, hasError := body["error"].(map[string]any); hasError {
		assertContains(t, errObj["message"].(string), "not found")
	} else {
		result := parseAPIMCPToolResultText(t, body)
		for _, key := range []string{"entity_id", "incoming", "outgoing"} {
			if _, ok := result[key]; !ok {
				t.Errorf("expected key %q", key)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// get_available_templates
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_GetAvailableTemplates(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "schema-list-available", map[string]any{})
	result := parseAPIMCPToolResultText(t, body)
	for _, key := range []string{"project_id", "templates", "total"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q", key)
		}
	}
	if _, ok := result["templates"].([]any); !ok {
		t.Error("expected templates to be an array")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// get_installed_templates
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_GetInstalledTemplates(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "schema-list-installed", map[string]any{})
	result := parseAPIMCPToolResultText(t, body)
	for _, key := range []string{"project_id", "templates", "total"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q", key)
		}
	}
	if _, ok := result["templates"].([]any); !ok {
		t.Error("expected templates to be an array")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// assign_template_pack validation
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_AssignTemplatePack_RequiresTemplatePackID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "schema-install", map[string]any{})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing template_pack_id")
	}
	assertContains(t, errObj["message"].(string), "template_pack_id")
}

func TestMCPSSE_Tool_AssignTemplatePack_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "schema-install", map[string]any{
		"template_pack_id": "00000000-0000-0000-0000-000000000000",
	})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for non-existent pack")
	}
	assertContains(t, errObj["message"].(string), "not found")
}

// ─────────────────────────────────────────────────────────────────────────────
// update_template_assignment validation
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_UpdateTemplateAssignment_RequiresAssignmentID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "schema-update-assignment", map[string]any{})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing assignment_id")
	}
	assertContains(t, errObj["message"].(string), "assignment_id")
}

func TestMCPSSE_Tool_UpdateTemplateAssignment_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "schema-update-assignment", map[string]any{
		"assignment_id": "00000000-0000-0000-0000-000000000000",
		"active":        true,
	})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for non-existent assignment")
	}
	assertContains(t, errObj["message"].(string), "not found")
}

// ─────────────────────────────────────────────────────────────────────────────
// uninstall_template_pack validation
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_UninstallTemplatePack_RequiresAssignmentID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "schema-uninstall", map[string]any{})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing assignment_id")
	}
	assertContains(t, errObj["message"].(string), "assignment_id")
}

func TestMCPSSE_Tool_UninstallTemplatePack_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	body := callAPIMCPTool(t, projectID, sessionID, "schema-uninstall", map[string]any{
		"assignment_id": "00000000-0000-0000-0000-000000000000",
	})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for non-existent assignment")
	}
	assertContains(t, errObj["message"].(string), "not found")
}

// ─────────────────────────────────────────────────────────────────────────────
// create_template_pack validation
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_CreateTemplatePack_RequiresName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	sessionID := initAPIUnifiedMCPSession(t, "")
	body := callAPIMCPTool(t, "", sessionID, "schema-create", map[string]any{
		"version":             "1.0.0",
		"object_type_schemas": map[string]any{},
	})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing name")
	}
	assertContains(t, errObj["message"].(string), "name")
}

func TestMCPSSE_Tool_CreateTemplatePack_RequiresVersion(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	sessionID := initAPIUnifiedMCPSession(t, "")
	body := callAPIMCPTool(t, "", sessionID, "schema-create", map[string]any{
		"name":                "Test",
		"object_type_schemas": map[string]any{},
	})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing version")
	}
	assertContains(t, errObj["message"].(string), "version")
}

func TestMCPSSE_Tool_CreateTemplatePack_RequiresSchemas(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	sessionID := initAPIUnifiedMCPSession(t, "")
	body := callAPIMCPTool(t, "", sessionID, "schema-create", map[string]any{
		"name":    "Test",
		"version": "1.0.0",
	})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing object_type_schemas")
	}
	assertContains(t, errObj["message"].(string), "object_type_schemas")
}

// ─────────────────────────────────────────────────────────────────────────────
// delete_template_pack validation
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_DeleteTemplatePack_RequiresPackID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	sessionID := initAPIUnifiedMCPSession(t, "")
	body := callAPIMCPTool(t, "", sessionID, "schema-delete", map[string]any{})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing pack_id")
	}
	assertContains(t, errObj["message"].(string), "pack_id")
}

func TestMCPSSE_Tool_DeleteTemplatePack_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("tool removed from MCP server")

	sessionID := initAPIUnifiedMCPSession(t, "")
	body := callAPIMCPTool(t, "", sessionID, "schema-delete", map[string]any{
		"pack_id": "00000000-0000-0000-0000-000000000000",
	})
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for non-existent pack")
	}
	assertContains(t, errObj["message"].(string), "not found")
}

// ─────────────────────────────────────────────────────────────────────────────
// Session termination
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Session_DELETE_Terminates(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	sessionID := initAPIUnifiedMCPSession(t, "")
	token := e2eTestToken()

	// Terminate session using the same doDeleteMCP helper from mcp_test.go
	// but for /api/mcp path — build the request manually
	delResp := doDeleteAPIMCP(t, token, sessionID)
	mustStatus(t, delResp, http.StatusNoContent)

	// Subsequent request should get 404
	resp := framework.DoMCPJSON(t, serverURL()+"/api/mcp", token, "", sessionID, "2025-11-25",
		jsonBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      2,
		}))
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// TemplatePackLifecycle (external server only)
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPSSE_Tool_TemplatePackLifecycle(t *testing.T) {
	t.Skip("Lifecycle test requires external server due to transaction isolation")
}
