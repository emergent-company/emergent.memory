package discoveryjobs

import (
	"encoding/json"
	"testing"
)

// documentID is a fixed UUID used by the retry-config tests.
const (
	retryDocA = "11111111-1111-1111-1111-111111111111"
	retryDocB = "22222222-2222-2222-2222-222222222222"
)

// A job config read back from jsonb decodes numbers as float64 and lists as
// []any. A retry must reproduce the original run's settings from that shape.
func TestStartRequestFromConfigJSONShape(t *testing.T) {
	raw := `{
		"document_ids": ["` + retryDocA + `", "` + retryDocB + `"],
		"batch_size": 5,
		"min_confidence": 0.5,
		"include_relationships": true,
		"max_iterations": 3
	}`
	var config JSONMap
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	req, err := startRequestFromConfig(config)
	if err != nil {
		t.Fatalf("startRequestFromConfig: %v", err)
	}
	if len(req.DocumentIDs) != 2 {
		t.Fatalf("document ids = %d, want 2", len(req.DocumentIDs))
	}
	if got := req.DocumentIDs[0].String(); got != retryDocA {
		t.Fatalf("first document id = %s, want %s", got, retryDocA)
	}
	if req.BatchSize != 5 || req.MinConfidence != 0.5 || req.MaxIterations != 3 {
		t.Fatalf("settings = batch %d, confidence %v, iterations %d; want 5, 0.5, 3",
			req.BatchSize, req.MinConfidence, req.MaxIterations)
	}
	if !req.IncludeRelationships {
		t.Fatal("include_relationships = false, want true")
	}
}

// An in-memory config (the shape StartDiscovery builds before persisting) holds
// a []string, not []any. Both must be readable.
func TestStartRequestFromConfigStringSlice(t *testing.T) {
	req, err := startRequestFromConfig(JSONMap{
		"document_ids": []string{retryDocA},
		"batch_size":   2,
	})
	if err != nil {
		t.Fatalf("startRequestFromConfig: %v", err)
	}
	if len(req.DocumentIDs) != 1 || req.BatchSize != 2 {
		t.Fatalf("request = %+v, want one document and batch size 2", req)
	}
}

// A retry that cannot reconstruct the run's inputs must fail loudly: starting a
// job with an empty document set would fail again for a different reason.
func TestStartRequestFromConfigRejectsMissingDocuments(t *testing.T) {
	for name, config := range map[string]JSONMap{
		"no document_ids": {},
		"empty list":      {"document_ids": []any{}},
		"bad id":          {"document_ids": []any{"not-a-uuid"}},
		"non-string id":   {"document_ids": []any{42}},
	} {
		if _, err := startRequestFromConfig(config); err == nil {
			t.Fatalf("%s: err = nil, want an error", name)
		}
	}
}
