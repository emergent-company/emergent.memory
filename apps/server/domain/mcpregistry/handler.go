package mcpregistry

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
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
// @Success      200 {object} mcpregistry.APIResponse[[]mcpregistry.MCPServerDTO]
// @Failure      401 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/mcp-servers [get]
// @Security     bearerAuth
func (h *Handler) ListServers(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	// Ensure the builtin server exists for this project so it appears in the list.
	if err := h.svc.EnsureBuiltinServer(c.Request().Context(), user.ProjectID); err != nil {
		// Non-fatal: log and proceed — listing still works for external servers.
		slog.Warn("failed to ensure builtin server on list", "error", err, "project_id", user.ProjectID)
	}

	servers, err := h.svc.ListServers(c.Request().Context(), user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to list MCP servers", err)
	}

	dtos := make([]*MCPServerDTO, 0, len(servers))
	for _, s := range servers {
		dtos = append(dtos, s.ToDTO())
	}
	// Advisory presentation detail: a failure here must not break the list.
	if err := h.svc.DecorateOAuthSummaries(c.Request().Context(), user.ProjectID, user.ID, servers, dtos); err != nil {
		slog.Warn("failed to load MCP server authorization summaries", "error", err, "project_id", user.ProjectID)
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// GetServer handles GET /api/admin/mcp-servers/:id
// @Summary      Get MCP server details
// @Description  Get detailed information about a specific MCP server
// @Tags         mcp-registry
// @Produce      json
// @Param        id path string true "Server ID"
// @Success      200 {object} mcpregistry.APIResponse[mcpregistry.MCPServerDetailDTO]
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetServer(c echo.Context) error {
	user := auth.MustGetUser(c)
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

	dto := server.ToDetailDTO()
	if err := h.svc.DecorateOAuthSummaries(c.Request().Context(), user.ProjectID, user.ID, []*MCPServer{server}, []*MCPServerDTO{&dto.MCPServerDTO}); err != nil {
		slog.Warn("failed to load MCP server authorization summary", "error", err, "server_id", id)
	}

	return c.JSON(http.StatusOK, SuccessResponse(dto))
}

// CreateServer handles POST /api/admin/mcp-servers
// @Summary      Create MCP server
// @Description  Register a new MCP server for the current project
// @Tags         mcp-registry
// @Accept       json
// @Produce      json
// @Param        server body CreateMCPServerDTO true "MCP Server configuration"
// @Success      201 {object} mcpregistry.APIResponse[mcpregistry.MCPServerDTO]
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/mcp-servers [post]
// @Security     bearerAuth
func (h *Handler) CreateServer(c echo.Context) error {
	user := auth.MustGetUser(c)
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
// @Success      200 {object} mcpregistry.APIResponse[mcpregistry.MCPServerDTO]
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Failure      500 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateServer(c echo.Context) error {
	user := auth.MustGetUser(c)
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
	user := auth.MustGetUser(c)
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
	user := auth.MustGetUser(c)

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
		dto := t.ToDTO()
		// For builtin servers, enrich each tool with its resolved enabled state and
		// inheritance source so org-level defaults are surfaced truthfully (#988).
		if server.Type == ServerTypeBuiltin {
			resolvedEnabled, _, source, resolveErr := h.svc.ResolveBuiltinToolSettings(c.Request().Context(), user.ProjectID, t.ToolName)
			if resolveErr == nil {
				dto.Enabled = resolvedEnabled
				dto.InheritedFrom = source
			}
		}
		dtos = append(dtos, dto)
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// ToggleTool handles PATCH /api/admin/mcp-servers/:id/tools/:toolId
func (h *Handler) ToggleTool(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	// The :id server param is authoritative (issue #978): the addressed server
	// must belong to the caller's project. A foreign or missing server is 404,
	// matching the sandboximages :id convention (#968/#974).
	serverID := c.Param("id")
	if serverID == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	server, err := h.svc.GetServer(c.Request().Context(), serverID, &user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to get MCP server", err)
	}
	if server == nil {
		return apperror.NewNotFound("mcp_server", serverID)
	}

	toolID := c.Param("toolId")
	if toolID == "" {
		return apperror.NewBadRequest("tool ID is required")
	}

	var dto UpdateMCPServerToolDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if dto.Enabled == nil && dto.Config == nil {
		return apperror.NewBadRequest("at least one of enabled or config must be provided")
	}

	if err := h.svc.UpdateTool(c.Request().Context(), user.ProjectID, toolID, dto.Enabled, dto.Config); err != nil {
		if errors.Is(err, ErrToolNotFound) {
			return apperror.NewNotFound("mcp_server_tool", toolID)
		}
		return apperror.NewBadRequest(err.Error())
	}

	msg := "tool updated"
	return c.JSON(http.StatusOK, APIResponse[any]{
		Success: true,
		Message: &msg,
	})
}

// callToolRequest is the request body for POST
// /api/admin/mcp-servers/:id/tools/:toolName/call. Arguments is nil when the
// body is absent or empty.
type callToolRequest struct {
	Arguments map[string]any `json:"arguments"`
}

// CallToolOnServer handles POST /api/admin/mcp-servers/:id/tools/:toolName/call
//
// Invokes a single tool on the addressed server with the provided arguments.
// Builtin tools execute in-process; external (stdio/sse/http) tools are proxied
// to their MCP server. The result is an mcp.ToolResult in the standard envelope.
// @Summary      Call a tool on an MCP server
// @Description  Invoke a single tool on the addressed MCP server (builtin tools run in-process; external tools are proxied)
// @Tags         mcp-registry
// @Accept       json
// @Produce      json
// @Param        id path string true "Server ID"
// @Param        toolName path string true "Tool name"
// @Param        request body object false "Call arguments: {\"arguments\": {}}"
// @Success      200 {object} mcpregistry.APIResponse[mcp.ToolResult]
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Failure      409 {object} apperror.Error
// @Failure      502 {object} apperror.Error
// @Router       /api/admin/mcp-servers/{id}/tools/{toolName}/call [post]
// @Security     bearerAuth
func (h *Handler) CallToolOnServer(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	serverID := c.Param("id")
	if serverID == "" {
		return apperror.NewBadRequest("server ID is required")
	}
	toolName := c.Param("toolName")
	if toolName == "" {
		return apperror.NewBadRequest("tool name is required")
	}

	var req callToolRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Stamp the caller into the context so the proxy resolves this user's OAuth
	// credential (personal row first, shared fallback) when the server needs one.
	ctx := auth.ContextWithUser(c.Request().Context(), user)

	// The explicit *mcp.ToolResult declaration keeps the package import live so
	// swag resolves the @Success type (mcp.ToolResult) in the annotation above.
	var result *mcp.ToolResult
	result, err := h.svc.CallToolOnServer(ctx, user.ProjectID, serverID, toolName, req.Arguments, user.Scopes, user.APITokenID)
	if err != nil {
		switch {
		case errors.Is(err, ErrServerNotFound):
			return apperror.NewNotFound("mcp_server", serverID)
		case errors.Is(err, ErrToolNotFound):
			return apperror.NewNotFound("mcp_server_tool", toolName)
		case errors.Is(err, ErrServerDisabled):
			return apperror.New(http.StatusConflict, "mcp_server_disabled", "MCP server is disabled")
		case errors.Is(err, ErrToolDisabled):
			return apperror.New(http.StatusConflict, "mcp_tool_disabled", "MCP tool is disabled")
		case errors.Is(err, ErrToolForbidden):
			return apperror.New(http.StatusForbidden, "mcp_tool_forbidden", "not authorized to invoke this tool")
		case errors.Is(err, ErrUpstreamFailure):
			// The detail (stdio command lines / URLs) is logged server-side in the
			// service; return a sanitized message so it never leaks to the caller.
			return apperror.New(http.StatusBadGateway, "mcp_upstream_failure", "upstream MCP server call failed")
		default:
			return apperror.NewInternal("failed to call tool", err)
		}
	}

	return c.JSON(http.StatusOK, SuccessResponse(result))
}

// InspectServer handles POST /api/admin/mcp-servers/:id/inspect
//
// Performs a diagnostic test-connection to an MCP server. Creates a fresh
// ephemeral connection, captures the InitializeResult (server name, version,
// capabilities), and enumerates tools, prompts, and resources based on what
// the server advertises. Always returns 200 with the inspect result — connection
// errors are reported in the response body (status: "error"), not as HTTP errors.
func (h *Handler) InspectServer(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	serverID := c.Param("id")
	if serverID == "" {
		return apperror.NewBadRequest("server ID is required")
	}

	// Resolve the caller's OAuth credential (not the shared fallback) so inspect
	// reports this user's actual authorization state.
	ctx := auth.ContextWithUser(c.Request().Context(), user)

	result, err := h.svc.InspectServer(ctx, serverID, user.ProjectID)
	if err != nil {
		if errors.Is(err, ErrOutboundURLNotAllowed) {
			return apperror.NewBadRequest(err.Error())
		}
		if errors.Is(err, ErrServerNotInspectable) {
			return apperror.New(http.StatusConflict, "mcp_server_not_inspectable", err.Error())
		}
		return apperror.NewInternal("failed to inspect MCP server", err)
	}
	if result == nil {
		return apperror.NewNotFound("mcp_server", serverID)
	}

	return c.JSON(http.StatusOK, SuccessResponse(result))
}

// testServerConfigRequest binds a CreateMCPServerDTO plus an optional serverId
// for edit-mode backfill (a stored server's secret values are reused for
// required keys left blank in the draft).
type testServerConfigRequest struct {
	CreateMCPServerDTO
	ServerID *string `json:"serverId"`
}

// TestServerConfig handles POST /api/admin/mcp-servers/test
//
// Probes a draft (create or edit) server configuration without persisting it.
// When serverId is set, required keys left blank in the draft are backfilled
// from the stored server's encrypted secrets so an edit-mode test reuses the
// saved credential. Secrets are never persisted or logged; the result is the
// same MCPServerInspectDTO envelope as InspectServer.
func (h *Handler) TestServerConfig(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	var req testServerConfigRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Resolve the caller's OAuth credential for the edit-mode probe.
	ctx := auth.ContextWithUser(c.Request().Context(), user)

	result, err := h.svc.TestServerConfig(ctx, user.ProjectID, &req.CreateMCPServerDTO, req.ServerID)
	if err != nil {
		return apperror.NewBadRequest(err.Error())
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
	user := auth.MustGetUser(c)
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

	ctx := auth.ContextWithUser(c.Request().Context(), user)

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
			if err := h.svc.SyncServerTools(ctx, user.ProjectID, serverID, discoveredTools); err != nil {
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
// Built-in Tools Handlers
// ============================================================================

// ListBuiltinTools handles GET /api/admin/builtin-tools
//
// Returns all built-in (Go-native) tools registered for the current project,
// with each tool's enabled state, config keys, and resolved inheritance source
// ("project", "org", or "global"). Unlike GET /api/admin/mcp-servers this
// endpoint never surfaces the internal "builtin" MCPServer record — it exposes
// only the flat tool list.
func (h *Handler) ListBuiltinTools(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	dtos, err := h.svc.ListBuiltinToolsForProject(c.Request().Context(), user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to list built-in tools", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// UpdateBuiltinTool handles PATCH /api/admin/builtin-tools/:toolId
//
// Enables/disables a built-in tool or updates its runtime config for the
// current project. Accepts the same body as PATCH /api/admin/mcp-servers/:id/tools/:toolId.
func (h *Handler) UpdateBuiltinTool(c echo.Context) error {
	user := auth.MustGetUser(c)
	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	toolID := c.Param("toolId")
	if toolID == "" {
		return apperror.NewBadRequest("tool ID is required")
	}

	var dto UpdateMCPServerToolDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if dto.Enabled == nil && dto.Config == nil {
		return apperror.NewBadRequest("at least one of enabled or config must be provided")
	}

	// The tool is scoped to the caller's project; a foreign or missing tool is
	// 404 (issue #978), matching the sandboximages :id convention (#968/#974).
	if err := h.svc.UpdateTool(c.Request().Context(), user.ProjectID, toolID, dto.Enabled, dto.Config); err != nil {
		if errors.Is(err, ErrToolNotFound) {
			return apperror.NewNotFound("mcp_server_tool", toolID)
		}
		return apperror.NewBadRequest(err.Error())
	}

	msg := "tool updated"
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

	query := c.QueryParam("q")
	limitStr := c.QueryParam("limit")
	cursor := c.QueryParam("cursor")

	// limit <= 0 means "no cap": browse is served from the small embedded
	// catalog with no pagination, so an omitted limit must return the whole
	// curated set rather than a page-sized default.
	limit := 0
	if limitStr != "" {
		if _, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil {
			return apperror.NewBadRequest("invalid limit parameter")
		}
	}

	result, err := h.svc.SearchRegistry(c.Request().Context(), query, limit, cursor)
	if err != nil {
		return apperror.New(http.StatusBadGateway, "registry_unavailable", "failed to search MCP registry")
	}

	return c.JSON(http.StatusOK, SuccessResponse(result))
}

// GetRegistryServer handles GET /api/admin/mcp-registry/servers/:name
// The name parameter is the registry server name (URL-encoded, e.g. "io.github.github%2Fgithub-mcp-server").
func (h *Handler) GetRegistryServer(c echo.Context) error {

	raw := c.Param("name")
	if raw == "" {
		return apperror.NewBadRequest("server name is required")
	}
	// Echo's router stores the raw path segment without unescaping, so a
	// %2F-encoded name arrives still escaped. Decode it once before the exact
	// name lookup against the embedded catalog's literal slash-named entries
	// (e.g. "io.github.github/github-mcp-server"). Registry names never contain
	// a literal '%', so this is safe and idempotent.
	name, err := url.PathUnescape(raw)
	if err != nil {
		return apperror.NewBadRequest("invalid server name")
	}

	result, err := h.svc.GetRegistryServer(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, ErrRegistryServerNotFound) {
			return apperror.NewNotFound("registry_server", name)
		}
		return apperror.New(http.StatusBadGateway, "registry_unavailable", "failed to get registry server")
	}

	return c.JSON(http.StatusOK, SuccessResponse(result))
}

// InstallFromRegistry handles POST /api/admin/mcp-registry/install
func (h *Handler) InstallFromRegistry(c echo.Context) error {
	user := auth.MustGetUser(c)
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
		switch {
		case errors.Is(err, ErrRegistryServerNotFound):
			return apperror.NewNotFound("registry_server", dto.RegistryName)
		case errors.Is(err, ErrServerNameConflict):
			return apperror.New(http.StatusConflict, "conflict", err.Error())
		case errors.Is(err, ErrUnsupportedRegistryTransport), errors.Is(err, ErrRegistryRemoteNotAllowed):
			return apperror.NewBadRequest(err.Error())
		default:
			return apperror.NewInternal("failed to install from registry", err)
		}
	}

	return c.JSON(http.StatusCreated, SuccessResponse(result))
}
