// Package cli_test — helpers_test.go
//
// Thin wrappers around framework exported functions.
package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// ─────────────────────────────────────────────────────────────────────────────
// Type aliases
// ─────────────────────────────────────────────────────────────────────────────

type runLog = framework.RunLog

func newRunLog(t *testing.T) *runLog {
	t.Helper()
	return framework.NewRunLog(t)
}

func describe(rl *runLog, summary string, bullets ...string) {
	rl.Describe(summary, bullets...)
}

// ─────────────────────────────────────────────────────────────────────────────
// Server helpers
// ─────────────────────────────────────────────────────────────────────────────

func serverURL() string    { return framework.ServerURL() }
func e2eTestToken() string { return framework.E2ETestToken() }
func skipIfServerDown(t *testing.T, rl *framework.RunLog) {
	t.Helper()
	framework.SkipIfServerDown(t, rl)
}
func filteredEnv() []string { return framework.FilteredEnv() }

func setupCLIAuth(t *testing.T, home string) {
	t.Helper()
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if rl, ok := v.(*framework.RunLog); ok {
			rl.Section("Setup CLI auth")
		}
	}
	framework.SetupCLIAuth(t, home)
}

// ─────────────────────────────────────────────────────────────────────────────
// CLI helpers
// ─────────────────────────────────────────────────────────────────────────────

func mustRunCLI(t *testing.T, args ...string) string {
	t.Helper()
	return mustRunCLIInDir(t, "", args...)
}

func mustRunCLIInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return mustRunCLIInDirWithHome(t, dir, t.TempDir(), args...)
}

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

// runCLIInDirWithHome is the non-fatal variant — returns (output, error)
// instead of calling t.Fatal on non-zero exit.
func runCLIInDirWithHome(t *testing.T, dir, home string, args ...string) (string, error) {
	t.Helper()
	out, err := framework.RunCLIInDirWithHome(t, dir, home, args...)
	invocation := fmt.Sprintf("memory %s", strings.Join(args, " "))
	emitCLIErr(t, invocation, out, err)
	return out, err
}

func logStatusPreamble(t *testing.T, home ...string) {
	t.Helper()
	framework.LogStatusPreamble(t, home...)
}

func logSession(t *testing.T, invocation, output string) {
	t.Helper()
	if _, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		return
	}
	framework.LogSession(t, invocation, output)
}

// ─────────────────────────────────────────────────────────────────────────────
// Project helpers
// ─────────────────────────────────────────────────────────────────────────────

func createProject(t *testing.T, home, srv, name string) string {
	t.Helper()
	return framework.CreateProject(t, home, srv, name)
}

func deleteProjectOnCleanup(t *testing.T, home, projectID string) {
	t.Helper()
	framework.DeleteProjectOnCleanup(t, home, projectID)
}

func uniqueProjectName(prefix string) string {
	return framework.UniqueProjectName(prefix)
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

func readBody(t *testing.T, resp *http.Response) string {
	return framework.ReadBody(t, resp)
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
func parseLineField(s, key string) string         { return framework.ParseLineField(s, key) }

func parseFrontmatterFields(content string) (name, description string) {
	return framework.ParseFrontmatterFields(content)
}

func prettyJSONOutput(output string) string { return framework.PrettyJSONOutput(output) }

// parseEntityID extracts an entity UUID from CLI create output.
// It handles multiple output formats:
//   - Tab-delimited: "<uuid>\t<type>\t<name>"  (graph objects/relationships create)
//   - Labelled:      "ID:   <uuid>"  or  "Entity ID:   <uuid>"
//   - JSON:          {"entity_id": "<uuid>", ...} or {"id": "<uuid>", ...}
//
// Returns "" if no UUID could be extracted.
func parseEntityID(output string) string {
	// Try labelled fields first (most explicit).
	if id := parseLineField(output, "Entity ID:"); id != "" {
		return id
	}
	if id := parseLineField(output, "ID:"); id != "" {
		return id
	}
	// Try JSON.
	if id := parseJSONField(output, "entity_id"); id != "" {
		return id
	}
	if id := parseJSONField(output, "id"); id != "" {
		return id
	}
	// Try tab-delimited: first field of first non-empty line should be a UUID.
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		candidate := strings.TrimSpace(fields[0])
		// Minimal UUID check: 36 chars, 4 hyphens.
		if len(candidate) == 36 && strings.Count(candidate, "-") == 4 {
			return candidate
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Org helpers
// ─────────────────────────────────────────────────────────────────────────────

func orgIDArgs() []string            { return framework.OrgIDArgs() }
func projectCreateOrgArgs() []string { return framework.ProjectCreateOrgArgs() }

// ─────────────────────────────────────────────────────────────────────────────
// Structured step API helpers
// ─────────────────────────────────────────────────────────────────────────────

// Type aliases for the structured step API.
type testOpts = framework.TestOpts
type testContext = framework.TestContext
type step = framework.Step

func newTest(t *testing.T, opts testOpts) *testContext {
	t.Helper()
	return framework.NewTest(t, opts)
}

// ─────────────────────────────────────────────────────────────────────────────
// Provider helpers
// ─────────────────────────────────────────────────────────────────────────────

// setupTestProvider configures whichever LLM provider is available from
// environment variables (DEEPSEEK_API_KEY > GOOGLE_AI_API_KEY > OPENAI_API_KEY).
// The test is skipped when no provider env vars are set.
func setupTestProvider(t *testing.T, rl *runLog, home, projectID string) {
	t.Helper()
	framework.SetupTestProvider(t, rl, home, projectID)
}

// skipIfNoLLMProvider skips t when no LLM provider env var is set.
func skipIfNoLLMProvider(t *testing.T, rl *runLog) {
	t.Helper()
	provider, _, _ := framework.ProviderFromEnv()
	if provider == "" {
		framework.DoSkipf(t, rl,
			"no LLM provider configured — set DEEPSEEK_API_KEY, GOOGLE_AI_API_KEY, or OPENAI_API_KEY")
	}
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
// JSON helpers
// ─────────────────────────────────────────────────────────────────────────────

// countJSONArrayItems counts the number of items in a JSON array string.
// Handles both a bare array "[...]" and a wrapped object with an "items" key
// ("{"items":[...],...}") as returned by newer server versions.
// Returns 0 if the input is not a valid JSON array or object.
func countJSONArrayItems(jsonStr string) int {
	trimmed := strings.TrimSpace(jsonStr)
	// Try top-level array first (original format).
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
		return len(arr)
	}
	// Try wrapped object with "items" key (newer server format).
	var wrapper struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err == nil {
		return len(wrapper.Items)
	}
	return 0
}

// ─────────────────────────────────────────────────────────────────────────────
// Cost tracking helpers
// ─────────────────────────────────────────────────────────────────────────────

// captureAskCost attempts to extract and record token usage/cost from a
// `memory ask` operation.  It tries to:
//  1. Extract a run ID from the ask output (stderr may contain trace info)
//  2. Query the agent-runs API to fetch token usage
//  3. Record the usage via rl.RecordTokenUsage()
//
// This is best-effort — if the run ID can't be found or the API call fails,
// no cost is recorded (but no error is raised).
//
// Parameters:
//   - t: testing.T for logging
//   - rl: RunLog to record cost to (may be nil)
//   - projectID: project ID the ask was scoped to
//   - askOutput: combined stdout+stderr from `memory ask`
func captureAskCost(t *testing.T, rl *runLog, projectID, askOutput string) {
	t.Helper()
	if rl == nil || projectID == "" {
		return
	}

	// Try to extract run ID from output.  The ask command may print trace info
	// to stderr that includes the run ID in UUID format.
	runID := extractUUIDFromText(askOutput)
	if runID == "" {
		// No run ID found in output — this is expected for non-LLM asks.
		return
	}

	srv := serverURL()
	token := e2eTestToken()

	// Fetch token usage from the server.
	inputTokens, outputTokens, costUSD := framework.FetchRunTokenUsage(t, srv, token, projectID, runID)
	if inputTokens > 0 || outputTokens > 0 {
		rl.RecordTokenUsage(inputTokens, outputTokens, costUSD)
	}
}

// configureProjectModel sets the generative model for a project via the API.
// Skips if no LLM provider is configured in env vars.
func configureProjectModel(t *testing.T, projectID string) {
	t.Helper()
	provider, apiKey, model := framework.ProviderFromEnv()
	if provider == "" || model == "" {
		return // no provider configured — skip silently
	}
	srv := serverURL()
	token := e2eTestToken()
	cfg, _ := json.Marshal(map[string]any{"generativeModel": provider + "/" + model})
	resp := framework.DoJSON(t, "PUT", srv+"/api/v1/projects/"+projectID+"/model-config", token, projectID, cfg)
	resp.Body.Close()

	// For OpenAI-compatible providers, also configure the API key at the project level.
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
			provCfg, _ := json.Marshal(map[string]any{"apiKey": apiKey, "generativeModel": model})
			req, _ := http.NewRequest("PUT", srv+"/api/v1/projects/"+projectID+"/providers/"+provider, bytes.NewReader(provCfg))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Project-ID", projectID)
			req.Header.Set("X-Org-ID", orgID)
			if r, err := http.DefaultClient.Do(req); err == nil {
				r.Body.Close()
			}
		}
	}
}
