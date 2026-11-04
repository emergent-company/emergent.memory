package llmjson

import (
	"testing"
)

type entities struct {
	Entities []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Note string `json:"note"`
	} `json:"entities"`
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name: "plain json",
			raw:  `{"entities":[{"name":"Ada","type":"Person"}]}`,
			want: "Ada",
		},
		{
			name: "markdown fence",
			raw:  "```json\n{\"entities\":[{\"name\":\"Ada\",\"type\":\"Person\"}]}\n```",
			want: "Ada",
		},
		{
			name: "prose preamble and trailing note",
			raw:  `Here is the result: {"entities":[{"name":"Ada","type":"Person"}]} — hope that helps.`,
			want: "Ada",
		},
		{
			name: "literal newline inside string",
			raw:  "{\"entities\":[{\"name\":\"Ada\",\"type\":\"Person\",\"note\":\"line one\nline two\"}]}",
			want: "Ada",
		},
		{
			name: "parenthetical after a value",
			raw:  `{"entities":[{"name":"Ada" (estimated),"type":"Person"}]}`,
			want: "Ada",
		},
		{
			name: "stray closing bracket",
			raw:  `{"entities":[{"name":"Ada","type":"Person"}]}]}`,
			want: "Ada",
		},
		{
			name: "truncated mid array",
			raw:  `{"entities":[{"name":"Ada","type":"Person"}`,
			want: "Ada",
		},
		{
			name: "truncated mid string",
			raw:  `{"entities":[{"name":"Ada","type":"Per`,
			want: "Ada",
		},
		{
			name:    "unrecoverable",
			raw:     `not json at all`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got entities
			err := Decode(tc.raw, &got)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(got.Entities) != 1 || got.Entities[0].Name != tc.want {
				t.Fatalf("decoded = %+v, want one entity named %q", got, tc.want)
			}
		})
	}
}

func TestDecodeWithMeta(t *testing.T) {
	var got entities

	repaired, truncated, err := DecodeWithMeta(`{"entities":[{"name":"Ada","type":"Per`, &got)
	if err != nil {
		t.Fatalf("decode truncated: %v", err)
	}
	if !repaired || !truncated {
		t.Fatalf("repaired=%v truncated=%v, want both true", repaired, truncated)
	}
	if len(got.Entities) != 1 || got.Entities[0].Name != "Ada" {
		t.Fatalf("entities = %+v, want Ada", got.Entities)
	}

	repaired, truncated, err = DecodeWithMeta(`{"entities":[]}`, &got)
	if err != nil {
		t.Fatalf("decode clean: %v", err)
	}
	if repaired || truncated {
		t.Fatalf("clean response repaired=%v truncated=%v, want both false", repaired, truncated)
	}

	repaired, truncated, err = DecodeWithMeta("```json\n{\"entities\":[]}\n```", &got)
	if err != nil {
		t.Fatalf("decode fenced: %v", err)
	}
	if !repaired || truncated {
		t.Fatalf("fenced response repaired=%v truncated=%v, want repaired true/truncated false", repaired, truncated)
	}
}

func TestDecodeTruncationErrorIsClassifiable(t *testing.T) {
	var got map[string]any
	if err := Decode(`{"a": tru`, &got); err == nil {
		t.Fatal("expected error")
	}
}
