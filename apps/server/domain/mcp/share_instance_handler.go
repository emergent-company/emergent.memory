package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// requestBaseURL derives the public base URL (scheme://host) from a request.
func requestBaseURL(c echo.Context) string {
	scheme := "https"
	if c.Request().TLS == nil && c.Request().Header.Get("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	if proto := c.Request().Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + c.Request().Host
}

// HandleCreateShareInstance handles POST /api/projects/:projectId/mcp/shares.
//
// @Summary      Create an MCP share instance
// @Description  Creates a named MCP share instance bound to a new project API token with the selected tool allowlist (tools is required and must be non-empty), and optionally emails the one-time key and client setup snippets to the listed recipients. Returns the raw token exactly once.
// @Tags         mcp
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body CreateShareInstanceHTTPRequest true "Share instance"
// @Success      201 {object} CreateShareInstanceResponse
// @Failure      403 {object} apperror.Error "Project admin required"
// @Failure      409 {object} apperror.Error "Duplicate name"
// @Failure      422 {object} apperror.Error "Invalid allowlist or email address"
// @Router       /api/projects/{projectId}/mcp/shares [post]
// @Security     bearerAuth
func (h *Handler) HandleCreateShareInstance(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	var req CreateShareInstanceHTTPRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Reject a malformed address before anything is created.
	emails, err := normalizeInviteEmails(req.Emails)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()
	baseURL := requestBaseURL(c)
	resp, err := h.svc.CreateShareInstance(ctx, projectID, user.ID, baseURL, req.CreateShareInstanceRequest)
	if err != nil {
		return err
	}
	h.sendShareInstanceInvites(ctx, user, projectID, baseURL, emails, resp)
	return c.JSON(http.StatusCreated, resp)
}

// sendShareInstanceInvites queues the create/rotate invite emails for a freshly
// minted share key. Delivery problems are reported on the response, never as an
// error: the key exists and the share is usable regardless.
func (h *Handler) sendShareInstanceInvites(ctx context.Context, user *auth.AuthUser, projectID, baseURL string, emails []string, resp *CreateShareInstanceResponse) {
	if resp == nil || len(emails) == 0 {
		return
	}
	senderName := h.senderDisplayName(ctx, user.ID, user.Email)
	sent, problem := h.svc.SendShareInviteEmails(ctx, projectID, senderName, baseURL, resp.Token, emails)
	resp.EmailsSent = sent
	resp.EmailError = problem
}

// HandleListShareInstances handles GET /api/projects/:projectId/mcp/shares.
func (h *Handler) HandleListShareInstances(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	resp, err := h.svc.ListShareInstances(c.Request().Context(), projectID, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// HandleGetShareInstance handles GET /api/projects/:projectId/mcp/shares/:id.
func (h *Handler) HandleGetShareInstance(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	dto, err := h.svc.GetShareInstance(c.Request().Context(), projectID, user.ID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// HandleUpdateShareInstance handles PATCH /api/projects/:projectId/mcp/shares/:id.
func (h *Handler) HandleUpdateShareInstance(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}

	var req UpdateShareInstanceRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	dto, err := h.svc.UpdateShareInstance(c.Request().Context(), projectID, user.ID, id, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// HandleRevokeShareInstance handles DELETE /api/projects/:projectId/mcp/shares/:id.
func (h *Handler) HandleRevokeShareInstance(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	if err := h.svc.RevokeShareInstance(c.Request().Context(), projectID, user.ID, id); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// HandleRotateShareInstance handles POST /api/projects/:projectId/mcp/shares/:id/rotate.
//
// @Summary      Rotate an MCP share key
// @Description  Issues a new key for a share instance, invalidating the previous one, and optionally emails the new key and client setup snippets to the listed recipients. The body may be omitted.
// @Tags         mcp
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        id path string true "Share instance ID"
// @Param        request body RotateShareInstanceHTTPRequest false "Optional invite recipients"
// @Success      200 {object} CreateShareInstanceResponse
// @Failure      404 {object} apperror.Error "Unknown share"
// @Failure      409 {object} apperror.Error "Share is revoked"
// @Failure      422 {object} apperror.Error "Invalid email address"
// @Router       /api/projects/{projectId}/mcp/shares/{id}/rotate [post]
// @Security     bearerAuth
func (h *Handler) HandleRotateShareInstance(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}

	// The body is optional: a bare rotate (no invite) is the common case. Echo
	// skips body binding for ContentLength 0, and a chunked request with no body
	// decodes to io.EOF — both mean "no recipients".
	var req RotateShareInstanceHTTPRequest
	if err := c.Bind(&req); err != nil && !errors.Is(err, io.EOF) {
		return apperror.NewBadRequest("invalid request body")
	}
	emails, err := normalizeInviteEmails(req.Emails)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()
	baseURL := requestBaseURL(c)
	resp, err := h.svc.RotateShareInstance(ctx, projectID, user.ID, id, baseURL)
	if err != nil {
		return err
	}
	h.sendShareInstanceInvites(ctx, user, projectID, baseURL, emails, resp)
	return c.JSON(http.StatusOK, resp)
}

// HandleInviteShareInstance handles POST /api/projects/:projectId/mcp/shares/:id/invite.
//
// @Summary      Email an existing MCP share key
// @Description  Emails the share instance's CURRENT key and client setup snippets to the listed recipients. The key is recovered from encrypted storage, never rotated, so existing clients keep working. Returns a 422 not_recoverable when the key was stored without encryption — rotate the share instead.
// @Tags         mcp
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        id path string true "Share instance ID"
// @Param        request body InviteShareInstanceHTTPRequest true "Invite recipients"
// @Success      200 {object} InviteShareInstanceResponse
// @Failure      400 {object} apperror.Error "No recipients supplied"
// @Failure      403 {object} apperror.Error "Project admin required"
// @Failure      404 {object} apperror.Error "Unknown share"
// @Failure      409 {object} apperror.Error "Share is revoked"
// @Failure      422 {object} apperror.Error "Invalid email address, or the key is not recoverable"
// @Router       /api/projects/{projectId}/mcp/shares/{id}/invite [post]
// @Security     bearerAuth
func (h *Handler) HandleInviteShareInstance(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}

	var req InviteShareInstanceHTTPRequest
	if err := c.Bind(&req); err != nil && !errors.Is(err, io.EOF) {
		return apperror.NewBadRequest("invalid request body")
	}
	// Reject a malformed address before recovering anything.
	emails, err := normalizeInviteEmails(req.Emails)
	if err != nil {
		return err
	}
	if len(emails) == 0 {
		return apperror.NewBadRequest("at least one recipient email address is required")
	}

	ctx := c.Request().Context()
	key, err := h.svc.RevealShareInstanceKey(ctx, projectID, user.ID, id)
	if err != nil {
		return err
	}
	senderName := h.senderDisplayName(ctx, user.ID, user.Email)
	sent, problem := h.svc.SendShareInviteEmails(ctx, projectID, senderName, requestBaseURL(c), key, emails)
	return c.JSON(http.StatusOK, InviteShareInstanceResponse{
		TokenPrefix: shareKeyPrefix(key),
		EmailsSent:  sent,
		EmailError:  problem,
	})
}

// HandleListToolCatalog handles GET /api/projects/:projectId/mcp/tools.
//
// @Summary      List includable MCP tools
// @Description  Returns the memory tools that can be granted to an MCP share instance (name, description, required scope, category), excluding agent-only tools, plus the named share templates resolved against that catalog. Admin only.
// @Tags         mcp
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {object} CatalogResponse
// @Failure      403 {object} apperror.Error "Project admin required"
// @Router       /api/projects/{projectId}/mcp/tools [get]
// @Security     bearerAuth
func (h *Handler) HandleListToolCatalog(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	resp, err := h.svc.ListToolCatalog(c.Request().Context(), projectID, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
