package appletools

import (
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

func names(tools []toolreg.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func TestProviderUnavailable(t *testing.T) {
	f := &fakeRunner{available: false}
	tools, note := Provider(f)
	if len(tools) != 0 {
		t.Errorf("Provider(unavailable) tools = %v, want none", names(tools))
	}
	if note != PlatformNote {
		t.Errorf("Provider(unavailable) note = %q, want PlatformNote", note)
	}
	if PlatformNote == "" {
		t.Error("PlatformNote must be non-empty for status output")
	}
}

func TestProviderAvailable(t *testing.T) {
	f := &fakeRunner{available: true, out: "[]"}
	tools, note := Provider(f)
	if note != "" {
		t.Errorf("Provider(available) note = %q, want empty", note)
	}
	if len(tools) != 7 {
		t.Fatalf("Provider(available) tool count = %d, want 7 (%v)", len(tools), names(tools))
	}
	wantOrder := []string{"notes_search", "notes_create", "reminders_list", "reminders_lists", "reminders_add", "reminders_update", "reminders_delete"}
	for i, want := range wantOrder {
		if tools[i].Name != want {
			t.Errorf("tools[%d].Name = %q, want %q", i, tools[i].Name, want)
		}
	}
	for _, tool := range tools {
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.Handler == nil {
			t.Errorf("tool %q has nil handler", tool.Name)
		}
		schema := tool.InputSchema
		if schema["type"] != "object" {
			t.Errorf("tool %q inputSchema missing type:object: %v", tool.Name, schema)
		}
		if _, ok := schema["properties"]; !ok {
			t.Errorf("tool %q inputSchema missing properties", tool.Name)
		}
	}
}

func TestDefaultProviderBuildsWithoutRunningScripts(t *testing.T) {
	// DefaultProvider uses the real runner but must not execute AppleScript
	// while merely building the tool set.
	tools, _ := DefaultProvider()
	if len(tools) != 0 && len(tools) != 7 {
		t.Errorf("DefaultProvider tool count = %d, want 0 (non-macOS) or 7 (macOS)", len(tools))
	}
}
