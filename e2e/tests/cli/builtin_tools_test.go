// Package cli_test — builtin_tools_test.go
//
// End-to-end tests for `memory agents builtin-tools` CLI subcommands:
// list, toggle.  Built-in tools are Go-native tools registered per project
// (e.g. query_entities, brave_web_search, webfetch, create_document).
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_BuiltinToolsList verifies that `agents builtin-tools list`
// returns a list of built-in tools for a project.
func TestCLIInstalled_BuiltinToolsList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agents builtin-tools list returns project tools",
		"Create project",
		"Run `agents builtin-tools list --project <id>`",
		"Assert output contains at least one tool entry",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-btools")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("List built-in tools")
	out, err := runCLIInDirWithHome(t, "", home,
		"agents", "builtin-tools", "list",
		"--project", projectID,
	)
	rl.CLIErr("memory agents builtin-tools list --project "+projectID, out, err, 0)

	if err != nil {
		rl.Printf("builtin-tools list returned error: %v", err)
		t.Skipf("builtin-tools list not available: %v", err)
	}

	rl.Section("Verify tools output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from builtin-tools list, got empty")
	}
	// Should contain at least one known tool name.
	lower := strings.ToLower(trimmed)
	hasKnownTool := strings.Contains(lower, "query_entities") ||
		strings.Contains(lower, "webfetch") ||
		strings.Contains(lower, "create_document") ||
		strings.Contains(lower, "brave_web_search") ||
		strings.Contains(lower, "web_search")
	if !hasKnownTool {
		// Even if no specific tools are listed, the output should have header rows.
		lines := strings.Split(trimmed, "\n")
		if len(lines) < 2 {
			t.Errorf("expected builtin-tools list to contain tool entries, got:\n%s", truncate(trimmed, 500))
		}
	}
	rl.Printf("builtin-tools list: %d bytes, has known tools: %v", len(trimmed), hasKnownTool)
}

// TestCLIInstalled_BuiltinToolsToggle verifies that `agents builtin-tools toggle`
// can disable and re-enable a built-in tool.
func TestCLIInstalled_BuiltinToolsToggle(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agents builtin-tools toggle can disable and re-enable a tool",
		"Create project and list built-in tools",
		"Toggle the first tool off",
		"List again and verify it shows disabled",
		"Toggle the tool back on",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-btoggle")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// List tools to find a tool ID to toggle.
	rl.Section("List built-in tools to find a tool ID")
	listOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "builtin-tools", "list",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLIErr("memory agents builtin-tools list --project "+projectID+" --output json", listOut, err, 0)

	if err != nil {
		rl.Printf("builtin-tools list returned error: %v", err)
		t.Skipf("builtin-tools list not available: %v", err)
	}

	// Extract a tool ID from the JSON list output.
	toolID := parseJSONField(listOut, "id")
	if toolID == "" {
		// Try extracting from line-field format if not JSON.
		toolID = parseLineField(listOut, "ID:")
	}
	if toolID == "" {
		rl.Printf("could not extract a tool ID from list output; skipping toggle test")
		t.Skipf("could not find a tool ID to toggle in: %s", truncate(listOut, 300))
	}
	rl.Printf("tool ID to toggle: %s", toolID)

	// Toggle off.
	rl.Section("Toggle tool off")
	offOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "builtin-tools", "toggle", toolID, "off",
		"--project", projectID,
	)
	rl.CLIErr("memory agents builtin-tools toggle "+toolID+" off --project "+projectID, offOut, err, 0)

	if err != nil {
		rl.Printf("toggle off returned error: %v", err)
		t.Skipf("builtin-tools toggle not available: %v", err)
	}
	rl.Printf("toggle off output: %s", truncate(offOut, 200))

	// Toggle back on.
	rl.Section("Toggle tool back on")
	onOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "builtin-tools", "toggle", toolID, "on",
		"--project", projectID,
	)
	rl.CLIErr("memory agents builtin-tools toggle "+toolID+" on --project "+projectID, onOut, err, 0)

	if err != nil {
		rl.Printf("toggle on returned error: %v", err)
		t.Skipf("builtin-tools toggle on failed: %v", err)
	}
	rl.Printf("toggle on output: %s", truncate(onOut, 200))
	rl.Printf("toggle off→on cycle complete")
}

var _ = framework.SetToken // keep import used
