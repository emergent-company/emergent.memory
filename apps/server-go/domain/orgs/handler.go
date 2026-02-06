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
// GET /api/v2/orgs
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
// GET /api/v2/orgs/:id
func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")

	org, err := h.svc.GetByID(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, org)
}

// Create creates a new organization
// POST /api/v2/orgs
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
// DELETE /api/v2/orgs/:id
func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")

	if err := h.svc.Delete(c.Request().Context(), id); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}
