package embeddingpolicies

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles embedding policy HTTP requests
type Handler struct {
	svc *Service
}

// NewHandler creates a new embedding policies handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List handles GET /api/v2/graph/embedding-policies
func (h *Handler) List(c echo.Context) error {
	// Get project_id from query param (required for this endpoint)
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id query parameter is required")
	}

	// Optional object_type filter
	var objectType *string
	if ot := c.QueryParam("object_type"); ot != "" {
		objectType = &ot
	}

	policies, err := h.svc.List(c.Request().Context(), projectID, objectType)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ToResponseList(policies))
}

// GetByID handles GET /api/v2/graph/embedding-policies/:id
func (h *Handler) GetByID(c echo.Context) error {
	// Get project_id from query param (required for this endpoint)
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id query parameter is required")
	}

	policyID := c.Param("id")
	if policyID == "" {
		return apperror.ErrBadRequest.WithMessage("policy id required")
	}

	policy, err := h.svc.GetByID(c.Request().Context(), projectID, policyID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ToResponse(policy))
}

// Create handles POST /api/v2/graph/embedding-policies
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req CreateRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid request body")
	}

	// Validate required fields
	if req.ProjectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}
	if req.ObjectType == "" {
		return apperror.ErrBadRequest.WithMessage("objectType is required")
	}

	policy, err := h.svc.Create(c.Request().Context(), req.ProjectID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, ToResponse(policy))
}

// Update handles PATCH /api/v2/graph/embedding-policies/:id
func (h *Handler) Update(c echo.Context) error {
	// Get project_id from query param (required for this endpoint)
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id query parameter is required")
	}

	policyID := c.Param("id")
	if policyID == "" {
		return apperror.ErrBadRequest.WithMessage("policy id required")
	}

	var req UpdateRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid request body")
	}

	policy, err := h.svc.Update(c.Request().Context(), projectID, policyID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ToResponse(policy))
}

// Delete handles DELETE /api/v2/graph/embedding-policies/:id
func (h *Handler) Delete(c echo.Context) error {
	// Get project_id from query param (required for this endpoint)
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("project_id query parameter is required")
	}

	policyID := c.Param("id")
	if policyID == "" {
		return apperror.ErrBadRequest.WithMessage("policy id required")
	}

	err := h.svc.Delete(c.Request().Context(), projectID, policyID)
	if err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
