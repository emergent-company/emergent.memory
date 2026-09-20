// Package tools_test — helpers_test.go
//
// Thin wrappers around framework exported functions, plus tools-specific
// helpers shared across all files in this package.
package tools_test

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
// Constants shared across tools tests
// ─────────────────────────────────────────────────────────────────────────────

const (
	// blueprintURL is the local path to the workspace-memory-blueprint directory.
	blueprintURL = "/root/workspace-memory-blueprint"

	// taskCLIDir is the source directory for the task-cli tool.
	taskCLIDir = "/root/workspace-memory-blueprint/tools/task-cli"

	// pollInterval is how often to re-check WorkPackage/agent status.
	pollInterval = 5 * time.Second
)

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

func compactRunsOutput(runsOut string) string {
	return framework.CompactRunsOutput(runsOut)
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

func logStatusPreamble(t *testing.T, home ...string) {
	t.Helper()
	framework.LogStatusPreamble(t, home...)
}

// ─────────────────────────────────────────────────────────────────────────────
// Skip guards
// ─────────────────────────────────────────────────────────────────────────────

// skipIfNoBraveKey skips t when BRAVE_SEARCH_API_KEY is not set.
func skipIfNoBraveKey(t *testing.T, rl *framework.RunLog) {
	t.Helper()
	if os.Getenv("BRAVE_SEARCH_API_KEY") == "" {
		framework.DoSkipf(t, rl, "BRAVE_SEARCH_API_KEY not set — skipping Brave Search tests")
	}
}

// skipIfNoGoogleAIKey skips t when GOOGLE_AI_API_KEY is not set.
func skipIfNoGoogleAIKey(t *testing.T, rl *framework.RunLog) {
	t.Helper()
	if os.Getenv("GOOGLE_AI_API_KEY") == "" {
		framework.DoSkipf(t, rl, "GOOGLE_AI_API_KEY not set — skipping Google AI tests")
	}
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

// runTaskCLI runs the task-cli binary with the given arguments and returns
// combined stdout+stderr output. It returns an error when the process exits
// with a non-zero status.
func runTaskCLI(t *testing.T, bin, srv, apiKey, projectID string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = []string{
		"MEMORY_SERVER_URL=" + srv,
		"MEMORY_API_KEY=" + apiKey,
		"MEMORY_PROJECT_ID=" + projectID,
		"HOME=" + os.TempDir(),
		"PATH=" + os.Getenv("PATH"),
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
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
func parseJSONField(jsonStr, field string) string { return framework.ParseJSONField(jsonStr, field) }

func projectCreateOrgArgs() []string { return framework.ProjectCreateOrgArgs() }
func orgIDArgs() []string            { return framework.OrgIDArgs() }

// prettyJSON re-indents a JSON string for human-readable log output.
func prettyJSON(raw string) string {
	return framework.PrettyJSONOutput(raw)
}

// jsonStringLiteral encodes s as a JSON string literal.
func jsonStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
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

// ─────────────────────────────────────────────────────────────────────────────
// Agent helpers
// ─────────────────────────────────────────────────────────────────────────────

func dumpAgentRunDetails(t *testing.T, rl *runLog, srv, token, projectID string, agentNames, agentIDs []string) {
	framework.DumpAgentRunDetails(t, rl, srv, token, projectID, agentNames, agentIDs)
}

// ─────────────────────────────────────────────────────────────────────────────
// Brave Search helpers
// ─────────────────────────────────────────────────────────────────────────────

// getBraveToolIDs returns the builtin server ID and the brave_web_search tool ID.
func getBraveToolIDs(t *testing.T, srv, token, projectID string) (serverID, toolID string) {
	t.Helper()
	sid, tool := getBraveToolIDsWithDetail(t, srv, token, projectID)
	if tool == nil {
		t.Skip("brave_web_search not found in builtin tools — skipping")
	}
	id, _ := tool["id"].(string)
	return sid, id
}

// getBraveToolIDsWithDetail returns the builtin server ID and the full
// brave_web_search tool object (or nil if not found).
// Uses the /api/mcp/rpc endpoint and protocol version "2025-06-18".
// Tool objects use the "toolName" field (not "name").
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

	// Now list tools for this server
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()

	req2, _ := http.NewRequestWithContext(ctx2, "GET",
		fmt.Sprintf("%s/api/admin/mcp-servers/%s/tools", srv, serverID), nil)
	setAuthHeader(req2, token)
	req2.Header.Set("X-Project-ID", projectID)

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("list tools: %v", err)
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
		if name, _ := tl["toolName"].(string); name == "brave_web_search" {
			return serverID, tl
		}
	}
	return serverID, nil
}

// getOrgIDForProject fetches the project detail and returns its orgId.
func getOrgIDForProject(t *testing.T, srv, token, projectID string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET",
		srv+"/api/projects/"+projectID, nil)
	setAuthHeader(req, token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("get project: want 200, got %d — %s", resp.StatusCode, body)
	}

	var result struct {
		OrgID string `json:"orgId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode project response: %v", err)
	}

	if result.OrgID == "" {
		t.Fatal("project response missing orgId field")
	}
	return result.OrgID
}

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

// callBraveSearchViaMCP sends a JSON-RPC tools/call request to the MCP endpoint
// and returns the parsed search results array from the tool response.
// It first calls initialize (required by the MCP protocol), captures the
// Mcp-Session-Id header, then calls tools/call with that session ID.
// Uses /api/mcp/rpc endpoint and protocol version "2025-06-18".
func callBraveSearchViaMCP(t *testing.T, srv, token, projectID, query string) []map[string]any {
	t.Helper()

	mcpURL := srv + "/api/mcp/rpc"
	const protocolVersion = "2025-06-18"

	// 1. Initialize session — capture Mcp-Session-Id from response header.
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

	// 2. Send initialized notification (required by MCP spec before tools/call).
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

	// Parse the JSON-RPC result
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
		// Extract error text from content
		var msgs []string
		for _, c := range rpcResp.Result.Content {
			msgs = append(msgs, c.Text)
		}
		t.Fatalf("brave_web_search returned error: %s", strings.Join(msgs, "; "))
	}

	// The tool returns results as a JSON array in the first text content block.
	for _, c := range rpcResp.Result.Content {
		if c.Type != "text" || c.Text == "" {
			continue
		}
		// Try to parse as JSON array of result objects
		var results []map[string]any
		if err := json.Unmarshal([]byte(c.Text), &results); err == nil {
			return results
		}
		// It may be a JSON object with a "results" key
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
		// Fallback: treat non-empty text as 1 result (confirms the tool ran)
		if strings.TrimSpace(c.Text) != "" {
			t.Logf("brave_web_search returned non-JSON text — treating as 1 result")
			return []map[string]any{{"text": c.Text}}
		}
	}

	return nil
}
