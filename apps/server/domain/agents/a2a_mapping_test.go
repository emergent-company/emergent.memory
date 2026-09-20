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

func TestAgentRunMessageToA2AMessage_SystemAndToolResultFiltered(t *testing.T) {
	for _, role := range []string{"system", "tool", "tool_result", ""} {
		msg := &AgentRunMessage{ID: "m-x", Role: role, Content: map[string]any{"text": "x"}}
		m := AgentRunMessageToA2AMessage(msg)
		assert.Nil(t, m, "role %q must be filtered from A2A history", role)
	}
}

func TestAgentRunMessageToA2AMessage_SanitizedAgentNameMapsToAgent(t *testing.T) {
	msg := &AgentRunMessage{
		ID:      "m-3",
		Role:    "Research_Agent",
		Content: map[string]any{"text": "computed answer"},
	}
	m := AgentRunMessageToA2AMessage(msg)
	require.NotNil(t, m)
	assert.Equal(t, RoleAgent, m.Role, "sanitized agent name must map to ROLE_AGENT")
	assert.Equal(t, "m-3", m.MessageID)
	require.Len(t, m.Parts, 1)
	assert.Equal(t, "computed answer", *m.Parts[0].Text)
}

func TestMapRoleToA2A_Classification(t *testing.T) {
	cases := map[string]Role{
		"user":           RoleUser,
		"assistant":      RoleAgent,
		"Research_Agent": RoleAgent,
		"operator":       RoleAgent,
		"reasoning":      RoleAgent,
	}
	for role, want := range cases {
		assert.Equal(t, want, MapRoleToA2A(role), "role %q", role)
	}
}

func TestIsA2AHistoryRole(t *testing.T) {
	cases := map[string]bool{
		"user":           true,
		"assistant":      true,
		"Research_Agent": true,
		"system":         false,
		"tool":           false,
		"tool_result":    false,
		"":               false,
	}
	for role, want := range cases {
		assert.Equal(t, want, isA2AHistoryRole(role), "role %q", role)
	}
}

func TestMessagesToA2A_FiltersSystemAndTool(t *testing.T) {
	messages := []AgentRunMessage{
		{Role: "system", Content: map[string]any{"text": "secret prompt"}},
		{Role: "user", Content: map[string]any{"text": "hi"}},
		{Role: "tool_result", Content: map[string]any{"text": "raw output"}},
		{Role: "Research_Agent", Content: map[string]any{"text": "answer"}},
	}
	out := MessagesToA2A(messages)
	require.Len(t, out, 2)
	assert.Equal(t, RoleUser, out[0].Role)
	assert.Equal(t, "hi", *out[0].Parts[0].Text)
	assert.Equal(t, RoleAgent, out[1].Role)
	assert.Equal(t, "answer", *out[1].Parts[0].Text)
}

func TestTextStatusMessage_HasMessageID(t *testing.T) {
	m := textStatusMessage("boom")
	require.NotNil(t, m)
	assert.NotEmpty(t, m.MessageID, "generated status message must carry a messageId")
	assert.Equal(t, RoleAgent, m.Role)
	require.Len(t, m.Parts, 1)
	assert.Equal(t, "boom", *m.Parts[0].Text)
}

func TestFinalTextArtifact_UsesSanitizedAgentName(t *testing.T) {
	messages := []AgentRunMessage{
		{Role: "user", Content: map[string]any{"text": "question"}},
		{Role: "Research_Agent", Content: map[string]any{"text": "final answer"}},
	}
	artifacts := finalTextArtifact(messages)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "result", artifacts[0].ArtifactID, "final text artifact must carry a non-empty artifactId")
	require.Len(t, artifacts[0].Parts, 1)
	assert.Equal(t, "final answer", *artifacts[0].Parts[0].Text)
}

func TestFinalTextArtifact_IgnoresSystemAndTool(t *testing.T) {
	messages := []AgentRunMessage{
		{Role: "system", Content: map[string]any{"text": "prompt"}},
		{Role: "tool_result", Content: map[string]any{"text": "raw"}},
	}
	assert.Nil(t, finalTextArtifact(messages))
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

func TestToolCallArtifacts(t *testing.T) {
	toolCalls := []*AgentRunToolCall{
		{ID: "tc-1", ToolName: "search", Input: map[string]any{"q": "x"}, Output: map[string]any{"r": "y"}},
		{ID: "tc-2", ToolName: "entity-query", Input: map[string]any{"t": "person"}, Output: map[string]any{}},
	}
	artifacts := toolCallArtifacts(toolCalls)
	require.Len(t, artifacts, 2)

	assert.Equal(t, "tc-1", artifacts[0].ArtifactID)
	assert.Equal(t, "search", artifacts[0].Name)
	require.Len(t, artifacts[0].Parts, 1)
	assert.NotNil(t, artifacts[0].Parts[0].Data)
	assert.Nil(t, artifacts[0].Parts[0].Text)

	assert.Equal(t, "tc-2", artifacts[1].ArtifactID)
	assert.Equal(t, "entity-query", artifacts[1].Name)
}

func TestToolCallArtifacts_NilAndEmpty(t *testing.T) {
	assert.Empty(t, toolCallArtifacts(nil))
	assert.Empty(t, toolCallArtifacts([]*AgentRunToolCall{nil}))
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

func TestIsInternalOnlyRole_ReasoningAndOperator(t *testing.T) {
	for _, role := range []string{"reasoning", "operator", "system", "tool", "tool_result"} {
		assert.True(t, isInternalOnlyRole(role), "role %q must be internal-only", role)
		assert.False(t, isA2AHistoryRole(role), "role %q must be excluded from history", role)
	}
	assert.False(t, isInternalOnlyRole("user"))
	assert.False(t, isInternalOnlyRole("assistant"))
	assert.False(t, isInternalOnlyRole("Research_Agent"))
}

func TestAgentRunMessageToA2AMessage_ZeroTextDropped(t *testing.T) {
	// A history message with no text content (e.g. a turn whose payload was
	// tool data) must be dropped, not serialized as an empty-part Message.
	assert.Nil(t, AgentRunMessageToA2AMessage(&AgentRunMessage{ID: "m", Role: "assistant", Content: map[string]any{}}))
	assert.Nil(t, AgentRunMessageToA2AMessage(&AgentRunMessage{ID: "m", Role: "user", Content: map[string]any{"text": ""}}))
}

func TestMessagesToA2A_SkipsReasoningAndOperator(t *testing.T) {
	messages := []AgentRunMessage{
		{Role: "reasoning", Content: map[string]any{"text": "chain of thought"}},
		{Role: "operator", Content: map[string]any{"text": "planning text"}},
		{Role: "user", Content: map[string]any{"text": "hi"}},
		{Role: "Research_Agent", Content: map[string]any{"text": "answer"}},
	}
	out := MessagesToA2A(messages)
	require.Len(t, out, 2)
	assert.Equal(t, RoleUser, out[0].Role)
	assert.Equal(t, "hi", *out[0].Parts[0].Text)
	assert.Equal(t, RoleAgent, out[1].Role)
	assert.Equal(t, "answer", *out[1].Parts[0].Text)
}
