package schemas

import (
	"encoding/json"
	"testing"
)

func TestCitationTypeMetaFromCompiled(t *testing.T) {
	compiled := &CompiledTypesResponse{
		ObjectTypes: []ObjectTypeSchema{
			{Name: "Law", Label: "Legal Case", UI: json.RawMessage(`{"icon":"lucide--scale","color":"#4F46E5"}`)},
			{Name: "Person", Label: ""}, // no declared label/color
			{Name: "Shadowed", Label: "Wins", Shadowed: true},
		},
		RelationshipTypes: []RelationshipTypeSchema{
			{Name: "references", Label: "References"},
		},
	}

	got := CitationTypeMetaFromCompiled(compiled)

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
	dupe := &CompiledTypesResponse{
		ObjectTypes:       []ObjectTypeSchema{{Name: "link", Label: "Link", UI: json.RawMessage(`{"color":"#111111"}`)}},
		RelationshipTypes: []RelationshipTypeSchema{{Name: "link", Label: "Relationship Link"}},
	}
	if m := CitationTypeMetaFromCompiled(dupe)["link"]; m.Label != "Link" || m.Color != "#111111" {
		t.Errorf("dupe link = %+v, want object type to win (Label Link, Color #111111)", m)
	}
}

func TestCitationTypeMetaFromCompiledNil(t *testing.T) {
	if got := CitationTypeMetaFromCompiled(nil); got != nil {
		t.Errorf("CitationTypeMetaFromCompiled(nil) = %v, want nil", got)
	}
	empty := CitationTypeMetaFromCompiled(&CompiledTypesResponse{})
	if len(empty) != 0 {
		t.Errorf("CitationTypeMetaFromCompiled(empty) = %v, want empty map", empty)
	}
}

func TestTypeUIColor(t *testing.T) {
	if got := typeUIColor(json.RawMessage(`{"icon":"x","color":"#4F46E5"}`)); got != "#4F46E5" {
		t.Errorf("typeUIColor = %q, want #4F46E5", got)
	}
	if got := typeUIColor(json.RawMessage(`{"icon":"x"}`)); got != "" {
		t.Errorf("typeUIColor without color = %q, want empty", got)
	}
	if got := typeUIColor(json.RawMessage(`{not json`)); got != "" {
		t.Errorf("typeUIColor malformed = %q, want empty", got)
	}
	if got := typeUIColor(nil); got != "" {
		t.Errorf("typeUIColor nil = %q, want empty", got)
	}
}
