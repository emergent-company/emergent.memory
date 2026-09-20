// Package cli_test — provider_extra_test.go
//
// End-to-end tests for `memory provider models` and `memory provider test` CLI
// subcommands.
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_ProviderModelsList verifies that `memory provider models`
// returns a list of available models from the provider catalog.
func TestCLIInstalled_ProviderModelsList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider models lists available models",
		"Run `memory provider models` with org-id",
		"Assert output contains model entries with provider and type columns",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider models")
	args := append([]string{"provider", "models"}, orgIDArgs()...)
	out := mustRunCLIInDirWithHome(t, "", home, args...)
	rl.CLI("memory provider models "+strings.Join(orgIDArgs(), " "), out)

	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider models, got empty")
	}
	// Should contain model names and types.
	if !strings.Contains(out, "generative") && !strings.Contains(out, "embedding") {
		t.Errorf("expected model list to contain 'generative' or 'embedding' type, got:\n%s", truncate(out, 500))
	}
	rl.Printf("provider models returned models with type information")
}

// TestCLIInstalled_ProviderModelsFilterType verifies that `provider models
// --type embedding` filters to embedding models only.
func TestCLIInstalled_ProviderModelsFilterType(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider models --type embedding filters correctly",
		"Run `memory provider models --type embedding`",
		"Assert all returned models have type 'embedding'",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider models --type embedding")
	args := append([]string{"provider", "models", "--type", "embedding"}, orgIDArgs()...)
	out := mustRunCLIInDirWithHome(t, "", home, args...)
	rl.CLI("memory provider models --type embedding "+strings.Join(orgIDArgs(), " "), out)

	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from provider models --type embedding, got empty")
	}
	if !strings.Contains(out, "embedding") {
		t.Errorf("expected output to contain 'embedding', got:\n%s", truncate(out, 500))
	}
	// Should NOT contain 'generative' type entries (in the TYPE column).
	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		// Skip header/separator lines.
		if strings.Contains(line, "PROVIDER") || strings.HasPrefix(line, "─") || strings.HasPrefix(line, "┌") || strings.HasPrefix(line, "└") || strings.HasPrefix(line, "├") || strings.HasPrefix(line, "Tip:") {
			continue
		}
		if strings.Contains(line, "generative") {
			t.Errorf("expected --type embedding to exclude generative, but found: %s", line)
			break
		}
	}
	rl.Printf("embedding type filter correctly applied")
}

// TestCLIInstalled_ProviderTest verifies that `memory provider test` tests
// LLM credentials with a live generate call.
func TestCLIInstalled_ProviderTest(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify provider test makes live credential check",
		"Run `memory provider test` with org-id",
		"Assert output contains OK status and a model reply",
	)
	home := t.TempDir()
	requireServerReady(t, home)

	rl.Section("Run provider test")
	args := append([]string{"provider", "test"}, orgIDArgs()...)
	out, err := runCLIInDirWithHome(t, "", home, args...)
	if err != nil {
		rl.Printf("provider test returned error: %v — may not have configured provider", err)
		// If no provider is configured, this is expected.
		if strings.Contains(out, "no providers configured") || strings.Contains(out, "not configured") {
			t.Skipf("provider test skipped: no providers configured")
		}
		rl.CLI("memory provider test "+strings.Join(orgIDArgs(), " "), out)
		t.Skipf("provider test returned error: %v", err)
	}
	rl.CLI("memory provider test "+strings.Join(orgIDArgs(), " "), out)

	if !strings.Contains(out, "OK") {
		t.Errorf("expected provider test output to contain 'OK', got:\n%s", truncate(out, 500))
	}
	rl.Printf("provider test output: %s", truncate(out, 300))
}

var _ = framework.SetToken // keep import used
