package agents

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// A2A turn lifecycle (#866/#867/#868) — server half.
//
// #867: a task that paused for a reason which writes no question row (step
// limit, watchdog) must accept a follow-up as a continuation. Rejecting it with
// "no pending question for task" strands a task the client has already latched
// from its INPUT_REQUIRED status, and the pause is unrecoverable.
//
// #868: an A2A stream whose consumer went away is a cancel, not a slow client.
// The handler must record the durable cancel intent for the run that is
// actually executing, and must never flip a paused row to "cancelling" — that
// would strand the resume #867 is about.
// =============================================================================

func TestA2AResumeMessage(t *testing.T) {
	const userMessage = "keep going, use the staging bucket"

	t.Run("questionless pause passes the user message through", func(t *testing.T) {
		assert.Equal(t, userMessage, a2aResumeMessage(nil, userMessage),
			"a step-limit pause asked nothing; the user's message IS the continuation")
	})

	t.Run("answered question is quoted against the prompt", func(t *testing.T) {
		got := a2aResumeMessage(&AgentQuestion{Question: "which bucket?"}, userMessage)
		assert.Contains(t, got, `which bucket?`)
		assert.Contains(t, got, userMessage)
		assert.NotEqual(t, userMessage, got,
			"an answered question must be replayed so the model sees what it asked")
	})
}

// TestStreamResumeTask_PausedWithoutQuestion_IsContinuation reproduces #867 on
// the SSE path: the paused run has no pending question, so the follow-up must
// reach the executor instead of being rejected as an empty question set.
func TestStreamResumeTask_PausedWithoutQuestion_IsContinuation(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setQuestionsMissing(true)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	c, rec := newStreamContext(http.MethodPost, "/message:stream")
	err := h.streamResumeTask(c, "proj-1", "user-1", "keep going", "task-1", "")
	require.NoError(t, err)

	body := rec.Body.String()
	assert.Contains(t, body, ErrExecutionDisabled.Error(),
		"the resume must reach the executor kill switch, not the question gate")
	assert.NotContains(t, body, "no pending question for task",
		"a paused run with no question is a continuation (#867)")
	for _, q := range drv.queriesCopy() {
		assert.NotContains(t, q, `UPDATE "kb"."agent_questions"`,
			"a questionless continuation has no question to claim")
	}
}

// TestResumeA2ATask_PausedWithoutQuestion_IsContinuation is the JSON /message
// variant of the same defect: both surfaces share the shape, so both must accept
// the continuation.
func TestResumeA2ATask_PausedWithoutQuestion_IsContinuation(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setQuestionsMissing(true)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	c, rec := newStreamContext(http.MethodPost, "/message:send")
	err := h.resumeA2ATask(c, "proj-1", "user-1", "keep going", "task-1", "", nil)
	require.NoError(t, err)

	body := rec.Body.String()
	assert.Contains(t, body, ErrExecutionDisabled.Error(),
		"the resume must reach the executor kill switch, not the question gate")
	assert.NotContains(t, body, "no pending question for task",
		"a paused run with no question is a continuation (#867)")
	for _, q := range drv.queriesCopy() {
		assert.NotContains(t, q, `UPDATE "kb"."agent_questions"`)
	}
}

// TestStreamResumeTask_QuestionLoadError_IsNotAnEmptyQuestionSet pins that a
// failed question read is reported as a read failure. Before the fix the error
// and the empty set shared one branch, so a transient DB error told the client
// its task had no question — a state that reads as "give up".
func TestStreamResumeTask_QuestionLoadError_IsNotAnEmptyQuestionSet(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setQuestionsError(true)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	c, rec := newStreamContext(http.MethodPost, "/message:stream")
	err := h.streamResumeTask(c, "proj-1", "user-1", "keep going", "task-1", "")
	require.NoError(t, err)

	body := rec.Body.String()
	assert.Contains(t, body, "failed to load pending questions")
	assert.NotContains(t, body, "no pending question for task")
}

// deadConsumerWriter is an http.ResponseWriter whose peer is gone: every body
// write fails the way a write to a closed SSE socket does. Start() still
// succeeds because it only sets headers and (with no Flusher here) returns.
type deadConsumerWriter struct {
	header http.Header
	status int
}

func (w *deadConsumerWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *deadConsumerWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func (w *deadConsumerWriter) WriteHeader(status int) { w.status = status }

// TestStreamRun_DeadConsumerCancelsRun reproduces #868: the SSE writes start
// failing, and the handler must record the durable cancel intent for the run it
// is streaming instead of running to completion for nobody.
func TestStreamRun_DeadConsumerCancelsRun(t *testing.T) {
	rootDriver.reset()
	repo := newRootCaptureRepository(t)
	h := &A2AHandler{repo: repo, executor: disabledExecutor(), log: testAgentLogger()}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/message:stream", nil)
	c := e.NewContext(req, &deadConsumerWriter{})
	run := &AgentRun{ID: "run-1", Status: RunStatusRunning}

	err := h.streamRun(c, "proj-1", "task-1", "ctx-1", run,
		ExecuteRequest{ProjectID: "proj-1", UserMessage: "hi"}, false)
	require.NoError(t, err)

	var cancelStmt string
	for _, q := range rootDriver.queriesCopy() {
		if strings.Contains(q, `UPDATE "kb"."agent_runs"`) && strings.Contains(q, `SET status = 'cancelling'`) {
			cancelStmt = q
		}
	}
	require.NotEmpty(t, cancelStmt,
		"a dead consumer must record a cancel intent, got queries:\n%s",
		strings.Join(rootDriver.queriesCopy(), "\n"))
	assert.Contains(t, cancelStmt, `'run-1'`, "the cancel must target the streamed run")
	assert.NotContains(t, cancelStmt, "'"+string(RunStatusPaused)+"'",
		"a paused run has already stopped; cancelling it would strand the resume (#867)")
}

// TestCancellableWhileStreamingStatuses_ExcludesPaused pins the status list the
// abandoned-stream cancel is allowed to touch. A paused row has already stopped
// and is waiting for the resume #867 is about; flipping it to "cancelling" parks
// the task in a state no resume accepts. The explicit-cancel path
// (RequestRunCancellation) does include paused on purpose, which is exactly why
// the abandoned-stream path needs its own narrower list.
func TestCancellableWhileStreamingStatuses_ExcludesPaused(t *testing.T) {
	statuses := cancellableWhileStreamingStatuses()
	assert.Contains(t, statuses, string(RunStatusQueued))
	assert.Contains(t, statuses, string(RunStatusRunning))
	assert.Contains(t, statuses, string(RunStatusCancelling))
	assert.NotContains(t, statuses, string(RunStatusPaused))
	assert.NotContains(t, statuses, string(RunStatusSuccess))
	assert.NotContains(t, statuses, string(RunStatusError))
}
