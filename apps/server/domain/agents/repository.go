package agents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
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
		Tools:     []string{},
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

// MarkStaleRunsAsError finds runs stuck in "running" status for longer than
// the given threshold and marks them as errored. Unlike MarkOrphanedRunsAsError
// (which runs at startup), this runs periodically to catch runs abandoned
// mid-execution (e.g. CLI connection drop without graceful close).
func (r *Repository) MarkStaleRunsAsError(ctx context.Context, threshold time.Duration) (int, error) {
	cutoff := time.Now().Add(-threshold)
	now := time.Now()
	res, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusError).
		Set("completed_at = ?", now).
		Set("error_message = ?", "run exceeded idle timeout (likely abandoned by client)").
		Where("status = ?", RunStatusRunning).
		Where("started_at < ?", cutoff).
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

// ResolveDefinitionForAgent looks up the AgentDefinition for a runtime Agent.
// Resolution order:
//  1. FK: agent.AgentDefinitionID (direct DB relationship — authoritative)
//  2. StrategyType: parse "chat-session:{defID}" to extract definition ID
//  3. Exact name match (agent.Name == definition.Name)
//  4. Agent name + "-def" suffix (e.g. agent "foo" → definition "foo-def")
//  5. Strip "Chat session for " prefix (legacy chat agents)
//
// Returns (nil, nil) when no definition is found.
func (r *Repository) ResolveDefinitionForAgent(ctx context.Context, agent *Agent) (*AgentDefinition, error) {
	if agent == nil {
		return nil, nil
	}
	// 1. FK lookup — authoritative, no name guessing needed
	if agent.AgentDefinitionID != nil && *agent.AgentDefinitionID != "" {
		var def AgentDefinition
		err := r.db.NewSelect().Model(&def).Where("id = ?", *agent.AgentDefinitionID).Scan(ctx)
		if err == nil {
			return &def, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		// Definition was deleted — fall through to name-based lookup
	}
	// 2. Parse "chat-session:{defID}" from StrategyType — CLI-triggered agents use this pattern
	if defID, ok := strings.CutPrefix(agent.StrategyType, "chat-session:"); ok && defID != "" {
		projectID := agent.ProjectID
		def, err := r.FindDefinitionByID(ctx, defID, &projectID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if def != nil {
			return def, nil
		}
	}
	// 3. Exact name match
	def, err := r.FindDefinitionByName(ctx, agent.ProjectID, agent.Name)
	if err != nil {
		return nil, err
	}
	if def != nil {
		return def, nil
	}
	// 4. Try agent name + "-def" suffix (e.g. runtime "foo" → definition "foo-def")
	def, err = r.FindDefinitionByName(ctx, agent.ProjectID, agent.Name+"-def")
	if err != nil {
		return nil, err
	}
	if def != nil {
		return def, nil
	}
	// 5. Strip known prefixes and retry
	if stripped, ok := strings.CutPrefix(agent.Name, "Chat session for "); ok && stripped != "" {
		return r.FindDefinitionByName(ctx, agent.ProjectID, stripped)
	}
	return nil, nil
}

// FindEnabledByTriggerType returns all enabled agents matching the given trigger type,
// excluding agents that belong to soft-deleted projects.
func (r *Repository) FindEnabledByTriggerType(ctx context.Context, triggerType AgentTriggerType) ([]*Agent, error) {
	var agents []*Agent
	err := r.db.NewSelect().
		Model(&agents).
		Join("JOIN kb.projects AS p ON p.id = agent.project_id").
		Where("agent.enabled = true").
		Where("agent.trigger_type = ?", triggerType).
		Where("p.deleted_at IS NULL").
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
const graphQueryAgentSystemPrompt = `You are a knowledge graph query assistant. Help users explore and understand data in their knowledge graph.

## Rules
1. ALWAYS use tools to look up data. Never answer from training data or fabricate entities, relationships, or facts.
2. Cite specific entity names, types, and relationship types from tool results.
3. If tools return no results, clearly state that no matching data was found.
4. Format responses using markdown. Use tables for structured data.
5. Keep responses concise and factual.
6. Start with search-hybrid for most queries. Use entity-query to list by type. Use entity-edges-get to explore relationships.

## Context budget and field selection
The model has a 1M token input window, but large entity payloads are expensive and slow. A single entity with full properties is ~200-500 tokens. Always use the minimum fields needed:
- Every entity always returns: id, key, name, type, created_at, updated_at — these are free, never request them in fields[].
- Before fetching entities, decide which *additional* property fields your answer requires. Pass fields=["field1","field2"] to entity-query to retrieve only those — never fetch the full object when you only need a subset.
- For counting or listing names: omit fields entirely (name is already free).
- For summarizing by a dimension (e.g. method, status, domain): fields=["<dimension_field>"] only.
- For full detail on a specific entity: omit fields (returns everything) or use ids=[...] with no fields filter.

Response size thresholds:
- ≤50 entities: return full results freely.
- 51–200 entities: summarize in your answer (counts, patterns, key names) rather than listing everything.
- >200 entities: return only id+name, then offer to fetch details for specific IDs via entity-query ids=[...].

## Pagination strategy
When a question requires a complete list ("how many", "list all", "which ones"):
- Step 1: Call entity-type-list first — it returns exact per-type counts at near-zero cost. Use this to decide whether pagination is needed before fetching any entities.
- Step 2: If count ≤ 200, fetch in one call with limit=200.
- Step 3: If count > 200, paginate: call entity-query repeatedly with limit=200, incrementing offset by 200 each time, until has_more=false. The first response includes pagination.total so you can compute total pages upfront.
- Step 4: Accumulate results across pages in your context. Do NOT re-fetch pages already retrieved.
- Step 5: Summarize — report counts, group by key properties, highlight patterns. For >200 results, list id+name only and offer to fetch details on request via entity-query ids=[...].

## Versioning
Entities are versioned. Each update creates a new version; the canonical ID stays constant across versions.
- entity-query always returns the current (HEAD) version. The response includes a "version" field (integer, starting at 1).
- To see all versions of an entity: call entity-history with the canonical entity_id. Returns [{version, physical_id, updated_at}, ...].
- To fetch a specific historical version's properties: call entity-query with ids=[physical_id] (the physical_id from entity-history, NOT the canonical_id).`

// EnsureGraphQueryAgent returns the graph-query-agent for the project, creating it if it
// does not exist yet. Uses VisibilityInternal so it never appears in the public list.
// Safe to call concurrently — a race between two callers results in one insert and one
// subsequent read (FindDefinitionByName will find the winner's row).
func (r *Repository) EnsureGraphQueryAgent(ctx context.Context, projectID string) (*AgentDefinition, error) {
	existing, err := r.FindDefinitionByName(ctx, projectID, "graph-query-agent")
	if err != nil {
		return nil, fmt.Errorf("failed to look up graph-query-agent: %w", err)
	}

	temperature := float32(0.1)
	maxSteps := 15
	systemPrompt := graphQueryAgentSystemPrompt

	// MCP tools for direct graph queries — no sandbox/SDK needed.
	canonicalTools := []string{
		"search-hybrid",
		"entity-query",
		"entity-history",
		"entity-edges-get",
		"relationship-list",
		"entity-type-list",
	}

	if existing != nil {
		// Self-heal: update tools, system prompt, model, and sandbox config to pick up
		// any changes deployed in code.
		existing.Tools = canonicalTools
		existing.SystemPrompt = &systemPrompt
		if existing.Model == nil {
			existing.Model = &ModelConfig{}
		}
		existing.Model.Name = "gemini-3.1-flash-lite-preview"
		existing.Model.Temperature = &temperature
		existing.MaxSteps = &maxSteps
		// Clear sandbox config — this agent uses MCP tools, not SDK/sandbox.
		existing.SandboxConfig = nil

		// Apply per-project overrides (if any) on top of canonical defaults.
		if projectID != "" {
			if override, oErr := r.GetAgentOverride(ctx, projectID, "graph-query-agent"); oErr == nil && override != nil {
				ApplyAgentOverride(existing, override)
			}
		}

		if updateErr := r.UpdateDefinition(ctx, existing); updateErr != nil {
			// Non-fatal — return existing as-is rather than failing the query call.
			return existing, nil
		}
		return existing, nil
	}

	def := &AgentDefinition{
		ProjectID:    projectID,
		Name:         "graph-query-agent",
		Description:  strPtr("Knowledge graph query assistant — explores data via MCP tools"),
		SystemPrompt: &systemPrompt,
		Model: &ModelConfig{
			Name:        "gemini-3.1-flash-lite-preview",
			Temperature: &temperature,
		},
		Tools:      canonicalTools,
		Skills:     []string{},
		FlowType:   FlowTypeSingle,
		IsDefault:  true,
		MaxSteps:   &maxSteps,
		Visibility: VisibilityInternal,
		Config:     map[string]any{},
	}

	// Apply per-project overrides (if any) on top of canonical defaults.
	if projectID != "" {
		if override, oErr := r.GetAgentOverride(ctx, projectID, "graph-query-agent"); oErr == nil && override != nil {
			ApplyAgentOverride(def, override)
		}
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

// graphInsertAgentSystemPrompt is the default system prompt for the graph-insert-agent.
const graphInsertAgentSystemPrompt = `You are a knowledge graph insertion agent. Your job is to understand natural language input and persist it as structured data in the knowledge graph.

## Workflow — follow these steps in order

### 1. EXTRACT ENTITIES (think before acting)
Before calling any tool, read the entire input carefully and produce an exhaustive mental list:

**Every entity mentioned**, no matter how briefly — people, places, organisations, objects, events, concepts.
For each entity write down:
- Canonical name
- Type (person / place / organisation / event / object / concept / …)
- Every specific property stated or clearly implied: age, role, status, identity, date, location, description, etc.
- Do NOT summarise or merge facts — capture them at the finest grain possible.
  - Bad: {name: "Caroline", notes: "transgender, single, looking to adopt"}
  - Good: {name: "Caroline", gender_identity: "transgender woman", relationship_status: "single", goal: "adopt a child"}
- For each entity, exhaust every fact the input contains about it — preferences, history, activities, affiliations, physical traits, opinions, anything stated or clearly implied. Each distinct fact → its own typed property. Never bundle multiple facts into a single string value.

**Every relationship** between those entities:
- Who/what → verb → who/what
- Include both directions if the relationship is symmetric (e.g. met_with should appear on both sides as separate edges, or as one edge with an inverse noted)
- Named relationship types must be snake_case verbs: is_pursuing, works_at, attended, located_in, met_with, owns, manages, member_of, etc.

### 2. CHECK SCHEMA
Call schema-compiled-types to get all active object and relationship types.
- Match each entity type and relationship type from step 1 against existing types.
- Only call schema-create if NO existing type is a reasonable match AND schema_policy != "reuse_only".
- If schema_policy = "ask": call ask_user before creating any new type.
- If schema_policy = "auto": create new types autonomously, keep names snake_case and minimal.
- If schema_policy = "reuse_only": never call schema-create.
- Define only properties present in the input. snake_case names only.

### 3. DEDUP CHECK
For EVERY entity from step 1, call search-hybrid with "<name> <type>" as query.
- High-confidence match (same name + type + clearly the same thing) → plan UPDATE, record existing canonical_id.
- Uncertain → plan CREATE.
- For matched entities call entity-edges-get to find already-existing relationships — skip creating duplicates.

### 4. CREATE BRANCH
Call graph-branch-create with name "remember/<short-kebab-slug>".
Record the branch_id — pass it to ALL subsequent write calls.

### 5. WRITE ENTITIES
Create or update every entity from step 1 on the branch:
- entity-create: set key (<type>-<slug>), name, and ALL properties from step 1.
- entity-update: for dedup matches, patch only the new/changed properties.
- Batch multiple entities in a single entity-create call where possible.

### 6. WRITE RELATIONSHIPS
After all entities exist, create every relationship from step 1:
- Call relationship-create for each directed edge: src_id → type → dst_id.
- Always use the canonical_id (or newly created entity id) for src/dst.
- For symmetric relationships (e.g. met_with) create both directions explicitly.
- Include any properties on the relationship (date, weight, context, etc.).

### 7. MERGE (skip if dry_run = true)
Call graph-branch-merge with branch_id and execute=true.
- Conflicts → surface to user, do not force-merge.

### 8. CLEANUP
Merge succeeded or dry_run → call graph-branch-delete.
Merge failed → leave branch, report its name.

### 9. REPORT
Summarise in markdown:
- Entities created / updated (keys + key properties)
- Relationships created (src → type → dst)
- New schema types created
- Branch and merge status

## Rules
- ALWAYS key format: <type>-<identifying-slug> e.g. "person-caroline", "event-school-lgbtq-talk".
- NEVER write to main directly. Always use a branch.
- NEVER skip step 3 (dedup). Search for EVERY entity before writing.
- NEVER skip step 6 (relationships). If step 1 identified relationships, they MUST be written.
- Capture facts at finest grain — separate properties, not blob strings.
- snake_case for all type names and property names.
- If input is ambiguous, state your interpretation in the report.`

// EnsureGraphInsertAgent returns the graph-insert-agent for the project, creating it if it
// does not exist yet. schemaPolicy controls whether the agent may create new schema types:
//   - "auto"        (default) — create new types if no existing type is a good match
//   - "reuse_only"  — never call schema-create; always reuse closest existing type
//   - "ask"         — call ask_user before creating any new schema type
//
// Safe to call concurrently — a race between two callers results in one insert and one
// subsequent read (FindDefinitionByName will find the winner's row).
func (r *Repository) EnsureGraphInsertAgent(ctx context.Context, projectID string, schemaPolicy string) (*AgentDefinition, error) {
	existing, err := r.FindDefinitionByName(ctx, projectID, "graph-insert-agent")
	if err != nil {
		return nil, fmt.Errorf("failed to look up graph-insert-agent: %w", err)
	}

	temperature := float32(0.2)
	maxSteps := 30
	systemPrompt := graphInsertAgentSystemPrompt

	// MCP tools for graph insertion — read (dedup/schema), write (branch + data).
	canonicalTools := []string{
		// Read / understand
		"search-hybrid",
		"entity-query",
		"entity-type-list",
		"entity-edges-get",
		"schema-compiled-types",
		"schema-list-installed",
		// Schema write (conditionally used per schema_policy)
		"schema-create",
		// Branch lifecycle
		"graph-branch-create",
		"graph-branch-merge",
		"graph-branch-delete",
		// Data write
		"entity-create",
		"entity-update",
		"relationship-create",
	}

	// For schema_policy="ask", include ask_user so the agent can prompt before schema creation.
	if schemaPolicy == "ask" {
		canonicalTools = append(canonicalTools, "ask_user")
	}

	if existing != nil {
		existing.Tools = canonicalTools
		existing.SystemPrompt = &systemPrompt
		if existing.Model == nil {
			existing.Model = &ModelConfig{}
		}
		existing.Model.Temperature = &temperature
		existing.MaxSteps = &maxSteps
		existing.SandboxConfig = nil

		// Apply per-project overrides on top of canonical defaults.
		if projectID != "" {
			if override, oErr := r.GetAgentOverride(ctx, projectID, "graph-insert-agent"); oErr == nil && override != nil {
				ApplyAgentOverride(existing, override)
			}
		}

		if updateErr := r.UpdateDefinition(ctx, existing); updateErr != nil {
			return existing, nil
		}
		return existing, nil
	}

	def := &AgentDefinition{
		ProjectID:    projectID,
		Name:         "graph-insert-agent",
		Description:  strPtr("Knowledge graph insertion agent — understands natural language and persists structured data via MCP tools"),
		SystemPrompt: &systemPrompt,
		Model: &ModelConfig{
			Temperature: &temperature,
		},
		Tools:      canonicalTools,
		Skills:     []string{},
		FlowType:   FlowTypeSingle,
		IsDefault:  false,
		MaxSteps:   &maxSteps,
		Visibility: VisibilityInternal,
		Config:     map[string]any{},
	}

	// Apply per-project overrides on top of canonical defaults.
	if projectID != "" {
		if override, oErr := r.GetAgentOverride(ctx, projectID, "graph-insert-agent"); oErr == nil && override != nil {
			ApplyAgentOverride(def, override)
		}
	}

	if err := r.CreateDefinition(ctx, def); err != nil {
		// Race condition: another caller inserted first — retry the read.
		if existing, err2 := r.FindDefinitionByName(ctx, projectID, "graph-insert-agent"); err2 == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create graph-insert-agent: %w", err)
	}

	return def, nil
}

// cliAssistantAgentSystemPrompt is the default system prompt for the cli-assistant-agent.
const cliAssistantAgentSystemPrompt = `You are a CLI assistant for the Memory knowledge management platform.
You answer questions and take direct action: create, update, delete entities, relationships, agents, schemas, MCP servers, and projects.

## Context & Auth

You are told whether the user is authenticated and has a project context.
- **Not authenticated**: answer docs questions only. For tasks, tell user to run "memory login", set MEMORY_API_KEY, or pass --project-token.
- **Authenticated, no project**: docs + account-level tasks (create_project, list_traces, etc.). For project-scoped tasks, say: pass --project <id> or run "memory config set project_id <id>".
- **Authenticated + project**: full access — use all available tools.

## Classification & Tool Constraints

Classify each request, then strictly follow tool rules:
- **DOCS**: use ONLY web-fetch. No graph/agent/schema/skill tools.
- **TASK**: use ONLY action/data tools. No web-fetch unless user explicitly asks for docs.
- **MIXED**: fetch docs first, then action tools.

Not authenticated + TASK → do NOT call tools. Explain what would be done and how to authenticate.

## Docs Lookup

Fetch from: https://emergent-company.github.io/emergent.memory/latest/

URL pattern: .../latest/<section>/<page>/

| Section | Pages |
|---|---|
| user-guide | getting-started, agents, knowledge-graph, documents, datasources, tasks, chat, branches, backups, api-tokens, integrations, notifications |
| developer-guide | provider-setup, mcp-servers, schema, schema-registry, sandbox, extraction, scheduler, security-scopes, health-ops, email-setup |
| go-sdk | (single page) |
| api-reference | (single page) |

Fetch the specific page directly. If unsure, fetch the section index. Never re-fetch a URL already retrieved.

## Response Format

Default to CLI commands. Only include REST/curl/HTTP examples if the user explicitly asks about the API, SDK, or endpoints.

## Task Execution

Use tools to fulfill requests directly. Never fabricate live state.

For writes: briefly state intent before executing. For deletes: warn explicitly. Do not ask for confirmation.

## CLI Reference

Knowledge Base:
  memory blueprints <source>
  memory documents list|get|upload|delete
  memory embeddings status|pause|resume|config
  memory graph objects create|create-batch|list|get|update|delete|edges
  memory graph relationships create|create-batch|list|get|delete
  memory graph branches create|list|get|update|delete|merge
  memory query "<question>"          (agent or --mode=search)
  memory schemas list|installed|install|uninstall|get|create|delete|compiled-types
  memory browse

Agents & AI:
  memory adk-sessions list|get
  memory agent-definitions create|list|get|update|delete    (aliases: agent-defs, defs)
  memory agents create|list|get|update|delete|trigger
  memory agents runs <agent-id> [--limit N]
  memory agents get-run <run-id>
  memory agents hooks|questions
  memory agents mcp-servers create|list|get|update|delete|inspect|sync|tools|configure
  memory ask "<question>"
  memory mcp-guide
  memory provider configure|configure-project|models|test|usage
  memory skills create|list|get|update|delete|import

Relocated: "memory mcp-servers ..." is now "memory agents mcp-servers ...". Always use the new path.

Account & Access:
  memory config set|set-server|set-credentials|show
  memory login / memory logout
  memory projects create|list|get|set|delete|create-token|set-info|set-provider
  memory set-token / memory status / memory tokens create|list|get|revoke

Server (self-hosted):
  memory server install|upgrade|uninstall
  memory server ctl start|stop|restart|status|logs|health|shell|pull
  memory server doctor [--fix]

Other: memory traces list|get|search / memory upgrade / memory version

Common flags: --server <url>, --project <id>, --project-token <tok>, --output table|json|yaml|csv, --compact, --debug, --no-color

## Branching

Branches are isolated workspaces for the knowledge graph. Objects/relationships written with
--branch <id> are invisible to the main graph until merged.

Key facts:
- The main graph has NO branch ID. Omitting --branch writes to the main graph.
- "memory graph branches list" shows all branch IDs for the project.
- --branch is a flag on graph write commands (objects create/update, relationships create), NOT on branches subcommands.
- --parent on "branches create" is optional lineage metadata only — it does NOT affect merge behavior.
- Merge direction: source → target. Both must be branch IDs from "branches list".
- "branches merge <target-id> --source <source-id>" is a dry run by default. Add --execute to apply.
- Conflicts block --execute. Resolve by making source and target agree on the conflicting object, then re-run.
- Merge is all-or-nothing (single transaction). If any write fails, the whole merge rolls back.

Workflow:
  BRANCH_ID=$(memory graph branches create --name "my-branch" --output json | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
  memory graph objects create --type Foo --key bar --branch "$BRANCH_ID"
  memory graph branches merge <target-id> --source "$BRANCH_ID"          # dry run
  memory graph branches merge <target-id> --source "$BRANCH_ID" --execute # apply

## Platform Facts

- **Providers**: Google AI (Gemini API) and Google Cloud Vertex AI only. No OpenAI/Anthropic.
- **Models**: Gemini family (gemini-2.0-flash, gemini-2.5-flash, gemini-2.5-pro, gemini-3.1-flash-lite-preview).
- **Provider config**: org-level via "memory provider configure"; project override via "memory provider configure-project".`

// EnsureCliAssistantAgent returns the cli-assistant-agent for the project (or with an empty
// project ID for the user-level /api/ask endpoint), creating it if it does not exist yet.
// If it already exists, its Tools list, SystemPrompt, and SandboxConfig are updated to
// reflect any changes made to cliAssistantAgentSystemPrompt and the canonical tool whitelist.
// Uses VisibilityInternal so it never appears in the public agent list.
// Safe to call concurrently — a race between two callers results in one insert and one
// subsequent read (FindDefinitionByName will find the winner's row).
// runtime parameter is accepted for API compatibility but no longer affects behavior
// (both variants use MCP tools, not sandbox).
func (r *Repository) EnsureCliAssistantAgent(ctx context.Context, projectID string, runtime string) (*AgentDefinition, error) {
	agentName := "cli-assistant-agent"

	existing, err := r.FindDefinitionByName(ctx, projectID, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to look up %s: %w", agentName, err)
	}

	temperature := float32(0.3)
	maxSteps := 20
	systemPrompt := cliAssistantAgentSystemPrompt

	// MCP tools — no sandbox/SDK needed.
	canonicalTools := []string{
		// Web access for documentation lookups (DOCS classification)
		"web-fetch",
		// Project info
		"project-get",
		// Knowledge graph — read
		"search-hybrid",
		"entity-query",
		"entity-search",
		"search-semantic",
		"search-similar",
		"entity-edges-get",
		"graph-traverse",
		"entity-type-list",
		"schema-version",
		"relationship-list",
		// Knowledge graph — write
		"entity-create",
		"entity-update",
		"entity-delete",
		"relationship-create",
		"relationship-update",
		"relationship-delete",
		// Agent management — read
		"agent-def-list",
		"agent-def-get",
		"agent-list",
		"agent-get",
		"agent-run-list",
		"agent-run-get",
		"agent-run-tool-calls",
		"agent-list-available",
		// Agent definition — write
		"agent-def-create",
		"update_agent_definition",
		"agent-def-delete",
		// Runtime agent — write
		"agent-create",
		"update_agent",
		"agent-delete",
		"trigger_agent",
		// Schema registry — read
		"schema-list",
		"schema-get",
		"schema-list-available",
		"schema-list-installed",
		// Schema registry — write
		"schema-create",
		"schema-delete",
		"schema-assign",
		"schema-assignment-update",
		// MCP registry — write
		"mcp-server-create",
		"update_mcp_server",
		"mcp-server-delete",
		"mcp-registry-install",
		"sync_mcp_server_tools",
		// Project — write
		"project-create",
		// Documents — read/write (non-destructive uploads allowed)
		"document-list",
		"document-get",
		"document-upload",
		"document-delete",
		// Skills — read/write
		"skill-list",
		"skill-get",
		"skill-create",
		"skill-update",
		"skill-delete",
		// Embeddings — read only (no pause/resume/config changes)
		"embedding-status",
		// Agent Questions and ADK sessions — read
		"agent-question-list",
		"agent-question-list-project",
		"agent-question-respond",
		"adk-session-list",
		"adk-session-get",
		// Traces — read
		"trace-list",
		"trace-get",
	}

	if existing != nil {
		// Self-heal: update tools, system prompt, model, and clear sandbox config
		// to pick up any changes deployed in code.
		existing.Tools = canonicalTools
		existing.SystemPrompt = &systemPrompt
		if existing.Model == nil {
			existing.Model = &ModelConfig{}
		}
		existing.Model.Name = "gemini-3.1-flash-lite-preview"
		existing.Model.Temperature = &temperature
		existing.MaxSteps = &maxSteps
		// Clear sandbox config — this agent uses MCP tools, not SDK/sandbox.
		existing.SandboxConfig = nil

		// Apply per-project overrides (if any) on top of canonical defaults.
		if projectID != "" {
			if override, oErr := r.GetAgentOverride(ctx, projectID, agentName); oErr == nil && override != nil {
				ApplyAgentOverride(existing, override)
			}
		}

		if updateErr := r.UpdateDefinition(ctx, existing); updateErr != nil {
			// Non-fatal — return existing as-is rather than failing the ask call.
			return existing, nil
		}
		return existing, nil
	}

	def := &AgentDefinition{
		ProjectID:    projectID,
		Name:         agentName,
		Description:  strPtr("CLI and platform assistant — answers documentation questions and executes tasks using available tools"),
		SystemPrompt: &systemPrompt,
		Model: &ModelConfig{
			Name:        "gemini-3.1-flash-lite-preview",
			Temperature: &temperature,
		},
		Tools:      canonicalTools,
		Skills:     []string{},
		FlowType:   FlowTypeSingle,
		IsDefault:  false,
		MaxSteps:   &maxSteps,
		Visibility: VisibilityInternal,
		Config:     map[string]any{},
	}

	// Apply per-project overrides (if any) on top of canonical defaults.
	if projectID != "" {
		if override, oErr := r.GetAgentOverride(ctx, projectID, agentName); oErr == nil && override != nil {
			ApplyAgentOverride(def, override)
		}
	}

	if err := r.CreateDefinition(ctx, def); err != nil {
		// Race condition: another caller inserted first — retry the read.
		if existing, err2 := r.FindDefinitionByName(ctx, projectID, agentName); err2 == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create %s: %w", agentName, err)
	}

	return def, nil
}

// cliAssistantAgentV2SystemPrompt is the code-generation-focused system prompt for v2.
// Instead of 57 MCP tools, the agent writes a single Python SDK script and calls run_python.
const cliAssistantAgentV2SystemPrompt = `You are a CLI assistant for the Memory knowledge management platform.
You accomplish tasks by writing and executing Python scripts using the Memory SDK.

## How you work

1. Classify the request:
   - **DOCS_QUESTION** → answer from your knowledge below; no tools needed
   - **TASK** → write a Python script, execute it with run_python, format the output
   - **MIXED** → answer the docs part from knowledge, then run a script for live data

2. For TASK requests, write a **complete, self-contained Python script** and call run_python once.
   Do NOT make multiple run_python calls — get everything done in one script.

3. Format the output as clean markdown for the terminal.

## Authentication & Context

You will be told whether the user is authenticated and whether a project context is active.
- **Not authenticated**: answer docs questions only. For tasks, explain how to authenticate:
  "memory login" for OAuth, MEMORY_API_KEY for standalone, --project-token for project-scoped.
- **Auth, no project**: account-level tasks work (list projects, etc.). For project tasks, tell user to pass --project <id>.
- **Auth + project**: full access — write scripts that operate on the active project.

## Python SDK Reference

The sandbox has credentials pre-injected. Use Client.from_env() — never hardcode keys.

ALL SDK methods return plain dicts. Use bracket access (p['name']), NOT attribute access (p.name).

~~~python
from emergent import Client

client = Client.from_env()

# ── Projects ──
client.projects.list() -> list[dict]                    # keys: id, name, orgId
client.projects.get(id) -> dict
client.projects.create({"name": "..."}) -> dict
client.projects.update(id, {"name": "..."}) -> dict
client.projects.delete(id) -> None

# ── Graph Objects ──
client.graph.list_objects(type=None, status=None, limit=50, cursor=None) -> dict
    # Returns: {data: [...], cursor: str|None, total: int}
    # Each object: {id, entity_id, type, properties, labels, status, ...}
client.graph.create_object({"type": "...", "properties": {...}}) -> dict
client.graph.update_object(id, {"properties": {...}}) -> dict  # WARNING: returns NEW id
client.graph.delete_object(id) -> None
client.graph.hybrid_search({"query": "..."}) -> dict    # {data: [{object, score}]}
client.graph.bulk_create_objects([...]) -> dict          # max 100, {items, errors}

# ── Agents ──
client.agents.list() -> list[dict]
client.agent_definitions.list() -> list[dict]
client.agent_definitions.get(id) -> dict
client.agent_definitions.create({...}) -> dict
client.agent_definitions.update(id, {...}) -> dict
client.agent_definitions.delete(id) -> None

# ── Schemas ──
client.schemas.list() -> list[dict]
client.schemas.get(id) -> dict
client.schemas.create({...}) -> dict
client.schemas.delete(id) -> None

# ── Documents ──
client.documents.list() -> list[dict]
client.documents.get(id) -> dict
client.documents.delete(id) -> None

# ── Skills ──
client.skills.list() -> list[dict]
client.skills.get(id) -> dict
client.skills.create({...}) -> dict
client.skills.update(id, {...}) -> dict
client.skills.delete(id) -> None

# ── Search ──
client.search.hybrid({"query": "...", "limit": 10}) -> dict
~~~

### Script template

~~~python
from emergent import Client

client = Client.from_env()

# ... your logic here ...

# ALWAYS print results — empty stdout = no output shown to user
print("Result:", result)
~~~

### Important rules
- Always print results with print(). Empty stdout means nothing is shown.
- Check for empty results and print a clear message (e.g. "No matching projects found").
- Use try/except for error handling when appropriate.
- Non-zero exit_code means an exception — read stderr for the traceback.

## CLI Knowledge (for DOCS_QUESTION)

Knowledge Base:
  memory blueprints <source>         Apply packs/agents/skills/seed from a dir or GitHub URL
  memory documents list|get|upload|delete
  memory embeddings status|pause|resume|config
  memory graph objects create|create-batch|list|get|update|delete|edges
  memory graph relationships create|create-batch|list|get|delete
  memory query "<question>"          Natural-language query
  memory schemas list|installed|install|uninstall|get|create|delete|compiled-types
  memory browse                      Interactive TUI

Agents & AI:
  memory agent-definitions create|list|get|update|delete    (aliases: agent-defs, defs)
  memory agents create|list|get|update|delete|trigger
  memory agents runs <agent-id>       List recent runs
  memory agents get-run <run-id>      Full run detail
  memory agents hooks|questions
  memory agents mcp-servers create|list|get|update|delete|inspect|sync|tools|configure
  memory ask "<question>"             Ask the CLI assistant
  memory provider configure|configure-project|models|test|usage
  memory skills create|list|get|update|delete|import

Account & Access:
  memory config set|set-server|set-credentials|show
  memory login / memory logout
  memory projects create|list|get|set|delete|create-token|set-info|set-provider
  memory status                       Show auth status
  memory tokens create|list|get|revoke

Server (self-hosted):
  memory server install|upgrade|ctl|doctor|uninstall

Other:
  memory traces list|get|search
  memory upgrade [--force]
  memory version

Platform facts:
- Supported LLM providers: Google AI (Gemini API) and Google Cloud Vertex AI ONLY.
- Supported models: Gemini family (gemini-2.0-flash, gemini-2.5-flash, gemini-2.5-pro, gemini-3.1-flash-lite-preview).

## Response Style
- Keep responses concise and focused
- Use markdown with code blocks for CLI commands
- For lists, use tables when there are multiple columns
- Number multi-step instructions clearly`

// EnsureCliAssistantAgentV2 creates the v2 code-generation variant of the CLI assistant.
// Instead of 57 MCP tools, v2 uses only run_python + bash and a code-gen system prompt.
// The agent writes a complete Python SDK script per request, reducing LLM round-trips from
// ~5-10 (tool call per action) to 1-2 (generate script + execute).
// runtime parameter is accepted for signature compatibility but v2 always uses Python.
func (r *Repository) EnsureCliAssistantAgentV2(ctx context.Context, projectID string, runtime string) (*AgentDefinition, error) {
	agentName := "cli-assistant-agent-v2"

	existing, err := r.FindDefinitionByName(ctx, projectID, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to look up %s: %w", agentName, err)
	}

	temperature := float32(0.3)
	maxSteps := 5
	systemPrompt := cliAssistantAgentV2SystemPrompt

	sandboxCfg := &sandbox.AgentSandboxConfig{
		Enabled:   true,
		BaseImage: "emergent-memory-python-sdk:latest",
		Tools:     []string{"run_python", "bash"},
		RepoSource: &sandbox.RepoSourceConfig{
			Type: sandbox.RepoSourceNone,
		},
	}
	sandboxMap, sandboxMapErr := sandboxCfg.ToMap()
	if sandboxMapErr != nil {
		sandboxMap = nil
	}

	// V2 uses only sandbox tools — no MCP tools at all.
	canonicalTools := []string{}

	if existing != nil {
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
			return existing, nil
		}
		return existing, nil
	}

	def := &AgentDefinition{
		ProjectID:    projectID,
		Name:         agentName,
		Description:  strPtr("CLI assistant v2 — code-generation mode using Python SDK scripts"),
		SystemPrompt: &systemPrompt,
		Model: &ModelConfig{
			Name:        "gemini-3.1-flash-lite-preview",
			Temperature: &temperature,
		},
		Tools:         canonicalTools,
		Skills:        []string{},
		FlowType:      FlowTypeSingle,
		IsDefault:     false,
		MaxSteps:      &maxSteps,
		Visibility:    VisibilityInternal,
		Config:        map[string]any{},
		SandboxConfig: sandboxMap,
	}

	if err := r.CreateDefinition(ctx, def); err != nil {
		if existing, err2 := r.FindDefinitionByName(ctx, projectID, agentName); err2 == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create %s: %w", agentName, err)
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
		AgentID:           opts.AgentID,
		Status:            RunStatusRunning,
		StartedAt:         time.Now(),
		Summary:           make(map[string]any),
		ParentRunID:       opts.ParentRunID,
		MaxSteps:          opts.MaxSteps,
		ResumedFrom:       opts.ResumedFrom,
		StepCount:         opts.InitialStepCount,
		TriggerSource:     opts.TriggerSource,
		TriggerMetadata:   opts.TriggerMetadata,
		TriggerMessage:    opts.TriggerMessage,
		Model:             opts.Model,
		AgentDefinitionID: opts.AgentDefinitionID,
		Tools:             []string{},
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
// FindParentAwaitingChild returns any agent run that is paused waiting for the given
// child run ID to complete (suspend_context->>'waiting_for_run_id' = childRunID).
// Returns nil (no error) if no such parent exists.
func (r *Repository) FindParentAwaitingChild(ctx context.Context, childRunID string) (*AgentRun, error) {
	var run AgentRun
	err := r.db.NewSelect().
		Model(&run).
		Where("status = ?", RunStatusPaused).
		Where("suspend_context->>'waiting_for_run_id' = ?", childRunID).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

func (r *Repository) FindRunByID(ctx context.Context, runID string) (*AgentRun, error) {
	run := new(AgentRun)
	err := r.db.NewSelect().
		Model(run).
		Relation("Agent").
		Where("ar.id = ?", runID).
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
// UpdateSuspendContext persists the suspend_context JSONB on an agent run row.
// Call this before PauseRun so the context is available when the run is later resumed.
func (r *Repository) UpdateSuspendContext(ctx context.Context, runID string, sc map[string]any) error {
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("suspend_context = ?", sc).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

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

// MarkRunResumed transitions a paused run to running state when a new resume run has been created.
// This prevents the old paused run from appearing stuck indefinitely.
func (r *Repository) MarkRunResumed(ctx context.Context, runID string) error {
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusRunning).
		Set("completed_at = NULL").
		Where("id = ?", runID).
		Where("status = ?", RunStatusPaused).
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

// UpdateRunModel sets the model name and optional provider on an agent run.
func (r *Repository) UpdateRunModel(ctx context.Context, runID string, model string, provider ...string) error {
	q := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("model = ?", model).
		Where("id = ?", runID)
	if len(provider) > 0 && provider[0] != "" {
		q = q.Set("provider = ?", provider[0])
	}
	_, err := q.Exec(ctx)
	return err
}

// UpdateRunTools sets the tool names on an agent run.
// Uses a raw array literal to avoid Bun serialising []string as a JSON string
// (which causes SQLSTATE 22P02 "malformed array literal" against a text[] column).
func (r *Repository) UpdateRunTools(ctx context.Context, runID string, tools []string) error {
	if len(tools) == 0 {
		return nil
	}
	// Build a Postgres array literal: ARRAY['a','b','c']
	// bun.In handles the comma-separated quoting; we wrap in ARRAY[].
	_, err := r.db.NewRaw(
		`UPDATE kb.agent_runs SET tools = ARRAY[?] WHERE id = ?`,
		bun.In(tools), runID,
	).Exec(ctx)
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

// RunLogEntry is a unified log entry merging a message or tool call from a run,
// ordered by creation time. Used by the GET /api/v1/runs/:runId/logs endpoint.
type RunLogEntry struct {
	// Kind is either "message" or "tool_call"
	Kind       string    `json:"kind"`
	StepNumber int       `json:"stepNumber"`
	CreatedAt  time.Time `json:"createdAt"`
	// Message fields (Kind == "message")
	Role    string         `json:"role,omitempty"`
	Content map[string]any `json:"content,omitempty"`
	// Tool call fields (Kind == "tool_call")
	ToolName   string         `json:"toolName,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	Output     map[string]any `json:"output,omitempty"`
	Status     string         `json:"status,omitempty"`
	DurationMs *int           `json:"durationMs,omitempty"`
}

// FindRunLogEntries returns a unified, chronologically ordered log for a run by
// merging agent_run_messages and agent_run_tool_calls. This powers the
// GET /api/v1/runs/:runId/logs streaming endpoint.
func (r *Repository) FindRunLogEntries(ctx context.Context, runID string) ([]*RunLogEntry, error) {
	msgs, err := r.FindMessagesByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("FindRunLogEntries messages: %w", err)
	}
	tcs, err := r.FindToolCallsByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("FindRunLogEntries tool_calls: %w", err)
	}

	entries := make([]*RunLogEntry, 0, len(msgs)+len(tcs))
	for _, m := range msgs {
		entries = append(entries, &RunLogEntry{
			Kind:       "message",
			StepNumber: m.StepNumber,
			CreatedAt:  m.CreatedAt,
			Role:       m.Role,
			Content:    m.Content,
		})
	}
	for _, tc := range tcs {
		entries = append(entries, &RunLogEntry{
			Kind:       "tool_call",
			StepNumber: tc.StepNumber,
			CreatedAt:  tc.CreatedAt,
			ToolName:   tc.ToolName,
			Input:      tc.Input,
			Output:     tc.Output,
			Status:     tc.Status,
			DurationMs: tc.DurationMs,
		})
	}

	// Sort by created_at ascending; break ties with messages before tool_calls
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].Kind == "message" && entries[j].Kind == "tool_call"
		}
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})

	return entries, nil
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
	var maxPendingJobs int
	if len(opts) > 0 {
		parentRunID = opts[0].ParentRunID
		triggerMessage = opts[0].TriggerMessage
		triggerMetadata = opts[0].TriggerMetadata
		maxPendingJobs = opts[0].MaxPendingJobs
	}

	run := &AgentRun{
		AgentID:         agentID,
		Status:          RunStatusQueued,
		StartedAt:       time.Now(),
		Summary:         make(map[string]any),
		ParentRunID:     parentRunID,
		TriggerMessage:  triggerMessage,
		TriggerMetadata: triggerMetadata,
		Tools:           []string{},
	}

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Queue depth check inside the transaction so the count and insert are
		// atomic. This prevents the TOCTOU race where multiple concurrent workers
		// each see count < max and all insert new jobs simultaneously, causing
		// exponential queue growth. Only enforced when MaxPendingJobs > 0.
		if maxPendingJobs > 0 {
			var count int
			err := tx.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM kb.agent_run_jobs arj
				JOIN kb.agent_runs ar ON ar.id = arj.run_id
				WHERE ar.agent_id = $1
				  AND arj.status IN ('pending', 'processing')
			`, agentID).Scan(&count)
			if err != nil {
				// Fail-open on count error so a DB hiccup doesn't halt agents.
				// Callers log the error; we proceed with the insert.
			} else if count >= maxPendingJobs {
				return &QueueFullError{
					AgentID:        agentID,
					PendingJobs:    count,
					MaxPendingJobs: maxPendingJobs,
				}
			}
		}

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
		// Unwrap QueueFullError from the transaction wrapper so callers can type-assert it.
		var qfe *QueueFullError
		if errors.As(err, &qfe) {
			return nil, qfe
		}
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
// events exist for the run.
func (r *Repository) GetRunTokenUsage(ctx context.Context, runID string) (*RunTokenUsage, error) {
	type row struct {
		TotalInput  int64   `bun:"total_input"`
		TotalOutput int64   `bun:"total_output"`
		TotalCached int64   `bun:"total_cached"`
		TotalCost   float64 `bun:"total_cost"`
		Provider    string  `bun:"provider"`
		Model       string  `bun:"model"`
	}
	var result row
	err := r.db.NewRaw(`
		SELECT
			COALESCE(SUM(text_input_tokens + image_input_tokens + video_input_tokens + audio_input_tokens), 0) AS total_input,
			COALESCE(SUM(output_tokens), 0)        AS total_output,
			COALESCE(SUM(cached_tokens), 0)        AS total_cached,
			COALESCE(SUM(estimated_cost_usd), 0.0) AS total_cost,
			COALESCE(
				(SELECT provider FROM kb.llm_usage_events
				 WHERE run_id = ?
				 GROUP BY provider ORDER BY COUNT(*) DESC LIMIT 1), ''
			) AS provider,
			COALESCE(
				(SELECT model FROM kb.llm_usage_events
				 WHERE run_id = ?
				 GROUP BY model ORDER BY COUNT(*) DESC LIMIT 1), ''
			) AS model
		FROM kb.llm_usage_events
		WHERE run_id = ?`,
		runID, runID, runID,
	).Scan(ctx, &result)
	if err != nil {
		return nil, fmt.Errorf("get run token usage: %w", err)
	}
	// Return nil when there is no recorded usage for this run.
	if result.TotalInput == 0 && result.TotalOutput == 0 && result.TotalCost == 0 {
		return nil, nil
	}
	usage := &RunTokenUsage{
		TotalInputTokens:  result.TotalInput,
		TotalOutputTokens: result.TotalOutput,
		CachedTokens:      result.TotalCached,
		EstimatedCostUSD:  result.TotalCost,
		Provider:          result.Provider,
		Model:             result.Model,
	}
	return usage, nil
}

// GetRunsTokenUsage returns aggregated LLM token counts and estimated cost for
// a batch of agent runs in a single query. The returned map is keyed by run ID.
// Runs with no usage events are absent from the map.
func (r *Repository) GetRunsTokenUsage(ctx context.Context, runIDs []string) (map[string]*RunTokenUsage, error) {
	if len(runIDs) == 0 {
		return map[string]*RunTokenUsage{}, nil
	}

	type row struct {
		RunID       string  `bun:"run_id"`
		TotalInput  int64   `bun:"total_input"`
		TotalOutput int64   `bun:"total_output"`
		TotalCached int64   `bun:"total_cached"`
		TotalCost   float64 `bun:"total_cost"`
		Provider    string  `bun:"provider"`
		Model       string  `bun:"model"`
	}

	var rows []row
	err := r.db.NewRaw(`
		SELECT
			run_id,
			COALESCE(SUM(text_input_tokens + image_input_tokens + video_input_tokens + audio_input_tokens), 0) AS total_input,
			COALESCE(SUM(output_tokens), 0)        AS total_output,
			COALESCE(SUM(cached_tokens), 0)        AS total_cached,
			COALESCE(SUM(estimated_cost_usd), 0.0) AS total_cost,
			(SELECT provider FROM kb.llm_usage_events sub
			 WHERE sub.run_id = main.run_id
			 GROUP BY provider ORDER BY COUNT(*) DESC LIMIT 1) AS provider,
			(SELECT model FROM kb.llm_usage_events sub
			 WHERE sub.run_id = main.run_id
			 GROUP BY model ORDER BY COUNT(*) DESC LIMIT 1) AS model
		FROM kb.llm_usage_events AS main
		WHERE run_id = ANY(?)
		GROUP BY run_id`,
		bun.In(runIDs),
	).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("get runs token usage: %w", err)
	}

	result := make(map[string]*RunTokenUsage, len(rows))
	for _, r := range rows {
		if r.RunID == "" {
			continue
		}
		result[r.RunID] = &RunTokenUsage{
			TotalInputTokens:  r.TotalInput,
			TotalOutputTokens: r.TotalOutput,
			CachedTokens:      r.TotalCached,
			EstimatedCostUSD:  r.TotalCost,
			Provider:          r.Provider,
			Model:             r.Model,
		}
	}
	return result, nil
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

// ============================================================================
// Agent Safeguards Repository Methods
// ============================================================================

// CountPendingJobsForAgent returns the number of pending and processing jobs
// for the given agent. Used to enforce per-agent queue depth limits.
func (r *Repository) CountPendingJobsForAgent(ctx context.Context, agentID string) (int, error) {
	var count int
	err := r.db.NewSelect().
		TableExpr("kb.agent_run_jobs AS arj").
		ColumnExpr("COUNT(*)").
		Join("JOIN kb.agent_runs AS ar ON ar.id = arj.run_id").
		Where("ar.agent_id = ?", agentID).
		Where("arj.status IN (?)", bun.In([]string{
			string(JobStatusPending),
			string(JobStatusProcessing),
		})).
		Scan(ctx, &count)
	if err != nil {
		return 0, fmt.Errorf("CountPendingJobsForAgent: %w", err)
	}
	return count, nil
}

// IncrementFailureCounter atomically increments the consecutive_failures counter
// for the given agent.
func (r *Repository) IncrementFailureCounter(ctx context.Context, agentID string) error {
	_, err := r.db.NewUpdate().
		TableExpr("kb.agents").
		Set("consecutive_failures = consecutive_failures + 1").
		Where("id = ?", agentID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("IncrementFailureCounter: %w", err)
	}
	return nil
}

// ResetFailureCounter resets the consecutive_failures counter to 0 for the
// given agent (called after a successful run).
func (r *Repository) ResetFailureCounter(ctx context.Context, agentID string) error {
	_, err := r.db.NewUpdate().
		TableExpr("kb.agents").
		Set("consecutive_failures = 0").
		Where("id = ?", agentID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("ResetFailureCounter: %w", err)
	}
	return nil
}

// DisableAgent sets enabled=false for the given agent. The reason is logged by
// the caller and persisted to the disabled_reason column.
func (r *Repository) DisableAgent(ctx context.Context, agentID string, reason string) error {
	_, err := r.db.NewUpdate().
		TableExpr("kb.agents").
		Set("enabled = false").
		Set("disabled_reason = ?", reason).
		Where("id = ?", agentID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("DisableAgent(%s): %w", reason, err)
	}
	return nil
}

// EnableAgent sets enabled=true and clears disabled_reason for the given agent.
func (r *Repository) EnableAgent(ctx context.Context, agentID string) error {
	_, err := r.db.NewUpdate().
		TableExpr("kb.agents").
		Set("enabled = true").
		Set("disabled_reason = NULL").
		Where("id = ?", agentID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("EnableAgent: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ACP Repository Methods
// ---------------------------------------------------------------------------

// FindExternalAgentDefinitions returns all agent definitions with visibility = 'external'
// for the given project. Used by ACP agent discovery endpoints.
func (r *Repository) FindExternalAgentDefinitions(ctx context.Context, projectID string) ([]*AgentDefinition, error) {
	var defs []*AgentDefinition
	err := r.db.NewSelect().
		Model(&defs).
		Where("project_id = ?", projectID).
		Where("visibility = ?", VisibilityExternal).
		Order("name ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("FindExternalAgentDefinitions: %w", err)
	}
	return defs, nil
}

// FindExternalAgentBySlug returns an external agent definition matching the given
// ACP slug within a project. Since slugs are computed (not stored), this fetches
// all external definitions and matches in-memory.
// Returns nil, nil when no matching agent is found.
func (r *Repository) FindExternalAgentBySlug(ctx context.Context, projectID, slug string) (*AgentDefinition, error) {
	defs, err := r.FindExternalAgentDefinitions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, def := range defs {
		if ACPSlugFromName(def.Name) == slug {
			return def, nil
		}
	}
	return nil, nil
}

// FindAgentDefinitionBySlug returns any agent definition (regardless of visibility)
// matching the given ACP slug within a project. Used as a fallback when the agent
// is not marked as external but should still be accessible via ACP by name.
// Returns nil, nil when no matching agent is found.
func (r *Repository) FindAgentDefinitionBySlug(ctx context.Context, projectID, slug string) (*AgentDefinition, error) {
	var defs []*AgentDefinition
	err := r.db.NewSelect().
		Model(&defs).
		Where("project_id = ?", projectID).
		Order("name ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("FindAgentDefinitionBySlug: %w", err)
	}
	for _, def := range defs {
		if ACPSlugFromName(def.Name) == slug {
			return def, nil
		}
	}
	return nil, nil
}

// GetAgentStatusMetrics computes live metrics for an agent definition based on
// runs from the last 30 days: average tokens per run, average duration in seconds,
// and success rate (fraction of terminal runs that succeeded).
// Returns nil when no runs exist in the window.
func (r *Repository) GetAgentStatusMetrics(ctx context.Context, agentDefID string) (*AgentStatusMetrics, error) {
	since := time.Now().Add(-30 * 24 * time.Hour)

	// Compute success rate and average duration from agent_runs.
	// We need the agent_id (legacy Agent), not the definition ID directly.
	// agent_runs.agent_id references kb.agents.id, and agents have a definition_id.
	// For simplicity we query runs whose agent's definition matches.
	type runStats struct {
		TotalRuns   int64    `bun:"total_runs"`
		SuccessRuns int64    `bun:"success_runs"`
		AvgDuration *float64 `bun:"avg_duration"`
	}
	var rs runStats
	err := r.db.NewSelect().
		TableExpr("kb.agent_runs AS ar").
		Join("JOIN kb.agents AS a ON a.id = ar.agent_id").
		ColumnExpr("COUNT(*) AS total_runs").
		ColumnExpr("COUNT(*) FILTER (WHERE ar.status = 'completed') AS success_runs").
		ColumnExpr("AVG(ar.duration_ms) FILTER (WHERE ar.duration_ms IS NOT NULL) AS avg_duration").
		Where("a.definition_id = ?", agentDefID).
		Where("ar.created_at >= ?", since).
		Where("ar.status IN (?)", bun.In([]string{"completed", "failed", "cancelled", "skipped"})).
		Scan(ctx, &rs)
	if err != nil {
		return nil, fmt.Errorf("GetAgentStatusMetrics runs: %w", err)
	}
	if rs.TotalRuns == 0 {
		return nil, nil
	}

	metrics := &AgentStatusMetrics{}

	// Success rate
	rate := float64(rs.SuccessRuns) / float64(rs.TotalRuns)
	metrics.SuccessRate = &rate

	// Average duration (ms → seconds)
	if rs.AvgDuration != nil {
		secs := *rs.AvgDuration / 1000.0
		metrics.AvgRunTimeSeconds = &secs
	}

	// Average tokens from llm_usage_events
	type tokenStats struct {
		AvgTokens *float64 `bun:"avg_tokens"`
	}
	var ts tokenStats
	err = r.db.NewRaw(`
		SELECT AVG(run_total) AS avg_tokens FROM (
			SELECT lue.run_id,
				SUM(lue.text_input_tokens + lue.image_input_tokens + lue.video_input_tokens + lue.audio_input_tokens + lue.output_tokens) AS run_total
			FROM kb.llm_usage_events lue
			JOIN kb.agent_runs ar ON ar.id = lue.run_id
			JOIN kb.agents a ON a.id = ar.agent_id
			WHERE a.definition_id = ?
			  AND ar.created_at >= ?
			GROUP BY lue.run_id
		) sub`, agentDefID, since).Scan(ctx, &ts)
	if err != nil {
		return nil, fmt.Errorf("GetAgentStatusMetrics tokens: %w", err)
	}
	metrics.AvgRunTokens = ts.AvgTokens

	return metrics, nil
}

// CreateACPSession inserts a new ACP session record.
func (r *Repository) CreateACPSession(ctx context.Context, session *ACPSession) error {
	_, err := r.db.NewInsert().
		Model(session).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("CreateACPSession: %w", err)
	}
	return nil
}

// GetACPSession returns an ACP session by ID, scoped to the given project.
// Returns nil, nil when not found.
func (r *Repository) GetACPSession(ctx context.Context, projectID, sessionID string) (*ACPSession, error) {
	session := new(ACPSession)
	err := r.db.NewSelect().
		Model(session).
		Where("id = ?", sessionID).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("GetACPSession: %w", err)
	}
	return session, nil
}

// ListACPSessions returns all ACP sessions for a project, ordered by created_at descending.
func (r *Repository) ListACPSessions(ctx context.Context, projectID string) ([]*ACPSession, error) {
	var sessions []*ACPSession
	err := r.db.NewSelect().
		Model(&sessions).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListACPSessions: %w", err)
	}
	return sessions, nil
}

// ListSessionRunsByProjectID returns all agent runs that belong to any ACP session
// in the given project, with Agent relation loaded, ordered by created_at ASC.
// Results are grouped by acp_session_id for building history URLs.
func (r *Repository) ListSessionRunsByProjectID(ctx context.Context, projectID string) (map[string][]*AgentRun, error) {
	var runs []*AgentRun
	err := r.db.NewSelect().
		Model(&runs).
		Relation("Agent").
		Join("JOIN kb.acp_sessions AS s ON s.id = ar.acp_session_id").
		Where("s.project_id = ?", projectID).
		Where("ar.acp_session_id IS NOT NULL").
		Order("ar.created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListSessionRunsByProjectID: %w", err)
	}
	grouped := make(map[string][]*AgentRun, len(runs))
	for _, run := range runs {
		if run.ACPSessionID != nil {
			sid := *run.ACPSessionID
			grouped[sid] = append(grouped[sid], run)
		}
	}
	return grouped, nil
}

// ACPSessionStats holds aggregated stats for a single ACP session.
type ACPSessionStats struct {
	SessionID    string  `bun:"session_id"`
	MessageCount int64   `bun:"message_count"`
	TotalTokens  int64   `bun:"total_tokens"`
	TotalCostUSD float64 `bun:"total_cost_usd"`
}

// GetSessionStatsByProjectID returns aggregated message count, token usage, and
// estimated cost for every ACP session in the given project.
func (r *Repository) GetSessionStatsByProjectID(ctx context.Context, projectID string) (map[string]*ACPSessionStats, error) {
	var rows []ACPSessionStats
	err := r.db.NewRaw(`
		SELECT
			s.id                                                            AS session_id,
			COUNT(DISTINCT CASE WHEN m.role = 'user' THEN m.id END)        AS message_count,
			COALESCE(SUM(u.text_input_tokens + u.output_tokens), 0)        AS total_tokens,
			COALESCE(SUM(u.estimated_cost_usd), 0)                         AS total_cost_usd
		FROM kb.acp_sessions s
		LEFT JOIN kb.agent_runs     ar ON ar.acp_session_id = s.id
		LEFT JOIN kb.agent_run_messages m ON m.run_id = ar.id
		LEFT JOIN kb.llm_usage_events   u  ON u.run_id  = ar.id
		WHERE s.project_id = ?
		GROUP BY s.id
	`, projectID).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("GetSessionStatsByProjectID: %w", err)
	}
	out := make(map[string]*ACPSessionStats, len(rows))
	for i := range rows {
		out[rows[i].SessionID] = &rows[i]
	}
	return out, nil
}

// GetSessionRunHistory returns all agent runs linked to the given ACP session,
// ordered by created_at ascending (oldest first).
func (r *Repository) GetSessionRunHistory(ctx context.Context, sessionID string) ([]*AgentRun, error) {
	var runs []*AgentRun
	err := r.db.NewSelect().
		Model(&runs).
		Relation("Agent").
		Where("ar.acp_session_id = ?", sessionID).
		Order("ar.created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetSessionRunHistory: %w", err)
	}
	return runs, nil
}

// InsertACPRunEvent persists an SSE event emitted during an ACP run.
func (r *Repository) InsertACPRunEvent(ctx context.Context, event *ACPRunEvent) error {
	_, err := r.db.NewInsert().
		Model(event).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("InsertACPRunEvent: %w", err)
	}
	return nil
}

// GetACPRunEvents returns all persisted SSE events for a run, ordered by
// created_at ascending. Used to serve the events replay endpoint.
func (r *Repository) GetACPRunEvents(ctx context.Context, runID string) ([]*ACPRunEvent, error) {
	var events []*ACPRunEvent
	err := r.db.NewSelect().
		Model(&events).
		Where("run_id = ?", runID).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetACPRunEvents: %w", err)
	}
	return events, nil
}

// GetACPRunEventsByRunIDs returns all persisted SSE events for the given run IDs
// in a single query, grouped by run ID. Used to bulk-load events for session history.
func (r *Repository) GetACPRunEventsByRunIDs(ctx context.Context, runIDs []string) (map[string][]*ACPRunEvent, error) {
	if len(runIDs) == 0 {
		return map[string][]*ACPRunEvent{}, nil
	}
	var events []*ACPRunEvent
	err := r.db.NewSelect().
		Model(&events).
		Where("run_id IN (?)", bun.In(runIDs)).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetACPRunEventsByRunIDs: %w", err)
	}
	grouped := make(map[string][]*ACPRunEvent, len(runIDs))
	for _, e := range events {
		grouped[e.RunID] = append(grouped[e.RunID], e)
	}
	return grouped, nil
}

// SetRunCancelling transitions a run to the "cancelling" intermediate state.
// This is the first step of the ACP two-step cancel protocol: the intent is
// acknowledged but the executor hasn't stopped yet.
func (r *Repository) SetRunCancelling(ctx context.Context, runID string) error {
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("status = ?", RunStatusCancelling).
		Where("id = ?", runID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("SetRunCancelling: %w", err)
	}
	return nil
}

// UpdateACPSessionTitle sets the title field on an ACP session.
func (r *Repository) UpdateACPSessionTitle(ctx context.Context, projectID, sessionID, title string) error {
	_, err := r.db.NewUpdate().
		Model((*ACPSession)(nil)).
		Set("title = ?", title).
		Set("updated_at = now()").
		Where("id = ?", sessionID).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("UpdateACPSessionTitle: %w", err)
	}
	return nil
}

// ConversationHistoryItem represents one event in the unified session timeline.
type ConversationHistoryItem struct {
	// Kind is one of: "user_message", "assistant_message", "tool_call", "tool_result", "run_start", "run_end"
	Kind string `json:"kind"`
	// RunID is the agent run this item belongs to.
	RunID string `json:"run_id"`
	// StepNumber within the run (0 for run_start/run_end).
	StepNumber int `json:"step_number"`
	// CreatedAt is the wall-clock time of this item.
	CreatedAt time.Time `json:"created_at"`

	// Fields populated for user_message / assistant_message
	Role    string         `json:"role,omitempty"`
	Content map[string]any `json:"content,omitempty"`

	// Fields populated for tool_call / tool_result
	ToolName   string         `json:"tool_name,omitempty"`
	ToolInput  map[string]any `json:"tool_input,omitempty"`
	ToolOutput map[string]any `json:"tool_output,omitempty"`
	ToolStatus string         `json:"tool_status,omitempty"` // completed | error
	DurationMs *int           `json:"duration_ms,omitempty"`

	// Fields populated for run_start / run_end
	RunStatus    string     `json:"run_status,omitempty"`
	RunModel     *string    `json:"run_model,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
}

// GetConversationFullHistory returns the unified ordered timeline for an ACP session:
// run lifecycle events, all LLM messages (user + assistant), and all tool invocations.
// Items are ordered by (run.created_at asc, item.step_number asc, item.created_at asc).
func (r *Repository) GetConversationFullHistory(ctx context.Context, acpSessionID string) ([]*ConversationHistoryItem, error) {
	// Fetch runs ordered by start time
	var runs []*AgentRun
	if err := r.db.NewSelect().
		Model(&runs).
		Where("ar.acp_session_id = ?", acpSessionID).
		OrderExpr("ar.created_at ASC").
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("GetConversationFullHistory runs: %w", err)
	}
	if len(runs) == 0 {
		return []*ConversationHistoryItem{}, nil
	}

	runIDs := make([]string, len(runs))
	for i, r := range runs {
		runIDs[i] = r.ID
	}

	// Fetch all messages for these runs
	var messages []*AgentRunMessage
	if err := r.db.NewSelect().
		Model(&messages).
		Where("arm.run_id IN (?)", bun.In(runIDs)).
		OrderExpr("arm.step_number ASC, arm.created_at ASC").
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("GetConversationFullHistory messages: %w", err)
	}

	// Fetch all tool calls for these runs
	var toolCalls []*AgentRunToolCall
	if err := r.db.NewSelect().
		Model(&toolCalls).
		Where("artc.run_id IN (?)", bun.In(runIDs)).
		OrderExpr("artc.step_number ASC, artc.created_at ASC").
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("GetConversationFullHistory tool_calls: %w", err)
	}

	// Group messages and tool calls by run ID
	msgsByRun := make(map[string][]*AgentRunMessage, len(runs))
	for _, m := range messages {
		msgsByRun[m.RunID] = append(msgsByRun[m.RunID], m)
	}
	tcByRun := make(map[string][]*AgentRunToolCall, len(runs))
	for _, tc := range toolCalls {
		tcByRun[tc.RunID] = append(tcByRun[tc.RunID], tc)
	}

	var items []*ConversationHistoryItem

	for _, run := range runs {
		// run_start
		items = append(items, &ConversationHistoryItem{
			Kind:      "run_start",
			RunID:     run.ID,
			CreatedAt: run.CreatedAt,
			RunStatus: string(run.Status),
			RunModel:  run.Model,
		})

		// Merge messages and tool calls into a single ordered slice by step_number + created_at
		// We interleave them: for each step, tool calls come after messages in same step
		type timelineEntry struct {
			step      int
			createdAt time.Time
			item      *ConversationHistoryItem
		}
		var entries []timelineEntry

		for _, m := range msgsByRun[run.ID] {
			entries = append(entries, timelineEntry{
				step:      m.StepNumber,
				createdAt: m.CreatedAt,
				item: &ConversationHistoryItem{
					Kind:       "message",
					RunID:      run.ID,
					StepNumber: m.StepNumber,
					CreatedAt:  m.CreatedAt,
					Role:       m.Role,
					Content:    m.Content,
				},
			})
		}
		for _, tc := range tcByRun[run.ID] {
			kind := "tool_call"
			entries = append(entries, timelineEntry{
				step:      tc.StepNumber,
				createdAt: tc.CreatedAt,
				item: &ConversationHistoryItem{
					Kind:       kind,
					RunID:      run.ID,
					StepNumber: tc.StepNumber,
					CreatedAt:  tc.CreatedAt,
					ToolName:   tc.ToolName,
					ToolInput:  tc.Input,
					ToolOutput: tc.Output,
					ToolStatus: tc.Status,
					DurationMs: tc.DurationMs,
				},
			})
		}

		// Sort by step then created_at
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].step != entries[j].step {
				return entries[i].step < entries[j].step
			}
			return entries[i].createdAt.Before(entries[j].createdAt)
		})

		for _, e := range entries {
			items = append(items, e.item)
		}

		// run_end
		items = append(items, &ConversationHistoryItem{
			Kind:         "run_end",
			RunID:        run.ID,
			CreatedAt:    run.CreatedAt,
			RunStatus:    string(run.Status),
			CompletedAt:  run.CompletedAt,
			ErrorMessage: run.ErrorMessage,
		})
	}

	return items, nil
}

// GetConversationFullHistoryRaw implements mcp.SessionHistoryProvider.
// Returns the same unified timeline as GetConversationFullHistory but serialised
// as []map[string]any so the mcp package can consume it without importing agents.
func (r *Repository) GetConversationFullHistoryRaw(ctx context.Context, acpSessionID string) ([]map[string]any, error) {
	items, err := r.GetConversationFullHistory(ctx, acpSessionID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m := map[string]any{
			"kind":        item.Kind,
			"run_id":      item.RunID,
			"step_number": item.StepNumber,
			"created_at":  item.CreatedAt,
		}
		if item.Role != "" {
			m["role"] = item.Role
		}
		if item.Content != nil {
			m["content"] = item.Content
		}
		if item.ToolName != "" {
			m["tool_name"] = item.ToolName
			m["tool_input"] = item.ToolInput
			m["tool_output"] = item.ToolOutput
			m["tool_status"] = item.ToolStatus
		}
		if item.DurationMs != nil {
			m["duration_ms"] = *item.DurationMs
		}
		if item.RunStatus != "" {
			m["run_status"] = item.RunStatus
		}
		if item.CompletedAt != nil {
			m["completed_at"] = item.CompletedAt
		}
		if item.ErrorMessage != nil {
			m["error_message"] = *item.ErrorMessage
		}
		out = append(out, m)
	}
	return out, nil
}

// EnsureConversationACPSession returns the acp_session_id linked to the given
// chat conversation, creating a new ACPSession (and persisting the FK back onto
// the conversation row) if one does not yet exist.
//
// The chat_conversations table must have an acp_session_id uuid column (added by
// migration 00115) and kb.acp_sessions must exist (added earlier).
func (r *Repository) EnsureConversationACPSession(ctx context.Context, conversationID, projectID string, agentName *string) (string, error) {
	// 1. Fast path: read existing session ID from the conversation row.
	var existing struct {
		ACPSessionID *string `bun:"acp_session_id"`
	}
	err := r.db.NewSelect().
		TableExpr("kb.chat_conversations").
		ColumnExpr("acp_session_id::text").
		Where("id = ?", conversationID).
		Scan(ctx, &existing)
	if err != nil {
		return "", fmt.Errorf("EnsureConversationACPSession read: %w", err)
	}
	if existing.ACPSessionID != nil && *existing.ACPSessionID != "" {
		return *existing.ACPSessionID, nil
	}

	// 2. Create a new ACP session.
	session := &ACPSession{
		ProjectID: projectID,
		AgentName: agentName,
	}
	if err := r.CreateACPSession(ctx, session); err != nil {
		return "", fmt.Errorf("EnsureConversationACPSession create: %w", err)
	}

	// 3. Persist the FK back onto the conversation row.
	_, err = r.db.NewUpdate().
		TableExpr("kb.chat_conversations").
		Set("acp_session_id = ?", session.ID).
		Where("id = ?", conversationID).
		Exec(ctx)
	if err != nil {
		// Non-fatal: session was created, just the backlink failed.
		// Log and continue — the session ID is still usable.
		return session.ID, nil
	}

	return session.ID, nil
}

// UpdateRunACPSessionID sets the acp_session_id column on an agent run.
func (r *Repository) UpdateRunACPSessionID(ctx context.Context, runID, sessionID string) error {
	_, err := r.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("acp_session_id = ?", sessionID).
		Where("id = ?", runID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("UpdateRunACPSessionID: %w", err)
	}
	return nil
}

// --- Analytics store methods ---

// RunStatsRow is an intermediate struct for scanning per-agent run aggregates.
type RunStatsRow struct {
	AgentName       string  `bun:"agent_name"`
	Total           int64   `bun:"total"`
	Success         int64   `bun:"success"`
	Failed          int64   `bun:"failed"`
	Errored         int64   `bun:"errored"`
	AvgDurationMs   float64 `bun:"avg_duration_ms"`
	MaxDurationMs   int64   `bun:"max_duration_ms"`
	AvgCostUSD      float64 `bun:"avg_cost_usd"`
	TotalCostUSD    float64 `bun:"total_cost_usd"`
	AvgInputTokens  float64 `bun:"avg_input_tokens"`
	AvgOutputTokens float64 `bun:"avg_output_tokens"`
}

// GetRunStatsOverview returns aggregate run counts/cost for a project within a time window.
func (r *Repository) GetRunStatsOverview(ctx context.Context, projectID string, agentID *string, since, until time.Time) ([]RunStatsRow, error) {
	agentFilter := ""
	args := []interface{}{projectID, since, until}
	if agentID != nil {
		agentFilter = " AND ar.agent_id = ?"
		args = append(args, *agentID)
	}
	var rows []RunStatsRow
	err := r.db.NewRaw(`
		SELECT
			a.name                                                   AS agent_name,
			COUNT(*)                                                  AS total,
			COUNT(*) FILTER (WHERE ar.status = 'completed')          AS success,
			COUNT(*) FILTER (WHERE ar.status = 'failed')             AS failed,
			COUNT(*) FILTER (WHERE ar.status = 'cancelled')          AS errored,
			COALESCE(AVG(ar.duration_ms), 0)                         AS avg_duration_ms,
			COALESCE(MAX(ar.duration_ms), 0)                         AS max_duration_ms,
			COALESCE(AVG(u.total_cost), 0)                           AS avg_cost_usd,
			COALESCE(SUM(u.total_cost), 0)                           AS total_cost_usd,
			COALESCE(AVG(u.total_input), 0)                          AS avg_input_tokens,
			COALESCE(AVG(u.total_output), 0)                         AS avg_output_tokens
		FROM kb.agent_runs ar
		JOIN kb.agents a ON a.id = ar.agent_id
		LEFT JOIN (
			SELECT run_id,
				SUM(estimated_cost_usd) AS total_cost,
				SUM(text_input_tokens + image_input_tokens + video_input_tokens + audio_input_tokens) AS total_input,
				SUM(output_tokens) AS total_output
			FROM kb.llm_usage_events
			GROUP BY run_id
		) u ON u.run_id = ar.id
		WHERE a.project_id = ?
		  AND ar.started_at >= ?
		  AND ar.started_at <= ?`+agentFilter+`
		GROUP BY a.name
		ORDER BY total DESC`,
		args...,
	).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("GetRunStatsOverview: %w", err)
	}
	return rows, nil
}

// RunStatsErrorRow is an intermediate struct for scanning top error messages.
type RunStatsErrorRow struct {
	Message string `bun:"message"`
	Count   int64  `bun:"cnt"`
}

// GetRunStatsTopErrors returns the most frequent error messages within the window.
func (r *Repository) GetRunStatsTopErrors(ctx context.Context, projectID string, agentID *string, since, until time.Time, limit int) ([]RunStatsErrorRow, error) {
	agentFilter := ""
	// Base args for first half of UNION (runs) and second half (tool calls).
	// Both halves use: projectID, since, until [, agentID]
	halfArgs := []interface{}{projectID, since, until}
	if agentID != nil {
		agentFilter = " AND ar.agent_id = ?"
		halfArgs = append(halfArgs, *agentID)
	}
	// Full args = first half + second half + limit
	allArgs := append(append(halfArgs, halfArgs...), limit)
	var rows []RunStatsErrorRow
	err := r.db.NewRaw(`
		SELECT message, COUNT(*) AS cnt FROM (
			SELECT ar.error_message AS message
			FROM kb.agent_runs ar
			JOIN kb.agents a ON a.id = ar.agent_id
			WHERE a.project_id = ?
			  AND ar.started_at >= ?
			  AND ar.started_at <= ?
			  AND ar.error_message IS NOT NULL`+agentFilter+`
			UNION ALL
			SELECT tc.output->>'error' AS message
			FROM kb.agent_run_tool_calls tc
			JOIN kb.agent_runs ar ON ar.id = tc.run_id
			JOIN kb.agents a ON a.id = ar.agent_id
			WHERE a.project_id = ?
			  AND ar.started_at >= ?
			  AND ar.started_at <= ?
			  AND tc.status = 'failed'
			  AND tc.output->>'error' IS NOT NULL`+agentFilter+`
		) AS errs
		WHERE message IS NOT NULL AND message <> ''
		GROUP BY message
		ORDER BY cnt DESC
		LIMIT ?`,
		allArgs...,
	).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("GetRunStatsTopErrors: %w", err)
	}
	return rows, nil
}

// RunStatsToolRow is an intermediate struct for scanning per-tool aggregates.
type RunStatsToolRow struct {
	ToolName      string  `bun:"tool_name"`
	Total         int64   `bun:"total"`
	Success       int64   `bun:"success"`
	Failed        int64   `bun:"failed"`
	AvgDurationMs float64 `bun:"avg_duration_ms"`
	MaxDurationMs int64   `bun:"max_duration_ms"`
}

// GetRunStatsTools returns per-tool call aggregates within the time window.
func (r *Repository) GetRunStatsTools(ctx context.Context, projectID string, agentID *string, since, until time.Time) ([]RunStatsToolRow, error) {
	agentFilter := ""
	args := []interface{}{projectID, since, until}
	if agentID != nil {
		agentFilter = " AND ar.agent_id = ?"
		args = append(args, *agentID)
	}
	var rows []RunStatsToolRow
	err := r.db.NewRaw(`
		SELECT
			tc.tool_name,
			COUNT(*)                                              AS total,
			COUNT(*) FILTER (WHERE tc.status = 'completed')      AS success,
			COUNT(*) FILTER (WHERE tc.status = 'failed')         AS failed,
			COALESCE(AVG(tc.duration_ms), 0)                     AS avg_duration_ms,
			COALESCE(MAX(tc.duration_ms), 0)                     AS max_duration_ms
		FROM kb.agent_run_tool_calls tc
		JOIN kb.agent_runs ar ON ar.id = tc.run_id
		JOIN kb.agents a ON a.id = ar.agent_id
		WHERE a.project_id = ?
		  AND ar.started_at >= ?
		  AND ar.started_at <= ?`+agentFilter+`
		GROUP BY tc.tool_name
		ORDER BY total DESC`,
		args...,
	).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("GetRunStatsTools: %w", err)
	}
	return rows, nil
}

// RunStatsHourRow is an intermediate struct for scanning hourly run counts.
type RunStatsHourRow struct {
	Hour      time.Time `bun:"hour"`
	AgentName string    `bun:"agent_name"`
	Runs      int64     `bun:"runs"`
}

// GetRunStatsTimeSeries returns hourly run counts by agent within the time window.
func (r *Repository) GetRunStatsTimeSeries(ctx context.Context, projectID string, agentID *string, since, until time.Time) ([]RunStatsHourRow, error) {
	agentFilter := ""
	args := []interface{}{projectID, since, until}
	if agentID != nil {
		agentFilter = " AND ar.agent_id = ?"
		args = append(args, *agentID)
	}
	var rows []RunStatsHourRow
	err := r.db.NewRaw(`
		SELECT
			date_trunc('hour', ar.started_at) AS hour,
			a.name                             AS agent_name,
			COUNT(*)                           AS runs
		FROM kb.agent_runs ar
		JOIN kb.agents a ON a.id = ar.agent_id
		WHERE a.project_id = ?
		  AND ar.started_at >= ?
		  AND ar.started_at <= ?`+agentFilter+`
		GROUP BY hour, a.name
		ORDER BY hour ASC`,
		args...,
	).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("GetRunStatsTimeSeries: %w", err)
	}
	return rows, nil
}

// RunSessionRow is an intermediate struct for scanning session-level aggregates.
type RunSessionRow struct {
	Platform      string    `bun:"platform"`
	ChannelID     string    `bun:"channel_id"`
	ThreadID      string    `bun:"thread_id"`
	TotalRuns     int64     `bun:"total_runs"`
	ActiveRuns    int64     `bun:"active_runs"`
	LastRunAt     time.Time `bun:"last_run_at"`
	AvgDurationMs float64   `bun:"avg_duration_ms"`
	TotalCostUSD  float64   `bun:"total_cost_usd"`
}

// GetRunSessionStats returns session-level analytics grouped by trigger metadata.
// A "session" is the unique (platform, channelId, threadId) tuple from trigger_metadata.
func (r *Repository) GetRunSessionStats(ctx context.Context, projectID string, platform *string, since, until time.Time, topN int) ([]RunSessionRow, error) {
	platformFilter := ""
	args := []interface{}{projectID, since, until}
	if platform != nil {
		platformFilter = " AND ar.trigger_metadata->>'platform' = ?"
		args = append(args, *platform)
	}
	args = append(args, topN)
	var rows []RunSessionRow
	err := r.db.NewRaw(`
		SELECT
			COALESCE(ar.trigger_metadata->>'platform', 'unknown')  AS platform,
			COALESCE(ar.trigger_metadata->>'channelId', '')        AS channel_id,
			COALESCE(ar.trigger_metadata->>'threadId', '')         AS thread_id,
			COUNT(*)                                               AS total_runs,
			COUNT(*) FILTER (WHERE ar.status IN ('working','submitted','input-required')) AS active_runs,
			MAX(ar.started_at)                                     AS last_run_at,
			COALESCE(AVG(ar.duration_ms), 0)                       AS avg_duration_ms,
			COALESCE(SUM(u.total_cost), 0)                         AS total_cost_usd
		FROM kb.agent_runs ar
		JOIN kb.agents a ON a.id = ar.agent_id
		LEFT JOIN (
			SELECT run_id, SUM(estimated_cost_usd) AS total_cost
			FROM kb.llm_usage_events
			GROUP BY run_id
		) u ON u.run_id = ar.id
		WHERE a.project_id = ?
		  AND ar.started_at >= ?
		  AND ar.started_at <= ?
		  AND ar.trigger_metadata IS NOT NULL
		  AND ar.trigger_metadata->>'platform' IS NOT NULL`+platformFilter+`
		GROUP BY platform, channel_id, thread_id
		ORDER BY total_runs DESC
		LIMIT ?`,
		args...,
	).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("GetRunSessionStats: %w", err)
	}
	return rows, nil
}
