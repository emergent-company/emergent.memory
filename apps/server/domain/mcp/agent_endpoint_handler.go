package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// AgentEndpointHandler implements the per-agent MCP endpoint at
// /api/mcp/agents/:agentId. It exposes a FIXED five-tool catalog — call_agent
// plus the four persistent-session tools — and authenticates via a credential
// bound to that agent. There is no per-endpoint tool picking and no project
// tool surface.
type AgentEndpointHandler struct {
	svc *Service
	log *slog.Logger
}

// NewAgentEndpointHandler creates a new per-agent MCP handler.
func NewAgentEndpointHandler(svc *Service, log *slog.Logger) *AgentEndpointHandler {
	return &AgentEndpointHandler{
		svc: svc,
		log: log.With(logger.Scope("mcp.agent_endpoint")),
	}
}

// The fixed five-tool catalog exposed by the per-agent endpoint.
const (
	agentCallToolName            = "call_agent"
	agentStartSessionToolName    = "start_session"
	agentContinueSessionToolName = "continue_session"
	agentGetSessionToolName      = "get_session"
	agentListSessionsToolName    = "list_sessions"
)

// HandleAgentEndpoint handles POST /api/mcp/agents/:agentId.
func (h *AgentEndpointHandler) HandleAgentEndpoint(c echo.Context) error {
	user := auth.MustGetUser(c)
	agentID := c.Param("agentId")
	if agentID == "" {
		return c.JSON(http.StatusBadRequest, NewErrorResponse(nil, ErrCodeInvalidParams, "agentId is required", nil))
	}

	// Only credentials minted for an agent share carry the marker scope. Normal
	// project tokens MUST NOT be able to reach the per-agent endpoint.
	if !hasAgentCallScope(user.Scopes) {
		return c.JSON(http.StatusForbidden, NewErrorResponse(nil, ErrCodeForbidden, "mcp:agent-call scope required", nil))
	}

	var req Request
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusOK, NewErrorResponse(nil, ErrCodeParseError, "Failed to parse JSON-RPC request", map[string]string{"error": err.Error()}))
	}
	if req.JSONRPC != "2.0" {
		return c.JSON(http.StatusOK, NewErrorResponse(req.ID, ErrCodeInvalidRequest, "Invalid JSON-RPC version. Must be \"2.0\"", nil))
	}

	// Authorize before handling notifications: an unbound or foreign credential
	// must never receive an accepted (202) response, even for notifications/*.
	// This re-authorization runs on EVERY request, including every session tool
	// call, so a revoked/rotated key is rejected immediately and the key identity
	// used to scope sessions is always freshly resolved.
	endpoint, key, err := h.svc.AuthorizeAgentEndpoint(c.Request().Context(), user.APITokenID, agentID)
	if err != nil {
		return c.JSON(http.StatusForbidden, NewErrorResponse(req.ID, ErrCodeForbidden, err.Error(), nil))
	}

	// Notifications (e.g. notifications/initialized) are accepted without a response.
	if req.IsNotification() {
		return c.NoContent(http.StatusAccepted)
	}

	ctx := c.Request().Context()
	var response *Response
	switch req.Method {
	case "initialize":
		response = h.handleInitialize(&req)
	case "tools/list":
		response = h.handleToolsList(ctx, &req, endpoint)
	case "tools/call":
		response = h.handleToolsCall(ctx, &req, endpoint, key)
	default:
		response = NewErrorResponse(req.ID, ErrCodeMethodNotFound, "Method not found: "+req.Method, map[string]any{
			"method":            req.Method,
			"supported_methods": []string{"initialize", "tools/list", "tools/call"},
		})
	}
	return c.JSON(http.StatusOK, response)
}

// handleInitialize returns server capabilities without persisting session state.
func (h *AgentEndpointHandler) handleInitialize(req *Request) *Response {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid initialize params", map[string]string{"error": err.Error()})
		}
	}
	if params.ProtocolVersion == "" || params.ClientInfo.Name == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required parameters: protocolVersion, clientInfo", map[string]any{
			"required": []string{"protocolVersion", "clientInfo"},
		})
	}
	if !isSupportedProtocolVersion(params.ProtocolVersion) {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Unsupported protocol version: "+params.ProtocolVersion, map[string]any{
			"requested": params.ProtocolVersion,
			"supported": SupportedProtocolVersions,
		})
	}
	return NewSuccessResponse(req.ID, InitializeResult{
		ProtocolVersion: params.ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools:     ToolsCapability{ListChanged: false},
			Resources: ResourcesCapability{Subscribe: false, ListChanged: false},
			Prompts:   PromptsCapability{ListChanged: false},
		},
		ServerInfo: ServerInfo,
	})
}

// handleToolsList returns the fixed five-tool catalog.
func (h *AgentEndpointHandler) handleToolsList(ctx context.Context, req *Request, endpoint *AgentMCPEndpoint) *Response {
	name := ""
	if agent, err := h.svc.resolveProjectAgent(ctx, endpoint.ProjectID, endpoint.AgentID); err == nil && agent != nil {
		name = agent.Name
	}
	return NewSuccessResponse(req.ID, ToolsListResult{Tools: agentEndpointToolDefinitions(name)})
}

// handleToolsCall dispatches one tool from the fixed catalog. An unknown tool
// name is a method/tool-not-found error and executes nothing.
func (h *AgentEndpointHandler) handleToolsCall(ctx context.Context, req *Request, endpoint *AgentMCPEndpoint, key *AgentMCPKey) *Response {
	var params ToolsCallParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid tools/call params", map[string]string{"error": err.Error()})
		}
	}
	if params.Name == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required parameter: name", map[string]any{"required": []string{"name"}})
	}

	switch params.Name {
	case agentCallToolName:
		return h.handleCallAgent(ctx, req, params, endpoint)
	case agentStartSessionToolName:
		message, _ := params.Arguments["message"].(string)
		return h.toolResult(req, h.svc.StartSession(ctx, endpoint, key, message))
	case agentContinueSessionToolName:
		sessionID, _ := params.Arguments["session_id"].(string)
		message, _ := params.Arguments["message"].(string)
		if strings.TrimSpace(sessionID) == "" {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required argument: session_id", map[string]any{"required": []string{"session_id", "message"}})
		}
		if strings.TrimSpace(message) == "" {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required argument: message", map[string]any{"required": []string{"session_id", "message"}})
		}
		return h.toolResult(req, h.svc.ContinueSession(ctx, endpoint, key, sessionID, message))
	case agentGetSessionToolName:
		sessionID, _ := params.Arguments["session_id"].(string)
		if strings.TrimSpace(sessionID) == "" {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required argument: session_id", map[string]any{"required": []string{"session_id"}})
		}
		return h.toolResult(req, h.svc.GetSession(ctx, endpoint, key, sessionID))
	case agentListSessionsToolName:
		return h.toolResult(req, h.svc.ListSessions(ctx, endpoint, key))
	default:
		return NewErrorResponse(req.ID, ErrCodeMethodNotFound, "Tool not found: "+params.Name, nil)
	}
}

// handleCallAgent runs the one-shot path. Its result is deliberately NOT
// enveloped: call_agent stays bare text with agentRunErrorResult errors for
// back-compat with existing clients.
func (h *AgentEndpointHandler) handleCallAgent(ctx context.Context, req *Request, params ToolsCallParams, endpoint *AgentMCPEndpoint) *Response {
	message, _ := params.Arguments["message"].(string)
	message = strings.TrimSpace(message)
	if message == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required argument: message", map[string]any{"required": []string{"message"}})
	}
	result := h.svc.CallAgentOnce(ctx, endpoint.ProjectID, endpoint.AgentID, message)
	return NewSuccessResponse(req.ID, result)
}

// toolResult wraps a session tool's envelope ToolResult in a JSON-RPC response.
func (h *AgentEndpointHandler) toolResult(req *Request, result *ToolResult) *Response {
	return NewSuccessResponse(req.ID, result)
}

// agentEndpointToolDefinitions builds the fixed five-tool catalog. The bound
// agent's name, when known, is woven into the descriptions.
func agentEndpointToolDefinitions(agentName string) []ToolDefinition {
	target := "this agent"
	if strings.TrimSpace(agentName) != "" {
		target = fmt.Sprintf("the %q agent", agentName)
	}
	return []ToolDefinition{
		{
			Name:        agentCallToolName,
			Description: fmt.Sprintf("Send a message to %s and receive its reply. One-shot: each call is independent.", target),
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"message": {Type: "string", Description: "The message to send to the agent."},
				},
				Required: []string{"message"},
			},
		},
		{
			Name:        agentStartSessionToolName,
			Description: fmt.Sprintf("Start a persistent conversation session with %s. With a message, runs the first turn and returns the reply; without one, creates an empty session.", target),
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"message": {Type: "string", Description: "Optional first message. Omit to create an empty session."},
				},
			},
			OutputSchema: envelopeOutputSchema(),
		},
		{
			Name:        agentContinueSessionToolName,
			Description: fmt.Sprintf("Continue an existing session with %s. The turn sees the session's prior conversation context.", target),
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"session_id": {Type: "string", Description: "The session id returned by start_session."},
					"message":    {Type: "string", Description: "The message to send in this turn."},
				},
				Required: []string{"session_id", "message"},
			},
			OutputSchema: envelopeOutputSchema(),
		},
		{
			Name:        agentGetSessionToolName,
			Description: "Get the status, timestamps, and turn count of one of this key's sessions.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"session_id": {Type: "string", Description: "The session id to look up."},
				},
				Required: []string{"session_id"},
			},
			OutputSchema: envelopeOutputSchema(),
		},
		{
			Name:        agentListSessionsToolName,
			Description: "List the sessions created by this credential, most recently active first.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
			},
			OutputSchema: envelopeOutputSchema(),
		},
	}
}

// isSupportedProtocolVersion reports whether v is a known MCP protocol version.
func isSupportedProtocolVersion(v string) bool {
	for _, supported := range SupportedProtocolVersions {
		if v == supported {
			return true
		}
	}
	return false
}
