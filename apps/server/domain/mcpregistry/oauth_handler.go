package mcpregistry

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

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
// @Failure      422 {object} apperror.Error "Provider publishes no OAuth authorization metadata — configure a token header instead"
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

	status, err := h.svc.MCPOAuthStatus(c.Request().Context(), user.ProjectID, id, user.ID)
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

	if err := h.svc.DisconnectMCPOAuth(c.Request().Context(), user.ProjectID, id, user.ID); err != nil {
		return mcpOAuthError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// ProvisionMCPOAuthClient handles PUT /api/admin/mcp-servers/:id/oauth/client.
// @Summary      Provision a pre-registered OAuth client for an MCP server
// @Description  Stores a pre-registered client id (and optional secret) plus the optional scope set to request for a remote server, clearing any token set issued to a previous client. A blank clientId with a scope updates only the stored client's requested scope set.
// @Tags         mcp-registry
// @Accept       json
// @Produce      json
// @Param        id path string true "Server ID"
// @Param        body body mcpregistry.ProvisionMCPOAuthClientDTO true "Client identity"
// @Success      200 {object} mcpregistry.APIResponse[mcpregistry.MCPServerOAuthStatusDTO]
// @Failure      400 {object} apperror.Error "Server is not remote or both clientId and scope are blank"
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id}/oauth/client [put]
// @Security     bearerAuth
func (h *Handler) ProvisionMCPOAuthClient(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}
	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("server ID is required")
	}
	var body ProvisionMCPOAuthClientDTO
	if err := c.Bind(&body); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if strings.TrimSpace(body.ClientID) == "" && strings.TrimSpace(body.Scope) == "" {
		return apperror.NewBadRequest("clientId is required until an OAuth client is provisioned")
	}

	status, err := h.svc.ProvisionOAuthClient(c.Request().Context(), user.ProjectID, id, user.ID, body.ClientID, body.ClientSecret, body.Scope)
	if err != nil {
		return mcpOAuthError(err)
	}
	return c.JSON(http.StatusOK, SuccessResponse(status))
}

// ClearMCPOAuthClient handles DELETE /api/admin/mcp-servers/:id/oauth/client.
// @Summary      Clear an MCP server's OAuth client identity
// @Description  Clears the stored client id, client secret, and any token set, leaving discovered endpoints in place
// @Tags         mcp-registry
// @Param        id path string true "Server ID"
// @Success      204 "Client identity cleared"
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id}/oauth/client [delete]
// @Security     bearerAuth
func (h *Handler) ClearMCPOAuthClient(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}
	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	if err := h.svc.ClearOAuthClient(c.Request().Context(), user.ProjectID, id, user.ID); err != nil {
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
		// The authorization server rejected before the state was consumed, so
		// the flow's project is unknown; the UI falls back to the caller's scope.
		return c.Redirect(http.StatusSeeOther, h.svc.oauthCfg.landingURL("denied", ""))
	}

	serverID, projectID, err := h.svc.CompleteMCPOAuth(ctx, c.QueryParam("state"), c.QueryParam("code"))
	if err != nil {
		// Browser-facing: log the detail, land on a generic error so no internal
		// detail (or any part of the code/verifier) reaches the redirect. The
		// project is forwarded as a hint so the UI returns to that scope.
		h.svc.log.Warn("MCP OAuth callback failed",
			slog.String("project_id", projectID),
			slog.String("server_id", serverID),
			slog.String("error", err.Error()),
		)
		return c.Redirect(http.StatusSeeOther, h.svc.oauthCfg.landingURL("error", projectID))
	}
	return c.Redirect(http.StatusSeeOther, h.svc.oauthCfg.landingURL("connected", projectID))
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
		errors.Is(err, ErrOAuthClientIDRequired),
		errors.Is(err, ErrOutboundURLNotAllowed):
		return apperror.NewBadRequest(err.Error())
	case errors.Is(err, ErrOAuthClientRequired):
		// The authorization server will not issue us a client (registration
		// unavailable or rejected). This is actionable, not an internal fault.
		return apperror.NewBadRequest(oauthClientRequiredMessage(err))
	case errors.Is(err, ErrOAuthMetadataUnavailable):
		// The provider publishes no discoverable OAuth metadata, so the browser
		// connect flow cannot work. Actionable, not an internal fault.
		return apperror.NewValidation(oauthMetadataUnavailableMessage(err))
	case errors.Is(err, ErrOAuthNeedsReconnect):
		return apperror.NewConflict(err.Error())
	case errors.Is(err, ErrOAuthDiscoveryFailed):
		// Upstream authorization-server / metadata failure, not bad input.
		return apperror.NewServiceUnavailable(err.Error(), err)
	default:
		return apperror.NewInternal("MCP OAuth connect failed", err)
	}
}

// oauthClientRequiredMessage builds the user-facing copy for ErrOAuthClientRequired.
// When the rejection carries an HTTP status (a dynamic client registration was
// answered with a non-2xx), it is named; otherwise the message covers the
// registration-unavailable case. Either way it is one actionable sentence with
// no internal identifiers.
func oauthClientRequiredMessage(err error) string {
	var rej *oauthRegistrationRejected
	if errors.As(err, &rej) && rej.Status != 0 {
		return fmt.Sprintf("the authorization server rejected dynamic client registration (HTTP %d); this server needs a pre-registered OAuth client — configure one for this server and retry", rej.Status)
	}
	return "this authorization server will not issue a client — dynamic client registration is unavailable or was rejected; configure a pre-registered OAuth client for this server and retry"
}

// oauthMetadataUnavailableRemedy is the token-header remedy shown when a
// provider publishes no discoverable OAuth metadata.
const oauthMetadataUnavailableRemedy = "configure a token header for this server instead (e.g. Authorization: Bearer <token>)"

// oauthMetadataUnavailableMessage builds the user-facing copy for
// ErrOAuthMetadataUnavailable: one actionable sentence naming the token-header
// remedy, with no internal identifiers.
func oauthMetadataUnavailableMessage(_ error) string {
	return "this provider publishes no OAuth authorization metadata, so the browser connect flow cannot be used — " + oauthMetadataUnavailableRemedy
}
