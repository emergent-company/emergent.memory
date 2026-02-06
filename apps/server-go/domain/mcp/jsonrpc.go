package mcp

import "encoding/json"

// JSON-RPC 2.0 types for MCP protocol

// Request represents a JSON-RPC 2.0 request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // Can be string, number, or null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

// ErrorObject represents a JSON-RPC 2.0 error
type ErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC 2.0 error codes
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603

	// Custom MCP error codes
	ErrCodeUnauthorized = -32001
	ErrCodeForbidden    = -32002
	ErrCodeNotFound     = -32003
)

// MCP Protocol constants
var SupportedProtocolVersions = []string{"2025-06-18", "2025-11-25"}

const LatestProtocolVersion = "2025-11-25"

var ServerInfo = map[string]string{
	"name":    "emergent-mcp-server-go",
	"version": "1.0.0",
}

// NewErrorResponse creates a JSON-RPC error response
func NewErrorResponse(id json.RawMessage, code int, message string, data any) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &ErrorObject{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// NewSuccessResponse creates a JSON-RPC success response
func NewSuccessResponse(id json.RawMessage, result any) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// IsNotification checks if request is a notification (no ID)
func (r *Request) IsNotification() bool {
	return r.ID == nil || len(r.ID) == 0
}

// GetIDString returns the ID as a string (for logging)
func (r *Request) GetIDString() string {
	if r.ID == nil || len(r.ID) == 0 {
		return "<notification>"
	}
	return string(r.ID)
}
