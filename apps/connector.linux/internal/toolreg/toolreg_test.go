package toolreg

import (
	"context"
	"strings"
	"testing"
)

func sampleTool(name string) Tool {
	return Tool{
		Name:        name,
		Description: "does " + name,
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     func(context.Context, map[string]any) (map[string]any, error) { return map[string]any{}, nil },
	}
}

func mustRegister(t *testing.T, r *Registry, tools ...Tool) {
	t.Helper()
	for _, tool := range tools {
		if err := r.Register(tool); err != nil {
			t.Fatalf("Register(%q): %v", tool.Name, err)
		}
	}
}

func TestRegisterListOrderAndFields(t *testing.T) {
	r := New()
	mustRegister(t, r, sampleTool("a"), sampleTool("b"), sampleTool("c"))

	got := r.List()
	if len(got) != 3 {
		t.Fatalf("List() length = %d, want 3", len(got))
	}
	for i, want := range []string{"a", "b", "c"} {
		if got[i].Name != want {
			t.Errorf("List()[%d].Name = %q, want %q", i, got[i].Name, want)
		}
	}
	if got[0].Description != "does a" || got[0].Handler == nil || got[0].InputSchema == nil {
		t.Errorf("List() entry lost fields: %+v", got[0])
	}
}

func TestLookup(t *testing.T) {
	r := New()
	mustRegister(t, r, sampleTool("notes_search"))

	tool, ok := r.Lookup("notes_search")
	if !ok {
		t.Fatal("Lookup(existing) = false, want true")
	}
	if tool.Name != "notes_search" {
		t.Errorf("Lookup name = %q", tool.Name)
	}
	if _, ok := r.Lookup("unknown"); ok {
		t.Error("Lookup(unknown) = true, want false")
	}
}

func TestRegisterErrors(t *testing.T) {
	cases := []struct {
		name string
		tool Tool
		want string
	}{
		{name: "empty name", tool: Tool{Name: "", Handler: func(context.Context, map[string]any) (map[string]any, error) { return nil, nil }}, want: "empty"},
		{name: "nil handler", tool: Tool{Name: "x"}, want: "handler"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			err := r.Register(tc.tool)
			if err == nil {
				t.Fatal("Register: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Register error %q should mention %q", err, tc.want)
			}
		})
	}

	t.Run("duplicate", func(t *testing.T) {
		r := New()
		mustRegister(t, r, sampleTool("dup"))
		err := r.Register(sampleTool("dup"))
		if err == nil {
			t.Fatal("Register(duplicate): expected error, got nil")
		}
		if !strings.Contains(err.Error(), "already registered") {
			t.Errorf("Register(duplicate) error %q should say already registered", err)
		}
		if len(r.List()) != 1 {
			t.Errorf("duplicate register changed registry, list length = %d", len(r.List()))
		}
	})
}

func TestToolsListPayloadNestedShape(t *testing.T) {
	r := New()
	mustRegister(t, r, sampleTool("notes_search"), sampleTool("reminders_add"))

	payload := r.ToolsListPayload()
	rawTools, ok := payload["tools"]
	if !ok {
		t.Fatalf("ToolsListPayload() missing nested \"tools\" key: %v", payload)
	}
	items, ok := rawTools.([]map[string]any)
	if !ok {
		t.Fatalf("ToolsListPayload()[\"tools\"] type = %T, want []map[string]any", rawTools)
	}
	if len(items) != 2 {
		t.Fatalf("payload tools length = %d, want 2", len(items))
	}
	for _, item := range items {
		for _, key := range []string{"name", "description", "inputSchema"} {
			if _, ok := item[key]; !ok {
				t.Errorf("payload item missing %q: %v", key, item)
			}
		}
		schema, ok := item["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("inputSchema type = %T, want map[string]any", item["inputSchema"])
		}
		if schema["type"] != "object" {
			t.Errorf("inputSchema type = %v, want \"object\"", schema["type"])
		}
		if _, ok := schema["properties"]; !ok {
			t.Errorf("inputSchema missing properties: %v", schema)
		}
	}
	if items[0]["name"] != "notes_search" || items[1]["name"] != "reminders_add" {
		t.Errorf("payload order = [%v, %v], want registration order", items[0]["name"], items[1]["name"])
	}
}

func TestEmptyRegistry(t *testing.T) {
	r := New()
	if got := r.List(); len(got) != 0 {
		t.Errorf("List() = %v, want empty", got)
	}
	payload := r.ToolsListPayload()
	if items := payload["tools"].([]map[string]any); len(items) != 0 {
		t.Errorf("payload tools = %v, want empty", items)
	}
}

func TestFilterDisabled(t *testing.T) {
	tools := []Tool{sampleTool("notes_search"), sampleTool("notes_create"), sampleTool("reminders_list")}

	got := FilterDisabled(tools, []string{"notes_create"})
	if len(got) != 2 || got[0].Name != "notes_search" || got[1].Name != "reminders_list" {
		t.Errorf("FilterDisabled = %v, want order preserved minus notes_create", namesOf(got))
	}

	// Unknown names are ignored, not an error.
	if got := FilterDisabled(tools, []string{"nope", "reminders_list"}); len(got) != 2 {
		t.Errorf("FilterDisabled with unknown name = %v, want 2 tools", namesOf(got))
	}

	// Empty disabled list returns the tools unchanged.
	if got := FilterDisabled(tools, nil); len(got) != 3 {
		t.Errorf("FilterDisabled(nil) = %v, want all 3", namesOf(got))
	}

	// All disabled -> empty (non-nil).
	if got := FilterDisabled(tools, []string{"notes_search", "notes_create", "reminders_list"}); len(got) != 0 {
		t.Errorf("FilterDisabled(all) = %v, want empty", got)
	}
}

func namesOf(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func TestMarkDisabledExcludesFromListAndPayload(t *testing.T) {
	r := New()
	mustRegister(t, r, sampleTool("notes_search"), sampleTool("reminders_list"), sampleTool("notes_create"))
	r.MarkDisabled("notes_create")

	if got := namesOf(r.List()); len(got) != 2 || got[0] != "notes_search" || got[1] != "reminders_list" {
		t.Errorf("List after disable = %v, want notes_search, reminders_list", got)
	}
	items := r.ToolsListPayload()["tools"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("payload tools = %v, want 2", items)
	}
	for _, item := range items {
		if item["name"] == "notes_create" {
			t.Errorf("payload still contains disabled tool: %v", items)
		}
	}
}

func TestMarkDisabledLookupRejectsWithNamedError(t *testing.T) {
	r := New()
	mustRegister(t, r, sampleTool("notes_search"))
	r.MarkDisabled("notes_search")

	// Lookup still resolves the tool so dispatch answers with a distinct
	// "disabled" error instead of "unknown tool".
	tool, ok := r.Lookup("notes_search")
	if !ok {
		t.Fatal("Lookup(disabled) = false, want true")
	}
	_, err := tool.Handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("disabled tool handler: expected error, got nil")
	}
	if !strings.Contains(err.Error(), `"notes_search"`) || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled handler error = %q, want it naming the tool as disabled", err)
	}
}

func TestMarkDisabledIdempotentAndUnknownNoop(t *testing.T) {
	r := New()
	mustRegister(t, r, sampleTool("notes_search"))

	r.MarkDisabled("notes_search")
	r.MarkDisabled("notes_search") // idempotent
	r.MarkDisabled("never_registered")

	items := r.ToolsListPayload()["tools"].([]map[string]any)
	if len(items) != 0 {
		t.Errorf("payload tools = %v, want none", items)
	}
	if _, ok := r.Lookup("notes_search"); !ok {
		t.Error("disabled tool should still be resolvable for dispatch")
	}
}
