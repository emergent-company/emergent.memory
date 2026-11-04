package useractivity

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// OrgActivity returns the org's members ranked by activity over a window.
// @Summary      Rank organization members by activity
// @Description  Returns per-member last-seen, action counts, tier and score for the requested window. Requires org_admin.
// @Tags         user-activity
// @Produce      json
// @Param        id path string true "Organization ID (UUID)"
// @Param        range query string false "Window: 7d, 30d (default) or 90d"
// @Success      200 {object} ActivityResponse
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Router       /api/orgs/{id}/activity [get]
// @Security     bearerAuth
func (h *Handler) OrgActivity(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ID == "" {
		return apperror.ErrUnauthorized
	}
	res, err := h.svc.OrgActivity(
		c.Request().Context(),
		c.Param("id"),
		user.ID,
		ParseActivityRange(c.QueryParam("range")),
	)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

// ProjectActivity returns the project's members ranked by activity over a window.
// @Summary      Rank project members by activity
// @Description  Returns per-member last-seen, action counts, tier and score for the requested window. Requires project_admin or org_admin.
// @Tags         user-activity
// @Produce      json
// @Param        id path string true "Project ID (UUID)"
// @Param        range query string false "Window: 7d, 30d (default) or 90d"
// @Success      200 {object} ActivityResponse
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Router       /api/projects/{id}/activity [get]
// @Security     bearerAuth
func (h *Handler) ProjectActivity(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ID == "" {
		return apperror.ErrUnauthorized
	}
	res, err := h.svc.ProjectActivity(
		c.Request().Context(),
		c.Param("id"),
		user.ID,
		ParseActivityRange(c.QueryParam("range")),
	)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

// MyActivity returns the caller's own activity summary.
// @Summary      Get my activity summary
// @Description  Returns the authenticated user's own last-seen, action counts, tier and score. Never exposes other members.
// @Tags         user-activity
// @Produce      json
// @Param        range query string false "Window: 7d, 30d (default) or 90d"
// @Success      200 {object} MyActivityResponse
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/me/activity [get]
// @Security     bearerAuth
func (h *Handler) MyActivity(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ID == "" {
		return apperror.ErrUnauthorized
	}
	res, err := h.svc.MyActivity(
		c.Request().Context(),
		user.ID,
		ParseActivityRange(c.QueryParam("range")),
	)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}
