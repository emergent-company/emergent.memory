package schemas

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestParseIncomingObjectTypesStrict pins the PreviewMerge request-boundary
// contract for objectTypes. Unlike the shared tolerant storage parser, a
// payload that is present but unreadable must be reported instead of being
// silently equal to "no object types".
func TestParseIncomingObjectTypesStrict(t *testing.T) {
	const shapeExample = `{"name":"Person"`

	tests := []struct {
		name       string
		data       json.RawMessage
		wantNames  []string
		wantSchema map[string]string // type name → substring expected in its raw schema
		wantErr    bool
		wantInErr  []string
	}{
		{
			name:      "nil is absent",
			data:      nil,
			wantNames: nil,
		},
		{
			name:      "empty raw message is absent",
			data:      json.RawMessage{},
			wantNames: nil,
		},
		{
			name:      "JSON null is treated as absent",
			data:      json.RawMessage(`null`),
			wantNames: nil,
		},
		{
			name:      "empty array has no types",
			data:      json.RawMessage(`[]`),
			wantNames: nil,
		},
		{
			name:      "empty object has no types",
			data:      json.RawMessage(`{}`),
			wantNames: nil,
		},
		{
			name:      "valid array format",
			data:      json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}}},{"name":"Note"}]`),
			wantNames: []string{"Note", "Person"},
			wantSchema: map[string]string{
				"Person": `"properties":{"name":{"type":"string"}}`,
			},
		},
		{
			name:      "valid map format",
			data:      json.RawMessage(`{"Person":{"label":"Person","properties":{"name":{"type":"string"}}},"Note":{}}`),
			wantNames: []string{"Note", "Person"},
			wantSchema: map[string]string{
				"Person": `"label":"Person","properties":{"name":{"type":"string"}}`,
				"Note":   `{}`,
			},
		},
		{
			name:      "JSON string scalar",
			data:      json.RawMessage(`"hello"`),
			wantErr:   true,
			wantInErr: []string{"objectTypes", "JSON array", "JSON object", shapeExample},
		},
		{
			name:      "JSON number scalar",
			data:      json.RawMessage(`123`),
			wantErr:   true,
			wantInErr: []string{"objectTypes", "JSON array", "JSON object", shapeExample},
		},
		{
			name:      "invalid JSON object",
			data:      json.RawMessage(`{"Person": </bad/>}`),
			wantErr:   true,
			wantInErr: []string{"objectTypes", "not valid JSON", "JSON array", "JSON object", shapeExample},
		},
		{
			name:      "array with a valid entry and one missing name",
			data:      json.RawMessage(`[{"name":"Person"},{"properties":{}}]`),
			wantErr:   true,
			wantInErr: []string{"objectTypes", "entry 2", `"name"`, "JSON array", "JSON object", shapeExample},
		},
		{
			name:      "array with a non-object entry",
			data:      json.RawMessage(`[{"name":"Person"}, 42]`),
			wantErr:   true,
			wantInErr: []string{"objectTypes", "entry 2", "JSON object", "JSON array", shapeExample},
		},
		{
			name:      "array with a non-string name",
			data:      json.RawMessage(`[{"name":"Person"},{"name":42}]`),
			wantErr:   true,
			wantInErr: []string{"objectTypes", "entry 2", `"name"`, "string", "JSON array", "JSON object", shapeExample},
		},
		{
			name:      "array entry with a non-string label",
			data:      json.RawMessage(`[{"name":"Person"},{"name":"Note","label":42}]`),
			wantErr:   true,
			wantInErr: []string{"objectTypes", "entry 2", "label", "expected a string", "JSON array", "JSON object", shapeExample},
		},
		{
			name:      "map with a non-schema value",
			data:      json.RawMessage(`{"Person":"hello"}`),
			wantErr:   true,
			wantInErr: []string{"objectTypes", `objectTypes["Person"]`, "JSON object schema", "JSON array", "JSON object", shapeExample},
		},
		{
			name:      "map with a null schema value",
			data:      json.RawMessage(`{"Person":null}`),
			wantErr:   true,
			wantInErr: []string{"objectTypes", `objectTypes["Person"]`, "JSON object schema"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseIncomingObjectTypesStrict(tc.data)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got map with %d entries: %v", len(got), keys(got))
				}
				if got != nil {
					t.Errorf("expected nil map on error, got %v", keys(got))
				}
				msg := err.Error()
				for _, want := range tc.wantInErr {
					if !strings.Contains(msg, want) {
						t.Errorf("error message does not mention %q: %s", want, msg)
					}
				}
				// The message must be actionable, not a Go decoder dump.
				for _, unwanted := range []string{"Go value", "struct {", "cannot unmarshal"} {
					if strings.Contains(msg, unwanted) {
						t.Errorf("error message leaks decoder internals (%q): %s", unwanted, msg)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("expected an empty map (not nil) for a valid/absent payload")
			}
			names := keys(got)
			if strings.Join(names, ",") != strings.Join(tc.wantNames, ",") {
				t.Errorf("type names = %v, want %v", names, tc.wantNames)
			}
			for typeName, want := range tc.wantSchema {
				if !strings.Contains(string(got[typeName]), want) {
					t.Errorf("schema for %q = %s, want it to contain %s", typeName, got[typeName], want)
				}
			}
		})
	}
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
