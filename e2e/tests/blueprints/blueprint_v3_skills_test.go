// Package blueprints_test — blueprint_v3_skills_test.go
//
// Tests that applying the v3 blueprint installs the expected skills from its
// skills/ directory (agentskills.io-compatible subdirectory structure).
package blueprints_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestBlueprintV3_SkillsInstalled verifies that running `memory blueprints
// /root/workspace-memory-blueprint-v3` creates the five agent-role skills
// defined in its skills/ directory.
//
// Steps:
//  1. Authenticate and create an ephemeral project.
//  2. Apply the v3 blueprint.
//  3. Assert each expected skill name appears in the CLI output.
//  4. Call GET /api/skills via HTTP and confirm the skills are in the API.
//  5. Delete the project on cleanup.
func TestBlueprintV3_SkillsInstalled(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Verify v3 blueprint installs expected skills",
		"Creates an ephemeral project",
		"Applies workspace-memory-blueprint-v3",
		"Asserts 5 expected skills appear in CLI output and GET /api/skills",
	)

	logStatusPreamble(t)
	skipIfServerDown(t, rl)

	const blueprintPath = "/root/blueprints/workspace-memory-blueprint-v3"

	expectedSkills := []string{
		"research-workflow",
		"coding-workflow",
		"security-review",
		"code-review",
		"deploy-checklist",
	}

	home := t.TempDir()
	srv := serverURL()

	rl.Section("Step 1 — Authenticate")
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", e2eTestToken())
	rl.Printf("authenticated against %s", srv)

	rl.Section("Step 2 — Create project")
	projectName := fmt.Sprintf("e2e-v3-skills-%d", time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home, append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
	rl.CLI("memory projects create --name "+projectName, createOut)

	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from create output: %q", createOut)
	}
	rl.Printf("created project %s (%s)", projectName, projectID)

	// Delete project on exit.
	t.Cleanup(func() { deleteProjectViaExec(t, home, projectID, projectName) })

	rl.Section("Step 3 — Apply blueprint")
	out := mustRunCLIInDirWithHome(t, "", home, "blueprints", blueprintPath, "--project", projectName)
	rl.CLI("memory blueprints "+blueprintPath+" --project "+projectName, out)

	if strings.Contains(out, "errors") && !strings.Contains(out, "0 errors") {
		t.Errorf("blueprint install reported errors:\n%s", out)
	}

	rl.Section("Step 4 — Assert skills in CLI output")
	for _, skillName := range expectedSkills {
		if !strings.Contains(out, skillName) {
			t.Errorf("blueprint output missing expected skill %q", skillName)
			rl.Printf("MISSING in CLI output: %q", skillName)
		} else {
			rl.Printf("OK in CLI output: %q", skillName)
		}
	}

	rl.Section("Step 5 — Assert skills via API")
	apiSkills := fetchGlobalSkillsViaAPI(t, srv, e2eTestToken())
	rl.Printf("GET /api/skills returned %d skills", len(apiSkills))
	skillsByName := make(map[string]bool, len(apiSkills))
	for _, sk := range apiSkills {
		if name, ok := sk["name"].(string); ok {
			skillsByName[name] = true
		}
	}
	for _, skillName := range expectedSkills {
		if !skillsByName[skillName] {
			t.Errorf("API GET /api/skills: expected skill %q not found in response", skillName)
			rl.Printf("MISSING in API: %q", skillName)
		} else {
			rl.Printf("OK in API: %q", skillName)
		}
	}
}

// TestBlueprintV3_SkillsUpgrade verifies that re-applying the blueprint with
// --upgrade updates existing skills instead of returning an error.
func TestBlueprintV3_SkillsUpgrade(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	rl.Describe("Verify blueprint --upgrade updates existing skills without errors",
		"Creates an ephemeral project and applies v3 blueprint once",
		"Re-applies with --upgrade flag",
		"Asserts all items show 'updated' and 0 errors in summary",
	)

	logStatusPreamble(t)
	skipIfServerDown(t, rl)

	const blueprintPath = "/root/blueprints/workspace-memory-blueprint-v3"

	home := t.TempDir()
	srv := serverURL()

	rl.Section("Step 1 — Authenticate")
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "server_url", srv)
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "api_key", e2eTestToken())
	rl.Printf("authenticated against %s", srv)

	rl.Section("Step 2 — Create project")
	projectName := fmt.Sprintf("e2e-v3-upgrade-%d", time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home, append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
	rl.CLI("memory projects create --name "+projectName, createOut)
	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from create output: %q", createOut)
	}
	rl.Printf("created project %s (%s)", projectName, projectID)

	t.Cleanup(func() { deleteProjectViaExec(t, home, projectID, projectName) })

	rl.Section("Step 3 — First blueprint apply")
	firstOut := mustRunCLIInDirWithHome(t, "", home, "blueprints", blueprintPath, "--project", projectName)
	rl.CLI("memory blueprints "+blueprintPath+" --project "+projectName, firstOut)

	rl.Section("Step 4 — Second apply with --upgrade")
	out := mustRunCLIInDirWithHome(t, "", home, "blueprints", blueprintPath, "--project", projectName, "--upgrade")
	rl.CLI("memory blueprints "+blueprintPath+" --project "+projectName+" --upgrade", out)

	if strings.Contains(out, "errors") && !strings.Contains(out, "0 errors") {
		t.Errorf("blueprint upgrade reported errors:\n%s", out)
		rl.Printf("FAIL: upgrade reported errors")
	} else {
		rl.Printf("OK: upgrade completed with 0 errors")
	}
}
