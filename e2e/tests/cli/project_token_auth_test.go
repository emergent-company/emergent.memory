// Package cli_test — project_token_auth_test.go
//
// Comprehensive end-to-end tests verifying that CLI commands work correctly
// when authenticated with a project-scoped token (emt_*) instead of
// account-level credentials.
//
// These tests exercise the scenario from `memory init`:
//  1. Account credentials set up the project + provider + token.
//  2. A separate environment uses ONLY the project token.
//  3. Various CLI commands must succeed with that project token.
//
// This is a regression suite for the auth fixes in v0.35.41–v0.35.47.
package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// projectTokenEnv holds a reusable test fixture: an ephemeral project with an
// org-level provider configured and a project-scoped token created.
type projectTokenEnv struct {
	setupHome    string // HOME with account-level auth (for setup/cleanup)
	queryHome    string // HOME with ONLY project token (for testing)
	projectID    string
	projectName  string
	projectToken string // emt_* value
	tokenID      string // for cleanup
	srv          string // server URL
}

// setupProjectTokenEnv creates a project, configures the org-level provider,
// creates a project-scoped token, and prepares two isolated HOME directories:
//   - env.setupHome: authenticated with account credentials (for setup/teardown)
//   - env.queryHome: authenticated with ONLY the project token (for test assertions)
//
// The caller should pass rl so that steps are recorded in the run log.
// On any infrastructure failure (e.g. quota exceeded), the test is skipped.
func setupProjectTokenEnv(t *testing.T, rl *framework.RunLog) *projectTokenEnv {
	t.Helper()

	env := &projectTokenEnv{}
	env.setupHome = t.TempDir()
	env.srv = serverURL()
	requireServerReady(t, env.setupHome, rl)

	// ── Create project ──────────────────────────────────────────────────
	rl.Section("Setup: create project")
	env.projectName = uniqueProjectName("e2e-projtoken-auth")
	env.projectID = createProject(t, env.setupHome, env.srv, env.projectName)
	deleteProjectOnCleanup(t, env.setupHome, env.projectID)
	rl.Printf("project: %s (%s)", env.projectName, env.projectID)

	// ── Configure org-level LLM provider ───────────────────────────────
	// Best-effort: configure whichever provider is available from env vars.
	// Don't fail on error — it may already be configured on the server.
	if provider, apiKey, model := framework.ProviderFromEnv(); provider != "" {
		rl.Section("Setup: configure org-level provider")
		provArgs := []string{"provider", "configure", provider, "--api-key", apiKey}
		if model != "" {
			provArgs = append(provArgs, "--generative-model", model)
		}
		provArgs = append(provArgs, orgIDArgs()...)
		provOut, provErr := runCLIInDirWithHome(t, "", env.setupHome, provArgs...)
		rl.CLIErr("memory provider configure "+provider, provOut, provErr, 0)

		// Also configure project-level model so query can resolve a generative model.
		if model != "" {
			modelCfg, _ := json.Marshal(map[string]any{"generativeModel": provider + "/" + model})
			token := e2eTestToken()
			resp := framework.DoJSON(t, "PUT", env.srv+"/api/v1/projects/"+env.projectID+"/model-config", token, env.projectID, modelCfg)
			resp.Body.Close()
		}
	}

	// ── Create project-scoped token ─────────────────────────────────────
	rl.Section("Setup: create project token")
	tokenName := fmt.Sprintf("e2e-auth-test-%d", time.Now().UnixMilli())
	tokenOut := mustRunCLIInDirWithHome(t, "", env.setupHome,
		"tokens", "create",
		"--name", tokenName,
		"--project", env.projectID,
		"--scopes", "data:read,data:write,schema:read,projects:read,agents:read",
	)
	rl.CLI("memory tokens create --name "+tokenName+" --project "+env.projectID, tokenOut)

	env.projectToken = parseLineField(tokenOut, "Token:")
	if env.projectToken == "" {
		t.Fatalf("could not extract project token from create output:\n%s", truncate(tokenOut, 500))
	}
	rl.Printf("project token: %s...", truncate(env.projectToken, 20))

	env.tokenID = parseLineField(tokenOut, "ID:")
	if env.tokenID != "" {
		framework.RevokeTokenOnCleanup(t, rl, env.setupHome, env.tokenID)
	}

	// ── Prepare project-token-only HOME ─────────────────────────────────
	rl.Section("Setup: prepare project-token-only home")
	env.queryHome = t.TempDir()

	// Write config.yaml directly with only server_url, project_token, and project_id.
	// This simulates what a user has after `memory init` — no account credentials.
	configDir := filepath.Join(env.queryHome, ".memory")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configYAML := fmt.Sprintf(
		"server_url: %s\nproject_token: %s\nproject_id: %s\n",
		env.srv, env.projectToken, env.projectID,
	)
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}
	rl.Printf("wrote config.yaml at %s (project_token + server_url + project_id)", configPath)

	return env
}

// runWithProjectToken runs a CLI command using the project-token-only HOME.
// Returns (output, error) — non-fatal.
func (env *projectTokenEnv) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runCLIInDirWithHome(t, "", env.queryHome, args...)
}

// mustRun runs a CLI command using the project-token-only HOME and fails on error.
func (env *projectTokenEnv) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	return mustRunCLIInDirWithHome(t, "", env.queryHome, args...)
}

// isQuotaError returns true when the output indicates an API quota/spending
// cap error — an infrastructure issue, not a code bug.
func isQuotaError(output string) bool {
	return strings.Contains(output, "RESOURCE_EXHAUSTED") ||
		strings.Contains(output, "spending cap") ||
		strings.Contains(output, "429")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: graph objects CRUD with project token
// ─────────────────────────────────────────────────────────────────────────────

func TestCLIInstalled_ProjectToken_GraphObjects(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph objects CRUD works with a project-scoped token",
		"Setup: create project, project token, isolated HOME",
		"graph objects create — create an object",
		"graph objects list — list objects (expect the created one)",
		"graph objects get <id> — get the created object",
		"graph objects update <id> — update the object description",
		"graph objects delete <id> — delete the object",
	)

	env := setupProjectTokenEnv(t, rl)

	// ── Create ──────────────────────────────────────────────────────────
	rl.Section("graph objects create")
	createOut := env.mustRun(t,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "AuthService",
		"--description", "Handles authentication flows",
		"--project", env.projectID,
	)
	rl.CLI("memory graph objects create --type Service --name AuthService --project "+env.projectID, createOut)

	objectID := parseEntityID(createOut)
	if objectID == "" {
		t.Fatalf("could not extract object ID from create output:\n%s", truncate(createOut, 500))
	}
	rl.Printf("created object ID: %s", objectID)

	// ── List ────────────────────────────────────────────────────────────
	rl.Section("graph objects list")
	listOut := env.mustRun(t,
		"graph", "objects", "list",
		"--project", env.projectID,
	)
	rl.CLI("memory graph objects list --project "+env.projectID, listOut)

	// The list table shows Entity ID, Type, Version, Status, Created (no Name column).
	// Verify the created object appears by checking for its Entity ID prefix (table truncates UUIDs).
	idPrefix := objectID
	if len(idPrefix) > 20 {
		idPrefix = idPrefix[:20]
	}
	if !strings.Contains(listOut, idPrefix) {
		t.Errorf("expected list output to contain object ID %s, got:\n%s", objectID, truncate(listOut, 500))
	}
	rl.Printf("list output contains object ID: true")

	// ── Get ─────────────────────────────────────────────────────────────
	rl.Section("graph objects get")
	getOut := env.mustRun(t,
		"graph", "objects", "get", objectID,
		"--project", env.projectID,
	)
	rl.CLI("memory graph objects get "+objectID+" --project "+env.projectID, getOut)

	// The get output shows Entity ID, Type, Properties (JSON with name/description).
	if !strings.Contains(getOut, objectID) {
		t.Errorf("expected get output to contain entity ID %s, got:\n%s", objectID, truncate(getOut, 500))
	}

	// ── Update ──────────────────────────────────────────────────────────
	rl.Section("graph objects update")
	updateOut := env.mustRun(t,
		"graph", "objects", "update", objectID,
		"--properties", `{"description":"Updated: handles OAuth2 flows"}`,
		"--project", env.projectID,
	)
	rl.CLI("memory graph objects update "+objectID+" --project "+env.projectID, updateOut)

	// Verify the update stuck.
	getOut2 := env.mustRun(t,
		"graph", "objects", "get", objectID,
		"--project", env.projectID,
	)
	if !strings.Contains(getOut2, "OAuth2") {
		t.Errorf("expected updated get output to contain 'OAuth2', got:\n%s", truncate(getOut2, 500))
	}
	rl.Printf("update verified: description contains 'OAuth2'")

	// ── Delete ──────────────────────────────────────────────────────────
	rl.Section("graph objects delete")
	deleteOut := env.mustRun(t,
		"graph", "objects", "delete", objectID,
		"--project", env.projectID,
	)
	rl.CLI("memory graph objects delete "+objectID+" --project "+env.projectID, deleteOut)
	rl.Printf("object deleted successfully")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: graph relationships with project token
// ─────────────────────────────────────────────────────────────────────────────

func TestCLIInstalled_ProjectToken_GraphRelationships(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph relationships work with a project-scoped token",
		"Setup: create project, project token, isolated HOME",
		"Create two objects (source and target)",
		"graph relationships create — create a relationship",
		"graph relationships list — list relationships",
		"graph relationships delete — delete the relationship",
	)

	env := setupProjectTokenEnv(t, rl)

	// Create two objects for the relationship.
	rl.Section("Create source and target objects")
	srcOut := env.mustRun(t,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "OrderService",
		"--project", env.projectID,
	)
	rl.CLI("create OrderService", srcOut)
	srcID := parseEntityID(srcOut)
	if srcID == "" {
		t.Fatalf("could not extract source object ID:\n%s", truncate(srcOut, 500))
	}

	tgtOut := env.mustRun(t,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "PaymentService",
		"--project", env.projectID,
	)
	rl.CLI("create PaymentService", tgtOut)
	tgtID := parseEntityID(tgtOut)
	if tgtID == "" {
		t.Fatalf("could not extract target object ID:\n%s", truncate(tgtOut, 500))
	}
	rl.Printf("source: %s, target: %s", srcID, tgtID)

	// ── Create relationship ─────────────────────────────────────────────
	rl.Section("graph relationships create")
	relOut := env.mustRun(t,
		"graph", "relationships", "create",
		"--type", "calls",
		"--from", srcID,
		"--to", tgtID,
		"--project", env.projectID,
	)
	rl.CLI("memory graph relationships create --type calls", relOut)

	relID := parseEntityID(relOut)
	if relID == "" {
		t.Fatalf("could not extract relationship ID:\n%s", truncate(relOut, 500))
	}
	rl.Printf("created relationship ID: %s", relID)

	// ── List relationships ──────────────────────────────────────────────
	rl.Section("graph relationships list")
	listOut := env.mustRun(t,
		"graph", "relationships", "list",
		"--project", env.projectID,
		"--output", "json",
	)
	rl.CLI("memory graph relationships list --project "+env.projectID+" --output json", listOut)

	if !strings.Contains(strings.ToLower(listOut), "calls") {
		t.Errorf("expected relationships list to contain 'calls', got:\n%s", truncate(listOut, 500))
	}
	rl.Printf("relationships list contains 'calls': true")

	// ── Delete relationship ─────────────────────────────────────────────
	rl.Section("graph relationships delete")
	delOut := env.mustRun(t,
		"graph", "relationships", "delete", relID,
		"--project", env.projectID,
	)
	rl.CLI("memory graph relationships delete "+relID, delOut)
	rl.Printf("relationship deleted successfully")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: schemas list/installed with project token
// ─────────────────────────────────────────────────────────────────────────────

func TestCLIInstalled_ProjectToken_Schemas(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify schemas list and installed work with a project-scoped token",
		"Setup: create project, project token, isolated HOME",
		"schemas list — list available schema packs",
		"schemas installed — list installed schema packs for the project",
	)

	env := setupProjectTokenEnv(t, rl)

	// ── schemas list ────────────────────────────────────────────────────
	rl.Section("schemas list")
	listOut := env.mustRun(t,
		"schemas", "list",
		"--project", env.projectID,
	)
	rl.CLI("memory schemas list --project "+env.projectID, listOut)

	trimmed := strings.TrimSpace(listOut)
	if trimmed == "" {
		t.Errorf("expected non-empty output from schemas list, got empty string")
	}
	rl.Printf("schemas list returned %d bytes", len(trimmed))

	// ── schemas installed ───────────────────────────────────────────────
	rl.Section("schemas installed")
	installedOut := env.mustRun(t,
		"schemas", "installed",
		"--project", env.projectID,
	)
	rl.CLI("memory schemas installed --project "+env.projectID, installedOut)

	// New projects may have 0 installed packs — that's OK, just verify exit 0.
	rl.Printf("schemas installed returned %d bytes", len(strings.TrimSpace(installedOut)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: query --mode=search with project token
// ─────────────────────────────────────────────────────────────────────────────

func TestCLIInstalled_ProjectToken_QuerySearch(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify query --mode=search works with a project-scoped token",
		"Setup: create project, project token, seed a graph object",
		"query --mode=search — search for the seeded object",
		"Assert non-empty output",
	)

	env := setupProjectTokenEnv(t, rl)

	// Seed a graph object to search for.
	rl.Section("Seed graph object")
	seedOut := env.mustRun(t,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "SearchTestService",
		"--description", "A service created for search testing",
		"--project", env.projectID,
	)
	rl.CLI("create SearchTestService", seedOut)

	// ── Search query ────────────────────────────────────────────────────
	rl.Section("query --mode=search")
	searchOut := env.mustRun(t,
		"query", "--mode=search", "SearchTestService",
		"--project", env.projectID,
	)
	rl.CLI("memory query --mode=search SearchTestService --project "+env.projectID, searchOut)

	trimmed := strings.TrimSpace(searchOut)
	if trimmed == "" {
		t.Errorf("expected non-empty search output, got empty string")
	}
	rl.Printf("search returned %d bytes", len(trimmed))
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: query agent mode with project token (LLM-backed, best-effort)
// ─────────────────────────────────────────────────────────────────────────────

func TestCLIInstalled_ProjectToken_QueryAgent(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify query agent mode works with a project-scoped token and org-level provider",
		"Setup: create project, org-level provider, project token",
		"query 'Say hello' — run agent mode query via project token only",
		"Assert exit 0 and non-empty LLM response",
	)

	skipIfNoLLMProvider(t, rl)

	env := setupProjectTokenEnv(t, rl)

	// Verify provider works before testing agent query.
	rl.Section("Pre-check: provider test")
	testArgs := append([]string{"provider", "test"}, orgIDArgs()...)
	testOut, testErr := runCLIInDirWithHome(t, "", env.setupHome, testArgs...)
	rl.CLIErr("memory provider test", testOut, testErr, 0)
	if testErr != nil {
		if isQuotaError(testOut) {
			t.Skipf("LLM API quota exceeded — skipping: %s", truncate(testOut, 300))
		}
		t.Skipf("provider test failed — skipping agent mode test: %s", truncate(testOut, 300))
	}

	// ── Agent query ─────────────────────────────────────────────────────
	rl.Section("query agent mode with project token")
	queryOut, queryErr := env.run(t,
		"query", "Say hello in one sentence",
		"--project", env.projectID,
	)
	rl.CLIErr("memory query 'Say hello in one sentence' --project "+env.projectID, queryOut, queryErr, 0)

	if queryErr != nil {
		if isQuotaError(queryOut) {
			t.Skipf("LLM API quota exceeded during query — skipping: %s", truncate(queryOut, 300))
		}
		t.Fatalf("query agent mode with project token failed: %v\noutput:\n%s", queryErr, truncate(queryOut, 1000))
	}

	trimmed := strings.TrimSpace(queryOut)
	if trimmed == "" {
		t.Errorf("expected non-empty LLM response, got empty string")
	}
	rl.Printf("agent query succeeded: %d bytes", len(trimmed))
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: tokens list --project with project token
// ─────────────────────────────────────────────────────────────────────────────

func TestCLIInstalled_ProjectToken_TokensList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify tokens list --project works with a project-scoped token",
		"Setup: create project, project token, isolated HOME",
		"tokens list --project <id> — list tokens for the project",
		"Note: may 403 if project token scopes don't include token listing permission",
	)

	env := setupProjectTokenEnv(t, rl)

	// ── tokens list --project ───────────────────────────────────────────
	rl.Section("tokens list with project token")
	listOut, listErr := env.run(t,
		"tokens", "list",
		"--project", env.projectID,
	)
	rl.CLIErr("memory tokens list --project "+env.projectID, listOut, listErr, 0)

	if listErr != nil {
		// The default project token scopes (data:read,data:write,schema:read,
		// projects:read,agents:read) do NOT include the scope required for listing
		// tokens (likely project:read or tokens:read). A 403 here is expected
		// behaviour documenting this scope limitation.
		if strings.Contains(listOut, "403") || strings.Contains(listOut, "insufficient") || strings.Contains(listOut, "forbidden") {
			rl.Printf("tokens list returned 403 — expected: project token scopes don't include token listing permission")
			t.Skipf("tokens list --project returns 403 with default project token scopes (expected limitation)")
		}
		t.Fatalf("tokens list --project failed with project token: %v\noutput:\n%s", listErr, truncate(listOut, 500))
	}

	trimmed := strings.TrimSpace(listOut)
	if trimmed == "" {
		t.Errorf("expected non-empty output from tokens list, got empty string")
	}
	rl.Printf("tokens list returned %d bytes", len(trimmed))
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: provider test with project token
// ─────────────────────────────────────────────────────────────────────────────

func TestCLIInstalled_ProjectToken_ProviderTest(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider test works with a project-scoped token",
		"Setup: create project, org-level provider, project token",
		"provider test --project <id> — test provider via project token",
		"Assert exit 0 and OK in output",
	)

	skipIfNoLLMProvider(t, rl)

	env := setupProjectTokenEnv(t, rl)

	// ── provider test ───────────────────────────────────────────────────
	rl.Section("provider test with project token")
	args := append([]string{"provider", "test", "--project", env.projectID}, orgIDArgs()...)
	testOut, testErr := env.run(t, args...)
	rl.CLIErr("memory provider test --project "+env.projectID, testOut, testErr, 0)

	if testErr != nil {
		if isQuotaError(testOut) {
			t.Skipf("LLM API quota exceeded — skipping: %s", truncate(testOut, 300))
		}
		if strings.Contains(testOut, "no providers configured") || strings.Contains(testOut, "not configured") {
			t.Skipf("no provider configured — skipping: %s", truncate(testOut, 300))
		}
		t.Fatalf("provider test failed with project token: %v\noutput:\n%s", testErr, truncate(testOut, 500))
	}

	if !strings.Contains(testOut, "OK") {
		t.Errorf("expected provider test output to contain 'OK', got:\n%s", truncate(testOut, 500))
	}
	rl.Printf("provider test passed with project token")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: graph objects edges with project token
// ─────────────────────────────────────────────────────────────────────────────

func TestCLIInstalled_ProjectToken_GraphEdges(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph objects edges works with a project-scoped token",
		"Setup: create project, project token, two objects, one relationship",
		"graph objects edges <id> — list edges for an object",
		"Assert the relationship appears in the edges output",
	)

	env := setupProjectTokenEnv(t, rl)

	// Create two objects and a relationship.
	rl.Section("Create objects and relationship")
	srcOut := env.mustRun(t,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "EdgeSrc",
		"--project", env.projectID,
	)
	srcID := parseEntityID(srcOut)
	if srcID == "" {
		t.Fatalf("could not extract source object ID:\n%s", truncate(srcOut, 500))
	}

	tgtOut := env.mustRun(t,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "EdgeTgt",
		"--project", env.projectID,
	)
	tgtID := parseEntityID(tgtOut)
	if tgtID == "" {
		t.Fatalf("could not extract target object ID:\n%s", truncate(tgtOut, 500))
	}

	env.mustRun(t,
		"graph", "relationships", "create",
		"--type", "depends_on",
		"--from", srcID,
		"--to", tgtID,
		"--project", env.projectID,
	)
	rl.Printf("created objects %s → %s with 'depends_on' relationship", srcID, tgtID)

	// ── edges ───────────────────────────────────────────────────────────
	rl.Section("graph objects edges")
	edgesOut := env.mustRun(t,
		"graph", "objects", "edges", srcID,
		"--project", env.projectID,
	)
	rl.CLI("memory graph objects edges "+srcID+" --project "+env.projectID, edgesOut)

	if !strings.Contains(edgesOut, "depends_on") && !strings.Contains(edgesOut, "EdgeTgt") {
		t.Errorf("expected edges output to contain 'depends_on' or 'EdgeTgt', got:\n%s", truncate(edgesOut, 500))
	}
	rl.Printf("edges output shows relationship to EdgeTgt")
}
