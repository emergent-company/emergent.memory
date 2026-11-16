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
// continuation prompt, claim the run (not a question), and never touch
// agent_questions.
func TestPrepareA2AResume_StepLimitPauseContinues(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setNoPendingQuestions(true)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	msg, claim, a2aErr := h.prepareA2AResume(context.Background(), stepLimitPausedRun(), "keep going", "user-1")
	require.Nil(t, a2aErr)
	require.NotNil(t, claim, "a continuation must claim the run")
	assert.Contains(t, msg, "keep going")
	assert.Contains(t, msg, "Continue from where you left off")

	var claimedRun, claimedQuestion bool
	for _, q := range drv.queriesCopy() {
		if containsAll(q, "UPDATE", `"kb"."agent_runs"`, "resume_claim") {
			claimedRun = true
		}
		if containsAll(q, "UPDATE", `"kb"."agent_questions"`) {
			claimedQuestion = true
		}
	}
	assert.True(t, claimedRun, "the run must be claimed with the resume-claim CAS")
	assert.False(t, claimedQuestion, "a continuation has no question to claim")
}

// TestPrepareA2AResume_StepLimitPauseClaimLost pins the concurrency guard: when a
// concurrent follow-up already claimed the run, the loser must be rejected
// instead of resuming a second time.
func TestPrepareA2AResume_StepLimitPauseClaimLost(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setNoPendingQuestions(true)
	drv.setClaimLost(true)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	msg, claim, a2aErr := h.prepareA2AResume(context.Background(), stepLimitPausedRun(), "keep going", "user-1")
	require.NotNil(t, a2aErr)
	assert.Empty(t, msg)
	assert.Nil(t, claim)
	assert.Equal(t, a2aReasonResumeInProgress, a2aErr.Reason)
	assert.Contains(t, a2aErr.Message, "already being resumed")
}

// TestPrepareA2AResume_UnidentifiedPauseRejected pins the fail-closed default: a
// paused run with no suspend marker is not a continuation.
func TestPrepareA2AResume_UnidentifiedPauseRejected(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	drv.setNoPendingQuestions(true)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	run := &AgentRun{ID: "task-1", Status: RunStatusPaused, StepCount: 30}

	msg, claim, a2aErr := h.prepareA2AResume(context.Background(), run, "keep going", "user-1")
	require.NotNil(t, a2aErr)
	assert.Empty(t, msg)
	assert.Nil(t, claim)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
	for _, q := range drv.queriesCopy() {
		assert.NotContains(t, q, "resume_claim",
			"an unidentified pause must not be claimed for resume")
	}
}

// TestPrepareA2AResume_PendingQuestionClaimed pins the question path: the
// follow-up message answers the question and the claim is applied once. No run
// claim is taken, because AnswerQuestion already serialises the answer.
func TestPrepareA2AResume_PendingQuestionClaimed(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	run := &AgentRun{ID: "task-1", Status: RunStatusPaused}

	msg, claim, a2aErr := h.prepareA2AResume(context.Background(), run, "yes", "user-1")
	require.Nil(t, a2aErr)
	assert.Nil(t, claim, "the pending question is the claim")
	assert.Contains(t, msg, `Previously you asked: "Approve?"`)
	assert.Contains(t, msg, `The user responded: "yes"`)

	var claimed, runClaimed bool
	for _, q := range drv.queriesCopy() {
		if containsAll(q, "UPDATE", `"kb"."agent_questions"`) {
			claimed = true
		}
		if strings.Contains(q, "resume_claim") {
			runClaimed = true
		}
	}
	assert.True(t, claimed, "the pending question must be claimed")
	assert.False(t, runClaimed, "the question is the claim; the run needs none")
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

	msg, claim, a2aErr := h.prepareA2AResume(context.Background(), run, "yes", "user-1")
	require.NotNil(t, a2aErr)
	assert.Empty(t, msg)
	assert.Nil(t, claim)
	assert.Contains(t, a2aErr.Message, "no pending question for task")
}

// TestA2AResumeClaim_Release pins the compensation path: a claim whose resume
// never took ownership is dropped so the task stays resumable. A nil claim (the
// question path or the new-task path) releases as a no-op.
func TestA2AResumeClaim_Release(t *testing.T) {
	repo, drv := newA2AResumeRepository(t)
	h := &A2AHandler{repo: repo, executor: enabledExecutor(), log: testAgentLogger()}

	claim := &a2aResumeClaim{h: h, runID: "run-1", claimID: "claim-1"}
	claim.release()

	var released bool
	for _, q := range drv.queriesCopy() {
		if containsAll(q, "UPDATE", `"kb"."agent_runs"`, "resume_claim") {
			released = true
		}
	}
	assert.True(t, released, "release must drop the claim")

	before := len(drv.queriesCopy())
	var nilClaim *a2aResumeClaim
	nilClaim.release()
	assert.Len(t, drv.queriesCopy(), before, "a nil claim must not hit the database")
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
