package tasks

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for tasks
type Handler struct {
	svc *Service
}

// NewHandler creates a new tasks handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetCounts handles GET /api/tasks/counts
// Returns task counts by status for a specific project
func (h *Handler) GetCounts(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.QueryParam("project_id")
	if projectID == "" {
		// Try to get from header (X-Project-ID)
		projectID = c.Request().Header.Get("X-Project-ID")
	}

	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id is required")
	}

	counts, err := h.svc.GetCountsByProject(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, TaskCountsResponse{
		Pending:   counts.Pending,
		Accepted:  counts.Accepted,
		Rejected:  counts.Rejected,
		Cancelled: counts.Cancelled,
	})
}

// GetAllCounts handles GET /api/tasks/all/counts
// Returns task counts by status across all accessible projects
func (h *Handler) GetAllCounts(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	counts, err := h.svc.GetAllCounts(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, TaskCountsResponse{
		Pending:   counts.Pending,
		Accepted:  counts.Accepted,
		Rejected:  counts.Rejected,
		Cancelled: counts.Cancelled,
	})
}

// List handles GET /api/tasks
// Returns tasks for a specific project
func (h *Handler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.QueryParam("project_id")
	if projectID == "" {
		projectID = c.Request().Header.Get("X-Project-ID")
	}

	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id is required")
	}

	params := TaskListParams{
		ProjectID: projectID,
		Status:    c.QueryParam("status"),
		Type:      c.QueryParam("type"),
	}

	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			params.Limit = l
		}
	}

	if offsetStr := c.QueryParam("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			params.Offset = o
		}
	}

	result, err := h.svc.List(c.Request().Context(), params)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// ListAll handles GET /api/tasks/all
// Returns tasks across all accessible projects
func (h *Handler) ListAll(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	params := TaskListParams{
		Status: c.QueryParam("status"),
		Type:   c.QueryParam("type"),
	}

	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			params.Limit = l
		}
	}

	if offsetStr := c.QueryParam("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			params.Offset = o
		}
	}

	result, err := h.svc.ListAll(c.Request().Context(), user.ID, params)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// GetByID handles GET /api/tasks/:id
// Returns a specific task
func (h *Handler) GetByID(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	taskID := c.Param("id")
	if taskID == "" {
		return apperror.ErrBadRequest.WithMessage("task id is required")
	}

	projectID := c.QueryParam("project_id")
	if projectID == "" {
		projectID = c.Request().Header.Get("X-Project-ID")
	}

	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id is required")
	}

	task, err := h.svc.GetByID(c.Request().Context(), projectID, taskID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, TaskResponse{Data: *task})
}

// Resolve handles POST /api/tasks/:id/resolve
// Resolves a task as accepted or rejected
func (h *Handler) Resolve(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	taskID := c.Param("id")
	if taskID == "" {
		return apperror.ErrBadRequest.WithMessage("task id is required")
	}

	projectID := c.QueryParam("project_id")
	if projectID == "" {
		projectID = c.Request().Header.Get("X-Project-ID")
	}

	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id is required")
	}

	var req ResolveTaskRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid request body")
	}

	if err := h.svc.Resolve(c.Request().Context(), projectID, taskID, user.ID, &req); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "resolved"})
}

// Cancel handles POST /api/tasks/:id/cancel
// Cancels a pending task
func (h *Handler) Cancel(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	taskID := c.Param("id")
	if taskID == "" {
		return apperror.ErrBadRequest.WithMessage("task id is required")
	}

	projectID := c.QueryParam("project_id")
	if projectID == "" {
		projectID = c.Request().Header.Get("X-Project-ID")
	}

	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id is required")
	}

	if err := h.svc.Cancel(c.Request().Context(), projectID, taskID, user.ID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "cancelled"})
}
