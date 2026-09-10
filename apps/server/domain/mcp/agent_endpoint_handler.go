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
// /api/mcp/agents/:agentId. It exposes exactly one tool, call_agent, and
// authenticates via a credential bound to that agent. It is stateless: no
// server-side session state is required between calls.
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

// agentCallToolName is the single tool exposed by the per-agent endpoint.
const agentCallToolName = "call_agent"

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
	share, err := h.svc.AuthorizeAgentShare(c.Request().Context(), user.APITokenID, agentID)
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
		response = h.handleToolsList(ctx, &req, share)
	case "tools/call":
		response = h.handleToolsCall(ctx, &req, share)
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

// handleToolsList returns exactly one call_agent tool definition.
func (h *AgentEndpointHandler) handleToolsList(ctx context.Context, req *Request, share *AgentMCPShare) *Response {
	name := ""
	if agent, err := h.svc.resolveProjectAgent(ctx, share.ProjectID, share.AgentID); err == nil && agent != nil {
		name = agent.Name
	}
	return NewSuccessResponse(req.ID, ToolsListResult{Tools: []ToolDefinition{agentCallToolDefinition(name)}})
}

// handleToolsCall executes call_agent or rejects any other tool.
func (h *AgentEndpointHandler) handleToolsCall(ctx context.Context, req *Request, share *AgentMCPShare) *Response {
	var params ToolsCallParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Invalid tools/call params", map[string]string{"error": err.Error()})
		}
	}
	if params.Name == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required parameter: name", map[string]any{"required": []string{"name"}})
	}
	if params.Name != agentCallToolName {
		return NewErrorResponse(req.ID, ErrCodeMethodNotFound, "Tool not found: "+params.Name, nil)
	}

	message, _ := params.Arguments["message"].(string)
	message = strings.TrimSpace(message)
	if message == "" {
		return NewErrorResponse(req.ID, ErrCodeInvalidParams, "Missing required argument: message", map[string]any{"required": []string{"message"}})
	}

	result := h.svc.CallAgentOnce(ctx, share.ProjectID, share.AgentID, message)
	return NewSuccessResponse(req.ID, result)
}

// agentCallToolDefinition builds the single call_agent tool definition. The
// description identifies the bound agent when its name is known.
func agentCallToolDefinition(agentName string) ToolDefinition {
	desc := "Send a message to this agent and receive its reply."
	if strings.TrimSpace(agentName) != "" {
		desc = fmt.Sprintf("Send a message to the %q agent and receive its reply.", agentName)
	}
	return ToolDefinition{
		Name:        agentCallToolName,
		Description: desc,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"message": {
					Type:        "string",
					Description: "The message to send to the agent.",
				},
			},
			Required: []string{"message"},
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
