package mcprelay

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

var upgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin:      func(r *http.Request) bool { return true },
}

// Handler handles HTTP requests for the MCP relay.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler creates a new relay handler.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// -----------------------------------------------------------------------------
// WebSocket endpoint — remote MCP provider connects here
// -----------------------------------------------------------------------------

// Connect handles WebSocket upgrades from remote MCP providers.
//
// @Summary      Connect MCP relay (WebSocket)
// @Description  Remote MCP providers (e.g. Diane) open an outbound WebSocket here. First frame must be a register frame with instance_id and tools list.
// @Tags         mcp-relay
// @Accept       json
// @Produce      json
// @Param        projectId query  string true "Project ID"
// @Success      101
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Router       /api/mcp-relay/connect [get]
func (h *Handler) Connect(c echo.Context) error {
	projectID, err := auth.GetProjectID(c)
	if err != nil || projectID == "" {
		return apperror.ErrBadRequest.WithMessage("missing project context")
	}

	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return nil // upgrader already wrote the error
	}
	defer conn.Close()

	// First frame must be register.
	conn.SetReadDeadline(time.Now().Add(idleTimeout))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil
	}

	var reg RegisterFrame
	if jsonErr := json.Unmarshal(msg, &reg); jsonErr != nil || reg.Type != FrameRegister {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"first frame must be register"}`))
		return nil
	}
	if reg.InstanceID == "" {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"instance_id required"}`))
		return nil
	}

	sess := newSession(projectID, reg.InstanceID, reg.Version, reg.Tools, conn)
	h.svc.Register(sess)
	defer h.svc.Unregister(projectID, reg.InstanceID)

	// Ack registration.
	ack, _ := json.Marshal(map[string]string{"type": "registered", "instance_id": reg.InstanceID})
	conn.WriteMessage(websocket.TextMessage, ack)

	// ── WebSocket-level keepalive ──
	// Use protocol-level ping/pong so that any intermediate proxy (Traefik, Cloudflare, etc.)
	// won't drop the connection due to idle timeout. The gorilla/websocket client libraries
	// (including Diane's) respond to WS pings automatically — no app-level changes needed.
	const (
		pongWait   = 60 * time.Second  // server waits this long for a pong before closing
		pingPeriod = 45 * time.Second  // server sends pings at this interval
	)

	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Start a goroutine that sends WS protocol-level pings on a ticker.
	// Stopped via sess.done when the connection drops or Unregister is called.
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(writeTimeout))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-sess.done:
				return
			}
		}
	}()

	// Read loop — handle response frames and app-level pings.
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var base struct {
			Type FrameType `json:"type"`
		}
		if err := json.Unmarshal(data, &base); err != nil {
			continue
		}

		switch base.Type {
		case FramePing:
			pong, _ := json.Marshal(PingFrame{Type: FramePong})
			conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			conn.WriteMessage(websocket.TextMessage, pong)

		case FrameResponse:
			var resp ResponseFrame
			if err := json.Unmarshal(data, &resp); err == nil {
				sess.deliverResponse(resp)
			}

		default:
			h.log.Debug("mcprelay: unknown frame type", slog.String("type", string(base.Type)))
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// REST endpoints
// -----------------------------------------------------------------------------

// ListSessions returns connected MCP relay instances for the current project.
//
// @Summary      List relay sessions
// @Description  Returns all currently connected MCP relay instances for the project.
// @Tags         mcp-relay
// @Produce      json
// @Success      200 {object} ListSessionsResponse
// @Failure      401 {object} apperror.Error
// @Router       /api/mcp-relay/sessions [get]
func (h *Handler) ListSessions(c echo.Context) error {
	projectID, err := auth.GetProjectID(c)
	if err != nil || projectID == "" {
		return apperror.ErrBadRequest.WithMessage("missing project context")
	}

	sessions := h.svc.ListByProject(projectID)
	items := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, SessionInfo{
			InstanceID:  s.InstanceID,
			Version:     s.Version,
			ToolCount:   len(s.Tools),
			ConnectedAt: s.ConnectedAt,
		})
	}
	return c.JSON(http.StatusOK, ListSessionsResponse{Sessions: items})
}

// GetTools returns the tool list for a connected relay instance.
//
// @Summary      Get relay instance tools
// @Description  Returns the MCP tools/list result for a connected relay instance.
// @Tags         mcp-relay
// @Produce      json
// @Param        instanceId path string true "Instance ID"
// @Success      200 {object} map[string]any
// @Failure      404 {object} apperror.Error
// @Router       /api/mcp-relay/sessions/{instanceId}/tools [get]
func (h *Handler) GetTools(c echo.Context) error {
	projectID, err := auth.GetProjectID(c)
	if err != nil || projectID == "" {
		return apperror.ErrBadRequest.WithMessage("missing project context")
	}

	instanceID := c.Param("instanceId")
	sess, ok := h.svc.Get(projectID, instanceID)
	if !ok {
		return apperror.ErrNotFound.WithMessage("relay instance not found or disconnected")
	}

	return c.JSON(http.StatusOK, sess.Tools)
}

// CallTool forwards a tool call to a connected relay instance and returns the result.
//
// @Summary      Call tool on relay instance
// @Description  Forwards an MCP tool call to the specified relay instance and returns the response.
// @Tags         mcp-relay
// @Accept       json
// @Produce      json
// @Param        instanceId path  string         true "Instance ID"
// @Param        body       body  CallToolRequest true "Tool call request"
// @Success      200 {object} map[string]any
// @Failure      404 {object} apperror.Error
// @Failure      503 {object} apperror.Error
// @Router       /api/mcp-relay/sessions/{instanceId}/call [post]
func (h *Handler) CallTool(c echo.Context) error {
	projectID, err := auth.GetProjectID(c)
	if err != nil || projectID == "" {
		return apperror.ErrBadRequest.WithMessage("missing project context")
	}

	instanceID := c.Param("instanceId")
	sess, ok := h.svc.Get(projectID, instanceID)
	if !ok {
		return apperror.ErrNotFound.WithMessage("relay instance not found or disconnected")
	}

	var req CallToolRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}
	if req.Name == "" {
		return apperror.ErrBadRequest.WithMessage("tool name required")
	}

	reqID := fmt.Sprintf("relay-%d", time.Now().UnixNano())
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      req.Name,
			"arguments": req.Arguments,
		},
	}

	result, callErr := sess.SendRequest(c.Request().Context(), reqID, payload)
	if callErr != nil {
		h.log.Warn("mcprelay: tool call failed",
			slog.String("instance_id", instanceID),
			slog.String("tool", req.Name),
			slog.String("error", callErr.Error()),
		)
		return apperror.ErrServiceUnavailable.WithMessage(callErr.Error())
	}

	return c.JSON(http.StatusOK, result)
}

// -----------------------------------------------------------------------------
// DTOs
// -----------------------------------------------------------------------------

// SessionInfo is a summary of a connected relay session.
type SessionInfo struct {
	InstanceID  string    `json:"instance_id"`
	Version     string    `json:"version,omitempty"`
	ToolCount   int       `json:"tool_count"`
	ConnectedAt time.Time `json:"connected_at"`
}

// ListSessionsResponse is returned by ListSessions.
type ListSessionsResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// CallToolRequest is the request body for CallTool.
type CallToolRequest struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}
