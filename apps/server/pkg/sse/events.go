package sse

// ChatEventType represents the type of SSE event in chat streaming.
type ChatEventType string

const (
	// EventMeta is the first event, containing conversation metadata.
	EventMeta ChatEventType = "meta"

	// EventToken is emitted for each streamed text token.
	EventToken ChatEventType = "token"

	// EventMCPTool is emitted for MCP tool invocations.
	EventMCPTool ChatEventType = "mcp_tool"

	// EventError is emitted when an error occurs during streaming.
	EventError ChatEventType = "error"

	// EventDone is the final event, signaling end of stream.
	EventDone ChatEventType = "done"
)

// MetaEvent is the first event in a chat stream containing metadata.
type MetaEvent struct {
	Type           string    `json:"type"`
	ConversationID string    `json:"conversationId"`
	Citations      []any     `json:"citations"`
	GraphObjects   []any     `json:"graphObjects,omitempty"`
	GraphNeighbors any       `json:"graphNeighbors,omitempty"`
}

// NewMetaEvent creates a new meta event.
func NewMetaEvent(conversationID string) MetaEvent {
	return MetaEvent{
		Type:           string(EventMeta),
		ConversationID: conversationID,
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
	Type string `json:"type"`
}

// NewDoneEvent creates a new done event.
func NewDoneEvent() DoneEvent {
	return DoneEvent{
		Type: string(EventDone),
	}
}
