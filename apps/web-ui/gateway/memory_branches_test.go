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
