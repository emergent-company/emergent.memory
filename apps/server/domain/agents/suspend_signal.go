package agents

// SuspendReason identifies why a run was suspended.
type SuspendReason string

const (
	// SuspendReasonAwaitingHuman means the run is waiting for a human to respond to a question.
	SuspendReasonAwaitingHuman SuspendReason = "awaiting_human"
	// SuspendReasonAwaitingChild means the run is waiting for a spawned child run to complete.
	SuspendReasonAwaitingChild SuspendReason = "awaiting_child"
)

// SuspendSignal is set by a tool (or spawn handler) to indicate that the current
// run should be suspended after the tool call completes. The executor's afterToolCb
// checks for this signal and performs the actual pause.
type SuspendSignal struct {
	Reason SuspendReason

	// QuestionID is set when Reason == SuspendReasonAwaitingHuman.
	QuestionID string

	// WaitingForRunID is set when Reason == SuspendReasonAwaitingChild.
	WaitingForRunID string

	// PendingToolCallID is the function call ID from the LLM that triggered the suspend.
	// On resume, the injected FunctionResponse must reference this ID so the LLM sees
	// a valid reply to its tool invocation.
	PendingToolCallID string

	// PendingToolName is the tool name that triggered the suspend (e.g. "ask_user").
	PendingToolName string
}

// ToMap serialises the SuspendSignal for storage as JSONB suspend_context.
func (s SuspendSignal) ToMap() map[string]any {
	m := map[string]any{
		"reason":               string(s.Reason),
		"pending_tool_call_id": s.PendingToolCallID,
		"pending_tool_name":    s.PendingToolName,
	}
	if s.QuestionID != "" {
		m["question_id"] = s.QuestionID
	}
	if s.WaitingForRunID != "" {
		m["waiting_for_run_id"] = s.WaitingForRunID
	}
	return m
}

// SuspendSignalFromMap deserialises a suspend_context JSONB map back into a SuspendSignal.
// Returns nil if m is nil or missing required fields.
func SuspendSignalFromMap(m map[string]any) *SuspendSignal {
	if m == nil {
		return nil
	}
	reason, _ := m["reason"].(string)
	if reason == "" {
		return nil
	}
	s := &SuspendSignal{
		Reason: SuspendReason(reason),
	}
	s.QuestionID, _ = m["question_id"].(string)
	s.WaitingForRunID, _ = m["waiting_for_run_id"].(string)
	s.PendingToolCallID, _ = m["pending_tool_call_id"].(string)
	s.PendingToolName, _ = m["pending_tool_name"].(string)
	return s
}
