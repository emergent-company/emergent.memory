package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/emergent-company/emergent/domain/mcp"
)

// MCPToolHandler implements mcp.AgentToolHandler, providing 16 agent management
// MCP tools. It bridges the mcp package (which cannot import agents) to the
// agents domain by implementing the interface defined in mcp/entity.go.
type MCPToolHandler struct {
	repo     *Repository
	executor *AgentExecutor
	log      *slog.Logger
}

// NewMCPToolHandler creates a new MCPToolHandler.
func NewMCPToolHandler(repo *Repository, executor *AgentExecutor, log *slog.Logger) *MCPToolHandler {
	return &MCPToolHandler{
		repo:     repo,
		executor: executor,
		log:      log,
	}
}

// wrapResult marshals data as indented JSON into an MCP ToolResult.
func wrapResult(data any) (*mcp.ToolResult, error) {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{
			{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}, nil
}

// errResult creates an error ToolResult (non-fatal tool error returned to LLM).
func errResult(msg string) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, msg)},
		},
	}, nil
}

// ============================================================================
// Agent Definition Tools
// ============================================================================

// ExecuteListAgentDefinitions lists all agent definitions for a project.
func (h *MCPToolHandler) ExecuteListAgentDefinitions(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	includeInternal, _ := args["include_internal"].(bool)

	definitions, err := h.repo.FindAllDefinitions(ctx, projectID, includeInternal)
	if err != nil {
		return errResult("failed to list agent definitions: " + err.Error())
	}

	dtos := make([]*AgentDefinitionSummaryDTO, len(definitions))
	for i, def := range definitions {
		dtos[i] = def.ToSummaryDTO()
	}

	return wrapResult(dtos)
}

// ExecuteGetAgentDefinition gets a single agent definition by ID.
func (h *MCPToolHandler) ExecuteGetAgentDefinition(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["definition_id"].(string)
	if id == "" {
		return errResult("definition_id is required")
	}

	def, err := h.repo.FindDefinitionByID(ctx, id, &projectID)
	if err != nil {
		return errResult("failed to get agent definition: " + err.Error())
	}
	if def == nil {
		return errResult(fmt.Sprintf("agent definition not found: %s", id))
	}

	return wrapResult(def.ToDTO())
}

// ExecuteCreateAgentDefinition creates a new agent definition.
func (h *MCPToolHandler) ExecuteCreateAgentDefinition(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return errResult("name is required")
	}

	// Set defaults
	flowType := FlowTypeSingle
	if ft, ok := args["flow_type"].(string); ok && ft != "" {
		flowType = AgentFlowType(ft)
	}

	visibility := VisibilityProject
	if v, ok := args["visibility"].(string); ok && v != "" {
		visibility = AgentVisibility(v)
	}

	isDefault := false
	if d, ok := args["is_default"].(bool); ok {
		isDefault = d
	}

	tools := []string{}
	if t, ok := args["tools"].([]any); ok {
		for _, v := range t {
			if s, ok := v.(string); ok {
				tools = append(tools, s)
			}
		}
	}

	config := map[string]any{}
	if c, ok := args["config"].(map[string]any); ok {
		config = c
	}

	def := &AgentDefinition{
		ProjectID:  projectID,
		Name:       name,
		FlowType:   flowType,
		Visibility: visibility,
		IsDefault:  isDefault,
		Tools:      tools,
		Config:     config,
	}

	// Optional fields
	if desc, ok := args["description"].(string); ok {
		def.Description = &desc
	}
	if sp, ok := args["system_prompt"].(string); ok {
		def.SystemPrompt = &sp
	}
	if ms, ok := args["max_steps"].(float64); ok {
		v := int(ms)
		def.MaxSteps = &v
	}
	if dt, ok := args["default_timeout"].(float64); ok {
		v := int(dt)
		def.DefaultTimeout = &v
	}

	if err := h.repo.CreateDefinition(ctx, def); err != nil {
		return errResult("failed to create agent definition: " + err.Error())
	}

	return wrapResult(def.ToDTO())
}

// ExecuteUpdateAgentDefinition updates an existing agent definition (partial update).
func (h *MCPToolHandler) ExecuteUpdateAgentDefinition(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["definition_id"].(string)
	if id == "" {
		return errResult("definition_id is required")
	}

	def, err := h.repo.FindDefinitionByID(ctx, id, &projectID)
	if err != nil {
		return errResult("failed to get agent definition: " + err.Error())
	}
	if def == nil {
		return errResult(fmt.Sprintf("agent definition not found: %s", id))
	}

	// Apply partial updates
	if name, ok := args["name"].(string); ok {
		def.Name = name
	}
	if desc, ok := args["description"].(string); ok {
		def.Description = &desc
	}
	if sp, ok := args["system_prompt"].(string); ok {
		def.SystemPrompt = &sp
	}
	if ft, ok := args["flow_type"].(string); ok {
		def.FlowType = AgentFlowType(ft)
	}
	if v, ok := args["visibility"].(string); ok {
		def.Visibility = AgentVisibility(v)
	}
	if d, ok := args["is_default"].(bool); ok {
		def.IsDefault = d
	}
	if t, ok := args["tools"].([]any); ok {
		tools := []string{}
		for _, v := range t {
			if s, ok := v.(string); ok {
				tools = append(tools, s)
			}
		}
		def.Tools = tools
	}
	if ms, ok := args["max_steps"].(float64); ok {
		v := int(ms)
		def.MaxSteps = &v
	}
	if dt, ok := args["default_timeout"].(float64); ok {
		v := int(dt)
		def.DefaultTimeout = &v
	}
	if c, ok := args["config"].(map[string]any); ok {
		def.Config = c
	}

	if err := h.repo.UpdateDefinition(ctx, def); err != nil {
		return errResult("failed to update agent definition: " + err.Error())
	}

	return wrapResult(def.ToDTO())
}

// ExecuteDeleteAgentDefinition deletes an agent definition by ID.
func (h *MCPToolHandler) ExecuteDeleteAgentDefinition(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["definition_id"].(string)
	if id == "" {
		return errResult("definition_id is required")
	}

	def, err := h.repo.FindDefinitionByID(ctx, id, &projectID)
	if err != nil {
		return errResult("failed to get agent definition: " + err.Error())
	}
	if def == nil {
		return errResult(fmt.Sprintf("agent definition not found: %s", id))
	}

	if err := h.repo.DeleteDefinition(ctx, id); err != nil {
		return errResult("failed to delete agent definition: " + err.Error())
	}

	return wrapResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("Agent definition %q deleted", def.Name),
	})
}

// ============================================================================
// Agent (Runtime) Tools
// ============================================================================

// ExecuteListAgents lists all agents for a project.
func (h *MCPToolHandler) ExecuteListAgents(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	agents, err := h.repo.FindAll(ctx, projectID)
	if err != nil {
		return errResult("failed to list agents: " + err.Error())
	}

	dtos := make([]*AgentDTO, len(agents))
	for i, agent := range agents {
		dtos[i] = agent.ToDTO()
	}

	return wrapResult(dtos)
}

// ExecuteGetAgent gets a single agent by ID.
func (h *MCPToolHandler) ExecuteGetAgent(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["agent_id"].(string)
	if id == "" {
		return errResult("agent_id is required")
	}

	agent, err := h.repo.FindByID(ctx, id, &projectID)
	if err != nil {
		return errResult("failed to get agent: " + err.Error())
	}
	if agent == nil {
		return errResult(fmt.Sprintf("agent not found: %s", id))
	}

	return wrapResult(agent.ToDTO())
}

// ExecuteCreateAgent creates a new agent.
func (h *MCPToolHandler) ExecuteCreateAgent(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return errResult("name is required")
	}

	strategyType, _ := args["strategy_type"].(string)
	if strategyType == "" {
		return errResult("strategy_type is required")
	}

	cronSchedule, _ := args["cron_schedule"].(string)
	if cronSchedule == "" {
		return errResult("cron_schedule is required")
	}

	// Defaults
	enabled := true
	if e, ok := args["enabled"].(bool); ok {
		enabled = e
	}

	triggerType := TriggerTypeSchedule
	if tt, ok := args["trigger_type"].(string); ok && tt != "" {
		triggerType = AgentTriggerType(tt)
	}

	executionMode := ExecutionModeExecute
	if em, ok := args["execution_mode"].(string); ok && em != "" {
		executionMode = AgentExecutionMode(em)
	}

	config := map[string]any{}
	if c, ok := args["config"].(map[string]any); ok {
		config = c
	}

	agent := &Agent{
		ProjectID:     projectID,
		Name:          name,
		StrategyType:  strategyType,
		CronSchedule:  cronSchedule,
		Enabled:       enabled,
		TriggerType:   triggerType,
		ExecutionMode: executionMode,
		Config:        config,
	}

	// Optional fields
	if prompt, ok := args["prompt"].(string); ok {
		agent.Prompt = &prompt
	}
	if desc, ok := args["description"].(string); ok {
		agent.Description = &desc
	}

	if err := h.repo.Create(ctx, agent); err != nil {
		return errResult("failed to create agent: " + err.Error())
	}

	return wrapResult(agent.ToDTO())
}

// ExecuteUpdateAgent updates an existing agent (partial update).
func (h *MCPToolHandler) ExecuteUpdateAgent(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["agent_id"].(string)
	if id == "" {
		return errResult("agent_id is required")
	}

	agent, err := h.repo.FindByID(ctx, id, &projectID)
	if err != nil {
		return errResult("failed to get agent: " + err.Error())
	}
	if agent == nil {
		return errResult(fmt.Sprintf("agent not found: %s", id))
	}

	// Apply partial updates
	if name, ok := args["name"].(string); ok {
		agent.Name = name
	}
	if prompt, ok := args["prompt"].(string); ok {
		agent.Prompt = &prompt
	}
	if enabled, ok := args["enabled"].(bool); ok {
		agent.Enabled = enabled
	}
	if cs, ok := args["cron_schedule"].(string); ok {
		agent.CronSchedule = cs
	}
	if tt, ok := args["trigger_type"].(string); ok {
		agent.TriggerType = AgentTriggerType(tt)
	}
	if em, ok := args["execution_mode"].(string); ok {
		agent.ExecutionMode = AgentExecutionMode(em)
	}
	if c, ok := args["config"].(map[string]any); ok {
		agent.Config = c
	}
	if desc, ok := args["description"].(string); ok {
		agent.Description = &desc
	}

	if err := h.repo.Update(ctx, agent); err != nil {
		return errResult("failed to update agent: " + err.Error())
	}

	return wrapResult(agent.ToDTO())
}

// ExecuteDeleteAgent deletes an agent by ID.
func (h *MCPToolHandler) ExecuteDeleteAgent(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["agent_id"].(string)
	if id == "" {
		return errResult("agent_id is required")
	}

	agent, err := h.repo.FindByID(ctx, id, &projectID)
	if err != nil {
		return errResult("failed to get agent: " + err.Error())
	}
	if agent == nil {
		return errResult(fmt.Sprintf("agent not found: %s", id))
	}

	if err := h.repo.Delete(ctx, id); err != nil {
		return errResult("failed to delete agent: " + err.Error())
	}

	return wrapResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("Agent %q deleted", agent.Name),
	})
}

// ExecuteTriggerAgent triggers an immediate run of an agent.
func (h *MCPToolHandler) ExecuteTriggerAgent(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["agent_id"].(string)
	if id == "" {
		return errResult("agent_id is required")
	}

	agent, err := h.repo.FindByID(ctx, id, &projectID)
	if err != nil {
		return errResult("failed to get agent: " + err.Error())
	}
	if agent == nil {
		return errResult(fmt.Sprintf("agent not found: %s", id))
	}

	// Check if executor is available
	if h.executor == nil {
		// Stub mode: create a run record but skip execution
		run, err := h.repo.CreateRun(ctx, id)
		if err != nil {
			return errResult("failed to create run: " + err.Error())
		}
		_ = h.repo.SkipRun(ctx, run.ID, "Executor not available")
		return wrapResult(map[string]any{
			"success": true,
			"run_id":  run.ID,
			"message": "Agent triggered (stub mode)",
		})
	}

	// Look up the agent definition for this agent (if one exists)
	agentDef, _ := h.repo.FindDefinitionByName(ctx, agent.ProjectID, agent.Name)

	// Build the user message
	userMessage := "Execute agent tasks"
	if msg, ok := args["message"].(string); ok && msg != "" {
		userMessage = msg
	} else if agent.Prompt != nil && *agent.Prompt != "" {
		userMessage = *agent.Prompt
	}

	result, err := h.executor.Execute(ctx, ExecuteRequest{
		Agent:           agent,
		AgentDefinition: agentDef,
		ProjectID:       agent.ProjectID,
		UserMessage:     userMessage,
	})
	if err != nil {
		return errResult("failed to execute agent: " + err.Error())
	}

	return wrapResult(map[string]any{
		"success":  true,
		"run_id":   result.RunID,
		"status":   result.Status,
		"steps":    result.Steps,
		"duration": result.Duration.String(),
		"message":  "Agent triggered successfully",
	})
}

// ============================================================================
// Agent Run Tools
// ============================================================================

// ExecuteListAgentRuns lists agent runs for a project with pagination and filters.
func (h *MCPToolHandler) ExecuteListAgentRuns(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	// Parse pagination
	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 100 {
			limit = 100
		}
	}
	offset := 0
	if o, ok := args["offset"].(float64); ok && o >= 0 {
		offset = int(o)
	}

	// Parse filters
	var filters RunFilters
	if agentID, ok := args["agent_id"].(string); ok && agentID != "" {
		filters.AgentID = &agentID
	}
	if statusStr, ok := args["status"].(string); ok && statusStr != "" {
		status := AgentRunStatus(statusStr)
		filters.Status = &status
	}

	runs, totalCount, err := h.repo.FindRunsByProjectPaginated(ctx, projectID, filters, limit, offset)
	if err != nil {
		return errResult("failed to list agent runs: " + err.Error())
	}

	dtos := make([]*AgentRunDTO, len(runs))
	for i, run := range runs {
		dtos[i] = run.ToDTO()
	}

	return wrapResult(PaginatedResponse[*AgentRunDTO]{
		Items:      dtos,
		TotalCount: totalCount,
		Limit:      limit,
		Offset:     offset,
	})
}

// ExecuteGetAgentRun gets a single agent run by ID.
func (h *MCPToolHandler) ExecuteGetAgentRun(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return errResult("run_id is required")
	}

	run, err := h.repo.FindRunByIDForProject(ctx, runID, projectID)
	if err != nil {
		return errResult("failed to get agent run: " + err.Error())
	}
	if run == nil {
		return errResult(fmt.Sprintf("agent run not found: %s", runID))
	}

	return wrapResult(run.ToDTO())
}

// ExecuteGetAgentRunMessages gets messages for an agent run.
func (h *MCPToolHandler) ExecuteGetAgentRunMessages(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return errResult("run_id is required")
	}

	// Verify the run belongs to this project
	run, err := h.repo.FindRunByIDForProject(ctx, runID, projectID)
	if err != nil {
		return errResult("failed to get agent run: " + err.Error())
	}
	if run == nil {
		return errResult(fmt.Sprintf("agent run not found: %s", runID))
	}

	messages, err := h.repo.FindMessagesByRunID(ctx, runID)
	if err != nil {
		return errResult("failed to get run messages: " + err.Error())
	}

	dtos := make([]*AgentRunMessageDTO, len(messages))
	for i, msg := range messages {
		dtos[i] = msg.ToDTO()
	}

	return wrapResult(dtos)
}

// ExecuteGetAgentRunToolCalls gets tool calls for an agent run.
func (h *MCPToolHandler) ExecuteGetAgentRunToolCalls(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return errResult("run_id is required")
	}

	// Verify the run belongs to this project
	run, err := h.repo.FindRunByIDForProject(ctx, runID, projectID)
	if err != nil {
		return errResult("failed to get agent run: " + err.Error())
	}
	if run == nil {
		return errResult(fmt.Sprintf("agent run not found: %s", runID))
	}

	toolCalls, err := h.repo.FindToolCallsByRunID(ctx, runID)
	if err != nil {
		return errResult("failed to get run tool calls: " + err.Error())
	}

	dtos := make([]*AgentRunToolCallDTO, len(toolCalls))
	for i, tc := range toolCalls {
		dtos[i] = tc.ToDTO()
	}

	return wrapResult(dtos)
}

// ============================================================================
// Agent Catalog Tools
// ============================================================================

// ExecuteListAvailableAgents returns a lightweight catalog of all agent definitions
// for the project, including name, description, tools, flow_type, and visibility.
// Unlike list_agent_definitions, this returns a simplified summary without IDs or timestamps,
// matching the format used by agent coordination tools (spawn_agents).
func (h *MCPToolHandler) ExecuteListAvailableAgents(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	// Always include internal — MCP callers should see the full catalog
	defs, err := h.repo.FindAllDefinitions(ctx, projectID, true)
	if err != nil {
		return errResult("failed to list available agents: " + err.Error())
	}

	agents := make([]AgentSummary, 0, len(defs))
	for _, def := range defs {
		desc := ""
		if def.Description != nil {
			desc = *def.Description
		}
		agents = append(agents, AgentSummary{
			Name:        def.Name,
			Description: desc,
			Tools:       def.Tools,
			FlowType:    def.FlowType,
			Visibility:  string(def.Visibility),
		})
	}

	return wrapResult(ListAvailableAgentsResult{
		Agents: agents,
		Count:  len(agents),
	})
}

// ============================================================================
// Tool Definitions
// ============================================================================

// GetAgentToolDefinitions returns MCP tool definitions for all agent tools.
func (h *MCPToolHandler) GetAgentToolDefinitions() []mcp.ToolDefinition {
	return []mcp.ToolDefinition{
		// --- Agent Definitions ---
		{
			Name:        "list_agent_definitions",
			Description: "List all agent definitions for the current project. Agent definitions store agent configurations (system prompt, tools, flow type, visibility, ACP config). Returns summary DTOs with tool counts.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"include_internal": {
						Type:        "boolean",
						Description: "Include internal-visibility definitions (default: false)",
						Default:     false,
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "get_agent_definition",
			Description: "Get a single agent definition by ID with full details including system prompt, tools, model config, ACP config, and visibility settings.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"definition_id": {
						Type:        "string",
						Description: "The UUID of the agent definition to retrieve",
					},
				},
				Required: []string{"definition_id"},
			},
		},
		{
			Name:        "create_agent_definition",
			Description: "Create a new agent definition. Defines the configuration template for an agent including its system prompt, available tools, flow type, and visibility.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"name": {
						Type:        "string",
						Description: "Unique name for the agent definition",
					},
					"description": {
						Type:        "string",
						Description: "Human-readable description of what this agent does",
					},
					"system_prompt": {
						Type:        "string",
						Description: "System prompt that instructs the LLM how to behave",
					},
					"tools": {
						Type:        "string",
						Description: "JSON array of tool names available to this agent (e.g. [\"query_entities\", \"create_entity\"])",
					},
					"flow_type": {
						Type:        "string",
						Description: "Execution flow type",
						Enum:        []string{"single", "sequential", "loop"},
						Default:     "single",
					},
					"visibility": {
						Type:        "string",
						Description: "Visibility level: external (ACP-discoverable), project (admin UI only), internal (other agents only)",
						Enum:        []string{"external", "project", "internal"},
						Default:     "project",
					},
					"is_default": {
						Type:        "boolean",
						Description: "Whether this is the default definition for the project",
						Default:     false,
					},
					"max_steps": {
						Type:        "integer",
						Description: "Maximum number of steps per run (global cap: 500)",
					},
					"default_timeout": {
						Type:        "integer",
						Description: "Default timeout in seconds for agent runs",
					},
					"config": {
						Type:        "string",
						Description: "Additional configuration as JSON object",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "update_agent_definition",
			Description: "Update an existing agent definition. Only provided fields are updated (partial update).",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"definition_id": {
						Type:        "string",
						Description: "The UUID of the agent definition to update",
					},
					"name": {
						Type:        "string",
						Description: "New name for the agent definition",
					},
					"description": {
						Type:        "string",
						Description: "New description",
					},
					"system_prompt": {
						Type:        "string",
						Description: "New system prompt",
					},
					"flow_type": {
						Type:        "string",
						Description: "New flow type",
						Enum:        []string{"single", "sequential", "loop"},
					},
					"visibility": {
						Type:        "string",
						Description: "New visibility level",
						Enum:        []string{"external", "project", "internal"},
					},
					"is_default": {
						Type:        "boolean",
						Description: "Whether this is the default definition",
					},
					"max_steps": {
						Type:        "integer",
						Description: "New maximum steps per run",
					},
					"default_timeout": {
						Type:        "integer",
						Description: "New default timeout in seconds",
					},
				},
				Required: []string{"definition_id"},
			},
		},
		{
			Name:        "delete_agent_definition",
			Description: "Delete an agent definition by ID. This removes the configuration template but does not affect running agents.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"definition_id": {
						Type:        "string",
						Description: "The UUID of the agent definition to delete",
					},
				},
				Required: []string{"definition_id"},
			},
		},

		// --- Agents (runtime) ---
		{
			Name:        "list_agents",
			Description: "List all runtime agents for the current project. Agents track runtime state like last_run_at, cron_schedule, and enabled status.",
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]mcp.PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "get_agent",
			Description: "Get a single runtime agent by ID with full details including schedule, trigger type, execution mode, and last run status.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"agent_id": {
						Type:        "string",
						Description: "The UUID of the agent to retrieve",
					},
				},
				Required: []string{"agent_id"},
			},
		},
		{
			Name:        "create_agent",
			Description: "Create a new runtime agent with schedule, trigger type, and execution configuration.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"name": {
						Type:        "string",
						Description: "Name for the agent",
					},
					"strategy_type": {
						Type:        "string",
						Description: "Strategy type for the agent (e.g. 'extraction', 'enrichment', 'analysis')",
					},
					"cron_schedule": {
						Type:        "string",
						Description: "Cron expression for scheduling (e.g. '0 */6 * * *' for every 6 hours)",
					},
					"prompt": {
						Type:        "string",
						Description: "Prompt/instructions for the agent",
					},
					"description": {
						Type:        "string",
						Description: "Human-readable description",
					},
					"enabled": {
						Type:        "boolean",
						Description: "Whether the agent is enabled (default: true)",
						Default:     true,
					},
					"trigger_type": {
						Type:        "string",
						Description: "How the agent is triggered",
						Enum:        []string{"schedule", "manual", "reaction"},
						Default:     "schedule",
					},
					"execution_mode": {
						Type:        "string",
						Description: "How the agent executes actions",
						Enum:        []string{"suggest", "execute", "hybrid"},
						Default:     "execute",
					},
					"config": {
						Type:        "string",
						Description: "Additional configuration as JSON object",
					},
				},
				Required: []string{"name", "strategy_type", "cron_schedule"},
			},
		},
		{
			Name:        "update_agent",
			Description: "Update an existing runtime agent. Only provided fields are updated (partial update).",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"agent_id": {
						Type:        "string",
						Description: "The UUID of the agent to update",
					},
					"name": {
						Type:        "string",
						Description: "New name",
					},
					"prompt": {
						Type:        "string",
						Description: "New prompt/instructions",
					},
					"description": {
						Type:        "string",
						Description: "New description",
					},
					"enabled": {
						Type:        "boolean",
						Description: "Enable or disable the agent",
					},
					"cron_schedule": {
						Type:        "string",
						Description: "New cron schedule",
					},
					"trigger_type": {
						Type:        "string",
						Description: "New trigger type",
						Enum:        []string{"schedule", "manual", "reaction"},
					},
					"execution_mode": {
						Type:        "string",
						Description: "New execution mode",
						Enum:        []string{"suggest", "execute", "hybrid"},
					},
				},
				Required: []string{"agent_id"},
			},
		},
		{
			Name:        "delete_agent",
			Description: "Delete a runtime agent by ID. This stops any scheduled runs and removes the agent.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"agent_id": {
						Type:        "string",
						Description: "The UUID of the agent to delete",
					},
				},
				Required: []string{"agent_id"},
			},
		},
		{
			Name:        "trigger_agent",
			Description: "Trigger an immediate run of an agent. Optionally provide a custom message for this run. Returns the run ID and execution status.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"agent_id": {
						Type:        "string",
						Description: "The UUID of the agent to trigger",
					},
					"message": {
						Type:        "string",
						Description: "Optional custom message/instructions for this specific run",
					},
				},
				Required: []string{"agent_id"},
			},
		},

		// --- Agent Runs ---
		{
			Name:        "list_agent_runs",
			Description: "List agent runs for the current project with pagination and optional filters by agent ID or status.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"agent_id": {
						Type:        "string",
						Description: "Filter by agent ID (UUID)",
					},
					"status": {
						Type:        "string",
						Description: "Filter by run status",
						Enum:        []string{"running", "success", "skipped", "error", "paused", "cancelled"},
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of results (1-100, default: 20)",
						Default:     20,
						Minimum:     intPtr(1),
						Maximum:     intPtr(100),
					},
					"offset": {
						Type:        "integer",
						Description: "Number of results to skip (default: 0)",
						Default:     0,
						Minimum:     intPtr(0),
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "get_agent_run",
			Description: "Get details of a single agent run including status, duration, step count, and error information.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"run_id": {
						Type:        "string",
						Description: "The UUID of the agent run to retrieve",
					},
				},
				Required: []string{"run_id"},
			},
		},
		{
			Name:        "get_agent_run_messages",
			Description: "Get all LLM messages exchanged during an agent run, ordered by step number. Includes system, user, assistant, and tool_result messages.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"run_id": {
						Type:        "string",
						Description: "The UUID of the agent run",
					},
				},
				Required: []string{"run_id"},
			},
		},
		{
			Name:        "get_agent_run_tool_calls",
			Description: "Get all tool calls made during an agent run, including input, output, status, and duration for each call.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"run_id": {
						Type:        "string",
						Description: "The UUID of the agent run",
					},
				},
				Required: []string{"run_id"},
			},
		},

		// --- Agent Catalog ---
		{
			Name:        "list_available_agents",
			Description: "List all available agents in the project catalog. Returns a lightweight summary with name, description, tools list, flow_type, and visibility for each agent definition. Does not include system prompts or IDs — use list_agent_definitions for full details.",
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]mcp.PropertySchema{},
				Required:   []string{},
			},
		},
	}
}

// intPtr returns a pointer to an int value.
func intPtr(i int) *int {
	return &i
}
