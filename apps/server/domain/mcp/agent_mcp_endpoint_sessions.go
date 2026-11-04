package mcp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ============================================================================
// DTOs
// ============================================================================

// AgentMCPSessionView is one row of the project-admin session list for an agent
// MCP endpoint. It exposes lifecycle/counter metadata only: never message
// content and never credential material.
type AgentMCPSessionView struct {
	SessionID    string     `json:"session_id"`
	KeyID        string     `json:"key_id"`
	KeyLabel     string     `json:"key_label"`
	Status       string     `json:"status"`
	TurnCount    int        `json:"turn_count"`
	TotalSteps   int        `json:"total_steps"`
	CreatedAt    time.Time  `json:"created_at"`
	LastActiveAt time.Time  `json:"last_active_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
}

// AgentMCPSessionListResponse is the response for GET
// /api/projects/:projectId/agent-mcp-endpoints/:id/sessions. Sessions is always
// non-null, including when the endpoint has no sessions.
type AgentMCPSessionListResponse struct {
	Sessions []AgentMCPSessionView `json:"sessions"`
}

// ============================================================================
// Pure helpers
// ============================================================================

// validAgentMCPStatusFilter reports whether status names a real session
// lifecycle state.
func validAgentMCPStatusFilter(status string) bool {
	switch status {
	case AgentMCPSessionStatusActive,
		AgentMCPSessionStatusRunning,
		AgentMCPSessionStatusInterrupted,
		AgentMCPSessionStatusExpired:
		return true
	default:
		return false
	}
}

// agentMCPSessionView maps an endpoint-session read row to its wire form.
func agentMCPSessionView(row *AgentMCPSessionDetail) AgentMCPSessionView {
	return AgentMCPSessionView{
		SessionID:    row.SessionRef,
		KeyID:        row.KeyID,
		KeyLabel:     row.KeyLabel,
		Status:       row.Status,
		TurnCount:    row.TurnCount,
		TotalSteps:   row.TotalSteps,
		CreatedAt:    row.CreatedAt,
		LastActiveAt: row.LastActiveAt,
		ExpiresAt:    row.ExpiresAt,
	}
}

// ============================================================================
// Service
// ============================================================================

// ListAgentSessions returns the sessions of one agent MCP endpoint for a project
// admin, most recently active first. An optional status filters by lifecycle
// state; an unrecognized status is a 400. An endpoint id from another project
// resolves as not-found so its existence is never revealed.
func (s *Service) ListAgentSessions(ctx context.Context, projectID, userID, endpointID, status string) (*AgentMCPSessionListResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	statusFilter := strings.TrimSpace(status)
	if statusFilter != "" && !validAgentMCPStatusFilter(statusFilter) {
		return nil, apperror.NewBadRequest("invalid session status: " + statusFilter)
	}

	endpointStore := s.agentEndpointStore()
	sessionStore := s.agentSessionStore()
	if endpointStore == nil || sessionStore == nil {
		return nil, apperror.NewInternal("agent MCP session storage unavailable", nil)
	}

	endpoint, err := endpointStore.GetEndpointByID(ctx, endpointID)
	if err != nil {
		return nil, err
	}
	if endpoint == nil || endpoint.ProjectID != projectID {
		return nil, apperror.NewNotFound("Agent MCP endpoint", endpointID)
	}

	rows, err := sessionStore.ListSessionsByEndpoint(ctx, endpointID, statusFilter)
	if err != nil {
		return nil, err
	}
	out := make([]AgentMCPSessionView, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, agentMCPSessionView(row))
	}
	return &AgentMCPSessionListResponse{Sessions: out}, nil
}

// ============================================================================
// Handler
// ============================================================================

// HandleListAgentSessions handles
// GET /api/projects/:projectId/agent-mcp-endpoints/:id/sessions.
//
// @Summary      List an agent MCP endpoint's sessions
// @Description  Returns session metadata (owning key label, status, turn/step counters, activity and expiry timestamps) for one agent MCP endpoint. Message content and credentials are never returned.
// @Tags         mcp
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        id path string true "Endpoint ID (UUID)"
// @Param        status query string false "Filter by session status (active, running, interrupted, expired)"
// @Success      200 {object} AgentMCPSessionListResponse
// @Failure      400 {object} apperror.Error "Invalid status filter"
// @Failure      403 {object} apperror.Error "Project admin required"
// @Failure      404 {object} apperror.Error "Endpoint not found"
// @Router       /api/projects/{projectId}/agent-mcp-endpoints/{id}/sessions [get]
// @Security     bearerAuth
func (h *Handler) HandleListAgentSessions(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	endpointID := c.Param("id")
	if projectID == "" || endpointID == "" {
		return apperror.NewBadRequest("projectId and id are required")
	}
	resp, err := h.svc.ListAgentSessions(c.Request().Context(), projectID, user.ID, endpointID, c.QueryParam("status"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
