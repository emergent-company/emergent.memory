// Package cli_test — cost_demo_test.go
//
// Simple demonstration of cost tracking without requiring full CLI setup.
// This test directly uses the API to simulate what memory ask does.
package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCostTracking_DirectAPI demonstrates cost tracking by directly calling
// the /api/ask endpoint and then fetching cost data from the agent-runs API.
//
// This test doesn't require the memory CLI to be installed — it uses direct
// HTTP calls to the Memory server.
func TestCostTracking_DirectAPI(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Demonstrate cost tracking via direct API calls",
		"Create a test project",
		"Call /api/ask directly",
		"Fetch cost data from /api/projects/{id}/agent-runs/{id}",
		"Record cost to runlog",
		"Verify cost appears in runlog output",
	)

	srv := serverURL()
	token := e2eTestToken()

	// Skip if server is not available.
	skipIfServerDown(t, rl)

	rl.Section("Create test project")
	// Create a project via API.
	createReq := map[string]any{
		"name": fmt.Sprintf("cost-demo-%d", time.Now().Unix()),
	}
	if orgID := orgIDArgs(); len(orgID) >= 2 {
		createReq["orgId"] = orgID[1]
	}
	createJSON, _ := json.Marshal(createReq)
	createResp := framework.DoJSON(t, "POST", srv+"/api/projects", token, "", createJSON)
	if createResp.StatusCode != 201 && createResp.StatusCode != 200 {
		body := framework.ReadBody(t, createResp)
		rl.Failf("failed to create project: %d %s", createResp.StatusCode, body)
	}
	createBody := framework.ReadBody(t, createResp)
	var createData struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &createData); err != nil {
		rl.Failf("failed to parse create project response: %v", err)
	}
	projectID := createData.ID
	rl.Printf("created project: %s", projectID)

	// Clean up project at the end.
	t.Cleanup(func() {
		deleteResp := framework.DoJSON(t, "DELETE", srv+"/api/projects/"+projectID, token, projectID, nil)
		deleteResp.Body.Close()
	})

	// Configure LLM model for the project so /api/ask can resolve the generative model.
	if provider, apiKey, model := framework.ProviderFromEnv(); provider != "" && model != "" {
		// Set generative model on the project.
		modelCfg := map[string]any{"generativeModel": provider + "/" + model}
		modelCfgJSON, _ := json.Marshal(modelCfg)
		modelResp := framework.DoJSON(t, "PUT", srv+"/api/v1/projects/"+projectID+"/model-config", token, projectID, modelCfgJSON)
		modelResp.Body.Close()
		// For OpenAI-compatible providers (e.g. deepseek), also configure the
		// provider credentials at the project level so the server can resolve the API key.
		if apiKey != "" {
			orgArgs := orgIDArgs()
			orgID := ""
			for i, a := range orgArgs {
				if a == "--org-id" && i+1 < len(orgArgs) {
					orgID = orgArgs[i+1]
					break
				}
			}
			if orgID != "" {
				provCfg := map[string]any{"apiKey": apiKey, "generativeModel": model}
				provCfgJSON, _ := json.Marshal(provCfg)
				req, _ := http.NewRequest("PUT", srv+"/api/v1/projects/"+projectID+"/providers/"+provider, strings.NewReader(string(provCfgJSON)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("X-Project-ID", projectID)
				req.Header.Set("X-Org-ID", orgID)
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					resp.Body.Close()
				}
			}
		}
		rl.Printf("configured project model: %s/%s", provider, model)
	}

	rl.Section("Call /api/ask")
	askReq := map[string]any{
		"message": "What is 2 + 2?",
	}
	askJSON, _ := json.Marshal(askReq)
	askResp := framework.DoJSON(t, "POST", srv+"/api/projects/"+projectID+"/ask", token, projectID, askJSON)
	if askResp.StatusCode != 200 {
		body := framework.ReadBody(t, askResp)
		rl.Printf("ask API call failed: %d %s", askResp.StatusCode, body)
		// If ask endpoint is not available, skip.
		if askResp.StatusCode == 404 {
			rl.Skipf("/api/ask endpoint not available")
		}
		rl.Failf("ask API failed: %d", askResp.StatusCode)
	}
	askBody := framework.ReadBody(t, askResp)
	var askData struct {
		Data struct {
			Answer string `json:"answer"`
			RunID  string `json:"runId"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(askBody), &askData); err != nil {
		// Ask endpoint may return SSE stream — JSON parse is best-effort.
		rl.Printf("note: ask response is not plain JSON (may be SSE): %v", err)
	}
	rl.Printf("ask response length: %d bytes", len(askData.Data.Answer))

	// Extract run ID.
	runID := askData.Data.RunID
	if runID == "" {
		// Try extracting from response body.
		runID = extractUUIDFromText(askBody)
	}
	if runID == "" {
		rl.Printf("warn: no run ID found in ask response")
		rl.Skipf("cannot fetch cost without run ID")
	}
	rl.Printf("agent run ID: %s", runID)

	rl.Section("Fetch cost data")
	// Wait a moment for the server to finalize cost calculation.
	time.Sleep(2 * time.Second)

	inputTokens, outputTokens, costUSD := framework.FetchRunTokenUsage(t, srv, token, projectID, runID)
	if inputTokens == 0 && outputTokens == 0 {
		rl.Printf("warn: no cost data available (may not be calculated yet)")
		// This is not a failure — cost tracking might not be enabled on this server.
	} else {
		rl.Printf("cost data retrieved:")
		rl.Printf("  input tokens:  %d", inputTokens)
		rl.Printf("  output tokens: %d", outputTokens)
		rl.Printf("  cost (USD):    $%.6f", costUSD)

		// Record cost to runlog!
		rl.RecordTokenUsage(inputTokens, outputTokens, costUSD)
		rl.Printf("cost recorded to runlog (will appear in `runlog runs` output)")
	}

	rl.Section("Test complete")
	rl.Printf("Run `runlog runs --since 5m` to see cost data")
	rl.Printf("Run `runlog show <run-id>` to see detailed breakdown")
}

// extractUUIDFromText is duplicated here for convenience (defined in ask_test.go).
func extractUUIDFromTextLocal(text string) string {
	const hexChars = "0123456789abcdefABCDEF"
	isHex := func(b byte) bool {
		for i := 0; i < len(hexChars); i++ {
			if b == hexChars[i] {
				return true
			}
		}
		return false
	}
	for i := 0; i+35 < len(text); i++ {
		s := text[i : i+36]
		if s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-' {
			ok := true
			for j, c := range []byte(s) {
				if j == 8 || j == 13 || j == 18 || j == 23 {
					continue
				}
				if !isHex(c) {
					ok = false
					break
				}
			}
			if ok {
				return strings.ToLower(s)
			}
		}
	}
	return ""
}
