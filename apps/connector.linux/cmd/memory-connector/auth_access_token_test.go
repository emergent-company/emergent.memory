package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkauth "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"

	"github.com/emergent-company/memory.web-ui/connector/internal/account"
)

// seedAccessSession persists a session for serverURL in the account store that
// lines up with configPath.
func seedAccessSession(t *testing.T, configPath, serverURL string, sess *account.Session) {
	t.Helper()
	m := account.NewManager(account.BaseDirForConfig(configPath))
	if err := m.Save(serverURL, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := m.SetActive(serverURL); err != nil {
		t.Fatalf("seed active: %v", err)
	}
}

// withAccessTokenManager injects offline deps into the manager used by the auth
// subcommands.
func withAccessTokenManager(t *testing.T, deps account.Deps) {
	t.Helper()
	old := newAuthManager
	newAuthManager = func(baseDir string) *account.Manager {
		return account.NewManagerWithDeps(baseDir, deps)
	}
	t.Cleanup(func() { newAuthManager = old })
}

func TestAuthAccessTokenText(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	seedAccessSession(t, configPath, serverURL, &account.Session{
		ServerURL:    serverURL,
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-valid",
		RefreshToken: "refresh-secret",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	code, stdout, stderr := runAuthCapture("access-token", "--config", configPath)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "access-valid\n" {
		t.Errorf("stdout = %q, want the raw access token only", stdout)
	}
	if strings.Contains(stdout, "refresh-secret") {
		t.Errorf("stdout must never expose the refresh token: %q", stdout)
	}
}

func TestAuthAccessTokenJSON(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	seedAccessSession(t, configPath, serverURL, &account.Session{
		ServerURL:    serverURL,
		AccessToken:  "access-valid",
		RefreshToken: "refresh-secret",
		ExpiresAt:    expires,
	})

	code, stdout, stderr := runAuthCapture("access-token", "--config", configPath, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		SchemaVersion int    `json:"schema_version"`
		ServerURL     string `json:"server_url"`
		AccessToken   string `json:"access_token"`
		ExpiresAt     string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", doc.SchemaVersion)
	}
	if doc.ServerURL != serverURL {
		t.Errorf("server_url = %q, want %q", doc.ServerURL, serverURL)
	}
	if doc.AccessToken != "access-valid" {
		t.Errorf("access_token = %q, want access-valid", doc.AccessToken)
	}
	if doc.ExpiresAt != expires.UTC().Format(time.RFC3339) {
		t.Errorf("expires_at = %q, want %q", doc.ExpiresAt, expires.UTC().Format(time.RFC3339))
	}
	if strings.Contains(stdout, "refresh") {
		t.Errorf("JSON must never expose the refresh token: %s", stdout)
	}
}

func TestAuthAccessTokenNotSignedIn(t *testing.T) {
	dir := t.TempDir()
	configPath := writeAuthConfig(t, dir, "https://memory.example.test")

	code, stdout, stderr := runAuthCapture("access-token", "--config", configPath)
	if code != 1 {
		t.Fatalf("code = %d, want 1 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "not signed in") || !strings.Contains(stderr, "auth login") {
		t.Errorf("stderr = %q, want a sign-in hint", stderr)
	}
}

func TestAuthAccessTokenRefreshesExpired(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	seedAccessSession(t, configPath, serverURL, &account.Session{
		ServerURL:    serverURL,
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})

	refreshCalls := 0
	now := time.Now()
	withAccessTokenManager(t, account.Deps{
		DiscoverOIDC: func(issuer string) (*sdkauth.OIDCConfig, error) {
			return &sdkauth.OIDCConfig{Issuer: issuer, TokenEndpoint: "https://issuer.test/token"}, nil
		},
		RefreshToken: func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
			refreshCalls++
			return &sdkauth.Credentials{AccessToken: "access-refreshed", RefreshToken: "refresh-2", ExpiresAt: now.Add(time.Hour)}, nil
		},
	})

	code, stdout, stderr := runAuthCapture("access-token", "--config", configPath)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if refreshCalls != 1 {
		t.Errorf("refresh calls = %d, want 1", refreshCalls)
	}
	if stdout != "access-refreshed\n" {
		t.Errorf("stdout = %q, want the refreshed token", stdout)
	}
	// The refreshed session is persisted (re-read from disk).
	sess, err := account.NewManager(account.BaseDirForConfig(configPath)).SessionFor(serverURL)
	if err != nil || sess == nil {
		t.Fatalf("re-read session = (%v, %v)", sess, err)
	}
	if sess.AccessToken != "access-refreshed" {
		t.Errorf("persisted access token = %q, want access-refreshed", sess.AccessToken)
	}
}

func TestAuthAccessTokenRefreshesNearExpiry(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	seedAccessSession(t, configPath, serverURL, &account.Session{
		ServerURL:    serverURL,
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-about-to-expire",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(30 * time.Second),
	})

	refreshCalls := 0
	now := time.Now()
	withAccessTokenManager(t, account.Deps{
		DiscoverOIDC: func(issuer string) (*sdkauth.OIDCConfig, error) {
			return &sdkauth.OIDCConfig{Issuer: issuer, TokenEndpoint: "https://issuer.test/token"}, nil
		},
		RefreshToken: func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
			refreshCalls++
			return &sdkauth.Credentials{AccessToken: "access-refreshed", RefreshToken: "refresh-2", ExpiresAt: now.Add(time.Hour)}, nil
		},
	})

	code, stdout, stderr := runAuthCapture("access-token", "--config", configPath)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if refreshCalls != 1 {
		t.Errorf("refresh calls = %d, want 1 within the refresh window", refreshCalls)
	}
	if stdout != "access-refreshed\n" {
		t.Errorf("stdout = %q, want the refreshed token", stdout)
	}
}

func TestAuthAccessTokenRefreshFailure(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	seedAccessSession(t, configPath, serverURL, &account.Session{
		ServerURL:    serverURL,
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})

	withAccessTokenManager(t, account.Deps{
		DiscoverOIDC: func(issuer string) (*sdkauth.OIDCConfig, error) {
			return &sdkauth.OIDCConfig{Issuer: issuer, TokenEndpoint: "https://issuer.test/token"}, nil
		},
		RefreshToken: func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
			return nil, errors.New("refresh rejected")
		},
	})

	code, stdout, stderr := runAuthCapture("access-token", "--config", configPath)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on refresh failure", stdout)
	}
	if !strings.Contains(stderr, "refresh") {
		t.Errorf("stderr = %q, want the refresh error", stderr)
	}
}

func TestAuthAccessTokenMissingServer(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	code, stdout, stderr := runAuthCapture("access-token", "--config", configPath)
	if code != 2 {
		t.Fatalf("code = %d, want 2 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "no server URL") {
		t.Errorf("stderr = %q, want a server URL hint", stderr)
	}
}

func TestAuthAccessTokenUsageErrors(t *testing.T) {
	cases := [][]string{
		{"access-token", "--bogus"},
		{"access-token", "extra", "--server", "https://memory.example.test"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if code, _, _ := runAuthCapture(args...); code != 2 {
				t.Errorf("runAuth(%v) code = %d, want 2", args, code)
			}
		})
	}
}
