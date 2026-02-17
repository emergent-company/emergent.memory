package workspace

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// MCPHostingHandler handles HTTP requests for MCP server hosting.
type MCPHostingHandler struct {
	hosting *MCPHostingService
	log     *slog.Logger
}

// NewMCPHostingHandler creates a new MCP hosting handler.
func NewMCPHostingHandler(hosting *MCPHostingService, log *slog.Logger) *MCPHostingHandler {
	return &MCPHostingHandler{
		hosting: hosting,
		log:     log.With("component", "mcp-hosting-handler"),
	}
}

// RegisterServer handles POST /api/v1/mcp/hosted
// @Summary      Register and start an MCP server
// @Description  Creates a persistent container for an MCP server with optional stdio bridge
// @Tags         mcp-hosting
// @Accept       json
// @Produce      json
// @Param        request body RegisterMCPServerRequest true "MCP server configuration"
// @Success      201 {object} MCPServerStatus "Registered MCP server"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/mcp/hosted [post]
// @Security     bearerAuth
func (h *MCPHostingHandler) RegisterServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var req RegisterMCPServerRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	status, err := h.hosting.Register(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, status)
}

// CallServer handles POST /api/v1/mcp/hosted/:id/call
// @Summary      Call an MCP method via stdio bridge
// @Description  Routes a JSON-RPC method call to the MCP server's stdin and returns the stdout response
// @Tags         mcp-hosting
// @Accept       json
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Param        request body MCPCallRequest true "MCP method call"
// @Success      200 {object} MCPCallResponse "MCP response"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "MCP server not found"
// @Router       /api/v1/mcp/hosted/{id}/call [post]
// @Security     bearerAuth
func (h *MCPHostingHandler) CallServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	var req MCPCallRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.Method == "" {
		return apperror.ErrBadRequest.WithMessage("method is required")
	}

	timeout := defaultCallTimeout
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}

	resp, err := h.hosting.Call(c.Request().Context(), id, req.Method, req.Params, timeout)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, MCPCallResponse{
		Result: resp.Result,
		Error:  resp.Error,
	})
}

// GetServer handles GET /api/v1/mcp/hosted/:id
// @Summary      Get MCP server status
// @Description  Returns the status, uptime, restart count, and configuration of a hosted MCP server
// @Tags         mcp-hosting
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Success      200 {object} MCPServerStatus "MCP server status"
// @Failure      400 {object} apperror.Error "Invalid workspace ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "MCP server not found"
// @Router       /api/v1/mcp/hosted/{id} [get]
// @Security     bearerAuth
func (h *MCPHostingHandler) GetServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	status, err := h.hosting.GetStatus(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, status)
}

// ListServers handles GET /api/v1/mcp/hosted
// @Summary      List all hosted MCP servers
// @Description  Returns all registered MCP servers with their current status
// @Tags         mcp-hosting
// @Produce      json
// @Success      200 {array} MCPServerStatus "List of MCP servers"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/v1/mcp/hosted [get]
// @Security     bearerAuth
func (h *MCPHostingHandler) ListServers(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	statuses, err := h.hosting.ListAll(c.Request().Context())
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, statuses)
}

// RemoveServer handles DELETE /api/v1/mcp/hosted/:id
// @Summary      Stop and remove an MCP server
// @Description  Stops the MCP server container, removes it, and deletes the workspace record
// @Tags         mcp-hosting
// @Param        id path string true "Workspace ID (UUID)"
// @Success      204 "No content"
// @Failure      400 {object} apperror.Error "Invalid workspace ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "MCP server not found"
// @Router       /api/v1/mcp/hosted/{id} [delete]
// @Security     bearerAuth
func (h *MCPHostingHandler) RemoveServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	if err := h.hosting.Remove(c.Request().Context(), id); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// RestartServer handles POST /api/v1/mcp/hosted/:id/restart
// @Summary      Restart an MCP server
// @Description  Gracefully restarts the MCP server container and re-establishes the stdio bridge
// @Tags         mcp-hosting
// @Produce      json
// @Param        id path string true "Workspace ID (UUID)"
// @Success      200 {object} MCPServerStatus "Restarted MCP server"
// @Failure      400 {object} apperror.Error "Invalid workspace ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "MCP server not found"
// @Router       /api/v1/mcp/hosted/{id}/restart [post]
// @Security     bearerAuth
func (h *MCPHostingHandler) RestartServer(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.ErrBadRequest.WithMessage("workspace id required")
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid workspace id format")
	}

	status, err := h.hosting.Restart(c.Request().Context(), id)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, status)
}

// RegisterMCPHostingRoutes registers MCP hosting HTTP routes.
func RegisterMCPHostingRoutes(e *echo.Echo, h *MCPHostingHandler, authMiddleware *auth.Middleware, log *slog.Logger) {
	g := e.Group("/api/v1/mcp/hosted")
	g.Use(authMiddleware.RequireAuth())

	// Read operations
	readGroup := g.Group("")
	readGroup.Use(authMiddleware.RequireScopes("admin:read"))
	readGroup.GET("", h.ListServers)
	readGroup.GET("/:id", h.GetServer)

	// Write operations
	writeGroup := g.Group("")
	writeGroup.Use(authMiddleware.RequireScopes("admin:write"))
	writeGroup.POST("", h.RegisterServer)
	writeGroup.POST("/:id/call", h.CallServer)
	writeGroup.POST("/:id/restart", h.RestartServer)
	writeGroup.DELETE("/:id", h.RemoveServer)

	log.Info("MCP hosting routes registered at /api/v1/mcp/hosted")
}
