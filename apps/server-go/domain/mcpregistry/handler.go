package mcpregistry

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for MCP server registry.
type Handler struct {
	svc *Service
}

// NewHandler creates a new MCP registry handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListServers handles GET /api/admin/mcp-servers
// @Summary      List MCP servers
// @Description  List all MCP servers registered for the current project
// @Tags         mcp-registry
// @Produce      json
// @Success      200 {object} SuccessResponse{data=[]MCPServerDTO}
// @Failure      401 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/mcp-servers [get]
// @Security     bearerAuth
func (h *Handler) ListServers(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	servers, err := h.svc.ListServers(c.Request().Context(), user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to list MCP servers", err)
	}

	dtos := make([]*MCPServerDTO, 0, len(servers))
	for _, s := range servers {
		dtos = append(dtos, s.ToDTO())
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// GetServer handles GET /api/admin/mcp-servers/:id
// @Summary      Get MCP server details
// @Description  Get detailed information about a specific MCP server
// @Tags         mcp-registry
// @Produce      json
// @Param        id path string true "Server ID"
// @Success      200 {object} SuccessResponse{data=MCPServerDetailDTO}
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	server, err := h.svc.GetServer(c.Request().Context(), id, &user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to get MCP server", err)
	}
	if server == nil {
		return apperror.NewNotFound("mcp_server", id)
	}

	return c.JSON(http.StatusOK, SuccessResponse(server.ToDetailDTO()))
}

// CreateServer handles POST /api/admin/mcp-servers
// @Summary      Create MCP server
// @Description  Register a new MCP server for the current project
// @Tags         mcp-registry
// @Accept       json
// @Produce      json
// @Param        server body CreateMCPServerDTO true "MCP Server configuration"
// @Success      201 {object} SuccessResponse{data=MCPServerDTO}
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/mcp-servers [post]
// @Security     bearerAuth
func (h *Handler) CreateServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	var dto CreateMCPServerDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	server, err := h.svc.CreateServer(c.Request().Context(), user.ProjectID, &dto)
	if err != nil {
		return apperror.NewBadRequest(err.Error())
	}

	return c.JSON(http.StatusCreated, SuccessResponse(server.ToDTO()))
}

// UpdateServer handles PATCH /api/admin/mcp-servers/:id
// @Summary      Update MCP server
// @Description  Update an existing MCP server configuration
// @Tags         mcp-registry
// @Accept       json
// @Produce      json
// @Param        id path string true "Server ID"
// @Param        server body UpdateMCPServerDTO true "Updated server configuration"
// @Success      200 {object} SuccessResponse{data=MCPServerDTO}
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	var dto UpdateMCPServerDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	server, err := h.svc.UpdateServer(c.Request().Context(), id, user.ProjectID, &dto)
	if err != nil {
		return apperror.NewBadRequest(err.Error())
	}
	if server == nil {
		return apperror.NewNotFound("mcp_server", id)
	}

	return c.JSON(http.StatusOK, SuccessResponse(server.ToDTO()))
}

// DeleteServer handles DELETE /api/admin/mcp-servers/:id
// @Summary      Delete MCP server
// @Description  Delete an MCP server and all its tools
// @Tags         mcp-registry
// @Produce      json
// @Param        id path string true "Server ID"
// @Success      204 "No Content"
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	if err := h.svc.DeleteServer(c.Request().Context(), id, user.ProjectID); err != nil {
		return apperror.NewBadRequest(err.Error())
	}

	msg := "MCP server deleted"
	return c.JSON(http.StatusOK, APIResponse[any]{
		Success: true,
		Message: &msg,
	})
}

// ListServerTools handles GET /api/admin/mcp-servers/:id/tools
func (h *Handler) ListServerTools(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	serverID := c.Param("id")
	if serverID == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	// Verify the server exists and belongs to the user's project
	server, err := h.svc.GetServer(c.Request().Context(), serverID, &user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to get MCP server", err)
	}
	if server == nil {
		return apperror.NewNotFound("mcp_server", serverID)
	}

	tools, err := h.svc.ListTools(c.Request().Context(), serverID)
	if err != nil {
		return apperror.NewInternal("failed to list server tools", err)
	}

	dtos := make([]*MCPServerToolDTO, 0, len(tools))
	for _, t := range tools {
		dtos = append(dtos, t.ToDTO())
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// ToggleTool handles PATCH /api/admin/mcp-servers/:id/tools/:toolId
func (h *Handler) ToggleTool(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	toolID := c.Param("toolId")
	if toolID == "" {
		return apperror.NewBadRequest("tool ID is required")
	}

	var dto UpdateMCPServerToolDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if dto.Enabled == nil {
		return apperror.NewBadRequest("enabled field is required")
	}

	if err := h.svc.ToggleTool(c.Request().Context(), toolID, *dto.Enabled); err != nil {
		return apperror.NewBadRequest(err.Error())
	}

	msg := "tool updated"
	return c.JSON(http.StatusOK, APIResponse[any]{
		Success: true,
		Message: &msg,
	})
}

// InspectServer handles POST /api/admin/mcp-servers/:id/inspect
//
// Performs a diagnostic test-connection to an MCP server. Creates a fresh
// ephemeral connection, captures the InitializeResult (server name, version,
// capabilities), and enumerates tools, prompts, and resources based on what
// the server advertises. Always returns 200 with the inspect result — connection
// errors are reported in the response body (status: "error"), not as HTTP errors.
func (h *Handler) InspectServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	serverID := c.Param("id")
	if serverID == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	result, err := h.svc.InspectServer(c.Request().Context(), serverID, user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to inspect MCP server", err)
	}
	if result == nil {
		return apperror.NewNotFound("mcp_server", serverID)
	}

	return c.JSON(http.StatusOK, SuccessResponse(result))
}

// SyncTools handles POST /api/admin/mcp-servers/:id/sync
//
// Two modes:
//   - Auto-discover (default): If the request body is empty or contains
//     {"auto_discover": true}, connects to the external MCP server and calls
//     tools/list to discover tools automatically.
//   - Manual sync: If the request body contains an array of tool definitions,
//     syncs those tools directly (useful for testing or when auto-discover fails).
func (h *Handler) SyncTools(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	serverID := c.Param("id")
	if serverID == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	// Verify the server exists and belongs to the user's project
	server, err := h.svc.GetServer(c.Request().Context(), serverID, &user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to get MCP server", err)
	}
	if server == nil {
		return apperror.NewNotFound("mcp_server", serverID)
	}

	ctx := c.Request().Context()

	// Try to parse the body to determine sync mode.
	// If body is empty, a JSON object, or contains auto_discover: true → auto-discover.
	// If body is a JSON array of tool definitions → manual sync.
	var toolCount int

	body := c.Request().Body
	if body == nil {
		// Empty body → auto-discover
		discovered, err := h.svc.DiscoverAndSyncTools(ctx, serverID, user.ProjectID)
		if err != nil {
			return apperror.NewBadRequest("auto-discover failed: " + err.Error())
		}
		toolCount = len(discovered)
	} else {
		// Try to decode as array (manual sync mode)
		var discoveredTools []DiscoveredTool
		if err := c.Bind(&discoveredTools); err != nil || len(discoveredTools) == 0 {
			// Not an array or empty array → auto-discover
			discovered, err := h.svc.DiscoverAndSyncTools(ctx, serverID, user.ProjectID)
			if err != nil {
				return apperror.NewBadRequest("auto-discover failed: " + err.Error())
			}
			toolCount = len(discovered)
		} else {
			// Manual sync with provided tools
			if err := h.svc.SyncServerTools(ctx, serverID, discoveredTools); err != nil {
				return apperror.NewInternal("failed to sync tools", err)
			}
			toolCount = len(discoveredTools)
		}
	}

	msg := fmt.Sprintf("synced %d tools successfully", toolCount)
	return c.JSON(http.StatusOK, APIResponse[any]{
		Success: true,
		Message: &msg,
	})
}

// ============================================================================
// Official MCP Registry Browse/Install Handlers
// ============================================================================

// SearchRegistry handles GET /api/admin/mcp-registry/search
// Query params: q (search query), limit (int), cursor (pagination)
func (h *Handler) SearchRegistry(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	query := c.QueryParam("q")
	limitStr := c.QueryParam("limit")
	cursor := c.QueryParam("cursor")

	limit := 20
	if limitStr != "" {
		if _, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil {
			return apperror.NewBadRequest("invalid limit parameter")
		}
	}

	result, err := h.svc.SearchRegistry(c.Request().Context(), query, limit, cursor)
	if err != nil {
		return apperror.NewInternal("failed to search MCP registry", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(result))
}

// GetRegistryServer handles GET /api/admin/mcp-registry/servers/:name
// The name parameter is the registry server name (URL-encoded, e.g. "io.github.github%2Fgithub-mcp-server").
func (h *Handler) GetRegistryServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	name := c.Param("name")
	if name == "" {
		return apperror.NewBadRequest("server name is required")
	}

	result, err := h.svc.GetRegistryServer(c.Request().Context(), name)
	if err != nil {
		return apperror.NewBadRequest("failed to get registry server: " + err.Error())
	}

	return c.JSON(http.StatusOK, SuccessResponse(result))
}

// InstallFromRegistry handles POST /api/admin/mcp-registry/install
func (h *Handler) InstallFromRegistry(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	var dto InstallFromRegistryDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if dto.RegistryName == "" {
		return apperror.NewBadRequest("registryName is required")
	}

	result, err := h.svc.InstallFromRegistry(c.Request().Context(), user.ProjectID, &dto)
	if err != nil {
		return apperror.NewBadRequest("failed to install from registry: " + err.Error())
	}

	return c.JSON(http.StatusCreated, SuccessResponse(result))
}
