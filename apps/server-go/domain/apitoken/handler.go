package apitoken

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for API tokens
type Handler struct {
	svc *Service
}

// NewHandler creates a new API token handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Create creates a new API token
// POST /api/v2/projects/:projectId/tokens
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req CreateApiTokenRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Validate request
	if req.Name == "" {
		return apperror.ErrBadRequest.WithMessage("name is required")
	}
	if len(req.Name) > 255 {
		return apperror.ErrBadRequest.WithMessage("name must be at most 255 characters")
	}
	if len(req.Scopes) == 0 {
		return apperror.ErrBadRequest.WithMessage("at least one scope is required")
	}

	result, err := h.svc.Create(c.Request().Context(), projectID, user.ID, req.Name, req.Scopes)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, result)
}

// List returns all API tokens for a project
// GET /api/v2/projects/:projectId/tokens
func (h *Handler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	result, err := h.svc.ListByProject(c.Request().Context(), projectID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}

// Get returns a single API token by ID
// GET /api/v2/projects/:projectId/tokens/:tokenId
func (h *Handler) Get(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	tokenID := c.Param("tokenId")

	if projectID == "" || tokenID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId and tokenId are required")
	}

	result, err := h.svc.GetByID(c.Request().Context(), tokenID, projectID)
	if err != nil {
		return err
	}
	if result == nil {
		return apperror.ErrNotFound.WithMessage("Token not found")
	}

	return c.JSON(http.StatusOK, result)
}

// Revoke revokes an API token
// DELETE /api/v2/projects/:projectId/tokens/:tokenId
func (h *Handler) Revoke(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	tokenID := c.Param("tokenId")

	if projectID == "" || tokenID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId and tokenId are required")
	}

	if err := h.svc.Revoke(c.Request().Context(), tokenID, projectID, user.ID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}
