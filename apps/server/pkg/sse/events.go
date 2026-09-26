package sse

import "github.com/emergent-company/emergent.memory/pkg/a2ui"

// ChatEventType represents the type of SSE event in chat streaming.
type ChatEventType string

const (
	// EventMeta is the first event, containing conversation metadata.
	EventMeta ChatEventType = "meta"

	// EventToken is emitted for each streamed text token.
	EventToken ChatEventType = "token"

	// EventThinking is emitted for the agent's reasoning/planning text, so
	// clients can surface it separately from the final answer.
	EventThinking ChatEventType = "thinking"

	// EventMCPTool is emitted for MCP tool invocations.
	EventMCPTool ChatEventType = "mcp_tool"

	// EventUI is emitted when the agent produces declarative A2UI surface
	// messages (structured cards).
	EventUI ChatEventType = "ui"

	// EventError is emitted when an error occurs during streaming.
	EventError ChatEventType = "error"

	// EventApproval is emitted when a tool-policy confirmation gate intercepts
	// a tool call and pauses the run awaiting user approval.
	EventApproval ChatEventType = "approval"

	// EventDone is the final event, signaling end of stream.
	EventDone ChatEventType = "done"
)

// MetaEvent is the first event in a chat stream containing metadata.
type MetaEvent struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversationId"`
	RunID          string `json:"runId,omitempty"`
	Citations      []any  `json:"citations"`
	GraphObjects   []any  `json:"graphObjects,omitempty"`
	GraphNeighbors any    `json:"graphNeighbors,omitempty"`
}

// NewMetaEvent creates a new meta event.
func NewMetaEvent(conversationID string) MetaEvent {
	return MetaEvent{
		Type:           string(EventMeta),
		ConversationID: conversationID,
		Citations:      []any{},
	}
}

// NewMetaEventWithRun creates a meta event that also carries the agent run ID.
func NewMetaEventWithRun(conversationID, runID string) MetaEvent {
	return MetaEvent{
		Type:           string(EventMeta),
		ConversationID: conversationID,
		RunID:          runID,
		Citations:      []any{},
	}
}

// TokenEvent is emitted for each streamed text token.
type TokenEvent struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

// NewTokenEvent creates a new token event.
func NewTokenEvent(token string) TokenEvent {
	return TokenEvent{
		Type:  string(EventToken),
		Token: token,
	}
}

// ThinkingEvent is emitted for the agent's reasoning/planning text, so clients
// can surface it separately from the final answer.
type ThinkingEvent struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Role string `json:"role"` // "operator" (planning) or "reasoning" (chain-of-thought)
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// NewThinkingEvent creates a new thinking event. id is a stable identifier for
// the reasoning segment so clients can group incremental deltas; role selects
// "operator" (planning) or "reasoning" (chain-of-thought).
func NewThinkingEvent(id, role, text string) ThinkingEvent {
	return ThinkingEvent{
		Type: string(EventThinking),
		ID:   id,
		Role: role,
		Text: text,
		Done: true,
	}
}

// MCPToolEvent is emitted for MCP tool invocations.
type MCPToolEvent struct {
	Type   string `json:"type"`
	Tool   string `json:"tool"`
	Status string `json:"status"` // "started", "completed", "error"
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// NewMCPToolEvent creates a new MCP tool event.
func NewMCPToolEvent(tool, status string, result any, errMsg string) MCPToolEvent {
	return MCPToolEvent{
		Type:   string(EventMCPTool),
		Tool:   tool,
		Status: status,
		Result: result,
		Error:  errMsg,
	}
}

// UIEvent is emitted when the agent produces declarative A2UI surface messages.
type UIEvent struct {
	Type      string         `json:"type"`
	SurfaceID string         `json:"surfaceId"`
	Messages  []a2ui.Message `json:"messages"`
}

// NewUIEvent creates a new structured-UI event.
func NewUIEvent(surfaceID string, messages []a2ui.Message) UIEvent {
	return UIEvent{
		Type:      string(EventUI),
		SurfaceID: surfaceID,
		Messages:  messages,
	}
}

// ApprovalEvent is emitted when a tool-policy confirmation gate intercepts a
// tool call and pauses the run awaiting user approval. The gateway surfaces it
// as an approval card in the chat stream.
type ApprovalEvent struct {
	Type       string         `json:"type"`
	Tool       string         `json:"tool"`
	Input      map[string]any `json:"input,omitempty"`
	QuestionID string         `json:"questionId"`
}

// NewApprovalEvent creates a new approval event.
func NewApprovalEvent(tool string, input map[string]any, questionID string) ApprovalEvent {
	return ApprovalEvent{
		Type:       string(EventApproval),
		Tool:       tool,
		Input:      input,
		QuestionID: questionID,
	}
}

// ErrorEvent is emitted when an error occurs during streaming.
type ErrorEvent struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

// NewErrorEvent creates a new error event.
func NewErrorEvent(errMsg string) ErrorEvent {
	return ErrorEvent{
		Type:  string(EventError),
		Error: errMsg,
	}
}

// DoneEvent is the final event signaling end of stream.
type DoneEvent struct {
	Type    string `json:"type"`
	RunID   string `json:"runId,omitempty"`
	TraceID string `json:"traceId,omitempty"`
}

// NewDoneEvent creates a new done event.
func NewDoneEvent() DoneEvent {
	return DoneEvent{
		Type: string(EventDone),
	}
}

// NewDoneEventWithRun creates a done event that also carries the agent run ID.
func NewDoneEventWithRun(runID string) DoneEvent {
	return DoneEvent{
		Type:  string(EventDone),
		RunID: runID,
	}
}

// NewDoneEventWithRunAndTrace creates a done event carrying the agent run ID
// and traceId so external callers can query traces without a DB lookup.
func NewDoneEventWithRunAndTrace(runID, traceID string) DoneEvent {
	return DoneEvent{
		Type:    string(EventDone),
		RunID:   runID,
		TraceID: traceID,
	}
}
