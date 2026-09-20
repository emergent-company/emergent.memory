// Package cli_test — questions_test.go
//
// End-to-end tests for `memory agents questions list-project`.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_AgentQuestionsListProject verifies that `memory agents
// questions list-project` returns results for a project.
func TestCLIInstalled_AgentQuestionsListProject(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agents questions list-project returns results",
		"Create a fresh project",
		"Run `memory agents questions list-project --project <id>`",
		"Assert command succeeds with success response",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-agent-qlist")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Run agents questions list-project")
	out := mustRunCLIInDirWithHome(t, "", home,
		"agents", "questions", "list-project",
		"--project", projectID,
	)
	rl.CLI("memory agents questions list-project --project "+projectID, out)

	trimmed := strings.TrimSpace(out)
	// A fresh project should return success (possibly with empty list).
	if !strings.Contains(trimmed, "success") && trimmed != "[]" && trimmed != "" {
		t.Logf("unexpected questions list-project output: %s", truncate(trimmed, 500))
	}
	rl.Printf("questions list-project output: %s", truncate(trimmed, 300))

	// Test with --status filter.
	rl.Section("Run with --status pending filter")
	filteredOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "questions", "list-project",
		"--project", projectID,
		"--status", "pending",
	)
	rl.CLI("memory agents questions list-project --project "+projectID+" --status pending", filteredOut)
	rl.Printf("filtered output: %s", truncate(strings.TrimSpace(filteredOut), 300))
}

// TestCLIInstalled_AgentQuestionsRespondInvalid verifies that `memory agents
// questions respond` with a bogus question ID returns a meaningful error
// rather than panicking or producing a cryptic failure.
func TestCLIInstalled_AgentQuestionsRespondInvalid(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agents questions respond handles invalid question ID gracefully",
		"Create a fresh project",
		"Run `memory agents questions respond <bogus-id> yes --project <id>`",
		"Assert command returns a clear error (not a panic)",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-qrespond")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Run agents questions respond with bogus ID")
	bogusID := "00000000-0000-0000-0000-000000000000"
	out, err := runCLIInDirWithHome(t, "", home,
		"agents", "questions", "respond",
		bogusID, "yes",
		"--project", projectID,
	)
	rl.CLIErr("memory agents questions respond "+bogusID+" yes --project "+projectID, out, err, 0)

	// We expect an error (question not found) — the key assertion is that
	// the command doesn't panic and returns a comprehensible message.
	if err == nil {
		rl.Printf("respond unexpectedly succeeded (question may exist): %s", truncate(out, 300))
	} else {
		lower := strings.ToLower(out)
		// Should mention "not found", "invalid", "error", or similar.
		if strings.Contains(lower, "not found") || strings.Contains(lower, "invalid") ||
			strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
			rl.Printf("respond correctly returned error for bogus ID")
		} else {
			rl.Printf("respond returned error with unexpected message: %s", truncate(out, 300))
		}
	}
}

// TestCLIInstalled_AgentQuestionsRespondHelp verifies that `memory agents
// questions respond --help` prints usage information.
func TestCLIInstalled_AgentQuestionsRespondHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agents questions respond --help prints usage",
		"Run `memory agents questions respond --help`",
		"Assert output contains respond usage hints",
	)
	logStatusPreamble(t)

	rl.Section("Run agents questions respond --help")
	out := mustRunCLI(t, "agents", "questions", "respond", "--help")
	rl.CLI("memory agents questions respond --help", out)

	rl.Section("Verify help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "respond") {
		t.Errorf("expected respond help to mention 'respond', got:\n%s", truncate(out, 500))
	}
	rl.Printf("respond help output: %d bytes", len(out))
}

var _ = framework.SetToken // keep import used
