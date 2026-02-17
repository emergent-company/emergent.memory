package useraccess

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
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
// @Summary      Get user access tree
// @Description  Returns all organizations and projects the user has access to with their respective roles
// @Tags         user-access
// @Accept       json
// @Produce      json
// @Success      200 {array} OrgWithProjects "Access tree"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /user/orgs-and-projects [get]
// @Security     bearerAuth
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
