// Package blueprints_test — blueprint_test.go
//
// Tests for applying Memory blueprints from a local directory.
// These tests create a dedicated project, apply the blueprint, assert the
// expected resources were created, and delete the project on cleanup.
package blueprints_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test: multi-agent blueprint install from GitHub
// ─────────────────────────────────────────────────────────────────────────────

// TestBlueprintMultiAgent_Install verifies that the multi-agent blueprint can
// be applied to a fresh project directly from its local directory.
//
// Steps:
//  1. Authenticate against the test server.
//  2. Create a dedicated ephemeral project.
//  3. Run `memory blueprints <local-path> --project <name>`.
//  4. Assert the expected resources appear in the output.
//  5. Delete the project on cleanup.
func TestBlueprintMultiAgent_Install(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Verify multi-agent blueprint installs cleanly into a fresh project",
		"Creates an ephemeral project",
		"Runs `memory blueprints` from the local directory",
		"Asserts expected agents, pack and seed objects appear in output",
	)

	logStatusPreamble(t)
	skipIfServerDown(t, rl)

	home := t.TempDir()
	srv := serverURL()

	rl.Section("Step 1 — Authenticate")
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", e2eTestToken())

	rl.Section("Step 2 — Create project")
	projectName := fmt.Sprintf("e2e-multi-agent-%d", time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home, append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
	rl.CLI("memory projects create --name "+projectName, createOut)

	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from create output: %q", createOut)
	}
	rl.Printf("created project %s (%s)", projectName, projectID)

	// Always delete the project when the test exits, even on failure.
	t.Cleanup(func() { deleteProjectViaExec(t, home, projectID, projectName) })

	rl.Section("Step 3 — Apply blueprint")
	out := mustRunCLIInDirWithHome(t, "", home, "blueprints", blueprintURL, "--project", projectName)
	rl.CLI("memory blueprints "+blueprintURL+" --project "+projectName, out)

	rl.Section("Step 4 — Assert expected resources")
	expectedItems := []string{
		"multi-agent-task-pack",
		"orchestrator",
		"janitor",
		"coder",
		"web-researcher",
		"coding-manager",
	}
	for _, item := range expectedItems {
		if !strings.Contains(out, item) {
			t.Errorf("blueprint output missing expected item %q", item)
			rl.Printf("MISSING: %q", item)
		} else {
			rl.Printf("OK: %q present", item)
		}
	}

	if strings.Contains(out, "errors") && !strings.Contains(out, "0 errors") {
		t.Errorf("blueprint install reported errors:\n%s", out)
	}
}
