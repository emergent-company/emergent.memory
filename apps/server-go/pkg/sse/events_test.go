package sse

import (
	"testing"
)

func TestNewMetaEvent(t *testing.T) {
	tests := []struct {
		name           string
		conversationID string
	}{
		{
			name:           "with valid conversation ID",
			conversationID: "conv-123",
		},
		{
			name:           "with empty conversation ID",
			conversationID: "",
		},
		{
			name:           "with UUID conversation ID",
			conversationID: "550e8400-e29b-41d4-a716-446655440000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewMetaEvent(tt.conversationID)

			if event.Type != string(EventMeta) {
				t.Errorf("Type = %q, want %q", event.Type, string(EventMeta))
			}
			if event.ConversationID != tt.conversationID {
				t.Errorf("ConversationID = %q, want %q", event.ConversationID, tt.conversationID)
			}
			if event.Citations == nil {
				t.Error("Citations should not be nil")
			}
			if len(event.Citations) != 0 {
				t.Errorf("Citations should be empty, got %d items", len(event.Citations))
			}
		})
	}
}

func TestNewTokenEvent(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "with simple token",
			token: "hello",
		},
		{
			name:  "with empty token",
			token: "",
		},
		{
			name:  "with whitespace token",
			token: " ",
		},
		{
			name:  "with multi-word token",
			token: "Hello, World!",
		},
		{
			name:  "with special characters",
			token: "<script>alert('xss')</script>",
		},
		{
			name:  "with unicode token",
			token: "你好世界",
		},
		{
			name:  "with newline token",
			token: "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewTokenEvent(tt.token)

			if event.Type != string(EventToken) {
				t.Errorf("Type = %q, want %q", event.Type, string(EventToken))
			}
			if event.Token != tt.token {
				t.Errorf("Token = %q, want %q", event.Token, tt.token)
			}
		})
	}
}

func TestNewMCPToolEvent(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		status     string
		result     any
		errMsg     string
		wantResult any
		wantError  string
	}{
		{
			name:       "started status",
			tool:       "search",
			status:     "started",
			result:     nil,
			errMsg:     "",
			wantResult: nil,
			wantError:  "",
		},
		{
			name:       "completed with result",
			tool:       "calculator",
			status:     "completed",
			result:     map[string]int{"answer": 42},
			errMsg:     "",
			wantResult: map[string]int{"answer": 42},
			wantError:  "",
		},
		{
			name:       "error status",
			tool:       "database",
			status:     "error",
			result:     nil,
			errMsg:     "connection timeout",
			wantResult: nil,
			wantError:  "connection timeout",
		},
		{
			name:       "empty tool name",
			tool:       "",
			status:     "started",
			result:     nil,
			errMsg:     "",
			wantResult: nil,
			wantError:  "",
		},
		{
			name:       "completed with string result",
			tool:       "echo",
			status:     "completed",
			result:     "echoed value",
			errMsg:     "",
			wantResult: "echoed value",
			wantError:  "",
		},
		{
			name:       "completed with array result",
			tool:       "list_files",
			status:     "completed",
			result:     []string{"file1.txt", "file2.txt"},
			errMsg:     "",
			wantResult: []string{"file1.txt", "file2.txt"},
			wantError:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewMCPToolEvent(tt.tool, tt.status, tt.result, tt.errMsg)

			if event.Type != string(EventMCPTool) {
				t.Errorf("Type = %q, want %q", event.Type, string(EventMCPTool))
			}
			if event.Tool != tt.tool {
				t.Errorf("Tool = %q, want %q", event.Tool, tt.tool)
			}
			if event.Status != tt.status {
				t.Errorf("Status = %q, want %q", event.Status, tt.status)
			}
			if event.Error != tt.wantError {
				t.Errorf("Error = %q, want %q", event.Error, tt.wantError)
			}
			// Note: Can't easily compare Result due to interface{} type
			// Just check it's set when expected
			if tt.wantResult == nil && event.Result != nil {
				t.Errorf("Result should be nil, got %v", event.Result)
			}
			if tt.wantResult != nil && event.Result == nil {
				t.Error("Result should not be nil")
			}
		})
	}
}

func TestNewErrorEvent(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
	}{
		{
			name:   "simple error message",
			errMsg: "something went wrong",
		},
		{
			name:   "empty error message",
			errMsg: "",
		},
		{
			name:   "detailed error message",
			errMsg: "error: database connection failed: timeout after 30s",
		},
		{
			name:   "error with special characters",
			errMsg: "error: invalid JSON at position 5: unexpected '}'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewErrorEvent(tt.errMsg)

			if event.Type != string(EventError) {
				t.Errorf("Type = %q, want %q", event.Type, string(EventError))
			}
			if event.Error != tt.errMsg {
				t.Errorf("Error = %q, want %q", event.Error, tt.errMsg)
			}
		})
	}
}

func TestNewDoneEvent(t *testing.T) {
	event := NewDoneEvent()

	if event.Type != string(EventDone) {
		t.Errorf("Type = %q, want %q", event.Type, string(EventDone))
	}
}

func TestChatEventTypeConstants(t *testing.T) {
	// Verify constants have expected values
	tests := []struct {
		name     string
		constant ChatEventType
		expected string
	}{
		{"EventMeta", EventMeta, "meta"},
		{"EventToken", EventToken, "token"},
		{"EventMCPTool", EventMCPTool, "mcp_tool"},
		{"EventError", EventError, "error"},
		{"EventDone", EventDone, "done"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, string(tt.constant), tt.expected)
			}
		})
	}
}

func TestMetaEventFields(t *testing.T) {
	// Test that MetaEvent can hold optional fields
	event := NewMetaEvent("test-conv")
	event.GraphObjects = []any{"obj1", "obj2"}
	event.GraphNeighbors = map[string]any{"neighbor1": true}

	if len(event.GraphObjects) != 2 {
		t.Errorf("GraphObjects length = %d, want 2", len(event.GraphObjects))
	}
	if event.GraphNeighbors == nil {
		t.Error("GraphNeighbors should not be nil after setting")
	}
}
