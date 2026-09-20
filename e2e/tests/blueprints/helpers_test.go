// Package blueprints_test — helpers_test.go
//
// Thin wrappers around framework exported functions, plus blueprint-specific
// helpers shared across all files in this package.
package blueprints_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Constants shared across blueprint tests
// ─────────────────────────────────────────────────────────────────────────────

const (
	blueprintURL = "/root/blueprints/workspace-memory-blueprint"

	// blueprintAINewsPath is the local path to the AI news blueprint directory.
	blueprintAINewsPath = "/root/blueprints/ai-news-memory-blueprint"

	// blueprintModel is the model name pinned inside the blueprint YAML files.
	blueprintModel = "google/gemini-3.1-flash-lite-preview"

	// pollInterval is how often to re-check WorkPackage/agent status.
	pollInterval = 5 * time.Second

	// orchestratorTimeout is the maximum time to wait for the orchestrator to
	// complete the WorkPackage.  Must stay well below `go test -timeout 10m`.
	orchestratorTimeout = 8 * time.Minute

	// seniorCoderTaskTitle and seniorCoderTaskMessage were previously defined in
	// the deleted python_senior_coder_test.go (v1).
	seniorCoderTaskTitle = "Open-Meteo Weather Script"

	seniorCoderTaskMessage = "Title: Open-Meteo Weather Script\n" +
		"Description: Search the web for the Open-Meteo API documentation. " +
		"Then write a Python script that fetches the current weather for London " +
		"(latitude 51.5, longitude -0.1) using the Open-Meteo API (no API key needed). " +
		"The script must print the current temperature in Celsius. " +
		"Use only the standard library plus the requests package."

	// blueprintV2URL is the local path to the v2 blueprint directory.
	blueprintV2URL = "/root/blueprints/workspace-memory-blueprint-v2"

	// blueprintV2Model is the model name pinned inside the v2 blueprint YAML files.
	blueprintV2Model = "google/gemini-3.1-flash-lite-preview"

	// v2OrchestratorTimeout is the maximum time to wait for the v2 orchestrator.
	// Must stay well below `go test -timeout 10m`.
	v2OrchestratorTimeout = 8 * time.Minute

	// blueprintV3URL is the local path to the v3 blueprint directory.
	blueprintV3URL = "/root/blueprints/workspace-memory-blueprint-v3"

	// blueprintV3Model is the model name pinned inside the v3 blueprint YAML files.
	blueprintV3Model = "google/gemini-3.1-flash-lite-preview"

	// v3OrchestratorTimeout is the maximum time to wait for the v3 orchestrator.
	// Must stay well below `go test -timeout 10m`.
	v3OrchestratorTimeout = 8 * time.Minute

	// v3SeniorCoderTimeout is the maximum time to wait for the v3 senior coder agent.
	// Must stay well below `go test -timeout 10m`.
	v3SeniorCoderTimeout = 8 * time.Minute

	// v3NativeSeniorCoderTimeout is the maximum time to wait for the native senior coder agent.
	// Must stay well below `go test -timeout 10m`.
	v3NativeSeniorCoderTimeout = 8 * time.Minute

	// aiNewsResearchTimeout is the max wait for a skill/research agent to finish.
	// Must stay well below `go test -timeout 10m`.
	aiNewsResearchTimeout = 8 * time.Minute

	// aiNewsClassifierTimeout is the max wait for the content-classifier to finish.
	// Must stay well below `go test -timeout 10m`.
	aiNewsClassifierTimeout = 8 * time.Minute

	// aiNewsDigestTimeout is the max wait for the digest-writer agent to finish.
	aiNewsDigestTimeout = 5 * time.Minute
)

// orchestratorModel returns the model to use for all agent definitions.
// Override via ORCHESTRATOR_MODEL env var; defaults to blueprintModel.
func orchestratorModel() string {
	if m := os.Getenv("ORCHESTRATOR_MODEL"); m != "" {
		return m
	}
	return blueprintModel
}

// activeModel returns the model actually in use by the installed agent definitions.
func activeModel(defsOut string) string {
	if m := parseAgentDefsModel(defsOut); m != "" {
		return m
	}
	return orchestratorModel()
}

// ─────────────────────────────────────────────────────────────────────────────
// Type aliases
// ─────────────────────────────────────────────────────────────────────────────

type runLog = framework.RunLog
type agentRunInterval = framework.AgentRunInterval
type agentInfo = framework.AgentInfo

// ─────────────────────────────────────────────────────────────────────────────
// Run log helpers
// ─────────────────────────────────────────────────────────────────────────────

func newRunLog(t *testing.T) *runLog {
	t.Helper()
	return framework.NewRunLog(t)
}

func describe(rl *runLog, summary string, bullets ...string) {
	rl.Describe(summary, bullets...)
}

func tag(rl *runLog, tags ...string) {
	rl.Tag(tags...)
}

func setExperiment(rl *runLog, name string) {
	rl.SetExperiment(name)
}

func fetchRunTokenUsage(t *testing.T, srv, token, projectID, runID string) (int64, int64, float64) {
	return framework.FetchRunTokenUsage(t, srv, token, projectID, runID)
}

func buildAgentRunIntervals(t *testing.T, rl *framework.RunLog, srv, token, projectID string, agents []agentInfo) []agentRunInterval {
	return framework.BuildAgentRunIntervals(t, rl, srv, token, projectID, agents)
}

func printGanttTimeline(rl *framework.RunLog, intervals []agentRunInterval) {
	framework.PrintGantt(rl, intervals)
}

func printTokenUsageSummary(rl *framework.RunLog, intervals []agentRunInterval) {
	framework.PrintTokenSummary(rl, intervals)
}

func formatInt(n int64) string {
	return framework.FormatInt(n)
}

func compactRunsOutput(runsOut string) string {
	return framework.CompactRunsOutput(runsOut)
}

func allRunsTerminal(runsOut string) bool {
	return framework.AllRunsTerminal(runsOut)
}

// ─────────────────────────────────────────────────────────────────────────────
// Server helpers
// ─────────────────────────────────────────────────────────────────────────────

func serverURL() string     { return framework.ServerURL() }
func e2eTestToken() string  { return framework.E2ETestToken() }
func filteredEnv() []string { return framework.FilteredEnv() }

func skipIfServerDown(t *testing.T, rl *framework.RunLog) {
	t.Helper()
	framework.SkipIfServerDown(t, rl)
}

func setupCLIAuth(t *testing.T, home string) {
	t.Helper()
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if rl, ok := v.(*framework.RunLog); ok {
			rl.Section("Setup CLI auth")
		}
	}
	framework.SetupCLIAuth(t, home)
}

func logStatusPreamble(t *testing.T, home ...string) {
	t.Helper()
	framework.LogStatusPreamble(t, home...)
}

// ─────────────────────────────────────────────────────────────────────────────
// CLI helpers
// ─────────────────────────────────────────────────────────────────────────────

func emitCLI(t *testing.T, invocation, out string) {
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if rl, ok := v.(*framework.RunLog); ok {
			rl.CLI(invocation, out)
			return
		}
	}
	logSession(t, invocation, out)
}

func emitCLIErr(t *testing.T, invocation, out string, err error) {
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if rl, ok := v.(*framework.RunLog); ok {
			rl.CLIErr(invocation, out, err, 0)
		}
	}
}

func mustRunCLIInDirWithHome(t *testing.T, dir, home string, args ...string) string {
	t.Helper()
	out := framework.MustRunCLIInDirWithHome(t, dir, home, args...)
	invocation := fmt.Sprintf("memory %s", strings.Join(args, " "))
	emitCLI(t, invocation, out)
	return out
}

// runCLIInDirWithHome is like mustRunCLIInDirWithHome but returns an error
// instead of failing the test — used for polling where transient failures are OK.
func runCLIInDirWithHome(t *testing.T, dir, home string, args ...string) (string, error) {
	t.Helper()
	out, err := framework.RunCLIInDirWithHome(t, dir, home, args...)
	invocation := fmt.Sprintf("memory %s", strings.Join(args, " "))
	emitCLIErr(t, invocation, out, err)
	return out, err
}

func logSession(t *testing.T, invocation, output string) {
	t.Helper()
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if _, ok := v.(*framework.RunLog); ok {
			return
		}
	}
	framework.LogSession(t, invocation, output)
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

func setAuthHeader(req *http.Request, token string) {
	framework.SetAuthHeader(req, token)
}

func doJSON(t *testing.T, method, url, token, projectID string, body []byte) *http.Response {
	return framework.DoJSON(t, method, url, token, projectID, body)
}

func readBody(t *testing.T, resp *http.Response) string {
	return framework.ReadBody(t, resp)
}

func doMCPJSON(t *testing.T, url, token, projectID, sessionID, protocolVersion string, body []byte) *http.Response {
	return framework.DoMCPJSON(t, url, token, projectID, sessionID, protocolVersion, body)
}

func truncate(s string, n int) string {
	return framework.Truncate(s, n)
}

// ─────────────────────────────────────────────────────────────────────────────
// Parse helpers
// ─────────────────────────────────────────────────────────────────────────────

func parseProjectID(output string) string         { return framework.ParseProjectID(output) }
func parseAgentID(output string) string           { return framework.ParseAgentID(output) }
func parseAgentDefsModel(output string) string    { return framework.ParseAgentDefsModel(output) }
func parseJSONField(jsonStr, field string) string { return framework.ParseJSONField(jsonStr, field) }

func projectCreateOrgArgs() []string { return framework.ProjectCreateOrgArgs() }
func orgIDArgs() []string            { return framework.OrgIDArgs() }

func prettyJSONOutput(output string) string { return framework.PrettyJSONOutput(output) }

// prettyJSON re-indents a JSON string for human-readable log output.
func prettyJSON(raw string) string {
	return framework.PrettyJSONOutput(raw)
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent helpers
// ─────────────────────────────────────────────────────────────────────────────

func dumpAgentRunDetails(t *testing.T, rl *runLog, srv, token, projectID string, agentNames, agentIDs []string) {
	framework.DumpAgentRunDetails(t, rl, srv, token, projectID, agentNames, agentIDs)
}

func pollAgentUntilSuccess(
	t *testing.T,
	rl *runLog,
	home, srv, token, projectID, agentID, agentName string,
	timeout time.Duration,
) bool {
	return framework.PollUntilSuccess(t, rl, home, srv, token, projectID, agentID, agentName, timeout, pollInterval)
}

// agentQuestion is a type alias for the framework AgentQuestion struct.
type agentQuestion = framework.AgentQuestion

// listPendingQuestions lists pending agent questions for a project via the CLI.
func listPendingQuestions(t *testing.T, rl *runLog, home, projectID string) ([]agentQuestion, string, error) {
	return framework.ListPendingQuestions(t, rl, home, projectID)
}

// listAllQuestions lists all agent questions for a project via the CLI (no status filter).
func listAllQuestions(t *testing.T, rl *runLog, home, projectID string) ([]agentQuestion, string, error) {
	return framework.ListAllQuestions(t, rl, home, projectID)
}

// respondToQuestion responds to a pending agent question via the CLI.
func respondToQuestion(t *testing.T, rl *runLog, home, projectID, questionID, response string) (string, error) {
	return framework.RespondToQuestion(t, rl, home, projectID, questionID, response)
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph helpers
// ─────────────────────────────────────────────────────────────────────────────

func listGraphObjectsByType(t *testing.T, srv, token, projectID, objectType string) []map[string]any {
	return framework.ListByType(t, srv, token, projectID, objectType)
}

func listGraphObjectsByLabel(t *testing.T, srv, token, projectID, label string) []map[string]any {
	return framework.ListByLabel(t, srv, token, projectID, label)
}

func listRelationshipsByTarget(t *testing.T, srv, token, projectID, targetID, relType string) []map[string]any {
	return framework.ListRelationships(t, srv, token, projectID, targetID, relType)
}

func listRelationshipsFromCLI(t *testing.T, home, projectID, fromID, relType string) []map[string]any {
	return framework.ListRelationshipsFromCLI(t, home, projectID, fromID, relType)
}

func propString(props map[string]any, keys ...string) string {
	return framework.PropString(props, keys...)
}

// fetchGlobalSkillsViaAPI calls GET /api/skills and returns the parsed skills slice.
func fetchGlobalSkillsViaAPI(t *testing.T, serverURL, token string) []map[string]any {
	t.Helper()

	resp := doJSON(t, "GET", serverURL+"/api/skills", token, "", nil)
	respBody := readBody(t, resp)

	var envelope map[string]any
	if err := json.Unmarshal([]byte(respBody), &envelope); err != nil {
		t.Fatalf("fetchGlobalSkillsViaAPI: unmarshal response: %v\nbody: %s", err, respBody)
	}

	raw, ok := envelope["skills"]
	if !ok {
		t.Fatalf("fetchGlobalSkillsViaAPI: response has no 'skills' key: %s", respBody)
	}

	rawSlice, ok := raw.([]any)
	if !ok {
		t.Fatalf("fetchGlobalSkillsViaAPI: 'skills' is not an array: %T", raw)
	}

	result := make([]map[string]any, 0, len(rawSlice))
	for _, item := range rawSlice {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Brave Search helpers (used by orchestrator and senior coder tests)
// ─────────────────────────────────────────────────────────────────────────────

// getBraveToolIDs returns the builtin server ID and the brave_web_search tool ID.
func getBraveToolIDs(t *testing.T, srv, token, projectID string) (serverID, toolID string) {
	t.Helper()
	sid, tool := getBraveToolIDsWithDetail(t, srv, token, projectID)
	if tool == nil {
		t.Fatal("brave_web_search not found in builtin tools — server may not have Brave Search configured")
	}
	id, _ := tool["id"].(string)
	return sid, id
}

// getBraveToolIDsWithDetail returns the builtin server ID and the full brave_web_search
// tool object (or nil if not found).
func getBraveToolIDsWithDetail(t *testing.T, srv, token, projectID string) (serverID string, tool map[string]any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv+"/api/admin/mcp-servers", nil)
	setAuthHeader(req, token)
	req.Header.Set("X-Project-ID", projectID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list mcp-servers: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("list mcp-servers: want 200, got %d — %s", resp.StatusCode, body)
	}

	var result struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode mcp-servers response: %v", err)
	}

	for _, s := range result.Data {
		if st, _ := s["type"].(string); st != "builtin" {
			continue
		}
		serverID = s["id"].(string)
		break
	}
	if serverID == "" {
		t.Fatal("builtin MCP server not found")
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()

	req2, _ := http.NewRequestWithContext(ctx2, "GET",
		fmt.Sprintf("%s/api/admin/mcp-servers/%s/tools", srv, serverID), nil)
	setAuthHeader(req2, token)
	req2.Header.Set("X-Project-ID", projectID)

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("list tools for server %s: %v", serverID, err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("list tools: want 200, got %d — %s", resp2.StatusCode, body)
	}

	var toolsResult struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&toolsResult); err != nil {
		t.Fatalf("decode tools response: %v", err)
	}

	for _, tl := range toolsResult.Data {
		// The server returns the tool name as "toolName" (not "name").
		name, _ := tl["toolName"].(string)
		if name == "" {
			name, _ = tl["name"].(string) // fallback for older server versions
		}
		if name == "brave_web_search" {
			return serverID, tl
		}
	}
	return serverID, nil
}

// callBraveSearchViaMCP executes the brave_web_search MCP tool and returns the results.
func callBraveSearchViaMCP(t *testing.T, srv, token, projectID, query string) []map[string]any {
	t.Helper()

	mcpURL := srv + "/mcp"
	const protocolVersion = "2024-11-05"

	// 1. Initialize MCP session.
	initBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": protocolVersion,
			"clientInfo":      map[string]any{"name": "e2e-test", "version": "1.0"},
			"capabilities":    map[string]any{},
			"project_id":      projectID,
		},
	})

	initResp := doMCPJSON(t, mcpURL, token, projectID, "", protocolVersion, initBody)
	sessionID := initResp.Header.Get("Mcp-Session-Id")
	initBody2 := readBody(t, initResp)
	if initResp.StatusCode != http.StatusOK {
		t.Fatalf("MCP initialize: want 200, got %d — %s", initResp.StatusCode, initBody2)
	}
	t.Logf("MCP initialize OK, session=%s", sessionID)

	// 2. Send initialized notification.
	notifyBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})
	notifyResp := doMCPJSON(t, mcpURL, token, projectID, sessionID, protocolVersion, notifyBody)
	notifyResp.Body.Close()

	// 3. Call brave_web_search.
	callBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "brave_web_search",
			"arguments": map[string]any{
				"query": query,
				"count": 5,
			},
		},
	})

	callResp := doMCPJSON(t, mcpURL, token, projectID, sessionID, protocolVersion, callBody)
	body := readBody(t, callResp)
	t.Logf("MCP tools/call status: %d", callResp.StatusCode)
	t.Logf("MCP tools/call response: %s", truncate(body, 500))

	if callResp.StatusCode != http.StatusOK {
		t.Fatalf("MCP tools/call: want 200, got %d — %s", callResp.StatusCode, body)
	}

	var rpcResp struct {
		Result *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &rpcResp); err != nil {
		t.Fatalf("decode MCP response: %v\nraw: %s", err, body)
	}

	if rpcResp.Error != nil {
		t.Fatalf("MCP tools/call error: code=%d message=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if rpcResp.Result == nil {
		t.Fatalf("MCP tools/call: nil result in response: %s", body)
	}
	if rpcResp.Result.IsError {
		var msgs []string
		for _, c := range rpcResp.Result.Content {
			msgs = append(msgs, c.Text)
		}
		t.Fatalf("brave_web_search returned error: %s", strings.Join(msgs, "; "))
	}

	for _, c := range rpcResp.Result.Content {
		if c.Type != "text" || c.Text == "" {
			continue
		}
		var results []map[string]any
		if err := json.Unmarshal([]byte(c.Text), &results); err == nil {
			return results
		}
		var wrapper map[string]any
		if err := json.Unmarshal([]byte(c.Text), &wrapper); err == nil {
			if arr, ok := wrapper["results"].([]any); ok {
				results := make([]map[string]any, 0, len(arr))
				for _, item := range arr {
					if m, ok := item.(map[string]any); ok {
						results = append(results, m)
					}
				}
				return results
			}
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Blueprint-specific orchestrator helpers
// ─────────────────────────────────────────────────────────────────────────────

// skipIfNoLLMProvider skips t when no LLM provider env var is set.
func skipIfNoLLMProvider(t *testing.T, rl *framework.RunLog) {
	t.Helper()
	provider, _, _ := framework.ProviderFromEnv()
	if provider == "" {
		framework.DoSkipf(t, rl,
			"no LLM provider configured — set DEEPSEEK_API_KEY, GOOGLE_AI_API_KEY, or OPENAI_API_KEY")
	}
}

// parseWorkPackageStatus finds the WorkPackage with the given ID in the JSON
// array output of `graph objects list` and returns its status property.
func parseWorkPackageStatus(jsonStr, wpID string) string {
	var objects []struct {
		ID          string         `json:"id"`
		CanonicalID string         `json:"canonical_id"`
		Properties  map[string]any `json:"properties"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(jsonStr)), &objects); err != nil {
		return ""
	}
	for _, obj := range objects {
		if obj.ID == wpID || obj.CanonicalID == wpID {
			status, _ := obj.Properties["status"].(string)
			return status
		}
	}
	return ""
}

// jsonStringLiteral encodes s as a JSON string literal.
func jsonStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// overrideAgentDefsModel updates every agent definition in the project to use
// the given model name. It is a no-op when model == currentBlueprintModel.
func overrideAgentDefsModel(t *testing.T, rl *runLog, home, projectID, model, currentBlueprintModel string) {
	t.Helper()
	if model == currentBlueprintModel {
		return
	}

	listOut, err := runCLIInDirWithHome(t, "", home,
		"agent-definitions", "list",
		"--project", projectID,
	)
	if err != nil {
		rl.Failf("overrideAgentDefsModel: list failed: %v\n%s", err, listOut)
	}

	type agentDef struct{ id, name string }
	var defs []agentDef
	var currentName string
	for _, line := range strings.Split(listOut, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			if idx := strings.Index(trimmed, ". "); idx != -1 {
				currentName = strings.TrimSpace(trimmed[idx+2:])
			}
		}
		if strings.HasPrefix(trimmed, "ID:") {
			id := strings.TrimSpace(strings.TrimPrefix(trimmed, "ID:"))
			if id != "" {
				defs = append(defs, agentDef{id: id, name: currentName})
				currentName = ""
			}
		}
	}

	if len(defs) == 0 {
		rl.Failf("overrideAgentDefsModel: found 0 agent definitions in output:\n%s", listOut)
	}

	rl.Printf("overriding model to %q on %d agent definition(s)", model, len(defs))
	for _, def := range defs {
		updateOut, updateErr := runCLIInDirWithHome(t, "", home,
			"agent-definitions", "update", def.id,
			"--project", projectID,
			"--model", model,
		)
		if updateErr != nil {
			rl.Failf("overrideAgentDefsModel: update %s (%s) failed: %v\n%s", def.name, def.id, updateErr, updateOut)
		}
		rl.Printf("  updated %s (%s) → model=%s", def.name, def.id, model)
	}
}

// getOrgIDForProject resolves the org ID for the given project via the REST API.
func getOrgIDForProject(t *testing.T, srv, token, projectID string) string {
	t.Helper()
	resp := doJSON(t, "GET", srv+"/api/projects/"+projectID, token, "", nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/projects/%s: %d — %s", projectID, resp.StatusCode, truncate(body, 200))
	}
	return parseJSONField(body, "orgId")
}

// ─────────────────────────────────────────────────────────────────────────────
// AI News blueprint helpers
// ─────────────────────────────────────────────────────────────────────────────

// skipIfNoBlueprintKey skips the test if key is not set in the blueprint env
// files (blueprintAINewsPath/.env / .env.local) or the shell environment.
func skipIfNoBlueprintKey(t *testing.T, rl *framework.RunLog, key, serviceName string) {
	t.Helper()
	if framework.BlueprintEnvVar(blueprintAINewsPath, key) == "" {
		framework.DoSkipf(t, rl, "%s not set in %s/.env.local or environment — skipping (set %s to run)",
			key, blueprintAINewsPath, key)
	}
}

// blueprintEnvVar looks up key from blueprint env files, falling back to os.Getenv.
func blueprintEnvVar(key string) string {
	return framework.BlueprintEnvVar(blueprintAINewsPath, key)
}

// ─────────────────────────────────────────────────────────────────────────────
// Pre-check helpers
// ─────────────────────────────────────────────────────────────────────────────

func requireServerReady(t *testing.T, home string, rl ...*framework.RunLog) {
	t.Helper()
	framework.RequireServerReady(t, home, rl...)
}

func skipIfEndpointMissing(t *testing.T, path, bearerToken string, rl ...*framework.RunLog) {
	t.Helper()
	framework.SkipIfEndpointMissing(t, path, bearerToken, rl...)
}

// ─────────────────────────────────────────────────────────────────────────────
// Misc helpers
// ─────────────────────────────────────────────────────────────────────────────

// deleteProjectViaExec deletes a project using the memory CLI binary directly.
// Unlike mustRunCLIInDirWithHome it does not call t.Fatal — safe for t.Cleanup.
func deleteProjectViaExec(t *testing.T, home, projectID, projectName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "memory", "projects", "delete", projectID)
	env := filteredEnv()
	env = append(env, "HOME="+home)
	env = append(env, "PATH="+home+"/.memory/bin:"+os.Getenv("PATH"))
	cmd.Env = env

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("warn: failed to delete test project %s: %v\n%s", projectID, err, out)
	} else {
		t.Logf("deleted test project %s (%s)", projectName, projectID)
	}
}

// parseBlueprintEnvFiles reads .env then .env.local from dir (forwarding to
// framework.ParseBlueprintEnvFiles).
func parseBlueprintEnvFiles(dir string) map[string]string {
	return framework.ParseBlueprintEnvFiles(dir)
}

// parseIDFromOutput attempts to extract a UUID from the output of a
// `memory graph objects create` command. The CLI typically prints lines like:
//
//	ID: <uuid>
//	Created object: <uuid>
//	canonical_id: <uuid>
//
// Returns an empty string if no UUID is found.
func parseIDFromOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"ID:", "id:", "Created:", "canonical_id:", "CanonicalID:"} {
			if strings.HasPrefix(line, prefix) {
				candidate := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				candidate = strings.Trim(candidate, "().,")
				if len(candidate) == 36 && strings.Count(candidate, "-") == 4 {
					return candidate
				}
			}
		}
		if strings.Contains(line, "(") {
			start := strings.LastIndex(line, "(")
			end := strings.LastIndex(line, ")")
			if start >= 0 && end > start {
				candidate := strings.TrimSpace(line[start+1 : end])
				if len(candidate) == 36 && strings.Count(candidate, "-") == 4 {
					return candidate
				}
			}
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Step-level helpers — high-level building blocks for blueprint tests
// ─────────────────────────────────────────────────────────────────────────────

// setupProject creates an ephemeral project, sets project_id in the CLI config,
// and registers a t.Cleanup that deletes it. Returns (projectName, projectID).
func setupProject(t *testing.T, rl *runLog, home, namePrefix string) (string, string) {
	t.Helper()
	rl.Section("Create project")
	projectName := fmt.Sprintf("%s-%d", namePrefix, time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home, append([]string{"projects", "create", "--name", projectName}, projectCreateOrgArgs()...)...)
	rl.CLI("memory projects create --name "+projectName, createOut)

	projectID := parseProjectID(createOut)
	if projectID == "" {
		rl.Failf("could not parse project ID from: %q", createOut)
	}
	mustRunCLIInDirWithHome(t, "", home, "config", "set", "project_id", projectID)

	t.Cleanup(func() { deleteProjectViaExec(t, home, projectID, projectName) })
	return projectName, projectID
}

// setupTestProvider configures whichever LLM provider is available from env vars.
// model overrides the env-var model when non-empty (e.g. for blueprint-pinned models).
// Reads provider keys via blueprintEnvVar (blueprint env files take precedence over os.Getenv).
// If no provider env vars are set the test is skipped.
func setupTestProvider(t *testing.T, rl *runLog, home, model string) {
	t.Helper()

	// Use blueprintEnvVar so that blueprint-specific .env files are respected.
	var provider, apiKey, envModel string
	if key := blueprintEnvVar("DEEPSEEK_API_KEY"); key != "" {
		provider, apiKey, envModel = "deepseek", key, blueprintEnvVar("DEEPSEEK_MODEL")
	} else if key := blueprintEnvVar("GOOGLE_AI_API_KEY"); key != "" {
		provider, apiKey, envModel = "google", key, blueprintEnvVar("GOOGLE_AI_MODEL")
	} else if key := blueprintEnvVar("OPENAI_API_KEY"); key != "" {
		provider, apiKey, envModel = "openai", key, blueprintEnvVar("OPENAI_MODEL")
	}
	if provider == "" {
		framework.DoSkipf(t, rl,
			"no LLM provider configured — set DEEPSEEK_API_KEY, GOOGLE_AI_API_KEY, or OPENAI_API_KEY")
	}
	if model == "" {
		model = envModel
	}

	rl.Section("Configure provider")
	args := []string{"provider", "configure", provider, "--api-key", apiKey}
	if model != "" {
		args = append(args, "--generative-model", model)
	}
	args = append(args, orgIDArgs()...)
	out, provErr := runCLIInDirWithHome(t, "", home, args...)
	rl.CLI("memory provider configure "+provider, out)
	if provErr != nil {
		testOut, testErr := runCLIInDirWithHome(t, "", home,
			append([]string{"provider", "test"}, orgIDArgs()...)...)
		rl.CLI("memory provider test", testOut)
		if testErr != nil {
			rl.Failf("provider configure failed and provider test also failed: %v\n%s", testErr, testOut)
		}
	}
}

// installBlueprint installs a blueprint, verifies requiredDefs are present,
// overrides agent definition models when model != blueprintModel, and tags the
// run log. extraTags are appended after the "model:..." tag. Returns the
// post-override agent-definitions list output.
func installBlueprint(t *testing.T, rl *runLog, home, projectName, projectID, url, model, bpModel string, requiredDefs []string, extraTags ...string) string {
	t.Helper()
	rl.Section("Install blueprint")
	blueprintOut := mustRunCLIInDirWithHome(t, "", home,
		"blueprints", url,
		"--project", projectName,
		"--upgrade",
	)
	rl.CLI("memory blueprints "+url+" --project "+projectName+" --upgrade", blueprintOut)
	if strings.Contains(blueprintOut, "errors") && !strings.Contains(blueprintOut, "0 errors") {
		rl.Failf("blueprint install reported errors:\n%s", blueprintOut)
	}

	defsOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "list",
		"--project", projectID,
	)
	rl.CLI("memory agent-definitions list --project "+projectID, defsOut)
	for _, required := range requiredDefs {
		if !strings.Contains(defsOut, required) {
			rl.Failf("agent definition %q not found after blueprint install", required)
		}
	}

	overrideAgentDefsModel(t, rl, home, projectID, model, bpModel)
	postOut := mustRunCLIInDirWithHome(t, "", home,
		"agent-definitions", "list", "--project", projectID)
	tags := append([]string{"model:" + activeModel(postOut)}, extraTags...)
	rl.Tag(tags...)
	return postOut
}

// createRuntimeAgents creates runtime agent entities for each entry in agents.
// Each entry's Name is required; Cron defaults to "0 0 0 1 1 *" when empty.
// The ID field of each entry is populated and the updated slice is returned.
func createRuntimeAgents(t *testing.T, rl *runLog, home, projectID string, agents []agentInfo) []agentInfo {
	t.Helper()
	rl.Section("Create runtime agents")
	const defaultCron = "0 0 0 1 1 *"
	result := make([]agentInfo, len(agents))
	for i, a := range agents {
		cron := a.Cron
		if cron == "" {
			cron = defaultCron
		}
		agentOut := mustRunCLIInDirWithHome(t, "", home,
			"agents", "create",
			"--name", a.Name,
			"--trigger-type", "schedule",
			"--cron", cron,
			"--strategy-type", "agentic",
			"--project", projectID,
		)
		rl.CLI("memory agents create --name "+a.Name, agentOut)
		id := parseAgentID(agentOut)
		if id == "" {
			rl.Failf("could not parse agent ID for %q from: %q", a.Name, agentOut)
		}
		result[i] = agentInfo{Name: a.Name, ID: id, Cron: cron}
	}
	return result
}

// configureBraveSearch enables the brave_web_search MCP tool via the memory CLI.
// Uses `memory mcp-servers configure brave_web_search api_key=<key>` which
// handles tool materialisation and enablement in a single idempotent command.
func configureBraveSearch(t *testing.T, rl *runLog, home, projectID string) {
	t.Helper()
	rl.Section("Configure Brave Search")
	braveKey := blueprintEnvVar("BRAVE_SEARCH_API_KEY")
	if braveKey == "" {
		rl.Failf("BRAVE_SEARCH_API_KEY not set — required for this test (set in .env.local or environment)")
	}

	out, err := runCLIInDirWithHome(t, "", home,
		"agents", "mcp-servers", "configure", "brave_web_search",
		"api_key="+braveKey,
		"--project", projectID,
	)
	rl.CLIErr("memory agents mcp-servers configure brave_web_search api_key=*** --project "+projectID, out, err, 0)
	if err != nil {
		t.Fatalf("agents mcp-servers configure brave_web_search failed: %v\n%s", err, out)
	}
	rl.Printf("brave_web_search configured with project-level API key")
}

// createWorkPackage pre-creates a WorkPackage graph object with the given title
// and returns its canonical ID.
func createWorkPackage(t *testing.T, rl *runLog, home, projectID, title string) string {
	t.Helper()
	rl.Section("Create WorkPackage")
	wpPropsJSON, _ := json.Marshal(map[string]string{
		"title":  title,
		"status": "created",
	})
	wpCreateOut := mustRunCLIInDirWithHome(t, "", home,
		"graph", "objects", "create",
		"--type", "WorkPackage",
		"--properties", string(wpPropsJSON),
		"--project", projectID,
		"--output", "json",
	)
	rl.CLI("memory graph objects create --type WorkPackage", wpCreateOut)
	wpID := parseJSONField(wpCreateOut, "id")
	if wpID == "" {
		wpID = parseJSONField(wpCreateOut, "canonical_id")
	}
	if wpID == "" {
		rl.Failf("could not parse WorkPackage ID from: %q", wpCreateOut)
	}
	return wpID
}

// dumpAgents writes per-run detail logs for every agent in the slice.
func dumpAgents(t *testing.T, rl *runLog, srv, token, projectID string, agents []agentInfo) {
	t.Helper()
	names := make([]string, len(agents))
	ids := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
		ids[i] = a.ID
	}
	dumpAgentRunDetails(t, rl, srv, token, projectID, names, ids)
}

// printSummary opens a Summary section and prints the Gantt timeline and token
// usage summary for the given agents.
func printSummary(t *testing.T, rl *runLog, srv, token, projectID string, agents []agentInfo) {
	t.Helper()
	rl.Section("Summary")
	intervals := buildAgentRunIntervals(t, rl, srv, token, projectID, agents)
	printGanttTimeline(rl, intervals)
	printTokenUsageSummary(rl, intervals)
}

// suppressUnused prevents "declared but not used" errors for helpers that are
// only called from certain test files.
var _ = bytes.NewReader
var _ = io.Discard
