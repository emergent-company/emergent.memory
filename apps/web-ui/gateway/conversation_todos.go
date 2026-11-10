package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// conversationTodosResponse is the body of GET /api/conversations/:id/todos.
//
// `todos` is always present and always an array (never null), so a client can
// decode it without a nil check and hide the plan card when it is empty.
type conversationTodosResponse struct {
	Todos []TodoItem `json:"todos"`
}

// getConversationTodos implements GET /api/conversations/:id/todos: the JSON
// twin of the TodoCard fragment (`uiChatTodos`, GET /partial/chat-todos), so the
// iOS chat can show the agent's plan above the composer (issue #440).
//
// Contract: `{"todos":[{"id","content","status","order"}]}`, ordered by the
// server's todo list. The lookup is the partial's, exactly —
// GetConversation → detail.SessionID → ListSessionTodos — and it never fails the
// request: a conversation the caller cannot see (memory answers 404 under its
// owner-or-shared scope), a conversation with no ACP session, a memory error, or
// no todos at all all answer 200 with an empty list. A plan card is not worth a
// banner, and the partial behaves the same way.
//
// It is read-only and on the device surface (deviceConversationReadPath,
// auth.go), so a device credential may read the plan of a conversation its own
// memory scope can see — the same rule as history and events.
func (s *Server) getConversationTodos(c echo.Context) error {
	ctx := c.Request().Context()
	convID := c.Param("id")
	items := []TodoItem{}
	if convID != "" {
		if detail, err := s.memory.GetConversation(ctx, convID); err == nil && detail.SessionID != "" {
			if todos, terr := s.memory.ListSessionTodos(ctx, detail.SessionID); terr == nil {
				items = todoItems(todos)
			} else {
				captureError(terr)
			}
		}
	}
	return c.JSON(http.StatusOK, conversationTodosResponse{Todos: items})
}
