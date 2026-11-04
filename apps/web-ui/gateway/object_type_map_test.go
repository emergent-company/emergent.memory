package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/components/layout"
)

func compiledType(name, label, ui string) CompiledType {
	t := CompiledType{Name: name, Label: label}
	if ui != "" {
		t.UI = json.RawMessage(ui)
	}
	return t
}

// objectTypeMapFor must carry the human label and the declared ui color, merge
// object and relationship types, and skip empty names.
func TestObjectTypeMapFor(t *testing.T) {
	objs := []CompiledType{
		compiledType("LegalParagraph", "Legal paragraph", `{"icon":"lucide--file-text","color":"#4F46E5"}`),
		compiledType("Person", "", ""),
		compiledType("", "no name", ""),
	}
	rels := []CompiledType{
		compiledType("works_at", "Works at", ""),
		compiledType("LegalParagraph", "shadowed", ""), // duplicate name: object wins
	}
	m := objectTypeMapFor(objs, rels)

	if len(m) != 3 {
		t.Fatalf("expected 3 entries, got %d (%v)", len(m), m)
	}
	if got := m["LegalParagraph"]; got.Label != "Legal paragraph" || got.Color != "#4F46E5" {
		t.Errorf("LegalParagraph entry = %+v", got)
	}
	// No label declared → the name is the label (compiledTypeLabel fallback).
	if got := m["Person"]; got.Label != "Person" {
		t.Errorf("Person label = %q, want fallback to name", got.Label)
	}
	if got := m["works_at"]; got.Label != "Works at" {
		t.Errorf("relationship entry label = %q", got.Label)
	}
	if _, ok := m[""]; ok {
		t.Error("empty type name should be skipped")
	}
}

// Shadowed types are inactive and must never enter the client map — neither a
// superseded object type nor a shadowed relationship type.
func TestObjectTypeMapForSkipsShadowed(t *testing.T) {
	objs := []CompiledType{
		{Name: "note", Label: "Old note", Shadowed: true},
		{Name: "note", Label: "Note"},
	}
	rels := []CompiledType{
		{Name: "annotates", Shadowed: true},
	}
	m := objectTypeMapFor(objs, rels)
	if _, ok := m["annotates"]; ok {
		t.Error("shadowed relationship must not be in the map")
	}
	if got := m["note"].Label; got != "Note" {
		t.Errorf("winning note label = %q, want the non-shadowed declaration", got)
	}
}

// objectTypeMapJSON must always be valid JSON and must not permit a </script>
// breakout when embedded in the shell.
func TestObjectTypeMapJSON(t *testing.T) {
	if got := objectTypeMapJSON(nil); got != "{}" {
		t.Errorf("nil map = %q, want {}", got)
	}
	m := objectTypeMapFor([]CompiledType{
		compiledType("Evil", "</script><img src=x>", ""),
	}, nil)
	got := objectTypeMapJSON(m)
	if strings.Contains(got, "</script>") || strings.Contains(got, "<img") {
		t.Fatalf("serialized map is not script-safe: %s", got)
	}
	var back map[string]objectTypeUI
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("serialized map is not valid JSON: %v", err)
	}
	if back["Evil"].Label != "</script><img src=x>" {
		t.Errorf("label round-trip = %q", back["Evil"].Label)
	}
}

// The shell embeds the type map as a JSON script the chat client reads.
func TestAppShellEmbedsObjectTypeMap(t *testing.T) {
	m := objectTypeMapFor([]CompiledType{
		compiledType("LegalParagraph", "Legal paragraph", `{"color":"#4F46E5"}`),
	}, nil)
	html := renderHTML(t, appShell(
		"Chat", []layout.SidebarGroup{}, false, nil, "", nil, "", nil, nil, nil, nil, false,
		0,
		nil, nil, templ.NopComponent, m,
		"", "", 0, 0, 0, "",
	))
	if !strings.Contains(html, `id="memory-object-types"`) {
		t.Fatal("shell is missing the embedded type map script")
	}
	if !strings.Contains(html, `Legal paragraph`) {
		t.Fatal("embedded type map is missing the human label")
	}
}

// A missing map still renders a valid (empty) script element, so the client
// falls back to humanized type names instead of erroring.
func TestAppShellObjectTypeMapEmpty(t *testing.T) {
	html := renderHTML(t, appShell(
		"Chat", nil, false, nil, "", nil, "", nil, nil, nil, nil, false,
		0,
		nil, nil, templ.NopComponent, nil,
		"", "", 0, 0, 0, "",
	))
	if !strings.Contains(html, `id="memory-object-types">{}</script>`) {
		t.Fatalf("expected empty map script, got: %s", html)
	}
}
