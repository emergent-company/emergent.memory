package agents

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// assertSingleMember asserts a StreamResponse serializes with exactly one of the
// four A2A members and never a `kind` discriminator.
func assertSingleMember(t *testing.T, sr StreamResponse) {
	t.Helper()
	j := mustJSON(t, sr)
	assertNotContains(t, j, `"kind"`)

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(j), &m))
	var count int
	for _, k := range []string{"task", "message", "statusUpdate", "artifactUpdate"} {
		if _, ok := m[k]; ok {
			count++
		}
	}
	assert.Equal(t, 1, count, "StreamResponse must have exactly one member: %s", j)
}

// ============================================================================
// Event translation
// ============================================================================

func TestA2AStreamTranslator_TextDelta_StableArtifactID_GrowingPart(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")

	first := tr.translate(StreamEvent{Type: StreamEventTextDelta, Text: "Hel"})
	require.Len(t, first, 1)
	au := first[0].ArtifactUpdate
	require.NotNil(t, au)
	assert.Equal(t, "artifact-t1", au.Artifact.ArtifactID)
	assert.Len(t, au.Artifact.Parts, 1)
	require.NotNil(t, au.Artifact.Parts[0].Text)
	assert.Equal(t, "Hel", *au.Artifact.Parts[0].Text)
	assertSingleMember(t, first[0])

	second := tr.translate(StreamEvent{Type: StreamEventTextDelta, Text: "lo"})
	require.Len(t, second, 1)
	au2 := second[0].ArtifactUpdate
	require.NotNil(t, au2)
	assert.Equal(t, "artifact-t1", au2.Artifact.ArtifactID, "text artifactId must be stable across deltas")
	require.NotNil(t, au2.Artifact.Parts[0].Text)
	assert.Equal(t, "Hello", *au2.Artifact.Parts[0].Text, "text part must grow across deltas")
}

func TestA2AStreamTranslator_ThinkingDelta_NoStreamMember(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")

	resps := tr.translate(StreamEvent{Type: StreamEventThinking, Role: "reasoning", Text: "chain of thought"})
	assert.Empty(t, resps, "thinking deltas must never produce a stream member")
}

func TestA2AStreamTranslator_ThinkingFoldsIntoFinalMessageMetadata(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")
	_ = tr.translate(StreamEvent{Type: StreamEventThinking, Text: "thinking out loud"})
	_ = tr.translate(StreamEvent{Type: StreamEventTextDelta, Text: "the answer"})

	msg := tr.finalMessage(nil)
	require.NotNil(t, msg)
	assert.Equal(t, RoleAgent, msg.Role)
	require.NotNil(t, msg.Metadata)
	assert.Equal(t, "thinking out loud", msg.Metadata["thinking"])
}

func TestA2AStreamTranslator_ToolCall_DataPart(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")

	start := tr.translate(StreamEvent{Type: StreamEventToolCallStart, Tool: "search", Input: map[string]any{"q": "x"}})
	require.Len(t, start, 1)
	au := start[0].ArtifactUpdate
	require.NotNil(t, au)
	require.Len(t, au.Artifact.Parts, 1)
	data, ok := au.Artifact.Parts[0].Data.(map[string]any)
	require.True(t, ok, "tool call must map to a data part")
	assert.Equal(t, "search", data["toolName"])
	assert.Equal(t, map[string]any{"q": "x"}, data["input"])
	artID := au.Artifact.ArtifactID
	assertSingleMember(t, start[0])

	end := tr.translate(StreamEvent{Type: StreamEventToolCallEnd, Tool: "search", Output: map[string]any{"r": "y"}})
	require.Len(t, end, 1)
	au2 := end[0].ArtifactUpdate
	require.NotNil(t, au2)
	assert.Equal(t, artID, au2.Artifact.ArtifactID, "tool start/end must share an artifactId")
	data2, ok := au2.Artifact.Parts[0].Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "search", data2["toolName"])
	assert.Equal(t, map[string]any{"r": "y"}, data2["output"])
	assert.True(t, au2.Append, "tool end must append to the start artifact")
	assertSingleMember(t, end[0])
}

func TestA2AStreamTranslator_Error_FailedStatusUpdate(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")

	resps := tr.translate(StreamEvent{Type: StreamEventError, Error: "boom"})
	require.Len(t, resps, 1)
	su := resps[0].StatusUpdate
	require.NotNil(t, su)
	assert.Equal(t, TaskStateFailed, su.Status.State)
	require.NotNil(t, su.Status.Message)
	assert.Equal(t, RoleAgent, su.Status.Message.Role)
	assert.Equal(t, "boom", *su.Status.Message.Parts[0].Text)
	assertSingleMember(t, resps[0])
}

func TestA2AStreamTranslator_ToolApproval_NoStreamMember(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")
	resps := tr.translate(StreamEvent{Type: StreamEventToolApproval, Tool: "run", Input: map[string]any{}})
	assert.Empty(t, resps, "tool-approval gate is reported only as the terminal INPUT_REQUIRED statusUpdate")
}

// ============================================================================
// Terminal sequences
// ============================================================================

func TestA2AStreamTranslator_CompletedEvents_TerminalTaskCompleted(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")
	_ = tr.translate(StreamEvent{Type: StreamEventTextDelta, Text: "done"})

	latest := &AgentRun{ID: "t1", Status: RunStatusSuccess, ACPSessionID: strPtr("c1")}
	messages := []AgentRunMessage{{Role: "assistant", Content: map[string]any{"text": "done"}}}

	events := tr.completedEvents(messages, latest, nil)
	require.NotEmpty(t, events)

	last := events[len(events)-1]
	require.NotNil(t, last.Task, "terminal completed event must be a task")
	assert.Equal(t, TaskStateCompleted, last.Task.Status.State)

	for _, sr := range events {
		assertSingleMember(t, sr)
	}

	// A final ROLE_AGENT message is present.
	var sawMessage bool
	for _, sr := range events {
		if sr.Message != nil {
			sawMessage = true
			assert.Equal(t, RoleAgent, sr.Message.Role)
			assert.Equal(t, "done", *sr.Message.Parts[0].Text)
		}
	}
	assert.True(t, sawMessage, "completion must emit a final ROLE_AGENT message")
}

func TestA2AStreamTranslator_CompletedEvents_NoText_NoMessage(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")
	latest := &AgentRun{ID: "t1", Status: RunStatusSuccess}

	events := tr.completedEvents(nil, latest, nil)
	require.NotEmpty(t, events)
	for _, sr := range events {
		assert.Nil(t, sr.Message, "no text means no final message")
		assert.Nil(t, sr.ArtifactUpdate, "no streamed text means no final artifact")
	}
	assert.NotNil(t, events[len(events)-1].Task)
	assert.Equal(t, TaskStateCompleted, events[len(events)-1].Task.Status.State)
}

func TestA2AStreamTranslator_InputRequiredEvents_CarriesPrompt(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")

	events := tr.inputRequiredEvents("Approve this action?", nil)
	require.Len(t, events, 1)
	su := events[0].StatusUpdate
	require.NotNil(t, su)
	assert.Equal(t, TaskStateInputRequired, su.Status.State)
	require.NotNil(t, su.Status.Message)
	assert.Equal(t, RoleAgent, su.Status.Message.Role)
	assert.Equal(t, "Approve this action?", *su.Status.Message.Parts[0].Text)
	assert.Nil(t, su.Metadata, "no run → no HITL discriminator metadata")
	assertSingleMember(t, events[0])
}

func TestA2AStreamTranslator_InputRequiredEvents_AskUserMetadata(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")
	run := &AgentRun{ID: "t1", Status: RunStatusPaused, SuspendContext: map[string]any{"reason": "awaiting_human"}}

	events := tr.inputRequiredEvents("Which environment?", run)
	require.Len(t, events, 1)
	su := events[0].StatusUpdate
	require.NotNil(t, su)
	assert.Equal(t, TaskStateInputRequired, su.Status.State)
	require.NotNil(t, su.Metadata)
	assert.Equal(t, "ask_user", su.Metadata["pauseSource"])
}

func TestA2AStreamTranslator_InputRequiredEvents_ToolApprovalMetadata(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")
	run := &AgentRun{ID: "t1", Status: RunStatusPaused, SuspendContext: map[string]any{"reason": "awaiting_tool_confirm"}}

	events := tr.inputRequiredEvents("Approve run_shell?", run)
	require.Len(t, events, 1)
	su := events[0].StatusUpdate
	require.NotNil(t, su)
	assert.Equal(t, TaskStateInputRequired, su.Status.State)
	require.NotNil(t, su.Metadata)
	assert.Equal(t, "tool_approval", su.Metadata["pauseSource"])
}

func TestA2AStreamTranslator_FailedEvents_CancelledEvents(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")

	failed := tr.failedEvents("boom")
	require.Len(t, failed, 1)
	assert.Equal(t, TaskStateFailed, failed[0].StatusUpdate.Status.State)

	cancelled := tr.cancelledEvents()
	require.Len(t, cancelled, 1)
	assert.Equal(t, TaskStateCanceled, cancelled[0].StatusUpdate.Status.State)
}

// ============================================================================
// Ordering: SUBMITTED task → WORKING statusUpdate → terminal
// ============================================================================

func TestA2AStream_Ordering_SubmittedThenWorkingThenTerminal(t *testing.T) {
	// Reproduce the new-task stream sequence using the same helpers the handler
	// uses, then assert member order.
	run := &AgentRun{ID: "t1", Status: RunStatusQueued}
	tr := newA2aStreamTranslator("t1", "c1")

	var seq []StreamResponse

	// 1. initial SUBMITTED task
	task := RunToA2ATask(run, nil, nil, nil)
	task.ID = "t1"
	task.ContextID = "c1"
	seq = append(seq, StreamResponse{Task: &task})

	// 2. WORKING statusUpdate
	seq = append(seq, a2aStatusUpdate("t1", "c1", TaskStateWorking, ""))

	// deltas in between (order preserved)
	seq = append(seq, tr.translate(StreamEvent{Type: StreamEventTextDelta, Text: "hi"})...)

	// 3. terminal COMPLETED
	seq = append(seq, tr.completedEvents(nil, &AgentRun{ID: "t1", Status: RunStatusSuccess}, nil)...)

	require.GreaterOrEqual(t, len(seq), 3)

	// [0] SUBMITTED task
	require.NotNil(t, seq[0].Task)
	assert.Equal(t, TaskStateSubmitted, seq[0].Task.Status.State)

	// [1] WORKING statusUpdate
	require.NotNil(t, seq[1].StatusUpdate)
	assert.Equal(t, TaskStateWorking, seq[1].StatusUpdate.Status.State)

	// last is terminal COMPLETED task
	last := seq[len(seq)-1]
	require.NotNil(t, last.Task)
	assert.Equal(t, TaskStateCompleted, last.Task.Status.State)

	// Ordering: SUBMITTED at index 0, WORKING at index 1, terminal at the end.
	assert.Less(t, 1, len(seq)-1, "terminal must come after the WORKING transition")
}

// ============================================================================
// Handler validation / error paths (nil repo)
// ============================================================================

func TestA2AStreamMessage_NoAuth_Returns401(t *testing.T) {
	h := newTestA2AHandler()
	c, _ := newA2AMessageContext(http.MethodPost, "/message:stream", `{"message":{"parts":[{"text":"hi"}]}}`, false)

	err := h.StreamMessage(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestA2AStreamMessage_UnsupportedVersion_ReturnsJSONErrorNotSSE(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodPost, "/message:stream", `{"message":{"parts":[{"text":"hi"}]}}`, true)
	c.Request().Header.Set(A2AVersionHeader, "2.0")

	require.NoError(t, h.StreamMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.NotContains(t, rec.Header().Get("Content-Type"), "text/event-stream", "version rejection must not start an SSE stream")
	env := mustA2AErr(t, rec)
	assert.Equal(t, "VERSION_NOT_SUPPORTED", env.Error.Details[0].Reason)
}

func TestA2AStreamMessage_EmptyParts_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodPost, "/message:stream", `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[]}}`, true)

	require.NoError(t, h.StreamMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_ARGUMENT", mustA2AErr(t, rec).Error.Details[0].Reason)
}

func TestA2AStreamMessage_NoTextPart_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	body := `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"data":{"toolName":"search"}}]}}`
	c, rec := newA2AMessageContext(http.MethodPost, "/message:stream", body, true)

	require.NoError(t, h.StreamMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_ARGUMENT", mustA2AErr(t, rec).Error.Details[0].Reason)
}

func TestA2AStreamMessage_InvalidBody_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodPost, "/message:stream", `not-json`, true)

	require.NoError(t, h.StreamMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestA2AStreamMessage_ValidBody_ReachesRepo_Panics(t *testing.T) {
	h := newTestA2AHandler()
	body := `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hello"}]}}`
	c, _ := newA2AMessageContext(http.MethodPost, "/message:stream", body, true)

	assertPanics(t, func() { _ = h.StreamMessage(c) })
}

func TestA2AStreamMessage_WithSkillID_ReachesRepo_Panics(t *testing.T) {
	h := newTestA2AHandler()
	body := `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hello"}],"metadata":{"skillId":"my-agent"}}}`
	c, _ := newA2AMessageContext(http.MethodPost, "/message:stream", body, true)

	assertPanics(t, func() { _ = h.StreamMessage(c) })
}

func TestA2AStreamMessage_MissingMessageID_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AMessageContext(http.MethodPost, "/message:stream", `{"message":{"role":"ROLE_USER","parts":[{"text":"hi"}]}}`, true)

	require.NoError(t, h.StreamMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_ARGUMENT", mustA2AErr(t, rec).Error.Details[0].Reason)
}

func TestA2AStreamMessage_MissingRole_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	body := `{"message":{"messageId":"m1","role":"ROLE_UNSPECIFIED","parts":[{"text":"hi"}]}}`
	c, rec := newA2AMessageContext(http.MethodPost, "/message:stream", body, true)

	require.NoError(t, h.StreamMessage(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "INVALID_ARGUMENT", mustA2AErr(t, rec).Error.Details[0].Reason)
}

// ============================================================================
// [B] final message id · [K] initial SUBMITTED task · [L] payload round-trip
// ============================================================================

func TestA2AStreamTranslator_FinalMessage_HasMessageID(t *testing.T) {
	tr := newA2aStreamTranslator("t1", "c1")
	_ = tr.translate(StreamEvent{Type: StreamEventTextDelta, Text: "the answer"})

	msg := tr.finalMessage(nil)
	require.NotNil(t, msg)
	assert.NotEmpty(t, msg.MessageID, "generated final message must carry a non-empty messageId")
	assert.Equal(t, RoleAgent, msg.Role)
}

func TestA2AInitialTask_ForcesSubmitted(t *testing.T) {
	run := &AgentRun{ID: "run-1", Status: RunStatusRunning}

	task := a2aInitialTask(run, "task-1", "ctx-1")

	assert.Equal(t, "task-1", task.ID)
	assert.Equal(t, "ctx-1", task.ContextID)
	assert.Equal(t, TaskStateSubmitted, task.Status.State,
		"the initial streamed task must be SUBMITTED even though the internal run is WORKING")
}

func TestA2AStreamPayload_RoundTripsThroughEventData(t *testing.T) {
	sr := StreamResponse{
		ArtifactUpdate: &TaskArtifactUpdateEvent{
			TaskID:    "t1",
			ContextID: "c1",
			Artifact:  Artifact{ArtifactID: "artifact-t1", Parts: []Part{TextPart("hi")}},
		},
	}

	payload := a2aStreamPayload(sr)
	require.NotNil(t, payload)
	require.NotNil(t, payload["stream"])

	// Simulate the jsonb round-trip: marshal to JSON, decode back into
	// map[string]any (what bun returns from the acp_run_events.data column).
	j := mustJSON(t, payload)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(j), &decoded))

	got, ok := a2aStreamResponseFromEventData(decoded, "t1", "c1")
	require.True(t, ok)
	require.NotNil(t, got.ArtifactUpdate)
	assert.Equal(t, "artifact-t1", got.ArtifactUpdate.Artifact.ArtifactID)
	require.Len(t, got.ArtifactUpdate.Artifact.Parts, 1)
	assert.Equal(t, "hi", *got.ArtifactUpdate.Artifact.Parts[0].Text)
}

func TestA2AStreamResponseFromEventData_NoPayloadFallsBack(t *testing.T) {
	// A lifecycle-only event (no "stream" key) must return ok=false so callers
	// fall back to a2aStreamResponseFromEventType.
	_, ok := a2aStreamResponseFromEventData(map[string]any{"type": ACPEventRunCompleted}, "t1", "c1")
	assert.False(t, ok)

	_, ok = a2aStreamResponseFromEventData(nil, "t1", "c1")
	assert.False(t, ok)

	_, ok = a2aStreamResponseFromEventData(map[string]any{"stream": nil}, "t1", "c1")
	assert.False(t, ok)
}

func TestA2AStreamResponseFromEventData_StatusUpdatePayload(t *testing.T) {
	sr := StreamResponse{
		StatusUpdate: &TaskStatusUpdateEvent{
			TaskID:    "t1",
			ContextID: "c1",
			Status:    TaskStatus{State: TaskStateWorking},
		},
	}
	payload := a2aStreamPayload(sr)
	j := mustJSON(t, payload)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(j), &decoded))

	got, ok := a2aStreamResponseFromEventData(decoded, "t1", "c1")
	require.True(t, ok)
	require.NotNil(t, got.StatusUpdate)
	assert.Equal(t, TaskStateWorking, got.StatusUpdate.Status.State)
	assert.False(t, isTerminalTaskState(got.StatusUpdate.Status.State))
}
