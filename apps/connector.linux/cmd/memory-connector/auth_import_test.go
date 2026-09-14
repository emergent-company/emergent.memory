package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/memory.web-ui/connector/internal/account"
)

// withAuthStdin feeds `auth import` a fixed stdin payload for the duration of
// the test.
func withAuthStdin(t *testing.T, contents string) {
	t.Helper()
	old := authStdin
	authStdin = strings.NewReader(contents)
	t.Cleanup(func() { authStdin = old })
}

func TestAuthImportValidText(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	serverURL := "https://memory.example.test"
	withAuthStdin(t, `{"access_token":"at-secret","refresh_token":"rt-secret",`+
		`"expires_at":"2030-01-02T03:04:05Z","issuer":"https://issuer.test",`+
		`"client_id":"cid","email":"user@example.com"}`)

	code, stdout, stderr := runAuthCapture("import", "--config", configPath, "--server", serverURL)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"memory-connector auth status", "signed in: yes", "email: user@example.com", "issuer: https://issuer.test"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "at-secret") || strings.Contains(stdout, "rt-secret") {
		t.Errorf("stdout must never echo tokens:\n%s", stdout)
	}

	m := account.NewManager(account.BaseDirForConfig(configPath))
	sess, err := m.SessionFor(serverURL)
	if err != nil || sess == nil {
		t.Fatalf("SessionFor = (%v, %v), want a persisted session", sess, err)
	}
	if sess.AccessToken != "at-secret" || sess.RefreshToken != "rt-secret" {
		t.Errorf("tokens = %q/%q", sess.AccessToken, sess.RefreshToken)
	}
	if sess.IssuerURL != "https://issuer.test" || sess.UserEmail != "user@example.com" {
		t.Errorf("session = %+v", sess)
	}
	wantExpiry := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	if !sess.ExpiresAt.UTC().Equal(wantExpiry) {
		t.Errorf("ExpiresAt = %v, want %v", sess.ExpiresAt, wantExpiry)
	}
	if active, ok := m.Active(); !ok || active != serverURL {
		t.Errorf("active = (%q, %v), want (%q, true)", active, ok, serverURL)
	}
}

func TestAuthImportJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	serverURL := "https://memory.example.test"
	withAuthStdin(t, `{"access_token":"at-secret","refresh_token":"rt-secret",`+
		`"expires_at":"2030-01-02T03:04:05Z","issuer":"https://issuer.test","email":"user@example.com"}`)

	code, stdout, stderr := runAuthCapture("import", "--config", configPath, "--server", serverURL, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		SchemaVersion int    `json:"schema_version"`
		Server        string `json:"server"`
		SignedIn      bool   `json:"signed_in"`
		Email         string `json:"email"`
		Issuer        string `json:"issuer"`
		ExpiresAt     string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 || doc.Server != serverURL || !doc.SignedIn {
		t.Errorf("import doc = %+v", doc)
	}
	if doc.Email != "user@example.com" || doc.Issuer != "https://issuer.test" {
		t.Errorf("import doc = %+v", doc)
	}
	if doc.ExpiresAt != "2030-01-02T03:04:05Z" {
		t.Errorf("expires_at = %q", doc.ExpiresAt)
	}
	if strings.Contains(stdout, "at-secret") || strings.Contains(stdout, "rt-secret") {
		t.Errorf("JSON must never echo tokens:\n%s", stdout)
	}
}

func TestAuthImportMissingAccessTokenIsUsageError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	withAuthStdin(t, `{"refresh_token":"rt-secret","issuer":"https://issuer.test"}`)

	code, stdout, stderr := runAuthCapture("import", "--config", configPath, "--server", "https://memory.example.test")
	if code != 2 {
		t.Fatalf("code = %d, want 2 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "access_token") {
		t.Errorf("stderr = %q, want an access_token message", stderr)
	}
}

func TestAuthImportMalformedJSONIsUsageError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	withAuthStdin(t, "this is not JSON")

	code, _, stderr := runAuthCapture("import", "--config", configPath, "--server", "https://memory.example.test")
	if code != 2 {
		t.Fatalf("code = %d, want 2 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "invalid JSON") {
		t.Errorf("stderr = %q, want an invalid-JSON message", stderr)
	}
}

func TestAuthImportMissingServerIsUsageError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	withAuthStdin(t, `{"access_token":"at-1"}`)

	code, _, stderr := runAuthCapture("import", "--config", configPath)
	if code != 2 {
		t.Fatalf("code = %d, want 2 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "--server") {
		t.Errorf("stderr = %q, want a --server message", stderr)
	}
}

func TestAuthImportOptionalExpiryAndCamelCaseAliases(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	serverURL := "https://memory.example.test"
	withAuthStdin(t, `{"accessToken":"at-1","refreshToken":"rt-1","issuer":"https://issuer.test"}`)

	code, stdout, stderr := runAuthCapture("import", "--config", configPath, "--server", serverURL)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.Contains(stdout, "expires at:") {
		t.Errorf("stdout should omit expiry when absent:\n%s", stdout)
	}
	sess, err := account.NewManager(account.BaseDirForConfig(configPath)).SessionFor(serverURL)
	if err != nil || sess == nil {
		t.Fatalf("SessionFor = (%v, %v)", sess, err)
	}
	if sess.AccessToken != "at-1" || sess.RefreshToken != "rt-1" {
		t.Errorf("session tokens = %q/%q", sess.AccessToken, sess.RefreshToken)
	}
	if !sess.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want zero when omitted", sess.ExpiresAt)
	}
}

func TestAuthImportInvalidExpiryIsUsageError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	withAuthStdin(t, `{"access_token":"at-1","expires_at":"not-a-time"}`)

	code, _, stderr := runAuthCapture("import", "--config", configPath, "--server", "https://memory.example.test")
	if code != 2 {
		t.Fatalf("code = %d, want 2 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "expires_at") {
		t.Errorf("stderr = %q, want an expires_at message", stderr)
	}
}

func TestAuthImportUnexpectedArgumentIsUsageError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	withAuthStdin(t, `{"access_token":"at-1"}`)
	if code, _, _ := runAuthCapture("import", "extra", "--config", configPath, "--server", "https://memory.example.test"); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}
