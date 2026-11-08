package chat

import (
	"encoding/json"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/schemas"
)

func TestCompiledTypeMeta(t *testing.T) {
	compiled := &schemas.CompiledTypesResponse{
		ObjectTypes: []schemas.ObjectTypeSchema{
			{Name: "Law", Label: "Legal Case", UI: json.RawMessage(`{"icon":"lucide--scale","color":"#4F46E5"}`)},
			{Name: "Person", Label: ""}, // no declared label/color
			{Name: "Shadowed", Label: "Wins", Shadowed: true},
		},
		RelationshipTypes: []schemas.RelationshipTypeSchema{
			{Name: "references", Label: "References"},
		},
	}

	got := compiledTypeMeta(compiled)

	if m := got["Law"]; m.Label != "Legal Case" || m.Color != "#4F46E5" {
		t.Errorf("Law = %+v, want Label Legal Case Color #4F46E5", m)
	}
	if m, ok := got["references"]; !ok || m.Label != "References" || m.Color != "" {
		t.Errorf("references = %+v (present %v), want Label References, no color", m, ok)
	}
	// No declared label/color -> omitted.
	if _, ok := got["Person"]; ok {
		t.Errorf("Person should be omitted, got %+v", got["Person"])
	}
	// Shadowed types are skipped.
	if _, ok := got["Shadowed"]; ok {
		t.Errorf("Shadowed should be omitted, got %+v", got["Shadowed"])
	}

	// An object type wins over a same-named relationship type.
	dupe := &schemas.CompiledTypesResponse{
		ObjectTypes:       []schemas.ObjectTypeSchema{{Name: "link", Label: "Link", UI: json.RawMessage(`{"color":"#111111"}`)}},
		RelationshipTypes: []schemas.RelationshipTypeSchema{{Name: "link", Label: "Relationship Link"}},
	}
	if m := compiledTypeMeta(dupe)["link"]; m.Label != "Link" || m.Color != "#111111" {
		t.Errorf("dupe link = %+v, want object type to win (Label Link, Color #111111)", m)
	}
}

func TestCompiledTypeMetaNil(t *testing.T) {
	if got := compiledTypeMeta(nil); got != nil {
		t.Errorf("compiledTypeMeta(nil) = %v, want nil", got)
	}
	empty := compiledTypeMeta(&schemas.CompiledTypesResponse{})
	if len(empty) != 0 {
		t.Errorf("compiledTypeMeta(empty) = %v, want empty map", empty)
	}
}

func TestUIColor(t *testing.T) {
	if got := uiColor(json.RawMessage(`{"icon":"x","color":"#4F46E5"}`)); got != "#4F46E5" {
		t.Errorf("uiColor = %q, want #4F46E5", got)
	}
	if got := uiColor(json.RawMessage(`{"icon":"x"}`)); got != "" {
		t.Errorf("uiColor without color = %q, want empty", got)
	}
	if got := uiColor(json.RawMessage(`{not json`)); got != "" {
		t.Errorf("uiColor malformed = %q, want empty", got)
	}
	if got := uiColor(nil); got != "" {
		t.Errorf("uiColor nil = %q, want empty", got)
	}
}
