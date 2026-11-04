package main

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// boardStreamEventRelevant is the whole point of the live refresh: the work
// path announces a move with entity.status_changed (and reassignment with
// entity.assigned), not entity.updated. A filter that misses them leaves the
// board frozen until a manual reload — the defect this endpoint fixes.
func TestBoardStreamEventRelevant(t *testing.T) {
	tests := []struct {
		name  string
		event string
		data  string
		want  bool
	}{
		{"status change on a work item", "entity.status_changed", `{"entity":"graph_object","id":"w1"}`, true},
		{"reassignment on a work item", "entity.assigned", `{"entity":"graph_object","id":"w1"}`, true},
		{"work item created", "entity.created", `{"entity":"graph_object","id":"w1"}`, true},
		{"work item deleted", "entity.deleted", `{"entity":"graph_object","id":"w1"}`, true},
		{"agent run badge moved", "entity.updated", `{"entity":"agent_run","id":"r1"}`, true},
		{"batch of objects", "entity.batch", `{"entity":"graph_object","ids":["a","b"]}`, true},
		{"document event is not board state", "entity.updated", `{"entity":"document","id":"d1"}`, false},
		{"notification event is not board state", "entity.created", `{"entity":"notification","id":"n1"}`, false},
		{"heartbeat frame", "heartbeat", `{"ts":"now"}`, false},
		{"connected frame", "connected", `{"connection_id":"c1"}`, false},
		{"malformed payload", "entity.status_changed", `not json`, false},
		{"empty payload", "entity.status_changed", ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boardStreamEventRelevant(tt.event, tt.data); got != tt.want {
				t.Errorf("boardStreamEventRelevant(%q, %q) = %v, want %v", tt.event, tt.data, got, tt.want)
			}
		})
	}
}

// The SSE wire format is line-oriented, so a multi-line HTML fragment must be
// split across `data:` lines and rejoined by the client parser. Emitting raw
// newlines would truncate the frame at its first line.
func TestWriteSSEPartialSplitsLines(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := writeSSEPartial(rec, "#board", "outerMorph", "<div id=\"board\">\n<p>hi</p>\n</div>"); err != nil {
		t.Fatalf("writeSSEPartial: %v", err)
	}
	got := rec.Body.String()
	want := "data: <hx-partial hx-target=\"#board\" hx-swap=\"outerMorph\"><div id=\"board\">\n" +
		"data: <p>hi</p>\n" +
		"data: </div></hx-partial>\n\n"
	if got != want {
		t.Errorf("frame mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(strings.TrimSuffix(got, "\n\n"), "\n\n") {
		t.Errorf("frame contains a blank line before its terminator: %q", got)
	}
}

// TestBoardStreamPushesFrames asserts the endpoint's contract end to end over a
// real HTTP connection: it answers with text/event-stream, sends a catch-up
// frame set immediately, and pushes another #board frame when the upstream
// project stream reports an entity.status_changed event.
func TestBoardStreamPushesFrames(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })

	f := &fakeMemory{
		workItems: []WorkItem{
			{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready"},
		},
		eventStreamBody: pr,
	}
	_, e := newBoardEcho(f)
	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/board/stream")
	if err != nil {
		t.Fatalf("GET /board/stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	reader := bufio.NewReader(resp.Body)
	first := readSSEUntil(t, reader, `hx-target="#board" hx-swap`)
	if !strings.Contains(first, `id="board"`) {
		t.Fatalf("catch-up frame does not carry the columns fragment:\n%s", first)
	}

	if _, err := io.WriteString(pw, "event: entity.status_changed\ndata: {\"entity\":\"graph_object\",\"id\":\"w1\"}\n\n"); err != nil {
		t.Fatalf("write upstream event: %v", err)
	}
	second := readSSEUntil(t, reader, `hx-target="#board" hx-swap`)
	if !strings.Contains(second, `data-board-status-map`) {
		t.Fatalf("event-driven frame does not carry the rendered board:\n%s", second)
	}
	if strings.Contains(second, "#board-drawer") {
		t.Errorf("background frame must not clear the drawer:\n%s", second)
	}
}

// A failed upstream connection must still answer as an event stream: htmx only
// takes over text/event-stream responses, so an HTTP error here would end the
// live connection for the life of the tab.
func TestBoardStreamUnavailableAnswersWithRetryableSSE(t *testing.T) {
	f := &fakeMemory{eventStreamErr: errors.New("memory 502 bad gateway")}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/stream", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (so hx-sse keeps the connection)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "retry:") {
		t.Errorf("unavailable response carries no retry hint: %q", body)
	}
}

// Background refreshes must not touch the drawer, while an action response must
// still close it — an action is taken *from* the drawer, a live update is not.
func TestBoardPartialDoesNotClearTheDrawer(t *testing.T) {
	f := &fakeMemory{workItems: []WorkItem{
		{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready"},
	}}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/partial", nil))
	body := rec.Body.String()

	if !strings.Contains(body, `id="board"`) {
		t.Fatalf("partial did not render the board region:\n%s", body)
	}
	for _, forbidden := range []string{"board-drawer", "hx-swap-oob"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("/board/partial carries %q — a background refresh would close an open drawer:\n%s", forbidden, body)
		}
	}

	action := renderHTML(t, BoardRefresh(
		[]WorkItem{{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready"}},
		[]string{"ready"}, nil, defaultBoardStatusMap(), "",
	))
	if !strings.Contains(action, "board-drawer") {
		t.Errorf("action response lost the drawer clear, so acting in the drawer no longer closes it")
	}
}

// readSSEUntil reads until needle appears in the accumulated body, failing the
// test rather than hanging when the stream stalls.
func readSSEUntil(t *testing.T, r *bufio.Reader, needle string) string {
	t.Helper()
	type result struct {
		body string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := r.ReadString('\n')
			b.WriteString(line)
			if strings.Contains(b.String(), needle) {
				ch <- result{body: b.String()}
				return
			}
			if err != nil {
				ch <- result{body: b.String(), err: err}
				return
			}
		}
	}()
	select {
	case got := <-ch:
		if got.err != nil && !strings.Contains(got.body, needle) {
			t.Fatalf("stream ended before %q (err=%v):\n%s", needle, got.err, got.body)
		}
		return got.body
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %q", needle)
		return ""
	}
}
