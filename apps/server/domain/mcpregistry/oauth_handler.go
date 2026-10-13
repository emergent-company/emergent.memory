package mcpregistry

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// StartMCPOAuth handles POST /api/admin/mcp-servers/:id/oauth/authorize.
// @Summary      Start an OAuth connect for an MCP server
// @Description  Discovers the server's authorization requirement, registers a client when offered, and returns an authorization URL (state + PKCE)
// @Tags         mcp-registry
// @Produce      json
// @Param        id path string true "Server ID"
// @Success      200 {object} mcpregistry.APIResponse[mcpregistry.StartMCPOAuthResponseDTO]
// @Failure      400 {object} apperror.Error "Not a remote server, no authorization requirement, or the deployment is not configured"
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Failure      502 {object} apperror.Error "Authorization server metadata could not be fetched"
// @Router       /api/admin/mcp-servers/{id}/oauth/authorize [post]
// @Security     bearerAuth
func (h *Handler) StartMCPOAuth(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}
	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	resp, err := h.svc.StartMCPOAuth(c.Request().Context(), user.ProjectID, id, user.ID)
	if err != nil {
		return mcpOAuthError(err)
	}
	return c.JSON(http.StatusOK, SuccessResponse(resp))
}

// MCPOAuthStatus handles GET /api/admin/mcp-servers/:id/oauth/status.
// @Summary      MCP server authorization status
// @Description  Reports whether the server has a stored OAuth credential, its authorization server, scope, and expiry (never token values)
// @Tags         mcp-registry
// @Produce      json
// @Param        id path string true "Server ID"
// @Success      200 {object} mcpregistry.APIResponse[mcpregistry.MCPServerOAuthStatusDTO]
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id}/oauth/status [get]
// @Security     bearerAuth
func (h *Handler) MCPOAuthStatus(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}
	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	status, err := h.svc.MCPOAuthStatus(c.Request().Context(), user.ProjectID, id)
	if err != nil {
		return mcpOAuthError(err)
	}
	return c.JSON(http.StatusOK, SuccessResponse(status))
}

// DisconnectMCPOAuth handles DELETE /api/admin/mcp-servers/:id/oauth.
// @Summary      Disconnect an MCP server's OAuth credential
// @Description  Deletes the stored OAuth credential and closes the pooled connection; static headers are untouched
// @Tags         mcp-registry
// @Param        id path string true "Server ID"
// @Success      204 "Credential removed"
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id}/oauth [delete]
// @Security     bearerAuth
func (h *Handler) DisconnectMCPOAuth(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}
	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	if err := h.svc.DisconnectMCPOAuth(c.Request().Context(), user.ProjectID, id); err != nil {
		return mcpOAuthError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// CompleteMCPOAuth handles GET /api/mcp-servers/oauth/callback.
//
// It is intentionally unauthenticated: the browser arrives cross-site from the
// authorization server. The trust anchor is the single-use, short-lived,
// unguessable state (which also carries the PKCE verifier), exactly as in the
// data-source connect flow. Every failure path lands the browser back on the UI
// with an error indication — the callback never returns raw JSON to a user.
// @Summary      MCP OAuth connect callback
// @Description  Consumes the pending state, exchanges the authorization code, stores the token set, and redirects to the UI
// @Tags         mcp-registry
// @Param        code query string false "Authorization code"
// @Param        state query string true "Opaque single-use state"
// @Success      303 "Redirect to the MCP servers page"
// @Router       /api/mcp-servers/oauth/callback [get]
func (h *Handler) CompleteMCPOAuth(c echo.Context) error {
	ctx := c.Request().Context()
	if provErr := c.QueryParam("error"); provErr != "" {
		h.svc.log.Warn("MCP OAuth authorization was denied", slog.String("error", provErr))
		return c.Redirect(http.StatusSeeOther, h.svc.oauthCfg.landingURL("denied"))
	}

	serverID, projectID, err := h.svc.CompleteMCPOAuth(ctx, c.QueryParam("state"), c.QueryParam("code"))
	if err != nil {
		// Browser-facing: log the detail, land on a generic error so no internal
		// detail (or any part of the code/verifier) reaches the redirect.
		h.svc.log.Warn("MCP OAuth callback failed",
			slog.String("project_id", projectID),
			slog.String("server_id", serverID),
			slog.String("error", err.Error()),
		)
		return c.Redirect(http.StatusSeeOther, h.svc.oauthCfg.landingURL("error"))
	}
	return c.Redirect(http.StatusSeeOther, h.svc.oauthCfg.landingURL("connected"))
}

// mcpOAuthError maps the OAuth connect flow's sentinel errors onto HTTP
// responses. Errors the user can act on are 400/404; an upstream authorization
// server failure is 502.
func mcpOAuthError(err error) error {
	switch {
	case errors.Is(err, ErrServerNotFound):
		return apperror.NewNotFound("mcp_server", "server not found")
	case errors.Is(err, ErrOAuthNotConfigured),
		errors.Is(err, ErrOAuthNotSupported),
		errors.Is(err, ErrOAuthNotRequired),
		errors.Is(err, ErrOutboundURLNotAllowed):
		return apperror.NewBadRequest(err.Error())
	case errors.Is(err, ErrOAuthNeedsReconnect):
		return apperror.NewConflict(err.Error())
	case errors.Is(err, ErrOAuthDiscoveryFailed):
		// Upstream authorization-server / metadata failure, not bad input.
		return apperror.NewServiceUnavailable(err.Error(), err)
	default:
		return apperror.NewInternal("MCP OAuth connect failed", err)
	}
}
