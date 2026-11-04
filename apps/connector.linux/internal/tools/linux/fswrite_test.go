package linux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFSWriteCreatesFileAtomically(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "out.txt")
	tool := newFSWriteTool([]Root{{Path: root, Write: true}}, 0, discardAudit())

	res, err := tool.Handler(context.Background(), map[string]any{"path": target, "content": "hello"})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want hello", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	if res["written"] != true {
		t.Errorf("written = %v, want true", res["written"])
	}
	if got, ok := res["bytes"].(int); !ok || got != 5 {
		t.Errorf("bytes = %v, want 5", res["bytes"])
	}
	// No temp files left behind.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("root entries = %v, want only the target", entries)
	}
}

func TestFSWriteOverwritesExisting(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "out.txt")
	writeFile(t, target, "old")
	tool := newFSWriteTool([]Root{{Path: root, Write: true}}, 0, discardAudit())

	if _, err := tool.Handler(context.Background(), map[string]any{"path": target, "content": "new"}); err != nil {
		t.Fatalf("Handler: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Errorf("content = %q, want new", got)
	}
}

func TestFSWriteEmptyContentAllowed(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "empty.txt")
	tool := newFSWriteTool([]Root{{Path: root, Write: true}}, 0, discardAudit())

	if _, err := tool.Handler(context.Background(), map[string]any{"path": target, "content": ""}); err != nil {
		t.Fatalf("Handler: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
}

func TestFSWriteOutsideDenied(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	tool := newFSWriteTool([]Root{{Path: root, Write: true}}, 0, discardAudit())

	_, err := tool.Handler(context.Background(), map[string]any{"path": filepath.Join(outside, "x.txt"), "content": "x"})
	if err == nil || !strings.Contains(err.Error(), "outside the allowed roots") {
		t.Fatalf("error = %v, want ErrOutsideRoots", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "x.txt")); !os.IsNotExist(statErr) {
		t.Errorf("outside target should not have been created")
	}
}

func TestFSWriteCapabilityDenied(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "x.txt")
	tool := newFSWriteTool([]Root{{Path: root, Write: false}}, 0, discardAudit())

	_, err := tool.Handler(context.Background(), map[string]any{"path": target, "content": "x"})
	if err == nil || !strings.Contains(err.Error(), "write denied") {
		t.Fatalf("error = %v, want write denied", err)
	}
}

func TestFSWriteOversizedLeavesTargetUnchanged(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "x.txt")
	writeFile(t, target, "original")
	tool := newFSWriteTool([]Root{{Path: root, Write: true}}, 4, discardAudit())

	_, err := tool.Handler(context.Background(), map[string]any{"path": target, "content": "too long"})
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("error = %v, want exceeds limit", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "original" {
		t.Errorf("target changed on oversized write: %q", got)
	}
}

func TestFSWriteFailureLeavesNoPartial(t *testing.T) {
	root := t.TempDir()
	// The target is a directory, so the final rename must fail after the temp
	// file has been written.
	target := filepath.Join(root, "target-dir")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tool := newFSWriteTool([]Root{{Path: root, Write: true}}, 0, discardAudit())

	if _, err := tool.Handler(context.Background(), map[string]any{"path": target, "content": "data"}); err == nil {
		t.Fatal("Handler over a directory: expected error, got nil")
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatalf("target disturbed by failed write: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".target-dir.tmp-") {
			t.Errorf("temp file %q left behind after failed write", e.Name())
		}
	}
}

func TestFSWriteMissingArguments(t *testing.T) {
	tool := newFSWriteTool([]Root{{Path: t.TempDir(), Write: true}}, 0, discardAudit())
	if _, err := tool.Handler(context.Background(), map[string]any{"content": "x"}); err == nil {
		t.Error("missing path: expected error")
	}
	if _, err := tool.Handler(context.Background(), map[string]any{"path": "/tmp/x"}); err == nil {
		t.Error("missing content: expected error")
	}
}
