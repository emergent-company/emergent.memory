package agents

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// activeRunChecker reports whether a non-terminal agent run already exists for
// the given agent and reaction target object. Implemented by *Repository.
type activeRunChecker interface {
	HasActiveRunForAgentObject(ctx context.Context, agentID, objectID, objectType string) (bool, error)
}

// TriggerService manages trigger registration for agents.
// Scheduling config lives on Agent (runtime entity), not AgentDefinition (config).
// It registers cron schedules in the scheduler and provides hooks for event-driven triggers.
type TriggerService struct {
	scheduler *scheduler.Scheduler
	executor  *AgentExecutor
	repo      *Repository
	events    *events.Service
	log       *slog.Logger

	// workObjects is the graph surface used to resolve the HEAD version at
	// dispatch and to perform the claim transition. Nil in unit tests that only
	// exercise trigger registration and the legacy inline path.
	workObjects WorkObjectStore

	// activeRuns backs ConcurrencyStrategy=skip. nil fails open (no checker wired,
	// e.g. unit tests).
	activeRuns activeRunChecker
	// dispatch executes a matched reaction trigger. Overridable in tests.
	dispatch func(ctx context.Context, agent *Agent, projectID, objectType, objectID string) error

	// mu protects eventListeners
	mu             sync.RWMutex
	eventListeners map[string][]*Agent // eventKey -> agents
}

// SetWorkObjectStore injects the graph work-object surface after construction.
// It is optional: without it, object-driven dispatch falls back to the legacy
// inline path (or skips when a HEAD lookup is required).
func (ts *TriggerService) SetWorkObjectStore(store WorkObjectStore) {
	ts.workObjects = store
}

// NewTriggerService creates a new TriggerService.
func NewTriggerService(
	sched *scheduler.Scheduler,
	executor *AgentExecutor,
	repo *Repository,
	eventService *events.Service,
	log *slog.Logger,
) *TriggerService {
	ts := &TriggerService{
		scheduler:      sched,
		executor:       executor,
		repo:           repo,
		events:         eventService,
		log:            log.With(logger.Scope("agents.triggers")),
		eventListeners: make(map[string][]*Agent),
	}
	if repo != nil {
		ts.activeRuns = repo
	}
	ts.dispatch = func(ctx context.Context, agent *Agent, projectID, objectType, objectID string) error {
		return ts.executeTriggeredAgent(ctx, agent.ID, projectID, objectType, objectID)
	}

	// Subscribe to all events
	if ts.events != nil {
		ts.events.Subscribe("*", ts.onEntityEvent)
	}

	return ts
}

// onEntityEvent handles incoming events from the global event bus.
//
// The historical global "drop all agent-originated events" gate has moved into
// per-agent matching (see shouldTriggerAgentForActor), so each ReactionConfig's
// IgnoreAgentTriggered/IgnoreSelfTriggered flags are honoured. The originating
// actor is carried down so an agent that opts in can be told apart from another.
func (ts *TriggerService) onEntityEvent(evt events.EntityEvent) {
	// Map entity event type to reaction event type
	var reactionType ReactionEventType
	switch evt.Type {
	case events.EventTypeCreated:
		reactionType = EventTypeCreated
	case events.EventTypeUpdated:
		reactionType = EventTypeUpdated
	case events.EventTypeDeleted:
		reactionType = EventTypeDeleted
	default:
		return // Unsupported event type
	}

	// Determine object type. Fallback to the entity type string
	objType := string(evt.Entity)
	if evt.ObjectType != "" {
		objType = evt.ObjectType
	}

	// For single entity events
	if evt.ID != nil {
		input := map[string]any{
			"id": *evt.ID,
		}
		if evt.Version != nil {
			input["version"] = *evt.Version
		}
		if evt.Data != nil {
			input["data"] = evt.Data
		}

		ts.handleEvent(context.Background(), objType, reactionType, evt.ProjectID, input, evt.Actor)
	}

	// For batch entity events
	if len(evt.IDs) > 0 {
		for _, id := range evt.IDs {
			input := map[string]any{
				"id": id,
			}
			if evt.Data != nil {
				input["data"] = evt.Data
			}
			ts.handleEvent(context.Background(), objType, reactionType, evt.ProjectID, input, evt.Actor)
		}
	}
}

// triggerTaskName returns the scheduler task name for an agent.
func triggerTaskName(agentID string) string {
	return "agent:" + agentID
}

// eventKey builds a lookup key for event routing.
// Format: "objectType:eventType" (e.g., "document:created").
func eventKey(objectType string, event ReactionEventType) string {
	return objectType + ":" + string(event)
}

// SyncAllTriggers scans all enabled agents and registers their triggers.
// Called on server startup.
func (ts *TriggerService) SyncAllTriggers(ctx context.Context) error {
	// Sync scheduled agents (cron)
	cronAgents, err := ts.repo.FindEnabledByTriggerType(ctx, TriggerTypeSchedule)
	if err != nil {
		return fmt.Errorf("failed to load scheduled agents: %w", err)
	}

	// Sync reaction agents (events)
	reactionAgents, err := ts.repo.FindEnabledByTriggerType(ctx, TriggerTypeReaction)
	if err != nil {
		return fmt.Errorf("failed to load reaction agents: %w", err)
	}

	ts.log.Info("syncing agent triggers",
		slog.Int("scheduled", len(cronAgents)),
		slog.Int("reaction", len(reactionAgents)),
	)

	cronCount := 0
	for _, agent := range cronAgents {
		if agent.CronSchedule == "" {
			ts.log.Warn("scheduled agent has no cron expression, skipping",
				slog.String("agent", agent.Name),
				slog.String("agent_id", agent.ID),
			)
			continue
		}
		if err := ts.registerCronTrigger(agent); err != nil {
			ts.log.Error("failed to register cron trigger",
				slog.String("agent", agent.Name),
				slog.String("agent_id", agent.ID),
				slog.String("schedule", agent.CronSchedule),
				slog.String("error", err.Error()),
			)
			continue
		}
		cronCount++
	}

	eventCount := 0
	for _, agent := range reactionAgents {
		if agent.ReactionConfig == nil || len(agent.ReactionConfig.Events) == 0 {
			ts.log.Warn("reaction agent has no reaction config or events, skipping",
				slog.String("agent", agent.Name),
				slog.String("agent_id", agent.ID),
			)
			continue
		}
		ts.registerEventTrigger(agent)
		eventCount++
	}

	ts.log.Info("agent triggers synced",
		slog.Int("cron", cronCount),
		slog.Int("event", eventCount),
	)
	return nil
}

// registerCronTrigger registers a cron schedule for an agent.
func (ts *TriggerService) registerCronTrigger(agent *Agent) error {
	taskName := triggerTaskName(agent.ID)

	// Validate minimum cron interval before registering.
	minMinutes := ts.executor.safeguards.MinCronIntervalMinutes
	if minMinutes <= 0 {
		minMinutes = 15 // safe fallback
	}
	if err := validateCronInterval(agent.CronSchedule, minMinutes); err != nil {
		return fmt.Errorf("cron validation failed for agent %q: %w", agent.Name, err)
	}

	// Capture for closure
	agentID := agent.ID
	projectID := agent.ProjectID

	// The scheduler uses seconds-precision (6-field) cron, while agent
	// schedules are standard 5-field expressions (validated above). Prefix a
	// zero seconds field so the scheduler parses it.
	schedule := "0 " + agent.CronSchedule
	err := ts.scheduler.AddCronTask(taskName, schedule, func(ctx context.Context) error {
		return ts.executeTriggeredAgent(ctx, agentID, projectID, "", "")
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", agent.CronSchedule, err)
	}

	ts.log.Info("registered cron trigger",
		slog.String("agent", agent.Name),
		slog.String("agent_id", agent.ID),
		slog.String("project_id", agent.ProjectID),
		slog.String("schedule", agent.CronSchedule),
	)
	return nil
}

// registerEventTrigger registers event listeners for a reaction agent.
// It registers listeners for all combinations of object types and events from the ReactionConfig.
func (ts *TriggerService) registerEventTrigger(agent *Agent) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if agent.ReactionConfig == nil {
		return
	}

	rc := agent.ReactionConfig
	objectTypes := rc.ObjectTypes
	if len(objectTypes) == 0 {
		// Wildcard: listen for all object types via a special key
		objectTypes = []string{"*"}
	}

	for _, objType := range objectTypes {
		for _, evt := range rc.Events {
			key := eventKey(objType, evt)
			ts.eventListeners[key] = append(ts.eventListeners[key], agent)
		}
	}

	ts.log.Info("registered event trigger",
		slog.String("agent", agent.Name),
		slog.String("agent_id", agent.ID),
		slog.String("project_id", agent.ProjectID),
		slog.Any("object_types", rc.ObjectTypes),
		slog.Any("events", rc.Events),
	)
}

// SyncAgentTrigger updates the trigger registration for a single agent.
// Call this when an agent is created or updated.
func (ts *TriggerService) SyncAgentTrigger(agent *Agent) {
	// First, remove any existing trigger for this agent
	ts.RemoveAgentTrigger(agent.ID)

	if !agent.Enabled {
		return
	}

	switch agent.TriggerType {
	case TriggerTypeSchedule:
		if agent.CronSchedule == "" {
			return
		}
		if err := ts.registerCronTrigger(agent); err != nil {
			ts.log.Error("failed to register cron trigger on sync",
				slog.String("agent", agent.Name),
				slog.String("agent_id", agent.ID),
				slog.String("error", err.Error()),
			)
		}
	case TriggerTypeReaction:
		if agent.ReactionConfig == nil || len(agent.ReactionConfig.Events) == 0 {
			return
		}
		ts.registerEventTrigger(agent)
	}
}

// RemoveAgentTrigger removes all trigger registrations for an agent.
// Call this when an agent is deleted or disabled.
func (ts *TriggerService) RemoveAgentTrigger(agentID string) {
	// Remove from scheduler
	ts.scheduler.RemoveTask(triggerTaskName(agentID))

	// Remove from event listeners
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for key, agents := range ts.eventListeners {
		filtered := make([]*Agent, 0, len(agents))
		for _, a := range agents {
			if a.ID != agentID {
				filtered = append(filtered, a)
			}
		}
		if len(filtered) == 0 {
			delete(ts.eventListeners, key)
		} else {
			ts.eventListeners[key] = filtered
		}
	}
}

// HandleEvent dispatches an event to all registered agents for that event type.
// objectType is the type of object the event pertains to (e.g., "document").
// eventType is the type of event (e.g., "created", "updated", "deleted").
// The input map provides context about the event (e.g., object ID, project ID).
func (ts *TriggerService) HandleEvent(ctx context.Context, objectType string, eventType ReactionEventType, projectID string, input map[string]any) {
	ts.handleEvent(ctx, objectType, eventType, projectID, input, nil)
}

// handleEvent is the actor-aware dispatch path shared by HandleEvent (actor nil:
// external/manual callers) and onEntityEvent (the originating event actor).
func (ts *TriggerService) handleEvent(ctx context.Context, objectType string, eventType ReactionEventType, projectID string, input map[string]any, actor *events.ActorContext) {
	ts.mu.RLock()
	// Collect agents matching either the specific object type or the wildcard
	specificKey := eventKey(objectType, eventType)
	wildcardKey := eventKey("*", eventType)

	var matchedAgents []*Agent
	matchedAgents = append(matchedAgents, ts.eventListeners[specificKey]...)
	matchedAgents = append(matchedAgents, ts.eventListeners[wildcardKey]...)
	ts.mu.RUnlock()

	if len(matchedAgents) == 0 {
		return
	}

	objectID, _ := input["id"].(string)

	// Deduplicate (an agent could match both specific and wildcard)
	seen := make(map[string]bool)
	for _, agent := range matchedAgents {
		if seen[agent.ID] {
			continue
		}
		seen[agent.ID] = true

		// Only trigger agents that belong to the same project
		if agent.ProjectID != projectID {
			continue
		}

		// Per-agent agent-origin gate, replacing the old global drop.
		if !shouldTriggerAgentForActor(agent, actor) {
			ts.log.Info("skipping agent-originated event for agent",
				slog.String("object_type", objectType),
				slog.String("event", string(eventType)),
				slog.String("agent", agent.Name),
				slog.String("agent_id", agent.ID),
				slog.String("actor_type", string(actor.ActorType)),
				slog.String("actor_id", actor.ActorID),
			)
			continue
		}

		if ts.skipForConcurrency(ctx, agent, objectType, objectID) {
			continue
		}

		matched := agent
		go func() {
			ts.dispatchMatchedAgent(ctx, matched, objectType, eventType, projectID, input)
		}()
	}
}

// dispatchMatchedAgent routes a matched reaction agent to either the
// object-driven enqueue path (definition has a work config or queued dispatch
// mode) or the inline execution path.
func (ts *TriggerService) dispatchMatchedAgent(ctx context.Context, agent *Agent, objectType string, eventType ReactionEventType, projectID string, input map[string]any) {
	var agentDef *AgentDefinition
	if ts.repo != nil {
		var err error
		agentDef, err = ts.repo.ResolveDefinitionForAgent(ctx, agent)
		if err != nil {
			ts.log.Warn("failed to resolve definition for matched agent",
				slog.String("agent_id", agent.ID),
				slog.String("error", err.Error()),
			)
		}
	}
	if ts.shouldEnqueueWork(agentDef) {
		ts.enqueueWorkRun(ctx, agent, agentDef, objectType, eventType, projectID, input)
		return
	}

	objectID, _ := input["id"].(string)
	ts.log.Info("executing event-triggered agent",
		slog.String("object_type", objectType),
		slog.String("event", string(eventType)),
		slog.String("agent", agent.Name),
		slog.String("agent_id", agent.ID),
		slog.String("project_id", projectID),
	)
	if err := ts.dispatch(ctx, agent, projectID, objectType, objectID); err != nil {
		ts.log.Error("event-triggered agent execution failed",
			slog.String("object_type", objectType),
			slog.String("event", string(eventType)),
			slog.String("agent", agent.Name),
			slog.String("agent_id", agent.ID),
			slog.String("error", err.Error()),
		)
	}
}

// shouldEnqueueWork reports whether a reaction dispatch for the given definition
// must enqueue a run (object-driven work) rather than execute inline. A
// definition without a work config keeps today's inline behaviour.
func (ts *TriggerService) shouldEnqueueWork(def *AgentDefinition) bool {
	if def == nil {
		return false
	}
	return def.DispatchMode == DispatchModeQueued || !def.WorkConfig.IsZero()
}

// enqueueWorkRun performs the object-driven dispatch: assignee routing, dispatch
// dedup against kb.agent_processing_log, and enqueue of a run linked to the work
// object via subject_object_id / subject_object_type.
func (ts *TriggerService) enqueueWorkRun(ctx context.Context, agent *Agent, agentDef *AgentDefinition, objectType string, eventType ReactionEventType, projectID string, input map[string]any) {
	// For graph objects the event ID is already the object's canonical_id
	// (graph/service.go emitObjectCreated passes obj.CanonicalID).
	canonicalID, _ := input["id"].(string)
	if canonicalID == "" {
		ts.log.Warn("cannot enqueue work run: missing object id",
			slog.String("agent_id", agent.ID),
			slog.String("object_type", objectType),
		)
		return
	}

	// Assignee routing: an assigned object wakes only the listener whose name
	// matches the assignee (project scoping was already applied by handleEvent).
	if data, ok := input["data"].(map[string]any); ok {
		if assignee, ok := data["assignee"].(string); ok && assignee != "" && agent.Name != assignee {
			return // not the assigned lane
		}
	}

	// Resolve the object version at dispatch. The single-event path carries it in
	// the payload; the batch path omits it, so fall back to the HEAD version.
	version, hasVersion := intFromInput(input["version"])
	if !hasVersion {
		if ts.workObjects == nil {
			ts.log.Warn("cannot resolve work object version: no work object store",
				slog.String("agent_id", agent.ID),
				slog.String("canonical_id", canonicalID),
			)
			return
		}
		head, err := ts.workObjects.GetHeadObject(ctx, projectID, canonicalID)
		if err != nil {
			ts.log.Warn("failed to resolve work object version",
				slog.String("agent_id", agent.ID),
				slog.String("canonical_id", canonicalID),
				slog.String("error", err.Error()),
			)
			return
		}
		if head == nil {
			return // object gone
		}
		version = head.Version
	}

	// Dedup + enqueue atomically on (agent_id, graph_object_id, object_version,
	// event_type): the processing-log dispatch slot is claimed and the queued run
	// enqueued in a single transaction, so a repeated created delivery yields a
	// single run and a failed enqueue leaves no orphaned log row to suppress
	// redelivery.
	maxAttempts := 1
	if agentDef != nil && agentDef.WorkConfig.RetryPolicy.MaxAttempts > 0 {
		maxAttempts = agentDef.WorkConfig.RetryPolicy.MaxAttempts
	}
	maxPendingJobs := 0
	if ts.executor != nil {
		maxPendingJobs = ts.executor.safeguards.MaxPendingJobs
	}

	logEntry := &AgentProcessingLog{
		AgentID:       agent.ID,
		GraphObjectID: canonicalID,
		ObjectVersion: version,
		EventType:     eventType,
		Status:        ProcessingStatusPending,
	}
	run, claimed, err := ts.repo.EnqueueWorkRunDeduped(ctx, logEntry, agent.ID, maxAttempts, CreateRunQueuedOptions{
		SubjectObjectID:   &canonicalID,
		SubjectObjectType: &objectType,
		TrustedInternal:   true,
		MaxPendingJobs:    maxPendingJobs,
	})
	if err != nil {
		ts.log.Warn("failed to enqueue work run",
			slog.String("agent_id", agent.ID),
			slog.String("canonical_id", canonicalID),
			slog.String("error", err.Error()),
		)
		return
	}
	if !claimed {
		return // already dispatched
	}

	ts.log.Info("enqueued object-driven work run",
		slog.String("object_type", objectType),
		slog.String("event", string(eventType)),
		slog.String("agent", agent.Name),
		slog.String("agent_id", agent.ID),
		slog.String("project_id", projectID),
		slog.String("canonical_id", canonicalID),
		slog.Int("object_version", version),
		slog.String("run_id", run.ID),
	)
}

// intFromInput extracts an int from an event input value that may be a plain
// int (in-process events) or a float64 (JSON-decoded events).
func intFromInput(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// shouldTriggerAgentForActor applies the per-agent agent-origin gate.
//
// A nil actor or a non-agent actor (user/system) always triggers — this is the
// common path and the pre-existing HTTP behaviour. For an agent-originated event:
//
//   - IgnoreAgentTriggered: nil (unset) or true → skip. Only an explicit false
//     opts the agent in to agent-originated events.
//   - When the originating agent is THIS agent, IgnoreSelfTriggered is also
//     consulted: nil (unset) or true → skip, only explicit false allows it.
//
// Both flags defaulting to nil therefore reproduce exactly the historical
// "all agent-originated events are ignored" behaviour.
func shouldTriggerAgentForActor(agent *Agent, actor *events.ActorContext) bool {
	if actor == nil || actor.ActorType != events.ActorAgent {
		return true
	}

	rc := agent.ReactionConfig
	// nil config cannot opt in; unset or true means ignore.
	if rc == nil || rc.IgnoreAgentTriggered == nil || *rc.IgnoreAgentTriggered {
		return false
	}

	if isSelfTrigger(agent, actor) {
		return rc.IgnoreSelfTriggered != nil && !*rc.IgnoreSelfTriggered
	}
	return true
}

// isSelfTrigger reports whether the originating agent is this candidate agent.
//
// The canonical actor id for actor_type='agent' is the agent DEFINITION id
// (kb.agent_definitions.id), stamped at the run boundary — not the runtime
// kb.agents id. Agents without a linked definition are never attributed as an
// agent actor, so they cannot be a "self" trigger.
func isSelfTrigger(agent *Agent, actor *events.ActorContext) bool {
	if agent == nil || actor == nil || actor.ActorID == "" || agent.AgentDefinitionID == nil {
		return false
	}
	return *agent.AgentDefinitionID == actor.ActorID
}

// skipForConcurrency enforces ConcurrencyStrategy=skip. The "same object" key is
// (agent id, subject object id, subject object type): the object identity is
// recorded on each reaction run's trigger_metadata at dispatch time, and an
// active (non-terminal) run with the same key means the new trigger is dropped.
//
// Empty and "parallel" strategies never consult the checker. A missing object id
// or unavailable checker fails open (proceed) so a transient lookup problem
// cannot silently swallow triggers.
func (ts *TriggerService) skipForConcurrency(ctx context.Context, agent *Agent, objectType, objectID string) bool {
	rc := agent.ReactionConfig
	if rc == nil || rc.ConcurrencyStrategy != ConcurrencySkip {
		return false
	}
	if objectID == "" || ts.activeRuns == nil {
		return false
	}

	active, err := ts.activeRuns.HasActiveRunForAgentObject(ctx, agent.ID, objectID, objectType)
	if err != nil {
		ts.log.Warn("could not check active runs for concurrency skip, proceeding",
			slog.String("agent_id", agent.ID),
			slog.String("object_id", objectID),
			slog.String("object_type", objectType),
			slog.String("error", err.Error()),
		)
		return false
	}
	if active {
		ts.log.Warn("skipping trigger: active run already exists for agent + object",
			slog.String("agent_id", agent.ID),
			slog.String("object_id", objectID),
			slog.String("object_type", objectType),
		)
	}
	return active
}

// executeTriggeredAgent looks up the agent and its corresponding AgentDefinition,
// then executes it via the AgentExecutor.
//
// objectType/objectID are the reaction target object, recorded on the run's
// trigger_metadata as subjectObjectType/subjectObjectId so that
// ConcurrencyStrategy=skip can find in-flight runs for the same agent + object.
// Cron triggers pass empty values.
func (ts *TriggerService) executeTriggeredAgent(ctx context.Context, agentID string, projectID string, objectType string, objectID string) error {
	// Check pending job queue depth before executing.
	// This prevents cron triggers from flooding the queue when the agent is backed up.
	maxPendingJobs := ts.executor.safeguards.MaxPendingJobs
	if maxPendingJobs <= 0 {
		maxPendingJobs = 10 // safe fallback
	}
	count, err := ts.repo.CountPendingJobsForAgent(ctx, agentID)
	if err != nil {
		// Fail-open: log warning and proceed so a DB hiccup doesn't halt agents.
		ts.log.Warn("could not check pending job count, proceeding with execution",
			slog.String("agent_id", agentID),
			slog.String("error", err.Error()),
		)
	} else if count >= maxPendingJobs {
		ts.log.Warn("skipping cron trigger, queue full",
			slog.String("agent_id", agentID),
			slog.Int("pending_jobs", count),
			slog.Int("max_pending_jobs", maxPendingJobs),
		)
		return nil
	}

	// Look up the runtime agent
	agent, err := ts.repo.FindByID(ctx, agentID, &projectID)
	if err != nil {
		return fmt.Errorf("failed to find agent %s: %w", agentID, err)
	}
	if agent == nil {
		return fmt.Errorf("agent %s not found (may have been deleted)", agentID)
	}

	// Look up the corresponding AgentDefinition for config (system prompt, tools, etc.)
	var agentDef *AgentDefinition
	agentDef, _ = ts.repo.ResolveDefinitionForAgent(ctx, agent)

	// Build user message
	userMessage := fmt.Sprintf("Scheduled execution of agent %q", agent.Name)
	if agent.Prompt != nil && *agent.Prompt != "" {
		userMessage = *agent.Prompt
	}

	// Use max_steps from definition if available, otherwise from agent
	var maxSteps *int
	if agentDef != nil && agentDef.MaxSteps != nil {
		maxSteps = agentDef.MaxSteps
	}

	// Resolve org ID for the agent's project so the tracking model can attribute
	// LLM usage events to the correct tenant.
	orgID, _ := ts.repo.GetOrgIDByProjectID(ctx, projectID)

	// Reaction dispatches record the target object on the run so
	// ConcurrencyStrategy=skip can key on (agent, subjectObjectId, subjectObjectType).
	var triggerMetadata map[string]any
	if objectID != "" {
		triggerMetadata = map[string]any{
			"subjectObjectId":   objectID,
			"subjectObjectType": objectType,
		}
	}

	// Execute the agent
	result, err := ts.executor.Execute(ctx, ExecuteRequest{
		Agent:           agent,
		AgentDefinition: agentDef,
		ProjectID:       projectID,
		OrgID:           orgID,
		UserMessage:     userMessage,
		MaxSteps:        maxSteps,
		TriggerMetadata: triggerMetadata,
		TrustedInternal: true, // scheduled runs are a trusted surface (full internal coordination)
	})
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}
	if err != nil {
		return fmt.Errorf("agent execution failed: %w", err)
	}

	ts.log.Info("triggered agent execution completed",
		slog.String("agent", agent.Name),
		slog.String("agent_id", agent.ID),
		slog.String("run_id", result.RunID),
		slog.String("status", string(result.Status)),
		slog.Int("steps", result.Steps),
		slog.Duration("duration", result.Duration),
	)
	return nil
}

// GetEventListeners returns the agents registered for a given event key (for testing/debugging).
// eventKey format: "objectType:eventType"
func (ts *TriggerService) GetEventListeners(key string) []*Agent {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.eventListeners[key]
}

// validateCronInterval parses the given cron expression and returns an error if
// the interval between consecutive executions is shorter than minMinutes.
// This prevents agents from being scheduled too frequently and causing queue explosions.
func validateCronInterval(cronExpr string, minMinutes int) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	now := time.Now()
	next1 := schedule.Next(now)
	next2 := schedule.Next(next1)
	interval := next2.Sub(next1)

	minDuration := time.Duration(minMinutes) * time.Minute
	if interval < minDuration {
		return fmt.Errorf("cron interval %.0fm is below minimum %dm (expression: %q)", interval.Minutes(), minMinutes, cronExpr)
	}
	return nil
}
