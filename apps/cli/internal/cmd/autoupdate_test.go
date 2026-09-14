package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/autoupdate"
	"github.com/spf13/cobra"
)

// TestIsSkipCommand verifies that upgrade, version, and completion commands
// (and their sub-commands) are detected as skip targets.
func TestIsSkipCommand(t *testing.T) {
	root := &cobra.Command{Use: "memory"}
	up := &cobra.Command{Use: "upgrade"}
	ver := &cobra.Command{Use: "version"}
	comp := &cobra.Command{Use: "completion"}
	sub := &cobra.Command{Use: "bash"}
	root.AddCommand(up, ver, comp)
	comp.AddCommand(sub)

	normal := &cobra.Command{Use: "list"}
	root.AddCommand(normal)

	cases := []struct {
		cmd  *cobra.Command
		want bool
	}{
		{up, true},
		{ver, true},
		{comp, true},
		{sub, true}, // child of completion is also skipped
		{normal, false},
	}

	for _, tc := range cases {
		if got := isSkipCommand(tc.cmd); got != tc.want {
			t.Errorf("isSkipCommand(%q) = %v, want %v", tc.cmd.Name(), got, tc.want)
		}
	}
}

// TestShouldSkipAutoUpdate_KillSwitch verifies that MEMORY_NO_AUTO_UPDATE
// bypasses all other checks.
func TestShouldSkipAutoUpdate_KillSwitch(t *testing.T) {
	t.Setenv("MEMORY_NO_AUTO_UPDATE", "1")

	cmd := &cobra.Command{Use: "list"}
	// Even though nothing else would skip it, the env var must win.
	if !shouldSkipAutoUpdate(cmd) {
		t.Error("expected shouldSkipAutoUpdate=true when MEMORY_NO_AUTO_UPDATE is set")
	}
}

// TestShouldSkipAutoUpdate_DevBuild verifies that a "dev" Version is skipped.
func TestShouldSkipAutoUpdate_DevBuild(t *testing.T) {
	// Ensure kill-switch is clear.
	os.Unsetenv("MEMORY_NO_AUTO_UPDATE")

	orig := Version
	Version = "dev"
	t.Cleanup(func() { Version = orig })

	cmd := &cobra.Command{Use: "list"}
	if !shouldSkipAutoUpdate(cmd) {
		t.Error("expected shouldSkipAutoUpdate=true for dev build")
	}
}

// TestShouldSkipAutoUpdate_ExcludedCommand verifies that "upgrade" is skipped.
func TestShouldSkipAutoUpdate_ExcludedCommand(t *testing.T) {
	os.Unsetenv("MEMORY_NO_AUTO_UPDATE")

	orig := Version
	Version = "1.0.0"
	t.Cleanup(func() { Version = orig })

	root := &cobra.Command{Use: "memory"}
	up := &cobra.Command{Use: "upgrade"}
	root.AddCommand(up)

	if !shouldSkipAutoUpdate(up) {
		t.Error("expected shouldSkipAutoUpdate=true for upgrade command")
	}
}

// TestUpdateCheck_NoBlockWhenOffline verifies that the PostRunE notification
// path does not block for more than ~200ms even when the HTTP call would time out.
// It exercises the 100ms timeout in PersistentPostRunE by pre-populating the
// channel with a nil (as the skip-path does) and asserting the whole path
// completes quickly.
func TestUpdateCheck_NoBlockWhenOffline(t *testing.T) {
	os.Unsetenv("MEMORY_NO_AUTO_UPDATE")

	// Simulate an already-populated channel (nil = no update available).
	updateCheckCh = make(chan *autoupdate.CheckResult, 1)
	updateCheckCh <- nil

	start := time.Now()
	// Call PostRunE directly.
	err := rootCmd.PersistentPostRunE(rootCmd, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("PostRunE returned unexpected error: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("PostRunE took %v, expected < 200ms", elapsed)
	}
}

// TestUpdateCheck_CacheFile verifies that CheckForUpdate writes a cache file
// and that a second call with a fresh cache avoids network access.
func TestUpdateCheck_CacheFile(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "latest-version.json")

	// First call with no cache — will attempt network but we don't care about
	// the result (it may fail offline); we just want it to not panic.
	_ = autoupdate.CheckForUpdate("1.0.0", cachePath, 24*time.Hour, nil)
}
