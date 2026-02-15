package agents

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/emergent/emergent-core/domain/scheduler"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Known event trigger names — these are not cron expressions.
const (
	TriggerOnDocumentIngested = "on_document_ingested"
)

// knownEventTriggers is the set of trigger values that represent events (not cron expressions).
var knownEventTriggers = map[string]bool{
	TriggerOnDocumentIngested: true,
}

// TriggerService manages trigger registration for agent definitions.
// It registers cron schedules in the scheduler and provides hooks for event-driven triggers.
type TriggerService struct {
	scheduler *scheduler.Scheduler
	executor  *AgentExecutor
	repo      *Repository
	log       *slog.Logger

	// mu protects eventListeners
	mu             sync.RWMutex
	eventListeners map[string][]*AgentDefinition // eventName -> definitions
}

// NewTriggerService creates a new TriggerService.
func NewTriggerService(
	sched *scheduler.Scheduler,
	executor *AgentExecutor,
	repo *Repository,
	log *slog.Logger,
) *TriggerService {
	return &TriggerService{
		scheduler:      sched,
		executor:       executor,
		repo:           repo,
		log:            log.With(logger.Scope("agents.triggers")),
		eventListeners: make(map[string][]*AgentDefinition),
	}
}

// triggerTaskName returns the scheduler task name for an agent definition.
func triggerTaskName(defID string) string {
	return "agent_def:" + defID
}

// isEventTrigger returns true if the trigger value is a known event name.
func isEventTrigger(trigger string) bool {
	return knownEventTriggers[trigger]
}

// isCronTrigger returns true if the trigger value looks like a cron expression.
// Cron expressions contain spaces and are not known event names.
func isCronTrigger(trigger string) bool {
	if trigger == "" || isEventTrigger(trigger) {
		return false
	}
	// A cron expression has at least 4 spaces (5 fields for standard, 6 for seconds-based).
	// But even a 5-field cron ("* * * * *") has 4 spaces.
	// We accept anything with spaces that isn't a known event trigger.
	return strings.Contains(trigger, " ") || strings.HasPrefix(trigger, "@")
}

// SyncAllTriggers scans all agent definitions with triggers and registers them.
// Called on server startup.
func (ts *TriggerService) SyncAllTriggers(ctx context.Context) error {
	defs, err := ts.repo.FindAllTriggeredDefinitions(ctx)
	if err != nil {
		return fmt.Errorf("failed to load triggered definitions: %w", err)
	}

	ts.log.Info("syncing agent triggers", slog.Int("definitions", len(defs)))

	cronCount := 0
	eventCount := 0

	for _, def := range defs {
		if def.Trigger == nil || *def.Trigger == "" {
			continue
		}
		trigger := *def.Trigger

		if isCronTrigger(trigger) {
			if err := ts.registerCronTrigger(def, trigger); err != nil {
				ts.log.Error("failed to register cron trigger",
					slog.String("definition", def.Name),
					slog.String("definition_id", def.ID),
					slog.String("schedule", trigger),
					slog.String("error", err.Error()),
				)
				continue
			}
			cronCount++
		} else if isEventTrigger(trigger) {
			ts.registerEventTrigger(def, trigger)
			eventCount++
		} else {
			ts.log.Warn("unknown trigger type",
				slog.String("definition", def.Name),
				slog.String("definition_id", def.ID),
				slog.String("trigger", trigger),
			)
		}
	}

	ts.log.Info("agent triggers synced",
		slog.Int("cron", cronCount),
		slog.Int("event", eventCount),
	)
	return nil
}

// registerCronTrigger registers a cron schedule for an agent definition.
func (ts *TriggerService) registerCronTrigger(def *AgentDefinition, schedule string) error {
	taskName := triggerTaskName(def.ID)

	// Capture definition ID and project for the closure
	defID := def.ID
	projectID := def.ProjectID

	err := ts.scheduler.AddCronTask(taskName, schedule, func(ctx context.Context) error {
		return ts.executeTriggeredAgent(ctx, defID, projectID)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", schedule, err)
	}

	ts.log.Info("registered cron trigger",
		slog.String("definition", def.Name),
		slog.String("definition_id", def.ID),
		slog.String("project_id", def.ProjectID),
		slog.String("schedule", schedule),
	)
	return nil
}

// registerEventTrigger registers an event listener for an agent definition.
func (ts *TriggerService) registerEventTrigger(def *AgentDefinition, eventName string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.eventListeners[eventName] = append(ts.eventListeners[eventName], def)

	ts.log.Info("registered event trigger",
		slog.String("definition", def.Name),
		slog.String("definition_id", def.ID),
		slog.String("project_id", def.ProjectID),
		slog.String("event", eventName),
	)
}

// SyncDefinitionTrigger updates the trigger registration for a single definition.
// Call this when a definition is created or updated.
func (ts *TriggerService) SyncDefinitionTrigger(def *AgentDefinition) {
	// First, remove any existing trigger for this definition
	ts.RemoveDefinitionTrigger(def.ID)

	if def.Trigger == nil || *def.Trigger == "" {
		return
	}
	trigger := *def.Trigger

	if isCronTrigger(trigger) {
		if err := ts.registerCronTrigger(def, trigger); err != nil {
			ts.log.Error("failed to register cron trigger on sync",
				slog.String("definition", def.Name),
				slog.String("definition_id", def.ID),
				slog.String("error", err.Error()),
			)
		}
	} else if isEventTrigger(trigger) {
		ts.registerEventTrigger(def, trigger)
	}
}

// RemoveDefinitionTrigger removes all trigger registrations for a definition.
// Call this when a definition is deleted or its trigger is cleared.
func (ts *TriggerService) RemoveDefinitionTrigger(defID string) {
	// Remove from scheduler
	ts.scheduler.RemoveTask(triggerTaskName(defID))

	// Remove from event listeners
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for eventName, defs := range ts.eventListeners {
		filtered := make([]*AgentDefinition, 0, len(defs))
		for _, d := range defs {
			if d.ID != defID {
				filtered = append(filtered, d)
			}
		}
		if len(filtered) == 0 {
			delete(ts.eventListeners, eventName)
		} else {
			ts.eventListeners[eventName] = filtered
		}
	}
}

// HandleEvent dispatches an event to all registered agent definitions for that event type.
// This is the hook that document ingestion (or other event sources) should call.
// The input map provides context about the event (e.g., document ID, project ID).
func (ts *TriggerService) HandleEvent(ctx context.Context, eventName string, projectID string, input map[string]any) {
	ts.mu.RLock()
	defs := ts.eventListeners[eventName]
	ts.mu.RUnlock()

	if len(defs) == 0 {
		return
	}

	for _, def := range defs {
		// Only trigger agents that belong to the same project
		if def.ProjectID != projectID {
			continue
		}

		defID := def.ID
		defName := def.Name
		go func() {
			ts.log.Info("executing event-triggered agent",
				slog.String("event", eventName),
				slog.String("definition", defName),
				slog.String("definition_id", defID),
				slog.String("project_id", projectID),
			)
			if err := ts.executeTriggeredAgent(ctx, defID, projectID); err != nil {
				ts.log.Error("event-triggered agent execution failed",
					slog.String("event", eventName),
					slog.String("definition", defName),
					slog.String("definition_id", defID),
					slog.String("error", err.Error()),
				)
			}
		}()
	}
}

// executeTriggeredAgent looks up the agent definition and its corresponding Agent,
// then executes it via the AgentExecutor.
func (ts *TriggerService) executeTriggeredAgent(ctx context.Context, defID string, projectID string) error {
	// Look up the agent definition to get its config
	def, err := ts.repo.FindDefinitionByID(ctx, defID, &projectID)
	if err != nil {
		return fmt.Errorf("failed to find agent definition %s: %w", defID, err)
	}
	if def == nil {
		return fmt.Errorf("agent definition %s not found (may have been deleted)", defID)
	}

	// Find the corresponding runtime Agent by name
	agent, err := ts.repo.FindByName(ctx, projectID, def.Name)
	if err != nil {
		return fmt.Errorf("failed to find runtime agent for definition %s: %w", def.Name, err)
	}
	if agent == nil {
		ts.log.Warn("no runtime agent found for triggered definition, skipping",
			slog.String("definition", def.Name),
			slog.String("definition_id", defID),
			slog.String("project_id", projectID),
		)
		return nil
	}

	// Execute the agent
	result, err := ts.executor.Execute(ctx, ExecuteRequest{
		Agent:           agent,
		AgentDefinition: def,
		ProjectID:       projectID,
		UserMessage:     fmt.Sprintf("Scheduled execution of agent %q", def.Name),
		MaxSteps:        def.MaxSteps,
	})
	if err != nil {
		return fmt.Errorf("agent execution failed: %w", err)
	}

	ts.log.Info("triggered agent execution completed",
		slog.String("definition", def.Name),
		slog.String("run_id", result.RunID),
		slog.String("status", string(result.Status)),
		slog.Int("steps", result.Steps),
		slog.Duration("duration", result.Duration),
	)
	return nil
}

// GetEventListeners returns the definitions registered for a given event (for testing/debugging).
func (ts *TriggerService) GetEventListeners(eventName string) []*AgentDefinition {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.eventListeners[eventName]
}
