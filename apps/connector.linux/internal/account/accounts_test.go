package account

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountsEmpty(t *testing.T) {
	m := NewManager(t.TempDir())
	infos, err := m.Accounts()
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}
	if infos == nil {
		t.Fatal("Accounts returned a nil slice, want non-nil empty")
	}
	if len(infos) != 0 {
		t.Errorf("Accounts = %+v, want empty", infos)
	}
}

func TestAccountsListsSessionsSortedAndMarksActive(t *testing.T) {
	m := NewManager(t.TempDir())
	expiry := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

	save := func(serverURL, email string) {
		t.Helper()
		sess := &Session{
			IssuerURL:   "https://issuer.test",
			AccessToken: "at-" + email,
			ExpiresAt:   expiry,
			UserEmail:   email,
		}
		if err := m.Save(serverURL, sess); err != nil {
			t.Fatalf("Save %s: %v", serverURL, err)
		}
	}
	save("https://b.test", "b@example.com")
	save("https://a.test", "a@example.com")
	if err := m.SetActive("https://b.test"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	infos, err := m.Accounts()
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("Accounts = %+v, want 2", infos)
	}
	if infos[0].ServerURL != "https://a.test" || infos[1].ServerURL != "https://b.test" {
		t.Errorf("Accounts not sorted by server URL: %+v", infos)
	}
	if infos[0].Active {
		t.Errorf("a.test must not be active: %+v", infos[0])
	}
	if !infos[1].Active {
		t.Errorf("b.test must be active: %+v", infos[1])
	}
	for _, info := range infos {
		if !info.SignedIn {
			t.Errorf("%s SignedIn = false, want true", info.ServerURL)
		}
		if info.Issuer != "https://issuer.test" {
			t.Errorf("%s Issuer = %q", info.ServerURL, info.Issuer)
		}
		if !info.ExpiresAt.Equal(expiry) {
			t.Errorf("%s ExpiresAt = %v, want %v", info.ServerURL, info.ExpiresAt, expiry)
		}
	}
	if infos[0].Email != "a@example.com" || infos[1].Email != "b@example.com" {
		t.Errorf("emails = %q/%q", infos[0].Email, infos[1].Email)
	}
}

func TestAccountsSkipsCorruptAndForeignFiles(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	if err := m.Save("https://a.test", &Session{AccessToken: "at-a"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	accountsDir := filepath.Join(dir, "accounts")
	corrupt := filepath.Join(accountsDir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	// A stray non-JSON file (e.g. a leftover temp file) must be ignored.
	if err := os.WriteFile(filepath.Join(accountsDir, ".tmp-123"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	infos, err := m.Accounts()
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}
	if len(infos) != 1 || infos[0].ServerURL != "https://a.test" {
		t.Errorf("Accounts = %+v, want only a.test", infos)
	}
}
