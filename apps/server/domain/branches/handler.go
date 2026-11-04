package branches

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler handles branch HTTP requests
type Handler struct {
	svc *Service
}

// NewHandler creates a new branches handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// resolveProjectID resolves the project ID for branch-scoped operations from
// (in priority order) the ?project_id query param, then X-Project-ID header /
// API token binding. Returns a bad-request error when none is resolvable.
func resolveProjectID(c echo.Context) (string, error) {
	if pid := c.QueryParam("project_id"); pid != "" {
		if _, err := uuid.Parse(pid); err != nil {
			return "", apperror.ErrBadRequest.WithMessage("invalid project_id format")
		}
		return pid, nil
	}
	if contextPID, err := auth.GetProjectID(c); err == nil && contextPID != "" {
		return contextPID, nil
	}
	return "", apperror.ErrBadRequest.WithMessage("project_id required")
}

// List handles GET /api/graph/branches
// @Summary      List branches
// @Description  Returns branches for the current project. Project is resolved from (in priority order): ?project_id query param, X-Project-ID header, API token project binding.
// @Tags         branches
// @Accept       json
// @Produce      json
// @Param        project_id query string false "Filter by project ID (UUID); overrides X-Project-ID header"
// @Param        X-Project-ID header string false "Project ID (used when project_id query param is absent)"
// @Success      200 {array} BranchResponse "List of branches"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/branches [get]
// @Security     bearerAuth
func (h *Handler) List(c echo.Context) error {

	projectID, err := resolveProjectID(c)
	if err != nil {
		return err
	}
	if err := h.svc.RequireProjectMember(c.Request().Context(), projectID); err != nil {
		return err
	}

	branches, err := h.svc.List(c.Request().Context(), &projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, branches)
}

// GetByID handles GET /api/graph/branches/:id
// @Summary      Get branch
// @Description  Retrieves a specific branch by ID
// @Tags         branches
// @Accept       json
// @Produce      json
// @Param        id path string true "Branch ID (UUID)"
// @Success      200 {object} BranchResponse "Branch details"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Branch not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/branches/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetByID(c echo.Context) error {

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("branch id required")
	}

	// Validate UUID format
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid branch id format")
	}

	projectID, err := resolveProjectID(c)
	if err != nil {
		return err
	}
	if err := h.svc.RequireProjectMember(c.Request().Context(), projectID); err != nil {
		return err
	}

	branch, err := h.svc.GetByID(c.Request().Context(), id, projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, branch)
}

// Create handles POST /api/graph/branches
// @Summary      Create branch
// @Description  Creates a new branch for graph versioning and isolation
// @Tags         branches
// @Accept       json
// @Produce      json
// @Param        request body CreateBranchRequest true "Branch creation request"
// @Success      201 {object} BranchResponse "Created branch"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/branches [post]
// @Security     bearerAuth
func (h *Handler) Create(c echo.Context) error {

	var req CreateBranchRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Resolve project ID: explicit body field takes precedence, then query
	// param, then X-Project-ID header / API token binding. Branches must
	// always be project-scoped.
	if req.ProjectID == nil || *req.ProjectID == "" {
		var pid string
		if p := c.QueryParam("project_id"); p != "" {
			pid = p
		} else if contextPID, err := auth.GetProjectID(c); err == nil && contextPID != "" {
			pid = contextPID
		}
		if pid != "" {
			req.ProjectID = &pid
		}
	}

	// Validate project_id if provided
	if req.ProjectID != nil && *req.ProjectID != "" {
		if _, err := uuid.Parse(*req.ProjectID); err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid project_id format")
		}
		if err := h.svc.RequireProjectMember(c.Request().Context(), *req.ProjectID); err != nil {
			return err
		}
	}

	// Validate parent_branch_id if provided
	if req.ParentBranchID != nil && *req.ParentBranchID != "" {
		if _, err := uuid.Parse(*req.ParentBranchID); err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid parent_branch_id format")
		}
	}

	branch, err := h.svc.Create(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, branch)
}

// Update handles PATCH /api/graph/branches/:id
// @Summary      Update branch
// @Description  Updates a branch's name or metadata
// @Tags         branches
// @Accept       json
// @Produce      json
// @Param        id path string true "Branch ID (UUID)"
// @Param        request body UpdateBranchRequest true "Branch update request"
// @Success      200 {object} BranchResponse "Updated branch"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Branch not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/branches/{id} [patch]
// @Security     bearerAuth
func (h *Handler) Update(c echo.Context) error {

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("branch id required")
	}

	// Validate UUID format
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid branch id format")
	}

	projectID, err := resolveProjectID(c)
	if err != nil {
		return err
	}
	if err := h.svc.RequireProjectMember(c.Request().Context(), projectID); err != nil {
		return err
	}

	var req UpdateBranchRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	branch, err := h.svc.Update(c.Request().Context(), id, projectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, branch)
}

// Delete handles DELETE /api/graph/branches/:id
// @Summary      Delete branch
// @Description  Deletes a branch by ID
// @Tags         branches
// @Accept       json
// @Produce      json
// @Param        id path string true "Branch ID (UUID)"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Branch not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/branches/{id} [delete]
// @Security     bearerAuth
func (h *Handler) Delete(c echo.Context) error {

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("branch id required")
	}

	// Validate UUID format
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid branch id format")
	}

	projectID, err := resolveProjectID(c)
	if err != nil {
		return err
	}
	if err := h.svc.RequireProjectMember(c.Request().Context(), projectID); err != nil {
		return err
	}

	err = h.svc.Delete(c.Request().Context(), id, projectID)
	if err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
