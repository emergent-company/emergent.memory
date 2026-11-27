package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// StreamableHTTPHandler implements MCP Streamable HTTP transport (spec 2025-11-25)
// Single endpoint that handles both POST and GET requests with SSE support
type StreamableHTTPHandler struct {
	svc *Service
	log *slog.Logger

	// admin, when true, marks this instance as the operator transport: it skips
	// project claim, serves the operator catalog, executes with an empty project,
	// and skips instance-scope. The project instance leaves admin false.
	admin bool

	// Session management (session ID -> session state)
	sessions   map[string]*MCPSession
	sessionsMu sync.RWMutex

	// SSE streams (session ID -> list of active streams)
	streams   map[string][]*SSEStream
	streamsMu sync.RWMutex

	// Event storage for resumability (Last-Event-ID support)
	eventStore *EventStore
}

// MCPSession represents an MCP session state.
//
// A session is bound to the identity and credential that created it and to the
// project it was initialized for. The session ID is a lookup key, never a
// credential: every request that presents one is authorized against the
// authenticated caller before the session's identity or project is used (see
// authorizeSession).
type MCPSession struct {
	ID        string
	ProjectID string
	UserID    string
	// APITokenID is the API token that created the session ("" for
	// OIDC/session auth). It is never rewritten after creation, so a session
	// cannot be rebound to a different credential.
	APITokenID      string
	OrgID           string
	Initialized     bool
	ProtocolVersion string
	// ClientName is the sanitized clientInfo.name from initialize, carried into
	// tools/call so tools can name the real client (issue #472).
	ClientName   string
	CreatedAt    time.Time
	LastAccessAt time.Time

	// ProjectSwitched is true once the client explicitly selected the session's
	// project — via initialize project_id or `project` action=use. It gates write
	// tools when MCP_REQUIRE_PROJECT_SWITCH is enabled (issue #1163).
	ProjectSwitched bool

	// persisted reports whether this value came from (or has been written to)
	// the session map. A snapshot read from the map carries persisted=true, so
	// putSession can refuse to resurrect a session a concurrent DELETE removed;
	// a session created by getOrCreateSession has not been stored yet.
	persisted bool
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
	closeOnce   sync.Once
}

// Close signals the stream's reader exactly once. Both the GET handler's
// deferred cleanup and session termination (DELETE) can reach a stream, so a
// bare close(stream.Done) raced and panicked with "close of closed channel"
// (#2338).
func (s *SSEStream) Close() {
	s.closeOnce.Do(func() { close(s.Done) })
}

// NewStreamableHTTPHandler creates a new spec-compliant MCP handler
func NewStreamableHTTPHandler(svc *Service, log *slog.Logger) *StreamableHTTPHandler {
	return newStreamableHTTPHandler(svc, log, false)
}

// AdminStreamableHTTPHandler is the operator transport handle: a distinct type
// wrapping *StreamableHTTPHandler. The distinct type is required for fx, which
// rejects two providers of the same result type — the project transport and the
// operator transport must be addressable independently in the dependency graph.
// The embedded handler carries admin=true.
type AdminStreamableHTTPHandler struct {
	*StreamableHTTPHandler
}

// NewAdminStreamableHTTPHandler creates the operator MCP transport (admin mode).
// It serves the operator catalog, skips project claim and instance scope, and
// executes with an empty project. It is registered on /api/mcp/admin behind
// RequireAuth + RequireSuperadminFull.
func NewAdminStreamableHTTPHandler(svc *Service, log *slog.Logger) *AdminStreamableHTTPHandler {
	return &AdminStreamableHTTPHandler{newStreamableHTTPHandler(svc, log, true)}
}

func newStreamableHTTPHandler(svc *Service, log *slog.Logger, admin bool) *StreamableHTTPHandler {
	return &StreamableHTTPHandler{
		svc:        svc,
		log:        log.With(logger.Scope("mcp.streamable")),
		admin:      admin,
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
		// No header: default to latest without validation (backward compat with older clients)
		protocolVersion = LatestProtocolVersion
		h.log.Debug("no protocol version header, defaulting to latest", slog.String("version", LatestProtocolVersion))
	} else if !h.isValidProtocolVersion(protocolVersion) {
		// Header present but unsupported version
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error": map[string]any{
				"code":      "invalid_protocol_version",
				"message":   fmt.Sprintf("Unsupported protocol version: %s", protocolVersion),
				"supported": SupportedProtocolVersions,
			},
		})
	}

	// Fail closed: a per-agent share credential is only valid at its agent
	// endpoint, never on the project MCP endpoint. Reject before any
	// session/tool listing or execution.
	if hasAgentCallScope(auth.MustGetUser(c).Scopes) {
		return c.JSON(http.StatusForbidden, map[string]any{
			"error": map[string]string{
				"code":    "agent_share_credential",
				"message": "Agent-share credentials are not valid on the project MCP endpoint",
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
	user := auth.MustGetUser(c)

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
		// A session the caller may not use is indistinguishable on the wire from
		// an expired one: the client learns only that it must re-initialize, and
		// a caller probing a leaked session ID learns nothing about its owner.
		if errors.Is(err, errSessionNotFound) || errors.Is(err, errSessionForbidden) {
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

	// Get session. The session ID is not a credential: the caller must be the
	// user and credential that created the session, and must address the
	// session's project.
	sessionID := c.Request().Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"code":    "session_required",
				"message": "Mcp-Session-Id header is required for GET requests",
			},
		})
	}

	if _, err := h.sessionForCaller(sessionID, auth.MustGetUser(c)); err != nil {
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
		stream.Close()
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

	// Terminating a session is session-bound too: a leaked ID must not let
	// another caller kill the owner's session.
	if _, err := h.sessionForCaller(sessionID, auth.MustGetUser(c)); err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{
			"error": map[string]string{
				"code":    "session_not_found",
				"message": "Session not found or expired",
			},
		})
	}

	h.sessionsMu.Lock()
	delete(h.sessions, sessionID)
	h.sessionsMu.Unlock()

	h.streamsMu.Lock()
	streams := h.streams[sessionID]
	for _, stream := range streams {
		stream.Close()
	}
	delete(h.streams, sessionID)
	h.streamsMu.Unlock()

	h.eventStore.ClearSession(sessionID)

	h.log.Info("session terminated", slog.String("session_id", sessionID))

	return c.NoContent(http.StatusNoContent)
}

// Sentinel session errors. A session the caller may not use is reported
// identically to a missing one on the wire (both map to 404 session_not_found);
// they are distinct internally so the attempt can be logged.
var (
	errSessionNotFound  = errors.New("session_not_found")
	errSessionForbidden = errors.New("session_forbidden")
)

// getOrCreateSession gets an existing session or creates a new one.
func (h *StreamableHTTPHandler) getOrCreateSession(sessionID string, user *auth.AuthUser, protocolVersion string) (*MCPSession, error) {
	if sessionID != "" {
		return h.sessionForCaller(sessionID, user)
	}

	// Create new session (will be assigned ID during initialize)
	projectID := user.ProjectID
	orgID := user.OrgID
	if h.admin {
		// The operator endpoint resolves no project context.
		projectID = ""
		orgID = ""
	}
	session := &MCPSession{
		ID:              uuid.New().String(),
		ProjectID:       projectID,
		UserID:          user.ID,
		APITokenID:      user.APITokenID,
		OrgID:           orgID,
		Initialized:     false,
		ProtocolVersion: protocolVersion,
		CreatedAt:       time.Now(),
		LastAccessAt:    time.Now(),
	}

	return session, nil
}

// sessionForCaller looks a session up and authorizes the authenticated caller
// before returning it. The returned value is a snapshot: handlers mutate the
// snapshot and persist it with putSession, so the fields authorized here cannot
// change under a request that is already executing (a concurrent re-initialize
// writes a new snapshot instead).
func (h *StreamableHTTPHandler) sessionForCaller(sessionID string, user *auth.AuthUser) (*MCPSession, error) {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()

	stored, exists := h.sessions[sessionID]
	if !exists {
		return nil, errSessionNotFound
	}
	if err := h.authorizeSession(stored, user); err != nil {
		callerID, callerProjectID := "", ""
		if user != nil {
			callerID, callerProjectID = user.ID, user.ProjectID
		}
		h.log.Warn("rejected MCP session access",
			slog.String("session_id", sessionID),
			slog.String("session_project_id", stored.ProjectID),
			slog.String("caller_project_id", callerProjectID),
			slog.String("session_user_id", stored.UserID),
			slog.String("caller_user_id", callerID),
			slog.Bool("session_credential_bound", stored.APITokenID != ""),
		)
		return nil, err
	}
	stored.LastAccessAt = time.Now()
	return cloneMCPSession(stored), nil
}

// authorizeSession reports whether the authenticated caller may use an existing
// MCP session. A session is bound to the identity and credential that created
// it, and to the project it was initialized for:
//
//   - identity: the creating user must be the caller;
//   - credential: if either the session or the caller is API-token
//     authenticated, the token IDs must match, so one project credential can
//     never resume another's session (or a narrower token inherit a broader
//     session);
//   - project: a session initialized for a project may only be used while
//     addressing that project, unless the client explicitly switched it
//     (issue #1163). A switch is authorized against org membership by
//     authorizeProjectClaim and persisted, and every executing request
//     (tools/call, resources/read, prompts/get) re-runs that same membership
//     check for the session's project — so a switched session intentionally
//     differs from the caller's default project without weakening the gate. A
//     session with no project keeps resolving the project from the caller, as
//     before.
//
// The operator transport resolves no project context, so it enforces identity
// and credential only. A nil session or user fails closed.
func (h *StreamableHTTPHandler) authorizeSession(session *MCPSession, user *auth.AuthUser) error {
	if session == nil || user == nil {
		return errSessionForbidden
	}
	if session.UserID != user.ID {
		return errSessionForbidden
	}
	if session.APITokenID != user.APITokenID && (session.APITokenID != "" || user.APITokenID != "") {
		return errSessionForbidden
	}
	if !h.admin && !session.ProjectSwitched && session.ProjectID != "" && user.ProjectID != "" && session.ProjectID != user.ProjectID {
		return errSessionForbidden
	}
	return nil
}

// cloneMCPSession copies a session so request paths operate on a snapshot.
func cloneMCPSession(session *MCPSession) *MCPSession {
	if session == nil {
		return nil
	}
	clone := *session
	return &clone
}

// putSession stores a session snapshot. Writes replace the map entry under the
// lock rather than mutating the stored value, which keeps snapshots handed to
// in-flight requests immutable.
//
// A snapshot that came from the map only updates an entry that still exists: a
// request whose session was terminated while it was in flight (a DELETE, or a
// refusal path) must not insert it back. A session being stored for the first
// time (the initialize of a fresh session) is inserted normally.
func (h *StreamableHTTPHandler) putSession(session *MCPSession) {
	if session == nil {
		return
	}
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()
	if session.persisted {
		if _, exists := h.sessions[session.ID]; !exists {
			return
		}
	}
	session.persisted = true
	h.sessions[session.ID] = cloneMCPSession(session)
}

// markSessionInitialized sets the initialized flag on the stored session, if it
// still exists. It is a field-scoped update rather than a whole-snapshot write,
// so a notifications/initialized racing a re-initialize cannot restore the
// project id the re-initialize just changed, and cannot resurrect a session that
// was terminated.
func (h *StreamableHTTPHandler) markSessionInitialized(sessionID string) {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()
	if stored, exists := h.sessions[sessionID]; exists {
		stored.Initialized = true
	}
}

// getSession retrieves a snapshot of an existing session without authorizing the
// caller. Request paths must use sessionForCaller; this is for tests and
// internal inspection only.
func (h *StreamableHTTPHandler) getSession(sessionID string) *MCPSession {
	h.sessionsMu.RLock()
	defer h.sessionsMu.RUnlock()
	return cloneMCPSession(h.sessions[sessionID])
}

// processRequest processes JSON-RPC requests
func (h *StreamableHTTPHandler) processRequest(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser) *Response {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(c, req, session, user)
	case "tools/list":
		return h.handleToolsList(c, req, session, user)
	case "tools/call":
		return h.handleToolsCall(c, req, session, user)
	case "resources/list":
		return h.handleResourcesList(c, req, session, user)
	case "resources/read":
		return h.handleResourcesRead(c, req, session, user)
	case "prompts/list":
		return h.handlePromptsList(c, req, session, user)
	case "prompts/get":
		return h.handlePromptsGet(c, req, session, user)
	default:
		return NewErrorResponse(
			req.ID,
			ErrCodeMethodNotFound,
			"Method not found: "+req.Method,
			map[string]any{
				"method":            req.Method,
				"supported_methods": []string{"initialize", "tools/list", "tools/call", "resources/list", "resources/read", "prompts/list", "prompts/get"},
			},
		)
	}
}

// handleInitialize handles initialize method
func (h *StreamableHTTPHandler) handleInitialize(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser) *Response {
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

	// Resolve the candidate project WITHOUT mutating the session: the initialize
	// params when supplied, otherwise the session's current project. Authorize
	// the candidate before touching any session field so a denied re-init leaves
	// the stored session exactly as it was — a re-init that names a foreign
	// project must not poison the existing session (issue #868 follow-up:
	// mutation-before-check). Session callers must be org members of the claimed
	// project and project-bound tokens cannot claim a foreign project.
	candidate := params.ProjectID
	if candidate == "" {
		candidate = session.ProjectID
	}
	if h.admin {
		// The operator endpoint resolves no project context and never claims one.
		candidate = ""
	} else if err := h.svc.authorizeProjectClaim(c.Request().Context(), user, candidate); err != nil {
		return NewErrorResponse(req.ID, ErrCodeForbidden, "Project access denied", nil)
	}

	// Authorized: commit the session update and store it.
	session.Initialized = true
	session.ProtocolVersion = params.ProtocolVersion
	session.ProjectID = candidate
	if params.ProjectID != "" {
		// An explicit initialize claim counts as an explicit project selection for
		// the require-switch write gate; a header/personal default does not.
		session.ProjectSwitched = true
	}
	session.ClientName = sanitizeClientName(params.ClientInfo.Name)

	h.putSession(session)

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

	if h.admin {
		h.svc.auditSuperadmin(c.Request().Context(), SuperadminAuditEntry{
			EventType:    "mcp.superadmin.initialize",
			Outcome:      "success",
			UserID:       user.ID,
			UserEmail:    user.Email,
			ResourceType: "session",
			ResourceID:   session.ID,
			Action:       "initialize",
			Endpoint:     "/api/mcp/admin",
			HTTPMethod:   c.Request().Method,
			StatusCode:   http.StatusOK,
			RequestID:    session.ID,
		})
	}

	return NewSuccessResponse(req.ID, result)
}

// handleToolsList handles tools/list method
func (h *StreamableHTTPHandler) handleToolsList(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser) *Response {
	if h.admin {
		return h.handleAdminToolsList(req, session)
	}
	if !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before tools/list",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}

	tools := h.svc.GetToolDefinitionsForProject(c.Request().Context(), session.ProjectID)
	// Filter on the caller's current scopes, not creation-time session state: a
	// reduced token scope must be reflected immediately, not at re-initialize.
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

// handleAdminToolsList serves the operator catalog on the admin endpoint. It is
// the same catalog regardless of caller project context (there is none) and is
// not filtered by token scope or share-instance allowlist.
func (h *StreamableHTTPHandler) handleAdminToolsList(req *Request, session *MCPSession) *Response {
	if !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before tools/list",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}
	tools := h.svc.GetSuperadminToolDefinitions()
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

	if h.admin {
		return h.handleAdminToolsCall(c, req, session, user, params)
	}

	// Enforce per-tool scope.
	if toolDef := h.svc.GetToolByName(params.Name); toolDef != nil {
		if toolDef.AgentOnly {
			return NewErrorResponse(req.ID, ErrCodeMethodNotFound,
				"Tool not found: "+params.Name, nil)
		}
		if toolDef.SuperadminOnly {
			// Hard split: operator tools are reachable only on /api/mcp/admin,
			// never on the project endpoint, regardless of caller superadmin status.
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

	// Session project control (issue #1163): list/use mutate the session, so the
	// transport handles them before dispatch. The switch is authorized by the
	// same check initialize performs.
	if action, ok := projectSessionAction(params.Name, params.Arguments); ok {
		return h.handleProjectSessionAction(c, req, session, user, action, params.Arguments)
	}

	projectID := session.ProjectID
	if projectID == "" {
		projectID = user.ProjectID
	}

	// Re-check the session's project on every call, not just at initialize
	// (issue #1163): a user removed from the project's organization must lose
	// access to an already-configured session immediately.
	if err := h.svc.authorizeProjectClaim(c.Request().Context(), user, projectID); err != nil {
		return NewErrorResponse(req.ID, ErrCodeForbidden, "Project access denied", nil)
	}

	// Optional explicit-switch requirement: refuse write tools until the client
	// has selected the session's project with `project` action=use.
	if h.svc.ProjectSwitchRequired(c.Request().Context(), projectID, session.ProjectSwitched, params.Name, params.Arguments) {
		return NewErrorResponse(req.ID, ErrCodeForbidden, ProjectSwitchRequiredMessage(), nil)
	}

	if projectID == "" && requiresProject(params.Name, params.Arguments) {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams,
			"Project ID is required. Provide project_id in initialize params or X-Project-Id header.",
			map[string]string{"hint": "Call initialize with project_id parameter or set X-Project-Id header"},
		)
	}

	// Execute tool
	execCtx := WithInstanceScope(c.Request().Context(), scope)
	execCtx = withClientName(execCtx, session.ClientName)
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

// handleProjectSessionAction performs `project` action=list or action=use on
// this session. It returns the JSON-RPC response; the caller always returns it.
func (h *StreamableHTTPHandler) handleProjectSessionAction(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser, action string, args map[string]any) *Response {
	// The service resolves the caller from the standard context (the echo context
	// carries it for auth.MustGetUser only), so stamp it for the project-list query.
	ctx := auth.ContextWithUser(c.Request().Context(), user)
	switch action {
	case projectActionList:
		// session is this request's snapshot, so its project is the active one
		// (a switch persisted by an earlier request is already in it).
		activeProjectID := session.ProjectID
		result, err := h.svc.executeListMemberProjects(ctx, activeProjectID)
		if err != nil {
			return toolExecutionErrorResponse(req.ID, err)
		}
		return NewSuccessResponse(req.ID, result)
	case projectActionUse:
		projectID, err := projectUseProjectID(args)
		if err != nil {
			return toolExecutionErrorResponse(req.ID, err)
		}
		// Same authorization contract as initialize: project-bound tokens stay
		// bound (except user-scoped OAuth connectors) and session callers must be
		// org members of the claimed project.
		if err := h.svc.authorizeProjectClaim(ctx, user, projectID); err != nil {
			h.log.Info("MCP project switch denied",
				slog.String("session_id", session.ID),
				slog.String("project_id", projectID),
				logger.Error(err),
			)
			return NewErrorResponse(req.ID, ErrCodeForbidden, "Project access denied", nil)
		}
		// The session handed to this request is a snapshot; persist the switch so
		// later requests on this session id read it (see putSession). The
		// authorization above already ran, so nothing is mutated on a refusal.
		session.ProjectID = projectID
		session.ProjectSwitched = true
		h.putSession(session)
		h.log.Info("MCP project switched",
			slog.String("session_id", session.ID),
			slog.String("project_id", projectID),
			slog.String("user_id", user.ID),
		)
		result, err := h.svc.projectUseResult(ctx, projectID)
		if err != nil {
			return toolExecutionErrorResponse(req.ID, err)
		}
		return NewSuccessResponse(req.ID, result)
	}
	return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Unknown project action: "+action, nil)
}

// handleAdminToolsCall executes an operator tool on the admin endpoint. The
// route is already gated on superadmin_full, so the dispatch re-gates fail-closed
// (ExecuteSuperadminTool resolves the platform grant again) and every call is
// audited best-effort.
func (h *StreamableHTTPHandler) handleAdminToolsCall(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser, params ToolsCallParams) *Response {
	// Only the operator catalog is served on the admin endpoint. An unknown or
	// project-scoped tool name is method-not-found (never reveals the catalog).
	if !h.svc.IsAdminOperatorTool(params.Name) {
		return NewErrorResponse(req.ID, ErrCodeMethodNotFound,
			"Tool not found: "+params.Name, nil)
	}

	start := time.Now()
	execCtx := ContextWithTransportEnforced(c.Request().Context())
	result, err := h.svc.ExecuteSuperadminTool(execCtx, params.Name, params.Arguments)

	// Audit the call best-effort (never fails the tool call).
	outcome := "success"
	statusCode := http.StatusOK
	errMsg := ""
	if err != nil {
		outcome = "error"
		errMsg = RedactString(err.Error())
		// The transport maps tool errors to a 200 JSON-RPC error response, but
		// the audit status reflects the outcome at the JSON-RPC layer.
		statusCode = http.StatusOK
	}
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	if requestID == "" {
		requestID = session.ID
	}
	h.svc.auditSuperadmin(c.Request().Context(), SuperadminAuditEntry{
		EventType:    "mcp.superadmin.tool_call",
		Outcome:      outcome,
		UserID:       user.ID,
		UserEmail:    user.Email,
		ResourceType: "tool",
		ResourceID:   params.Name,
		Action:       params.Name,
		Endpoint:     "/api/mcp/admin",
		HTTPMethod:   c.Request().Method,
		StatusCode:   statusCode,
		ErrorMessage: errMsg,
		IPAddress:    c.RealIP(),
		UserAgent:    c.Request().UserAgent(),
		RequestID:    requestID,
		Details:      RedactSecrets(params.Arguments),
		Duration:     time.Since(start),
	})

	if err != nil {
		h.log.Error("tool execution failed",
			slog.String("tool", params.Name),
			slog.String("error", errMsg),
		)
		return toolExecutionErrorResponse(req.ID, err)
	}

	return NewSuccessResponse(req.ID, result)
}

// error when a restricted share instance requests MCP resources/prompts, which
// are not covered by its allowlists. Resolution failures fail closed.
func (h *StreamableHTTPHandler) denyInstanceContent(c echo.Context, req *Request, user *auth.AuthUser, kind string) *Response {
	scope, err := h.svc.ResolveInstanceScope(c.Request().Context(), user.APITokenID)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeInternalError,
			"Failed to resolve share instance scope", nil)
	}
	if InstanceRestrictsContent(scope) {
		return NewErrorResponse(req.ID, ErrCodeForbidden,
			kind+" are not available to this MCP share instance", nil)
	}
	return nil
}

// handleResourcesList handles resources/list method. The initialize response
// advertises the resources capability, so this transport must serve it: a
// client that checks capabilities before calling resources/list (Cloudflare's
// MCP portal among them) otherwise fails sync against /api/mcp with -32601
// (issue #1089).
func (h *StreamableHTTPHandler) handleResourcesList(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser) *Response {
	if !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before resources/list",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}
	if resp := h.denyInstanceContent(c, req, user, "Resources"); resp != nil {
		return resp
	}
	resources := h.svc.GetResourceDefinitions()
	return NewSuccessResponse(req.ID, ResourcesListResult{Resources: resources})
}

// handleResourcesRead handles resources/read method. Project context follows the
// session established at initialize, falling back to the authenticated user's
// project, exactly like the other session-scoped methods on this transport.
func (h *StreamableHTTPHandler) handleResourcesRead(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser) *Response {
	if !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before resources/read",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}
	if resp := h.denyInstanceContent(c, req, user, "Resources"); resp != nil {
		return resp
	}
	var params ResourceReadParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams,
				"Invalid resources/read params", map[string]string{"error": err.Error()})
		}
	}
	if params.URI == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams,
			"Missing required parameter: uri",
			map[string]any{"required": []string{"uri"}},
		)
	}
	projectID := session.ProjectID
	if projectID == "" {
		projectID = user.ProjectID
	}
	if err := h.svc.authorizeProjectClaim(c.Request().Context(), user, projectID); err != nil {
		return NewErrorResponse(req.ID, ErrCodeForbidden, "Project access denied", nil)
	}
	result, err := h.svc.ReadResource(c.Request().Context(), projectID, params.URI)
	if err != nil {
		h.log.Error("resource read failed",
			slog.String("uri", params.URI),
			logger.Error(err),
		)
		return NewErrorResponse(req.ID, jsonRPCErrorCodeFor(err),
			apperror.PublicMessage(err),
			map[string]string{"uri": params.URI},
		)
	}
	return NewSuccessResponse(req.ID, result)
}

// handlePromptsList handles prompts/list method
func (h *StreamableHTTPHandler) handlePromptsList(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser) *Response {
	if !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before prompts/list",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}
	if resp := h.denyInstanceContent(c, req, user, "Prompts"); resp != nil {
		return resp
	}
	prompts := h.svc.GetPromptDefinitions()
	return NewSuccessResponse(req.ID, PromptsListResult{Prompts: prompts})
}

// handlePromptsGet handles prompts/get method
func (h *StreamableHTTPHandler) handlePromptsGet(c echo.Context, req *Request, session *MCPSession, user *auth.AuthUser) *Response {
	if !session.Initialized {
		return NewErrorResponse(req.ID, ErrCodeInvalidRequest,
			"Client must call initialize before prompts/get",
			map[string]string{"hint": "Call initialize method first to establish session"},
		)
	}
	if resp := h.denyInstanceContent(c, req, user, "Prompts"); resp != nil {
		return resp
	}
	var params PromptGetParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams,
				"Invalid prompts/get params", map[string]string{"error": err.Error()})
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
	if err := h.svc.authorizeProjectClaim(c.Request().Context(), user, projectID); err != nil {
		return NewErrorResponse(req.ID, ErrCodeForbidden, "Project access denied", nil)
	}
	result, err := h.svc.GetPrompt(c.Request().Context(), projectID, params.Name, params.Arguments)
	if err != nil {
		return NewErrorResponse(req.ID, jsonRPCErrorCodeFor(err),
			"Failed to get prompt: "+apperror.PublicMessage(err),
			map[string]string{"name": params.Name},
		)
	}
	return NewSuccessResponse(req.ID, result)
}

func (h *StreamableHTTPHandler) handleNotification(c echo.Context, req *Request, session *MCPSession) {
	switch req.Method {
	case "notifications/initialized":
		h.markSessionInitialized(session.ID)
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
