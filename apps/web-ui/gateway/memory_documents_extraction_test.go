package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListExtractionJobs(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"jobs":[
			{"id":"job1","project_id":"proj-1","source_type":"document","source_id":"d1","status":"completed","total_items":10,"processed_items":10,"successful_items":9,"failed_items":1,"discovered_types":["person","task"],"created_objects":["o1","o2"],"error_message":null,"started_at":"2026-08-26T10:00:00Z","completed_at":"2026-08-26T10:01:00Z","created_at":"2026-08-26T10:00:00Z","updated_at":"2026-08-26T10:01:00Z"},
			{"id":"job2","project_id":"proj-1","source_type":"document","source_id":"d1","status":"failed","total_items":5,"processed_items":3,"successful_items":2,"failed_items":1,"discovered_types":[],"created_objects":[],"error_message":"boom","started_at":"2026-08-26T11:00:00Z","completed_at":null,"created_at":"2026-08-26T11:00:00Z","updated_at":"2026-08-26T11:00:30Z"}
		],"total":2,"page":1,"limit":100,"total_pages":1}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	jobs, err := m.ListExtractionJobs(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	wantPath := "/api/admin/extraction-jobs/projects/proj-1?source_id=d1&source_type=document&limit=100"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	if jobs[0].ID != "job1" || jobs[0].Status != "completed" || jobs[0].TotalItems != 10 {
		t.Errorf("job0 = %+v", jobs[0])
	}
	if len(jobs[0].DiscoveredTypes) != 2 || jobs[0].DiscoveredTypes[0] != "person" {
		t.Errorf("job0 discovered_types = %v", jobs[0].DiscoveredTypes)
	}
	if len(jobs[0].CreatedObjects) != 2 || jobs[0].CreatedObjects[1] != "o2" {
		t.Errorf("job0 created_objects = %v", jobs[0].CreatedObjects)
	}
	if jobs[0].ErrorMessage != nil {
		t.Errorf("job0 error_message = %v, want nil", *jobs[0].ErrorMessage)
	}
	if jobs[1].ErrorMessage == nil || *jobs[1].ErrorMessage != "boom" {
		t.Errorf("job1 error_message = %v, want boom", jobs[1].ErrorMessage)
	}
	if jobs[1].CompletedAt != "" {
		t.Errorf("job1 completed_at = %q, want empty (null)", jobs[1].CompletedAt)
	}
}

func TestDownloadDocumentStreams(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = io.WriteString(w, "PDF-BYTES")
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	body, ct, err := m.DownloadDocument(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	if gotPath != "/api/documents/d1/download" {
		t.Errorf("path = %q", gotPath)
	}
	if ct != "application/pdf" {
		t.Errorf("content type = %q, want application/pdf", ct)
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "PDF-BYTES" {
		t.Errorf("body = %q, want PDF-BYTES", string(raw))
	}
}

func TestDownloadDocument404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"not_found","message":"document d1 not found"}}`, http.StatusNotFound)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, _, err := m.DownloadDocument(context.Background(), "d1"); err == nil {
		t.Fatal("want error, got nil")
	} else if !isMemoryNotFound(err) {
		t.Errorf("err = %v, want a 404 memory error", err)
	}
}

func TestGetDocumentContent(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":"hello world"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	content, err := m.GetDocumentContent(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/documents/d1/content" {
		t.Errorf("path = %q", gotPath)
	}
	if content != "hello world" {
		t.Errorf("content = %q, want hello world", content)
	}
}

func TestGetDocumentContentNull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":null}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	content, err := m.GetDocumentContent(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if content != "" {
		t.Errorf("content = %q, want empty (null)", content)
	}
}
