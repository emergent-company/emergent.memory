package useraccess

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for user access
type Handler struct {
	svc *Service
}

// NewHandler creates a new user access handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetOrgsAndProjects returns the user's access tree (organizations and projects with roles)
// GET /user/orgs-and-projects
func (h *Handler) GetOrgsAndProjects(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	tree, err := h.svc.GetAccessTree(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, tree)
}
