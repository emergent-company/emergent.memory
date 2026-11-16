package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// a2aStreamGuard: one abort signal for a vanished SSE consumer (issue #868)
// ============================================================================

// recordingAbandon captures the run ids passed to the guard's callback. The
// callback runs under the guard's lock, so a plain slice with a local mutex
// keeps the race detector honest if a future change moves it off the lock.
type recordingAbandon struct {
	mu  sync.Mutex
	ids []string
}

func (r *recordingAbandon) record(runID string) {
	r.mu.Lock()
	r.ids = append(r.ids, runID)
	r.mu.Unlock()
}

func (r *recordingAbandon) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.ids))
	copy(out, r.ids)
	return out
}

// TestA2AStreamGuard_AbandonCancelsTrackedRunOnce pins the primary path: a
// failed write or a client disconnect cancels the run exactly once, even when
// both signals arrive.
func TestA2AStreamGuard_AbandonCancelsTrackedRunOnce(t *testing.T) {
	rec := &recordingAbandon{}
	guard := newA2AStreamGuard("run-1", rec.record)

	guard.abandon()
	guard.abandon() // the other signal arriving late must not double-cancel

	assert.Equal(t, []string{"run-1"}, rec.calls())
}

// TestA2AStreamGuard_AbandonBeforeTrackCancelsLateRun covers the resume window:
// the consumer vanishes before the resumed child run row exists. The local
// execution is stopped immediately (empty id), and the durable cancel is issued
// for the child run once OnRunCreated reports it — never for the paused
// checkpoint, which the guard deliberately does not track on the resume path.
func TestA2AStreamGuard_AbandonBeforeTrackCancelsLateRun(t *testing.T) {
	rec := &recordingAbandon{}
	guard := newA2AStreamGuard("", rec.record) // resume path: no executing run yet

	guard.abandon()
	guard.trackRun("child-run-1")

	assert.Equal(t, []string{"", "child-run-1"}, rec.calls(),
		"the local stop must fire immediately and the durable cancel once the child run exists")
}

// TestA2AStreamGuard_StopDisarms proves a normally finished stream is never
// cancelled after the fact: the handler disarms the guard before the request
// context is cancelled on return.
func TestA2AStreamGuard_StopDisarms(t *testing.T) {
	rec := &recordingAbandon{}
	guard := newA2AStreamGuard("run-1", rec.record)

	guard.stop()
	guard.abandon()
	guard.trackRun("run-2")

	assert.Empty(t, rec.calls(), "a disarmed guard must never cancel a run")
	if id, abandoned := guard.abandonedRunID(); abandoned || id != "run-1" {
		t.Fatalf("abandonedRunID() = (%q, %v), want (run-1, false)", id, abandoned)
	}
}

// TestA2AStreamGuard_StopEndsWatcherWithoutFiring proves the watcher goroutine
// is released on a clean return (no leak) and does not treat the handler's own
// return as a disconnect.
func TestA2AStreamGuard_StopEndsWatcherWithoutFiring(t *testing.T) {
	rec := &recordingAbandon{}
	guard := newA2AStreamGuard("run-1", rec.record)

	done := make(chan struct{})
	go func() {
		guard.watch(context.Background())
		close(done)
	}()

	guard.stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not return after stop")
	}
	assert.Empty(t, rec.calls())
}

// TestA2AStreamGuard_WatchFiresOnContextCancel is the client-disconnect signal:
// the request context is cancelled while the handler runs, and the guard
// cancels the tracked run.
func TestA2AStreamGuard_WatchFiresOnContextCancel(t *testing.T) {
	rec := &recordingAbandon{}
	guard := newA2AStreamGuard("run-1", rec.record)

	ctx, cancel := context.WithCancel(context.Background())
	go guard.watch(ctx)
	cancel()

	deadline := time.After(2 * time.Second)
	for {
		if got := rec.calls(); len(got) > 0 {
			assert.Equal(t, []string{"run-1"}, got)
			break
		}
		select {
		case <-deadline:
			t.Fatal("cancelled request context did not abandon the run")
		case <-time.After(time.Millisecond):
		}
	}
	guard.stop()
}

// ============================================================================
// streamRun wiring: a vanished consumer records the durable cancel intent
// ============================================================================

// failingResponseWriter fails every body write, standing in for a closed SSE
// connection whose writes the handler used to discard.
type failingResponseWriter struct {
	header http.Header
}

func newFailingResponseWriter() *failingResponseWriter {
	return &failingResponseWriter{header: http.Header{}}
}

func (w *failingResponseWriter) Header() http.Header       { return w.header }
func (w *failingResponseWriter) WriteHeader(int)           {}
func (w *failingResponseWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// TestStreamRun_WriteFailureRequestsRunCancellation drives streamRun with a
// ResponseWriter that rejects every write. The failed SSE write must record the
// durable cancel intent for the executing run instead of being discarded, so a
// run whose consumer is gone does not keep executing (issue #868).
func TestStreamRun_WriteFailureRequestsRunCancellation(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/message:stream", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	c.Response().Writer = newFailingResponseWriter()

	run := &AgentRun{ID: "run-1", Status: RunStatusRunning}
	err := h.streamRun(c, "proj-1", "task-1", "ctx-1", run,
		ExecuteRequest{ProjectID: "proj-1", UserMessage: "hi"}, false)
	require.NoError(t, err)

	joined := strings.Join(rootDriver.queriesCopy(), "\n")
	assert.Contains(t, joined, `UPDATE "kb"."agent_runs"`,
		"an abandoned stream must write the run row")
	assert.Contains(t, joined, `'cancelling'`,
		"an abandoned stream must record the durable cancel intent")
	assert.Contains(t, joined, `'run-1'`,
		"the cancel intent must target the executing run")
}

// TestStreamRun_DisconnectBeforeExecutionRequestsRunCancellation covers the
// other signal — the request context is already cancelled when the handler
// starts (the consumer vanished during setup). The run must not be executed at
// all: the cancel intent is recorded before the executor is reached.
func TestStreamRun_DisconnectBeforeExecutionRequestsRunCancellation(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/message:stream", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	rec := httptest.NewRecorder()
	c := e.NewContext(req.WithContext(ctx), rec)

	run := &AgentRun{ID: "run-1", Status: RunStatusRunning}
	err := h.streamRun(c, "proj-1", "task-1", "ctx-1", run,
		ExecuteRequest{ProjectID: "proj-1", UserMessage: "hi"}, false)
	require.NoError(t, err)

	joined := strings.Join(rootDriver.queriesCopy(), "\n")
	assert.Contains(t, joined, `'cancelling'`,
		"a disconnected consumer must record the durable cancel intent")
	assert.Contains(t, joined, `'run-1'`)
}
