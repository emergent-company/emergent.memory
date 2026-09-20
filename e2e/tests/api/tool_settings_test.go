// Package api_test — tool_settings_test.go
//
// Tests for per-org and per-project built-in tool settings.
// Ported from emergent.memory/apps/server/tests/e2e/tool_settings_test.go
package api_test

import (
	"net/http"
	"testing"
)

// =============================================================================
// Helpers
// =============================================================================

// getBuiltinServerID calls GET /api/admin/mcp-servers and returns the ID
// of the builtin server for the given project.
func getBuiltinServerID(t *testing.T, projectID string) (string, bool) {
	t.Helper()
	resp := doAPI(t, "GET", "/api/admin/mcp-servers", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	data, ok := result["data"].([]any)
	if !ok {
		return "", false
	}
	for _, item := range data {
		srv, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if srv["type"] == "builtin" {
			id, _ := srv["id"].(string)
			return id, id != ""
		}
	}
	return "", false
}

// listBuiltinTools returns the tools slice from the builtin server's tool list.
func listBuiltinTools(t *testing.T, projectID, serverID string) []map[string]any {
	t.Helper()
	resp := doAPI(t, "GET", "/api/admin/mcp-servers/"+serverID+"/tools", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	raw, _ := result["data"].([]any)
	tools := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			tools = append(tools, m)
		}
	}
	return tools
}

// findBuiltinTool returns the first tool in the list with the given name.
func findBuiltinTool(tools []map[string]any, name string) (map[string]any, bool) {
	for _, tool := range tools {
		if tool["toolName"] == name {
			return tool, true
		}
	}
	return nil, false
}

// =============================================================================
// 10.2 TestProjectToolSettings_ToggleBuiltinTool
// =============================================================================

func TestToolSettings_ToggleBuiltinTool(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	serverID, ok := getBuiltinServerID(t, projectID)
	if !ok {
		t.Skip("builtin server not found")
	}

	tools := listBuiltinTools(t, projectID, serverID)
	tool, ok := findBuiltinTool(tools, "brave_web_search")
	if !ok {
		t.Skip("brave_web_search not in builtin tools list — skipping")
	}

	toolID, _ := tool["id"].(string)
	initialEnabled, _ := tool["enabled"].(bool)

	// Toggle to the opposite
	resp := doAPILogged(t, rl, "PATCH", "/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
		e2eTestToken(), projectID,
		jsonBody(map[string]any{"enabled": !initialEnabled}))
	mustStatus(t, resp, http.StatusOK)

	// Re-fetch and verify
	tools = listBuiltinTools(t, projectID, serverID)
	tool, ok = findBuiltinTool(tools, "brave_web_search")
	if !ok {
		t.Fatal("tool disappeared after toggle")
	}
	if tool["enabled"] != !initialEnabled {
		t.Errorf("expected enabled=%v after toggle, got %v", !initialEnabled, tool["enabled"])
	}

	// Restore
	resp = doAPILogged(t, rl, "PATCH", "/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
		e2eTestToken(), projectID,
		jsonBody(map[string]any{"enabled": initialEnabled}))
	mustStatus(t, resp, http.StatusOK)
	rl.LogStep("toggled brave_web_search tool and restored original state", nil)
}

// =============================================================================
// 10.3 TestOrgToolSettings_CRUD
// =============================================================================

func TestToolSettings_OrgToolSettings_CRUD(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)
	toolName := "brave_web_search"

	// Create (upsert) org tool setting
	resp := doAPIWithOrg(t, "PUT", "/api/admin/orgs/"+orgID+"/tool-settings/"+toolName,
		e2eTestToken(), "", orgID,
		jsonBody(map[string]any{
			"enabled": false,
			"config":  map[string]any{"api_key": "test-key-123"},
		}))
	body := mustStatus(t, resp, http.StatusOK)

	var upsertResult map[string]any
	parseBodyJSON(t, body, &upsertResult)
	if upsertResult["toolName"] != toolName {
		t.Errorf("expected toolName=%s, got %v", toolName, upsertResult["toolName"])
	}
	if upsertResult["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", upsertResult["enabled"])
	}

	// Read: list org tool settings
	resp = doAPIWithOrg(t, "GET", "/api/admin/orgs/"+orgID+"/tool-settings",
		e2eTestToken(), "", orgID, nil)
	body = mustStatus(t, resp, http.StatusOK)

	var list []any
	parseBodyJSON(t, body, &list)
	var found map[string]any
	for _, item := range list {
		ts, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if ts["toolName"] == toolName {
			found = ts
			break
		}
	}
	if found == nil {
		t.Fatal("org tool setting not found in list")
	}
	if found["enabled"] != false {
		t.Errorf("expected enabled=false in list, got %v", found["enabled"])
	}

	// Update: re-upsert with enabled=true
	resp = doAPIWithOrg(t, "PUT", "/api/admin/orgs/"+orgID+"/tool-settings/"+toolName,
		e2eTestToken(), "", orgID,
		jsonBody(map[string]any{"enabled": true}))
	body = mustStatus(t, resp, http.StatusOK)

	var updated map[string]any
	parseBodyJSON(t, body, &updated)
	if updated["enabled"] != true {
		t.Errorf("expected enabled=true after update, got %v", updated["enabled"])
	}

	// Delete
	resp = doAPIWithOrg(t, "DELETE", "/api/admin/orgs/"+orgID+"/tool-settings/"+toolName,
		e2eTestToken(), "", orgID, nil)
	mustStatus(t, resp, http.StatusOK)

	// Verify gone
	resp = doAPIWithOrg(t, "GET", "/api/admin/orgs/"+orgID+"/tool-settings",
		e2eTestToken(), "", orgID, nil)
	body = mustStatus(t, resp, http.StatusOK)
	var verifyList []any
	parseBodyJSON(t, body, &verifyList)
	for _, item := range verifyList {
		ts, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if ts["toolName"] == toolName {
			t.Error("deleted tool setting should be gone from list")
		}
	}
	rl.Printf("org tool settings CRUD verified for org %s", orgID)
}

// =============================================================================
// 10.4 TestToolInheritance_OrgDefaultUsedWhenNoProjectOverride
// =============================================================================

func TestToolSettings_OrgDefaultUsedWhenNoProjectOverride(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	toolName := "brave_web_search"

	// Disable at org level
	resp := doAPIWithOrg(t, "PUT", "/api/admin/orgs/"+orgID+"/tool-settings/"+toolName,
		e2eTestToken(), "", orgID,
		jsonBody(map[string]any{"enabled": false}))
	mustStatus(t, resp, http.StatusOK)
	t.Cleanup(func() {
		doAPIWithOrg(t, "DELETE", "/api/admin/orgs/"+orgID+"/tool-settings/"+toolName,
			e2eTestToken(), "", orgID, nil).Body.Close()
	})

	serverID, ok := getBuiltinServerID(t, projectID)
	if !ok {
		t.Skip("builtin server not found")
	}

	tools := listBuiltinTools(t, projectID, serverID)
	tool, ok := findBuiltinTool(tools, toolName)
	if !ok {
		t.Skip("brave_web_search not in builtin tools — skipping")
	}

	if tool["enabled"] != false {
		t.Errorf("expected tool disabled per org default, got enabled=%v", tool["enabled"])
	}
	if tool["inheritedFrom"] != "org" {
		t.Errorf("expected inheritedFrom=org, got %v", tool["inheritedFrom"])
	}
	rl.Printf("org default used when no project override: inheritedFrom=org, enabled=false")
}

// =============================================================================
// 10.5 TestToolInheritance_ProjectOverridesOrg
// =============================================================================

func TestToolSettings_ProjectOverridesOrg(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	toolName := "brave_web_search"

	// Enable at org level
	resp := doAPIWithOrg(t, "PUT", "/api/admin/orgs/"+orgID+"/tool-settings/"+toolName,
		e2eTestToken(), "", orgID,
		jsonBody(map[string]any{"enabled": true}))
	mustStatus(t, resp, http.StatusOK)
	t.Cleanup(func() {
		doAPIWithOrg(t, "DELETE", "/api/admin/orgs/"+orgID+"/tool-settings/"+toolName,
			e2eTestToken(), "", orgID, nil).Body.Close()
	})

	serverID, ok := getBuiltinServerID(t, projectID)
	if !ok {
		t.Skip("builtin server not found")
	}

	tools := listBuiltinTools(t, projectID, serverID)
	tool, ok := findBuiltinTool(tools, toolName)
	if !ok {
		t.Skip("brave_web_search not in builtin tools — skipping")
	}
	toolID, _ := tool["id"].(string)

	// Disable at project level (override)
	resp = doAPILogged(t, rl, "PATCH", "/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
		e2eTestToken(), projectID,
		jsonBody(map[string]any{"enabled": false}))
	mustStatus(t, resp, http.StatusOK)

	// Re-list and verify project override wins
	tools = listBuiltinTools(t, projectID, serverID)
	tool, ok = findBuiltinTool(tools, toolName)
	if !ok {
		t.Fatal("tool disappeared")
	}
	if tool["enabled"] != false {
		t.Errorf("expected project-level disabled to override org enabled, got enabled=%v", tool["enabled"])
	}
	if tool["inheritedFrom"] != "project" {
		t.Errorf("expected inheritedFrom=project, got %v", tool["inheritedFrom"])
	}
	rl.Printf("project override wins over org default")
}

// =============================================================================
// 10.6 TestBraveWebSearch_ProjectApiKey
// =============================================================================

func TestToolSettings_BraveWebSearch_ProjectApiKey(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	toolName := "brave_web_search"

	serverID, ok := getBuiltinServerID(t, projectID)
	if !ok {
		t.Skip("builtin server not found")
	}

	tools := listBuiltinTools(t, projectID, serverID)
	tool, ok := findBuiltinTool(tools, toolName)
	if !ok {
		t.Skip("brave_web_search not in builtin tools — skipping")
	}
	toolID, _ := tool["id"].(string)

	// Store a sentinel API key at project level
	resp := doAPILogged(t, rl, "PATCH", "/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
		e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"enabled": true,
			"config":  map[string]any{"api_key": "PROJ-TEST-KEY"},
		}))
	mustStatus(t, resp, http.StatusOK)

	// Verify config is persisted
	tools = listBuiltinTools(t, projectID, serverID)
	tool, ok = findBuiltinTool(tools, toolName)
	if !ok {
		t.Fatal("tool disappeared")
	}

	cfg, hasCfg := tool["config"]
	if !hasCfg {
		t.Fatal("config field should be present in tool response")
	}
	if cfgMap, ok := cfg.(map[string]any); ok {
		if cfgMap["api_key"] != "PROJ-TEST-KEY" {
			t.Errorf("expected api_key=PROJ-TEST-KEY, got %v", cfgMap["api_key"])
		}
	}
	if tool["inheritedFrom"] != "project" {
		t.Errorf("expected inheritedFrom=project, got %v", tool["inheritedFrom"])
	}
	rl.Printf("project API key persisted for brave_web_search")
}
