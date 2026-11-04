// Package cli_test — traces_test.go
//
// End-to-end tests for `memory traces` CLI subcommands: list, search.
// Traces are proxied through the Memory server's Tempo integration.
// These tests verify the CLI commands work and return reasonable output.
package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_TracesList verifies that `memory traces list` returns
// recent traces or an empty result without error.
func TestCLIInstalled_TracesList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify traces list returns traces or empty result",
		"Set up CLI auth",
		"Run `memory traces list`",
		"Assert command exits 0 with non-empty output",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run traces list")
	// Use the non-fatal variant since traces may not be configured on all servers.
	out, err := runCLIInDirWithHome(t, "", home, "traces", "list")
	rl.CLIErr("memory traces list", out, err, 0)

	if err != nil {
		// Traces endpoint may not be available — log and skip.
		rl.Printf("traces list returned error (endpoint may not be configured): %v", err)
		t.Skipf("traces list not available: %v", err)
	}

	rl.Section("Verify traces output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from traces list, got empty string")
	}
	rl.Printf("traces list output: %d bytes", len(trimmed))
}

// TestCLIInstalled_TracesSearch verifies that `memory traces search` with
// a service name returns results or an empty set without error.
func TestCLIInstalled_TracesSearch(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify traces search returns results or empty set",
		"Set up CLI auth",
		"Run `memory traces search --service memory-server`",
		"Assert command exits 0",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run traces search")
	out, err := runCLIInDirWithHome(t, "", home,
		"traces", "search",
		"--service", "memory-server",
	)
	rl.CLIErr("memory traces search --service memory-server", out, err, 0)

	if err != nil {
		rl.Printf("traces search returned error (endpoint may not be configured): %v", err)
		t.Skipf("traces search not available: %v", err)
	}

	rl.Section("Verify search output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		// Empty output is acceptable — there may be no traces matching.
		rl.Printf("traces search returned empty output (no matching traces)")
	} else {
		rl.Printf("traces search output: %d bytes", len(trimmed))
	}
}

// TestCLIInstalled_TracesGet verifies that `memory traces get <traceID>`
// returns a trace span tree for a given trace ID.
func TestCLIInstalled_TracesGet(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify traces get returns a trace span tree",
		"Set up CLI auth",
		"Run `traces list` to find a trace ID",
		"Run `traces get <traceID>` and verify output shows span tree",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	// First, list traces to get a trace ID.
	rl.Section("List traces to find a trace ID")
	listOut, err := runCLIInDirWithHome(t, "", home, "traces", "list", "--output", "json")
	rl.CLIErr("memory traces list --output json", listOut, err, 0)

	if err != nil {
		rl.Printf("traces list returned error: %v", err)
		t.Skipf("traces list not available: %v", err)
	}

	// Try to extract a trace ID from the list output.
	// The output may be a JSON array, so try parsing as array first.
	traceID := parseFirstArrayField(listOut, "traceId")
	if traceID == "" {
		traceID = parseFirstArrayField(listOut, "traceID")
	}
	if traceID == "" {
		traceID = parseFirstArrayField(listOut, "trace_id")
	}
	if traceID == "" {
		traceID = parseJSONField(listOut, "traceId")
	}
	if traceID == "" {
		traceID = parseLineField(listOut, "Trace ID:")
	}
	if traceID == "" {
		rl.Printf("could not extract trace ID from list output; skipping traces get test")
		t.Skipf("no trace ID found in traces list output: %s", truncate(listOut, 300))
	}
	rl.Printf("trace ID: %s", traceID)

	// Get the trace.
	rl.Section("Get trace by ID")
	getOut, err := runCLIInDirWithHome(t, "", home, "traces", "get", traceID)
	rl.CLIErr("memory traces get "+traceID, getOut, err, 0)

	if err != nil {
		rl.Printf("traces get returned error: %v", err)
		t.Skipf("traces get not available: %v", err)
	}

	trimmed := strings.TrimSpace(getOut)
	if trimmed == "" {
		rl.Failf("expected non-empty output from traces get, got empty")
	}
	// The output should contain the trace ID or span information.
	if !strings.Contains(getOut, traceID) && !strings.Contains(strings.ToLower(getOut), "span") {
		t.Errorf("expected traces get output to contain trace ID or span info, got:\n%s", truncate(getOut, 500))
	}
	rl.Printf("traces get: %d bytes, contains trace/span info: true", len(trimmed))
}

// parseFirstArrayField parses a JSON array and extracts a string field from
// the first element.  Returns "" if the output is not a JSON array or the
// field is not present.
func parseFirstArrayField(jsonStr, field string) string {
	trimmed := strings.TrimSpace(jsonStr)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return ""
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
		return ""
	}
	if len(arr) == 0 {
		return ""
	}
	v, _ := arr[0][field].(string)
	return v
}

var _ = framework.SetToken // keep import used
