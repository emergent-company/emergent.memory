package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/emergent/emergent-core/pkg/logger"
)

// StreamableHTTPHandler implements MCP Streamable HTTP transport (spec 2025-11-25)
// Single endpoint that handles both POST and GET requests with SSE support
type StreamableHTTPHandler struct {
	svc *Service
	log *slog.Logger

	// Session management (session ID -> session state)
	sessions   map[string]*MCPSession
	sessionsMu sync.RWMutex

	// SSE streams (session ID -> list of active streams)
	streams   map[string][]*SSEStream
	streamsMu sync.RWMutex

	// Event storage for resumability (Last-Event-ID support)
	eventStore *EventStore
}

// MCPSession represents an MCP session state
type MCPSession struct {
	ID              string
	ProjectID       string
	UserID          string
	OrgID           string
	Initialized     bool
	ProtocolVersion string
	CreatedAt       time.Time
	LastAccessAt    time.Time
}

// SSEStream represents an active SSE connection
type SSEStream struct {
	ID          string
	SessionID   string
	Writer      http.ResponseWriter
	Flusher     http.Flusher
	Done        chan struct{}
	LastEventID int64
	CreatedAt   time.Time
}

// NewStreamableHTTPHandler creates a new spec-compliant MCP handler
func NewStreamableHTTPHandler(svc *Service, log *slog.Logger) *StreamableHTTPHandler {
	return &StreamableHTTPHandler{
		svc:        svc,
		log:        log.With(logger.Scope("mcp.streamable")),
		sessions:   make(map[string]*MCPSession),
		streams:    make(map[string][]*SSEStream),
		eventStore: NewEventStore(100),
	}
}

// HandleUnifiedEndpoint handles both GET and POST requests on the MCP endpoint
// Implements the Streamable HTTP transport specification
func (h *StreamableHTTPHandler) HandleUnifiedEndpoint(c echo.Context) error {
	// Validate and extract protocol version
	protocolVersion := c.Request().Header.Get("MCP-Protocol-Version")
	if protocolVersion == "" {
		// Backward compatibility: assume 2025-03-26 if not specified
		protocolVersion = "2025-03-26"
		h.log.Debug("no protocol version header, assuming 2025-03-26")
	}

	// Validate protocol version
	if !h.isValidProtocolVersion(protocolVersion) {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error": map[string]any{
				"code":      "invalid_protocol_version",
				"message":   fmt.Sprintf("Unsupported protocol version: %s", protocolVersion),
				"supported": SupportedProtocolVersions,
			},
		})
	}

	switch c.Request().Method {
	case http.MethodPost:
		return h.handlePOST(c, protocolVersion)
	case http.MethodGet:
		return h.handleGET(c, protocolVersion)
	case http.MethodDelete:
		return h.handleDELETE(c)
	default:
		return c.JSON(http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]string{
				"code":    "method_not_allowed",
				"message": "Only POST, GET, and DELETE methods are supported",
			},
		})
	}
}

// handlePOST handles POST requests (send messages to server)
func (h *StreamableHTTPHandler) handlePOST(c echo.Context, protocolVersion string) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// Check Accept header
	acceptHeader := c.Request().Header.Get("Accept")
	supportsSSE := strings.Contains(acceptHeader, "text/event-stream")
	supportsJSON := strings.Contains(acceptHeader, "application/json")

	if !supportsSSE && !supportsJSON {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"code":    "invalid_accept_header",
				"message": "Accept header must include application/json or text/event-stream",
			},
		})
	}

	// Parse JSON-RPC request
	var req Request
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, NewErrorResponse(
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

	// Get or validate session
	sessionID := c.Request().Header.Get("Mcp-Session-Id")
	session, err := h.getOrCreateSession(sessionID, user, protocolVersion)
	if err != nil {
		if err.Error() == "session_not_found" {
			return c.JSON(http.StatusNotFound, map[string]any{
				"error": map[string]string{
					"code":    "session_not_found",
					"message": "Session not found or expired. Please reinitialize.",
				},
			})
		}
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"code":    "session_error",
				"message": err.Error(),
			},
		})
	}

	// Handle notification (no response expected)
	if req.IsNotification() {
		h.handleNotification(c, &req, session)
		return c.NoContent(http.StatusAccepted)
	}

	// Handle JSON-RPC response (client responding to server request)
	if req.ID == nil && req.Method == "" {
		// This is a response, not a request
		return c.NoContent(http.StatusAccepted)
	}

	// Handle JSON-RPC request
	response := h.processRequest(c, &req, session, user)

	// For initialize request, set session ID header
	if req.Method == "initialize" && response.Error == nil {
		c.Response().Header().Set("Mcp-Session-Id", session.ID)
	}

	// Decide whether to stream or return JSON
	// For now, return JSON directly (simpler, SSE streaming can be added later if needed)
	return c.JSON(http.StatusOK, response)
}

// handleGET handles GET requests (open SSE stream for server-initiated messages)
func (h *StreamableHTTPHandler) handleGET(c echo.Context, protocolVersion string) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// Check Accept header
	acceptHeader := c.Request().Header.Get("Accept")
	if !strings.Contains(acceptHeader, "text/event-stream") {
		return c.JSON(http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]string{
				"code":    "sse_not_supported",
				"message": "Server-initiated messages via GET require Accept: text/event-stream",
			},
		})
	}

	// Get session
	sessionID := c.Request().Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"code":    "session_required",
				"message": "Mcp-Session-Id header is required for GET requests",
			},
		})
	}

	session := h.getSession(sessionID)
	if session == nil {
		return c.JSON(http.StatusNotFound, map[string]any{
			"error": map[string]string{
				"code":    "session_not_found",
				"message": "Session not found or expired",
			},
		})
	}

	// Set up SSE stream
	w := c.Response().Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"error": map[string]string{
				"code":    "streaming_not_supported",
				"message": "Server does not support streaming",
			},
		})
	}

	// Create SSE stream
	stream := &SSEStream{
		ID:          uuid.New().String(),
		SessionID:   sessionID,
		Writer:      w,
		Flusher:     flusher,
		Done:        make(chan struct{}),
		LastEventID: -1,
		CreatedAt:   time.Now(),
	}

	// Check for Last-Event-ID header for resumability (MCP spec 2025-11-25)
	lastEventIDStr := c.Request().Header.Get("Last-Event-ID")
	if lastEventIDStr != "" {
		if lastEventID, err := strconv.ParseInt(lastEventIDStr, 10, 64); err == nil {
			stream.LastEventID = lastEventID
			h.log.Debug("resuming stream from event",
				slog.String("session_id", sessionID),
				slog.Int64("last_event_id", lastEventID),
			)
		} else {
			h.log.Warn("invalid Last-Event-ID header",
				slog.String("value", lastEventIDStr),
				logger.Error(err),
			)
		}
	}

	// Register stream
	h.streamsMu.Lock()
	h.streams[sessionID] = append(h.streams[sessionID], stream)
	h.streamsMu.Unlock()

	defer func() {
		h.streamsMu.Lock()
		streams := h.streams[sessionID]
		for i, s := range streams {
			if s.ID == stream.ID {
				h.streams[sessionID] = append(streams[:i], streams[i+1:]...)
				break
			}
		}
		h.streamsMu.Unlock()
		close(stream.Done)
	}()

	h.log.Info("SSE stream opened",
		slog.String("session_id", sessionID),
		slog.String("stream_id", stream.ID),
		slog.Int64("from_event_id", stream.LastEventID),
	)

	// Send priming event (MCP spec requirement: establishes event ID sequence)
	primingEventID := h.eventStore.GetNextEventID(sessionID)
	fmt.Fprintf(stream.Writer, "id: %d\ndata: \n\n", primingEventID)
	stream.Flusher.Flush()
	stream.LastEventID = primingEventID

	// If resuming, replay missed events
	if lastEventIDStr != "" {
		missedEvents := h.eventStore.GetEventsSince(sessionID, stream.LastEventID-1)
		for _, event := range missedEvents {
			fmt.Fprintf(stream.Writer, "event: %s\nid: %d\ndata: %s\n\n",
				event.EventType, event.ID, event.Data)
			stream.Flusher.Flush()
			stream.LastEventID = event.ID
		}
		if len(missedEvents) > 0 {
			h.log.Info("replayed missed events",
				slog.String("session_id", sessionID),
				slog.Int("count", len(missedEvents)),
			)
		}
	}

	// Keep connection alive with spec-compliant keepalive comments
	ticker := time.NewTicker(4 * time.Hour)
	defer ticker.Stop()

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			h.log.Info("SSE stream closed (client disconnected)",
				slog.String("session_id", sessionID),
				slog.String("stream_id", stream.ID),
			)
			return nil
		case <-ticker.C:
			fmt.Fprintf(stream.Writer, ": keepalive\n\n")
			stream.Flusher.Flush()
		}
	}
}

// handleDELETE handles DELETE requests (terminate session)
func (h *StreamableHTTPHandler) handleDELETE(c echo.Context) error {
	sessionID := c.Request().Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"code":    "session_required",
				"message": "Mcp-Session-Id header is required",
			},
		})
	}

	h.sessionsMu.Lock()
	delete(h.sessions, sessionID)
	h.sessionsMu.Unlock()

	h.streamsMu.Lock()
	streams := h.streams[sessionID]
	for _, stream := range streams {
		close(stream.Done)
	}
	delete(h.streams, sessionID)
	h.streamsMu.Unlock()

	h.eventStore.ClearSession(sessionID)

	h.log.Info("session terminated", slog.String("session_id", sessionID))

	return c.NoContent(http.StatusNoContent)
}

// getOrCreateSession gets existing session or creates new one
func (h *StreamableHTTPHandler) getOrCreateSession(sessionID string, user *auth.AuthUser, protocolVersion string) (*MCPSession, error) {
	if sessionID != "" {
		// Validate existing session
		h.sessionsMu.RLock()
		session, exists := h.sessions[sessionID]
		h.sessionsMu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("session_not_found")
		}

		// Update last access time
		h.sessionsMu.Lock()
		session.LastAccessAt = time.Now()
		h.sessionsMu.Unlock()

		return session, nil
	}

	// Create new session (will be assigned ID during initialize)
	session := &MCPSession{
		ID:              uuid.New().String(),
		ProjectID:       user.ProjectID,
		UserID:          user.ID,
		OrgID:           user.OrgID,
		Initialized:     false,
		ProtocolVersion: protocolVersion,
		CreatedAt:       time.Now(),
		LastAccessAt:    time.Now(),
	}

	return session, nil
}

// getSession retrieves an existing session
func (h *StreamableHTTPHandler) getSession(sessionID string) *MCPSession {
	h.sessionsMu.RLock()
	defer h.sessionsMu.RUnlock()
	return h.sessions[sessionID]
}

// processRequest processes JSON-RPC requests
func (h *StreamableHTTPHandler) processRequest(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser) *Response {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(c, req, session)
	case "tools/list":
		return h.handleToolsList(c, req, session)
	case "tools/call":
		return h.handleToolsCall(c, req, session, user)
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

// handleInitialize handles initialize method
func (h *StreamableHTTPHandler) handleInitialize(c echo.Context, req *Request, session *MCPSession) *Response {
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
	if !h.isValidProtocolVersion(params.ProtocolVersion) {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams,
			"Unsupported protocol version: "+params.ProtocolVersion,
			map[string]any{
				"requested": params.ProtocolVersion,
				"supported": SupportedProtocolVersions,
			},
		)
	}

	// Update session
	session.Initialized = true
	session.ProtocolVersion = params.ProtocolVersion
	if params.ProjectID != "" {
		session.ProjectID = params.ProjectID
	}

	// Store session
	h.sessionsMu.Lock()
	h.sessions[session.ID] = session
	h.sessionsMu.Unlock()

	// Build response
	result := InitializeResult{
		ProtocolVersion: params.ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools:     ToolsCapability{ListChanged: false},
			Resources: ResourcesCapability{Subscribe: false, ListChanged: false},
			Prompts:   PromptsCapability{ListChanged: false},
		},
		ServerInfo: ServerInfo,
	}

	if session.ProjectID != "" {
		result.ProjectContext = map[string]string{"projectId": session.ProjectID}
	}

	h.log.Info("MCP session initialized",
		slog.String("session_id", session.ID),
		slog.String("client", params.ClientInfo.Name),
		slog.String("version", params.ClientInfo.Version),
		slog.String("protocol", params.ProtocolVersion),
		slog.String("project_id", session.ProjectID),
	)

	return NewSuccessResponse(req.ID, result)
}

// handleToolsList handles tools/list method
func (h *StreamableHTTPHandler) handleToolsList(c echo.Context, req *Request, session *MCPSession) *Response {
	if !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before tools/list",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}

	tools := h.svc.GetToolDefinitions()
	return NewSuccessResponse(req.ID, ToolsListResult{Tools: tools})
}

// handleToolsCall handles tools/call method
func (h *StreamableHTTPHandler) handleToolsCall(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser) *Response {
	if !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before tools/call",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}

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

	projectID := session.ProjectID
	if projectID == "" {
		projectID = user.ProjectID
	}

	if projectID == "" && requiresProject(params.Name) {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams,
			"Project ID is required. Provide project_id in initialize params or X-Project-Id header.",
			map[string]string{"hint": "Call initialize with project_id parameter or set X-Project-Id header"},
		)
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

// handleNotification handles JSON-RPC notifications
func (h *StreamableHTTPHandler) handleNotification(c echo.Context, req *Request, session *MCPSession) {
	switch req.Method {
	case "notifications/initialized":
		session.Initialized = true
		h.log.Debug("client sent initialized notification", slog.String("session_id", session.ID))
	default:
		h.log.Debug("unknown notification", slog.String("method", req.Method))
	}
}

// isValidProtocolVersion checks if protocol version is supported
func (h *StreamableHTTPHandler) isValidProtocolVersion(version string) bool {
	for _, v := range SupportedProtocolVersions {
		if v == version {
			return true
		}
	}
	return false
}

// SendServerMessage sends a JSON-RPC message to all active SSE streams for a session
func (h *StreamableHTTPHandler) SendServerMessage(sessionID string, message *Response) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	eventID := h.eventStore.AddEvent(sessionID, data)

	h.streamsMu.RLock()
	streams := h.streams[sessionID]
	h.streamsMu.RUnlock()

	for _, stream := range streams {
		fmt.Fprintf(stream.Writer, "event: message\nid: %d\ndata: %s\n\n", eventID, data)
		stream.Flusher.Flush()
		stream.LastEventID = eventID
	}

	h.log.Debug("server message sent",
		slog.String("session_id", sessionID),
		slog.Int64("event_id", eventID),
		slog.Int("stream_count", len(streams)),
	)

	return nil
}
