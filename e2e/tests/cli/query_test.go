// Package cli_test — query_test.go
//
// End-to-end tests for `memory query` CLI subcommand in search mode and agent
// mode.  Search mode is deterministic; agent mode is LLM-backed and best-effort.
package cli_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_QuerySearchMode verifies that `memory query --mode=search`
// returns results (or an empty set) without error.
func TestCLIInstalled_QuerySearchMode(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory query --mode=search runs without error",
		"Create project and a graph object to search for",
		"Run `memory query --mode=search` with a term matching the object",
		"Assert exit code 0 and output is non-empty",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-query-search")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Seed a graph object so there is something to find.
	rl.Section("Seed graph object")
	seedOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "PaymentGateway",
		"--description", "Handles credit card transactions",
		"--project", projectID,
	)
	rl.CLI("memory graph objects create --type Service --name PaymentGateway --project "+projectID, seedOut)

	// Run search query.
	rl.Section("Run query in search mode")
	out := mustRunCLIInDirWithHome(t, "", home,
		"query", "--mode=search", "PaymentGateway",
		"--project", projectID,
	)
	rl.CLI("memory query --mode=search PaymentGateway --project "+projectID, out)

	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		t.Errorf("expected non-empty output from query --mode=search, got empty string")
	}
	rl.Printf("search returned %d bytes of output", len(trimmed))
}

// TestCLIInstalled_QueryJSONOutput verifies that `memory query --mode=search
// --json` returns parseable JSON.
func TestCLIInstalled_QueryJSONOutput(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory query --mode=search --json returns valid JSON",
		"Create project, seed a graph object",
		"Run `memory query --mode=search --json` and validate JSON output",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-query-json")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Seed a graph object.
	rl.Section("Seed graph object")
	seedOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Component",
		"--name", "QueryJsonTestNode",
		"--project", projectID,
	)
	rl.CLI("memory graph objects create --type Component --name QueryJsonTestNode --project "+projectID, seedOut)

	// Run query with JSON output.
	rl.Section("Run query with --json")
	out := mustRunCLIInDirWithHome(t, "", home,
		"query", "--mode=search", "--json", "QueryJsonTestNode",
		"--project", projectID,
	)
	rl.CLI("memory query --mode=search --json QueryJsonTestNode --project "+projectID, out)

	trimmed := strings.TrimSpace(out)
	if !json.Valid([]byte(trimmed)) {
		t.Errorf("expected valid JSON from query --json, got:\n%s", truncate(trimmed, 500))
	}
	rl.Printf("query --json returned valid JSON (%d bytes)", len(trimmed))
}

// TestCLIInstalled_QueryAgentMode verifies that `memory query` in default agent
// mode returns a response.  This is LLM-backed and may be slower, so the test
// is lenient: it only checks for exit 0 and non-empty output.
func TestCLIInstalled_QueryAgentMode(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory query (agent mode) runs without error",
		"Create project with a graph object",
		"Run `memory query` in default agent mode",
		"Assert exit code 0 and output is non-empty (best-effort)",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-query-agent")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Seed data.
	rl.Section("Seed graph object")
	seedOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "InventoryService",
		"--description", "Manages product inventory levels",
		"--project", projectID,
	)
	rl.CLI("memory graph objects create --type Service --name InventoryService --project "+projectID, seedOut)

	// Run agent query — use runCLIInDirWithHome so a 503 or timeout does not
	// hard-fail the test.  Agent mode requires a configured provider which may
	// not be available in all test environments.
	rl.Section("Run query in agent mode")
	out, err := runCLIInDirWithHome(t, "", home,
		"query", "What services exist in this project?",
		"--project", projectID,
	)
	rl.CLI("memory query 'What services exist in this project?' --project "+projectID, out)

	if err != nil {
		// Agent mode may fail if no provider is configured — log and skip.
		rl.Printf("agent query returned error (may be expected without provider): %v", err)
		t.Skipf("memory query agent mode failed (no provider?): %v", err)
	}

	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		t.Errorf("expected non-empty output from query agent mode, got empty string")
	}
	rl.Printf("agent query returned %d bytes", len(trimmed))
}

// TestCLIInstalled_QueryWithProjectToken verifies the full scenario that was
// broken before v0.35.41–v0.35.47:
//
//   - Create a project
//   - Configure an LLM provider at org level (provider from env vars)
//   - Create a project-scoped token with the necessary scopes
//   - Use ONLY that project token (no account credentials) to run `memory query`
//   - Assert the query succeeds (exit 0, non-empty LLM response)
//
// This exercises the server-side provider resolution chain (project → org
// fallback) and the CLI's ability to authenticate with a project-scoped token.
func TestCLIInstalled_QueryWithProjectToken(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory query works with a project-scoped token and org-level provider",
		"Create ephemeral project",
		"Configure LLM provider at org level (from env vars)",
		"Create project-scoped token with data:read,agents:read,projects:read scopes",
		"Run `memory query` authenticated only via the project token",
		"Assert exit 0 and non-empty LLM response",
	)

	// Pre-check: at least one LLM provider must be configured via env vars.
	skipIfNoLLMProvider(t, rl)

	// Use the account-level home for setup operations (project create, provider
	// configure, token create).  A separate isolated home will be used for the
	// query itself, authenticated only with the project token.
	setupHome := t.TempDir()
	requireServerReady(t, setupHome, rl)

	// ── Step 1: Create project ──────────────────────────────────────────
	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-query-projtoken")
	projectID := createProject(t, setupHome, srv, name)
	deleteProjectOnCleanup(t, setupHome, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// ── Step 2: Configure org-level LLM provider ────────────────────────
	setupTestProvider(t, rl, setupHome, projectID)

	// ── Step 3: Configure project-level LLM provider ────────────────────
	configureProjectModel(t, projectID)

	// ── Step 4: Create project-scoped token ─────────────────────────────
	rl.Section("Create project-scoped token")
	tokenName := fmt.Sprintf("e2e-projtoken-%d", time.Now().UnixMilli())
	tokenOut := mustRunCLIInDirWithHome(t, "", setupHome,
		"tokens", "create",
		"--name", tokenName,
		"--project", projectID,
		"--scopes", "data:read,data:write,schema:read,projects:read,agents:read",
	)
	rl.CLI("memory tokens create --name "+tokenName+" --project "+projectID, tokenOut)

	// Extract the raw token value (emt_...) from the output.
	projectToken := parseLineField(tokenOut, "Token:")
	if projectToken == "" {
		t.Fatalf("could not extract project token value from create output:\n%s", truncate(tokenOut, 500))
	}
	rl.Printf("project token: %s...", truncate(projectToken, 20))

	// Extract token ID for cleanup.
	tokenID := parseLineField(tokenOut, "ID:")
	if tokenID != "" {
		framework.RevokeTokenOnCleanup(t, rl, setupHome, tokenID)
	}

	// ── Step 5: Run query using ONLY the project token ──────────────────
	rl.Section("Query with project token only")

	// Create a fresh home directory with NO account credentials.
	// Configure it to use only the project token for auth.
	queryHome := t.TempDir()
	mustRunCLIInDirWithHome(t, "", queryHome, "config", "set", "server_url", srv)
	mustRunCLIInDirWithHome(t, "", queryHome, "config", "set", "api_key", projectToken)
	mustRunCLIInDirWithHome(t, "", queryHome, "config", "set", "project_id", projectID)

	queryOut, queryErr := runCLIInDirWithHome(t, "", queryHome,
		"query", "Say hello in one sentence",
		"--project", projectID,
	)
	rl.CLIErr("memory query 'Say hello in one sentence' --project "+projectID, queryOut, queryErr, 0)

	if queryErr != nil {
		// Skip on billing/quota errors — infrastructure issue, not a code bug.
		if strings.Contains(queryOut, "RESOURCE_EXHAUSTED") ||
			strings.Contains(queryOut, "spending cap") ||
			strings.Contains(queryOut, "429") {
			t.Skipf("LLM API quota exceeded during query — skipping: %s", truncate(queryOut, 300))
		}
		// If it's a 503 / provider error, log details and fail (this is
		// exactly the bug we're regression-testing).
		t.Fatalf("query with project token failed: %v\noutput:\n%s", queryErr, truncate(queryOut, 1000))
	}

	trimmed := strings.TrimSpace(queryOut)
	if trimmed == "" {
		t.Errorf("expected non-empty LLM response from query, got empty string")
	}
	rl.Printf("query with project token succeeded: %d bytes", len(trimmed))
}
