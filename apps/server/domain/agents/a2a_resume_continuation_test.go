package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// =============================================================================
// Issue #867: a step-limit pause writes no question row, so an A2A follow-up
// message used to be rejected with "no pending question for task" and the task
// could never be resumed. A paused run with nothing to answer is a
// continuation; a paused run waiting on an external decision is not.
// =============================================================================

func enabledExecutor() *AgentExecutor {
	return &AgentExecutor{
		safeguards: config.AgentSafeguardsConfig{ExecutionEnabled: true},
		log:        testAgentLogger(),
	}
}

func TestA2AResumeQuestion_PendingQuestionWins(t *testing.T) {
	run := &AgentRun{ID: "run-1", Status: RunStatusPaused}
	q := &AgentQuestion{ID: "q-1", Question: "Approve?"}

	got, a2aErr := a2aResumeQuestion(run, []*AgentQuestion{q})
	require.Nil(t, a2aErr)
	assert.Same(t, q, got)
}

func TestA2AResumeQuestion_StepLimitPauseIsContinuation(t *testing.T) {
	// A step-limit pause leaves no suspend signal and no question row.
	run := &AgentRun{ID: "run-1", Status: RunStatusPaused, StepCount: 30}

	got, a2aErr := a2aResumeQuestion(run, nil)
	require.Nil(t, a2aErr)
	assert.Nil(t, got)
}

func TestA2AResumeQuestion_AnsweredHumanPauseIsContinuation(t *testing.T) {
	// The question was consumed but the resume never created a run (the row is
	// still paused): the follow-up message is the human answer.
	run := &AgentRun{
		ID:             "run-1",
		Status:         RunStatusPaused,
		SuspendContext: map[string]any{"reason": string(SuspendReasonAwaitingHuman)},
	}

	got, a2aErr := a2aResumeQuestion(run, []*AgentQuestion{})
	require.Nil(t, a2aErr)
	assert.Nil(t, got)
}

func TestA2AResumeQuestion_ToolApprovalWithoutQuestionRejected(t *testing.T) {
	run := &AgentRun{
		ID:             "run-1",
		Status:         RunStatusPaused,
		SuspendContext: map[string]any{"reason": string(SuspendReasonAwaitingToolConfirm)},
	}

	got, a2aErr := a2aResumeQuestion(run, nil)
	require.NotNil(t, a2aErr)
	assert.Nil(t, got)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
}

func TestA2AResumeQuestion_ClientToolWithoutQuestionRejected(t *testing.T) {
	run := &AgentRun{
		ID:             "run-1",
		Status:         RunStatusPaused,
		SuspendContext: map[string]any{"reason": string(SuspendReasonAwaitingClientTool)},
	}

	_, a2aErr := a2aResumeQuestion(run, nil)
	require.NotNil(t, a2aErr)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
}

func TestA2AResumeUserMessage_QuestionAnswer(t *testing.T) {
	msg := a2aResumeUserMessage(&AgentQuestion{Question: "Which branch?"}, "dev")
	assert.Contains(t, msg, `Previously you asked: "Which branch?"`)
	assert.Contains(t, msg, `The user responded: "dev"`)
}

func TestA2AResumeUserMessage_ContinuationCarriesUserText(t *testing.T) {
	msg := a2aResumeUserMessage(nil, "keep going")
	assert.Contains(t, msg, "Continue from where you left off")
	assert.Contains(t, msg, "keep going")
}

// TestPrepareA2AResume_StepLimitPauseContinues drives the shared resume
// preparation over a paused run with no pending question (the step-limit
// pause): it must return a continuation prompt and must not claim/update
// anything.
func TestPrepareA2AResume_StepLimitPauseContinues(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setNoPendingQuestions(true)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	run := &AgentRun{ID: "task-1", Status: RunStatusPaused, StepCount: 30}

	msg, a2aErr := h.prepareA2AResume(context.Background(), run, "keep going", "user-1")
	require.Nil(t, a2aErr)
	assert.Contains(t, msg, "keep going")
	assert.Contains(t, msg, "Continue from where you left off")

	for _, q := range drv.queriesCopy() {
		assert.NotContains(t, q, "UPDATE",
			"a continuation has no question to claim")
	}
}

// TestPrepareA2AResume_PendingQuestionClaimed pins the question path: the
// follow-up message answers the question and the claim is applied once.
func TestPrepareA2AResume_PendingQuestionClaimed(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	run := &AgentRun{ID: "task-1", Status: RunStatusPaused}

	msg, a2aErr := h.prepareA2AResume(context.Background(), run, "yes", "user-1")
	require.Nil(t, a2aErr)
	assert.Contains(t, msg, `Previously you asked: "Approve?"`)
	assert.Contains(t, msg, `The user responded: "yes"`)

	var claimed bool
	for _, q := range drv.queriesCopy() {
		if containsAll(q, "UPDATE", `"kb"."agent_questions"`) {
			claimed = true
		}
	}
	assert.True(t, claimed, "the pending question must be claimed")
}

// TestPrepareA2AResume_ToolApprovalPauseRejected pins that a pause waiting on a
// tool approval still cannot be advanced by a plain A2A message.
func TestPrepareA2AResume_ToolApprovalPauseRejected(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setNoPendingQuestions(true)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	run := &AgentRun{
		ID:             "task-1",
		Status:         RunStatusPaused,
		SuspendContext: map[string]any{"reason": string(SuspendReasonAwaitingToolConfirm)},
	}

	msg, a2aErr := h.prepareA2AResume(context.Background(), run, "yes", "user-1")
	require.NotNil(t, a2aErr)
	assert.Empty(t, msg)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
}

// TestA2AHITLMetadata_StepLimitPause pins the pause discriminator a client can
// use to tell a step-limit pause from a HITL question.
func TestA2AHITLMetadata_StepLimitPause(t *testing.T) {
	md := hitlMetadata(&AgentRun{Status: RunStatusPaused})
	require.NotNil(t, md)
	assert.Equal(t, "step_limit", md["pauseSource"])
	assert.Equal(t, true, md["inputRequired"])
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
