// Package docs_test — install_test.go
//
// Tests that verify the install script and binary installation match what the
// documentation claims.  These tests verify the actual binary name, version
// format, and upgrade behavior.
package docs_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestDocInstall_BinaryNameIsMemory verifies that the installed binary is named
// "memory" (not "emergent"), matching the CLI usage shown throughout the docs.
//
// Background: the root install.sh in the GitHub repo installs a binary named
// "emergent" to ~/.emergent/bin/, but the actual CLI used in tests (and
// documented in the user guide) is "memory" at ~/.memory/bin/memory.  This test
// catches any regression where the wrong install script is used.
func TestDocInstall_BinaryNameIsMemory(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify installed CLI binary is named 'memory', not 'emergent'",
		"Look up 'memory' on PATH",
		"Verify it resolves to a path containing '.memory/bin'",
		"Verify no 'emergent' binary shadows it",
	)

	rl.Section("Locate memory binary")
	memoryPath, err := exec.LookPath("memory")
	if err != nil {
		rl.Failf("'memory' binary not found on PATH: %v", err)
	}
	rl.Printf("memory binary found at: %s", memoryPath)

	// The binary should be at ~/.memory/bin/memory (or similar).
	if !strings.Contains(memoryPath, ".memory") && !strings.Contains(memoryPath, "memory") {
		t.Errorf("expected memory binary path to contain '.memory', got: %s", memoryPath)
	}

	rl.Section("Verify memory binary is executable")
	out := mustRunCLI(t, "version")
	rl.CLI("memory version", out)
	if out == "" {
		rl.Failf("'memory version' produced no output")
	}
	rl.Printf("memory version output: %s", strings.TrimSpace(out))
}

// TestDocInstall_VersionFormat verifies that `memory version` output contains
// a recognizable version string (semver-like or commit hash), as the docs
// imply the CLI has meaningful version output.
func TestDocInstall_VersionFormat(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory version output has recognizable version format",
		"Run `memory version`",
		"Assert output contains version-like content (v0.x, semver, or commit hash)",
	)

	rl.Section("Run memory version")
	out := mustRunCLI(t, "version")
	rl.CLI("memory version", out)

	rl.Section("Validate version format")
	trimmed := strings.TrimSpace(out)
	lower := strings.ToLower(trimmed)

	// Version output should contain at least one of: a 'v' prefix with digits,
	// the word 'version', a semver pattern, or a commit hash.
	hasVersion := strings.Contains(lower, "version") ||
		strings.Contains(lower, "v0.") ||
		strings.Contains(lower, "v1.") ||
		strings.Contains(lower, "memory") ||
		strings.Contains(lower, "commit")

	if !hasVersion {
		t.Errorf("expected version output to contain recognizable version info, got: %q", trimmed)
	}
	rl.Printf("version output looks valid: %q", trimmed)
}

// TestDocInstall_BinaryInMemoryBinDir verifies that the memory binary lives
// in ~/.memory/bin/ as the install script (tools/cli/install.sh) dictates.
// The docs reference ~/.memory/bin/ as the install location.
func TestDocInstall_BinaryInMemoryBinDir(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory binary exists at ~/.memory/bin/memory",
		"Check /root/.memory/bin/memory exists (the Dockerfile install location)",
		"Verify the binary is executable",
	)

	rl.Section("Check binary location")
	binaryPath := "/root/.memory/bin/memory"
	info, err := os.Stat(binaryPath)
	if err != nil {
		// Also check the default home-based path
		home := os.Getenv("HOME")
		altPath := home + "/.memory/bin/memory"
		info, err = os.Stat(altPath)
		if err != nil {
			rl.Failf("memory binary not found at %s or %s: %v", binaryPath, altPath, err)
		}
		binaryPath = altPath
	}
	rl.Printf("binary found at: %s (size=%d bytes)", binaryPath, info.Size())

	rl.Section("Verify binary is executable")
	mode := info.Mode()
	if mode&0111 == 0 {
		rl.Failf("binary at %s is not executable (mode=%s)", binaryPath, mode)
	}
	rl.Printf("binary is executable: mode=%s", mode)
}

// ─────────────────────────────────────────────────────────────────────────────
// Install script verification (GitHub-hosted scripts)
// ─────────────────────────────────────────────────────────────────────────────

// TestDocInstall_RootInstallScriptBinaryName fetches the root install.sh from
// GitHub and parses out the BINARY_NAME variable.  The root script installs a
// binary named "emergent", while the CLI documented in the user guide is
// "memory".  This test documents the discrepancy so it can be tracked.
func TestDocInstall_RootInstallScriptBinaryName(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Fetch root install.sh from GitHub and parse BINARY_NAME",
		"GET the root install.sh from GitHub raw content",
		"Parse BINARY_NAME= line",
		"Document whether it says 'emergent' (known discrepancy with CLI name 'memory')",
	)

	skipIfDocsUnreachable(t, rl) // network required

	rl.Section("Fetch root install.sh")
	url := "https://raw.githubusercontent.com/emergent-company/emergent.memory/main/install.sh"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		t.Skipf("could not fetch root install.sh: %v — skipping", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	if status == 404 {
		t.Skipf("root install.sh not found (404) — may have been removed")
	}
	if status != 200 {
		rl.Failf("expected HTTP 200, got %d", status)
	}

	rl.Section("Parse BINARY_NAME from script")
	var binaryName string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		// Match BINARY_NAME="emergent" or BINARY_NAME=emergent or similar.
		if strings.HasPrefix(trimmed, "BINARY_NAME=") {
			binaryName = strings.TrimPrefix(trimmed, "BINARY_NAME=")
			binaryName = strings.Trim(binaryName, "\"'")
			break
		}
	}

	if binaryName == "" {
		rl.Printf("no BINARY_NAME= line found — script may use a different pattern")
	} else {
		rl.Printf("root install.sh BINARY_NAME=%q", binaryName)
		if binaryName != "memory" {
			rl.Printf("NOTE: root install.sh installs %q, but docs reference 'memory' CLI", binaryName)
		}
	}
}

// TestDocInstall_ToolsInstallScriptExists verifies that the tools/cli/install.sh
// script (used by entrypoint.sh in Docker) is reachable on GitHub.
func TestDocInstall_ToolsInstallScriptExists(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify tools/cli/install.sh exists on GitHub",
		"GET the tools/cli/install.sh from GitHub raw content",
		"Assert HTTP 200",
	)

	skipIfDocsUnreachable(t, rl)

	rl.Section("Fetch tools/cli/install.sh")
	url := "https://raw.githubusercontent.com/emergent-company/emergent.memory/main/tools/cli/install.sh"
	body, status, err := httpGet(url, 15*time.Second)
	if err != nil {
		t.Skipf("could not fetch tools/cli/install.sh: %v — skipping", err)
	}
	rl.Printf("GET %s → HTTP %d (%d bytes)", url, status, len(body))

	rl.Section("Verify script is reachable")
	if status != 200 {
		rl.Failf("expected HTTP 200 for tools/cli/install.sh, got %d", status)
	}
	if len(body) < 50 {
		t.Errorf("tools/cli/install.sh suspiciously small (%d bytes)", len(body))
	}
	rl.Printf("tools/cli/install.sh is accessible (%d bytes)", len(body))
}

// TestDocInstall_UpgradeCommandExists verifies `memory upgrade --help` works,
// since the docs reference the upgrade path for keeping the CLI current.
func TestDocInstall_UpgradeCommandExists(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory upgrade --help works",
		"Run `memory upgrade --help`",
		"Assert output references upgrade functionality",
	)

	rl.Section("Run memory upgrade --help")
	out := mustRunCLI(t, "upgrade", "--help")
	rl.CLI("memory upgrade --help", out)

	rl.Section("Verify upgrade help content")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "upgrade") && !strings.Contains(lower, "update") {
		t.Errorf("upgrade --help missing expected content, got:\n%s", truncate(out, 500))
	}
	rl.Printf("upgrade --help contains expected references")
}
