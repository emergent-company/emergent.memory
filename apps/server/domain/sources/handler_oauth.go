package sources

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// startOAuthBody is the optional JSON body for starting an OAuth connect.
type startOAuthBody struct {
	Config map[string]any `json:"config"`
	Scopes []string       `json:"scopes,omitempty"`
}

// StartOAuth handles GET /api/data-source-integrations/providers/:providerType/oauth/authorize.
// @Summary      Start an OAuth connect
// @Description  Builds a provider authorization URL (state + PKCE) and persists the pending state
// @Tags         data-source-integrations
// @Accept       json
// @Produce      json
// @Param        providerType path string true "Provider type (e.g. google_drive)"
// @Param        request body startOAuthBody false "Intended source config"
// @Success      200 {object} StartOAuthResponse "Authorization URL"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Unknown provider"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /data-source-integrations/providers/{providerType}/oauth/authorize [get]
// @Security     bearerAuth
func (h *Handler) StartOAuth(c echo.Context) error {
	projectID, err := h.projectID(c)
	if err != nil {
		return err
	}
	providerType := strings.TrimSpace(c.Param("providerType"))
	if providerType == "" {
		return apperror.NewValidation("providerType is required")
	}
	var body startOAuthBody
	if c.Request().ContentLength > 0 {
		_ = c.Bind(&body)
	}
	resp, err := h.svc.StartOAuth(c.Request().Context(), projectID, providerType, StartOAuthRequest{
		Config: body.Config,
		Scopes: body.Scopes,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// CompleteOAuth handles GET /api/data-source-integrations/oauth/:providerType/callback.
// It is intentionally unauthenticated (the browser arrives cross-site from the
// provider); the single-use, short-lived, unguessable state is the trust anchor.
// @Summary      OAuth connect callback
// @Description  Consumes the pending state, exchanges the code, creates the source, and redirects to the UI
// @Tags         data-source-integrations
// @Param        providerType path string true "Provider type (e.g. google_drive)"
// @Param        code query string true "Authorization code"
// @Param        state query string true "Opaque state"
// @Success      303 "Redirect to the UI"
// @Failure      400 {object} apperror.Error "Bad request"
// @Router       /data-source-integrations/oauth/{providerType}/callback [get]
func (h *Handler) CompleteOAuth(c echo.Context) error {
	providerType := c.Param("providerType")
	if provErr := c.QueryParam("error"); provErr != "" {
		return c.Redirect(http.StatusSeeOther, h.svc.OAuthLandingURL("?oauth_error="+url.QueryEscape(provErr)))
	}
	integration, err := h.svc.CompleteOAuth(c.Request().Context(), providerType, c.QueryParam("state"), c.QueryParam("code"))
	if err != nil {
		// Browser-facing: log the detail, land on a generic error.
		h.svc.log.Warn("oauth callback failed",
			slog.String("provider", providerType), slog.String("error", err.Error()))
		return c.Redirect(http.StatusSeeOther, h.svc.OAuthLandingURL("?oauth_error=connect_failed"))
	}
	return c.Redirect(http.StatusSeeOther, h.svc.OAuthLandingURL("?connected="+url.QueryEscape(integration.ID)))
}
