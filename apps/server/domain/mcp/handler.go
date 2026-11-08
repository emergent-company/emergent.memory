package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/oauthas"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Keep apperror imported for swagger type resolution (@Failure {object} apperror.Error).
var _ = apperror.Error{}

// Handler handles MCP HTTP requests
type Handler struct {
	svc            *Service
	log            *slog.Logger
	userProfileSvc *auth.UserProfileService

	// Session management (token -> session)
	sessions   map[string]*Session
	sessionsMu sync.RWMutex
}

// Session stores MCP session state
type Session struct {
	Initialized bool
	ProjectID   string
	// ClientName is the sanitized clientInfo.name from initialize, carried into
	// tools/call so tools can name the real client (issue #472).
	ClientName string
}

// NewHandler creates a new MCP handler
func NewHandler(svc *Service, log *slog.Logger, userProfileSvc *auth.UserProfileService) *Handler {
	return &Handler{
		svc:            svc,
		log:            log.With(logger.Scope("mcp.handler")),
		userProfileSvc: userProfileSvc,
		sessions:       make(map[string]*Session),
	}
}

// prmWellKnownPath is the RFC 9728 protected-resource-metadata path prefix. The
// metadata for a resource at https://host/api/mcp lives at
// https://host/.well-known/oauth-protected-resource/api/mcp: the well-known
// segment is inserted before the resource's own path (RFC 9728 §3.1). A client
// that requests metadata for a path-bearing resource looks there first, so
// serving only the origin form makes discovery fail before the OAuth flow
// starts.
const prmWellKnownPath = "/.well-known/oauth-protected-resource"

// HandleOAuthProtectedResource handles GET /.well-known/oauth-protected-resource
// and its path-inserted form (RFC 9728 §3.1). This endpoint is required by the
// MCP 2025-11-25 authorization spec. The returned "resource" must be the
// identifier the client addressed: a client that connects to
// https://host/api/mcp MUST reject metadata whose resource is the bare origin
// (RFC 9728 §3.3), which is what strict clients (e.g. Google's Gemini
// connector) do. An empty authorization_servers array means bearer token /
// API key auth with no OAuth flow available.
//
// @Summary      OAuth Protected Resource Metadata
// @Description  Returns OAuth 2.0 Protected Resource Metadata per RFC 9728 for the addressed resource, advertising the built-in authorization server when OAuth is enabled.
// @Tags         mcp
// @Produce      json
// @Success      200 {object} map[string]any "Protected resource metadata"
// @Router       /.well-known/oauth-protected-resource [get]
// @Router       /.well-known/oauth-protected-resource/{resourcePath} [get]
func (h *Handler) HandleOAuthProtectedResource(c echo.Context) error {
	origin := requestOrigin(c)
	resourceURL := origin
	// Keep the addressed path verbatim: RFC 9728 §3.3 requires the returned
	// "resource" to be identical to the identifier the client used, and a
	// trailing slash makes it a different URI. Only the bare well-known path
	// (which addresses the origin) collapses to the origin.
	if rest := strings.TrimPrefix(c.Request().URL.Path, prmWellKnownPath); rest != "" && rest != "/" {
		resourceURL += rest
	}

	// When the built-in authorization server is enabled, advertise it so MCP
	// clients can run the OAuth flow. The issuer comes from config when set,
	// else from the request origin - never from resourceURL, which carries the
	// resource path and would make the issuer look like
	// https://host/api/mcp (the issuer is the origin: PRM's
	// authorization_servers entries are issuer identifiers, RFC 9728 §2/§3.3).
	authorizationServers := []string{}
	if h.svc != nil && h.svc.oauthASEnabled {
		issuer := h.svc.oauthASIssuer
		if issuer == "" {
			issuer = origin
		}
		authorizationServers = []string{issuer}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"resource":                 resourceURL,
		"bearer_methods_supported": []string{"header"},
		"authorization_servers":    authorizationServers,
		// Advertised so clients can ask for exactly the scopes this server
		// understands; an MCP client SHOULD use these instead of inventing
		// scopes of its own.
		"scopes_supported": oauthas.SupportedScopes,
	})
}

// requestOrigin returns the externally visible origin of the request, honouring
// the reverse proxy's X-Forwarded-Proto.
func requestOrigin(c echo.Context) string {
	scheme := "https"
	if c.Request().TLS == nil && c.Request().Header.Get("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	if fwdProto := c.Request().Header.Get("X-Forwarded-Proto"); fwdProto != "" {
		scheme = fwdProto
	}
	return scheme + "://" + c.Request().Host
}

// HandleRPC handles POST /mcp/rpc - JSON-RPC 2.0 endpoint (legacy)
// @Summary      Execute Model Context Protocol JSON-RPC 2.0 requests
// @Description  Legacy JSON-RPC endpoint for Model Context Protocol. Supports initialize, tools/*, resources/*, and prompts/* methods. Use unified /api/mcp endpoint for SSE support (Spec 2025-11-25).
// @Tags         mcp
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID (UUID)"
// @Param        request body Request true "JSON-RPC 2.0 request (method: initialize, tools/list, tools/call, resources/list, resources/read, prompts/list, prompts/get)"
// @Success      200 {object} Response "JSON-RPC 2.0 response (success or error)"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/mcp/rpc [post]
// @Security     bearerAuth
func (h *Handler) HandleRPC(c echo.Context) error {
	user := auth.MustGetUser(c)

	// Fail closed: a per-agent share credential is only valid at its agent
	// endpoint, never on the project MCP endpoint.
	if hasAgentCallScope(user.Scopes) {
		return c.JSON(http.StatusForbidden, NewErrorResponse(
			nil,
			ErrCodeForbidden,
			"Agent-share credentials are not valid on the project MCP endpoint",
			nil,
		))
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
	case "resources/list":
		return h.handleResourcesList(c, req, user)
	case "resources/read":
		return h.handleResourcesRead(c, req, user)
	case "prompts/list":
		return h.handlePromptsList(c, req, user)
	case "prompts/get":
		return h.handlePromptsGet(c, req, user)
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

	// Reconcile the initialize-claimed project against the authorized header
	// (issue #868): session callers must be org members of the claimed project
	// and project-bound tokens cannot claim a foreign project.
	if err := h.svc.authorizeProjectClaim(c.Request().Context(), user, projectID); err != nil {
		return NewErrorResponse(req.ID, ErrCodeForbidden, "Project access denied", nil)
	}

	if token != "" {
		h.sessionsMu.Lock()
		h.sessions[token] = &Session{
			Initialized: true,
			ProjectID:   projectID,
			ClientName:  sanitizeClientName(params.ClientInfo.Name),
		}
		h.sessionsMu.Unlock()
	}

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

	tools := h.svc.GetToolDefinitionsForProject(c.Request().Context(), user.ProjectID)
	tools = FilterToolsForScopes(tools, user.Scopes)
	// Hard split: operator tools are never listed on the project endpoint,
	// regardless of the caller's superadmin status.
	tools = FilterToolsForSuperadmin(tools, false)
	scope, serr := h.svc.ResolveInstanceScope(c.Request().Context(), user.APITokenID)
	if serr != nil {
		// Fail closed: never list the full scope-permitted catalog when the
		// share-instance allowlist could not be resolved.
		return NewErrorResponse(req.ID, ErrCodeInternalError,
			"Failed to resolve share instance scope", nil)
	}
	tools = FilterToolsForInstance(tools, scope)
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

	// Enforce per-tool scope: reject AgentOnly tools and tools requiring a scope the caller lacks.
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
		// Fail closed: an unresolved allowlist must never execute the tool.
		return NewErrorResponse(req.ID, ErrCodeInternalError,
			"Failed to resolve share instance scope", nil)
	}
	if InstanceDeniesTool(scope, legacyDispatchName(params.Name, params.Arguments)) {
		return NewErrorResponse(req.ID, ErrCodeForbidden,
			"Tool not allowed: "+params.Name, nil)
	}

	// Get project ID: prefer the per-request header (auth-middleware-resolved) so that
	// callers with org-level tokens can target different projects on the same session.
	// Fall back to the session value set at initialize time only when the header is absent.
	projectID := user.ProjectID
	if projectID == "" {
		projectID = session.ProjectID
	}

	if projectID == "" {
		// Some tools require project ID
		if requiresProject(params.Name, params.Arguments) {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams,
				"Project ID is required. Provide project_id in initialize params or X-Project-Id header.",
				map[string]string{"hint": "Call initialize with project_id parameter or set X-Project-Id header"},
			)
		}
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

// extractToken extracts session identifier from request (Bearer token or X-API-Key).
func extractToken(c echo.Context) string {
	authHeader := c.Request().Header.Get("Authorization")
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		return authHeader[7:]
	}

	if apiKey := c.Request().Header.Get("X-API-Key"); apiKey != "" {
		return apiKey
	}

	return ""
}

// requiresProject returns true if the call needs a project context. The gate is
// per dispatch target, not per advertised tool: the consolidation merged actions
// that disagreed (schema-write action=create never needed a project, while
// schema action=owned does), so decide on the per-verb name the call resolves to
// and keep the pre-consolidation list verbatim.
func requiresProject(toolName string, args map[string]any) bool {
	switch legacyDispatchName(toolName, args) {
	case "entity-type-list", "entity-query", "entity-search", "entity-edges-get",
		"schema-version", "schema-list", "schema-get", "schema-delete",
		"schema-list-available", "schema-list-installed",
		"schema-assign", "schema-assignment-update", "schema-uninstall",
		// Catalog/profile reads added for blueprint-schema parity (#18). All
		// three read a project-scoped surface, so a context-less call must get
		// the documented invalid-params error, not an internal tool error.
		// blueprint-import-preview is deliberately absent: it supports a global
		// (superadmin) preview with no project. The write-side #18 tools that
		// mirror a RequireProjectID REST route (updates check/apply, model
		// overrides PUT, github-credentials PUT) require project the same way;
		// blueprint-import/blueprint-deprecate stay absent because they support
		// a global (superadmin) write, matching create/publish/new-version.
		"schema-pack-claims", "blueprint-updates-status", "blueprint-model-overrides-get",
		"blueprint-updates-check", "blueprint-updates-apply",
		"blueprint-model-overrides-put", "blueprint-github-credentials-put":
		return true
	default:
		return false
	}
}

// denyInstanceContent resolves the share-instance scope for the request and
// returns a JSON-RPC error when the caller is a restricted share instance whose
// allowlist does not cover MCP resources/prompts. Resolution failures fail
// closed. A nil return means the request may proceed.
func (h *Handler) denyInstanceContent(c echo.Context, req *Request, user *auth.AuthUser, kind string) *Response {
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

func (h *Handler) handleResourcesList(c echo.Context, req *Request, user *auth.AuthUser) *Response {
	if resp := h.denyInstanceContent(c, req, user, "Resources"); resp != nil {
		return resp
	}
	resources := h.svc.GetResourceDefinitions()
	result := ResourcesListResult{Resources: resources}

	return NewSuccessResponse(req.ID, result)
}

func (h *Handler) handleResourcesRead(c echo.Context, req *Request, user *auth.AuthUser) *Response {
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

	projectID := user.ProjectID
	if projectID == "" {
		projectID = c.Request().Header.Get("X-Project-ID")
	}

	result, err := h.svc.ReadResource(c.Request().Context(), projectID, params.URI)
	if err != nil {
		h.log.Error("resource read failed",
			slog.String("uri", params.URI),
			logger.Error(err),
		)
		return NewErrorResponse(req.ID, ErrCodeInternalError,
			"Failed to read resource: "+err.Error(),
			map[string]string{"uri": params.URI},
		)
	}

	return NewSuccessResponse(req.ID, result)
}

func (h *Handler) handlePromptsList(c echo.Context, req *Request, user *auth.AuthUser) *Response {
	if resp := h.denyInstanceContent(c, req, user, "Prompts"); resp != nil {
		return resp
	}
	prompts := h.svc.GetPromptDefinitions()
	result := PromptsListResult{Prompts: prompts}

	return NewSuccessResponse(req.ID, result)
}

func (h *Handler) handlePromptsGet(c echo.Context, req *Request, user *auth.AuthUser) *Response {
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

	projectID := user.ProjectID
	if projectID == "" {
		projectID = c.Request().Header.Get("X-Project-ID")
	}

	result, err := h.svc.GetPrompt(c.Request().Context(), projectID, params.Name, params.Arguments)
	if err != nil {
		h.log.Error("prompt get failed",
			slog.String("name", params.Name),
			logger.Error(err),
		)
		return NewErrorResponse(req.ID, ErrCodeInternalError,
			"Failed to get prompt: "+err.Error(),
			map[string]string{"name": params.Name},
		)
	}

	return NewSuccessResponse(req.ID, result)
}
