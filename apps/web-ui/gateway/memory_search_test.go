package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSearchUnifiedRequest exercises the POST /api/search/unified request the
// page sends: the result types it asks for, the relationship leg it disables,
// and the optional limit/branch that are omitted when unset.
func TestSearchUnifiedRequest(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"results":[
				{"type":"text","id":"chunk-1","snippet":"a passage","score":0.71,"mode":"hybrid","document_id":"doc-1","source":"doc-1"},
				{"type":"graph","id":"obj-1","object_id":"obj-1","canonical_id":"canon-1","object_type":"Note","key":"a-note","score":0.33,"fields":{"title":"A note"}}
			],
			"metadata":{"totalResults":2,"graphResultCount":1,"textResultCount":1,"fusionStrategy":"weighted","executionTime":{"fusionMs":1,"totalMs":42}}
		}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	out, err := m.SearchUnified(context.Background(), UnifiedSearchParams{
		Query:       "passage",
		ResultTypes: "both",
		Limit:       25,
		BranchID:    "branch-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != "/api/search/unified" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/search/unified", gotMethod, gotPath)
	}
	if gotBody["query"] != "passage" {
		t.Errorf("query = %v, want passage", gotBody["query"])
	}
	if gotBody["resultTypes"] != "both" {
		t.Errorf("resultTypes = %v, want both", gotBody["resultTypes"])
	}
	if gotBody["limit"] != float64(25) {
		t.Errorf("limit = %v, want 25", gotBody["limit"])
	}
	if gotBody["branch_id"] != "branch-1" {
		t.Errorf("branch_id = %v, want branch-1", gotBody["branch_id"])
	}
	// The page renders objects and chunks only, so the relationship leg is
	// always sent as false rather than left to the server default.
	if gotBody["includeRelationships"] != false {
		t.Errorf("includeRelationships = %v, want false", gotBody["includeRelationships"])
	}

	if len(out.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(out.Results))
	}
	text := out.Results[0]
	if text.Type != "text" || text.DocumentID != "doc-1" || text.Mode != "hybrid" || text.Snippet != "a passage" {
		t.Errorf("text hit = %+v", text)
	}
	graph := out.Results[1]
	if graph.Type != "graph" || graph.CanonicalID != "canon-1" || graph.ObjectType != "Note" || graph.Key != "a-note" {
		t.Errorf("graph hit = %+v", graph)
	}
	if out.Metadata.TotalResults != 2 || out.Metadata.TextResultCount != 1 || out.Metadata.ExecutionTime.TotalMs != 42 {
		t.Errorf("metadata = %+v", out.Metadata)
	}
}

// TestSearchUnifiedOmitsUnsetOptionals pins the sparse request: an empty result
// type, zero limit and empty branch are omitted so the server applies its own
// defaults instead of receiving zero values.
func TestSearchUnifiedOmitsUnsetOptionals(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[],"metadata":{}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	if _, err := m.SearchUnified(context.Background(), UnifiedSearchParams{Query: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"resultTypes", "limit", "branch_id"} {
		if _, ok := gotBody[key]; ok {
			t.Errorf("%s present in body, want omitted: %v", key, gotBody)
		}
	}
}

// TestSearchUnifiedError surfaces a failed search as an error the page can
// render, rather than an empty successful response.
func TestSearchUnifiedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"forbidden","message":"no search scope"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	if _, err := m.SearchUnified(context.Background(), UnifiedSearchParams{Query: "x", ResultTypes: "both"}); err == nil {
		t.Fatal("expected an error for a 403 response")
	}
}
