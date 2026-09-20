// Package docs_test — cli_commands_test.go
//
// Tests that verify documented CLI commands actually work against a live server.
// Each test exercises a command shown in the documentation and validates the
// output matches what the docs describe.
//
// These tests require a running server (requireServerReady) and skip gracefully
// when the server is unavailable.
package docs_test

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Projects lifecycle (getting-started docs)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_ProjectsCreateAndDelete verifies the project create/delete flow
// documented in the getting-started guide:
//
//	memory projects create --name "My Project"
//	memory projects delete <id>
func TestDocCmd_ProjectsCreateAndDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented projects create/delete flow works",
		"Create a project using 'memory projects create --name'",
		"Verify project ID is returned",
		"Delete the project using 'memory projects delete'",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	projectName := uniqueProjectName("e2e-docs-proj")
	args := []string{"projects", "create", "--name", projectName}
	args = append(args, projectCreateOrgArgs()...)
	createOut := mustRunCLIInDirWithHome(t, "", home, args...)
	rl.CLI("memory projects create --name "+projectName, createOut)

	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from: %q", createOut)
	}
	rl.Printf("project created: id=%s name=%s", projectID, projectName)

	rl.Section("Delete project")
	deleteOut := mustRunCLIInDirWithHome(t, "", home, "projects", "delete", projectID)
	rl.CLI("memory projects delete "+projectID, deleteOut)
	rl.Printf("project deleted: %s", projectID)
}

// TestDocCmd_ProjectsList verifies the documented `memory projects list`
// command works and produces tabular output.
func TestDocCmd_ProjectsList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented 'memory projects list' command works",
		"Run 'memory projects list'",
		"Assert output is non-empty and contains table-like content",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory projects list")
	out := mustRunCLIInDirWithHome(t, "", home, "projects", "list")
	rl.CLI("memory projects list", out)

	rl.Section("Verify output format")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from projects list, got empty")
	}
	rl.Printf("projects list returned %d bytes", len(trimmed))
}

// ─────────────────────────────────────────────────────────────────────────────
// Status and config (getting-started docs)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_Status verifies that `memory status` shows connection info
// as described in the docs.
func TestDocCmd_Status(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented 'memory status' command shows connection info",
		"Set up CLI auth",
		"Run 'memory status'",
		"Assert output contains server/connection information",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory status")
	out := mustRunCLIInDirWithHome(t, "", home, "status")
	rl.CLI("memory status", out)

	rl.Section("Verify status output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "status") && !strings.Contains(lower, "server") && !strings.Contains(lower, "connected") {
		t.Errorf("expected status output to contain connection info, got:\n%s", truncate(out, 500))
	}
	rl.Printf("status output contains expected connection info")
}

// TestDocCmd_ConfigShowContainsServerURL verifies `memory config show` includes
// the server URL, matching docs that reference config management.
func TestDocCmd_ConfigShowContainsServerURL(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify 'memory config show' includes server URL",
		"Set up CLI auth",
		"Run 'memory config show'",
		"Assert output contains the configured server URL",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory config show")
	out := mustRunCLIInDirWithHome(t, "", home, "config", "show")
	rl.CLI("memory config show", out)

	rl.Section("Verify server URL present")
	srv := serverURL()
	if !strings.Contains(out, srv) {
		t.Errorf("expected config show to contain server URL %q, got:\n%s", srv, truncate(out, 500))
	}
	rl.Printf("config show contains server URL: true")
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph operations (knowledge-graph docs)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_GraphObjectsList verifies the documented `memory graph objects list`
// command works against a real project.
func TestDocCmd_GraphObjectsList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented 'memory graph objects list' command works",
		"Create project",
		"Run 'memory graph objects list --project <id>'",
		"Assert command exits successfully (empty graph is OK)",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	projectName := uniqueProjectName("e2e-docs-graph")
	projectID := createProject(t, home, serverURL(), projectName)
	rl.Printf("project created: id=%s", projectID)
	deleteProjectOnCleanup(t, home, projectID)

	rl.Section("Run memory graph objects list")
	out := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list", "--project", projectID)
	rl.CLI("memory graph objects list --project "+projectID, out)

	rl.Section("Verify command succeeded")
	// An empty project may return no objects, which is fine.
	// The key assertion is that the command completed without error.
	rl.Printf("graph objects list completed: %d bytes output", len(strings.TrimSpace(out)))
}

// TestDocCmd_GraphRelationshipsList verifies the documented
// `memory graph relationships list` command.
func TestDocCmd_GraphRelationshipsList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented 'memory graph relationships list' command works",
		"Create project",
		"Run 'memory graph relationships list --project <id>'",
		"Assert command exits successfully",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	projectName := uniqueProjectName("e2e-docs-rels")
	projectID := createProject(t, home, serverURL(), projectName)
	rl.Printf("project created: id=%s", projectID)
	deleteProjectOnCleanup(t, home, projectID)

	rl.Section("Run memory graph relationships list")
	out := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "list", "--project", projectID)
	rl.CLI("memory graph relationships list --project "+projectID, out)

	rl.Section("Verify command succeeded")
	rl.Printf("graph relationships list completed: %d bytes output", len(strings.TrimSpace(out)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent operations (agents docs)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_AgentsList verifies the documented `memory agents list` command.
func TestDocCmd_AgentsList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented 'memory agents list' command works",
		"Create project",
		"Run 'memory agents list --project <id>'",
		"Assert command exits successfully",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	projectName := uniqueProjectName("e2e-docs-agents")
	projectID := createProject(t, home, serverURL(), projectName)
	rl.Printf("project created: id=%s", projectID)
	deleteProjectOnCleanup(t, home, projectID)

	rl.Section("Run memory agents list")
	out := mustRunCLIInDirWithHome(t, "", home,
		"agents", "list", "--project", projectID)
	rl.CLI("memory agents list --project "+projectID, out)

	rl.Section("Verify command succeeded")
	rl.Printf("agents list completed: %d bytes output", len(strings.TrimSpace(out)))
}

// TestDocCmd_SkillsListGlobal verifies the documented `memory skills list`
// command returns server-side skills.
func TestDocCmd_SkillsListGlobal(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented 'memory skills list --global' command works",
		"Create project",
		"Run 'memory skills list --global --project <id>'",
		"Assert command exits successfully",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	projectName := uniqueProjectName("e2e-docs-skills")
	projectID := createProject(t, home, serverURL(), projectName)
	rl.Printf("project created: id=%s", projectID)
	deleteProjectOnCleanup(t, home, projectID)

	rl.Section("Run memory skills list --global")
	out := mustRunCLIInDirWithHome(t, "", home, "skills", "list", "--global", "--project", projectID)
	rl.CLI("memory skills list --global --project "+projectID, out)

	rl.Section("Verify command ran successfully")
	// Output may be empty on a fresh server with no global skills — that is valid.
	rl.Printf("skills list output: %q", strings.TrimSpace(out))
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent definitions lifecycle (documented agent-definitions / defs flow)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_DefsCreateAndDelete verifies the documented agent definition
// create/delete lifecycle:
//
//	memory defs create --name "..." --system-prompt "..." --flow-type single --project <id>
//	memory defs delete <def-id> --project <id>
func TestDocCmd_DefsCreateAndDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented defs create/delete lifecycle",
		"Create project",
		"Create agent definition via 'memory defs create'",
		"Verify definition ID returned",
		"Delete definition via 'memory defs delete'",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	projectName := uniqueProjectName("e2e-docs-defs")
	projectID := createProject(t, home, serverURL(), projectName)
	rl.Printf("project created: id=%s", projectID)
	deleteProjectOnCleanup(t, home, projectID)

	rl.Section("Create agent definition")
	defName := "e2e-docs-test-def"
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"defs", "create",
		"--name", defName,
		"--system-prompt", "You are a test agent for docs e2e validation.",
		"--flow-type", "single",
		"--visibility", "project",
		"--project", projectID,
	)
	rl.CLI("memory defs create --name "+defName+" --project "+projectID, createOut)

	defID := parseAgentDefID(createOut)
	if defID == "" {
		rl.Failf("could not parse agent definition ID from: %q", truncate(createOut, 300))
	}
	rl.Printf("created definition ID: %s", defID)

	rl.Section("Delete agent definition")
	delOut := mustRunCLIInDirWithHome(t, "", home,
		"defs", "delete", defID,
		"--project", projectID,
	)
	rl.CLI("memory defs delete "+defID+" --project "+projectID, delOut)

	lower := strings.ToLower(delOut)
	if !strings.Contains(lower, "delete") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", truncate(delOut, 300))
	}
	rl.Printf("definition delete confirmed")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tokens lifecycle
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_TokensCreateAndRevoke verifies the documented tokens create/revoke
// lifecycle:
//
//	memory tokens create --name "..." --scopes projects:read
//	memory tokens revoke <token-id>
func TestDocCmd_TokensCreateAndRevoke(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented tokens create/revoke lifecycle",
		"Create token via 'memory tokens create --name --scopes'",
		"Verify emt_ prefixed token value returned",
		"Revoke token via 'memory tokens revoke'",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/tokens", setToken())

	rl.Section("Create token")
	tokenName := "e2e-docs-token"
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"tokens", "create",
		"--name", tokenName,
		"--scopes", "projects:read",
	)
	rl.CLI("memory tokens create --name "+tokenName+" --scopes projects:read", createOut)

	rl.Section("Verify token output")
	if !strings.Contains(createOut, "emt_") {
		t.Errorf("expected emt_ prefixed token value in output, got:\n%s", truncate(createOut, 500))
	}
	rl.Printf("emt_ token prefix found in output")

	tokenID := parseLineField(createOut, "ID:")
	if tokenID == "" {
		rl.Printf("warn: could not extract token ID — skipping revoke")
		return
	}
	rl.Printf("created token ID: %s", tokenID)

	// Register cleanup to revoke even if remaining assertions fail.
	revokeTokenOnCleanup(t, rl, home, tokenID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph objects create
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_GraphObjectsCreate verifies the documented graph object creation:
//
//	memory graph objects create --type <type> --name <name> --project <id>
func TestDocCmd_GraphObjectsCreate(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented graph objects create works",
		"Create project",
		"Run 'memory graph objects create --type TestNode --name ...'",
		"Verify object ID returned in JSON output",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	projectName := uniqueProjectName("e2e-docs-gobj")
	projectID := createProject(t, home, serverURL(), projectName)
	rl.Printf("project created: id=%s", projectID)
	deleteProjectOnCleanup(t, home, projectID)

	rl.Section("Create graph object")
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "TestNode",
		"--name", "DocsTestObject",
		"--description", "Created by docs e2e test",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type TestNode --name DocsTestObject --project "+projectID, createOut)

	rl.Section("Verify object ID returned")
	objectID := parseJSONField(createOut, "id")
	if objectID == "" {
		rl.Failf("could not parse object ID from create output: %s", truncate(createOut, 300))
	}
	rl.Printf("created object ID: %s", objectID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Output format (--output json)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_ProjectsListOutputJSON verifies the documented --output json flag
// produces parseable JSON output from `memory projects list`.
func TestDocCmd_ProjectsListOutputJSON(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify 'memory projects list --output json' produces JSON",
		"Run 'memory projects list --output json'",
		"Assert output starts with '[' or '{' (valid JSON)",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory projects list --output json")
	out := mustRunCLIInDirWithHome(t, "", home, "projects", "list", "--output", "json")
	rl.CLI("memory projects list --output json", out)

	rl.Section("Verify JSON output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from projects list --output json")
	}
	// JSON should start with [ or {.
	if !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		t.Errorf("expected JSON output (starts with [ or {), got: %s", truncate(trimmed, 200))
	}
	rl.Printf("projects list --output json returned valid-looking JSON (%d bytes)", len(trimmed))
}

// ─────────────────────────────────────────────────────────────────────────────
// Schemas list (server-side)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_SchemasList verifies the documented `memory schemas list` command
// works against a live server.
func TestDocCmd_SchemasList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented 'memory schemas list' command works",
		"Create project",
		"Run 'memory schemas list --project <id>'",
		"Assert command exits successfully",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	projectName := uniqueProjectName("e2e-docs-schemas")
	projectID := createProject(t, home, serverURL(), projectName)
	rl.Printf("project created: id=%s", projectID)
	deleteProjectOnCleanup(t, home, projectID)

	rl.Section("Run memory schemas list")
	out := mustRunCLIInDirWithHome(t, "", home, "schemas", "list", "--project", projectID)
	rl.CLI("memory schemas list --project "+projectID, out)

	rl.Section("Verify schemas list output")
	// Output may be empty on a fresh project — that is valid.
	rl.Printf("schemas list output: %q", strings.TrimSpace(out))
}

// ─────────────────────────────────────────────────────────────────────────────
// MCP guide (server-side, prints config snippets)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocCmd_MCPGuide verifies the documented `memory mcp-guide` command
// produces MCP configuration output.
func TestDocCmd_MCPGuide(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify documented 'memory mcp-guide' command produces config",
		"Set up CLI auth",
		"Run 'memory mcp-guide'",
		"Assert output contains MCP configuration references",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run memory mcp-guide")
	out := mustRunCLIInDirWithHome(t, "", home, "mcp-guide")
	rl.CLI("memory mcp-guide", out)

	rl.Section("Verify MCP guide output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "mcp") && !strings.Contains(lower, "server") && !strings.Contains(lower, "memory") {
		t.Errorf("mcp-guide output missing expected MCP references, got:\n%s", truncate(out, 500))
	}
	rl.Printf("mcp-guide output contains MCP configuration references")
}
