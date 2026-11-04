package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// runMemorySkillsCmd executes runInstallMemorySkills against the given target
// directory, with optional flags set via the provided setter func.
func runMemorySkillsCmd(t *testing.T, targetDir string, setup func()) string {
	t.Helper()

	// Reset package-level flag vars between test runs.
	installMemorySkillsForce = false
	installMemorySkillsDir = targetDir
	installMemorySkillsPrune = false

	if setup != nil {
		setup()
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := runInstallMemorySkills(cmd, nil)
	require.NoError(t, err)

	return buf.String()
}

// makeStaleDirs creates subdirectories in targetDir to simulate stale skills.
func makeStaleDirs(t *testing.T, targetDir string, names ...string) {
	t.Helper()
	for _, name := range names {
		err := os.MkdirAll(filepath.Join(targetDir, name), 0o755)
		require.NoError(t, err)
	}
}

// ---------------------------------------------------------------------------
// Prune with --prune flag (non-interactive)
// ---------------------------------------------------------------------------

func TestInstallMemorySkills_PruneFlag_RemovesStaleMemoryDirs(t *testing.T) {
	targetDir := t.TempDir()

	// Create a stale memory-* dir that is NOT in the embedded catalog.
	makeStaleDirs(t, targetDir, "memory-old-skill", "memory-ancient")

	out := runMemorySkillsCmd(t, targetDir, func() {
		installMemorySkillsPrune = true
	})

	// Stale dirs should be gone.
	_, err1 := os.Stat(filepath.Join(targetDir, "memory-old-skill"))
	assert.True(t, os.IsNotExist(err1), "memory-old-skill should have been pruned")

	_, err2 := os.Stat(filepath.Join(targetDir, "memory-ancient"))
	assert.True(t, os.IsNotExist(err2), "memory-ancient should have been pruned")

	// Output should mention the pruned skills.
	assert.Contains(t, out, "pruned memory-old-skill")
	assert.Contains(t, out, "pruned memory-ancient")

	// Summary line should mention pruned count.
	assert.Contains(t, out, "pruned")
}

func TestInstallMemorySkills_PruneFlag_KeepsNonMemoryDirs(t *testing.T) {
	targetDir := t.TempDir()

	// Create dirs: one non-memory (should survive) and one stale memory (should go).
	makeStaleDirs(t, targetDir, "my-custom-skill", "memory-stale")

	runMemorySkillsCmd(t, targetDir, func() {
		installMemorySkillsPrune = true
	})

	// Non-memory dir must remain.
	_, err := os.Stat(filepath.Join(targetDir, "my-custom-skill"))
	assert.NoError(t, err, "non-memory-* dir should not be pruned")

	// Stale memory dir should be gone.
	_, err2 := os.Stat(filepath.Join(targetDir, "memory-stale"))
	assert.True(t, os.IsNotExist(err2), "memory-stale should have been pruned")
}

func TestInstallMemorySkills_PruneFlag_KeepsCatalogSkills(t *testing.T) {
	targetDir := t.TempDir()

	// Pre-install once (no prune) so catalog skills exist in targetDir.
	runMemorySkillsCmd(t, targetDir, nil)

	// Collect installed skills.
	entries, err := os.ReadDir(targetDir)
	require.NoError(t, err)
	var catalogSkillDirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "memory-") {
			catalogSkillDirs = append(catalogSkillDirs, e.Name())
		}
	}
	require.NotEmpty(t, catalogSkillDirs, "expected at least one catalog skill to be installed")

	// Run again with --prune; catalog skills should still be present.
	runMemorySkillsCmd(t, targetDir, func() {
		installMemorySkillsPrune = true
		installMemorySkillsForce = true // re-install fresh
	})

	for _, name := range catalogSkillDirs {
		_, statErr := os.Stat(filepath.Join(targetDir, name))
		assert.NoError(t, statErr, "catalog skill %s should not be pruned", name)
	}
}

// ---------------------------------------------------------------------------
// Non-interactive, no --prune: hint message
// ---------------------------------------------------------------------------

func TestInstallMemorySkills_NoPrune_NonInteractive_PrintsHint(t *testing.T) {
	targetDir := t.TempDir()
	makeStaleDirs(t, targetDir, "memory-old-skill")

	// isInteractiveTerminal() returns false in test environment (no real TTY).
	out := runMemorySkillsCmd(t, targetDir, nil)

	// Stale dir should NOT be removed.
	_, err := os.Stat(filepath.Join(targetDir, "memory-old-skill"))
	assert.NoError(t, err, "stale dir should remain when --prune is not set")

	// Output should hint about --prune.
	assert.Contains(t, out, "--prune")
	assert.Contains(t, out, "stale")
}

func TestInstallMemorySkills_NoPrune_NoStale_NoHint(t *testing.T) {
	targetDir := t.TempDir()
	// No stale dirs; only catalog-matching skills will be installed.

	out := runMemorySkillsCmd(t, targetDir, nil)

	// No hint about --prune when there are no stale skills.
	assert.NotContains(t, out, "--prune")
}

// ---------------------------------------------------------------------------
// Summary line counts
// ---------------------------------------------------------------------------

func TestInstallMemorySkills_SummaryIncludesPrunedCount(t *testing.T) {
	targetDir := t.TempDir()
	makeStaleDirs(t, targetDir, "memory-gone1", "memory-gone2")

	out := runMemorySkillsCmd(t, targetDir, func() {
		installMemorySkillsPrune = true
	})

	// Summary line should include "2 pruned" or ", 2 pruned".
	assert.Contains(t, out, "2 pruned")
}

func TestInstallMemorySkills_SummaryNoPrunedWhenNoneRemoved(t *testing.T) {
	targetDir := t.TempDir()

	out := runMemorySkillsCmd(t, targetDir, func() {
		installMemorySkillsPrune = true
	})

	// When nothing is pruned, the word "pruned" should not appear in summary.
	// (It may appear in individual skill lines if nothing stale, which there isn't.)
	assert.NotContains(t, out, "pruned")
}
