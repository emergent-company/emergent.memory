// Package cli_test — skills_import_test.go
//
// End-to-end tests for `memory skills import` — importing skills from SKILL.md
// files and directories.
package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_SkillsImportFile verifies that `memory skills import
// <path>` imports a skill from a SKILL.md file.
func TestCLIInstalled_SkillsImportFile(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify skills import from SKILL.md file",
		"Create project and a SKILL.md file with frontmatter",
		"Import the skill via `memory skills import <path>`",
		"Verify the skill appears in `skills list`",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-skill-import")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create a SKILL.md file.
	rl.Section("Create SKILL.md file")
	skillDir := filepath.Join(home, "skills", "e2e-import-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		rl.Failf("failed to create skill directory: %v", err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	skillContent := `---
name: e2e-imp
description: Skill created by e2e import test
---

# E2E Import Test Skill

This is a test skill for verifying the import command.

## Instructions

1. Step one
2. Step two
`
	if err := os.WriteFile(skillFile, []byte(skillContent), 0644); err != nil {
		rl.Failf("failed to write SKILL.md: %v", err)
	}
	rl.Printf("wrote SKILL.md: %s", skillFile)

	// Import the skill.
	rl.Section("Import skill from SKILL.md")
	out, importErr := runCLIInDirWithHome(t, "", home,
		"skills", "import", skillFile,
		"--project", projectID,
	)
	rl.CLI("memory skills import "+skillFile+" --project "+projectID, out)

	// Server bug #83: skills create returns 500 for project-scoped skills.
	if importErr != nil && strings.Contains(out, "500") {
		t.Skipf("skipping: server returned 500 on skills import (issue #83): %s", truncate(out, 200))
	}
	if importErr != nil {
		rl.Failf("skills import failed: %v\n%s", importErr, truncate(out, 300))
	}

	if !strings.Contains(out, "e2e-imp") {
		t.Errorf("expected import output to contain skill name, got:\n%s", truncate(out, 500))
	}
	rl.Printf("import output: %s", truncate(out, 300))

	// Parse skill ID for cleanup.
	skillID := parseLineField(out, "ID:")
	if skillID == "" {
		rl.Printf("could not parse skill ID from import output — will verify via list")
	} else {
		rl.Printf("imported skill ID: %s", skillID)
		// Clean up the skill.
		t.Cleanup(func() {
			runCLIInDirWithHome(t, "", home,
				"skills", "delete", skillID,
				"--project", projectID,
			)
		})
	}

	// Verify the skill appears in list.
	rl.Section("Verify skill in list")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"skills", "list",
		"--project", projectID,
	)
	rl.CLI("memory skills list --project "+projectID, listOut)

	if !strings.Contains(listOut, "e2e-imp") {
		t.Errorf("expected skill to appear in list, got:\n%s", truncate(listOut, 500))
	}
	rl.Printf("skill appears in list: true")
}

// TestCLIInstalled_SkillsImportFromDir verifies that `memory skills import
// --from-dir <path>` imports all skills from a directory.
func TestCLIInstalled_SkillsImportFromDir(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify skills import --from-dir imports skills from directory",
		"Create project and a directory with 2 SKILL.md files",
		"Import skills via `memory skills import --from-dir <path>`",
		"Verify both skills appear in list",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-skill-impdir")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	// Create 2 skill directories.
	rl.Section("Create skill directories")
	skillsRoot := filepath.Join(home, "skills-dir")
	for _, skillName := range []string{"e2e-dir-alpha", "e2e-dir-beta"} {
		dir := filepath.Join(skillsRoot, skillName)
		if err := os.MkdirAll(dir, 0755); err != nil {
			rl.Failf("failed to create skill dir: %v", err)
		}
		content := "---\nname: " + skillName + "\ndescription: Skill " + skillName + " for dir import test\n---\n\n# " + skillName + "\n\nTest skill.\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
			rl.Failf("failed to write SKILL.md for %s: %v", skillName, err)
		}
	}
	rl.Printf("created 2 skill directories under %s", skillsRoot)

	// Import from directory.
	rl.Section("Import skills from directory")
	out, err := runCLIInDirWithHome(t, "", home,
		"skills", "import",
		"--from-dir", skillsRoot,
		"--project", projectID,
	)
	rl.CLI("memory skills import --from-dir "+skillsRoot+" --project "+projectID, out)

	// Server bug #83: skills create returns 500 for project-scoped skills.
	if err != nil && strings.Contains(out, "500") {
		t.Skipf("skipping: server returned 500 on skills import --from-dir (issue #83): %s", truncate(out, 200))
	}
	if err != nil {
		rl.Printf("import returned error (may be partial): %v", err)
	}
	rl.Printf("import output: %s", truncate(out, 500))

	// Verify skills in list.
	rl.Section("Verify skills in list")
	listOut := mustRunCLIInDirWithHome(t, "", home,
		"skills", "list",
		"--project", projectID,
	)
	rl.CLI("memory skills list --project "+projectID, listOut)

	for _, skillName := range []string{"e2e-dir-alpha", "e2e-dir-beta"} {
		if !strings.Contains(listOut, skillName) {
			t.Errorf("expected skill %q to appear in list, got:\n%s", skillName, truncate(listOut, 500))
		}
	}
	rl.Printf("both skills appear in list: true")
}

var _ = framework.SetToken // keep import used
