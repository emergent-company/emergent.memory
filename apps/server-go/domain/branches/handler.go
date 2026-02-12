package branches

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles branch HTTP requests
type Handler struct {
	svc *Service
}

// NewHandler creates a new branches handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List handles GET /api/graph/branches
// @Summary      List branches
// @Description  Returns all branches, optionally filtered by project
// @Tags         branches
// @Accept       json
// @Produce      json
// @Param        project_id query string false "Filter by project ID (UUID)"
// @Success      200 {array} BranchResponse "List of branches"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/branches [get]
// @Security     bearerAuth
func (h *Handler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// project_id is optional for list
	var projectID *string
	if pid := c.QueryParam("project_id"); pid != "" {
		// Validate UUID format
		if _, err := uuid.Parse(pid); err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid project_id format")
		}
		projectID = &pid
	}

	branches, err := h.svc.List(c.Request().Context(), projectID)
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
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("branch id required")
	}

	// Validate UUID format
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid branch id format")
	}

	branch, err := h.svc.GetByID(c.Request().Context(), id)
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
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req CreateBranchRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Validate project_id if provided
	if req.ProjectID != nil && *req.ProjectID != "" {
		if _, err := uuid.Parse(*req.ProjectID); err != nil {
			return apperror.ErrBadRequest.WithMessage("invalid project_id format")
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
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("branch id required")
	}

	// Validate UUID format
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid branch id format")
	}

	var req UpdateBranchRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	branch, err := h.svc.Update(c.Request().Context(), id, &req)
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
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("branch id required")
	}

	// Validate UUID format
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid branch id format")
	}

	err := h.svc.Delete(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
