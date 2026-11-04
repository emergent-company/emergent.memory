package main

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkauth "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
)

// pkceTestDeps returns offline, deterministic Deps for the PKCE CLI tests. The
// clock is shared with the caller so expiry can be advanced.
func pkceTestDeps(now *time.Time) account.Deps {
	return account.Deps{
		DiscoverIssuer: func(context.Context, string) (string, error) { return "https://issuer.test", nil },
		DiscoverOIDC: func(issuer string) (*sdkauth.OIDCConfig, error) {
			return &sdkauth.OIDCConfig{
				Issuer:                issuer,
				AuthorizationEndpoint: "https://issuer.test/authorize",
				TokenEndpoint:         "https://issuer.test/token",
			}, nil
		},
		FetchIdentity: func(context.Context, string, string) (account.Identity, error) {
			return account.Identity{UserID: "user-1", Email: "user@example.com"}, nil
		},
		ExchangeCode: func(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error) {
			return &sdkauth.Credentials{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: now.Add(time.Hour)}, nil
		},
		Now: func() time.Time { return *now },
	}
}

func withFakePKCEManager(t *testing.T, deps account.Deps) {
	t.Helper()
	old := newAuthManager
	newAuthManager = func(baseDir string) *account.Manager {
		return account.NewManagerWithDeps(baseDir, deps)
	}
	t.Cleanup(func() { newAuthManager = old })
}

func TestAuthStartJSON(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	withFakePKCEManager(t, pkceTestDeps(&now))

	code, stdout, stderr := runAuthCapture("start", "--config", configPath, "--server", "https://memory.example.test", "--redirect-uri", "myapp://callback", "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		SchemaVersion int    `json:"schema_version"`
		LoginID       string `json:"login_id"`
		AuthorizeURL  string `json:"authorize_url"`
		State         string `json:"state"`
		ExpiresAt     string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal start JSON: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 || doc.LoginID == "" || doc.State == "" || doc.ExpiresAt == "" {
		t.Errorf("start doc = %+v", doc)
	}
	for _, want := range []string{"response_type=code", "code_challenge_method=S256", "code_challenge="} {
		if !strings.Contains(doc.AuthorizeURL, want) {
			t.Errorf("authorize_url %q missing %q", doc.AuthorizeURL, want)
		}
	}
	if strings.Contains(stdout, "code_verifier") {
		t.Errorf("start output must not expose the code verifier:\n%s", stdout)
	}
}

func TestAuthStartText(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	withFakePKCEManager(t, pkceTestDeps(&now))

	code, stdout, stderr := runAuthCapture("start", "--config", configPath, "--server", "https://memory.example.test", "--redirect-uri", "myapp://callback")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"memory-connector auth start", "authorize url:", "login id:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestAuthCompleteSignsIn(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	serverURL := "https://memory.example.test"
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	withFakePKCEManager(t, pkceTestDeps(&now))

	_, startOut, stderr := runAuthCapture("start", "--config", configPath, "--server", serverURL, "--redirect-uri", "myapp://callback", "--json")
	if stderr != "" {
		t.Fatalf("start stderr = %q", stderr)
	}
	var startDoc struct {
		LoginID string `json:"login_id"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal([]byte(startOut), &startDoc); err != nil {
		t.Fatalf("unmarshal start doc: %v\n%s", err, startOut)
	}

	code, stdout, stderr := runAuthCapture("complete", "--config", configPath, "--server", serverURL, "--login-id", startDoc.LoginID, "--code", "code-1", "--state", startDoc.State)
	if code != 0 {
		t.Fatalf("complete code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "memory-connector auth status") || !strings.Contains(stdout, "signed in: yes") {
		t.Errorf("complete stdout = %q, want signed-in status", stdout)
	}

	// The account persists and status reports it.
	code, statusOut, stderr := runAuthCapture("status", "--config", configPath, "--server", serverURL, "--json")
	if code != 0 {
		t.Fatalf("status code = %d (stderr: %s)", code, stderr)
	}
	var statusDoc struct {
		SignedIn bool   `json:"signed_in"`
		Email    string `json:"email"`
	}
	if err := json.Unmarshal([]byte(statusOut), &statusDoc); err != nil {
		t.Fatalf("unmarshal status doc: %v\n%s", err, statusOut)
	}
	if !statusDoc.SignedIn || statusDoc.Email != "user@example.com" {
		t.Errorf("status doc = %+v, want signed in as user@example.com", statusDoc)
	}
}

func TestAuthCompleteStateMismatch(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	serverURL := "https://memory.example.test"
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	withFakePKCEManager(t, pkceTestDeps(&now))

	_, startOut, _ := runAuthCapture("start", "--config", configPath, "--server", serverURL, "--redirect-uri", "myapp://callback", "--json")
	var startDoc struct {
		LoginID string `json:"login_id"`
	}
	if err := json.Unmarshal([]byte(startOut), &startDoc); err != nil {
		t.Fatalf("unmarshal start doc: %v", err)
	}

	code, _, stderr := runAuthCapture("complete", "--config", configPath, "--server", serverURL, "--login-id", startDoc.LoginID, "--code", "code-1", "--state", "wrong")
	if code != 1 {
		t.Fatalf("complete code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "state mismatch") {
		t.Errorf("stderr = %q, want a state mismatch message", stderr)
	}
}

func TestAuthCompleteUnknownLogin(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	withFakePKCEManager(t, pkceTestDeps(&now))

	code, _, stderr := runAuthCapture("complete", "--config", configPath, "--server", "https://memory.example.test", "--login-id", "nope", "--code", "c", "--state", "s")
	if code != 1 {
		t.Fatalf("complete code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "unknown or already-used login") {
		t.Errorf("stderr = %q, want an unknown-login message", stderr)
	}
}

func TestAuthCancel(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	serverURL := "https://memory.example.test"
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	withFakePKCEManager(t, pkceTestDeps(&now))

	_, startOut, _ := runAuthCapture("start", "--config", configPath, "--server", serverURL, "--redirect-uri", "myapp://callback", "--json")
	var startDoc struct {
		LoginID string `json:"login_id"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal([]byte(startOut), &startDoc); err != nil {
		t.Fatalf("unmarshal start doc: %v", err)
	}

	if code, stdout, stderr := runAuthCapture("cancel", "--config", configPath, "--login-id", startDoc.LoginID); code != 0 {
		t.Fatalf("cancel code = %d, want 0 (stderr: %s)", code, stderr)
	} else if !strings.Contains(stdout, startDoc.LoginID) {
		t.Errorf("cancel stdout = %q, want the login id", stdout)
	}

	if code, _, _ := runAuthCapture("complete", "--config", configPath, "--server", serverURL, "--login-id", startDoc.LoginID, "--code", "c", "--state", startDoc.State); code != 1 {
		t.Errorf("complete after cancel code = %d, want 1", code)
	}
}

func TestAuthPKCEUsageErrors(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	cases := [][]string{
		{"start", "--config", configPath, "--server", "https://memory.example.test"}, // missing redirect-uri
		{"start", "--config", configPath, "--redirect-uri", "myapp://cb"},            // no server
		{"start", "--bogus"}, // bad flag
		{"complete", "--config", configPath, "--login-id", "x"},             // missing code/state
		{"complete", "--config", configPath, "--code", "c", "--state", "s"}, // missing login-id
		{"cancel", "--config", configPath},                                  // missing login-id
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if code, _, _ := runAuthCapture(args...); code != 2 {
				t.Errorf("runAuth(%v) code = %d, want 2", args, code)
			}
		})
	}
}

func TestAuthStartNativeClientIDWithoutServer(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	discoverCalls := 0
	deps := pkceTestDeps(&now)
	inner := deps.DiscoverIssuer
	deps.DiscoverIssuer = func(ctx context.Context, serverURL string) (string, error) {
		discoverCalls++
		return inner(ctx, serverURL)
	}
	withFakePKCEManager(t, deps)

	const nativeClientID = "390138006318678019"
	const redirect = "com.emergent.memory.connector://callback"

	// --issuer lets the caller omit --server entirely.
	code, stdout, stderr := runAuthCapture("start", "--config", configPath,
		"--issuer", "https://issuer.native.test",
		"--redirect-uri", redirect,
		"--client-id", nativeClientID,
		"--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if discoverCalls != 0 {
		t.Errorf("DiscoverIssuer called %d times, want 0 when --issuer is set", discoverCalls)
	}
	var doc struct {
		LoginID      string `json:"login_id"`
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal start JSON: %v\n%s", err, stdout)
	}
	u, err := url.Parse(doc.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	if got := u.Query().Get("client_id"); got != nativeClientID {
		t.Errorf("client_id = %q, want %q", got, nativeClientID)
	}
	if got := u.Query().Get("redirect_uri"); got != redirect {
		t.Errorf("redirect_uri = %q, want %q", got, redirect)
	}
	if doc.LoginID == "" {
		t.Error("login_id must not be empty")
	}
}
