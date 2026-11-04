package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/google/uuid"
)

// decodeEnvelope parses the JSON text of an envelope ToolResult into a map.
func decodeEnvelope(t *testing.T, res *ToolResult, err error) map[string]any {
	t.Helper()
	if err != nil {
		t.Fatalf("envelopeResult returned error: %v", err)
	}
	if res == nil {
		t.Fatal("envelopeResult returned nil result")
	}
	if len(res.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(res.Content))
	}
	if res.Content[0].Type != "text" {
		t.Fatalf("Content[0].Type = %q, want \"text\"", res.Content[0].Type)
	}
	if strings.Contains(res.Content[0].Text, "\n") {
		t.Errorf("envelope text must be compact single-line JSON, got newline: %q", res.Content[0].Text)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &m); err != nil {
		t.Fatalf("envelope text is not valid JSON (%v): %q", err, res.Content[0].Text)
	}
	return m
}

func TestEnvelopeResult_OKTrueOmitsErrorAndEmptyMeta(t *testing.T) {
	res, err := envelopeResult(true, map[string]any{"entity_id": "abc"}, nil, "")
	m := decodeEnvelope(t, res, err)

	okVal, present := m["ok"]
	if !present {
		t.Fatal("envelope must carry top-level ok")
	}
	ok, isBool := okVal.(bool)
	if !isBool {
		t.Fatalf("ok = %v (%T), want bool", okVal, okVal)
	}
	if !ok {
		t.Error("ok = false, want true")
	}
	if _, present := m["error"]; present {
		t.Error("ok=true envelope must omit the error field")
	}
	if _, present := m["meta"]; present {
		t.Error("empty meta must be omitted")
	}
	data, present := m["data"]
	if !present {
		t.Fatal("envelope must carry data")
	}
	dm, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want map[string]any", data)
	}
	if dm["entity_id"] != "abc" {
		t.Errorf("data.entity_id = %v, want abc", dm["entity_id"])
	}
}

func TestEnvelopeResult_OKFalseIncludesError(t *testing.T) {
	res, err := envelopeResult(false, map[string]any{"results": []any{}}, nil, "2 of 3 items failed")
	m := decodeEnvelope(t, res, err)

	okVal, present := m["ok"]
	if !present {
		t.Fatal("envelope must carry top-level ok")
	}
	ok, isBool := okVal.(bool)
	if !isBool {
		t.Fatalf("ok = %v (%T), want bool", okVal, okVal)
	}
	if ok {
		t.Error("ok = true, want false for failed envelope")
	}
	errMsg, present := m["error"]
	if !present {
		t.Fatal("ok=false envelope must carry the error field")
	}
	if errMsg != "2 of 3 items failed" {
		t.Errorf("error = %v, want %q", errMsg, "2 of 3 items failed")
	}
}

func TestEnvelopeResult_NonEmptyMetaPresent(t *testing.T) {
	res, err := envelopeResult(true, map[string]any{"results": []any{}},
		map[string]any{"created": 2, "failed": 1, "total": 3}, "")
	m := decodeEnvelope(t, res, err)

	meta, present := m["meta"]
	if !present {
		t.Fatal("non-empty meta must be present")
	}
	mm, ok := meta.(map[string]any)
	if !ok {
		t.Fatalf("meta = %T, want map[string]any", meta)
	}
	if mm["created"] != float64(2) {
		t.Errorf("meta.created = %v, want 2", mm["created"])
	}
	if mm["failed"] != float64(1) {
		t.Errorf("meta.failed = %v, want 1", mm["failed"])
	}
	if mm["total"] != float64(3) {
		t.Errorf("meta.total = %v, want 3", mm["total"])
	}
}

// searchResponseFixture builds a minimal graph search response.
func searchResponseFixture(meta *graph.SearchResponseMeta) *graph.SearchResponse {
	return &graph.SearchResponse{
		Data: []*graph.SearchResultItem{
			{
				Object: &graph.GraphObjectResponse{CanonicalID: uuid.New(), Type: "Note"},
				Score:  0.9,
			},
		},
		Total:   1,
		HasMore: false,
		Meta:    meta,
	}
}

func TestEnvelopeSearchResponse_SlimPayloadUnderDataNoMeta(t *testing.T) {
	res := envelopeResultSafe(t, searchResponseFixture(nil), ResponseOpts{})
	data, ok := res["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want map[string]any", res["data"])
	}
	items, _ := data["data"].([]any)
	if len(items) != 1 {
		t.Errorf("data.data length = %d, want 1", len(items))
	}
	if data["total"] != float64(1) {
		t.Errorf("data.total = %v, want 1", data["total"])
	}
	if data["has_more"] != false {
		t.Errorf("data.has_more = %v, want false", data["has_more"])
	}
	if _, present := res["meta"]; present {
		t.Error("slim (non-verbose) search result must omit envelope meta")
	}
	item, _ := items[0].(map[string]any)
	if _, hasScore := item["score"]; !hasScore {
		t.Error("per-item score must be a sibling of object under data.data")
	}
}

func TestEnvelopeSearchResponse_VerboseMetaMovesToEnvelopeMeta(t *testing.T) {
	res := envelopeResultSafe(t, searchResponseFixture(&graph.SearchResponseMeta{ElapsedMs: 12.5}),
		ResponseOpts{Verbose: true})
	meta, ok := res["meta"].(map[string]any)
	if !ok {
		t.Fatalf("verbose search must carry envelope meta, got %T", res["meta"])
	}
	if meta["elapsed_ms"] != float64(12.5) {
		t.Errorf("meta.elapsed_ms = %v, want 12.5", meta["elapsed_ms"])
	}
	data, _ := res["data"].(map[string]any)
	if _, nested := data["meta"]; nested {
		t.Error("meta must not remain nested inside data")
	}
}

// envelopeResultSafe runs envelopeSearchResponse and decodes the result text.
func envelopeResultSafe(t *testing.T, res *graph.SearchResponse, opts ResponseOpts) map[string]any {
	t.Helper()
	result, err := envelopeSearchResponse(res, opts)
	return decodeEnvelope(t, result, err)
}
