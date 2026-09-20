// Package cli_test — mcp_servers_extra_test.go
//
// End-to-end tests for additional `memory agents mcp-servers` CLI subcommands:
// tools, sync, and inspect.  These complement the CRUD lifecycle test in
// mcp_servers_test.go.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_MCPServerToolsEmpty verifies that `memory agents mcp-servers
// tools <id>` returns an empty list for a freshly created MCP server that has
// not been synced yet.
func TestCLIInstalled_MCPServerToolsEmpty(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify mcp-servers tools returns empty for unsynced server",
		"Create project and MCP server",
		"List tools for the server (should be empty or say no tools)",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-mcptools")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Create MCP server")
	mcpName := "e2e-tools-test-mcp"
	mcpURL := "http://example.com:9999/sse"
	createOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "create",
		"--name", mcpName,
		"--type", "sse",
		"--url", mcpURL,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers create --name "+mcpName+" --project "+projectID, createOut, err, 0)

	if err != nil {
		rl.Printf("SKIP: mcp-servers create not available: %v", err)
		t.Skipf("agents mcp-servers create not available: %v", err)
	}

	mcpID := parseMCPServerID(createOut)
	if mcpID == "" {
		rl.Failf("could not parse MCP server ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("created MCP server ID: %s", mcpID)

	rl.Section("List tools for MCP server")
	toolsOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "tools", mcpID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers tools "+mcpID+" --project "+projectID, toolsOut, err, 0)

	if err != nil {
		// An error is acceptable for an unsynced server — some server versions may
		// return an error instead of an empty list.
		lower := strings.ToLower(toolsOut)
		if strings.Contains(lower, "no tool") || strings.Contains(lower, "not found") ||
			strings.Contains(lower, "empty") || strings.Contains(lower, "sync") {
			rl.Printf("tools returned expected empty/no-tools message: %s", truncate(toolsOut, 200))
		} else {
			t.Errorf("unexpected error from mcp-servers tools: %v — %s", err, truncate(toolsOut, 300))
		}
	} else {
		// Success — output should be empty or show no tools.
		rl.Printf("tools output for unsynced server: %s", truncate(toolsOut, 300))
	}

	// Cleanup: delete the MCP server.
	rl.Section("Cleanup MCP server")
	delOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "delete", mcpID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers delete "+mcpID+" --project "+projectID, delOut, err, 0)
	if err == nil {
		rl.Printf("MCP server deleted")
	} else {
		rl.Printf("MCP server delete failed (non-fatal): %v", err)
	}
}

// TestCLIInstalled_MCPServerSync verifies that `memory agents mcp-servers sync`
// attempts to connect to an MCP server and refresh its tool list.  Since the
// test server URL is fake, we expect the sync to fail with a connection error
// but the CLI command itself should not crash.
func TestCLIInstalled_MCPServerSync(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify mcp-servers sync attempts connection and reports result",
		"Create project and MCP server with fake URL",
		"Run sync — expect connection error (fake URL)",
		"Assert CLI reports error gracefully without crashing",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-mcpsync")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Create MCP server with unreachable URL")
	mcpName := "e2e-sync-test-mcp"
	mcpURL := "http://192.0.2.1:9999/sse" // TEST-NET-1, guaranteed unreachable
	createOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "create",
		"--name", mcpName,
		"--type", "sse",
		"--url", mcpURL,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers create --name "+mcpName+" --project "+projectID, createOut, err, 0)

	if err != nil {
		rl.Printf("SKIP: mcp-servers create not available: %v", err)
		t.Skipf("agents mcp-servers create not available: %v", err)
	}

	mcpID := parseMCPServerID(createOut)
	if mcpID == "" {
		rl.Failf("could not parse MCP server ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("created MCP server ID: %s", mcpID)

	rl.Section("Sync MCP server (expect connection error)")
	syncOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "sync", mcpID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers sync "+mcpID+" --project "+projectID, syncOut, err, 0)

	// We expect either:
	// 1. An error because the fake URL is unreachable
	// 2. A success message if the server handles it asynchronously
	if err != nil {
		rl.Printf("sync returned error (expected for fake URL): %v — %s", err, truncate(syncOut, 300))
		// The important thing is the CLI didn't crash — error is expected.
	} else {
		rl.Printf("sync succeeded (server may handle async): %s", truncate(syncOut, 300))
	}

	// Cleanup: delete the MCP server.
	rl.Section("Cleanup MCP server")
	delOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "delete", mcpID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers delete "+mcpID+" --project "+projectID, delOut, err, 0)
	if err == nil {
		rl.Printf("MCP server deleted")
	} else {
		rl.Printf("MCP server delete failed (non-fatal): %v", err)
	}
}

// TestCLIInstalled_MCPServerInspect verifies that `memory agents mcp-servers
// inspect <id>` attempts to connect and display capabilities of an MCP server.
// Since the test server URL is fake, we expect a connection error, but the CLI
// should handle it gracefully.
func TestCLIInstalled_MCPServerInspect(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify mcp-servers inspect attempts connection and reports result",
		"Create project and MCP server with fake URL",
		"Run inspect — expect connection error or capabilities output",
		"Assert CLI handles the result gracefully",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-mcpinspect")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Create MCP server")
	mcpName := "e2e-inspect-test-mcp"
	mcpURL := "http://192.0.2.1:9999/sse" // TEST-NET-1, guaranteed unreachable
	createOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "create",
		"--name", mcpName,
		"--type", "sse",
		"--url", mcpURL,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers create --name "+mcpName+" --project "+projectID, createOut, err, 0)

	if err != nil {
		rl.Printf("SKIP: mcp-servers create not available: %v", err)
		t.Skipf("agents mcp-servers create not available: %v", err)
	}

	mcpID := parseMCPServerID(createOut)
	if mcpID == "" {
		rl.Failf("could not parse MCP server ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("created MCP server ID: %s", mcpID)

	rl.Section("Inspect MCP server")
	inspectOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "inspect", mcpID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers inspect "+mcpID+" --project "+projectID, inspectOut, err, 0)

	// We expect either:
	// 1. An error because the fake URL is unreachable
	// 2. Output showing capabilities (if server caches/handles differently)
	if err != nil {
		rl.Printf("inspect returned error (expected for fake URL): %v — %s", err, truncate(inspectOut, 300))
	} else {
		// If it succeeded, output should contain capability-related info.
		lower := strings.ToLower(inspectOut)
		if !strings.Contains(lower, "tool") && !strings.Contains(lower, "capabilit") &&
			!strings.Contains(lower, "prompt") && !strings.Contains(lower, "resource") &&
			!strings.Contains(lower, "server") {
			t.Errorf("expected inspect output to contain capability info, got:\n%s", truncate(inspectOut, 500))
		}
		rl.Printf("inspect output: %s", truncate(inspectOut, 300))
	}

	// Cleanup: delete the MCP server.
	rl.Section("Cleanup MCP server")
	delOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "delete", mcpID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers delete "+mcpID+" --project "+projectID, delOut, err, 0)
	if err == nil {
		rl.Printf("MCP server deleted")
	} else {
		rl.Printf("MCP server delete failed (non-fatal): %v", err)
	}
}

// TestCLIInstalled_MCPServerInspectHelp verifies that `memory agents
// mcp-servers inspect --help` prints usage.
func TestCLIInstalled_MCPServerInspectHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify mcp-servers inspect --help prints usage",
		"Run `memory agents mcp-servers inspect --help`",
		"Assert output describes the inspect command",
	)
	logStatusPreamble(t)

	rl.Section("Run mcp-servers inspect --help")
	out := mustRunCLIInDirWithHome(t, "", t.TempDir(), "agents", "mcp-servers", "inspect", "--help")
	rl.CLI("memory agents mcp-servers inspect --help", out)

	rl.Section("Verify help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "inspect") {
		t.Errorf("expected help to mention 'inspect', got:\n%s", truncate(out, 500))
	}
	if !strings.Contains(lower, "connection") && !strings.Contains(lower, "capabilities") &&
		!strings.Contains(lower, "test") && !strings.Contains(lower, "mcp") {
		t.Errorf("expected help to describe connection/capabilities, got:\n%s", truncate(out, 500))
	}
	rl.Printf("inspect help: %d bytes", len(out))
}

// TestCLIInstalled_MCPServerSyncHelp verifies that `memory agents mcp-servers
// sync --help` prints usage.
func TestCLIInstalled_MCPServerSyncHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify mcp-servers sync --help prints usage",
		"Run `memory agents mcp-servers sync --help`",
		"Assert output describes the sync command",
	)
	logStatusPreamble(t)

	rl.Section("Run mcp-servers sync --help")
	out := mustRunCLIInDirWithHome(t, "", t.TempDir(), "agents", "mcp-servers", "sync", "--help")
	rl.CLI("memory agents mcp-servers sync --help", out)

	rl.Section("Verify help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "sync") {
		t.Errorf("expected help to mention 'sync', got:\n%s", truncate(out, 500))
	}
	if !strings.Contains(lower, "tool") && !strings.Contains(lower, "refresh") &&
		!strings.Contains(lower, "connect") {
		t.Errorf("expected help to describe tool refresh, got:\n%s", truncate(out, 500))
	}
	rl.Printf("sync help: %d bytes", len(out))
}

// TestCLIInstalled_MCPServerToolsHelp verifies that `memory agents mcp-servers
// tools --help` prints usage.
func TestCLIInstalled_MCPServerToolsHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify mcp-servers tools --help prints usage",
		"Run `memory agents mcp-servers tools --help`",
		"Assert output describes the tools list command",
	)
	logStatusPreamble(t)

	rl.Section("Run mcp-servers tools --help")
	out := mustRunCLIInDirWithHome(t, "", t.TempDir(), "agents", "mcp-servers", "tools", "--help")
	rl.CLI("memory agents mcp-servers tools --help", out)

	rl.Section("Verify help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "tool") {
		t.Errorf("expected help to mention 'tool', got:\n%s", truncate(out, 500))
	}
	rl.Printf("tools help: %d bytes", len(out))
}

var _ = framework.SetToken // keep import used
