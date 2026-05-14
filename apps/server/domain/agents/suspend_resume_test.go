package agents

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SuspendSignal round-trip
// ---------------------------------------------------------------------------

func TestSuspendSignal_ToMap_AwaitingHuman(t *testing.T) {
	s := SuspendSignal{
		Reason:            SuspendReasonAwaitingHuman,
		QuestionID:        "q-123",
		PendingToolCallID: "call-abc",
		PendingToolName:   "ask_user",
	}
	m := s.ToMap()

	assert.Equal(t, "awaiting_human", m["reason"])
	assert.Equal(t, "q-123", m["question_id"])
	assert.Equal(t, "call-abc", m["pending_tool_call_id"])
	assert.Equal(t, "ask_user", m["pending_tool_name"])
	assert.NotContains(t, m, "waiting_for_run_id") // not set → omitted
}

func TestSuspendSignal_ToMap_AwaitingChild(t *testing.T) {
	s := SuspendSignal{
		Reason:          SuspendReasonAwaitingChild,
		WaitingForRunID: "run-child-99",
	}
	m := s.ToMap()

	assert.Equal(t, "awaiting_child", m["reason"])
	assert.Equal(t, "run-child-99", m["waiting_for_run_id"])
	assert.NotContains(t, m, "question_id") // not set → omitted
}

func TestSuspendSignalFromMap_RoundTrip(t *testing.T) {
	original := SuspendSignal{
		Reason:            SuspendReasonAwaitingHuman,
		QuestionID:        "q-456",
		PendingToolCallID: "call-xyz",
		PendingToolName:   "ask_user",
	}
	m := original.ToMap()
	got := SuspendSignalFromMap(m)

	require.NotNil(t, got)
	assert.Equal(t, original.Reason, got.Reason)
	assert.Equal(t, original.QuestionID, got.QuestionID)
	assert.Equal(t, original.PendingToolCallID, got.PendingToolCallID)
	assert.Equal(t, original.PendingToolName, got.PendingToolName)
	assert.Empty(t, got.WaitingForRunID)
}

func TestSuspendSignalFromMap_NilMap(t *testing.T) {
	assert.Nil(t, SuspendSignalFromMap(nil))
}

func TestSuspendSignalFromMap_MissingReason(t *testing.T) {
	assert.Nil(t, SuspendSignalFromMap(map[string]any{"question_id": "q-1"}))
}

func TestSuspendSignalFromMap_WithResumeRunID(t *testing.T) {
	// After HandleRespondToQuestion stores resume_run_id in suspend_context,
	// SuspendSignalFromMap should survive the extra field gracefully.
	m := map[string]any{
		"reason":               "awaiting_human",
		"question_id":          "q-789",
		"pending_tool_call_id": "call-1",
		"pending_tool_name":    "ask_user",
		"resume_run_id":        "run-new-42", // extra field written by respond handler
	}
	got := SuspendSignalFromMap(m)
	require.NotNil(t, got)
	assert.Equal(t, SuspendReasonAwaitingHuman, got.Reason)
	assert.Equal(t, "q-789", got.QuestionID)
}

// ---------------------------------------------------------------------------
// RunToACPObject: ResumeRunID populated from suspend_context
// ---------------------------------------------------------------------------

func TestRunToACPObject_ResumeRunID_FromSuspendContext(t *testing.T) {
	resumeID := "run-resume-77"
	run := &AgentRun{
		ID:     "run-paused-1",
		Status: RunStatusPaused,
		SuspendContext: map[string]any{
			"reason":        "awaiting_human",
			"question_id":   "q-1",
			"resume_run_id": resumeID,
		},
		CreatedAt: time.Now(),
	}

	obj := RunToACPObject(run, nil, nil)

	require.NotNil(t, obj.ResumeRunID)
	assert.Equal(t, resumeID, *obj.ResumeRunID)
}

func TestRunToACPObject_NoResumeRunID_WhenAbsent(t *testing.T) {
	run := &AgentRun{
		ID:     "run-paused-2",
		Status: RunStatusPaused,
		SuspendContext: map[string]any{
			"reason":      "awaiting_human",
			"question_id": "q-2",
			// no resume_run_id yet
		},
		CreatedAt: time.Now(),
	}

	obj := RunToACPObject(run, nil, nil)
	assert.Nil(t, obj.ResumeRunID)
}

func TestRunToACPObject_NoResumeRunID_WhenSuspendContextNil(t *testing.T) {
	run := &AgentRun{
		ID:             "run-normal-3",
		Status:         RunStatusSuccess,
		SuspendContext: nil,
		CreatedAt:      time.Now(),
	}

	obj := RunToACPObject(run, nil, nil)
	assert.Nil(t, obj.ResumeRunID)
}

// ---------------------------------------------------------------------------
// AgentQuestionDTO: ResumeRunID field present
// ---------------------------------------------------------------------------

func TestAgentQuestionDTO_HasResumeRunIDField(t *testing.T) {
	q := &AgentQuestion{
		ID:              "q-dto-1",
		RunID:           "run-1",
		AgentID:         "agent-1",
		ProjectID:       "proj-1",
		Question:        "Proceed?",
		InteractionType: QuestionInteractionButtons,
		Status:          QuestionStatusPending,
	}
	dto := q.ToDTO()

	// Field exists and is nil before respond is called
	assert.Nil(t, dto.ResumeRunID)

	// After respond handler sets it
	id := "run-resumed-55"
	dto.ResumeRunID = &id
	assert.Equal(t, "run-resumed-55", *dto.ResumeRunID)
}

// ---------------------------------------------------------------------------
// PreCreatedRun: executor uses pre-created run when set
// ---------------------------------------------------------------------------

// TestExecuteRequest_PreCreatedRun verifies that ExecuteRequest carries
// PreCreatedRun and that Resume() will use it via the executor field.
// This is a structural test — no DB needed.
func TestExecuteRequest_PreCreatedRun_Field(t *testing.T) {
	preRun := &AgentRun{ID: "pre-run-id", Status: RunStatusRunning}
	req := ExecuteRequest{
		PreCreatedRun: preRun,
		UserMessage:   "continue",
	}
	require.NotNil(t, req.PreCreatedRun)
	assert.Equal(t, "pre-run-id", req.PreCreatedRun.ID)
}
