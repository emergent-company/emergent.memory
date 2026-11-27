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
	ProjectID string
	UserID    string
	// APITokenID is the credential that created the session ("" for
	// OIDC/session auth). The session ID is a lookup key, so the state is
	// authorized against the current caller on every use.
	APITokenID  string
	Initialized bool
	// ClientName is the sanitized clientInfo.name from initialize, carried into
	// tools/call so tools can name the real client (issue #472).
	ClientName string
	CreatedAt  time.Time
}

// SSESession represents an active SSE streaming connection
type SSESession struct {
	ID         string
	ProjectID  string
	UserID     string
	APITokenID string
	Done       chan struct{}
	Writer     http.ResponseWriter
	Flusher    http.Flusher
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

// lookupSSEState returns a copy of a session's durable state, read under the
// lock, and whether it existed. Callers must use the copy rather than holding a
// pointer into the map: the initialize and notifications/initialized paths write
// Initialized and ClientName under the write lock, so reading a stored pointer
// after the lock is released races those writes.
func (h *SSEHandler) lookupSSEState(sessionID string) (*sessionInitState, bool) {
	h.sessionStateMu.RLock()
	defer h.sessionStateMu.RUnlock()
	stored := h.sessionState[sessionID]
	if stored == nil {
		return nil, false
	}
	snapshot := *stored
	return &snapshot, true
}

// lookupSSESession returns the durable state and the live stream for a session
// ID. Either may be nil (a session may have state but no open stream, or a
// stream that has not initialized yet). The state is returned as a copy read
// under the lock: the caller must judge the session's binding, and must not
// observe fields changing underneath it.
func (h *SSEHandler) lookupSSESession(sessionID string) (*sessionInitState, *SSESession) {
	if sessionID == "" {
		return nil, nil
	}
	state, _ := h.lookupSSEState(sessionID)

	h.sseSessionsMu.RLock()
	stream := h.sseSessions[sessionID]
	h.sseSessionsMu.RUnlock()
	return state, stream
}

// authorizeSSESession reports whether the authenticated caller may use a
// session ID minted by this transport. The ID is a lookup key, never a
// credential: the creating user, the creating credential (when either side is
// API-token authenticated) and the addressed project must all match. A session
// ID that has neither state nor a stream is not owned by anyone yet and is
// allowed — the initialize flow binds it to the caller.
func authorizeSSESession(projectID string, user *auth.AuthUser, state *sessionInitState, stream *SSESession) error {
	// An absent caller has no identity to bind a session to. auth.RequireUser
	// treats an empty id the same way, and both call sites come through
	// MustGetUser, so this is a fail-closed contract rather than a live path.
	callerID, callerToken := "", ""
	if user != nil {
		callerID, callerToken = user.ID, user.APITokenID
	}
	if callerID == "" {
		return apperror.ErrUnauthorized
	}

	check := func(sessionProject, sessionUser, sessionToken string) error {
		if sessionProject != "" && sessionProject != projectID {
			return apperror.NewForbidden("MCP session does not belong to this project")
		}
		if sessionUser != callerID {
			return apperror.NewForbidden("MCP session does not belong to this caller")
		}
		if sessionToken != callerToken && (sessionToken != "" || callerToken != "") {
			return apperror.NewForbidden("MCP session does not belong to this caller")
		}
		return nil
	}
	if state != nil {
		if err := check(state.ProjectID, state.UserID, state.APITokenID); err != nil {
			return err
		}
	}
	if stream != nil {
		if err := check(stream.ProjectID, stream.UserID, stream.APITokenID); err != nil {
			return err
		}
	}
	return nil
}

// HandleSSEConnect handles GET /mcp/sse/:projectId - SSE connection endpoint
func (h *SSEHandler) HandleSSEConnect(c echo.Context) error {
	user := auth.MustGetUser(c)

	// Fail closed: per-agent share credentials are not valid on the project MCP
	// SSE transport.
	if hasAgentCallScope(user.Scopes) {
		return apperror.NewForbidden("agent-share credentials are not valid on the project MCP endpoint")
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
		ID:         sessionID,
		ProjectID:  projectID,
		UserID:     user.ID,
		APITokenID: user.APITokenID,
		Done:       make(chan struct{}),
		Writer:     w,
		Flusher:    flusher,
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
	user := auth.MustGetUser(c)

	// Fail closed: per-agent share credentials are not valid on the project MCP
	// SSE transport.
	if hasAgentCallScope(user.Scopes) {
		return apperror.NewForbidden("agent-share credentials are not valid on the project MCP endpoint")
	}

	projectID := c.Param("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid project ID")
	}

	sessionID := c.QueryParam("sessionId")

	// A session ID is a lookup key, not a credential. Authorize it against the
	// authenticated caller before its durable state is used or a response is
	// pushed onto its stream: the ID alone must never let another user, token or
	// project act on (or read) the session.
	state, stream := h.lookupSSESession(sessionID)
	if err := authorizeSSESession(projectID, user, state, stream); err != nil {
		h.log.Warn("rejected MCP SSE session access",
			slog.String("session_id", sessionID),
			slog.String("project_id", projectID),
			slog.String("user_id", user.ID),
		)
		return err
	}

	// Parse JSON-RPC request
	var req Request
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, NewErrorResponse(nil, ErrCodeParseError, "Failed to parse JSON-RPC request", nil))
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		resp := NewErrorResponse(req.ID, ErrCodeInvalidRequest, "Invalid JSON-RPC version", nil)
		return h.sendResponseToSession(c, stream, resp)
	}

	// Process request
	response := h.processRequest(c, &req, projectID, user, sessionID)

	// Notifications return nil — no response per JSON-RPC 2.0 spec
	if response == nil {
		return c.NoContent(http.StatusAccepted)
	}

	// Also push response via SSE stream if the client has an active connection.
	// This is optional — clients that only use the HTTP response body don't need it.
	if stream != nil {
		jsonBytes, _ := json.Marshal(response)
		h.sendSSEEvent(stream, "message", string(jsonBytes))
	}

	// Return the JSON-RPC response directly in the HTTP body with 200 OK.
	// OpenCode (and the MCP SSE spec) expects the response in the POST body.
	return c.JSON(http.StatusOK, response)
}

// processRequest processes a JSON-RPC request for SSE transport.
// sessionID is the ?sessionId query param (may be empty for sessionless clients).
func (h *SSEHandler) processRequest(c echo.Context, req *Request, projectID string, user *auth.AuthUser, sessionID string) *Response {
	// Look up durable session state (survives SSE reconnects). The copy is taken
	// under the lock: the initialize and initialized-notification paths below
	// write these same fields, so reading them from the map after releasing the
	// lock would race those writes.
	state, stateExists := h.lookupSSEState(sessionID)

	initialized := stateExists && state.Initialized
	clientName := ""
	if stateExists {
		clientName = state.ClientName
	}

	// Handle notifications (no id = no response expected per JSON-RPC 2.0)
	if req.IsNotification() {
		if req.Method == "notifications/initialized" && sessionID != "" {
			h.sessionStateMu.Lock()
			if h.sessionState[sessionID] == nil {
				h.sessionState[sessionID] = &sessionInitState{
					ProjectID:  projectID,
					UserID:     user.ID,
					APITokenID: user.APITokenID,
					CreatedAt:  time.Now().UTC(),
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
					ProjectID:  projectID,
					UserID:     user.ID,
					APITokenID: user.APITokenID,
					CreatedAt:  time.Now().UTC(),
				}
			}
			h.sessionState[sessionID].Initialized = true
			h.sessionState[sessionID].ClientName = sanitizeClientName(initializeClientName(req))
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
		return h.handleToolsList(c, req, projectID, user)

	case "tools/call":
		if !initialized {
			return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
				"Client must call initialize before tools/call",
				map[string]string{"hint": "Call initialize method first to establish session"},
			)
		}
		return h.handleToolsCall(c, req, projectID, user, clientName)

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
func (h *SSEHandler) handleToolsList(c echo.Context, req *Request, projectID string, user *auth.AuthUser) *Response {
	tools := h.svc.GetToolDefinitionsForProject(c.Request().Context(), projectID)
	tools = FilterToolsForScopes(tools, user.Scopes)
	// Hard split: operator tools are never listed on the project endpoint,
	// regardless of the caller's superadmin status.
	tools = FilterToolsForSuperadmin(tools, false)
	scope, serr := h.svc.ResolveInstanceScope(c.Request().Context(), user.APITokenID)
	if serr != nil {
		// Fail closed on allowlist resolution failure.
		return NewErrorResponse(req.ID, ErrCodeInternalError,
			"Failed to resolve share instance scope", nil)
	}
	tools = FilterToolsForInstance(tools, scope)
	return NewSuccessResponse(req.ID, ToolsListResult{Tools: tools})
}

// handleToolsCall handles tools/call for SSE transport
func (h *SSEHandler) handleToolsCall(c echo.Context, req *Request, projectID string, user *auth.AuthUser, clientName string) *Response {
	var params ToolsCallParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", nil)
		}
	}

	if params.Name == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required parameter: name", nil)
	}

	// Enforce per-tool scope.
	if toolDef := h.svc.GetToolByName(params.Name); toolDef != nil {
		if toolDef.AgentOnly {
			return NewErrorResponse(req.ID, ErrCodeMethodNotFound,
				"Tool not found: "+params.Name, nil)
		}
		if toolDef.SuperadminOnly {
			// Hard split: operator tools are reachable only on /api/mcp/admin.
			return NewErrorResponse(req.ID, ErrCodeMethodNotFound,
				"Tool not found: "+params.Name, nil)
		}
		if toolDef.RequiredScope != "" {
			expanded := expandScopesSet(user.Scopes)
			if !expanded[toolDef.RequiredScope] {
				return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
					"Insufficient scope to call tool: "+params.Name,
					map[string]string{"required_scope": toolDef.RequiredScope},
				)
			}
		}
	}

	// Enforce the share-instance tool allowlist (deny-by-default) before any side effect.
	scope, serr := h.svc.ResolveInstanceScope(c.Request().Context(), user.APITokenID)
	if serr != nil {
		return NewErrorResponse(req.ID, ErrCodeInternalError,
			"Failed to resolve share instance scope", nil)
	}
	if InstanceDeniesTool(scope, legacyDispatchName(params.Name, params.Arguments)) {
		return NewErrorResponse(req.ID, ErrCodeForbidden,
			"Tool not allowed: "+params.Name, nil)
	}

	execCtx := WithInstanceScope(c.Request().Context(), scope)
	execCtx = withClientName(execCtx, clientName)
	// The transport already enforced per-tool AgentOnly / RequiredScope /
	// SuperadminOnly above. Mark the call transport-enforced so ExecuteTool's
	// in-process gate (which covers the agent-run path) does not re-fire for an
	// authenticated HTTP client. This is NOT the trusted-internal marker: relay
	// (agent-only) tools are gated on trusted-internal only, so an HTTP caller
	// can never reach them through the relay fallback (issue #1017).
	execCtx = ContextWithTransportEnforced(execCtx)
	result, err := h.svc.ExecuteTool(execCtx, projectID, params.Name, params.Arguments)
	if err != nil {
		h.log.Error("tool execution failed",
			slog.String("tool", params.Name),
			logger.Error(err),
		)
		return toolExecutionErrorResponse(req.ID, err)
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

// sendResponseToSession sends a response via SSE (when the caller has an
// authorized live stream) and always returns it in the HTTP body.
func (h *SSEHandler) sendResponseToSession(c echo.Context, session *SSESession, resp *Response) error {
	if session != nil {
		jsonBytes, _ := json.Marshal(resp)
		h.sendSSEEvent(session, "message", string(jsonBytes))
	}
	return c.JSON(http.StatusOK, resp)
}

// generateSessionID creates a unique session ID
func (h *SSEHandler) generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("mcp_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(bytes)[:12])
}
