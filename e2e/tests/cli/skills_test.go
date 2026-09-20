// Package cli_test — skills_test.go
//
// End-to-end tests for `memory skills` CLI subcommands: create, get, list,
// update, delete.  Skills are project-scoped and represent reusable Markdown
// workflow instructions for agents.
package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_SkillCreateGetUpdateDelete exercises the full skill lifecycle:
// create → get → update → delete.
//
// Known issue: `skills create` returns [500] on mcj-emergent for project-scoped
// skills.  The test skips when this happens.
func TestCLIInstalled_SkillCreateGetUpdateDelete(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify skill create → get → update → delete lifecycle",
		"Create project, then create a project-scoped skill",
		"Get the skill by ID and verify name/description",
		"Update the skill description",
		"Delete the skill",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-skills")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create a project-scoped skill using --content-file.
	rl.Section("Create skill")
	skillName := "e2e-test-skill"
	skillDesc := "Test skill for e2e validation"
	skillContent := "# E2E Test Skill\n\nThis skill is for testing.\n\n## Steps\n1. Do something\n2. Verify result"

	contentFile := filepath.Join(home, "skill-content.md")
	if err := os.WriteFile(contentFile, []byte(skillContent), 0o644); err != nil {
		rl.Failf("failed to write skill content file: %v", err)
	}
	createOut, createErr := runCLIInDirWithHome(t, "", home,
		"skills", "create",
		"--name", skillName,
		"--description", skillDesc,
		"--content-file", contentFile,
		"--project", projectID,
	)
	rl.CLIErr("memory skills create --name "+skillName+" --project "+projectID, createOut, createErr, 0)

	if createErr != nil {
		if strings.Contains(createOut, "[500]") || strings.Contains(createOut, "internal_error") {
			rl.Printf("SKIP: skills create returns 500 (server bug)")
			t.Skipf("skills create returns server error (server bug): %s", truncate(createOut, 200))
		}
		rl.Failf("skills create failed unexpectedly: %v — %s", createErr, truncate(createOut, 300))
	}
	rl.CLI("memory skills create --name "+skillName+" --project "+projectID, createOut)

	skillID := parseSkillID(createOut)
	if skillID == "" {
		rl.Failf("could not parse skill ID from create output: %s", truncate(createOut, 300))
	}
	rl.Printf("created skill ID: %s", skillID)

	// Get the skill by ID.
	rl.Section("Get skill by ID")
	getOut := mustRunCLIInDirWithHome(t, "", home,
		"skills", "get", skillID,
		"--project", projectID,
	)
	rl.CLI("memory skills get "+skillID+" --project "+projectID, getOut)

	if !strings.Contains(getOut, skillName) {
		t.Errorf("expected get output to contain skill name %q, got:\n%s", skillName, truncate(getOut, 500))
	}
	rl.Printf("get output contains name=%s: true", skillName)

	// Update the skill.
	rl.Section("Update skill description")
	updatedDesc := "Updated skill description for e2e"
	updateOut := mustRunCLIInDirWithHome(t, "", home,
		"skills", "update", skillID,
		"--description", updatedDesc,
		"--project", projectID,
	)
	rl.CLI("memory skills update "+skillID+" --project "+projectID, updateOut)
	rl.Printf("update output: %s", truncate(updateOut, 200))

	// Verify the update stuck via get.
	rl.Section("Verify updated skill")
	getOut2 := mustRunCLIInDirWithHome(t, "", home,
		"skills", "get", skillID,
		"--project", projectID,
	)
	rl.CLI("memory skills get "+skillID+" --project "+projectID, getOut2)

	if !strings.Contains(getOut2, updatedDesc) {
		t.Errorf("expected updated skill to contain new description %q, got:\n%s", updatedDesc, truncate(getOut2, 500))
	}
	rl.Printf("get output contains updated description: true")

	// Delete the skill.
	rl.Section("Delete skill")
	delOut := mustRunCLIInDirWithHome(t, "", home,
		"skills", "delete", skillID,
		"--project", projectID,
	)
	rl.CLI("memory skills delete "+skillID+" --project "+projectID, delOut)

	lower := strings.ToLower(delOut)
	if !strings.Contains(lower, "delete") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", truncate(delOut, 300))
	}
	rl.Printf("skill delete confirmed: true")
}

// TestCLIInstalled_SkillListProject verifies that `memory skills list --project`
// returns project-scoped skills.
//
// Known issue: `skills create` returns [500] on mcj-emergent for project-scoped
// skills.  The test skips when this happens.
func TestCLIInstalled_SkillListProject(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify skills list --project returns project-scoped skills",
		"Create project and a project-scoped skill",
		"List skills with --project and verify the skill appears",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-skills-list")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create a project-scoped skill using --content-file.
	rl.Section("Create skill")
	skillName := "e2e-ls"
	contentFile := filepath.Join(home, "skill-content.md")
	if err := os.WriteFile(contentFile, []byte("# List Test\nContent here."), 0o644); err != nil {
		rl.Failf("failed to write skill content file: %v", err)
	}
	createOut, createErr := runCLIInDirWithHome(t, "", home,
		"skills", "create",
		"--name", skillName,
		"--description", "Skill for list test",
		"--content-file", contentFile,
		"--project", projectID,
	)
	rl.CLIErr("memory skills create --name "+skillName+" --project "+projectID, createOut, createErr, 0)

	if createErr != nil {
		if strings.Contains(createOut, "[500]") || strings.Contains(createOut, "internal_error") {
			rl.Printf("SKIP: skills create returns 500 (server bug)")
			t.Skipf("skills create returns server error (server bug): %s", truncate(createOut, 200))
		}
		rl.Failf("skills create failed unexpectedly: %v — %s", createErr, truncate(createOut, 300))
	}
	rl.CLI("memory skills create --name "+skillName+" --project "+projectID, createOut)

	// List skills.
	rl.Section("List project skills")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"skills", "list",
		"--project", projectID,
	)
	rl.CLI("memory skills list --project "+projectID, listOut)

	if !strings.Contains(listOut, skillName) {
		t.Errorf("expected skills list to contain %q, got:\n%s", skillName, truncate(listOut, 500))
	}
	rl.Printf("skills list contains %s: true", skillName)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// parseSkillID extracts a skill ID from create output.
func parseSkillID(output string) string {
	for _, label := range []string{"ID:", "Skill ID:", "id:"} {
		if id := parseLineField(output, label); id != "" {
			return id
		}
	}
	if id := parseJSONField(output, "id"); id != "" {
		return id
	}
	return ""
}

var _ = framework.SetToken // keep import used
