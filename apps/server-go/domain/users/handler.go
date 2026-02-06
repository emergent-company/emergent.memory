package users

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for users
type Handler struct {
	svc *Service
}

// NewHandler creates a new users handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Search searches for users by email
// GET /api/v2/users/search?email=<query>
func (h *Handler) Search(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	emailQuery := c.QueryParam("email")
	if emailQuery == "" {
		return apperror.ErrBadRequest.WithMessage("email query parameter is required")
	}

	// Exclude the current user from search results
	excludeUserID := &user.ID

	result, err := h.svc.SearchByEmail(c.Request().Context(), emailQuery, excludeUserID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, result)
}
