package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		expected   string
	}{
		{
			name:       "valid bearer token",
			authHeader: "Bearer abc123token",
			expected:   "abc123token",
		},
		{
			name:       "valid bearer token with spaces in token",
			authHeader: "Bearer token with spaces",
			expected:   "token with spaces",
		},
		{
			name:       "empty authorization header",
			authHeader: "",
			expected:   "",
		},
		{
			name:       "only Bearer prefix",
			authHeader: "Bearer ",
			expected:   "",
		},
		{
			name:       "Bearer without space",
			authHeader: "Bearertoken",
			expected:   "",
		},
		{
			name:       "lowercase bearer",
			authHeader: "bearer abc123",
			expected:   "",
		},
		{
			name:       "Basic auth instead of Bearer",
			authHeader: "Basic dXNlcjpwYXNz",
			expected:   "",
		},
		{
			name:       "short header less than 7 chars",
			authHeader: "Bear",
			expected:   "",
		},
		{
			name:       "exactly 7 chars but no space",
			authHeader: "Bearer1",
			expected:   "",
		},
		{
			name:       "JWT-like token",
			authHeader: "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			expected:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test request with the authorization header
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			result := extractToken(c)
			if result != tt.expected {
				t.Errorf("extractToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRequiresProject(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{
			name:     "list_entity_types requires project",
			toolName: "list_entity_types",
			expected: true,
		},
		{
			name:     "query_entities requires project",
			toolName: "query_entities",
			expected: true,
		},
		{
			name:     "search_entities requires project",
			toolName: "search_entities",
			expected: true,
		},
		{
			name:     "unknown tool does not require project",
			toolName: "unknown_tool",
			expected: false,
		},
		{
			name:     "empty string does not require project",
			toolName: "",
			expected: false,
		},
		{
			name:     "similar but different name",
			toolName: "list_entities",
			expected: false,
		},
		{
			name:     "case sensitive - uppercase",
			toolName: "LIST_ENTITY_TYPES",
			expected: false,
		},
		{
			name:     "partial match at start",
			toolName: "list_entity",
			expected: false,
		},
		{
			name:     "partial match at end",
			toolName: "entity_types",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := requiresProject(tt.toolName)
			if result != tt.expected {
				t.Errorf("requiresProject(%q) = %v, want %v", tt.toolName, result, tt.expected)
			}
		})
	}
}

func TestRequestIsNotification(t *testing.T) {
	tests := []struct {
		name     string
		id       json.RawMessage
		expected bool
	}{
		{
			name:     "nil ID is notification",
			id:       nil,
			expected: true,
		},
		{
			name:     "empty ID is notification",
			id:       json.RawMessage{},
			expected: true,
		},
		{
			name:     "string ID is not notification",
			id:       json.RawMessage(`"request-1"`),
			expected: false,
		},
		{
			name:     "number ID is not notification",
			id:       json.RawMessage(`123`),
			expected: false,
		},
		{
			name:     "null ID is not notification",
			id:       json.RawMessage(`null`),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{ID: tt.id}
			result := req.IsNotification()
			if result != tt.expected {
				t.Errorf("IsNotification() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRequestGetIDString(t *testing.T) {
	tests := []struct {
		name     string
		id       json.RawMessage
		expected string
	}{
		{
			name:     "nil ID returns notification marker",
			id:       nil,
			expected: "<notification>",
		},
		{
			name:     "empty ID returns notification marker",
			id:       json.RawMessage{},
			expected: "<notification>",
		},
		{
			name:     "string ID returns raw value",
			id:       json.RawMessage(`"request-1"`),
			expected: `"request-1"`,
		},
		{
			name:     "number ID returns raw value",
			id:       json.RawMessage(`42`),
			expected: "42",
		},
		{
			name:     "null ID returns null string",
			id:       json.RawMessage(`null`),
			expected: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{ID: tt.id}
			result := req.GetIDString()
			if result != tt.expected {
				t.Errorf("GetIDString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestNewErrorResponse(t *testing.T) {
	tests := []struct {
		name    string
		id      json.RawMessage
		code    int
		message string
		data    any
	}{
		{
			name:    "parse error",
			id:      json.RawMessage(`"req-1"`),
			code:    ErrCodeParseError,
			message: "Parse error",
			data:    nil,
		},
		{
			name:    "invalid request with data",
			id:      json.RawMessage(`123`),
			code:    ErrCodeInvalidRequest,
			message: "Invalid request",
			data:    map[string]string{"field": "method"},
		},
		{
			name:    "internal error",
			id:      nil,
			code:    ErrCodeInternalError,
			message: "Internal error",
			data:    "stack trace here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := NewErrorResponse(tt.id, tt.code, tt.message, tt.data)

			if resp.JSONRPC != "2.0" {
				t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, "2.0")
			}
			if string(resp.ID) != string(tt.id) {
				t.Errorf("ID = %v, want %v", resp.ID, tt.id)
			}
			if resp.Result != nil {
				t.Errorf("Result = %v, want nil", resp.Result)
			}
			if resp.Error == nil {
				t.Fatal("Error is nil")
			}
			if resp.Error.Code != tt.code {
				t.Errorf("Error.Code = %d, want %d", resp.Error.Code, tt.code)
			}
			if resp.Error.Message != tt.message {
				t.Errorf("Error.Message = %q, want %q", resp.Error.Message, tt.message)
			}
		})
	}
}

func TestNewSuccessResponse(t *testing.T) {
	tests := []struct {
		name   string
		id     json.RawMessage
		result any
	}{
		{
			name:   "nil result",
			id:     json.RawMessage(`"req-1"`),
			result: nil,
		},
		{
			name:   "string result",
			id:     json.RawMessage(`123`),
			result: "success",
		},
		{
			name:   "map result",
			id:     json.RawMessage(`"req-2"`),
			result: map[string]any{"status": "ok", "count": 42},
		},
		{
			name:   "slice result",
			id:     json.RawMessage(`1`),
			result: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := NewSuccessResponse(tt.id, tt.result)

			if resp.JSONRPC != "2.0" {
				t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, "2.0")
			}
			if string(resp.ID) != string(tt.id) {
				t.Errorf("ID = %v, want %v", resp.ID, tt.id)
			}
			if resp.Error != nil {
				t.Errorf("Error = %v, want nil", resp.Error)
			}
			// Result comparison depends on type
			if tt.result == nil && resp.Result != nil {
				t.Errorf("Result = %v, want nil", resp.Result)
			}
		})
	}
}

func TestIntPtr(t *testing.T) {
	tests := []struct {
		name  string
		input int
	}{
		{
			name:  "zero",
			input: 0,
		},
		{
			name:  "positive",
			input: 42,
		},
		{
			name:  "negative",
			input: -10,
		},
		{
			name:  "max int",
			input: int(^uint(0) >> 1),
		},
		{
			name:  "min int",
			input: -int(^uint(0)>>1) - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := intPtr(tt.input)
			if result == nil {
				t.Fatal("intPtr returned nil")
			}
			if *result != tt.input {
				t.Errorf("intPtr(%d) = %d, want %d", tt.input, *result, tt.input)
			}
		})
	}
}

func TestIntPtrReturnsUniquePointers(t *testing.T) {
	// Ensure each call returns a new pointer
	ptr1 := intPtr(5)
	ptr2 := intPtr(5)

	if ptr1 == ptr2 {
		t.Error("intPtr should return unique pointers for each call")
	}
	if *ptr1 != *ptr2 {
		t.Error("intPtr pointers should have equal values")
	}
}
