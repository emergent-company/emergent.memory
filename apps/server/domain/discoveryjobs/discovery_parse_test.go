package discoveryjobs

import (
	"errors"
	"io"
	"log/slog"
	"testing"
)

func testService() *Service {
	return &Service{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// TestParseTypeDiscoveryResponse_RepairsTruncatedJSON covers the failure mode
// that surfaced as "failed to parse LLM response: unexpected EOF": the model
// hit its output limit mid-object. The repaired prefix is still useful.
func TestParseTypeDiscoveryResponse_RepairsTruncatedJSON(t *testing.T) {
	raw := `{"discovered_types":[` +
		`{"type_name":"Person","description":"A person","confidence":0.9},` +
		`{"type_name":"Place","description":"A pla`

	types, truncated, err := testService().parseTypeDiscoveryResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !truncated {
		t.Fatal("truncated response not flagged")
	}
	if len(types) == 0 {
		t.Fatal("repaired response produced no types; expected at least the complete first type")
	}
	if types[0].TypeName != "Person" {
		t.Fatalf("first type = %q, want Person", types[0].TypeName)
	}
}

func TestParseTypeDiscoveryResponse_RepairsLiteralNewline(t *testing.T) {
	raw := "{\"discovered_types\":[{\"type_name\":\"Person\",\"description\":\"line one\nline two\",\"confidence\":0.9}]}"

	types, truncated, err := testService().parseTypeDiscoveryResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if truncated {
		t.Fatal("complete response flagged as truncated")
	}
	if len(types) != 1 || types[0].TypeName != "Person" {
		t.Fatalf("types = %+v, want one Person", types)
	}
}

func TestParseTypeDiscoveryResponse_UnwrapsJSONSchemaObject(t *testing.T) {
	// Regression for #234: a model that answers with a complete JSON Schema
	// object must not produce fields literally named after the schema's own
	// keys, and its `required` list must reach RequiredProperties.
	raw := `{"discovered_types":[{"type_name":"Episode","description":"A podcast episode",` +
		`"inferred_schema":{"type":"object","properties":{` +
		`"title":{"type":"string"},` +
		`"guest":{"type":"string","description":"reference to Guest"}},` +
		`"required":["title","guest"]},` +
		`"confidence":0.9,"occurrences":3}]}`

	types, _, err := testService().parseTypeDiscoveryResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(types) != 1 {
		t.Fatalf("types = %d, want 1", len(types))
	}

	got := types[0]
	for _, leaked := range []string{"type", "properties", "required"} {
		if _, present := got.Properties[leaked]; present {
			t.Errorf("schema key %q leaked into properties: %#v", leaked, got.Properties)
		}
	}
	if got.Properties["title"] == nil || got.Properties["guest"] == nil {
		t.Errorf("properties = %#v, want title and guest", got.Properties)
	}
	if len(got.RequiredProperties) != 2 || got.RequiredProperties[0] != "title" || got.RequiredProperties[1] != "guest" {
		t.Errorf("required = %v, want [title guest]", got.RequiredProperties)
	}
}

// TestParseTypeDiscoveryResponse_TableColumnsNotWrapperKeywords is the direct
// regression for #234: a DOCX table (Date, Guest, Title, Format, Min) whose
// answer states the column map under `properties` but omits the enclosing
// `type`/`required` markers. The columns are the fields; the wrapper key is not.
func TestParseTypeDiscoveryResponse_TableColumnsNotWrapperKeywords(t *testing.T) {
	raw := `{"discovered_types":[{"type_name":"PodcastEpisode",` +
		`"description":"One row of the episode table",` +
		`"inferred_schema":{"properties":{` +
		`"Date":{"type":"string"},` +
		`"Guest":{"type":"string","description":"reference to Guest"},` +
		`"Title":{"type":"string"},` +
		`"Format":{"type":"string"},` +
		`"Min":{"type":"number"}}},` +
		`"confidence":0.9,"occurrences":12}]}`

	types, _, err := testService().parseTypeDiscoveryResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(types) != 1 {
		t.Fatalf("types = %d, want 1", len(types))
	}

	got := types[0]
	want := []string{"Date", "Guest", "Title", "Format", "Min"}
	for _, column := range want {
		if _, present := got.Properties[column]; !present {
			t.Errorf("table column %q missing from properties: %#v", column, got.Properties)
		}
	}
	for _, leaked := range []string{"properties", "required", "type", "items"} {
		if _, present := got.Properties[leaked]; present {
			t.Errorf("schema keyword %q leaked into properties: %#v", leaked, got.Properties)
		}
	}
	if len(got.Properties) != len(want) {
		t.Errorf("properties = %#v, want exactly the table columns", got.Properties)
	}
}

func TestParseTypeDiscoveryResponse_FlatSchemaUnchanged(t *testing.T) {
	raw := `{"discovered_types":[{"type_name":"Guest","description":"A guest",` +
		`"inferred_schema":{"name":{"type":"string"},"bio":{"type":"string"}},` +
		`"confidence":0.8,"occurrences":1}]}`

	types, _, err := testService().parseTypeDiscoveryResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(types) != 1 {
		t.Fatalf("types = %d, want 1", len(types))
	}
	if types[0].Properties["name"] == nil || types[0].Properties["bio"] == nil {
		t.Errorf("properties = %#v, want name and bio", types[0].Properties)
	}
}

func TestDiscoveryRetrySmaller(t *testing.T) {
	cases := []struct {
		name      string
		callErr   error
		docCount  int
		typeCount int
		want      bool
	}{
		{"truncated multi-doc empty", ErrOutputTruncated, 2, 0, true},
		{"truncated single-doc empty", ErrOutputTruncated, 1, 0, false},
		{"truncated multi-doc with types", ErrOutputTruncated, 2, 3, false},
		{"clean multi-doc empty", nil, 2, 0, false},
		{"other error multi-doc empty", errors.New("parse"), 2, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := discoveryRetrySmaller(tc.callErr, tc.docCount, tc.typeCount); got != tc.want {
				t.Fatalf("discoveryRetrySmaller = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDecideDiscoveryBatch(t *testing.T) {
	cases := []struct {
		name      string
		response  string
		callErr   error
		docCount  int
		wantTypes int
		wantRetry bool
		wantErr   bool
	}{
		{
			name:     "complete empty answer is a valid batch",
			response: `{"discovered_types":[]}`,
			docCount: 3,
		},
		{
			name:      "truncated empty answer splits",
			response:  `{"discovered_types":[`,
			callErr:   ErrOutputTruncated,
			docCount:  3,
			wantRetry: true,
			wantErr:   true,
		},
		{
			name:     "truncated empty single document fails",
			response: `{"discovered_types":[`,
			callErr:  ErrOutputTruncated,
			docCount: 1,
			wantErr:  true,
		},
		{
			name:      "truncated answer with a complete prefix is accepted",
			response:  `{"discovered_types":[{"type_name":"Person","confidence":0.9},`,
			callErr:   ErrOutputTruncated,
			docCount:  3,
			wantTypes: 1,
		},
		{
			name:      "empty body splits",
			response:  "",
			docCount:  3,
			wantRetry: true,
			wantErr:   true,
		},
		{
			name:     "malformed non-truncated answer fails without split",
			response: "not json",
			docCount: 3,
			wantErr:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := testService().decideDiscoveryBatch(tc.response, tc.callErr, tc.docCount)
			if len(got.Types) != tc.wantTypes {
				t.Fatalf("types = %d, want %d", len(got.Types), tc.wantTypes)
			}
			if got.RetrySmaller != tc.wantRetry {
				t.Fatalf("RetrySmaller = %v, want %v", got.RetrySmaller, tc.wantRetry)
			}
			if (got.Err != nil) != tc.wantErr {
				t.Fatalf("Err = %v, wantErr %v", got.Err, tc.wantErr)
			}
		})
	}
}
