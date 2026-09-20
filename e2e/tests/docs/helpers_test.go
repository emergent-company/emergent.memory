// Package docs_test — helpers_test.go
//
// Thin wrappers around framework exported functions.
package docs_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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
	if v, ok := framework.ActiveRunLogs.Load(t.Name()); ok {
		if _, ok := v.(*framework.RunLog); ok {
			return
		}
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

// httpGet performs a simple GET request with a timeout, returning the body and
// any error.  Used by docs site tests to check page availability.
func httpGet(url string, timeout time.Duration) (body string, statusCode int, err error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(b), resp.StatusCode, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Parse helpers
// ─────────────────────────────────────────────────────────────────────────────

func parseProjectID(output string) string   { return framework.ParseProjectID(output) }
func parseAgentID(output string) string     { return framework.ParseAgentID(output) }
func parseJSONField(s, field string) string { return framework.ParseJSONField(s, field) }
func parseLineField(s, key string) string   { return framework.ParseLineField(s, key) }

// parseAgentDefID extracts a definition ID from `memory agent-definitions create` output.
func parseAgentDefID(output string) string {
	for _, label := range []string{"ID:", "Definition ID:", "id:"} {
		if id := parseLineField(output, label); id != "" {
			return id
		}
	}
	if id := parseJSONField(output, "id"); id != "" {
		return id
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Token helpers
// ─────────────────────────────────────────────────────────────────────────────

func setToken() string { return framework.SetToken() }

func revokeTokenOnCleanup(t *testing.T, rl *runLog, home, tokenID string) {
	t.Helper()
	framework.RevokeTokenOnCleanup(t, rl, home, tokenID)
}

// skipIfEndpointMissing performs a HEAD check on the given API path and skips
// the test when the endpoint doesn't exist (404/405).
func skipIfEndpointMissing(t *testing.T, path, bearerToken string, rl ...*framework.RunLog) {
	t.Helper()
	var runlog *framework.RunLog
	if len(rl) > 0 {
		runlog = rl[0]
	}
	srv := serverURL()
	url := srv + path
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		framework.DoSkipf(t, runlog, "skipIfEndpointMissing: bad request: %v", err)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		framework.DoSkipf(t, runlog, "skipIfEndpointMissing: %s unreachable: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 405 {
		framework.DoSkipf(t, runlog, "endpoint %s not found (HTTP %d) — skipping", path, resp.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Org helpers
// ─────────────────────────────────────────────────────────────────────────────

func orgIDArgs() []string            { return framework.OrgIDArgs() }
func projectCreateOrgArgs() []string { return framework.ProjectCreateOrgArgs() }

// ─────────────────────────────────────────────────────────────────────────────
// Pre-check helpers
// ─────────────────────────────────────────────────────────────────────────────

func requireServerReady(t *testing.T, home string, rl ...*framework.RunLog) {
	t.Helper()
	framework.RequireServerReady(t, home, rl...)
}

// skipIfDocsUnreachable skips the test if the documentation site cannot be
// reached (e.g. no internet in CI or air-gapped environment).
func skipIfDocsUnreachable(t *testing.T, rl *framework.RunLog) {
	t.Helper()
	const docsBaseURL = "https://emergent-company.github.io/emergent.memory/latest/"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(docsBaseURL)
	if err != nil {
		framework.DoSkipf(t, rl, "docs site unreachable (%s): %v — skipping", docsBaseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		framework.DoSkipf(t, rl, "docs site returned %d (%s) — skipping", resp.StatusCode, docsBaseURL)
	}
}
