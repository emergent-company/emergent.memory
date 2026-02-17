package agents

import (
	"context"
	"database/sql"
	"errors"
	"time"

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

// GetRecentRuns returns recent runs for an agent
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

// CreateRunWithOptions creates a new agent run with coordination options.
func (r *Repository) CreateRunWithOptions(ctx context.Context, opts CreateRunOptions) (*AgentRun, error) {
	run := &AgentRun{
		AgentID:     opts.AgentID,
		Status:      RunStatusRunning,
		StartedAt:   time.Now(),
		Summary:     make(map[string]any),
		ParentRunID: opts.ParentRunID,
		MaxSteps:    opts.MaxSteps,
		ResumedFrom: opts.ResumedFrom,
		StepCount:   opts.InitialStepCount,
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
