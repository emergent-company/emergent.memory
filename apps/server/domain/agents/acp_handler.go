package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/sse"
)

// ACPHandler handles ACP v1 HTTP requests.
type ACPHandler struct {
	repo      *Repository
	executor  *AgentExecutor
	eventsSvc *events.Service
	log       *slog.Logger
}

// NewACPHandler creates a new ACP handler.
func NewACPHandler(repo *Repository, executor *AgentExecutor, eventsSvc *events.Service, log *slog.Logger) *ACPHandler {
	return &ACPHandler{repo: repo, executor: executor, eventsSvc: eventsSvc, log: log}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// acpProjectID extracts the project ID from the authenticated user context.
func acpProjectID(c echo.Context) (string, error) {
	user := auth.GetUser(c)
	if user == nil {
		return "", apperror.ErrUnauthorized
	}
	if user.ProjectID == "" {
		return "", apperror.NewBadRequest("project context is required (set via API token)")
	}
	return user.ProjectID, nil
}

// acpBaseURL returns the scheme+host base URL for the current request,
// used to build absolute history URLs in ACP session responses.
func acpBaseURL(c echo.Context) string {
	req := c.Request()
	scheme := "https"
	if req.TLS == nil {
		// Check X-Forwarded-Proto (set by reverse proxy / Traefik)
		if proto := req.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + req.Host
}

// acpOrgID resolves the org ID for the project, used for executor requests.
func (h *ACPHandler) acpOrgID(ctx context.Context, c echo.Context, projectID string) string {
	user := auth.GetUser(c)
	if user != nil && user.OrgID != "" {
		return user.OrgID
	}
	orgID, _ := h.repo.GetOrgIDByProjectID(ctx, projectID)
	return orgID
}

// resolveAgentBySlug looks up an agent definition by ACP slug,
// then resolves the corresponding runtime Agent record.
// It first searches external-visibility definitions, then falls back to all
// definitions so that agents not explicitly marked external are still reachable.
// Returns (def, agent, error). Returns 404 apperror when not found.
func (h *ACPHandler) resolveAgentBySlug(ctx context.Context, projectID, slug string) (*AgentDefinition, *Agent, error) {
	def, err := h.resolveAgentDefinitionBySlug(ctx, projectID, slug)
	if err != nil {
		return nil, nil, err
	}

	agent, err := h.repo.FindByName(ctx, projectID, def.Name)
	if err != nil {
		return nil, nil, apperror.NewInternal("failed to look up runtime agent", err)
	}
	if agent == nil {
		return nil, nil, apperror.NewNotFound("Agent", slug)
	}

	return def, agent, nil
}

// resolveAgentDefinitionBySlug looks up an agent definition by ACP slug.
func (h *ACPHandler) resolveAgentDefinitionBySlug(ctx context.Context, projectID, slug string) (*AgentDefinition, error) {
	// Try external-visibility first (preferred ACP agents)
	def, err := h.repo.FindExternalAgentBySlug(ctx, projectID, slug)
	if err != nil {
		return nil, apperror.NewInternal("failed to look up agent", err)
	}
	// Fall back to any visibility so agents without explicit external visibility
	// are still accessible via ACP (fixes 404 for project/internal agents).
	if def == nil {
		def, err = h.repo.FindAgentDefinitionBySlug(ctx, projectID, slug)
		if err != nil {
			return nil, apperror.NewInternal("failed to look up agent", err)
		}
	}
	if def == nil {
		return nil, apperror.NewNotFound("Agent", slug)
	}
	return def, nil
}

// acpUserMessageFromParts converts ACP message parts to a plain-text user message string.
func acpUserMessageFromParts(parts []ACPMessagePart) string {
	var text string
	for _, p := range parts {
		if p.ContentType == "" || p.ContentType == "text/plain" {
			text += p.Content
		}
	}
	return text
}

// isTerminalACPStatus returns true if the ACP status represents a terminal state.
func isTerminalACPStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

// persistACPEvent is a convenience wrapper that inserts an ACP run event and logs on error.
func (h *ACPHandler) persistACPEvent(ctx context.Context, runID, eventType string, data map[string]any) {
	event := &ACPRunEvent{
		RunID:     runID,
		EventType: eventType,
		Data:      data,
	}
	if err := h.repo.InsertACPRunEvent(ctx, event); err != nil {
		h.log.Warn("failed to persist ACP run event",
			slog.String("run_id", runID),
			slog.String("event_type", eventType),
			slog.String("error", err.Error()),
		)
	}
}

// emitToSSEBus publishes an ACP run event onto the main events.Service SSE bus so
// that any client subscribed to /api/events/stream?projectId=X&runId=Y receives it
// in real-time without polling. The payload key matches the ACP spec discriminated
// union (e.g. "run" for run.* events, "part" for message.part).
func (h *ACPHandler) emitToSSEBus(projectID, runID, eventType string, payload map[string]any) {
	if h.eventsSvc == nil || projectID == "" {
		return
	}
	busPayload := ACPSSEBusPayload{
		Type:    eventType,
		RunID:   runID,
		Payload: payload,
	}
	// Encode the bus payload as map[string]any so it fits into EntityEvent.Data.
	data := map[string]any{
		"type":    busPayload.Type,
		"run_id":  busPayload.RunID,
		"payload": busPayload.Payload,
	}
	h.eventsSvc.EmitCreated(events.EntityAgentRun, runID, projectID, &events.EmitOptions{Data: data})
}

// ---------------------------------------------------------------------------
// 4.2 Ping — GET /acp/v1/ping (no auth)
// ---------------------------------------------------------------------------

// Ping is a simple health check endpoint for ACP clients.
func (h *ACPHandler) Ping(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{})
}

// ---------------------------------------------------------------------------
// 4.3 ListAgents — GET /acp/v1/agents
// ---------------------------------------------------------------------------

// ListAgents returns ACP manifests for all external agent definitions.
func (h *ACPHandler) ListAgents(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	defs, err := h.repo.FindExternalAgentDefinitions(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to list agents", err)
	}

	manifests := make([]ACPAgentManifest, 0, len(defs))
	for _, def := range defs {
		metrics, _ := h.repo.GetAgentStatusMetrics(c.Request().Context(), def.ID)
		manifests = append(manifests, AgentDefinitionToManifest(def, metrics))
	}

	return c.JSON(http.StatusOK, manifests)
}

// ---------------------------------------------------------------------------
// 4.4 GetAgent — GET /acp/v1/agents/:name
// ---------------------------------------------------------------------------

// GetAgent returns a single ACP agent manifest by slug name.
func (h *ACPHandler) GetAgent(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	slug := c.Param("name")
	if slug == "" {
		return apperror.NewBadRequest("agent name is required")
	}

	def, err := h.repo.FindExternalAgentBySlug(c.Request().Context(), projectID, slug)
	if err != nil {
		return apperror.NewInternal("failed to look up agent", err)
	}
	if def == nil {
		return apperror.NewNotFound("Agent", slug)
	}

	metrics, _ := h.repo.GetAgentStatusMetrics(c.Request().Context(), def.ID)
	manifest := AgentDefinitionToManifest(def, metrics)

	return c.JSON(http.StatusOK, manifest)
}

// ---------------------------------------------------------------------------
// 4.5–4.9 CreateRun — POST /acp/v1/agents/:name/runs
// ---------------------------------------------------------------------------

// CreateRun creates and executes an agent run via ACP. Supports sync/async/stream modes.
func (h *ACPHandler) CreateRun(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	slug := c.Param("name")
	if slug == "" {
		return apperror.NewBadRequest("agent name is required")
	}

	ctx := c.Request().Context()

	// Resolve agent
	def, agent, err := h.resolveAgentBySlug(ctx, projectID, slug)
	if err != nil {
		return err
	}

	// Parse request
	var req ACPCreateRunRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Default mode is sync
	mode := req.Mode
	if mode == "" {
		mode = "sync"
	}
	if mode != "sync" && mode != "async" && mode != "stream" {
		return apperror.NewBadRequest("mode must be sync, async, or stream")
	}

	// Validate session_id if provided
	if req.SessionID != nil {
		session, err := h.repo.GetACPSession(ctx, projectID, *req.SessionID)
		if err != nil {
			return apperror.NewInternal("failed to validate session", err)
		}
		if session == nil {
			return apperror.NewBadRequest("session_id does not exist")
		}
	}

	// Extract user message from ACP message parts
	userMessage := acpUserMessageFromParts(req.Message)
	if userMessage == "" {
		return apperror.NewBadRequest("message content is required")
	}

	// Build trigger metadata
	triggerSource := "acp"
	orgID := h.acpOrgID(ctx, c, projectID)

	// Create the run record
	run, err := h.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:        agent.ID,
		TriggerSource:  &triggerSource,
		TriggerMessage: &userMessage,
	})
	if err != nil {
		return apperror.NewInternal("failed to create run", err)
	}

	// Link to ACP session if provided
	if req.SessionID != nil {
		run.ACPSessionID = req.SessionID
		if _, err := h.repo.db.NewUpdate().
			Model(run).
			Column("acp_session_id").
			Where("id = ?", run.ID).
			Exec(ctx); err != nil {
			h.log.Warn("failed to link run to ACP session",
				slog.String("run_id", run.ID),
				slog.String("session_id", *req.SessionID),
				slog.String("error", err.Error()),
			)
		}
	}

	user := auth.GetUser(c)
	var userID string
	if user != nil {
		userID = user.ID
	}

	execReq := ExecuteRequest{
		Agent:           agent,
		AgentDefinition: def,
		ProjectID:       projectID,
		OrgID:           orgID,
		UserID:          userID, // propagate for ask_user notifications
		UserMessage:     userMessage,
		EnvVars:         req.EnvVars,
	}

	// Propagate the authenticated user ID so ask_user notifications target them.
	if u := auth.GetUser(c); u != nil {
		execReq.UserID = u.ID
	}

	switch mode {
	case "async":
		return h.createRunAsync(c, ctx, run, execReq, def)
	case "sync":
		return h.createRunSync(c, ctx, run, execReq, def)
	case "stream":
		return h.createRunStream(c, ctx, run, execReq, def)
	}

	return nil
}

// createRunAsync enqueues the run and returns 202 immediately.
func (h *ACPHandler) createRunAsync(c echo.Context, ctx context.Context, run *AgentRun, execReq ExecuteRequest, def *AgentDefinition) error {
	projectID := execReq.ProjectID
	agentSlug := ACPSlugFromName(def.Name)

	// Persist creation event
	createdData := map[string]any{
		"run": map[string]any{"run_id": run.ID, "agent_name": agentSlug, "status": ACPStatusSubmitted},
	}
	h.persistACPEvent(ctx, run.ID, ACPEventRunCreated, createdData)
	h.emitToSSEBus(projectID, run.ID, ACPEventRunCreated, createdData)

	// Execute in background
	go func() {
		bgCtx := context.Background()
		inProgressData := map[string]any{
			"run": map[string]any{"run_id": run.ID, "agent_name": agentSlug, "status": ACPStatusWorking},
		}
		h.persistACPEvent(bgCtx, run.ID, ACPEventRunInProgress, inProgressData)
		h.emitToSSEBus(projectID, run.ID, ACPEventRunInProgress, inProgressData)

		execReq.StreamCallback = h.makeEventPersistingCallback(bgCtx, run.ID, projectID)
		result, execErr := h.executor.ExecuteWithRun(bgCtx, run, execReq)
		if result != nil && result.Cleanup != nil {
			result.Cleanup()
		}

		if execErr != nil {
			h.log.Error("ACP async run failed",
				slog.String("run_id", run.ID),
				slog.String("error", execErr.Error()),
			)
			data := map[string]any{
				"run": map[string]any{"run_id": run.ID, "error": map[string]any{"message": execErr.Error()}},
			}
			h.persistACPEvent(bgCtx, run.ID, ACPEventRunFailed, data)
			h.emitToSSEBus(projectID, run.ID, ACPEventRunFailed, data)
		} else {
			terminalEvent := ACPEventRunCompleted
			acpStatus := ACPStatusCompleted
			if result != nil && result.Status == RunStatusPaused {
				terminalEvent = ACPEventRunAwaiting
				acpStatus = ACPStatusInputRequired
			} else if result != nil && result.Status == RunStatusError {
				terminalEvent = ACPEventRunFailed
				acpStatus = ACPStatusFailed
			}
			data := map[string]any{
				"run": map[string]any{"run_id": run.ID, "status": acpStatus},
			}
			h.persistACPEvent(bgCtx, run.ID, terminalEvent, data)
			h.emitToSSEBus(projectID, run.ID, terminalEvent, data)
		}
	}()

	// Return the run object immediately with submitted status
	now := run.CreatedAt
	acpRun := ACPRunObject{
		ID:        run.ID,
		AgentName: agentSlug,
		Status:    ACPStatusSubmitted,
		CreatedAt: run.CreatedAt,
		UpdatedAt: &now,
	}
	return c.JSON(http.StatusAccepted, acpRun)
}

// createRunSync blocks until the run completes and returns the final state.
func (h *ACPHandler) createRunSync(c echo.Context, ctx context.Context, run *AgentRun, execReq ExecuteRequest, def *AgentDefinition) error {
	projectID := execReq.ProjectID
	agentSlug := ACPSlugFromName(def.Name)
	bgCtx := context.Background()

	createdData := map[string]any{"run": map[string]any{"run_id": run.ID, "agent_name": agentSlug, "status": ACPStatusSubmitted}}
	h.persistACPEvent(bgCtx, run.ID, ACPEventRunCreated, createdData)
	h.emitToSSEBus(projectID, run.ID, ACPEventRunCreated, createdData)

	inProgressData := map[string]any{"run": map[string]any{"run_id": run.ID, "status": ACPStatusWorking}}
	h.persistACPEvent(bgCtx, run.ID, ACPEventRunInProgress, inProgressData)
	h.emitToSSEBus(projectID, run.ID, ACPEventRunInProgress, inProgressData)

	execReq.StreamCallback = h.makeEventPersistingCallback(bgCtx, run.ID, projectID)

	result, execErr := h.executor.ExecuteWithRun(ctx, run, execReq)
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}

	if execErr != nil {
		h.log.Error("ACP sync run failed",
			slog.String("run_id", run.ID),
			slog.String("error", execErr.Error()),
		)
		data := map[string]any{"run": map[string]any{"run_id": run.ID, "error": map[string]any{"message": execErr.Error()}}}
		h.persistACPEvent(bgCtx, run.ID, ACPEventRunFailed, data)
		h.emitToSSEBus(projectID, run.ID, ACPEventRunFailed, data)
	} else if result != nil {
		termEvent := ACPEventRunCompleted
		acpStatus := MapMemoryStatusToACP(result.Status)
		if result.Status == RunStatusPaused {
			termEvent = ACPEventRunAwaiting
		} else if result.Status == RunStatusError {
			termEvent = ACPEventRunFailed
		}
		data := map[string]any{"run": map[string]any{"run_id": run.ID, "status": acpStatus}}
		h.persistACPEvent(bgCtx, run.ID, termEvent, data)
		h.emitToSSEBus(projectID, run.ID, termEvent, data)
	}

	// Re-fetch the run to get the final state
	return h.respondWithRunObject(c, run.ID, def)
}

// createRunStream sets up SSE and streams events inline on the response.
func (h *ACPHandler) createRunStream(c echo.Context, ctx context.Context, run *AgentRun, execReq ExecuteRequest, def *AgentDefinition) error {
	bgCtx := context.Background()
	projectID := execReq.ProjectID
	agentSlug := ACPSlugFromName(def.Name)

	writer := sse.NewWriter(c.Response().Writer)
	if err := writer.Start(); err != nil {
		return apperror.NewInternal("SSE streaming not supported", err)
	}

	// Send run.created
	createdData := map[string]any{"run": map[string]any{"run_id": run.ID, "agent_name": agentSlug, "status": ACPStatusSubmitted}}
	h.persistACPEvent(bgCtx, run.ID, ACPEventRunCreated, createdData)
	h.emitToSSEBus(projectID, run.ID, ACPEventRunCreated, createdData)
	_ = writer.WriteEvent(ACPEventRunCreated, createdData)

	// Send run.in-progress
	inProgressData := map[string]any{"run": map[string]any{"run_id": run.ID, "status": ACPStatusWorking}}
	h.persistACPEvent(bgCtx, run.ID, ACPEventRunInProgress, inProgressData)
	h.emitToSSEBus(projectID, run.ID, ACPEventRunInProgress, inProgressData)
	_ = writer.WriteEvent(ACPEventRunInProgress, inProgressData)

	// Set up streaming callback that writes to SSE wire, persistence, and SSE bus
	execReq.StreamCallback = func(event StreamEvent) {
		switch event.Type {
		case StreamEventTextDelta:
			data := map[string]any{
				"part": map[string]any{"content_type": "text/plain", "content": event.Text},
			}
			_ = writer.WriteEvent(ACPEventMessagePart, data)
			h.persistACPEvent(bgCtx, run.ID, ACPEventMessagePart, data)
			h.emitToSSEBus(projectID, run.ID, ACPEventMessagePart, data)

		case StreamEventToolCallStart:
			inputJSON, _ := json.Marshal(event.Input)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "application/json",
					"metadata": map[string]any{
						"kind":       "trajectory",
						"tool_name":  event.Tool,
						"tool_input": json.RawMessage(inputJSON),
					},
				},
			}
			_ = writer.WriteEvent(ACPEventMessagePart, data)
			h.persistACPEvent(bgCtx, run.ID, ACPEventMessagePart, data)
			h.emitToSSEBus(projectID, run.ID, ACPEventMessagePart, data)
			toolCallData := map[string]any{
				"tool_call": map[string]any{
					"name":      event.Tool,
					"arguments": json.RawMessage(inputJSON),
				},
			}
			h.persistACPEvent(bgCtx, run.ID, ACPEventToolCall, toolCallData)
			h.emitToSSEBus(projectID, run.ID, ACPEventToolCall, toolCallData)

		case StreamEventToolCallEnd:
			outputJSON, _ := json.Marshal(event.Output)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "application/json",
					"metadata": map[string]any{
						"kind":        "trajectory",
						"tool_name":   event.Tool,
						"tool_output": json.RawMessage(outputJSON),
					},
				},
			}
			_ = writer.WriteEvent(ACPEventMessagePart, data)
			h.persistACPEvent(bgCtx, run.ID, ACPEventMessagePart, data)
			h.emitToSSEBus(projectID, run.ID, ACPEventMessagePart, data)
			toolResultData := map[string]any{
				"tool_result": map[string]any{
					"name":   event.Tool,
					"output": json.RawMessage(outputJSON),
				},
			}
			h.persistACPEvent(bgCtx, run.ID, ACPEventToolResult, toolResultData)
			h.emitToSSEBus(projectID, run.ID, ACPEventToolResult, toolResultData)

		case StreamEventError:
			data := map[string]any{"error": map[string]any{"message": event.Error}}
			_ = writer.WriteEvent(ACPEventError, data)
			h.persistACPEvent(bgCtx, run.ID, ACPEventError, data)
			h.emitToSSEBus(projectID, run.ID, ACPEventError, data)
		}
	}

	// Execute synchronously on this goroutine (SSE response is already streaming)
	result, execErr := h.executor.ExecuteWithRun(ctx, run, execReq)
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}

	// Send terminal event
	if execErr != nil {
		h.log.Error("ACP stream run failed",
			slog.String("run_id", run.ID),
			slog.String("error", execErr.Error()),
		)
		termData := map[string]any{"run": map[string]any{"run_id": run.ID, "error": map[string]any{"message": execErr.Error()}}}
		_ = writer.WriteEvent(ACPEventRunFailed, termData)
		h.persistACPEvent(bgCtx, run.ID, ACPEventRunFailed, termData)
		h.emitToSSEBus(projectID, run.ID, ACPEventRunFailed, termData)
	} else if result != nil {
		switch result.Status {
		case RunStatusPaused:
			questions, _ := h.repo.FindPendingQuestionsByRunID(bgCtx, run.ID)
			termData := map[string]any{"run": map[string]any{"run_id": run.ID, "status": ACPStatusInputRequired}}
			if len(questions) > 0 {
				q := questions[0]
				termData["run"].(map[string]any)["await_request"] = map[string]any{
					"question_id": q.ID,
					"question":    q.Question,
					"options":     q.Options,
				}
			}
			_ = writer.WriteEvent(ACPEventRunAwaiting, termData)
			h.persistACPEvent(bgCtx, run.ID, ACPEventRunAwaiting, termData)
			h.emitToSSEBus(projectID, run.ID, ACPEventRunAwaiting, termData)

		case RunStatusError:
			errRun, _ := h.repo.FindRunByID(bgCtx, run.ID)
			errMsg := "unknown error"
			if errRun != nil && errRun.ErrorMessage != nil {
				errMsg = *errRun.ErrorMessage
			}
			termData := map[string]any{"run": map[string]any{"run_id": run.ID, "error": map[string]any{"message": errMsg}}}
			_ = writer.WriteEvent(ACPEventRunFailed, termData)
			h.persistACPEvent(bgCtx, run.ID, ACPEventRunFailed, termData)
			h.emitToSSEBus(projectID, run.ID, ACPEventRunFailed, termData)

		default:
			termData := map[string]any{"run": map[string]any{"run_id": run.ID, "status": MapMemoryStatusToACP(result.Status)}}
			_ = writer.WriteEvent(ACPEventRunCompleted, termData)
			h.persistACPEvent(bgCtx, run.ID, ACPEventRunCompleted, termData)
			h.emitToSSEBus(projectID, run.ID, ACPEventRunCompleted, termData)
		}
	}

	writer.Close()
	return nil
}

// makeEventPersistingCallback returns a StreamCallback that persists events to the DB
// and emits them on the SSE bus for real-time delivery to subscribers.
func (h *ACPHandler) makeEventPersistingCallback(ctx context.Context, runID, projectID string) StreamCallback {
	return func(event StreamEvent) {
		switch event.Type {
		case StreamEventTextDelta:
			data := map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      event.Text,
				},
			}
			h.persistACPEvent(ctx, runID, ACPEventMessagePart, data)
			h.emitToSSEBus(projectID, runID, ACPEventMessagePart, data)
		case StreamEventToolCallStart:
			inputJSON, _ := json.Marshal(event.Input)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "application/json",
					"metadata": map[string]any{
						"kind":       "trajectory",
						"tool_name":  event.Tool,
						"tool_input": json.RawMessage(inputJSON),
					},
				},
			}
			h.persistACPEvent(ctx, runID, ACPEventMessagePart, data)
			h.emitToSSEBus(projectID, runID, ACPEventMessagePart, data)
			toolCallData := map[string]any{
				"tool_call": map[string]any{
					"name":      event.Tool,
					"arguments": json.RawMessage(inputJSON),
				},
			}
			h.persistACPEvent(ctx, runID, ACPEventToolCall, toolCallData)
			h.emitToSSEBus(projectID, runID, ACPEventToolCall, toolCallData)
		case StreamEventToolCallEnd:
			outputJSON, _ := json.Marshal(event.Output)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "application/json",
					"metadata": map[string]any{
						"kind":        "trajectory",
						"tool_name":   event.Tool,
						"tool_output": json.RawMessage(outputJSON),
					},
				},
			}
			h.persistACPEvent(ctx, runID, ACPEventMessagePart, data)
			h.emitToSSEBus(projectID, runID, ACPEventMessagePart, data)
			toolResultData := map[string]any{
				"tool_result": map[string]any{
					"name":   event.Tool,
					"output": json.RawMessage(outputJSON),
				},
			}
			h.persistACPEvent(ctx, runID, ACPEventToolResult, toolResultData)
			h.emitToSSEBus(projectID, runID, ACPEventToolResult, toolResultData)
		case StreamEventError:
			data := map[string]any{
				"error": map[string]any{"message": event.Error},
			}
			h.persistACPEvent(ctx, runID, ACPEventError, data)
			h.emitToSSEBus(projectID, runID, ACPEventError, data)
		}
	}
}

// respondWithRunObject re-fetches a run and returns its full ACP representation.
func (h *ACPHandler) respondWithRunObject(c echo.Context, runID string, def *AgentDefinition) error {
	ctx := c.Request().Context()
	run, err := h.repo.FindRunByID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to fetch run", err)
	}
	if run == nil {
		return apperror.NewNotFound("Run", runID)
	}

	messages, _ := h.repo.FindMessagesByRunID(ctx, runID)

	// Convert []*AgentRunMessage to []AgentRunMessage for DTO conversion
	msgValues := make([]AgentRunMessage, len(messages))
	for i, m := range messages {
		msgValues[i] = *m
	}

	var question *AgentQuestion
	if run.Status == RunStatusPaused {
		questions, _ := h.repo.FindPendingQuestionsByRunID(ctx, runID)
		if len(questions) > 0 {
			question = questions[0]
		}
	}

	acpRun := RunToACPObject(run, msgValues, question)
	acpRun.AgentName = ACPSlugFromName(def.Name)

	return c.JSON(http.StatusOK, acpRun)
}

// ---------------------------------------------------------------------------
// 4.10 GetRun — GET /acp/v1/agents/:name/runs/:runId
// ---------------------------------------------------------------------------

// GetRun returns the current state of a run.
func (h *ACPHandler) GetRun(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	slug := c.Param("name")
	runID := c.Param("runId")
	if slug == "" || runID == "" {
		return apperror.NewBadRequest("agent name and run ID are required")
	}

	ctx := c.Request().Context()

	// Resolve agent
	def, _, err := h.resolveAgentBySlug(ctx, projectID, slug)
	if err != nil {
		return err
	}

	// Fetch run and verify it belongs to this agent
	run, err := h.repo.FindRunByID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to fetch run", err)
	}
	if run == nil {
		return apperror.NewNotFound("Run", runID)
	}

	// Verify the run belongs to the correct agent by checking agent name
	if run.Agent != nil && run.Agent.Name != def.Name {
		return apperror.NewNotFound("Run", runID)
	}

	messages, _ := h.repo.FindMessagesByRunID(ctx, runID)

	// Convert []*AgentRunMessage to []AgentRunMessage for DTO conversion
	msgVals := make([]AgentRunMessage, len(messages))
	for i, m := range messages {
		msgVals[i] = *m
	}

	var question *AgentQuestion
	if run.Status == RunStatusPaused {
		questions, _ := h.repo.FindPendingQuestionsByRunID(ctx, runID)
		if len(questions) > 0 {
			question = questions[0]
		}
	}

	acpRun := RunToACPObject(run, msgVals, question)
	acpRun.AgentName = ACPSlugFromName(def.Name)

	return c.JSON(http.StatusOK, acpRun)
}

// ---------------------------------------------------------------------------
// 4.11 CancelRun — DELETE /acp/v1/agents/:name/runs/:runId
// ---------------------------------------------------------------------------

// CancelRun initiates cancellation of an active run.
func (h *ACPHandler) CancelRun(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	slug := c.Param("name")
	runID := c.Param("runId")
	if slug == "" || runID == "" {
		return apperror.NewBadRequest("agent name and run ID are required")
	}

	ctx := c.Request().Context()

	def, _, err := h.resolveAgentBySlug(ctx, projectID, slug)
	if err != nil {
		return err
	}

	run, err := h.repo.FindRunByID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to fetch run", err)
	}
	if run == nil {
		return apperror.NewNotFound("Run", runID)
	}

	// Verify the run belongs to the correct agent
	if run.Agent != nil && run.Agent.Name != def.Name {
		return apperror.NewNotFound("Run", runID)
	}

	acpStatus := MapMemoryStatusToACP(run.Status)

	// Terminal states → 409 Conflict
	if isTerminalACPStatus(acpStatus) {
		return apperror.New(http.StatusConflict, "conflict",
			fmt.Sprintf("run is already in terminal state: %s", acpStatus))
	}

	// Queued runs can be cancelled directly
	if run.Status == RunStatusQueued {
		if err := h.repo.CancelRun(ctx, runID); err != nil {
			return apperror.NewInternal("failed to cancel run", err)
		}
		h.persistACPEvent(ctx, run.ID, "run.cancelled", map[string]any{
			"run_id": run.ID,
			"status": "cancelled",
		})
	} else {
		// Active runs transition to cancelling first
		if err := h.repo.SetRunCancelling(ctx, runID); err != nil {
			return apperror.NewInternal("failed to set run to cancelling", err)
		}
	}

	// Re-fetch and return updated state
	return h.respondWithRunObject(c, runID, def)
}

// ---------------------------------------------------------------------------
// 4.12 ResumeRun — POST /acp/v1/agents/:name/runs/:runId/resume
// ---------------------------------------------------------------------------

// ResumeRun resumes a paused (input-required) run with a human response.
func (h *ACPHandler) ResumeRun(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	slug := c.Param("name")
	runID := c.Param("runId")
	if slug == "" || runID == "" {
		return apperror.NewBadRequest("agent name and run ID are required")
	}

	ctx := c.Request().Context()

	def, err := h.resolveAgentDefinitionBySlug(ctx, projectID, slug)
	if err != nil {
		return err
	}

	// Parse request
	var req ACPResumeRunRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	mode := req.Mode
	if mode == "" {
		mode = "sync"
	}
	if mode != "sync" && mode != "async" && mode != "stream" {
		return apperror.NewBadRequest("mode must be sync, async, or stream")
	}

	// Fetch the run and verify status
	run, err := h.repo.FindRunByID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to fetch run", err)
	}
	if run == nil {
		return apperror.NewNotFound("Run", runID)
	}
	if run.Agent != nil && run.Agent.Name != def.Name {
		return apperror.NewNotFound("Run", runID)
	}
	if run.Status != RunStatusPaused {
		return apperror.New(http.StatusConflict, "conflict",
			fmt.Sprintf("run status is %s, expected input-required", MapMemoryStatusToACP(run.Status)))
	}

	// Find and answer the pending question
	questions, err := h.repo.FindPendingQuestionsByRunID(ctx, runID)
	if err != nil || len(questions) == 0 {
		return apperror.New(http.StatusConflict, "conflict", "no pending question found for this run")
	}

	responseText := acpUserMessageFromParts(req.Message)
	if responseText == "" {
		return apperror.NewBadRequest("message content is required for resume")
	}

	user := auth.GetUser(c)
	userID := ""
	if user != nil {
		userID = user.ID
	}

	// Answer the question
	if err := h.repo.AnswerQuestion(ctx, questions[0].ID, responseText, userID); err != nil {
		return apperror.NewInternal("failed to answer question", err)
	}

	// Build resume message
	userMessage := fmt.Sprintf(
		"Previously you asked: \"%s\"\nThe user responded: \"%s\"\nContinue from where you left off.",
		questions[0].Question, responseText,
	)

	orgID := h.acpOrgID(ctx, c, projectID)

	// Pre-create the resume run synchronously so we can return its ID in async mode.
	maxSteps := MaxTotalStepsPerRun
	resumedFromID := run.ID
	preCreatedRun, err := h.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:          run.AgentID,
		MaxSteps:         &maxSteps,
		ResumedFrom:      &resumedFromID,
		InitialStepCount: run.StepCount,
		TriggerMetadata:  run.TriggerMetadata,
	})
	if err != nil {
		return apperror.NewInternal("failed to pre-create resume run", err)
	}

	// Persist resume_run_id in suspend_context so GET run can expose it.
	if run.SuspendContext != nil {
		sc := make(map[string]any, len(run.SuspendContext)+1)
		for k, v := range run.SuspendContext {
			sc[k] = v
		}
		sc["resume_run_id"] = preCreatedRun.ID
		_ = h.repo.UpdateSuspendContext(ctx, run.ID, sc)
	}

	execReq := ExecuteRequest{
		Agent:           run.Agent, // Use the agent record from the run if it exists
		AgentDefinition: def,
		ProjectID:       projectID,
		OrgID:           orgID,
		UserID:          userID, // propagate for ask_user notifications on resumed run
		UserMessage:     userMessage,
		PreCreatedRun:   preCreatedRun,
	}

	switch mode {
	case "async":
		// Resume asynchronously
		go func() {
			bgCtx := context.Background()
			execReq.StreamCallback = h.makeEventPersistingCallback(bgCtx, run.ID, projectID)
			result, execErr := h.executor.Resume(bgCtx, run, execReq)
			if result != nil && result.Cleanup != nil {
				result.Cleanup()
			}
			if execErr != nil {
				h.log.Error("ACP async resume failed",
					slog.String("run_id", run.ID),
					slog.String("error", execErr.Error()),
				)
			}
		}()

		nowResume := time.Now()
		acpRun := ACPRunObject{
			ID:        preCreatedRun.ID,
			AgentName: ACPSlugFromName(def.Name),
			Status:    ACPStatusWorking,
			CreatedAt: preCreatedRun.CreatedAt,
			UpdatedAt: &nowResume,
		}
		return c.JSON(http.StatusAccepted, acpRun)

	case "stream":
		return h.resumeRunStream(c, ctx, run, execReq, def)

	default: // sync
		execReq.StreamCallback = h.makeEventPersistingCallback(context.Background(), run.ID, projectID)
		result, execErr := h.executor.Resume(ctx, run, execReq)
		if result != nil && result.Cleanup != nil {
			defer result.Cleanup()
		}
		if execErr != nil {
			h.log.Error("ACP sync resume failed",
				slog.String("run_id", run.ID),
				slog.String("error", execErr.Error()),
			)
		}

		// The resumed run creates a new run record. Find the latest run ID.
		// For now, re-fetch the original run (its status gets updated after resume chain).
		// The executor.Resume creates a new run with ResumedFrom set. Return that.
		if result != nil {
			return h.respondWithRunObject(c, result.RunID, def)
		}
		return h.respondWithRunObject(c, run.ID, def)
	}
}

// resumeRunStream handles the SSE streaming path for resume.
func (h *ACPHandler) resumeRunStream(c echo.Context, ctx context.Context, run *AgentRun, execReq ExecuteRequest, def *AgentDefinition) error {
	bgCtx := context.Background()
	projectID := execReq.ProjectID
	agentSlug := ACPSlugFromName(def.Name)

	writer := sse.NewWriter(c.Response().Writer)
	if err := writer.Start(); err != nil {
		return apperror.NewInternal("SSE streaming not supported", err)
	}

	inProgressData := map[string]any{"run": map[string]any{"run_id": run.ID, "agent_name": agentSlug, "status": ACPStatusWorking}}
	h.emitToSSEBus(projectID, run.ID, ACPEventRunInProgress, inProgressData)
	_ = writer.WriteEvent(ACPEventRunInProgress, inProgressData)

	// Set up streaming callback
	execReq.StreamCallback = func(event StreamEvent) {
		switch event.Type {
		case StreamEventTextDelta:
			data := map[string]any{
				"part": map[string]any{"content_type": "text/plain", "content": event.Text},
			}
			_ = writer.WriteEvent(ACPEventMessagePart, data)
			h.persistACPEvent(bgCtx, run.ID, ACPEventMessagePart, data)
			h.emitToSSEBus(projectID, run.ID, ACPEventMessagePart, data)

		case StreamEventToolCallStart:
			inputJSON, _ := json.Marshal(event.Input)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "application/json",
					"metadata": map[string]any{
						"kind":       "trajectory",
						"tool_name":  event.Tool,
						"tool_input": json.RawMessage(inputJSON),
					},
				},
			}
			_ = writer.WriteEvent(ACPEventMessagePart, data)
			h.persistACPEvent(bgCtx, run.ID, ACPEventMessagePart, data)
			h.emitToSSEBus(projectID, run.ID, ACPEventMessagePart, data)
			toolCallData := map[string]any{
				"tool_call": map[string]any{
					"name":      event.Tool,
					"arguments": json.RawMessage(inputJSON),
				},
			}
			h.persistACPEvent(bgCtx, run.ID, ACPEventToolCall, toolCallData)
			h.emitToSSEBus(projectID, run.ID, ACPEventToolCall, toolCallData)

		case StreamEventToolCallEnd:
			outputJSON, _ := json.Marshal(event.Output)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "application/json",
					"metadata": map[string]any{
						"kind":        "trajectory",
						"tool_name":   event.Tool,
						"tool_output": json.RawMessage(outputJSON),
					},
				},
			}
			_ = writer.WriteEvent(ACPEventMessagePart, data)
			h.persistACPEvent(bgCtx, run.ID, ACPEventMessagePart, data)
			h.emitToSSEBus(projectID, run.ID, ACPEventMessagePart, data)
			toolResultData := map[string]any{
				"tool_result": map[string]any{
					"name":   event.Tool,
					"output": json.RawMessage(outputJSON),
				},
			}
			h.persistACPEvent(bgCtx, run.ID, ACPEventToolResult, toolResultData)
			h.emitToSSEBus(projectID, run.ID, ACPEventToolResult, toolResultData)

		case StreamEventError:
			data := map[string]any{"error": map[string]any{"message": event.Error}}
			_ = writer.WriteEvent(ACPEventError, data)
			h.persistACPEvent(bgCtx, run.ID, ACPEventError, data)
			h.emitToSSEBus(projectID, run.ID, ACPEventError, data)
		}
	}

	result, execErr := h.executor.Resume(ctx, run, execReq)
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}

	// Send terminal event
	if execErr != nil {
		termData := map[string]any{"run": map[string]any{"run_id": run.ID, "error": map[string]any{"message": execErr.Error()}}}
		_ = writer.WriteEvent(ACPEventRunFailed, termData)
		h.persistACPEvent(bgCtx, run.ID, ACPEventRunFailed, termData)
		h.emitToSSEBus(projectID, run.ID, ACPEventRunFailed, termData)
	} else if result != nil {
		switch result.Status {
		case RunStatusPaused:
			questions, _ := h.repo.FindPendingQuestionsByRunID(bgCtx, result.RunID)
			termData := map[string]any{"run": map[string]any{"run_id": result.RunID, "status": ACPStatusInputRequired}}
			if len(questions) > 0 {
				q := questions[0]
				termData["run"].(map[string]any)["await_request"] = map[string]any{
					"question_id": q.ID,
					"question":    q.Question,
					"options":     q.Options,
				}
			}
			_ = writer.WriteEvent(ACPEventRunAwaiting, termData)
			h.persistACPEvent(bgCtx, run.ID, ACPEventRunAwaiting, termData)
			h.emitToSSEBus(projectID, run.ID, ACPEventRunAwaiting, termData)

		case RunStatusError:
			errRun, _ := h.repo.FindRunByID(bgCtx, result.RunID)
			errMsg := "unknown error"
			if errRun != nil && errRun.ErrorMessage != nil {
				errMsg = *errRun.ErrorMessage
			}
			termData := map[string]any{"run": map[string]any{"run_id": result.RunID, "error": map[string]any{"message": errMsg}}}
			_ = writer.WriteEvent(ACPEventRunFailed, termData)
			h.persistACPEvent(bgCtx, run.ID, ACPEventRunFailed, termData)
			h.emitToSSEBus(projectID, run.ID, ACPEventRunFailed, termData)

		default:
			termData := map[string]any{"run": map[string]any{"run_id": result.RunID, "status": MapMemoryStatusToACP(result.Status)}}
			_ = writer.WriteEvent(ACPEventRunCompleted, termData)
			h.persistACPEvent(bgCtx, run.ID, ACPEventRunCompleted, termData)
			h.emitToSSEBus(projectID, run.ID, ACPEventRunCompleted, termData)
		}
	}

	writer.Close()
	return nil
}

// ---------------------------------------------------------------------------
// 4.13 GetRunEvents — GET /acp/v1/agents/:name/runs/:runId/events
// ---------------------------------------------------------------------------

// GetRunEvents returns the persisted event log for a run as a JSON array.
// @Summary      Get run event log
// @Description  Returns all persisted ACP SSE events for an agent run: run lifecycle events (run.created, run.in-progress, run.completed, run.failed), trajectory events (tool calls with full input/output, thought chunks), and message parts. Useful for reconstructing what the agent did step-by-step or replaying a run. This endpoint also serves as the resource-server URL referenced in session history.
// @Tags         acp
// @Produce      json
// @Param        name   path string true "ACP slug name of the agent"
// @Param        runId  path string true "UUID of the agent run"
// @Success      200 {array}  ACPSSEEvent
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /acp/v1/agents/{name}/runs/{runId}/events [get]
// @Security     bearerAuth
func (h *ACPHandler) GetRunEvents(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	slug := c.Param("name")
	runID := c.Param("runId")
	if slug == "" || runID == "" {
		return apperror.NewBadRequest("agent name and run ID are required")
	}

	ctx := c.Request().Context()

	def, _, err := h.resolveAgentBySlug(ctx, projectID, slug)
	if err != nil {
		return err
	}

	// Verify the run exists and belongs to this agent
	run, err := h.repo.FindRunByID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to fetch run", err)
	}
	if run == nil {
		return apperror.NewNotFound("Run", runID)
	}
	if run.Agent != nil && run.Agent.Name != def.Name {
		return apperror.NewNotFound("Run", runID)
	}

	events, err := h.repo.GetACPRunEvents(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to fetch events", err)
	}

	// Convert to ACP SSE event format
	acpEvents := make([]ACPSSEEvent, 0, len(events))
	for _, e := range events {
		acpEvents = append(acpEvents, RunEventToACPSSEEvent(e))
	}

	return c.JSON(http.StatusOK, acpEvents)
}

// ---------------------------------------------------------------------------
// 4.14 CreateSession — POST /acp/v1/sessions
// ---------------------------------------------------------------------------

// CreateSession creates a new ACP session.
func (h *ACPHandler) CreateSession(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()

	var req ACPCreateSessionRequest
	_ = c.Bind(&req) // body is optional

	// Validate agent_name if provided
	if req.AgentName != nil && *req.AgentName != "" {
		def, err := h.repo.FindExternalAgentBySlug(ctx, projectID, *req.AgentName)
		if err != nil {
			return apperror.NewInternal("failed to validate agent", err)
		}
		if def == nil {
			return apperror.NewNotFound("Agent", *req.AgentName)
		}
	}

	session := &ACPSession{
		ProjectID: projectID,
		AgentName: req.AgentName,
	}
	if err := h.repo.CreateACPSession(ctx, session); err != nil {
		return apperror.NewInternal("failed to create session", err)
	}

	acpSession := SessionToACPObject(session, nil, nil, nil)
	return c.JSON(http.StatusCreated, acpSession)
}

// ---------------------------------------------------------------------------
// 4.15 GetSession — GET /acp/v1/sessions/:sessionId
// ---------------------------------------------------------------------------

// GetSession returns an ACP session with its run history.
// @Summary      Get ACP session
// @Description  Returns an ACP session descriptor. The `history` field contains an ordered list of URL references (one per run) pointing to GET /acp/v1/agents/:name/runs/:runId/events. Clients fetch each URL to reconstruct full message history for that run. MP acts as both ACP server and resource server — history URLs resolve back to this server.
// @Tags         acp
// @Produce      json
// @Param        sessionId path string true "ACP session ID"
// @Success      200 {object} ACPSessionObject
// @Failure      400 {object} apperror.Error
// @Failure      401 {object} apperror.Error
// @Failure      404 {object} apperror.Error
// @Router       /acp/v1/sessions/{sessionId} [get]
// @Security     bearerAuth
func (h *ACPHandler) GetSession(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return apperror.NewBadRequest("session ID is required")
	}

	ctx := c.Request().Context()

	session, err := h.repo.GetACPSession(ctx, projectID, sessionID)
	if err != nil {
		return apperror.NewInternal("failed to fetch session", err)
	}
	if session == nil {
		return apperror.NewNotFound("Session", sessionID)
	}

	runs, err := h.repo.GetSessionRunHistory(ctx, sessionID)
	if err != nil {
		return apperror.NewInternal("failed to fetch session history", err)
	}

	runIDs := make([]string, len(runs))
	for i, r := range runs {
		runIDs[i] = r.ID
	}
	eventsByRun, err := h.repo.GetACPRunEventsByRunIDs(ctx, runIDs)
	if err != nil {
		return apperror.NewInternal("failed to fetch run events", err)
	}

	acpSession := SessionToACPObject(session, runs, eventsByRun, nil)
	return c.JSON(http.StatusOK, acpSession)
}

// ---------------------------------------------------------------------------
// 4.16 ListSessions — GET /acp/v1/sessions
// ---------------------------------------------------------------------------

// ListSessions returns all ACP sessions for the authenticated project.
// @Summary      List ACP sessions
// @Description  Returns a list of all ACP sessions for the project, ordered by creation time descending.
// @Tags         acp
// @Produce      json
// @Success      200 {array} ACPSessionObject
// @Failure      401 {object} apperror.Error
// @Router       /acp/v1/sessions [get]
// @Security     bearerAuth
func (h *ACPHandler) ListSessions(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()

	includeArchived := c.QueryParam("include_archived") == "true"

	sessions, err := h.repo.ListACPSessions(ctx, projectID, includeArchived)
	if err != nil {
		return apperror.NewInternal("failed to list sessions", err)
	}

	runsBySession, err := h.repo.ListSessionRunsByProjectID(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("failed to fetch session runs", err)
	}

	// For list view: do not fetch run events — that is expensive and unnecessary.
	// Clients that need full event history should GET /acp/v1/sessions/:id.
	emptyEvents := map[string][]*ACPRunEvent{}

	statsBySession, err := h.repo.GetSessionStatsByProjectID(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("failed to fetch session stats", err)
	}

	result := make([]ACPSessionObject, len(sessions))
	for i, s := range sessions {
		result[i] = SessionToACPObject(s, runsBySession[s.ID], emptyEvents, statsBySession[s.ID])
		// Clear history entries — list view only needs metadata (run_count, last_run_status).
		result[i].History = nil
	}
	return c.JSON(http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// 4.17 ArchiveSession — PATCH /acp/v1/sessions/:sessionId/archive
// ---------------------------------------------------------------------------

// @Summary      Archive a session
// @Tags         ACP Sessions
// @Produce      json
// @Param        sessionId  path  string  true  "Session ID"
// @Success      200  {object}  ACPSessionObject
// @Failure      401  {object}  apperror.Error
// @Failure      404  {object}  apperror.Error
// @Router       /acp/v1/sessions/{sessionId}/archive [patch]
func (h *ACPHandler) ArchiveSession(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}
	sessionID := c.Param("sessionId")
	ctx := c.Request().Context()

	if err := h.repo.ArchiveACPSession(ctx, projectID, sessionID); err != nil {
		return apperror.NewInternal("failed to archive session", err)
	}
	session, err := h.repo.GetACPSession(ctx, projectID, sessionID)
	if err != nil {
		return apperror.NewInternal("failed to fetch session", err)
	}
	return c.JSON(http.StatusOK, SessionToACPObject(session, nil, nil, nil))
}

// ---------------------------------------------------------------------------
// 4.18 UnarchiveSession — PATCH /acp/v1/sessions/:sessionId/unarchive
// ---------------------------------------------------------------------------

// @Summary      Unarchive a session
// @Tags         ACP Sessions
// @Produce      json
// @Param        sessionId  path  string  true  "Session ID"
// @Success      200  {object}  ACPSessionObject
// @Failure      401  {object}  apperror.Error
// @Failure      404  {object}  apperror.Error
// @Router       /acp/v1/sessions/{sessionId}/unarchive [patch]
func (h *ACPHandler) UnarchiveSession(c echo.Context) error {
	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}
	sessionID := c.Param("sessionId")
	ctx := c.Request().Context()

	if err := h.repo.UnarchiveACPSession(ctx, projectID, sessionID); err != nil {
		return apperror.NewInternal("failed to unarchive session", err)
	}
	session, err := h.repo.GetACPSession(ctx, projectID, sessionID)
	if err != nil {
		return apperror.NewInternal("failed to fetch session", err)
	}
	return c.JSON(http.StatusOK, SessionToACPObject(session, nil, nil, nil))
}
