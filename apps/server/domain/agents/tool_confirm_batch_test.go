package agents

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SuspendSignal batch round-trip ---

func TestSuspendSignalBatchRoundTrip(t *testing.T) {
	sig := SuspendSignal{
		Reason: SuspendReasonAwaitingToolConfirm,
		PendingToolConfirmations: []ToolConfirmation{
			{QuestionID: "q1", FunctionCallID: "fc1", ToolName: "create_note", ToolArgs: map[string]any{"title": "a"}, Decision: "approve"},
			{QuestionID: "q2", FunctionCallID: "fc2", ToolName: "set_field", ToolArgs: map[string]any{"name": "b"}, Decision: "reject", Message: "too long"},
		},
	}
	m := sig.ToMap()
	back := SuspendSignalFromMap(m)
	require.NotNil(t, back)
	require.Len(t, back.PendingToolConfirmations, 2)
	assert.Equal(t, "q1", back.PendingToolConfirmations[0].QuestionID)
	assert.Equal(t, "fc1", back.PendingToolConfirmations[0].FunctionCallID)
	assert.Equal(t, "create_note", back.PendingToolConfirmations[0].ToolName)
	assert.Equal(t, "approve", back.PendingToolConfirmations[0].Decision)
	assert.Equal(t, "reject", back.PendingToolConfirmations[1].Decision)
	assert.Equal(t, "too long", back.PendingToolConfirmations[1].Message)
}

// --- legacy single-confirmation blob compat ---

func TestSuspendSignalLegacyBlobCompat(t *testing.T) {
	legacy := map[string]any{
		"reason":                    "awaiting_tool_confirm",
		"question_id":               "q1",
		"pending_tool_call_id":      "fc1",
		"pending_tool_name":         "create_note",
		"pending_tool_confirm_args": map[string]any{"title": "a"},
	}
	sc := SuspendSignalFromMap(legacy)
	require.NotNil(t, sc)
	assert.Empty(t, sc.PendingToolConfirmations, "legacy blob must yield an empty batch")
	assert.Equal(t, "q1", sc.QuestionID)
	assert.Equal(t, "fc1", sc.PendingToolCallID)
}

// --- mutex batch accumulator ---

func TestToolConfirmPauseStateConcurrentAdd(t *testing.T) {
	state := &ToolConfirmPauseState{}
	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			state.AddConfirmation(ToolConfirmation{
				QuestionID:     fmt.Sprintf("q%d", i),
				FunctionCallID: fmt.Sprintf("fc%d", i),
				ToolName:       "tool",
			})
		}(i)
	}
	wg.Wait()

	assert.Len(t, state.Confirmations(), n, "no confirmation may be lost under concurrent add")
	assert.True(t, state.HasPending())

	// Confirmations() returns a copy: mutating it must not mutate internal state.
	got := state.Confirmations()
	got[0].Decision = "approve"
	assert.Empty(t, state.Confirmations()[0].Decision, "snapshot must be a copy")
}

// --- confirmResponseBody decision mapping ---

func TestConfirmResponseBodyReject(t *testing.T) {
	ae := &AgentExecutor{}
	body := ae.confirmResponseBody(context.Background(), "", "p", "reject", "too long", "tool", nil)
	assert.Equal(t, "rejected", body["policy_decision"])
	assert.Equal(t, "rejected", body["status"])
	assert.Equal(t, "too long", body["reason"])
}

func TestConfirmResponseBodyCancel(t *testing.T) {
	ae := &AgentExecutor{}
	body := ae.confirmResponseBody(context.Background(), "", "p", "cancel", "", "tool", nil)
	assert.Equal(t, "cancelled", body["policy_decision"])
	assert.Equal(t, "not_taken", body["status"])
}

func TestConfirmResponseBodyDefaultRejects(t *testing.T) {
	ae := &AgentExecutor{}
	body := ae.confirmResponseBody(context.Background(), "", "p", "whatever", "", "tool", nil)
	assert.Equal(t, "rejected", body["policy_decision"])
	assert.Equal(t, "rejected", body["status"])
	_, hasReason := body["reason"]
	assert.False(t, hasReason, "no reason expected when message is empty")
}

// --- orDefaultToolName ---

func TestOrDefaultToolName(t *testing.T) {
	assert.Equal(t, "ask_user", orDefaultToolName(""))
	assert.Equal(t, "create_note", orDefaultToolName("create_note"))
}
