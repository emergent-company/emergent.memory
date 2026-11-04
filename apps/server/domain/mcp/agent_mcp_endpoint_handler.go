package mcp

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// HandleCreateAgentEndpoint handles
// POST /api/projects/:projectId/agents/:agentId/mcp-endpoint.
//
// @Summary      Create an agent's MCP endpoint
// @Description  Establishes the single active MCP endpoint owned by an agent. Returns 409 when an active endpoint already exists.
// @Tags         mcp
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        agentId path string true "Agent ID (UUID)"
// @Success      201 {object} AgentMCPEndpointDTO
// @Failure      403 {object} apperror.Error "Project admin required"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      409 {object} apperror.Error "Active endpoint already exists"
// @Router       /api/projects/{projectId}/agents/{agentId}/mcp-endpoint [post]
// @Security     bearerAuth
func (h *Handler) HandleCreateAgentEndpoint(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	agentID := c.Param("agentId")
	if projectID == "" || agentID == "" {
		return apperror.NewBadRequest("projectId and agentId are required")
	}
	dto, err := h.svc.CreateAgentEndpoint(c.Request().Context(), projectID, user.ID, requestBaseURL(c), agentID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, dto)
}

// HandleGetAgentEndpoint handles
// GET /api/projects/:projectId/agents/:agentId/mcp-endpoint.
func (h *Handler) HandleGetAgentEndpoint(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	agentID := c.Param("agentId")
	if projectID == "" || agentID == "" {
		return apperror.NewBadRequest("projectId and agentId are required")
	}
	dto, err := h.svc.GetAgentEndpoint(c.Request().Context(), projectID, user.ID, requestBaseURL(c), agentID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, dto)
}

// HandleRevokeAgentEndpoint handles
// DELETE /api/projects/:projectId/agent-mcp-endpoints/:id.
func (h *Handler) HandleRevokeAgentEndpoint(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	if err := h.svc.RevokeAgentEndpoint(c.Request().Context(), projectID, user.ID, id); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// HandleCreateAgentKey handles
// POST /api/projects/:projectId/agent-mcp-endpoints/:id/keys.
//
// @Summary      Create a labeled key on an agent MCP endpoint
// @Description  Mints a project API token bound to the endpoint as a labeled key. Returns the raw token exactly once.
// @Tags         mcp
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        id path string true "Endpoint ID (UUID)"
// @Param        request body CreateAgentMCPKeyRequest true "Key"
// @Success      201 {object} CreateAgentMCPKeyResponse
// @Failure      403 {object} apperror.Error "Project admin required"
// @Failure      404 {object} apperror.Error "Endpoint not found"
// @Failure      409 {object} apperror.Error "Duplicate label"
// @Router       /api/projects/{projectId}/agent-mcp-endpoints/{id}/keys [post]
// @Security     bearerAuth
func (h *Handler) HandleCreateAgentKey(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	endpointID := c.Param("id")
	if projectID == "" || endpointID == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	var req CreateAgentMCPKeyRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	resp, err := h.svc.CreateAgentKey(c.Request().Context(), projectID, user.ID, requestBaseURL(c), endpointID, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, resp)
}

// HandleListAgentKeys handles
// GET /api/projects/:projectId/agent-mcp-endpoints/:id/keys.
func (h *Handler) HandleListAgentKeys(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	endpointID := c.Param("id")
	if projectID == "" || endpointID == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	resp, err := h.svc.ListAgentKeys(c.Request().Context(), projectID, user.ID, endpointID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

// HandleRevokeAgentKey handles
// DELETE /api/projects/:projectId/agent-mcp-keys/:id.
func (h *Handler) HandleRevokeAgentKey(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	if err := h.svc.RevokeAgentKey(c.Request().Context(), projectID, user.ID, id); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// HandleRotateAgentKey handles
// POST /api/projects/:projectId/agent-mcp-keys/:id/rotate.
func (h *Handler) HandleRotateAgentKey(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	id := c.Param("id")
	if projectID == "" || id == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	resp, err := h.svc.RotateAgentKey(c.Request().Context(), projectID, user.ID, requestBaseURL(c), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
