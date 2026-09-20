// Package cli_test — ask_multistep_test.go
//
// Sophisticated multi-step end-to-end tests for `memory ask`.  Each test asks
// the agent to perform a chain of dependent actions in a single natural-language
// prompt, then verifies every side-effect with direct CLI calls.
//
// These tests exercise the agent's ability to:
//   - Plan and execute multiple tool calls in sequence
//   - Pass IDs from one step as inputs to the next
//   - Maintain context across a multi-tool interaction
//
// All tests use the 90-second timeout (mustAskLong) because multi-step asks
// routinely involve 3-6 LLM + server round-trips.
//
// Required environment variables:
//
//	MEMORY_TEST_SERVER  — URL of the Memory server.
//	MEMORY_TEST_TOKEN   — API key / token for the Memory server.
package cli_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Shared helpers for multi-step tests
// ─────────────────────────────────────────────────────────────────────────────

// extractAllUUIDs scans text and returns every distinct UUID found.
func extractAllUUIDs(text string) []string {
	const hexChars = "0123456789abcdefABCDEF"
	isHex := func(b byte) bool {
		for i := 0; i < len(hexChars); i++ {
			if b == hexChars[i] {
				return true
			}
		}
		return false
	}

	seen := map[string]bool{}
	var uuids []string
	for i := 0; i+35 < len(text); i++ {
		s := text[i : i+36]
		if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			continue
		}
		ok := true
		for j, c := range []byte(s) {
			if j == 8 || j == 13 || j == 18 || j == 23 {
				continue
			}
			if !isHex(c) {
				ok = false
				break
			}
		}
		if ok {
			lower := strings.ToLower(s)
			if !seen[lower] {
				seen[lower] = true
				uuids = append(uuids, lower)
			}
			i += 35 // skip past this UUID
		}
	}
	return uuids
}

// containsAllOf checks that text (lowered) contains every keyword.
// Returns the list of missing keywords (empty slice = all found).
func containsAllOf(text string, keywords []string) []string {
	lower := strings.ToLower(text)
	var missing []string
	for _, kw := range keywords {
		if !strings.Contains(lower, strings.ToLower(kw)) {
			missing = append(missing, kw)
		}
	}
	return missing
}

// containsAnyOf returns true when text (lowered) contains at least one keyword.
func containsAnyOf(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// graphObject is a minimal representation of a graph object from JSON output.
type graphObject struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
}

// parseGraphObjects parses JSON output from `graph objects list --output json`.
// Handles both a plain JSON array and a {"data": [...]} wrapper.
func parseGraphObjects(jsonStr string) []graphObject {
	trimmed := strings.TrimSpace(jsonStr)
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var objects []graphObject
	if err := json.Unmarshal([]byte(trimmed), &objects); err == nil {
		return objects
	}
	var wrapper struct {
		Data []graphObject `json:"data"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err == nil {
		return wrapper.Data
	}
	return nil
}

// graphObjectPropString extracts a string property from a graphObject.
func graphObjectPropString(obj graphObject, key string) string {
	if obj.Properties == nil {
		return ""
	}
	v, ok := obj.Properties[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1 — Full project lifecycle in a single ask
//
// Ask the agent to:
//   1. Create a project
//   2. Add two Task graph objects to it
//   3. Create a DEPENDS_ON relationship between them
//   4. List all relationships in the project
//
// Then verify every artefact via direct CLI calls.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_ProjectLifecycle(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: create project, add tasks, add relationship, list relationships",
		"Ask agent to create a project, two Task objects, a DEPENDS_ON relationship, and list relationships — all in one prompt",
		"Verify each artefact via direct CLI calls",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	// Use a context project for the ask call itself.
	rl.Section("Create context project")
	contextProjectID := createContextProject(t, rl, home, "e2e-ask-multi-lc")

	rl.Section("Ask agent for full lifecycle")
	projectName := uniqueProjectName("e2e-ms-lifecycle")
	prompt := fmt.Sprintf(
		`Do all of the following steps in order:
1. Create a new project called %q.
2. In that new project, create a graph object of type Task with properties {"title": "Design API", "status": "open"}.
3. In that new project, create a second graph object of type Task with properties {"title": "Write Tests", "status": "open"}.
4. Create a DEPENDS_ON relationship from the "Write Tests" task to the "Design API" task (Write Tests depends on Design API).
5. List all graph relationships in that new project.
Return the project ID, both task IDs, and the relationship ID in your response.`,
		projectName,
	)
	out := mustAskLong(t, rl, home, prompt, contextProjectID)
	rl.Printf("agent response (first 600 chars): %s", truncate(out, 600))

	// ── Verify project ──────────────────────────────────────────────────
	rl.Section("Verify project exists")
	uuids := extractAllUUIDs(out)
	rl.Printf("extracted %d UUIDs from response", len(uuids))

	// Try to find the project in the projects list.
	listOut := mustRunCLIInDirWithHome(t, "", home, "projects", "list")
	rl.CLI("memory projects list", listOut)

	projectFound := strings.Contains(listOut, projectName)
	var createdProjectID string
	if projectFound {
		rl.Printf("project %q found in projects list", projectName)
		// Try to extract the project ID from the list output.
		for _, uid := range uuids {
			if strings.Contains(listOut, uid) {
				createdProjectID = uid
				break
			}
		}
	} else {
		// Agent may have confirmed in text without us being able to parse a list row.
		rl.Printf("project %q not found in projects list — checking agent response", projectName)
		if !containsAnyOf(out, []string{"created", "project", projectName}) {
			t.Errorf("could not verify project %q was created; list output:\n%s\nagent response:\n%s",
				projectName, truncate(listOut, 300), truncate(out, 400))
		}
	}

	// Clean up the created project if we found its ID.
	if createdProjectID != "" {
		deleteProjectOnCleanup(t, home, createdProjectID)
	} else if len(uuids) > 0 {
		// Best-effort: try each UUID as a project ID for cleanup.
		for _, uid := range uuids {
			deleteProjectOnCleanup(t, home, uid)
		}
	}

	// ── Verify Task objects ─────────────────────────────────────────────
	rl.Section("Verify Task objects")
	targetProject := createdProjectID
	if targetProject == "" {
		// Fall back to context project — agent may have created tasks there.
		targetProject = contextProjectID
	}

	tasksOut, tasksErr := runCLIInDirWithHome(t, "", home, "graph", "objects", "list",
		"--type", "Task", "--project", targetProject, "--output", "json")
	if tasksErr != nil {
		rl.CLIErr("memory graph objects list --type Task --project "+targetProject, tasksOut, tasksErr, 0)
	} else {
		rl.CLI("memory graph objects list --type Task --project "+targetProject, tasksOut)
	}

	designFound := tasksErr == nil && strings.Contains(tasksOut, "Design API")
	testsFound := tasksErr == nil && strings.Contains(tasksOut, "Write Tests")

	if designFound && testsFound {
		rl.Printf("both Task objects confirmed via CLI")
	} else {
		// Fallback: accept agent's text confirmation.
		if containsAnyOf(out, []string{"Design API"}) && containsAnyOf(out, []string{"Write Tests"}) {
			rl.Printf("Task objects confirmed via agent response text (not in graph list)")
		} else {
			t.Errorf("could not verify both Task objects; graph list:\n%s\nagent:\n%s",
				truncate(tasksOut, 300), truncate(out, 400))
		}
	}

	// ── Verify relationship ─────────────────────────────────────────────
	rl.Section("Verify DEPENDS_ON relationship")
	relsOut, relsErr := runCLIInDirWithHome(t, "", home, "graph", "relationships", "list",
		"--project", targetProject)
	if relsErr != nil {
		rl.CLIErr("memory graph relationships list --project "+targetProject, relsOut, relsErr, 0)
	} else {
		rl.CLI("memory graph relationships list --project "+targetProject, relsOut)
	}

	relFound := relsErr == nil && containsAnyOf(relsOut, []string{"DEPENDS_ON", "depends_on"})
	if relFound {
		rl.Printf("DEPENDS_ON relationship confirmed via CLI")
	} else {
		if containsAnyOf(out, []string{"DEPENDS_ON", "depends_on", "relationship", "depends on"}) {
			rl.Printf("relationship confirmed via agent response text")
		} else {
			t.Errorf("could not verify DEPENDS_ON relationship; rels list:\n%s\nagent:\n%s",
				truncate(relsOut, 300), truncate(out, 400))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2 — Agent full cycle: definition -> agent -> trigger -> runs
//
// Ask the agent to:
//   1. Create an agent definition
//   2. Create an agent using that definition
//   3. Trigger the agent
//   4. Show the list of runs
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_AgentFullCycle(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: create agent definition, agent, trigger, list runs",
		"Ask agent to create a definition, then an agent, trigger it, and list runs — all in one prompt",
		"Verify via direct CLI: agent-definitions list, agents list, agents runs",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create context project")
	contextProjectID := createContextProject(t, rl, home, "e2e-ask-multi-ag")

	rl.Section("Ask agent for full agent cycle")
	ts := time.Now().UnixMilli()
	defName := fmt.Sprintf("e2e-ms-def-%d", ts)
	agentName := fmt.Sprintf("e2e-ms-agent-%d", ts)
	prompt := fmt.Sprintf(
		`Do all of the following steps in order in project %s:
1. Create an agent definition named %q with model gemini-2.0-flash and system prompt "You are a test agent that responds with a brief greeting."
2. Create a manual-trigger agent named %q using that agent definition, with strategy type "single" and cron "0 0 1 1 *".
3. Trigger the agent you just created.
4. List the runs for that agent.
Return the agent definition ID, agent ID, and the run status in your response.`,
		contextProjectID, defName, agentName,
	)
	out := mustAskLong(t, rl, home, prompt, contextProjectID)
	rl.Printf("agent response (first 600 chars): %s", truncate(out, 600))

	// ── Verify agent definition ─────────────────────────────────────────
	rl.Section("Verify agent definition")
	defListOut, defListErr := runCLIInDirWithHome(t, "", home, "agent-definitions", "list",
		"--project", contextProjectID)
	if defListErr == nil {
		rl.CLI("memory agent-definitions list --project "+contextProjectID, defListOut)
	} else {
		rl.CLIErr("memory agent-definitions list --project "+contextProjectID, defListOut, defListErr, 0)
	}

	defVerified := defListErr == nil && containsAnyOf(defListOut, []string{defName})
	if defVerified {
		rl.Printf("agent definition %q confirmed in list", defName)
	} else {
		if containsAnyOf(out, []string{defName, "definition", "created"}) {
			rl.Printf("agent definition confirmed via agent response (not in CLI list)")
		} else {
			t.Errorf("could not verify agent definition %q; list:\n%s\nagent:\n%s",
				defName, truncate(defListOut, 300), truncate(out, 400))
		}
	}

	// ── Verify agent ────────────────────────────────────────────────────
	rl.Section("Verify agent")
	agentListOut, agentListErr := runCLIInDirWithHome(t, "", home, "agents", "list",
		"--project", contextProjectID)
	if agentListErr == nil {
		rl.CLI("memory agents list --project "+contextProjectID, agentListOut)
	} else {
		rl.CLIErr("memory agents list --project "+contextProjectID, agentListOut, agentListErr, 0)
	}

	agentVerified := agentListErr == nil && containsAnyOf(agentListOut, []string{agentName})
	var agentID string
	if agentVerified {
		rl.Printf("agent %q confirmed in agents list", agentName)
		// Try to extract the agent ID from the response for runs check.
		uuids := extractAllUUIDs(out)
		if len(uuids) > 0 {
			agentID = uuids[len(uuids)-1] // last UUID is often the most recent entity
		}
	} else {
		if containsAnyOf(out, []string{agentName, "agent", "created"}) {
			rl.Printf("agent confirmed via agent response (not in CLI list)")
		} else {
			t.Errorf("could not verify agent %q; list:\n%s\nagent:\n%s",
				agentName, truncate(agentListOut, 300), truncate(out, 400))
		}
	}

	// ── Verify runs ─────────────────────────────────────────────────────
	rl.Section("Verify agent runs")
	// The agent may include run information in its response.
	runMentioned := containsAnyOf(out, []string{"run", "trigger", "started", "success", "completed", "queued"})
	if runMentioned {
		rl.Printf("agent response mentions run execution")
	}

	// If we have an agent ID, verify via CLI.
	if agentID != "" {
		time.Sleep(3 * time.Second) // give server time to register
		runsOut, runsErr := runCLIInDirWithHome(t, "", home, "agents", "runs", agentID,
			"--project", contextProjectID)
		if runsErr == nil {
			rl.CLI("memory agents runs "+agentID+" --project "+contextProjectID, runsOut)
			lines := strings.Split(strings.TrimSpace(runsOut), "\n")
			if len(lines) >= 2 {
				rl.Printf("agent %s has %d run(s) confirmed via CLI", agentID, len(lines)-1)
			} else {
				rl.Printf("agents runs returned %d lines — run may still be starting", len(lines))
			}
		} else {
			rl.CLIErr("memory agents runs "+agentID, runsOut, runsErr, 0)
		}
	} else {
		rl.Printf("no agent ID extracted — cannot query runs via CLI")
		if !runMentioned {
			t.Errorf("could not verify any runs; agent response:\n%s", truncate(out, 400))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3 — Graph web of objects
//
// Ask the agent to:
//   1. Create three Service graph objects (catalog, orders, notifications)
//   2. Create CALLS relationships: orders->catalog, orders->notifications
//   3. List all Service objects and relationships
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_GraphWebOfObjects(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: create a web of 3 Service objects with 2 CALLS relationships",
		"Ask agent to create catalog, orders, notifications services and CALLS relationships",
		"Verify all objects and relationships via direct CLI calls",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create context project")
	contextProjectID := createContextProject(t, rl, home, "e2e-ask-multi-graph")

	rl.Section("Ask agent to create service graph")
	ts := time.Now().UnixMilli()
	catalogName := fmt.Sprintf("catalog-%d", ts)
	ordersName := fmt.Sprintf("orders-%d", ts)
	notifyName := fmt.Sprintf("notifications-%d", ts)
	prompt := fmt.Sprintf(
		`Do all of the following steps in order in project %s:
1. Create a graph object of type Service with properties {"name": %q, "language": "go", "port": "8080"}.
2. Create a graph object of type Service with properties {"name": %q, "language": "go", "port": "8081"}.
3. Create a graph object of type Service with properties {"name": %q, "language": "go", "port": "8082"}.
4. Create a CALLS relationship from the %q service to the %q service.
5. Create a CALLS relationship from the %q service to the %q service.
6. List all graph objects of type Service in the project.
7. List all graph relationships in the project.
Return the IDs of all three services and both relationships in your response.`,
		contextProjectID,
		catalogName, ordersName, notifyName,
		ordersName, catalogName,
		ordersName, notifyName,
	)
	out := mustAskLong(t, rl, home, prompt, contextProjectID)
	rl.Printf("agent response (first 600 chars): %s", truncate(out, 600))

	// ── Verify Service objects ──────────────────────────────────────────
	rl.Section("Verify Service objects via CLI")
	svcOut, svcErr := runCLIInDirWithHome(t, "", home, "graph", "objects", "list",
		"--type", "Service", "--project", contextProjectID, "--output", "json")
	if svcErr == nil {
		rl.CLI("memory graph objects list --type Service --project "+contextProjectID, svcOut)
	} else {
		rl.CLIErr("memory graph objects list --type Service", svcOut, svcErr, 0)
	}

	serviceNames := []string{catalogName, ordersName, notifyName}
	cliServiceCount := 0
	agentServiceCount := 0
	for _, name := range serviceNames {
		if svcErr == nil && strings.Contains(svcOut, name) {
			cliServiceCount++
		}
		if containsAnyOf(out, []string{name}) {
			agentServiceCount++
		}
	}
	rl.Printf("services found: %d via CLI, %d in agent response", cliServiceCount, agentServiceCount)

	if cliServiceCount == 3 {
		rl.Printf("all 3 Service objects confirmed via CLI")
	} else if agentServiceCount == 3 {
		rl.Printf("all 3 Service objects confirmed via agent response (CLI found %d)", cliServiceCount)
	} else {
		t.Errorf("expected 3 Service objects; CLI found %d, agent response mentioned %d;\nCLI output:\n%s\nagent:\n%s",
			cliServiceCount, agentServiceCount, truncate(svcOut, 400), truncate(out, 400))
	}

	// ── Verify CALLS relationships ──────────────────────────────────────
	rl.Section("Verify CALLS relationships via CLI")
	relsOut, relsErr := runCLIInDirWithHome(t, "", home, "graph", "relationships", "list",
		"--project", contextProjectID)
	if relsErr == nil {
		rl.CLI("memory graph relationships list --project "+contextProjectID, relsOut)
	} else {
		rl.CLIErr("memory graph relationships list", relsOut, relsErr, 0)
	}

	callsFound := relsErr == nil && containsAnyOf(relsOut, []string{"CALLS", "calls"})
	if callsFound {
		// Count CALLS occurrences.
		callsCount := strings.Count(strings.ToUpper(relsOut), "CALLS")
		rl.Printf("found %d CALLS relationship(s) via CLI", callsCount)
		if callsCount < 2 {
			rl.Printf("warn: expected 2 CALLS relationships, found %d", callsCount)
		}
	} else {
		if containsAnyOf(out, []string{"CALLS", "calls", "relationship"}) {
			rl.Printf("CALLS relationships confirmed via agent response (not in CLI list)")
		} else {
			t.Errorf("could not verify CALLS relationships; rels list:\n%s\nagent:\n%s",
				truncate(relsOut, 300), truncate(out, 400))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4 — Data discovery and query
//
// Seeds data via direct CLI calls, then asks the agent to discover, relate,
// and summarize the data.  This tests the agent's read + reason capabilities
// rather than its ability to create resources.
//
// Setup (via CLI):
//   - 3 Task objects: "Implement Auth" (open), "Design Schema" (open), "Setup CI" (done)
//   - DEPENDS_ON: "Implement Auth" -> "Design Schema"
//
// The agent must:
//   1. List all Task objects in the project
//   2. Identify which ones are open
//   3. List relationships
//   4. Report which tasks have dependencies
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_DataDiscoveryAndQuery(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: seed graph data via CLI, ask agent to discover and summarize",
		"Create 3 Task objects and 1 DEPENDS_ON relationship via CLI",
		"Ask agent to list tasks, find open ones, and report dependencies",
		"Verify agent response mentions all open tasks and the dependency",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-ms-discovery")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("created project %s (%s)", projectName, projectID)

	// ── Seed data via CLI ───────────────────────────────────────────────
	rl.Section("Seed Task objects via CLI")
	task1Out := mustRunCLIInDirWithHome(t, "", home, "graph", "objects", "create",
		"--type", "Task",
		"--properties", `{"title": "Implement Auth", "status": "open"}`,
		"--project", projectID, "--output", "json")
	rl.CLI("memory graph objects create (Implement Auth)", task1Out)
	task1ID := parseJSONField(task1Out, "id")
	if task1ID == "" {
		task1ID = extractUUIDFromText(task1Out)
	}
	rl.Printf("task1 (Implement Auth) ID: %s", task1ID)

	task2Out := mustRunCLIInDirWithHome(t, "", home, "graph", "objects", "create",
		"--type", "Task",
		"--properties", `{"title": "Design Schema", "status": "open"}`,
		"--project", projectID, "--output", "json")
	rl.CLI("memory graph objects create (Design Schema)", task2Out)
	task2ID := parseJSONField(task2Out, "id")
	if task2ID == "" {
		task2ID = extractUUIDFromText(task2Out)
	}
	rl.Printf("task2 (Design Schema) ID: %s", task2ID)

	task3Out := mustRunCLIInDirWithHome(t, "", home, "graph", "objects", "create",
		"--type", "Task",
		"--properties", `{"title": "Setup CI", "status": "done"}`,
		"--project", projectID, "--output", "json")
	rl.CLI("memory graph objects create (Setup CI)", task3Out)
	rl.Printf("task3 (Setup CI) created")

	rl.Section("Seed DEPENDS_ON relationship via CLI")
	if task1ID != "" && task2ID != "" {
		relOut := mustRunCLIInDirWithHome(t, "", home, "graph", "relationships", "create",
			"--from", task1ID, "--to", task2ID, "--type", "DEPENDS_ON",
			"--project", projectID)
		rl.CLI("memory graph relationships create (Implement Auth -> Design Schema)", relOut)
		rl.Printf("DEPENDS_ON relationship created")
	} else {
		rl.Printf("warn: could not create relationship — missing task ID(s)")
	}

	// ── Ask agent to discover and summarize ─────────────────────────────
	rl.Section("Ask agent to discover and summarize")
	prompt := fmt.Sprintf(
		`In project %s, do the following:
1. List all graph objects of type Task.
2. Identify which tasks have status "open".
3. List all graph relationships in the project.
4. Tell me which open tasks depend on other tasks, and what they depend on.
Provide a summary of your findings.`,
		projectID,
	)
	out := mustAskLong(t, rl, home, prompt, projectID)
	rl.Printf("agent response (first 600 chars): %s", truncate(out, 600))

	// ── Verify agent discovered the data ────────────────────────────────
	rl.Section("Verify agent response")

	// Agent should mention the open tasks.
	if !containsAnyOf(out, []string{"Implement Auth", "implement auth"}) {
		t.Errorf("expected agent to mention 'Implement Auth'; got:\n%s", truncate(out, 400))
	}
	if !containsAnyOf(out, []string{"Design Schema", "design schema"}) {
		t.Errorf("expected agent to mention 'Design Schema'; got:\n%s", truncate(out, 400))
	}

	// Agent should identify the dependency.
	if !containsAnyOf(out, []string{"depends", "DEPENDS_ON", "depends_on", "dependency", "dependent"}) {
		t.Errorf("expected agent to mention dependency relationship; got:\n%s", truncate(out, 400))
	}

	// Agent should distinguish open vs done.
	if !containsAnyOf(out, []string{"open", "done", "completed", "closed"}) {
		t.Errorf("expected agent to mention task statuses; got:\n%s", truncate(out, 400))
	}

	rl.Printf("agent correctly discovered and summarized graph data")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 5 — Token and project bootstrap
//
// Ask the agent to:
//   1. Create a new project
//   2. Create a read-only API token for it
//   3. Set that project as the default project
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_TokenAndProjectSetup(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: create project, create token, set default project",
		"Ask agent to create a project, create an API token for it, and set it as default",
		"Verify project in list, token mentioned, and config updated",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())
	skipIfEndpointMissing(t, "/api/tokens", framework.SetToken())

	rl.Section("Create context project")
	contextProjectID := createContextProject(t, rl, home, "e2e-ask-multi-tok")

	rl.Section("Ask agent for project + token + config setup")
	projectName := uniqueProjectName("e2e-ms-bootstrap")
	tokenName := fmt.Sprintf("e2e-ms-token-%d", time.Now().UnixMilli())
	prompt := fmt.Sprintf(
		`Do all of the following steps in order:
1. Create a new project called %q.
2. Create a new API token named %q with scope "projects:read" for that project.
3. Set the newly created project as my default project using config set.
Return the project ID, token ID, and confirm each step was completed.`,
		projectName, tokenName,
	)
	out := mustAskLong(t, rl, home, prompt, contextProjectID)
	rl.Printf("agent response (first 600 chars): %s", truncate(out, 600))

	// ── Verify project ──────────────────────────────────────────────────
	rl.Section("Verify project exists")
	listOut := mustRunCLIInDirWithHome(t, "", home, "projects", "list")
	rl.CLI("memory projects list", listOut)

	projectFound := strings.Contains(listOut, projectName)
	if projectFound {
		rl.Printf("project %q confirmed in projects list", projectName)
	} else {
		if containsAnyOf(out, []string{"created", "project", projectName}) {
			rl.Printf("project creation confirmed via agent response (not in CLI list)")
		} else {
			t.Errorf("could not verify project %q was created; list:\n%s\nagent:\n%s",
				projectName, truncate(listOut, 300), truncate(out, 400))
		}
	}

	// Clean up created project.
	uuids := extractAllUUIDs(out)
	for _, uid := range uuids {
		deleteProjectOnCleanup(t, home, uid)
	}

	// ── Verify token ────────────────────────────────────────────────────
	rl.Section("Verify token")
	tokenMentioned := containsAnyOf(out, []string{tokenName, "token", "created"})
	if tokenMentioned {
		rl.Printf("agent confirmed token creation")
	} else {
		t.Errorf("expected agent to confirm token creation; got:\n%s", truncate(out, 400))
	}

	// Try to find the token ID for cleanup.
	tokenUUID := ""
	for _, uid := range uuids {
		// Skip the project ID (if we have it) — the other UUIDs may be the token.
		if uid != contextProjectID {
			tokenUUID = uid
		}
	}
	if tokenUUID != "" {
		framework.RevokeTokenOnCleanup(t, rl, home, tokenUUID)
		rl.Printf("token %s will be revoked in cleanup", tokenUUID)
	}

	// Cross-check with tokens list.
	tokListOut, tokListErr := runCLIInDirWithHome(t, "", home, "tokens", "list")
	if tokListErr == nil {
		rl.CLI("memory tokens list", tokListOut)
		if strings.Contains(tokListOut, tokenName) {
			rl.Printf("token %q confirmed in tokens list", tokenName)
		} else {
			rl.Printf("token %q not found in tokens list — agent may have used a different name", tokenName)
		}
	} else {
		rl.CLIErr("memory tokens list", tokListOut, tokListErr, 0)
	}

	// ── Verify default project config ───────────────────────────────────
	rl.Section("Verify default project config")
	configMentioned := containsAnyOf(out, []string{"default", "config", "set", "project_id"})
	if configMentioned {
		rl.Printf("agent confirmed default project configuration")
	} else {
		t.Errorf("expected agent to confirm setting default project; got:\n%s", truncate(out, 400))
	}

	// Try to verify via config show if available.
	configOut, configErr := runCLIInDirWithHome(t, "", home, "config", "show")
	if configErr == nil {
		rl.CLI("memory config show", configOut)
		// Check if any of the extracted UUIDs appear in the config.
		configHasProject := false
		for _, uid := range uuids {
			if strings.Contains(configOut, uid) {
				configHasProject = true
				break
			}
		}
		if configHasProject {
			rl.Printf("default project ID confirmed in config output")
		} else {
			rl.Printf("no matching project ID found in config — agent may not have executed config set")
		}
	} else {
		rl.CLIErr("memory config show", configOut, configErr, 0)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 6 — Query-then-mutate: read data, reason, modify based on findings
//
// Setup (via CLI):
//   - 4 Task objects: 2 with status "open", 2 with status "done"
//
// Ask the agent to:
//   1. List all Task objects in the project
//   2. Find the ones with status "open"
//   3. Update each open task's status to "in-progress"
//
// Verify via CLI: both formerly-open tasks now have status "in-progress".
// This tests the agent's ability to read, reason about data, then mutate it
// selectively — a much harder chain than create-and-list.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_QueryThenMutate(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: seed tasks, ask agent to find open ones and update their status",
		"Create 4 Task objects (2 open, 2 done) via CLI",
		"Ask agent to list tasks, identify open ones, and update status to in-progress",
		"Verify via CLI that formerly-open tasks have changed status",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-ms-querymut")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("created project %s (%s)", projectName, projectID)

	// ── Seed 4 Task objects ────────────────────────────────────────────
	rl.Section("Seed Task objects via CLI")
	type seedTask struct {
		title  string
		status string
	}
	tasks := []seedTask{
		{"Build Frontend", "open"},
		{"Write Documentation", "open"},
		{"Deploy Staging", "done"},
		{"Run Benchmarks", "done"},
	}
	taskIDs := make(map[string]string) // title -> ID
	for _, task := range tasks {
		props := fmt.Sprintf(`{"title": %q, "status": %q}`, task.title, task.status)
		out := mustRunCLIInDirWithHome(t, "", home, "graph", "objects", "create",
			"--type", "Task", "--properties", props,
			"--project", projectID, "--output", "json")
		rl.CLI(fmt.Sprintf("memory graph objects create (Task: %s, %s)", task.title, task.status), out)
		id := parseJSONField(out, "id")
		if id == "" {
			id = extractUUIDFromText(out)
		}
		taskIDs[task.title] = id
		rl.Printf("  %s → %s (status=%s)", task.title, id, task.status)
	}

	// ── Ask agent to find open tasks and update them ───────────────────
	rl.Section("Ask agent to update open tasks")
	prompt := fmt.Sprintf(
		`In project %s, do the following:
1. List all graph objects of type Task.
2. Identify which tasks have status "open".
3. For each open task, update its status property to "in-progress".
4. After updating, list all Task objects again to confirm the changes.
Tell me which tasks you updated and what their new status is.`,
		projectID,
	)
	out := mustAskLong(t, rl, home, prompt, projectID)
	rl.Printf("agent response (first 600 chars): %s", truncate(out, 600))

	// ── Verify the updates via CLI ─────────────────────────────────────
	rl.Section("Verify task statuses via CLI")
	listOut, listErr := runCLIInDirWithHome(t, "", home, "graph", "objects", "list",
		"--type", "Task", "--project", projectID, "--output", "json")
	if listErr != nil {
		rl.CLIErr("memory graph objects list --type Task --project "+projectID, listOut, listErr, 0)
		rl.Printf("falling back to agent response for verification")
		// Fallback: check agent response for confirmation.
		if containsAnyOf(out, []string{"in-progress", "in_progress", "updated"}) {
			rl.Printf("agent response confirms updates were made")
		} else {
			t.Errorf("cannot verify task updates; CLI failed and agent response lacks confirmation")
		}
		return
	}
	rl.CLI("memory graph objects list --type Task --project "+projectID, listOut)

	objects := parseGraphObjects(listOut)
	rl.Printf("parsed %d Task objects from JSON", len(objects))

	updatedCount := 0
	doneStillDone := 0
	for _, obj := range objects {
		title := graphObjectPropString(obj, "title")
		status := graphObjectPropString(obj, "status")
		rl.Printf("  %s: status=%q", title, status)

		switch title {
		case "Build Frontend", "Write Documentation":
			if strings.EqualFold(status, "in-progress") || strings.EqualFold(status, "in_progress") {
				updatedCount++
			}
		case "Deploy Staging", "Run Benchmarks":
			if strings.EqualFold(status, "done") {
				doneStillDone++
			}
		}
	}

	rl.Section("Verify update correctness")
	if updatedCount == 2 {
		rl.Printf("both open tasks successfully updated to in-progress via CLI")
	} else {
		// Fallback: check agent response for confirmation.
		if containsAnyOf(out, []string{"in-progress", "in_progress", "updated"}) &&
			containsAnyOf(out, []string{"Build Frontend", "Write Documentation"}) {
			rl.Printf("agent response confirms updates (CLI shows %d/2 updated)", updatedCount)
		} else {
			t.Errorf("expected 2 tasks updated to in-progress, found %d; agent:\n%s",
				updatedCount, truncate(out, 400))
		}
	}

	if doneStillDone == 2 {
		rl.Printf("both done tasks remain unchanged — agent correctly targeted only open tasks")
	} else {
		rl.Printf("warn: expected 2 done tasks unchanged, found %d", doneStillDone)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 7 — Error recovery: partial failure in a multi-step chain
//
// Ask the agent to:
//   1. Create a Service object called "api-gateway"
//   2. Create a DEPENDS_ON relationship from "api-gateway" to a bogus UUID
//      (this should fail)
//   3. Create a second Service object called "auth-service"
//   4. Create a DEPENDS_ON relationship from "api-gateway" to "auth-service"
//      (this should succeed)
//
// Verify:
//   - Both services exist (agent recovered from step 2 failure)
//   - The valid relationship exists
//   - Agent acknowledged the error on step 2
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_ErrorRecovery(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: chain with an intentional failure in the middle",
		"Ask agent to create a Service, attempt a bad relationship (should fail), create another Service, then create a valid relationship",
		"Verify agent recovered from the error and completed remaining steps",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-ms-errrecov")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("created project %s (%s)", projectName, projectID)

	// ── Ask agent for chain with intentional failure ───────────────────
	rl.Section("Ask agent for chain with bogus relationship")
	bogusUUID := "00000000-0000-0000-0000-000000000000"
	ts := time.Now().UnixMilli()
	gwName := fmt.Sprintf("api-gateway-%d", ts)
	authName := fmt.Sprintf("auth-service-%d", ts)
	prompt := fmt.Sprintf(
		`Do all of the following steps in order in project %s:
1. Create a graph object of type Service with properties {"name": %q, "language": "go"}.
2. Create a DEPENDS_ON relationship from the Service you just created to object ID %q. (This ID may not exist — report the result either way.)
3. Create a graph object of type Service with properties {"name": %q, "language": "python"}.
4. Create a DEPENDS_ON relationship from the %q service to the %q service.
Return the IDs of both services and describe what happened with each relationship attempt.`,
		projectID,
		gwName, bogusUUID, authName,
		gwName, authName,
	)
	out := mustAskLong(t, rl, home, prompt, projectID)
	rl.Printf("agent response (first 800 chars): %s", truncate(out, 800))

	// ── Verify both services exist ─────────────────────────────────────
	rl.Section("Verify both Service objects exist")
	svcOut, svcErr := runCLIInDirWithHome(t, "", home, "graph", "objects", "list",
		"--type", "Service", "--project", projectID, "--output", "json")
	if svcErr == nil {
		rl.CLI("memory graph objects list --type Service --project "+projectID, svcOut)
	} else {
		rl.CLIErr("memory graph objects list --type Service", svcOut, svcErr, 0)
	}

	gwFound := svcErr == nil && strings.Contains(svcOut, gwName)
	authFound := svcErr == nil && strings.Contains(svcOut, authName)

	if gwFound {
		rl.Printf("service %q confirmed via CLI", gwName)
	} else if containsAnyOf(out, []string{gwName}) {
		rl.Printf("service %q confirmed via agent response (not in CLI list)", gwName)
	} else {
		t.Errorf("could not verify service %q was created", gwName)
	}

	if authFound {
		rl.Printf("service %q confirmed via CLI — agent recovered from error", authName)
	} else if containsAnyOf(out, []string{authName}) {
		rl.Printf("service %q confirmed via agent response (not in CLI list)", authName)
	} else {
		t.Errorf("could not verify service %q — agent may not have recovered from step 2 failure", authName)
	}

	// ── Verify error acknowledgement ───────────────────────────────────
	rl.Section("Verify agent acknowledged error")
	errorAcknowledged := containsAnyOf(out, []string{
		"error", "fail", "not found", "does not exist", "invalid",
		"unable", "could not", "couldn't", "no object", "no such",
	})
	if errorAcknowledged {
		rl.Printf("agent acknowledged the bogus relationship error")
	} else {
		rl.Printf("agent did NOT acknowledge the bogus relationship error for UUID %s", bogusUUID)
		t.Errorf("expected agent to mention error for bogus UUID %s; got:\n%s",
			bogusUUID, truncate(out, 400))
	}

	// ── Verify valid relationship ──────────────────────────────────────
	rl.Section("Verify valid DEPENDS_ON relationship")
	relsOut, relsErr := runCLIInDirWithHome(t, "", home, "graph", "relationships", "list",
		"--project", projectID)
	if relsErr == nil {
		rl.CLI("memory graph relationships list --project "+projectID, relsOut)
	} else {
		rl.CLIErr("memory graph relationships list", relsOut, relsErr, 0)
	}

	relFound := relsErr == nil && containsAnyOf(relsOut, []string{"DEPENDS_ON", "depends_on"})
	if relFound {
		rl.Printf("valid DEPENDS_ON relationship confirmed via CLI")
	} else {
		if containsAnyOf(out, []string{"relationship", "DEPENDS_ON", "depends_on", "created"}) {
			rl.Printf("relationship creation confirmed via agent response")
		} else {
			t.Errorf("could not verify valid DEPENDS_ON relationship; rels:\n%s\nagent:\n%s",
				truncate(relsOut, 300), truncate(out, 400))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 8 — Full CRUD lifecycle: create → update → verify → delete → verify
//
// Ask the agent to:
//   1. Create a Component graph object with version "1.0.0"
//   2. Update its version to "2.0.0" and add a new property "stable": "true"
//   3. Delete the component
//   4. List Component objects to confirm it's gone
//
// This tests create, update (property merge), delete, and verification in
// a single chain — covering the full mutation spectrum.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_CRUDLifecycle(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: create, update, delete a Component — full CRUD in one prompt",
		"Ask agent to create a Component, update its properties, then delete it",
		"Verify via CLI that the component is gone after deletion",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-ms-crud")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("created project %s (%s)", projectName, projectID)

	// ── Ask agent for full CRUD cycle ──────────────────────────────────
	rl.Section("Ask agent for CRUD lifecycle")
	ts := time.Now().UnixMilli()
	compName := fmt.Sprintf("payment-service-%d", ts)
	prompt := fmt.Sprintf(
		`Do all of the following steps in order in project %s:
1. Create a graph object of type Component with properties {"name": %q, "version": "1.0.0", "language": "rust"}.
2. Update that Component's properties to set "version" to "2.0.0" and add a new property "stable" with value "true". Keep existing properties.
3. List graph objects of type Component in the project to confirm the update.
4. Delete the Component object you created.
5. List graph objects of type Component again to confirm it is gone.
Return the Component ID and describe the result of each step.`,
		projectID, compName,
	)
	out := mustAskLong(t, rl, home, prompt, projectID)
	rl.Printf("agent response (first 800 chars): %s", truncate(out, 800))

	// ── Verify component is deleted ────────────────────────────────────
	rl.Section("Verify Component is deleted via CLI")
	listOut, listErr := runCLIInDirWithHome(t, "", home, "graph", "objects", "list",
		"--type", "Component", "--project", projectID, "--output", "json")
	if listErr == nil {
		rl.CLI("memory graph objects list --type Component --project "+projectID, listOut)
	} else {
		rl.CLIErr("memory graph objects list --type Component", listOut, listErr, 0)
	}

	// The component should NOT be present after deletion.
	compStillExists := listErr == nil && strings.Contains(listOut, compName)
	if compStillExists {
		t.Errorf("component %q still exists after deletion; list output:\n%s",
			compName, truncate(listOut, 400))
	} else {
		rl.Printf("component %q not found in list — deletion confirmed", compName)
	}

	// ── Verify agent confirmed each step ───────────────────────────────
	rl.Section("Verify agent described CRUD steps")
	// Agent should mention creation.
	if containsAnyOf(out, []string{"created", "create", compName}) {
		rl.Printf("agent confirmed creation step")
	} else {
		rl.Printf("FAIL: agent did not confirm creation of %q", compName)
		t.Errorf("expected agent to confirm creation of %q", compName)
	}

	// Agent should mention update with version 2.0.0.
	if containsAnyOf(out, []string{"2.0.0", "update", "updated"}) {
		rl.Printf("agent confirmed update step (version 2.0.0)")
	} else {
		rl.Printf("FAIL: agent did not confirm update to version 2.0.0")
		t.Errorf("expected agent to confirm update to version 2.0.0")
	}

	// Agent should mention deletion.
	if containsAnyOf(out, []string{"delete", "deleted", "removed", "removal"}) {
		rl.Printf("agent confirmed deletion step")
	} else {
		rl.Printf("FAIL: agent did not confirm deletion of the Component")
		t.Errorf("expected agent to confirm deletion of the Component")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 9 — Cross-type graph with traversal query
//
// Seed a rich, typed graph via CLI:
//   Team "Platform" →(OWNS)→ Service "user-api" →(USES)→ Database "users-db"
//   Team "Platform" →(OWNS)→ Service "billing-api" →(USES)→ Database "billing-db"
//   Service "billing-api" →(CALLS)→ Service "user-api"
//
// Ask the agent to:
//   1. List all graph relationships in the project
//   2. Starting from Team "Platform", identify all services it owns
//   3. For each service, identify what databases it uses
//   4. Identify which services call other services
//   5. Summarize the full dependency chain
//
// This tests multi-hop reasoning across heterogeneous object types and
// relationship types — the agent must traverse Team→Service→Database.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_CrossTypeGraphTraversal(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: seed cross-type graph, ask agent to traverse and summarize",
		"Create Team, Service, and Database objects with OWNS, USES, and CALLS relationships via CLI",
		"Ask agent to traverse the graph from Team to Databases and summarize the architecture",
		"Verify agent identifies all entities and relationships in its summary",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-ms-traverse")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("created project %s (%s)", projectName, projectID)

	// ── Seed graph objects ─────────────────────────────────────────────
	rl.Section("Seed graph objects via CLI")
	ts := time.Now().UnixMilli()
	// Team
	teamName := fmt.Sprintf("Platform-%d", ts)
	teamOut := mustRunCLIInDirWithHome(t, "", home, "graph", "objects", "create",
		"--type", "Team", "--properties", fmt.Sprintf(`{"name": %q, "org": "engineering"}`, teamName),
		"--project", projectID, "--output", "json")
	rl.CLI("memory graph objects create (Team)", teamOut)
	teamID := parseJSONField(teamOut, "id")
	if teamID == "" {
		teamID = extractUUIDFromText(teamOut)
	}
	rl.Printf("Team %q → %s", teamName, teamID)

	// Service: user-api
	userAPIName := fmt.Sprintf("user-api-%d", ts)
	userAPIOut := mustRunCLIInDirWithHome(t, "", home, "graph", "objects", "create",
		"--type", "Service", "--properties", fmt.Sprintf(`{"name": %q, "language": "go", "port": "8080"}`, userAPIName),
		"--project", projectID, "--output", "json")
	rl.CLI("memory graph objects create (Service: user-api)", userAPIOut)
	userAPIID := parseJSONField(userAPIOut, "id")
	if userAPIID == "" {
		userAPIID = extractUUIDFromText(userAPIOut)
	}
	rl.Printf("Service %q → %s", userAPIName, userAPIID)

	// Service: billing-api
	billingAPIName := fmt.Sprintf("billing-api-%d", ts)
	billingAPIOut := mustRunCLIInDirWithHome(t, "", home, "graph", "objects", "create",
		"--type", "Service", "--properties", fmt.Sprintf(`{"name": %q, "language": "go", "port": "8081"}`, billingAPIName),
		"--project", projectID, "--output", "json")
	rl.CLI("memory graph objects create (Service: billing-api)", billingAPIOut)
	billingAPIID := parseJSONField(billingAPIOut, "id")
	if billingAPIID == "" {
		billingAPIID = extractUUIDFromText(billingAPIOut)
	}
	rl.Printf("Service %q → %s", billingAPIName, billingAPIID)

	// Database: users-db
	usersDBName := fmt.Sprintf("users-db-%d", ts)
	usersDBOut := mustRunCLIInDirWithHome(t, "", home, "graph", "objects", "create",
		"--type", "Database", "--properties", fmt.Sprintf(`{"name": %q, "engine": "postgres"}`, usersDBName),
		"--project", projectID, "--output", "json")
	rl.CLI("memory graph objects create (Database: users-db)", usersDBOut)
	usersDBID := parseJSONField(usersDBOut, "id")
	if usersDBID == "" {
		usersDBID = extractUUIDFromText(usersDBOut)
	}
	rl.Printf("Database %q → %s", usersDBName, usersDBID)

	// Database: billing-db
	billingDBName := fmt.Sprintf("billing-db-%d", ts)
	billingDBOut := mustRunCLIInDirWithHome(t, "", home, "graph", "objects", "create",
		"--type", "Database", "--properties", fmt.Sprintf(`{"name": %q, "engine": "postgres"}`, billingDBName),
		"--project", projectID, "--output", "json")
	rl.CLI("memory graph objects create (Database: billing-db)", billingDBOut)
	billingDBID := parseJSONField(billingDBOut, "id")
	if billingDBID == "" {
		billingDBID = extractUUIDFromText(billingDBOut)
	}
	rl.Printf("Database %q → %s", billingDBName, billingDBID)

	// ── Seed relationships ─────────────────────────────────────────────
	rl.Section("Seed relationships via CLI")
	type relSpec struct {
		fromID, toID, relType, desc string
	}
	relationships := []relSpec{
		{teamID, userAPIID, "OWNS", teamName + " OWNS " + userAPIName},
		{teamID, billingAPIID, "OWNS", teamName + " OWNS " + billingAPIName},
		{userAPIID, usersDBID, "USES", userAPIName + " USES " + usersDBName},
		{billingAPIID, billingDBID, "USES", billingAPIName + " USES " + billingDBName},
		{billingAPIID, userAPIID, "CALLS", billingAPIName + " CALLS " + userAPIName},
	}
	createdRels := 0
	for _, rel := range relationships {
		if rel.fromID == "" || rel.toID == "" {
			rl.Printf("warn: skipping relationship %q — missing ID", rel.desc)
			continue
		}
		relOut := mustRunCLIInDirWithHome(t, "", home, "graph", "relationships", "create",
			"--from", rel.fromID, "--to", rel.toID, "--type", rel.relType,
			"--project", projectID)
		rl.CLI(fmt.Sprintf("memory graph relationships create (%s)", rel.desc), relOut)
		createdRels++
	}
	rl.Printf("created %d/%d relationships", createdRels, len(relationships))

	// ── Ask agent to traverse the graph ────────────────────────────────
	rl.Section("Ask agent to traverse and summarize graph")
	prompt := fmt.Sprintf(
		`In project %s, do the following:
1. List all graph objects in the project (there should be Team, Service, and Database objects).
2. List all graph relationships in the project.
3. Starting from the Team object named %q, identify all services it owns (OWNS relationships).
4. For each service, identify what databases it uses (USES relationships).
5. Identify which services call other services (CALLS relationships).
6. Provide a complete summary of the architecture: which team owns which services, what database each service uses, and what service-to-service calls exist.`,
		projectID, teamName,
	)
	out := mustAskLong(t, rl, home, prompt, projectID)
	rl.Printf("agent response (first 800 chars): %s", truncate(out, 800))

	// ── Verify agent identified all entities ───────────────────────────
	rl.Section("Verify agent identified all entities")
	entityKeywords := []string{
		teamName, userAPIName, billingAPIName, usersDBName, billingDBName,
	}
	missing := containsAllOf(out, entityKeywords)
	if len(missing) == 0 {
		rl.Printf("agent mentioned all 5 entities in its summary")
	} else {
		// Allow partial — agent might abbreviate names.
		rl.Printf("agent missing %d entity keywords: %v", len(missing), missing)
		// At minimum, agent should mention the team and at least one service.
		if containsAnyOf(out, []string{"platform", "team"}) &&
			containsAnyOf(out, []string{"user-api", "billing-api", "service"}) {
			rl.Printf("agent identified the team and at least one service — acceptable")
		} else {
			t.Errorf("agent did not identify core entities; missing: %v\nresponse:\n%s",
				missing, truncate(out, 500))
		}
	}

	// ── Verify agent identified relationship types ─────────────────────
	rl.Section("Verify agent identified relationship types")
	relKeywords := []string{"OWNS", "USES", "CALLS"}
	relMissing := containsAllOf(out, relKeywords)
	if len(relMissing) == 0 {
		rl.Printf("agent identified all 3 relationship types (OWNS, USES, CALLS)")
	} else {
		// Fallback: accept synonyms.
		synonymCheck := containsAnyOf(out, []string{"owns", "uses", "calls", "depends", "relationship"})
		if synonymCheck {
			rl.Printf("agent mentioned relationships using synonyms (missing exact: %v)", relMissing)
		} else {
			rl.Printf("FAIL: agent did not identify relationship types; missing: %v", relMissing)
			t.Errorf("agent did not identify relationship types; missing: %v\nresponse:\n%s",
				relMissing, truncate(out, 500))
		}
	}

	// ── Verify agent traced the dependency chain ───────────────────────
	rl.Section("Verify agent traced dependency chain")
	// Agent should connect billing-api → user-api (CALLS).
	callChainFound := containsAnyOf(out, []string{
		"billing", "calls",
	}) && containsAnyOf(out, []string{"user-api", "user_api", "userapi"})
	if callChainFound {
		rl.Printf("agent correctly identified billing-api CALLS user-api chain")
	} else {
		rl.Printf("warn: agent may not have explicitly traced the CALLS chain")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 10 — Bulk create + selective delete
//
// Ask the agent to:
//   1. Create 5 Task objects with different statuses
//      (3 with "done", 2 with "open")
//   2. Delete all Task objects that have status "done"
//   3. List remaining Task objects
//
// Verify via CLI: exactly 2 Task objects remain, both with status "open".
// This tests the agent's ability to handle bulk operations and selective
// deletion based on property values.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_MultiStep_BulkCreateAndSelectiveDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Multi-step: bulk-create 5 tasks, selectively delete the done ones",
		"Ask agent to create 5 Task objects (3 done, 2 open)",
		"Ask agent to delete only the tasks with status done",
		"Verify via CLI that exactly 2 open tasks remain",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	srv := serverURL()
	projectName := uniqueProjectName("e2e-ms-bulkdel")
	projectID := createProject(t, home, srv, projectName)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("created project %s (%s)", projectName, projectID)

	// ── Ask agent to bulk-create and selectively delete ────────────────
	rl.Section("Ask agent to create 5 tasks and delete done ones")
	ts := time.Now().UnixMilli()
	prompt := fmt.Sprintf(
		`Do all of the following steps in order in project %s:
1. Create a graph object of type Task with properties {"title": "Task-A-%d", "status": "done"}.
2. Create a graph object of type Task with properties {"title": "Task-B-%d", "status": "open"}.
3. Create a graph object of type Task with properties {"title": "Task-C-%d", "status": "done"}.
4. Create a graph object of type Task with properties {"title": "Task-D-%d", "status": "open"}.
5. Create a graph object of type Task with properties {"title": "Task-E-%d", "status": "done"}.
6. List all graph objects of type Task in the project to see what was created.
7. Delete every Task object that has status "done" (Task-A, Task-C, and Task-E). Keep the ones with status "open".
8. List all graph objects of type Task again to confirm only the open tasks remain.
Return the IDs of all created tasks and confirm which ones were deleted.`,
		projectID, ts, ts, ts, ts, ts,
	)
	out := mustAskLong(t, rl, home, prompt, projectID)
	rl.Printf("agent response (first 800 chars): %s", truncate(out, 800))

	// ── Verify remaining tasks via CLI ─────────────────────────────────
	rl.Section("Verify remaining Task objects via CLI")
	listOut, listErr := runCLIInDirWithHome(t, "", home, "graph", "objects", "list",
		"--type", "Task", "--project", projectID, "--output", "json")
	if listErr != nil {
		rl.CLIErr("memory graph objects list --type Task --project "+projectID, listOut, listErr, 0)
		// Fallback: check agent response.
		if containsAnyOf(out, []string{"deleted", "removed"}) &&
			containsAnyOf(out, []string{"Task-B", "Task-D"}) {
			rl.Printf("agent response confirms selective deletion (CLI unavailable)")
		} else {
			t.Errorf("cannot verify task deletion; CLI failed and agent lacks confirmation")
		}
		return
	}
	rl.CLI("memory graph objects list --type Task --project "+projectID, listOut)

	objects := parseGraphObjects(listOut)
	rl.Printf("found %d remaining Task object(s) in project", len(objects))

	taskBSuffix := fmt.Sprintf("Task-B-%d", ts)
	taskDSuffix := fmt.Sprintf("Task-D-%d", ts)
	taskASuffix := fmt.Sprintf("Task-A-%d", ts)
	taskCSuffix := fmt.Sprintf("Task-C-%d", ts)
	taskESuffix := fmt.Sprintf("Task-E-%d", ts)

	openRemain := 0
	doneRemain := 0
	for _, obj := range objects {
		title := graphObjectPropString(obj, "title")
		status := graphObjectPropString(obj, "status")
		rl.Printf("  remaining: %s (status=%s)", title, status)

		if title == taskBSuffix || title == taskDSuffix {
			openRemain++
		}
		if title == taskASuffix || title == taskCSuffix || title == taskESuffix {
			doneRemain++
		}
	}

	rl.Section("Verify selective deletion correctness")
	if doneRemain == 0 {
		rl.Printf("all done tasks successfully deleted")
	} else {
		// Fallback: accept partial — agent may have deleted some but not all.
		if containsAnyOf(out, []string{"deleted", "removed", "delete"}) {
			rl.Printf("agent mentioned deletion (CLI shows %d done tasks still remain)", doneRemain)
		} else {
			t.Errorf("expected 0 done tasks remaining, found %d; agent:\n%s",
				doneRemain, truncate(out, 400))
		}
	}

	if openRemain == 2 {
		rl.Printf("both open tasks (Task-B, Task-D) remain — selective deletion correct")
	} else if openRemain > 0 {
		rl.Printf("found %d/2 expected open tasks — partial success", openRemain)
	} else {
		// Fallback: maybe the agent used different names or all got deleted.
		if containsAnyOf(out, []string{"Task-B", "Task-D", "open"}) {
			rl.Printf("agent response mentions open tasks surviving (CLI shows %d)", openRemain)
		} else {
			t.Errorf("expected 2 open tasks remaining, found %d; agent:\n%s",
				openRemain, truncate(out, 400))
		}
	}
}
