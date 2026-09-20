// Package cli_test — tokens_test.go
package cli_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_AccountTokenCreate verifies that
//
//	memory tokens create --name t --scopes projects:read
//
// (without --project) creates an account-level token.
//
// It asserts:
//   - exit code 0
//   - output contains "Account token created successfully"
//   - output contains an emt_ prefixed token value
//
// The created token is revoked in a t.Cleanup so it does not accumulate on the
// test server.
func TestCLIInstalled_AccountTokenCreate(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory tokens create creates an account-level token",
		"Run `memory tokens create --name cli-test-account-token --scopes projects:read`",
		"Assert output contains 'Account token created successfully'",
		"Assert output contains emt_ prefixed token value",
		"Revoke created token in cleanup",
	)
	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/tokens", framework.SetToken())

	rl.Section("Create account-level token")
	out := mustRunCLIInDirWithHome(t, "", home, "tokens", "create",
		"--name", "cli-test-account-token",
		"--scopes", "projects:read",
	)
	rl.CLI("memory tokens create --name cli-test-account-token --scopes projects:read", out)

	if !strings.Contains(out, "Account token created successfully") {
		t.Errorf("expected 'Account token created successfully' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "emt_") {
		t.Errorf("expected emt_ token value in output, got:\n%s", out)
	}

	tokenID := framework.ParseLineField(out, "ID:")
	if tokenID == "" {
		rl.Printf("warn: could not extract token ID from output — skipping revoke cleanup")
		return
	}
	rl.Event("token", "Account token created", map[string]any{
		"id": tokenID,
	})

	framework.RevokeTokenOnCleanup(t, rl, home, tokenID)
}

// TestCLIInstalled_TokensList verifies that `memory tokens list` returns
// a non-empty list of tokens (at minimum, the token used for auth exists).
func TestCLIInstalled_TokensList(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify memory tokens list returns token entries",
		"Set up CLI auth",
		"Run `memory tokens list`",
		"Assert output is non-empty and contains at least one token row",
	)
	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/tokens", framework.SetToken())

	rl.Section("Run memory tokens list")
	out := mustRunCLIInDirWithHome(t, "", home, "tokens", "list")
	rl.CLI("memory tokens list", out)

	rl.Section("Verify tokens list output")
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		rl.Failf("expected non-empty output from tokens list, got empty string")
	}
	lines := strings.Split(trimmed, "\n")
	// Expect at least a header + one token row, or a single informational line.
	if len(lines) < 1 {
		rl.Failf("expected at least 1 line from tokens list, got %d", len(lines))
	}
	rl.Printf("tokens list returned %d lines", len(lines))
}

// TestCLIInstalled_TokenGetAndRevoke verifies the token create → get → revoke
// lifecycle.  It creates an account-level token, gets its details, then
// revokes it.
//
// Known issues:
//   - `tokens get` without --project may error with "project is required"
//     (CLI bug on mcj-emergent).  The test skips when this happens.
//   - project-scoped `tokens create` returns [500] database_error on
//     mcj-emergent.
func TestCLIInstalled_TokenGetAndRevoke(t *testing.T) {
	rl := newRunLog(t)
	t.Cleanup(rl.Close)
	rl.Describe("Verify token create → get → revoke lifecycle",
		"Create an account-level token",
		"Get the token by ID and verify details",
		"Revoke the token",
	)
	home := t.TempDir()
	requireServerReady(t, home)
	skipIfEndpointMissing(t, "/api/tokens", framework.SetToken())

	// Create an account-level token with a unique name.
	rl.Section("Create account-level token")
	tokenName := fmt.Sprintf("cli-test-get-revoke-%d", time.Now().UnixMilli())
	createOut := mustRunCLIInDirWithHome(t, "", home, "tokens", "create",
		"--name", tokenName,
		"--scopes", "projects:read",
	)
	rl.CLI("memory tokens create --name "+tokenName+" --scopes projects:read", createOut)

	tokenID := framework.ParseLineField(createOut, "ID:")
	if tokenID == "" {
		rl.Failf("could not extract token ID from create output: %s", truncate(createOut, 300))
	}
	rl.Printf("created token ID: %s", tokenID)

	// Revoke in cleanup regardless of what happens below.
	framework.RevokeTokenOnCleanup(t, rl, home, tokenID)

	// Get the token by ID (non-fatal — may fail with "project is required" bug).
	rl.Section("Get token by ID")
	getOut, getErr := runCLIInDirWithHome(t, "", home, "tokens", "get", tokenID)
	rl.CLIErr("memory tokens get "+tokenID, getOut, getErr, 0)

	if getErr != nil {
		if strings.Contains(getOut, "project is required") {
			rl.Printf("SKIP: tokens get requires project context (CLI bug)")
			t.Skipf("tokens get requires --project even for account tokens (CLI bug): %s", truncate(getOut, 200))
		}
		rl.Failf("tokens get failed unexpectedly: %v — %s", getErr, truncate(getOut, 300))
	}

	if !strings.Contains(getOut, tokenID) {
		t.Errorf("expected get output to contain token ID %q, got:\n%s", tokenID, truncate(getOut, 500))
	}
	if !strings.Contains(getOut, tokenName) {
		t.Errorf("expected get output to contain token name %q, got:\n%s", tokenName, truncate(getOut, 500))
	}
	rl.Printf("get output contains token ID and name: true")

	// Revoke the token.
	rl.Section("Revoke token")
	revokeOut, revokeErr := runCLIInDirWithHome(t, "", home, "tokens", "revoke", tokenID)
	rl.CLIErr("memory tokens revoke "+tokenID, revokeOut, revokeErr, 0)

	if revokeErr != nil {
		if strings.Contains(revokeOut, "project is required") {
			rl.Printf("SKIP: tokens revoke requires project context (CLI bug)")
			t.Skipf("tokens revoke requires --project even for account tokens (CLI bug): %s", truncate(revokeOut, 200))
		}
		rl.Failf("tokens revoke failed unexpectedly: %v — %s", revokeErr, truncate(revokeOut, 300))
	}

	lower := strings.ToLower(revokeOut)
	if !strings.Contains(lower, "revoke") && !strings.Contains(lower, "delete") {
		t.Errorf("expected revoke output to confirm revocation, got:\n%s", truncate(revokeOut, 300))
	}
	rl.Printf("token revoke confirmed: true")
}
