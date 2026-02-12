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

// List handles GET /api/graph/embedding-policies
// @Summary      List embedding policies
// @Description  Returns embedding policies for a project with optional filtering by object type
// @Tags         embedding-policies
// @Accept       json
// @Produce      json
// @Param        project_id query string true "Project ID (UUID)"
// @Param        object_type query string false "Filter by object type"
// @Success      200 {array} Response "List of policies"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/embedding-policies [get]
// @Security     bearerAuth
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

// GetByID handles GET /api/graph/embedding-policies/:id
// @Summary      Get embedding policy
// @Description  Retrieves a specific embedding policy by ID
// @Tags         embedding-policies
// @Accept       json
// @Produce      json
// @Param        project_id query string true "Project ID (UUID)"
// @Param        id path string true "Policy ID (UUID)"
// @Success      200 {object} Response "Policy details"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      404 {object} apperror.Error "Policy not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/embedding-policies/{id} [get]
// @Security     bearerAuth
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

// Create handles POST /api/graph/embedding-policies
// @Summary      Create embedding policy
// @Description  Creates a new embedding policy for controlling vector embedding generation
// @Tags         embedding-policies
// @Accept       json
// @Produce      json
// @Param        request body CreateRequest true "Policy creation request"
// @Success      201 {object} Response "Created policy"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/embedding-policies [post]
// @Security     bearerAuth
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

// Update handles PATCH /api/graph/embedding-policies/:id
// @Summary      Update embedding policy
// @Description  Updates an existing embedding policy's configuration
// @Tags         embedding-policies
// @Accept       json
// @Produce      json
// @Param        project_id query string true "Project ID (UUID)"
// @Param        id path string true "Policy ID (UUID)"
// @Param        request body UpdateRequest true "Policy update request"
// @Success      200 {object} Response "Updated policy"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      404 {object} apperror.Error "Policy not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/embedding-policies/{id} [patch]
// @Security     bearerAuth
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

// Delete handles DELETE /api/graph/embedding-policies/:id
// @Summary      Delete embedding policy
// @Description  Deletes an embedding policy by ID
// @Tags         embedding-policies
// @Accept       json
// @Produce      json
// @Param        project_id query string true "Project ID (UUID)"
// @Param        id path string true "Policy ID (UUID)"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      404 {object} apperror.Error "Policy not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/graph/embedding-policies/{id} [delete]
// @Security     bearerAuth
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
