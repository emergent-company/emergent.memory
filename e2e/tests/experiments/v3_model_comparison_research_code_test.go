// Package experiments_test — v3_model_comparison_research_code_test.go
//
// Experiment: compare two Gemini models on the research-and-code task using the
// python-senior-coder-native agent (Google native search, no Brave key needed).
//
// Both test functions set the experiment name "research-and-code-model-comparison"
// so they appear grouped in the Experiments tab of the runlog TUI.
//
// Models under test:
//   - gemini-2.5-flash          (TestResearchAndCodeModelComparison_Flash25)
//   - gemini-3.1-flash-lite-preview (TestResearchAndCodeModelComparison_FlashLite)
//
// Steps (identical for both):
//
//  1. Create an ephemeral project.
//  2. Configure the Google AI provider with the target model.
//  3. Install the v3 blueprint.
//  4. Override agent definitions to use the target model (if different from blueprint default).
//  5. Create ONE runtime agent (python-senior-coder-native).
//  6. Pre-create a WorkPackage graph object.
//  7. Trigger python-senior-coder-native with prompt + context {"wp_id": <id>}.
//  8. Poll until the agent run reaches status=success.
//  9. Assert: ≥1 TaskResult whose content looks like Python code.
//
// 10. Dump run detail log.
//
// Required environment variables:
//
//	DEEPSEEK_API_KEY or GOOGLE_AI_API_KEY or OPENAI_API_KEY — LLM provider key; test is skipped when absent.
//	MEMORY_TEST_SERVER — URL of the Memory server.
//	MEMORY_TEST_TOKEN  — API key for the Memory server.
package experiments_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestResearchAndCodeModelComparison_Flash25(t *testing.T) {
	runResearchAndCodeModelComparison(t, "gemini-2.5-flash")
}

func TestResearchAndCodeModelComparison_FlashLite(t *testing.T) {
	runResearchAndCodeModelComparison(t, "gemini-3.1-flash-lite-preview")
}

// runResearchAndCodeModelComparison runs the full python-senior-coder-native
// end-to-end test with the given model and records the run under the shared
// experiment name so both variants appear grouped in the runlog TUI.
func runResearchAndCodeModelComparison(t *testing.T, model string) {
	t.Helper()

	rl := newRunLog(t)
	defer rl.Close()

	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t, rl)

	home := t.TempDir()
	srv := serverURL()
	token := e2eTestToken()
	rl.SetExperiment(researchCodeExperimentName)
	describe(rl,
		fmt.Sprintf("Model comparison experiment: python-senior-coder-native with %s", model),
		"Uses Google native google_search tool — no Brave API key needed",
		"Creates ephemeral project, installs v3 blueprint, overrides model in agent definitions",
		"Pre-creates a WorkPackage; triggers agent with prompt + context wp_id",
		"Polls until agent run reaches status=success; asserts TaskResult contains Python code",
	)

	logStatusPreamble(t, home)
	setupCLIAuth(t, home)

	projectName, projectID := setupProject(t, rl, home, "e2e-v3-model-cmp")
	setupTestProvider(t, rl, home, model)
	installBlueprint(t, rl, home, projectName, projectID,
		blueprintV3URL, model, blueprintV3Model,
		[]string{"python-senior-coder-native"},
		"blueprint:v3", "search:google-native",
	)
	agents := createRuntimeAgents(t, rl, home, projectID, []agentInfo{{Name: "python-senior-coder-native"}})
	agentID := agents[0].ID
	wpID := createWorkPackage(t, rl, home, projectID, seniorCoderTaskTitle)

	// ── Trigger python-senior-coder-native ───────────────────────────────────
	// HTTP trigger is required — CLI "agents trigger" has no --prompt or --context flags.
	rl.Section("Trigger python-senior-coder-native")
	triggerURL := fmt.Sprintf("%s/api/projects/%s/agents/%s/trigger", srv, projectID, agentID)
	triggerBody, _ := json.Marshal(map[string]any{
		"prompt":  seniorCoderTaskMessage,
		"context": map[string]any{"wp_id": wpID},
	})
	triggerResp := doJSON(t, "POST", triggerURL, token, projectID, triggerBody)
	triggerRespBody := readBody(t, triggerResp)
	if triggerResp.StatusCode != 200 {
		rl.Failf("trigger python-senior-coder-native: want 200, got %d — %s", triggerResp.StatusCode, triggerRespBody)
	}
	rl.Printf("POST %s → %d", triggerURL, triggerResp.StatusCode)
	rl.Printf("trigger response: %s", triggerRespBody)
	rl.Printf("agent triggered at %s", time.Now().UTC().Format(time.RFC3339))

	// ── Poll until the agent run completes ───────────────────────────────────
	rl.Section("Poll for agent run completion")
	rl.Printf("timeout: %s, poll interval: %s", v3NativeSeniorCoderTimeout, pollInterval)
	deadline := time.Now().Add(v3NativeSeniorCoderTimeout)
	pollCount := 0
	agentDone := false

	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		pollCount++

		runsOut, err := runCLIInDirWithHome(t, "", home,
			"agents", "runs", agentID,
			"--project", projectID,
		)
		if err != nil {
			rl.Printf("poll #%d: agents runs error: %v", pollCount, err)
			continue
		}

		compact := compactRunsOutput(runsOut)
		rl.Printf("poll #%d: [agent runs] %s", pollCount, compact)

		if strings.Contains(compact, "status=success") {
			rl.Printf("poll #%d: agent run reached success", pollCount)
			agentDone = true
			break
		}
		if strings.Contains(compact, "status=error") || strings.Contains(compact, "status=failed") {
			rl.Section("Early Failure — Agent Run Detail Logs")
			dumpAgents(t, rl, srv, token, projectID, agents)
			rl.Failf("agent run reached failure status: %s", compact)
		}
	}

	if !agentDone {
		rl.Section("Timeout Failure — Agent Run Detail Logs")
		dumpAgents(t, rl, srv, token, projectID, agents)
		rl.Failf("agent run did not complete within %s", v3NativeSeniorCoderTimeout)
	}

	// ── Results ───────────────────────────────────────────────────────────────
	rl.Section("Results")

	wpFinalOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "WorkPackage",
		"--project", projectID,
		"--output", "json",
	)
	var wps []map[string]any
	if err := json.Unmarshal([]byte(wpFinalOut), &wps); err == nil {
		rl.Printf("WorkPackage final state:")
		for _, wp := range wps {
			props, _ := wp["properties"].(map[string]any)
			rl.Printf("  status : %v", props["status"])
			rl.Printf("  title  : %v", props["title"])
		}
	}

	resultsOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "list",
		"--type", "TaskResult",
		"--project", projectID,
		"--output", "json",
	)
	var results []map[string]any
	if err := json.Unmarshal([]byte(resultsOut), &results); err != nil {
		rl.Failf("could not parse TaskResult list: %v\nraw: %s", err, resultsOut)
	}
	if len(results) < 1 {
		t.Errorf("expected at least 1 TaskResult, got 0")
	}
	rl.Printf("TaskResults created: %d", len(results))

	foundPython := false
	for i, result := range results {
		props, _ := result["properties"].(map[string]any)
		content, _ := props["content"].(string)
		summary, _ := props["summary"].(string)
		status, _ := props["status"].(string)
		rl.Printf("── TaskResult %d [%s] ──────────────────────────────────────", i+1, status)
		rl.Printf("summary: %s", summary)
		if content != "" {
			if len(content) > 600 {
				rl.Printf("content (truncated):\n%s\n...", content[:600])
			} else {
				rl.Printf("content:\n%s", content)
			}
		}
		if strings.Contains(content, "def ") ||
			strings.Contains(content, "import ") ||
			strings.Contains(content, "if __name__") {
			foundPython = true
		}
	}
	if !foundPython {
		t.Errorf("expected at least one TaskResult containing Python code (def/import/if __name__), but none found")
	}
	rl.Printf("Python code found in TaskResult: %v", foundPython)

	dumpAgents(t, rl, srv, token, projectID, agents)
	printSummary(t, rl, srv, token, projectID, agents)
}
