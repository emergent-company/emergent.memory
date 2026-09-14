package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/singleflight"
)

// Build-time metadata injected via -X ldflags (see repo-root Dockerfile and
// gateway/Taskfile.yml). Defaults keep local `go build`/`go test` builds
// informative without any tooling.
var (
	version = "dev"
	commit  = "unknown"
)

type Server struct {
	cfg        Config
	memory     MemoryBackend
	supervisor *Supervisor
	hub        *conversationHub   // live conversation-state push (SSE), nil in bare test servers
	registry   *accountRegistry   // in-memory multi-account registry (nil in bare test servers)
	bindings   *voiceBindingStore // per-room voice bindings (nil in bare test servers)
	// shutdownCh is closed on process shutdown so long-lived SSE streams end
	// promptly instead of holding http.Server.Shutdown until its deadline.
	// nil in bare test servers (selecting on it then blocks forever).
	shutdownCh chan struct{}
	// refreshGroup coalesces concurrent session-token refreshes per refresh
	// token so a rotating token is redeemed exactly once.
	refreshGroup singleflight.Group
	// Provider-presence cache backing projectHasNoProviders: each project's
	// "has no LLM providers" result lives ~10s so page loads skip an extra
	// ListProjectProviders round trip. Nil map = cold cache; reset on provider
	// save/remove. Guarded by missingProvidersMu.
	missingProvidersMu    sync.Mutex
	missingProvidersCache map[string]providerMissingEntry
	// schemaWritePolicy optionally gates schema mutation routes. Nil means every
	// gateway-authenticated caller is allowed (memory enforces token scopes on
	// the mutation endpoints); a deployment or test can inject a stricter policy.
	schemaWritePolicy func(echo.Context) bool
}

// beginShutdown signals long-lived streams to end. Safe to call once.
func (s *Server) beginShutdown() {
	if s.shutdownCh != nil {
		close(s.shutdownCh)
	}
}

func (s *Server) health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version,
		"commit":  commit,
	})
}

func (s *Server) workers(c echo.Context) error {
	return c.JSON(http.StatusOK, s.supervisor.Status())
}

func (s *Server) listAgents(c echo.Context) error {
	agents, err := s.memory.ListAgentDefinitions(c.Request().Context())
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, agents)
}

func (s *Server) getAgent(c echo.Context) error {
	a, err := s.memory.GetAgentDefinition(c.Request().Context(), c.Param("id"))
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	deriveDelegation(a)
	return c.JSON(http.StatusOK, a)
}

func (s *Server) createAgent(c echo.Context) error {
	var in AgentDefinition
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
	}
	if err := applyDelegation(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	a, err := s.memory.CreateAgentDefinition(c.Request().Context(), &in)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusCreated, a)
}

func (s *Server) updateAgent(c echo.Context) error {
	var in AgentDefinition
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if err := applyDelegation(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	a, err := s.memory.UpdateAgentDefinition(c.Request().Context(), c.Param("id"), &in)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, a)
}

func (s *Server) deleteAgent(c echo.Context) error {
	if err := s.memory.DeleteAgentDefinition(c.Request().Context(), c.Param("id")); err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.NoContent(http.StatusNoContent)
}

// setAgentEnabled flips an agent's Enabled flag and persists it to memory.
// Shared by activateAgent/deactivateAgent.
func (s *Server) setAgentEnabled(c echo.Context, enabled bool) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	def, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return agentMemoryError(c, err)
	}
	def.Enabled = enabled
	a, err := s.memory.UpdateAgentDefinition(ctx, id, def)
	if err != nil {
		return agentMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, a)
}

// agentMemoryError maps memory API errors onto gateway status codes: unknown
// agent ids (404/not_found) are 404, everything else is 502.
func agentMemoryError(c echo.Context, err error) error {
	msg := err.Error()
	if isMemoryNotFound(err) || strings.Contains(msg, "invalid conversation id") {
		return c.JSON(http.StatusNotFound, map[string]string{"error": msg})
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": msg})
}

func (s *Server) activateAgent(c echo.Context) error {
	return s.setAgentEnabled(c, true)
}

func (s *Server) deactivateAgent(c echo.Context) error {
	return s.setAgentEnabled(c, false)
}

func (s *Server) chat(c echo.Context) error {
	var in ChatRequest
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if in.Message == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "message required"})
	}
	body, err := s.memory.ChatStream(c.Request().Context(), in)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	defer func() { _ = body.Close() }()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-s.shutdownCh:
			_ = body.Close()
		case <-stop:
		}
	}()
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		if err := rewriteChatStream(pw, body); err != nil {
			captureError(err)
		}
	}()
	return c.Stream(http.StatusOK, "text/event-stream", pr)
}

func (s *Server) respondQuestion(c echo.Context) error {
	var in struct {
		Response string `json:"response"`
		Message  string `json:"message"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if in.Response == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "response required"})
	}
	result, err := s.memory.RespondQuestion(c.Request().Context(), c.Param("questionId"), in.Response, in.Message)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"ok":          true,
		"questionId":  result.QuestionID,
		"resumeRunId": result.ResumeRunID,
	})
}

func (s *Server) cancelQuestion(c echo.Context) error {
	result, err := s.memory.CancelQuestion(c.Request().Context(), c.Param("questionId"))
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"ok":          true,
		"questionId":  result.QuestionID,
		"resumeRunId": result.ResumeRunID,
	})
}

func (s *Server) listConversations(c echo.Context) error {
	out, err := s.memory.ListConversations(c.Request().Context())
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, out)
}

// --- scheduled agents (runtime agents proxied to memory) ---

// scheduledAgentRequest is the create/update body for a scheduled agent.
// strategyType is implied ("definition"); memory has no server default.
type scheduledAgentRequest struct {
	Name              string  `json:"name"`
	Prompt            *string `json:"prompt"`
	CronSchedule      string  `json:"cronSchedule"`
	Enabled           *bool   `json:"enabled"`
	AgentDefinitionID *string `json:"agentDefinitionId"`
	Description       *string `json:"description"`
}

func (s *Server) listScheduledAgents(c echo.Context) error {
	agents, err := s.memory.ListScheduledAgents(c.Request().Context())
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusOK, agents)
}

func (s *Server) getScheduledAgent(c echo.Context) error {
	a, err := s.memory.GetScheduledAgent(c.Request().Context(), c.Param("id"))
	if err != nil {
		return agentMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, a)
}

// bindScheduledAgent validates the create/update body and maps it onto a
// ScheduledAgent with the "definition" strategy. agentDefinitionId is required
// (memory resolves model/system-prompt/tools through the FK).
func (s *Server) bindScheduledAgent(c echo.Context) (*ScheduledAgent, error) {
	var in scheduledAgentRequest
	if err := c.Bind(&in); err != nil {
		return nil, fmt.Errorf("invalid body: %w", err)
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	if strings.TrimSpace(in.CronSchedule) == "" {
		return nil, fmt.Errorf("cronSchedule is required")
	}
	if in.AgentDefinitionID == nil || strings.TrimSpace(*in.AgentDefinitionID) == "" {
		return nil, fmt.Errorf("agentDefinitionId is required — pick an agent definition to run")
	}
	a := &ScheduledAgent{
		StrategyType:      "definition",
		Name:              strings.TrimSpace(in.Name),
		Prompt:            in.Prompt,
		CronSchedule:      strings.TrimSpace(in.CronSchedule),
		TriggerType:       "schedule",
		AgentDefinitionID: in.AgentDefinitionID,
		Description:       in.Description,
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	} else {
		a.Enabled = true // new agents default to enabled
	}
	return a, nil
}

func (s *Server) createScheduledAgent(c echo.Context) error {
	in, err := s.bindScheduledAgent(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	a, err := s.memory.CreateScheduledAgent(c.Request().Context(), in)
	if err != nil {
		captureError(err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "memory service unavailable"})
	}
	return c.JSON(http.StatusCreated, a)
}

func (s *Server) updateScheduledAgent(c echo.Context) error {
	in, err := s.bindScheduledAgent(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	a, err := s.memory.UpdateScheduledAgent(c.Request().Context(), c.Param("id"), in)
	if err != nil {
		return agentMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, a)
}

func (s *Server) enableScheduledAgent(c echo.Context) error {
	a, err := s.memory.EnableScheduledAgent(c.Request().Context(), c.Param("id"))
	if err != nil {
		return agentMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, a)
}

func (s *Server) deleteScheduledAgent(c echo.Context) error {
	if err := s.memory.DeleteScheduledAgent(c.Request().Context(), c.Param("id")); err != nil {
		return agentMemoryError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) triggerScheduledAgent(c echo.Context) error {
	res, err := s.memory.TriggerScheduledAgent(c.Request().Context(), c.Param("id"))
	if err != nil {
		return agentMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, res)
}

func (s *Server) listScheduledAgentRuns(c echo.Context) error {
	runs, err := s.memory.ListScheduledAgentRuns(c.Request().Context(), c.Param("id"))
	if err != nil {
		return agentMemoryError(c, err)
	}
	return c.JSON(http.StatusOK, runs)
}
