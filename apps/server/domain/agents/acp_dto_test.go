package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Task 9.1: ACPSlugFromName tests
// ============================================================================

func TestACPSlugFromName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple lowercase", "my-agent", "my-agent"},
		{"uppercase converted", "My-Agent", "my-agent"},
		{"spaces to hyphens", "my cool agent", "my-cool-agent"},
		{"special chars removed", "agent@v2.0!", "agent-v2-0"},
		{"underscores to hyphens", "my_agent_name", "my-agent-name"},
		{"consecutive hyphens collapsed", "my---agent", "my-agent"},
		{"leading hyphens trimmed", "---agent", "agent"},
		{"trailing hyphens trimmed", "agent---", "agent"},
		{"mixed special chars", "Hello World! (v2.0)", "hello-world-v2-0"},
		{"empty string", "", ""},
		{"all special chars", "!@#$%", ""},
		{"single char", "a", "a"},
		{"numbers preserved", "agent123", "agent123"},
		{"long name truncated to 63", "abcdefghijklmnopqrstuvwxyz-abcdefghijklmnopqrstuvwxyz-abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxyz-abcdefghijklmnopqrstuvwxyz-abcdefghi"},
		{"truncation trims trailing hyphen", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbb", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ACPSlugFromName(tt.input)
			assert.Equal(t, tt.expected, result)
			// Verify length constraint
			assert.LessOrEqual(t, len(result), 63, "slug must be <= 63 chars")
		})
	}
}

// ============================================================================
// Task 9.2: MapMemoryStatusToACP tests
// ============================================================================

func TestMapMemoryStatusToACP(t *testing.T) {
	tests := []struct {
		name     string
		input    AgentRunStatus
		expected string
	}{
		{"queued -> submitted", RunStatusQueued, ACPStatusSubmitted},
		{"running -> working", RunStatusRunning, ACPStatusWorking},
		{"paused -> input-required", RunStatusPaused, ACPStatusInputRequired},
		{"success -> completed", RunStatusSuccess, ACPStatusCompleted},
		{"error -> failed", RunStatusError, ACPStatusFailed},
		{"cancelling -> cancelling", RunStatusCancelling, ACPStatusCancelling},
		{"cancelled -> cancelled", RunStatusCancelled, ACPStatusCancelled},
		{"skipped -> completed", RunStatusSkipped, ACPStatusCompleted},
		{"unknown passes through", AgentRunStatus("unknown"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MapMemoryStatusToACP(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ============================================================================
// Task 9.3: Message format translation tests
// ============================================================================

func TestMemoryMessagesToACP_BasicText(t *testing.T) {
	messages := []AgentRunMessage{
		{
			Role:    "assistant",
			Content: map[string]any{"text": "Hello, world!"},
		},
	}
	result := memoryMessagesToACP(messages)
	assert.Len(t, result, 1)
	assert.Equal(t, "agent", result[0].Role)
	assert.Len(t, result[0].Parts, 1)
	assert.Equal(t, "text/plain", result[0].Parts[0].ContentType)
	assert.Equal(t, "Hello, world!", result[0].Parts[0].Content)
}

func TestMemoryMessagesToACP_MultipleMessages(t *testing.T) {
	messages := []AgentRunMessage{
		{
			Role:    "user",
			Content: map[string]any{"text": "What is 2+2?"},
		},
		{
			Role:    "assistant",
			Content: map[string]any{"text": "The answer is 4."},
		},
	}
	result := memoryMessagesToACP(messages)
	assert.Len(t, result, 2)
	assert.Equal(t, "user", result[0].Role)
	assert.Equal(t, "agent", result[1].Role)
}

func TestMemoryMessagesToACP_EmptyContent(t *testing.T) {
	messages := []AgentRunMessage{
		{
			Role:    "assistant",
			Content: map[string]any{"text": ""},
		},
	}
	result := memoryMessagesToACP(messages)
	// Empty text should be filtered out
	assert.Len(t, result, 0)
}

func TestMemoryMessagesToACP_NoTextKey(t *testing.T) {
	messages := []AgentRunMessage{
		{
			Role:    "assistant",
			Content: map[string]any{"function_calls": []any{}},
		},
	}
	result := memoryMessagesToACP(messages)
	// No text part means no message
	assert.Len(t, result, 0)
}

func TestMemoryMessagesToACP_NilContent(t *testing.T) {
	messages := []AgentRunMessage{
		{
			Role:    "assistant",
			Content: nil,
		},
	}
	result := memoryMessagesToACP(messages)
	assert.Len(t, result, 0)
}

func TestMemoryRoleToACP(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"assistant -> agent", "assistant", "agent"},
		{"user -> user", "user", "user"},
		{"system -> agent", "system", "agent"},
		{"tool_result -> agent", "tool_result", "agent"},
		{"unknown passes through", "custom", "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := memoryRoleToACP(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMemoryContentToACPParts_TextOnly(t *testing.T) {
	content := map[string]any{"text": "Hello"}
	parts := memoryContentToACPParts(content)
	assert.Len(t, parts, 1)
	assert.Equal(t, "text/plain", parts[0].ContentType)
	assert.Equal(t, "Hello", parts[0].Content)
}

func TestMemoryContentToACPParts_EmptyMap(t *testing.T) {
	parts := memoryContentToACPParts(map[string]any{})
	assert.Len(t, parts, 0)
}

func TestMemoryContentToACPParts_NilMap(t *testing.T) {
	parts := memoryContentToACPParts(nil)
	assert.Len(t, parts, 0)
}

// ============================================================================
// RunToACPObject tests
// ============================================================================

func TestRunToACPObject_BasicCompleted(t *testing.T) {
	agent := &Agent{Name: "My Agent"}
	run := &AgentRun{
		ID:     "run-123",
		Status: RunStatusSuccess,
		Agent:  agent,
	}
	messages := []AgentRunMessage{
		{
			Role:    "assistant",
			Content: map[string]any{"text": "Done!"},
		},
	}

	obj := RunToACPObject(run, messages, nil)
	assert.Equal(t, "run-123", obj.ID)
	assert.Equal(t, "my-agent", obj.AgentName)
	assert.Equal(t, ACPStatusCompleted, obj.Status)
	assert.Len(t, obj.Output, 1)
	assert.Nil(t, obj.AwaitRequest)
	assert.Nil(t, obj.Error)
}

func TestRunToACPObject_PausedWithQuestion(t *testing.T) {
	agent := &Agent{Name: "helper"}
	run := &AgentRun{
		ID:     "run-456",
		Status: RunStatusPaused,
		Agent:  agent,
	}
	question := &AgentQuestion{
		ID:       "q-1",
		Question: "Which option?",
		Options: []AgentQuestionOption{
			{Label: "A"},
			{Label: "B"},
		},
	}

	obj := RunToACPObject(run, nil, question)
	assert.Equal(t, ACPStatusInputRequired, obj.Status)
	assert.NotNil(t, obj.AwaitRequest)
	assert.Equal(t, "q-1", obj.AwaitRequest.QuestionID)
	assert.Equal(t, "Which option?", obj.AwaitRequest.Question)
	assert.Len(t, obj.AwaitRequest.Options, 2)
}

func TestRunToACPObject_Error(t *testing.T) {
	agent := &Agent{Name: "broken"}
	errMsg := "something went wrong"
	run := &AgentRun{
		ID:           "run-789",
		Status:       RunStatusError,
		Agent:        agent,
		ErrorMessage: &errMsg,
	}

	obj := RunToACPObject(run, nil, nil)
	assert.Equal(t, ACPStatusFailed, obj.Status)
	assert.NotNil(t, obj.Error)
	assert.Equal(t, "execution_error", obj.Error.Code)
	assert.Equal(t, "something went wrong", obj.Error.Message)
}

func TestRunToACPObject_Skipped(t *testing.T) {
	agent := &Agent{Name: "skipper"}
	reason := "no work to do"
	run := &AgentRun{
		ID:         "run-skip",
		Status:     RunStatusSkipped,
		Agent:      agent,
		SkipReason: &reason,
	}

	obj := RunToACPObject(run, nil, nil)
	assert.Equal(t, ACPStatusCompleted, obj.Status)
	assert.NotNil(t, obj.Metadata)
	assert.Equal(t, true, obj.Metadata["skipped"])
	assert.Equal(t, "no work to do", obj.Metadata["skip_reason"])
}

func TestRunToACPObject_NilAgent(t *testing.T) {
	run := &AgentRun{
		ID:     "run-no-agent",
		Status: RunStatusSuccess,
		Agent:  nil,
	}

	obj := RunToACPObject(run, nil, nil)
	assert.Equal(t, "", obj.AgentName)
}

func TestRunToACPObject_SessionID(t *testing.T) {
	agent := &Agent{Name: "sessioned"}
	sessID := "sess-abc"
	run := &AgentRun{
		ID:           "run-sess",
		Status:       RunStatusRunning,
		Agent:        agent,
		ACPSessionID: &sessID,
	}

	obj := RunToACPObject(run, nil, nil)
	assert.Equal(t, ACPStatusWorking, obj.Status)
	assert.NotNil(t, obj.SessionID)
	assert.Equal(t, "sess-abc", *obj.SessionID)
}

// ============================================================================
// AgentDefinitionToManifest tests
// ============================================================================

func TestAgentDefinitionToManifest_Basic(t *testing.T) {
	desc := "A helpful agent"
	def := &AgentDefinition{
		ID:          "def-1",
		Name:        "My Helper",
		Description: &desc,
	}

	manifest := AgentDefinitionToManifest(def, nil)
	assert.Equal(t, "my-helper", manifest.Name)
	assert.Equal(t, "A helpful agent", manifest.Description)
	assert.Equal(t, "0.2.0", manifest.Version)
	assert.Equal(t, []string{"text/plain"}, manifest.DefaultInputModes)
	assert.Equal(t, []string{"text/plain"}, manifest.DefaultOutputModes)
	assert.NotNil(t, manifest.Capabilities)
	assert.True(t, manifest.Capabilities.Streaming)
	assert.True(t, manifest.Capabilities.HumanInTheLoop)
	assert.True(t, manifest.Capabilities.SessionSupport)
	assert.Nil(t, manifest.Status)
	assert.NotNil(t, manifest.Provider)
	assert.Equal(t, "Emergent", manifest.Provider.Organization)
}

func TestAgentDefinitionToManifest_WithACPConfig(t *testing.T) {
	def := &AgentDefinition{
		ID:   "def-2",
		Name: "Custom Agent",
		ACPConfig: &ACPConfig{
			Description: "Custom ACP description",
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/markdown"},
		},
	}

	manifest := AgentDefinitionToManifest(def, nil)
	assert.Equal(t, "Custom ACP description", manifest.Description)
	assert.Equal(t, []string{"text/plain", "application/json"}, manifest.DefaultInputModes)
	assert.Equal(t, []string{"text/markdown"}, manifest.DefaultOutputModes)
}

func TestAgentDefinitionToManifest_WithStatus(t *testing.T) {
	desc := "Agent with metrics"
	def := &AgentDefinition{
		ID:          "def-3",
		Name:        "metrics-agent",
		Description: &desc,
	}
	avgTokens := 1500.0
	avgTime := 12.5
	successRate := 0.95
	status := &AgentStatusMetrics{
		AvgRunTokens:      &avgTokens,
		AvgRunTimeSeconds: &avgTime,
		SuccessRate:       &successRate,
	}

	manifest := AgentDefinitionToManifest(def, status)
	assert.NotNil(t, manifest.Status)
	assert.Equal(t, 1500.0, *manifest.Status.AvgRunTokens)
	assert.Equal(t, 12.5, *manifest.Status.AvgRunTimeSeconds)
	assert.Equal(t, 0.95, *manifest.Status.SuccessRate)
}

// ============================================================================
// ToolCallToTrajectoryMetadata tests
// ============================================================================

func TestToolCallToTrajectoryMetadata(t *testing.T) {
	tc := &AgentRunToolCall{
		ToolName: "search",
		Input:    map[string]any{"query": "hello"},
		Output:   map[string]any{"results": []any{"r1", "r2"}},
	}

	meta := ToolCallToTrajectoryMetadata(tc)
	assert.Equal(t, "trajectory", meta.Kind)
	assert.Equal(t, "search", *meta.ToolName)
	assert.Contains(t, string(meta.ToolInput), "hello")
	assert.Contains(t, string(meta.ToolOutput), "r1")
}

// ============================================================================
// SessionToACPObject tests
// ============================================================================

func TestSessionToACPObject_Empty(t *testing.T) {
	session := &ACPSession{
		ID: "sess-1",
	}

	obj := SessionToACPObject(session, nil, nil, nil)
	assert.Equal(t, "sess-1", obj.ID)
	assert.Len(t, obj.History, 0)
}

func TestSessionToACPObject_WithRuns(t *testing.T) {
	session := &ACPSession{
		ID: "sess-2",
	}
	runs := []*AgentRun{
		{
			ID:     "run-1",
			Status: RunStatusSuccess,
			Agent:  &Agent{Name: "agent-a"},
		},
		{
			ID:     "run-2",
			Status: RunStatusRunning,
			Agent:  &Agent{Name: "agent-b"},
		},
	}

	obj := SessionToACPObject(session, runs, nil, nil)
	assert.Len(t, obj.History, 2)
	assert.Equal(t, "run-1", obj.History[0].RunID)
	assert.Equal(t, "run-2", obj.History[1].RunID)
	assert.Equal(t, 2, obj.RunCount)
	require.NotNil(t, obj.LastRunStatus)
	assert.Equal(t, string(RunStatusRunning), *obj.LastRunStatus)
}

func TestSessionToACPObject_Empty_NoStatus(t *testing.T) {
	session := &ACPSession{ID: "sess-3"}
	obj := SessionToACPObject(session, nil, nil, nil)
	assert.Equal(t, 0, obj.RunCount)
	assert.Nil(t, obj.LastRunStatus)
}
