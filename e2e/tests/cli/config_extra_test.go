// Package cli_test — config_extra_test.go
//
// End-to-end tests for `memory config set-server` and `memory config
// set-credentials` CLI subcommands.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_ConfigSetServer verifies that `memory config set-server`
// updates the server URL in the configuration file.
func TestCLIInstalled_ConfigSetServer(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify config set-server updates server URL",
		"Run `memory config set-server <url>` with a test URL",
		"Run `memory config show` and verify the URL is reflected",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	testURL := serverURL()

	rl.Section("Run config set-server")
	out := mustRunCLIInDirWithHome(t, "", home,
		"config", "set-server", testURL,
	)
	rl.CLI("memory config set-server "+testURL, out)

	if !strings.Contains(out, testURL) {
		t.Errorf("expected set-server output to confirm URL %q, got:\n%s", testURL, truncate(out, 500))
	}
	rl.Printf("set-server confirmed URL: %s", testURL)

	// Verify via config show.
	rl.Section("Verify via config show")
	showOut := mustRunCLIInDirWithHome(t, "", home,
		"config", "show",
	)
	rl.CLI("memory config show", showOut)

	if !strings.Contains(showOut, testURL) {
		t.Errorf("expected config show to contain server URL %q, got:\n%s", testURL, truncate(showOut, 500))
	}
	rl.Printf("config show contains server URL: true")
}

// TestCLIInstalled_ConfigSetCredentials verifies that `memory config
// set-credentials` updates the email in the configuration file.
func TestCLIInstalled_ConfigSetCredentials(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify config set-credentials updates email",
		"Run `memory config set-credentials <email>` with a test email",
		"Run `memory config show` and verify email is reflected",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	testEmail := "e2e-test@example.com"

	rl.Section("Run config set-credentials")
	out := mustRunCLIInDirWithHome(t, "", home,
		"config", "set-credentials", testEmail,
	)
	rl.CLI("memory config set-credentials "+testEmail, out)

	if !strings.Contains(out, testEmail) {
		t.Errorf("expected set-credentials output to confirm email, got:\n%s", truncate(out, 500))
	}
	rl.Printf("set-credentials confirmed email: %s", testEmail)

	// Verify via config show.
	rl.Section("Verify via config show")
	showOut := mustRunCLIInDirWithHome(t, "", home,
		"config", "show",
	)
	rl.CLI("memory config show", showOut)

	if !strings.Contains(showOut, testEmail) {
		t.Errorf("expected config show to contain email %q, got:\n%s", testEmail, truncate(showOut, 500))
	}
	rl.Printf("config show contains email: true")
}

var _ = framework.SetToken // keep import used
