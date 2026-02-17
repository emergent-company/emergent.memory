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
// @Summary      List user's projects
// @Description  Returns all projects the authenticated user is a member of. Supports filtering by organization and pagination.
// @Tags         projects
// @Produce      json
// @Param        limit query int false "Max results (1-500, default 100)" minimum(1) maximum(500)
// @Param        orgId query string false "Filter by organization ID (UUID)"
// @Success      200 {array} ProjectDTO "List of projects"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects [get]
// @Security     bearerAuth
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

	// If authenticated via a project-scoped API token, restrict to that project only
	var projectID string
	if user.APITokenProjectID != "" {
		projectID = user.APITokenProjectID
	}

	projects, err := h.svc.List(c.Request().Context(), ServiceListParams{
		UserID:    user.ID,
		OrgID:     orgID,
		ProjectID: projectID,
		Limit:     limit,
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, projects)
}

// Get returns a single project by ID
// @Summary      Get project by ID
// @Description  Returns a single project by its unique identifier
// @Tags         projects
// @Produce      json
// @Param        id path string true "Project ID (UUID)"
// @Success      200 {object} Project "Project details"
// @Failure      400 {object} apperror.Error "Invalid project ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Project not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{id} [get]
// @Security     bearerAuth
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
// @Summary      Create a new project
// @Description  Creates a new project with the authenticated user as the initial member
// @Tags         projects
// @Accept       json
// @Produce      json
// @Param        request body CreateProjectRequest true "Project creation request"
// @Success      201 {object} Project "Project created"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects [post]
// @Security     bearerAuth
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
// @Summary      Update project
// @Description  Updates an existing project's properties (name, KB purpose, chat template, auto-extraction settings)
// @Tags         projects
// @Accept       json
// @Produce      json
// @Param        id path string true "Project ID (UUID)"
// @Param        request body UpdateProjectRequest true "Project update request"
// @Success      200 {object} Project "Updated project"
// @Failure      400 {object} apperror.Error "Invalid request body or project ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Project not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{id} [patch]
// @Security     bearerAuth
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
// @Summary      Delete project
// @Description  Permanently deletes a project and all associated data
// @Tags         projects
// @Produce      json
// @Param        id path string true "Project ID (UUID)"
// @Success      200 {object} map[string]string "Deletion status"
// @Failure      400 {object} apperror.Error "Invalid project ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Project not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{id} [delete]
// @Security     bearerAuth
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
// @Summary      List project members
// @Description  Returns all users who are members of the specified project with their roles
// @Tags         projects
// @Produce      json
// @Param        id path string true "Project ID (UUID)"
// @Success      200 {array} ProjectMemberDTO "List of project members"
// @Failure      400 {object} apperror.Error "Invalid project ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Project not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{id}/members [get]
// @Security     bearerAuth
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
// @Summary      Remove project member
// @Description  Removes a user from the specified project
// @Tags         projects
// @Produce      json
// @Param        id path string true "Project ID (UUID)"
// @Param        userId path string true "User ID to remove (UUID)"
// @Success      200 {object} map[string]string "Removal status"
// @Failure      400 {object} apperror.Error "Invalid project ID or user ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Project or user not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{id}/members/{userId} [delete]
// @Security     bearerAuth
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
