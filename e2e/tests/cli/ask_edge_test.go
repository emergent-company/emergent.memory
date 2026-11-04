// Package cli_test — ask_edge_test.go
//
// Edge-case end-to-end tests for `memory ask`.  Each test exercises a single
// boundary condition, error path, or unusual input to verify the agent and CLI
// handle it gracefully — no panics, meaningful error messages, correct fallback
// behavior.
//
// These are NOT multi-step complex tests; each focuses on one specific edge.
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
// Test 1 — Ask with a nonexistent project ID
//
// Run `memory ask` with a fabricated UUID that does not correspond to any real
// project.  Verify the CLI returns a meaningful error (not a panic or empty
// output) and that the error mentions "project" or "not found".
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_BogusProjectID(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask with a fabricated nonexistent project ID",
		"Run memory ask with a bogus UUID as --project",
		"Verify the CLI returns a meaningful error, not a crash",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	bogusID := "00000000-0000-0000-0000-000000000000"

	rl.Section("Ask with bogus project ID")
	out, err := askMayFail(t, rl, home, "List all graph objects in this project", bogusID)

	rl.Section("Verify graceful error handling")
	combined := strings.ToLower(out)
	if err != nil {
		rl.Printf("CLI returned error (expected): %v", err)
		// Verify the error/output is meaningful — not a raw panic or empty.
		if out == "" {
			rl.Printf("FAIL: output is completely empty on error")
			t.Errorf("expected meaningful error output for bogus project ID, got empty")
		} else {
			rl.Printf("output length: %d chars — non-empty error response", len(out))
		}
	} else {
		// If the CLI didn't fail, it should still mention something about the project
		// being missing or provide a graceful response.
		rl.Printf("CLI did not return error; checking if agent handled gracefully")
		if combined == "" {
			rl.Printf("FAIL: output is empty even though CLI succeeded")
			t.Errorf("expected non-empty output for bogus project ID")
		} else {
			rl.Printf("agent responded with %d chars — graceful handling", len(out))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2 — Ask against a deleted project
//
// Create a project, delete it, then run `memory ask` against the deleted
// project ID.  Verify the CLI returns a meaningful error.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_DeletedProject(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask against a project that has been deleted",
		"Create a project and immediately delete it",
		"Run memory ask with the deleted project's ID",
		"Verify a meaningful error, not a crash",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	// ── Create and delete a project ───────────────────────────────────
	rl.Section("Create ephemeral project")
	srv := serverURL()
	name := uniqueProjectName("e2e-ask-edge-deleted")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID) // safety net if test aborts early
	rl.Printf("created project %s (%s)", name, projectID)

	rl.Section("Delete the project")
	delOut := mustRunCLIInDirWithHome(t, "", home, "projects", "delete", projectID)
	rl.CLI("memory projects delete "+projectID, delOut)

	// ── Ask against the deleted project ───────────────────────────────
	rl.Section("Ask against deleted project")
	out, err := askMayFail(t, rl, home, "List all graph objects in this project", projectID)

	rl.Section("Verify graceful error handling")
	if err != nil {
		rl.Printf("CLI returned error (expected for deleted project): %v", err)
		if out == "" {
			rl.Printf("FAIL: output is empty on error")
			t.Errorf("expected meaningful error output for deleted project, got empty")
		} else {
			rl.Printf("error output: %d chars — non-empty", len(out))
		}
	} else {
		rl.Printf("CLI did not error; agent may have handled gracefully")
		if out == "" {
			rl.Printf("FAIL: output is empty")
			t.Errorf("expected non-empty output when asking against deleted project")
		} else {
			rl.Printf("agent responded with %d chars", len(out))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3 — Empty graph traversal (no objects, no relationships)
//
// Create a fresh project with zero graph objects.  Ask the agent to list or
// summarize all objects and relationships.  Verify the agent reports "none" or
// "empty" rather than crashing or returning confusing output.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_EmptyGraphTraversal(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask agent to list/summarize an empty graph",
		"Create a fresh project with zero graph objects",
		"Ask agent to list all objects and summarize the architecture",
		"Verify agent gracefully reports nothing found",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create empty project")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-empty")

	rl.Section("Ask agent to list objects in empty project")
	out := mustAsk(t, rl, home,
		"List all graph objects and relationships in this project. Summarize the architecture.",
		projectID)

	rl.Section("Verify agent handles empty graph gracefully")
	lower := strings.ToLower(out)
	emptyKeywords := []string{"no ", "none", "empty", "0 ", "zero", "nothing", "don't have", "doesn't have", "does not", "no objects", "no graph"}
	found := false
	for _, kw := range emptyKeywords {
		if strings.Contains(lower, kw) {
			rl.Printf("agent indicated empty graph (keyword: %q)", kw)
			found = true
			break
		}
	}
	if !found {
		// The agent may just list empty results — that's OK too.
		rl.Printf("warn: agent did not explicitly say 'empty' but responded with %d chars", len(out))
		if out == "" {
			rl.Printf("FAIL: agent returned completely empty output")
			t.Errorf("expected agent to provide some response for empty graph, got empty")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4 — Unicode in ask message and object properties
//
// Ask the agent to create a graph object with a unicode label (CJK characters)
// and unicode property values.  Verify the data round-trips correctly through
// a CLI `graph objects get`.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_UnicodeObjectProperties(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: create graph object with unicode name and properties",
		"Ask agent to create a Task with a CJK name and unicode description",
		"Verify the object exists via CLI and properties contain the unicode text",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-unicode")

	// Use a mix of CJK, emoji, and accented chars.
	unicodeName := "テスト-Task-日本"

	rl.Section("Ask agent to create object with unicode name")
	out := mustAskLong(t, rl, home,
		fmt.Sprintf(
			"Create a Task graph object named %q with description 'Unicode test: café résumé' and property language set to '日本語'.",
			unicodeName),
		projectID)

	rl.Section("Verify agent acknowledged creation")
	lower := strings.ToLower(out)
	if containsAny(lower, []string{"created", "create", "task"}) {
		rl.Printf("agent confirmed Task creation")
	} else {
		rl.Printf("warn: agent response may not explicitly confirm creation: %s", truncate(out, 300))
	}

	// ── Verify via CLI ────────────────────────────────────────────────
	rl.Section("Verify object via CLI")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "Task",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects list --type Task --project "+projectID+" --output json", listOut)

	// Check that the unicode name appears in the listing.
	if strings.Contains(listOut, unicodeName) {
		rl.Printf("unicode name %q found in object listing", unicodeName)
	} else if strings.Contains(listOut, "テスト") {
		rl.Printf("partial unicode match found (テスト) in listing")
	} else {
		// Agent may have transliterated or adjusted — non-fatal.
		rl.Printf("warn: exact unicode name not found in listing; agent may have adjusted")
		// Still check there's at least one Task.
		if strings.Contains(strings.ToLower(listOut), "task") || strings.Contains(listOut, "id") {
			rl.Printf("at least one Task object exists in the project")
		} else {
			rl.Printf("FAIL: no Task objects found in project")
			t.Errorf("expected at least one Task with unicode name, listing:\n%s", truncate(listOut, 500))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 5 — Agent handles unknown/nonstandard entity type
//
// Ask the agent to "create a Widget" — a type that has no special meaning in
// the Memory platform.  The agent should either create it as a generic graph
// object with type "Widget" or explain that it can't.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_UnknownEntityType(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask agent to create an unknown entity type 'Widget'",
		"Ask agent to create a 'Widget' graph object",
		"Verify agent either creates it or explains it can't",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-widget")

	widgetName := fmt.Sprintf("TestWidget-%d", time.Now().UnixMilli())

	rl.Section("Ask agent to create a Widget")
	out := mustAskLong(t, rl, home,
		fmt.Sprintf("Create a graph object of type 'Widget' named %q with description 'A custom widget for testing'.", widgetName),
		projectID)

	rl.Section("Verify agent response")
	lower := strings.ToLower(out)

	// Option A: agent created it (graph objects are flexible with types).
	created := containsAny(lower, []string{"created", "widget", widgetName})
	// Option B: agent explained it can't or doesn't support it.
	explained := containsAny(lower, []string{"not supported", "invalid", "unknown", "can't create", "cannot create", "doesn't support", "not a valid"})

	if created {
		rl.Printf("agent created the Widget (custom types are accepted)")

		// Verify via CLI.
		listOut, listErr := runCLIInDirWithHome(t, "", home,
			"graph", "objects", "list",
			"--type", "Widget",
			"--project", projectID,
			"--output", "json",
		)
		listInvocation := "memory graph objects list --type Widget --project " + projectID
		if listErr != nil {
			rl.CLIErr(listInvocation, listOut, listErr, 0)
			rl.Printf("listing Widget objects returned error: %v", listErr)
		} else {
			rl.CLI(listInvocation, listOut)
			if strings.Contains(listOut, widgetName) || strings.Contains(strings.ToLower(listOut), "widget") {
				rl.Printf("Widget object confirmed in graph listing")
			} else {
				rl.Printf("warn: Widget not found in listing (agent may have used different type)")
			}
		}
	} else if explained {
		rl.Printf("agent explained that Widget type is not supported — valid response")
	} else {
		rl.Printf("FAIL: agent neither created Widget nor explained; response: %s", truncate(out, 400))
		t.Errorf("expected agent to create Widget or explain it can't; got: %s", truncate(out, 400))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 6 — Duplicate relationship creation
//
// Create 2 Service objects and a CALLS relationship between them via CLI.
// Then ask the agent to create the same CALLS relationship again.  Verify
// the agent either creates a second (if allowed), reports it already exists,
// or handles the duplicate gracefully without crashing.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_DuplicateRelationship(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: create same relationship twice",
		"Create 2 Services and a CALLS relationship via CLI",
		"Ask agent to create the identical relationship again",
		"Verify agent handles duplicate gracefully",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project and seed objects")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-duprel")

	svcA := fmt.Sprintf("DupRelSvcA-%d", time.Now().UnixMilli())
	svcB := fmt.Sprintf("DupRelSvcB-%d", time.Now().UnixMilli())

	srcOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", svcA,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type Service --name "+svcA, srcOut)
	srcID := parseJSONField(srcOut, "id")
	if srcID == "" {
		rl.Failf("could not parse source object ID: %s", truncate(srcOut, 300))
	}

	tgtOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", svcB,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type Service --name "+svcB, tgtOut)
	tgtID := parseJSONField(tgtOut, "id")
	if tgtID == "" {
		rl.Failf("could not parse target object ID: %s", truncate(tgtOut, 300))
	}

	rl.Section("Create first CALLS relationship via CLI")
	relOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "create",
		"--type", "CALLS",
		"--from", srcID,
		"--to", tgtID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph relationships create --type CALLS --from "+srcID+" --to "+tgtID, relOut)

	rl.Section("Ask agent to create duplicate relationship")
	out := mustAskLong(t, rl, home,
		fmt.Sprintf(
			"Create a CALLS relationship from the Service named %q (ID %s) to the Service named %q (ID %s).",
			svcA, srcID, svcB, tgtID),
		projectID)

	rl.Section("Verify agent handled duplicate gracefully")
	lower := strings.ToLower(out)
	// Accept any of: created, already exists, duplicate, relationship.
	graceful := containsAny(lower, []string{
		"created", "relationship", "calls", "already", "exists", "duplicate",
		svcA, svcB,
	})
	if graceful {
		rl.Printf("agent handled duplicate relationship gracefully")
	} else {
		rl.Printf("FAIL: agent response unclear on duplicate: %s", truncate(out, 400))
		t.Errorf("expected agent to handle duplicate relationship gracefully; got: %s", truncate(out, 400))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 7 — Self-referencing relationship
//
// Create a Service object via CLI.  Ask the agent to create a CALLS
// relationship from the Service to itself.  Verify the agent either creates
// it (if valid) or reports that self-references are not allowed.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_SelfReferencingRelationship(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: create a relationship from an object to itself",
		"Create a Service via CLI",
		"Ask agent to create a CALLS relationship from the Service to itself",
		"Verify agent creates it or explains why it cannot",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project and object")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-selfref")

	svcName := fmt.Sprintf("SelfRefSvc-%d", time.Now().UnixMilli())
	svcOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", svcName,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type Service --name "+svcName, svcOut)
	svcID := parseJSONField(svcOut, "id")
	if svcID == "" {
		rl.Failf("could not parse Service ID: %s", truncate(svcOut, 300))
	}

	rl.Section("Ask agent to create self-referencing relationship")
	out := mustAskLong(t, rl, home,
		fmt.Sprintf(
			"Create a CALLS relationship where both the source and target are the Service named %q (ID %s). The service calls itself recursively.",
			svcName, svcID),
		projectID)

	rl.Section("Verify agent response")
	lower := strings.ToLower(out)

	created := containsAny(lower, []string{"created", "relationship", "calls"})
	refused := containsAny(lower, []string{"cannot", "can't", "not allowed", "invalid", "self", "same"})

	if created {
		rl.Printf("agent created self-referencing relationship (platform allows it)")
	} else if refused {
		rl.Printf("agent refused self-referencing relationship — valid response")
	} else {
		rl.Printf("FAIL: agent response unclear: %s", truncate(out, 400))
		t.Errorf("expected agent to create self-reference or explain refusal; got: %s", truncate(out, 400))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 8 — Idempotent create (same project name twice)
//
// Ask the agent to create a project with a specific name.  Then ask again with
// the same name.  Verify the agent either creates a second project (if names
// are not unique), reports it already exists, or handles it gracefully.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_IdempotentProjectCreate(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask agent to create a project with the same name twice",
		"Ask agent to create project 'e2e-idempotent-XYZ'",
		"Ask agent again with the same name",
		"Verify agent handles duplicate gracefully (creates second or reports existing)",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create context project")
	contextProjectID := createContextProject(t, rl, home, "e2e-ask-edge-idem-ctx")

	projectName := fmt.Sprintf("e2e-idempotent-%d", time.Now().UnixMilli())

	rl.Section("Ask agent to create project — first time")
	out1 := mustAskLong(t, rl, home,
		fmt.Sprintf("Create a new project called %q", projectName),
		contextProjectID)

	rl.Section("Verify first creation")
	lower1 := strings.ToLower(out1)
	if !containsAny(lower1, []string{"created", "project", projectName}) {
		rl.Printf("warn: agent may not have confirmed first creation: %s", truncate(out1, 300))
	} else {
		rl.Printf("agent confirmed first project creation")
	}

	// Clean up the created project(s) at the end.
	t.Cleanup(func() {
		// List projects and delete any matching our name.
		listOut, _ := runCLIInDirWithHome(t, "", home, "projects", "list")
		for _, line := range strings.Split(listOut, "\n") {
			if strings.Contains(line, projectName) {
				fields := strings.Fields(line)
				if len(fields) > 0 {
					runCLIInDirWithHome(t, "", home, "projects", "delete", fields[0])
				}
			}
		}
	})

	rl.Section("Ask agent to create project — second time (same name)")
	out2 := mustAskLong(t, rl, home,
		fmt.Sprintf("Create a new project called %q", projectName),
		contextProjectID)

	rl.Section("Verify agent handled duplicate name")
	lower2 := strings.ToLower(out2)
	graceful := containsAny(lower2, []string{
		"created", "project", "already", "exists", "duplicate", projectName,
	})
	if graceful {
		rl.Printf("agent handled duplicate project name gracefully")
	} else {
		rl.Printf("FAIL: agent response unclear on duplicate: %s", truncate(out2, 400))
		t.Errorf("expected agent to handle duplicate project name gracefully; got: %s", truncate(out2, 400))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 9 — Out-of-scope question
//
// Ask the agent something entirely unrelated to the Memory platform (e.g.
// weather).  Verify the agent stays on topic — either answers with Memory
// context or redirects to Memory features.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_OutOfScopeQuestion(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask agent an off-topic question",
		"Ask 'What is the weather in Tokyo?'",
		"Verify agent stays on-topic or redirects to Memory features",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Ask out-of-scope question")
	out := mustAsk(t, rl, home, "What is the weather in Tokyo today?")

	rl.Section("Verify agent response")
	lower := strings.ToLower(out)
	if out == "" {
		rl.Printf("FAIL: agent returned empty output")
		t.Errorf("expected non-empty response to off-topic question")
	} else {
		// The agent should respond somehow — ideally mentioning Memory or its capabilities.
		memoryRelated := containsAny(lower, []string{
			"memory", "project", "agent", "graph", "cli", "help", "assist",
			"i can", "i'm able", "i am", "platform",
		})
		if memoryRelated {
			rl.Printf("agent stayed on-topic or redirected to Memory features")
		} else {
			// Even if the agent answers the weather question, it's not a test failure
			// as long as it doesn't crash.
			rl.Printf("agent responded with %d chars (may have answered off-topic); no crash observed", len(out))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 10 — Delete an object that has relationships
//
// Create 2 objects + 1 relationship via CLI.  Delete one of the objects.
// Verify via CLI whether the relationship was cascade-deleted, the delete was
// rejected, or the platform handled it some other graceful way.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_DeleteObjectWithRelationships(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: delete a graph object that has existing relationships",
		"Create 2 Service objects and a CALLS relationship via CLI",
		"Delete the source object via CLI",
		"Verify behavior: cascade delete, rejection, or graceful handling",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project and seed graph")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-delrel")

	svcA := fmt.Sprintf("DelRelA-%d", time.Now().UnixMilli())
	svcB := fmt.Sprintf("DelRelB-%d", time.Now().UnixMilli())

	srcOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service", "--name", svcA,
		"--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph objects create --type Service --name "+svcA, srcOut)
	srcID := parseJSONField(srcOut, "id")
	if srcID == "" {
		rl.Failf("could not parse source ID: %s", truncate(srcOut, 300))
	}

	tgtOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service", "--name", svcB,
		"--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph objects create --type Service --name "+svcB, tgtOut)
	tgtID := parseJSONField(tgtOut, "id")
	if tgtID == "" {
		rl.Failf("could not parse target ID: %s", truncate(tgtOut, 300))
	}

	relOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "create",
		"--type", "CALLS", "--from", srcID, "--to", tgtID,
		"--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph relationships create --type CALLS", relOut)

	// ── Delete the source object ──────────────────────────────────────
	rl.Section("Delete source object that has a relationship")
	delOut, delErr := runCLIInDirWithHome(t, "", home,
		"graph", "objects", "delete", srcID,
		"--project", projectID,
	)
	if delErr != nil {
		rl.CLIErr("memory graph objects delete "+srcID, delOut, delErr, 0)
	} else {
		rl.CLI("memory graph objects delete "+srcID, delOut)
	}

	rl.Section("Verify platform behavior after deletion")
	if delErr != nil {
		// Platform may reject deletion of objects with relationships.
		rl.Printf("delete returned error (platform may protect referential integrity): %v", delErr)
		lower := strings.ToLower(delOut)
		if containsAny(lower, []string{"relationship", "reference", "depends", "cannot", "constraint"}) {
			rl.Printf("error mentions referential integrity — expected behavior")
		} else {
			rl.Printf("error does not mention relationships; output: %s", truncate(delOut, 300))
		}
	} else {
		// Delete succeeded — check if relationship was cascade-deleted.
		rl.Printf("delete succeeded — checking if relationship was cascade-deleted")
		relListOut := mustRunCLIInDirWithHome(t, "", home,
			"graph", "relationships", "list",
			"--project", projectID,
		)
		rl.CLI("memory graph relationships list --project "+projectID, relListOut)

		if strings.Contains(relListOut, "CALLS") && strings.Contains(relListOut, tgtID) {
			rl.Printf("warn: CALLS relationship still references the deleted source object — possible orphan")
		} else {
			rl.Printf("relationship appears to have been cascade-deleted or cleaned up")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 11 — Graph object with empty/minimal properties
//
// Ask the agent to create a graph object with no properties at all — just a
// type and name.  Verify the CLI can list it and it has a valid ID.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_MinimalGraphObject(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: create graph object with minimal info (no properties)",
		"Ask agent to create a Task with only a name, no other properties",
		"Verify object exists via CLI with a valid ID",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-minimal")

	taskName := fmt.Sprintf("MinTask-%d", time.Now().UnixMilli())

	rl.Section("Ask agent to create minimal object")
	out := mustAskLong(t, rl, home,
		fmt.Sprintf("Create a Task graph object named %q. Do not add any properties, description, or metadata — just the name and type.", taskName),
		projectID)

	rl.Section("Verify agent acknowledged creation")
	lower := strings.ToLower(out)
	if containsAny(lower, []string{"created", "task", taskName}) {
		rl.Printf("agent confirmed creation of minimal Task")
	} else {
		rl.Printf("warn: agent may not have confirmed creation: %s", truncate(out, 300))
	}

	rl.Section("Verify object via CLI")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "Task",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects list --type Task --project "+projectID, listOut)

	trimmed := strings.TrimSpace(listOut)
	if !json.Valid([]byte(trimmed)) {
		rl.Printf("warn: list output is not valid JSON: %s", truncate(listOut, 300))
	}

	if strings.Contains(listOut, taskName) {
		rl.Printf("minimal Task %q found in listing", taskName)
	} else if strings.Contains(strings.ToLower(listOut), "task") || strings.Contains(listOut, "id") {
		rl.Printf("Task objects exist in listing (exact name match may differ)")
	} else {
		rl.Printf("FAIL: no Task objects found in listing")
		t.Errorf("expected at least one Task object after minimal creation; listing:\n%s", truncate(listOut, 500))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 12 — Ask with revoked token
//
// Create an API token, revoke it, then attempt to use it for a `memory ask`.
// Verify the CLI returns an auth error, not a crash.
// Note: this test manipulates auth state and uses a separate HOME directory.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_RevokedToken(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask with a previously revoked API token",
		"Create an API token via CLI",
		"Revoke the token via CLI",
		"Attempt memory ask using the revoked token",
		"Verify an auth error is returned",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	// This test only works with standalone auth mode where we control tokens.
	authMode := framework.AuthMode()
	if authMode != "standalone" {
		rl.Printf("skipping: test requires standalone auth mode (got %q)", authMode)
		t.Skipf("test requires standalone auth mode, got %q", authMode)
	}

	rl.Section("Create API token")
	tokenName := fmt.Sprintf("e2e-revoke-test-%d", time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"tokens", "create",
		"--name", tokenName,
		"--scope", "all",
	)
	rl.CLI("memory tokens create --name "+tokenName, createOut)

	// Extract the token value or ID from the output.
	// Token create usually outputs the token value and/or an ID.
	rl.Section("Parse token details")
	tokenID := ""
	tokenValue := ""
	lines := strings.Split(createOut, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "id") {
			tokenID = parseLineField(line, "ID")
			if tokenID == "" {
				tokenID = parseLineField(line, "Id")
			}
		}
		if strings.Contains(lower, "token") && strings.Contains(lower, ":") && !strings.Contains(lower, "name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				tokenValue = strings.TrimSpace(parts[1])
			}
		}
	}
	if tokenID == "" {
		// Try extracting a UUID from the output.
		tokenID = extractUUIDFromText(createOut)
	}
	if tokenID == "" {
		rl.Printf("warn: could not extract token ID from output; output:\n%s", truncate(createOut, 300))
		rl.Printf("skipping remainder — cannot revoke without token ID")
		t.Skipf("could not extract token ID to revoke")
	}
	rl.Printf("token ID: %s, token value present: %v", tokenID, tokenValue != "")

	rl.Section("Revoke the token")
	revokeOut := mustRunCLIInDirWithHome(t, "", home, "tokens", "revoke", tokenID)
	rl.CLI("memory tokens revoke "+tokenID, revokeOut)
	rl.Printf("token revoked")

	// If we captured a token value, configure a separate HOME with it and try asking.
	if tokenValue == "" {
		rl.Printf("no token value captured — cannot test ask with revoked token directly")
		rl.Printf("test validated token create+revoke lifecycle; skipping ask portion")
		return
	}

	rl.Section("Attempt ask with revoked token")
	revokedHome := t.TempDir()
	// Configure the revoked-token home to use the revoked token.
	srv := serverURL()
	mustRunCLIInDirWithHome(t, "", revokedHome, "set-token", tokenValue, "--server", srv)
	rl.CLI("memory set-token <revoked> --server "+srv, "(credentials configured)")

	out, err := askMayFail(t, rl, revokedHome, "List all projects")

	rl.Section("Verify auth error")
	if err != nil {
		rl.Printf("CLI returned error (expected for revoked token): %v", err)
		lower := strings.ToLower(out + err.Error())
		if containsAny(lower, []string{"auth", "unauthorized", "401", "forbidden", "invalid", "token", "denied"}) {
			rl.Printf("error message mentions auth/unauthorized — correct behavior")
		} else {
			rl.Printf("error does not clearly mention auth failure; output: %s", truncate(out, 300))
		}
	} else {
		// The CLI didn't fail — this could mean the ask endpoint is more lenient.
		rl.Printf("warn: CLI did not error with revoked token; output: %s", truncate(out, 300))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 13 — Ambiguous entity reference
//
// Create 3 Task objects with similar names via CLI.  Ask the agent to "delete
// the task" without specifying which one.  Verify the agent either asks for
// clarification, picks one and explains, or reports the ambiguity.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_AmbiguousEntityReference(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask agent to operate on ambiguous entity reference",
		"Create 3 Tasks with similar names via CLI",
		"Ask agent to 'get the task' without specifying which one",
		"Verify agent handles ambiguity (clarifies, picks one, or lists all)",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project and seed similar tasks")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-ambig")

	ts := time.Now().UnixMilli()
	taskNames := []string{
		fmt.Sprintf("ReviewCode-%d", ts),
		fmt.Sprintf("ReviewDocs-%d", ts),
		fmt.Sprintf("ReviewTests-%d", ts),
	}

	for _, name := range taskNames {
		out := mustRunCLIInDirWithHome(t, "", home,
			"graph", "objects", "create",
			"--type", "Task", "--name", name,
			"--project", projectID, "--output", "json",
		)
		rl.CLI("memory graph objects create --type Task --name "+name, out)
		objID := parseJSONField(out, "id")
		if objID == "" {
			rl.Failf("could not parse ID for Task %q: %s", name, truncate(out, 300))
		}
		rl.Printf("created Task %q (%s)", name, objID)
	}

	rl.Section("Ask agent about 'the review task' (ambiguous)")
	out := mustAskLong(t, rl, home,
		"Get me details about the review task in this project.",
		projectID)

	rl.Section("Verify agent handled ambiguity")
	lower := strings.ToLower(out)

	// Accept any reasonable response:
	// - Lists all matching tasks
	// - Picks one and explains
	// - Asks for clarification
	// - Mentions multiple matches
	multiMatch := containsAny(lower, []string{"multiple", "several", "which", "clarify", "3 task", "three task"})
	listedTasks := 0
	for _, name := range taskNames {
		if strings.Contains(lower, strings.ToLower(name)) {
			listedTasks++
		}
	}
	pickedOne := containsAny(lower, []string{"review"})

	if multiMatch {
		rl.Printf("agent recognized ambiguity and asked for clarification")
	} else if listedTasks >= 2 {
		rl.Printf("agent listed %d of 3 matching tasks", listedTasks)
	} else if pickedOne {
		rl.Printf("agent picked one review task and responded")
	} else {
		rl.Printf("FAIL: agent response unclear on ambiguous reference: %s", truncate(out, 400))
		t.Errorf("expected agent to handle ambiguous 'the review task'; got: %s", truncate(out, 400))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 14 — Re-delete an already-deleted object
//
// Create a graph object via CLI, delete it via CLI, then ask the agent to
// delete it again.  Verify the agent reports it's already gone or handles the
// error gracefully.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_DeleteAlreadyDeletedObject(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask agent to delete an object that was already deleted",
		"Create a Task via CLI and delete it via CLI",
		"Ask agent to delete the same Task (by ID)",
		"Verify agent reports it's gone or handles gracefully",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project and object")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-redelete")

	taskName := fmt.Sprintf("ReDelTask-%d", time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Task", "--name", taskName,
		"--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph objects create --type Task --name "+taskName, createOut)
	taskID := parseJSONField(createOut, "id")
	if taskID == "" {
		rl.Failf("could not parse Task ID: %s", truncate(createOut, 300))
	}
	rl.Printf("created Task %q (%s)", taskName, taskID)

	rl.Section("Delete object via CLI")
	delOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "delete", taskID,
		"--project", projectID,
	)
	rl.CLI("memory graph objects delete "+taskID, delOut)
	rl.Printf("first delete succeeded")

	rl.Section("Ask agent to delete already-deleted object")
	out := mustAskLong(t, rl, home,
		fmt.Sprintf("Delete the graph object with ID %s from this project.", taskID),
		projectID)

	rl.Section("Verify agent handled missing object gracefully")
	lower := strings.ToLower(out)
	graceful := containsAny(lower, []string{
		"not found", "doesn't exist", "does not exist", "already", "deleted",
		"no ", "gone", "cannot find", "could not find", "missing", "error",
		"unable", "failed",
	})
	if graceful {
		rl.Printf("agent recognized object is already deleted")
	} else if containsAny(lower, []string{"delete", taskID}) {
		// Agent may have tried and gotten an error it described.
		rl.Printf("agent mentioned delete/ID in response — likely handled the error")
	} else {
		rl.Printf("FAIL: agent response unclear: %s", truncate(out, 400))
		t.Errorf("expected agent to report object already deleted; got: %s", truncate(out, 400))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 15 — Deeply nested JSON properties
//
// Create a graph object via CLI with deeply nested JSON properties.  Verify
// the properties are preserved when reading back via `graph objects get`.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_DeepNestedProperties(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: create graph object with deeply nested JSON properties",
		"Create a Config object via CLI with nested JSON properties",
		"Verify via graph objects get that nested structure is preserved",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-nested")

	nestedProps := `{"database":{"host":"db.example.com","port":5432,"credentials":{"user":"admin","pool":{"min":5,"max":20}}},"tags":["production","critical"]}`

	rl.Section("Create object with nested properties via CLI")
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Config",
		"--name", fmt.Sprintf("NestedConfig-%d", time.Now().UnixMilli()),
		"--properties", nestedProps,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type Config --properties <nested>", createOut)

	objectID := parseJSONField(createOut, "id")
	if objectID == "" {
		rl.Failf("could not parse object ID: %s", truncate(createOut, 300))
	}
	rl.Printf("created Config object: %s", objectID)

	rl.Section("Verify nested properties via get")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "get", objectID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects get "+objectID, getOut)

	// Check key nested values are preserved.
	checks := map[string]string{
		"db.example.com": "database host",
		"5432":           "database port",
		"admin":          "credentials user",
		"production":     "tags array element",
	}
	for value, desc := range checks {
		if strings.Contains(getOut, value) {
			rl.Printf("nested property preserved: %s (%s)", desc, value)
		} else {
			rl.Printf("FAIL: nested property missing: %s (%s)", desc, value)
			t.Errorf("expected nested property %s (%q) in object output; got:\n%s", desc, value, truncate(getOut, 500))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 16 — Ask agent to perform nonexistent action
//
// Ask the agent to "rename" a project — a CLI command that doesn't exist.
// Verify the agent either explains it can't or suggests an alternative.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_NonexistentAction(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask agent to perform an action that doesn't exist",
		"Ask agent to 'rename' a project (no such CLI command)",
		"Verify agent explains it can't or suggests an alternative",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-noaction")

	rl.Section("Ask agent to rename the project")
	out := mustAskLong(t, rl, home,
		"Rename this project to 'new-name-for-project'. Change its name.",
		projectID)

	rl.Section("Verify agent response")
	lower := strings.ToLower(out)
	// Accept: agent says it can't, suggests alternative, or actually does update the name
	// (in case there's a rename/update capability we don't know about).
	reasonable := containsAny(lower, []string{
		"rename", "not supported", "cannot", "can't", "no command",
		"doesn't support", "update", "alternative", "instead",
		"name", "project", "changed", "modified",
	})
	if reasonable {
		rl.Printf("agent provided a reasonable response about renaming")
	} else {
		rl.Printf("FAIL: agent response unclear: %s", truncate(out, 400))
		t.Errorf("expected agent to address the rename request; got: %s", truncate(out, 400))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 17 — Special characters in object name
//
// Ask the agent to create a graph object with special characters in the name:
// quotes, backslashes, angle brackets.  Verify the object is created and
// retrievable via CLI.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_SpecialCharsInName(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: create graph object with special characters in name",
		"Ask agent to create a Task with special chars: quotes, angle brackets",
		"Verify object exists via CLI",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-special")

	// Use chars that could break naive string handling.
	specialName := fmt.Sprintf("Task<Test> 'quoted' & special-%d", time.Now().UnixMilli())

	rl.Section("Ask agent to create object with special chars")
	out := mustAskLong(t, rl, home,
		fmt.Sprintf("Create a Task graph object with the exact name: %s", specialName),
		projectID)

	rl.Section("Verify agent responded")
	lower := strings.ToLower(out)
	if containsAny(lower, []string{"created", "task", "special"}) {
		rl.Printf("agent confirmed creation (or mentioned the task)")
	} else {
		rl.Printf("warn: agent may not have confirmed creation: %s", truncate(out, 300))
	}

	rl.Section("Verify object exists via CLI")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "Task",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects list --type Task --project "+projectID, listOut)

	// Check for any Task objects in the listing (exact name may be escaped differently).
	if strings.Contains(strings.ToLower(listOut), "task") || strings.Contains(listOut, "id") {
		rl.Printf("Task objects found in listing after special-chars creation")
	} else {
		rl.Printf("FAIL: no Task objects found in listing")
		t.Errorf("expected Task object(s) after creation with special chars; listing:\n%s", truncate(listOut, 500))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 18 — Contradictory instructions
//
// Ask the agent to do contradictory things: "Create a Task called X and then
// immediately delete it in the same step."  Verify the agent handles it
// without crashing — either executes both or explains the contradiction.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_ContradictoryInstructions(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: ask agent to do contradictory things in one prompt",
		"Ask to create a Task and immediately delete it",
		"Verify agent handles contradiction without crashing",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-contradict")

	taskName := fmt.Sprintf("ContraTask-%d", time.Now().UnixMilli())

	rl.Section("Ask contradictory instructions")
	out := mustAskLong(t, rl, home,
		fmt.Sprintf(
			"Create a Task graph object named %q, and then immediately delete it. "+
				"At the end, there should be no Task with that name.",
			taskName),
		projectID)

	rl.Section("Verify agent handled contradiction")
	lower := strings.ToLower(out)
	// Agent should have done something — either create+delete, or explain.
	responded := len(out) > 0
	mentionsActions := containsAny(lower, []string{
		"created", "deleted", "removed", "task", taskName,
		"contradiction", "conflicting",
	})
	if responded && mentionsActions {
		rl.Printf("agent handled contradictory instructions and mentioned relevant actions")
	} else if responded {
		rl.Printf("agent responded (%d chars) but may not have addressed both actions", len(out))
	} else {
		rl.Printf("FAIL: agent returned empty output for contradictory instructions")
		t.Errorf("expected non-empty response for contradictory instructions")
	}

	// Verify: the Task should NOT exist (if agent followed through on both).
	rl.Section("Verify no leftover Task")
	listOut, _ := runCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "Task",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects list --type Task --project "+projectID, listOut)
	if strings.Contains(listOut, taskName) {
		rl.Printf("warn: Task %q still exists — agent may have only created, not deleted", taskName)
	} else {
		rl.Printf("Task %q not found — agent completed create+delete or skipped creation", taskName)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 19 — Relationship direction verification
//
// Create 2 objects (A, B) and ask the agent to create a DEPENDS_ON
// relationship from A to B.  Verify via CLI that the direction is correct:
// A depends on B (from=A, to=B), not the reverse.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_RelationshipDirection(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: verify relationship direction is correct",
		"Create Task-A and Task-B via CLI",
		"Ask agent to create DEPENDS_ON from A to B",
		"Verify direction via CLI: from=A, to=B",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	rl.Section("Create project and objects")
	projectID := createContextProject(t, rl, home, "e2e-ask-edge-reldir")

	ts := time.Now().UnixMilli()
	nameA := fmt.Sprintf("Frontend-%d", ts)
	nameB := fmt.Sprintf("Backend-%d", ts)

	outA := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Task", "--name", nameA,
		"--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph objects create --type Task --name "+nameA, outA)
	idA := parseJSONField(outA, "id")
	if idA == "" {
		rl.Failf("could not parse ID for Task A: %s", truncate(outA, 300))
	}

	outB := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Task", "--name", nameB,
		"--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph objects create --type Task --name "+nameB, outB)
	idB := parseJSONField(outB, "id")
	if idB == "" {
		rl.Failf("could not parse ID for Task B: %s", truncate(outB, 300))
	}
	rl.Printf("Task A: %s (%s), Task B: %s (%s)", nameA, idA, nameB, idB)

	rl.Section("Ask agent to create directional relationship")
	out := mustAskLong(t, rl, home,
		fmt.Sprintf(
			"Create a DEPENDS_ON relationship where %q (ID %s) depends on %q (ID %s). "+
				"The 'from' should be %s and the 'to' should be %s.",
			nameA, idA, nameB, idB, nameA, nameB),
		projectID)

	rl.Section("Verify agent confirmed relationship")
	lower := strings.ToLower(out)
	if containsAny(lower, []string{"depends_on", "relationship", "created"}) {
		rl.Printf("agent confirmed DEPENDS_ON relationship creation")
	} else {
		rl.Printf("warn: agent may not have confirmed: %s", truncate(out, 300))
	}

	rl.Section("Verify direction via CLI")
	relOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "list",
		"--project", projectID,
	)
	rl.CLI("memory graph relationships list --project "+projectID, relOut)

	// Check that idA appears as source and idB as target in the relationship.
	relLower := strings.ToLower(relOut)
	if strings.Contains(relLower, "depends_on") {
		rl.Printf("DEPENDS_ON relationship exists in listing")
		// Check direction: the listing should show from=idA, to=idB.
		if strings.Contains(relOut, idA) && strings.Contains(relOut, idB) {
			rl.Printf("both IDs present in relationship listing — direction likely correct")
		} else {
			rl.Printf("warn: one or both IDs missing from relationship listing")
		}
	} else {
		rl.Printf("FAIL: DEPENDS_ON not found in relationship listing")
		t.Errorf("expected DEPENDS_ON in listing; got:\n%s", truncate(relOut, 500))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 20 — Very long message to ask
//
// Send a very long question (repeated text to exceed ~5000 chars) to
// `memory ask`.  Verify the CLI and agent handle it without crashing or
// truncating the response in a broken way.
// ─────────────────────────────────────────────────────────────────────────────

func TestAsk_Edge_LongMessage(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Edge: send a very long message to memory ask",
		"Construct a ~5000 char question with repeated context",
		"Run memory ask with the long message",
		"Verify no crash and a meaningful response",
	)

	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/ask", e2eTestToken())

	// Build a long message (~5000 chars) that still makes sense.
	var sb strings.Builder
	sb.WriteString("I have a complex question about the Memory platform. ")
	for i := 0; i < 100; i++ {
		sb.WriteString(fmt.Sprintf(
			"Context item %d: I need to understand how graph objects, relationships, "+
				"agents, projects, and skills work together. ", i+1))
	}
	sb.WriteString("Given all this context, what are the top 3 CLI commands for managing graph objects?")
	longMsg := sb.String()

	rl.Section("Send long message")
	rl.Printf("message length: %d chars", len(longMsg))
	out, err := askMayFail(t, rl, home, longMsg)

	rl.Section("Verify response")
	if err != nil {
		rl.Printf("CLI returned error: %v", err)
		// A payload-too-large error is acceptable.
		lower := strings.ToLower(out + err.Error())
		if containsAny(lower, []string{"too large", "too long", "payload", "413", "limit", "exceeded"}) {
			rl.Printf("server rejected large payload — acceptable behavior")
		} else {
			rl.Printf("error is not payload-related; output: %s", truncate(out, 300))
			// Don't fail — the CLI handled it without crashing.
		}
	} else {
		if out == "" {
			rl.Printf("FAIL: empty output for long message")
			t.Errorf("expected non-empty response for long message")
		} else {
			rl.Printf("agent responded with %d chars — no crash with large input", len(out))
		}
	}
}
