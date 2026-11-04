package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- live-state hub ---

func thinkingJSON(id, text string, done bool) string {
	b, _ := json.Marshal(map[string]any{"type": "thinking", "id": id, "role": "reasoning", "text": text, "done": done})
	return string(b)
}

func toolJSON(id, tool, status string) string {
	m := map[string]any{"type": "mcp_tool", "tool": tool, "status": status, "result": map[string]any{"q": "x"}}
	if id != "" {
		m["id"] = id
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func frameTypes(t *testing.T, p liveReplayPayload) []string {
	t.Helper()
	var types []string
	for _, f := range p.Frames {
		var m map[string]any
		if err := json.Unmarshal(f, &m); err != nil {
			t.Fatalf("frame not JSON: %v (%s)", err, f)
		}
		types = append(types, m["type"].(string))
	}
	return types
}

// TestLiveHubThinkingRetainedUntilDone asserts an open thinking segment is
// retained (latest delta wins) until its `done:true` close drops it.
func TestLiveHubThinkingRetainedUntilDone(t *testing.T) {
	h := newLiveStateHub()
	h.beginRun("c1", "r1")

	h.observeThinking("c1", thinkingJSON("seg1", "hello", false))
	if got := len(h.replay("c1").Frames); got != 1 {
		t.Fatalf("open segment frames = %d, want 1", got)
	}

	// A second delta for the same segment replaces, not appends.
	h.observeThinking("c1", thinkingJSON("seg1", "hello world", false))
	p := h.replay("c1")
	if len(p.Frames) != 1 {
		t.Fatalf("second delta frames = %d, want 1 (upsert, not append)", len(p.Frames))
	}
	var m map[string]any
	if err := json.Unmarshal(p.Frames[0], &m); err != nil {
		t.Fatal(err)
	}
	if m["text"] != "hello world" {
		t.Errorf("segment text = %v, want the latest delta", m["text"])
	}

	// A distinct segment is retained alongside the first.
	h.observeThinking("c1", thinkingJSON("seg2", "other", false))
	if len(h.replay("c1").Frames) != 2 {
		t.Fatalf("two open segments frames = %d, want 2", len(h.replay("c1").Frames))
	}

	// Closing seg1 drops it, leaving seg2.
	h.observeThinking("c1", thinkingJSON("seg1", "", true))
	p = h.replay("c1")
	if len(p.Frames) != 1 {
		t.Fatalf("after close frames = %d, want 1", len(p.Frames))
	}
	if types := frameTypes(t, p); len(types) != 1 || types[0] != "thinking" {
		t.Fatalf("remaining frame types = %v, want [thinking]", types)
	}
}

// TestLiveHubToolRetainedUntilTerminal asserts a started tool is retained until
// a terminal status drops it.
func TestLiveHubToolRetainedUntilTerminal(t *testing.T) {
	h := newLiveStateHub()
	h.beginRun("c1", "r1")

	h.observeTool("c1", toolJSON("call1", "web_search", "started"))
	if got := len(h.replay("c1").Frames); got != 1 {
		t.Fatalf("running tool frames = %d, want 1", got)
	}

	h.observeTool("c1", toolJSON("call1", "web_search", "completed"))
	if got := len(h.replay("c1").Frames); got != 0 {
		t.Fatalf("completed tool frames = %d, want 0", got)
	}

	// awaiting_confirmation and error are also terminal (non-green).
	h.observeTool("c1", toolJSON("call2", "shell", "started"))
	h.observeTool("c1", toolJSON("call2", "shell", "awaiting_confirmation"))
	if got := len(h.replay("c1").Frames); got != 0 {
		t.Fatalf("awaiting_confirmation tool frames = %d, want 0", got)
	}
	h.observeTool("c1", toolJSON("call3", "shell", "started"))
	h.observeTool("c1", toolJSON("call3", "shell", "error"))
	if got := len(h.replay("c1").Frames); got != 0 {
		t.Fatalf("errored tool frames = %d, want 0", got)
	}
}

// TestLiveHubClearedWhenRunLeavesRunning asserts clearIfNotRunning drops state
// on any non-running bucket and keeps it while running.
func TestLiveHubClearedWhenRunLeavesRunning(t *testing.T) {
	h := newLiveStateHub()
	h.beginRun("c1", "r1")
	h.observeTool("c1", toolJSON("call1", "web_search", "started"))
	h.observeThinking("c1", thinkingJSON("seg1", "x", false))

	h.clearIfNotRunning("c1", runBucketRunning)
	if got := len(h.replay("c1").Frames); got != 2 {
		t.Fatalf("running bucket must keep state, frames = %d", got)
	}

	for _, bucket := range []string{runBucketDone, runBucketFailed, runBucketNeedsInput} {
		h.clearIfNotRunning("c1", bucket)
		if got := len(h.replay("c1").Frames); got != 0 {
			t.Fatalf("bucket %q must clear state, frames = %d", bucket, got)
		}
		// Repopulate for the next iteration.
		h.beginRun("c1", "r1")
		h.observeTool("c1", toolJSON("call1", "web_search", "started"))
		h.observeThinking("c1", thinkingJSON("seg1", "x", false))
	}
}

// TestLiveHubToolIDlessFallbackDropsOldest asserts id-less tools key on tool
// name and a terminal event drops the oldest matching call.
func TestLiveHubToolIDlessFallbackDropsOldest(t *testing.T) {
	h := newLiveStateHub()
	h.beginRun("c1", "r1")

	h.observeTool("c1", toolJSON("", "web_search", "started"))
	h.observeTool("c1", toolJSON("", "web_search", "started"))
	if got := len(h.replay("c1").Frames); got != 2 {
		t.Fatalf("two id-less same-name tools = %d, want 2", got)
	}

	// Terminal (id-less) drops the oldest match, leaving one running.
	h.observeTool("c1", toolJSON("", "web_search", "completed"))
	if got := len(h.replay("c1").Frames); got != 1 {
		t.Fatalf("after terminal drop = %d frames, want 1", got)
	}
}

// TestLiveHubRunIDAndDone asserts beginRun resets state for a new run and
// endRun (the stream's `done`) clears it.
func TestLiveHubRunIDAndDone(t *testing.T) {
	h := newLiveStateHub()
	h.beginRun("c1", "r1")
	h.observeTool("c1", toolJSON("call1", "web_search", "started"))
	if got := h.replay("c1").RunID; got != "r1" {
		t.Fatalf("run id = %q, want r1", got)
	}

	// A new run replaces the buffer and its run id.
	h.beginRun("c1", "r2")
	p := h.replay("c1")
	if p.RunID != "r2" || len(p.Frames) != 0 {
		t.Fatalf("new run state = %+v, want runID r2 with no frames", p)
	}

	h.observeTool("c1", toolJSON("call1", "web_search", "started"))
	h.endRun("c1")
	if got := len(h.replay("c1").Frames); got != 0 {
		t.Fatalf("after endRun frames = %d, want 0", got)
	}
}

// --- live tee ---

// TestLiveTeeForwardsAndRecords asserts the tee forwards bytes verbatim while
// resolving the conversation/run id from the `meta` frame and recording
// thinking + tool frames.
func TestLiveTeeForwardsAndRecords(t *testing.T) {
	h := newLiveStateHub()
	var out bytes.Buffer
	tee := newLiveTee(&out, h, "")

	stream := `data: {"type":"meta","conversationId":"c9","runId":"r9"}` + "\n\n" +
		`data: {"type":"thinking","id":"s1","role":"reasoning","text":"x","done":false}` + "\n\n" +
		`data: {"type":"mcp_tool","id":"call1","tool":"web_search","status":"started","result":{"q":"x"}}` + "\n\n"
	if _, err := tee.Write([]byte(stream)); err != nil {
		t.Fatal(err)
	}

	if out.String() != stream {
		t.Errorf("tee must forward verbatim, got %q", out.String())
	}
	p := h.replay("c9")
	if p.RunID != "r9" {
		t.Errorf("run id = %q, want r9", p.RunID)
	}
	if types := frameTypes(t, p); len(types) != 2 || types[0] != "thinking" || types[1] != "mcp_tool" {
		t.Fatalf("recorded frames = %v, want [thinking mcp_tool]", types)
	}
}

// TestLiveTeeUsesRequestConversationID asserts the request-supplied
// conversation id wins over the meta frame's (they match for an existing
// conversation; the request value is authoritative).
func TestLiveTeeUsesRequestConversationID(t *testing.T) {
	h := newLiveStateHub()
	var out bytes.Buffer
	tee := newLiveTee(&out, h, "c-from-request")

	stream := `data: {"type":"meta","conversationId":"c-from-meta","runId":"r1"}` + "\n\n" +
		`data: {"type":"thinking","id":"s1","role":"reasoning","text":"x","done":false}` + "\n\n"
	_, _ = tee.Write([]byte(stream))

	if got := len(h.replay("c-from-request").Frames); got != 1 {
		t.Fatalf("request-scoped frames = %d, want 1 (request id must be authoritative)", got)
	}
	if got := len(h.replay("c-from-meta").Frames); got != 0 {
		t.Fatalf("meta-only id must not be used when the request carried an id, frames = %d", got)
	}
}

// --- events endpoint ---

// sseRecorder is an http.ResponseWriter + Flusher that captures written SSE
// frames in a buffer, so streamEvents can be driven without a live network.
type sseRecorder struct {
	header http.Header
	buf    bytes.Buffer
}

func (r *sseRecorder) Header() http.Header         { return r.header }
func (r *sseRecorder) Write(p []byte) (int, error) { return r.buf.Write(p) }
func (r *sseRecorder) WriteHeader(int)             {}
func (r *sseRecorder) Flush()                      {}

// sseFrames splits a captured SSE stream body into its ordered frame payloads
// (the JSON following each "data: " prefix).
func sseFrames(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, chunk := range strings.Split(body, "\n\n") {
		chunk = strings.TrimSpace(chunk)
		if !strings.HasPrefix(chunk, "data: ") {
			continue
		}
		out = append(out, strings.TrimPrefix(chunk, "data: "))
	}
	return out
}

func startEventsStream(t *testing.T, s *Server, key string) (*sseRecorder, func()) {
	t.Helper()
	e := echo.New()
	rec := &sseRecorder{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/"+key+"/events", nil)
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(key)
	done := make(chan struct{})
	go func() { _ = s.streamEvents(c, key); close(done) }()
	return rec, func() {
		s.beginShutdown()
		<-done
	}
}

// TestConversationEventsEmitsLiveReplay asserts the connect sequence is
// refresh → live_replay (with the in-flight frames and run id) → tail.
func TestConversationEventsEmitsLiveReplay(t *testing.T) {
	live := newLiveStateHub()
	live.beginRun("c1", "r1")
	live.observeThinking("c1", thinkingJSON("s1", "x", false))
	live.observeTool("c1", toolJSON("call1", "web_search", "started"))

	s := &Server{cfg: Config{AuthMode: "dev"}, shutdownCh: make(chan struct{})}
	s.hub = newConversationHub(s)
	s.live = live
	s.memory = &fakeMemory{}

	rec, stop := startEventsStream(t, s, "c1")
	time.Sleep(100 * time.Millisecond)
	stop()

	body := rec.buf.String()
	iRefresh := strings.Index(body, `"type":"refresh"`)
	iReplay := strings.Index(body, `"type":"live_replay"`)
	if iRefresh < 0 || iReplay < 0 {
		t.Fatalf("missing refresh or live_replay frame: %q", body)
	}
	if iRefresh > iReplay {
		t.Fatalf("live_replay must follow refresh: %q", body)
	}
	if !strings.Contains(body, `"runId":"r1"`) {
		t.Errorf("live_replay missing run id: %q", body)
	}
	if !strings.Contains(body, `"id":"s1"`) || !strings.Contains(body, `"id":"call1"`) {
		t.Errorf("live_replay missing in-flight frames: %q", body)
	}
}

// TestConversationEventsEmitsEmptyLiveReplay asserts an empty frames array is
// still emitted (additive, deterministic) when nothing is in flight.
func TestConversationEventsEmitsEmptyLiveReplay(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "dev"}, shutdownCh: make(chan struct{})}
	s.hub = newConversationHub(s)
	s.live = newLiveStateHub()
	s.memory = &fakeMemory{}

	rec, stop := startEventsStream(t, s, "c1")
	time.Sleep(100 * time.Millisecond)
	stop()

	body := rec.buf.String()
	iRefresh := strings.Index(body, `"type":"refresh"`)
	iReplay := strings.Index(body, `"type":"live_replay"`)
	if iRefresh < 0 || iReplay < 0 || iRefresh > iReplay {
		t.Fatalf("expected refresh then live_replay: %q", body)
	}
	if !strings.Contains(body, `"frames":[]`) {
		t.Errorf("empty live_replay must carry an empty frames array: %q", body)
	}
}

// TestConversationEventsReplaysCachedRefreshFrame asserts a subscriber that
// connects AFTER a previous broadcast receives that cached state-bearing
// refresh frame (bucket/runId/runStatus) before live_replay — the fix for a
// page opened mid-run, while an existing subscriber already holds the
// conversation, showing no "agent is working" indicator.
func TestConversationEventsReplaysCachedRefreshFrame(t *testing.T) {
	live := newLiveStateHub()
	live.beginRun("c1", "r1")
	live.observeThinking("c1", thinkingJSON("s1", "x", false))

	runStart := json.RawMessage(`{"kind":"run_start","run_id":"r1","run_status":"working"}`)
	m := &scriptedMemory{
		fakeMemory: &fakeMemory{},
		histories:  []*ConversationHistory{{ConversationID: "c1", Items: []json.RawMessage{runStart}}},
	}

	s := &Server{cfg: Config{AuthMode: "dev"}, shutdownCh: make(chan struct{})}
	s.hub = newConversationHub(s)
	s.live = live
	s.memory = m

	// An existing subscriber holds the conversation and triggers a broadcast
	// whose refresh frame carries the running run state. That broadcast now
	// populates the hub's per-conversation cache.
	first := s.hub.subscribe("c1", nil)
	defer s.hub.unsubscribe("c1", first)
	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())
	select {
	case <-first:
	default:
		t.Fatal("first broadcast must deliver a refresh frame to the existing subscriber")
	}

	// A second subscriber connects and must receive the cached state-bearing
	// refresh frame (not a bare refresh) before live_replay.
	rec, stop := startEventsStream(t, s, "c1")
	time.Sleep(100 * time.Millisecond)
	stop()

	frames := sseFrames(t, rec.buf.String())
	if len(frames) < 2 {
		t.Fatalf("expected refresh then live_replay frames, got %d: %q", len(frames), rec.buf.String())
	}
	var rf refreshPayload
	if err := json.Unmarshal([]byte(frames[0]), &rf); err != nil {
		t.Fatalf("first frame is not a refresh payload: %v (%s)", err, frames[0])
	}
	if rf.Type != "refresh" {
		t.Fatalf("first frame type = %q, want refresh", rf.Type)
	}
	if rf.Bucket != runBucketRunning || rf.RunID != "r1" || rf.RunStatus != "working" {
		t.Fatalf("cached refresh frame = %+v, want running bucket with run r1 status working", rf)
	}
	var replay map[string]any
	if err := json.Unmarshal([]byte(frames[1]), &replay); err != nil {
		t.Fatalf("second frame is not JSON: %v (%s)", err, frames[1])
	}
	if replay["type"] != "live_replay" {
		t.Fatalf("second frame type = %v, want live_replay", replay["type"])
	}
}
