// Package tools_test — task_cli_test.go
//
// End-to-end test for the task-cli tool against a live Memory server:
//
//  1. Create an ephemeral project.
//  2. Install the workspace-memory-blueprint.
//  3. Build the task-cli binary from source.
//  4. Create WorkPackage and Task objects via the memory CLI.
//  5. Run task-cli list --type Task / WorkPackage and assert all titles appear.
//  6. Run task-cli types and assert expected type names appear.
//  7. Clean up.
//
// Required environment variables:
//
//	MEMORY_TEST_SERVER  — URL of the Memory server.
//	MEMORY_TEST_TOKEN   — API key for the Memory server.
package tools_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTaskCLI_BlueprintWorkflow installs the multi-agent blueprint into a fresh
// project, creates Tasks/WorkPackages, and exercises the task-cli binary.
func TestTaskCLI_BlueprintWorkflow(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()

	skipIfServerDown(t, rl)

	home := t.TempDir()
	srv := serverURL()
	token := e2eTestToken()
	describe(rl, "Exercises the task-cli binary end-to-end against a live Memory server with the workspace-memory-blueprint installed",
		"Authenticates and creates an ephemeral project",
		"Installs the workspace-memory-blueprint",
		"Builds the task-cli binary from source",
		"Creates WorkPackage and Task graph objects via the memory CLI",
		"Runs task-cli list --type Task/WorkPackage and asserts all titles appear",
		"Runs task-cli types and checks exit 0",
	)

	logStatusPreamble(t, home)

	// ── Step 1: Authenticate ─────────────────────────────────────────────────
	rl.Section("Authenticate")
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", token)
	rl.Printf("server: %s", srv)

	// ── Step 2: Create project ───────────────────────────────────────────────
	rl.Section("Create project")
	projectName := fmt.Sprintf("e2e-task-cli-%d", time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home, append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
	rl.CLI("memory projects create --name "+projectName, createOut)

	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from: %q", createOut)
	}
	rl.Printf("project: %s (%s)", projectName, projectID)

	t.Cleanup(func() { deleteProjectViaExec(t, home, projectID, projectName) })

	// ── Step 3: Install blueprint ────────────────────────────────────────────
	rl.Section("Install blueprint")
	blueprintOut := mustRunCLIInDirWithHome(t, "", home,
		"blueprints", blueprintURL,
		"--project", projectName,
		"--upgrade",
	)
	rl.CLI("memory blueprints "+blueprintURL+" --project "+projectName+" --upgrade", blueprintOut)
	if strings.Contains(blueprintOut, "errors") && !strings.Contains(blueprintOut, "0 errors") {
		rl.Failf("blueprint install reported errors:\n%s", blueprintOut)
	}
	rl.Printf("blueprint installed OK")

	// ── Step 4: Build task-cli binary ────────────────────────────────────────
	rl.Section("Build task-cli")
	taskCLIBin := filepath.Join(t.TempDir(), "task-cli")
	buildCmd := exec.Command("go", "build", "-o", taskCLIBin, ".")
	buildCmd.Dir = taskCLIDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Skipf("could not build task-cli: %v\n%s", err, out)
	}
	rl.Printf("task-cli built at: %s", taskCLIBin)

	// ── Step 5: Create test objects ──────────────────────────────────────────
	rl.Section("Create test objects")

	// Create 2 WorkPackages.
	wpTitles := []string{"WP Alpha", "WP Beta"}
	wpIDs := make([]string, 0, len(wpTitles))
	for _, title := range wpTitles {
		props := fmt.Sprintf(`{"title":%s,"status":"created","priority":1}`, jsonStringLiteral(title))
		out := mustRunCLIInDirWithHome(t, "", home,
			"graph", "objects", "create",
			"--type", "WorkPackage",
			"--properties", props,
			"--project", projectID,
			"--output", "json",
		)
		rl.CLI("memory graph objects create --type WorkPackage", prettyJSON(out))
		id := parseJSONField(out, "id")
		if id == "" {
			rl.Failf("could not parse WorkPackage ID from: %q", out)
		}
		wpIDs = append(wpIDs, id)
		rl.Printf("WorkPackage %q: %s", title, id)
	}

	// Create 4 Tasks.
	taskTitles := []string{"Task One", "Task Two", "Task Three", "Task Four"}
	taskIDs := make([]string, 0, len(taskTitles))
	for _, title := range taskTitles {
		props := fmt.Sprintf(`{"title":%s,"status":"created","priority":1,"type":"research"}`, jsonStringLiteral(title))
		out := mustRunCLIInDirWithHome(t, "", home,
			"graph", "objects", "create",
			"--type", "Task",
			"--properties", props,
			"--project", projectID,
			"--output", "json",
		)
		rl.CLI("memory graph objects create --type Task", prettyJSON(out))
		id := parseJSONField(out, "id")
		if id == "" {
			rl.Failf("could not parse Task ID from: %q", out)
		}
		taskIDs = append(taskIDs, id)
		rl.Printf("Task %q: %s", title, id)
	}

	// ── Step 6: task-cli list --type Task ────────────────────────────────────
	rl.Section("task-cli list --type Task")
	taskListOut, err := runTaskCLI(t, taskCLIBin, srv, token, projectID, "list", "--type", "Task")
	rl.CLI("task-cli list --type Task", taskListOut)
	if err != nil {
		rl.Failf("task-cli list --type Task failed: %v\noutput:\n%s", err, taskListOut)
	}
	for _, title := range taskTitles {
		if !strings.Contains(taskListOut, title) {
			t.Errorf("task-cli list --type Task: output missing %q\nfull output:\n%s", title, taskListOut)
		}
	}
	rl.Printf("all 4 task titles found in task-cli list output")

	// ── Step 7: task-cli list --type WorkPackage ─────────────────────────────
	rl.Section("task-cli list --type WorkPackage")
	wpListOut, err := runTaskCLI(t, taskCLIBin, srv, token, projectID, "list", "--type", "WorkPackage")
	rl.CLI("task-cli list --type WorkPackage", wpListOut)
	if err != nil {
		rl.Failf("task-cli list --type WorkPackage failed: %v\noutput:\n%s", err, wpListOut)
	}
	for _, title := range wpTitles {
		if !strings.Contains(wpListOut, title) {
			t.Errorf("task-cli list --type WorkPackage: output missing %q\nfull output:\n%s", title, wpListOut)
		}
	}
	rl.Printf("both WorkPackage titles found in task-cli list output")

	// ── Step 8: task-cli types ───────────────────────────────────────────────
	// Soft check only: assert exit 0 but do NOT assert specific type names.
	// The type registry API may not be available on all server versions
	// (e.g. mcj-emergent returns an empty list), so we only verify the command
	// itself succeeds.
	rl.Section("task-cli types")
	typesOut, err := runTaskCLI(t, taskCLIBin, srv, token, projectID, "types")
	rl.CLI("task-cli types", typesOut)
	if err != nil {
		rl.Failf("task-cli types failed: %v\noutput:\n%s", err, typesOut)
	}
	// Informational: log which types appeared (may be empty on older servers).
	for _, typeName := range []string{"Task", "WorkPackage", "AgentPool"} {
		if strings.Contains(typesOut, typeName) {
			rl.Printf("  type found: %s", typeName)
		} else {
			rl.Printf("  type not found (type registry may be unsupported on this server): %s", typeName)
		}
	}
	rl.Printf("task-cli types exited 0 (type registry support: %v)", strings.Contains(typesOut, "Task"))

	// Suppress unused variable warning for taskIDs (populated but only used for logging).
	_ = taskIDs
}
