package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/emergent/emergent-core/pkg/logger"
)

// SSEHandler handles MCP SSE transport
type SSEHandler struct {
	svc *Service
	log *slog.Logger

	// SSE sessions
	sseSessions   map[string]*SSESession
	sseSessionsMu sync.RWMutex
}

// SSESession represents an SSE connection session
type SSESession struct {
	ID          string
	ProjectID   string
	UserID      string
	Initialized bool
	Done        chan struct{}
	Writer      http.ResponseWriter
	Flusher     http.Flusher
}

// NewSSEHandler creates a new SSE handler
func NewSSEHandler(svc *Service, log *slog.Logger) *SSEHandler {
	return &SSEHandler{
		svc:         svc,
		log:         log.With(logger.Scope("mcp.sse")),
		sseSessions: make(map[string]*SSESession),
	}
}

// HandleSSEConnect handles GET /mcp/sse/:projectId - SSE connection endpoint
func (h *SSEHandler) HandleSSEConnect(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project ID")
	}

	// Generate session ID
	sessionID := h.generateSessionID()

	// Set up SSE headers
	w := c.Response().Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return apperror.ErrInternal.WithMessage("streaming not supported")
	}

	// Create session
	session := &SSESession{
		ID:          sessionID,
		ProjectID:   projectID,
		UserID:      user.ID,
		Initialized: false,
		Done:        make(chan struct{}),
		Writer:      w,
		Flusher:     flusher,
	}

	h.sseSessionsMu.Lock()
	h.sseSessions[sessionID] = session
	h.sseSessionsMu.Unlock()

	defer func() {
		h.sseSessionsMu.Lock()
		delete(h.sseSessions, sessionID)
		h.sseSessionsMu.Unlock()
		close(session.Done)
	}()

	h.log.Info("SSE session started",
		slog.String("session_id", sessionID),
		slog.String("project_id", projectID),
		slog.String("user_id", user.ID),
	)

	// Send endpoint event (tells client where to POST messages)
	messageEndpoint := fmt.Sprintf("/mcp/sse/%s/message?sessionId=%s", projectID, sessionID)
	h.sendSSEEvent(session, "endpoint", messageEndpoint)

	// Keep connection alive with periodic pings
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Wait for disconnect
	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			h.log.Info("SSE session ended (client disconnected)",
				slog.String("session_id", sessionID))
			return nil
		case <-ticker.C:
			h.sendSSEEvent(session, "ping", time.Now().UTC().Format(time.RFC3339))
		}
	}
}

// HandleSSEMessage handles POST /mcp/sse/:projectId/message - Send messages to MCP server
func (h *SSEHandler) HandleSSEMessage(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project ID")
	}

	sessionID := c.QueryParam("sessionId")

	// Parse JSON-RPC request
	var req Request
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"code":    "parse_error",
				"message": "Failed to parse JSON-RPC request",
			},
		})
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		resp := NewErrorResponse(req.ID, ErrCodeInvalidRequest, "Invalid JSON-RPC version", nil)
		return h.sendResponseToSession(c, sessionID, resp)
	}

	// Process request
	response := h.processRequest(c, &req, projectID, user)

	// Send response via SSE if session exists
	if sessionID != "" {
		h.sseSessionsMu.RLock()
		session, ok := h.sseSessions[sessionID]
		h.sseSessionsMu.RUnlock()

		if ok && session != nil {
			jsonBytes, _ := json.Marshal(response)
			h.sendSSEEvent(session, "message", string(jsonBytes))
		}
	}

	// Also return response directly (for clients that prefer HTTP response)
	return c.JSON(http.StatusAccepted, map[string]any{
		"status":  "accepted",
		"jsonrpc": response.JSONRPC,
		"id":      response.ID,
		"result":  response.Result,
		"error":   response.Error,
	})
}

// processRequest processes a JSON-RPC request for SSE transport
func (h *SSEHandler) processRequest(c echo.Context, req *Request, projectID string, user *auth.AuthUser) *Response {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(req, projectID)
	case "tools/list":
		return h.handleToolsList(req)
	case "tools/call":
		return h.handleToolsCall(c, req, projectID, user)
	case "notifications/initialized":
		return NewSuccessResponse(req.ID, map[string]bool{"acknowledged": true})
	default:
		return NewErrorResponse(req.ID, ErrCodeMethodNotFound, "Method not found: "+req.Method, nil)
	}
}

// handleInitialize handles initialize for SSE transport
func (h *SSEHandler) handleInitialize(req *Request, projectID string) *Response {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", nil)
		}
	}

	if params.ProtocolVersion == "" || params.ClientInfo.Name == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams,
			"Missing required parameters: protocolVersion, clientInfo", nil)
	}

	// Validate protocol version
	valid := false
	for _, v := range SupportedProtocolVersions {
		if v == params.ProtocolVersion {
			valid = true
			break
		}
	}
	if !valid {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams,
			"Unsupported protocol version: "+params.ProtocolVersion,
			map[string]any{"supported": SupportedProtocolVersions})
	}

	result := InitializeResult{
		ProtocolVersion: params.ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: ToolsCapability{ListChanged: false},
		},
		ServerInfo:     ServerInfo,
		ProjectContext: map[string]string{"projectId": projectID},
	}

	return NewSuccessResponse(req.ID, result)
}

// handleToolsList handles tools/list for SSE transport
func (h *SSEHandler) handleToolsList(req *Request) *Response {
	tools := h.svc.GetToolDefinitions()
	return NewSuccessResponse(req.ID, ToolsListResult{Tools: tools})
}

// handleToolsCall handles tools/call for SSE transport
func (h *SSEHandler) handleToolsCall(c echo.Context, req *Request, projectID string, user *auth.AuthUser) *Response {
	var params ToolsCallParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", nil)
		}
	}

	if params.Name == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required parameter: name", nil)
	}

	result, err := h.svc.ExecuteTool(c.Request().Context(), projectID, params.Name, params.Arguments)
	if err != nil {
		h.log.Error("tool execution failed",
			slog.String("tool", params.Name),
			logger.Error(err),
		)
		return NewErrorResponse(req.ID, ErrCodeInternalError, "Tool execution failed: "+err.Error(), nil)
	}

	return NewSuccessResponse(req.ID, result)
}

// sendSSEEvent sends an SSE event to a session
func (h *SSEHandler) sendSSEEvent(session *SSESession, event, data string) {
	select {
	case <-session.Done:
		return
	default:
	}

	fmt.Fprintf(session.Writer, "event: %s\n", event)
	fmt.Fprintf(session.Writer, "data: %s\n\n", data)
	session.Flusher.Flush()
}

// sendResponseToSession sends a response via SSE or HTTP
func (h *SSEHandler) sendResponseToSession(c echo.Context, sessionID string, resp *Response) error {
	if sessionID != "" {
		h.sseSessionsMu.RLock()
		session, ok := h.sseSessions[sessionID]
		h.sseSessionsMu.RUnlock()

		if ok && session != nil {
			jsonBytes, _ := json.Marshal(resp)
			h.sendSSEEvent(session, "message", string(jsonBytes))
		}
	}
	return c.JSON(http.StatusOK, resp)
}

// generateSessionID creates a unique session ID
func (h *SSEHandler) generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("mcp_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(bytes)[:12])
}
