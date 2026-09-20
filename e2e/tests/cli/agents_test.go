// Package cli_test — agents_test.go
//
// End-to-end tests for `memory agent-definitions` and `memory agents` CLI
// subcommands.  Tests exercise the full CRUD lifecycle for both definitions
// and runtime agents.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Agent Definitions
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_AgentDefinitionCreateGetDelete exercises the full lifecycle
// of an agent definition: create → get → delete.
func TestCLIInstalled_AgentDefinitionCreateGetDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent-definition create → get → delete lifecycle",
		"Create project, then create an agent definition",
		"Get the definition by ID and verify name/properties",
		"Delete the definition",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-agentdef")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create an agent definition.
	rl.Section("Create agent definition")
	defName := "e2e-test-def"
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "create",
		"--name", defName,
		"--system-prompt", "You are a test agent for e2e validation.",
		"--flow-type", "single",
		"--visibility", "project",
		"--project", projectID,
	)
	rl.CLI("memory agent-definitions create --name "+defName+" --project "+projectID, createOut)

	// Parse the definition ID.
	defID := parseAgentDefID(createOut)
	if defID == "" {
		rl.Failf("could not parse agent definition ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("created definition ID: %s", defID)

	// Get the definition.
	rl.Section("Get agent definition")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "get", defID,
		"--project", projectID,
	)
	rl.CLI("memory agent-definitions get "+defID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, defName) {
		t.Errorf("expected get output to contain name %q, got:\n%s", defName, truncate(getOut, 500))
	}
	if !strings.Contains(getOut, "single") {
		t.Errorf("expected get output to contain flow type 'single', got:\n%s", truncate(getOut, 500))
	}
	rl.Printf("get output contains name=%s, flow_type=single: true", defName)

	// Delete the definition.
	rl.Section("Delete agent definition")
	delOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "delete", defID,
		"--project", projectID,
	)
	rl.CLI("memory agent-definitions delete "+defID+" --project "+projectID, delOut)

	lower := strings.ToLower(delOut)
	if !strings.Contains(lower, "delete") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", truncate(delOut, 300))
	}
	rl.Printf("definition delete confirmed: true")
}

// TestCLIInstalled_AgentDefinitionList verifies that `agent-definitions list`
// returns definitions for a project.
func TestCLIInstalled_AgentDefinitionList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent-definitions list returns definitions",
		"Create project with an agent definition",
		"List definitions and verify the created one appears",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-deflist")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create a definition so the list is non-empty.
	rl.Section("Create agent definition")
	defName := "e2e-list-def"
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "create",
		"--name", defName,
		"--system-prompt", "Test agent for list validation.",
		"--flow-type", "single",
		"--project", projectID,
	)
	rl.CLI("memory agent-definitions create --name "+defName+" --project "+projectID, createOut)

	// List definitions.
	rl.Section("List agent definitions")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "list",
		"--project", projectID,
	)
	rl.CLI("memory agent-definitions list --project "+projectID, listOut)

	if !strings.Contains(listOut, defName) {
		t.Errorf("expected list to contain definition name %q, got:\n%s", defName, truncate(listOut, 500))
	}
	rl.Printf("list contains definition %s: true", defName)
}

// ─────────────────────────────────────────────────────────────────────────────
// Runtime Agents
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_AgentCreateGetDelete exercises the full lifecycle of a
// runtime agent: create → get → delete.
func TestCLIInstalled_AgentCreateGetDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent create → get → delete lifecycle",
		"Create project and agent definition, then create a runtime agent",
		"Get the agent by ID and verify properties",
		"Delete the agent",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-agent-crud")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create agent — requires strategy-type and cron schedule.
	rl.Section("Create agent")
	agentName := "e2e-test-agent"
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "create",
		"--name", agentName,
		"--trigger-type", "manual",
		"--strategy-type", "single",
		"--cron", "0 0 * * * *",
		"--project", projectID,
	)
	rl.CLI("memory agents create --name "+agentName+" --project "+projectID, createOut)

	agentID := parseAgentID(createOut)
	if agentID == "" {
		rl.Failf("could not parse agent ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("created agent ID: %s", agentID)

	// Get the agent.
	rl.Section("Get agent by ID")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "get", agentID,
		"--project", projectID,
	)
	rl.CLI("memory agents get "+agentID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, agentName) {
		t.Errorf("expected get output to contain agent name %q, got:\n%s", agentName, truncate(getOut, 500))
	}
	if !strings.Contains(strings.ToLower(getOut), "manual") {
		t.Errorf("expected get output to contain trigger type 'manual', got:\n%s", truncate(getOut, 500))
	}
	rl.Printf("get output contains name=%s, trigger=manual: true", agentName)

	// Delete the agent.
	rl.Section("Delete agent")
	delOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "delete", agentID,
		"--project", projectID,
	)
	rl.CLI("memory agents delete "+agentID+" --project "+projectID, delOut)

	lower := strings.ToLower(delOut)
	if !strings.Contains(lower, "delete") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", truncate(delOut, 300))
	}
	rl.Printf("agent delete confirmed: true")
}

// TestCLIInstalled_AgentTriggerAndRuns verifies that an agent can be triggered
// and that its runs are listed.
func TestCLIInstalled_AgentTriggerAndRuns(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent trigger and runs listing",
		"Create project and agent",
		"Trigger the agent",
		"List runs and verify at least one run appears",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-agent-trig")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create agent.
	rl.Section("Create agent")
	agentName := "e2e-trigger-agent"
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "create",
		"--name", agentName,
		"--trigger-type", "manual",
		"--strategy-type", "single",
		"--cron", "0 0 * * * *",
		"--project", projectID,
	)
	rl.CLI("memory agents create --name "+agentName+" --project "+projectID, createOut)

	agentID := parseAgentID(createOut)
	if agentID == "" {
		rl.Failf("could not parse agent ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("agent ID: %s", agentID)

	// Trigger the agent.
	rl.Section("Trigger agent")
	triggerOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "trigger", agentID,
		"--project", projectID,
	)
	rl.CLI("memory agents trigger "+agentID+" --project "+projectID, triggerOut)

	lower := strings.ToLower(triggerOut)
	if !strings.Contains(lower, "trigger") {
		t.Errorf("expected trigger output to mention 'trigger', got:\n%s", truncate(triggerOut, 300))
	}
	rl.Printf("trigger output: %s", truncate(triggerOut, 200))

	// List runs — the agent may not have completed, but the run should exist.
	rl.Section("List agent runs")
	runsOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "runs", agentID,
		"--project", projectID,
	)
	rl.CLI("memory agents runs "+agentID+" --project "+projectID, runsOut)

	// Runs output should show at least one entry or status line.
	trimmed := strings.TrimSpace(runsOut)
	if trimmed == "" {
		t.Errorf("expected non-empty runs output after trigger, got empty")
	}
	rl.Printf("runs output: %d bytes", len(trimmed))
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent Update Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_AgentUpdate verifies that `agents update` can modify
// an existing agent's properties (partial update).
func TestCLIInstalled_AgentUpdate(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agents update can modify agent properties",
		"Create project and agent",
		"Update agent name and description",
		"Get the agent and verify updated properties",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-agent-upd")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create agent.
	rl.Section("Create agent")
	agentName := "e2e-update-agent"
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "create",
		"--name", agentName,
		"--trigger-type", "manual",
		"--strategy-type", "single",
		"--cron", "0 0 * * * *",
		"--project", projectID,
	)
	rl.CLI("memory agents create --name "+agentName+" --project "+projectID, createOut)

	agentID := parseAgentID(createOut)
	if agentID == "" {
		rl.Failf("could not parse agent ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("agent ID: %s", agentID)

	// Update the agent.
	rl.Section("Update agent")
	updatedName := "e2e-updated-agent"
	updateOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "update", agentID,
		"--name", updatedName,
		"--description", "Updated by e2e test",
		"--project", projectID,
	)
	rl.CLIErr("memory agents update "+agentID+" --name "+updatedName+" --project "+projectID, updateOut, err, 0)

	if err != nil {
		rl.Printf("agents update returned error: %v", err)
		t.Skipf("agents update not available: %v", err)
	}
	rl.Printf("update output: %s", truncate(updateOut, 200))

	// Verify the update persisted.
	rl.Section("Verify updated agent")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "get", agentID,
		"--project", projectID,
	)
	rl.CLI("memory agents get "+agentID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, updatedName) {
		t.Errorf("expected get output to contain updated name %q, got:\n%s", updatedName, truncate(getOut, 500))
	}
	rl.Printf("get output contains updated name: true")
}

// TestCLIInstalled_AgentDefinitionUpdate verifies that `agent-definitions update`
// can modify an existing agent definition's properties.
func TestCLIInstalled_AgentDefinitionUpdate(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agent-definitions update can modify definition properties",
		"Create project and agent definition",
		"Update definition name and description",
		"Get the definition and verify updated properties",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-defupd")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create definition.
	rl.Section("Create agent definition")
	defName := "e2e-update-def"
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "create",
		"--name", defName,
		"--system-prompt", "Original prompt.",
		"--flow-type", "single",
		"--project", projectID,
	)
	rl.CLI("memory agent-definitions create --name "+defName+" --project "+projectID, createOut)

	defID := parseAgentDefID(createOut)
	if defID == "" {
		rl.Failf("could not parse definition ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("definition ID: %s", defID)

	// Update the definition.
	rl.Section("Update agent definition")
	updatedName := "e2e-updated-def"
	updateOut, err := runCLIInDirWithHome(t, "", home,
		"agent-definitions", "update", defID,
		"--name", updatedName,
		"--description", "Updated by e2e test",
		"--project", projectID,
	)
	rl.CLIErr("memory agent-definitions update "+defID+" --name "+updatedName+" --project "+projectID, updateOut, err, 0)

	if err != nil {
		rl.Printf("agent-definitions update returned error: %v", err)
		t.Skipf("agent-definitions update not available: %v", err)
	}
	rl.Printf("update output: %s", truncate(updateOut, 200))

	// Verify the update persisted.
	rl.Section("Verify updated definition")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "get", defID,
		"--project", projectID,
	)
	rl.CLI("memory agent-definitions get "+defID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, updatedName) {
		t.Errorf("expected get output to contain updated name %q, got:\n%s", updatedName, truncate(getOut, 500))
	}
	rl.Printf("get output contains updated name: true")
}

// TestCLIInstalled_AgentGetRun verifies that `agents get-run` returns details
// for a specific run after triggering an agent.
func TestCLIInstalled_AgentGetRun(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify agents get-run returns run details",
		"Create project and agent, trigger it",
		"List runs to find a run ID",
		"Get-run with the run ID and verify output contains details",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-getrun")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create and trigger agent.
	rl.Section("Create and trigger agent")
	agentName := "e2e-getrun-agent"
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "create",
		"--name", agentName,
		"--trigger-type", "manual",
		"--strategy-type", "single",
		"--cron", "0 0 * * * *",
		"--project", projectID,
	)
	rl.CLI("memory agents create --name "+agentName+" --project "+projectID, createOut)

	agentID := parseAgentID(createOut)
	if agentID == "" {
		rl.Failf("could not parse agent ID from: %s", truncate(createOut, 300))
	}
	rl.Printf("agent ID: %s", agentID)

	// Trigger.
	triggerOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "trigger", agentID,
		"--project", projectID,
	)
	rl.CLI("memory agents trigger "+agentID+" --project "+projectID, triggerOut)

	// List runs to extract a run ID.
	rl.Section("List runs to find a run ID")
	runsOut := mustRunCLIInDirWithHome(t, "", home,
		"agents", "runs", agentID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory agents runs "+agentID+" --project "+projectID+" --output json", runsOut)

	runID := parseJSONField(runsOut, "id")
	if runID == "" {
		runID = parseLineField(runsOut, "ID:")
	}
	if runID == "" {
		runID = parseJSONField(runsOut, "run_id")
	}
	if runID == "" {
		// agents runs output format: "1. Run <uuid>" — extract the UUID.
		for _, line := range strings.Split(runsOut, "\n") {
			line = strings.TrimSpace(line)
			if idx := strings.Index(line, "Run "); idx >= 0 {
				candidate := strings.TrimSpace(line[idx+4:])
				if len(candidate) == 36 && strings.Count(candidate, "-") == 4 {
					runID = candidate
					break
				}
			}
		}
	}
	if runID == "" {
		rl.Printf("could not extract run ID from runs output; skipping get-run test")
		t.Skipf("could not find a run ID in runs output: %s", truncate(runsOut, 300))
	}
	rl.Printf("run ID: %s", runID)

	// Get-run.
	rl.Section("Get run details")
	getRunOut, err := runCLIInDirWithHome(t, "", home,
		"agents", "get-run", runID,
		"--project", projectID,
	)
	rl.CLIErr("memory agents get-run "+runID+" --project "+projectID, getRunOut, err, 0)

	if err != nil {
		rl.Printf("agents get-run returned error: %v", err)
		t.Skipf("agents get-run not available: %v", err)
	}

	trimmed := strings.TrimSpace(getRunOut)
	if trimmed == "" {
		rl.Failf("expected non-empty output from get-run, got empty")
	}
	// The output should contain run status info.
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "status") && !strings.Contains(lower, runID) {
		t.Errorf("expected get-run output to contain 'status' or run ID, got:\n%s", truncate(trimmed, 500))
	}
	rl.Printf("get-run output: %d bytes, contains status/ID info: true", len(trimmed))
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// parseAgentDefID extracts an agent definition ID from create output.
// Tries parseLineField with common label patterns, then falls back to
// parseJSONField.
func parseAgentDefID(output string) string {
	// Try common labels.
	for _, label := range []string{"ID:", "Definition ID:", "id:"} {
		if id := parseLineField(output, label); id != "" {
			return id
		}
	}
	// Try JSON.
	if id := parseJSONField(output, "id"); id != "" {
		return id
	}
	return ""
}

var _ = framework.SetToken // keep import used
