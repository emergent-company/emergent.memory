package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"sync"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Handler handles MCP HTTP requests
type Handler struct {
	svc *Service
	log *slog.Logger

	// Session management (token -> session)
	sessions   map[string]*Session
	sessionsMu sync.RWMutex
}

// Session stores MCP session state
type Session struct {
	Initialized bool
	ProjectID   string
}

// NewHandler creates a new MCP handler
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{
		svc:      svc,
		log:      log.With(logger.Scope("mcp.handler")),
		sessions: make(map[string]*Session),
	}
}

// HandleRPC handles POST /mcp/rpc - JSON-RPC 2.0 endpoint (legacy)
func (h *Handler) HandleRPC(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// Parse JSON-RPC request
	var req Request
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusOK, NewErrorResponse(
			nil,
			ErrCodeParseError,
			"Failed to parse JSON-RPC request",
			map[string]string{"error": err.Error()},
		))
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		return c.JSON(http.StatusOK, NewErrorResponse(
			req.ID,
			ErrCodeInvalidRequest,
			"Invalid JSON-RPC version. Must be \"2.0\"",
			nil,
		))
	}

	// Handle notification (no ID = no response)
	if req.IsNotification() {
		h.handleNotification(c, &req)
		return c.NoContent(http.StatusAccepted)
	}

	// Route to method handler
	response := h.routeMethod(c, &req, user)
	return c.JSON(http.StatusOK, response)
}

// handleNotification processes JSON-RPC notifications
func (h *Handler) handleNotification(c echo.Context, req *Request) {
	switch req.Method {
	case "notifications/initialized":
		// Mark session as fully initialized
		token := extractToken(c)
		if token != "" {
			h.sessionsMu.Lock()
			if session, ok := h.sessions[token]; ok {
				session.Initialized = true
			}
			h.sessionsMu.Unlock()
		}
	default:
		// Unknown notification - ignore per JSON-RPC 2.0 spec
		h.log.Debug("unknown notification", slog.String("method", req.Method))
	}
}

// routeMethod routes JSON-RPC requests to the appropriate handler
func (h *Handler) routeMethod(c echo.Context, req *Request, user *auth.AuthUser) *Response {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(c, req, user)
	case "tools/list":
		return h.handleToolsList(c, req, user)
	case "tools/call":
		return h.handleToolsCall(c, req, user)
	default:
		return NewErrorResponse(
			req.ID,
			ErrCodeMethodNotFound,
			"Method not found: "+req.Method,
			map[string]any{
				"method":            req.Method,
				"supported_methods": []string{"initialize", "tools/list", "tools/call"},
			},
		)
	}
}

// handleInitialize handles the initialize method
func (h *Handler) handleInitialize(c echo.Context, req *Request, user *auth.AuthUser) *Response {
	// Parse params
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams,
				"Invalid initialize params", map[string]string{"error": err.Error()})
		}
	}

	// Validate required fields
	if params.ProtocolVersion == "" || params.ClientInfo.Name == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams,
			"Missing required parameters: protocolVersion, clientInfo",
			map[string]any{
				"required": []string{"protocolVersion", "clientInfo"},
			},
		)
	}

	// Validate protocol version
	if !slices.Contains(SupportedProtocolVersions, params.ProtocolVersion) {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams,
			"Unsupported protocol version: "+params.ProtocolVersion,
			map[string]any{
				"requested": params.ProtocolVersion,
				"supported": SupportedProtocolVersions,
			},
		)
	}

	// Create/update session
	token := extractToken(c)
	projectID := params.ProjectID
	if projectID == "" {
		projectID = user.ProjectID // Fall back to header
	}

	if token != "" {
		h.sessionsMu.Lock()
		h.sessions[token] = &Session{
			Initialized: true,
			ProjectID:   projectID,
		}
		h.sessionsMu.Unlock()
	}

	// Build response
	result := InitializeResult{
		ProtocolVersion: params.ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: ToolsCapability{ListChanged: false},
		},
		ServerInfo: ServerInfo,
	}

	if projectID != "" {
		result.ProjectContext = map[string]string{"projectId": projectID}
	}

	h.log.Info("MCP session initialized",
		slog.String("client", params.ClientInfo.Name),
		slog.String("version", params.ClientInfo.Version),
		slog.String("project_id", projectID),
	)

	return NewSuccessResponse(req.ID, result)
}

// handleToolsList handles the tools/list method
func (h *Handler) handleToolsList(c echo.Context, req *Request, user *auth.AuthUser) *Response {
	// Check session initialization
	token := extractToken(c)
	session := h.getSession(token)
	if session == nil || !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before tools/list",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}

	tools := h.svc.GetToolDefinitions()
	return NewSuccessResponse(req.ID, ToolsListResult{Tools: tools})
}

// handleToolsCall handles the tools/call method
func (h *Handler) handleToolsCall(c echo.Context, req *Request, user *auth.AuthUser) *Response {
	// Check session initialization
	token := extractToken(c)
	session := h.getSession(token)
	if session == nil || !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before tools/call",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}

	// Parse params
	var params ToolsCallParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams,
				"Invalid tools/call params", map[string]string{"error": err.Error()})
		}
	}

	if params.Name == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams,
			"Missing required parameter: name",
			map[string]any{"required": []string{"name"}},
		)
	}

	// Get project ID from session or user context
	projectID := session.ProjectID
	if projectID == "" {
		projectID = user.ProjectID
	}

	if projectID == "" {
		// Some tools require project ID
		if requiresProject(params.Name) {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams,
				"Project ID is required. Provide project_id in initialize params or X-Project-Id header.",
				map[string]string{"hint": "Call initialize with project_id parameter or set X-Project-Id header"},
			)
		}
	}

	// Execute tool
	result, err := h.svc.ExecuteTool(c.Request().Context(), projectID, params.Name, params.Arguments)
	if err != nil {
		h.log.Error("tool execution failed",
			slog.String("tool", params.Name),
			logger.Error(err),
		)
		return NewErrorResponse(req.ID, ErrCodeInternalError,
			"Tool execution failed: "+err.Error(),
			nil,
		)
	}

	return NewSuccessResponse(req.ID, result)
}

// getSession returns the session for a token
func (h *Handler) getSession(token string) *Session {
	if token == "" {
		return nil
	}
	h.sessionsMu.RLock()
	defer h.sessionsMu.RUnlock()
	return h.sessions[token]
}

// extractToken extracts bearer token from request
func extractToken(c echo.Context) string {
	authHeader := c.Request().Header.Get("Authorization")
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		return authHeader[7:]
	}
	return ""
}

// requiresProject returns true if the tool requires a project context
func requiresProject(toolName string) bool {
	switch toolName {
	case "list_entity_types", "query_entities", "search_entities", "get_entity_edges",
		"get_available_templates", "get_installed_templates",
		"assign_template_pack", "update_template_assignment", "uninstall_template_pack":
		return true
	default:
		return false
	}
}
