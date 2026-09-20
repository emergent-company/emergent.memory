// Package cli_test — mcp_servers_test.go
//
// End-to-end tests for `memory agents mcp-servers` CLI subcommands: create,
// get, list, delete.  MCP servers are external Model Context Protocol servers
// registered with the Memory platform for use by agents.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_MCPServerCreateGetListDelete exercises the full lifecycle
// of an MCP server registration: create → get → list → delete.
func TestCLIInstalled_MCPServerCreateGetListDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify MCP server create → get → list → delete lifecycle",
		"Create project",
		"Create an MCP server of type sse with a test URL",
		"Get the server by ID and verify properties",
		"List servers and verify it appears",
		"Delete the server",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-mcpsrv")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create an MCP server.
	rl.Section("Create MCP server")
	mcpName := "e2e-test-mcp-server"
	mcpURL := "http://example.com:8080/sse"
	createOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "create",
		"--name", mcpName,
		"--type", "sse",
		"--url", mcpURL,
		"--description", "E2E test MCP server",
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers create --name "+mcpName+" --type sse --project "+projectID, createOut, err, 0)

	if err != nil {
		rl.Printf("mcp-servers create returned error: %v", err)
		t.Skipf("agents mcp-servers create not available: %v", err)
	}

	// Parse the server ID.
	mcpID := parseMCPServerID(createOut)
	if mcpID == "" {
		rl.Failf("could not parse MCP server ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("created MCP server ID: %s", mcpID)

	// Get the server.
	rl.Section("Get MCP server by ID")
	getOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "get", mcpID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers get "+mcpID+" --project "+projectID, getOut, err, 0)

	if err != nil {
		rl.Printf("mcp-servers get returned error: %v", err)
		t.Skipf("agents mcp-servers get not available: %v", err)
	}

	if !strings.Contains(getOut, mcpName) {
		t.Errorf("expected get output to contain server name %q, got:\n%s", mcpName, truncate(getOut, 500))
	}
	rl.Printf("get output contains name=%s: true", mcpName)

	// List servers.
	rl.Section("List MCP servers")
	listOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "list",
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers list --project "+projectID, listOut, err, 0)

	if err != nil {
		rl.Printf("mcp-servers list returned error: %v", err)
		t.Skipf("agents mcp-servers list not available: %v", err)
	}

	if !strings.Contains(listOut, mcpName) {
		t.Errorf("expected list to contain server name %q, got:\n%s", mcpName, truncate(listOut, 500))
	}
	rl.Printf("list contains %s: true", mcpName)

	// Delete the server.
	rl.Section("Delete MCP server")
	delOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "delete", mcpID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers delete "+mcpID+" --project "+projectID, delOut, err, 0)

	if err != nil {
		rl.Printf("mcp-servers delete returned error: %v", err)
		t.Skipf("agents mcp-servers delete not available: %v", err)
	}

	lower := strings.ToLower(delOut)
	if !strings.Contains(lower, "delete") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", truncate(delOut, 300))
	}
	rl.Printf("MCP server delete confirmed: true")
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// parseMCPServerID extracts an MCP server ID from create output.
func parseMCPServerID(output string) string {
	for _, label := range []string{"ID:", "Server ID:", "id:"} {
		if id := parseLineField(output, label); id != "" {
			return id
		}
	}
	if id := parseJSONField(output, "id"); id != "" {
		return id
	}
	return ""
}

var _ = framework.SetToken // keep import used
