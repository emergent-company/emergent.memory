package chat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/sse"
)

// guardedWriter is a mutex-guarded http.ResponseWriter + http.Flusher stub that
// records writes so tests can inspect frames without racing the keepalive
// goroutine (httptest.ResponseRecorder is not safe for concurrent use).
type guardedWriter struct {
	mu       sync.Mutex
	body     []byte
	header   http.Header
	status   int
	flushed  bool
	writeErr error
}

func newGuardedWriter() *guardedWriter {
	return &guardedWriter{header: http.Header{}}
}

func (w *guardedWriter) Header() http.Header { return w.header }

func (w *guardedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	w.body = append(w.body, p...)
	return len(p), nil
}

func (w *guardedWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = code
}

func (w *guardedWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushed = true
}

func (w *guardedWriter) bodyString() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.body)
}

func (w *guardedWriter) pingCount() int {
	return strings.Count(w.bodyString(), ": ping\n\n")
}

func TestStartKeepaliveEmitsPing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stub := newGuardedWriter()
	w := sse.NewWriter(stub)
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stop := startKeepalive(ctx, w, 5*time.Millisecond, nil)
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for stub.pingCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if stub.pingCount() == 0 {
		t.Fatal("no : ping frame emitted")
	}
}

func TestStartKeepaliveStopJoins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stub := newGuardedWriter()
	w := sse.NewWriter(stub)
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stop := startKeepalive(ctx, w, 5*time.Millisecond, nil)

	deadline := time.Now().Add(2 * time.Second)
	for stub.pingCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if stub.pingCount() == 0 {
		stop()
		t.Fatal("no : ping frame emitted")
	}

	stop() // cancels and joins the goroutine
	after := stub.pingCount()
	time.Sleep(50 * time.Millisecond)
	if got := stub.pingCount(); got != after {
		t.Fatalf("frame written after stop: %d -> %d", after, got)
	}
}

func TestStartKeepaliveWriteErrorStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stub := newGuardedWriter()
	stub.writeErr = errors.New("boom")
	w := sse.NewWriter(stub)
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stop := startKeepalive(ctx, w, 5*time.Millisecond, nil)

	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
		// stop returned promptly because the goroutine exited on the write
		// error (or the cancel raced ahead of it).
	case <-time.After(2 * time.Second):
		t.Fatal("stop func did not return promptly after write error")
	}
}
