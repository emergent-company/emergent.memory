package agents

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/domain/workspace"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
)

// Handler handles HTTP requests for agents
type Handler struct {
	repo     *Repository
	executor *AgentExecutor // may be nil in tests
}

// NewHandler creates a new agents handler
func NewHandler(repo *Repository, executor *AgentExecutor) *Handler {
	return &Handler{repo: repo, executor: executor}
}

// ListAgents handles GET /api/admin/agents
// @Summary      List all agents
// @Description  Returns all agents for the current project
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        X-Project-ID header string true "Project ID"
// @Success      200 {object} APIResponse[[]AgentDTO] "List of agents"
// @Failure      400 {object} apperror.Error "X-Project-ID header required"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents [get]
// @Security     bearerAuth
func (h *Handler) ListAgents(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	agents, err := h.repo.FindAll(c.Request().Context(), user.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to list agents", err)
	}

	// Convert to DTOs
	dtos := make([]*AgentDTO, len(agents))
	for i, agent := range agents {
		dtos[i] = agent.ToDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// GetAgent handles GET /api/admin/agents/:id
// @Summary      Get agent by ID
// @Description  Returns an agent by ID
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent ID (UUID)"
// @Param        X-Project-ID header string false "Project ID (optional)"
// @Success      200 {object} APIResponse[AgentDTO] "Agent details"
// @Failure      400 {object} apperror.Error "Invalid agent ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetAgent(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}

	agent, err := h.repo.FindByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", id)
	}

	return c.JSON(http.StatusOK, SuccessResponse(agent.ToDTO()))
}

// GetAgentRuns handles GET /api/admin/agents/:id/runs
// @Summary      Get agent run history
// @Description  Returns recent runs for an agent
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent ID (UUID)"
// @Param        limit query int false "Max results (default 10)" minimum(1) maximum(100) default(10)
// @Param        X-Project-ID header string false "Project ID (optional)"
// @Success      200 {object} APIResponse[[]AgentRunDTO] "List of agent runs"
// @Failure      400 {object} apperror.Error "Invalid agent ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents/{id}/runs [get]
// @Security     bearerAuth
func (h *Handler) GetAgentRuns(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	// Verify agent exists and belongs to project
	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}
	agent, err := h.repo.FindByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", id)
	}

	// Get limit from query param (default 10)
	limit := 10
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	runs, err := h.repo.GetRecentRuns(c.Request().Context(), id, limit)
	if err != nil {
		return apperror.NewInternal("failed to get agent runs", err)
	}

	// Convert to DTOs
	dtos := make([]*AgentRunDTO, len(runs))
	for i, run := range runs {
		dtos[i] = run.ToDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// CreateAgent handles POST /api/admin/agents
// @Summary      Create a new agent
// @Description  Creates a new agent with the specified configuration
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        request body CreateAgentDTO true "Agent data"
// @Success      201 {object} APIResponse[AgentDTO] "Created agent"
// @Failure      400 {object} apperror.Error "Invalid request body or validation error"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents [post]
// @Security     bearerAuth
func (h *Handler) CreateAgent(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	var dto CreateAgentDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Validate required fields
	var missing []string
	if dto.ProjectID == "" {
		missing = append(missing, "projectId")
	}
	if dto.Name == "" {
		missing = append(missing, "name")
	}
	if dto.StrategyType == "" {
		missing = append(missing, "strategyType")
	}
	if dto.CronSchedule == "" {
		missing = append(missing, "cronSchedule")
	}
	if len(missing) > 0 {
		return apperror.NewBadRequest("missing required fields: " + strings.Join(missing, ", "))
	}

	// Set defaults
	enabled := true
	if dto.Enabled != nil {
		enabled = *dto.Enabled
	}

	triggerType := TriggerTypeSchedule
	if dto.TriggerType != "" {
		triggerType = dto.TriggerType
	}

	executionMode := ExecutionModeExecute
	if dto.ExecutionMode != "" {
		executionMode = dto.ExecutionMode
	}

	config := dto.Config
	if config == nil {
		config = make(map[string]any)
	}

	agent := &Agent{
		ProjectID:      dto.ProjectID,
		Name:           dto.Name,
		StrategyType:   dto.StrategyType,
		Prompt:         dto.Prompt,
		CronSchedule:   dto.CronSchedule,
		Enabled:        enabled,
		TriggerType:    triggerType,
		ReactionConfig: dto.ReactionConfig,
		ExecutionMode:  executionMode,
		Capabilities:   dto.Capabilities,
		Config:         config,
		Description:    dto.Description,
	}

	if err := h.repo.Create(c.Request().Context(), agent); err != nil {
		return apperror.NewInternal("failed to create agent", err)
	}

	return c.JSON(http.StatusCreated, SuccessResponse(agent.ToDTO()))
}

// UpdateAgent handles PATCH /api/admin/agents/:id
// @Summary      Update an agent
// @Description  Updates an agent's configuration (partial update)
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent ID (UUID)"
// @Param        request body UpdateAgentDTO true "Agent update data"
// @Param        X-Project-ID header string false "Project ID (optional)"
// @Success      200 {object} APIResponse[AgentDTO] "Updated agent"
// @Failure      400 {object} apperror.Error "Invalid agent ID or request body"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents/{id} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateAgent(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	var dto UpdateAgentDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Get existing agent
	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}
	agent, err := h.repo.FindByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", id)
	}

	// Apply updates
	if dto.Name != nil {
		agent.Name = *dto.Name
	}
	if dto.Prompt != nil {
		agent.Prompt = dto.Prompt
	}
	if dto.Enabled != nil {
		agent.Enabled = *dto.Enabled
	}
	if dto.CronSchedule != nil {
		agent.CronSchedule = *dto.CronSchedule
	}
	if dto.TriggerType != nil {
		agent.TriggerType = *dto.TriggerType
	}
	if dto.ReactionConfig != nil {
		agent.ReactionConfig = dto.ReactionConfig
	}
	if dto.ExecutionMode != nil {
		agent.ExecutionMode = *dto.ExecutionMode
	}
	if dto.Capabilities != nil {
		agent.Capabilities = dto.Capabilities
	}
	if dto.Config != nil {
		agent.Config = dto.Config
	}
	if dto.Description != nil {
		agent.Description = dto.Description
	}

	if err := h.repo.Update(c.Request().Context(), agent); err != nil {
		return apperror.NewInternal("failed to update agent", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(agent.ToDTO()))
}

// DeleteAgent handles DELETE /api/admin/agents/:id
// @Summary      Delete an agent
// @Description  Deletes an agent by ID
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent ID (UUID)"
// @Param        X-Project-ID header string false "Project ID (optional)"
// @Success      200 {object} APIResponse[any] "Agent deleted successfully"
// @Failure      400 {object} apperror.Error "Invalid agent ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents/{id} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteAgent(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	// Verify agent exists and belongs to project
	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}
	agent, err := h.repo.FindByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", id)
	}

	if err := h.repo.Delete(c.Request().Context(), id); err != nil {
		return apperror.NewInternal("failed to delete agent", err)
	}

	return c.JSON(http.StatusOK, APIResponse[any]{Success: true})
}

// TriggerAgent handles POST /api/admin/agents/:id/trigger
// @Summary      Trigger agent execution
// @Description  Triggers an immediate run of an agent (manual execution)
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent ID (UUID)"
// @Param        X-Project-ID header string false "Project ID (optional)"
// @Success      200 {object} TriggerResponseDTO "Agent triggered successfully"
// @Failure      400 {object} apperror.Error "Invalid agent ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents/{id}/trigger [post]
// @Security     bearerAuth
func (h *Handler) TriggerAgent(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	// Verify agent exists and belongs to project
	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}
	agent, err := h.repo.FindByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", id)
	}

	// Check if executor is available
	if h.executor == nil {
		// Fallback: create a run record but skip execution (test mode or executor not wired)
		run, err := h.repo.CreateRun(c.Request().Context(), id)
		if err != nil {
			return apperror.NewInternal("failed to create run", err)
		}
		_ = h.repo.SkipRun(c.Request().Context(), run.ID, "Executor not available")
		msg := "Agent triggered (stub mode, run ID: " + run.ID + ")"
		return c.JSON(http.StatusOK, TriggerResponseDTO{
			Success: true,
			RunID:   &run.ID,
			Message: &msg,
		})
	}

	// Look up the agent definition for this agent (if one exists)
	var agentDef *AgentDefinition
	agentDef, _ = h.repo.FindDefinitionByName(c.Request().Context(), agent.ProjectID, agent.Name)

	// Parse optional request body for dynamic prompt
	var triggerReq TriggerRequestDTO
	_ = c.Bind(&triggerReq) // Ignore bind errors — body is optional

	// Build the user message: request body prompt > agent stored prompt > fallback
	userMessage := "Execute agent tasks"
	if triggerReq.Prompt != "" {
		userMessage = triggerReq.Prompt
	} else if agent.Prompt != nil && *agent.Prompt != "" {
		userMessage = *agent.Prompt
	}

	result, err := h.executor.Execute(c.Request().Context(), ExecuteRequest{
		Agent:           agent,
		AgentDefinition: agentDef,
		ProjectID:       agent.ProjectID,
		UserMessage:     userMessage,
	})
	if err != nil {
		return apperror.NewInternal("failed to execute agent", err)
	}

	msg := "Agent triggered successfully (run ID: " + result.RunID + ")"
	return c.JSON(http.StatusOK, TriggerResponseDTO{
		Success: true,
		RunID:   &result.RunID,
		Message: &msg,
	})
}

// CancelRun handles POST /api/admin/agents/:id/runs/:runId/cancel
// @Summary      Cancel a running agent run
// @Description  Cancels a running agent run
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent ID (UUID)"
// @Param        runId path string true "Run ID (UUID)"
// @Param        X-Project-ID header string false "Project ID (optional)"
// @Success      200 {object} APIResponse[map[string]string] "Run cancelled successfully"
// @Failure      400 {object} apperror.Error "Invalid agent ID or run ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent or run not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents/{id}/runs/{runId}/cancel [post]
// @Security     bearerAuth
func (h *Handler) CancelRun(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	agentID := c.Param("id")
	if agentID == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	// Verify agent exists and belongs to project
	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}
	agent, err := h.repo.FindByID(c.Request().Context(), agentID, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", agentID)
	}

	// Verify run exists and belongs to this agent
	run, err := h.repo.FindRunByID(c.Request().Context(), runID)
	if err != nil {
		return apperror.NewInternal("failed to get run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}
	if run.AgentID != agentID {
		return apperror.NewNotFound("AgentRun", runID)
	}

	// Cancel the run
	if err := h.repo.CancelRun(c.Request().Context(), runID); err != nil {
		return apperror.NewInternal("failed to cancel run", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(map[string]string{
		"message": "Run cancelled successfully",
		"runId":   runID,
	}))
}

// GetPendingEvents handles GET /api/admin/agents/:id/pending-events
// @Summary      Get pending events for reaction agent
// @Description  Returns pending events (unprocessed graph objects) for a reaction agent
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent ID (UUID)"
// @Param        limit query int false "Max results (1-100)" minimum(1) maximum(100) default(100)
// @Param        X-Project-ID header string false "Project ID (optional)"
// @Success      200 {object} APIResponse[PendingEventsResponseDTO] "Pending events"
// @Failure      400 {object} apperror.Error "Invalid agent ID or not a reaction agent"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents/{id}/pending-events [get]
// @Security     bearerAuth
func (h *Handler) GetPendingEvents(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	// Verify agent exists and belongs to project
	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}
	agent, err := h.repo.FindByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", id)
	}

	// Check if this is a reaction agent
	if agent.TriggerType != TriggerTypeReaction {
		return apperror.NewBadRequest("pending events are only available for reaction agents")
	}

	// Get limit from query param (default 100)
	limit := 100
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	objects, totalCount, err := h.repo.GetPendingEvents(c.Request().Context(), agent, limit)
	if err != nil {
		return apperror.NewInternal("failed to get pending events", err)
	}

	response := PendingEventsResponseDTO{
		TotalCount: totalCount,
		Objects:    objects,
	}

	// Include reaction config info
	if agent.ReactionConfig != nil {
		response.ReactionConfig.ObjectTypes = agent.ReactionConfig.ObjectTypes
		events := make([]string, len(agent.ReactionConfig.Events))
		for i, e := range agent.ReactionConfig.Events {
			events[i] = string(e)
		}
		response.ReactionConfig.Events = events
	}

	return c.JSON(http.StatusOK, SuccessResponse(response))
}

// BatchTrigger handles POST /api/admin/agents/:id/batch-trigger
// @Summary      Batch trigger reaction agent
// @Description  Batch triggers a reaction agent for multiple graph objects (max 100)
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent ID (UUID)"
// @Param        request body BatchTriggerDTO true "Batch trigger request (objectIds)"
// @Param        X-Project-ID header string false "Project ID (optional)"
// @Success      200 {object} APIResponse[BatchTriggerResponseDTO] "Batch trigger result (queued/skipped counts)"
// @Failure      400 {object} apperror.Error "Invalid agent ID, not a reaction agent, or invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agents/{id}/batch-trigger [post]
// @Security     bearerAuth
func (h *Handler) BatchTrigger(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	var dto BatchTriggerDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	if len(dto.ObjectIDs) == 0 {
		return apperror.NewBadRequest("objectIds is required")
	}
	if len(dto.ObjectIDs) > 100 {
		return apperror.NewBadRequest("maximum 100 objects allowed per batch")
	}

	// Verify agent exists and belongs to project
	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}
	agent, err := h.repo.FindByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", id)
	}

	// Check if this is a reaction agent
	if agent.TriggerType != TriggerTypeReaction {
		return apperror.NewBadRequest("batch trigger is only available for reaction agents")
	}

	// Process each object
	ctx := c.Request().Context()
	queued := 0
	skipped := 0
	var skippedDetails []struct {
		ObjectID string `json:"objectId"`
		Reason   string `json:"reason"`
	}

	for _, objectID := range dto.ObjectIDs {
		// Check if already processing
		isProcessing, err := h.repo.IsAgentProcessingObject(ctx, id, objectID)
		if err != nil {
			// Log error but continue
			skipped++
			skippedDetails = append(skippedDetails, struct {
				ObjectID string `json:"objectId"`
				Reason   string `json:"reason"`
			}{ObjectID: objectID, Reason: "error checking status"})
			continue
		}

		if isProcessing {
			skipped++
			skippedDetails = append(skippedDetails, struct {
				ObjectID string `json:"objectId"`
				Reason   string `json:"reason"`
			}{ObjectID: objectID, Reason: "already processing"})
			continue
		}

		// Create processing log entry
		log := &AgentProcessingLog{
			AgentID:       id,
			GraphObjectID: objectID,
			ObjectVersion: 0, // Will be updated when actually processing
			EventType:     EventTypeUpdated,
			Status:        ProcessingStatusPending,
		}
		if err := h.repo.CreateProcessingLog(ctx, log); err != nil {
			skipped++
			skippedDetails = append(skippedDetails, struct {
				ObjectID string `json:"objectId"`
				Reason   string `json:"reason"`
			}{ObjectID: objectID, Reason: "failed to queue"})
			continue
		}

		queued++
	}

	return c.JSON(http.StatusOK, SuccessResponse(BatchTriggerResponseDTO{
		Queued:         queued,
		Skipped:        skipped,
		SkippedDetails: skippedDetails,
	}))
}

// --- Agent Definition Handlers ---

// ListDefinitions handles GET /api/admin/agent-definitions
func (h *Handler) ListDefinitions(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	definitions, err := h.repo.FindAllDefinitions(c.Request().Context(), user.ProjectID, false)
	if err != nil {
		return apperror.NewInternal("failed to list agent definitions", err)
	}

	dtos := make([]*AgentDefinitionSummaryDTO, len(definitions))
	for i, def := range definitions {
		dtos[i] = def.ToSummaryDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// GetDefinition handles GET /api/admin/agent-definitions/:id
func (h *Handler) GetDefinition(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("definition id is required")
	}

	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}

	def, err := h.repo.FindDefinitionByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent definition", err)
	}
	if def == nil {
		return apperror.NewNotFound("AgentDefinition", id)
	}

	return c.JSON(http.StatusOK, SuccessResponse(def.ToDTO()))
}

// CreateDefinition handles POST /api/admin/agent-definitions
func (h *Handler) CreateDefinition(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	if user.ProjectID == "" {
		return apperror.NewBadRequest("X-Project-ID header is required")
	}

	var dto CreateAgentDefinitionDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	if dto.Name == "" {
		return apperror.NewBadRequest("name is required")
	}

	// Set defaults
	flowType := FlowTypeSingle
	if dto.FlowType != "" {
		flowType = dto.FlowType
	}

	visibility := VisibilityProject
	if dto.Visibility != "" {
		visibility = dto.Visibility
	}

	isDefault := false
	if dto.IsDefault != nil {
		isDefault = *dto.IsDefault
	}

	tools := dto.Tools
	if tools == nil {
		tools = []string{}
	}

	config := dto.Config
	if config == nil {
		config = map[string]any{}
	}

	def := &AgentDefinition{
		ProjectID:       user.ProjectID,
		Name:            dto.Name,
		Description:     dto.Description,
		SystemPrompt:    dto.SystemPrompt,
		Model:           dto.Model,
		Tools:           tools,
		FlowType:        flowType,
		IsDefault:       isDefault,
		MaxSteps:        dto.MaxSteps,
		DefaultTimeout:  dto.DefaultTimeout,
		Visibility:      visibility,
		ACPConfig:       dto.ACPConfig,
		Config:          config,
		WorkspaceConfig: dto.WorkspaceConfig,
	}

	if err := h.repo.CreateDefinition(c.Request().Context(), def); err != nil {
		return apperror.NewInternal("failed to create agent definition", err)
	}

	return c.JSON(http.StatusCreated, SuccessResponse(def.ToDTO()))
}

// UpdateDefinition handles PATCH /api/admin/agent-definitions/:id
func (h *Handler) UpdateDefinition(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("definition id is required")
	}

	var dto UpdateAgentDefinitionDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}

	def, err := h.repo.FindDefinitionByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent definition", err)
	}
	if def == nil {
		return apperror.NewNotFound("AgentDefinition", id)
	}

	// Apply updates
	if dto.Name != nil {
		def.Name = *dto.Name
	}
	if dto.Description != nil {
		def.Description = dto.Description
	}
	if dto.SystemPrompt != nil {
		def.SystemPrompt = dto.SystemPrompt
	}
	if dto.Model != nil {
		def.Model = dto.Model
	}
	if dto.Tools != nil {
		def.Tools = dto.Tools
	}
	if dto.FlowType != nil {
		def.FlowType = *dto.FlowType
	}
	if dto.IsDefault != nil {
		def.IsDefault = *dto.IsDefault
	}
	if dto.MaxSteps != nil {
		def.MaxSteps = dto.MaxSteps
	}
	if dto.DefaultTimeout != nil {
		def.DefaultTimeout = dto.DefaultTimeout
	}
	if dto.Visibility != nil {
		def.Visibility = *dto.Visibility
	}
	if dto.ACPConfig != nil {
		def.ACPConfig = dto.ACPConfig
	}
	if dto.Config != nil {
		def.Config = dto.Config
	}
	if dto.WorkspaceConfig != nil {
		def.WorkspaceConfig = dto.WorkspaceConfig
	}

	if err := h.repo.UpdateDefinition(c.Request().Context(), def); err != nil {
		return apperror.NewInternal("failed to update agent definition", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(def.ToDTO()))
}

// DeleteDefinition handles DELETE /api/admin/agent-definitions/:id
func (h *Handler) DeleteDefinition(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("definition id is required")
	}

	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}

	def, err := h.repo.FindDefinitionByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent definition", err)
	}
	if def == nil {
		return apperror.NewNotFound("AgentDefinition", id)
	}

	if err := h.repo.DeleteDefinition(c.Request().Context(), id); err != nil {
		return apperror.NewInternal("failed to delete agent definition", err)
	}

	return c.JSON(http.StatusOK, APIResponse[any]{Success: true})
}

// --- Project-Scoped Run History Handlers ---

// ListProjectRuns handles GET /api/projects/:projectId/agent-runs
func (h *Handler) ListProjectRuns(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	// Parse pagination params
	limit := 20
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	offset := 0
	if offsetStr := c.QueryParam("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Parse filters
	var filters RunFilters
	if agentID := c.QueryParam("agentId"); agentID != "" {
		filters.AgentID = &agentID
	}
	if statusStr := c.QueryParam("status"); statusStr != "" {
		status := AgentRunStatus(statusStr)
		filters.Status = &status
	}

	runs, totalCount, err := h.repo.FindRunsByProjectPaginated(c.Request().Context(), projectID, filters, limit, offset)
	if err != nil {
		return apperror.NewInternal("failed to list agent runs", err)
	}

	dtos := make([]*AgentRunDTO, len(runs))
	for i, run := range runs {
		dtos[i] = run.ToDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(PaginatedResponse[*AgentRunDTO]{
		Items:      dtos,
		TotalCount: totalCount,
		Limit:      limit,
		Offset:     offset,
	}))
}

// GetProjectRun handles GET /api/projects/:projectId/agent-runs/:runId
func (h *Handler) GetProjectRun(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	run, err := h.repo.FindRunByIDForProject(c.Request().Context(), runID, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	return c.JSON(http.StatusOK, SuccessResponse(run.ToDTO()))
}

// GetRunMessages handles GET /api/projects/:projectId/agent-runs/:runId/messages
func (h *Handler) GetRunMessages(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	// Verify the run belongs to this project
	run, err := h.repo.FindRunByIDForProject(c.Request().Context(), runID, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	messages, err := h.repo.FindMessagesByRunID(c.Request().Context(), runID)
	if err != nil {
		return apperror.NewInternal("failed to get run messages", err)
	}

	dtos := make([]*AgentRunMessageDTO, len(messages))
	for i, msg := range messages {
		dtos[i] = msg.ToDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// GetRunToolCalls handles GET /api/projects/:projectId/agent-runs/:runId/tool-calls
func (h *Handler) GetRunToolCalls(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	// Verify the run belongs to this project
	run, err := h.repo.FindRunByIDForProject(c.Request().Context(), runID, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	toolCalls, err := h.repo.FindToolCallsByRunID(c.Request().Context(), runID)
	if err != nil {
		return apperror.NewInternal("failed to get run tool calls", err)
	}

	dtos := make([]*AgentRunToolCallDTO, len(toolCalls))
	for i, tc := range toolCalls {
		dtos[i] = tc.ToDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// --- Workspace Config Handlers ---

// GetSession handles GET /api/v1/agent/sessions/:id
// @Summary      Get agent session status
// @Description  Returns session status (workspace lifecycle) for an agent run
// @Tags         agent-sessions
// @Accept       json
// @Produce      json
// @Param        id path string true "Run ID (UUID)"
// @Success      200 {object} APIResponse[AgentRunDTO] "Session/run details with session status"
// @Failure      400 {object} apperror.Error "Invalid run ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Session not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/agent/sessions/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetSession(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("session id is required")
	}

	// Use project-scoped lookup if project ID is available
	var run *AgentRun
	var err error
	if user.ProjectID != "" {
		run, err = h.repo.FindRunByIDForProject(c.Request().Context(), id, user.ProjectID)
	} else {
		run, err = h.repo.FindRunByID(c.Request().Context(), id)
	}
	if err != nil {
		return apperror.NewInternal("failed to get session", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentSession", id)
	}

	return c.JSON(http.StatusOK, SuccessResponse(run.ToDTO()))
}

// GetWorkspaceConfig handles GET /api/admin/agent-definitions/:id/workspace-config
// @Summary      Get workspace config for an agent definition
// @Description  Returns the workspace configuration for an agent definition, or defaults if not set
// @Tags         agent-definitions
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent Definition ID (UUID)"
// @Success      200 {object} APIResponse[workspace.AgentWorkspaceConfig] "Workspace config"
// @Failure      400 {object} apperror.Error "Invalid definition ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent definition not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agent-definitions/{id}/workspace-config [get]
// @Security     bearerAuth
func (h *Handler) GetWorkspaceConfig(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("definition id is required")
	}

	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}

	def, err := h.repo.FindDefinitionByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent definition", err)
	}
	if def == nil {
		return apperror.NewNotFound("AgentDefinition", id)
	}

	// Parse workspace config from the definition's JSONB field
	cfg, err := workspace.ParseAgentWorkspaceConfig(def.WorkspaceConfig)
	if err != nil {
		return apperror.NewInternal("failed to parse workspace config", err)
	}

	// Return default config if none is set
	if cfg == nil {
		cfg = workspace.DefaultAgentWorkspaceConfig()
	}

	return c.JSON(http.StatusOK, SuccessResponse(cfg))
}

// UpdateWorkspaceConfig handles PUT /api/admin/agent-definitions/:id/workspace-config
// @Summary      Update workspace config for an agent definition
// @Description  Sets or replaces the workspace configuration for an agent definition
// @Tags         agent-definitions
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent Definition ID (UUID)"
// @Param        request body workspace.AgentWorkspaceConfig true "Workspace configuration"
// @Success      200 {object} APIResponse[workspace.AgentWorkspaceConfig] "Updated workspace config"
// @Failure      400 {object} apperror.Error "Invalid definition ID, request body, or validation error"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent definition not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agent-definitions/{id}/workspace-config [put]
// @Security     bearerAuth
func (h *Handler) UpdateWorkspaceConfig(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("definition id is required")
	}

	var cfg workspace.AgentWorkspaceConfig
	if err := c.Bind(&cfg); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Normalize tools before validation
	cfg.NormalizeTools()

	// Validate
	if errs := cfg.Validate(); len(errs) > 0 {
		return apperror.NewBadRequest("workspace config validation failed: " + strings.Join(errs, "; "))
	}

	// Look up the definition
	var projectID *string
	if user.ProjectID != "" {
		projectID = &user.ProjectID
	}

	def, err := h.repo.FindDefinitionByID(c.Request().Context(), id, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent definition", err)
	}
	if def == nil {
		return apperror.NewNotFound("AgentDefinition", id)
	}

	// Convert to map for JSONB storage
	cfgMap, err := cfg.ToMap()
	if err != nil {
		return apperror.NewInternal("failed to serialize workspace config", err)
	}

	def.WorkspaceConfig = cfgMap

	if err := h.repo.UpdateDefinition(c.Request().Context(), def); err != nil {
		return apperror.NewInternal("failed to update agent definition", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(&cfg))
}
