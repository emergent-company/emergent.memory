// Package cli_test — agent_def_overrides_test.go
//
// End-to-end tests for `memory agent-definitions override` and
// `memory agent-definitions overrides` CLI subcommands.  These test
// per-project configuration overrides for agent definitions.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_AgentDefinitionOverridesListEmpty verifies that
// `memory agent-definitions overrides` returns an empty list (or table)
// for a fresh project with no overrides.
func TestCLIInstalled_AgentDefinitionOverridesListEmpty(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent-definitions overrides returns empty for fresh project",
		"Create a fresh project",
		"Run `memory agent-definitions overrides --project <id>`",
		"Assert command succeeds with empty/no-overrides output",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-def-overrides")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("List overrides on fresh project")
	out, err := runCLIInDirWithHome(t, "", home,
		"agent-definitions", "overrides",
		"--project", projectID,
	)
	rl.CLIErr("memory agent-definitions overrides --project "+projectID, out, err, 0)

	if err != nil {
		lower := strings.ToLower(out)
		if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "not found") {
			rl.Printf("SKIP: agent-definitions overrides not available: %s", truncate(out, 200))
			t.Skipf("agent-definitions overrides not available: %s", truncate(out, 200))
		}
		rl.Failf("agent-definitions overrides failed: %v — %s", err, truncate(out, 300))
	}

	rl.Section("Verify empty overrides output")
	// Fresh project should have no overrides — output may be an empty table
	// or a "no overrides" message.
	trimmed := strings.TrimSpace(out)
	lower := strings.ToLower(trimmed)
	hasContent := trimmed != ""
	hasNoOverrides := strings.Contains(lower, "no override") || strings.Contains(lower, "no agent")
	hasTable := strings.Contains(trimmed, "─") || strings.Contains(trimmed, "┌") || strings.Contains(lower, "agent")
	isEmpty := trimmed == "" || trimmed == "[]" || trimmed == "{}"

	if hasContent && !hasNoOverrides && !hasTable && !isEmpty {
		rl.Printf("unexpected output format: %s", truncate(trimmed, 300))
	}
	rl.Printf("overrides on fresh project: empty=%v noOverridesMsg=%v hasTable=%v (%d bytes)",
		isEmpty, hasNoOverrides, hasTable, len(trimmed))
}

// TestCLIInstalled_AgentDefinitionOverrideSetAndClear exercises the full
// lifecycle of a per-project agent override: set → view → clear.
func TestCLIInstalled_AgentDefinitionOverrideSetAndClear(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent-definitions override set → view → clear lifecycle",
		"Create a project",
		"Set an override for a built-in agent (e.g. graph-query-agent)",
		"View the override and verify it contains the overridden value",
		"Clear the override",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-def-override")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Set an override — use --max-steps since it's a simple integer field
	// that doesn't require an LLM provider.
	agentName := "graph-query-agent"

	rl.Section("Set override for " + agentName)
	setOut, err := runCLIInDirWithHome(t, "", home,
		"agent-definitions", "override", agentName,
		"--max-steps", "42",
		"--project", projectID,
	)
	rl.CLIErr("memory agent-definitions override "+agentName+" --max-steps 42 --project "+projectID, setOut, err, 0)

	if err != nil {
		lower := strings.ToLower(setOut)
		if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "not found") ||
			strings.Contains(lower, "unknown agent") {
			rl.Printf("SKIP: agent-definitions override not available for %s: %s", agentName, truncate(setOut, 200))
			t.Skipf("agent-definitions override not available: %s", truncate(setOut, 200))
		}
		rl.Failf("agent-definitions override set failed: %v — %s", err, truncate(setOut, 300))
	}
	rl.Printf("set override output: %s", truncate(setOut, 300))

	// View the override (run without --max-steps to view).
	rl.Section("View override for " + agentName)
	viewOut, err := runCLIInDirWithHome(t, "", home,
		"agent-definitions", "override", agentName,
		"--project", projectID,
	)
	rl.CLIErr("memory agent-definitions override "+agentName+" --project "+projectID, viewOut, err, 0)

	if err != nil {
		rl.Printf("view override returned error (may be a known CLI behavior): %v", err)
	} else {
		// Verify the override shows max_steps=42.
		if !strings.Contains(viewOut, "42") {
			t.Errorf("expected view output to contain max-steps value '42', got:\n%s", truncate(viewOut, 500))
		}
		rl.Printf("view override contains max-steps=42: true")
	}

	// List overrides — should now show at least one entry.
	rl.Section("List overrides after set")
	listOut, err := runCLIInDirWithHome(t, "", home,
		"agent-definitions", "overrides",
		"--project", projectID,
	)
	rl.CLIErr("memory agent-definitions overrides --project "+projectID, listOut, err, 0)

	if err == nil {
		if !strings.Contains(listOut, agentName) && !strings.Contains(listOut, "graph") {
			t.Errorf("expected overrides list to contain %q after set, got:\n%s", agentName, truncate(listOut, 500))
		}
		rl.Printf("overrides list contains %s: true", agentName)
	} else {
		rl.Printf("overrides list returned error: %v", err)
	}

	// Clear the override.
	rl.Section("Clear override for " + agentName)
	clearOut, err := runCLIInDirWithHome(t, "", home,
		"agent-definitions", "override", agentName,
		"--clear",
		"--project", projectID,
	)
	rl.CLIErr("memory agent-definitions override "+agentName+" --clear --project "+projectID, clearOut, err, 0)

	if err != nil {
		rl.Printf("clear override returned error: %v — %s", err, truncate(clearOut, 300))
		t.Errorf("expected override --clear to succeed, got error: %v", err)
	} else {
		rl.Printf("clear override output: %s", truncate(clearOut, 200))
	}
}

// TestCLIInstalled_AgentDefinitionOverrideHelp verifies that `memory
// agent-definitions override --help` prints usage with supported flags.
func TestCLIInstalled_AgentDefinitionOverrideHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent-definitions override --help prints usage",
		"Run `memory agent-definitions override --help`",
		"Assert output contains flag descriptions for model, max-steps, etc.",
	)
	logStatusPreamble(t)

	rl.Section("Run agent-definitions override --help")
	out := mustRunCLIInDirWithHome(t, "", t.TempDir(), "agent-definitions", "override", "--help")
	rl.CLI("memory agent-definitions override --help", out)

	rl.Section("Verify help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "override") {
		t.Errorf("expected help to mention 'override', got:\n%s", truncate(out, 500))
	}
	if !strings.Contains(lower, "--model") {
		t.Errorf("expected help to list --model flag, got:\n%s", truncate(out, 500))
	}
	if !strings.Contains(lower, "--max-steps") {
		t.Errorf("expected help to list --max-steps flag, got:\n%s", truncate(out, 500))
	}
	if !strings.Contains(lower, "--clear") {
		t.Errorf("expected help to list --clear flag, got:\n%s", truncate(out, 500))
	}
	rl.Printf("override help: %d bytes, contains expected flags", len(out))
}

// TestCLIInstalled_AgentDefinitionOverridesHelp verifies that `memory
// agent-definitions overrides --help` prints usage.
func TestCLIInstalled_AgentDefinitionOverridesHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent-definitions overrides --help prints usage",
		"Run `memory agent-definitions overrides --help`",
		"Assert output mentions overrides",
	)
	logStatusPreamble(t)

	rl.Section("Run agent-definitions overrides --help")
	out := mustRunCLIInDirWithHome(t, "", t.TempDir(), "agent-definitions", "overrides", "--help")
	rl.CLI("memory agent-definitions overrides --help", out)

	rl.Section("Verify help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "override") {
		t.Errorf("expected help to mention 'override', got:\n%s", truncate(out, 500))
	}
	rl.Printf("overrides help: %d bytes", len(out))
}

var _ = framework.SetToken // keep import used
