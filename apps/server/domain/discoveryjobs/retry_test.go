package discoveryjobs

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestIsTransientLLMError pins the classification boundary the retry loop rests
// on: network faults and provider 429/5xx retry; auth/invalid-request, output
// truncation and cancellation do not; ambiguous errors default to transient.
func TestIsTransientLLMError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"eof", io.EOF, true},
		{"wrapped eof", &urlErr{err: io.EOF}, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"cancelled", context.Canceled, false},
		{"truncated", ErrOutputTruncated, false},
		{"provider 429", errors.New("openai-compatible: endpoint returned 429: rate limited"), true},
		{"provider 500", errors.New("anthropic: endpoint returned 500 (internal)"), true},
		{"provider 503", errors.New("API error 503: service unavailable"), true},
		{"provider 401", errors.New("endpoint returned 401: unauthorized"), false},
		{"provider 400", errors.New("endpoint returned 400: bad request"), false},
		{"ambiguous", errors.New("something went wrong"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientLLMError(tc.err); got != tc.want {
				t.Fatalf("isTransientLLMError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// urlErr is a minimal error wrapper so the EOF test can exercise the errors.Is
// path through a wrap (as *url.Error does for "Post ...: EOF").
type urlErr struct{ err error }

func (e *urlErr) Error() string { return "Post \"...\": " + e.err.Error() }
func (e *urlErr) Unwrap() error { return e.err }

// TestDiscoverBatchTypes_TransientThenSuccess pins the recovery: a transient
// network fault on the first call is retried and the batch succeeds with the
// discovered type.
func TestDiscoverBatchTypes_TransientThenSuccess(t *testing.T) {
	llm := &sequenceLLM{responses: []seqResponse{
		{err: io.EOF},
		{text: completeAnswer},
	}}
	svc := newBatchTestService(llm)

	groups, err := svc.discoverBatchTypes(context.Background(), testDocs(1), 1, "test purpose", 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if types := allGroupTypes(groups); len(types) != 1 || types[0].TypeName != "Customer" {
		t.Fatalf("types = %+v, want one Customer", types)
	}
	if llm.calls != 2 {
		t.Fatalf("model calls = %d, want 2 (transient fault, then success)", llm.calls)
	}
}

// TestDiscoverBatchTypes_PermanentErrorFailsFast pins the other side: a
// permanent error (auth/invalid request) is not retried, so the batch fails
// after a single call instead of burning the full backoff.
func TestDiscoverBatchTypes_PermanentErrorFailsFast(t *testing.T) {
	llm := &sequenceLLM{responses: []seqResponse{
		{err: errors.New("openai-compatible: endpoint returned 401: unauthorized")},
	}}
	svc := newBatchTestService(llm)

	_, err := svc.discoverBatchTypes(context.Background(), testDocs(1), 1, "test purpose", 0, nil)
	if err == nil {
		t.Fatal("expected an error for a permanent failure")
	}
	if llm.calls != 1 {
		t.Fatalf("model calls = %d, want 1 (permanent failure fails fast)", llm.calls)
	}
}

// TestDiscoverBatchTypes_TransientExhaustedFails pins the bounded retry: after
// every attempt fails with a transient fault the batch fails with an error that
// names the exhaustion, not silently the last gateway fault.
func TestDiscoverBatchTypes_TransientExhaustedFails(t *testing.T) {
	llm := &sequenceLLM{responses: []seqResponse{
		{err: io.EOF},
		{err: io.EOF},
		{err: io.EOF},
	}}
	svc := newBatchTestService(llm)

	_, err := svc.discoverBatchTypes(context.Background(), testDocs(1), 1, "test purpose", 0, nil)
	if err == nil {
		t.Fatal("expected an error after transient retries are exhausted")
	}
	if !strings.Contains(err.Error(), "attempts") {
		t.Fatalf("error = %v, want it to name the exhausted attempts", err)
	}
	if llm.calls != discoveryLLMMaxAttempts {
		t.Fatalf("model calls = %d, want %d", llm.calls, discoveryLLMMaxAttempts)
	}
}

// TestDiscoverBatchTypes_EmptyBlipRecovers pins the empty-result retry: a single
// empty answer (a model blip) is re-asked and the following real answer wins.
func TestDiscoverBatchTypes_EmptyBlipRecovers(t *testing.T) {
	llm := &sequenceLLM{responses: []seqResponse{
		{text: `{"discovered_types":[]}`},
		{text: completeAnswer},
	}}
	svc := newBatchTestService(llm)

	groups, err := svc.discoverBatchTypes(context.Background(), testDocs(1), 1, "test purpose", 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if types := allGroupTypes(groups); len(types) != 1 || types[0].TypeName != "Customer" {
		t.Fatalf("types = %+v, want one Customer", types)
	}
	if llm.calls != 2 {
		t.Fatalf("model calls = %d, want 2 (empty blip, then success)", llm.calls)
	}
}
