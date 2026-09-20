// Package tools_test — brave_search_test.go
//
// End-to-end tests for the brave_web_search built-in MCP tool.
//
// These tests verify:
//  1. That a per-project Brave Search API key can be stored via the API.
//  2. That the brave_web_search MCP tool executes a real search and returns
//     results when a valid API key is configured for the project.
//  3. That the tool is enabled and the inheritedFrom field reflects the
//     project-level configuration.
//
// Required environment variables:
//
//	BRAVE_SEARCH_API_KEY  — Brave Search API key; tests are skipped when absent.
//	MEMORY_TEST_SERVER    — URL of the Memory server.
//	MEMORY_TEST_TOKEN     — API key for the Memory server.
package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// TestBraveSearch_ConfigureAndSearch
//
// Full happy-path: create a project, store the API key at project level, call
// the brave_web_search MCP tool, assert results are returned.
// ─────────────────────────────────────────────────────────────────────────────

func TestBraveSearch_ConfigureAndSearch(t *testing.T) {
	logStatusPreamble(t)

	rl := newRunLog(t)
	defer rl.Close()

	skipIfServerDown(t, rl)
	skipIfNoBraveKey(t, rl)

	braveKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	home := t.TempDir()
	srv := serverURL()
	token := e2eTestToken()

	rl.Describe("Verify Brave Search MCP tool with project-level API key",
		"Create project, store Brave API key at project level",
		"Call brave_web_search via MCP JSON-RPC",
		"Assert search results are returned",
	)

	// ── Step 1: authenticate ────────────────────────────────────────────────
	rl.Section("Authenticate")
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", token)
	rl.Printf("configured server_url=%s", srv)

	// ── Step 2: create ephemeral project ────────────────────────────────────
	rl.Section("Create project")
	projectName := fmt.Sprintf("e2e-brave-search-%d", time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home, append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
	rl.CLI("memory projects create --name "+projectName, createOut)

	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from: %q", createOut)
	}
	rl.Printf("project: %s (%s)", projectName, projectID)

	t.Cleanup(func() {
		delOut, delErr := runCLIInDirWithHome(t, "", home, "projects", "delete", projectID)
		if delErr != nil {
			rl.Printf("warn: delete project failed: %v\n%s", delErr, delOut)
		} else {
			rl.Printf("deleted project %s", projectName)
		}
	})

	// ── Step 3: get the builtin MCP server ID ───────────────────────────────
	// GET /api/admin/mcp-servers — no CLI equivalent
	rl.Section("Get builtin MCP server and brave tool IDs")
	serverID, braveToolID := getBraveToolIDs(t, srv, token, projectID)
	rl.CLIStep("Get brave tool IDs",
		fmt.Sprintf("GET %s/api/admin/mcp-servers (+ tools)", srv),
		fmt.Sprintf("serverID=%s braveToolID=%s", serverID, braveToolID))

	// ── Step 4: store the API key at project level ───────────────────────────
	// PATCH /api/admin/mcp-servers/:id/tools/:id — no CLI equivalent
	rl.Section("Store project-level Brave API key")
	patchURL := fmt.Sprintf("%s/api/admin/mcp-servers/%s/tools/%s", srv, serverID, braveToolID)
	patchBody, _ := json.Marshal(map[string]any{
		"enabled": true,
		"config":  map[string]any{"api_key": braveKey},
	})
	patchResp := doJSON(t, "PATCH", patchURL, token, projectID, patchBody)
	patchRespBody := readBody(t, patchResp)
	rl.CLIStep("Store Brave API key at project level",
		fmt.Sprintf("PATCH %s", patchURL),
		fmt.Sprintf("HTTP %d: %s", patchResp.StatusCode, truncate(patchRespBody, 200)))
	if patchResp.StatusCode != http.StatusOK {
		rl.Failf("PATCH tool config: want 200, got %d — %s", patchResp.StatusCode, patchRespBody)
	}

	// ── Step 5: verify the tool config round-trips correctly ─────────────────
	rl.Section("Verify tool config round-trip")
	_, braveToolAfter := getBraveToolIDsWithDetail(t, srv, token, projectID)
	if braveToolAfter == nil {
		rl.Failf("brave_web_search not found after storing API key")
	}

	inheritedFrom, _ := braveToolAfter["inheritedFrom"].(string)
	if inheritedFrom != "project" {
		t.Errorf("inheritedFrom: got %q, want %q", inheritedFrom, "project")
	}
	rl.Printf("config round-trip OK: inheritedFrom=%q", inheritedFrom)

	// ── Step 6: call the MCP tool via the JSON-RPC endpoint ──────────────────
	rl.Section("Call brave_web_search via MCP")
	results := callBraveSearchViaMCP(t, srv, token, projectID, "Emergent Memory knowledge graph")
	rl.Printf("search returned %d results", len(results))

	if len(results) == 0 {
		rl.Failf("expected at least 1 search result, got 0")
	}

	for i, r := range results {
		title, _ := r["title"].(string)
		url, _ := r["url"].(string)
		rl.Printf("  result %d: %s (%s)", i+1, title, url)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestBraveSearch_OrgLevelKey
//
// Stores the Brave API key at org level and verifies the tool executes
// correctly via the org-inherited configuration.
// ─────────────────────────────────────────────────────────────────────────────

func TestBraveSearch_OrgLevelKey(t *testing.T) {
	logStatusPreamble(t)

	rl := newRunLog(t)
	defer rl.Close()

	skipIfServerDown(t, rl)
	skipIfNoBraveKey(t, rl)

	braveKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	home := t.TempDir()
	srv := serverURL()
	token := e2eTestToken()

	rl.Describe("Verify Brave Search MCP tool with org-level API key",
		"Create project, store Brave API key at org level",
		"Call brave_web_search via MCP JSON-RPC",
		"Assert search results use org-inherited key",
	)

	// ── Step 1: authenticate + create project ───────────────────────────────
	rl.Section("Authenticate and create project")
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", token)

	projectName := fmt.Sprintf("e2e-brave-org-%d", time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home, append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
	rl.CLI("memory projects create --name "+projectName, createOut)

	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from: %q", createOut)
	}
	rl.Printf("project: %s (%s)", projectName, projectID)

	t.Cleanup(func() {
		delOut, delErr := runCLIInDirWithHome(t, "", home, "projects", "delete", projectID)
		if delErr != nil {
			rl.Printf("warn: delete project failed: %v\n%s", delErr, delOut)
		} else {
			rl.Printf("deleted project %s", projectName)
		}
	})

	// ── Step 2: resolve the org ID via the project ──────────────────────────
	// GET /api/projects/:id — no CLI equivalent for reading orgId field
	rl.Section("Resolve org ID")
	orgID := getOrgIDForProject(t, srv, token, projectID)
	rl.CLIStep("Get org ID from project",
		fmt.Sprintf("GET %s/api/projects/%s", srv, projectID),
		fmt.Sprintf("orgID=%s", orgID))

	// ── Step 3: store API key at org level ──────────────────────────────────
	// PUT /api/admin/orgs/:id/tool-settings — no CLI equivalent
	rl.Section("Store org-level Brave API key")
	orgURL := fmt.Sprintf("%s/api/admin/orgs/%s/tool-settings/brave_web_search", srv, orgID)
	orgBody, _ := json.Marshal(map[string]any{
		"enabled": true,
		"config":  map[string]any{"api_key": braveKey},
	})
	orgResp := doJSON(t, "PUT", orgURL, token, "", orgBody)
	orgRespBody := readBody(t, orgResp)
	rl.CLIStep("Store Brave API key at org level",
		fmt.Sprintf("PUT %s", orgURL),
		fmt.Sprintf("HTTP %d: %s", orgResp.StatusCode, truncate(orgRespBody, 200)))
	if orgResp.StatusCode != http.StatusOK {
		rl.Failf("PUT org tool-settings: want 200, got %d — %s", orgResp.StatusCode, orgRespBody)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "DELETE", orgURL, nil)
		setAuthHeader(req, token)
		resp, _ := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		rl.Printf("removed org tool setting for brave_web_search")
	})

	// ── Step 4: verify the tool is enabled at project level ─────────────────
	rl.Section("Verify tool enabled at project level")
	_, braveToolDetail := getBraveToolIDsWithDetail(t, srv, token, projectID)
	if braveToolDetail == nil {
		rl.Failf("brave_web_search not found")
	}

	enabled, _ := braveToolDetail["enabled"].(bool)
	if !enabled {
		t.Error("expected brave_web_search to be enabled")
	}
	rl.Printf("tool enabled=%v, inheritedFrom=%q", enabled, braveToolDetail["inheritedFrom"])

	// ── Step 5: call the MCP tool — should use the org-level key ────────────
	rl.Section("Call brave_web_search via MCP (org-inherited key)")
	// Wait briefly to avoid hitting the Brave free-plan rate limit (1 req/s)
	time.Sleep(2 * time.Second)
	results := callBraveSearchViaMCP(t, srv, token, projectID, "knowledge graph AI")
	rl.Printf("search returned %d results (via org-inherited key)", len(results))

	if len(results) == 0 {
		rl.Failf("expected at least 1 search result via org-inherited key, got 0")
	}
}
