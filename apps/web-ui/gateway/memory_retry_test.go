package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// roundTripFunc adapts a function to http.RoundTripper so tests can inject
// transport-level failures.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func okResponse(r *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    r,
	}
}

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

func TestDoHRetriesTransportErrorGET(t *testing.T) {
	var calls atomic.Int32
	mc := NewMemoryClient("http://memory.test", "tok", "proj")
	mc.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) < 3 {
			return nil, errors.New("connection reset by peer")
		}
		return okResponse(r, `{"ok":true}`), nil
	})

	var out struct {
		OK bool `json:"ok"`
	}
	if err := mc.do(t.Context(), http.MethodGet, "/api/x", nil, &out); err != nil {
		t.Fatalf("do() error = %v, want nil", err)
	}
	if !out.OK {
		t.Fatalf("out.OK = false, want true")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("transport calls = %d, want 3", got)
	}
}

func TestDoHTransportErrorPostFailsFast(t *testing.T) {
	var calls atomic.Int32
	mc := NewMemoryClient("http://memory.test", "tok", "proj")
	mc.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("connection reset by peer")
	})

	err := mc.do(t.Context(), http.MethodPost, "/api/x", map[string]string{"a": "b"}, nil)
	if err == nil {
		t.Fatal("do() error = nil, want non-nil")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("transport calls = %d, want 1 (non-idempotent must fail fast)", got)
	}
}

func TestDoHTransportErrorGETExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	mc := NewMemoryClient("http://memory.test", "tok", "proj")
	mc.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("eof")
	})

	err := mc.do(t.Context(), http.MethodGet, "/api/x", nil, nil)
	if err == nil {
		t.Fatal("do() error = nil, want non-nil")
	}
	if got := calls.Load(); got != maxMemoryAttempts {
		t.Fatalf("transport calls = %d, want %d", got, maxMemoryAttempts)
	}
	if got := memoryStatus(err); got != 0 {
		t.Fatalf("memoryStatus(err) = %d, want 0 for a transport error", got)
	}
}

func TestRetryBackoffDelayBounds(t *testing.T) {
	// Out-of-range attempts must not panic: <1 clamps to the first delay,
	// >len clamps to the last, and the invariant len(retryBackoff) ==
	// maxMemoryAttempts-1 keeps the slice in step with the attempt budget.
	for _, attempt := range []int{-100, -1, 0, 1, 2, 3, len(retryBackoff), len(retryBackoff) + 1, 1000} {
		d := retryBackoffDelay(attempt)
		if d < retryBackoff[0] || d > retryBackoff[len(retryBackoff)-1]+100*time.Millisecond {
			t.Fatalf("retryBackoffDelay(%d) = %v, out of range", attempt, d)
		}
	}

	// Attempt 0 clamps to the first base; attempt 1 uses the first base too.
	for range 100 {
		if d := retryBackoffDelay(0); d < 200*time.Millisecond || d >= 300*time.Millisecond {
			t.Fatalf("retryBackoffDelay(0) = %v, want [200ms,300ms)", d)
		}
		if d := retryBackoffDelay(1); d < 200*time.Millisecond || d >= 300*time.Millisecond {
			t.Fatalf("retryBackoffDelay(1) = %v, want [200ms,300ms)", d)
		}
		// Attempts at/after the slice length use the last base.
		if d := retryBackoffDelay(2); d < 600*time.Millisecond || d >= 700*time.Millisecond {
			t.Fatalf("retryBackoffDelay(2) = %v, want [600ms,700ms)", d)
		}
		if d := retryBackoffDelay(99); d < 600*time.Millisecond || d >= 700*time.Millisecond {
			t.Fatalf("retryBackoffDelay(99) = %v, want [600ms,700ms)", d)
		}
	}

	if len(retryBackoff) != maxMemoryAttempts-1 {
		t.Fatalf("len(retryBackoff) = %d, want maxMemoryAttempts-1 = %d", len(retryBackoff), maxMemoryAttempts-1)
	}
}

func TestRetryAfterDelayForms(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{name: "empty", header: "", want: 0},
		{name: "delta seconds with whitespace", header: " 2 ", want: 2 * time.Second},
		{name: "delta seconds capped", header: "99", want: 5 * time.Second},
		{name: "zero delta", header: "0", want: 0},
		{name: "negative delta", header: "-1", want: 0},
		{name: "unparseable", header: "soon", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryAfterDelay(tt.header); got != tt.want {
				t.Fatalf("retryAfterDelay(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}

	t.Run("http-date in the future is honored", func(t *testing.T) {
		header := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
		got := retryAfterDelay(header)
		if got <= 0 || got > 5*time.Second {
			t.Fatalf("retryAfterDelay(%q) = %v, want (0s,5s]", header, got)
		}
	})

	t.Run("http-date in the past falls back", func(t *testing.T) {
		header := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
		if got := retryAfterDelay(header); got != 0 {
			t.Fatalf("retryAfterDelay(%q) = %v, want 0", header, got)
		}
	})
}

// TestDoOnceFreshReaderPerAttempt asserts doOnce builds a new reader for every
// call, so a payload is never truncated on a second attempt.
func TestDoOnceFreshReaderPerAttempt(t *testing.T) {
	var (
		calls atomic.Int32
		got   atomic.Value // string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		got.Store(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mc := NewMemoryClient(srv.URL, "tok", "proj")
	body := []byte(`{"a":"b"}`)
	// Two attempts' worth of doOnce calls must each send the full payload.
	for range 2 {
		if _, _, _, err := mc.doOnce(t.Context(), http.MethodPut, "/api/x", body, true, nil); err != nil {
			t.Fatalf("doOnce: %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("requests = %d, want 2", calls.Load())
	}
	if s, _ := got.Load().(string); s != string(body) {
		t.Fatalf("last body = %q, want %q", s, string(body))
	}
}

// TestDoHBodySentInFull asserts a body-carrying request through doH still sends
// the complete payload (no body == nil retry path).
func TestDoHBodySentInFull(t *testing.T) {
	var got atomic.Value // string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.Store(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mc := NewMemoryClient(srv.URL, "tok", "proj")
	if err := mc.do(t.Context(), http.MethodPost, "/api/x", map[string]string{"a": "b"}, nil); err != nil {
		t.Fatalf("do() error = %v, want nil", err)
	}
	if s, _ := got.Load().(string); s != `{"a":"b"}` {
		t.Fatalf("body = %q, want %q", s, `{"a":"b"}`)
	}
}
