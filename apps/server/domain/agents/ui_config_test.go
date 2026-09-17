package agents

import (
	"encoding/json"
	"testing"
)

func TestNormalizeUIConfig(t *testing.T) {
	cases := []struct {
		name string
		in   json.RawMessage
		want json.RawMessage
	}{
		{"nil is omitted", nil, nil},
		{"empty is omitted", json.RawMessage(""), nil},
		{"null becomes empty object", json.RawMessage("null"), json.RawMessage("{}")},
		{"null is case-insensitive and trimmed", json.RawMessage(" NuLl "), json.RawMessage("{}")},
		{"object unchanged", json.RawMessage(`{"icon":"bot"}`), json.RawMessage(`{"icon":"bot"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeUIConfig(tc.in)
			if string(got) != string(tc.want) {
				t.Fatalf("normalizeUIConfig(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
