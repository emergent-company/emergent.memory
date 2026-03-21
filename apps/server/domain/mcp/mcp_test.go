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
		apiKey     string
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
		{
			name:     "API key only",
			apiKey:   "test-api-key-123",
			expected: "test-api-key-123",
		},
		{
			name:       "bearer token takes precedence over API key",
			authHeader: "Bearer bearer-token",
			apiKey:     "api-key-ignored",
			expected:   "bearer-token",
		},
		{
			name:     "API key with underscore and dot",
			apiKey:   "test-key_123.abc",
			expected: "test-key_123.abc",
		},
		{
			name:       "invalid bearer falls back to API key",
			authHeader: "bearer lowercase",
			apiKey:     "fallback-key",
			expected:   "fallback-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
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
		// Entity tools that require project
		{
			name:     "list_entity_types requires project",
			toolName: "entity-type-list",
			expected: true,
		},
		{
			name:     "entity-query requires project",
			toolName: "entity-query",
			expected: true,
		},
		{
			name:     "entity-search requires project",
			toolName: "entity-search",
			expected: true,
		},
		{
			name:     "entity-edges-get requires project",
			toolName: "entity-edges-get",
			expected: true,
		},
		// Template pack tools that require project (project-scoped)
		{
			name:     "get_available_templates requires project",
			toolName: "schema-list-available",
			expected: true,
		},
		{
			name:     "get_installed_templates requires project",
			toolName: "schema-list-installed",
			expected: true,
		},
		{
			name:     "assign_schema requires project",
			toolName: "schema-assign",
			expected: true,
		},
		{
			name:     "update_template_assignment requires project",
			toolName: "schema-assignment-update",
			expected: true,
		},
		{
			name:     "uninstall_template_pack requires project",
			toolName: "schema-uninstall",
			expected: true,
		},
		// Template pack tools that do NOT require project (global registry)
		{
			name:     "template-list does not require project",
			toolName: "schema-list",
			expected: false,
		},
		{
			name:     "template-get does not require project",
			toolName: "schema-get",
			expected: false,
		},
		{
			name:     "template-create does not require project",
			toolName: "schema-create",
			expected: false,
		},
		{
			name:     "template-delete does not require project",
			toolName: "schema-delete",
			expected: false,
		},
		// Global tool
		{
			name:     "schema_version does not require project",
			toolName: "schema-version",
			expected: false,
		},
		{
			name:     "search-knowledge does not require project via requiresProject",
			toolName: "search-knowledge",
			expected: false,
		},
		{
			name:     "journal-list does not require project via requiresProject",
			toolName: "journal-list",
			expected: false,
		},
		{
			name:     "journal-add-note does not require project via requiresProject",
			toolName: "journal-add-note",
			expected: false,
		},
		// Edge cases
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

func TestGetToolDefinitions(t *testing.T) {
	svc := &Service{}
	tools := svc.GetToolDefinitions()

	expectedTools := []string{
		"project-get",
		"schema-version",
		"entity-type-list",
		"entity-query",
		"entity-search",
		"entity-edges-get",
		"schema-list",
		"schema-get",
		"schema-list-available",
		"schema-list-installed",
		"schema-assign",
		"schema-assignment-update",
		"schema-uninstall",
		"schema-create",
		"schema-delete",
		"entity-create",
		"relationship-create",
		"entity-update",
		"entity-delete",
		"entity-restore",
		"search-hybrid",
		"search-semantic",
		"search-similar",
		"graph-traverse",
		"relationship-list",
		"relationship-update",
		"relationship-delete",
		"tag-list",
		"schema-migration-preview",
		"migration-archive-list",
		"migration-archive-get",
		"web-search-brave",
		"web-fetch",
		"web-search-reddit",
		"search-knowledge",
		"journal-list",
		"journal-add-note",
	}

	if len(tools) < len(expectedTools) {
		t.Errorf("GetToolDefinitions() returned %d tools, want at least %d", len(tools), len(expectedTools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true

		if tool.Description == "" {
			t.Errorf("Tool %q has empty description", tool.Name)
		}
		if tool.InputSchema.Type != "object" {
			t.Errorf("Tool %q has InputSchema.Type = %q, want \"object\"", tool.Name, tool.InputSchema.Type)
		}
	}

	for _, expectedName := range expectedTools {
		if !toolNames[expectedName] {
			t.Errorf("Expected tool %q not found in GetToolDefinitions()", expectedName)
		}
	}
}

func TestToolInputSchemas(t *testing.T) {
	svc := &Service{}
	tools := svc.GetToolDefinitions()

	toolMap := make(map[string]ToolDefinition)
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	t.Run("entity-query requires type_name", func(t *testing.T) {
		tool := toolMap["entity-query"]
		if len(tool.InputSchema.Required) == 0 {
			t.Error("entity-query should have required fields")
		}
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "type_name" {
				found = true
				break
			}
		}
		if !found {
			t.Error("entity-query should require type_name")
		}
	})

	t.Run("entity-search requires query", func(t *testing.T) {
		tool := toolMap["entity-search"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "query" {
				found = true
				break
			}
		}
		if !found {
			t.Error("entity-search should require query")
		}
	})

	t.Run("schema-get requires schema_id", func(t *testing.T) {
		tool := toolMap["schema-get"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "schema_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("schema-get should require schema_id")
		}
	})

	t.Run("assign_schema requires schema_id", func(t *testing.T) {
		tool := toolMap["schema-assign"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "schema_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("assign_schema should require schema_id")
		}
	})

	t.Run("template-create requires name, version, object_type_schemas", func(t *testing.T) {
		tool := toolMap["schema-create"]
		requiredFields := map[string]bool{"name": false, "version": false, "object_type_schemas": false}
		for _, r := range tool.InputSchema.Required {
			if _, ok := requiredFields[r]; ok {
				requiredFields[r] = true
			}
		}
		for field, found := range requiredFields {
			if !found {
				t.Errorf("template-create should require %s", field)
			}
		}
	})

	t.Run("template-list has optional pagination params", func(t *testing.T) {
		tool := toolMap["schema-list"]
		props := tool.InputSchema.Properties
		if _, ok := props["limit"]; !ok {
			t.Error("template-list should have limit property")
		}
		if _, ok := props["page"]; !ok {
			t.Error("template-list should have page property")
		}
		if _, ok := props["search"]; !ok {
			t.Error("template-list should have search property")
		}
	})

	t.Run("entity-edges-get requires entity_id", func(t *testing.T) {
		tool := toolMap["entity-edges-get"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "entity_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("entity-edges-get should require entity_id")
		}
	})

	t.Run("update_template_assignment requires assignment_id", func(t *testing.T) {
		tool := toolMap["schema-assignment-update"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "assignment_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("update_template_assignment should require assignment_id")
		}
	})

	t.Run("uninstall_template_pack requires assignment_id", func(t *testing.T) {
		tool := toolMap["schema-uninstall"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "assignment_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("uninstall_template_pack should require assignment_id")
		}
	})

	t.Run("schema-delete requires schema_id", func(t *testing.T) {
		tool := toolMap["schema-delete"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "schema_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("schema-delete should require schema_id")
		}
	})
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
	ptr1 := intPtr(5)
	ptr2 := intPtr(5)

	if ptr1 == ptr2 {
		t.Error("intPtr should return unique pointers for each call")
	}
	if *ptr1 != *ptr2 {
		t.Error("intPtr pointers should have equal values")
	}
}

func TestExecuteToolUnknownTool(t *testing.T) {
	svc := &Service{}

	_, err := svc.ExecuteTool(nil, "some-project-id", "nonexistent_tool", nil)

	if err == nil {
		t.Error("ExecuteTool should return error for unknown tool")
	}

	expectedMsg := "tool not found: nonexistent_tool"
	if err.Error() != expectedMsg {
		t.Errorf("ExecuteTool error = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestExecuteToolRouting(t *testing.T) {
	svc := &Service{}

	unknownTools := []string{
		"nonexistent_tool",
		"foo_bar",
		"LIST_ENTITY_TYPES",
	}

	for _, toolName := range unknownTools {
		t.Run(toolName+" returns tool not found", func(t *testing.T) {
			_, err := svc.ExecuteTool(nil, "", toolName, nil)

			if err == nil {
				t.Errorf("ExecuteTool(%q) should return error", toolName)
				return
			}
			expectedErr := "tool not found: " + toolName
			if err.Error() != expectedErr {
				t.Errorf("ExecuteTool(%q) error = %q, want %q", toolName, err.Error(), expectedErr)
			}
		})
	}
}

func TestWrapResult(t *testing.T) {
	svc := &Service{}

	t.Run("wraps simple struct", func(t *testing.T) {
		data := SchemaVersionResult{
			Version:      "abc123",
			Timestamp:    "2025-02-08T10:00:00Z",
			PackCount:    5,
			CacheHintTTL: 300,
		}

		result, err := svc.wrapResult(data)
		if err != nil {
			t.Fatalf("wrapResult error: %v", err)
		}

		if len(result.Content) != 1 {
			t.Errorf("Content length = %d, want 1", len(result.Content))
		}
		if result.Content[0].Type != "text" {
			t.Errorf("Content[0].Type = %q, want \"text\"", result.Content[0].Type)
		}
		if result.Content[0].Text == "" {
			t.Error("Content[0].Text should not be empty")
		}
	})

	t.Run("wraps list result", func(t *testing.T) {
		data := ListSchemasResult{
			Packs:   []MemorySchemaSummary{},
			Total:   0,
			Page:    1,
			Limit:   20,
			HasMore: false,
		}

		result, err := svc.wrapResult(data)
		if err != nil {
			t.Fatalf("wrapResult error: %v", err)
		}

		if len(result.Content) != 1 {
			t.Errorf("Content length = %d, want 1", len(result.Content))
		}
	})
}
