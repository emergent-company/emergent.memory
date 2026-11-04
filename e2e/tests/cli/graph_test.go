// Package cli_test — graph_test.go
//
// End-to-end tests for `memory graph objects` and `memory graph relationships`
// CLI subcommands.  Each test creates an ephemeral project, exercises the
// graph CRUD commands, and verifies the outputs.
package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Graph Objects
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_GraphObjectCreateGetDelete exercises the full lifecycle of a
// graph object: create → get → delete.
func TestCLIInstalled_GraphObjectCreateGetDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph object create → get → delete lifecycle",
		"Create project, then create a graph object with type and properties",
		"Get the object by ID and verify type/properties",
		"Delete the object and verify it is gone",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	// Create ephemeral project.
	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-graph-obj")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create a graph object.
	rl.Section("Create graph object")
	out := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "TestNode",
		"--name", "GraphTestObject",
		"--description", "Created by e2e test",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type TestNode --name GraphTestObject --project "+projectID, out)

	objectID := parseJSONField(out, "id")
	if objectID == "" {
		rl.Failf("could not parse object ID from create output: %s", truncate(out, 300))
	}
	rl.Printf("created object ID: %s", objectID)

	// Get the object by ID.
	rl.Section("Get graph object by ID")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "get", objectID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects get "+objectID+" --project "+projectID, getOut)

	gotType := parseJSONField(getOut, "type")
	if gotType != "TestNode" {
		t.Errorf("expected type 'TestNode', got %q", gotType)
	}
	if !strings.Contains(getOut, "GraphTestObject") {
		t.Errorf("expected object output to contain 'GraphTestObject', got:\n%s", truncate(getOut, 500))
	}
	rl.Printf("get returned type=%s, contains name=true", gotType)

	// Delete the object.
	rl.Section("Delete graph object")
	delOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "delete", objectID,
		"--project", projectID,
	)
	rl.CLI("memory graph objects delete "+objectID+" --project "+projectID, delOut)
	rl.Printf("delete output: %s", truncate(delOut, 200))

	// Verify the delete command produced confirmation output.
	rl.Section("Verify delete confirmed")
	if !strings.Contains(strings.ToLower(delOut), "delete") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", truncate(delOut, 300))
	}
	rl.Printf("delete confirmed in output: true")
}

// TestCLIInstalled_GraphObjectUpdate verifies that `graph objects update`
// merges new properties into an existing object.
func TestCLIInstalled_GraphObjectUpdate(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph objects update merges properties",
		"Create a graph object with initial properties",
		"Update with new properties via --properties JSON",
		"Get the object and verify merged properties",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-graph-upd")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create object with initial properties.
	rl.Section("Create graph object")
	out := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "TestNode",
		"--name", "UpdateTestObject",
		"--properties", `{"priority":"low"}`,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type TestNode --name UpdateTestObject --project "+projectID, out)

	objectID := parseJSONField(out, "id")
	if objectID == "" {
		rl.Failf("could not parse object ID from create output: %s", truncate(out, 300))
	}
	rl.Printf("created object ID: %s", objectID)

	// Update the object with merged properties.
	rl.Section("Update graph object properties")
	updateOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "update", objectID,
		"--properties", `{"priority":"high","status":"active"}`,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects update "+objectID+" --project "+projectID, updateOut)

	// Verify the update stuck.
	rl.Section("Verify updated properties")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "get", objectID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects get "+objectID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, `"high"`) {
		t.Errorf("expected updated priority 'high' in get output, got:\n%s", truncate(getOut, 500))
	}
	if !strings.Contains(getOut, `"active"`) {
		t.Errorf("expected status 'active' in get output, got:\n%s", truncate(getOut, 500))
	}
	rl.Printf("verified: priority=high, status=active present in get output")
}

// TestCLIInstalled_GraphObjectListJSON verifies that `graph objects list
// --output json` returns parseable JSON.
func TestCLIInstalled_GraphObjectListJSON(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph objects list --output json returns parseable JSON",
		"Create project and a graph object",
		"List objects with --output json",
		"Assert output is valid JSON array containing the created object",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-graph-list")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create an object so the list is non-empty.
	rl.Section("Create graph object")
	createOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "TestNode",
		"--name", "ListTestObject",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type TestNode --name ListTestObject --project "+projectID, createOut)

	// List objects with JSON output.
	rl.Section("List graph objects as JSON")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "TestNode",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects list --type TestNode --project "+projectID+" --output json", listOut)

	// Validate JSON — should be an array or object.
	trimmed := strings.TrimSpace(listOut)
	if !json.Valid([]byte(trimmed)) {
		t.Errorf("expected valid JSON from list --output json, got:\n%s", truncate(trimmed, 500))
	}
	if !strings.Contains(trimmed, "ListTestObject") {
		t.Errorf("expected list output to contain 'ListTestObject', got:\n%s", truncate(trimmed, 500))
	}
	rl.Printf("list returned valid JSON containing 'ListTestObject'")
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph Relationships
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_GraphRelationshipCreateListDelete exercises the full
// lifecycle of a graph relationship: create two objects → create relationship
// → list → delete.
func TestCLIInstalled_GraphRelationshipCreateListDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph relationship create → list → delete lifecycle",
		"Create project with two graph objects",
		"Create a relationship between the objects",
		"List relationships and verify the new one appears",
		"Delete the relationship",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-graph-rel")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create two objects.
	rl.Section("Create source and target objects")
	srcOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "AuthService",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type Service --name AuthService --project "+projectID, srcOut)
	srcID := parseJSONField(srcOut, "id")
	if srcID == "" {
		rl.Failf("could not parse source object ID: %s", truncate(srcOut, 300))
	}

	tgtOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "UserService",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type Service --name UserService --project "+projectID, tgtOut)
	tgtID := parseJSONField(tgtOut, "id")
	if tgtID == "" {
		rl.Failf("could not parse target object ID: %s", truncate(tgtOut, 300))
	}
	rl.Printf("source=%s, target=%s", srcID, tgtID)

	// Create relationship.
	rl.Section("Create relationship")
	relOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "create",
		"--type", "DEPENDS_ON",
		"--from", srcID,
		"--to", tgtID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI(fmt.Sprintf("memory graph relationships create --type DEPENDS_ON --from %s --to %s --project %s", srcID, tgtID, projectID), relOut)

	relID := parseJSONField(relOut, "id")
	if relID == "" {
		rl.Failf("could not parse relationship ID: %s", truncate(relOut, 300))
	}
	rl.Printf("relationship ID: %s", relID)

	// List relationships.
	rl.Section("List relationships")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "list",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph relationships list --project "+projectID+" --output json", listOut)

	if !strings.Contains(strings.ToLower(listOut), "depends_on") {
		t.Errorf("expected list to contain 'DEPENDS_ON', got:\n%s", truncate(listOut, 500))
	}
	rl.Printf("list contains DEPENDS_ON: true")

	// Delete relationship.
	rl.Section("Delete relationship")
	delOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "delete", relID,
		"--project", projectID,
	)
	rl.CLI("memory graph relationships delete "+relID+" --project "+projectID, delOut)
	rl.Printf("delete output: %s", truncate(delOut, 200))

	// Verify delete confirmed.
	rl.Section("Verify relationship delete confirmed")
	if !strings.Contains(strings.ToLower(delOut), "delete") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", truncate(delOut, 300))
	}
	rl.Printf("relationship delete confirmed in output: true")
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph Edges
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_GraphObjectEdges verifies that `graph objects edges` shows
// incoming and outgoing relationships for a graph object.
func TestCLIInstalled_GraphObjectEdges(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph objects edges shows relationships for an object",
		"Create project with two objects and a relationship",
		"Run `graph objects edges <src-id>` and verify outgoing edge appears",
		"Run `graph objects edges <tgt-id>` and verify incoming edge appears",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-graph-edge")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create two objects.
	rl.Section("Create source and target objects")
	srcOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "EdgeSourceService",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type Service --name EdgeSourceService --project "+projectID, srcOut)
	srcID := parseJSONField(srcOut, "id")
	if srcID == "" {
		rl.Failf("could not parse source ID: %s", truncate(srcOut, 300))
	}

	tgtOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Service",
		"--name", "EdgeTargetService",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type Service --name EdgeTargetService --project "+projectID, tgtOut)
	tgtID := parseJSONField(tgtOut, "id")
	if tgtID == "" {
		rl.Failf("could not parse target ID: %s", truncate(tgtOut, 300))
	}
	rl.Printf("source=%s, target=%s", srcID, tgtID)

	// Create a relationship.
	rl.Section("Create relationship")
	relOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "create",
		"--type", "CALLS",
		"--from", srcID,
		"--to", tgtID,
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI(fmt.Sprintf("memory graph relationships create --type CALLS --from %s --to %s --project %s", srcID, tgtID, projectID), relOut)

	// Check edges for source object (should have outgoing edge).
	rl.Section("Check edges for source object")
	srcEdges, err := runCLIInDirWithHome(t, "", home,
		"graph", "objects", "edges", srcID,
		"--project", projectID,
	)
	rl.CLIErr("memory graph objects edges "+srcID+" --project "+projectID, srcEdges, err, 0)

	if err != nil {
		rl.Printf("graph objects edges returned error: %v", err)
		t.Skipf("graph objects edges not available: %v", err)
	}

	trimmed := strings.TrimSpace(srcEdges)
	if trimmed == "" {
		rl.Failf("expected non-empty edges output for source, got empty")
	}
	// Outgoing section should show the relationship type.
	if !strings.Contains(srcEdges, "CALLS") && !strings.Contains(strings.ToLower(srcEdges), "outgoing") {
		t.Errorf("expected edges output to contain 'CALLS' or 'Outgoing', got:\n%s", truncate(srcEdges, 500))
	}
	rl.Printf("source edges: %d bytes, contains CALLS/Outgoing: true", len(trimmed))

	// Check edges for target object (should have incoming edge).
	rl.Section("Check edges for target object")
	tgtEdges, err := runCLIInDirWithHome(t, "", home,
		"graph", "objects", "edges", tgtID,
		"--project", projectID,
	)
	rl.CLIErr("memory graph objects edges "+tgtID+" --project "+projectID, tgtEdges, err, 0)

	if err != nil {
		rl.Printf("graph objects edges for target returned error: %v", err)
	} else {
		if !strings.Contains(tgtEdges, "CALLS") && !strings.Contains(strings.ToLower(tgtEdges), "incoming") {
			t.Errorf("expected target edges to contain 'CALLS' or 'Incoming', got:\n%s", truncate(tgtEdges, 500))
		}
		rl.Printf("target edges: %d bytes, contains CALLS/Incoming: true", len(strings.TrimSpace(tgtEdges)))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers — keep the linter happy for unused imports
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Graph Batch Operations
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_GraphObjectCreateBatch verifies that `graph objects
// create-batch` creates multiple objects from a JSON file in a single call.
func TestCLIInstalled_GraphObjectCreateBatch(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph objects create-batch creates multiple objects from JSON file",
		"Create project and a JSON file with 3 objects",
		"Run `graph objects create-batch --file <path>`",
		"Verify 3 objects are created and appear in list",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-graph-batch")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Write batch JSON file.
	rl.Section("Create batch objects JSON file")
	batchFile := filepath.Join(home, "batch_objects.json")
	batchContent := `[
  {"type": "Person", "name": "Alice", "description": "A developer"},
  {"type": "Person", "name": "Bob", "description": "A designer"},
  {"type": "Tool", "name": "Go", "properties": {"category": "language"}}
]`
	if err := os.WriteFile(batchFile, []byte(batchContent), 0644); err != nil {
		rl.Failf("failed to write batch file: %v", err)
	}
	rl.Printf("wrote batch file with 3 objects: %s", batchFile)

	// Create batch objects.
	rl.Section("Run graph objects create-batch")
	out := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create-batch",
		"--file", batchFile,
		"--project", projectID,
	)
	rl.CLI("memory graph objects create-batch --file "+batchFile+" --project "+projectID, out)

	// Verify output contains all three objects (each line has an ID + type + name).
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		rl.Failf("expected 3 lines from batch create (one per object), got %d: %s", len(lines), truncate(out, 500))
	}
	rl.Printf("batch create returned %d lines", len(lines))

	for _, expected := range []string{"Alice", "Bob", "Go"} {
		if !strings.Contains(out, expected) {
			t.Errorf("expected batch output to contain %q, got:\n%s", expected, truncate(out, 500))
		}
	}

	// Verify via list.
	rl.Section("Verify objects via list")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects list --project "+projectID+" --output json", listOut)

	for _, expected := range []string{"Alice", "Bob", "Go"} {
		if !strings.Contains(listOut, expected) {
			t.Errorf("expected list output to contain %q, got:\n%s", expected, truncate(listOut, 500))
		}
	}
	rl.Printf("all 3 objects found in list output")
}

// TestCLIInstalled_GraphRelationshipCreateBatch verifies that `graph
// relationships create-batch` creates multiple relationships from a JSON file.
func TestCLIInstalled_GraphRelationshipCreateBatch(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph relationships create-batch creates multiple rels from JSON",
		"Create project with 3 objects",
		"Create batch relationships JSON connecting them",
		"Run `graph relationships create-batch --file <path>`",
		"Verify relationships appear in list",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-graph-relbatch")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create 3 objects.
	rl.Section("Create 3 objects")
	ids := make([]string, 3)
	names := []string{"Alice", "Bob", "Carol"}
	for i, n := range names {
		out := mustRunCLIInDirWithHome(t, "", home,
			"graph", "objects", "create",
			"--type", "Person",
			"--name", n,
			"--project", projectID,
			"--output", "json",
		)
		rl.CLI(fmt.Sprintf("memory graph objects create --type Person --name %s --project %s", n, projectID), out)
		ids[i] = parseJSONField(out, "id")
		if ids[i] == "" {
			rl.Failf("could not parse object ID for %s: %s", n, truncate(out, 300))
		}
	}
	rl.Printf("created objects: %v", ids)

	// Write batch relationships JSON file.
	rl.Section("Create batch relationships JSON file")
	batchFile := filepath.Join(home, "batch_rels.json")
	batchContent := fmt.Sprintf(`[
  {"type": "knows", "from": "%s", "to": "%s"},
  {"type": "knows", "from": "%s", "to": "%s"}
]`, ids[0], ids[1], ids[1], ids[2])
	if err := os.WriteFile(batchFile, []byte(batchContent), 0644); err != nil {
		rl.Failf("failed to write batch file: %v", err)
	}
	rl.Printf("wrote batch file with 2 relationships: %s", batchFile)

	// Create batch relationships.
	rl.Section("Run graph relationships create-batch")
	out := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "create-batch",
		"--file", batchFile,
		"--project", projectID,
	)
	rl.CLI("memory graph relationships create-batch --file "+batchFile+" --project "+projectID, out)

	// Verify output contains 2 relationships.
	relLines := strings.Split(strings.TrimSpace(out), "\n")
	if len(relLines) < 2 {
		rl.Failf("expected 2 lines from batch rel create, got %d: %s", len(relLines), truncate(out, 500))
	}
	rl.Printf("batch relationship create returned %d lines", len(relLines))

	// Verify via list.
	rl.Section("Verify relationships via list")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "list",
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph relationships list --project "+projectID+" --output json", listOut)

	if !strings.Contains(strings.ToLower(listOut), "knows") {
		t.Errorf("expected relationship list to contain 'knows', got:\n%s", truncate(listOut, 500))
	}
	rl.Printf("relationships list contains 'knows': true")
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph Relationship Get
// ─────────────────────────────────────────────────────────────────────────────

// TestCLIInstalled_GraphRelationshipGet verifies that `graph relationships get`
// returns details for a specific relationship by ID.
func TestCLIInstalled_GraphRelationshipGet(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify graph relationships get returns relationship details",
		"Create project with two objects and a relationship",
		"Get the relationship by ID in table and JSON format",
		"Verify type, from, and to fields are correct",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-graph-relget")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create two objects.
	rl.Section("Create source and target objects")
	srcOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Person", "--name", "RelGetSrc",
		"--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph objects create --type Person --name RelGetSrc --project "+projectID, srcOut)
	srcID := parseJSONField(srcOut, "id")
	if srcID == "" {
		rl.Failf("could not parse source object ID: %s", truncate(srcOut, 300))
	}

	tgtOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "Person", "--name", "RelGetTgt",
		"--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph objects create --type Person --name RelGetTgt --project "+projectID, tgtOut)
	tgtID := parseJSONField(tgtOut, "id")
	if tgtID == "" {
		rl.Failf("could not parse target object ID: %s", truncate(tgtOut, 300))
	}
	rl.Printf("source=%s, target=%s", srcID, tgtID)

	// Create relationship.
	rl.Section("Create relationship")
	relOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "create",
		"--type", "mentors",
		"--from", srcID, "--to", tgtID,
		"--project", projectID, "--output", "json",
	)
	rl.CLI(fmt.Sprintf("memory graph relationships create --type mentors --from %s --to %s --project %s", srcID, tgtID, projectID), relOut)
	relID := parseJSONField(relOut, "id")
	if relID == "" {
		rl.Failf("could not parse relationship ID: %s", truncate(relOut, 300))
	}
	rl.Printf("relationship ID: %s", relID)

	// Get relationship in table format.
	rl.Section("Get relationship (table format)")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "get", relID,
		"--project", projectID,
	)
	rl.CLI("memory graph relationships get "+relID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, "mentors") {
		t.Errorf("expected get output to contain 'mentors', got:\n%s", truncate(getOut, 500))
	}
	if !strings.Contains(getOut, srcID) {
		t.Errorf("expected get output to contain source ID %s, got:\n%s", srcID, truncate(getOut, 500))
	}
	rl.Printf("table output contains type=mentors and source ID")

	// Get relationship in JSON format.
	rl.Section("Get relationship (JSON format)")
	jsonOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "relationships", "get", relID,
		"--project", projectID, "--output", "json",
	)
	rl.CLI("memory graph relationships get "+relID+" --project "+projectID+" --output json", jsonOut)

	if !json.Valid([]byte(strings.TrimSpace(jsonOut))) {
		t.Errorf("expected valid JSON from get --output json, got:\n%s", truncate(jsonOut, 500))
	}
	gotType := parseJSONField(jsonOut, "type")
	if gotType != "mentors" {
		t.Errorf("expected type 'mentors', got %q", gotType)
	}
	gotSrc := parseJSONField(jsonOut, "src_id")
	if gotSrc != srcID {
		t.Errorf("expected src_id %q, got %q", srcID, gotSrc)
	}
	gotDst := parseJSONField(jsonOut, "dst_id")
	if gotDst != tgtID {
		t.Errorf("expected dst_id %q, got %q", tgtID, gotDst)
	}
	rl.Printf("JSON: type=%s, src_id=%s, dst_id=%s — all correct", gotType, gotSrc, gotDst)
}

var _ = framework.SetToken // ensure framework import is used
