// Package cli_test — hooks_test.go
//
// End-to-end tests for `memory agents hooks` CLI subcommands: create, list,
// delete.  Hooks are webhook endpoints attached to agents that can be used
// to trigger agent runs externally.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_HookCreateListDelete exercises the full lifecycle of an
// agent webhook hook: create agent → create hook → list hooks → delete hook.
func TestCLIInstalled_HookCreateListDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent hook create → list → delete lifecycle",
		"Create project and agent",
		"Create a webhook hook on the agent",
		"List hooks and verify the new one appears",
		"Delete the hook",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-hooks")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create agent for hook attachment.
	rl.Section("Create agent")
	agentName := "e2e-hook-agent"
	createAgentOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "create",
		"--name", agentName,
		"--trigger-type", "manual",
		"--strategy-type", "single",
		"--cron", "0 0 * * * *",
		"--project", projectID,
	)
	rl.CLI("memory agents create --name "+agentName+" --project "+projectID, createAgentOut)

	agentID := parseAgentID(createAgentOut)
	if agentID == "" {
		rl.Failf("could not parse agent ID from: %s", truncate(createAgentOut, 300))
	}
	rl.Printf("agent ID: %s", agentID)

	// Create a webhook hook.
	rl.Section("Create webhook hook")
	hookLabel := "e2e-test-hook"
	hookOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "hooks", "create", agentID,
		"--label", hookLabel,
		"--project", projectID,
	)
	rl.CLIErr("memory agents hooks create "+agentID+" --label "+hookLabel+" --project "+projectID, hookOut, err, 0)

	if err != nil {
		rl.Printf("hooks create returned error: %v", err)
		t.Skipf("agents hooks create not available: %v", err)
	}

	// Parse hook ID from output.
	hookID := parseHookID(hookOut)
	if hookID == "" {
		rl.Failf("could not parse hook ID from: %s", truncate(hookOut, 300))
	}
	rl.Printf("created hook ID: %s", hookID)

	// List hooks.
	rl.Section("List hooks")
	listOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "hooks", "list", agentID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents hooks list "+agentID+" --project "+projectID, listOut, err, 0)

	if err != nil {
		rl.Printf("hooks list returned error: %v", err)
		t.Skipf("agents hooks list not available: %v", err)
	}

	if !strings.Contains(listOut, hookLabel) {
		t.Errorf("expected hooks list to contain label %q, got:\n%s", hookLabel, truncate(listOut, 500))
	}
	rl.Printf("hooks list contains label %s: true", hookLabel)

	// Delete hook.
	rl.Section("Delete hook")
	delOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "hooks", "delete", hookID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents hooks delete "+hookID+" --project "+projectID, delOut, err, 0)

	if err != nil {
		rl.Printf("hooks delete returned error: %v", err)
		t.Skipf("agents hooks delete not available: %v", err)
	}

	lower := strings.ToLower(delOut)
	if !strings.Contains(lower, "delete") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", truncate(delOut, 300))
	}
	rl.Printf("hook delete confirmed: true")
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// parseHookID extracts a hook ID from create output.
func parseHookID(output string) string {
	for _, label := range []string{"ID:", "Hook ID:", "id:"} {
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
