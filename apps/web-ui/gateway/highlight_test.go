package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHighlightJSONValue(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantOK  bool
		contain []string // substrings that must be present in the HTML
		absent  []string // substrings that must NOT be present
	}{
		{
			name:   "object",
			in:     `{"ok":true,"count":3}`,
			wantOK: true,
			contain: []string{
				`<span`,        // styled token present
				`&#34;ok&#34;`, // keys escaped but visible
				`&#34;count&#34;`,
			},
		},
		{
			name:   "double-encoded result unwrapped",
			in:     `{"ok":true,"result":"[\n  {\n    \"id\": \"a1\"\n  }\n]"}`,
			wantOK: true,
			contain: []string{
				// inner JSON is expanded to a structured array, not a blob of \n
				`&#34;id&#34;`, `&#34;a1&#34;`,
			},
		},
		{
			name:   "double-encoded result not left as escaped string",
			in:     `{"ok":true,"result":"[{\"id\":\"a1\"}]"}`,
			wantOK: true,
			contain: []string{
				`&#34;result&#34;`, `&#34;a1&#34;`,
			},
		},
		{
			name:   "plain string output is not highlighted",
			in:     `"just a message"`,
			wantOK: false,
		},
		{
			name:   "scalar number is not highlighted",
			in:     `42`,
			wantOK: false,
		},
		{
			name:   "invalid json",
			in:     `not json`,
			wantOK: false,
		},
		{
			name:   "empty",
			in:     ``,
			wantOK: false,
		},
		{
			name:   "xss escaped",
			in:     `{"x":"<script>alert(1)</script>"}`,
			wantOK: true,
			absent: []string{`<script>alert(1)</script>`},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := highlightJSONValue(json.RawMessage(c.in))
			if ok != c.wantOK {
				t.Fatalf("highlightJSONValue(%q) ok = %v, want %v (got len %d)", c.in, ok, c.wantOK, len(got))
			}
			for _, w := range c.contain {
				if !strings.Contains(got, w) {
					t.Errorf("highlightJSONValue(%q) missing %q in %q", c.in, w, got)
				}
			}
			for _, n := range c.absent {
				if strings.Contains(got, n) {
					t.Errorf("highlightJSONValue(%q) unexpectedly contains %q in %q", c.in, n, got)
				}
			}
		})
	}
}

func TestUnwrapJSONStrings(t *testing.T) {
	// nested + array + scalar-safety + depth
	in := map[string]any{
		"a":   `{"inner": [1, 2]}`,
		"b":   []any{`[true]`, "plain"},
		"num": "123",
		"txt": "hello",
		"nul": "null",
	}
	out := unwrapJSONStrings(in, 0).(map[string]any)

	if _, ok := out["a"].(map[string]any); !ok {
		t.Errorf("a not unwrapped to object: %#v", out["a"])
	}
	if arr, ok := out["b"].([]any); ok {
		if _, ok2 := arr[0].([]any); !ok2 {
			t.Errorf("b[0] not unwrapped to array: %#v", arr[0])
		}
	} else {
		t.Fatalf("b not a slice: %#v", out["b"])
	}
	if out["num"] != "123" || out["txt"] != "hello" || out["nul"] != "null" {
		t.Errorf("scalars mutated: %#v", out)
	}
}
