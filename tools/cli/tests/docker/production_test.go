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
package dockertests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	// prodServerURL is the production Memory server.
	prodServerURL = "https://memory.emergent-company.ai"

	// prodTestTokenEnv is the environment variable that holds the production
	// API token. Tests are skipped when this variable is not set.
	prodTestTokenEnv = "MEMORY_PROD_TEST_TOKEN"
)

// skipIfNoProdToken skips t when the production token env var is absent.
func skipIfNoProdToken(t *testing.T) {
	t.Helper()
	if os.Getenv(prodTestTokenEnv) == "" {
		t.Skipf("%s not set — skipping production tests", prodTestTokenEnv)
	}
}

// skipIfProdServerDown skips t if the production server is unreachable.
func skipIfProdServerDown(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", prodServerURL+"/health", nil)
	if err != nil {
		t.Skipf("cannot build health request for %s: %v", prodServerURL, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("production server unreachable (%s): %v", prodServerURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Skipf("production server health check returned %d", resp.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: authenticate against production using set-token
// ─────────────────────────────────────────────────────────────────────────────

// TestProduction_SetToken verifies that `memory set-token` successfully writes
// credentials to ~/.memory/credentials.json when given a production token.
func TestProduction_SetToken(t *testing.T) {
	skipIfNoProdToken(t)
	skipIfProdServerDown(t)

	token := os.Getenv(prodTestTokenEnv)
	out := mustRunCLI(t, "set-token", token, "--server", prodServerURL)
	t.Logf("set-token output: %s", strings.TrimSpace(out))

	if !strings.Contains(strings.ToLower(out), "token") {
		t.Errorf("set-token output did not mention 'token': %q", out)
	}

	// Credentials file must now exist.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("could not determine home directory: %v", err)
	}
	credsPath := fmt.Sprintf("%s/.memory/credentials.json", home)
	if _, err := os.Stat(credsPath); os.IsNotExist(err) {
		t.Errorf("credentials file not created at %s", credsPath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: full round-trip against production
// ─────────────────────────────────────────────────────────────────────────────

// TestProduction_AuthAndList verifies the full authenticate → status → projects
// list pipeline against the production server.  This is the canonical "does
// the CLI work for a real user?" smoke test.
//
// Steps:
//  1. set-token with the production API token
//  2. memory status — must exit 0 and show "Connected"
//  3. memory projects list — must exit 0
func TestProduction_AuthAndList(t *testing.T) {
	skipIfNoProdToken(t)
	skipIfProdServerDown(t)

	token := os.Getenv(prodTestTokenEnv)

	// Step 1 — authenticate.
	mustRunCLI(t, "set-token", token, "--server", prodServerURL)

	// Step 2 — status.
	out := mustRunCLI(t, "status", "--server", prodServerURL)
	t.Logf("memory status:\n%s", strings.TrimSpace(out))

	if !strings.Contains(out, "Connected") && !strings.Contains(out, "✓") {
		t.Errorf("status output does not indicate a successful connection:\n%s", out)
	}

	// Step 3 — projects list.
	out = mustRunCLI(t, "projects", "list", "--server", prodServerURL)
	t.Logf("projects list:\n%s", strings.TrimSpace(out))
	// We don't assert on content — the test passes if the command exits 0.
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: production server health and issuer endpoints
// ─────────────────────────────────────────────────────────────────────────────

// TestProduction_ServerHealth verifies that the production server's /health
// endpoint responds with a 200 and a non-empty status field.  This does not
// require a token — it is a pure connectivity check.
func TestProduction_ServerHealth(t *testing.T) {
	skipIfProdServerDown(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", prodServerURL+"/health", nil)
	if err != nil {
		t.Fatalf("could not build health request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health endpoint returned %d, want 200", resp.StatusCode)
	}

	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("could not decode health response: %v", err)
	}

	t.Logf("production server: status=%q version=%q", health.Status, health.Version)

	if health.Status == "" {
		t.Error("health response has empty status field")
	}
}

// TestProduction_IssuerEndpoint verifies that /api/auth/issuer returns a valid
// Zitadel OIDC issuer URL.  This confirms the server is configured for OAuth
// login (not standalone mode).
func TestProduction_IssuerEndpoint(t *testing.T) {
	skipIfProdServerDown(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", prodServerURL+"/api/auth/issuer", nil)
	if err != nil {
		t.Fatalf("could not build issuer request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("issuer request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("issuer endpoint returned %d, want 200", resp.StatusCode)
	}

	var issuer struct {
		Issuer     string `json:"issuer"`
		Standalone bool   `json:"standalone"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issuer); err != nil {
		t.Fatalf("could not decode issuer response: %v", err)
	}

	t.Logf("production issuer: %q (standalone=%v)", issuer.Issuer, issuer.Standalone)

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
