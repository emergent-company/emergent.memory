package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// ============================================================================
// Fakes
// ============================================================================

type fakeOnceRepo struct {
	agent     *Agent
	agentErr  error
	def       *AgentDefinition
	run       *AgentRun
	msgs      []*AgentRunMessage
	questions []*AgentQuestion
	orgID     string
}

func (f *fakeOnceRepo) FindByID(_ context.Context, _ string, _ *string) (*Agent, error) {
	return f.agent, f.agentErr
}
func (f *fakeOnceRepo) FindDefinitionByID(_ context.Context, _ string, _ *string) (*AgentDefinition, error) {
	return f.def, nil
}

// ResolveDefinitionForAgent mirrors the real repository's FK/marker/name
// resolution; the fake simply returns the configured definition.
func (f *fakeOnceRepo) ResolveDefinitionForAgent(_ context.Context, _ *Agent) (*AgentDefinition, error) {
	return f.def, nil
}
func (f *fakeOnceRepo) FindRunByID(_ context.Context, _ string) (*AgentRun, error) {
	return f.run, nil
}
func (f *fakeOnceRepo) FindMessagesByRunID(_ context.Context, _ string) ([]*AgentRunMessage, error) {
	return f.msgs, nil
}
func (f *fakeOnceRepo) FindPendingQuestionsByRunID(_ context.Context, _ string) ([]*AgentQuestion, error) {
	return f.questions, nil
}
func (f *fakeOnceRepo) GetOrgIDByProjectID(_ context.Context, _ string) (string, error) {
	return f.orgID, nil
}

type fakeRunner struct {
	result *ExecuteResult
	err    error
	block  bool
	gotReq ExecuteRequest
	calls  int
}

func (f *fakeRunner) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	f.calls++
	f.gotReq = req
	if f.block {
		<-ctx.Done()
		return &ExecuteResult{RunID: "r1", Status: RunStatusError}, nil
	}
	return f.result, f.err
}

func enabledAgent() *Agent {
	return &Agent{ID: "agent-1", ProjectID: "proj-1", Name: "Alpha", Enabled: true}
}

// agentReplyRole is the role the executor actually persists for an assistant
// turn: the sanitized agent name (ADK event Author), NOT "assistant". Tests use
// it to exercise the real production path.
const agentReplyRole = "Research_Agent"

func assistantMessages(text string) []*AgentRunMessage {
	return []*AgentRunMessage{
		{Role: "user", Content: map[string]any{"text": "hello"}},
		{Role: agentReplyRole, Content: map[string]any{"text": text}},
	}
}

// ============================================================================
// Tests
// ============================================================================

func TestRunAgentOnceSuccess(t *testing.T) {
	repo := &fakeOnceRepo{agent: enabledAgent(), msgs: assistantMessages("the answer")}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	reply, runID, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	require.NoError(t, err)
	assert.Equal(t, "the answer", reply)
	assert.Equal(t, "r1", runID)
	require.NotNil(t, runner.gotReq.MaxSteps)
	assert.Equal(t, defaultAgentOnceMaxSteps, *runner.gotReq.MaxSteps)
	require.NotNil(t, runner.gotReq.Timeout)
	assert.Equal(t, defaultAgentOnceTimeout, *runner.gotReq.Timeout)
	assert.Equal(t, "ping", runner.gotReq.UserMessage)
}

// B1: FK-less runtime agents (chat dummies and OpenAI-compat rows carry no
// agent_definition_id) must still have their backing definition resolved and
// attached to the execute request, exactly like FK-linked agents.
func TestRunAgentOnceResolvesDefinitionForFKLessAgent(t *testing.T) {
	definition := &AgentDefinition{ID: "def-1", ProjectID: "proj-1", Name: "Shared"}
	tests := []struct {
		name  string
		agent *Agent
	}{
		{
			name:  "FK-linked agent",
			agent: &Agent{ID: "agent-1", ProjectID: "proj-1", Name: "Alpha", Enabled: true, AgentDefinitionID: strPtr("def-1")},
		},
		{
			name:  "FK-less chat marker agent",
			agent: &Agent{ID: "agent-2", ProjectID: "proj-1", Name: "Chat session for Shared", Enabled: true, StrategyType: "chat-session:def-1"},
		},
		{
			name:  "FK-less agent-def marker agent",
			agent: &Agent{ID: "agent-3", ProjectID: "proj-1", Name: "Shared", Enabled: true, StrategyType: "agent-def:def-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeOnceRepo{agent: tt.agent, def: definition, msgs: assistantMessages("ok")}
			runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
			h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

			_, _, err := h.RunAgentOnce(context.Background(), "proj-1", tt.agent.ID, "ping", mcp.AgentRunBudget{})
			require.NoError(t, err)
			require.NotNil(t, runner.gotReq.AgentDefinition, "definition must be attached to the execute request")
			assert.Equal(t, "def-1", runner.gotReq.AgentDefinition.ID)
		})
	}
}

// H1: the reply MUST come from the agent-authored turn and MUST NOT leak
// system prompts or tool output. The executor persists tool responses under
// both "tool" and, historically/ACP, "tool_result"; the ACP mapping also
// collapses system/tool_result to "agent", so a mapped-role filter would leak
// those texts.
func TestRunAgentOnceReplyOnlyFromAgentRole(t *testing.T) {
	repo := &fakeOnceRepo{
		agent: enabledAgent(),
		msgs: []*AgentRunMessage{
			{Role: "system", Content: map[string]any{"text": "SECRET SYSTEM PROMPT"}},
			{Role: "user", Content: map[string]any{"text": "hello"}},
			{Role: "tool", Content: map[string]any{"text": "RAW TOOL OUTPUT"}},
			{Role: "tool_result", Content: map[string]any{"text": "RAW TOOL RESULT"}},
			{Role: agentReplyRole, Content: map[string]any{"text": "the real answer"}},
		},
	}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	reply, runID, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	require.NoError(t, err)
	assert.Equal(t, "the real answer", reply)
	assert.NotContains(t, reply, "SECRET")
	assert.NotContains(t, reply, "RAW TOOL")
	assert.Equal(t, "r1", runID)
}

// The executor persists assistant turns with role == sanitized agent name
// (ADK event Author), not the literal "assistant". This regression test uses
// that real role to prove call_agent returns a reply instead of failing with
// "agent run produced no assistant reply".
func TestRunAgentOnceReplyFromSanitizedAgentNameRole(t *testing.T) {
	repo := &fakeOnceRepo{
		agent: enabledAgent(),
		msgs: []*AgentRunMessage{
			{Role: "user", Content: map[string]any{"text": "hello"}},
			{Role: sanitizeAgentName("Research Agent"), Content: map[string]any{"text": "production reply"}},
		},
	}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	reply, runID, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	require.NoError(t, err)
	assert.Equal(t, "production reply", reply)
	assert.Equal(t, "r1", runID)
}

// A literal "assistant" role from an ACP-mapped or legacy run is still
// accepted for backwards compatibility.
func TestRunAgentOnceReplyFromLiteralAssistantRole(t *testing.T) {
	repo := &fakeOnceRepo{
		agent: enabledAgent(),
		msgs: []*AgentRunMessage{
			{Role: "user", Content: map[string]any{"text": "hello"}},
			{Role: "assistant", Content: map[string]any{"text": "legacy reply"}},
		},
	}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	reply, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	require.NoError(t, err)
	assert.Equal(t, "legacy reply", reply)
}

func TestRunAgentOnceSystemOnlyIsStructuredError(t *testing.T) {
	repo := &fakeOnceRepo{
		agent: enabledAgent(),
		msgs: []*AgentRunMessage{
			{Role: "system", Content: map[string]any{"text": "SECRET SYSTEM PROMPT"}},
		},
	}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	reply, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorFailed, runErr.Kind)
	assert.Empty(t, reply)
	assert.NotContains(t, runErr.Message, "SECRET")
}

func TestRunAgentOnceToolResultOnlyIsStructuredError(t *testing.T) {
	repo := &fakeOnceRepo{
		agent: enabledAgent(),
		msgs: []*AgentRunMessage{
			{Role: "tool_result", Content: map[string]any{"text": "RAW TOOL OUTPUT"}},
		},
	}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	reply, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorFailed, runErr.Kind)
	assert.Empty(t, reply)
}

func TestRunAgentOnceNoAssistantReplyIsStructuredError(t *testing.T) {
	repo := &fakeOnceRepo{agent: enabledAgent(), msgs: []*AgentRunMessage{{Role: "user", Content: map[string]any{"text": "hi"}}}}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	reply, runID, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorFailed, runErr.Kind)
	assert.Empty(t, reply)
	assert.Equal(t, "r1", runID)
}

// A trailing assistant message after tool output is preferred over earlier text.
func TestRunAgentOncePicksLastAssistantMessage(t *testing.T) {
	repo := &fakeOnceRepo{
		agent: enabledAgent(),
		msgs: []*AgentRunMessage{
			{Role: agentReplyRole, Content: map[string]any{"text": "first"}},
			{Role: "tool_result", Content: map[string]any{"text": "RAW TOOL OUTPUT"}},
			{Role: agentReplyRole, Content: map[string]any{"text": "second"}},
		},
	}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	reply, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	require.NoError(t, err)
	assert.Equal(t, "second", reply)
}

func TestRunAgentOnceUsesProvidedBudget(t *testing.T) {
	repo := &fakeOnceRepo{agent: enabledAgent(), msgs: assistantMessages("ok")}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusSuccess}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	_, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{MaxSteps: 3, Timeout: 5 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, 3, *runner.gotReq.MaxSteps)
	assert.Equal(t, 5*time.Second, *runner.gotReq.Timeout)
}

func TestRunAgentOnceMissingAgent(t *testing.T) {
	h := &MCPToolHandler{onceRepo: &fakeOnceRepo{}, onceRunner: &fakeRunner{}}
	_, _, err := h.RunAgentOnce(context.Background(), "proj-1", "missing", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorUnavailable, runErr.Kind)
}

func TestRunAgentOnceDisabledAgent(t *testing.T) {
	agent := enabledAgent()
	agent.Enabled = false
	h := &MCPToolHandler{onceRepo: &fakeOnceRepo{agent: agent}, onceRunner: &fakeRunner{}}
	_, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorUnavailable, runErr.Kind)
	assert.Contains(t, runErr.Message, "disabled")
}

func TestRunAgentOnceRunFailure(t *testing.T) {
	msg := "tool exploded"
	repo := &fakeOnceRepo{
		agent: enabledAgent(),
		run:   &AgentRun{ID: "r1", Status: RunStatusError, ErrorMessage: &msg},
	}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusError}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	_, runID, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorFailed, runErr.Kind)
	assert.Equal(t, "tool exploded", runErr.Message)
	assert.Equal(t, "r1", runID)
}

func TestRunAgentOncePausedWithQuestion(t *testing.T) {
	repo := &fakeOnceRepo{
		agent:     enabledAgent(),
		questions: []*AgentQuestion{{ID: "q1", Question: "Which environment?"}},
	}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusPaused}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	_, runID, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorPaused, runErr.Kind)
	assert.Equal(t, "Which environment?", runErr.Question)
	assert.Equal(t, "r1", runID)
}

func TestRunAgentOncePausedWithoutQuestionIsBudget(t *testing.T) {
	repo := &fakeOnceRepo{agent: enabledAgent()}
	runner := &fakeRunner{result: &ExecuteResult{RunID: "r1", Status: RunStatusPaused}}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	_, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorBudget, runErr.Kind)
}

func TestRunAgentOnceTimeout(t *testing.T) {
	repo := &fakeOnceRepo{agent: enabledAgent()}
	runner := &fakeRunner{block: true}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	_, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{Timeout: 20 * time.Millisecond})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorBudget, runErr.Kind)
}

func TestRunAgentOnceExecutorError(t *testing.T) {
	repo := &fakeOnceRepo{agent: enabledAgent()}
	runner := &fakeRunner{err: errors.New("kaput")}
	h := &MCPToolHandler{onceRepo: repo, onceRunner: runner}

	_, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorFailed, runErr.Kind)
	assert.Contains(t, runErr.Message, "kaput")
}

func TestRunAgentOnceUnavailableWithoutDeps(t *testing.T) {
	h := &MCPToolHandler{}
	_, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "ping", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorUnavailable, runErr.Kind)
}

func TestRunAgentOnceEmptyMessage(t *testing.T) {
	h := &MCPToolHandler{onceRepo: &fakeOnceRepo{agent: enabledAgent()}, onceRunner: &fakeRunner{}}
	_, _, err := h.RunAgentOnce(context.Background(), "proj-1", "agent-1", "   ", mcp.AgentRunBudget{})
	var runErr *mcp.AgentRunError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, mcp.AgentRunErrorFailed, runErr.Kind)
}
