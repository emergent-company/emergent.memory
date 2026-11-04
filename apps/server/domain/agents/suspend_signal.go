package agents

// SuspendReason identifies why a run was suspended.
type SuspendReason string

const (
	// SuspendReasonAwaitingHuman means the run is waiting for a human to respond to a question.
	SuspendReasonAwaitingHuman SuspendReason = "awaiting_human"
	// SuspendReasonAwaitingChild means the run is waiting for a spawned child run to complete.
	SuspendReasonAwaitingChild SuspendReason = "awaiting_child"
	// SuspendReasonAwaitingToolConfirm means the run is paused waiting for the user to
	// approve or reject a tool call governed by a tool policy.
	SuspendReasonAwaitingToolConfirm SuspendReason = "awaiting_tool_confirm"
	// SuspendReasonAwaitingClientTool means the run is paused waiting for the OpenAI-compat
	// client to execute a caller-supplied tool and POST the result back.
	SuspendReasonAwaitingClientTool SuspendReason = "awaiting_client_tool"
)

// ToolConfirmation is one pending tool-policy confirmation within a batch. When
// the ADK runner dispatches multiple tool calls from one LLM turn in parallel,
// each intercepted tool produces one ToolConfirmation.
type ToolConfirmation struct {
	QuestionID     string         `json:"question_id"`
	FunctionCallID string         `json:"function_call_id"`
	ToolName       string         `json:"tool_name"`
	ToolArgs       map[string]any `json:"tool_args,omitempty"`
	Decision       string         `json:"decision,omitempty"` // pending, approved, rejected, cancelled
	Message        string         `json:"message,omitempty"`  // optional reject message
}

// ToMap serialises the confirmation for JSONB storage.
func (c ToolConfirmation) ToMap() map[string]any {
	m := map[string]any{
		"question_id":      c.QuestionID,
		"function_call_id": c.FunctionCallID,
		"tool_name":        c.ToolName,
	}
	if c.ToolArgs != nil {
		m["tool_args"] = c.ToolArgs
	}
	if c.Decision != "" {
		m["decision"] = c.Decision
	}
	if c.Message != "" {
		m["message"] = c.Message
	}
	return m
}

// SuspendSignal is set by a tool (or spawn handler) to indicate that the current
// run should be suspended after the tool call completes. The executor's afterToolCb
// checks for this signal and performs the actual pause.
type SuspendSignal struct {
	Reason SuspendReason

	// QuestionID is set when Reason == SuspendReasonAwaitingHuman or SuspendReasonAwaitingToolConfirm.
	QuestionID string

	// WaitingForRunID is set when Reason == SuspendReasonAwaitingChild.
	WaitingForRunID string

	// PendingToolCallID is the function call ID from the LLM that triggered the suspend.
	// On resume, the injected FunctionResponse must reference this ID so the LLM sees
	// a valid reply to its tool invocation.
	PendingToolCallID string

	// PendingToolName is the tool name that triggered the suspend (e.g. "ask_user").
	PendingToolName string

	// PendingToolConfirmArgs holds the original tool args when Reason == SuspendReasonAwaitingToolConfirm.
	// On confirm-resume, the executor injects a synthetic "approved" FunctionResponse.
	// On reject-resume, the executor injects an error FunctionResponse.
	PendingToolConfirmArgs map[string]any

	// PendingToolConfirmations holds the batch of tool-policy confirmations when
	// Reason == SuspendReasonAwaitingToolConfirm and multiple tools in one turn each
	// required confirmation. Empty/nil selects the legacy single-confirmation path.
	PendingToolConfirmations []ToolConfirmation

	// PendingClientToolArgs holds the args for SuspendReasonAwaitingClientTool.
	// The agentcompat layer serialises these into an OpenAI tool_calls response so
	// the caller can execute the function and POST results back.
	PendingClientToolArgs map[string]any
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
	if s.PendingToolConfirmArgs != nil {
		m["pending_tool_confirm_args"] = s.PendingToolConfirmArgs
	}
	if len(s.PendingToolConfirmations) > 0 {
		list := make([]any, 0, len(s.PendingToolConfirmations))
		for _, c := range s.PendingToolConfirmations {
			list = append(list, c.ToMap())
		}
		m["pending_tool_confirmations"] = list
	}
	if s.PendingClientToolArgs != nil {
		m["pending_client_tool_args"] = s.PendingClientToolArgs
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
	if args, ok := m["pending_tool_confirm_args"].(map[string]any); ok {
		s.PendingToolConfirmArgs = args
	}
	if raw, ok := m["pending_tool_confirmations"].([]any); ok {
		s.PendingToolConfirmations = make([]ToolConfirmation, 0, len(raw))
		for _, item := range raw {
			im, ok := item.(map[string]any)
			if !ok {
				continue
			}
			tc := ToolConfirmation{}
			tc.QuestionID, _ = im["question_id"].(string)
			tc.FunctionCallID, _ = im["function_call_id"].(string)
			tc.ToolName, _ = im["tool_name"].(string)
			tc.Decision, _ = im["decision"].(string)
			tc.Message, _ = im["message"].(string)
			if cargs, ok := im["tool_args"].(map[string]any); ok {
				tc.ToolArgs = cargs
			}
			s.PendingToolConfirmations = append(s.PendingToolConfirmations, tc)
		}
	}
	if args, ok := m["pending_client_tool_args"].(map[string]any); ok {
		s.PendingClientToolArgs = args
	}
	return s
}
