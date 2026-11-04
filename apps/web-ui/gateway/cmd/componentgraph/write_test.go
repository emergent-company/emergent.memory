package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWriteIfChanged pins the idempotence that keeps `task dev` (air) from
// rebuilding forever: an unchanged payload must not touch the file, because the
// generated graph is a watched source file and a bumped mtime restarts the build.
func TestWriteIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gen.go")
	past := time.Unix(1600000000, 0)

	// First write creates the file with the payload.
	if err := writeIfChanged(path, []byte("package main\n")); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after create: %v", err)
	}
	if string(got) != "package main\n" {
		t.Fatalf("content after create = %q", got)
	}

	// Stamp an old mtime so a rewrite would be visible without sleeping.
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Identical content: the file must not be touched.
	if err := writeIfChanged(path, []byte("package main\n")); err != nil {
		t.Fatalf("rewrite identical: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.ModTime().Equal(past) {
		t.Fatalf("identical payload touched the file: mtime = %v, want %v", info.ModTime(), past)
	}

	// Changed content: the file is written.
	if err := writeIfChanged(path, []byte("package main\n\nvar x = 1\n")); err != nil {
		t.Fatalf("rewrite changed: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after change: %v", err)
	}
	if string(got) != "package main\n\nvar x = 1\n" {
		t.Fatalf("content after change = %q", got)
	}
}
