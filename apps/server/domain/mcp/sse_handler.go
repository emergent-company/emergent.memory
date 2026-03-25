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

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// SSEHandler handles MCP SSE transport
type SSEHandler struct {
	svc     *Service
	handler *Handler
	log     *slog.Logger

	// SSE sessions — active streaming connections (keyed by session ID)
	sseSessions   map[string]*SSESession
	sseSessionsMu sync.RWMutex

	// sessionState tracks per-session initialization state independently of the
	// SSE stream. This allows tools/list and tools/call to work even after an
	// SSE reconnect, as long as initialize was called at least once.
	sessionState   map[string]*sessionInitState
	sessionStateMu sync.RWMutex
}

// sessionInitState holds durable per-session state that survives SSE reconnects.
type sessionInitState struct {
	ProjectID   string
	UserID      string
	Initialized bool
	CreatedAt   time.Time
}

// SSESession represents an active SSE streaming connection
type SSESession struct {
	ID        string
	ProjectID string
	UserID    string
	Done      chan struct{}
	Writer    http.ResponseWriter
	Flusher   http.Flusher
}

// NewSSEHandler creates a new SSE handler
func NewSSEHandler(svc *Service, handler *Handler, log *slog.Logger) *SSEHandler {
	return &SSEHandler{
		svc:          svc,
		handler:      handler,
		log:          log.With(logger.Scope("mcp.sse")),
		sseSessions:  make(map[string]*SSESession),
		sessionState: make(map[string]*sessionInitState),
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
		ID:        sessionID,
		ProjectID: projectID,
		UserID:    user.ID,
		Done:      make(chan struct{}),
		Writer:    w,
		Flusher:   flusher,
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
	messageEndpoint := fmt.Sprintf("/api/mcp/sse/%s/message?sessionId=%s", projectID, sessionID)
	h.sendSSEEvent(session, "endpoint", messageEndpoint)

	// Keep connection alive with periodic pings (every 4 hours, well under 8 hour timeout)
	ticker := time.NewTicker(4 * time.Hour)
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
		return c.JSON(http.StatusBadRequest, NewErrorResponse(nil, ErrCodeParseError, "Failed to parse JSON-RPC request", nil))
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		resp := NewErrorResponse(req.ID, ErrCodeInvalidRequest, "Invalid JSON-RPC version", nil)
		return h.sendResponseToSession(c, sessionID, resp)
	}

	// Process request
	response := h.processRequest(c, &req, projectID, user, sessionID)

	// Notifications return nil — no response per JSON-RPC 2.0 spec
	if response == nil {
		return c.NoContent(http.StatusAccepted)
	}

	// Also push response via SSE stream if the client has an active connection.
	// This is optional — clients that only use the HTTP response body don't need it.
	if sessionID != "" {
		h.sseSessionsMu.RLock()
		session, ok := h.sseSessions[sessionID]
		h.sseSessionsMu.RUnlock()

		if ok && session != nil {
			jsonBytes, _ := json.Marshal(response)
			h.sendSSEEvent(session, "message", string(jsonBytes))
		}
	}

	// Return the JSON-RPC response directly in the HTTP body with 200 OK.
	// OpenCode (and the MCP SSE spec) expects the response in the POST body.
	return c.JSON(http.StatusOK, response)
}

// processRequest processes a JSON-RPC request for SSE transport.
// sessionID is the ?sessionId query param (may be empty for sessionless clients).
func (h *SSEHandler) processRequest(c echo.Context, req *Request, projectID string, user *auth.AuthUser, sessionID string) *Response {
	// Look up durable session state (survives SSE reconnects)
	h.sessionStateMu.RLock()
	state, stateExists := h.sessionState[sessionID]
	h.sessionStateMu.RUnlock()

	initialized := stateExists && state.Initialized

	// Handle notifications (no id = no response expected per JSON-RPC 2.0)
	if req.IsNotification() {
		if req.Method == "notifications/initialized" && sessionID != "" {
			h.sessionStateMu.Lock()
			if h.sessionState[sessionID] == nil {
				h.sessionState[sessionID] = &sessionInitState{
					ProjectID: projectID,
					UserID:    user.ID,
					CreatedAt: time.Now().UTC(),
				}
			}
			h.sessionState[sessionID].Initialized = true
			h.sessionStateMu.Unlock()
		}
		return nil
	}

	switch req.Method {
	case "initialize":
		// Mark session as initialized in the durable state map
		if sessionID != "" {
			h.sessionStateMu.Lock()
			if h.sessionState[sessionID] == nil {
				h.sessionState[sessionID] = &sessionInitState{
					ProjectID: projectID,
					UserID:    user.ID,
					CreatedAt: time.Now().UTC(),
				}
			}
			h.sessionState[sessionID].Initialized = true
			h.sessionStateMu.Unlock()
		}
		return h.handleInitialize(req, projectID)

	case "tools/list":
		if !initialized {
			return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
				"Client must call initialize before tools/list",
				map[string]string{"hint": "Call initialize method first to establish session"},
			)
		}
		return h.handleToolsList(req, user.Scopes)

	case "tools/call":
		if !initialized {
			return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
				"Client must call initialize before tools/call",
				map[string]string{"hint": "Call initialize method first to establish session"},
			)
		}
		return h.handleToolsCall(c, req, projectID, user)

	default:
		return h.handler.routeMethod(c, req, user)
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
			Tools:     ToolsCapability{ListChanged: false},
			Resources: ResourcesCapability{Subscribe: false, ListChanged: false},
			Prompts:   PromptsCapability{ListChanged: false},
		},
		ServerInfo:     ServerInfo,
		ProjectContext: map[string]string{"projectId": projectID},
	}

	return NewSuccessResponse(req.ID, result)
}

// handleToolsList handles tools/list for SSE transport
func (h *SSEHandler) handleToolsList(req *Request, scopes []string) *Response {
	tools := h.svc.GetToolDefinitions()
	tools = FilterToolsForScopes(tools, scopes)
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
