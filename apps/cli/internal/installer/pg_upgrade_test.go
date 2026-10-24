package installer

import (
	"os"
	"path/filepath"
	"testing"
)

// The installer's own writer quotes a password that contains a "#" (or starts
// with a quote), and the previous prefix scan handed those bytes — quotes and
// inline comment included — to pg_upgrade, which then failed to authenticate
// against its own database. The reader goes through the shared env-file scanner
// now, so it sees the value the service sees (#2170).
func TestReadPgCredsFromEnvUnquotesAQuotedPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	content := "POSTGRES_USER=emergent\nPOSTGRES_PASSWORD='p@ss #1'\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	user, password := readPgCredsFromEnv(path)
	if user != "emergent" {
		t.Errorf("user = %q, want emergent", user)
	}
	if password != "p@ss #1" {
		t.Fatalf("password = %q, want the unquoted value the service reads", password)
	}
}

// The old scan also kept a trailing inline comment and missed an "export "
// prefix, so both are pinned here.
func TestReadPgCredsFromEnvReadsTheStatementNotTheLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	content := "export POSTGRES_USER=emergent # the app role\nPOSTGRES_PASSWORD=plain\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	user, password := readPgCredsFromEnv(path)
	if user != "emergent" {
		t.Errorf("user = %q, want emergent without the comment", user)
	}
	if password != "plain" {
		t.Errorf("password = %q, want plain", password)
	}
}

// The pgautoupgrade image's default is ("postgres", ""), which is what both a
// missing file and an empty assignment must fall back to.
func TestReadPgCredsFromEnvDefaults(t *testing.T) {
	user, password := readPgCredsFromEnv(filepath.Join(t.TempDir(), "missing.env"))
	if user != "postgres" || password != "" {
		t.Errorf("missing file = (%q, %q), want (postgres, \"\")", user, password)
	}

	path := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(path, []byte("POSTGRES_USER=\nPOSTGRES_PASSWORD=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	user, password = readPgCredsFromEnv(path)
	if user != "postgres" {
		t.Errorf("empty POSTGRES_USER = %q, want the default postgres", user)
	}
	if password != "" {
		t.Errorf("empty POSTGRES_PASSWORD = %q, want empty", password)
	}
}
