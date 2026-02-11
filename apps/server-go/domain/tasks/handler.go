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
// @Summary      Get task counts by project
// @Description  Returns task counts grouped by status (pending, accepted, rejected, cancelled) for a specific project
// @Tags         tasks
// @Produce      json
// @Param        project_id query string false "Project ID (alternative to X-Project-ID header)"
// @Param        X-Project-ID header string false "Project ID (alternative to project_id query param)"
// @Success      200 {object} TaskCountsResponse "Task counts by status"
// @Failure      400 {object} apperror.Error "Missing project_id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/tasks/counts [get]
// @Security     bearerAuth
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
// @Summary      Get task counts across all projects
// @Description  Returns aggregated task counts by status (pending, accepted, rejected, cancelled) across all projects accessible to the current user
// @Tags         tasks
// @Produce      json
// @Success      200 {object} TaskCountsResponse "Aggregated task counts by status"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/tasks/all/counts [get]
// @Security     bearerAuth
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
// @Summary      List tasks by project
// @Description  Returns paginated list of tasks for a specific project with optional filtering by status and type
// @Tags         tasks
// @Produce      json
// @Param        project_id query string false "Project ID (alternative to X-Project-ID header)"
// @Param        X-Project-ID header string false "Project ID (alternative to project_id query param)"
// @Param        status query string false "Filter by status (pending, accepted, rejected, cancelled)"
// @Param        type query string false "Filter by task type"
// @Param        limit query int false "Max results to return" minimum(1) maximum(100)
// @Param        offset query int false "Number of results to skip" minimum(0)
// @Success      200 {object} TaskListResponse "Paginated task list"
// @Failure      400 {object} apperror.Error "Missing project_id"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/tasks [get]
// @Security     bearerAuth
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
// @Summary      List tasks across all projects
// @Description  Returns paginated list of tasks across all projects accessible to the current user with optional filtering by status and type
// @Tags         tasks
// @Produce      json
// @Param        status query string false "Filter by status (pending, accepted, rejected, cancelled)"
// @Param        type query string false "Filter by task type"
// @Param        limit query int false "Max results to return" minimum(1) maximum(100)
// @Param        offset query int false "Number of results to skip" minimum(0)
// @Success      200 {object} TaskListResponse "Paginated task list"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/tasks/all [get]
// @Security     bearerAuth
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
// @Summary      Get task by ID
// @Description  Returns a specific task by its ID for a given project
// @Tags         tasks
// @Produce      json
// @Param        id path string true "Task ID (UUID)"
// @Param        project_id query string false "Project ID (alternative to X-Project-ID header)"
// @Param        X-Project-ID header string false "Project ID (alternative to project_id query param)"
// @Success      200 {object} TaskResponse "Task details"
// @Failure      400 {object} apperror.Error "Missing task ID or project_id"
// @Failure      404 {object} apperror.Error "Task not found"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/tasks/{id} [get]
// @Security     bearerAuth
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
// @Summary      Resolve task
// @Description  Mark a pending task as accepted or rejected with optional resolution notes
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        id path string true "Task ID (UUID)"
// @Param        request body ResolveTaskRequest true "Resolution data (resolution: accepted|rejected, optional notes)"
// @Param        project_id query string false "Project ID (alternative to X-Project-ID header)"
// @Param        X-Project-ID header string false "Project ID (alternative to project_id query param)"
// @Success      200 {object} map[string]string "Resolution confirmation"
// @Failure      400 {object} apperror.Error "Invalid request or missing project_id"
// @Failure      404 {object} apperror.Error "Task not found"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/tasks/{id}/resolve [post]
// @Security     bearerAuth
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
// @Summary      Cancel task
// @Description  Cancel a pending task (marks as cancelled, cannot be undone)
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        id path string true "Task ID (UUID)"
// @Param        project_id query string false "Project ID (alternative to X-Project-ID header)"
// @Param        X-Project-ID header string false "Project ID (alternative to project_id query param)"
// @Success      200 {object} map[string]string "Cancellation confirmation"
// @Failure      400 {object} apperror.Error "Missing task ID or project_id"
// @Failure      404 {object} apperror.Error "Task not found"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/tasks/{id}/cancel [post]
// @Security     bearerAuth
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
