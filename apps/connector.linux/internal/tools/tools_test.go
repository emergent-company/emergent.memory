package tools

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/emergent-company/memory.web-ui/connector/internal/appletools"
	"github.com/emergent-company/memory.web-ui/connector/internal/config"
	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
	"github.com/emergent-company/memory.web-ui/connector/internal/tools/linux"
)

func names(tools []toolreg.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func hasTool(tools []toolreg.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func reasonFor(disabled []DisabledTool, name string) (string, bool) {
	for _, d := range disabled {
		if d.Name == name {
			return d.Reason, true
		}
	}
	return "", false
}

func boolPtr(v bool) *bool { return &v }

func TestProviderLinuxDefaults(t *testing.T) {
	set, err := providerFor("linux", &config.Config{})
	if err != nil {
		t.Fatalf("providerFor(linux): %v", err)
	}
	if !hasTool(set.Tools, linux.ToolHostInfo) {
		t.Errorf("tools = %v, want host info enabled by default", names(set.Tools))
	}
	for _, name := range []string{linux.ToolFSList, linux.ToolFSRead, linux.ToolFSWrite, linux.ToolFSDelete} {
		if hasTool(set.Tools, name) {
			t.Errorf("tools = %v, %s should be absent without a filesystem config", names(set.Tools), name)
		}
		if reason, ok := reasonFor(set.Disabled, name); !ok || reason != "no filesystem config" {
			t.Errorf("disabled[%s] = (%q, %v), want no filesystem config", name, reason, ok)
		}
	}
}

func TestProviderLinuxConfiguredRootGrantsTools(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Tools: config.ToolsConfig{Linux: config.LinuxToolsConfig{
		Filesystem: &config.FilesystemConfig{
			Roots: []config.RootConfig{{Path: root, Read: true, Write: true, Delete: true}},
		},
	}}}
	set, err := providerFor("linux", cfg)
	if err != nil {
		t.Fatalf("providerFor(linux): %v", err)
	}
	for _, name := range []string{linux.ToolHostInfo, linux.ToolFSList, linux.ToolFSRead, linux.ToolFSWrite, linux.ToolFSDelete} {
		if !hasTool(set.Tools, name) {
			t.Errorf("tools = %v, missing %s", names(set.Tools), name)
		}
	}
	if len(set.Disabled) != 0 {
		t.Errorf("Disabled = %v, want none", set.Disabled)
	}
}

func TestProviderLinuxHostInfoDisabled(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{Linux: config.LinuxToolsConfig{
		HostInfo: &config.HostInfoConfig{Enabled: boolPtr(false)},
	}}}
	set, err := providerFor("linux", cfg)
	if err != nil {
		t.Fatalf("providerFor(linux): %v", err)
	}
	if hasTool(set.Tools, linux.ToolHostInfo) {
		t.Errorf("tools = %v, host info should be disabled", names(set.Tools))
	}
	if reason, ok := reasonFor(set.Disabled, linux.ToolHostInfo); !ok || reason != "host_info disabled in config" {
		t.Errorf("disabled host info = (%q, %v)", reason, ok)
	}
}

func TestProviderLinuxFilesystemDisabled(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Tools: config.ToolsConfig{Linux: config.LinuxToolsConfig{
		Filesystem: &config.FilesystemConfig{
			Enabled: boolPtr(false),
			Roots:   []config.RootConfig{{Path: root, Read: true}},
		},
	}}}
	set, err := providerFor("linux", cfg)
	if err != nil {
		t.Fatalf("providerFor(linux): %v", err)
	}
	if hasTool(set.Tools, linux.ToolFSRead) || hasTool(set.Tools, linux.ToolFSList) {
		t.Errorf("tools = %v, filesystem should be disabled", names(set.Tools))
	}
	if reason, ok := reasonFor(set.Disabled, linux.ToolFSRead); !ok || reason != "filesystem disabled in config" {
		t.Errorf("disabled read = (%q, %v), want filesystem disabled in config", reason, ok)
	}
}

func TestProviderLinuxCapabilityReasons(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Tools: config.ToolsConfig{Linux: config.LinuxToolsConfig{
		Filesystem: &config.FilesystemConfig{
			Roots: []config.RootConfig{{Path: root, Write: true}},
		},
	}}}
	set, err := providerFor("linux", cfg)
	if err != nil {
		t.Fatalf("providerFor(linux): %v", err)
	}
	if !hasTool(set.Tools, linux.ToolFSWrite) {
		t.Errorf("tools = %v, want write tool from a write-capable root", names(set.Tools))
	}
	for _, name := range []string{linux.ToolFSList, linux.ToolFSRead} {
		if hasTool(set.Tools, name) {
			t.Errorf("tools = %v, %s should need a read root", names(set.Tools), name)
		}
		if reason, ok := reasonFor(set.Disabled, name); !ok || reason != "no read-capable root configured" {
			t.Errorf("disabled[%s] = (%q, %v), want no read-capable root configured", name, reason, ok)
		}
	}
	if reason, ok := reasonFor(set.Disabled, linux.ToolFSDelete); !ok || reason != "no delete-capable root configured" {
		t.Errorf("disabled delete = (%q, %v)", reason, ok)
	}
}

func TestProviderLinuxReadOnlyRoot(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Tools: config.ToolsConfig{Linux: config.LinuxToolsConfig{
		Filesystem: &config.FilesystemConfig{Roots: []config.RootConfig{{Path: root, Read: true}}},
	}}}
	set, err := providerFor("linux", cfg)
	if err != nil {
		t.Fatalf("providerFor(linux): %v", err)
	}
	if !hasTool(set.Tools, linux.ToolFSList) || !hasTool(set.Tools, linux.ToolFSRead) {
		t.Errorf("tools = %v, want read + list", names(set.Tools))
	}
	for _, name := range []string{linux.ToolFSWrite, linux.ToolFSDelete} {
		if hasTool(set.Tools, name) {
			t.Errorf("tools = %v, %s should need write/delete capability", names(set.Tools), name)
		}
	}
}

func TestProviderLinuxInvalidRootErrors(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{Linux: config.LinuxToolsConfig{
		Filesystem: &config.FilesystemConfig{Roots: []config.RootConfig{{Path: "relative/path", Read: true}}},
	}}}
	if _, err := providerFor("linux", cfg); err == nil {
		t.Fatal("providerFor(linux) with a relative root: expected error, got nil")
	}
}

func TestProviderNonLinuxFallsBackToAppletools(t *testing.T) {
	wantTools, wantNote := appletools.DefaultProvider()
	set, err := providerFor("darwin", &config.Config{})
	if err != nil {
		t.Fatalf("providerFor(darwin): %v", err)
	}
	if !reflect.DeepEqual(set.Tools, wantTools) {
		t.Errorf("tools = %v, want appletools %v", names(set.Tools), names(wantTools))
	}
	if set.Note != wantNote {
		t.Errorf("note = %q, want %q", set.Note, wantNote)
	}
	if len(set.Disabled) != 0 {
		t.Errorf("Disabled = %v, want none on the appletools path", set.Disabled)
	}
}

func TestProviderNilConfig(t *testing.T) {
	set, err := providerFor("linux", nil)
	if err != nil {
		t.Fatalf("providerFor(linux, nil): %v", err)
	}
	if !hasTool(set.Tools, linux.ToolHostInfo) {
		t.Errorf("tools = %v, want host info with a nil config", names(set.Tools))
	}
}

func TestProviderUsesRuntimePlatform(t *testing.T) {
	want, err := providerFor(runtime.GOOS, &config.Config{})
	if err != nil {
		t.Fatalf("providerFor: %v", err)
	}
	got, err := Provider(&config.Config{})
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if !reflect.DeepEqual(names(got.Tools), names(want.Tools)) {
		t.Errorf("Provider tools = %v, want %v", names(got.Tools), names(want.Tools))
	}
}
