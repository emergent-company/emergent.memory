package linux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTrashPath(t *testing.T, fn func(string) error) {
	t.Helper()
	old := trashPath
	trashPath = fn
	t.Cleanup(func() { trashPath = old })
}

func TestFSDeleteTrashesTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "gone.txt")
	writeFile(t, target, "x")
	var calledWith string
	withTrashPath(t, func(p string) error {
		calledWith = p
		return nil
	})
	tool := newFSDeleteTool([]Root{{Path: root, Delete: true}}, false, discardAudit())

	res, err := tool.Handler(context.Background(), map[string]any{"path": target})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res["trashed"] != true || res["permanent"] != false {
		t.Errorf("result = %v, want trashed=true permanent=false", res)
	}
	if !strings.HasSuffix(calledWith, "gone.txt") {
		t.Errorf("trashPath called with %q, want the resolved target", calledWith)
	}
}

func TestFSDeleteOutsideDenied(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "x.txt")
	writeFile(t, target, "x")
	withTrashPath(t, func(string) error { return nil })
	tool := newFSDeleteTool([]Root{{Path: root, Delete: true}}, false, discardAudit())

	_, err := tool.Handler(context.Background(), map[string]any{"path": target})
	if err == nil || !strings.Contains(err.Error(), "outside the allowed roots") {
		t.Fatalf("error = %v, want ErrOutsideRoots", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Errorf("outside target must be intact: %v", statErr)
	}
}

func TestFSDeleteCapabilityDenied(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "x.txt")
	writeFile(t, target, "x")
	tool := newFSDeleteTool([]Root{{Path: root, Delete: false}}, false, discardAudit())

	_, err := tool.Handler(context.Background(), map[string]any{"path": target})
	if err == nil || !strings.Contains(err.Error(), "delete denied") {
		t.Fatalf("error = %v, want delete denied", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Errorf("target must be intact on capability denial: %v", statErr)
	}
}

func TestFSDeleteTrashUnavailableLeavesTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "x.txt")
	writeFile(t, target, "x")
	withTrashPath(t, func(string) error { return errors.New("trash unavailable") })
	tool := newFSDeleteTool([]Root{{Path: root, Delete: true}}, false, discardAudit())

	_, err := tool.Handler(context.Background(), map[string]any{"path": target})
	if err == nil || !strings.Contains(err.Error(), "trash unavailable") {
		t.Fatalf("error = %v, want trash failure", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Errorf("target must be left intact when trashing fails: %v", statErr)
	}
}

func TestFSDeletePermanentOnlyWhenAllowed(t *testing.T) {
	root := t.TempDir()
	withTrashPath(t, func(string) error { return errors.New("trash unavailable") })

	// Not allowed: error, target intact.
	keep := filepath.Join(root, "keep.txt")
	writeFile(t, keep, "x")
	tool := newFSDeleteTool([]Root{{Path: root, Delete: true}}, false, discardAudit())
	if _, err := tool.Handler(context.Background(), map[string]any{"path": keep}); err == nil {
		t.Fatal("expected error when trashing fails and permanent delete is disallowed")
	}
	if _, statErr := os.Stat(keep); statErr != nil {
		t.Errorf("target must be intact when permanent delete is disallowed: %v", statErr)
	}

	// Allowed: permanent removal.
	remove := filepath.Join(root, "remove.txt")
	writeFile(t, remove, "x")
	permTool := newFSDeleteTool([]Root{{Path: root, Delete: true}}, true, discardAudit())
	res, err := permTool.Handler(context.Background(), map[string]any{"path": remove})
	if err != nil {
		t.Fatalf("Handler (permanent): %v", err)
	}
	if res["permanent"] != true || res["trashed"] != false {
		t.Errorf("result = %v, want permanent=true trashed=false", res)
	}
	if _, statErr := os.Stat(remove); !os.IsNotExist(statErr) {
		t.Errorf("target should be permanently removed: %v", statErr)
	}
}

func TestFSDeleteMissingArgument(t *testing.T) {
	tool := newFSDeleteTool([]Root{{Path: t.TempDir(), Delete: true}}, false, discardAudit())
	if _, err := tool.Handler(context.Background(), map[string]any{}); err == nil {
		t.Fatal("missing path: expected error")
	}
}
