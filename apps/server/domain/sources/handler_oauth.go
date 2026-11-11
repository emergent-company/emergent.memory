package sources

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// startOAuthBody is the optional JSON body for starting an OAuth connect.
type startOAuthBody struct {
	Config map[string]any `json:"config"`
	Scopes []string       `json:"scopes,omitempty"`
	// ReconnectSourceID re-runs consent for an existing source; the callback
	// replaces that source's grant in place (#2107).
	ReconnectSourceID string `json:"reconnectSourceId,omitempty"`
	// OAuthAppID selects a named OAuth app for this connection.
	OAuthAppID string `json:"oauthAppId,omitempty"`
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
	var reconnectID *uuid.UUID
	if raw := strings.TrimSpace(body.ReconnectSourceID); raw != "" {
		id, perr := uuid.Parse(raw)
		if perr != nil {
			return apperror.NewValidation("reconnectSourceId must be a uuid")
		}
		reconnectID = &id
	}
	resp, err := h.svc.StartOAuth(c.Request().Context(), projectID, providerType, StartOAuthRequest{
		Config:            body.Config,
		Scopes:            body.Scopes,
		OAuthAppID:        &body.OAuthAppID,
		ReconnectSourceID: reconnectID,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// oauthScopeProjectParam names the untrusted project hint carried on the
// post-callback landing URL. The web UI (a separate Go module) validates it
// against the caller's access tree and ignores it when it does not resolve —
// it is never an authorization input. Keep the name in sync with
// apps/web-ui/gateway/oauth_landing.go.
const oauthScopeProjectParam = "scope_project"

// oauthLandingQuery builds the landing query string for one callback outcome:
// the one-shot outcome parameter plus, when the flow reached the point of
// knowing it, the project it started from so the UI can land the caller back in
// that scope. An empty projectID means "unknown" (the state was never read).
func oauthLandingQuery(key, value, projectID string) string {
	q := url.Values{}
	q.Set(key, value)
	if projectID != "" {
		q.Set(oauthScopeProjectParam, projectID)
	}
	return "?" + q.Encode()
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
		// The provider rejected before the state was consumed, so the flow's
		// project is unknown; the UI falls back to the caller's active scope.
		return c.Redirect(http.StatusSeeOther, h.svc.OAuthLandingURL(oauthLandingQuery("oauth_error", provErr, "")))
	}
	completion, err := h.svc.CompleteOAuth(c.Request().Context(), providerType, c.QueryParam("state"), c.QueryParam("code"))
	if err != nil {
		// Browser-facing: log the detail, land on a generic error scoped to the
		// project the flow started from (when the state was consumed).
		projectID := ""
		var connectErr *OAuthConnectError
		if errors.As(err, &connectErr) && connectErr.ProjectID != uuid.Nil {
			projectID = connectErr.ProjectID.String()
		}
		h.svc.log.Warn("oauth callback failed",
			slog.String("provider", providerType),
			slog.String("project_id", projectID),
			slog.String("error", err.Error()))
		return c.Redirect(http.StatusSeeOther, h.svc.OAuthLandingURL(oauthLandingQuery("oauth_error", "connect_failed", projectID)))
	}
	return c.Redirect(http.StatusSeeOther, h.svc.OAuthLandingURL(oauthLandingQuery(completion.LandingKey(), completion.Integration.ID, completion.Integration.ProjectID)))
}
