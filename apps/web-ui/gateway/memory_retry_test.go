package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDoHRetriesTransientGET(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n := count.Add(1); n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	mc := NewMemoryClient(srv.URL, "tok", "proj")
	var out struct {
		OK bool `json:"ok"`
	}
	if err := mc.do(t.Context(), http.MethodGet, "/api/x", nil, &out); err != nil {
		t.Fatalf("do() error = %v, want nil", err)
	}
	if !out.OK {
		t.Fatalf("out.OK = false, want true")
	}
	if got := count.Load(); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}

func TestDoHGETExhaustsRetries(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	mc := NewMemoryClient(srv.URL, "tok", "proj")
	err := mc.do(t.Context(), http.MethodGet, "/api/x", nil, nil)
	if err == nil {
		t.Fatal("do() error = nil, want non-nil")
	}
	// memoryStatus must resolve through the memoryAttemptError retry wrapper.
	if got := memoryStatus(err); got != http.StatusBadGateway {
		t.Fatalf("memoryStatus(err) = %d, want %d", got, http.StatusBadGateway)
	}
	if got := count.Load(); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}

func TestDoHGETDoesNotRetryClientError(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"bad_request","message":"nope"}}`))
	}))
	defer srv.Close()

	mc := NewMemoryClient(srv.URL, "tok", "proj")
	err := mc.do(t.Context(), http.MethodGet, "/api/x", nil, nil)
	if err == nil {
		t.Fatal("do() error = nil, want non-nil")
	}
	if got := memoryStatus(err); got != http.StatusBadRequest {
		t.Fatalf("memoryStatus(err) = %d, want %d", got, http.StatusBadRequest)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestDoHPostDoesNotRetry(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	mc := NewMemoryClient(srv.URL, "tok", "proj")
	err := mc.do(t.Context(), http.MethodPost, "/api/x", map[string]string{"a": "b"}, nil)
	if err == nil {
		t.Fatal("do() error = nil, want non-nil")
	}
	if got := memoryStatus(err); got != http.StatusBadGateway {
		t.Fatalf("memoryStatus(err) = %d, want %d", got, http.StatusBadGateway)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 (non-idempotent must not retry)", got)
	}
}

func TestDoHHeadRetriesTransient(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n := count.Add(1); n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mc := NewMemoryClient(srv.URL, "tok", "proj")
	if err := mc.do(t.Context(), http.MethodHead, "/api/x", nil, nil); err != nil {
		t.Fatalf("do() error = %v, want nil", err)
	}
	if got := count.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}
