package agents

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// normalizeQueueName trims a queue name and maps empty to the default queue.
func normalizeQueueName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultQueueName
	}
	return name
}

// AgentQueueDTO is the response representation of a named work queue.
type AgentQueueDTO struct {
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description"`
	Concurrency int       `json:"concurrency"`
	Priority    int       `json:"priority"`
	Enabled     bool      `json:"enabled"`
	Pending     int       `json:"pending"`
	Processing  int       `json:"processing"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// CreateAgentQueueDTO is the request body for creating a queue.
type CreateAgentQueueDTO struct {
	Name        string `json:"name" validate:"required"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Concurrency int    `json:"concurrency"`
	Priority    *int   `json:"priority"`
	Enabled     *bool  `json:"enabled"`
}

// UpdateAgentQueueDTO is the request body for updating a queue.
type UpdateAgentQueueDTO struct {
	DisplayName *string `json:"displayName"`
	Description *string `json:"description"`
	Concurrency *int    `json:"concurrency"`
	Priority    *int    `json:"priority"`
	Enabled     *bool   `json:"enabled"`
}

// EnqueueWorkItemDTO is the request body for enqueuing a work item.
type EnqueueWorkItemDTO struct {
	AgentID   string         `json:"agentId"`
	AgentName string         `json:"agentName"`
	Message   string         `json:"message"`
	Priority  int            `json:"priority"`
	Metadata  map[string]any `json:"metadata"`
}

// EnqueueWorkItemResponseDTO is returned after a work item is enqueued.
type EnqueueWorkItemResponseDTO struct {
	RunID   string `json:"runId"`
	Queue   string `json:"queue"`
	AgentID string `json:"agentId"`
	Status  string `json:"status"`
}

func toAgentQueueDTO(q *AgentQueue) *AgentQueueDTO {
	return &AgentQueueDTO{
		Name:        q.Name,
		DisplayName: q.DisplayName,
		Description: q.Description,
		Concurrency: q.Concurrency,
		Priority:    q.Priority,
		Enabled:     q.Enabled,
		Pending:     q.Pending,
		Processing:  q.Processing,
		CreatedAt:   q.CreatedAt,
		UpdatedAt:   q.UpdatedAt,
	}
}

// ListQueues handles GET /api/projects/:projectId/agent-queues
//
// @Summary      List agent work queues
// @Description  Lists a project's named agent work queues with live pending/processing depth
// @Tags         agents
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Success      200 {object} APIResponse[[]AgentQueueDTO] "Agent queues"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Router       /api/projects/{projectId}/agent-queues [get]
// @Security     bearerAuth
func (h *Handler) ListQueues(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	if err := h.repo.EnsureDefaultQueue(c.Request().Context(), projectID); err != nil {
		return apperror.NewInternal("failed to ensure default queue", err)
	}
	queues, err := h.repo.ListQueues(c.Request().Context(), projectID)
	if err != nil {
		return apperror.NewInternal("failed to list agent queues", err)
	}
	out := make([]*AgentQueueDTO, 0, len(queues))
	for i := range queues {
		out = append(out, toAgentQueueDTO(&queues[i]))
	}
	return c.JSON(http.StatusOK, SuccessResponse(out))
}

// CreateQueue handles POST /api/projects/:projectId/agent-queues
//
// @Summary      Create an agent work queue
// @Description  Creates a named agent work queue with an optional concurrency and priority
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        body body CreateAgentQueueDTO true "Queue"
// @Success      201 {object} APIResponse[AgentQueueDTO] "Created queue"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      409 {object} apperror.Error "Queue already exists"
// @Router       /api/projects/{projectId}/agent-queues [post]
// @Security     bearerAuth
func (h *Handler) CreateQueue(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	var dto CreateAgentQueueDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return apperror.NewBadRequest("queue name is required")
	}
	if dto.Concurrency < 0 {
		return apperror.NewBadRequest("concurrency must be at least 1")
	}
	concurrency := dto.Concurrency
	if concurrency == 0 {
		concurrency = 1
	}
	priority := DefaultQueuePriority
	if dto.Priority != nil {
		priority = *dto.Priority
	}
	enabled := true
	if dto.Enabled != nil {
		enabled = *dto.Enabled
	}

	existing, err := h.repo.GetQueue(c.Request().Context(), projectID, name)
	if err != nil && err != ErrQueueNotFound {
		return apperror.NewInternal("failed to check for existing queue", err)
	}
	if existing != nil {
		return apperror.New(http.StatusConflict, "conflict", "a queue named '"+name+"' already exists in this project")
	}

	q := &AgentQueue{
		ProjectID:   projectID,
		Name:        name,
		DisplayName: dto.DisplayName,
		Description: dto.Description,
		Concurrency: concurrency,
		Priority:    priority,
		Enabled:     enabled,
	}
	if err := h.repo.UpsertQueue(c.Request().Context(), q); err != nil {
		return apperror.NewInternal("failed to create agent queue", err)
	}
	return c.JSON(http.StatusCreated, SuccessResponse(toAgentQueueDTO(q)))
}

// UpdateQueue handles PATCH /api/projects/:projectId/agent-queues/:name
//
// @Summary      Update an agent work queue
// @Description  Updates a queue's display name, description, concurrency, priority, or enabled flag
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        name path string true "Queue name"
// @Param        body body UpdateAgentQueueDTO true "Queue updates"
// @Success      200 {object} APIResponse[AgentQueueDTO] "Updated queue"
// @Failure      404 {object} apperror.Error "Queue not found"
// @Router       /api/projects/{projectId}/agent-queues/{name} [patch]
// @Security     bearerAuth
func (h *Handler) UpdateQueue(c echo.Context) error {
	projectID := c.Param("projectId")
	name := c.Param("name")
	if projectID == "" || name == "" {
		return apperror.NewBadRequest("projectId and name are required")
	}
	var dto UpdateAgentQueueDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	q, err := h.repo.GetQueue(c.Request().Context(), projectID, name)
	if err != nil {
		if err == ErrQueueNotFound {
			return apperror.NewNotFound("AgentQueue", name)
		}
		return apperror.NewInternal("failed to get agent queue", err)
	}
	if dto.DisplayName != nil {
		q.DisplayName = *dto.DisplayName
	}
	if dto.Description != nil {
		q.Description = *dto.Description
	}
	if dto.Concurrency != nil {
		if *dto.Concurrency < 1 {
			return apperror.NewBadRequest("concurrency must be at least 1")
		}
		q.Concurrency = *dto.Concurrency
	}
	if dto.Priority != nil {
		q.Priority = *dto.Priority
	}
	if dto.Enabled != nil {
		q.Enabled = *dto.Enabled
	}
	if err := h.repo.UpdateQueue(c.Request().Context(), q); err != nil {
		if err == ErrQueueNotFound {
			return apperror.NewNotFound("AgentQueue", name)
		}
		return apperror.NewInternal("failed to update agent queue", err)
	}
	return c.JSON(http.StatusOK, SuccessResponse(toAgentQueueDTO(q)))
}

// DeleteQueue handles DELETE /api/projects/:projectId/agent-queues/:name
//
// @Summary      Delete an agent work queue
// @Description  Deletes a queue that has no pending or processing jobs
// @Tags         agents
// @Param        projectId path string true "Project ID"
// @Param        name path string true "Queue name"
// @Success      204 "Deleted"
// @Failure      404 {object} apperror.Error "Queue not found"
// @Failure      409 {object} apperror.Error "Queue has jobs in flight"
// @Router       /api/projects/{projectId}/agent-queues/{name} [delete]
// @Security     bearerAuth
func (h *Handler) DeleteQueue(c echo.Context) error {
	projectID := c.Param("projectId")
	name := c.Param("name")
	if projectID == "" || name == "" {
		return apperror.NewBadRequest("projectId and name are required")
	}
	err := h.repo.DeleteQueue(c.Request().Context(), projectID, name)
	if err != nil {
		switch err {
		case ErrQueueNotFound:
			return apperror.NewNotFound("AgentQueue", name)
		case ErrQueueNotEmpty:
			return apperror.New(http.StatusConflict, "conflict", "queue '"+name+"' still has pending or processing jobs")
		default:
			return apperror.NewInternal("failed to delete agent queue", err)
		}
	}
	return c.NoContent(http.StatusNoContent)
}

// EnqueueWorkItem handles POST /api/projects/:projectId/agent-queues/:name/enqueue
//
// @Summary      Enqueue a work item onto a queue
// @Description  Creates a queued agent run on the named queue, optionally selecting a runtime agent and attaching trigger metadata
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        name path string true "Queue name"
// @Param        body body EnqueueWorkItemDTO true "Work item"
// @Success      202 {object} APIResponse[EnqueueWorkItemResponseDTO] "Enqueued"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      404 {object} apperror.Error "Queue or agent not found"
// @Router       /api/projects/{projectId}/agent-queues/{name}/enqueue [post]
// @Security     bearerAuth
func (h *Handler) EnqueueWorkItem(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	name := c.Param("name")
	if projectID == "" || name == "" {
		return apperror.NewBadRequest("projectId and name are required")
	}
	var dto EnqueueWorkItemDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	ctx := c.Request().Context()

	if _, err := h.repo.GetQueue(ctx, projectID, name); err != nil {
		if err == ErrQueueNotFound {
			return apperror.NewNotFound("AgentQueue", name)
		}
		return apperror.NewInternal("failed to get agent queue", err)
	}

	var agent *Agent
	var err error
	switch {
	case dto.AgentID != "":
		agent, err = h.repo.FindByID(ctx, dto.AgentID, &projectID)
	case dto.AgentName != "":
		agent, err = h.repo.FindByName(ctx, projectID, dto.AgentName)
	default:
		return apperror.NewBadRequest("either agentId or agentName is required")
	}
	if err != nil {
		return apperror.NewInternal("failed to resolve agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", dto.AgentID+dto.AgentName)
	}
	// A queued run is executed by the worker, not by this request; scope the
	// project check to the caller's project to prevent cross-project enqueue.
	if user.ProjectID != "" && agent.ProjectID != user.ProjectID {
		return apperror.NewNotFound("Agent", agent.ID)
	}

	message := strings.TrimSpace(dto.Message)
	if message == "" && agent.Prompt != nil {
		message = *agent.Prompt
	}
	if message == "" {
		message = "Execute agent tasks"
	}

	run, err := h.repo.CreateRunQueued(ctx, agent.ID, 1, CreateRunQueuedOptions{
		Queue:           name,
		Priority:        dto.Priority,
		TriggerMessage:  &message,
		TriggerMetadata: dto.Metadata,
		TrustedInternal: true,
	})
	if err != nil {
		return apperror.NewInternal("failed to enqueue work item", err)
	}

	return c.JSON(http.StatusAccepted, SuccessResponse(&EnqueueWorkItemResponseDTO{
		RunID:   run.ID,
		Queue:   name,
		AgentID: agent.ID,
		Status:  string(RunStatusQueued),
	}))
}
