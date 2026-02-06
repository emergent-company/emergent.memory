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

// List handles GET /api/v2/graph/branches
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

// GetByID handles GET /api/v2/graph/branches/:id
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

// Create handles POST /api/v2/graph/branches
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

// Update handles PATCH /api/v2/graph/branches/:id
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

// Delete handles DELETE /api/v2/graph/branches/:id
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
