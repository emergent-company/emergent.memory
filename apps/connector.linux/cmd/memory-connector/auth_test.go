package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
)

func writeAuthConfig(t *testing.T, dir, serverURL string) string {
	t.Helper()
	path := filepath.Join(dir, "memory-connector.yml")
	body := "server_url: " + serverURL + "\ntoken: emt_test\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func runAuthCapture(args ...string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = runAuth(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func withFakeAuthLogin(t *testing.T, fn func(context.Context, *account.Manager, string, account.LoginOptions, io.Writer) (*account.Session, error)) {
	t.Helper()
	old := authLogin
	authLogin = fn
	t.Cleanup(func() { authLogin = old })
}

func fakeAuthLogin(t *testing.T) {
	t.Helper()
	withFakeAuthLogin(t, func(_ context.Context, m *account.Manager, serverURL string, _ account.LoginOptions, out io.Writer) (*account.Session, error) {
		sess := &account.Session{
			ServerURL:    serverURL,
			IssuerURL:    "https://issuer.test",
			AccessToken:  "access-1",
			RefreshToken: "refresh-1",
			ExpiresAt:    time.Now().Add(time.Hour).UTC(),
			UserEmail:    "user@example.com",
		}
		if err := m.Save(serverURL, sess); err != nil {
			return nil, err
		}
		if err := m.SetActive(serverURL); err != nil {
			return nil, err
		}
		_, _ = io.WriteString(out, "fake device flow\n")
		return sess, nil
	})
}

func TestAuthStatusSignedOutText(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	code, stdout, stderr := runAuthCapture("status", "--config", configPath)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "memory-connector auth status") || !strings.Contains(stdout, "signed in: no") {
		t.Errorf("stdout = %q, want signed-out status", stdout)
	}
}

func TestAuthStatusSignedOutJSON(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	code, stdout, stderr := runAuthCapture("status", "--config", configPath, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		SchemaVersion int  `json:"schema_version"`
		SignedIn      bool `json:"signed_in"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal JSON: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", doc.SchemaVersion)
	}
	if doc.SignedIn {
		t.Errorf("signed_in = true, want false")
	}
}

func TestAuthLoginPersistsThenStatusAndLogout(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)
	fakeAuthLogin(t)

	code, stdout, stderr := runAuthCapture("login", "--config", configPath)
	if code != 0 {
		t.Fatalf("login code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "signed in as user@example.com") {
		t.Errorf("login stdout = %q, want signed-in identity", stdout)
	}

	// Persistence: one 0600 account file, active pointer set.
	m := account.NewManager(account.BaseDirForConfig(configPath))
	sess, err := m.SessionFor(serverURL)
	if err != nil || sess == nil {
		t.Fatalf("SessionFor after login = (%v, %v)", sess, err)
	}
	if sess.RefreshToken != "refresh-1" {
		t.Errorf("persisted refresh token = %q, want refresh-1", sess.RefreshToken)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "memory-connector", "accounts", "*.json"))
	if len(files) != 1 {
		t.Fatalf("account files = %v, want 1", files)
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("stat account file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("account file mode = %o, want 600", perm)
	}
	if active, ok := m.Active(); !ok || active != serverURL {
		t.Errorf("active = (%q, %v), want (%q, true)", active, ok, serverURL)
	}

	// Status via the CLI reports the signed-in account.
	code, stdout, stderr = runAuthCapture("status", "--config", configPath, "--json")
	if code != 0 {
		t.Fatalf("status code = %d (stderr: %s)", code, stderr)
	}
	var doc struct {
		SignedIn bool   `json:"signed_in"`
		Email    string `json:"email"`
		Issuer   string `json:"issuer"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal status JSON: %v\n%s", err, stdout)
	}
	if !doc.SignedIn || doc.Email != "user@example.com" || doc.Issuer != "https://issuer.test" {
		t.Errorf("status doc = %+v, want signed in as user@example.com", doc)
	}

	// Logout clears it, and is idempotent (exit 0 the second time).
	code, stdout, stderr = runAuthCapture("logout", "--config", configPath)
	if code != 0 {
		t.Fatalf("logout code = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "signed out") {
		t.Errorf("logout stdout = %q", stdout)
	}
	if sess, err := m.SessionFor(serverURL); err != nil || sess != nil {
		t.Errorf("SessionFor after logout = (%v, %v), want (nil, nil)", sess, err)
	}
	if code, _, _ := runAuthCapture("logout", "--config", configPath); code != 0 {
		t.Errorf("second logout code = %d, want 0", code)
	}
}

func TestAuthUsageErrors(t *testing.T) {
	cases := [][]string{
		{},                   // no subcommand
		{"bogus"},            // unknown subcommand
		{"login", "--bogus"}, // bad flag
		{"status", "extra"},  // positional arg
		{"logout", "extra"},  // positional arg
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if code, _, _ := runAuthCapture(args...); code != 2 {
				t.Errorf("runAuth(%v) code = %d, want 2", args, code)
			}
		})
	}
}

func TestAuthLoginPassesClientIDAndIssuer(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)

	var got account.LoginOptions
	withFakeAuthLogin(t, func(_ context.Context, _ *account.Manager, s string, opts account.LoginOptions, _ io.Writer) (*account.Session, error) {
		got = opts
		return &account.Session{
			ServerURL:   s,
			AccessToken: "access-1",
			ExpiresAt:   time.Now().Add(time.Hour).UTC(),
			UserEmail:   "user@example.com",
		}, nil
	})

	code, _, stderr := runAuthCapture("login", "--config", configPath,
		"--client-id", "390138928478289930", "--issuer", "https://issuer.native.test")
	if code != 0 {
		t.Fatalf("login code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got.ClientID != "390138928478289930" {
		t.Errorf("login client id = %q, want the --client-id value", got.ClientID)
	}
	if got.Issuer != "https://issuer.native.test" {
		t.Errorf("login issuer = %q, want the --issuer value", got.Issuer)
	}
}

func TestAuthLoginDefaultClientIDFlag(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	configPath := writeAuthConfig(t, dir, serverURL)

	var got account.LoginOptions
	withFakeAuthLogin(t, func(_ context.Context, _ *account.Manager, s string, opts account.LoginOptions, _ io.Writer) (*account.Session, error) {
		got = opts
		return &account.Session{ServerURL: s, AccessToken: "access-1", ExpiresAt: time.Now().Add(time.Hour).UTC()}, nil
	})

	code, _, stderr := runAuthCapture("login", "--config", configPath)
	if code != 0 {
		t.Fatalf("login code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got.ClientID != account.ClientID {
		t.Errorf("login client id = %q, want default %q", got.ClientID, account.ClientID)
	}
	if got.Issuer != "" {
		t.Errorf("login issuer = %q, want empty when --issuer is omitted", got.Issuer)
	}
}

func TestAuthLoginWithoutServerIsUsageError(t *testing.T) {
	// Missing config and no --server: nothing to log in to.
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	if code, _, stderr := runAuthCapture("login", "--config", configPath); code != 2 {
		t.Errorf("login code = %d, want 2 (stderr: %s)", code, stderr)
	}
}
