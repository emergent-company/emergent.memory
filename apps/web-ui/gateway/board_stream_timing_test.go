package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The live board's coalescing window and heartbeat tick are both timing
// behaviours, which is why they went untested: #2459 left them unguarded, so a
// regression that rendered once per event (a push() inside the frame loop) or
// stopped writing the heartbeat would pass the suite. Both are cheap to make
// deterministic — boardStreamCoalesce and boardStreamHeartbeat are package-level
// vars precisely so a test can shrink them (see board_stream.go).
//
// These run against the real handler over a real (in-memory) ResponseWriter, so
// the assertion is about what a client receives on the wire, not about a helper.

// setBoardStreamTiming shrinks the stream's two intervals for one test.
func setBoardStreamTiming(t *testing.T, window, heartbeat time.Duration) {
	t.Helper()
	prevWindow, prevHeartbeat := boardStreamCoalesce, boardStreamHeartbeat
	boardStreamCoalesce, boardStreamHeartbeat = window, heartbeat
	t.Cleanup(func() {
		boardStreamCoalesce, boardStreamHeartbeat = prevWindow, prevHeartbeat
	})
}

// streamRecorder is an http.ResponseWriter a streaming handler can keep writing
// to while the test reads what has arrived so far. httptest.ResponseRecorder
// offers no safe concurrent access, and the handler's writes come from its own
// goroutine.
type streamRecorder struct {
	header http.Header
	mu     sync.Mutex
	buf    bytes.Buffer
}

func newStreamRecorder() *streamRecorder {
	return &streamRecorder{header: make(http.Header)}
}

func (r *streamRecorder) Header() http.Header { return r.header }

func (r *streamRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *streamRecorder) WriteHeader(int) {}

// Flush satisfies http.Flusher so writeSSEPartial's per-frame flush reaches a
// writer it recognises.
func (r *streamRecorder) Flush() {}

func (r *streamRecorder) body() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// startBoardStream runs GET /board/stream on an echo instance backed by f and
// returns the recording writer plus a stop func. The request context is the
// stream's only exit, so cancel is what ends it.
func startBoardStream(t *testing.T, f MemoryBackend) (*streamRecorder, func()) {
	t.Helper()
	_, e := newBoardEcho(f)
	rec := newStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/board/stream", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.ServeHTTP(rec, req)
	}()
	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("board stream did not stop after its context was cancelled")
		}
	}
	return rec, stop
}

// frameCount counts the <hx-partial> directives addressed at one region. The
// target is matched with its closing quote so "#board" does not also count
// "#board-header" or "#board-health".
func frameCount(body, target string) int {
	return strings.Count(body, `hx-target="`+target+`"`)
}

func waitForFrames(t *testing.T, rec *streamRecorder, target string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		body := rec.body()
		if frameCount(body, target) >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d %s frame(s); got:\n%s", want, target, body)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForOccurrences(t *testing.T, rec *streamRecorder, needle string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		body := rec.body()
		if strings.Count(body, needle) >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q ×%d; got:\n%s", needle, want, body)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// writeUpstreamEvents writes alternating event/data pairs as upstream SSE
// frames. Both lines of a pair are written together so a burst really lands in
// one coalescing window.
func writeUpstreamEvents(t *testing.T, w io.Writer, pairs ...string) {
	t.Helper()
	for i := 0; i+1 < len(pairs); i += 2 {
		frame := "event: " + pairs[i] + "\ndata: " + pairs[i+1] + "\n\n"
		if _, err := io.WriteString(w, frame); err != nil {
			t.Fatalf("write upstream event %s: %v", pairs[i], err)
		}
	}
}

func boardStreamTestMemory(pr io.ReadCloser) *fakeMemory {
	return &fakeMemory{
		workItems: []WorkItem{
			{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready"},
		},
		eventStreamBody: pr,
	}
}

// A burst of board-relevant events must produce one re-render per coalescing
// window, always from the newest state — not one render per event.
func TestBoardStreamCoalescesBursts(t *testing.T) {
	const window = 300 * time.Millisecond
	setBoardStreamTiming(t, window, time.Hour)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	rec, stop := startBoardStream(t, boardStreamTestMemory(pr))
	defer stop()

	// Catch-up frame first: the connection is live before the burst.
	waitForFrames(t, rec, "#board", 1, 2*time.Second)

	writeUpstreamEvents(t, pw,
		"entity.status_changed", `{"entity":"graph_object","id":"w1"}`,
		"entity.assigned", `{"entity":"graph_object","id":"w2"}`,
	)

	// One coalesced re-render: catch-up + burst.
	waitForFrames(t, rec, "#board", 2, 2*time.Second)
	// Let more than one window pass: a per-event renderer has already emitted
	// the burst's second frame by now.
	time.Sleep(3 * window)
	if got := frameCount(rec.body(), "#board"); got != 2 {
		t.Fatalf("a burst of two events produced %d #board frame(s), want 2 (catch-up + one coalesced re-render); a later frame means the renderer ran per event:\n%s", got, rec.body())
	}

	// A change after the window opens a new one and gets its own frame.
	writeUpstreamEvents(t, pw, "entity.created", `{"entity":"graph_object","id":"w3"}`)
	waitForFrames(t, rec, "#board", 3, 2*time.Second)
}

// An idle stream must still be kept alive: a comment-only heartbeat line stops
// proxies and CDNs from reaping the connection, and it has to keep coming.
func TestBoardStreamHeartbeatsAnIdleStream(t *testing.T) {
	setBoardStreamTiming(t, 10*time.Millisecond, 40*time.Millisecond)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	rec, stop := startBoardStream(t, boardStreamTestMemory(pr))
	defer stop()

	waitForFrames(t, rec, "#board", 1, 2*time.Second)
	waitForOccurrences(t, rec, ": heartbeat", 1, 3*time.Second)
	// A second heartbeat proves the first tick did not end the stream.
	waitForOccurrences(t, rec, ": heartbeat", 2, 3*time.Second)

	if body := rec.body(); strings.Contains(body, "<hx-partial") == false {
		t.Fatalf("heartbeat stream lost its frames:\n%s", body)
	}
}

// A background re-render whose item count and agent-health signature are
// unchanged touches only the columns: re-sending the health readout would morph
// a region the user may be reading for no reason (the behaviour the board-ux
// spec now states). The header is never re-sent at all.
func TestBoardStreamSendsOnlyTheBoardWhenNothingElseChanged(t *testing.T) {
	setBoardStreamTiming(t, 20*time.Millisecond, time.Hour)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	rec, stop := startBoardStream(t, boardStreamTestMemory(pr))
	defer stop()

	// Catch-up sends the columns and the health readout once. The header carries
	// no count, so it is never a frame target.
	waitForFrames(t, rec, "#board", 1, 2*time.Second)
	waitForFrames(t, rec, "#board-health", 1, 2*time.Second)

	writeUpstreamEvents(t, pw, "entity.status_changed", `{"entity":"graph_object","id":"w1"}`)
	waitForFrames(t, rec, "#board", 2, 2*time.Second)

	if got := frameCount(rec.body(), "#board-header"); got != 0 {
		t.Errorf("the header carries no count, so no frame may target it (%d frames, want 0)", got)
	}
	if got := frameCount(rec.body(), "#board-health"); got != 1 {
		t.Errorf("unchanged agent health re-sent the health readout (%d frames, want 1)", got)
	}
}
