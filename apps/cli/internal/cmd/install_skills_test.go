package cmd

import (
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
// validateSkillFrontmatter
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
// parseSkillFrontmatter
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
// discoverSkillsInDir
// ---------------------------------------------------------------------------

func TestDiscoverSkillsInDir_SingleSkill(t *testing.T) {
	dir := makeSkillDir(t, validSkillMD)
	skills, err := discoverSkillsInDir(dir)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	assert.Equal(t, "my-skill", skills[0].Name)
	assert.Equal(t, "A test skill for unit tests", skills[0].Description)
	assert.Contains(t, skills[0].Content, "# My Skill")
}

func TestDiscoverSkillsInDir_Catalog(t *testing.T) {
	catalog := t.TempDir()
	for _, name := range []string{"skill-a", "skill-b"} {
		skillMD := "---\nname: " + name + "\ndescription: Skill " + name + "\n---\n# " + name + "\n"
		d := filepath.Join(catalog, name)
		require.NoError(t, os.MkdirAll(d, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(skillMD), 0o644))
	}
	// A dir without SKILL.md should be skipped
	require.NoError(t, os.MkdirAll(filepath.Join(catalog, "not-a-skill"), 0o755))

	skills, err := discoverSkillsInDir(catalog)
	require.NoError(t, err)
	require.Len(t, skills, 2)
	names := []string{skills[0].Name, skills[1].Name}
	assert.Contains(t, names, "skill-a")
	assert.Contains(t, names, "skill-b")
}

func TestDiscoverSkillsInDir_NonExistent(t *testing.T) {
	skills, err := discoverSkillsInDir("/nonexistent/path/xyz")
	require.NoError(t, err) // non-existent returns empty, not error
	assert.Empty(t, skills)
}

func TestDiscoverSkillsInDir_Empty(t *testing.T) {
	dir := t.TempDir()
	skills, err := discoverSkillsInDir(dir)
	require.NoError(t, err)
	assert.Empty(t, skills)
}

// ---------------------------------------------------------------------------
// skillContentFromBytes
// ---------------------------------------------------------------------------

func TestSkillContentFromBytes_ExtractsContent(t *testing.T) {
	content := skillContentFromBytes([]byte(validSkillMD))
	assert.Contains(t, content, "# My Skill")
	assert.Contains(t, content, "Some content here.")
	assert.NotContains(t, content, "name: my-skill")
}

func TestSkillContentFromBytes_NoFrontmatter(t *testing.T) {
	raw := "# Just content\nNo frontmatter."
	content := skillContentFromBytes([]byte(raw))
	assert.Equal(t, raw, content)
}
