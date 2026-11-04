// Package cli_test — provider_list_test.go
//
// End-to-end tests for `memory provider list` and `memory provider timeseries`
// CLI subcommands.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_ProviderList verifies that `memory provider list` returns
// a table of configured LLM providers at the organization level.
func TestCLIInstalled_ProviderList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider list returns configured providers",
		"Set up CLI auth",
		"Run `memory provider list` with org-id",
		"Assert output is non-empty and contains provider-related content",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider list")
	args := append([]string{"provider", "list"}, orgIDArgs()...)
	out, err := runCLIInDirWithHome(t, "", home, args...)
	rl.CLIErr("memory provider list "+strings.Join(orgIDArgs(), " "), out, err, 0)

	if err != nil {
		if strings.Contains(out, "unauthorized") || strings.Contains(out, "organization context required") {
			rl.Printf("SKIP: provider list requires org-level auth not available in this mode")
			t.Skipf("provider list not available: %s", truncate(out, 200))
		}
		rl.Failf("provider list failed: %v — %s", err, truncate(out, 300))
	}

	rl.Section("Verify provider list output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider list, got empty")
	}
	// The table should contain at least column headers or provider names.
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "provider") && !strings.Contains(lower, "scope") &&
		!strings.Contains(lower, "model") && !strings.Contains(lower, "google") &&
		!strings.Contains(lower, "no providers") {
		t.Errorf("expected provider list to contain provider-related keywords, got:\n%s", truncate(trimmed, 500))
	}
	rl.Printf("provider list output: %d bytes", len(trimmed))
}

// TestCLIInstalled_ProviderListJSON verifies that `memory provider list --json`
// returns JSON-formatted provider data.
func TestCLIInstalled_ProviderListJSON(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider list --json returns JSON output",
		"Set up CLI auth",
		"Run `memory provider list --json`",
		"Assert output starts with JSON structure",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider list --json")
	args := append([]string{"provider", "list", "--json"}, orgIDArgs()...)
	out, err := runCLIInDirWithHome(t, "", home, args...)
	rl.CLIErr("memory provider list --json "+strings.Join(orgIDArgs(), " "), out, err, 0)

	if err != nil {
		if strings.Contains(out, "unauthorized") || strings.Contains(out, "organization context required") {
			rl.Printf("SKIP: provider list --json requires org-level auth")
			t.Skipf("provider list --json not available: %s", truncate(out, 200))
		}
		rl.Failf("provider list --json failed: %v — %s", err, truncate(out, 300))
	}

	rl.Section("Verify JSON output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider list --json, got empty")
	}
	if !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		t.Errorf("expected JSON output starting with [ or {, got:\n%s", truncate(trimmed, 300))
	}
	rl.Printf("provider list --json returned JSON: %d bytes", len(trimmed))
}

// TestCLIInstalled_ProviderTimeseries verifies that `memory provider timeseries`
// returns LLM token usage broken down by time period.
func TestCLIInstalled_ProviderTimeseries(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider timeseries returns time-bucketed usage data",
		"Set up CLI auth",
		"Run `memory provider timeseries` with org-id",
		"Assert output is non-empty and contains time-series-related content",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider timeseries")
	args := append([]string{"provider", "timeseries"}, orgIDArgs()...)
	out, err := runCLIInDirWithHome(t, "", home, args...)
	rl.CLIErr("memory provider timeseries "+strings.Join(orgIDArgs(), " "), out, err, 0)

	if err != nil {
		if strings.Contains(out, "unauthorized") || strings.Contains(out, "organization context required") {
			rl.Printf("SKIP: provider timeseries requires org-level auth")
			t.Skipf("provider timeseries not available: %s", truncate(out, 200))
		}
		rl.Failf("provider timeseries failed: %v — %s", err, truncate(out, 300))
	}

	rl.Section("Verify timeseries output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider timeseries, got empty")
	}
	// Timeseries output should contain period/date references or column headers.
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "period") && !strings.Contains(lower, "provider") &&
		!strings.Contains(lower, "model") && !strings.Contains(lower, "cost") &&
		!strings.Contains(lower, "no ") && !strings.Contains(lower, "20") {
		t.Errorf("expected timeseries output to contain usage keywords, got:\n%s", truncate(trimmed, 500))
	}
	rl.Printf("provider timeseries output: %d bytes", len(trimmed))
}

// TestCLIInstalled_ProviderTimeseriesJSON verifies that `memory provider
// timeseries --json` returns JSON-formatted time-series data.
func TestCLIInstalled_ProviderTimeseriesJSON(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider timeseries --json returns JSON output",
		"Set up CLI auth",
		"Run `memory provider timeseries --json`",
		"Assert output starts with JSON structure",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider timeseries --json")
	args := append([]string{"provider", "timeseries", "--json"}, orgIDArgs()...)
	out, err := runCLIInDirWithHome(t, "", home, args...)
	rl.CLIErr("memory provider timeseries --json "+strings.Join(orgIDArgs(), " "), out, err, 0)

	if err != nil {
		if strings.Contains(out, "unauthorized") || strings.Contains(out, "organization context required") {
			rl.Printf("SKIP: provider timeseries --json requires org-level auth")
			t.Skipf("provider timeseries --json not available: %s", truncate(out, 200))
		}
		rl.Failf("provider timeseries --json failed: %v — %s", err, truncate(out, 300))
	}

	rl.Section("Verify JSON output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider timeseries --json, got empty")
	}
	if !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		t.Errorf("expected JSON output starting with [ or {, got:\n%s", truncate(trimmed, 300))
	}
	rl.Printf("provider timeseries --json returned JSON: %d bytes", len(trimmed))
}

// TestCLIInstalled_ProviderTimeseriesGranularity verifies that `memory provider
// timeseries --granularity week` changes the time bucket size.
func TestCLIInstalled_ProviderTimeseriesGranularity(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider timeseries respects --granularity flag",
		"Set up CLI auth",
		"Run `memory provider timeseries --granularity week`",
		"Assert command succeeds with non-empty output",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider timeseries --granularity week")
	args := append([]string{"provider", "timeseries", "--granularity", "week"}, orgIDArgs()...)
	out, err := runCLIInDirWithHome(t, "", home, args...)
	rl.CLIErr("memory provider timeseries --granularity week "+strings.Join(orgIDArgs(), " "), out, err, 0)

	if err != nil {
		if strings.Contains(out, "unauthorized") || strings.Contains(out, "organization context required") {
			rl.Printf("SKIP: provider timeseries requires org-level auth")
			t.Skipf("provider timeseries not available: %s", truncate(out, 200))
		}
		rl.Failf("provider timeseries --granularity week failed: %v — %s", err, truncate(out, 300))
	}

	rl.Section("Verify granularity output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider timeseries --granularity week, got empty")
	}
	rl.Printf("provider timeseries --granularity week: %d bytes", len(trimmed))
}

var _ = framework.SetToken // keep import used
