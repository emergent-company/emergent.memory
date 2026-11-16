package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The work board's live refresh, exercised over a real (in-memory) response
// writer so the assertion is about what a client receives on the wire rather than
// about a helper. workStreamCoalesce and workStreamHeartbeat are package-level
// vars precisely so these tests can shrink them (see work_stream.go).

// setWorkStreamTiming shrinks the stream's two intervals for one test.
func setWorkStreamTiming(t *testing.T, window, heartbeat time.Duration) {
	t.Helper()
	prevWindow, prevHeartbeat := workStreamCoalesce, workStreamHeartbeat
	workStreamCoalesce, workStreamHeartbeat = window, heartbeat
	t.Cleanup(func() {
		workStreamCoalesce, workStreamHeartbeat = prevWindow, prevHeartbeat
	})
}

// TestWorkStreamEventRelevant is the whole point of the live refresh: the work
// path announces a move with entity.status_changed (and reassignment with
// entity.assigned), not entity.updated. A filter that misses them leaves the
// board frozen until a manual reload.
func TestWorkStreamEventRelevant(t *testing.T) {
	tests := []struct {
		name  string
		event string
		data  string
		want  bool
	}{
		{"status change on a work item", "entity.status_changed", `{"entity":"graph_object","id":"w1"}`, true},
		{"reassignment on a work item", "entity.assigned", `{"entity":"graph_object","id":"w1"}`, true},
		{"work item created", "entity.created", `{"entity":"graph_object","id":"w1"}`, true},
		{"work item updated", "entity.updated", `{"entity":"graph_object","id":"w1"}`, true},
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
			if got := workStreamEventRelevant(tt.event, tt.data); got != tt.want {
				t.Errorf("workStreamEventRelevant(%q, %q) = %v, want %v", tt.event, tt.data, got, tt.want)
			}
		})
	}
}

// The SSE wire format is line-oriented, so a multi-line HTML fragment must be
// split across `data:` lines and rejoined by the client parser. Emitting raw
// newlines would truncate the frame at its first line.
func TestWriteSSEPartialSplitsLines(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := writeSSEPartial(rec, "#work-board", "outerMorph", "<div id=\"work-board\">\n<p>hi</p>\n</div>"); err != nil {
		t.Fatalf("writeSSEPartial: %v", err)
	}
	got := rec.Body.String()
	want := "data: <hx-partial hx-target=\"#work-board\" hx-swap=\"outerMorph\"><div id=\"work-board\">\n" +
		"data: <p>hi</p>\n" +
		"data: </div></hx-partial>\n\n"
	if got != want {
		t.Errorf("frame mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(strings.TrimSuffix(got, "\n\n"), "\n\n") {
		t.Errorf("frame contains a blank line before its terminator: %q", got)
	}
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

// workStreamMemory is a fakeMemory whose work items and agent list the test can
// change while the stream goroutine reads them (fakeMemory's fields are
// unsynchronised, which the race detector rightly flags).
type workStreamMemory struct {
	*fakeMemory
	mu        sync.Mutex
	items     []WorkItem
	agents    []ScheduledAgent
	agentsErr error
}

func (m *workStreamMemory) ListWorkItems(context.Context, string, string, int, bool) ([]WorkItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.items, nil
}

func (m *workStreamMemory) ListScheduledAgents(context.Context) ([]ScheduledAgent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.agentsErr != nil {
		return nil, m.agentsErr
	}
	return m.agents, nil
}

func (m *workStreamMemory) setItems(items ...WorkItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = items
}

func (m *workStreamMemory) setAgents(agents ...ScheduledAgent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents = agents
}

func (m *workStreamMemory) setAgentsErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentsErr = err
}

// workStreamTestMemory is a two-type board (Task, Bug) holding one Task, so the
// catch-up frame renders one matrix of two types and both readouts are populated.
func workStreamTestMemory(pr io.ReadCloser) *workStreamMemory {
	return &workStreamMemory{
		fakeMemory: &fakeMemory{
			compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
				{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "in_progress", "done"}},
				{Name: "Bug", BoardEnabled: true, AllowedStatuses: []string{"open", "closed"}},
			}},
			eventStreamBody: pr,
		},
		items: []WorkItem{
			{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "ready"},
		},
	}
}

// startWorkBoardStream runs GET /work/board/stream on the work-board routes and
// returns the recording writer plus a stop func. The request context is the
// stream's only exit, so cancel is what ends it.
func startWorkBoardStream(t *testing.T, f MemoryBackend) (*streamRecorder, func()) {
	t.Helper()
	_, e := newWorkBoardEcho(f)
	rec := newStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/work/board/stream", nil).WithContext(ctx)
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
			t.Errorf("work board stream did not stop after its context was cancelled")
		}
	}
	return rec, stop
}

// frameCount counts the <hx-partial> directives addressed at one region. The
// target is matched with its closing quote so "#work-board" does not also count
// "#work-board-count" or "#work-board-health".
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

// TestWorkBoardStreamPushesFrames asserts the endpoint's contract end to end over
// a real HTTP connection: it answers with text/event-stream, sends a catch-up
// frame set immediately, and pushes another frame when the upstream project
// stream reports an entity.status_changed event.
func TestWorkBoardStreamPushesFrames(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })

	f := workStreamTestMemory(pr)
	_, e := newWorkBoardEcho(f)
	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/work/board/stream")
	if err != nil {
		t.Fatalf("GET /work/board/stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	reader := bufio.NewReader(resp.Body)
	first := readSSEUntil(t, reader, `hx-target="#work-board" hx-swap`)
	if !strings.Contains(first, `id="work-board"`) || !strings.Contains(first, "data-board-status-map") {
		t.Fatalf("catch-up frame does not carry the board fragment:\n%s", first)
	}
	if strings.Contains(first, "board-drawer") {
		t.Errorf("the frame must not target a drawer that no longer exists:\n%s", first)
	}

	if _, err := io.WriteString(pw, "event: entity.status_changed\ndata: {\"entity\":\"graph_object\",\"id\":\"w1\"}\n\n"); err != nil {
		t.Fatalf("write upstream event: %v", err)
	}
	second := readSSEUntil(t, reader, `hx-target="#work-board" hx-swap`)
	if !strings.Contains(second, `data-board-status-map`) {
		t.Fatalf("event-driven frame does not carry the rendered board:\n%s", second)
	}
}

// A failed upstream connection must still answer as an event stream: htmx only
// takes over text/event-stream responses, so an HTTP error here would end the
// live connection for the life of the tab.
func TestWorkBoardStreamUnavailableAnswersWithRetryableSSE(t *testing.T) {
	f := &fakeMemory{eventStreamErr: errors.New("memory 502 bad gateway")}
	_, e := newWorkBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/work/board/stream", nil))

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

// A burst of board-relevant events must produce one re-render per coalescing
// window, always from the newest state — not one render per event.
func TestWorkBoardStreamCoalescesBursts(t *testing.T) {
	const window = 300 * time.Millisecond
	setWorkStreamTiming(t, window, time.Hour)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	rec, stop := startWorkBoardStream(t, workStreamTestMemory(pr))
	defer stop()

	// Catch-up frame first: the connection is live before the burst.
	waitForFrames(t, rec, "#work-board", 1, 2*time.Second)

	writeUpstreamEvents(t, pw,
		"entity.status_changed", `{"entity":"graph_object","id":"w1"}`,
		"entity.assigned", `{"entity":"graph_object","id":"w2"}`,
	)

	// One coalesced re-render: catch-up + burst.
	waitForFrames(t, rec, "#work-board", 2, 2*time.Second)
	// Let more than one window pass: a per-event renderer has already emitted
	// the burst's second frame by now.
	time.Sleep(3 * window)
	if got := frameCount(rec.body(), "#work-board"); got != 2 {
		t.Fatalf("a burst of two events produced %d #work-board frame(s), want 2 (catch-up + one coalesced re-render); a later frame means the renderer ran per event:\n%s", got, rec.body())
	}

	// A change after the window opens a new one and gets its own frame.
	writeUpstreamEvents(t, pw, "entity.created", `{"entity":"graph_object","id":"w3"}`)
	waitForFrames(t, rec, "#work-board", 3, 2*time.Second)
}

// An idle stream must still be kept alive: a comment-only heartbeat line stops
// proxies and CDNs from reaping the connection, and it has to keep coming.
func TestWorkBoardStreamHeartbeatsAnIdleStream(t *testing.T) {
	setWorkStreamTiming(t, 10*time.Millisecond, 40*time.Millisecond)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	rec, stop := startWorkBoardStream(t, workStreamTestMemory(pr))
	defer stop()

	waitForFrames(t, rec, "#work-board", 1, 2*time.Second)
	waitForOccurrences(t, rec, ": heartbeat", 1, 3*time.Second)
	// A second heartbeat proves the first tick did not end the stream.
	waitForOccurrences(t, rec, ": heartbeat", 2, 3*time.Second)

	if body := rec.body(); !strings.Contains(body, "<hx-partial") {
		t.Fatalf("heartbeat stream lost its frames:\n%s", body)
	}
}

// A background re-render whose count and agent-health signature are unchanged
// touches only the matrices: re-sending the readouts would morph regions the user
// may be reading for no reason.
func TestWorkBoardStreamSendsOnlyTheBoardWhenNothingElseChanged(t *testing.T) {
	setWorkStreamTiming(t, 20*time.Millisecond, time.Hour)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	rec, stop := startWorkBoardStream(t, workStreamTestMemory(pr))
	defer stop()

	// Catch-up sends the matrices, the count and the (empty) health readout once.
	waitForFrames(t, rec, "#work-board", 1, 2*time.Second)
	waitForFrames(t, rec, "#work-board-count", 1, 2*time.Second)
	waitForFrames(t, rec, "#work-board-health", 1, 2*time.Second)

	writeUpstreamEvents(t, pw, "entity.status_changed", `{"entity":"graph_object","id":"w1"}`)
	waitForFrames(t, rec, "#work-board", 2, 2*time.Second)

	body := rec.body()
	if got := frameCount(body, "#work-board-count"); got != 1 {
		t.Errorf("an unchanged type count was re-sent (%d frames, want the catch-up only)", got)
	}
	if got := frameCount(body, "#work-board-health"); got != 1 {
		t.Errorf("unchanged agent health re-sent the health readout (%d frames, want 1)", got)
	}
}

// The count is a readout of its own: it is re-sent when the number of rendered
// matrices changes (an item of a previously-empty type appears, or the last one
// disappears), and only then.
func TestWorkBoardStreamRefreshesTheCountWhenItChanges(t *testing.T) {
	setWorkStreamTiming(t, 20*time.Millisecond, time.Hour)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	f := workStreamTestMemory(pr)
	rec, stop := startWorkBoardStream(t, f)
	defer stop()

	// One Task of two board types: the default view hides the empty Bug type.
	waitForFrames(t, rec, "#work-board-count", 1, 2*time.Second)
	if body := rec.body(); !strings.Contains(body, "1 of 2 types") {
		t.Fatalf("catch-up count does not read \"1 of 2 types\":\n%s", body)
	}

	// A Bug appears: the count changes and must be re-sent.
	f.setItems(
		WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "ready"},
		WorkItem{CanonicalID: "b1", Type: "Bug", Key: "crash-1", Status: "open"},
	)
	writeUpstreamEvents(t, pw, "entity.created", `{"entity":"graph_object","id":"b1"}`)
	waitForFrames(t, rec, "#work-board-count", 2, 2*time.Second)
	if body := rec.body(); !strings.Contains(body, "2 of 2 types") {
		t.Errorf("count frame does not read \"2 of 2 types\":\n%s", body)
	}

	// Another Task keeps the count: no third count frame.
	f.setItems(
		WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "ready"},
		WorkItem{CanonicalID: "w2", Type: "Task", Key: "ship-2", Status: "ready"},
		WorkItem{CanonicalID: "b1", Type: "Bug", Key: "crash-1", Status: "open"},
	)
	writeUpstreamEvents(t, pw, "entity.created", `{"entity":"graph_object","id":"w2"}`)
	waitForFrames(t, rec, "#work-board", 3, 2*time.Second)
	if got := frameCount(rec.body(), "#work-board-count"); got != 2 {
		t.Errorf("an unchanged count was re-sent (%d frames, want 2)", got)
	}
}

// The health readout exists to warn about an agent that stopped doing its job,
// but an agent-health change publishes no event at all: WorkerPool.handleFailure
// only logs, and toggling an agent emits nothing. Refreshing the banner on the
// next unrelated work-item event is backwards — it must catch up by itself,
// because the situations it warns about are exactly the ones that produce no work
// item. The idle tick re-checks it (#2466).
func TestWorkBoardStreamRefreshesAHealthChangeOnItsOwn(t *testing.T) {
	setWorkStreamTiming(t, 10*time.Millisecond, 40*time.Millisecond)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	f := workStreamTestMemory(pr)
	rec, stop := startWorkBoardStream(t, f)
	defer stop()

	// Catch-up frame: every agent is healthy, so the readout is empty.
	waitForFrames(t, rec, "#work-board-health", 1, 2*time.Second)

	// A reaction agent gets auto-disabled (its failure count tripped the
	// circuit breaker). No upstream event accompanies it.
	f.setAgents(ScheduledAgent{
		ID:                  "a1",
		Name:                "worker",
		TriggerType:         "reaction",
		Enabled:             false,
		ConsecutiveFailures: 3,
	})

	waitForFrames(t, rec, "#work-board-health", 2, 3*time.Second)
	body := rec.body()
	if !strings.Contains(body, "worker · disabled") {
		t.Fatalf("the health frame does not name the disabled agent:\n%s", body)
	}
	// Only the readout moves: a health change must not re-morph the matrices,
	// which would disturb a drag in progress for no reason.
	if got := frameCount(body, "#work-board"); got != 1 {
		t.Errorf("a health-only refresh re-sent the matrices (%d frames, want 1)", got)
	}

	// And the re-check must not become a frame storm: several more idle ticks
	// with an unchanged readout send nothing. This is the property the whole
	// signature comparison exists for, and the tick is now the only thing that
	// can exercise it.
	time.Sleep(8 * 40 * time.Millisecond)
	if got := frameCount(rec.body(), "#work-board-health"); got != 2 {
		t.Errorf("unchanged readout re-sent over idle ticks (%d frames, want 2)", got)
	}
}

// A failed ListScheduledAgents means the readout is unknown, not healthy.
// boardUnhealthyAgents(nil) is empty, so rendering the error as an empty list
// would clear the warning banner while the scheduling service was unreachable —
// the exact state the banner exists to disclose. The last good readout must
// survive, and a sustained outage must not emit a frame per heartbeat either.
func TestWorkBoardStreamKeepsLastHealthWhenListAgentsFails(t *testing.T) {
	setWorkStreamTiming(t, 10*time.Millisecond, 40*time.Millisecond)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	f := workStreamTestMemory(pr)
	f.setAgents(ScheduledAgent{
		ID:                  "a1",
		Name:                "worker",
		TriggerType:         "reaction",
		Enabled:             false,
		ConsecutiveFailures: 3,
	})
	rec, stop := startWorkBoardStream(t, f)
	defer stop()

	// Catch-up renders the warning from a successful fetch.
	waitForFrames(t, rec, "#work-board-health", 1, 2*time.Second)
	if body := rec.body(); !strings.Contains(body, "worker · disabled") {
		t.Fatalf("catch-up readout does not name the disabled agent:\n%s", body)
	}

	// The scheduling service goes away. Count from the current heartbeat so the
	// assertion below covers ticks that are certainly post-failure.
	before := strings.Count(rec.body(), ": heartbeat")
	f.setAgentsErr(errors.New("memory 502 bad gateway"))
	waitForOccurrences(t, rec, ": heartbeat", before+2, 3*time.Second)
	// Several more failed fetches: a per-heartbeat frame would be obvious.
	time.Sleep(6 * 40 * time.Millisecond)

	body := rec.body()
	if got := frameCount(body, "#work-board-health"); got != 1 {
		t.Fatalf("a failed ListScheduledAgents emitted a health frame (%d frames, want the catch-up only); an empty frame clears the warning banner:\n%s", got, body)
	}
	// The last good warning is still what the client holds.
	if !strings.Contains(body, "worker · disabled") {
		t.Errorf("the warning banner was cleared from the readout while the fetch was failing:\n%s", body)
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
