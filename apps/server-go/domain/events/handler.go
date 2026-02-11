package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/emergent/emergent-core/pkg/logger"
)

const (
	// HeartbeatInterval is how often to send heartbeat events
	HeartbeatInterval = 30 * time.Second
)

// Handler handles SSE connections for real-time events
type Handler struct {
	svc         *Service
	log         *slog.Logger
	connections map[string]*SSEConnection
	connMu      sync.RWMutex

	// Heartbeat management
	heartbeatCtx    context.Context
	heartbeatCancel context.CancelFunc
}

// NewHandler creates a new events handler
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	ctx, cancel := context.WithCancel(context.Background())

	h := &Handler{
		svc:             svc,
		log:             log.With(logger.Scope("events.handler")),
		connections:     make(map[string]*SSEConnection),
		heartbeatCtx:    ctx,
		heartbeatCancel: cancel,
	}

	// Start heartbeat goroutine
	go h.heartbeatLoop()

	return h
}

// Stop stops the handler and cleans up resources
func (h *Handler) Stop() {
	h.heartbeatCancel()

	// Close all connections
	h.connMu.Lock()
	defer h.connMu.Unlock()

	for connID, conn := range h.connections {
		close(conn.Done)
		delete(h.connections, connID)
	}
}

// heartbeatLoop sends periodic heartbeats to all connections
func (h *Handler) heartbeatLoop() {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.heartbeatCtx.Done():
			return
		case <-ticker.C:
			h.sendHeartbeats()
		}
	}
}

// sendHeartbeats sends a heartbeat to all active connections
func (h *Handler) sendHeartbeats() {
	h.connMu.RLock()
	connections := make([]*SSEConnection, 0, len(h.connections))
	for _, conn := range h.connections {
		connections = append(connections, conn)
	}
	h.connMu.RUnlock()

	if len(connections) == 0 {
		return
	}

	now := time.Now().UTC()
	heartbeat := HeartbeatEvent{
		Timestamp: now.Format(time.RFC3339),
		// Health status could be fetched here from health service if needed
	}

	for _, conn := range connections {
		select {
		case <-conn.Done:
			continue
		default:
			if err := h.sendEvent(conn, "heartbeat", heartbeat); err != nil {
				h.log.Warn("failed to send heartbeat",
					slog.String("connection_id", conn.ConnectionID),
					logger.Error(err),
				)
				// Remove failed connection
				h.removeConnection(conn.ConnectionID)
			} else {
				conn.LastHeartbeat = now
			}
		}
	}
}

// HandleStream handles GET /api/events/stream - SSE connection endpoint
// @Summary      Subscribe to real-time events
// @Description  Establish Server-Sent Events (SSE) connection to receive real-time entity updates for a project (documents, chunks, extraction jobs, graph objects, notifications). Connection requires projectId query parameter and sends periodic heartbeats.
// @Tags         events
// @Produce      text/event-stream
// @Param        projectId query string true "Project ID to subscribe to"
// @Success      200 {string} string "SSE stream (events: connected, entity.created, entity.updated, entity.deleted, entity.batch, heartbeat)"
// @Failure      400 {object} apperror.Error "Missing projectId parameter"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/events/stream [get]
// @Security     bearerAuth
func (h *Handler) HandleStream(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// Get projectId from query param (required)
	projectID := c.QueryParam("projectId")
	if projectID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Missing projectId query parameter",
		})
	}

	// Generate connection ID
	connectionID := h.generateConnectionID()

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

	// Create connection
	conn := &SSEConnection{
		ConnectionID:  connectionID,
		UserID:        user.ID,
		ProjectID:     projectID,
		Writer:        w,
		Flusher:       flusher,
		Done:          make(chan struct{}),
		LastHeartbeat: time.Now(),
	}

	// Store connection
	h.connMu.Lock()
	h.connections[connectionID] = conn
	h.connMu.Unlock()

	defer h.removeConnection(connectionID)

	h.log.Info("SSE connection established",
		slog.String("connection_id", connectionID),
		slog.String("project_id", projectID),
		slog.String("user_id", user.ID),
	)

	// Send connected event
	connectedEvent := ConnectedEvent{
		ConnectionID: connectionID,
		ProjectID:    projectID,
	}
	if err := h.sendEvent(conn, "connected", connectedEvent); err != nil {
		h.log.Error("failed to send connected event", logger.Error(err))
		return nil
	}

	// Subscribe to project events
	unsubscribe := h.svc.Subscribe(projectID, func(event EntityEvent) {
		select {
		case <-conn.Done:
			return
		default:
			payload := SSEEventPayload{
				Entity:    event.Entity,
				ID:        event.ID,
				IDs:       event.IDs,
				Data:      event.Data,
				Timestamp: event.Timestamp,
			}
			if err := h.sendEvent(conn, string(event.Type), payload); err != nil {
				h.log.Warn("failed to send event to connection",
					slog.String("connection_id", connectionID),
					logger.Error(err),
				)
			}
		}
	})
	defer unsubscribe()

	// Wait for client disconnect
	ctx := c.Request().Context()
	select {
	case <-ctx.Done():
		h.log.Info("SSE connection closed (client disconnected)",
			slog.String("connection_id", connectionID),
		)
	case <-conn.Done:
		h.log.Info("SSE connection closed (server closed)",
			slog.String("connection_id", connectionID),
		)
	}

	return nil
}

// HandleConnectionsCount handles GET /api/events/connections/count
// @Summary      Get active SSE connection count
// @Description  Returns the current number of active SSE connections to the events stream (for monitoring purposes)
// @Tags         events
// @Produce      json
// @Success      200 {object} map[string]int "Active connection count"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/events/connections/count [get]
// @Security     bearerAuth
func (h *Handler) HandleConnectionsCount(c echo.Context) error {
	h.connMu.RLock()
	count := len(h.connections)
	h.connMu.RUnlock()

	return c.JSON(http.StatusOK, map[string]int{
		"count": count,
	})
}

// sendEvent sends an SSE event to a connection
func (h *Handler) sendEvent(conn *SSEConnection, event string, data any) error {
	select {
	case <-conn.Done:
		return fmt.Errorf("connection closed")
	default:
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	fmt.Fprintf(conn.Writer, "event: %s\n", event)
	fmt.Fprintf(conn.Writer, "data: %s\n\n", jsonData)
	conn.Flusher.Flush()

	return nil
}

// removeConnection removes a connection from the map
func (h *Handler) removeConnection(connectionID string) {
	h.connMu.Lock()
	defer h.connMu.Unlock()

	if conn, ok := h.connections[connectionID]; ok {
		select {
		case <-conn.Done:
			// Already closed
		default:
			close(conn.Done)
		}
		delete(h.connections, connectionID)
	}
}

// generateConnectionID creates a unique connection ID
func (h *Handler) generateConnectionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("sse_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(bytes)[:12])
}
