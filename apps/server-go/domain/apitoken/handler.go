package apitoken

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
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
// @Summary      Create API token
// @Description  Creates a new API token for a project. Returns the full token value only once at creation.
// @Tags         api-tokens
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body CreateApiTokenRequest true "Token creation request"
// @Success      201 {object} CreateApiTokenResponseDTO "Token created (includes full token value)"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/tokens [post]
// @Security     bearerAuth
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
// @Summary      List API tokens
// @Description  Returns all API tokens for a project (active and revoked). Token values are not returned.
// @Tags         api-tokens
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {object} ApiTokenListResponseDTO "List of tokens"
// @Failure      400 {object} apperror.Error "Invalid project ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/tokens [get]
// @Security     bearerAuth
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
// @Summary      Get API token by ID
// @Description  Returns a single API token by ID, including the full token value if encryption is configured.
// @Tags         api-tokens
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        tokenId path string true "Token ID (UUID)"
// @Success      200 {object} GetApiTokenResponseDTO "Token details (includes full token value if available)"
// @Failure      400 {object} apperror.Error "Invalid project ID or token ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Token not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/tokens/{tokenId} [get]
// @Security     bearerAuth
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
// @Summary      Revoke API token
// @Description  Revokes an API token, making it permanently unusable
// @Tags         api-tokens
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        tokenId path string true "Token ID (UUID)"
// @Success      200 {object} map[string]string "Revocation status"
// @Failure      400 {object} apperror.Error "Invalid project ID or token ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Token not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/tokens/{tokenId} [delete]
// @Security     bearerAuth
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
