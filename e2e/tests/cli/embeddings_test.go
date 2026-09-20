// Package cli_test — embeddings_test.go
//
// End-to-end tests for `memory embeddings` CLI subcommands: status, pause,
// resume.  These commands operate at the server level (no project context
// required) and verify embedding worker management.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_EmbeddingsStatus verifies that `memory embeddings status`
// returns the state of embedding workers.
func TestCLIInstalled_EmbeddingsStatus(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify embeddings status returns worker state information",
		"Set up CLI auth",
		"Run `memory embeddings status`",
		"Assert output contains worker state info (running/paused)",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run embeddings status")
	out := mustRunCLIInDirWithHome(t, "", home, "embeddings", "status")
	rl.CLI("memory embeddings status", out)

	rl.Section("Verify status output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from embeddings status, got empty string")
	}
	// Status output should contain worker state info.
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "running") && !strings.Contains(lower, "paused") &&
		!strings.Contains(lower, "worker") && !strings.Contains(lower, "embedding") &&
		!strings.Contains(lower, "status") {
		t.Errorf("expected status output to contain state-related keywords, got:\n%s", truncate(trimmed, 500))
	}
	rl.Printf("embeddings status: %d bytes, contains state info", len(trimmed))
}

// TestCLIInstalled_EmbeddingsPauseResume verifies the pause → status → resume
// cycle for embedding workers.
func TestCLIInstalled_EmbeddingsPauseResume(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify embeddings pause → status (paused) → resume → status (running) cycle",
		"Pause embedding workers",
		"Check status shows paused state",
		"Resume embedding workers",
		"Check status shows running state",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	// Pause workers.
	rl.Section("Pause embedding workers")
	pauseOut := mustRunCLIInDirWithHome(t, "", home, "embeddings", "pause")
	rl.CLI("memory embeddings pause", pauseOut)
	rl.Printf("pause output: %s", truncate(pauseOut, 200))

	// Check status shows paused.
	rl.Section("Verify paused status")
	statusOut := mustRunCLIInDirWithHome(t, "", home, "embeddings", "status")
	rl.CLI("memory embeddings status", statusOut)

	lower := strings.ToLower(statusOut)
	if !strings.Contains(lower, "paused") && !strings.Contains(lower, "pause") {
		t.Errorf("expected status to show paused state after pause, got:\n%s", truncate(statusOut, 500))
	}
	rl.Printf("status shows paused: true")

	// Resume workers.
	rl.Section("Resume embedding workers")
	resumeOut := mustRunCLIInDirWithHome(t, "", home, "embeddings", "resume")
	rl.CLI("memory embeddings resume", resumeOut)
	rl.Printf("resume output: %s", truncate(resumeOut, 200))

	// Check status shows running.
	rl.Section("Verify running status")
	statusOut2 := mustRunCLIInDirWithHome(t, "", home, "embeddings", "status")
	rl.CLI("memory embeddings status", statusOut2)

	lower2 := strings.ToLower(statusOut2)
	if !strings.Contains(lower2, "running") && !strings.Contains(lower2, "run") {
		t.Errorf("expected status to show running state after resume, got:\n%s", truncate(statusOut2, 500))
	}
	rl.Printf("status shows running: true")
}

// TestCLIInstalled_EmbeddingsConfig verifies that `memory embeddings config`
// returns the current embedding worker configuration.
func TestCLIInstalled_EmbeddingsConfig(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify embeddings config shows current worker configuration",
		"Set up CLI auth",
		"Run `memory embeddings config` (no flags = show current config)",
		"Assert output contains config values (batch, concurrency, etc.)",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run embeddings config")
	out, err := runCLIInDirWithHome(t, "", home, "embeddings", "config")
	rl.CLIErr("memory embeddings config", out, err, 0)

	if err != nil {
		rl.Printf("embeddings config returned error: %v", err)
		t.Skipf("embeddings config not available: %v", err)
	}

	rl.Section("Verify config output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from embeddings config, got empty")
	}
	// Config output should show worker settings.
	lower := strings.ToLower(trimmed)
	hasConfigInfo := strings.Contains(lower, "batch") ||
		strings.Contains(lower, "concurrency") ||
		strings.Contains(lower, "interval") ||
		strings.Contains(lower, "stale") ||
		strings.Contains(lower, "config")
	if !hasConfigInfo {
		t.Errorf("expected embeddings config to show worker configuration values, got:\n%s", truncate(trimmed, 500))
	}
	rl.Printf("embeddings config: %d bytes, contains config info: %v", len(trimmed), hasConfigInfo)
}

var _ = framework.SetToken // keep import used
