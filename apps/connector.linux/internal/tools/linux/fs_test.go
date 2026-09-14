package linux

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFSReadContent(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "note.txt")
	writeFile(t, file, "hello")
	tool := newFSReadTool([]Root{{Path: root, Read: true}}, 0)

	res, err := tool.Handler(context.Background(), map[string]any{"path": file})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res["content"] != "hello" {
		t.Errorf("content = %v, want hello", res["content"])
	}
	if got, ok := res["bytes"].(int); !ok || got != 5 {
		t.Errorf("bytes = %v (%T), want 5", res["bytes"], res["bytes"])
	}
	if res["truncated"] != false {
		t.Errorf("truncated = %v, want false", res["truncated"])
	}
}

func TestFSReadTruncatesAtLimit(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "big.txt")
	writeFile(t, file, "0123456789")
	tool := newFSReadTool([]Root{{Path: root, Read: true}}, 4)

	res, err := tool.Handler(context.Background(), map[string]any{"path": file})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res["content"] != "0123" {
		t.Errorf("content = %q, want %q", res["content"], "0123")
	}
	if got, ok := res["bytes"].(int); !ok || got != 4 {
		t.Errorf("bytes = %v, want 4", res["bytes"])
	}
	if res["truncated"] != true {
		t.Errorf("truncated = %v, want true", res["truncated"])
	}
}

func TestFSReadExactLimitNotTruncated(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "exact.txt")
	writeFile(t, file, "0123")
	tool := newFSReadTool([]Root{{Path: root, Read: true}}, 4)

	res, err := tool.Handler(context.Background(), map[string]any{"path": file})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res["truncated"] != false {
		t.Errorf("truncated = %v, want false for an exactly-limit file", res["truncated"])
	}
}

func TestFSReadOutsideDenied(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(outside, "secret.txt")
	writeFile(t, file, "secret")
	tool := newFSReadTool([]Root{{Path: root, Read: true}}, 0)

	_, err := tool.Handler(context.Background(), map[string]any{"path": file})
	if err == nil || !strings.Contains(err.Error(), "outside the allowed roots") {
		t.Fatalf("Handler outside root error = %v, want ErrOutsideRoots", err)
	}
}

func TestFSReadCapabilityDenied(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "note.txt")
	writeFile(t, file, "hello")
	tool := newFSReadTool([]Root{{Path: root, Read: false}}, 0)

	_, err := tool.Handler(context.Background(), map[string]any{"path": file})
	if err == nil || !strings.Contains(err.Error(), "read denied") {
		t.Fatalf("Handler without Read capability error = %v, want read denied", err)
	}
}

func TestFSReadMissingFileError(t *testing.T) {
	root := t.TempDir()
	tool := newFSReadTool([]Root{{Path: root, Read: true}}, 0)

	_, err := tool.Handler(context.Background(), map[string]any{"path": filepath.Join(root, "nope.txt")})
	if err == nil {
		t.Fatal("Handler missing file: expected error, got nil")
	}
}

func TestFSReadMissingArgument(t *testing.T) {
	tool := newFSReadTool([]Root{{Path: t.TempDir(), Read: true}}, 0)
	if _, err := tool.Handler(context.Background(), map[string]any{}); err == nil {
		t.Fatal("Handler missing path: expected error, got nil")
	}
	if _, err := tool.Handler(context.Background(), map[string]any{"path": "  "}); err == nil {
		t.Fatal("Handler blank path: expected error, got nil")
	}
}

func TestFSListSortedWithTypes(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "a_dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(root, "b.txt"), "bb")
	if err := os.Symlink(root, filepath.Join(root, "c_link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	tool := newFSListTool([]Root{{Path: root, Read: true}})

	res, err := tool.Handler(context.Background(), map[string]any{"path": root})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	entries, ok := res["entries"].([]map[string]any)
	if !ok {
		t.Fatalf("entries type = %T, want []map[string]any", res["entries"])
	}
	var names, types []string
	for _, e := range entries {
		names = append(names, e["name"].(string))
		types = append(types, e["type"].(string))
	}
	if !reflect.DeepEqual(names, []string{"a_dir", "b.txt", "c_link"}) {
		t.Errorf("names = %v, want sorted [a_dir b.txt c_link]", names)
	}
	if !reflect.DeepEqual(types, []string{"dir", "file", "symlink"}) {
		t.Errorf("types = %v, want [dir file symlink]", types)
	}
	if got, ok := res["count"].(int); !ok || got != 3 {
		t.Errorf("count = %v, want 3", res["count"])
	}
}

func TestFSListOutsideDenied(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	tool := newFSListTool([]Root{{Path: root, Read: true}})

	_, err := tool.Handler(context.Background(), map[string]any{"path": outside})
	if err == nil || !strings.Contains(err.Error(), "outside the allowed roots") {
		t.Fatalf("Handler outside root error = %v, want ErrOutsideRoots", err)
	}
}

func TestFSListCapabilityDenied(t *testing.T) {
	root := t.TempDir()
	tool := newFSListTool([]Root{{Path: root, Read: false}})

	_, err := tool.Handler(context.Background(), map[string]any{"path": root})
	if err == nil || !strings.Contains(err.Error(), "read denied") {
		t.Fatalf("Handler without Read capability error = %v, want read denied", err)
	}
}
