package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/sse"
)

// ACPHandler handles ACP v1 HTTP requests.
type ACPHandler struct {
	repo     *Repository
	executor *AgentExecutor
	log      *slog.Logger
}

// NewACPHandler creates a new ACP handler.
func NewACPHandler(repo *Repository, executor *AgentExecutor, log *slog.Logger) *ACPHandler {
	return &ACPHandler{repo: repo, executor: executor, log: log}
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
	// Try external-visibility first (preferred ACP agents)
	def, err := h.repo.FindExternalAgentBySlug(ctx, projectID, slug)
	if err != nil {
		return nil, nil, apperror.NewInternal("failed to look up agent", err)
	}
	// Fall back to any visibility so agents without explicit external visibility
	// are still accessible via ACP (fixes 404 for project/internal agents).
	if def == nil {
		def, err = h.repo.FindAgentDefinitionBySlug(ctx, projectID, slug)
		if err != nil {
			return nil, nil, apperror.NewInternal("failed to look up agent", err)
		}
	}
	if def == nil {
		return nil, nil, apperror.NewNotFound("Agent", slug)
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

	execReq := ExecuteRequest{
		Agent:           agent,
		AgentDefinition: def,
		ProjectID:       projectID,
		OrgID:           orgID,
		UserMessage:     userMessage,
		EnvVars:         req.EnvVars,
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
	// Persist creation event
	h.persistACPEvent(ctx, run.ID, "run.created", map[string]any{
		"run_id":     run.ID,
		"agent_name": ACPSlugFromName(def.Name),
		"status":     "submitted",
	})

	// Execute in background
	go func() {
		bgCtx := context.Background()
		h.persistACPEvent(bgCtx, run.ID, "run.in-progress", map[string]any{
			"run_id": run.ID,
			"status": "working",
		})

		execReq.StreamCallback = h.makeEventPersistingCallback(bgCtx, run.ID)
		result, execErr := h.executor.ExecuteWithRun(bgCtx, run, execReq)
		if result != nil && result.Cleanup != nil {
			result.Cleanup()
		}

		if execErr != nil {
			h.log.Error("ACP async run failed",
				slog.String("run_id", run.ID),
				slog.String("error", execErr.Error()),
			)
			h.persistACPEvent(bgCtx, run.ID, "run.failed", map[string]any{
				"run_id": run.ID,
				"error":  map[string]any{"message": execErr.Error()},
			})
		} else {
			terminalEvent := "run.completed"
			if result != nil && result.Status == RunStatusPaused {
				terminalEvent = "run.awaiting"
			} else if result != nil && result.Status == RunStatusError {
				terminalEvent = "run.failed"
			}
			h.persistACPEvent(bgCtx, run.ID, terminalEvent, map[string]any{
				"run_id": run.ID,
				"status": MapMemoryStatusToACP(result.Status),
			})
		}
	}()

	// Return the run object immediately with submitted status
	now := run.CreatedAt
	acpRun := ACPRunObject{
		ID:        run.ID,
		AgentName: ACPSlugFromName(def.Name),
		Status:    "submitted",
		CreatedAt: run.CreatedAt,
		UpdatedAt: &now,
	}
	return c.JSON(http.StatusAccepted, acpRun)
}

// createRunSync blocks until the run completes and returns the final state.
func (h *ACPHandler) createRunSync(c echo.Context, ctx context.Context, run *AgentRun, execReq ExecuteRequest, def *AgentDefinition) error {
	// Persist creation event
	bgCtx := context.Background()
	h.persistACPEvent(bgCtx, run.ID, "run.created", map[string]any{
		"run_id":     run.ID,
		"agent_name": ACPSlugFromName(def.Name),
		"status":     "submitted",
	})
	h.persistACPEvent(bgCtx, run.ID, "run.in-progress", map[string]any{
		"run_id": run.ID,
		"status": "working",
	})

	execReq.StreamCallback = h.makeEventPersistingCallback(bgCtx, run.ID)

	result, execErr := h.executor.ExecuteWithRun(ctx, run, execReq)
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}

	if execErr != nil {
		h.log.Error("ACP sync run failed",
			slog.String("run_id", run.ID),
			slog.String("error", execErr.Error()),
		)
		h.persistACPEvent(bgCtx, run.ID, "run.failed", map[string]any{
			"run_id": run.ID,
			"error":  map[string]any{"message": execErr.Error()},
		})
	}

	// Re-fetch the run to get the final state
	return h.respondWithRunObject(c, run.ID, def)
}

// createRunStream sets up SSE and streams events inline on the response.
func (h *ACPHandler) createRunStream(c echo.Context, ctx context.Context, run *AgentRun, execReq ExecuteRequest, def *AgentDefinition) error {
	bgCtx := context.Background()
	agentSlug := ACPSlugFromName(def.Name)

	writer := sse.NewWriter(c.Response().Writer)
	if err := writer.Start(); err != nil {
		return apperror.NewInternal("SSE streaming not supported", err)
	}

	// Send run.created
	h.persistACPEvent(bgCtx, run.ID, "run.created", map[string]any{
		"run_id":     run.ID,
		"agent_name": agentSlug,
		"status":     "submitted",
	})
	_ = writer.WriteEvent("run.created", map[string]any{
		"run_id":     run.ID,
		"agent_name": agentSlug,
		"status":     "submitted",
	})

	// Send run.in-progress
	h.persistACPEvent(bgCtx, run.ID, "run.in-progress", map[string]any{
		"run_id": run.ID,
		"status": "working",
	})
	_ = writer.WriteEvent("run.in-progress", map[string]any{
		"run_id": run.ID,
		"status": "working",
	})

	// Set up streaming callback that writes to both SSE and persistence
	execReq.StreamCallback = func(event StreamEvent) {
		switch event.Type {
		case StreamEventTextDelta:
			data := map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      event.Text,
				},
			}
			_ = writer.WriteEvent("message.part", data)
			h.persistACPEvent(bgCtx, run.ID, "message.part", data)

		case StreamEventToolCallStart:
			inputJSON, _ := json.Marshal(event.Input)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      "",
					"metadata": map[string]any{
						"type":       "trajectory",
						"tool_name":  event.Tool,
						"tool_input": string(inputJSON),
					},
				},
			}
			_ = writer.WriteEvent("message.part", data)
			h.persistACPEvent(bgCtx, run.ID, "message.part", data)

		case StreamEventToolCallEnd:
			outputJSON, _ := json.Marshal(event.Output)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      "",
					"metadata": map[string]any{
						"type":        "trajectory",
						"tool_name":   event.Tool,
						"tool_output": string(outputJSON),
					},
				},
			}
			_ = writer.WriteEvent("message.part", data)
			h.persistACPEvent(bgCtx, run.ID, "message.part", data)

		case StreamEventError:
			data := map[string]any{
				"error": map[string]any{"message": event.Error},
			}
			_ = writer.WriteEvent("error", data)
			h.persistACPEvent(bgCtx, run.ID, "error", data)
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
		termData := map[string]any{
			"run_id": run.ID,
			"error":  map[string]any{"message": execErr.Error()},
		}
		_ = writer.WriteEvent("run.failed", termData)
		h.persistACPEvent(bgCtx, run.ID, "run.failed", termData)
	} else if result != nil {
		switch result.Status {
		case RunStatusPaused:
			// Fetch the pending question for await_request
			questions, _ := h.repo.FindPendingQuestionsByRunID(bgCtx, run.ID)
			termData := map[string]any{
				"run_id": run.ID,
				"status": "input-required",
			}
			if len(questions) > 0 {
				q := questions[0]
				termData["await_request"] = map[string]any{
					"question_id": q.ID,
					"question":    q.Question,
					"options":     q.Options,
				}
			}
			_ = writer.WriteEvent("run.awaiting", termData)
			h.persistACPEvent(bgCtx, run.ID, "run.awaiting", termData)

		case RunStatusError:
			errRun, _ := h.repo.FindRunByID(bgCtx, run.ID)
			errMsg := "unknown error"
			if errRun != nil && errRun.ErrorMessage != nil {
				errMsg = *errRun.ErrorMessage
			}
			termData := map[string]any{
				"run_id": run.ID,
				"error":  map[string]any{"message": errMsg},
			}
			_ = writer.WriteEvent("run.failed", termData)
			h.persistACPEvent(bgCtx, run.ID, "run.failed", termData)

		default:
			termData := map[string]any{
				"run_id": run.ID,
				"status": MapMemoryStatusToACP(result.Status),
			}
			_ = writer.WriteEvent("run.completed", termData)
			h.persistACPEvent(bgCtx, run.ID, "run.completed", termData)
		}
	}

	writer.Close()
	return nil
}

// makeEventPersistingCallback returns a StreamCallback that persists events to the DB.
func (h *ACPHandler) makeEventPersistingCallback(ctx context.Context, runID string) StreamCallback {
	return func(event StreamEvent) {
		switch event.Type {
		case StreamEventTextDelta:
			h.persistACPEvent(ctx, runID, "message.part", map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      event.Text,
				},
			})
		case StreamEventToolCallStart:
			inputJSON, _ := json.Marshal(event.Input)
			h.persistACPEvent(ctx, runID, "message.part", map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      "",
					"metadata": map[string]any{
						"type":       "trajectory",
						"tool_name":  event.Tool,
						"tool_input": string(inputJSON),
					},
				},
			})
		case StreamEventToolCallEnd:
			outputJSON, _ := json.Marshal(event.Output)
			h.persistACPEvent(ctx, runID, "message.part", map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      "",
					"metadata": map[string]any{
						"type":        "trajectory",
						"tool_name":   event.Tool,
						"tool_output": string(outputJSON),
					},
				},
			})
		case StreamEventError:
			h.persistACPEvent(ctx, runID, "error", map[string]any{
				"error": map[string]any{"message": event.Error},
			})
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

	def, agent, err := h.resolveAgentBySlug(ctx, projectID, slug)
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

	execReq := ExecuteRequest{
		Agent:           agent,
		AgentDefinition: def,
		ProjectID:       projectID,
		OrgID:           orgID,
		UserMessage:     userMessage,
	}

	switch mode {
	case "async":
		// Resume asynchronously
		go func() {
			bgCtx := context.Background()
			execReq.StreamCallback = h.makeEventPersistingCallback(bgCtx, run.ID)
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
			ID:        run.ID,
			AgentName: ACPSlugFromName(def.Name),
			Status:    "working",
			CreatedAt: run.CreatedAt,
			UpdatedAt: &nowResume,
		}
		return c.JSON(http.StatusAccepted, acpRun)

	case "stream":
		return h.resumeRunStream(c, ctx, run, execReq, def)

	default: // sync
		execReq.StreamCallback = h.makeEventPersistingCallback(context.Background(), run.ID)
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
	agentSlug := ACPSlugFromName(def.Name)

	writer := sse.NewWriter(c.Response().Writer)
	if err := writer.Start(); err != nil {
		return apperror.NewInternal("SSE streaming not supported", err)
	}

	_ = writer.WriteEvent("run.in-progress", map[string]any{
		"run_id":     run.ID,
		"agent_name": agentSlug,
		"status":     "working",
	})

	// Set up streaming callback
	execReq.StreamCallback = func(event StreamEvent) {
		switch event.Type {
		case StreamEventTextDelta:
			data := map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      event.Text,
				},
			}
			_ = writer.WriteEvent("message.part", data)
			h.persistACPEvent(bgCtx, run.ID, "message.part", data)

		case StreamEventToolCallStart:
			inputJSON, _ := json.Marshal(event.Input)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      "",
					"metadata": map[string]any{
						"type":       "trajectory",
						"tool_name":  event.Tool,
						"tool_input": string(inputJSON),
					},
				},
			}
			_ = writer.WriteEvent("message.part", data)
			h.persistACPEvent(bgCtx, run.ID, "message.part", data)

		case StreamEventToolCallEnd:
			outputJSON, _ := json.Marshal(event.Output)
			data := map[string]any{
				"part": map[string]any{
					"content_type": "text/plain",
					"content":      "",
					"metadata": map[string]any{
						"type":        "trajectory",
						"tool_name":   event.Tool,
						"tool_output": string(outputJSON),
					},
				},
			}
			_ = writer.WriteEvent("message.part", data)
			h.persistACPEvent(bgCtx, run.ID, "message.part", data)

		case StreamEventError:
			data := map[string]any{
				"error": map[string]any{"message": event.Error},
			}
			_ = writer.WriteEvent("error", data)
			h.persistACPEvent(bgCtx, run.ID, "error", data)
		}
	}

	result, execErr := h.executor.Resume(ctx, run, execReq)
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}

	// Send terminal event
	if execErr != nil {
		termData := map[string]any{
			"run_id": run.ID,
			"error":  map[string]any{"message": execErr.Error()},
		}
		_ = writer.WriteEvent("run.failed", termData)
		h.persistACPEvent(bgCtx, run.ID, "run.failed", termData)
	} else if result != nil {
		switch result.Status {
		case RunStatusPaused:
			questions, _ := h.repo.FindPendingQuestionsByRunID(bgCtx, result.RunID)
			termData := map[string]any{
				"run_id": result.RunID,
				"status": "input-required",
			}
			if len(questions) > 0 {
				q := questions[0]
				termData["await_request"] = map[string]any{
					"question_id": q.ID,
					"question":    q.Question,
					"options":     q.Options,
				}
			}
			_ = writer.WriteEvent("run.awaiting", termData)
			h.persistACPEvent(bgCtx, run.ID, "run.awaiting", termData)

		case RunStatusError:
			errRun, _ := h.repo.FindRunByID(bgCtx, result.RunID)
			errMsg := "unknown error"
			if errRun != nil && errRun.ErrorMessage != nil {
				errMsg = *errRun.ErrorMessage
			}
			termData := map[string]any{
				"run_id": result.RunID,
				"error":  map[string]any{"message": errMsg},
			}
			_ = writer.WriteEvent("run.failed", termData)
			h.persistACPEvent(bgCtx, run.ID, "run.failed", termData)

		default:
			termData := map[string]any{
				"run_id": result.RunID,
				"status": MapMemoryStatusToACP(result.Status),
			}
			_ = writer.WriteEvent("run.completed", termData)
			h.persistACPEvent(bgCtx, run.ID, "run.completed", termData)
		}
	}

	writer.Close()
	return nil
}

// ---------------------------------------------------------------------------
// 4.13 GetRunEvents — GET /acp/v1/agents/:name/runs/:runId/events
// ---------------------------------------------------------------------------

// GetRunEvents returns the persisted event log for a run as a JSON array.
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

	acpSession := SessionToACPObject(session, nil)
	return c.JSON(http.StatusCreated, acpSession)
}

// ---------------------------------------------------------------------------
// 4.15 GetSession — GET /acp/v1/sessions/:sessionId
// ---------------------------------------------------------------------------

// GetSession returns an ACP session with its run history.
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

	acpSession := SessionToACPObject(session, runs)
	return c.JSON(http.StatusOK, acpSession)
}
