package secretstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadRoundTripAndModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "secrets")
	s := New(dir)

	if err := s.Save("session.json", []byte("hello")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load("session.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("Load = %q, want %q", got, "hello")
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}
	fileInfo, err := os.Stat(s.Path("session.json"))
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
}

func TestSaveOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Save("token", []byte("old")); err != nil {
		t.Fatalf("Save old: %v", err)
	}
	if err := s.Save("token", []byte("new")); err != nil {
		t.Fatalf("Save new: %v", err)
	}
	got, err := s.Load("token")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("Load = %q, want %q", got, "new")
	}
}

func TestLoadMissingIsNilNotError(t *testing.T) {
	s := New(t.TempDir())
	got, err := s.Load("absent")
	if err != nil {
		t.Fatalf("Load missing = error %v, want nil", err)
	}
	if got != nil {
		t.Errorf("Load missing = %v, want nil", got)
	}
}

func TestDeleteIdempotent(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Save("token", []byte("x")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Delete("token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete("token"); err != nil {
		t.Fatalf("second Delete = %v, want nil (idempotent)", err)
	}
	got, err := s.Load("token")
	if err != nil || got != nil {
		t.Errorf("Load after Delete = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestNameValidation(t *testing.T) {
	s := New(t.TempDir())
	bad := []string{"", ".", "..", "../escape", "sub/name", `sub\name`, "a..b"}
	for _, name := range bad {
		t.Run(name, func(t *testing.T) {
			if err := s.Save(name, []byte("x")); err == nil {
				t.Errorf("Save(%q) = nil, want name validation error", name)
			}
			if _, err := s.Load(name); err == nil {
				t.Errorf("Load(%q) = nil error, want name validation error", name)
			}
			if err := s.Delete(name); err == nil {
				t.Errorf("Delete(%q) = nil, want name validation error", name)
			}
		})
	}
}

func TestSaveFailureLeavesExistingTargetUntouched(t *testing.T) {
	// A directory at the target path makes the final rename fail after the
	// temp file has been written. The existing "target" must be left intact
	// and no temp file may be left behind.
	dir := t.TempDir()
	target := filepath.Join(dir, "secret")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir target: %v", err)
	}
	marker := filepath.Join(target, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	s := New(dir)
	if err := s.Save("secret", []byte("new")); err == nil {
		t.Fatal("Save over a directory: expected error, got nil")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("existing target was disturbed by a failed Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".secret.tmp-") {
			t.Errorf("temporary file %q left behind after failed Save", e.Name())
		}
	}
}
