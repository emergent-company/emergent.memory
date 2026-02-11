package agents

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/auth"
)

// Handler handles HTTP requests for agents
type Handler struct {
	repo *Repository
}

// NewHandler creates a new agents handler
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
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
	if dto.ProjectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	if dto.Name == "" {
		return apperror.NewBadRequest("name is required")
	}
	if dto.StrategyType == "" {
		return apperror.NewBadRequest("strategyType is required")
	}
	if dto.CronSchedule == "" {
		return apperror.NewBadRequest("cronSchedule is required")
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

	// For now, just create a run record - actual execution would be handled by the scheduler
	// In a full implementation, this would trigger the agent execution via a job queue
	run, err := h.repo.CreateRun(c.Request().Context(), id)
	if err != nil {
		return apperror.NewInternal("failed to create run", err)
	}

	// Mark it as skipped for now since we don't have execution implemented
	_ = h.repo.SkipRun(c.Request().Context(), run.ID, "Manual trigger - execution not yet implemented in Go server")

	msg := "Agent triggered successfully (run ID: " + run.ID + ")"
	return c.JSON(http.StatusOK, TriggerResponseDTO{
		Success: true,
		Message: &msg,
	})
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
