package mcp

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

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
var SupportedProtocolVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"}

const LatestProtocolVersion = "2025-11-25"

var ServerInfo = map[string]string{
	"name":    "memory-mcp-server-go",
	"version": "1.0.0",
}

// jsonRPCErrorCodeFor maps an error to the JSON-RPC code that describes it.
//
// A transport that answers every service failure with internal-error tells the
// client "retry" for a URI or prompt name it should correct instead. Only an app
// error carries a status worth mapping; anything else is genuinely internal.
func jsonRPCErrorCodeFor(err error) int {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return ErrCodeInternalError
	}
	switch appErr.HTTPStatus {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ErrCodeInvalidParams
	case http.StatusUnauthorized:
		return ErrCodeUnauthorized
	case http.StatusForbidden:
		return ErrCodeForbidden
	case http.StatusNotFound:
		return ErrCodeNotFound
	default:
		return ErrCodeInternalError
	}
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
