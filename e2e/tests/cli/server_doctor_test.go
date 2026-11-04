// Package cli_test — server_doctor_test.go
//
// End-to-end tests for `memory server doctor` — health diagnostics.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_ServerDoctor verifies that `memory server doctor` runs
// health diagnostics and reports status.
func TestCLIInstalled_ServerDoctor(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify server doctor runs diagnostics successfully",
		"Run `memory server doctor` against the test server",
		"Assert output contains configuration, connectivity, and auth checks",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run server doctor")
	out, err := runCLIInDirWithHome(t, "", home, "server", "doctor")
	if err != nil {
		rl.Printf("server doctor returned error: %v", err)
		// Doctor may return non-zero if some checks have warnings.
		// That's OK — we still verify the output.
	}
	rl.CLI("memory server doctor", out)

	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from server doctor, got empty")
	}

	// Verify key diagnostic sections appear.
	rl.Section("Verify diagnostic output")
	expectedChecks := []string{
		"configuration",
		"server",
		"authentication",
	}
	for _, check := range expectedChecks {
		if !strings.Contains(strings.ToLower(out), check) {
			t.Errorf("expected doctor output to mention '%s', got:\n%s", check, truncate(out, 500))
		}
	}
	rl.Printf("doctor output contains: configuration, server, authentication")

	// Verify summary section.
	if strings.Contains(out, "passed") || strings.Contains(out, "Checks:") {
		rl.Printf("summary section present")
	} else {
		t.Logf("doctor output may not have standard summary format:\n%s", truncate(out, 500))
	}
	rl.Printf("server doctor completed successfully")
}

var _ = framework.SetToken // keep import used
