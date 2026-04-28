package sessiontodos

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Handler handles HTTP requests for session todos.
type Handler struct {
	svc *Service
}

// NewHandler creates a new session todos handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List handles GET /api/v1/agent/sessions/:sessionId/todos
// @Summary      List session todos
// @Tags         session-todos
// @Param        sessionId  path   string  true  "Session ID"
// @Param        status     query  string  false "Comma-separated status filter"
// @Success      200  {array}   SessionTodo
// @Failure      400  {object}  apperror.Error
// @Failure      401  {object}  apperror.Error
// @Router       /api/v1/agent/sessions/{sessionId}/todos [get]
func (h *Handler) List(c echo.Context) error {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return apperror.NewBadRequest("sessionId is required")
	}
	var statuses []TodoStatus
	if raw := c.QueryParam("status"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			statuses = append(statuses, TodoStatus(strings.TrimSpace(s)))
		}
	}
	todos, err := h.svc.List(c.Request().Context(), sessionID, statuses)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, todos)
}

// Create handles POST /api/v1/agent/sessions/:sessionId/todos
// @Summary      Create a session todo
// @Tags         session-todos
// @Param        sessionId  path   string              true  "Session ID"
// @Param        body       body   CreateTodoRequest   true  "Todo"
// @Success      201  {object}  SessionTodo
// @Failure      400  {object}  apperror.Error
// @Failure      401  {object}  apperror.Error
// @Router       /api/v1/agent/sessions/{sessionId}/todos [post]
func (h *Handler) Create(c echo.Context) error {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return apperror.NewBadRequest("sessionId is required")
	}
	var req CreateTodoRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	todo, err := h.svc.Create(c.Request().Context(), sessionID, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, todo)
}

// Update handles PATCH /api/v1/agent/sessions/:sessionId/todos/:todoId
// @Summary      Update a session todo
// @Tags         session-todos
// @Param        sessionId  path   string             true  "Session ID"
// @Param        todoId     path   string             true  "Todo ID"
// @Param        body       body   UpdateTodoRequest  true  "Updates"
// @Success      200  {object}  SessionTodo
// @Failure      400  {object}  apperror.Error
// @Failure      404  {object}  apperror.Error
// @Router       /api/v1/agent/sessions/{sessionId}/todos/{todoId} [patch]
func (h *Handler) Update(c echo.Context) error {
	sessionID := c.Param("sessionId")
	todoID := c.Param("todoId")
	if sessionID == "" || todoID == "" {
		return apperror.NewBadRequest("sessionId and todoId are required")
	}
	var req UpdateTodoRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	todo, err := h.svc.Update(c.Request().Context(), sessionID, todoID, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, todo)
}

// Delete handles DELETE /api/v1/agent/sessions/:sessionId/todos/:todoId
// @Summary      Delete a session todo
// @Tags         session-todos
// @Param        sessionId  path  string  true  "Session ID"
// @Param        todoId     path  string  true  "Todo ID"
// @Success      204
// @Failure      404  {object}  apperror.Error
// @Router       /api/v1/agent/sessions/{sessionId}/todos/{todoId} [delete]
func (h *Handler) Delete(c echo.Context) error {
	sessionID := c.Param("sessionId")
	todoID := c.Param("todoId")
	if sessionID == "" || todoID == "" {
		return apperror.NewBadRequest("sessionId and todoId are required")
	}
	if err := h.svc.Delete(c.Request().Context(), sessionID, todoID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
