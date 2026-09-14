package linux

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

func discardAudit() *auditLogger {
	return newAuditLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func auditBuffer() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func lookupTool(t *testing.T, tools []toolreg.Tool, name string) toolreg.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found in %v", name, toolNames(tools))
	return toolreg.Tool{}
}

func TestWriteAuditSuccess(t *testing.T) {
	root := t.TempDir()
	logger, buf := auditBuffer()
	p, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: root, Write: true}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.WithLogger(logger)
	tool := lookupTool(t, p.Tools(), ToolFSWrite)

	target := filepath.Join(root, "a.txt")
	if _, err := tool.Handler(context.Background(), map[string]any{"path": target, "content": "hello"}); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	logged := buf.String()
	if got := strings.Count(logged, "filesystem tool call"); got != 1 {
		t.Fatalf("audit records = %d, want exactly 1:\n%s", got, logged)
	}
	for _, want := range []string{"tool=linux-fs-write", "outcome=success", "root=", "path="} {
		if !strings.Contains(logged, want) {
			t.Errorf("audit record missing %q:\n%s", want, logged)
		}
	}
}

func TestWriteAuditDeniedOutsideRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	logger, buf := auditBuffer()
	p, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: root, Write: true}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.WithLogger(logger)
	tool := lookupTool(t, p.Tools(), ToolFSWrite)

	if _, err := tool.Handler(context.Background(), map[string]any{"path": filepath.Join(outside, "x.txt"), "content": "x"}); err == nil {
		t.Fatal("expected out-of-root denial")
	}
	if !strings.Contains(buf.String(), "outcome=denied") {
		t.Errorf("denial should be audited with outcome=denied:\n%s", buf.String())
	}
}

func TestWriteAuditDeniedWithoutCapability(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	tool := newFSWriteTool([]Root{{Path: t.TempDir(), Write: false}}, 0, newAuditLogger(logger))

	target := filepath.Join(t.TempDir(), "x")
	if _, err := tool.Handler(context.Background(), map[string]any{"path": target, "content": "x"}); err == nil {
		t.Fatal("expected capability denial")
	}
	if !strings.Contains(buf.String(), "outcome=denied") {
		t.Errorf("capability denial should be audited with outcome=denied:\n%s", buf.String())
	}
}

func TestDeleteAuditSuccess(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "gone.txt")
	writeFile(t, target, "x")
	withTrashPath(t, func(string) error { return nil })

	logger, buf := auditBuffer()
	p, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: root, Delete: true}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.WithLogger(logger)
	tool := lookupTool(t, p.Tools(), ToolFSDelete)

	if _, err := tool.Handler(context.Background(), map[string]any{"path": target}); err != nil {
		t.Fatalf("Handler: %v", err)
	}
	logged := buf.String()
	if got := strings.Count(logged, "filesystem tool call"); got != 1 {
		t.Fatalf("audit records = %d, want exactly 1:\n%s", got, logged)
	}
	for _, want := range []string{"tool=linux-fs-delete", "outcome=success"} {
		if !strings.Contains(logged, want) {
			t.Errorf("audit record missing %q:\n%s", want, logged)
		}
	}
}

func TestDeleteAuditDeniedWithoutCapability(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	root := t.TempDir()
	target := filepath.Join(root, "x.txt")
	writeFile(t, target, "x")
	tool := newFSDeleteTool([]Root{{Path: root, Delete: false}}, false, newAuditLogger(logger))

	if _, err := tool.Handler(context.Background(), map[string]any{"path": target}); err == nil {
		t.Fatal("expected capability denial")
	}
	if !strings.Contains(buf.String(), "outcome=denied") {
		t.Errorf("delete denial should be audited with outcome=denied:\n%s", buf.String())
	}
}

func TestAuditNeverLogsContent(t *testing.T) {
	const secret = "connector-audit-secret-content"
	root := t.TempDir()
	logger, buf := auditBuffer()
	p, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: root, Write: true}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.WithLogger(logger)
	tool := lookupTool(t, p.Tools(), ToolFSWrite)

	if _, err := tool.Handler(context.Background(), map[string]any{"path": filepath.Join(root, "a.txt"), "content": secret}); err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Errorf("audit log leaked file content:\n%s", buf.String())
	}
}
