package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/sessiontodos"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ============================================================================
// Session Todo Tool Definitions and Handlers
// ============================================================================

func sessionTodoToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "session-todo-list",
			Description: "List todos for an agent session. Optionally filter by status (comma-separated: draft,pending,in_progress,completed,cancelled).",
			OutputSchema: typedObjectOutputSchema(map[string]PropertySchema{
				"todos": {Type: "array", Items: &PropertySchema{Type: "object"}},
			}, []string{"todos"}),
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"session_id": {
						Type:        "string",
						Description: "The session ID to list todos for.",
					},
					"statuses": {
						Type:        "string",
						Description: "Comma-separated list of statuses to filter by. If omitted, all todos are returned.",
					},
				},
				Required: []string{"session_id"},
			},
		},
		{
			Name:         "session-todo-update",
			Description:  "Update a session todo's status, content, or sort order.",
			OutputSchema: objectOutputSchema(),
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"session_id": {
						Type:        "string",
						Description: "The session ID the todo belongs to.",
					},
					"todo_id": {
						Type:        "string",
						Description: "The ID of the todo to update.",
					},
					"status": {
						Type:        "string",
						Description: "New status: draft, pending, in_progress, completed, or cancelled.",
					},
					"content": {
						Type:        "string",
						Description: "New content text for the todo.",
					},
					"order": {
						Type:        "integer",
						Description: "New sort order within the session.",
					},
				},
				Required: []string{"session_id", "todo_id"},
			},
		},
	}
}

// sessionTodoOwnerUserID resolves the session-todo tool's caller identity from
// the run context (the user who initiated the run). The project id is passed in
// by ExecuteTool, mirroring session-get-messages. When the user is absent the
// value is empty, so the ownership predicate fails closed in the service.
func sessionTodoOwnerUserID(ctx context.Context) string {
	if u := auth.UserFromContext(ctx); u != nil {
		return u.ID
	}
	return ""
}

func (s *Service) executeSessionTodoList(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.sessionTodoSvc == nil {
		return nil, fmt.Errorf("session todo service not available")
	}
	ownerUserID := sessionTodoOwnerUserID(ctx)
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return nil, fmt.Errorf("session-todo-list: 'session_id' is required")
	}
	var statuses []sessiontodos.TodoStatus
	if statusStr, _ := args["statuses"].(string); statusStr != "" {
		for _, part := range strings.Split(statusStr, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				statuses = append(statuses, sessiontodos.TodoStatus(part))
			}
		}
	}
	todos, err := s.sessionTodoSvc.List(ctx, projectID, ownerUserID, sessionID, statuses)
	if err != nil {
		return nil, err
	}
	return sessionTodoListResult(todos), nil
}

// sessionTodoListResult builds the session-todo-list tool result: the text
// block stays the raw `[...]` array while structuredContent wraps it as
// {"todos": [...]} (issue #586).
func sessionTodoListResult(todos []*sessiontodos.SessionTodo) *ToolResult {
	data, _ := json.Marshal(todos)
	return &ToolResult{
		Content:           []ContentBlock{{Type: "text", Text: string(data)}},
		StructuredContent: map[string]any{"todos": todos},
	}
}

func (s *Service) executeSessionTodoUpdate(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.sessionTodoSvc == nil {
		return nil, fmt.Errorf("session todo service not available")
	}
	ownerUserID := sessionTodoOwnerUserID(ctx)
	sessionID, _ := args["session_id"].(string)
	todoID, _ := args["todo_id"].(string)
	if sessionID == "" || todoID == "" {
		return nil, fmt.Errorf("session-todo-update: 'session_id' and 'todo_id' are required")
	}
	req := sessiontodos.UpdateTodoRequest{}
	if statusStr, _ := args["status"].(string); statusStr != "" {
		st := sessiontodos.TodoStatus(statusStr)
		req.Status = &st
	}
	if content, _ := args["content"].(string); content != "" {
		req.Content = &content
	}
	todo, err := s.sessionTodoSvc.Update(ctx, projectID, ownerUserID, sessionID, todoID, req)
	if err != nil {
		return nil, err
	}
	return s.wrapResultCompact(todo)
}
