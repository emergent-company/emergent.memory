package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeSkillDir creates a temp directory with a SKILL.md file containing the
// provided content and returns the directory path.
func makeSkillDir(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644)
	require.NoError(t, err)
	return dir
}

const validSkillMD = `---
name: my-skill
description: A test skill for unit tests
---

# My Skill

Some content here.
`

// ---------------------------------------------------------------------------
// 2.3 — validateSkillFrontmatter
// ---------------------------------------------------------------------------

func TestValidateSkillFrontmatter_Valid(t *testing.T) {
	fm := &SkillFrontmatter{Name: "my-skill", Description: "A valid skill"}
	errs := validateSkillFrontmatter(fm, "")
	assert.Empty(t, errs)
}

func TestValidateSkillFrontmatter_ValidSingleChar(t *testing.T) {
	fm := &SkillFrontmatter{Name: "a", Description: "Single char name"}
	errs := validateSkillFrontmatter(fm, "")
	assert.Empty(t, errs)
}

func TestValidateSkillFrontmatter_MissingName(t *testing.T) {
	fm := &SkillFrontmatter{Name: "", Description: "No name"}
	errs := validateSkillFrontmatter(fm, "")
	assert.Contains(t, errs, "name is required")
}

func TestValidateSkillFrontmatter_UppercaseName(t *testing.T) {
	fm := &SkillFrontmatter{Name: "MySkill", Description: "Uppercase name"}
	errs := validateSkillFrontmatter(fm, "")
	assert.True(t, len(errs) > 0, "expected validation errors for uppercase name")
	found := false
	for _, e := range errs {
		if strings.Contains(e, "lowercase") {
			found = true
		}
	}
	assert.True(t, found, "expected lowercase error, got: %v", errs)
}

func TestValidateSkillFrontmatter_LeadingHyphen(t *testing.T) {
	fm := &SkillFrontmatter{Name: "-my-skill", Description: "Leading hyphen"}
	errs := validateSkillFrontmatter(fm, "")
	hasHyphenErr := false
	for _, e := range errs {
		if strings.Contains(e, "hyphen") {
			hasHyphenErr = true
		}
	}
	assert.True(t, hasHyphenErr, "expected hyphen error, got: %v", errs)
}

func TestValidateSkillFrontmatter_TrailingHyphen(t *testing.T) {
	fm := &SkillFrontmatter{Name: "my-skill-", Description: "Trailing hyphen"}
	errs := validateSkillFrontmatter(fm, "")
	hasHyphenErr := false
	for _, e := range errs {
		if strings.Contains(e, "hyphen") {
			hasHyphenErr = true
		}
	}
	assert.True(t, hasHyphenErr, "expected hyphen error, got: %v", errs)
}

func TestValidateSkillFrontmatter_ConsecutiveHyphens(t *testing.T) {
	fm := &SkillFrontmatter{Name: "my--skill", Description: "Consecutive hyphens"}
	errs := validateSkillFrontmatter(fm, "")
	hasConsecErr := false
	for _, e := range errs {
		if strings.Contains(e, "consecutive") {
			hasConsecErr = true
		}
	}
	assert.True(t, hasConsecErr, "expected consecutive hyphens error, got: %v", errs)
}

func TestValidateSkillFrontmatter_NameTooLong(t *testing.T) {
	fm := &SkillFrontmatter{
		Name:        strings.Repeat("a", 65),
		Description: "Name too long",
	}
	errs := validateSkillFrontmatter(fm, "")
	hasLenErr := false
	for _, e := range errs {
		if strings.Contains(e, "64 characters") {
			hasLenErr = true
		}
	}
	assert.True(t, hasLenErr, "expected length error, got: %v", errs)
}

func TestValidateSkillFrontmatter_NameExactly64(t *testing.T) {
	fm := &SkillFrontmatter{
		Name:        strings.Repeat("a", 64),
		Description: "Exactly 64 chars",
	}
	errs := validateSkillFrontmatter(fm, "")
	// 64 chars is valid length; may fail regex (all 'a's is fine)
	for _, e := range errs {
		assert.NotContains(t, e, "64 characters", "64 chars should be allowed")
	}
}

func TestValidateSkillFrontmatter_MissingDescription(t *testing.T) {
	fm := &SkillFrontmatter{Name: "my-skill", Description: ""}
	errs := validateSkillFrontmatter(fm, "")
	assert.Contains(t, errs, "description is required")
}

func TestValidateSkillFrontmatter_DescriptionTooLong(t *testing.T) {
	fm := &SkillFrontmatter{Name: "my-skill", Description: strings.Repeat("x", 1025)}
	errs := validateSkillFrontmatter(fm, "")
	hasLenErr := false
	for _, e := range errs {
		if strings.Contains(e, "1024") {
			hasLenErr = true
		}
	}
	assert.True(t, hasLenErr, "expected description length error, got: %v", errs)
}

func TestValidateSkillFrontmatter_DirNameMismatch(t *testing.T) {
	fm := &SkillFrontmatter{Name: "my-skill", Description: "A skill"}
	errs := validateSkillFrontmatter(fm, "other-name")
	hasMismatch := false
	for _, e := range errs {
		if strings.Contains(e, "match directory") {
			hasMismatch = true
		}
	}
	assert.True(t, hasMismatch, "expected dir name mismatch error, got: %v", errs)
}

func TestValidateSkillFrontmatter_DirNameMatch(t *testing.T) {
	fm := &SkillFrontmatter{Name: "my-skill", Description: "A skill"}
	errs := validateSkillFrontmatter(fm, "my-skill")
	assert.Empty(t, errs)
}

// ---------------------------------------------------------------------------
// 2.1 — parseSkillFrontmatter
// ---------------------------------------------------------------------------

func TestParseSkillFrontmatter_Valid(t *testing.T) {
	dir := makeSkillDir(t, validSkillMD)
	fm, err := parseSkillFrontmatter(dir)
	require.NoError(t, err)
	assert.Equal(t, "my-skill", fm.Name)
	assert.Equal(t, "A test skill for unit tests", fm.Description)
}

func TestParseSkillFrontmatter_MissingSKILLmd(t *testing.T) {
	dir := t.TempDir()
	_, err := parseSkillFrontmatter(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no SKILL.md found")
}

func TestParseSkillFrontmatter_NoFrontmatter(t *testing.T) {
	dir := makeSkillDir(t, "# Just a heading\n\nNo frontmatter here.\n")
	_, err := parseSkillFrontmatter(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no YAML frontmatter")
}

func TestParseSkillFrontmatter_MalformedFrontmatter(t *testing.T) {
	dir := makeSkillDir(t, "---\nname: test\n# missing closing ---\n")
	_, err := parseSkillFrontmatter(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed frontmatter")
}

// ---------------------------------------------------------------------------
// 3.3 — validate subcommand
// ---------------------------------------------------------------------------

func TestInstallSkillsValidate_ValidSkill(t *testing.T) {
	dir := makeSkillDir(t, validSkillMD)

	cmd := installSkillValidateCmd
	var out strings.Builder
	cmd.SetOut(&out)

	err := runValidateSkill(cmd, []string{dir})
	require.NoError(t, err)
}

func TestInstallSkillsValidate_InvalidSkill(t *testing.T) {
	dir := makeSkillDir(t, "---\nname: \ndescription: \n---\n")

	cmd := installSkillValidateCmd
	err := runValidateSkill(cmd, []string{dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestInstallSkillsValidate_DefaultsToCurrentDir(t *testing.T) {
	// Create a valid skill in a temp dir and chdir there
	dir := makeSkillDir(t, validSkillMD)
	orig, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(orig) })
	require.NoError(t, os.Chdir(dir))

	cmd := installSkillValidateCmd
	err = runValidateSkill(cmd, []string{})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// 4.6 — installFromLocalPath
// ---------------------------------------------------------------------------

func TestInstallFromLocalPath_HappyPath(t *testing.T) {
	src := makeSkillDir(t, validSkillMD)
	// add a second file to verify full tree copy
	err := os.WriteFile(filepath.Join(src, "extra.txt"), []byte("extra"), 0o644)
	require.NoError(t, err)

	targetDir := t.TempDir()
	err = installFromLocalPath(src, targetDir, false, false)
	require.NoError(t, err)

	dest := filepath.Join(targetDir, "my-skill")
	assert.DirExists(t, dest)
	assert.FileExists(t, filepath.Join(dest, "SKILL.md"))
	assert.FileExists(t, filepath.Join(dest, "extra.txt"))
}

func TestInstallFromLocalPath_AlreadyExists_NonInteractive_NoForce(t *testing.T) {
	src := makeSkillDir(t, validSkillMD)
	targetDir := t.TempDir()

	// First install
	err := installFromLocalPath(src, targetDir, false, false)
	require.NoError(t, err)

	// Second install without force — non-interactive (CI/test env has no TTY)
	err = installFromLocalPath(src, targetDir, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")
	assert.Contains(t, err.Error(), "--force")
}

func TestInstallFromLocalPath_AlreadyExists_WithForce(t *testing.T) {
	src := makeSkillDir(t, validSkillMD)
	targetDir := t.TempDir()

	// First install
	err := installFromLocalPath(src, targetDir, false, false)
	require.NoError(t, err)

	// Modify source to simulate an update
	err = os.WriteFile(filepath.Join(src, "new-file.txt"), []byte("new"), 0o644)
	require.NoError(t, err)

	// Second install with force
	err = installFromLocalPath(src, targetDir, true, false)
	require.NoError(t, err)

	// New file should be present
	assert.FileExists(t, filepath.Join(targetDir, "my-skill", "new-file.txt"))
}

func TestInstallFromLocalPath_MissingSKILLmd(t *testing.T) {
	src := t.TempDir() // empty dir — no SKILL.md, no subdirs
	targetDir := t.TempDir()

	err := installFromLocalPath(src, targetDir, false, false)
	require.Error(t, err)
	// Empty dir with no SKILL.md and no skill subdirs → catalog mode, no skills found
	assert.True(t,
		strings.Contains(err.Error(), "no SKILL.md found") || strings.Contains(err.Error(), "no skill subdirectories detected"),
		"unexpected error: %v", err)
}

func TestInstallFromLocalPath_InvalidSKILLmd(t *testing.T) {
	src := makeSkillDir(t, "---\nname: \ndescription: \n---\n")
	targetDir := t.TempDir()

	err := installFromLocalPath(src, targetDir, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestInstallFromLocalPath_CreatesTargetDir(t *testing.T) {
	src := makeSkillDir(t, validSkillMD)
	// Use a nested non-existent target
	targetDir := filepath.Join(t.TempDir(), "a", "b", "c")

	err := installFromLocalPath(src, targetDir, false, false)
	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(targetDir, "my-skill"))
}

func TestInstallFromLocalPath_SkipsGitDir(t *testing.T) {
	src := makeSkillDir(t, validSkillMD)
	gitDir := filepath.Join(src, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("git config"), 0o644))

	targetDir := t.TempDir()
	err := installFromLocalPath(src, targetDir, false, false)
	require.NoError(t, err)

	assert.NoDirExists(t, filepath.Join(targetDir, "my-skill", ".git"))
}

func TestInstallFromLocalPath_NonExistentSrc(t *testing.T) {
	err := installFromLocalPath("/nonexistent/path", t.TempDir(), false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestInstallFromLocalPath_SrcIsFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-dir-*.txt")
	require.NoError(t, err)
	f.Close()

	err = installFromLocalPath(f.Name(), t.TempDir(), false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

// ---------------------------------------------------------------------------
// Catalog install mode
// ---------------------------------------------------------------------------

func TestInstallFromLocalPath_CatalogHappyPath(t *testing.T) {
	// Build a catalog dir with two skill subdirs
	catalog := t.TempDir()
	for _, name := range []string{"skill-a", "skill-b"} {
		skillMD := "---\nname: " + name + "\ndescription: Skill " + name + "\n---\n"
		d := filepath.Join(catalog, name)
		require.NoError(t, os.MkdirAll(d, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(skillMD), 0o644))
	}

	targetDir := t.TempDir()
	err := installFromLocalPath(catalog, targetDir, false, false)
	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(targetDir, "skill-a"))
	assert.DirExists(t, filepath.Join(targetDir, "skill-b"))
}

func TestInstallFromLocalPath_CatalogSkipsNonSkillDirs(t *testing.T) {
	catalog := t.TempDir()
	// One real skill
	require.NoError(t, os.MkdirAll(filepath.Join(catalog, "real-skill"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(catalog, "real-skill", "SKILL.md"),
		[]byte("---\nname: real-skill\ndescription: Real\n---\n"), 0o644))
	// One dir without SKILL.md
	require.NoError(t, os.MkdirAll(filepath.Join(catalog, "not-a-skill"), 0o755))

	targetDir := t.TempDir()
	err := installFromLocalPath(catalog, targetDir, false, false)
	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(targetDir, "real-skill"))
	assert.NoDirExists(t, filepath.Join(targetDir, "not-a-skill"))
}

func TestInstallFromLocalPath_CatalogEmptyDir(t *testing.T) {
	catalog := t.TempDir() // no subdirs
	targetDir := t.TempDir()

	err := installFromLocalPath(catalog, targetDir, false, false)
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "no SKILL.md found") || strings.Contains(err.Error(), "no skill subdirectories"),
		"unexpected error: %v", err)
}

func TestRunInstallSkill_DefaultsToEmbedded(t *testing.T) {
	targetDir := t.TempDir()
	installSkillsDir = targetDir

	// No args — should install only emergent-* skills from the built-in catalog.
	err := runInstallSkill(installSkillInstallCmd, []string{})
	require.NoError(t, err)

	// emergent-* skills should be present.
	assert.DirExists(t, filepath.Join(targetDir, "emergent-onboard"))
	assert.DirExists(t, filepath.Join(targetDir, "emergent-query"))

	// Non-emergent skills should NOT be installed.
	assert.NoDirExists(t, filepath.Join(targetDir, "commit"))
	assert.NoDirExists(t, filepath.Join(targetDir, "release"))
}

// ---------------------------------------------------------------------------
// 6.6 — list subcommand
// ---------------------------------------------------------------------------

func TestRunListSkills_Empty(t *testing.T) {
	skillsDir := t.TempDir()
	installSkillsDir = skillsDir

	var out strings.Builder
	installSkillListCmd.SetOut(&out)

	err := runListSkills(installSkillListCmd, []string{})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "No skills installed")
}

func TestRunListSkills_NonExistentDir(t *testing.T) {
	installSkillsDir = filepath.Join(t.TempDir(), "does-not-exist")

	var out strings.Builder
	installSkillListCmd.SetOut(&out)

	err := runListSkills(installSkillListCmd, []string{})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "No skills installed")
}

func TestRunListSkills_WithSkills(t *testing.T) {
	skillsDir := t.TempDir()
	// Install a skill manually
	dest := filepath.Join(skillsDir, "my-skill")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(validSkillMD), 0o644))

	installSkillsDir = skillsDir

	var out strings.Builder
	installSkillListCmd.SetOut(&out)

	err := runListSkills(installSkillListCmd, []string{})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "my-skill")
}

func TestRunListSkills_JSONOutput(t *testing.T) {
	skillsDir := t.TempDir()
	dest := filepath.Join(skillsDir, "my-skill")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(validSkillMD), 0o644))

	installSkillsDir = skillsDir

	// Set --output json flag
	require.NoError(t, installSkillListCmd.Flags().Set("output", "json"))
	t.Cleanup(func() { _ = installSkillListCmd.Flags().Set("output", "table") })

	var out strings.Builder
	installSkillListCmd.SetOut(&out)
	t.Cleanup(func() { installSkillListCmd.SetOut(nil) })

	err := runListSkills(installSkillListCmd, []string{})
	require.NoError(t, err)

	var skills []SkillMeta
	require.NoError(t, json.Unmarshal([]byte(out.String()), &skills))
	require.Len(t, skills, 1)
	assert.Equal(t, "my-skill", skills[0].Name)
}

func TestRunListSkills_SkipsInvalidSkillDirs(t *testing.T) {
	skillsDir := t.TempDir()
	// A dir with no SKILL.md — should be silently skipped
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "not-a-skill"), 0o755))
	// A valid skill
	dest := filepath.Join(skillsDir, "my-skill")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(validSkillMD), 0o644))

	installSkillsDir = skillsDir

	var out strings.Builder
	installSkillListCmd.SetOut(&out)

	err := runListSkills(installSkillListCmd, []string{})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "my-skill")
	assert.NotContains(t, out.String(), "not-a-skill")
}

// ---------------------------------------------------------------------------
// 7.4 — remove subcommand
// ---------------------------------------------------------------------------

func TestRunRemoveSkill_HappyPath(t *testing.T) {
	skillsDir := t.TempDir()
	dest := filepath.Join(skillsDir, "my-skill")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(validSkillMD), 0o644))

	installSkillsDir = skillsDir

	// Set --force to skip interactive prompt
	require.NoError(t, installSkillRemoveCmd.Flags().Set("force", "true"))
	t.Cleanup(func() { _ = installSkillRemoveCmd.Flags().Set("force", "false") })

	err := runRemoveSkill(installSkillRemoveCmd, []string{"my-skill"})
	require.NoError(t, err)
	assert.NoDirExists(t, dest)
}

func TestRunRemoveSkill_NotInstalled(t *testing.T) {
	skillsDir := t.TempDir()
	installSkillsDir = skillsDir

	err := runRemoveSkill(installSkillRemoveCmd, []string{"nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestRunRemoveSkill_ForceSkipsPrompt(t *testing.T) {
	skillsDir := t.TempDir()
	dest := filepath.Join(skillsDir, "my-skill")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(validSkillMD), 0o644))

	installSkillsDir = skillsDir

	require.NoError(t, installSkillRemoveCmd.Flags().Set("force", "true"))
	t.Cleanup(func() { _ = installSkillRemoveCmd.Flags().Set("force", "false") })

	// Should succeed without needing stdin input
	err := runRemoveSkill(installSkillRemoveCmd, []string{"my-skill"})
	require.NoError(t, err)
	assert.NoDirExists(t, dest)
}
