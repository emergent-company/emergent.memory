package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validA2ATaskStates is the set of valid A2A TaskState values.
var validA2ATaskStates = map[TaskState]bool{
	TaskStateUnspecified:   true,
	TaskStateSubmitted:     true,
	TaskStateWorking:       true,
	TaskStateCompleted:     true,
	TaskStateFailed:        true,
	TaskStateCanceled:      true,
	TaskStateInputRequired: true,
	TaskStateRejected:      true,
	TaskStateAuthRequired:  true,
}

func TestMapRunStatusToTaskState_EveryInternalStatus(t *testing.T) {
	cases := []struct {
		status AgentRunStatus
		want   TaskState
	}{
		{RunStatusQueued, TaskStateSubmitted},
		{RunStatusRunning, TaskStateWorking},
		{RunStatusSuccess, TaskStateCompleted},
		{RunStatusSkipped, TaskStateCompleted},
		{RunStatusError, TaskStateFailed},
		{RunStatusPaused, TaskStateInputRequired},
		{RunStatusCancelled, TaskStateCanceled},
		{RunStatusCancelling, TaskStateWorking},
	}

	for _, tc := range cases {
		got := MapRunStatusToTaskState(tc.status)
		assert.Equal(t, tc.want, got, "status %q", tc.status)

		// Every output must be a valid A2A TaskState…
		assert.True(t, validA2ATaskStates[got], "output %q for %q is not a valid A2A TaskState", got, tc.status)

		// …and no internal status string may leak onto the wire.
		assert.True(t, strings.HasPrefix(string(got), "TASK_STATE_"), "output %q for %q leaks internal vocabulary", got, tc.status)
		assert.NotEqual(t, string(tc.status), string(got), "output %q must not equal internal status %q", got, tc.status)
	}
}

func TestMapRunStatusToTaskState_UnknownReturnsUnspecified(t *testing.T) {
	assert.Equal(t, TaskStateUnspecified, MapRunStatusToTaskState(AgentRunStatus("bogus")))
}

func TestAgentRunMessageToA2AMessage_AssistantText(t *testing.T) {
	msg := &AgentRunMessage{
		ID:      "m-1",
		Role:    "assistant",
		Content: map[string]any{"text": "hello there"},
	}
	m := AgentRunMessageToA2AMessage(msg)
	require.NotNil(t, m)
	assert.Equal(t, "m-1", m.MessageID)
	assert.Equal(t, RoleAgent, m.Role)
	require.Len(t, m.Parts, 1)
	assert.NotNil(t, m.Parts[0].Text)
	assert.Equal(t, "hello there", *m.Parts[0].Text)
	assert.Nil(t, m.Parts[0].Data)
}

func TestAgentRunMessageToA2AMessage_UserText(t *testing.T) {
	msg := &AgentRunMessage{
		ID:      "m-2",
		Role:    "user",
		Content: map[string]any{"text": "do it"},
	}
	m := AgentRunMessageToA2AMessage(msg)
	require.NotNil(t, m)
	assert.Equal(t, RoleUser, m.Role)
	require.Len(t, m.Parts, 1)
	assert.Equal(t, "do it", *m.Parts[0].Text)
}

func TestAgentRunMessageToA2AMessage_SystemAndToolResultMapToAgent(t *testing.T) {
	for _, role := range []string{"system", "tool_result"} {
		msg := &AgentRunMessage{Role: role, Content: map[string]any{"text": "x"}}
		m := AgentRunMessageToA2AMessage(msg)
		require.NotNil(t, m)
		assert.Equal(t, RoleAgent, m.Role, "role %q should map to ROLE_AGENT", role)
	}
}

func TestToolCallDataPart(t *testing.T) {
	tc := &AgentRunToolCall{
		ToolName: "search",
		Input:    map[string]any{"q": "memory"},
		Output:   map[string]any{"results": []any{"a", "b"}},
	}
	p := ToolCallDataPart(tc)
	assert.Nil(t, p.Text)
	assert.NotNil(t, p.Data)

	data, ok := p.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "search", data["toolName"])

	// Marshal and verify the tool name + input/output land in the `data` member.
	j, err := json.Marshal(p)
	require.NoError(t, err)
	assert.Contains(t, string(j), `"toolName":"search"`)
	assert.Contains(t, string(j), `"data"`)
	assert.NotContains(t, string(j), `"kind"`)
}

func TestRunToA2ATask_StableID_NoResumeRunID(t *testing.T) {
	run := &AgentRun{
		ID:           "run-123",
		Status:       RunStatusSuccess,
		ACPSessionID: strPtr("session-456"),
	}
	messages := []AgentRunMessage{
		{Role: "user", Content: map[string]any{"text": "question"}},
		{Role: "assistant", Content: map[string]any{"text": "final answer"}},
	}

	task := RunToA2ATask(run, messages, nil, nil)

	assert.Equal(t, "run-123", task.ID, "Task.id must be the stable run id")
	assert.Equal(t, "session-456", task.ContextID)
	assert.Equal(t, TaskStateCompleted, task.Status.State)
	require.Len(t, task.Artifacts, 1)
	assert.Equal(t, "final answer", *task.Artifacts[0].Parts[0].Text)
	assert.Len(t, task.History, 2)

	// The internal resume_run_id must never be exposed on the wire.
	j, err := json.Marshal(task)
	require.NoError(t, err)
	assert.NotContains(t, string(j), "resume_run_id")
	assert.NotContains(t, string(j), "resumeRunId")
	assert.NotContains(t, string(j), "ResumeRunID")
}

func TestRunToA2ATask_FailedCarriesErrorMessage(t *testing.T) {
	run := &AgentRun{
		ID:           "run-fail",
		Status:       RunStatusError,
		ErrorMessage: strPtr("boom"),
	}
	task := RunToA2ATask(run, nil, nil, nil)
	assert.Equal(t, TaskStateFailed, task.Status.State)
	require.NotNil(t, task.Status.Message)
	require.Len(t, task.Status.Message.Parts, 1)
	assert.Equal(t, "boom", *task.Status.Message.Parts[0].Text)
}

func TestRunToA2ATask_SkippedCarriesSkipReason(t *testing.T) {
	run := &AgentRun{
		ID:         "run-skip",
		Status:     RunStatusSkipped,
		SkipReason: strPtr("already processed"),
	}
	task := RunToA2ATask(run, nil, nil, nil)
	assert.Equal(t, TaskStateCompleted, task.Status.State)
	require.NotNil(t, task.Status.Message)
	assert.Equal(t, "already processed", *task.Status.Message.Parts[0].Text)
}

func TestRunToA2ATask_CancellingFoldsToWorking(t *testing.T) {
	run := &AgentRun{ID: "run-c", Status: RunStatusCancelling}
	task := RunToA2ATask(run, nil, nil, nil)
	assert.Equal(t, TaskStateWorking, task.Status.State)
	require.NotNil(t, task.Status.Message)
	assert.Equal(t, "cancellation requested", *task.Status.Message.Parts[0].Text)
}

func TestRunToA2ATask_NoSessionNilContextID(t *testing.T) {
	run := &AgentRun{ID: "run-x", Status: RunStatusRunning}
	task := RunToA2ATask(run, nil, nil, nil)
	assert.Equal(t, "", task.ContextID)
}
