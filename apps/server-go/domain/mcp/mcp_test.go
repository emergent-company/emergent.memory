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
		// Entity tools that require project
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
			name:     "get_entity_edges requires project",
			toolName: "get_entity_edges",
			expected: true,
		},
		// Template pack tools that require project (project-scoped)
		{
			name:     "get_available_templates requires project",
			toolName: "get_available_templates",
			expected: true,
		},
		{
			name:     "get_installed_templates requires project",
			toolName: "get_installed_templates",
			expected: true,
		},
		{
			name:     "assign_template_pack requires project",
			toolName: "assign_template_pack",
			expected: true,
		},
		{
			name:     "update_template_assignment requires project",
			toolName: "update_template_assignment",
			expected: true,
		},
		{
			name:     "uninstall_template_pack requires project",
			toolName: "uninstall_template_pack",
			expected: true,
		},
		// Template pack tools that do NOT require project (global registry)
		{
			name:     "list_template_packs does not require project",
			toolName: "list_template_packs",
			expected: false,
		},
		{
			name:     "get_template_pack does not require project",
			toolName: "get_template_pack",
			expected: false,
		},
		{
			name:     "create_template_pack does not require project",
			toolName: "create_template_pack",
			expected: false,
		},
		{
			name:     "delete_template_pack does not require project",
			toolName: "delete_template_pack",
			expected: false,
		},
		// Global tool
		{
			name:     "schema_version does not require project",
			toolName: "schema_version",
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
		"schema_version",
		"list_entity_types",
		"query_entities",
		"search_entities",
		"get_entity_edges",
		"list_template_packs",
		"get_template_pack",
		"get_available_templates",
		"get_installed_templates",
		"assign_template_pack",
		"update_template_assignment",
		"uninstall_template_pack",
		"create_template_pack",
		"delete_template_pack",
	}

	if len(tools) != len(expectedTools) {
		t.Errorf("GetToolDefinitions() returned %d tools, want %d", len(tools), len(expectedTools))
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

	t.Run("query_entities requires type_name", func(t *testing.T) {
		tool := toolMap["query_entities"]
		if len(tool.InputSchema.Required) == 0 {
			t.Error("query_entities should have required fields")
		}
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "type_name" {
				found = true
				break
			}
		}
		if !found {
			t.Error("query_entities should require type_name")
		}
	})

	t.Run("search_entities requires query", func(t *testing.T) {
		tool := toolMap["search_entities"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "query" {
				found = true
				break
			}
		}
		if !found {
			t.Error("search_entities should require query")
		}
	})

	t.Run("get_template_pack requires pack_id", func(t *testing.T) {
		tool := toolMap["get_template_pack"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "pack_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("get_template_pack should require pack_id")
		}
	})

	t.Run("assign_template_pack requires template_pack_id", func(t *testing.T) {
		tool := toolMap["assign_template_pack"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "template_pack_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("assign_template_pack should require template_pack_id")
		}
	})

	t.Run("create_template_pack requires name, version, object_type_schemas", func(t *testing.T) {
		tool := toolMap["create_template_pack"]
		requiredFields := map[string]bool{"name": false, "version": false, "object_type_schemas": false}
		for _, r := range tool.InputSchema.Required {
			if _, ok := requiredFields[r]; ok {
				requiredFields[r] = true
			}
		}
		for field, found := range requiredFields {
			if !found {
				t.Errorf("create_template_pack should require %s", field)
			}
		}
	})

	t.Run("list_template_packs has optional pagination params", func(t *testing.T) {
		tool := toolMap["list_template_packs"]
		props := tool.InputSchema.Properties
		if _, ok := props["limit"]; !ok {
			t.Error("list_template_packs should have limit property")
		}
		if _, ok := props["page"]; !ok {
			t.Error("list_template_packs should have page property")
		}
		if _, ok := props["search"]; !ok {
			t.Error("list_template_packs should have search property")
		}
	})

	t.Run("get_entity_edges requires entity_id", func(t *testing.T) {
		tool := toolMap["get_entity_edges"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "entity_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("get_entity_edges should require entity_id")
		}
	})

	t.Run("update_template_assignment requires assignment_id", func(t *testing.T) {
		tool := toolMap["update_template_assignment"]
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
		tool := toolMap["uninstall_template_pack"]
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

	t.Run("delete_template_pack requires pack_id", func(t *testing.T) {
		tool := toolMap["delete_template_pack"]
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "pack_id" {
				found = true
				break
			}
		}
		if !found {
			t.Error("delete_template_pack should require pack_id")
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
		data := ListTemplatePacksResult{
			Packs:   []TemplatePackSummary{},
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
