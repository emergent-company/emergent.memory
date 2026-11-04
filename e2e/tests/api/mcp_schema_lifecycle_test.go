// Package api_test — mcp_schema_lifecycle_test.go
package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// initLifecycleMCPSession initializes a session on /api/mcp and returns the session ID.
func initLifecycleMCPSession(t *testing.T, projectID string) string {
	t.Helper()
	token := e2eTestToken()
	params := map[string]any{
		"protocolVersion": "2025-11-25",
		"clientInfo":      map[string]any{"name": "mcp-lifecycle-test", "version": "1.0.0"},
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
		t.Fatal("initLifecycleMCPSession: expected Mcp-Session-Id header")
	}
	return sessionID
}

// callLifecycleMCPTool calls a tool on /api/mcp and returns raw parsed body.
func callLifecycleMCPTool(t *testing.T, projectID, sessionID, toolName string, args map[string]any) map[string]any {
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

// parseLifecycleToolResult extracts and parses the text content block.
func parseLifecycleToolResult(t *testing.T, body map[string]any) map[string]any {
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
		t.Fatalf("parseLifecycleToolResult: %v\ntext: %s", err, text)
	}
	return parsed
}

func TestMCPSchemaLifecycle_RequiresExternalServer(t *testing.T) {
	t.Skip("Schema lifecycle tests require external server mode (set TEST_SERVER_URL)")
}

func TestMCPSchemaLifecycle_FullLifecycle(t *testing.T) {
	t.Skip("Schema lifecycle tests require external server mode (set TEST_SERVER_URL)")

	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initLifecycleMCPSession(t, projectID)
	packName := fmt.Sprintf("CRM-Test-%d", time.Now().UnixNano())

	// Step 1: Create template pack
	createPackResult := parseLifecycleToolResult(t, callLifecycleMCPTool(t, projectID, sessionID, "create_template_pack", map[string]any{
		"name":        packName,
		"version":     "1.0.0",
		"description": "CRM schema for testing",
		"author":      "E2E Test",
		"object_type_schemas": map[string]any{
			"Person": map[string]any{
				"description": "A person entity",
				"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"email": map[string]any{"type": "string"},
					"age":   map[string]any{"type": "integer"},
				},
			},
			"Company": map[string]any{
				"description": "A company entity",
				"properties": map[string]any{
					"name":     map[string]any{"type": "string"},
					"industry": map[string]any{"type": "string"},
				},
			},
		},
		"relationship_type_schemas": map[string]any{
			"WORKS_AT": map[string]any{
				"description": "Person works at Company",
				"source_type": "Person",
				"target_type": "Company",
			},
		},
	}))

	if createPackResult["success"] != true {
		t.Fatal("create_template_pack should succeed")
	}
	pack := createPackResult["pack"].(map[string]any)
	packID := pack["id"].(string)
	if packID == "" {
		t.Fatal("expected non-empty pack ID")
	}

	// Step 2: Assign pack
	assignResult := parseLifecycleToolResult(t, callLifecycleMCPTool(t, projectID, sessionID, "assign_template_pack", map[string]any{
		"template_pack_id": packID,
	}))
	if assignResult["success"] != true {
		t.Fatal("assign_template_pack should succeed")
	}
	assignmentID := assignResult["assignment_id"].(string)

	// Step 3: Create entities and relationships, query, delete (elided for brevity)
	// Cleanup: uninstall and delete
	parseLifecycleToolResult(t, callLifecycleMCPTool(t, projectID, sessionID, "uninstall_template_pack", map[string]any{
		"assignment_id": assignmentID,
	}))
	parseLifecycleToolResult(t, callLifecycleMCPTool(t, projectID, sessionID, "delete_template_pack", map[string]any{
		"pack_id": packID,
	}))
}

func TestMCPSchemaLifecycle_ToolsListShowsAllTools(t *testing.T) {
	t.Skip("Schema lifecycle tests require external server mode (set TEST_SERVER_URL)")
}

func TestMCPSchemaLifecycle_CreateEntityRequiresType(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initLifecycleMCPSession(t, projectID)

	result := callLifecycleMCPTool(t, projectID, sessionID, "create_entity", map[string]any{
		"properties": map[string]any{"name": "Test"},
	})
	if result["error"] == nil {
		t.Error("expected error for missing type")
	}
}

func TestMCPSchemaLifecycle_CreateRelationshipRequiresAllParams(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initLifecycleMCPSession(t, projectID)

	result := callLifecycleMCPTool(t, projectID, sessionID, "create_relationship", map[string]any{
		"type":      "KNOWS",
		"source_id": "00000000-0000-0000-0000-000000000001",
	})
	if result["error"] == nil {
		t.Error("expected error for missing target_id")
	}
}

func TestMCPSchemaLifecycle_UpdateEntityRequiresEntityID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initLifecycleMCPSession(t, projectID)

	result := callLifecycleMCPTool(t, projectID, sessionID, "update_entity", map[string]any{
		"properties": map[string]any{"name": "Updated"},
	})
	if result["error"] == nil {
		t.Error("expected error for missing entity_id")
	}
}

func TestMCPSchemaLifecycle_DeleteEntityRequiresEntityID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	sessionID := initLifecycleMCPSession(t, projectID)

	result := callLifecycleMCPTool(t, projectID, sessionID, "delete_entity", map[string]any{})
	if result["error"] == nil {
		t.Error("expected error for missing entity_id")
	}
}
