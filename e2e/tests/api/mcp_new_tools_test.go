// Package api_test — mcp_new_tools_test.go
package api_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

// callMCPNewTool calls a tools/call on /api/mcp/rpc (new tools endpoint) and returns parsed JSON object.
func callMCPNewTool(t *testing.T, projectID, toolName string, args map[string]any) map[string]any {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      1,
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	if rpc["error"] != nil {
		t.Fatalf("callMCPNewTool %s: unexpected RPC error: %v", toolName, rpc["error"])
	}
	result := rpc["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("callMCPNewTool %s: empty content", toolName)
	}
	text := content[0].(map[string]any)["text"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("callMCPNewTool %s: parse text: %v\ntext: %s", toolName, err, text)
	}
	return parsed
}

// callMCPNewToolArray is like callMCPNewTool but expects the JSON text to be an array.
func callMCPNewToolArray(t *testing.T, projectID, toolName string, args map[string]any) []any {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      1,
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	if rpc["error"] != nil {
		t.Fatalf("callMCPNewToolArray %s: unexpected RPC error: %v", toolName, rpc["error"])
	}
	result := rpc["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("callMCPNewToolArray %s: empty content", toolName)
	}
	text := content[0].(map[string]any)["text"].(string)
	var parsed []any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("callMCPNewToolArray %s: expected JSON array: %v\ntext: %s", toolName, err, text)
	}
	return parsed
}

// initMCPNewSession initializes a session on /api/mcp/rpc with the given projectID.
func initMCPNewSession(t *testing.T, projectID string) {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
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

// ─────────────────────────────────────────────────────────────────────────────
// list_documents
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPNew_ListDocuments(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPNewSession(t, projectID)

	result := callMCPNewTool(t, projectID, "document-list", map[string]any{})
	docs, ok := result["documents"]
	if !ok {
		t.Fatal("expected 'documents' key in list_documents response")
	}
	if _, ok := docs.([]any); !ok {
		t.Error("expected 'documents' to be an array")
	}
	if _, ok := result["total"]; !ok {
		t.Error("expected 'total' key in list_documents response")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// list_skills
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPNew_ListSkills(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPNewSession(t, projectID)

	items := callMCPNewToolArray(t, projectID, "skill-list", map[string]any{})
	if items == nil {
		t.Error("expected non-nil array from list_skills")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// create_skill and get_skill
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPNew_CreateAndGetSkill(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPNewSession(t, projectID)

	created := callMCPNewTool(t, projectID, "skill-create", map[string]any{
		"name":        "mcp-test-skill",
		"description": "A skill created by MCP e2e test",
		"content":     "## Test Skill\nThis skill does nothing.",
	})
	if _, ok := created["id"]; !ok {
		t.Fatal("create_skill response must contain 'id'")
	}
	if created["name"] != "mcp-test-skill" {
		t.Errorf("expected name 'mcp-test-skill', got %v", created["name"])
	}
	skillID := created["id"].(string)
	if skillID == "" {
		t.Fatal("expected non-empty skill ID")
	}

	got := callMCPNewTool(t, projectID, "skill-get", map[string]any{"skill_id": skillID})
	if got["id"] != skillID {
		t.Errorf("expected skill ID %q, got %v", skillID, got["id"])
	}
	if got["name"] != "mcp-test-skill" {
		t.Errorf("expected name 'mcp-test-skill', got %v", got["name"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// get_embedding_status
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPNew_GetEmbeddingStatus_NotConfigured(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPNewSession(t, projectID)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      1,
		"params": map[string]any{
			"name":      "get_embedding_status",
			"arguments": map[string]any{},
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	// Either RPC error or tool result is acceptable in test env
	if rpc["error"] == nil {
		result, ok := rpc["result"].(map[string]any)
		if !ok {
			t.Fatal("expected result field")
		}
		if _, ok := result["content"]; !ok {
			t.Error("expected content in result")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// list_adk_sessions
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPNew_ListADKSessions(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPNewSession(t, projectID)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      1,
		"params": map[string]any{
			"name":      "list_adk_sessions",
			"arguments": map[string]any{},
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	if rpc["error"] != nil {
		rl.Skipf("list_adk_sessions not wired in test env (agent/ADK handler unavailable): %v", rpc["error"])
	}
	result := rpc["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("empty content from list_adk_sessions")
	}
	text := content[0].(map[string]any)["text"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("non-JSON from list_adk_sessions: %v\ntext: %s", err, text)
	}
	if _, ok := parsed["items"]; !ok {
		t.Error("expected 'items' key in list_adk_sessions response")
	}
	if _, ok := parsed["total_count"]; !ok {
		t.Error("expected 'total_count' key in list_adk_sessions response")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// list_traces
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPNew_ListTraces_SkipWhenTempoNotConfigured(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPNewSession(t, projectID)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      1,
		"params": map[string]any{
			"name":      "list_traces",
			"arguments": map[string]any{"since": "30m"},
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	if rpc["error"] != nil {
		rl.Skipf("list_traces not wired in test env (Tempo/tracing not configured): %v", rpc["error"])
	}
	result, ok := rpc["result"].(map[string]any)
	if !ok {
		t.Fatal("expected result field")
	}
	if _, ok := result["content"]; !ok {
		t.Error("expected content in result")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// query_knowledge
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPNew_QueryKnowledge(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPNewSession(t, projectID)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/mcp/rpc", token, projectID, jsonBody(map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      1,
		"params": map[string]any{
			"name":      "query_knowledge",
			"arguments": map[string]any{"question": "What is this project about?"},
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var rpc map[string]any
	parseBodyJSON(t, body, &rpc)
	if rpc["error"] != nil {
		rl.Skipf("query_knowledge not wired in test env (no LLM configured): %v", rpc["error"])
	}
	result, ok := rpc["result"].(map[string]any)
	if !ok {
		t.Fatal("expected result field")
	}
	if _, ok := result["content"]; !ok {
		t.Error("expected content in result")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// upload_document
// ─────────────────────────────────────────────────────────────────────────────

func TestMCPNew_UploadDocument(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	initMCPNewSession(t, projectID)

	content := "# Test Document\nThis document was uploaded via MCP tool."
	contentB64 := base64.StdEncoding.EncodeToString([]byte(content))

	created := callMCPNewTool(t, projectID, "document-upload", map[string]any{
		"filename":       "mcp-test.md",
		"content_base64": contentB64,
		"mime_type":      "text/markdown",
	})

	docObj, ok := created["document"].(map[string]any)
	if !ok {
		t.Fatal("upload_document response must contain 'document' object")
	}
	docID, ok := docObj["id"].(string)
	if !ok || docID == "" {
		t.Fatal("document object must contain string 'id'")
	}

	got := callMCPNewTool(t, projectID, "document-get", map[string]any{"document_id": docID})
	if got["id"] != docID {
		t.Errorf("expected document ID %q, got %v", docID, got["id"])
	}
}
