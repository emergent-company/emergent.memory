package sources

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ListOrgOAuthApps handles GET /api/orgs/:orgId/data-source-integrations/oauth-apps.
// @Summary      List org OAuth apps
// @Description  Returns the org's named, vendor-level OAuth apps (secrets redacted). Org admin only.
// @Tags         data-source-integrations
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Success      200 {array} OAuthAppDTO "Org OAuth apps"
// @Router       /api/orgs/{orgId}/data-source-integrations/oauth-apps [get]
// @Security     bearerAuth
func (h *Handler) ListOrgOAuthApps(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	user := auth.MustGetUser(c)
	items, err := h.svc.ListOrgOAuthApps(c.Request().Context(), orgID, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, items)
}

// CreateOrgOAuthApp handles POST /api/orgs/:orgId/data-source-integrations/oauth-apps.
// @Summary      Create an org OAuth app
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        request body CreateOAuthAppRequest true "OAuth app material"
// @Success      201 {object} OAuthAppDTO "Created org OAuth app"
// @Router       /api/orgs/{orgId}/data-source-integrations/oauth-apps [post]
// @Security     bearerAuth
func (h *Handler) CreateOrgOAuthApp(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	var req CreateOAuthAppRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	dto, err := h.svc.CreateOrgOAuthApp(c.Request().Context(), orgID, user.ID, &req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, dto)
}

// GetOrgOAuthApp handles GET /api/orgs/:orgId/data-source-integrations/oauth-apps/:appId.
// @Summary      Get an org OAuth app
// @Tags         data-source-integrations
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        appId path string true "OAuth app ID (UUID)"
// @Success      200 {object} OAuthAppDTO "Org OAuth app"
// @Router       /api/orgs/{orgId}/data-source-integrations/oauth-apps/{appId} [get]
// @Security     bearerAuth
func (h *Handler) GetOrgOAuthApp(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	user := auth.MustGetUser(c)
	dto, err := h.svc.GetOrgOAuthApp(c.Request().Context(), orgID, user.ID, c.Param("appId"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// UpdateOrgOAuthApp handles PUT /api/orgs/:orgId/data-source-integrations/oauth-apps/:appId.
// @Summary      Update an org OAuth app
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        appId path string true "OAuth app ID (UUID)"
// @Param        request body UpdateOAuthAppRequest true "OAuth app fields"
// @Success      200 {object} OAuthAppDTO "Updated org OAuth app"
// @Router       /api/orgs/{orgId}/data-source-integrations/oauth-apps/{appId} [put]
// @Security     bearerAuth
func (h *Handler) UpdateOrgOAuthApp(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	var req UpdateOAuthAppRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	dto, err := h.svc.UpdateOrgOAuthApp(c.Request().Context(), orgID, user.ID, c.Param("appId"), &req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// DeleteOrgOAuthApp handles DELETE /api/orgs/:orgId/data-source-integrations/oauth-apps/:appId.
// @Summary      Delete an org OAuth app
// @Tags         data-source-integrations
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        appId path string true "OAuth app ID (UUID)"
// @Success      204 "Deleted"
// @Router       /api/orgs/{orgId}/data-source-integrations/oauth-apps/{appId} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteOrgOAuthApp(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	user := auth.MustGetUser(c)
	if err := h.svc.DeleteOrgOAuthApp(c.Request().Context(), orgID, user.ID, c.Param("appId")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// ListProjectOAuthApps handles GET /api/projects/:projectId/data-source-integrations/oauth-apps.
// @Summary      List project OAuth apps
// @Tags         data-source-integrations
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {array} OAuthAppDTO "Project OAuth apps"
// @Router       /api/projects/{projectId}/data-source-integrations/oauth-apps [get]
// @Security     bearerAuth
func (h *Handler) ListProjectOAuthApps(c echo.Context) error {
	projectID, err := h.pathProjectID(c)
	if err != nil {
		return err
	}
	items, err := h.svc.ListProjectOAuthApps(c.Request().Context(), projectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, items)
}

// CreateProjectOAuthApp handles POST /api/projects/:projectId/data-source-integrations/oauth-apps.
// @Summary      Create a project OAuth app
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body CreateOAuthAppRequest true "OAuth app material"
// @Success      201 {object} OAuthAppDTO "Created project OAuth app"
// @Router       /api/projects/{projectId}/data-source-integrations/oauth-apps [post]
// @Security     bearerAuth
func (h *Handler) CreateProjectOAuthApp(c echo.Context) error {
	projectID, err := h.pathProjectID(c)
	if err != nil {
		return err
	}
	var req CreateOAuthAppRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	dto, err := h.svc.CreateProjectOAuthApp(c.Request().Context(), projectID, user.ID, &req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, dto)
}

// GetProjectOAuthApp handles GET /api/projects/:projectId/data-source-integrations/oauth-apps/:appId.
// @Summary      Get a project OAuth app
// @Tags         data-source-integrations
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        appId path string true "OAuth app ID (UUID)"
// @Success      200 {object} OAuthAppDTO "Project OAuth app"
// @Router       /api/projects/{projectId}/data-source-integrations/oauth-apps/{appId} [get]
// @Security     bearerAuth
func (h *Handler) GetProjectOAuthApp(c echo.Context) error {
	projectID, err := h.pathProjectID(c)
	if err != nil {
		return err
	}
	dto, err := h.svc.GetProjectOAuthApp(c.Request().Context(), projectID, c.Param("appId"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// UpdateProjectOAuthApp handles PUT /api/projects/:projectId/data-source-integrations/oauth-apps/:appId.
// @Summary      Update a project OAuth app
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        appId path string true "OAuth app ID (UUID)"
// @Param        request body UpdateOAuthAppRequest true "OAuth app fields"
// @Success      200 {object} OAuthAppDTO "Updated project OAuth app"
// @Router       /api/projects/{projectId}/data-source-integrations/oauth-apps/{appId} [put]
// @Security     bearerAuth
func (h *Handler) UpdateProjectOAuthApp(c echo.Context) error {
	projectID, err := h.pathProjectID(c)
	if err != nil {
		return err
	}
	var req UpdateOAuthAppRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	dto, err := h.svc.UpdateProjectOAuthApp(c.Request().Context(), projectID, user.ID, c.Param("appId"), &req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// DeleteProjectOAuthApp handles DELETE /api/projects/:projectId/data-source-integrations/oauth-apps/:appId.
// @Summary      Delete a project OAuth app
// @Tags         data-source-integrations
// @Param        projectId path string true "Project ID (UUID)"
// @Param        appId path string true "OAuth app ID (UUID)"
// @Success      204 "Deleted"
// @Router       /api/projects/{projectId}/data-source-integrations/oauth-apps/{appId} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteProjectOAuthApp(c echo.Context) error {
	projectID, err := h.pathProjectID(c)
	if err != nil {
		return err
	}
	user := auth.MustGetUser(c)
	if err := h.svc.DeleteProjectOAuthApp(c.Request().Context(), projectID, user.ID, c.Param("appId")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
