package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/memory.web-ui/connector/internal/account"
)

func TestAuthListEmptyIsNotAnError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")

	code, stdout, stderr := runAuthCapture("list", "--config", configPath)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "memory-connector auth list") || !strings.Contains(stdout, "no accounts") {
		t.Errorf("stdout = %q, want an empty list", stdout)
	}

	code, stdout, stderr = runAuthCapture("list", "--config", configPath, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, `"accounts": []`) {
		t.Errorf("stdout = %q, want an empty accounts array", stdout)
	}
	var doc struct {
		SchemaVersion int               `json:"schema_version"`
		Accounts      []authListAccount `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 || len(doc.Accounts) != 0 {
		t.Errorf("doc = %+v, want schema_version 1 with no accounts", doc)
	}
}

func TestAuthListMultipleMarksActive(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "memory-connector.yml")
	expiry := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

	m := account.NewManager(account.BaseDirForConfig(configPath))
	for _, acct := range []struct{ server, email string }{
		{"https://b.test", "b@example.com"},
		{"https://a.test", "a@example.com"},
	} {
		if err := m.Save(acct.server, &account.Session{
			AccessToken: "at-" + acct.email,
			UserEmail:   acct.email,
			IssuerURL:   "https://issuer.test",
			ExpiresAt:   expiry,
		}); err != nil {
			t.Fatalf("Save %s: %v", acct.server, err)
		}
	}
	if err := m.SetActive("https://a.test"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	// A corrupt file must not break the listing.
	if err := os.WriteFile(filepath.Join(account.BaseDirForConfig(configPath), "accounts", "corrupt.json"),
		[]byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	code, stdout, stderr := runAuthCapture("list", "--config", configPath, "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var doc struct {
		SchemaVersion int               `json:"schema_version"`
		Accounts      []authListAccount `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if doc.SchemaVersion != 1 || len(doc.Accounts) != 2 {
		t.Fatalf("doc = %+v, want 2 accounts", doc)
	}
	if doc.Accounts[0].ServerURL != "https://a.test" || !doc.Accounts[0].Active || !doc.Accounts[0].SignedIn {
		t.Errorf("first account = %+v, want active a.test", doc.Accounts[0])
	}
	if doc.Accounts[0].Email != "a@example.com" || doc.Accounts[0].ExpiresAt != "2030-01-02T03:04:05Z" {
		t.Errorf("first account = %+v", doc.Accounts[0])
	}
	if doc.Accounts[1].ServerURL != "https://b.test" || doc.Accounts[1].Active {
		t.Errorf("second account = %+v, want inactive b.test", doc.Accounts[1])
	}

	code, stdout, stderr = runAuthCapture("list", "--config", configPath)
	if code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"https://a.test  a@example.com  active  2030-01-02T03:04:05Z",
		"https://b.test  b@example.com  -  2030-01-02T03:04:05Z",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestAuthListUsageErrors(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memory-connector.yml")
	cases := [][]string{
		{"list", "--bogus"},
		{"list", "extra", "--config", configPath},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if code, _, _ := runAuthCapture(args...); code != 2 {
				t.Errorf("runAuth(%v) code = %d, want 2", args, code)
			}
		})
	}
}
