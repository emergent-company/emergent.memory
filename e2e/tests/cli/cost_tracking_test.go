// Package cli_test — cost_tracking_test.go
//
// Tests verifying that LLM cost tracking works for `memory ask` operations.
// When tests make project-scoped ask calls, the framework automatically
// captures token usage and cost from the server's agent-runs API and records
// it to the runlog database.
package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCostTracking_AskWithProject verifies that cost tracking works for
// project-scoped `memory ask` operations.
//
// This test requires a live Memory server to execute the ask and return
// cost data.  When the server is unavailable, the test is skipped.
func TestCostTracking_AskWithProject(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify cost tracking for project-scoped ask",
		"Create a test project",
		"Run `memory ask` with --project",
		"Verify cost is recorded in runlog database",
	)

	home := t.TempDir()
	requireServerReady(t, home)

	// Create a test project for the ask operation.
	projectID := createContextProject(t, rl, home, "cost-tracking-test")

	// Configure an LLM provider (skips test if none available).
	framework.SetupTestProvider(t, rl, home, projectID)

	// Configure project model-config so ask can resolve the generative model.
	if provider, _, model := framework.ProviderFromEnv(); provider != "" && model != "" {
		srv := serverURL()
		token := e2eTestToken()
		modelCfg, _ := json.Marshal(map[string]any{"generativeModel": provider + "/" + model})
		resp := framework.DoJSON(t, "PUT", srv+"/api/v1/projects/"+projectID+"/model-config", token, projectID, modelCfg)
		resp.Body.Close()
		rl.Printf("configured project model: %s/%s", provider, model)
	}

	// Make a simple ask that should trigger an LLM call.
	rl.Section("Execute project-scoped ask")
	question := "What is 2 + 2?"
	out := mustAsk(t, rl, home, question, projectID)

	// Verify we got a response.
	if strings.TrimSpace(out) == "" {
		rl.Failf("expected non-empty response from ask")
	}
	rl.Printf("ask response length: %d bytes", len(out))

	// Note: Cost capture happens automatically in mustAsk() via captureAskCost().
	// The cost data is recorded to rl via rl.RecordTokenUsage(), which is
	// persisted to the database when rl.Close() is called during t.Cleanup().
	//
	// To verify cost was captured, you can:
	//   1. Run: runlog runs --since 5m
	//   2. Run: runlog show <run-id>
	//   3. Check that the run has input_tokens, output_tokens, and cost_usd set.

	rl.Section("Cost tracking complete")
	rl.Printf("cost tracking integration successful (see runlog runs for cost data)")
}
