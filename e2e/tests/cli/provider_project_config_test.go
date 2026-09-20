// Package cli_test — provider_project_config_test.go
//
// End-to-end tests for `memory provider configure-project` CLI subcommand.
// Tests the lifecycle of project-level provider overrides: configure → verify
// via provider list → remove.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_ProviderConfigureProjectHelp verifies that `memory provider
// configure-project --help` prints usage information.
func TestCLIInstalled_ProviderConfigureProjectHelp(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider configure-project --help prints usage",
		"Run `memory provider configure-project --help`",
		"Assert output contains usage and flags",
	)
	logStatusPreamble(t)

	rl.Section("Run provider configure-project --help")
	out := mustRunCLIInDirWithHome(t, "", t.TempDir(), "provider", "configure-project", "--help")
	rl.CLI("memory provider configure-project --help", out)

	rl.Section("Verify help output")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "configure-project") && !strings.Contains(lower, "project") {
		t.Errorf("expected help to mention 'configure-project', got:\n%s", truncate(out, 500))
	}
	if !strings.Contains(lower, "--remove") {
		t.Errorf("expected help to mention --remove flag, got:\n%s", truncate(out, 500))
	}
	rl.Printf("provider configure-project help: %d bytes, contains expected flags", len(out))
}

// TestCLIInstalled_ProviderConfigureProjectRemoveNoop verifies that
// `memory provider configure-project <provider> --remove` works on a project
// that has no override (should succeed without error or report "no override").
func TestCLIInstalled_ProviderConfigureProjectRemoveNoop(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)

	// Use whichever provider is configured so the command is meaningful.
	provider, _, _ := framework.ProviderFromEnv()
	if provider == "" {
		provider = "google" // fall back to google for the --remove noop (no credentials needed)
	}

	rl.Describe("Verify provider configure-project --remove on project with no override",
		"Create a fresh project",
		"Run `memory provider configure-project <provider> --remove`",
		"Assert command succeeds or reports no override exists",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-prov-rm")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Remove non-existent project provider override")
	out, err := runCLIInDirWithHome(t, "", home,
		"provider", "configure-project", provider,
		"--remove",
		"--project", projectID,
	)
	rl.CLIErr("memory provider configure-project "+provider+" --remove --project "+projectID, out, err, 0)

	if err != nil {
		// Some servers may return an error if no override exists; that's acceptable.
		lower := strings.ToLower(out)
		if strings.Contains(lower, "not found") || strings.Contains(lower, "no override") ||
			strings.Contains(lower, "no project") || strings.Contains(lower, "does not exist") {
			rl.Printf("remove returned expected 'not found/no override' message")
		} else if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "organization context required") {
			rl.Printf("SKIP: configure-project requires auth not available in this mode")
			t.Skipf("provider configure-project not available: %s", truncate(out, 200))
		} else {
			// Log but don't fail — the CLI might legitimately error on removing
			// a non-existent override depending on server version.
			rl.Printf("remove returned unexpected error: %v — %s", err, truncate(out, 300))
			t.Errorf("unexpected error removing non-existent override: %v — %s", err, truncate(out, 300))
		}
	} else {
		rl.Printf("remove on empty project succeeded: %s", truncate(out, 200))
	}
}

var _ = framework.SetToken // keep import used
