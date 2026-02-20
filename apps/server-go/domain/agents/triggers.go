package agents

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/emergent-company/emergent/domain/events"
	"github.com/emergent-company/emergent/domain/scheduler"
	"github.com/emergent-company/emergent/pkg/logger"
)

// TriggerService manages trigger registration for agents.
// Scheduling config lives on Agent (runtime entity), not AgentDefinition (config).
// It registers cron schedules in the scheduler and provides hooks for event-driven triggers.
type TriggerService struct {
	scheduler *scheduler.Scheduler
	executor  *AgentExecutor
	repo      *Repository
	events    *events.Service
	log       *slog.Logger

	// mu protects eventListeners
	mu             sync.RWMutex
	eventListeners map[string][]*Agent // eventKey -> agents
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

	// Subscribe to all events
	if ts.events != nil {
		ts.events.Subscribe("*", ts.onEntityEvent)
	}

	return ts
}

// onEntityEvent handles incoming events from the global event bus
func (ts *TriggerService) onEntityEvent(evt events.EntityEvent) {
	// Ignore events caused by agents to prevent infinite loops (Task 5.3)
	if evt.Actor != nil && evt.Actor.ActorType == events.ActorAgent {
		return
	}

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

		ts.HandleEvent(context.Background(), objType, reactionType, evt.ProjectID, input)
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
			ts.HandleEvent(context.Background(), objType, reactionType, evt.ProjectID, input)
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

	// Capture for closure
	agentID := agent.ID
	projectID := agent.ProjectID

	err := ts.scheduler.AddCronTask(taskName, agent.CronSchedule, func(ctx context.Context) error {
		return ts.executeTriggeredAgent(ctx, agentID, projectID)
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

		agentID := agent.ID
		agentName := agent.Name
		go func() {
			ts.log.Info("executing event-triggered agent",
				slog.String("object_type", objectType),
				slog.String("event", string(eventType)),
				slog.String("agent", agentName),
				slog.String("agent_id", agentID),
				slog.String("project_id", projectID),
			)
			if err := ts.executeTriggeredAgent(ctx, agentID, projectID); err != nil {
				ts.log.Error("event-triggered agent execution failed",
					slog.String("object_type", objectType),
					slog.String("event", string(eventType)),
					slog.String("agent", agentName),
					slog.String("agent_id", agentID),
					slog.String("error", err.Error()),
				)
			}
		}()
	}
}

// executeTriggeredAgent looks up the agent and its corresponding AgentDefinition,
// then executes it via the AgentExecutor.
func (ts *TriggerService) executeTriggeredAgent(ctx context.Context, agentID string, projectID string) error {
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
	agentDef, _ = ts.repo.FindDefinitionByName(ctx, projectID, agent.Name)

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

	// Execute the agent
	result, err := ts.executor.Execute(ctx, ExecuteRequest{
		Agent:           agent,
		AgentDefinition: agentDef,
		ProjectID:       projectID,
		UserMessage:     userMessage,
		MaxSteps:        maxSteps,
	})
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
