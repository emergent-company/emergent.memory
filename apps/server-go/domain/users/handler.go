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
// @Summary      Search users by email
// @Description  Searches for users by email query (partial match). Returns matching users excluding the authenticated user.
// @Tags         users
// @Produce      json
// @Param        email query string true "Email query (partial match supported)"
// @Success      200 {object} UserSearchResponse "Search results"
// @Failure      400 {object} apperror.Error "Missing or invalid email query parameter"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/users/search [get]
// @Security     bearerAuth
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
