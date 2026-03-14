package agents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/pkg/adk/session/bunsession"
	"github.com/uptrace/bun"
)

// Repository handles database operations for agents
type Repository struct {
	db bun.IDB
}

// NewRepository creates a new agents repository
func NewRepository(db bun.IDB) *Repository {
	return &Repository{db: db}
}

// FindAll returns all agents for a project
func (r *Repository) FindAll(ctx context.Context, projectID string) ([]*Agent, error) {
	var agents []*Agent
	err := r.db.NewSelect().
		Model(&agents).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return agents, nil
}

// FindByID returns an agent by ID, optionally filtering by project
func (r *Repository) FindByID(ctx context.Context, id string, projectID *string) (*Agent, error) {
	agent := new(Agent)
	q := r.db.NewSelect().
		Model(agent).
		Where("id = ?", id)

	if projectID != nil {
		q = q.Where("project_id = ?", *projectID)
	}

	err := q.Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return agent, nil
}

// FindEnabled returns all enabled agents
func (r *Repository) FindEnabled(ctx context.Context) ([]*Agent, error) {
	var agents []*Agent
	err := r.db.NewSelect().
		Model(&agents).
		Where("enabled = true").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return agents, nil
}

// FindByStrategyType returns an agent by strategy type
func (r *Repository) FindByStrategyType(ctx context.Context, strategyType string) (*Agent, error) {
	agent := new(Agent)
	err := r.db.NewSelect().
		Model(agent).
		Where("strategy_type = ?", strategyType).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return agent, nil
}

// Create creates a new agent
func (r *Repository) Create(ctx context.Context, agent *Agent) error {
	_, err := r.db.NewInsert().
		Model(agent).
		Returning("*").
		Exec(ctx)
	return err
}

// Update updates an agent
func (r *Repository) Update(ctx context.Context, agent *Agent) error {
	agent.UpdatedAt = time.Now()
	_, err := r.db.NewUpdate().
		Model(agent).
		WherePK().
		Returning("*").
		Exec(ctx)
	return err
}

// Delete deletes an agent and all its runs
func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().
		Model((*Agent)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// UpdateLastRun updates the last run status of an agent
func (r *Repository) UpdateLastRun(ctx context.Context, id string, status string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*Agent)(nil)).
		Set("last_run_at = ?", now).
		Set("last_run_status = ?", status).
		Set("updated_at = ?", now).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// --- Agent Runs ---

// CreateRun creates a new agent run record
func (r *Repository) CreateRun(ctx context.Context, agentID string) (*AgentRun, error) {
	run := &AgentRun{
		AgentID:   agentID,
		Status:    RunStatusRunning,
		StartedAt: time.Now(),
		Summary:   make(map[string]any),
	}
	_, err := r.db.NewInsert().
		Model(run).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return run, nil
}

// CompleteRun marks a run as successful
func (r *Repository) CompleteRun(ctx context.Context, runID string, summary map[string]any) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusSuccess).
		Set("completed_at = ?", now).
		Set("summary = ?", summary).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// SkipRun marks a run as skipped
func (r *Repository) SkipRun(ctx context.Context, runID string, reason string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusSkipped).
		Set("completed_at = ?", now).
		Set("skip_reason = ?", reason).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// FailRun marks a run as failed
func (r *Repository) FailRun(ctx context.Context, runID string, errorMessage string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusError).
		Set("completed_at = ?", now).
		Set("error_message = ?", errorMessage).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// MarkOrphanedRunsAsError finds all runs stuck in "running" status and marks
// them as errored. This is called on server startup to recover from unclean
// shutdowns where the agent goroutine was killed mid-execution.
func (r *Repository) MarkOrphanedRunsAsError(ctx context.Context) (int, error) {
	now := time.Now()
	res, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusError).
		Set("completed_at = ?", now).
		Set("error_message = ?", "server restarted while run was in progress").
		Where("status = ?", RunStatusRunning).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
func (r *Repository) GetRecentRuns(ctx context.Context, agentID string, limit int) ([]*AgentRun, error) {
	if limit <= 0 {
		limit = 10
	}
	var runs []*AgentRun
	err := r.db.NewSelect().
		Model(&runs).
		Where("agent_id = ?", agentID).
		Order("started_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return runs, nil
}

// --- Agent Processing Log ---

// CreateProcessingLog creates a new processing log entry
func (r *Repository) CreateProcessingLog(ctx context.Context, log *AgentProcessingLog) error {
	_, err := r.db.NewInsert().
		Model(log).
		Returning("*").
		Exec(ctx)
	return err
}

// FindPendingOrProcessing finds an existing pending/processing entry
func (r *Repository) FindPendingOrProcessing(ctx context.Context, agentID, objectID string, version int, eventType ReactionEventType) (*AgentProcessingLog, error) {
	log := new(AgentProcessingLog)
	err := r.db.NewSelect().
		Model(log).
		Where("agent_id = ?", agentID).
		Where("graph_object_id = ?", objectID).
		Where("object_version = ?", version).
		Where("event_type = ?", eventType).
		Where("status IN (?)", bun.In([]AgentProcessingStatus{ProcessingStatusPending, ProcessingStatusProcessing})).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return log, nil
}

// MarkProcessingLogStatus updates the status of a processing log entry
func (r *Repository) MarkProcessingLogStatus(ctx context.Context, id string, status AgentProcessingStatus, errorMsg *string, summary map[string]any) error {
	now := time.Now()
	q := r.db.NewUpdate().
		Model((*AgentProcessingLog)(nil)).
		Set("status = ?", status).
		Where("id = ?", id)

	if status == ProcessingStatusProcessing {
		q = q.Set("started_at = ?", now)
	}
	if status == ProcessingStatusCompleted || status == ProcessingStatusFailed || status == ProcessingStatusSkipped || status == ProcessingStatusAbandoned {
		q = q.Set("completed_at = ?", now)
	}
	if errorMsg != nil {
		q = q.Set("error_message = ?", *errorMsg)
	}
	if summary != nil {
		q = q.Set("result_summary = ?", summary)
	}

	_, err := q.Exec(ctx)
	return err
}

// GetPendingEvents returns graph objects that haven't been processed by an agent
// This is used to show unprocessed objects in the admin UI
func (r *Repository) GetPendingEvents(ctx context.Context, agent *Agent, limit int) ([]PendingEventObjectDTO, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	// Build the query to find objects matching the agent's filters
	// that haven't been successfully processed by this agent
	type GraphObject struct {
		ID        string    `bun:"id"`
		Type      string    `bun:"type"`
		Key       string    `bun:"key"`
		Version   int       `bun:"version"`
		CreatedAt time.Time `bun:"created_at"`
		UpdatedAt time.Time `bun:"updated_at"`
	}

	var objects []GraphObject
	q := r.db.NewSelect().
		TableExpr("kb.graph_objects AS go").
		Column("go.id", "go.type", "go.key", "go.version", "go.created_at", "go.updated_at").
		Where("go.project_id = ?", agent.ProjectID).
		Where("go.deleted_at IS NULL")

	// Filter by object types if specified
	if agent.ReactionConfig != nil && len(agent.ReactionConfig.ObjectTypes) > 0 {
		q = q.Where("go.type IN (?)", bun.In(agent.ReactionConfig.ObjectTypes))
	}

	// Exclude objects that have been successfully processed
	q = q.Where(`NOT EXISTS (
		SELECT 1 FROM kb.agent_processing_log apl
		WHERE apl.agent_id = ?
		AND apl.graph_object_id = go.id
		AND apl.object_version = go.version
		AND apl.status = 'completed'
	)`, agent.ID)

	// Get total count
	totalCount, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Get limited results
	err = q.Order("go.updated_at DESC").
		Limit(limit).
		Scan(ctx, &objects)
	if err != nil {
		return nil, 0, err
	}

	// Convert to DTOs
	dtos := make([]PendingEventObjectDTO, len(objects))
	for i, obj := range objects {
		dtos[i] = PendingEventObjectDTO{
			ID:        obj.ID,
			Type:      obj.Type,
			Key:       obj.Key,
			Version:   obj.Version,
			CreatedAt: obj.CreatedAt,
			UpdatedAt: obj.UpdatedAt,
		}
	}

	return dtos, totalCount, nil
}

// IsAgentProcessingObject checks if an agent is currently processing an object
func (r *Repository) IsAgentProcessingObject(ctx context.Context, agentID, objectID string) (bool, error) {
	count, err := r.db.NewSelect().
		Model((*AgentProcessingLog)(nil)).
		Where("agent_id = ?", agentID).
		Where("graph_object_id = ?", objectID).
		Where("status = ?", ProcessingStatusProcessing).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// MarkStuckJobsAsAbandoned marks jobs stuck in processing state as abandoned
func (r *Repository) MarkStuckJobsAsAbandoned(ctx context.Context, olderThan time.Duration) (int, error) {
	threshold := time.Now().Add(-olderThan)
	res, err := r.db.NewUpdate().
		Model((*AgentProcessingLog)(nil)).
		Set("status = ?", ProcessingStatusAbandoned).
		Set("completed_at = ?", time.Now()).
		Set("error_message = ?", "Job abandoned - exceeded processing time limit").
		Where("status = ?", ProcessingStatusProcessing).
		Where("started_at < ?", threshold).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// --- Agent lookup by name ---

// FindByName finds an agent by name within a project.
func (r *Repository) FindByName(ctx context.Context, projectID, name string) (*Agent, error) {
	agent := new(Agent)
	err := r.db.NewSelect().
		Model(agent).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return agent, nil
}

// --- Agent Definitions ---

// FindAllDefinitions returns all agent definitions for a project.
// If includeInternal is false, definitions with visibility='internal' are excluded.
func (r *Repository) FindAllDefinitions(ctx context.Context, projectID string, includeInternal bool) ([]*AgentDefinition, error) {
	var defs []*AgentDefinition
	q := r.db.NewSelect().
		Model(&defs).
		Where("project_id = ?", projectID).
		Order("name ASC")
	if !includeInternal {
		q = q.Where("visibility != ?", VisibilityInternal)
	}
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return defs, nil
}

// FindDefinitionByID returns an agent definition by ID, optionally filtering by project.
func (r *Repository) FindDefinitionByID(ctx context.Context, id string, projectID *string) (*AgentDefinition, error) {
	def := new(AgentDefinition)
	q := r.db.NewSelect().
		Model(def).
		Where("id = ?", id)
	if projectID != nil {
		q = q.Where("project_id = ?", *projectID)
	}
	err := q.Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return def, nil
}

// FindDefinitionByName returns an agent definition by name within a project.
func (r *Repository) FindDefinitionByName(ctx context.Context, projectID, name string) (*AgentDefinition, error) {
	def := new(AgentDefinition)
	err := r.db.NewSelect().
		Model(def).
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return def, nil
}

// FindEnabledByTriggerType returns all enabled agents matching the given trigger type.
func (r *Repository) FindEnabledByTriggerType(ctx context.Context, triggerType AgentTriggerType) ([]*Agent, error) {
	var agents []*Agent
	err := r.db.NewSelect().
		Model(&agents).
		Where("enabled = true").
		Where("trigger_type = ?", triggerType).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return agents, nil
}

// CreateDefinition creates a new agent definition.
func (r *Repository) CreateDefinition(ctx context.Context, def *AgentDefinition) error {
	_, err := r.db.NewInsert().
		Model(def).
		Returning("*").
		Exec(ctx)
	return err
}

// graphQueryAgentSystemPrompt is the default system prompt for the graph-query-agent.
const graphQueryAgentSystemPrompt = `You are a knowledge graph query assistant. Your role is to help users explore and understand the data in their knowledge graph.

## Rules
1. ALWAYS use the provided tools to look up data. Never answer from your training data or fabricate entities, relationships, or facts.
2. When you retrieve results, cite specific entity names, types, and relationship types in your response.
3. If a tool returns no results, clearly state that no matching data was found. Do not fabricate or hallucinate results.
4. For complex questions, chain multiple tool calls (e.g., search first, then traverse relationships).
5. Format responses using markdown for clarity. Use tables for structured data when appropriate.
6. Keep responses concise and factual. Focus on what the data shows.`

// EnsureGraphQueryAgent returns the graph-query-agent for the project, creating it if it
// does not exist yet. Uses VisibilityInternal so it never appears in the public list.
// Safe to call concurrently — a race between two callers results in one insert and one
// subsequent read (FindDefinitionByName will find the winner's row).
func (r *Repository) EnsureGraphQueryAgent(ctx context.Context, projectID string) (*AgentDefinition, error) {
	existing, err := r.FindDefinitionByName(ctx, projectID, "graph-query-agent")
	if err != nil {
		return nil, fmt.Errorf("failed to look up graph-query-agent: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	temperature := float32(0.1)
	maxSteps := 15
	systemPrompt := graphQueryAgentSystemPrompt

	def := &AgentDefinition{
		ProjectID:    projectID,
		Name:         "graph-query-agent",
		Description:  strPtr("Knowledge graph query assistant with access to search, entity, and relationship tools"),
		SystemPrompt: &systemPrompt,
		Model: &ModelConfig{
			Name:        "gemini-3.1-flash-lite-preview",
			Temperature: &temperature,
		},
		Tools: []string{
			"project-get",
			"search-hybrid",
			"query_entities",
			"search_entities",
			"semantic_search",
			"find_similar",
			"get_entity_edges",
			"traverse_graph",
			"list_entity_types",
			"schema_version",
			"list_relationships",
		},
		FlowType:   FlowTypeSingle,
		IsDefault:  true,
		MaxSteps:   &maxSteps,
		Visibility: VisibilityInternal,
		Config:     map[string]any{},
	}

	if err := r.CreateDefinition(ctx, def); err != nil {
		// Race condition: another caller inserted first — retry the read.
		if existing, err2 := r.FindDefinitionByName(ctx, projectID, "graph-query-agent"); err2 == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create graph-query-agent: %w", err)
	}

	return def, nil
}

// cliAssistantAgentSystemPrompt is the default system prompt for the cli-assistant-agent.
const cliAssistantAgentSystemPrompt = `You are a CLI assistant for the Memory knowledge management platform.
Your job is to help users accomplish tasks using Memory — the CLI, SDK, REST API, agents, and knowledge graph features.
You can answer questions AND take direct action: create, update, and delete entities, relationships, agents, schemas, MCP servers, and projects.

## Authentication & Context Awareness

You will be told whether the user is authenticated and whether a project context is active.
Always adapt your response to the current context:

- **Not authenticated**: You can still answer documentation questions. For tasks that require
  authentication (listing agents, querying graph data, etc.), explain what credentials are needed:
    - Run "memory login" for interactive OAuth login
    - Set MEMORY_API_KEY for standalone/CI use
    - Pass --project-token for project-scoped access
- **Authenticated, no project**: You can answer doc questions and perform account-level tasks.
  For project-scoped tasks, tell the user to pass --project <id> or run "memory config set project_id <id>".
- **Authenticated with project**: Full access — answer questions and perform tasks using the available tools.

## Classification

Before responding, classify the user's request into one of:
- **DOCS_QUESTION**: asking how something works, what commands exist, what a feature does, SDK/API usage
- **TASK**: asking you to do something — list agents, query graph, create/update/delete objects, etc.
- **MIXED**: a question that requires both live data (tools) and documentation context

**Tool constraints by classification — strictly enforced:**
- **DOCS_QUESTION**: use ONLY "web-fetch". Do NOT call any graph, agent, schema, document, or skill tools.
- **TASK**: use ONLY the relevant action/data tools. Do NOT call "web-fetch" unless the user explicitly asks for documentation.
- **MIXED**: fetch docs first, then use action tools as needed.

## For DOCS_QUESTION

Use the "web-fetch" tool to retrieve relevant documentation pages from:
  https://emergent-company.github.io/emergent.memory/latest/

Documentation sections and their URLs:

- **User Guide** (features, how-to, CLI usage):
  https://emergent-company.github.io/emergent.memory/latest/user-guide/
  Pages: getting-started, agents, knowledge-graph, documents, datasources, tasks, chat,
         branches, backups, api-tokens, integrations, notifications
  Example: https://emergent-company.github.io/emergent.memory/latest/user-guide/agents/

- **Developer Guide** (configuration, ops, advanced setup):
  https://emergent-company.github.io/emergent.memory/latest/developer-guide/
  Pages: provider-setup, mcp-servers, schema, schema-registry, sandbox, extraction,
         scheduler, security-scopes, health-ops, email-setup
  Example: https://emergent-company.github.io/emergent.memory/latest/developer-guide/provider-setup/

- **Go SDK**: https://emergent-company.github.io/emergent.memory/latest/go-sdk/
- **API Reference**: https://emergent-company.github.io/emergent.memory/latest/api-reference/

Navigation strategy:
1. Identify which section covers the topic (user-guide for features/CLI, developer-guide for config/ops)
2. Fetch the specific page directly using the URL pattern above
3. If unsure which page, fetch the section index first (e.g. .../latest/user-guide/)
4. **Never fetch the same URL more than once in a session.** If you already retrieved a page, use that content — do not re-fetch it.

Always provide CLI command examples in code blocks. Use the real command names (memory graph, memory agents, memory defs, etc.).

## Response format rules

- **Default to CLI**: always answer with CLI commands. Do NOT include REST API instructions unless the user explicitly asks about the API, SDK, or HTTP endpoints.
- **API only when asked**: if the user says "API", "REST", "HTTP", "curl", "endpoint", or "SDK", then include API/HTTP examples in addition to (or instead of) CLI commands.
- **No unsolicited curl examples**: never show curl or HTTP snippets in response to a plain question about how to do something.

## For TASK (when authenticated + project available)

Use the available tools to fulfill the request directly. Confirm what was done afterward.
Never fabricate live state — always use tools to look up agents, objects, IDs, etc.

### Write action guardrails

Before executing any write tool (create, update, delete), briefly describe what you are about to do:
  "I will create an agent definition named 'summarizer' with model gemini-2.0-flash."

Before executing any **delete** operation, explicitly warn the user:
  "Warning: this will permanently delete <resource name/id>. Proceeding."

Do not ask for confirmation — state the intent and proceed immediately.

## For TASK (when authenticated, but no project context)

You ARE authenticated — use global tools freely:
- **create_project** — the user can ask "create a project called X"
- **list_traces**, **get_trace** — account-level trace access
- **list_agent_questions**, **respond_to_agent_question** — account-level agent questions
- **webfetch** — documentation questions always work

Do NOT attempt project-scoped tools (graph queries, agent management, schema, MCP, documents, skills, etc.).
For those, tell the user to pass --project <id> or run "memory config set project_id <id>".

## For TASK (when NOT authenticated)

Do NOT attempt any tool calls.
Instead, explain clearly:
1. What would have been done
2. What the user needs to do to authenticate:
   - Run "memory login" for interactive OAuth login
   - Set MEMORY_API_KEY for standalone/CI use
   - Pass --project-token for project-scoped access
3. The exact CLI commands to get set up

## CLI Knowledge

### Quick reference (all top-level commands)

Knowledge Base:
  memory blueprints <source>         Apply packs/agents/skills/seed from a dir or GitHub URL
  memory documents list|get|upload|delete
  memory embeddings status|pause|resume|config
  memory graph objects create|create-batch|list|get|update|delete|edges
  memory graph relationships create|create-batch|list|get|delete
  memory query "<question>"          Natural-language query (agent or --mode=search)
  memory schemas list|installed|install|uninstall|get|create|delete|compiled-types
  memory browse                      Interactive TUI

Agents & AI:
  memory adk-sessions list|get
  memory agent-definitions create|list|get|update|delete    (aliases: agent-defs, defs)
  memory agents create|list|get|update|delete|trigger
  memory agents runs <agent-id> [--limit N]   -- lists recent runs with status, token usage, and cost per run
  memory agents get-run <run-id>              -- full detail for one run: tokens in/out + estimated cost in USD
  memory agents hooks|questions
  memory agents mcp-servers create|list|get|update|delete|inspect|sync|tools|configure
  memory ask "<question>"            Ask the CLI assistant
  memory mcp-guide                   Show MCP config for AI agents
  memory provider configure <google|google-vertex>
  memory provider configure-project <google|google-vertex> [--remove]
  memory provider models [provider] [--type embedding|generative]
  memory provider test [provider]
  memory provider usage [--project <id>] [--since YYYY-MM-DD]
  memory skills create|list|get|update|delete|import

### IMPORTANT: Relocated commands

The following commands moved to a new location in a recent version. Do NOT suggest the old paths:

| Old (no longer valid)        | New (correct)                              |
|------------------------------|--------------------------------------------|
| memory mcp-servers ...       | memory agents mcp-servers ...              |

Examples:
  memory agents mcp-servers list --project <id>
  memory agents mcp-servers configure brave_web_search api_key=YOUR_KEY --project <id>
  memory agents mcp-servers create --name my-server --url http://... --project <id>

Account & Access:
  memory config set|set-server|set-credentials|show
  memory login / memory logout
  memory projects create|list|get|set|delete|create-token|set-info|set-provider
  memory set-token                   Save a static Bearer token
  memory status                      Show auth status
  memory tokens create|list|get|revoke

Server (self-hosted):
  memory server install [--port 3002] [--google-api-key KEY]
  memory server upgrade [--force]
  memory server ctl start|stop|restart|status|logs|health|shell|pull
  memory server doctor [--fix]
  memory server uninstall [--keep-data]

Other:
  memory traces list|get|search
  memory upgrade [--force]          Upgrade the CLI binary
  memory version

### Common flags (available on all commands)
  --server <url>         Override server URL
  --project <id>         Override active project
  --project-token <tok>  Project-scoped auth token
  --output table|json|yaml|csv
  --compact              Compact output layout
  --debug                Enable debug logging
  --no-color             Disable color

### Looking up full flag details

When you need the exact flags for a specific command (e.g. "memory agents create"), call:
  get_skill("memory-cli-reference")

The skill contains the complete auto-generated reference for every command and flag.
Always call it before guessing at flag names — do not hallucinate flags.

Use "skill-list" only when you need to find a specific skill by name or verify a skill exists.
Do NOT call "skill-list" for general orientation or when you already know the skill name.

## Platform Facts (authoritative — do not contradict these)

- **Supported LLM providers**: Google AI (Gemini API) and Google Cloud Vertex AI only.
  OpenAI, Anthropic, and other providers are NOT supported. Do not suggest them.
- **Supported models**: Gemini family (e.g. gemini-2.0-flash, gemini-2.5-flash, gemini-2.5-pro, gemini-3.1-flash-lite-preview).
  Refer to the developer-guide/provider-setup page for the current recommended model list.
- **Provider configuration**: set at the organization level via "memory provider configure" or the Admin UI.
  A project inherits the org provider unless overridden with "memory provider configure-project".

## Python Scripting (for bulk or complex tasks)

When a task involves many items or complex logic that would be tedious with individual tool calls
(e.g. "delete all projects with 'e2e' in the name"), write and run a Python script instead:

1. Use workspace_write to write a script to /workspace/task.py
2. Use workspace_bash to run it: python3 /workspace/task.py
3. The script can use the pre-installed emergent-memory SDK:

   from emergent import Client
   client = Client.from_env()
   # client.projects, client.documents, etc. are ready to use

The sandbox already has credentials injected — Client.from_env() picks them up automatically.

## Response Style
- Keep responses concise and focused
- Use markdown with code blocks for CLI commands and API examples
- For lists of items (agents, objects, etc.), use tables when there are multiple columns
- If a task has multiple steps, number them clearly`

// EnsureCliAssistantAgent returns the cli-assistant-agent for the project (or with an empty
// project ID for the user-level /api/ask endpoint), creating it if it does not exist yet.
// If it already exists, its Tools list, SystemPrompt, and SandboxConfig are updated to
// reflect any changes made to cliAssistantAgentSystemPrompt and the canonical tool whitelist.
// Uses VisibilityInternal so it never appears in the public agent list.
// Safe to call concurrently — a race between two callers results in one insert and one
// subsequent read (FindDefinitionByName will find the winner's row).
func (r *Repository) EnsureCliAssistantAgent(ctx context.Context, projectID string) (*AgentDefinition, error) {
	existing, err := r.FindDefinitionByName(ctx, projectID, "cli-assistant-agent")
	if err != nil {
		return nil, fmt.Errorf("failed to look up cli-assistant-agent: %w", err)
	}

	temperature := float32(0.3)
	maxSteps := 20
	systemPrompt := cliAssistantAgentSystemPrompt

	// Build the Python SDK sandbox config. The sandbox image has emergent-memory
	// pre-installed; ephemeral credentials are injected as env vars at runtime so
	// the agent can use Client.from_env() without any explicit credential handling.
	sandboxCfg := &sandbox.AgentSandboxConfig{
		Enabled:   true,
		BaseImage: "emergent-memory-python-sdk:latest",
		// Allow bash so the agent can execute python scripts; read/write/edit for
		// writing helper files when needed.
		Tools: []string{"bash", "read", "write", "edit"},
		RepoSource: &sandbox.RepoSourceConfig{
			Type: sandbox.RepoSourceNone,
		},
	}
	sandboxMap, sandboxMapErr := sandboxCfg.ToMap()
	if sandboxMapErr != nil {
		// Non-fatal: proceed without sandbox config rather than blocking ask calls.
		sandboxMap = nil
	}

	canonicalTools := []string{
		// Web access for documentation
		"web-fetch",
		// Project info
		"project-get",
		// Knowledge graph — read
		"search-hybrid",
		"query_entities",
		"search_entities",
		"semantic_search",
		"find_similar",
		"get_entity_edges",
		"traverse_graph",
		"list_entity_types",
		"schema_version",
		"list_relationships",
		// Knowledge graph — write
		"entity-create",
		"update_entity",
		"delete_entity",
		"create_relationship",
		"update_relationship",
		"delete_relationship",
		// Agent management — read
		"agent-def-list",
		"get_agent_definition",
		"agent-list",
		"get_agent",
		"list_agent_runs",
		"agent-run-get",
		"get_agent_run_tool_calls",
		"list_available_agents",
		// Agent definition — write
		"create_agent_definition",
		"update_agent_definition",
		"delete_agent_definition",
		// Runtime agent — write
		"create_agent",
		"update_agent",
		"delete_agent",
		"trigger_agent",
		// Schema registry — read
		"list_schemas",
		"get_schema",
		"get_available_templates",
		"get_installed_templates",
		// Schema registry — write
		"create_schema",
		"delete_schema",
		"schema-assign",
		"schema-assignment-update",
		// MCP registry — write
		"create_mcp_server",
		"update_mcp_server",
		"mcp-server-delete",
		"mcp-registry-install",
		"sync_mcp_server_tools",
		// Project — write
		"create_project",
		// Documents — read/write (non-destructive uploads allowed)
		"document-list",
		"get_document",
		"upload_document",
		"delete_document",
		// Skills — read/write
		"skill-list",
		"get_skill",
		"create_skill",
		"update_skill",
		"delete_skill",
		// Embeddings — read only (no pause/resume/config changes)
		"get_embedding_status",
		// Agent Questions and ADK sessions — read
		"agent-question-list",
		"list_project_agent_questions",
		"respond_to_agent_question",
		"adk-session-list",
		"get_adk_session",
		// Traces — read
		"trace-list",
		"get_trace",
		// Query knowledge
		"search-knowledge",
		// Provider usage — read-only cost reporting
		"get_provider_usage",
	}

	if existing != nil {
		// Update tools, system prompt, model, and sandbox config to pick up any changes.
		existing.Tools = canonicalTools
		existing.SystemPrompt = &systemPrompt
		if existing.Model == nil {
			existing.Model = &ModelConfig{}
		}
		existing.Model.Name = "gemini-3.1-flash-lite-preview"
		if sandboxMap != nil {
			existing.SandboxConfig = sandboxMap
		}
		if updateErr := r.UpdateDefinition(ctx, existing); updateErr != nil {
			// Non-fatal — return existing as-is rather than failing the ask call.
			return existing, nil
		}
		return existing, nil
	}

	def := &AgentDefinition{
		ProjectID:    projectID,
		Name:         "cli-assistant-agent",
		Description:  strPtr("CLI and platform assistant — answers documentation questions and executes tasks using available tools"),
		SystemPrompt: &systemPrompt,
		Model: &ModelConfig{
			Name:        "gemini-3.1-flash-lite-preview",
			Temperature: &temperature,
		},
		Tools:         canonicalTools,
		FlowType:      FlowTypeSingle,
		IsDefault:     false,
		MaxSteps:      &maxSteps,
		Visibility:    VisibilityInternal,
		Config:        map[string]any{},
		SandboxConfig: sandboxMap,
	}

	if err := r.CreateDefinition(ctx, def); err != nil {
		// Race condition: another caller inserted first — retry the read.
		if existing, err2 := r.FindDefinitionByName(ctx, projectID, "cli-assistant-agent"); err2 == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create cli-assistant-agent: %w", err)
	}

	return def, nil
}

// UpdateDefinition updates an agent definition.
func (r *Repository) UpdateDefinition(ctx context.Context, def *AgentDefinition) error {
	def.UpdatedAt = time.Now()
	_, err := r.db.NewUpdate().
		Model(def).
		WherePK().
		Returning("*").
		Exec(ctx)
	return err
}

// DeleteDefinition deletes an agent definition by ID.
func (r *Repository) DeleteDefinition(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().
		Model((*AgentDefinition)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// --- Extended Agent Run operations ---

// UpdateSessionStatus updates the workspace session status for an agent run.
func (r *Repository) UpdateSessionStatus(ctx context.Context, runID string, status SessionStatus) error {
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("session_status = ?", status).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// UpdateTraceAndRootRun persists the OTel trace_id and root_run_id on an agent run.
// Called immediately after the OTel span is created so the run row is linked to
// its trace and to the top-level orchestration run in the same request.
// Either argument may be empty string, in which case the corresponding column is set to NULL.
func (r *Repository) UpdateTraceAndRootRun(ctx context.Context, runID, traceID, rootRunID string) error {
	q := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Where("id = ?", runID)
	if traceID != "" {
		q = q.Set("trace_id = ?", traceID)
	} else {
		q = q.Set("trace_id = NULL")
	}
	if rootRunID != "" {
		q = q.Set("root_run_id = ?", rootRunID)
	} else {
		q = q.Set("root_run_id = NULL")
	}
	_, err := q.Exec(ctx)
	return err
}

// CreateRunWithOptions creates a new agent run with coordination options.
func (r *Repository) CreateRunWithOptions(ctx context.Context, opts CreateRunOptions) (*AgentRun, error) {
	run := &AgentRun{
		AgentID:         opts.AgentID,
		Status:          RunStatusRunning,
		StartedAt:       time.Now(),
		Summary:         make(map[string]any),
		ParentRunID:     opts.ParentRunID,
		MaxSteps:        opts.MaxSteps,
		ResumedFrom:     opts.ResumedFrom,
		StepCount:       opts.InitialStepCount,
		TriggerSource:   opts.TriggerSource,
		TriggerMetadata: opts.TriggerMetadata,
		TriggerMessage:  opts.TriggerMessage,
	}
	_, err := r.db.NewInsert().
		Model(run).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return run, nil
}

// FindRunByID returns an agent run by ID.
func (r *Repository) FindRunByID(ctx context.Context, runID string) (*AgentRun, error) {
	run := new(AgentRun)
	err := r.db.NewSelect().
		Model(run).
		Where("id = ?", runID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return run, nil
}

// PauseRun marks a run as paused, persisting the current step count.
func (r *Repository) PauseRun(ctx context.Context, runID string, stepCount int) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusPaused).
		Set("completed_at = ?", now).
		Set("step_count = ?", stepCount).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// CancelRun marks a run as cancelled.
func (r *Repository) CancelRun(ctx context.Context, runID string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusCancelled).
		Set("completed_at = ?", now).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// UpdateStepCount updates the step count for a running agent.
func (r *Repository) UpdateStepCount(ctx context.Context, runID string, stepCount int) error {
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("step_count = ?", stepCount).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// FailRunWithSteps marks a run as failed, persisting the step count at the time of failure.
func (r *Repository) FailRunWithSteps(ctx context.Context, runID string, errorMessage string, stepCount int) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusError).
		Set("completed_at = ?", now).
		Set("error_message = ?", errorMessage).
		Set("step_count = ?", stepCount).
		Set("session_status = ?", SessionStatusError).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// CompleteRunWithSteps marks a run as successfully completed with step count and duration.
func (r *Repository) CompleteRunWithSteps(ctx context.Context, runID string, summary map[string]any, stepCount int, durationMs int) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusSuccess).
		Set("completed_at = ?", now).
		Set("summary = ?", summary).
		Set("step_count = ?", stepCount).
		Set("duration_ms = ?", durationMs).
		Set("session_status = ?", SessionStatusCompleted).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// FindChildRuns returns all child runs of a parent run.
func (r *Repository) FindChildRuns(ctx context.Context, parentRunID string) ([]*AgentRun, error) {
	var runs []*AgentRun
	err := r.db.NewSelect().
		Model(&runs).
		Where("parent_run_id = ?", parentRunID).
		Order("started_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return runs, nil
}

// FindFirstChildRunForAgent returns the earliest run for a specific agent that
// has the given parent run ID. This is used to recover the original trigger
// message the parent sent to the child, even when the child has been re-enqueued
// multiple times (e.g. research-manager woken by web-researcher).
func (r *Repository) FindFirstChildRunForAgent(ctx context.Context, parentRunID, agentID string) (*AgentRun, error) {
	var run AgentRun
	err := r.db.NewSelect().
		Model(&run).
		Where("parent_run_id = ?", parentRunID).
		Where("agent_id = ?", agentID).
		Order("started_at ASC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// --- Agent Run Messages ---

// CreateMessage creates a new agent run message.
func (r *Repository) CreateMessage(ctx context.Context, msg *AgentRunMessage) error {
	_, err := r.db.NewInsert().
		Model(msg).
		Returning("*").
		Exec(ctx)
	return err
}

// FindMessagesByRunID returns all messages for a run, ordered by creation time.
func (r *Repository) FindMessagesByRunID(ctx context.Context, runID string) ([]*AgentRunMessage, error) {
	var msgs []*AgentRunMessage
	err := r.db.NewSelect().
		Model(&msgs).
		Where("run_id = ?", runID).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return msgs, nil
}

// --- Agent Run Tool Calls ---

// CreateToolCall creates a new agent run tool call record.
func (r *Repository) CreateToolCall(ctx context.Context, tc *AgentRunToolCall) error {
	_, err := r.db.NewInsert().
		Model(tc).
		Returning("*").
		Exec(ctx)
	return err
}

// FindToolCallsByRunID returns all tool calls for a run, ordered by creation time.
func (r *Repository) FindToolCallsByRunID(ctx context.Context, runID string) ([]*AgentRunToolCall, error) {
	var tcs []*AgentRunToolCall
	err := r.db.NewSelect().
		Model(&tcs).
		Where("run_id = ?", runID).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return tcs, nil
}

// --- Project-scoped Run History ---

// RunFilters holds optional filters for querying agent runs.
type RunFilters struct {
	AgentID *string
	Status  *AgentRunStatus
}

// FindRunsByProjectPaginated returns paginated agent runs for a project.
func (r *Repository) FindRunsByProjectPaginated(ctx context.Context, projectID string, filters RunFilters, limit, offset int) ([]*AgentRun, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	q := r.db.NewSelect().
		Model((*AgentRun)(nil)).
		Join("JOIN kb.agents AS a ON a.id = ar.agent_id").
		Where("a.project_id = ?", projectID)

	if filters.AgentID != nil {
		q = q.Where("ar.agent_id = ?", *filters.AgentID)
	}
	if filters.Status != nil {
		q = q.Where("ar.status = ?", *filters.Status)
	}

	totalCount, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	var runs []*AgentRun
	err = q.Order("ar.started_at DESC").
		Limit(limit).
		Offset(offset).
		Scan(ctx, &runs)
	if err != nil {
		return nil, 0, err
	}

	return runs, totalCount, nil
}

// FindRunByIDForProject returns a specific run scoped to a project.
func (r *Repository) FindRunByIDForProject(ctx context.Context, runID, projectID string) (*AgentRun, error) {
	run := new(AgentRun)
	err := r.db.NewSelect().
		Model(run).
		Join("JOIN kb.agents AS a ON a.id = ar.agent_id").
		Where("ar.id = ?", runID).
		Where("a.project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return run, nil
}

// --- Agent Webhook Hooks ---

// CreateWebhookHook creates a new webhook hook for an agent
func (r *Repository) CreateWebhookHook(ctx context.Context, hook *AgentWebhookHook) error {
	_, err := r.db.NewInsert().
		Model(hook).
		Returning("*").
		Exec(ctx)
	return err
}

// FindWebhookHooksByAgent returns all webhook hooks for a specific agent
func (r *Repository) FindWebhookHooksByAgent(ctx context.Context, agentID string, projectID string) ([]*AgentWebhookHook, error) {
	var hooks []*AgentWebhookHook
	err := r.db.NewSelect().
		Model(&hooks).
		Where("agent_id = ?", agentID).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return hooks, nil
}

// FindWebhookHookByID returns a webhook hook by its ID
func (r *Repository) FindWebhookHookByID(ctx context.Context, id string) (*AgentWebhookHook, error) {
	hook := new(AgentWebhookHook)
	err := r.db.NewSelect().
		Model(hook).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return hook, nil
}

// DeleteWebhookHook deletes a webhook hook
func (r *Repository) DeleteWebhookHook(ctx context.Context, id string, projectID string) error {
	res, err := r.db.NewDelete().
		Model((*AgentWebhookHook)(nil)).
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return errors.New("webhook hook not found or not authorized")
	}
	return nil
}

// --- Agent Questions ---

// CreateQuestion inserts a new agent question record.
func (r *Repository) CreateQuestion(ctx context.Context, q *AgentQuestion) error {
	_, err := r.db.NewInsert().Model(q).Exec(ctx)
	return err
}

// FindQuestionByID returns a question by ID.
func (r *Repository) FindQuestionByID(ctx context.Context, id string) (*AgentQuestion, error) {
	question := new(AgentQuestion)
	err := r.db.NewSelect().
		Model(question).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return question, nil
}

// FindPendingQuestionsByRunID returns all pending questions for a run.
func (r *Repository) FindPendingQuestionsByRunID(ctx context.Context, runID string) ([]*AgentQuestion, error) {
	var questions []*AgentQuestion
	err := r.db.NewSelect().
		Model(&questions).
		Where("run_id = ?", runID).
		Where("status = ?", QuestionStatusPending).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return questions, nil
}

// CancelPendingQuestionsForRun cancels all pending questions for a run.
func (r *Repository) CancelPendingQuestionsForRun(ctx context.Context, runID string) error {
	_, err := r.db.NewUpdate().
		Model((*AgentQuestion)(nil)).
		Set("status = ?", QuestionStatusCancelled).
		Set("updated_at = ?", time.Now()).
		Where("run_id = ?", runID).
		Where("status = ?", QuestionStatusPending).
		Exec(ctx)
	return err
}

// AnswerQuestion updates a question with the user's response.
func (r *Repository) AnswerQuestion(ctx context.Context, id string, response string, respondedBy string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentQuestion)(nil)).
		Set("response = ?", response).
		Set("responded_by = ?", respondedBy).
		Set("responded_at = ?", now).
		Set("status = ?", QuestionStatusAnswered).
		Set("updated_at = ?", now).
		Where("id = ?", id).
		Where("status = ?", QuestionStatusPending).
		Exec(ctx)
	return err
}

// ListQuestionsByRunID returns all questions for a run, ordered by creation time.
func (r *Repository) ListQuestionsByRunID(ctx context.Context, runID string) ([]*AgentQuestion, error) {
	var questions []*AgentQuestion
	err := r.db.NewSelect().
		Model(&questions).
		Where("run_id = ?", runID).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return questions, nil
}

// ListQuestionsByProject returns questions for a project with optional status filter.
func (r *Repository) ListQuestionsByProject(ctx context.Context, projectID string, status *AgentQuestionStatus) ([]*AgentQuestion, error) {
	var questions []*AgentQuestion
	q := r.db.NewSelect().
		Model(&questions).
		Where("project_id = ?", projectID)

	if status != nil {
		q = q.Where("status = ?", *status)
	}

	err := q.Order("created_at DESC").Scan(ctx)
	if err != nil {
		return nil, err
	}
	return questions, nil
}

// UpdateQuestionNotificationID sets the notification_id on a question record.
func (r *Repository) UpdateQuestionNotificationID(ctx context.Context, questionID string, notificationID string) error {
	_, err := r.db.NewUpdate().
		Model((*AgentQuestion)(nil)).
		Set("notification_id = ?", notificationID).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", questionID).
		Exec(ctx)
	return err
}

// UpdateNotificationActionStatus updates the action_status fields on a notification.
// This is a cross-domain update used when a user responds to an agent question.
func (r *Repository) UpdateNotificationActionStatus(ctx context.Context, notificationID string, status string, userID string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		TableExpr("kb.notifications").
		Set("action_status = ?", status).
		Set("action_status_at = ?", now).
		Set("action_status_by = ?", userID).
		Set("updated_at = ?", now).
		Where("id = ?", notificationID).
		Exec(ctx)
	return err
}

// --- ADK Sessions ---

// FindADKSessionsByProject returns ADK sessions associated with a specific project
// by joining against the agent_runs and agents tables.
func (r *Repository) FindADKSessionsByProject(ctx context.Context, projectID string, limit, offset int) ([]*bunsession.ADKSession, int, error) {
	var sessions []*bunsession.ADKSession

	// To enforce tenant isolation, we only return sessions where the session ID matches an agent run ID
	// or where there is a path from the run to the project.
	// We'll use an EXISTS subquery to check if there's a matching run for this project.

	q := r.db.NewSelect().
		Model(&sessions).
		Where("EXISTS (SELECT 1 FROM kb.agent_runs ar JOIN kb.agents a ON ar.agent_id = a.id WHERE ar.id::text = \"as\".id AND a.project_id = ?)", projectID).
		Order("update_time DESC")

	count, err := q.Limit(limit).Offset(offset).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, err
	}

	return sessions, count, nil
}

// FindADKSessionByIDForProject returns a specific ADK session and its events,
// ensuring the session belongs to the given project.
func (r *Repository) FindADKSessionByIDForProject(ctx context.Context, sessionID string, projectID string) (*bunsession.ADKSession, []*bunsession.ADKEvent, error) {
	// First verify the session belongs to the project
	var exists bool
	err := r.db.NewSelect().
		TableExpr("kb.agent_runs AS ar").
		Join("JOIN kb.agents AS a ON a.id = ar.agent_id").
		Where("ar.id::text = ?", sessionID).
		Where("a.project_id = ?", projectID).
		ColumnExpr("1").
		Limit(1).
		Scan(ctx, &exists)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil // Not found or no access
		}
		return nil, nil, err
	}

	if !exists {
		return nil, nil, nil
	}

	// Fetch the session
	session := new(bunsession.ADKSession)
	err = r.db.NewSelect().
		Model(session).
		Where("id = ?", sessionID).
		Scan(ctx)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	// Fetch events
	var events []*bunsession.ADKEvent
	err = r.db.NewSelect().
		Model(&events).
		Where("session_id = ?", sessionID).
		Order("timestamp ASC").
		Scan(ctx)

	if err != nil {
		return nil, nil, err
	}

	return session, events, nil
}

// ============================================================================
// Worker Pool Repository Methods (agent_run_jobs)
// ============================================================================

// CreateRunQueued creates an agent_runs row with status=queued and an
// agent_run_jobs row in the same transaction. Returns the new run.
func (r *Repository) CreateRunQueued(ctx context.Context, agentID string, maxAttempts int, opts ...CreateRunQueuedOptions) (*AgentRun, error) {
	var parentRunID *string
	var triggerMessage *string
	var triggerMetadata map[string]any
	if len(opts) > 0 {
		parentRunID = opts[0].ParentRunID
		triggerMessage = opts[0].TriggerMessage
		triggerMetadata = opts[0].TriggerMetadata
	}
	run := &AgentRun{
		AgentID:         agentID,
		Status:          RunStatusQueued,
		StartedAt:       time.Now(),
		Summary:         make(map[string]any),
		ParentRunID:     parentRunID,
		TriggerMessage:  triggerMessage,
		TriggerMetadata: triggerMetadata,
	}

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(run).Returning("*").Exec(ctx); err != nil {
			return fmt.Errorf("insert agent_runs: %w", err)
		}
		job := &AgentRunJob{
			RunID:       run.ID,
			Status:      JobStatusPending,
			MaxAttempts: maxAttempts,
			NextRunAt:   time.Now(),
		}
		if _, err := tx.NewInsert().Model(job).Exec(ctx); err != nil {
			return fmt.Errorf("insert agent_run_jobs: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return run, nil
}

// CreateRunJob inserts a new agent_run_jobs row for an existing run.
func (r *Repository) CreateRunJob(ctx context.Context, runID string, maxAttempts int) error {
	job := &AgentRunJob{
		RunID:       runID,
		Status:      JobStatusPending,
		MaxAttempts: maxAttempts,
		NextRunAt:   time.Now(),
	}
	_, err := r.db.NewInsert().Model(job).Exec(ctx)
	return err
}

// ClaimNextJob atomically claims the next pending job using FOR UPDATE SKIP LOCKED,
// transitions job→processing and run→running in a single transaction.
// Returns nil, nil when no job is available.
func (r *Repository) ClaimNextJob(ctx context.Context) (*AgentRunJob, error) {
	var job *AgentRunJob

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		j := new(AgentRunJob)
		err := tx.NewSelect().
			Model(j).
			Where("arj.status = ?", JobStatusPending).
			Where("arj.next_run_at <= now()").
			OrderExpr("arj.next_run_at ASC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // no job available
			}
			return fmt.Errorf("select next job: %w", err)
		}

		// Transition job to processing
		if _, err := tx.NewUpdate().
			Model(j).
			Set("status = ?", JobStatusProcessing).
			Set("attempt_count = attempt_count + 1").
			WherePK().
			Returning("*").
			Exec(ctx); err != nil {
			return fmt.Errorf("claim job: %w", err)
		}

		// Transition run to running
		if _, err := tx.NewUpdate().
			Model((*AgentRun)(nil)).
			Set("status = ?", RunStatusRunning).
			Where("id = ?", j.RunID).
			Exec(ctx); err != nil {
			return fmt.Errorf("update run to running: %w", err)
		}

		job = j
		return nil
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

// CompleteJob marks a job as completed and the run as success.
func (r *Repository) CompleteJob(ctx context.Context, jobID, runID string) error {
	now := time.Now()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().
			Model((*AgentRunJob)(nil)).
			Set("status = ?", JobStatusCompleted).
			Set("completed_at = ?", now).
			Where("id = ?", jobID).
			Exec(ctx); err != nil {
			return fmt.Errorf("complete job: %w", err)
		}
		if _, err := tx.NewUpdate().
			Model((*AgentRun)(nil)).
			Set("status = ?", RunStatusSuccess).
			Set("completed_at = ?", now).
			Where("id = ?", runID).
			Exec(ctx); err != nil {
			return fmt.Errorf("complete run: %w", err)
		}
		return nil
	})
}

// PauseJob marks a job as completed (preventing reprocessing) without overwriting
// the run status, which has already been set to paused by PauseRun.
func (r *Repository) PauseJob(ctx context.Context, jobID string) error {
	now := time.Now()
	_, err := r.db.NewUpdate().
		Model((*AgentRunJob)(nil)).
		Set("status = ?", JobStatusCompleted).
		Set("completed_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	return err
}

// FailJob marks a job failed. If requeue=true and attempt_count < max_attempts,
// sets job back to pending with exponential backoff; otherwise marks job failed and run error.
func (r *Repository) FailJob(ctx context.Context, jobID, runID, errMsg string, requeue bool, nextRunAt time.Time) error {
	now := time.Now()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if requeue {
			if _, err := tx.NewUpdate().
				Model((*AgentRunJob)(nil)).
				Set("status = ?", JobStatusPending).
				Set("next_run_at = ?", nextRunAt).
				Where("id = ?", jobID).
				Exec(ctx); err != nil {
				return fmt.Errorf("requeue job: %w", err)
			}
			// Run goes back to queued
			if _, err := tx.NewUpdate().
				Model((*AgentRun)(nil)).
				Set("status = ?", RunStatusQueued).
				Where("id = ?", runID).
				Exec(ctx); err != nil {
				return fmt.Errorf("requeue run: %w", err)
			}
		} else {
			if _, err := tx.NewUpdate().
				Model((*AgentRunJob)(nil)).
				Set("status = ?", JobStatusFailed).
				Set("completed_at = ?", now).
				Where("id = ?", jobID).
				Exec(ctx); err != nil {
				return fmt.Errorf("fail job: %w", err)
			}
			if _, err := tx.NewUpdate().
				Model((*AgentRun)(nil)).
				Set("status = ?", RunStatusError).
				Set("completed_at = ?", now).
				Set("error_message = ?", errMsg).
				Where("id = ?", runID).
				Exec(ctx); err != nil {
				return fmt.Errorf("fail run: %w", err)
			}
		}
		return nil
	})
}

// FindRunByIDForAgent returns a run by ID scoped to a project (via agent join).
// Returns nil, nil if not found or belongs to different project.
func (r *Repository) FindRunByIDProjectScoped(ctx context.Context, runID, projectID string) (*AgentRun, error) {
	return r.FindRunByIDForProject(ctx, runID, projectID)
}

// RequeueOrphanedQueuedRuns finds agent_runs with status=queued that have no
// active agent_run_jobs row (pending or processing) and inserts a new job row
// for each. Called at startup after MarkOrphanedRunsAsError.
func (r *Repository) RequeueOrphanedQueuedRuns(ctx context.Context) (int, error) {
	// Find queued runs that have no pending/processing job
	type runRow struct {
		ID string `bun:"id"`
	}
	var runs []runRow
	err := r.db.NewSelect().
		TableExpr("kb.agent_runs AS ar").
		ColumnExpr("ar.id").
		Where("ar.status = ?", RunStatusQueued).
		Where(`NOT EXISTS (
			SELECT 1 FROM kb.agent_run_jobs arj
			WHERE arj.run_id = ar.id
			  AND arj.status IN ('pending', 'processing')
		)`).
		Scan(ctx, &runs)
	if err != nil {
		return 0, fmt.Errorf("find orphaned queued runs: %w", err)
	}

	for _, row := range runs {
		if err := r.CreateRunJob(ctx, row.ID, 1); err != nil {
			return 0, fmt.Errorf("re-enqueue run %s: %w", row.ID, err)
		}
	}
	return len(runs), nil
}

// GetRunTokenUsage returns aggregated LLM token counts and estimated cost for a
// single agent run, reading from kb.llm_usage_events. Returns nil when no usage
// events exist for the run (e.g. the run has not yet executed any LLM calls).
func (r *Repository) GetRunTokenUsage(ctx context.Context, runID string) (*RunTokenUsage, error) {
	type row struct {
		TotalInput  int64   `bun:"total_input"`
		TotalOutput int64   `bun:"total_output"`
		TotalCost   float64 `bun:"total_cost"`
	}
	var result row
	err := r.db.NewRaw(`
		SELECT
			COALESCE(SUM(text_input_tokens + image_input_tokens + video_input_tokens + audio_input_tokens), 0) AS total_input,
			COALESCE(SUM(output_tokens), 0)        AS total_output,
			COALESCE(SUM(estimated_cost_usd), 0.0) AS total_cost
		FROM kb.llm_usage_events
		WHERE run_id = ?`,
		runID,
	).Scan(ctx, &result)
	if err != nil {
		return nil, fmt.Errorf("get run token usage: %w", err)
	}
	// Return nil when there is no recorded usage for this run.
	if result.TotalInput == 0 && result.TotalOutput == 0 && result.TotalCost == 0 {
		return nil, nil
	}
	return &RunTokenUsage{
		TotalInputTokens:  result.TotalInput,
		TotalOutputTokens: result.TotalOutput,
		EstimatedCostUSD:  result.TotalCost,
	}, nil
}

// GetOrgIDByProjectID returns the organization ID for the given project ID.
// It queries kb.projects directly. Returns an empty string (no error) when
// the project is not found.
func (r *Repository) GetOrgIDByProjectID(ctx context.Context, projectID string) (string, error) {
	var orgID string
	err := r.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("organization_id").
		Where("id = ?", projectID).
		Scan(ctx, &orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("GetOrgIDByProjectID: %w", err)
	}
	return orgID, nil
}

// GetFirstProjectIDByOrgID returns the ID of the first project in the given org.
// Used as an infrastructure project sentinel when no project context is active.
// Returns an empty string (no error) when no projects exist for the org.
func (r *Repository) GetFirstProjectIDByOrgID(ctx context.Context, orgID string) (string, error) {
	var projectID string
	q := r.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("id").
		OrderExpr("created_at ASC").
		Limit(1)
	if orgID != "" {
		q = q.Where("organization_id = ?", orgID)
	}
	err := q.Scan(ctx, &projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("GetFirstProjectIDByOrgID: %w", err)
	}
	return projectID, nil
}
