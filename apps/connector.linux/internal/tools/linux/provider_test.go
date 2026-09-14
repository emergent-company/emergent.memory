package linux

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

func toolNames(tools []toolreg.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

func hasTool(tools []toolreg.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func TestToolNameConstants(t *testing.T) {
	if ToolHostInfo != "linux-host-info" || ToolFSList != "linux-fs-list" || ToolFSRead != "linux-fs-read" {
		t.Errorf("unexpected tool-name constants: %q %q %q", ToolHostInfo, ToolFSList, ToolFSRead)
	}
	if ToolFSWrite != "linux-fs-write" || ToolFSDelete != "linux-fs-delete" {
		t.Errorf("unexpected reserved tool-name constants: %q %q", ToolFSWrite, ToolFSDelete)
	}
}

func TestProviderHostInfoDefaultEnabled(t *testing.T) {
	p, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !hasTool(p.Tools(), ToolHostInfo) {
		t.Errorf("tools = %v, want host info enabled by default", toolNames(p.Tools()))
	}
}

func TestProviderHostInfoDisabled(t *testing.T) {
	disabled := false
	p, err := New(Config{HostInfo: &disabled})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if hasTool(p.Tools(), ToolHostInfo) {
		t.Errorf("tools = %v, host info should be disabled", toolNames(p.Tools()))
	}
	if !containsName(p.Disabled(), ToolHostInfo) {
		t.Errorf("Disabled = %v, want %s", p.Disabled(), ToolHostInfo)
	}
	if !strings.Contains(p.Note(), "host info disabled") {
		t.Errorf("Note = %q, want a host-info reason", p.Note())
	}
}

func TestProviderFilesystemAbsentWithoutConfig(t *testing.T) {
	p, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if hasTool(p.Tools(), ToolFSList) || hasTool(p.Tools(), ToolFSRead) {
		t.Errorf("tools = %v, want no filesystem tools without config", toolNames(p.Tools()))
	}
	if !containsName(p.Disabled(), ToolFSList) || !containsName(p.Disabled(), ToolFSRead) {
		t.Errorf("Disabled = %v, want fs tools listed", p.Disabled())
	}
}

func TestProviderFilesystemNoRoots(t *testing.T) {
	p, err := New(Config{Filesystem: &Filesystem{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if hasTool(p.Tools(), ToolFSList) || hasTool(p.Tools(), ToolFSRead) {
		t.Errorf("tools = %v, want no filesystem tools with no roots", toolNames(p.Tools()))
	}
	if !strings.Contains(p.Note(), "no roots configured") {
		t.Errorf("Note = %q, want a no-roots reason", p.Note())
	}
}

func TestProviderFilesystemNoReadRoot(t *testing.T) {
	root := t.TempDir()
	p, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: root, Write: true}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if hasTool(p.Tools(), ToolFSList) || hasTool(p.Tools(), ToolFSRead) {
		t.Errorf("tools = %v, want no fs tools without a read root", toolNames(p.Tools()))
	}
	if !strings.Contains(p.Note(), "no read-capable root") {
		t.Errorf("Note = %q, want a no-read-root reason", p.Note())
	}
}

func TestProviderFilesystemReadRoot(t *testing.T) {
	root := t.TempDir()
	p, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: root, Read: true}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tools := p.Tools()
	if !hasTool(tools, ToolFSList) || !hasTool(tools, ToolFSRead) {
		t.Errorf("tools = %v, want fs list + read", toolNames(tools))
	}
	// A read-only root does not grant write/delete.
	if hasTool(tools, ToolFSWrite) || hasTool(tools, ToolFSDelete) {
		t.Errorf("write/delete must not be registered for a read-only root: %v", toolNames(tools))
	}
	if !containsName(p.Disabled(), ToolFSWrite) || !containsName(p.Disabled(), ToolFSDelete) {
		t.Errorf("Disabled = %v, want write/delete listed", p.Disabled())
	}
}

func TestProviderFilesystemWriteDeleteRoots(t *testing.T) {
	root := t.TempDir()
	p, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: root, Write: true, Delete: true}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tools := p.Tools()
	if !hasTool(tools, ToolFSWrite) || !hasTool(tools, ToolFSDelete) {
		t.Errorf("tools = %v, want write + delete registered", toolNames(tools))
	}
	if hasTool(tools, ToolFSList) || hasTool(tools, ToolFSRead) {
		t.Errorf("tools = %v, read tools should be absent without a read root", toolNames(tools))
	}
	if !containsName(p.Disabled(), ToolFSList) || !containsName(p.Disabled(), ToolFSRead) {
		t.Errorf("Disabled = %v, want read tools listed", p.Disabled())
	}
	if containsName(p.Disabled(), ToolFSWrite) || containsName(p.Disabled(), ToolFSDelete) {
		t.Errorf("Disabled = %v, write/delete should not be listed", p.Disabled())
	}
}

func TestProviderFilesystemAllCapabilities(t *testing.T) {
	root := t.TempDir()
	p, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: root, Read: true, Write: true, Delete: true}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tools := p.Tools()
	for _, name := range []string{ToolFSList, ToolFSRead, ToolFSWrite, ToolFSDelete} {
		if !hasTool(tools, name) {
			t.Errorf("tools = %v, missing %s", toolNames(tools), name)
		}
	}
	if len(p.Disabled()) != 0 {
		t.Errorf("Disabled = %v, want empty for all-capable roots", p.Disabled())
	}
	if p.Note() != "" {
		t.Errorf("Note = %q, want empty for all-capable roots", p.Note())
	}
}

func TestProviderDefaultSizes(t *testing.T) {
	root := t.TempDir()
	p, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: root, Read: true}}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.maxReadBytes != defaultMaxBytes {
		t.Errorf("maxReadBytes = %d, want default %d", p.maxReadBytes, defaultMaxBytes)
	}
	if p.maxWriteBytes != defaultMaxBytes {
		t.Errorf("maxWriteBytes = %d, want default %d", p.maxWriteBytes, defaultMaxBytes)
	}
}

func TestProviderCustomSizesAndFlags(t *testing.T) {
	root := t.TempDir()
	p, err := New(Config{Filesystem: &Filesystem{
		Roots:                []Root{{Path: root, Read: true, Write: true, Delete: true}},
		MaxReadBytes:         10,
		MaxWriteBytes:        20,
		AllowPermanentDelete: true,
	}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.maxReadBytes != 10 || p.maxWriteBytes != 20 {
		t.Errorf("sizes = (%d, %d), want (10, 20)", p.maxReadBytes, p.maxWriteBytes)
	}
	if !p.allowPermanentDelete {
		t.Error("AllowPermanentDelete should be carried through")
	}
}

func TestProviderNegativeSizesUseDefaults(t *testing.T) {
	p, err := New(Config{Filesystem: &Filesystem{
		Roots:         []Root{{Path: t.TempDir(), Read: true}},
		MaxReadBytes:  -1,
		MaxWriteBytes: -5,
	}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.maxReadBytes != defaultMaxBytes || p.maxWriteBytes != defaultMaxBytes {
		t.Errorf("sizes = (%d, %d), want defaults", p.maxReadBytes, p.maxWriteBytes)
	}
}

func TestProviderRelativeRootIsError(t *testing.T) {
	if _, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: "relative/dir", Read: true}}}}); err == nil {
		t.Fatal("New with relative root: expected error, got nil")
	}
	if _, err := New(Config{Filesystem: &Filesystem{Roots: []Root{{Path: "  ", Read: true}}}}); err == nil {
		t.Fatal("New with blank root: expected error, got nil")
	}
}

func TestProviderToolsReturnsCopy(t *testing.T) {
	p, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tools := p.Tools()
	tools = append(tools, toolreg.Tool{Name: "mutated"})
	if !hasTool(tools, "mutated") {
		t.Fatal("test setup: append did not add the tool")
	}
	if hasTool(p.Tools(), "mutated") {
		t.Error("Tools() must not expose the provider's internal slice")
	}
}
