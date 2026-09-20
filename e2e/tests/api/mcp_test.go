// Package api_test — mcp_test.go
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers for /mcp/rpc (legacy) and /mcp (unified) endpoints
// ─────────────────────────────────────────────────────────────────────────────

func initMCPSession(t *testing.T) {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "POST", "/api/mcp/rpc", token, "", jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"id":      1,
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
		},
	}))
	mustStatus(t, resp, http.StatusOK)
}

func initMCPSessionWithProject(t *testing.T, projectID string) {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"id":      1,
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			"project_id":      projectID,
		},
	}))
	mustStatus(t, resp, http.StatusOK)
}

// callMCPRPCTool calls a tools/call on /mcp/rpc and returns parsed content JSON.
func callMCPRPCTool(t *testing.T, projectID, toolName string, args map[string]any, id int) map[string]any {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      id,
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	if rpc["error"] != nil {
		t.Fatalf("callMCPRPCTool %s: unexpected error: %v", toolName, rpc["error"])
	}
	result := rpc["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("callMCPRPCTool %s: empty content", toolName)
	}
	text := content[0].(map[string]any)["text"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("callMCPRPCTool %s: parse text: %v\ntext: %s", toolName, err, text)
	}
	return parsed
}

// createEntityViaMCP creates a single entity via entity-create (batch API) and returns its canonical ID.
func createEntityViaMCP(t *testing.T, projectID, entityType string, props map[string]any, id int) string {
	t.Helper()
	key := fmt.Sprintf("e2e-%s-%d", entityType, time.Now().UnixNano())
	result := callMCPRPCTool(t, projectID, "entity-create", map[string]any{
		"entities": []any{map[string]any{
			"type":       entityType,
			"key":        key,
			"properties": props,
		}},
	}, id)
	results, ok := result["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("createEntityViaMCP: no results in response: %v", result)
	}
	first := results[0].(map[string]any)
	if first["success"] != true {
		t.Fatalf("createEntityViaMCP: create failed: %v", first)
	}
	entity := first["entity"].(map[string]any)
	return entity["id"].(string)
}

// initUnifiedMCPSession initializes a session on /mcp and returns the session ID.
func initUnifiedMCPSession(t *testing.T) string {
	t.Helper()
	token := e2eTestToken()
	resp := framework.DoMCPJSON(t, serverURL()+"/api/mcp", token, "", "", "2025-11-25",
		jsonBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]any{
				"protocolVersion": "2025-11-25",
				"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			},
		}))
	mustStatus(t, resp, http.StatusOK)
	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initUnifiedMCPSession: expected Mcp-Session-Id header")
	}
	return sessionID
}

// doDeleteMCP sends a DELETE /mcp request with Mcp-Session-Id header.
func doDeleteMCP(t *testing.T, token, sessionID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("DELETE", serverURL()+"/api/mcp", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("doDeleteMCP: create request: %v", err)
	}
	framework.SetAuthHeader(req, token)
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doDeleteMCP: do request: %v", err)
	}
	return resp
}

// ─────────────────────────────────────────────────────────────────────────────
// Auth
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_RPC_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", "", "", jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"id":      1,
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

// ─────────────────────────────────────────────────────────────────────────────
// JSON-RPC Protocol
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_RPC_InvalidJSONRPCVersion(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, "", jsonBody(map[string]any{
		"jsonrpc": "1.0",
		"method":  "initialize",
		"id":      1,
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	errObj, ok := rpc["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error field in response")
	}
	if errObj["code"].(float64) != -32600 {
		t.Errorf("expected code -32600, got %v", errObj["code"])
	}
}

func TestMCP_RPC_MethodNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	initMCPSession(t)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, "", jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "unknown/method",
		"id":      2,
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	errObj, ok := rpc["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error field in response")
	}
	if errObj["code"].(float64) != -32601 {
		t.Errorf("expected code -32601, got %v", errObj["code"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Initialize
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_RPC_Initialize(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, "", jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"id":      1,
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)

	if rpc["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %v", rpc["jsonrpc"])
	}
	if rpc["error"] != nil {
		t.Errorf("unexpected error: %v", rpc["error"])
	}
	result, ok := rpc["result"].(map[string]any)
	if !ok {
		t.Fatal("expected result field")
	}
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("expected protocolVersion 2025-06-18, got %v", result["protocolVersion"])
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatal("expected serverInfo field")
	}
	if serverInfo["name"] != "memory-mcp-server-go" {
		t.Errorf("expected server name memory-mcp-server-go, got %v", serverInfo["name"])
	}
}

func TestMCP_RPC_Initialize_MissingParams(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, "", jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"id":      1,
		"params":  map[string]any{},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	errObj, ok := rpc["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error field")
	}
	if errObj["code"].(float64) != -32602 {
		t.Errorf("expected code -32602, got %v", errObj["code"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tools/list
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_RPC_ToolsList(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	initMCPSession(t)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, "", jsonBody(map[string]any{
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
	if len(tools) < 4 {
		t.Errorf("expected at least 4 tools, got %d", len(tools))
	}
	toolNames := make(map[string]bool)
	for _, tt := range tools {
		tool := tt.(map[string]any)
		toolNames[tool["name"].(string)] = true
	}
	for _, name := range []string{"schema-version", "entity-type-list", "entity-query", "entity-search"} {
		if !toolNames[name] {
			t.Errorf("expected tool %q", name)
		}
	}
}

func TestMCP_RPC_ToolsList_RequiresInitialize(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", "all-scopes", "", jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/list",
		"id":      1,
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	errObj, ok := rpc["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for uninitialized session")
	}
	if errObj["code"].(float64) != -32600 {
		t.Errorf("expected code -32600, got %v", errObj["code"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tools/call — schema_version
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_RPC_ToolsCall_SchemaVersion(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	initMCPSession(t)
	parsed := callMCPRPCTool(t, "", "schema-version", map[string]any{}, 3)
	if _, ok := parsed["version"]; !ok {
		t.Error("expected 'version' in schema_version result")
	}
	if _, ok := parsed["timestamp"]; !ok {
		t.Error("expected 'timestamp' in schema_version result")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tools/call — list_entity_types
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_RPC_ToolsCall_ListEntityTypes(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPSessionWithProject(t, projectID)
	parsed := callMCPRPCTool(t, projectID, "entity-type-list", map[string]any{}, 4)
	if _, ok := parsed["projectId"]; !ok {
		t.Error("expected 'projectId' in result")
	}
	if _, ok := parsed["types"]; !ok {
		t.Error("expected 'types' in result")
	}
	if _, ok := parsed["total"]; !ok {
		t.Error("expected 'total' in result")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tools/call — query_entities
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_RPC_ToolsCall_QueryEntities_MissingTypeName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPSessionWithProject(t, projectID)

	// entity-query no longer requires type_name — omitting it returns all entities
	parsed := callMCPRPCTool(t, projectID, "entity-query", map[string]any{}, 5)
	if _, ok := parsed["entities"]; !ok {
		t.Error("expected 'entities' in result even without type_name")
	}
}

func TestMCP_RPC_ToolsCall_QueryEntities_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPSessionWithProject(t, projectID)
	parsed := callMCPRPCTool(t, projectID, "entity-query", map[string]any{"type_name": "NonExistentType"}, 6)
	entities, ok := parsed["entities"].([]any)
	if !ok {
		t.Fatal("expected entities array")
	}
	if len(entities) != 0 {
		t.Errorf("expected empty entities, got %d", len(entities))
	}
}

func TestMCP_RPC_ToolsCall_QueryEntities_ReturnsLatestVersionOnly(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPSessionWithProject(t, projectID)

	createResult := callMCPRPCTool(t, projectID, "entity-create", map[string]any{
		"entities": []any{map[string]any{
			"type":       "WorkPackage",
			"key":        fmt.Sprintf("wp-ver-test-%d", time.Now().UnixNano()),
			"properties": map[string]any{"name": "Version Test WP", "status": "created"},
		}},
	}, 10)
	results, ok := createResult["results"].([]any)
	if !ok || len(results) == 0 || results[0].(map[string]any)["success"] != true {
		t.Fatal("entity-create should succeed")
	}
	entityID := results[0].(map[string]any)["entity"].(map[string]any)["id"].(string)

	updateResult := callMCPRPCTool(t, projectID, "entity-update", map[string]any{
		"entity_id":  entityID,
		"properties": map[string]any{"name": "Version Test WP", "status": "in_progress"},
	}, 11)
	if updateResult["success"] != true {
		t.Fatal("entity-update should succeed")
	}

	queryResult := callMCPRPCTool(t, projectID, "entity-query", map[string]any{"type_name": "WorkPackage"}, 12)
	entities, ok := queryResult["entities"].([]any)
	if !ok {
		t.Fatal("expected entities array")
	}
	var matchCount int
	var latestProps map[string]any
	for _, e := range entities {
		entity := e.(map[string]any)
		if entity["id"].(string) == entityID {
			matchCount++
			latestProps = entity["properties"].(map[string]any)
		}
	}
	if matchCount != 1 {
		t.Errorf("expected exactly 1 row for entity, got %d", matchCount)
	}
	if latestProps != nil && latestProps["status"] != "in_progress" {
		t.Errorf("expected latest status 'in_progress', got %v", latestProps["status"])
	}

	callMCPRPCTool(t, projectID, "entity-delete", map[string]any{"entity_id": entityID}, 13)
}

func TestMCP_RPC_ToolsCall_QueryEntities_StableCanonicalID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPSessionWithProject(t, projectID)

	createResult2 := callMCPRPCTool(t, projectID, "entity-create", map[string]any{
		"entities": []any{map[string]any{
			"type":       "WorkPackage",
			"key":        fmt.Sprintf("wp-can-test-%d", time.Now().UnixNano()),
			"properties": map[string]any{"name": "Canonical ID Test WP", "value": "initial"},
		}},
	}, 20)
	res2, ok2 := createResult2["results"].([]any)
	if !ok2 || len(res2) == 0 || res2[0].(map[string]any)["success"] != true {
		t.Fatal("entity-create should succeed")
	}
	createdID := res2[0].(map[string]any)["entity"].(map[string]any)["id"].(string)

	callMCPRPCTool(t, projectID, "entity-update", map[string]any{
		"entity_id":  createdID,
		"properties": map[string]any{"name": "Canonical ID Test WP", "value": "updated"},
	}, 21)

	queryResult := callMCPRPCTool(t, projectID, "entity-query", map[string]any{"type_name": "WorkPackage"}, 22)
	entities, ok := queryResult["entities"].([]any)
	if !ok {
		t.Fatal("expected entities array")
	}
	var foundEntity map[string]any
	for _, e := range entities {
		entity := e.(map[string]any)
		if entity["id"].(string) == createdID {
			foundEntity = entity
			break
		}
	}
	if foundEntity == nil {
		t.Error("query_entities should find entity by canonical_id")
	} else {
		props := foundEntity["properties"].(map[string]any)
		if props["value"] != "updated" {
			t.Errorf("expected updated value, got %v", props["value"])
		}
	}

	callMCPRPCTool(t, projectID, "entity-delete", map[string]any{"entity_id": createdID}, 23)
}

func TestMCP_RPC_ToolsCall_QueryEntities_PaginationCountsOnlyLatestVersions(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPSessionWithProject(t, projectID)

	createResult3 := callMCPRPCTool(t, projectID, "entity-create", map[string]any{
		"entities": []any{map[string]any{
			"type":       "WorkPackage",
			"key":        fmt.Sprintf("wp-pag-test-%d", time.Now().UnixNano()),
			"properties": map[string]any{"name": "Pagination Count Test WP", "status": "v1"},
		}},
	}, 30)
	res3, ok3 := createResult3["results"].([]any)
	if !ok3 || len(res3) == 0 || res3[0].(map[string]any)["success"] != true {
		t.Fatal("entity-create should succeed")
	}
	entityID := res3[0].(map[string]any)["entity"].(map[string]any)["id"].(string)

	for i, status := range []string{"v2", "v3", "v4"} {
		callMCPRPCTool(t, projectID, "entity-update", map[string]any{
			"entity_id":  entityID,
			"properties": map[string]any{"name": "Pagination Count Test WP", "status": status},
		}, 31+i)
	}

	queryResult := callMCPRPCTool(t, projectID, "entity-query", map[string]any{
		"type_name": "WorkPackage",
		"limit":     float64(50),
	}, 34)
	entities, ok := queryResult["entities"].([]any)
	if !ok {
		t.Fatal("expected entities array")
	}
	var occurrences int
	for _, e := range entities {
		entity := e.(map[string]any)
		if entity["id"].(string) == entityID {
			occurrences++
		}
	}
	if occurrences != 1 {
		t.Errorf("expected 1 occurrence of entity, got %d", occurrences)
	}

	callMCPRPCTool(t, projectID, "entity-delete", map[string]any{"entity_id": entityID}, 35)
}

// ─────────────────────────────────────────────────────────────────────────────
// tools/call — search_entities
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_RPC_ToolsCall_SearchEntities_MissingQuery(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPSessionWithProject(t, projectID)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      7,
		"params": map[string]any{
			"name":      "entity-search",
			"arguments": map[string]any{},
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	errObj, ok := rpc["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error for missing query")
	}
	assertContains(t, errObj["message"].(string), "query")
}

func TestMCP_RPC_ToolsCall_SearchEntities_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPSessionWithProject(t, projectID)
	parsed := callMCPRPCTool(t, projectID, "entity-search", map[string]any{"query": "xyznonexistent123"}, 8)
	entities, ok := parsed["entities"].([]any)
	if !ok {
		t.Fatal("expected entities array")
	}
	if len(entities) != 0 {
		t.Errorf("expected empty entities, got %d", len(entities))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SSE Endpoints
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_SSE_Connect_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/mcp/sse/"+projectID, "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestMCP_SSE_Connect_InvalidProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/mcp/sse/invalid-uuid", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestMCP_SSE_Message_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/mcp/sse/"+projectID+"/message", "", "", jsonBody(map[string]any{
		"jsonrpc": "2.0", "method": "initialize", "id": 1,
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

// ─────────────────────────────────────────────────────────────────────────────
// Unified MCP Endpoint (Spec 2025-11-25)
// ─────────────────────────────────────────────────────────────────────────────

func TestMCP_Unified_POST_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := framework.DoMCPJSON(t, serverURL()+"/api/mcp", "", "", "", "2025-11-25",
		jsonBody(map[string]any{"jsonrpc": "2.0", "method": "initialize", "id": 1}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestMCP_Unified_POST_Initialize(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	token := e2eTestToken()
	resp := framework.DoMCPJSON(t, serverURL()+"/api/mcp", token, "", "", "2025-11-25",
		jsonBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]any{
				"protocolVersion": "2025-11-25",
				"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			},
		}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	if rpc["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %v", rpc["jsonrpc"])
	}
	if rpc["error"] != nil {
		t.Errorf("unexpected error: %v", rpc["error"])
	}
	result, ok := rpc["result"].(map[string]any)
	if !ok {
		t.Fatal("expected result field")
	}
	if result["protocolVersion"] != "2025-11-25" {
		t.Errorf("expected protocolVersion 2025-11-25, got %v", result["protocolVersion"])
	}
	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Error("expected Mcp-Session-Id header")
	}
}

func TestMCP_Unified_POST_SessionManagement(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	sessionID := initUnifiedMCPSession(t)

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
	if len(tools) < 4 {
		t.Errorf("expected at least 4 tools, got %d", len(tools))
	}
}

func TestMCP_Unified_DELETE_TerminatesSession(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	sessionID := initUnifiedMCPSession(t)
	token := e2eTestToken()

	delResp := doDeleteMCP(t, token, sessionID)
	mustStatus(t, delResp, http.StatusNoContent)

	// Subsequent request with the same session ID should get 404
	resp := framework.DoMCPJSON(t, serverURL()+"/api/mcp", token, "", sessionID, "2025-11-25",
		jsonBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      3,
		}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestMCP_Unified_DELETE_RequiresSessionID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doDeleteMCP(t, e2eTestToken(), "")
	mustStatus(t, resp, http.StatusBadRequest)
}
