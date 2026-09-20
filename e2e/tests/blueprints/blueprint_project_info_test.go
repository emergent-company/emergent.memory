// Package blueprints_test — blueprint_project_info_test.go
//
// Tests that applying a blueprint with a project.yaml also sets the project's
// project_info field, making it available to the get_project_info agent tool.
package blueprints_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestBlueprintProjectInfo_Install verifies that when a blueprint directory
// contains a project.yaml file, the `memory blueprints` command updates the
// project's project_info field on the server.
//
// Steps:
//  1. Authenticate against the test server.
//  2. Create a dedicated ephemeral project.
//  3. Run `memory blueprints /root/files-memory-blueprint --project <name>`.
//  4. Fetch the project via GET /api/projects/<id> and assert project_info is
//     non-empty and contains expected content from the blueprint's project.yaml.
//  5. Delete the project on cleanup.
func TestBlueprintProjectInfo_Install(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Verify blueprint with project.yaml sets the project_info field",
		"Creates an ephemeral project",
		"Applies files-memory-blueprint which contains a project.yaml",
		"Asserts GET /api/projects/:id returns non-empty project_info with expected content",
	)

	logStatusPreamble(t)
	skipIfServerDown(t, rl)

	const blueprintPath = "/root/blueprints/files-memory-blueprint"

	home := t.TempDir()
	srv := serverURL()
	token := e2eTestToken()

	rl.Section("Step 1 — Authenticate")
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", token)
	rl.Printf("authenticated against %s", srv)

	rl.Section("Step 2 — Create project")
	projectName := fmt.Sprintf("e2e-proj-info-%d", time.Now().UnixMilli())
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
	out := mustRunCLIInDirWithHome(t, "", home, "blueprints", blueprintPath, "--project", projectName)
	rl.CLI("memory blueprints "+blueprintPath+" --project "+projectName, out)

	if strings.Contains(out, "errors") && !strings.Contains(out, "0 errors") {
		t.Errorf("blueprint install reported errors:\n%s", out)
	}

	rl.Section("Step 4 — Assert project_info via API")
	resp := doJSON(t, "GET", srv+"/api/projects/"+projectID, token, "", nil)
	body := readBody(t, resp)
	rl.Printf("GET /api/projects/%s → HTTP %d", projectID, resp.StatusCode)
	rl.Event("api_response", "GET /api/projects/"+projectID, map[string]any{"status": resp.StatusCode, "body": body})

	if resp.StatusCode != 200 {
		rl.Failf("GET /api/projects/%s returned %d", projectID, resp.StatusCode)
	}

	projectInfo := parseJSONField(body, "project_info")
	if projectInfo == "" {
		rl.Failf("project_info is empty after blueprint install; full response:\n%s", body)
	}
	rl.Printf("project_info set (%d chars)", len(projectInfo))

	expectedSubstrings := []string{
		"Files Memory Blueprint",
		"unified file registry",
	}
	for _, want := range expectedSubstrings {
		if !strings.Contains(projectInfo, want) {
			t.Errorf("project_info missing expected text %q\ngot: %s", want, projectInfo)
			rl.Printf("MISSING: %q", want)
		} else {
			rl.Printf("OK: project_info contains %q", want)
		}
	}
}
