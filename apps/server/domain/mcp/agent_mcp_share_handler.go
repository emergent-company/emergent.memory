package mcp

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// HandleCreateAgentShare handles POST /api/projects/:projectId/agents/:id/mcp-share.
//
// @Summary      Create a per-agent MCP share
// @Description  Creates a share credential binding a new project API token to one agent, exposing it as a single-tool MCP server. Returns the raw token exactly once.
// @Tags         mcp
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        id path string true "Agent ID (UUID)"
// @Param        request body CreateAgentMCPShareRequest true "Share"
// @Success      201 {object} CreateAgentMCPShareResponse
// @Failure      403 {object} apperror.Error "Project admin required"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      409 {object} apperror.Error "Duplicate name"
// @Failure      422 {object} apperror.Error "Invalid request"
// @Router       /api/projects/{projectId}/agents/{id}/mcp-share [post]
// @Security     bearerAuth
func (h *Handler) HandleCreateAgentShare(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	agentID := c.Param("id")
	if projectID == "" || agentID == "" {
		return apperror.NewBadRequest("projectId and agent id are required")
	}

	var req CreateAgentMCPShareRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	resp, err := h.svc.CreateAgentShare(c.Request().Context(), projectID, user.ID, requestBaseURL(c), agentID, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, resp)
}

// HandleListAgentShares handles GET /api/projects/:projectId/agents/:id/mcp-shares.
func (h *Handler) HandleListAgentShares(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	agentID := c.Param("id")
	if projectID == "" || agentID == "" {
		return apperror.NewBadRequest("projectId and agent id are required")
	}
	resp, err := h.svc.ListAgentShares(c.Request().Context(), projectID, user.ID, agentID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// HandleListProjectAgentShares handles GET /api/projects/:projectId/agent-mcp-shares.
func (h *Handler) HandleListProjectAgentShares(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	resp, err := h.svc.ListProjectAgentShares(c.Request().Context(), projectID, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// HandleRevokeAgentShare handles DELETE /api/projects/:projectId/agent-mcp-shares/:id.
func (h *Handler) HandleRevokeAgentShare(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	if err := h.svc.RevokeAgentShare(c.Request().Context(), projectID, user.ID, id); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// HandleRotateAgentShare handles POST /api/projects/:projectId/agent-mcp-shares/:id/rotate.
func (h *Handler) HandleRotateAgentShare(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	resp, err := h.svc.RotateAgentShare(c.Request().Context(), projectID, user.ID, id, requestBaseURL(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
