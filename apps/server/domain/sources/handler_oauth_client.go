package sources

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// pathOrgID parses the :orgId path parameter as a UUID.
func (h *Handler) pathOrgID(c echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		return uuid.UUID{}, apperror.ErrBadRequest
	}
	return id, nil
}

// ListOrgOAuthClients handles GET /api/orgs/:orgId/data-source-integrations/oauth-clients.
// @Summary      List org OAuth client configs
// @Description  Returns the org's configured OAuth clients (secrets redacted). Org admin only.
// @Tags         data-source-integrations
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Success      200 {array} OAuthClientConfigDTO "Org OAuth client configs"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/orgs/{orgId}/data-source-integrations/oauth-clients [get]
// @Security     bearerAuth
func (h *Handler) ListOrgOAuthClients(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	user := auth.MustGetUser(c)
	items, err := h.svc.ListOrgOAuthClients(c.Request().Context(), orgID, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, items)
}

// GetOrgOAuthClient handles GET /api/orgs/:orgId/data-source-integrations/oauth-clients/:providerType.
// @Summary      Get an org OAuth client config
// @Description  Returns the org's client config for a provider (secret redacted); configured=false when unset
// @Tags         data-source-integrations
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        providerType path string true "Provider type (e.g. google_drive)"
// @Success      200 {object} OAuthClientConfigDTO "Org OAuth client config"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/orgs/{orgId}/data-source-integrations/oauth-clients/{providerType} [get]
// @Security     bearerAuth
func (h *Handler) GetOrgOAuthClient(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	user := auth.MustGetUser(c)
	dto, err := h.svc.GetOrgOAuthClient(c.Request().Context(), orgID, user.ID, c.Param("providerType"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// UpsertOrgOAuthClient handles PUT /api/orgs/:orgId/data-source-integrations/oauth-clients/:providerType.
// @Summary      Set an org OAuth client config
// @Description  Creates or replaces the org's OAuth client for a provider; the secret is encrypted and write-only
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        providerType path string true "Provider type (e.g. google_drive)"
// @Param        request body SetOAuthClientRequest true "OAuth client material"
// @Success      200 {object} OAuthClientConfigDTO "Saved org OAuth client config"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/orgs/{orgId}/data-source-integrations/oauth-clients/{providerType} [put]
// @Security     bearerAuth
func (h *Handler) UpsertOrgOAuthClient(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	var req SetOAuthClientRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest
	}
	user := auth.MustGetUser(c)
	dto, err := h.svc.UpsertOrgOAuthClient(c.Request().Context(), orgID, user.ID, c.Param("providerType"), &req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// DeleteOrgOAuthClient handles DELETE /api/orgs/:orgId/data-source-integrations/oauth-clients/:providerType.
// @Summary      Delete an org OAuth client config
// @Description  Removes the org's OAuth client for a provider, reverting to the deployment default
// @Tags         data-source-integrations
// @Param        orgId path string true "Organization ID (UUID)"
// @Param        providerType path string true "Provider type (e.g. google_drive)"
// @Success      204 "Deleted"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      403 {object} apperror.Error "Forbidden"
// @Failure      404 {object} apperror.Error "Not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/orgs/{orgId}/data-source-integrations/oauth-clients/{providerType} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteOrgOAuthClient(c echo.Context) error {
	orgID, err := h.pathOrgID(c)
	if err != nil {
		return err
	}
	user := auth.MustGetUser(c)
	if err := h.svc.DeleteOrgOAuthClient(c.Request().Context(), orgID, user.ID, c.Param("providerType")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
