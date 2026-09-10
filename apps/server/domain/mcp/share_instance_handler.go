package mcp

import (
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
// @Description  Creates a named MCP share instance bound to a new project API token with the selected tool/agent allowlists. Returns the raw token exactly once.
// @Tags         mcp
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        request body CreateShareInstanceRequest true "Share instance"
// @Success      201 {object} CreateShareInstanceResponse
// @Failure      403 {object} apperror.Error "Project admin required"
// @Failure      409 {object} apperror.Error "Duplicate name"
// @Failure      422 {object} apperror.Error "Invalid allowlist"
// @Router       /api/projects/{projectId}/mcp/shares [post]
// @Security     bearerAuth
func (h *Handler) HandleCreateShareInstance(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}

	var req CreateShareInstanceRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	resp, err := h.svc.CreateShareInstance(c.Request().Context(), projectID, user.ID, requestBaseURL(c), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, resp)
}

// HandleListShareInstances handles GET /api/projects/:projectId/mcp/shares.
func (h *Handler) HandleListShareInstances(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.ErrBadRequest.WithMessage("projectId is required")
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
		return apperror.ErrBadRequest.WithMessage("projectId and id are required")
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
		return apperror.ErrBadRequest.WithMessage("projectId and id are required")
	}

	var req UpdateShareInstanceRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
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
		return apperror.ErrBadRequest.WithMessage("projectId and id are required")
	}
	if err := h.svc.RevokeShareInstance(c.Request().Context(), projectID, user.ID, id); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// HandleRotateShareInstance handles POST /api/projects/:projectId/mcp/shares/:id/rotate.
func (h *Handler) HandleRotateShareInstance(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.ErrBadRequest.WithMessage("projectId and id are required")
	}
	resp, err := h.svc.RotateShareInstance(c.Request().Context(), projectID, user.ID, id, requestBaseURL(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// HandleListToolCatalog handles GET /api/projects/:projectId/mcp/tools.
//
// @Summary      List includable MCP tools
// @Description  Returns the memory tools that can be granted to an MCP share instance (name, description, required scope, category), excluding agent-only tools. Admin only.
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
		return apperror.ErrBadRequest.WithMessage("projectId is required")
	}
	resp, err := h.svc.ListToolCatalog(c.Request().Context(), projectID, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
