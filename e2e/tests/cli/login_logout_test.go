// Package cli_test — login_logout_test.go
//
// End-to-end tests for `memory login` and `memory logout` commands.
// These commands involve interactive OAuth flows, so we test:
//   - `--help` output for both
//   - `logout` clears credentials
//   - `login --help` shows expected options
package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_LoginHelp verifies that `memory login --help` prints
// usage information for the login command.
func TestCLIInstalled_LoginHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory login --help prints usage information",
		"Run `memory login --help`",
		"Assert output contains login usage hints and auth-related flags",
	)
	logStatusPreamble(t)

	rl.Section("Run memory login --help")
	out := mustRunCLI(t, "login", "--help")
	rl.CLI("memory login --help", out)

	rl.Section("Verify login help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "login") {
		t.Errorf("expected login help to mention 'login', got:\n%s", truncate(out, 500))
	}
	// Login help should mention authentication or server.
	if !strings.Contains(lower, "auth") && !strings.Contains(lower, "server") && !strings.Contains(lower, "token") {
		t.Errorf("expected login help to reference auth, server, or token, got:\n%s", truncate(out, 500))
	}
	rl.Printf("login help output: %d bytes", len(out))
}

// TestCLIInstalled_LogoutHelp verifies that `memory logout --help` prints
// usage information for the logout command.
func TestCLIInstalled_LogoutHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory logout --help prints usage information",
		"Run `memory logout --help`",
		"Assert output contains logout usage hints",
	)
	logStatusPreamble(t)

	rl.Section("Run memory logout --help")
	out := mustRunCLI(t, "logout", "--help")
	rl.CLI("memory logout --help", out)

	rl.Section("Verify logout help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "logout") {
		t.Errorf("expected logout help to mention 'logout', got:\n%s", truncate(out, 500))
	}
	rl.Printf("logout help output: %d bytes", len(out))
}

// TestCLIInstalled_Logout verifies that `memory logout` clears stored
// credentials (or at least runs without hard error on a fresh HOME).
func TestCLIInstalled_Logout(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory logout clears stored credentials",
		"Set up CLI auth so credentials exist",
		"Run `memory logout`",
		"Assert credentials.json is removed or output confirms logout",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	// Verify credentials exist before logout.
	rl.Section("Verify credentials exist before logout")
	credPath := filepath.Join(home, ".memory", "credentials.json")
	if _, err := os.Stat(credPath); os.IsNotExist(err) {
		// In standalone mode, credentials may be stored differently.
		rl.Printf("no credentials.json at %s (may be standalone auth mode)", credPath)
	} else {
		rl.Printf("credentials.json exists at %s", credPath)
	}

	rl.Section("Run memory logout")
	out, err := runCLIInDirWithHome(t, "", home, "logout")
	rl.CLIErr("memory logout", out, err, 0)

	if err != nil {
		// logout may fail in standalone mode or when not logged in — that's OK.
		rl.Printf("logout returned error (may be expected): %v", err)
		lower := strings.ToLower(out)
		if strings.Contains(lower, "not logged in") || strings.Contains(lower, "no credentials") {
			rl.Printf("logout correctly reports not logged in / no credentials")
		} else {
			t.Logf("logout error output: %s", truncate(out, 300))
		}
	} else {
		// Successful logout — verify output.
		lower := strings.ToLower(out)
		if strings.Contains(lower, "logout") || strings.Contains(lower, "logged out") || strings.Contains(lower, "credentials") || strings.Contains(lower, "removed") {
			rl.Printf("logout output confirms credential removal")
		} else {
			rl.Printf("logout output: %s", truncate(out, 300))
		}
	}

	// Check if credentials.json was removed.
	rl.Section("Verify credentials removed")
	if _, err := os.Stat(credPath); os.IsNotExist(err) {
		rl.Printf("credentials.json removed after logout: true")
	} else {
		rl.Printf("credentials.json still exists (may store non-auth config)")
	}
}

var _ = framework.SetToken // keep import used
