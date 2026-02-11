package orgs

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for organizations
type Handler struct {
	svc *Service
}

// NewHandler creates a new organization handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List returns all organizations the authenticated user is a member of
// @Summary      List user's organizations
// @Description  Returns all organizations the authenticated user is a member of
// @Tags         organizations
// @Produce      json
// @Success      200 {array} OrgDTO "List of organizations"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/orgs [get]
// @Security     bearerAuth
func (h *Handler) List(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	orgs, err := h.svc.List(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, orgs)
}

// Get returns a single organization by ID
// @Summary      Get organization by ID
// @Description  Returns a single organization by its unique identifier
// @Tags         organizations
// @Produce      json
// @Param        id path string true "Organization ID (UUID)"
// @Success      200 {object} Org "Organization details"
// @Failure      400 {object} apperror.Error "Invalid organization ID"
// @Failure      404 {object} apperror.Error "Organization not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/orgs/{id} [get]
// @Security     bearerAuth
func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")

	org, err := h.svc.GetByID(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, org)
}

// Create creates a new organization
// @Summary      Create a new organization
// @Description  Creates a new organization with the authenticated user as the initial member
// @Tags         organizations
// @Accept       json
// @Produce      json
// @Param        request body CreateOrgRequest true "Organization creation request"
// @Success      201 {object} Org "Organization created"
// @Failure      400 {object} apperror.Error "Invalid request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/orgs [post]
// @Security     bearerAuth
func (h *Handler) Create(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req CreateOrgRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	org, err := h.svc.Create(c.Request().Context(), req.Name, user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, org)
}

// Delete deletes an organization by ID
// @Summary      Delete organization
// @Description  Permanently deletes an organization by ID
// @Tags         organizations
// @Produce      json
// @Param        id path string true "Organization ID (UUID)"
// @Success      200 {object} map[string]string "Deletion status"
// @Failure      400 {object} apperror.Error "Invalid organization ID"
// @Failure      404 {object} apperror.Error "Organization not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/orgs/{id} [delete]
// @Security     bearerAuth
func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")

	if err := h.svc.Delete(c.Request().Context(), id); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}
