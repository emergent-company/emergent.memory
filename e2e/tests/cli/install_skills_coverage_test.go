// Package cli_test — install_skills_coverage_test.go
//
// Dedicated coverage for `memory install-memory-skills` edge paths:
//   - --dir <custom-path>: install into a non-default directory
//   - idempotency: running twice without --force skips existing skills
//   - --force: overwrites existing skill directories
//   - manifest validity: each memory-* skill has a parseable SKILL.md
//
// These tests complement the broader TestCLIInstalled_SkillsInstall* tests
// in install_test.go by specifically targeting the embedded-skills code path
// introduced in runlog v0.1.2.
package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallMemorySkills_CustomDir verifies that `memory install-memory-skills
// --dir <path>` installs skills into the specified directory rather than the
// default .agents/skills/ relative to cwd.
func TestInstallMemorySkills_CustomDir(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify install-memory-skills --dir installs into custom directory",
		"Run `memory install-memory-skills --dir <tmpdir>`",
		"Assert exit 0 and at least one memory-* subdirectory created in target",
		"Assert default .agents/skills/ was NOT created in cwd",
	)
	logStatusPreamble(t)

	customDir := t.TempDir()
	cwd := t.TempDir() // separate cwd so we can check default path is absent

	rl.Section("Run install-memory-skills with --dir")
	out := mustRunCLIInDir(t, cwd, "install-memory-skills", "--dir", customDir, "--force")
	rl.CLI("memory install-memory-skills --dir "+customDir+" --force", out)

	rl.Section("Verify skills installed in custom dir")
	entries, err := os.ReadDir(customDir)
	if err != nil {
		t.Fatalf("could not read custom dir %s: %v", customDir, err)
	}
	var memorySkills []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "memory-") {
			memorySkills = append(memorySkills, e.Name())
		}
	}
	if len(memorySkills) == 0 {
		t.Errorf("expected at least one memory-* skill directory in %s, found none", customDir)
	}
	rl.Printf("memory-* skills installed in custom dir: %v", memorySkills)

	rl.Section("Verify default .agents/skills/ was NOT created in cwd")
	defaultPath := filepath.Join(cwd, ".agents", "skills")
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Errorf("unexpected default skills path created at %s", defaultPath)
	}
	rl.Printf("default .agents/skills/ absent in cwd: true")
}

// TestInstallMemorySkills_Idempotent verifies that running
// `memory install-memory-skills` twice in the same directory (without --force)
// exits 0 on the second run and skips existing skills.
func TestInstallMemorySkills_Idempotent(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify install-memory-skills is idempotent without --force",
		"Run install-memory-skills once (with --force to ensure clean install)",
		"Run again without --force",
		"Assert second run exits 0 and output mentions skipped",
	)
	logStatusPreamble(t)

	skillsDir := t.TempDir()

	rl.Section("First install (--force)")
	out1 := mustRunCLIInDir(t, t.TempDir(), "install-memory-skills", "--dir", skillsDir, "--force")
	rl.CLI("memory install-memory-skills --dir "+skillsDir+" --force", out1)
	rl.Printf("first install output: %s", truncate(out1, 300))

	rl.Section("Second install (no --force)")
	out2, err := runCLIInDirWithHome(t, "", t.TempDir(), "install-memory-skills", "--dir", skillsDir)
	rl.CLIErr("memory install-memory-skills --dir "+skillsDir, out2, err, 0)

	if err != nil {
		t.Errorf("expected second install to exit 0, got error: %v — %s", err, truncate(out2, 300))
	}

	rl.Section("Assert second run indicates skipped")
	if !strings.Contains(out2, "skip") && !strings.Contains(out2, "already") && !strings.Contains(out2, "exist") && !strings.Contains(out2, "up to date") {
		t.Errorf("expected second run to report skipped skills, got:\n%s", truncate(out2, 500))
	}
	rl.Printf("second install skipped existing skills: true")
}

// TestInstallMemorySkills_Force_Overwrites verifies that running with --force
// replaces an existing skill directory with the embedded version.
func TestInstallMemorySkills_Force_Overwrites(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify install-memory-skills --force overwrites existing skill dirs",
		"Install skills once to get a skill directory",
		"Place a sentinel file inside one skill directory",
		"Re-run with --force",
		"Assert sentinel file is gone (skill directory was replaced)",
	)
	logStatusPreamble(t)

	skillsDir := t.TempDir()

	rl.Section("Initial install")
	out := mustRunCLIInDir(t, t.TempDir(), "install-memory-skills", "--dir", skillsDir, "--force")
	rl.CLI("memory install-memory-skills --dir "+skillsDir+" --force", out)

	rl.Section("Identify a skill dir and place sentinel file")
	entries, err := os.ReadDir(skillsDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected skills in %s after first install, got err=%v entries=%d", skillsDir, err, len(entries))
	}
	var targetSkill string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "memory-") {
			targetSkill = e.Name()
			break
		}
	}
	if targetSkill == "" {
		t.Fatal("no memory-* skill directories found after first install")
	}
	sentinelPath := filepath.Join(skillsDir, targetSkill, "SENTINEL_DO_NOT_KEEP.txt")
	if err := os.WriteFile(sentinelPath, []byte("test sentinel"), 0o644); err != nil {
		t.Fatalf("could not write sentinel file: %v", err)
	}
	rl.Printf("placed sentinel at: %s", sentinelPath)

	rl.Section("Re-run with --force")
	out2 := mustRunCLIInDir(t, t.TempDir(), "install-memory-skills", "--dir", skillsDir, "--force")
	rl.CLI("memory install-memory-skills --dir "+skillsDir+" --force", out2)

	rl.Section("Assert sentinel file is gone")
	if _, statErr := os.Stat(sentinelPath); !os.IsNotExist(statErr) {
		t.Errorf("expected sentinel file to be removed after --force overwrite, but it still exists at %s", sentinelPath)
	}
	rl.Printf("sentinel file removed after --force overwrite: true")
}

// TestInstallMemorySkills_SkillManifestsValid verifies that each installed
// memory-* skill directory contains a SKILL.md with non-empty name and
// description fields that reference the "memory" command.
func TestInstallMemorySkills_SkillManifestsValid(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify each installed memory-* skill has a valid SKILL.md",
		"Install skills into a temp directory",
		"For each memory-* dir: assert SKILL.md exists and references 'memory'",
	)
	logStatusPreamble(t)

	skillsDir := t.TempDir()

	rl.Section("Install skills")
	out := mustRunCLIInDir(t, t.TempDir(), "install-memory-skills", "--dir", skillsDir, "--force")
	rl.CLI("memory install-memory-skills --dir "+skillsDir+" --force", out)

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("could not read skills dir %s: %v", skillsDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no skills installed")
	}

	rl.Section("Validate SKILL.md for each memory-* skill")
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "memory-") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			skillMDPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
			data, err := os.ReadFile(skillMDPath)
			if err != nil {
				t.Errorf("SKILL.md missing in %s: %v", entry.Name(), err)
				return
			}
			content := string(data)
			if strings.TrimSpace(content) == "" {
				t.Errorf("SKILL.md is empty in %s", entry.Name())
				return
			}
			// Frontmatter should have a name field.
			if !strings.Contains(content, "name:") {
				t.Errorf("SKILL.md in %s missing 'name:' field", entry.Name())
			}
			// Frontmatter should have a description field.
			if !strings.Contains(content, "description:") {
				t.Errorf("SKILL.md in %s missing 'description:' field", entry.Name())
			}
			// Memory skills should reference the 'memory' CLI command.
			if !strings.Contains(content, "memory") {
				t.Errorf("SKILL.md in %s does not reference 'memory' CLI", entry.Name())
			}
			rl.Printf("  %s: SKILL.md valid", entry.Name())
		})
	}
}
