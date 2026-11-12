package main

import (
	"encoding/json"
	"testing"
)

// jsonRaw is the test-side shortcut for an authored raw JSON block. An empty
// string stays nil, which is how a type with no ui block reaches the parser.
func jsonRaw(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}

// TestParseTypeUIView covers the closed vocabulary a schema pack may author in
// `ui.view`. The parser is the only gate between authored data and the
// renderer, so both directions matter: an accepted view must land in the exact
// shape the resolver expects, and anything unrecognised must be refused rather
// than half-applied.
func TestParseTypeUIView(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		wantNil bool
		check   func(t *testing.T, v *typeUIView)
	}{
		{
			name:    "no ui block at all",
			raw:     ``,
			wantNil: true,
		},
		{
			name:    "ui block without a view",
			raw:     `{"icon":"🏠","color":"#123456"}`,
			wantNil: true,
		},
		{
			name:    "explicit null view",
			raw:     `{"icon":"🏠","view":null}`,
			wantNil: true,
		},
		{
			name:    "empty view declares nothing",
			raw:     `{"view":{"sections":[],"inline":[]}}`,
			wantNil: true,
		},
		{
			name: "full view",
			raw: `{"view":{
				"inline":["address","status"],
				"sections":[
					{"title":"Utilities","relation":["serves"],"dir":"in","limit":4},
					{"title":"Obligations","relation":["has_obligation"]}
				]}}`,
			check: func(t *testing.T, v *typeUIView) {
				if !v.More {
					t.Error("more should default to true so an undeclared relationship type stays visible")
				}
				if len(v.Inline) != 2 || v.Inline[0] != "address" || v.Inline[1] != "status" {
					t.Errorf("inline = %v, want [address status]", v.Inline)
				}
				if len(v.Sections) != 2 {
					t.Fatalf("sections = %d, want 2", len(v.Sections))
				}
				first := v.Sections[0]
				if first.Title != "Utilities" || first.direction() != "in" || first.rowLimit() != 4 {
					t.Errorf("first section = %+v, want Utilities/in/4", first)
				}
				second := v.Sections[1]
				if second.direction() != "both" {
					t.Errorf("direction = %q, want both (default)", second.direction())
				}
				if second.rowLimit() != viewDefaultLimit {
					t.Errorf("rowLimit = %d, want the default %d", second.rowLimit(), viewDefaultLimit)
				}
			},
		},
		{
			name: "bare string relation and derived title",
			raw:  `{"view":{"sections":[{"relation":"has_obligation"}]}}`,
			check: func(t *testing.T, v *typeUIView) {
				if len(v.Sections) != 1 {
					t.Fatalf("sections = %d, want 1", len(v.Sections))
				}
				if got := v.Sections[0].Title; got != "has obligation" {
					t.Errorf("title = %q, want the humanised relation type", got)
				}
			},
		},
		{
			name: "more can be switched off",
			raw:  `{"view":{"more":false,"sections":[{"relation":["serves"]}]}}`,
			check: func(t *testing.T, v *typeUIView) {
				if v.More {
					t.Error("more = true, want the authored false")
				}
			},
		},
		{
			name: "inline only",
			raw:  `{"view":{"inline":["status"]}}`,
			check: func(t *testing.T, v *typeUIView) {
				if v == nil || len(v.Inline) != 1 || len(v.Sections) != 0 {
					t.Fatalf("view = %+v, want inline-only", v)
				}
			},
		},
		{
			name:    "unknown key is refused",
			raw:     `{"view":{"sectons":[{"relation":["serves"]}]}}`,
			wantErr: true,
		},
		{
			name:    "section without a relation is refused",
			raw:     `{"view":{"sections":[{"title":"Utilities"}]}}`,
			wantErr: true,
		},
		{
			name:    "unknown direction is refused",
			raw:     `{"view":{"sections":[{"relation":["serves"],"dir":"sideways"}]}}`,
			wantErr: true,
		},
		{
			name:    "limit above the cap is refused",
			raw:     `{"view":{"sections":[{"relation":["serves"],"limit":500}]}}`,
			wantErr: true,
		},
		{
			name:    "negative limit is refused",
			raw:     `{"view":{"sections":[{"relation":["serves"],"limit":-1}]}}`,
			wantErr: true,
		},
		{
			name:    "relation of the wrong type is refused",
			raw:     `{"view":{"sections":[{"relation":{"type":"serves"}}]}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view, err := parseTypeUIView(jsonRaw(tt.raw))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseTypeUIView() = %+v, want an error", view)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTypeUIView() error = %v", err)
			}
			if tt.wantNil {
				if view != nil {
					t.Fatalf("parseTypeUIView() = %+v, want nil", view)
				}
				return
			}
			if view == nil {
				t.Fatal("parseTypeUIView() = nil, want a view")
			}
			if tt.check != nil {
				tt.check(t, view)
			}
		})
	}
}

// TestTypeUIViewOfDegradesSilently pins the render-path contract: a malformed
// view must never reach the page, and must never be an error either — the page
// falls back to the generic relationship list.
func TestTypeUIViewOfDegradesSilently(t *testing.T) {
	if got := typeUIViewOf(jsonRaw(`{"view":{"nonsense":true}}`)); got != nil {
		t.Fatalf("typeUIViewOf() = %+v, want nil for a malformed view", got)
	}
	if got := typeUIViewOf(jsonRaw(`{"view":{"inline":["status"]}}`)); got == nil {
		t.Fatal("typeUIViewOf() = nil for a valid view")
	}
}
