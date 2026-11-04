package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEditChunk(t *testing.T) {
	var gotMethod, gotPath, gotProject string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotProject = r.Method, r.URL.Path, r.Header.Get("X-Project-ID")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"chunkId":"c1","revision":2}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	res, err := m.EditChunk(context.Background(), "c1", "edited text")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/chunks/c1" {
		t.Errorf("method/path = %s %s", gotMethod, gotPath)
	}
	if gotProject != "proj-1" {
		t.Errorf("X-Project-ID = %q", gotProject)
	}
	if gotBody["text"] != "edited text" {
		t.Errorf("body = %v", gotBody)
	}
	if res.ChunkID != "c1" || res.Revision != 2 {
		t.Errorf("res = %+v", res)
	}
}

func TestListChunkRevisions(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"r1","chunkId":"c1","seq":1,"text":"orig","createdAt":"2026-10-03T00:00:00Z"},{"id":"r2","chunkId":"c1","seq":2,"text":"edit","createdAt":"2026-10-03T00:01:00Z"}],"totalCount":2}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	revs, err := m.ListChunkRevisions(context.Background(), "c1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/chunks/c1/revisions" {
		t.Errorf("path = %q", gotPath)
	}
	if len(revs) != 2 || revs[0].Seq != 1 || revs[1].Text != "edit" {
		t.Fatalf("revs = %+v", revs)
	}
}

func TestDiffChunkRevisions(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"from":1,"to":2,"lines":[{"type":"equal","text":"a"},{"type":"add","text":"b"}]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	diff, err := m.DiffChunkRevisions(context.Background(), "c1", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/chunks/c1/revisions/diff" || gotQuery != "from=1&to=2" {
		t.Errorf("path/query = %s %s", gotPath, gotQuery)
	}
	if diff.From != 1 || diff.To != 2 || len(diff.Lines) != 2 || diff.Lines[1].Type != "add" {
		t.Fatalf("diff = %+v", diff)
	}
}

func TestRollbackChunk(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"chunkId":"c1","revision":3}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	res, err := m.RollbackChunk(context.Background(), "c1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/chunks/c1/rollback" {
		t.Errorf("method/path = %s %s", gotMethod, gotPath)
	}
	if gotBody["seq"] != float64(1) {
		t.Errorf("body = %v", gotBody)
	}
	if res.Revision != 3 {
		t.Errorf("res = %+v", res)
	}
}

func TestEditChunkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"no chunk"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.EditChunk(context.Background(), "missing", "x"); err == nil {
		t.Fatal("want error, got nil")
	}
}
