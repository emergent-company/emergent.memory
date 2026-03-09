package orgs

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// handleListOrgToolSettings returns all tool settings for an org.
// @Summary      List org tool settings
// @Description  Returns all org-level tool setting overrides for a given organization
// @Tags         organizations
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Success      200 {array} OrgToolSettingDTO "List of org tool settings"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/orgs/{orgId}/tool-settings [get]
// @Security     bearerAuth
func (h *Handler) handleListOrgToolSettings(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	orgID := c.Param("orgId")

	settings, err := h.svc.GetOrgToolSettings(c.Request().Context(), orgID, user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, settings)
}

// handleUpsertOrgToolSetting creates or updates a tool setting for an org.
// @Summary      Upsert org tool setting
// @Description  Creates or updates the org-level override for a built-in tool
// @Tags         organizations
// @Accept       json
// @Produce      json
// @Param        orgId    path string                   true "Organization ID (UUID)"
// @Param        toolName path string                   true "Built-in tool name"
// @Param        request  body UpsertOrgToolSettingRequest true "Tool setting"
// @Success      200 {object} OrgToolSettingDTO "Updated tool setting"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/orgs/{orgId}/tool-settings/{toolName} [put]
// @Security     bearerAuth
func (h *Handler) handleUpsertOrgToolSetting(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	orgID := c.Param("orgId")
	toolName := c.Param("toolName")

	var req UpsertOrgToolSettingRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	setting, err := h.svc.UpsertOrgToolSetting(c.Request().Context(), orgID, toolName, user.ID, req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, setting)
}

// handleDeleteOrgToolSetting removes an org tool setting override.
// @Summary      Delete org tool setting
// @Description  Removes the org-level override for a built-in tool, reverting to global defaults
// @Tags         organizations
// @Produce      json
// @Param        orgId    path string true "Organization ID (UUID)"
// @Param        toolName path string true "Built-in tool name"
// @Success      200 {object} map[string]string "Deletion status"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      404 {object} apperror.Error "Setting not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/orgs/{orgId}/tool-settings/{toolName} [delete]
// @Security     bearerAuth
func (h *Handler) handleDeleteOrgToolSetting(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	orgID := c.Param("orgId")
	toolName := c.Param("toolName")

	if err := h.svc.DeleteOrgToolSetting(c.Request().Context(), orgID, toolName, user.ID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}
