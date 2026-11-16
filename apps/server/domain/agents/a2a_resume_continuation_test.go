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
// could never be resumed. A pause with nothing to answer is a continuation, but
// only when the pause is POSITIVELY identified as one: a pause with no marker
// stays fail-closed (issue #867 review).
// =============================================================================

func enabledExecutor() *AgentExecutor {
	return &AgentExecutor{
		safeguards: config.AgentSafeguardsConfig{ExecutionEnabled: true},
		log:        testAgentLogger(),
	}
}

// stepLimitPausedRun is a paused run carrying the executor's step-limit marker.
func stepLimitPausedRun() *AgentRun {
	return &AgentRun{
		ID:             "run-1",
		Status:         RunStatusPaused,
		StepCount:      30,
		SuspendContext: map[string]any{"reason": string(SuspendReasonStepLimit)},
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
	// A step-limit pause leaves no question row but writes the step-limit marker.
	got, a2aErr := a2aResumeQuestion(stepLimitPausedRun(), nil)
	require.Nil(t, a2aErr)
	assert.Nil(t, got)
}

func TestA2AResumeQuestion_UnidentifiedPauseRejected(t *testing.T) {
	// No suspend marker: the pause's origin is undetermined (a legacy row, or a
	// pause path that failed to persist its marker), so it must not be advanced
	// by a plain follow-up message.
	run := &AgentRun{ID: "run-1", Status: RunStatusPaused, StepCount: 30}

	got, a2aErr := a2aResumeQuestion(run, nil)
	require.NotNil(t, a2aErr)
	assert.Nil(t, got)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
}

func TestA2AResumeQuestion_UnrecognisedPauseRejected(t *testing.T) {
	run := &AgentRun{
		ID:             "run-1",
		Status:         RunStatusPaused,
		SuspendContext: map[string]any{"reason": "some_future_pause"},
	}

	_, a2aErr := a2aResumeQuestion(run, nil)
	require.NotNil(t, a2aErr)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
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

func TestA2AResumeQuestion_AwaitingChildWithoutQuestionRejected(t *testing.T) {
	run := &AgentRun{
		ID:             "run-1",
		Status:         RunStatusPaused,
		SuspendContext: map[string]any{"reason": string(SuspendReasonAwaitingChild)},
	}

	_, a2aErr := a2aResumeQuestion(run, nil)
	require.NotNil(t, a2aErr)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
}

func TestA2PauseIsContinuation_NilRun(t *testing.T) {
	assert.False(t, a2aPauseIsContinuation(nil))
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
// preparation over a run the executor paused at its step limit: it must return a
// continuation prompt, consume the pause atomically (so a concurrent follow-up
// cannot execute it too), and never touch agent_questions.
func TestPrepareA2AResume_StepLimitPauseContinues(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setNoPendingQuestions(true)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	msg, pauseClaimed, a2aErr := h.prepareA2AResume(context.Background(), stepLimitPausedRun(), "keep going", "user-1")
	require.Nil(t, a2aErr)
	assert.True(t, pauseClaimed, "a continuation must consume the pause before executing")
	assert.Contains(t, msg, "keep going")
	assert.Contains(t, msg, "Continue from where you left off")

	var claimedRun, claimedQuestion bool
	for _, q := range drv.queriesCopy() {
		if containsAll(q, "UPDATE", `"kb"."agent_runs"`, "completed_at = NOW()") {
			claimedRun = true
		}
		if containsAll(q, "UPDATE", `"kb"."agent_questions"`) {
			claimedQuestion = true
		}
	}
	assert.True(t, claimedRun, "the pause must be claimed atomically")
	assert.False(t, claimedQuestion, "a continuation has no question to claim")
}

// TestPrepareA2AResume_ClaimLostRejected pins the concurrency guard: when a
// concurrent follow-up already consumed the pause, the loser is rejected with its
// own reason instead of executing the turn a second time (issue #887).
func TestPrepareA2AResume_ClaimLostRejected(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setNoPendingQuestions(true)
	drv.setClaimLost(true)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	msg, pauseClaimed, a2aErr := h.prepareA2AResume(context.Background(), stepLimitPausedRun(), "keep going", "user-1")
	require.NotNil(t, a2aErr)
	assert.Empty(t, msg)
	assert.False(t, pauseClaimed)
	assert.Equal(t, a2aReasonResumeInProgress, a2aErr.Reason)
}

// TestPrepareA2AResume_UnidentifiedPauseRejected pins the fail-closed default: a
// paused run with no suspend marker is not a continuation, and is left untouched.
func TestPrepareA2AResume_UnidentifiedPauseRejected(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setNoPendingQuestions(true)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	run := &AgentRun{ID: "task-1", Status: RunStatusPaused, StepCount: 30}

	msg, pauseClaimed, a2aErr := h.prepareA2AResume(context.Background(), run, "keep going", "user-1")
	require.NotNil(t, a2aErr)
	assert.Empty(t, msg)
	assert.False(t, pauseClaimed)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
	for _, q := range drv.queriesCopy() {
		assert.NotContains(t, q, "completed_at = NOW()",
			"an unidentified pause must not be consumed")
	}
}

// TestPrepareA2AResume_TotalStepCapRefusedBeforeClaim pins the guard against
// consuming a pause that cannot be executed anyway: a run at the global
// cumulative step cap is refused before the pause is claimed, so the task is not
// left terminal with nothing to answer.
func TestPrepareA2AResume_TotalStepCapRefusedBeforeClaim(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setNoPendingQuestions(true)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	run := stepLimitPausedRun()
	run.StepCount = MaxTotalStepsPerRun

	msg, pauseClaimed, a2aErr := h.prepareA2AResume(context.Background(), run, "keep going", "user-1")
	require.NotNil(t, a2aErr)
	assert.Empty(t, msg)
	assert.False(t, pauseClaimed)
	assert.Contains(t, a2aErr.Message, "total step limit")
	for _, q := range drv.queriesCopy() {
		assert.NotContains(t, q, "completed_at = NOW()",
			"a pause that cannot execute must not be consumed")
	}
}

// TestPrepareA2AResume_PendingQuestionClaimed pins the question path: the
// follow-up message answers the question and the claim is applied once. The pause
// is left for the executor to claim, so pauseClaimed stays false.
func TestPrepareA2AResume_PendingQuestionClaimed(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	run := &AgentRun{ID: "task-1", Status: RunStatusPaused}

	msg, pauseClaimed, a2aErr := h.prepareA2AResume(context.Background(), run, "yes", "user-1")
	require.Nil(t, a2aErr)
	assert.False(t, pauseClaimed, "the executor claims the pause on the question path")
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

	msg, pauseClaimed, a2aErr := h.prepareA2AResume(context.Background(), run, "yes", "user-1")
	require.NotNil(t, a2aErr)
	assert.Empty(t, msg)
	assert.False(t, pauseClaimed)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
}

// TestA2AResumeInProgressError pins the rejection a follower gets when a
// concurrent follow-up consumed the pause first: a distinct reason, so a client
// does not mistake it for an unresumable task and start a duplicate one.
func TestA2AResumeInProgressError(t *testing.T) {
	a2aErr := a2aResumeInProgressError()
	require.NotNil(t, a2aErr)
	assert.Equal(t, a2aReasonResumeInProgress, a2aErr.Reason)
	assert.NotEqual(t, a2aReasonInvalidState, a2aErr.Reason)
	assert.Contains(t, a2aErr.Message, "already being resumed")
}

// TestA2AResumeConflictResponse pins the in-stream report for the same case: a
// message naming the task, and no fabricated terminal state for a task another
// follower is still executing.
func TestA2AResumeConflictResponse(t *testing.T) {
	sr := a2aResumeConflictResponse("task-1", "ctx-1")
	assert.Nil(t, sr.StatusUpdate, "a concurrent resume is not a terminal state")
	assert.Nil(t, sr.Task)
	require.NotNil(t, sr.Message)
	assert.Equal(t, "task-1", sr.Message.TaskID)
	assert.Equal(t, "ctx-1", sr.Message.ContextID)
	require.Len(t, sr.Message.Parts, 1)
	require.NotNil(t, sr.Message.Parts[0].Text)
	assert.Contains(t, *sr.Message.Parts[0].Text, "already being resumed")
}

// TestPauseRunAtStepLimit_PersistsMarkerBeforePause pins the executor side of the
// fix: the step-limit pause writes the suspend marker (which is what makes the
// pause identifiable as a continuation) BEFORE it flips the row to paused.
func TestPauseRunAtStepLimit_PersistsMarkerBeforePause(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	ae := &AgentExecutor{repo: repo, log: testAgentLogger()}

	ae.pauseRunAtStepLimit(context.Background(), "run-1", 7)

	queries := drv.queriesCopy()
	require.GreaterOrEqual(t, len(queries), 2, "expected a marker write then the pause")
	assert.Contains(t, queries[0], "suspend_context =",
		"the step-limit marker must be persisted first")
	assert.Contains(t, queries[0], string(SuspendReasonStepLimit),
		"the persisted marker must name the step limit")
	assert.Contains(t, queries[1], "status = 'input-required'",
		"the pause write must follow the marker")
}

// TestA2AHITLMetadata_StepLimitPause pins the pause discriminator a client can
// use to tell a step-limit pause from a HITL question.
func TestA2AHITLMetadata_StepLimitPause(t *testing.T) {
	md := hitlMetadata(stepLimitPausedRun())
	require.NotNil(t, md)
	assert.Equal(t, "step_limit", md["pauseSource"])
	assert.Equal(t, true, md["inputRequired"])
}

// TestA2AHITLMetadata_UnidentifiedPauseIsUnknown pins the fail-closed reporting
// default: a pause whose marker is missing is reported as unknown, never as a
// step-limit pause a client would auto-continue.
func TestA2AHITLMetadata_UnidentifiedPauseIsUnknown(t *testing.T) {
	md := hitlMetadata(&AgentRun{Status: RunStatusPaused})
	require.NotNil(t, md)
	assert.Equal(t, "unknown", md["pauseSource"])
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
