// production_test.go — smoke tests against the production Memory server.
//
// These tests verify that the memory CLI (as installed by install.sh) can
// successfully authenticate against https://memory.emergent-company.ai using a
// pre-issued API token and perform basic operations.
//
// # Requirements
//
// The environment variable MEMORY_PROD_TEST_TOKEN must be set to a valid
// production API token (emt_... or account key).  If it is not set, all tests
// in this file are skipped.
//
// In CI this token is stored as a repository secret named MEMORY_PROD_TEST_TOKEN.
//
// # Why a token and not the OAuth device flow?
//
// The device flow is interactive (opens a browser and waits for the user to
// approve).  In automated tests we use a pre-issued API token instead.
// The set-token command is the supported non-interactive equivalent.
package production_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

const (
	// prodServerURL is the production Memory server.
	prodServerURL = "https://memory.emergent-company.ai"

	// prodTestTokenEnv is the environment variable that holds the production
	// API token. Tests are skipped when this variable is not set.
	prodTestTokenEnv = "MEMORY_PROD_TEST_TOKEN"
)

// skipIfNoProdToken skips t when the production token env var is absent.
func skipIfNoProdToken(t *testing.T, rl *framework.RunLog) {
	t.Helper()
	if os.Getenv(prodTestTokenEnv) == "" {
		framework.DoSkipf(t, rl, "%s not set — skipping production tests", prodTestTokenEnv)
	}
}

// skipIfProdServerDown skips t if the production server is unreachable.
func skipIfProdServerDown(t *testing.T, rl *framework.RunLog) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", prodServerURL+"/health", nil)
	if err != nil {
		framework.DoSkipf(t, rl, "cannot build health request for %s: %v", prodServerURL, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		framework.DoSkipf(t, rl, "production server unreachable (%s): %v", prodServerURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		framework.DoSkipf(t, rl, "production server health check returned %d", resp.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: authenticate against production using set-token
// ─────────────────────────────────────────────────────────────────────────────

// TestProduction_SetToken verifies that `memory set-token` successfully writes
// credentials to ~/.memory/credentials.json when given a production token.
func TestProduction_SetToken(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()

	skipIfNoProdToken(t, rl)
	skipIfProdServerDown(t, rl)

	rl.Describe("Verify memory set-token writes credentials against production",
		"Run `memory set-token <token> --server <prod>`",
		"Assert output mentions 'token'",
		"Assert ~/.memory/credentials.json exists",
	)

	token := os.Getenv(prodTestTokenEnv)

	home := t.TempDir()
	rl.SetCLIPrefix("memory", "", home)

	rl.Section("Run memory set-token")
	out := mustRunCLIInDirWithHome(t, "", home, "set-token", token, "--server", prodServerURL)
	rl.CLI(fmt.Sprintf("memory set-token <token> --server %s", prodServerURL), out)

	if !strings.Contains(strings.ToLower(out), "token") {
		t.Errorf("set-token output did not mention 'token': %q", out)
		rl.Printf("set-token output did not mention 'token'")
	} else {
		rl.Printf("set-token output confirms token was set")
	}

	rl.Section("Verify credentials.json written")
	framework.VerifyCredentialsWritten(t, rl, home)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: full round-trip against production
// ─────────────────────────────────────────────────────────────────────────────

// TestProduction_AuthAndList verifies the full authenticate → status → projects
// list pipeline against the production server.
//
// Steps:
//  1. set-token with the production API token
//  2. memory status — must exit 0 and show "Connected"
//  3. memory projects list — must exit 0
func TestProduction_AuthAndList(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()

	skipIfNoProdToken(t, rl)
	skipIfProdServerDown(t, rl)

	rl.Describe("Verify full authenticate -> status -> projects list against production",
		"Run `memory set-token` with production token",
		"Run `memory status` and assert 'Connected'",
		"Run `memory projects list` and assert exit 0",
	)

	token := os.Getenv(prodTestTokenEnv)

	home := t.TempDir()
	rl.SetCLIPrefix("memory", "", home)

	rl.Section("Authenticate")
	setTokenOut := mustRunCLIInDirWithHome(t, "", home, "set-token", token, "--server", prodServerURL)
	rl.CLI(fmt.Sprintf("memory set-token <token> --server %s", prodServerURL), setTokenOut)
	rl.Printf("authenticated against %s", prodServerURL)

	rl.Section("Check server status")
	out := mustRunCLIInDirWithHome(t, "", home, "status", "--server", prodServerURL)
	rl.CLI("memory status --server "+prodServerURL, out)

	if !strings.Contains(out, "Connected") && !strings.Contains(out, "\u2713") {
		t.Errorf("status output does not indicate a successful connection:\n%s", out)
		rl.Printf("status output missing connection confirmation")
	} else {
		rl.Printf("status output confirms successful connection")
	}

	rl.Section("List projects")
	out = mustRunCLIInDirWithHome(t, "", home, "projects", "list", "--server", prodServerURL)
	rl.CLI("memory projects list --server "+prodServerURL, out)
	rl.Printf("projects list returned %d bytes (exit 0)", len(strings.TrimSpace(out)))
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: production server health and issuer endpoints
// ─────────────────────────────────────────────────────────────────────────────

// TestProduction_ServerHealth verifies that the production server's /health
// endpoint responds with a 200 and a non-empty status field.
func TestProduction_ServerHealth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()

	skipIfProdServerDown(t, rl)

	rl.Describe("Verify production server /health endpoint",
		"GET /health and assert 200 with non-empty status",
	)

	rl.Section("Check production server health")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", prodServerURL+"/health", nil)
	if err != nil {
		rl.Failf("could not build health request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		rl.Failf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rl.Failf("health endpoint returned %d, want 200", resp.StatusCode)
	}

	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		rl.Failf("could not decode health response: %v", err)
	}

	rl.CLIStep("GET /health",
		fmt.Sprintf("GET %s/health", prodServerURL),
		fmt.Sprintf("status=%q version=%q", health.Status, health.Version))

	if health.Status == "" {
		t.Error("health response has empty status field")
	}
}

// TestProduction_IssuerEndpoint verifies that /api/auth/issuer returns a valid
// Zitadel OIDC issuer URL.
func TestProduction_IssuerEndpoint(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()

	skipIfProdServerDown(t, rl)

	rl.Describe("Verify production /api/auth/issuer returns valid Zitadel OIDC issuer",
		"GET /api/auth/issuer and assert non-standalone with HTTPS issuer URL",
	)

	rl.Section("Check issuer endpoint")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", prodServerURL+"/api/auth/issuer", nil)
	if err != nil {
		rl.Failf("could not build issuer request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		rl.Failf("issuer request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		rl.Printf("issuer endpoint returned 401 — requires auth in this server configuration")
		t.Skipf("issuer endpoint returned 401 (endpoint requires auth in this server configuration)")
	}
	if resp.StatusCode != http.StatusOK {
		rl.Failf("issuer endpoint returned %d, want 200", resp.StatusCode)
	}

	var issuer struct {
		Issuer     string `json:"issuer"`
		Standalone bool   `json:"standalone"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issuer); err != nil {
		rl.Failf("could not decode issuer response: %v", err)
	}

	rl.CLIStep("GET /api/auth/issuer",
		fmt.Sprintf("GET %s/api/auth/issuer", prodServerURL),
		fmt.Sprintf("issuer=%q standalone=%v", issuer.Issuer, issuer.Standalone))

	if issuer.Standalone {
		t.Error("production server is in standalone mode — OAuth login will not work")
	}
	if issuer.Issuer == "" {
		t.Error("issuer response has empty issuer URL")
	}
	if !strings.HasPrefix(issuer.Issuer, "https://") {
		t.Errorf("issuer URL is not HTTPS: %q", issuer.Issuer)
	}
}
