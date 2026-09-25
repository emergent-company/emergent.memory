package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/sse"
)

// Handler handles HTTP requests for agents
type Handler struct {
	repo          *Repository
	executor      *AgentExecutor // may be nil in tests
	rateLimiter   *WebhookRateLimiter
	tempoBaseURL  string        // internal Tempo query URL; empty when tracing disabled
	pricing       pricingLookup // optional; nil when provider repo not available
	usage         usageLookup   // optional; nil when provider repo not available
	providerRepo  *provider.Repository
	sandboxStore  sandboxStoreLookup
	modelResolver modelResolverLookup // optional; nil when modelconfig not available
	mcpTools      *MCPToolHandler     // remember-status tool handler; wired via WithMCPToolHandler
	mcpService    *mcp.Service        // tool catalog source for the computed toolGroups; wired via WithMCPService
}

// usageLookup is the internal interface for looking up project spend.
type usageLookup interface {
	CheckBudgetExceeded(ctx context.Context, projectID string) (bool, error)
}

// ModelResolverLookup is the interface for resolving the effective generative
// model for a project. Implemented by domain/modelconfig.Service via an adapter.
// Exported so that domain/modelconfig can wire it in without an import cycle.
type ModelResolverLookup interface {
	ResolveGenerativeModelByID(ctx context.Context, projectID string) (model string, source string, err error)
}

// modelResolverLookup is kept as an internal alias for use within this package.
type modelResolverLookup = ModelResolverLookup

// sandboxStoreLookup is the internal interface for looking up sandbox records by session ID.
type sandboxStoreLookup interface {
	GetBySessionID(ctx context.Context, sessionID string) (*sandbox.AgentSandbox, error)
}

// pricingLookup is the internal interface used by Handler to resolve model pricing.
// Kept unexported; satisfied by *provider.Repository via providerPricingAdapter.
type pricingLookup interface {
	lookupModelPricing(ctx context.Context, model string) (provider string, textIn float64, out float64, found bool)
}

// NewHandler creates a new agents handler
func NewHandler(repo *Repository, executor *AgentExecutor, rateLimiter *WebhookRateLimiter, tempoBaseURL string, pricing pricingLookup, usage usageLookup, providerRepo *provider.Repository, sandboxStore sandboxStoreLookup) *Handler {
	return &Handler{repo: repo, executor: executor, rateLimiter: rateLimiter, tempoBaseURL: tempoBaseURL, pricing: pricing, usage: usage, providerRepo: providerRepo, sandboxStore: sandboxStore}
}

// WithModelResolver attaches a model resolver to the handler (called from fx Invoke).
func (h *Handler) WithModelResolver(mr modelResolverLookup) {
	h.modelResolver = mr
}

// WithMCPToolHandler attaches the remember-status MCP tool handler to the REST
// Handler so the project-scoped run route can reuse buildRememberStatus (the
// shared aggregation used by both the MCP tool and the HTTP route). Called from
// fx Invoke (registerHandlerMCPToolHandler).
func (h *Handler) WithMCPToolHandler(mt *MCPToolHandler) {
	h.mcpTools = mt
}

// WithMCPService attaches the MCP service to the REST Handler so the
// agent-definition read path can compute the full tool-group catalog (scope
// resolution for dynamic tools). Called from fx Invoke
// (registerHandlerMCPService). When unset, the read path degrades to
// agent-referenced-tools-only membership.
func (h *Handler) WithMCPService(s *mcp.Service) {
	h.mcpService = s
}

// getWorkspaceInfo loads sandbox details for a run, returning nil if unavailable.
func (h *Handler) getWorkspaceInfo(ctx context.Context, runID string) *RunWorkspaceDTO {
	if h.sandboxStore == nil {
		return nil
	}
	sb, err := h.sandboxStore.GetBySessionID(ctx, runID)
	if err != nil || sb == nil {
		return nil
	}
	return &RunWorkspaceDTO{
		Provider:    string(sb.Provider),
		ContainerID: sb.ProviderWorkspaceID,
		BaseImage:   sb.BaseImage,
		ImageDigest: sb.ImageDigest,
	}
}

// enrichRunTrace populates a run DTO's TokenUsage and Spans. Token usage is
// resolved from llm_usage_events (DB) when present, falling back to
// trace-based aggregation when no events exist but the run has a trace ID.
// When the trace fallback is used and a model name is found, cost is computed
// from the pricing table. The flattened trace spans are attached from the same
// single Tempo fetch so project-scoped clients can read spans without admin
// access to Tempo. Both fields stay nil (omitted) when tracing is disabled or
// Tempo is unreachable.
func (h *Handler) enrichRunTrace(ctx context.Context, dto *AgentRunDTO, runID string, traceID *string) {
	usage, err := h.repo.GetRunTokenUsage(ctx, runID)
	if err != nil {
		usage = nil
	}

	// Trace fetch only happens when tracing is enabled AND the run has a trace.
	if traceID != nil && *traceID != "" && h.tempoBaseURL != "" {
		if spans, traceUsage, _ := GetTraceSpans(ctx, h.tempoBaseURL, *traceID); spans != nil {
			dto.Spans = spans
			// Trace usage is only a fallback when no DB usage events exist.
			if usage == nil && traceUsage != nil {
				usage = traceUsage
				// Compute cost from pricing table when model is known.
				if usage.Model != "" && h.pricing != nil {
					if prov, textIn, out, ok := h.pricing.lookupModelPricing(ctx, usage.Model); ok {
						const perMillion = 1_000_000.0
						usage.Provider = prov
						usage.EstimatedCostUSD = float64(usage.TotalInputTokens)*textIn/perMillion +
							float64(usage.TotalOutputTokens)*out/perMillion
					}
				}
			}
		}
	}

	dto.TokenUsage = usage
}

// mapExecutorError converts typed executor errors to appropriate HTTP responses.
//   - BudgetExceededError → 402 Payment Required
//   - QueueFullError      → 429 Too Many Requests
//   - other              → 500 Internal Server Error
func mapExecutorError(err error) *apperror.Error {
	var budgetErr *BudgetExceededError
	if errors.As(err, &budgetErr) {
		return apperror.New(http.StatusPaymentRequired, "budget_exceeded",
			"Project monthly budget has been exceeded. Please review your spending or increase your budget.")
	}
	var queueErr *QueueFullError
	if errors.As(err, &queueErr) {
		return apperror.New(http.StatusTooManyRequests, "queue_full",
			fmt.Sprintf("Agent run queue is full (%d/%d pending jobs). Wait for existing runs to complete before triggering new ones.",
				queueErr.PendingJobs, queueErr.MaxPendingJobs))
	}
	return apperror.NewInternal("failed to execute agent", err)
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
	user := auth.MustGetUser(c)

	// Prefer URL :projectId param (project-scoped routes), fall back to header
	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	agents, err := h.repo.FindAll(c.Request().Context(), projectID)
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
	user := auth.MustGetUser(c)

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

	dto := agent.ToDTO()

	// Enrich with budget info if available
	if h.providerRepo != nil {
		// Get project budget
		var project struct {
			BudgetUSD *float64 `bun:"budget_usd"`
		}
		err := h.repo.db.NewSelect().
			TableExpr("kb.projects").
			ColumnExpr("budget_usd").
			Where("id = ?", agent.ProjectID).
			Scan(c.Request().Context(), &project)
		if err == nil {
			dto.BudgetUSD = project.BudgetUSD
		}

		// Get current spend
		spend, err := h.providerRepo.GetProjectCurrentMonthSpend(c.Request().Context(), agent.ProjectID)
		if err == nil {
			dto.CurrentMonthSpend = &spend
		}
	}

	return c.JSON(http.StatusOK, SuccessResponse(dto))
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
	user := auth.MustGetUser(c)

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

	var dto CreateAgentDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	// Allow projectId from URL param to override or supplement the body field
	if urlProjectID := c.Param("projectId"); urlProjectID != "" {
		dto.ProjectID = urlProjectID
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

	// Validate cron interval when trigger type is schedule.
	if triggerType == TriggerTypeSchedule && dto.CronSchedule != "" {
		minMinutes := 15 // safe default
		if h.executor != nil && h.executor.safeguards.MinCronIntervalMinutes > 0 {
			minMinutes = h.executor.safeguards.MinCronIntervalMinutes
		}
		if err := validateCronInterval(dto.CronSchedule, minMinutes); err != nil {
			return apperror.NewBadRequest(fmt.Sprintf("invalid cron schedule: %s", err.Error()))
		}
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
		ProjectID:         dto.ProjectID,
		Name:              dto.Name,
		StrategyType:      dto.StrategyType,
		Prompt:            dto.Prompt,
		CronSchedule:      dto.CronSchedule,
		Enabled:           enabled,
		TriggerType:       triggerType,
		ReactionConfig:    dto.ReactionConfig,
		ExecutionMode:     executionMode,
		Capabilities:      dto.Capabilities,
		Config:            config,
		Description:       dto.Description,
		AgentDefinitionID: dto.AgentDefinitionID,
	}

	if err := h.repo.Create(c.Request().Context(), agent); err != nil {
		if strings.Contains(err.Error(), "foreign key") || strings.Contains(err.Error(), "violates foreign key") {
			return apperror.NewNotFound("project", "not found")
		}
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
	user := auth.MustGetUser(c)

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
		// Clearing disabled_reason when re-enabling so the agent can run again.
		if *dto.Enabled {
			agent.DisabledReason = nil
		}
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
	if dto.AgentDefinitionID != nil {
		agent.AgentDefinitionID = dto.AgentDefinitionID
	}
	// disabledReason is admin-only: only applied if caller has admin:write scope.
	// An explicit empty string clears the disabled reason.
	if dto.DisabledReason != nil && user.HasScope("admin:write") {
		if *dto.DisabledReason == "" {
			agent.DisabledReason = nil
		} else {
			agent.DisabledReason = dto.DisabledReason
		}
	}

	// Validate cron interval if cron schedule is being changed or trigger type is schedule.
	if agent.TriggerType == TriggerTypeSchedule && agent.CronSchedule != "" {
		if dto.CronSchedule != nil || dto.TriggerType != nil {
			minMinutes := 15 // safe default
			if h.executor != nil && h.executor.safeguards.MinCronIntervalMinutes > 0 {
				minMinutes = h.executor.safeguards.MinCronIntervalMinutes
			}
			if err := validateCronInterval(agent.CronSchedule, minMinutes); err != nil {
				return apperror.NewBadRequest(fmt.Sprintf("invalid cron schedule: %s", err.Error()))
			}
		}
	}

	if err := h.repo.Update(c.Request().Context(), agent); err != nil {
		return apperror.NewInternal("failed to update agent", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(agent.ToDTO()))
}

// EnableAgent handles POST /api/projects/:projectId/agents/:id/enable
// @Summary      Enable an agent
// @Description  Enables an agent and clears any disabled reason (e.g. AI provider quota exhausted or project budget exceeded)
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID"
// @Param        id path string true "Agent ID (UUID)"
// @Success      200 {object} APIResponse[AgentDTO] "Enabled agent"
// @Failure      400 {object} apperror.Error "Invalid agent ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/agents/{id}/enable [post]
// @Security     bearerAuth
func (h *Handler) EnableAgent(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}

	// Verify agent exists and belongs to project
	agent, err := h.repo.FindByID(c.Request().Context(), id, &projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil {
		return apperror.NewNotFound("Agent", id)
	}

	if err := h.repo.EnableAgent(c.Request().Context(), id); err != nil {
		return apperror.NewInternal("failed to enable agent", err)
	}

	// Re-fetch to get updated state
	agent, _ = h.repo.FindByID(c.Request().Context(), id, &projectID)
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
	user := auth.MustGetUser(c)

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
	user := auth.MustGetUser(c)

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
	agentDef, _ = h.repo.ResolveDefinitionForAgent(c.Request().Context(), agent)

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

	// Create a run record synchronously so we can return the run ID immediately.
	triggerSource := "manual"

	// Resolve model override from trigger request
	var modelOverride *string
	if triggerReq.Model != "" {
		modelOverride = &triggerReq.Model
	}

	// Record which definition the run resolved to, so the run's
	// agent_definition_id matches the definition actually used at execution.
	var agentDefID *string
	if agentDef != nil {
		agentDefID = &agentDef.ID
	}

	run, err := h.repo.CreateRunWithOptions(c.Request().Context(), CreateRunOptions{
		AgentID:           agent.ID,
		TriggerSource:     &triggerSource,
		TriggerMetadata:   triggerReq.Context,
		Model:             modelOverride,
		AgentDefinitionID: agentDefID,
	})
	if err != nil {
		return apperror.NewInternal("failed to create agent run", err)
	}

	// Resolve org ID for the agent's project so the tracking model can
	// attribute LLM usage events to the correct tenant. user.OrgID may be
	// empty for static test tokens and admin users without an X-Org-ID header.
	orgID := user.OrgID
	if orgID == "" {
		orgID, _ = h.repo.GetOrgIDByProjectID(c.Request().Context(), agent.ProjectID)
	}

	// Launch execution asynchronously — manual triggers are fire-and-forget
	// from the caller's perspective. We use context.Background() so the run
	// is not cancelled when the HTTP response is sent.
	// Capture the auth token now (before the HTTP context is gone) so that
	// internal loopback calls (e.g. search-knowledge → /query) can authenticate
	// from within the background goroutine.
	triggerAuthToken := auth.RawTokenFromContext(c.Request().Context())
	go func() {
		defer func() {
			if r := recover(); r != nil {
				h.executor.log.Error("panic in trigger goroutine",
					slog.String("run_id", run.ID),
					slog.Any("panic", r),
				)
				ctx := context.Background()
				if repoErr := h.repo.FailRunWithSteps(ctx, run.ID, fmt.Sprintf("panic: %v", r), run.StepCount); repoErr != nil {
					h.executor.log.Error("failed to mark run as error after panic",
						slog.String("run_id", run.ID),
						slog.String("error", repoErr.Error()),
					)
				}
			}
		}()
		bgCtx := context.Background()
		execResult, execErr := h.executor.ExecuteWithRun(bgCtx, run, ExecuteRequest{
			Agent:           agent,
			AgentDefinition: agentDef,
			ProjectID:       agent.ProjectID,
			OrgID:           orgID,
			UserMessage:     userMessage,
			UserID:          user.ID, // propagate for ask_user notifications
			TriggerMetadata: triggerReq.Context,
			Model:           triggerReq.Model,
			EnvVars:         triggerReq.EnvVars,
			MaxSteps:        triggerReq.MaxSteps,
			AuthToken:       triggerAuthToken,
			SessionID:       triggerReq.SessionID,
			TrustedInternal: true, // session UI is a trusted surface (full internal coordination)
		})
		if execResult != nil && execResult.Cleanup != nil {
			execResult.Cleanup()
		}
		if execErr != nil {
			h.executor.log.Error("async agent execution failed",
				slog.String("run_id", run.ID),
				slog.String("agent_id", agent.ID),
				slog.String("error", execErr.Error()),
			)
		}
	}()

	msg := "Agent triggered successfully (run ID: " + run.ID + ")"
	return c.JSON(http.StatusOK, TriggerResponseDTO{
		Success: true,
		RunID:   &run.ID,
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
	user := auth.MustGetUser(c)

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
	user := auth.MustGetUser(c)

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
	user := auth.MustGetUser(c)

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

// --- Admin Webhook Hook Handlers ---

// CreateWebhookHook handles POST /api/admin/agents/:id/hooks
func (h *Handler) CreateWebhookHook(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("agent id is required")
	}

	var dto CreateAgentWebhookHookDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if dto.Label == "" {
		return apperror.NewBadRequest("label is required")
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

	rawToken, err := GenerateWebhookToken()
	if err != nil {
		return apperror.NewInternal("failed to generate token", err)
	}

	hashedToken, err := HashWebhookToken(rawToken)
	if err != nil {
		return apperror.NewInternal("failed to hash token", err)
	}

	hook := &AgentWebhookHook{
		AgentID:         agent.ID,
		ProjectID:       agent.ProjectID,
		Label:           dto.Label,
		TokenHash:       hashedToken,
		Enabled:         true,
		RateLimitConfig: dto.RateLimitConfig,
	}

	if err := h.repo.CreateWebhookHook(c.Request().Context(), hook); err != nil {
		return apperror.NewInternal("failed to create webhook hook", err)
	}

	hook.Token = &rawToken
	return c.JSON(http.StatusCreated, SuccessResponse(hook.ToDTO()))
}

// ListWebhookHooks handles GET /api/admin/agents/:id/hooks
func (h *Handler) ListWebhookHooks(c echo.Context) error {
	user := auth.MustGetUser(c)

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

	hooks, err := h.repo.FindWebhookHooksByAgent(c.Request().Context(), agent.ID, agent.ProjectID)
	if err != nil {
		return apperror.NewInternal("failed to list webhook hooks", err)
	}

	dtos := make([]*AgentWebhookHookDTO, len(hooks))
	for i, hook := range hooks {
		dtos[i] = hook.ToDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// DeleteWebhookHook handles DELETE /api/admin/agents/:id/hooks/:hookId
func (h *Handler) DeleteWebhookHook(c echo.Context) error {
	user := auth.MustGetUser(c)

	agentID := c.Param("id")
	hookID := c.Param("hookId")
	if agentID == "" || hookID == "" {
		return apperror.NewBadRequest("agent id and hook id are required")
	}

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

	if err := h.repo.DeleteWebhookHook(c.Request().Context(), hookID, agent.ProjectID); err != nil {
		return apperror.NewInternal("failed to delete webhook hook", err)
	}

	if h.rateLimiter != nil {
		h.rateLimiter.RemoveLimiter(hookID)
	}

	return c.JSON(http.StatusOK, APIResponse[any]{Success: true})
}

// --- Public Webhook Receiver ---

// ReceiveWebhook handles POST /api/webhooks/agents/:hookId
func (h *Handler) ReceiveWebhook(c echo.Context) error {
	hookID := c.Param("hookId")
	if hookID == "" {
		return apperror.NewBadRequest("hookId is required")
	}

	// Extract Bearer token
	authHeader := c.Request().Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		return apperror.ErrUnauthorized.WithMessage("missing or invalid authorization header")
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	// Find the webhook hook
	hook, err := h.repo.FindWebhookHookByID(c.Request().Context(), hookID)
	if err != nil {
		return apperror.NewInternal("failed to retrieve hook", err)
	}
	if hook == nil {
		return apperror.ErrUnauthorized.WithMessage("invalid hook")
	}
	if !hook.Enabled {
		return apperror.NewForbidden("hook is disabled")
	}

	// Verify the token
	if !VerifyWebhookToken(token, hook.TokenHash) {
		return apperror.ErrUnauthorized.WithMessage("invalid token")
	}

	// Enforce rate limiting
	if h.rateLimiter != nil {
		if !h.rateLimiter.CheckRateLimit(c.Request().Context(), hook.ID, hook.RateLimitConfig) {
			return apperror.New(http.StatusTooManyRequests, "rate_limit_exceeded", "too many requests")
		}
	}

	// Find the associated agent
	agent, err := h.repo.FindByID(c.Request().Context(), hook.AgentID, nil)
	if err != nil {
		return apperror.NewInternal("failed to get agent", err)
	}
	if agent == nil || !agent.Enabled {
		return apperror.NewBadRequest("agent not found or disabled")
	}

	// Look up the agent definition
	var agentDef *AgentDefinition
	agentDef, _ = h.repo.ResolveDefinitionForAgent(c.Request().Context(), agent)

	// Parse payload
	var payload WebhookTriggerPayloadDTO
	_ = c.Bind(&payload) // body is optional

	// Build metadata
	metadata := map[string]any{
		"hookId": hook.ID,
		"label":  hook.Label,
	}
	if payload.Context != nil {
		metadata["context"] = payload.Context
	}

	triggerSource := "webhook:" + hook.ID

	if h.executor == nil {
		// Fallback for tests/stub mode — use CreateRunWithOptions to persist trigger fields
		run, err := h.repo.CreateRunWithOptions(c.Request().Context(), CreateRunOptions{
			AgentID:         agent.ID,
			TriggerSource:   &triggerSource,
			TriggerMetadata: metadata,
		})
		if err != nil {
			return apperror.NewInternal("failed to create run", err)
		}

		_ = h.repo.SkipRun(c.Request().Context(), run.ID, "Executor not available")
		msg := "Agent triggered (stub mode)"
		return c.JSON(http.StatusAccepted, TriggerResponseDTO{
			Success: true,
			RunID:   &run.ID,
			Message: &msg,
		})
	}

	// Build user message
	userMessage := "Execute agent tasks"
	if payload.Prompt != "" {
		userMessage = payload.Prompt
	} else if agent.Prompt != nil && *agent.Prompt != "" {
		userMessage = *agent.Prompt
	}

	// Resolve org ID for the agent's project so the tracking model can attribute
	// LLM usage events to the correct tenant.
	orgID, _ := h.repo.GetOrgIDByProjectID(c.Request().Context(), agent.ProjectID)

	// Execute
	var maxSteps *int
	if payload.MaxSteps != nil {
		maxSteps = payload.MaxSteps // per-request override takes precedence
	} else if agentDef != nil && agentDef.MaxSteps != nil {
		maxSteps = agentDef.MaxSteps
	}
	req := ExecuteRequest{
		Agent:           agent,
		AgentDefinition: agentDef,
		ProjectID:       agent.ProjectID,
		OrgID:           orgID,
		UserMessage:     userMessage,
		TriggerSource:   &triggerSource,
		TriggerMetadata: metadata,
		MaxSteps:        maxSteps,
		// The webhook receiver is a public, external-facing surface authenticated
		// only by a per-hook bearer token (no RequireAuth). It must NOT be trusted
		// internal coordination, or a holder of a shared/leaked webhook secret could
		// reach internal-visible agents via spawn_agents / list_available_agents.
		TrustedInternal: false,
	}

	result, err := h.executor.Execute(c.Request().Context(), req)
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}
	if err != nil {
		return mapExecutorError(err)
	}

	msg := "Agent triggered successfully"
	return c.JSON(http.StatusAccepted, TriggerResponseDTO{
		Success: true,
		RunID:   &result.RunID,
		Message: &msg,
	})
}

// --- Agent Definition Handlers ---

// ListDefinitions handles GET /api/projects/:projectId/agent-definitions
// @Summary      List agent definitions
// @Description  Returns all agent definitions for a project, each with the effective generative model it would run with (per-agent override when set, else the project's resolved default)
// @Tags         agent-definitions
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Success      200 {object} APIResponse[[]AgentDefinitionSummaryDTO] "List of agent definitions"
// @Failure      400 {object} apperror.Error "Invalid project ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/agent-definitions [get]
// @Security     bearerAuth
func (h *Handler) ListDefinitions(c echo.Context) error {
	user := auth.MustGetUser(c)

	// Prefer URL :projectId param (project-scoped routes), fall back to header
	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	definitions, err := h.repo.FindAllDefinitions(c.Request().Context(), projectID, false)
	if err != nil {
		return apperror.NewInternal("failed to list agent definitions", err)
	}

	// Resolve the project's generative default once — the whole list shares one
	// project, so per-item resolution would be N+1. Each agent's own override
	// (when set) still takes precedence over the default.
	projectDefault := ""
	if h.modelResolver != nil && projectID != "" {
		if m, _, rerr := h.modelResolver.ResolveGenerativeModelByID(c.Request().Context(), projectID); rerr == nil {
			projectDefault = m
		}
	}

	dtos := make([]*AgentDefinitionSummaryDTO, len(definitions))
	for i, def := range definitions {
		dto := def.ToSummaryDTO()
		var override string
		if def.Model != nil {
			override = def.Model.Name
		}
		dto.EffectiveModel = pickEffectiveModel(override, projectDefault)
		dtos[i] = dto
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// GetDefinition handles GET /api/projects/:projectId/agent-definitions/:id
// @Summary      Get agent definition by ID
// @Description  Returns an agent definition with its effective generative model resolved (per-agent override when set, else project config → provider-credential generative model)
// @Tags         agent-definitions
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        id path string true "Agent Definition ID (UUID)"
// @Success      200 {object} APIResponse[AgentDefinitionDTO] "Agent definition details"
// @Failure      400 {object} apperror.Error "Missing definition ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent definition not found (invalid or unknown definition ID)"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/agent-definitions/{id} [get]
// @Security     bearerAuth
func (h *Handler) GetDefinition(c echo.Context) error {
	user := auth.MustGetUser(c)

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

	dto := h.toDefinitionDTO(c.Request().Context(), def)

	// Effective model = what this definition will run with. The per-agent
	// override is honored by the executor regardless of project config, so it
	// is reported unconditionally. Without an override, fall back to the
	// project's resolved generative default (project config →
	// provider-credential generative model); it may be empty only when the
	// project is truly unconfigured.
	var projectDefault string
	if h.modelResolver != nil {
		if resolved, _, err := h.modelResolver.ResolveGenerativeModelByID(c.Request().Context(), def.ProjectID); err == nil {
			projectDefault = resolved
		}
	}
	var override string
	if dto.Model != nil {
		override = dto.Model.Name
	}
	dto.EffectiveModel = pickEffectiveModel(override, projectDefault)

	return c.JSON(http.StatusOK, SuccessResponse(dto))
}

// pickEffectiveModel applies the precedence rule shared by GET and LIST agent
// definitions: a per-agent override always wins; otherwise the project's
// resolved generative default is reported (may be empty when unconfigured).
func pickEffectiveModel(override, projectDefault string) string {
	if override != "" {
		return override
	}
	return projectDefault
}

// toDefinitionDTO converts an agent definition to its full response DTO,
// enriching the computed toolGroups with the project-resolved tool catalog when
// the MCP service is attached. When the catalog is unavailable it degrades
// gracefully to agent-referenced-tools-only membership (via ToDTO) rather than
// failing.
func (h *Handler) toDefinitionDTO(ctx context.Context, def *AgentDefinition) *AgentDefinitionDTO {
	dto := def.ToDTO()
	if h.mcpService != nil {
		dto.ToolGroups = def.ToolGroupsWithCatalog(h.mcpService.GetToolDefinitionsForProject(ctx, def.ProjectID))
	}
	return dto
}

// normalizeUIConfig maps the documented "no appearance" wire value to the
// canonical ui_config representation so a bare jsonb `null` is never persisted.
// Semantics: len 0 (nil or empty) → nil — on create that omits the field so the
// DB default '{}' applies, on update it leaves the stored value untouched; JSON
// null (any case, any surrounding whitespace) → {}; anything else unchanged.
func normalizeUIConfig(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	if bytes.EqualFold(bytes.TrimSpace(raw), []byte("null")) {
		return json.RawMessage("{}")
	}
	return raw
}

// CreateDefinition handles POST /api/projects/:projectId/agent-definitions
func (h *Handler) CreateDefinition(c echo.Context) error {
	user := auth.MustGetUser(c)

	// Prefer URL :projectId param (project-scoped routes), fall back to header
	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	var dto CreateAgentDefinitionDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	if dto.Name == "" {
		return apperror.NewBadRequest("name is required")
	}

	if dto.Model != nil && dto.Model.Name != "" && !strings.Contains(dto.Model.Name, "/") {
		return apperror.NewBadRequest(`model.name must include a provider prefix, e.g. "deepseek/deepseek-v4-flash" or "google/gemini-2.5-flash"`)
	}

	// Set defaults
	flowType := FlowTypeSingle
	if dto.FlowType != "" {
		flowType = dto.FlowType
	}

	visibility := VisibilityProject
	if dto.Visibility != "" {
		nv, ok := NormalizeVisibility(dto.Visibility)
		if !ok {
			return apperror.NewBadRequest("visibility must be one of project, external, internal")
		}
		visibility = nv
	}

	isDefault := false
	if dto.IsDefault != nil {
		isDefault = *dto.IsDefault
	}

	enabled := true
	if dto.Enabled != nil {
		enabled = *dto.Enabled
	}

	tools := dto.Tools
	if tools == nil {
		tools = []string{}
	}

	skillNames := dto.Skills
	if skillNames == nil {
		skillNames = []string{}
	}

	config := dto.Config
	if config == nil {
		config = map[string]any{}
	}

	dispatchMode := DispatchModeSync
	if dto.DispatchMode != "" {
		dispatchMode = dto.DispatchMode
	}

	defaultToolPolicy := dto.DefaultToolPolicy
	if defaultToolPolicy == "" {
		defaultToolPolicy = ToolPolicyDefaultAllow
	}

	def := &AgentDefinition{
		ProjectID:         projectID,
		Name:              dto.Name,
		Description:       dto.Description,
		SystemPrompt:      dto.SystemPrompt,
		Model:             dto.Model,
		Tools:             tools,
		BannedTools:       dto.BannedTools,
		Skills:            skillNames,
		AutoLoadSkills:    dto.AutoLoadSkills != nil && *dto.AutoLoadSkills,
		FlowType:          flowType,
		IsDefault:         isDefault,
		Enabled:           enabled,
		MaxSteps:          dto.MaxSteps,
		DefaultTimeout:    dto.DefaultTimeout,
		Visibility:        visibility,
		DispatchMode:      dispatchMode,
		ACPConfig:         dto.ACPConfig,
		Config:            config,
		SandboxConfig:     dto.SandboxConfig,
		ToolPolicies:      dto.ToolPolicies,
		DefaultToolPolicy: defaultToolPolicy,
		UIConfig:          normalizeUIConfig(dto.UIConfig),
	}

	// Check for existing definition with same name to return a clear 409 instead of a 500
	// from a unique constraint violation on (project_id, name).
	existing, err := h.repo.FindDefinitionByName(c.Request().Context(), projectID, dto.Name)
	if err != nil {
		return apperror.NewInternal("failed to check for existing agent definition", err)
	}
	if existing != nil {
		return apperror.ErrConflict.WithMessage(fmt.Sprintf("an agent definition named '%s' already exists in this project", dto.Name))
	}

	if err := h.repo.CreateDefinition(c.Request().Context(), def); err != nil {
		return apperror.NewInternal("failed to create agent definition", err)
	}

	return c.JSON(http.StatusCreated, SuccessResponse(h.toDefinitionDTO(c.Request().Context(), def)))
}

// UpdateDefinition handles PATCH /api/projects/:projectId/agent-definitions/:id
func (h *Handler) UpdateDefinition(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("definition id is required")
	}

	var dto UpdateAgentDefinitionDTO
	if err := c.Bind(&dto); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	if dto.Model != nil && dto.Model.Name != "" && !strings.Contains(dto.Model.Name, "/") {
		return apperror.NewBadRequest(`model.name must include a provider prefix, e.g. "deepseek/deepseek-v4-flash" or "google/gemini-2.5-flash"`)
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
	if dto.BannedTools != nil {
		def.BannedTools = dto.BannedTools
	}
	if dto.Skills != nil {
		def.Skills = dto.Skills
	}
	if dto.AutoLoadSkills != nil {
		def.AutoLoadSkills = *dto.AutoLoadSkills
	}
	if dto.FlowType != nil {
		def.FlowType = *dto.FlowType
	}
	if dto.IsDefault != nil {
		def.IsDefault = *dto.IsDefault
	}
	if dto.Enabled != nil {
		def.Enabled = *dto.Enabled
	}
	if dto.MaxSteps != nil {
		def.MaxSteps = dto.MaxSteps
	}
	if dto.DefaultTimeout != nil {
		def.DefaultTimeout = dto.DefaultTimeout
	}
	if dto.Visibility != nil {
		nv, ok := NormalizeVisibility(*dto.Visibility)
		if !ok {
			return apperror.NewBadRequest("visibility must be one of project, external, internal")
		}
		def.Visibility = nv
	}
	if dto.DispatchMode != nil {
		def.DispatchMode = *dto.DispatchMode
	}
	if dto.ACPConfig != nil {
		def.ACPConfig = dto.ACPConfig
	}
	if dto.Config != nil {
		def.Config = dto.Config
	}
	if dto.SandboxConfig != nil {
		def.SandboxConfig = dto.SandboxConfig
	}
	if dto.ToolPolicies != nil {
		def.ToolPolicies = dto.ToolPolicies
	}
	if dto.DefaultToolPolicy != nil {
		def.DefaultToolPolicy = *dto.DefaultToolPolicy
	}
	if dto.UIConfig != nil {
		def.UIConfig = normalizeUIConfig(dto.UIConfig)
	}

	if err := h.repo.UpdateDefinition(c.Request().Context(), def); err != nil {
		return apperror.NewInternal("failed to update agent definition", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(h.toDefinitionDTO(c.Request().Context(), def)))
}

// DeleteDefinition handles DELETE /api/projects/:projectId/agent-definitions/:id
func (h *Handler) DeleteDefinition(c echo.Context) error {
	user := auth.MustGetUser(c)

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

	// Attach per-run token usage in a single batch query.
	if len(dtos) > 0 {
		runIDs := make([]string, len(dtos))
		for i, d := range dtos {
			runIDs[i] = d.ID
		}
		tokenUsageMap, err := h.repo.GetRunsTokenUsage(c.Request().Context(), runIDs)
		if err == nil {
			for _, d := range dtos {
				if usage, ok := tokenUsageMap[d.ID]; ok {
					d.TokenUsage = usage
				}
			}
		}
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

	dto := run.ToDTO()
	h.enrichRunTrace(c.Request().Context(), dto, runID, run.TraceID)
	dto.Workspace = h.getWorkspaceInfo(c.Request().Context(), runID)

	return c.JSON(http.StatusOK, SuccessResponse(dto))
}

// GetRunRememberStatus handles GET /api/projects/:projectId/agent-runs/:runId/remember-status
// @Summary      Get remember-status for a run
// @Description  Returns the graph-mutation summary (objects created/updated/deleted, relationships, discovered types, created/deleted ids) and completion state of an async remember/forget run, following queue-reextraction jobs and embedding readiness.
// @Tags         agents
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        runId path string true "Run ID (UUID)"
// @Success      200 {object} map[string]any "Remember-status result"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Run not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/agent-runs/{runId}/remember-status [get]
// @Security     bearerAuth
func (h *Handler) GetRunRememberStatus(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	if h.mcpTools == nil {
		return apperror.NewInternal("remember-status not configured", errors.New("mcp tool handler not wired"))
	}

	result, err := h.mcpTools.buildRememberStatus(c.Request().Context(), projectID, runID)
	if err != nil {
		if errors.Is(err, errRunNotFound) {
			return apperror.NewNotFound("AgentRun", runID)
		}
		return apperror.NewInternal("failed to get remember status", err)
	}

	return c.JSON(http.StatusOK, result)
}

// GetRunByID handles GET /api/v1/runs/:runId — global lookup, no project required.
// The run ID is a globally unique UUID so no project scoping is needed.
//
// @Summary      Get a run by ID (global)
// @Description  Returns full details for an agent run by its globally unique run ID.
// @Tags         agents
// @Produce      json
// @Param        runId path string true "Run ID (UUID)"
// @Success      200 {object} APIResponse[AgentRunDTO] "Run details"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Run not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/runs/{runId} [get]
// @Security     bearerAuth
func (h *Handler) GetRunByID(c echo.Context) error {

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	run, err := h.repo.FindRunByID(c.Request().Context(), runID)
	if err != nil {
		return apperror.NewInternal("failed to get agent run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	dto := run.ToDTO()
	h.enrichRunTrace(c.Request().Context(), dto, runID, run.TraceID)
	dto.Workspace = h.getWorkspaceInfo(c.Request().Context(), runID)

	return c.JSON(http.StatusOK, SuccessResponse(dto))
}

// GetRunMessages handles GET /api/projects/:projectId/agent-runs/:runId/messages
func (h *Handler) GetRunMessages(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	ctx := c.Request().Context()

	// Verify the run belongs to this project
	run, err := h.repo.FindRunByIDForProject(ctx, runID, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	messages, err := h.repo.FindMessagesByRunID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get run messages", err)
	}

	// Fetch tool calls and group by step number so we can attach their outputs
	// as function_responses on the corresponding assistant message.
	toolCalls, err := h.repo.FindToolCallsByRunID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get run tool calls", err)
	}

	// Build a map: stepNumber -> list of function_response entries
	type funcResponse struct {
		Name   string         `json:"name"`
		Output map[string]any `json:"output"`
		Status string         `json:"status"`
	}
	stepResponses := make(map[int][]funcResponse)
	for _, tc := range toolCalls {
		stepResponses[tc.StepNumber] = append(stepResponses[tc.StepNumber], funcResponse{
			Name:   tc.ToolName,
			Output: tc.Output,
			Status: tc.Status,
		})
	}

	dtos := make([]*AgentRunMessageDTO, len(messages))
	for i, msg := range messages {
		dto := msg.ToDTO()
		// Inject function_responses into messages that contain function_calls
		if _, hasCalls := dto.Content["function_calls"]; hasCalls {
			if responses, ok := stepResponses[msg.StepNumber]; ok && len(responses) > 0 {
				// Copy content map to avoid mutating the entity
				enriched := make(map[string]any, len(dto.Content)+1)
				for k, v := range dto.Content {
					enriched[k] = v
				}
				enriched["function_responses"] = responses
				dto.Content = enriched
			}
		}
		dtos[i] = dto
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// GetRunToolCalls handles GET /api/projects/:projectId/agent-runs/:runId/tool-calls
func (h *Handler) GetRunToolCalls(c echo.Context) error {

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

// GetProjectRunFull handles GET /api/projects/:projectId/agent-runs/:runId/full
// Returns run + messages + toolCalls + parentRun in a single response (issue #192).
func (h *Handler) GetProjectRunFull(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	ctx := c.Request().Context()

	run, err := h.repo.FindRunByIDForProject(ctx, runID, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	messages, err := h.repo.FindMessagesByRunID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get run messages", err)
	}

	toolCalls, err := h.repo.FindToolCallsByRunID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get run tool calls", err)
	}

	dto := run.ToDTO()
	h.enrichRunTrace(ctx, dto, runID, run.TraceID)
	dto.Workspace = h.getWorkspaceInfo(ctx, runID)

	msgDTOs := make([]*AgentRunMessageDTO, len(messages))
	for i, m := range messages {
		msgDTOs[i] = m.ToDTO()
	}
	tcDTOs := make([]*AgentRunToolCallDTO, len(toolCalls))
	for i, tc := range toolCalls {
		tcDTOs[i] = tc.ToDTO()
	}

	full := &AgentRunFullDTO{
		Run:       dto,
		Messages:  msgDTOs,
		ToolCalls: tcDTOs,
	}

	// Attach parent run if this run was spawned by another run.
	if run.ParentRunID != nil {
		parent, perr := h.repo.FindRunByID(ctx, *run.ParentRunID)
		if perr == nil && parent != nil {
			parentDTO := parent.ToDTO()
			h.enrichRunTrace(ctx, parentDTO, parent.ID, parent.TraceID)
			full.ParentRun = parentDTO
		}
	}

	return c.JSON(http.StatusOK, SuccessResponse(full))
}

// GetProjectRunStats handles GET /api/projects/:projectId/agent-runs/stats
// Returns aggregate analytics computed server-side (issue #193).
func (h *Handler) GetProjectRunStats(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	ctx := c.Request().Context()
	now := time.Now().UTC()

	// Parse time window — default last 24h.
	since := now.Add(-24 * time.Hour)
	until := now
	if s := c.QueryParam("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t
		}
	}
	if u := c.QueryParam("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			until = t
		}
	}

	var agentID *string
	if a := c.QueryParam("agentId"); a != "" {
		agentID = &a
	}

	// Per-agent aggregates.
	agentRows, err := h.repo.GetRunStatsOverview(ctx, projectID, agentID, since, until)
	if err != nil {
		return apperror.NewInternal("failed to compute run stats", err)
	}

	// Top errors.
	errRows, err := h.repo.GetRunStatsTopErrors(ctx, projectID, agentID, since, until, 20)
	if err != nil {
		return apperror.NewInternal("failed to compute run error stats", err)
	}

	// Tool call aggregates.
	toolRows, err := h.repo.GetRunStatsTools(ctx, projectID, agentID, since, until)
	if err != nil {
		return apperror.NewInternal("failed to compute tool stats", err)
	}

	// Hourly time series.
	tsRows, err := h.repo.GetRunStatsTimeSeries(ctx, projectID, agentID, since, until)
	if err != nil {
		return apperror.NewInternal("failed to compute time series", err)
	}

	// --- Build response ---
	overview := RunStatsOverviewDTO{}
	byAgent := make(map[string]RunStatsAgentDTO)
	for _, r := range agentRows {
		overview.TotalRuns += r.Total
		overview.SuccessCount += r.Success
		overview.FailedCount += r.Failed
		overview.ErrorCount += r.Errored
		byAgent[r.AgentName] = RunStatsAgentDTO{
			Total:           r.Total,
			Success:         r.Success,
			Failed:          r.Failed,
			Errored:         r.Errored,
			AvgDurationMs:   r.AvgDurationMs,
			MaxDurationMs:   r.MaxDurationMs,
			AvgCostUSD:      r.AvgCostUSD,
			TotalCostUSD:    r.TotalCostUSD,
			AvgInputTokens:  r.AvgInputTokens,
			AvgOutputTokens: r.AvgOutputTokens,
		}
		overview.AvgDurationMs += r.AvgDurationMs * float64(r.Total)
		overview.TotalCostUSD += r.TotalCostUSD
	}
	if overview.TotalRuns > 0 {
		overview.AvgDurationMs = overview.AvgDurationMs / float64(overview.TotalRuns)
		overview.SuccessRate = float64(overview.SuccessCount) / float64(overview.TotalRuns)
	}

	topErrors := make([]RunStatsErrorDTO, len(errRows))
	for i, r := range errRows {
		topErrors[i] = RunStatsErrorDTO{Message: r.Message, Count: r.Count}
	}

	byTool := make(map[string]RunStatsToolDTO)
	var totalToolCalls int64
	for _, r := range toolRows {
		totalToolCalls += r.Total
		byTool[r.ToolName] = RunStatsToolDTO{
			Total:         r.Total,
			Success:       r.Success,
			Failed:        r.Failed,
			AvgDurationMs: r.AvgDurationMs,
			MaxDurationMs: r.MaxDurationMs,
		}
	}

	// Build hourly time series: merge per-agent rows into per-hour points.
	type hourKey = time.Time
	hourMap := make(map[hourKey]*RunStatsTimePointDTO)
	for _, r := range tsRows {
		pt, ok := hourMap[r.Hour]
		if !ok {
			pt = &RunStatsTimePointDTO{Hour: r.Hour, ByAgent: make(map[string]int64)}
			hourMap[r.Hour] = pt
		}
		pt.Runs += r.Runs
		pt.ByAgent[r.AgentName] = r.Runs
	}
	// Sort hours ascending.
	hours := make([]time.Time, 0, len(hourMap))
	for h := range hourMap {
		hours = append(hours, h)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i].Before(hours[j]) })
	byHour := make([]RunStatsTimePointDTO, len(hours))
	for i, h := range hours {
		byHour[i] = *hourMap[h]
	}

	result := RunStatsDTO{
		Period:     RunStatsPeriodDTO{Since: since, Until: until},
		Overview:   overview,
		ByAgent:    byAgent,
		TopErrors:  topErrors,
		ToolStats:  RunStatsToolsDTO{TotalToolCalls: totalToolCalls, ByTool: byTool},
		TimeSeries: RunStatsTimeSeriesDTO{ByHour: byHour},
	}

	return c.JSON(http.StatusOK, SuccessResponse(result))
}

// GetProjectRunSessionStats handles GET /api/projects/:projectId/agent-runs/stats/sessions
// Returns session-level analytics grouped by trigger metadata (issue #194).
func (h *Handler) GetProjectRunSessionStats(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	ctx := c.Request().Context()
	now := time.Now().UTC()

	since := now.Add(-24 * time.Hour)
	until := now
	if s := c.QueryParam("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t
		}
	}
	if u := c.QueryParam("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			until = t
		}
	}

	var platform *string
	if p := c.QueryParam("platform"); p != "" {
		platform = &p
	}

	topN := 20
	if n := c.QueryParam("topN"); n != "" {
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 && parsed <= 100 {
			topN = parsed
		}
	}

	rows, err := h.repo.GetRunSessionStats(ctx, projectID, platform, since, until, topN)
	if err != nil {
		return apperror.NewInternal("failed to compute session stats", err)
	}

	platformCounts := make(map[string]int64)
	var totalSessions, activeSessions, maxRuns int64
	var totalRunsSum int64
	tops := make([]RunSessionSummaryDTO, 0, len(rows))

	for _, r := range rows {
		totalSessions++
		if r.ActiveRuns > 0 {
			activeSessions++
		}
		if r.TotalRuns > maxRuns {
			maxRuns = r.TotalRuns
		}
		totalRunsSum += r.TotalRuns
		platformCounts[r.Platform]++
		tops = append(tops, RunSessionSummaryDTO{
			Platform:      r.Platform,
			ChannelID:     r.ChannelID,
			ThreadID:      r.ThreadID,
			TotalRuns:     r.TotalRuns,
			LastRunAt:     r.LastRunAt,
			AvgDurationMs: r.AvgDurationMs,
			TotalCostUSD:  r.TotalCostUSD,
		})
	}

	var avg float64
	if totalSessions > 0 {
		avg = float64(totalRunsSum) / float64(totalSessions)
	}

	result := RunSessionStatsDTO{
		Period:             RunStatsPeriodDTO{Since: since, Until: until},
		TotalSessions:      totalSessions,
		ActiveSessions:     activeSessions,
		AvgRunsPerSession:  avg,
		MaxRunsPerSession:  maxRuns,
		SessionsByPlatform: platformCounts,
		TopSessions:        tops,
	}

	return c.JSON(http.StatusOK, SuccessResponse(result))
}

// GetRunSteps handles GET /api/projects/:projectId/agent-runs/:runId/steps
// It returns a per-step trace of the run, grouping messages and tool calls by
// step number so callers can inspect each LLM invocation turn.
func (h *Handler) GetRunSteps(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	ctx := c.Request().Context()

	// Verify the run belongs to this project
	run, err := h.repo.FindRunByIDForProject(ctx, runID, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get agent run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	messages, err := h.repo.FindMessagesByRunID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get run messages", err)
	}

	toolCalls, err := h.repo.FindToolCallsByRunID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get run tool calls", err)
	}

	// Group messages and tool calls by step number
	stepMap := make(map[int]*AgentRunStepDTO)
	for _, msg := range messages {
		s, ok := stepMap[msg.StepNumber]
		if !ok {
			s = &AgentRunStepDTO{
				StepNumber: msg.StepNumber,
				Messages:   []*AgentRunMessageDTO{},
				ToolCalls:  []*AgentRunToolCallDTO{},
			}
			stepMap[msg.StepNumber] = s
		}
		s.Messages = append(s.Messages, msg.ToDTO())
	}
	for _, tc := range toolCalls {
		s, ok := stepMap[tc.StepNumber]
		if !ok {
			s = &AgentRunStepDTO{
				StepNumber: tc.StepNumber,
				Messages:   []*AgentRunMessageDTO{},
				ToolCalls:  []*AgentRunToolCallDTO{},
			}
			stepMap[tc.StepNumber] = s
		}
		s.ToolCalls = append(s.ToolCalls, tc.ToDTO())
	}

	// Flatten and sort by step number
	steps := make([]*AgentRunStepDTO, 0, len(stepMap))
	for _, s := range stepMap {
		steps = append(steps, s)
	}
	sort.Slice(steps, func(i, j int) bool {
		return steps[i].StepNumber < steps[j].StepNumber
	})

	return c.JSON(http.StatusOK, SuccessResponse(steps))
}

// GetRunStepsStream handles GET /api/v1/runs/:runId/steps
// It returns run messages and tool calls as newline-delimited JSON (NDJSON).
// This allows flow-server's LogStreamer to stream agent run output.
func (h *Handler) GetRunStepsStream(c echo.Context) error {

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	ctx := c.Request().Context()

	// Verify the run exists (global lookup)
	run, err := h.repo.FindRunByID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get agent run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	entries, err := h.repo.FindRunLogEntries(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get run log entries", err)
	}

	// Set headers for NDJSON
	c.Response().Header().Set(echo.HeaderContentType, "application/x-ndjson")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	for _, entry := range entries {
		if err := c.JSON(0, entry); err != nil {
			return err
		}
		if _, err := c.Response().Write([]byte("\n")); err != nil {
			return err
		}
		c.Response().Flush()
	}

	return nil
}

// GetRunLogs handles GET /api/v1/runs/:runId/logs
//
// Returns a chronological log stream for a run, merging messages and tool calls
// ordered by creation time. Supports two response formats:
//
//   - SSE (default, or Accept: text/event-stream): streams each entry as a
//     named SSE event ("message" or "tool_call") with JSON data.
//   - Plain text (Accept: text/plain): streams each entry as a single
//     human-readable line followed by a newline, suitable for log viewers.
//
// For completed runs the snapshot is returned immediately. For in-progress runs
// the current snapshot is returned; clients should poll if live tailing is needed.
//
// @Summary      Stream run logs
// @Description  Returns the merged message + tool-call log for a run as an SSE stream or plain text.
// @Tags         agents
// @Produce      text/event-stream
// @Param        runId path string true "Run ID (UUID)"
// @Success      200 {string} string "SSE stream of log entries"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Run not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/v1/runs/{runId}/logs [get]
// @Security     bearerAuth
func (h *Handler) GetRunLogs(c echo.Context) error {

	runID := c.Param("runId")
	if runID == "" {
		return apperror.NewBadRequest("runId is required")
	}

	ctx := c.Request().Context()

	// Verify the run exists (global lookup — run IDs are globally unique UUIDs)
	run, err := h.repo.FindRunByID(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get agent run", err)
	}
	if run == nil {
		return apperror.NewNotFound("AgentRun", runID)
	}

	entries, err := h.repo.FindRunLogEntries(ctx, runID)
	if err != nil {
		return apperror.NewInternal("failed to get run log entries", err)
	}

	// Plain-text mode: Accept: text/plain
	if strings.Contains(c.Request().Header.Get("Accept"), "text/plain") {
		c.Response().Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Response().Header().Set("Cache-Control", "no-cache")
		c.Response().Header().Set("X-Content-Type-Options", "nosniff")
		c.Response().WriteHeader(http.StatusOK)

		flusher, canFlush := c.Response().Writer.(http.Flusher)
		for _, entry := range entries {
			var line string
			switch entry.Kind {
			case "message":
				// Extract text content if present, otherwise use role label
				text := ""
				if t, ok := entry.Content["text"].(string); ok && t != "" {
					text = t
				} else if parts, ok := entry.Content["parts"].([]any); ok && len(parts) > 0 {
					if p, ok := parts[0].(map[string]any); ok {
						if t, ok := p["text"].(string); ok {
							text = t
						}
					}
				}
				if text != "" {
					line = fmt.Sprintf("[%s] %s: %s\n",
						entry.CreatedAt.Format("15:04:05"), entry.Role, text)
				} else {
					line = fmt.Sprintf("[%s] %s\n",
						entry.CreatedAt.Format("15:04:05"), entry.Role)
				}
			case "tool_call":
				line = fmt.Sprintf("[%s] tool_call: %s (status=%s)\n",
					entry.CreatedAt.Format("15:04:05"), entry.ToolName, entry.Status)
			}
			if line != "" {
				_, _ = fmt.Fprint(c.Response().Writer, line)
				if canFlush {
					flusher.Flush()
				}
			}
		}
		return nil
	}

	// SSE mode (default)
	writer := sse.NewWriter(c.Response().Writer)
	if err := writer.Start(); err != nil {
		return apperror.NewInternal("SSE streaming not supported", err)
	}
	defer writer.Close()

	for _, entry := range entries {
		if err := writer.WriteEvent(entry.Kind, entry); err != nil {
			// Client disconnected — stop streaming
			break
		}
	}

	// Emit a terminal "done" event so clients know the stream is complete
	_ = writer.WriteEvent("done", map[string]any{
		"runId":  runID,
		"status": string(run.Status),
		"count":  len(entries),
	})

	return nil
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
	user := auth.MustGetUser(c)

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

// GetSandboxConfig handles GET /api/admin/agent-definitions/:id/sandbox-config
// @Summary      Get workspace config for an agent definition
// @Description  Returns the workspace configuration for an agent definition, or defaults if not set
// @Tags         agent-definitions
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent Definition ID (UUID)"
// @Success      200 {object} APIResponse[sandbox.AgentSandboxConfig] "Workspace config"
// @Failure      400 {object} apperror.Error "Missing definition ID"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent definition not found (invalid or unknown definition ID)"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agent-definitions/{id}/sandbox-config [get]
// @Security     bearerAuth
func (h *Handler) GetSandboxConfig(c echo.Context) error {
	user := auth.MustGetUser(c)

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
	cfg, err := sandbox.ParseAgentSandboxConfig(def.SandboxConfig)
	if err != nil {
		return apperror.NewInternal("failed to parse workspace config", err)
	}

	// Return default config if none is set
	if cfg == nil {
		cfg = sandbox.DefaultAgentSandboxConfig()
	}

	return c.JSON(http.StatusOK, SuccessResponse(cfg))
}

// UpdateSandboxConfig handles PUT /api/admin/agent-definitions/:id/sandbox-config
// @Summary      Update workspace config for an agent definition
// @Description  Sets or replaces the workspace configuration for an agent definition
// @Tags         agent-definitions
// @Accept       json
// @Produce      json
// @Param        id path string true "Agent Definition ID (UUID)"
// @Param        request body sandbox.AgentSandboxConfig true "Workspace configuration"
// @Success      200 {object} APIResponse[sandbox.AgentSandboxConfig] "Updated workspace config"
// @Failure      400 {object} apperror.Error "Missing definition ID, invalid request body, or validation error"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Agent definition not found (invalid or unknown definition ID)"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/admin/agent-definitions/{id}/sandbox-config [put]
// @Security     bearerAuth
func (h *Handler) UpdateSandboxConfig(c echo.Context) error {
	user := auth.MustGetUser(c)

	id := c.Param("id")
	if id == "" {
		return apperror.NewBadRequest("definition id is required")
	}

	var cfg sandbox.AgentSandboxConfig
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

	def.SandboxConfig = cfgMap

	if err := h.repo.UpdateDefinition(c.Request().Context(), def); err != nil {
		return apperror.NewInternal("failed to update agent definition", err)
	}

	return c.JSON(http.StatusOK, SuccessResponse(&cfg))
}

// --- Agent Question Handlers ---

// HandleRespondToQuestion handles POST /api/projects/:projectId/agent-questions/:questionId/respond
// @Summary      Respond to an agent question
// @Description  Responds to a pending agent question and resumes the paused agent run
// @Tags         agent-questions
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        questionId path string true "Question ID (UUID)"
// @Param        body body RespondToQuestionRequest true "Response body"
// @Success      202 {object} APIResponse[AgentQuestionDTO] "Question answered, agent resuming"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Question not found"
// @Failure      409 {object} apperror.Error "Question already answered or run not paused"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/agent-questions/{questionId}/respond [post]
// @Security     bearerAuth
func (h *Handler) HandleRespondToQuestion(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	questionID := c.Param("questionId")
	if questionID == "" {
		return apperror.NewBadRequest("questionId is required")
	}

	// Parse request body
	var req RespondToQuestionRequest
	if err := c.Bind(&req); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}
	if req.Response == "" {
		return apperror.NewBadRequest("response is required")
	}

	dto, err := h.RespondToQuestion(c.Request().Context(), RespondParams{
		ProjectID:   projectID,
		OrgID:       user.OrgID,
		RespondedBy: user.ID,
		UserID:      user.ID,
		QuestionID:  questionID,
		Response:    req.Response,
		Message:     req.Message,
		AuthToken:   auth.RawTokenFromContext(c.Request().Context()),
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusAccepted, SuccessResponse(dto))
}

// RespondParams parameterizes the shared question-respond/resume flow so both
// the authenticated owner path (HandleRespondToQuestion) and the public share
// path (share service) can drive it.
type RespondParams struct {
	ProjectID   string
	OrgID       string // may be empty; resolved from the agent's project when empty
	RespondedBy string // uuid string — owner user ID, or end_user_ref for share runs
	UserID      string // auth user ID for ask_user notifications ("" for share runs)
	QuestionID  string
	Response    string
	Message     string
	AuthToken   string

	// Share-run resume overrides (zero/empty = not a share run).
	ShareLinkID            string
	ShareToolDeny          []string
	DisableAuthMint        bool
	MaxApprovalsPerSession int
	ACPSessionID           string

	// OnRunSettled, if non-nil, is invoked (in the resume goroutine, with a
	// background context) once the resumed run settles, carrying the result of
	// executor.Resume. Share callers use it to reconcile budget usage from the
	// resume leg: the initial StreamMessage records usage only up to the first
	// pause, and the resumed leg spends more that must also be counted.
	OnRunSettled func(result *ExecuteResult)
}

// RespondToQuestion is the shared core of the question-respond flow. It looks up
// the question + paused run, atomically claims the question via AnswerQuestion,
// records any tool-approval decision via MarkToolConfirmationDecision, and
// resumes the run in a background goroutine. Share callers must verify
// question->run->session ownership BEFORE calling this.
func (h *Handler) RespondToQuestion(ctx context.Context, p RespondParams) (*AgentQuestionDTO, error) {
	if p.ProjectID == "" {
		return nil, apperror.NewBadRequest("projectId is required")
	}
	if p.QuestionID == "" {
		return nil, apperror.NewBadRequest("questionId is required")
	}
	if p.Response == "" {
		return nil, apperror.NewBadRequest("response is required")
	}

	// Look up the question
	question, err := h.repo.FindQuestionByID(ctx, p.QuestionID)
	if err != nil {
		return nil, apperror.NewInternal("failed to get question", err)
	}
	if question == nil {
		return nil, apperror.NewNotFound("AgentQuestion", p.QuestionID)
	}

	// Verify question belongs to this project
	if question.ProjectID != p.ProjectID {
		return nil, apperror.NewNotFound("AgentQuestion", p.QuestionID)
	}

	// Verify question is still pending
	if question.Status != QuestionStatusPending {
		return nil, apperror.ErrConflict.WithMessage(fmt.Sprintf("question is already %s", question.Status))
	}

	// Look up the run and verify it's paused
	run, err := h.repo.FindRunByID(ctx, question.RunID)
	if err != nil {
		return nil, apperror.NewInternal("failed to get run", err)
	}
	if run == nil {
		return nil, apperror.NewInternal("associated run not found", nil)
	}
	if run.Status != RunStatusPaused {
		return nil, apperror.ErrConflict.WithMessage(fmt.Sprintf("run is %s, expected paused", run.Status))
	}

	// Atomically claim the question. A concurrent respond/cancel that won the
	// race leaves claimed=false and must not resume the run.
	claimed, err := h.repo.AnswerQuestion(ctx, p.QuestionID, p.Response, p.RespondedBy)
	if err != nil {
		return nil, apperror.NewInternal("failed to answer question", err)
	}
	if !claimed {
		return nil, apperror.ErrConflict.WithMessage("question is no longer pending")
	}

	// Update notification action status if notification was created (non-fatal)
	if question.NotificationID != nil {
		_ = h.repo.UpdateNotificationActionStatus(ctx, *question.NotificationID, "completed", p.UserID)
	}

	// Resume the agent in a background goroutine
	var preCreatedRun *AgentRun
	if h.executor != nil {
		// Look up the agent to build the resume request
		agent, err := h.repo.FindByID(ctx, run.AgentID, nil)
		if err != nil || agent == nil {
			_ = h.repo.ReopenQuestion(ctx, p.QuestionID)
			return nil, apperror.NewInternal("failed to find agent for resume", err)
		}

		// Look up the agent definition (optional, may be nil)
		agentDef, _ := h.repo.ResolveDefinitionForAgent(ctx, agent)

		// Build the resume user message.
		var userMessage string
		var resumeNow = true
		sc := SuspendSignalFromMap(run.SuspendContext)
		if sc != nil && sc.Reason == SuspendReasonAwaitingToolConfirm {
			// Resolve the option value from the submitted label (or use as-is if it's already a value).
			userMessage = strings.ToLower(strings.TrimSpace(p.Response))
			for _, opt := range question.Options {
				if strings.EqualFold(opt.Label, p.Response) || strings.EqualFold(opt.Value, p.Response) {
					userMessage = strings.ToLower(strings.TrimSpace(opt.Value))
					break
				}
			}
			// Record the approval decision in the audit trail. Share runs enforce
			// the per-session approval cap atomically with the decision (advisory
			// lock + conditional flip).
			decision := "rejected"
			switch userMessage {
			case "approve":
				decision = "approved"
			case "cancel":
				decision = "cancelled"
			}
			if p.MaxApprovalsPerSession > 0 {
				decided, decErr := h.repo.ReserveAndDecideShareApproval(ctx, p.ShareLinkID, p.ACPSessionID, p.QuestionID, decision, p.Message, p.RespondedBy, p.MaxApprovalsPerSession)
				if decErr != nil {
					_ = h.repo.ReopenQuestion(ctx, p.QuestionID)
					return nil, apperror.NewInternal("failed to record decision", decErr)
				}
				if !decided {
					_ = h.repo.ReopenQuestion(ctx, p.QuestionID)
					return nil, apperror.New(429, "share_approval_limit", "approval limit reached for this session")
				}
			} else {
				_ = h.repo.UpdateToolApprovalDecision(ctx, p.QuestionID, decision, p.Message, p.RespondedBy)
			}
			// Batch coordination: mark this decision in suspend_context and
			// resume only once every confirmation in the batch is decided.
			if len(sc.PendingToolConfirmations) > 0 {
				var markErr error
				resumeNow, markErr = h.repo.MarkToolConfirmationDecision(ctx, run.ID, p.QuestionID, userMessage, p.Message)
				if markErr != nil {
					_ = h.repo.ReopenQuestion(ctx, p.QuestionID)
					return nil, apperror.NewInternal("failed to record decision", markErr)
				}
				if resumeNow {
					// Re-fetch the run so Resume reads the updated suspend_context.
					fresh, ferr := h.repo.FindRunByID(ctx, run.ID)
					if ferr != nil || fresh == nil {
						_ = h.repo.ReopenQuestion(ctx, p.QuestionID)
						return nil, apperror.NewInternal("failed to re-fetch run after decision", ferr)
					}
					run = fresh
				}
			}
		} else {
			userMessage = fmt.Sprintf(
				"Previously you asked: \"%s\"\nThe user responded: \"%s\"\nContinue from where you left off.",
				question.Question, p.Response,
			)
		}

		if resumeNow {
			preCreatedRun, err = h.resumeQuestionRun(ctx, p, run, agent, agentDef, userMessage, p.Message)
			if err != nil {
				_ = h.repo.ReopenQuestion(ctx, p.QuestionID)
				return nil, err
			}
		}
	}

	// Re-fetch the question to return the updated state
	updatedQuestion, err := h.repo.FindQuestionByID(ctx, p.QuestionID)
	var dto *AgentQuestionDTO
	if err != nil || updatedQuestion == nil {
		dto = question.ToDTO()
	} else {
		dto = updatedQuestion.ToDTO()
	}

	// Attach resume_run_id so clients can poll the correct run ID.
	if preCreatedRun != nil {
		dto.ResumeRunID = &preCreatedRun.ID
	}

	return dto, nil
}

// resumeQuestionRun resumes a paused run after a question decision. It pre-creates
// the resume run, persists the resume_run_id, and launches the resume goroutine
// with the given resolved user message and optional reject message. It returns
// the pre-created run (whose ID clients poll) or an error.
func (h *Handler) resumeQuestionRun(ctx context.Context, p RespondParams, run *AgentRun, agent *Agent, agentDef *AgentDefinition, userMessage, rejectMessage string) (*AgentRun, error) {
	orgID := p.OrgID
	if orgID == "" {
		orgID, _ = h.repo.GetOrgIDByProjectID(ctx, agent.ProjectID)
	}

	maxSteps := MaxTotalStepsPerRun
	resumedFrom := run.ID
	preCreatedRun, err := h.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:          run.AgentID,
		MaxSteps:         &maxSteps,
		ResumedFrom:      &resumedFrom,
		InitialStepCount: run.StepCount,
		TriggerMetadata:  run.TriggerMetadata,
	})
	if err != nil {
		return nil, apperror.NewInternal("failed to pre-create resume run", err)
	}

	if run.SuspendContext != nil {
		sc := make(map[string]any, len(run.SuspendContext)+1)
		for k, v := range run.SuspendContext {
			sc[k] = v
		}
		sc["resume_run_id"] = preCreatedRun.ID
		_ = h.repo.UpdateSuspendContext(ctx, run.ID, sc)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in resume goroutine",
					slog.String("run_id", run.ID),
					slog.Any("panic", r),
				)
				ctx := context.Background()
				if repoErr := h.repo.FailRunWithSteps(ctx, run.ID, fmt.Sprintf("panic: %v", r), run.StepCount); repoErr != nil {
					slog.Error("failed to mark run as error after panic",
						slog.String("run_id", run.ID),
						slog.String("error", repoErr.Error()),
					)
				}
			}
		}()
		ctx := context.Background()
		result, err := h.executor.Resume(ctx, run, ExecuteRequest{
			Agent:           agent,
			AgentDefinition: agentDef,
			ProjectID:       agent.ProjectID,
			OrgID:           orgID,
			UserMessage:     userMessage,
			RejectMessage:   rejectMessage,
			UserID:          p.UserID, // propagate for ask_user notifications ("" for share)
			AuthToken:       p.AuthToken,
			PreCreatedRun:   preCreatedRun,
			ShareLinkID:     p.ShareLinkID,
			ShareToolDeny:   p.ShareToolDeny,
			DisableAuthMint: p.DisableAuthMint,
		})
		if result != nil && result.Cleanup != nil {
			result.Cleanup()
		}
		if err != nil {
			slog.Error("failed to resume agent after question response",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
			// Mark the run as failed so it does not remain stuck in paused state.
			if repoErr := h.repo.FailRunWithSteps(ctx, run.ID, fmt.Sprintf("resume failed: %s", err.Error()), run.StepCount); repoErr != nil {
				slog.Error("failed to mark run as error after resume failure",
					slog.String("run_id", run.ID),
					slog.String("error", repoErr.Error()),
				)
			}
		}
		// Notify the caller (share) that the resumed leg has settled so it can
		// reconcile budget usage. Invoked last so Cleanup + failure marking are
		// already reflected before any accounting runs.
		if p.OnRunSettled != nil {
			p.OnRunSettled(result)
		}
	}()

	return preCreatedRun, nil
}

// HandleCancelQuestion handles POST /api/projects/:projectId/agent-questions/:questionId/cancel
// Revokes a pending tool-policy confirmation: the question is marked cancelled,
// the run resumes with the call treated as not taken, and the decision is audited.
func (h *Handler) HandleCancelQuestion(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	questionID := c.Param("questionId")
	if questionID == "" {
		return apperror.NewBadRequest("questionId is required")
	}

	question, err := h.repo.FindQuestionByID(c.Request().Context(), questionID)
	if err != nil {
		return apperror.NewInternal("failed to get question", err)
	}
	if question == nil {
		return apperror.NewNotFound("AgentQuestion", questionID)
	}
	if question.ProjectID != projectID {
		return apperror.NewNotFound("AgentQuestion", questionID)
	}
	if question.Status != QuestionStatusPending {
		return apperror.ErrConflict.WithMessage(fmt.Sprintf("question is already %s", question.Status))
	}

	run, err := h.repo.FindRunByID(c.Request().Context(), question.RunID)
	if err != nil {
		return apperror.NewInternal("failed to get run", err)
	}
	if run == nil {
		return apperror.NewInternal("associated run not found", nil)
	}
	if run.Status != RunStatusPaused {
		return apperror.ErrConflict.WithMessage(fmt.Sprintf("run is %s, expected paused", run.Status))
	}

	claimed, err := h.repo.CancelQuestion(c.Request().Context(), questionID)
	if err != nil {
		return apperror.NewInternal("failed to cancel question", err)
	}
	if !claimed {
		return apperror.ErrConflict.WithMessage("question is no longer pending")
	}

	// Record the cancellation in the audit trail (only affects tool-confirm approvals).
	_ = h.repo.UpdateToolApprovalDecision(c.Request().Context(), questionID, "cancelled", "", user.ID)

	if question.NotificationID != nil {
		_ = h.repo.UpdateNotificationActionStatus(c.Request().Context(), *question.NotificationID, "cancelled", user.ID)
	}

	var preCreatedRun *AgentRun
	var resumeNow = true
	if h.executor != nil {
		agent, err := h.repo.FindByID(c.Request().Context(), run.AgentID, nil)
		if err != nil || agent == nil {
			_ = h.repo.ReopenQuestion(c.Request().Context(), questionID)
			return apperror.NewInternal("failed to find agent for resume", err)
		}
		agentDef, _ := h.repo.ResolveDefinitionForAgent(c.Request().Context(), agent)

		// Batch coordination: mark this cancellation in suspend_context and
		// resume only once every confirmation in the batch is decided.
		if sc := SuspendSignalFromMap(run.SuspendContext); sc != nil && len(sc.PendingToolConfirmations) > 0 {
			var markErr error
			resumeNow, markErr = h.repo.MarkToolConfirmationDecision(c.Request().Context(), run.ID, questionID, "cancel", "")
			if markErr != nil {
				_ = h.repo.ReopenQuestion(c.Request().Context(), questionID)
				return apperror.NewInternal("failed to record decision", markErr)
			}
			if resumeNow {
				fresh, ferr := h.repo.FindRunByID(c.Request().Context(), run.ID)
				if ferr != nil || fresh == nil {
					_ = h.repo.ReopenQuestion(c.Request().Context(), questionID)
					return apperror.NewInternal("failed to re-fetch run after decision", ferr)
				}
				run = fresh
			}
		}

		if resumeNow {
			preCreatedRun, err = h.resumeQuestionRun(c.Request().Context(), RespondParams{
				ProjectID:   projectID,
				OrgID:       user.OrgID,
				RespondedBy: user.ID,
				UserID:      user.ID,
				AuthToken:   auth.RawTokenFromContext(c.Request().Context()),
			}, run, agent, agentDef, "cancel", "")
			if err != nil {
				_ = h.repo.ReopenQuestion(c.Request().Context(), questionID)
				return err
			}
		}
	}

	updatedQuestion, err := h.repo.FindQuestionByID(c.Request().Context(), questionID)
	var dto *AgentQuestionDTO
	if err != nil || updatedQuestion == nil {
		dto = question.ToDTO()
	} else {
		dto = updatedQuestion.ToDTO()
	}
	if preCreatedRun != nil {
		dto.ResumeRunID = &preCreatedRun.ID
	}

	return c.JSON(http.StatusAccepted, SuccessResponse(dto))
}

// HandleListQuestionsByRun handles GET /api/projects/:projectId/agent-runs/:runId/questions
// @Summary      List questions for an agent run
// @Description  Returns all questions for a specific agent run, ordered by creation time
// @Tags         agent-questions
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        runId path string true "Run ID (UUID)"
// @Success      200 {object} APIResponse[[]AgentQuestionDTO] "List of questions"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Run not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/agent-runs/{runId}/questions [get]
// @Security     bearerAuth
func (h *Handler) HandleListQuestionsByRun(c echo.Context) error {

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

	questions, err := h.repo.ListQuestionsByRunID(c.Request().Context(), runID)
	if err != nil {
		return apperror.NewInternal("failed to list questions", err)
	}

	dtos := make([]*AgentQuestionDTO, len(questions))
	for i, q := range questions {
		dtos[i] = q.ToDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// HandleListQuestionsByProject handles GET /api/projects/:projectId/agent-questions
// @Summary      List questions for a project
// @Description  Returns agent questions for a project with optional status filter
// @Tags         agent-questions
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        status query string false "Filter by status (pending, answered, expired, cancelled)"
// @Success      200 {object} APIResponse[[]AgentQuestionDTO] "List of questions"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/agent-questions [get]
// @Security     bearerAuth
func (h *Handler) HandleListQuestionsByProject(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	// Parse optional status filter
	var statusFilter *AgentQuestionStatus
	if statusStr := c.QueryParam("status"); statusStr != "" {
		s := AgentQuestionStatus(statusStr)
		switch s {
		case QuestionStatusPending, QuestionStatusAnswered, QuestionStatusExpired, QuestionStatusCancelled:
			statusFilter = &s
		default:
			return apperror.NewBadRequest("invalid status filter: must be pending, answered, expired, or cancelled")
		}
	}

	questions, err := h.repo.ListQuestionsByProject(c.Request().Context(), projectID, statusFilter)
	if err != nil {
		return apperror.NewInternal("failed to list questions", err)
	}

	dtos := make([]*AgentQuestionDTO, len(questions))
	for i, q := range questions {
		dtos[i] = q.ToDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(dtos))
}

// HandleListToolApprovals handles GET /api/projects/:projectId/agent-approvals
// @Summary      List tool-approval audit records
// @Description  Returns tool-policy approval decisions for the project, newest first
// @Tags         agent-questions
// @Accept       json
// @Produce      json
// @Param        projectId path string true "Project ID (UUID)"
// @Param        decision query string false "Filter by decision (pending, approved, rejected, cancelled)"
// @Success      200 {object} APIResponse[[]AgentToolApprovalDTO] "List of approvals"
// @Failure      400 {object} apperror.Error "Invalid request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/projects/{projectId}/agent-approvals [get]
// @Security     bearerAuth
func (h *Handler) HandleListToolApprovals(c echo.Context) error {

	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	var decisionFilter *string
	if d := c.QueryParam("decision"); d != "" {
		decisionFilter = &d
	}

	approvals, err := h.repo.ListToolApprovals(c.Request().Context(), projectID, decisionFilter)
	if err != nil {
		return apperror.NewInternal("failed to list tool approvals", err)
	}

	approvalDTOs := make([]*AgentToolApprovalDTO, len(approvals))
	for i, a := range approvals {
		approvalDTOs[i] = a.ToDTO()
	}

	return c.JSON(http.StatusOK, SuccessResponse(approvalDTOs))
}

// GetADKSessions handles GET /api/projects/:projectId/adk-sessions
func (h *Handler) GetADKSessions(c echo.Context) error {
	projectID := c.Param("projectId")
	if projectID == "" {
		return apperror.NewBadRequest("project ID is required")
	}

	limit := 50
	offset := 0

	sessions, count, err := h.repo.FindADKSessionsByProject(c.Request().Context(), projectID, limit, offset)
	if err != nil {
		return apperror.NewInternal("failed to list adk sessions", err)
	}

	dtos := make([]*ADKSessionDTO, len(sessions))
	for i, s := range sessions {
		dtos[i] = &ADKSessionDTO{
			ID:         s.ID,
			AppName:    s.AppName,
			UserID:     s.UserID,
			State:      s.State,
			CreateTime: s.CreateTime,
			UpdateTime: s.UpdateTime,
		}
	}

	return c.JSON(http.StatusOK, PaginatedResponse[*ADKSessionDTO]{
		Items:      dtos,
		TotalCount: count,
		Limit:      limit,
		Offset:     offset,
	})
}

// GetADKSessionByID handles GET /api/projects/:projectId/adk-sessions/:sessionId
func (h *Handler) GetADKSessionByID(c echo.Context) error {
	projectID := c.Param("projectId")
	sessionID := c.Param("sessionId")
	if projectID == "" || sessionID == "" {
		return apperror.NewBadRequest("project ID and session ID are required")
	}

	session, events, err := h.repo.FindADKSessionByIDForProject(c.Request().Context(), sessionID, projectID)
	if err != nil {
		return apperror.NewInternal("failed to get adk session", err)
	}
	if session == nil {
		return apperror.NewNotFound("adk_session", sessionID)
	}

	dto := &ADKSessionDTO{
		ID:         session.ID,
		AppName:    session.AppName,
		UserID:     session.UserID,
		State:      session.State,
		CreateTime: session.CreateTime,
		UpdateTime: session.UpdateTime,
	}

	eventDTOs := make([]*ADKEventDTO, len(events))
	for i, e := range events {

		eventDTOs[i] = &ADKEventDTO{
			ID:                     e.ID,
			SessionID:              e.SessionID,
			InvocationID:           e.InvocationID,
			Author:                 e.Author,
			Timestamp:              e.Timestamp,
			Branch:                 e.Branch,
			Actions:                e.Actions,
			LongRunningToolIDsJSON: e.LongRunningToolIDsJSON,
			Content:                e.Content,
			Partial:                e.Partial,
			TurnComplete:           e.TurnComplete,
			ErrorCode:              e.ErrorCode,
			ErrorMessage:           e.ErrorMessage,
			Interrupted:            e.Interrupted,
		}
	}

	dto.Events = eventDTOs

	return c.JSON(http.StatusOK, APIResponse[*ADKSessionDTO]{
		Data: dto,
	})
}

func strPtr(s string) *string {
	return &s
}

// --- Agent Override Handlers ---

// ListAgentOverrides handles GET /api/projects/:projectId/agent-definitions/overrides
func (h *Handler) ListAgentOverrides(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	settings, err := h.repo.ListProjectSettings(c.Request().Context(), projectID, SettingsCategoryAgentOverride)
	if err != nil {
		return apperror.NewInternal("failed to list agent overrides", err)
	}

	type OverrideEntry struct {
		AgentName string         `json:"agentName"`
		Override  map[string]any `json:"override"`
	}

	entries := make([]OverrideEntry, len(settings))
	for i, s := range settings {
		entries[i] = OverrideEntry{
			AgentName: s.Key,
			Override:  s.Value,
		}
	}

	return c.JSON(http.StatusOK, SuccessResponse(entries))
}

// GetAgentOverride handles GET /api/projects/:projectId/agent-definitions/overrides/:agentName
func (h *Handler) GetAgentOverride(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	agentName := c.Param("agentName")
	if agentName == "" {
		return apperror.NewBadRequest("agentName is required")
	}

	override, err := h.repo.GetAgentOverride(c.Request().Context(), projectID, agentName)
	if err != nil {
		return apperror.NewInternal("failed to get agent override", err)
	}
	if override == nil {
		return apperror.NewNotFound("AgentOverride", agentName)
	}

	return c.JSON(http.StatusOK, SuccessResponse(override))
}

// SetAgentOverride handles PUT /api/projects/:projectId/agent-definitions/overrides/:agentName
func (h *Handler) SetAgentOverride(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	agentName := c.Param("agentName")
	if agentName == "" {
		return apperror.NewBadRequest("agentName is required")
	}

	var override AgentOverride
	if err := c.Bind(&override); err != nil {
		return apperror.NewBadRequest("invalid request body")
	}

	setting, err := h.repo.SetAgentOverride(c.Request().Context(), projectID, agentName, &override)
	if err != nil {
		return apperror.NewInternal("failed to set agent override", err)
	}

	slog.Info("agent override set",
		slog.String("projectID", projectID),
		slog.String("agentName", agentName),
		slog.String("userID", user.ID))

	return c.JSON(http.StatusOK, SuccessResponse(setting))
}

// DeleteAgentOverride handles DELETE /api/projects/:projectId/agent-definitions/overrides/:agentName
func (h *Handler) DeleteAgentOverride(c echo.Context) error {
	user := auth.MustGetUser(c)

	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}

	agentName := c.Param("agentName")
	if agentName == "" {
		return apperror.NewBadRequest("agentName is required")
	}

	deleted, err := h.repo.DeleteAgentOverride(c.Request().Context(), projectID, agentName)
	if err != nil {
		return apperror.NewInternal("failed to delete agent override", err)
	}
	if !deleted {
		return apperror.NewNotFound("AgentOverride", agentName)
	}

	slog.Info("agent override deleted",
		slog.String("projectID", projectID),
		slog.String("agentName", agentName),
		slog.String("userID", user.ID))

	msg := fmt.Sprintf("Override for %s deleted — agent will use canonical defaults", agentName)
	return c.JSON(http.StatusOK, APIResponse[any]{Success: true, Message: &msg})
}

// --- Generic Project Settings Handlers (key/value config) ---

// GetProjectSetting handles GET /api/projects/:projectId/settings/:category/:key
func (h *Handler) GetProjectSetting(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	category := c.Param("category")
	key := c.Param("key")
	if category == "" || key == "" {
		return apperror.NewBadRequest("category and key are required")
	}
	setting, err := h.repo.GetProjectSetting(c.Request().Context(), projectID, category, key)
	if err != nil {
		return apperror.NewInternal("failed to get project setting", err)
	}
	if setting == nil {
		return apperror.NewNotFound("ProjectSetting", category+"/"+key)
	}
	return c.JSON(http.StatusOK, SuccessResponse(setting))
}

// SetProjectSetting handles PUT /api/projects/:projectId/settings/:category/:key
func (h *Handler) SetProjectSetting(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	category := c.Param("category")
	key := c.Param("key")
	if category == "" || key == "" {
		return apperror.NewBadRequest("category and key are required")
	}
	var value map[string]any
	if err := c.Bind(&value); err != nil {
		return apperror.NewBadRequest("invalid request body: expected a JSON object")
	}
	setting, err := h.repo.UpsertProjectSetting(c.Request().Context(), projectID, category, key, value)
	if err != nil {
		return apperror.NewInternal("failed to set project setting", err)
	}
	return c.JSON(http.StatusOK, SuccessResponse(setting))
}

// DeleteProjectSetting handles DELETE /api/projects/:projectId/settings/:category/:key
func (h *Handler) DeleteProjectSetting(c echo.Context) error {
	user := auth.MustGetUser(c)
	projectID := c.Param("projectId")
	if projectID == "" {
		projectID = user.ProjectID
	}
	if projectID == "" {
		return apperror.NewBadRequest("projectId is required")
	}
	category := c.Param("category")
	key := c.Param("key")
	if category == "" || key == "" {
		return apperror.NewBadRequest("category and key are required")
	}
	deleted, err := h.repo.DeleteProjectSetting(c.Request().Context(), projectID, category, key)
	if err != nil {
		return apperror.NewInternal("failed to delete project setting", err)
	}
	if !deleted {
		return apperror.NewNotFound("ProjectSetting", category+"/"+key)
	}
	return c.JSON(http.StatusOK, SuccessResponse(map[string]bool{"deleted": true}))
}
