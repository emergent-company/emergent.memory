package agents

import (
	"encoding/json"
	"time"

	"github.com/emergent-company/emergent.memory/domain/agents/toolgroups"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/httputil"
)

// AgentDTO is the response DTO for an agent
type AgentDTO struct {
	ID                  string             `json:"id"`
	ProjectID           string             `json:"projectId"`
	Name                string             `json:"name"`
	StrategyType        string             `json:"strategyType"`
	Prompt              *string            `json:"prompt"`
	CronSchedule        string             `json:"cronSchedule"`
	Enabled             bool               `json:"enabled"`
	TriggerType         AgentTriggerType   `json:"triggerType"`
	ReactionConfig      *ReactionConfig    `json:"reactionConfig"`
	ExecutionMode       AgentExecutionMode `json:"executionMode"`
	Capabilities        *AgentCapabilities `json:"capabilities"`
	Config              map[string]any     `json:"config"`
	Description         *string            `json:"description"`
	AgentDefinitionID   *string            `json:"agentDefinitionId,omitempty"`
	LastRunAt           *time.Time         `json:"lastRunAt"`
	LastRunStatus       *string            `json:"lastRunStatus"`
	ConsecutiveFailures int                `json:"consecutiveFailures"`
	DisabledReason      *string            `json:"disabledReason,omitempty"`
	BudgetUSD           *float64           `json:"budgetUsd,omitempty"`
	CurrentMonthSpend   *float64           `json:"currentMonthSpend,omitempty"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
}

// AgentRunDTO is the response DTO for an agent run
type AgentRunDTO struct {
	ID            string         `json:"id"`
	AgentID       string         `json:"agentId"`
	AgentName     string         `json:"agentName,omitempty"`
	Status        AgentRunStatus `json:"status"`
	SessionStatus SessionStatus  `json:"sessionStatus"`
	StartedAt     time.Time      `json:"startedAt"`
	CompletedAt   *time.Time     `json:"completedAt"`
	DurationMs    *int           `json:"durationMs"`
	Summary       map[string]any `json:"summary"`
	ErrorMessage  *string        `json:"errorMessage"`
	SkipReason    *string        `json:"skipReason"`

	// Multi-agent coordination fields
	ParentRunID *string `json:"parentRunId,omitempty"`
	StepCount   int     `json:"stepCount"`
	MaxSteps    *int    `json:"maxSteps,omitempty"`
	ResumedFrom *string `json:"resumedFrom,omitempty"`

	// Observability linkage
	TraceID   *string `json:"traceId,omitempty"`
	RootRunID *string `json:"rootRunId,omitempty"`

	// Model used for this run (resolved at execution time)
	Model string `json:"model,omitempty"`

	// Provider identifies the LLM provider used (e.g. "google", "google-vertex", "openai", "deepseek").
	Provider string `json:"provider,omitempty"`

	// Token usage aggregated from kb.llm_usage_events for this run.
	TokenUsage *RunTokenUsage `json:"tokenUsage,omitempty"`

	// Spans holds the flattened OTLP trace spans for this run, fetched from
	// Tempo when tracing is enabled. Omitted when tracing is disabled, Tempo is
	// unreachable, or the run has no trace. The tree is reconstructible via
	// parentSpanId (empty on the root span).
	Spans []RunTraceSpan `json:"spans,omitempty"`

	// Workspace/sandbox details for this run (when applicable).
	Workspace *RunWorkspaceDTO `json:"workspace,omitempty"`

	// TriggerMetadata is the structured metadata passed at trigger time (e.g. title, context).
	TriggerMetadata map[string]any `json:"triggerMetadata,omitempty"`

	AgentDefinitionID *string `json:"agentDefinitionId,omitempty"`

	Tools []string `json:"tools,omitempty"`
}

// RunWorkspaceDTO exposes sandbox/workspace details on a run response.
type RunWorkspaceDTO struct {
	Provider    string `json:"provider"`
	ContainerID string `json:"containerId,omitempty"`
	BaseImage   string `json:"baseImage,omitempty"`
	ImageDigest string `json:"imageDigest,omitempty"`
}

// RunTokenUsage holds aggregated LLM token counts and estimated cost for a run.
type RunTokenUsage struct {
	TotalInputTokens  int64 `json:"totalInputTokens"`
	TotalOutputTokens int64 `json:"totalOutputTokens"`
	// CachedTokens is the number of input tokens served from the provider's
	// prompt cache (e.g. Google context caching). These are already counted
	// within TotalInputTokens but are broken out here for cost analysis.
	CachedTokens     int64   `json:"cachedTokens"`
	EstimatedCostUSD float64 `json:"estimatedCostUsd"`
	// Provider and Model identify the LLM used. Format: "<provider>/<model>",
	// e.g. "google/gemini-2.0-flash". Empty when unknown.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// CreateAgentDTO is the request DTO for creating an agent
type CreateAgentDTO struct {
	ProjectID         string             `json:"projectId" validate:"required,uuid"`
	Name              string             `json:"name" validate:"required"`
	StrategyType      string             `json:"strategyType" validate:"required"`
	Prompt            *string            `json:"prompt"`
	CronSchedule      string             `json:"cronSchedule" validate:"required"`
	Enabled           *bool              `json:"enabled"`
	TriggerType       AgentTriggerType   `json:"triggerType"`
	ReactionConfig    *ReactionConfig    `json:"reactionConfig"`
	ExecutionMode     AgentExecutionMode `json:"executionMode"`
	Capabilities      *AgentCapabilities `json:"capabilities"`
	Config            map[string]any     `json:"config"`
	Description       *string            `json:"description"`
	AgentDefinitionID *string            `json:"agentDefinitionId,omitempty"`
}

// UpdateAgentDTO is the request DTO for updating an agent
type UpdateAgentDTO struct {
	Name              *string             `json:"name"`
	Prompt            *string             `json:"prompt"`
	Enabled           *bool               `json:"enabled"`
	CronSchedule      *string             `json:"cronSchedule"`
	TriggerType       *AgentTriggerType   `json:"triggerType"`
	ReactionConfig    *ReactionConfig     `json:"reactionConfig"`
	ExecutionMode     *AgentExecutionMode `json:"executionMode"`
	Capabilities      *AgentCapabilities  `json:"capabilities"`
	Config            map[string]any      `json:"config"`
	Description       *string             `json:"description"`
	AgentDefinitionID *string             `json:"agentDefinitionId,omitempty"`
	// DisabledReason is admin-only: sets the reason an agent is disabled.
	// Requires admin:write scope. Ignored for non-admin callers.
	DisabledReason *string `json:"disabledReason,omitempty"`
}

// BatchTriggerDTO is the request DTO for batch triggering an agent
type BatchTriggerDTO struct {
	ObjectIDs []string `json:"objectIds" validate:"required,min=1,max=100,dive,uuid"`
}

// PendingEventObjectDTO represents a graph object pending processing
type PendingEventObjectDTO struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Key       string    `json:"key"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PendingEventsResponseDTO is the response for pending events query
type PendingEventsResponseDTO struct {
	TotalCount     int                     `json:"totalCount"`
	Objects        []PendingEventObjectDTO `json:"objects"`
	ReactionConfig struct {
		ObjectTypes []string `json:"objectTypes"`
		Events      []string `json:"events"`
	} `json:"reactionConfig"`
}

// BatchTriggerResponseDTO is the response for batch trigger
type BatchTriggerResponseDTO struct {
	Queued         int `json:"queued"`
	Skipped        int `json:"skipped"`
	SkippedDetails []struct {
		ObjectID string `json:"objectId"`
		Reason   string `json:"reason"`
	} `json:"skippedDetails"`
}

// TriggerRequestDTO is the request body for triggering an agent
type TriggerRequestDTO struct {
	Prompt   string            `json:"prompt"`
	Context  map[string]any    `json:"context,omitempty"`
	Model    string            `json:"model,omitempty"`
	EnvVars  map[string]string `json:"env_vars,omitempty"`
	MaxSteps *int              `json:"maxSteps,omitempty"`
	// SessionID ties this trigger to a persistent ADK conversation session.
	// When provided, the ADK runner reuses the session associated with this ID,
	// accumulating all prior turns as conversation history for the agent.
	// When empty, each trigger starts a fresh session (current behavior).
	SessionID string `json:"sessionId,omitempty"`
}

// TriggerResponseDTO is the response for triggering an agent
type TriggerResponseDTO struct {
	Success bool    `json:"success"`
	RunID   *string `json:"runId,omitempty"`
	Message *string `json:"message,omitempty"`
	Error   *string `json:"error,omitempty"`
}

// APIResponse wraps API responses with success flag.
// Alias for httputil.APIResponse — single source of truth in pkg/httputil.
type APIResponse[T any] = httputil.APIResponse[T]

// PaginatedResponse wraps paginated API responses.
// Alias for httputil.PaginatedResponse — single source of truth in pkg/httputil.
type PaginatedResponse[T any] = httputil.PaginatedResponse[T]

// SuccessResponse creates a successful API response wrapping data.
func SuccessResponse[T any](data T) APIResponse[T] {
	return httputil.NewSuccessResponse(data)
}

// ErrorResponse creates an error API response with the given error message.
func ErrorResponse[T any](err string) APIResponse[T] {
	return httputil.NewErrorResponse[T](err)
}

// ToDTO converts an Agent entity to AgentDTO
func (a *Agent) ToDTO() *AgentDTO {
	return &AgentDTO{
		ID:                  a.ID,
		ProjectID:           a.ProjectID,
		Name:                a.Name,
		StrategyType:        a.StrategyType,
		Prompt:              a.Prompt,
		CronSchedule:        a.CronSchedule,
		Enabled:             a.Enabled,
		TriggerType:         a.TriggerType,
		ReactionConfig:      a.ReactionConfig,
		ExecutionMode:       a.ExecutionMode,
		Capabilities:        a.Capabilities,
		Config:              a.Config,
		Description:         a.Description,
		AgentDefinitionID:   a.AgentDefinitionID,
		LastRunAt:           a.LastRunAt,
		LastRunStatus:       a.LastRunStatus,
		ConsecutiveFailures: a.ConsecutiveFailures,
		DisabledReason:      a.DisabledReason,
		CreatedAt:           a.CreatedAt,
		UpdatedAt:           a.UpdatedAt,
	}
}

// ToDTO converts an AgentRun entity to AgentRunDTO
func (r *AgentRun) ToDTO() *AgentRunDTO {
	dto := &AgentRunDTO{
		ID:                r.ID,
		AgentID:           r.AgentID,
		Status:            r.Status,
		SessionStatus:     r.SessionStatus,
		StartedAt:         r.StartedAt,
		CompletedAt:       r.CompletedAt,
		DurationMs:        r.DurationMs,
		Summary:           r.Summary,
		ErrorMessage:      r.ErrorMessage,
		SkipReason:        r.SkipReason,
		ParentRunID:       r.ParentRunID,
		StepCount:         r.StepCount,
		MaxSteps:          r.MaxSteps,
		ResumedFrom:       r.ResumedFrom,
		TraceID:           r.TraceID,
		RootRunID:         r.RootRunID,
		AgentDefinitionID: r.AgentDefinitionID,
		Tools:             r.Tools,
	}
	if r.Agent != nil {
		dto.AgentName = r.Agent.Name
	}
	if r.Model != nil {
		dto.Model = *r.Model
	}
	if r.Provider != nil {
		dto.Provider = *r.Provider
	}
	if len(r.TriggerMetadata) > 0 {
		dto.TriggerMetadata = r.TriggerMetadata
	}
	return dto
}

// AgentWithDefinitionDTO enriches AgentDTO with fields from the agent's definition.
// Returned by list_agents MCP tool so callers get name, description, and config
// in a single call without a separate list_agent_definitions lookup.
type AgentWithDefinitionDTO struct {
	// Runtime fields (from kb.agents)
	ID            string             `json:"id"`
	ProjectID     string             `json:"projectId"`
	Name          string             `json:"name"`
	Enabled       bool               `json:"enabled"`
	TriggerType   AgentTriggerType   `json:"triggerType"`
	ExecutionMode AgentExecutionMode `json:"executionMode"`
	LastRunAt     *time.Time         `json:"lastRunAt,omitempty"`
	LastRunStatus *string            `json:"lastRunStatus,omitempty"`

	// Definition fields (from kb.agent_definitions, may be nil if no definition)
	Description *string       `json:"description,omitempty"`
	FlowType    AgentFlowType `json:"flowType,omitempty"`
	Model       *ModelConfig  `json:"model,omitempty"`
	AgentType   *string       `json:"agentType,omitempty"`
	Tier        *string       `json:"tier,omitempty"`
}

// --- Agent Definition DTOs ---

// ToolGroupDTO is the computed catalog entry for one tool group. It is derived
// on read from the definition's Tools/BannedTools and its stored @group: policy
// (never persisted). Policy is "" (inherit default), "allow", "ask", or "deny";
// Enabled means at least one member is in Tools and not banned.
type ToolGroupDTO struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Policy      string   `json:"policy"`
	Enabled     bool     `json:"enabled"`
	Tools       []string `json:"tools"`
}

// AgentDefinitionDTO is the full response DTO for an agent definition
type AgentDefinitionDTO struct {
	ID                string                `json:"id"`
	ProductID         *string               `json:"productId,omitempty"`
	ProjectID         string                `json:"projectId"`
	Name              string                `json:"name"`
	Description       *string               `json:"description,omitempty"`
	SystemPrompt      *string               `json:"systemPrompt,omitempty"`
	Model             *ModelConfig          `json:"model,omitempty"`
	Tools             []string              `json:"tools"`
	BannedTools       []string              `json:"bannedTools,omitempty"`
	Skills            []string              `json:"skills"`
	AutoLoadSkills    bool                  `json:"autoLoadSkills"`
	FlowType          AgentFlowType         `json:"flowType"`
	IsDefault         bool                  `json:"isDefault"`
	Enabled           bool                  `json:"enabled"`
	MaxSteps          *int                  `json:"maxSteps,omitempty"`
	DefaultTimeout    *int                  `json:"defaultTimeout,omitempty"`
	Visibility        AgentVisibility       `json:"visibility"`
	DispatchMode      AgentDispatchMode     `json:"dispatchMode"`
	ACPConfig         *ACPConfig            `json:"acpConfig,omitempty"`
	Config            map[string]any        `json:"config,omitempty"`
	SandboxConfig     map[string]any        `json:"workspaceConfig,omitempty"`
	ToolPolicies      map[string]ToolPolicy `json:"toolPolicies,omitempty"`
	DefaultToolPolicy ToolPolicyDefault     `json:"defaultToolPolicy,omitempty"`
	UIConfig          json.RawMessage       `json:"uiConfig,omitempty"`
	CreatedAt         time.Time             `json:"createdAt"`
	UpdatedAt         time.Time             `json:"updatedAt"`
	// ToolGroups is the computed capability-group catalog (server-owned
	// taxonomy) rendered by the gateway. Read-only; groups with no member tools
	// at all are omitted — a fully-disabled group still renders (with
	// enabled=false) so it can be re-enabled.
	ToolGroups []ToolGroupDTO `json:"toolGroups,omitempty"`
	// EffectiveModel is the resolved generative model this definition would run
	// with (per-agent override, else project config → provider-credential
	// generative model). Only populated on GET /agent-definitions/:id, not on
	// list endpoints.
	EffectiveModel string `json:"effectiveModel,omitempty"`
}

// AgentDefinitionSummaryDTO is a lightweight DTO for listing agent definitions
type AgentDefinitionSummaryDTO struct {
	ID               string          `json:"id"`
	ProjectID        string          `json:"projectId"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	FlowType         AgentFlowType   `json:"flowType"`
	Visibility       AgentVisibility `json:"visibility"`
	IsDefault        bool            `json:"isDefault"`
	Enabled          bool            `json:"enabled"`
	ToolCount        int             `json:"toolCount"`
	Skills           []string        `json:"skills"`
	HasSandboxConfig bool            `json:"hasSandboxConfig"`
	UIConfig         json.RawMessage `json:"uiConfig,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	// EffectiveModel is the model this definition would run with: the per-agent
	// override when set, otherwise the project's resolved generative default
	// (project config → provider-credential generative model). Empty when the
	// project has neither configured.
	EffectiveModel string `json:"effectiveModel,omitempty"`
}

// CreateAgentDefinitionDTO is the request DTO for creating an agent definition
type CreateAgentDefinitionDTO struct {
	Name              string                `json:"name" validate:"required"`
	Description       *string               `json:"description"`
	SystemPrompt      *string               `json:"systemPrompt"`
	Model             *ModelConfig          `json:"model"`
	Tools             []string              `json:"tools"`
	BannedTools       []string              `json:"bannedTools,omitempty"`
	Skills            []string              `json:"skills"`
	AutoLoadSkills    *bool                 `json:"autoLoadSkills"`
	FlowType          AgentFlowType         `json:"flowType"`
	IsDefault         *bool                 `json:"isDefault"`
	Enabled           *bool                 `json:"enabled"`
	MaxSteps          *int                  `json:"maxSteps"`
	DefaultTimeout    *int                  `json:"defaultTimeout"`
	Visibility        AgentVisibility       `json:"visibility"`
	DispatchMode      AgentDispatchMode     `json:"dispatchMode"`
	ACPConfig         *ACPConfig            `json:"acpConfig"`
	Config            map[string]any        `json:"config"`
	SandboxConfig     map[string]any        `json:"workspaceConfig"`
	ToolPolicies      map[string]ToolPolicy `json:"toolPolicies,omitempty"`
	DefaultToolPolicy ToolPolicyDefault     `json:"defaultToolPolicy,omitempty"`
	UIConfig          json.RawMessage       `json:"uiConfig,omitempty"`
}

// UpdateAgentDefinitionDTO is the request DTO for updating an agent definition
type UpdateAgentDefinitionDTO struct {
	Name              *string               `json:"name"`
	Description       *string               `json:"description"`
	SystemPrompt      *string               `json:"systemPrompt"`
	Model             *ModelConfig          `json:"model"`
	Tools             []string              `json:"tools"`
	BannedTools       []string              `json:"bannedTools,omitempty"`
	Skills            []string              `json:"skills"`
	AutoLoadSkills    *bool                 `json:"autoLoadSkills"`
	FlowType          *AgentFlowType        `json:"flowType"`
	IsDefault         *bool                 `json:"isDefault"`
	Enabled           *bool                 `json:"enabled"`
	MaxSteps          *int                  `json:"maxSteps"`
	DefaultTimeout    *int                  `json:"defaultTimeout"`
	Visibility        *AgentVisibility      `json:"visibility"`
	DispatchMode      *AgentDispatchMode    `json:"dispatchMode"`
	ACPConfig         *ACPConfig            `json:"acpConfig"`
	Config            map[string]any        `json:"config"`
	SandboxConfig     map[string]any        `json:"workspaceConfig"`
	ToolPolicies      map[string]ToolPolicy `json:"toolPolicies,omitempty"`
	DefaultToolPolicy *ToolPolicyDefault    `json:"defaultToolPolicy,omitempty"`
	UIConfig          json.RawMessage       `json:"uiConfig,omitempty"`
}

// --- Agent Run Message / Tool Call DTOs ---

// AgentRunMessageDTO is the response DTO for an agent run message
type AgentRunMessageDTO struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	Role       string         `json:"role"`
	Content    map[string]any `json:"content"`
	StepNumber int            `json:"stepNumber"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// AgentRunToolCallDTO is the response DTO for an agent run tool call
type AgentRunToolCallDTO struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	MessageID  *string        `json:"messageId,omitempty"`
	ToolName   string         `json:"toolName"`
	Input      map[string]any `json:"input"`
	Output     map[string]any `json:"output"`
	Status     string         `json:"status"`
	DurationMs *int           `json:"durationMs,omitempty"`
	StepNumber int            `json:"stepNumber"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// AgentRunStepDTO represents a single LLM invocation step within a run,
// grouping the assistant message with any tool calls made during that step.
type AgentRunStepDTO struct {
	StepNumber int                    `json:"stepNumber"`
	Messages   []*AgentRunMessageDTO  `json:"messages"`
	ToolCalls  []*AgentRunToolCallDTO `json:"toolCalls"`
}

// --- ToDTO methods ---

// ToDTO converts an AgentDefinition entity to AgentDefinitionDTO. It computes
// ToolGroups with agent-referenced-tools-only membership (no catalog); callers
// that hold the tool catalog should call ToolGroupsWithCatalog and assign the
// result to ToolGroups.
func (d *AgentDefinition) ToDTO() *AgentDefinitionDTO {
	return &AgentDefinitionDTO{
		ID:                d.ID,
		ProductID:         d.ProductID,
		ProjectID:         d.ProjectID,
		Name:              d.Name,
		Description:       d.Description,
		SystemPrompt:      d.SystemPrompt,
		Model:             d.Model,
		Tools:             d.Tools,
		BannedTools:       d.BannedTools,
		Skills:            d.Skills,
		AutoLoadSkills:    d.AutoLoadSkills,
		FlowType:          d.FlowType,
		IsDefault:         d.IsDefault,
		Enabled:           d.Enabled,
		MaxSteps:          d.MaxSteps,
		DefaultTimeout:    d.DefaultTimeout,
		Visibility:        d.Visibility,
		DispatchMode:      d.DispatchMode,
		ACPConfig:         d.ACPConfig,
		Config:            d.Config,
		SandboxConfig:     d.SandboxConfig,
		ToolPolicies:      d.ToolPolicies,
		DefaultToolPolicy: d.DefaultToolPolicy,
		UIConfig:          d.UIConfig,
		CreatedAt:         d.CreatedAt,
		UpdatedAt:         d.UpdatedAt,
		ToolGroups:        d.ToolGroupsWithCatalog(nil),
	}
}

// workspaceToolNames are the agent-facing workspace tool names (LLM/MCP names,
// not sandbox config keys). They are injected at execution time from
// SandboxConfig and live outside both the MCP catalog and def.Tools, so
// ToolGroupsWithCatalog adds them explicitly when the workspace is enabled.
var workspaceToolNames = []string{
	"workspace_bash", "workspace_read", "workspace_write", "workspace_edit",
	"workspace_glob", "workspace_grep", "workspace_git", "workspace_ast_grep",
	"run_python", "run_go",
}

// ToolGroupsWithCatalog computes the group catalog for the definition. Each
// group's `tools` is its full membership — the union of the group's catalog
// tools, the agent's own Tools ∪ BannedTools, and (when the workspace is
// enabled) the workspace tool names — so a group the agent has fully switched
// off still renders (with enabled=false) and can be re-enabled. Groups with
// zero members are omitted. A nil catalog degrades to agent-referenced-tools-only
// membership (never dropping a tool present in def.Tools or def.BannedTools).
func (d *AgentDefinition) ToolGroupsWithCatalog(catalog []mcp.ToolDefinition) []ToolGroupDTO {
	banned := make(map[string]bool, len(d.BannedTools))
	for _, t := range d.BannedTools {
		banned[t] = true
	}
	enabled := make(map[string]bool, len(d.Tools))
	for _, t := range d.Tools {
		enabled[t] = true
	}

	membership := make(map[string][]string)
	seen := make(map[string]bool)

	// 1. Catalog tools (deterministic catalog order) — full group membership for
	//    built-in and dynamic tools, resolved from the catalog's RequiredScope.
	for _, td := range catalog {
		if td.Name == "" || seen[td.Name] {
			continue
		}
		g := toolgroups.GroupForScope(td.RequiredScope, td.Name)
		membership[g] = append(membership[g], td.Name)
		seen[td.Name] = true
	}

	// 2. Agent-referenced tools (Tools then BannedTools) not already covered by
	//    the catalog — external/relay names, or anything the catalog missed. A
	//    tool the agent references must never be dropped.
	for _, t := range append(append([]string{}, d.Tools...), d.BannedTools...) {
		if t == "" || seen[t] {
			continue
		}
		g := toolgroups.GroupForTool(t)
		membership[g] = append(membership[g], t)
		seen[t] = true
	}

	// 3. Workspace tools: present when the definition has a non-empty
	//    SandboxConfig (workspace enabled). They are not MCP catalog tools and
	//    are not stored in def.Tools, so without this step the workspace groups
	//    never render and cannot be governed.
	if len(d.SandboxConfig) > 0 {
		for _, t := range workspaceToolNames {
			if seen[t] {
				continue
			}
			g := toolgroups.GroupForTool(t)
			membership[g] = append(membership[g], t)
			seen[t] = true
		}
	}

	var out []ToolGroupDTO
	for _, g := range toolgroups.Groups {
		tools := membership[g.ID]
		if len(tools) == 0 {
			continue
		}
		// The `other` group is display-only: it never reads a stored
		// @group:other entry, so external/relay/unmatched tools always inherit
		// the default policy.
		policy := ""
		if g.ID != toolgroups.GroupOther {
			p, present := d.ToolPolicies[toolGroupPolicyPrefix+g.ID]
			policy = groupPolicyString(p, present)
		}
		out = append(out, ToolGroupDTO{
			ID:          g.ID,
			Label:       g.Label,
			Description: g.Description,
			Policy:      policy,
			Enabled:     groupEnabled(tools, enabled, banned),
			Tools:       tools,
		})
	}
	return out
}

// groupEnabled reports whether at least one member tool is in Tools and not in
// BannedTools.
func groupEnabled(tools []string, enabled, banned map[string]bool) bool {
	for _, t := range tools {
		if enabled[t] && !banned[t] {
			return true
		}
	}
	return false
}

// groupPolicyString maps a stored group policy to the frozen DTO policy value:
// "deny" when Disabled, "ask" when Confirm, "allow" when stored-but-neither,
// and "" when no group entry exists (inherit the default).
func groupPolicyString(p ToolPolicy, present bool) string {
	if !present {
		return ""
	}
	switch {
	case p.Disabled:
		return "deny"
	case p.Confirm:
		return "ask"
	default:
		return "allow"
	}
}

// ToSummaryDTO converts an AgentDefinition entity to AgentDefinitionSummaryDTO
func (d *AgentDefinition) ToSummaryDTO() *AgentDefinitionSummaryDTO {
	return &AgentDefinitionSummaryDTO{
		ID:               d.ID,
		ProjectID:        d.ProjectID,
		Name:             d.Name,
		Description:      d.Description,
		FlowType:         d.FlowType,
		Visibility:       d.Visibility,
		IsDefault:        d.IsDefault,
		Enabled:          d.Enabled,
		ToolCount:        len(d.Tools),
		Skills:           d.Skills,
		HasSandboxConfig: len(d.SandboxConfig) > 0,
		UIConfig:         d.UIConfig,
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.UpdatedAt,
	}
}

// ToDTO converts an AgentRunMessage entity to AgentRunMessageDTO
func (m *AgentRunMessage) ToDTO() *AgentRunMessageDTO {
	return &AgentRunMessageDTO{
		ID:         m.ID,
		RunID:      m.RunID,
		Role:       m.Role,
		Content:    m.Content,
		StepNumber: m.StepNumber,
		CreatedAt:  m.CreatedAt,
	}
}

// ToDTO converts an AgentRunToolCall entity to AgentRunToolCallDTO
func (tc *AgentRunToolCall) ToDTO() *AgentRunToolCallDTO {
	return &AgentRunToolCallDTO{
		ID:         tc.ID,
		RunID:      tc.RunID,
		MessageID:  tc.MessageID,
		ToolName:   tc.ToolName,
		Input:      tc.Input,
		Output:     tc.Output,
		Status:     tc.Status,
		DurationMs: tc.DurationMs,
		StepNumber: tc.StepNumber,
		CreatedAt:  tc.CreatedAt,
	}
}

// --- Webhook Hooks DTOs ---

type AgentWebhookHookDTO struct {
	ID              string           `json:"id"`
	AgentID         string           `json:"agentId"`
	ProjectID       string           `json:"projectId"`
	Label           string           `json:"label"`
	Enabled         bool             `json:"enabled"`
	AllowInternal   bool             `json:"allowInternal,omitempty"`
	RateLimitConfig *RateLimitConfig `json:"rateLimitConfig"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
	Token           *string          `json:"token,omitempty"` // Only present on creation
}

func (h *AgentWebhookHook) ToDTO() *AgentWebhookHookDTO {
	return &AgentWebhookHookDTO{
		ID:              h.ID,
		AgentID:         h.AgentID,
		ProjectID:       h.ProjectID,
		Label:           h.Label,
		Enabled:         h.Enabled,
		AllowInternal:   h.AllowInternal,
		RateLimitConfig: h.RateLimitConfig,
		CreatedAt:       h.CreatedAt,
		UpdatedAt:       h.UpdatedAt,
		Token:           h.Token,
	}
}

type CreateAgentWebhookHookDTO struct {
	Label           string           `json:"label" validate:"required"`
	RateLimitConfig *RateLimitConfig `json:"rateLimitConfig"`
	// AllowInternal opts in to binding the hook to an internal-visibility agent.
	// The default is false (fail-closed): the webhook receiver is a public
	// surface authenticated only by a shared per-hook bearer token, so binding
	// an internal agent there would let a third party holding that secret invoke
	// an agent meant to be reachable only from within the platform.
	AllowInternal bool `json:"allowInternal,omitempty"`
}

type WebhookTriggerPayloadDTO struct {
	Prompt   string         `json:"prompt"`
	Context  map[string]any `json:"context"`
	MaxSteps *int           `json:"maxSteps,omitempty"`
}

// --- Agent Question DTOs ---

// AgentQuestionDTO is the API response format for an agent question.
type AgentQuestionDTO struct {
	ID        string `json:"id"`
	RunID     string `json:"runId"`
	AgentID   string `json:"agentId"`
	ProjectID string `json:"projectId"`
	Question  string `json:"question"`
	// Proposal is the optional structured proposal envelope {kind, summary, body}
	// attached to the question. Nil/omitted for plain-text questions.
	Proposal        map[string]any               `json:"proposal,omitempty"`
	Options         []AgentQuestionOption        `json:"options"`
	InteractionType AgentQuestionInteractionType `json:"interactionType"`
	Placeholder     string                       `json:"placeholder,omitempty"`
	MaxLength       int                          `json:"maxLength,omitempty"`
	Response        *string                      `json:"response,omitempty"`
	RespondedBy     *string                      `json:"respondedBy,omitempty"`
	RespondedAt     *time.Time                   `json:"respondedAt,omitempty"`
	Status          AgentQuestionStatus          `json:"status"`
	NotificationID  *string                      `json:"notificationId,omitempty"`
	ResumeRunID     *string                      `json:"resumeRunId,omitempty"` // new run ID created on resume
	CreatedAt       time.Time                    `json:"createdAt"`
	UpdatedAt       time.Time                    `json:"updatedAt"`
}

// ToDTO converts an AgentQuestion entity to a DTO.
func (q *AgentQuestion) ToDTO() *AgentQuestionDTO {
	return &AgentQuestionDTO{
		ID:              q.ID,
		RunID:           q.RunID,
		AgentID:         q.AgentID,
		ProjectID:       q.ProjectID,
		Question:        q.Question,
		Proposal:        q.Proposal,
		Options:         q.Options,
		InteractionType: q.InteractionType,
		Placeholder:     q.Placeholder,
		MaxLength:       q.MaxLength,
		Response:        q.Response,
		RespondedBy:     q.RespondedBy,
		RespondedAt:     q.RespondedAt,
		Status:          q.Status,
		NotificationID:  q.NotificationID,
		CreatedAt:       q.CreatedAt,
		UpdatedAt:       q.UpdatedAt,
	}
}

// AgentToolApprovalDTO is the API response shape for a tool-approval audit
// record.
type AgentToolApprovalDTO struct {
	ID             string         `json:"id"`
	RunID          string         `json:"runId"`
	AgentID        string         `json:"agentId"`
	ProjectID      string         `json:"projectId"`
	QuestionID     string         `json:"questionId"`
	ToolName       string         `json:"toolName"`
	ArgsSummary    map[string]any `json:"argsSummary"`
	Decision       string         `json:"decision"`
	Message        string         `json:"message,omitempty"`
	DecidedBy      *string        `json:"decidedBy,omitempty"`
	DecidedAt      *time.Time     `json:"decidedAt,omitempty"`
	ConversationID *string        `json:"conversationId,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// ToDTO converts an AgentToolApproval entity to a DTO.
func (a *AgentToolApproval) ToDTO() *AgentToolApprovalDTO {
	return &AgentToolApprovalDTO{
		ID:             a.ID,
		RunID:          a.RunID,
		AgentID:        a.AgentID,
		ProjectID:      a.ProjectID,
		QuestionID:     a.QuestionID,
		ToolName:       a.ToolName,
		ArgsSummary:    a.ArgsSummary,
		Decision:       a.Decision,
		Message:        a.Message,
		DecidedBy:      a.DecidedBy,
		DecidedAt:      a.DecidedAt,
		ConversationID: a.ConversationID,
		CreatedAt:      a.CreatedAt,
	}
}

// RespondToQuestionRequest is the request body for responding to an agent question.
type RespondToQuestionRequest struct {
	Response string `json:"response" validate:"required"`
	// Message is an optional human-written reason attached to a tool-policy
	// rejection. It is surfaced to the agent in the rejected result so it can
	// adapt instead of silently dropping the direction.
	Message string `json:"message,omitempty"`
}

// ADKEventDTO represents an event within an ADK session.
type ADKEventDTO struct {
	ID                     string         `json:"id"`
	SessionID              string         `json:"sessionId"`
	InvocationID           string         `json:"invocationId,omitempty"`
	Author                 string         `json:"author,omitempty"`
	Timestamp              time.Time      `json:"timestamp"`
	Branch                 *string        `json:"branch,omitempty"`
	Actions                map[string]any `json:"actions,omitempty"`
	LongRunningToolIDsJSON map[string]any `json:"longRunningToolIds,omitempty"`
	Content                map[string]any `json:"content,omitempty"`
	GroundingMetadata      map[string]any `json:"groundingMetadata,omitempty"`
	CustomMetadata         map[string]any `json:"customMetadata,omitempty"`
	UsageMetadata          map[string]any `json:"usageMetadata,omitempty"`
	CitationMetadata       map[string]any `json:"citationMetadata,omitempty"`
	Partial                *bool          `json:"partial,omitempty"`
	TurnComplete           *bool          `json:"turnComplete,omitempty"`
	ErrorCode              *string        `json:"errorCode,omitempty"`
	ErrorMessage           *string        `json:"errorMessage,omitempty"`
	Interrupted            *bool          `json:"interrupted,omitempty"`
}

// --- Issue #192: Full run trace DTO ---

// AgentRunFullDTO is the response for GET /agent-runs/:runId/full.
// It bundles the run, messages, tool calls, and parent run in a single response.
type AgentRunFullDTO struct {
	Run       *AgentRunDTO           `json:"run"`
	Messages  []*AgentRunMessageDTO  `json:"messages"`
	ToolCalls []*AgentRunToolCallDTO `json:"toolCalls"`
	ParentRun *AgentRunDTO           `json:"parentRun,omitempty"`
}

// --- Issue #193: Run stats DTOs ---

// RunStatsDTO is the response for GET /agent-runs/stats.
type RunStatsDTO struct {
	Period     RunStatsPeriodDTO           `json:"period"`
	Overview   RunStatsOverviewDTO         `json:"overview"`
	ByAgent    map[string]RunStatsAgentDTO `json:"byAgent"`
	TopErrors  []RunStatsErrorDTO          `json:"topErrors"`
	ToolStats  RunStatsToolsDTO            `json:"toolStats"`
	TimeSeries RunStatsTimeSeriesDTO       `json:"timeSeries"`
}

// RunStatsPeriodDTO captures the analysis window.
type RunStatsPeriodDTO struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
}

// RunStatsOverviewDTO holds aggregate run counts and cost.
type RunStatsOverviewDTO struct {
	TotalRuns     int64   `json:"totalRuns"`
	SuccessCount  int64   `json:"successCount"`
	FailedCount   int64   `json:"failedCount"`
	ErrorCount    int64   `json:"errorCount"`
	SuccessRate   float64 `json:"successRate"`
	AvgDurationMs float64 `json:"avgDurationMs"`
	TotalCostUSD  float64 `json:"totalCostUsd"`
}

// RunStatsAgentDTO holds per-agent aggregates.
type RunStatsAgentDTO struct {
	Total           int64   `json:"total"`
	Success         int64   `json:"success"`
	Failed          int64   `json:"failed"`
	Errored         int64   `json:"errored"`
	AvgDurationMs   float64 `json:"avgDurationMs"`
	MaxDurationMs   int64   `json:"maxDurationMs"`
	AvgCostUSD      float64 `json:"avgCostUsd"`
	TotalCostUSD    float64 `json:"totalCostUsd"`
	AvgInputTokens  float64 `json:"avgInputTokens"`
	AvgOutputTokens float64 `json:"avgOutputTokens"`
}

// RunStatsErrorDTO is an entry in the topErrors list.
type RunStatsErrorDTO struct {
	Message string `json:"message"`
	Count   int64  `json:"count"`
}

// RunStatsToolsDTO holds aggregate tool call statistics.
type RunStatsToolsDTO struct {
	TotalToolCalls int64                      `json:"totalToolCalls"`
	ByTool         map[string]RunStatsToolDTO `json:"byTool"`
}

// RunStatsToolDTO is per-tool aggregated metrics.
type RunStatsToolDTO struct {
	Total         int64   `json:"total"`
	Success       int64   `json:"success"`
	Failed        int64   `json:"failed"`
	AvgDurationMs float64 `json:"avgDurationMs"`
	MaxDurationMs int64   `json:"maxDurationMs"`
}

// RunStatsTimeSeriesDTO holds time-bucketed run counts.
type RunStatsTimeSeriesDTO struct {
	ByHour []RunStatsTimePointDTO `json:"byHour"`
}

// RunStatsTimePointDTO is a single time bucket with per-agent counts.
type RunStatsTimePointDTO struct {
	Hour    time.Time        `json:"hour"`
	Runs    int64            `json:"runs"`
	ByAgent map[string]int64 `json:"byAgent,omitempty"`
}

// --- Issue #194: Session analytics DTOs ---

// RunSessionStatsDTO is the response for GET /agent-runs/stats/sessions.
type RunSessionStatsDTO struct {
	Period             RunStatsPeriodDTO      `json:"period"`
	TotalSessions      int64                  `json:"totalSessions"`
	ActiveSessions     int64                  `json:"activeSessions"`
	AvgRunsPerSession  float64                `json:"avgRunsPerSession"`
	MaxRunsPerSession  int64                  `json:"maxRunsPerSession"`
	SessionsByPlatform map[string]int64       `json:"sessionsByPlatform"`
	TopSessions        []RunSessionSummaryDTO `json:"topSessions"`
}

// RunSessionSummaryDTO summarises a single logical session.
type RunSessionSummaryDTO struct {
	Platform      string    `json:"platform"`
	ChannelID     string    `json:"channelId,omitempty"`
	ThreadID      string    `json:"threadId,omitempty"`
	TotalRuns     int64     `json:"totalRuns"`
	LastRunAt     time.Time `json:"lastRunAt"`
	AvgDurationMs float64   `json:"avgDurationMs"`
	TotalCostUSD  float64   `json:"totalCostUsd"`
}

// ADKSessionDTO represents an ADK session.
type ADKSessionDTO struct {
	ID         string         `json:"id"`
	AppName    string         `json:"appName"`
	UserID     string         `json:"userId"`
	State      map[string]any `json:"state,omitempty"`
	CreateTime time.Time      `json:"createTime"`
	UpdateTime time.Time      `json:"updateTime"`
	Events     []*ADKEventDTO `json:"events,omitempty"`
}
