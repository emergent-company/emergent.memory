package userprofile

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for user profiles
type Handler struct {
	svc *Service
}

// NewHandler creates a new user profile handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Get returns the current user's profile
// @Summary      Get user profile
// @Description  Returns the authenticated user's profile information
// @Tags         user-profile
// @Accept       json
// @Produce      json
// @Success      200 {object} ProfileDTO "User profile"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Profile not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v2/user/profile [get]
// @Security     bearerAuth
func (h *Handler) Get(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	profile, err := h.svc.GetByID(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, profile)
}

// Update updates the current user's profile
// @Summary      Update user profile
// @Description  Updates the authenticated user's profile information (name, phone, etc.)
// @Tags         user-profile
// @Accept       json
// @Produce      json
// @Param        request body UpdateProfileRequest true "Profile updates"
// @Success      200 {object} ProfileDTO "Updated profile"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v2/user/profile [put]
// @Security     bearerAuth
func (h *Handler) Update(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req UpdateProfileRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Validate request - at least one field should be provided
	if req.FirstName == nil && req.LastName == nil && req.DisplayName == nil && req.PhoneE164 == nil {
		return apperror.ErrBadRequest.WithMessage("at least one field must be provided")
	}

	// Validate field lengths if provided
	if req.FirstName != nil && len(*req.FirstName) > 100 {
		return apperror.ErrBadRequest.WithMessage("firstName must be at most 100 characters")
	}
	if req.LastName != nil && len(*req.LastName) > 100 {
		return apperror.ErrBadRequest.WithMessage("lastName must be at most 100 characters")
	}
	if req.DisplayName != nil && len(*req.DisplayName) > 200 {
		return apperror.ErrBadRequest.WithMessage("displayName must be at most 200 characters")
	}
	if req.PhoneE164 != nil && len(*req.PhoneE164) > 20 {
		return apperror.ErrBadRequest.WithMessage("phoneE164 must be at most 20 characters")
	}

	profile, err := h.svc.Update(c.Request().Context(), user.ID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, profile)
}
