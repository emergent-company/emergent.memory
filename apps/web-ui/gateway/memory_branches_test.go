package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetMergeReadiness(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ready":false,"total_objects":12,"pending_embeddings":3}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	res, err := m.GetMergeReadiness(context.Background(), "b1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/graph/branches/b1/merge-readiness" {
		t.Errorf("path = %q", gotPath)
	}
	if res.Ready || res.TotalObjects != 12 || res.PendingEmbeddings != 3 {
		t.Errorf("readiness = %+v", res)
	}
}

func TestMergeBranch(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"total_objects":10,"added_count":5,"fast_forward_count":2,"conflict_count":1,"similar_count":1,"left_for_review_count":2,"merged_count":7}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	res, err := m.MergeBranch(context.Background(), "b1", "partial")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/graph/branches/main/merge" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["source_branch_id"] != "b1" {
		t.Errorf("source_branch_id = %v, want b1", gotBody["source_branch_id"])
	}
	if gotBody["execute"] != true {
		t.Errorf("execute = %v, want true", gotBody["execute"])
	}
	if gotBody["policy"] != "partial" {
		t.Errorf("policy = %v, want partial", gotBody["policy"])
	}
	if res.AddedCount != 5 || res.FastForwardCount != 2 || res.LeftForReviewCount != 2 || res.MergedCount != 7 {
		t.Errorf("result = %+v", res)
	}
}

// TestMergeBranchLeftForReviewObjects pins the per-object breakdown decode: the
// merge response's objects[] carries status/resolution/conflicts and the
// similar target, and LeftForReviewObjects filters it to the entries a partial
// merge left for review ("skipped" conflicts and "suggested" similarity matches).
func TestMergeBranchLeftForReviewObjects(t *testing.T) {
	const body = `{
		"total_objects": 4,
		"added_count": 1,
		"conflict_count": 1,
		"similar_count": 1,
		"left_for_review_count": 2,
		"objects": [
			{"canonical_id":"11111111-1111-1111-1111-111111111111","status":"added","resolution":"enriched"},
			{"canonical_id":"22222222-2222-2222-2222-222222222222","status":"conflict","resolution":"skipped","conflicts":["name","status"]},
			{"canonical_id":"33333333-3333-3333-3333-333333333333","status":"similar","resolution":"suggested","similarity_score":0.9,"similar_target_id":"44444444-4444-4444-4444-444444444444","similar_target_name":"Sam Altman"}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	res, err := m.MergeBranch(context.Background(), "b1", "partial")
	if err != nil {
		t.Fatal(err)
	}

	left := res.LeftForReviewObjects()
	if len(left) != 2 {
		t.Fatalf("left-for-review = %d entries, want 2: %+v", len(left), left)
	}
	if left[0].CanonicalID != "22222222-2222-2222-2222-222222222222" || left[0].Resolution != "skipped" {
		t.Errorf("conflict entry = %+v", left[0])
	}
	if len(left[0].Conflicts) != 2 || left[0].Conflicts[0] != "name" {
		t.Errorf("conflict keys = %v", left[0].Conflicts)
	}
	if left[1].Resolution != "suggested" || left[1].SimilarTargetName != "Sam Altman" || left[1].SimilarTargetID != "44444444-4444-4444-4444-444444444444" {
		t.Errorf("similar entry = %+v", left[1])
	}
}

func TestMergeBranchLeftForReviewObjectsNilSafe(t *testing.T) {
	var res *BranchMergeResult
	if got := res.LeftForReviewObjects(); got != nil {
		t.Errorf("nil result must yield nil, got %+v", got)
	}
	empty := &BranchMergeResult{}
	if got := empty.LeftForReviewObjects(); got != nil {
		t.Errorf("empty breakdown must yield nil, got %+v", got)
	}
}
