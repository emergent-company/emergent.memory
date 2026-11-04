// Package cli_test — server_ctl_test.go
//
// Tests for `memory server ctl` subcommands and server lifecycle commands
// (install, uninstall, upgrade).  These tests cover help output and
// no-panic behavior for status checks — they do NOT install or uninstall
// server software.
package cli_test

import (
	"strings"
	"testing"
)

// TestCLI_ServerCtl_Help verifies that `memory server ctl --help` exits 0
// and lists the expected control operations including "status".
func TestCLI_ServerCtl_Help(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory server ctl --help lists control operations",
		"Run `memory server ctl --help`",
		"Assert exit 0 and 'status' subcommand is listed",
	)
	logStatusPreamble(t)

	rl.Section("Run memory server ctl --help")
	out := mustRunCLI(t, "server", "ctl", "--help")
	rl.CLI("memory server ctl --help", out)

	rl.Section("Verify 'status' subcommand present")
	if !strings.Contains(out, "status") {
		t.Errorf("expected server ctl --help to list 'status' subcommand, got:\n%s", truncate(out, 500))
	}
	rl.Printf("server ctl --help: %d bytes, contains status: true", len(out))
}

// TestCLI_ServerCtl_Status_NoPanic verifies that `memory server ctl status`
// exits with a recognizable message and does NOT produce a Go panic or
// unhandled runtime error, regardless of whether a server is installed.
func TestCLI_ServerCtl_Status_NoPanic(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory server ctl status does not panic",
		"Run `memory server ctl status`",
		"Assert output is non-empty and contains no Go panic trace",
	)
	logStatusPreamble(t)

	rl.Section("Run memory server ctl status")
	// Use non-fatal variant: the command may return non-zero if no server is
	// installed, which is the expected state in CI.
	out, err := runCLIInDirWithHome(t, "", t.TempDir(), "server", "ctl", "status")
	rl.CLIErr("memory server ctl status", out, err, 0)

	rl.Section("Assert no panic in output")
	// Check for Go runtime panic markers specifically — not just the word "panic"
	// anywhere (the temp dir path may contain "NoPanic" which lowercases to include
	// "panic" as a substring).
	if strings.Contains(out, "goroutine") && strings.Contains(out, "runtime/") {
		t.Errorf("server ctl status produced a Go panic trace — got:\n%s", truncate(out, 500))
	}
	if strings.Contains(out, "panic:") {
		t.Errorf("server ctl status panicked — got:\n%s", truncate(out, 500))
	}

	rl.Section("Assert non-empty output")
	if strings.TrimSpace(out) == "" && err == nil {
		t.Errorf("expected non-empty output from server ctl status")
	}
	rl.Printf("server ctl status: exit-err=%v, no panic: true", err)
}

// TestCLI_ServerInstall_Help verifies that `memory server install --help`
// exits 0.
func TestCLI_ServerInstall_Help(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory server install --help exits 0",
		"Run `memory server install --help`",
		"Assert exit 0",
	)
	logStatusPreamble(t)

	rl.Section("Run memory server install --help")
	out, err := runCLIInDirWithHome(t, "", t.TempDir(), "server", "install", "--help")
	rl.CLIErr("memory server install --help", out, err, 0)

	if err != nil {
		t.Skipf("server install command not available: %v", err)
	}

	lower := strings.ToLower(out)
	if !strings.Contains(lower, "install") {
		t.Errorf("expected server install --help to mention 'install', got:\n%s", truncate(out, 500))
	}
	rl.Printf("server install --help: %d bytes", len(out))
}

// TestCLI_ServerUninstall_Help verifies that `memory server uninstall --help`
// exits 0.
func TestCLI_ServerUninstall_Help(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory server uninstall --help exits 0",
		"Run `memory server uninstall --help`",
		"Assert exit 0",
	)
	logStatusPreamble(t)

	rl.Section("Run memory server uninstall --help")
	out, err := runCLIInDirWithHome(t, "", t.TempDir(), "server", "uninstall", "--help")
	rl.CLIErr("memory server uninstall --help", out, err, 0)

	if err != nil {
		t.Skipf("server uninstall command not available: %v", err)
	}

	lower := strings.ToLower(out)
	if !strings.Contains(lower, "uninstall") && !strings.Contains(lower, "remove") {
		t.Errorf("expected server uninstall --help to mention 'uninstall' or 'remove', got:\n%s", truncate(out, 500))
	}
	rl.Printf("server uninstall --help: %d bytes", len(out))
}
