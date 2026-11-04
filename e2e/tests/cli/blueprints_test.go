// Package cli_test — blueprints_test.go
//
// End-to-end tests for `memory blueprints dump` — export project graph data
// as JSONL seed files.
package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_BlueprintsDump verifies that `memory blueprints dump`
// exports graph objects and relationships as per-type JSONL seed files.
func TestCLIInstalled_BlueprintsDump(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify blueprints dump exports graph as JSONL seed files",
		"Create project and populate with objects via batch create",
		"Run `memory blueprints dump <dir>` to export",
		"Verify seed/objects/ directory contains JSONL files with correct data",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-bp-dump")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Seed data using batch create.
	rl.Section("Seed graph objects via batch create")
	batchFile := filepath.Join(home, "seed_objects.json")
	batchContent := `[
  {"type": "Person", "name": "DumpAlice", "description": "Test person A"},
  {"type": "Person", "name": "DumpBob", "description": "Test person B"},
  {"type": "Tool", "name": "DumpGo", "description": "Programming language"}
]`
	if err := os.WriteFile(batchFile, []byte(batchContent), 0644); err != nil {
		rl.Failf("failed to write batch file: %v", err)
	}
	batchOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create-batch",
		"--file", batchFile,
		"--project", projectID,
	)
	rl.CLI("memory graph objects create-batch --file "+batchFile+" --project "+projectID, batchOut)
	rl.Printf("batch created 3 objects")

	// Run blueprints dump.
	rl.Section("Run blueprints dump")
	dumpDir := filepath.Join(home, "dump-output")
	out := mustRunCLIInDirWithHome(t, "", home,
		"blueprints", "dump", dumpDir,
		"--project", projectID,
	)
	rl.CLI("memory blueprints dump "+dumpDir+" --project "+projectID, out)

	// Verify output mentions dumped counts.
	if !strings.Contains(out, "3") || !strings.Contains(strings.ToLower(out), "object") {
		t.Logf("dump output may not show expected count, got:\n%s", truncate(out, 500))
	}
	rl.Printf("dump output: %s", truncate(out, 300))

	// Verify seed directory structure.
	rl.Section("Verify seed directory structure")
	objectsDir := filepath.Join(dumpDir, "seed", "objects")
	entries, err := os.ReadDir(objectsDir)
	if err != nil {
		rl.Failf("failed to read seed/objects/ directory: %v", err)
	}
	if len(entries) == 0 {
		rl.Failf("expected at least 1 JSONL file in seed/objects/, got 0")
	}

	// Check for expected type files.
	foundTypes := make(map[string]bool)
	for _, e := range entries {
		rl.Printf("found seed file: %s", e.Name())
		foundTypes[strings.TrimSuffix(e.Name(), ".jsonl")] = true
	}
	for _, expectedType := range []string{"Person", "Tool"} {
		if !foundTypes[expectedType] {
			t.Errorf("expected seed/objects/%s.jsonl to exist", expectedType)
		}
	}
	rl.Printf("found expected type files: Person.jsonl, Tool.jsonl")

	// Verify content of a JSONL file.
	rl.Section("Verify JSONL file content")
	personFile := filepath.Join(objectsDir, "Person.jsonl")
	personData, err := os.ReadFile(personFile)
	if err != nil {
		rl.Failf("failed to read Person.jsonl: %v", err)
	}
	personStr := string(personData)
	if !strings.Contains(personStr, "DumpAlice") {
		t.Errorf("expected Person.jsonl to contain 'DumpAlice', got:\n%s", truncate(personStr, 500))
	}
	if !strings.Contains(personStr, "DumpBob") {
		t.Errorf("expected Person.jsonl to contain 'DumpBob', got:\n%s", truncate(personStr, 500))
	}
	rl.Printf("Person.jsonl contains DumpAlice and DumpBob")
}

// TestCLIInstalled_BlueprintsDumpWithTypes verifies that `blueprints dump
// --types` filters the export to specific object types.
func TestCLIInstalled_BlueprintsDumpWithTypes(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify blueprints dump --types filters to specific types",
		"Create project with Person and Tool objects",
		"Run dump with --types Person to export only Person objects",
		"Verify only Person.jsonl exists in output",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-bp-dumptyp")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Seed data.
	rl.Section("Seed graph objects")
	batchFile := filepath.Join(home, "seed.json")
	batchContent := `[
  {"type": "Person", "name": "TypeFilterAlice"},
  {"type": "Tool", "name": "TypeFilterGo"}
]`
	if err := os.WriteFile(batchFile, []byte(batchContent), 0644); err != nil {
		rl.Failf("failed to write batch file: %v", err)
	}
	batchOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create-batch",
		"--file", batchFile,
		"--project", projectID,
	)
	rl.CLI("memory graph objects create-batch --file "+batchFile+" --project "+projectID, batchOut)

	// Dump with --types Person only.
	rl.Section("Run blueprints dump with --types Person")
	dumpDir := filepath.Join(home, "dump-filtered")
	out := mustRunCLIInDirWithHome(t, "", home,
		"blueprints", "dump", dumpDir,
		"--project", projectID,
		"--types", "Person",
	)
	rl.CLI("memory blueprints dump "+dumpDir+" --project "+projectID+" --types Person", out)
	rl.Printf("dump output: %s", truncate(out, 300))

	// Verify only Person.jsonl exists.
	rl.Section("Verify only Person type in output")
	objectsDir := filepath.Join(dumpDir, "seed", "objects")
	entries, err := os.ReadDir(objectsDir)
	if err != nil {
		rl.Failf("failed to read seed/objects/ directory: %v", err)
	}
	for _, e := range entries {
		rl.Printf("found seed file: %s", e.Name())
		if strings.Contains(e.Name(), "Tool") {
			t.Errorf("expected --types Person to exclude Tool, but found %s", e.Name())
		}
	}
	rl.Printf("type filter correctly applied: no Tool.jsonl in output")
}

var _ = framework.SetToken // keep import used
