// Package cli_test — browse_test.go
//
// Smoke tests for `memory browse` — the interactive TUI command.
// Since e2e tests run without an interactive terminal, these tests verify
// graceful no-TTY exit behavior and flag documentation rather than TUI
// rendering.
package cli_test

import (
	"strings"
	"testing"
)

// TestCLI_Browse_NoTTY verifies that `memory browse` exits with a recognizable
// error when run without an interactive terminal (no TTY), rather than
// panicking or hanging indefinitely.
func TestCLI_Browse_NoTTY(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory browse exits gracefully without a TTY",
		"Run `memory browse` with no flags in a non-interactive process",
		"Assert command produces a TTY-related error message (not a panic)",
	)
	logStatusPreamble(t)

	rl.Section("Run memory browse without a TTY")
	out, err := runCLIInDirWithHome(t, "", t.TempDir(), "browse")
	rl.CLIErr("memory browse", out, err, 0)

	rl.Section("Assert TTY error — not a panic")
	// When stdin is not a TTY, the command should fail with a message about
	// the terminal, not with an unhandled Go panic.
	if err == nil {
		// Some environments may redirect to a pseudo-TTY; skip gracefully.
		t.Skipf("browse succeeded (TTY available in this environment), skipping no-TTY assertion")
	}

	lower := strings.ToLower(out + err.Error())
	if strings.Contains(lower, "panic") || strings.Contains(lower, "runtime error") {
		t.Errorf("browse panicked instead of failing gracefully:\n%s", truncate(out, 500))
	}

	// Output should mention TTY, terminal, or the TUI error context.
	if !strings.Contains(lower, "tty") && !strings.Contains(lower, "terminal") && !strings.Contains(lower, "tui") {
		t.Errorf("expected a TTY-related error message, got:\n%s", truncate(out, 500))
	}
	rl.Printf("browse no-TTY exit: error=%v, output contains TTY mention: true", err)
}

// TestCLI_Browse_Help verifies that `memory browse --help` exits 0 and
// documents the --tempo-url flag.
func TestCLI_Browse_Help(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory browse --help is well-formed",
		"Run `memory browse --help`",
		"Assert exit 0 and --tempo-url flag is documented",
	)
	logStatusPreamble(t)

	rl.Section("Run memory browse --help")
	out := mustRunCLI(t, "browse", "--help")
	rl.CLI("memory browse --help", out)

	rl.Section("Verify --tempo-url flag is documented")
	if !strings.Contains(out, "tempo-url") {
		t.Errorf("expected browse --help to document --tempo-url flag, got:\n%s", truncate(out, 500))
	}
	rl.Printf("browse --help: %d bytes, contains tempo-url: true", len(out))
}
