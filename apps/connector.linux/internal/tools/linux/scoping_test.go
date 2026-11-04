package linux

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestResolveTargetInsideRoot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	writeFile(t, file, "x")
	roots := []Root{{Path: root, Read: true}}

	gotRoot, resolved, err := resolveTarget(roots, file)
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if gotRoot.Path != root {
		t.Errorf("matched root = %q, want %q", gotRoot.Path, root)
	}
	if !filepath.IsAbs(resolved) || filepath.Base(resolved) != "a.txt" {
		t.Errorf("resolved target = %q, want absolute .../a.txt", resolved)
	}
}

func TestResolveTargetOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	roots := []Root{{Path: root, Read: true}}

	if _, _, err := resolveTarget(roots, filepath.Join(outside, "f")); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("resolveTarget outside = %v, want ErrOutsideRoots", err)
	}
}

func TestResolveTargetTraversalRejected(t *testing.T) {
	root := t.TempDir()
	roots := []Root{{Path: root, Read: true}}

	if _, _, err := resolveTarget(roots, filepath.Join(root, "..", "escape")); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("resolveTarget traversal = %v, want ErrOutsideRoots", err)
	}
}

func TestResolveTargetSymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	writeFile(t, filepath.Join(outsideDir, "secret.txt"), "secret")
	if err := os.Symlink(outsideDir, filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	roots := []Root{{Path: root, Read: true}}

	if _, _, err := resolveTarget(roots, filepath.Join(root, "link", "secret.txt")); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("resolveTarget via symlink = %v, want ErrOutsideRoots", err)
	}
}

func TestResolveTargetPrefixConfusionRejected(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "data")
	outside := filepath.Join(parent, "database")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	roots := []Root{{Path: root, Read: true}}

	if _, _, err := resolveTarget(roots, filepath.Join(outside, "x")); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("resolveTarget /data vs /database = %v, want ErrOutsideRoots", err)
	}
}

func TestResolveTargetNonExistentUnderRootAllowed(t *testing.T) {
	root := t.TempDir()
	roots := []Root{{Path: root, Read: true}}
	target := filepath.Join(root, "new", "deep", "file.txt")

	_, resolved, err := resolveTarget(roots, target)
	if err != nil {
		t.Fatalf("resolveTarget non-existent under root: %v", err)
	}
	if filepath.Base(resolved) != "file.txt" {
		t.Errorf("resolved = %q, want a path ending in file.txt", resolved)
	}
}

func TestResolveTargetNonExistentOutsideRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	roots := []Root{{Path: root, Read: true}}

	if _, _, err := resolveTarget(roots, filepath.Join(outside, "new", "file.txt")); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("resolveTarget non-existent outside = %v, want ErrOutsideRoots", err)
	}
}

func TestResolveTargetFirstContainingRootWins(t *testing.T) {
	parent := t.TempDir()
	inner := filepath.Join(parent, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	roots := []Root{
		{Path: parent, Read: false},
		{Path: inner, Read: true},
	}

	got, _, err := resolveTarget(roots, filepath.Join(inner, "f"))
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if got.Read {
		t.Errorf("first containing root should win (outer, Read=false), got %+v", got)
	}
}

func TestResolveTargetRootItselfAllowed(t *testing.T) {
	root := t.TempDir()
	roots := []Root{{Path: root, Read: true}}
	if _, _, err := resolveTarget(roots, root); err != nil {
		t.Fatalf("resolveTarget(root) = %v, want nil (root contains itself)", err)
	}
}

func TestContainsIsNotPrefixBased(t *testing.T) {
	if contains("/data", "/database") {
		t.Error("contains must not treat /database as inside /data")
	}
	if !contains("/data", "/data/sub") {
		t.Error("contains should accept a real child")
	}
	if !contains("/data", "/data") {
		t.Error("contains should accept the root itself")
	}
	if contains("/data", "/data/../etc") {
		t.Error("contains must not accept a traversal above the root")
	}
}
