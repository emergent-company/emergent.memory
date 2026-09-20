// Package cli_test — provider_test.go
//
// End-to-end tests for `memory provider` CLI subcommands: usage.
// Note: `provider models` has a known bug (#66) where --org-id flag is
// suggested but doesn't exist. `provider configure` and `provider test`
// require actual LLM credentials, so they are tested only when
// GOOGLE_AI_API_KEY is available.
package cli_test

import (
	"strings"
	"testing"
)

// TestCLIInstalled_ProviderUsage verifies that `memory provider usage`
// returns LLM usage data (or an empty table if no usage exists).
func TestCLIInstalled_ProviderUsage(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider usage returns usage data or empty table",
		"Set up CLI auth",
		"Run `memory provider usage --org-id <id>`",
		"Assert output is non-empty (table header or usage rows)",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider usage")
	args := append([]string{"provider", "usage"}, orgIDArgs()...)
	out := mustRunCLIInDirWithHome(t, "", home, args...)
	rl.CLI("memory provider usage", out)

	rl.Section("Verify usage output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider usage, got empty string")
	}
	// Provider usage should contain at least a table header or a "no usage" message.
	// Common column headers: PROVIDER, MODEL, EST. COST
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "provider") && !strings.Contains(lower, "model") &&
		!strings.Contains(lower, "cost") && !strings.Contains(lower, "usage") &&
		!strings.Contains(lower, "no ") {
		t.Errorf("expected usage output to contain usage-related keywords, got:\n%s", truncate(trimmed, 500))
	}
	rl.Printf("provider usage output: %d bytes", len(trimmed))
}

// TestCLIInstalled_ProviderUsageJSON verifies that `memory provider usage --json`
// returns JSON-formatted usage data.
func TestCLIInstalled_ProviderUsageJSON(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider usage --json returns JSON output",
		"Set up CLI auth",
		"Run `memory provider usage --json --org-id <id>`",
		"Assert output starts with JSON structure",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider usage --json")
	args := append([]string{"provider", "usage", "--json"}, orgIDArgs()...)
	out := mustRunCLIInDirWithHome(t, "", home, args...)
	rl.CLI("memory provider usage --json", out)

	rl.Section("Verify JSON output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider usage --json, got empty string")
	}
	// JSON output should start with [ or {
	if !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		t.Errorf("expected JSON output starting with [ or {, got:\n%s", truncate(trimmed, 300))
	}
	rl.Printf("provider usage --json returned JSON output: %d bytes", len(trimmed))
}

// TestCLIInstalled_ProviderUsageWithProject verifies that `memory provider usage
// --project <id>` returns project-specific usage.
//
// Known issue: the server returns [401] "organization context required" even
// when --org-id is provided.  The test skips when this happens.
func TestCLIInstalled_ProviderUsageWithProject(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider usage with --project returns project-scoped usage",
		"Create project",
		"Run `memory provider usage --project <id> --org-id <id>`",
		"Assert command succeeds (exit 0) with non-empty output",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Create project")
	srv := serverURL()
	name := uniqueProjectName("e2e-prov-usage")
	projectID := createProject(t, home, srv, name)
	deleteProjectOnCleanup(t, home, projectID)
	rl.Printf("project: %s (%s)", name, projectID)

	rl.Section("Run provider usage with --project")
	args := append([]string{"provider", "usage", "--project", projectID}, orgIDArgs()...)
	out, err := runCLIInDirWithHome(t, "", home, args...)
	rl.CLIErr("memory provider usage --project "+projectID, out, err, 0)

	if err != nil {
		if strings.Contains(out, "organization context required") || strings.Contains(out, "unauthorized") {
			rl.Printf("SKIP: provider usage --project returns auth error (server bug)")
			t.Skipf("provider usage --project returns auth error (server bug): %s", truncate(out, 200))
		}
		rl.Failf("provider usage --project failed unexpectedly: %v — %s", err, truncate(out, 300))
	}

	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider usage --project, got empty")
	}
	rl.Printf("provider usage with project: %d bytes", len(trimmed))
}
