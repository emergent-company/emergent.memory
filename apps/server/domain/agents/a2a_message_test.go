package agents

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// newA2AMessageContext builds an Echo context with an optional body and optional
// authenticated project user.
func newA2AMessageContext(method, path, body string, authed bool) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if authed {
		c.Set(string(auth.UserContextKey), &auth.AuthUser{
			ID:        "user-test-id",
			Email:     "test@example.com",
			ProjectID: "proj-test-id",
		})
	}
	return c, rec
}

// mustA2AErr decodes an A2A error envelope from a recorder body.
func mustA2AErr(t *testing.T, rec *httptest.ResponseRecorder) A2AErrorEnvelope {
	t.Helper()
	var env A2AErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return env
}

// assertPanics runs fn and asserts it panicked (nil repo reached).
func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		fn()
	}()
	assert.True(t, panicked, "expected panic from nil repo after validation passed")
}

// ============================================================================
// Pure helpers
// ============================================================================

func TestA2AUserMessageFromParts_ConcatenatesText(t *testing.T) {
	parts := []Part{
		{Text: strPtr("Hello ")},
		{Text: strPtr("World")},
	}
	assert.Equal(t, "Hello World", a2aUserMessageFromParts(parts))
}

func TestA2AUserMessageFromParts_IgnoresNonText(t *testing.T) {
	parts := []Part{
		{Text: strPtr("Hello")},
		{Data: map[string]any{"toolName": "search"}},
		{Text: strPtr("!")},
	}
	assert.Equal(t, "Hello!", a2aUserMessageFromParts(parts))
}

func TestA2AUserMessageFromParts_Empty(t *testing.T) {
	assert.Equal(t, "", a2aUserMessageFromParts(nil))
	assert.Equal(t, "", a2aUserMessageFromParts([]Part{{Data: map[string]any{"x": 1}}}))
}

func TestA2AIsTerminalRunStatus(t *testing.T) {
	for status, terminal := range map[AgentRunStatus]bool{
		RunStatusSuccess:    true,
		RunStatusSkipped:    true,
		RunStatusError:      true,
		RunStatusCancelled:  true,
		RunStatusQueued:     false,
		RunStatusRunning:    false,
		RunStatusPaused:     false,
		RunStatusCancelling: false,
	} {
		assert.Equal(t, terminal, isTerminalRunStatus(status), "status %q", status)
	}
}

func TestA2AHITLMetadata_AskUser(t *testing.T) {
	run := &AgentRun{Status: RunStatusPaused, SuspendContext: map[string]any{"reason": "awaiting_human"}}
	md := hitlMetadata(run)
	require.NotNil(t, md)
	assert.Equal(t, "ask_user", md["pauseSource"])
	assert.Equal(t, true, md["inputRequired"])
}

func TestA2AHITLMetadata_ToolApproval(t *testing.T) {
	run := &AgentRun{Status: RunStatusPaused, SuspendContext: map[string]any{"reason": "awaiting_tool_confirm"}}
	md := hitlMetadata(run)
	require.NotNil(t, md)
	assert.Equal(t, "tool_approval", md["pauseSource"])
}

func TestA2AHITLMetadata_NonPausedNil(t *testing.T) {
	assert.Nil(t, hitlMetadata(&AgentRun{Status: RunStatusRunning, SuspendContext: map[string]any{"reason": "awaiting_human"}}))
	assert.Nil(t, hitlMetadata(nil))
}

func TestA2AInternalStatusesForTaskState(t *testing.T) {
	cases := map[TaskState][]AgentRunStatus{
		TaskStateSubmitted:     {RunStatusQueued},
		TaskStateWorking:       {RunStatusRunning, RunStatusCancelling},
		TaskStateCompleted:     {RunStatusSuccess, RunStatusSkipped},
		TaskStateFailed:        {RunStatusError},
		TaskStateInputRequired: {RunStatusPaused},
		TaskStateCanceled:      {RunStatusCancelled},
	}
	for state, want := range cases {
		assert.Equal(t, want, internalStatusesForTaskState(state), "state %q", state)
	}
	assert.Nil(t, internalStatusesForTaskState(TaskStateUnspecified))
	assert.Nil(t, internalStatusesForTaskState(TaskState("bogus")))
}

func TestA2APageSizeBounds(t *testing.T) {
	assert.Equal(t, 50, a2aPageSize(0))
	assert.Equal(t, 50, a2aPageSize(-5))
	assert.Equal(t, 50, a2aPageSize(50))
	assert.Equal(t, 100, a2aPageSize(100))
	assert.Equal(t, 100, a2aPageSize(500))
}

func TestA2APageTokenRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 50, 12345} {
		assert.Equal(t, n, a2aDecodePageToken(a2aEncodePageToken(n)))
	}
	assert.Equal(t, 0, a2aDecodePageToken(""))
	assert.Equal(t, 0, a2aDecodePageToken("not-base64!!!"))
}

func TestA2AEventTypeToTaskState(t *testing.T) {
	cases := map[string]TaskState{
		ACPEventRunCreated:    TaskStateSubmitted,
		ACPEventRunInProgress: TaskStateWorking,
		ACPEventRunAwaiting:   TaskStateInputRequired,
		ACPEventRunCompleted:  TaskStateCompleted,
		ACPEventRunFailed:     TaskStateFailed,
		ACPEventRunCancelled:  TaskStateCanceled,
	}
	for eventType, want := range cases {
		got, ok := a2aEventTypeToTaskState(eventType)
		assert.True(t, ok, "event type %q", eventType)
		assert.Equal(t, want, got)
	}
	_, ok := a2aEventTypeToTaskState(ACPEventMessagePart)
	assert.False(t, ok)
}

// ============================================================================
// buildA2ATask (stable task id + history truncation)
// ============================================================================

func TestBuildA2ATask_PreservesOriginalTaskID(t *testing.T) {
	latest := &AgentRun{ID: "resume-run-2", Status: RunStatusSuccess, ACPSessionID: strPtr("ctx-1")}
	messages := []AgentRunMessage{
		{Role: "assistant", Content: map[string]any{"text": "final answer"}},
	}

	task := buildA2ATask(latest, messages, nil, "original-run-1", -1)

	assert.Equal(t, "original-run-1", task.ID, "Task.id must be the original task id, never the resume run id")
	assert.Equal(t, "ctx-1", task.ContextID)
	assert.Equal(t, TaskStateCompleted, task.Status.State)

	j := mustJSON(t, task)
	assert.NotContains(t, j, "resume-run-2")
	assert.NotContains(t, j, "resume_run_id")
}

func TestBuildA2ATask_HistoryLengthTruncation(t *testing.T) {
	latest := &AgentRun{ID: "r1", Status: RunStatusSuccess}
	messages := []AgentRunMessage{
		{Role: "user", Content: map[string]any{"text": "a"}},
		{Role: "assistant", Content: map[string]any{"text": "b"}},
		{Role: "user", Content: map[string]any{"text": "c"}},
		{Role: "assistant", Content: map[string]any{"text": "d"}},
	}

	task := buildA2ATask(latest, messages, nil, "r1", 2)
	assert.Len(t, task.History, 2)
	assert.Equal(t, "c", *task.History[0].Parts[0].Text)
	assert.Equal(t, "d", *task.History[1].Parts[0].Text)
}

func TestBuildA2ATask_InputRequiredCarriesQuestionAndMetadata(t *testing.T) {
	latest := &AgentRun{
		ID:             "r1",
		Status:         RunStatusPaused,
		SuspendContext: map[string]any{"reason": "awaiting_human"},
	}
	question := &AgentQuestion{Question: "Approve this?"}

	task := buildA2ATask(latest, nil, question, "r1", -1)

	assert.Equal(t, TaskStateInputRequired, task.Status.State)
	require.NotNil(t, task.Status.Message)
	assert.Equal(t, "Approve this?", *task.Status.Message.Parts[0].Text)
	require.NotNil(t, task.Metadata)
	assert.Equal(t, "ask_user", task.Metadata["pauseSource"])
}

// ============================================================================
// Handler validation / auth / error paths (nil repo)
// ============================================================================

func TestA2ASendMessage_NoAuth_Returns401(t *testing.T) {
	h := newTestA2AHandler()
	c, _ := newA2AMessageContext(http.MethodPost, "/message:send", `{"message":{"parts":[{"text":"hi"}]}}`, false)

	err := h.SendMessage(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestA2ASendMessage_UnsupportedVersion_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodPost, "/message:send", `{"message":{"parts":[{"text":"hi"}]}}`, true)
	c.Request().Header.Set(A2AVersionHeader, "2.0")

	require.NoError(t, h.SendMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	env := mustA2AErr(t, rec)
	assert.Equal(t, "VERSION_NOT_SUPPORTED", env.Error.Details[0].Reason)
}

func TestA2ASendMessage_EmptyParts_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodPost, "/message:send", `{"message":{"role":"user","parts":[]}}`, true)

	require.NoError(t, h.SendMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	env := mustA2AErr(t, rec)
	assert.Equal(t, "INVALID_ARGUMENT", env.Error.Details[0].Reason)
	assert.NotEqual(t, "INVALID_AGENT_RESPONSE", env.Error.Details[0].Reason)
}

func TestA2ASendMessage_NoTextPart_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	body := `{"message":{"role":"user","parts":[{"data":{"toolName":"search"}}]}}`
	c, rec := newA2AMessageContext(http.MethodPost, "/message:send", body, true)

	require.NoError(t, h.SendMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_ARGUMENT", mustA2AErr(t, rec).Error.Details[0].Reason)
}

func TestA2ASendMessage_InvalidBody_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodPost, "/message:send", `not-json`, true)

	require.NoError(t, h.SendMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestA2ASendMessage_ValidBody_ReachesRepo_Panics(t *testing.T) {
	h := newTestA2AHandler()
	body := `{"message":{"role":"user","parts":[{"text":"hello"}]}}`
	c, _ := newA2AMessageContext(http.MethodPost, "/message:send", body, true)

	assertPanics(t, func() { _ = h.SendMessage(c) })
}

func TestA2AGetTask_NoAuth_Returns401(t *testing.T) {
	h := newTestA2AHandler()
	c, _ := newA2AMessageContext(http.MethodGet, "/tasks/t1", "", false)
	c.SetParamNames("id")
	c.SetParamValues("t1")

	err := h.GetTask(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestA2AGetTask_MissingID_Returns404(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodGet, "/tasks/", "", true)
	c.SetParamNames("id")
	c.SetParamValues("")

	require.NoError(t, h.GetTask(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "TASK_NOT_FOUND", mustA2AErr(t, rec).Error.Details[0].Reason)
}

func TestA2AGetTask_ValidID_ReachesRepo_Panics(t *testing.T) {
	h := newTestA2AHandler()
	c, _ := newA2AMessageContext(http.MethodGet, "/tasks/t1", "", true)
	c.SetParamNames("id")
	c.SetParamValues("t1")

	assertPanics(t, func() { _ = h.GetTask(c) })
}

func TestA2AListTasks_NoAuth_Returns401(t *testing.T) {
	h := newTestA2AHandler()
	c, _ := newA2AMessageContext(http.MethodGet, "/tasks", "", false)

	err := h.ListTasks(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestA2AListTasks_UnknownStatus_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodGet, "/tasks?status=TASK_STATE_BOGUS", "", true)

	require.NoError(t, h.ListTasks(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestA2ACancelTask_NoAuth_Returns401(t *testing.T) {
	h := newTestA2AHandler()
	c, _ := newA2AMessageContext(http.MethodPost, "/tasks/t1:cancel", "", false)
	c.SetParamNames("id")
	c.SetParamValues("t1")

	err := h.CancelTask(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestA2ACancelTask_MissingID_Returns404(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodPost, "/tasks/:cancel", "", true)
	c.SetParamNames("id")
	c.SetParamValues("")

	require.NoError(t, h.CancelTask(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "TASK_NOT_FOUND", mustA2AErr(t, rec).Error.Details[0].Reason)
}

func TestA2ASubscribeTask_NoAuth_Returns401(t *testing.T) {
	h := newTestA2AHandler()
	c, _ := newA2AMessageContext(http.MethodPost, "/tasks/t1:subscribe", "", false)
	c.SetParamNames("id")
	c.SetParamValues("t1")

	err := h.SubscribeTask(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestA2ASubscribeTask_MissingID_Returns404(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodPost, "/tasks/:subscribe", "", true)
	c.SetParamNames("id")
	c.SetParamValues("")

	require.NoError(t, h.SubscribeTask(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "TASK_NOT_FOUND", mustA2AErr(t, rec).Error.Details[0].Reason)
}

// ============================================================================
// Skill-id routing (message.metadata["skillId"])
// ============================================================================

func TestA2ASkillIDFromMetadata_CanonicalKey(t *testing.T) {
	assert.Equal(t, "my-agent", a2aSkillIDFromMetadata(map[string]any{"skillId": "my-agent"}))
}

func TestA2ASkillIDFromMetadata_SnakeCaseFallback(t *testing.T) {
	assert.Equal(t, "my-agent", a2aSkillIDFromMetadata(map[string]any{"skill_id": "my-agent"}))
}

func TestA2ASkillIDFromMetadata_CanonicalWins(t *testing.T) {
	assert.Equal(t, "canonical", a2aSkillIDFromMetadata(map[string]any{
		"skillId":  "canonical",
		"skill_id": "snake",
	}))
}

func TestA2ASkillIDFromMetadata_AbsentOrNonString(t *testing.T) {
	assert.Equal(t, "", a2aSkillIDFromMetadata(nil))
	assert.Equal(t, "", a2aSkillIDFromMetadata(map[string]any{}))
	assert.Equal(t, "", a2aSkillIDFromMetadata(map[string]any{"other": "x"}))
	assert.Equal(t, "", a2aSkillIDFromMetadata(map[string]any{"skillId": 42}))
	assert.Equal(t, "", a2aSkillIDFromMetadata(map[string]any{"skillId": ""}))
}

func TestA2ASkillNotFoundError_Shape(t *testing.T) {
	err := a2aSkillNotFoundError("nope")
	assert.Equal(t, A2AErrorCode(http.StatusBadRequest), err.Code)
	assert.Equal(t, a2aReasonSkillNotFound, err.Reason)
	assert.Contains(t, err.Message, "nope")

	env := err.Envelope()
	assert.Equal(t, "SKILL_NOT_FOUND", env.Error.Details[0].Reason)
}

func TestA2ASendMessage_WithSkillID_ReachesRepo_Panics(t *testing.T) {
	// A message carrying message.metadata["skillId"] routes through skill-id
	// resolution (resolveA2AAgentBySkillID → repo) rather than the assistant
	// fallback. With a nil repo this reaches the first DB call and panics.
	h := newTestA2AHandler()
	body := `{"message":{"role":"user","parts":[{"text":"hello"}],"metadata":{"skillId":"my-agent"}}}`
	c, _ := newA2AMessageContext(http.MethodPost, "/message:send", body, true)

	assertPanics(t, func() { _ = h.SendMessage(c) })
}

func TestA2ASendMessage_WithoutSkillID_StillReachesRepo_Panics(t *testing.T) {
	// No metadata skill id → assistant fallback path; still reaches repo.
	h := newTestA2AHandler()
	body := `{"message":{"role":"user","parts":[{"text":"hello"}]}}`
	c, _ := newA2AMessageContext(http.MethodPost, "/message:send", body, true)

	assertPanics(t, func() { _ = h.SendMessage(c) })
}
