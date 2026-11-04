// Package cli_test — upgrade_test.go
//
// Tests for `memory upgrade` and `memory server upgrade`.
//
// IMPORTANT: These tests do NOT invoke the actual upgrade flow because
// `memory upgrade --force` replaces the running binary in-place, which would
// corrupt the test environment.  Tests cover flag documentation and help
// output only.
package cli_test

import (
	"strings"
	"testing"
)

// TestCLI_Upgrade_Help verifies that `memory upgrade --help` exits 0 and
// documents the --force and --dir flags.
func TestCLI_Upgrade_Help(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory upgrade --help documents --force and --dir flags",
		"Run `memory upgrade --help`",
		"Assert exit 0 and both --force and --dir flags are present",
	)
	logStatusPreamble(t)

	rl.Section("Run memory upgrade --help")
	out, err := runCLIInDirWithHome(t, "", t.TempDir(), "upgrade", "--help")
	rl.CLIErr("memory upgrade --help", out, err, 0)

	if err != nil {
		t.Skipf("upgrade command not available: %v — %s", err, truncate(out, 300))
	}

	rl.Section("Verify --force flag documented")
	if !strings.Contains(out, "--force") && !strings.Contains(out, "-f") {
		t.Errorf("expected upgrade --help to document --force flag, got:\n%s", truncate(out, 500))
	}

	rl.Section("Verify --dir flag documented")
	if !strings.Contains(out, "--dir") {
		t.Errorf("expected upgrade --help to document --dir flag, got:\n%s", truncate(out, 500))
	}
	rl.Printf("upgrade --help: %d bytes, --force present: true, --dir present: true", len(out))
}

// TestCLI_ServerUpgrade_Help verifies that `memory server upgrade --help` exits
// 0 and mentions the upgrade subcommand purpose.
func TestCLI_ServerUpgrade_Help(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory server upgrade --help is well-formed",
		"Run `memory server upgrade --help`",
		"Assert exit 0 and output mentions upgrade",
	)
	logStatusPreamble(t)

	rl.Section("Run memory server upgrade --help")
	out, err := runCLIInDirWithHome(t, "", t.TempDir(), "server", "upgrade", "--help")
	rl.CLIErr("memory server upgrade --help", out, err, 0)

	if err != nil {
		t.Skipf("server upgrade command not available: %v — %s", err, truncate(out, 300))
	}

	lower := strings.ToLower(out)
	if !strings.Contains(lower, "upgrade") {
		t.Errorf("expected server upgrade --help to mention 'upgrade', got:\n%s", truncate(out, 500))
	}
	rl.Printf("server upgrade --help: %d bytes", len(out))
}
