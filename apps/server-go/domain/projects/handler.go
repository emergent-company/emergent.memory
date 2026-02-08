package projects

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for projects
type Handler struct {
	svc *Service
}

// NewHandler creates a new project handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List returns all projects the authenticated user is a member of
// GET /api/projects
// Query params: limit (1-500, default 100), orgId (optional UUID filter)
func (h *Handler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// Parse query parameters
	limit := DefaultLimit
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil {
			limit = parsed
		}
	}

	orgID := c.QueryParam("orgId")

	projects, err := h.svc.List(c.Request().Context(), ServiceListParams{
		UserID: user.ID,
		OrgID:  orgID,
		Limit:  limit,
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, projects)
}

// Get returns a single project by ID
// GET /api/projects/:id
func (h *Handler) Get(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")

	project, err := h.svc.GetByID(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, project)
}

// Create creates a new project
// POST /api/projects
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req CreateProjectRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	project, err := h.svc.Create(c.Request().Context(), req, user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, project)
}

// Update updates a project
// PATCH /api/projects/:id
func (h *Handler) Update(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")

	var req UpdateProjectRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	project, err := h.svc.Update(c.Request().Context(), id, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, project)
}

// Delete deletes a project by ID
// DELETE /api/projects/:id
func (h *Handler) Delete(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")

	if err := h.svc.Delete(c.Request().Context(), id, user.ID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// ListMembers returns all members of a project
// GET /api/projects/:id/members
func (h *Handler) ListMembers(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("id")

	members, err := h.svc.ListMembers(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, members)
}

// RemoveMember removes a member from a project
// DELETE /api/projects/:id/members/:userId
func (h *Handler) RemoveMember(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("id")
	userID := c.Param("userId")

	if err := h.svc.RemoveMember(c.Request().Context(), projectID, userID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "removed"})
}
