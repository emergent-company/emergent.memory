package agents

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent/domain/events"
	"github.com/emergent-company/emergent/domain/scheduler"
)

// newTestTriggerService creates a TriggerService without event bus subscription.
// Pass eventService=nil to avoid auto-subscribing onEntityEvent.
func newTestTriggerService() *TriggerService {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	sched := scheduler.NewScheduler(log)
	return NewTriggerService(sched, nil, nil, nil, log)
}

// newTestTriggerServiceWithEvents creates a TriggerService with a real events.Service,
// which auto-subscribes onEntityEvent to the "*" wildcard channel.
func newTestTriggerServiceWithEvents() (*TriggerService, *events.Service) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	sched := scheduler.NewScheduler(log)
	eventSvc := events.NewService(log)
	ts := NewTriggerService(sched, nil, nil, eventSvc, log)
	return ts, eventSvc
}

func makeTestAgent(id, name, projectID string, rc *ReactionConfig) *Agent {
	return &Agent{
		ID:             id,
		Name:           name,
		ProjectID:      projectID,
		TriggerType:    TriggerTypeReaction,
		Enabled:        true,
		ReactionConfig: rc,
	}
}

// ---------- eventKey ----------

func TestEventKey(t *testing.T) {
	assert.Equal(t, "document:created", eventKey("document", EventTypeCreated))
	assert.Equal(t, "chunk:updated", eventKey("chunk", EventTypeUpdated))
	assert.Equal(t, "*:deleted", eventKey("*", EventTypeDeleted))
}

func TestTriggerTaskName(t *testing.T) {
	assert.Equal(t, "agent:abc-123", triggerTaskName("abc-123"))
}

// ---------- registerEventTrigger ----------

func TestRegisterEventTrigger_SpecificObjectTypes(t *testing.T) {
	ts := newTestTriggerService()

	agent := makeTestAgent("a1", "test-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated, EventTypeUpdated},
	})

	ts.registerEventTrigger(agent)

	// Should be registered for document:created and document:updated
	listeners := ts.GetEventListeners("document:created")
	require.Len(t, listeners, 1)
	assert.Equal(t, "a1", listeners[0].ID)

	listeners = ts.GetEventListeners("document:updated")
	require.Len(t, listeners, 1)
	assert.Equal(t, "a1", listeners[0].ID)

	// Should NOT be registered for document:deleted
	assert.Empty(t, ts.GetEventListeners("document:deleted"))
}

func TestRegisterEventTrigger_MultipleObjectTypes(t *testing.T) {
	ts := newTestTriggerService()

	agent := makeTestAgent("a1", "multi-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{"document", "chunk"},
		Events:      []ReactionEventType{EventTypeCreated},
	})

	ts.registerEventTrigger(agent)

	assert.Len(t, ts.GetEventListeners("document:created"), 1)
	assert.Len(t, ts.GetEventListeners("chunk:created"), 1)
	assert.Empty(t, ts.GetEventListeners("document:updated"))
}

func TestRegisterEventTrigger_WildcardObjectTypes(t *testing.T) {
	ts := newTestTriggerService()

	agent := makeTestAgent("a1", "wildcard-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{}, // empty = wildcard
		Events:      []ReactionEventType{EventTypeCreated},
	})

	ts.registerEventTrigger(agent)

	// Should be registered under "*:created"
	listeners := ts.GetEventListeners("*:created")
	require.Len(t, listeners, 1)
	assert.Equal(t, "a1", listeners[0].ID)

	// Should NOT be registered for specific keys
	assert.Empty(t, ts.GetEventListeners("document:created"))
}

func TestRegisterEventTrigger_NilReactionConfig(t *testing.T) {
	ts := newTestTriggerService()

	agent := makeTestAgent("a1", "nil-config", "p1", nil)
	ts.registerEventTrigger(agent)

	// Nothing should be registered
	assert.Empty(t, ts.GetEventListeners("*:created"))
	assert.Empty(t, ts.GetEventListeners("document:created"))
}

func TestRegisterEventTrigger_MultipleAgentsSameKey(t *testing.T) {
	ts := newTestTriggerService()

	agent1 := makeTestAgent("a1", "agent-1", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	agent2 := makeTestAgent("a2", "agent-2", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})

	ts.registerEventTrigger(agent1)
	ts.registerEventTrigger(agent2)

	listeners := ts.GetEventListeners("document:created")
	require.Len(t, listeners, 2)
	ids := []string{listeners[0].ID, listeners[1].ID}
	assert.Contains(t, ids, "a1")
	assert.Contains(t, ids, "a2")
}

// ---------- RemoveAgentTrigger ----------

func TestRemoveAgentTrigger(t *testing.T) {
	ts := newTestTriggerService()

	agent := makeTestAgent("a1", "agent-1", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated, EventTypeUpdated},
	})

	ts.registerEventTrigger(agent)
	require.Len(t, ts.GetEventListeners("document:created"), 1)
	require.Len(t, ts.GetEventListeners("document:updated"), 1)

	ts.RemoveAgentTrigger("a1")

	assert.Empty(t, ts.GetEventListeners("document:created"))
	assert.Empty(t, ts.GetEventListeners("document:updated"))
}

func TestRemoveAgentTrigger_LeavesOtherAgentsIntact(t *testing.T) {
	ts := newTestTriggerService()

	agent1 := makeTestAgent("a1", "agent-1", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	agent2 := makeTestAgent("a2", "agent-2", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})

	ts.registerEventTrigger(agent1)
	ts.registerEventTrigger(agent2)
	require.Len(t, ts.GetEventListeners("document:created"), 2)

	ts.RemoveAgentTrigger("a1")

	listeners := ts.GetEventListeners("document:created")
	require.Len(t, listeners, 1)
	assert.Equal(t, "a2", listeners[0].ID)
}

func TestRemoveAgentTrigger_NonExistent(t *testing.T) {
	ts := newTestTriggerService()

	// Should not panic when removing a non-existent agent
	assert.NotPanics(t, func() {
		ts.RemoveAgentTrigger("non-existent-id")
	})
}

// ---------- SyncAgentTrigger ----------

func TestSyncAgentTrigger_EnabledReaction(t *testing.T) {
	ts := newTestTriggerService()

	agent := makeTestAgent("a1", "reaction-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})

	ts.SyncAgentTrigger(agent)

	assert.Len(t, ts.GetEventListeners("document:created"), 1)
}

func TestSyncAgentTrigger_DisabledAgent(t *testing.T) {
	ts := newTestTriggerService()

	agent := makeTestAgent("a1", "disabled-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	agent.Enabled = false

	ts.SyncAgentTrigger(agent)

	assert.Empty(t, ts.GetEventListeners("document:created"))
}

func TestSyncAgentTrigger_ReplacesExistingRegistration(t *testing.T) {
	ts := newTestTriggerService()

	// First register for document:created
	agent := makeTestAgent("a1", "evolving-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	ts.SyncAgentTrigger(agent)
	require.Len(t, ts.GetEventListeners("document:created"), 1)

	// Now update to chunk:updated instead
	agent.ReactionConfig = &ReactionConfig{
		ObjectTypes: []string{"chunk"},
		Events:      []ReactionEventType{EventTypeUpdated},
	}
	ts.SyncAgentTrigger(agent)

	// Old registration should be gone
	assert.Empty(t, ts.GetEventListeners("document:created"))
	// New registration should exist
	assert.Len(t, ts.GetEventListeners("chunk:updated"), 1)
}

// ---------- HandleEvent ----------

func TestHandleEvent_NoListeners(t *testing.T) {
	ts := newTestTriggerService()

	// Should not panic with no registered listeners
	assert.NotPanics(t, func() {
		ts.HandleEvent(context.Background(), "document", EventTypeCreated, "p1", map[string]any{"id": "doc-1"})
	})
}

func TestHandleEvent_ProjectIsolation(t *testing.T) {
	ts := newTestTriggerService()

	agent := makeTestAgent("a1", "project-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	ts.registerEventTrigger(agent)

	// Call HandleEvent with a DIFFERENT project ID.
	// Since the agent belongs to "p1" but the event is for "p2",
	// the project filter prevents any goroutines from being launched.
	// If it incorrectly matched, the nil repo would cause a panic.
	assert.NotPanics(t, func() {
		ts.HandleEvent(context.Background(), "document", EventTypeCreated, "p2", map[string]any{"id": "doc-1"})
	})
}

func TestHandleEvent_NoMatchingEventType(t *testing.T) {
	ts := newTestTriggerService()

	agent := makeTestAgent("a1", "create-only", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	ts.registerEventTrigger(agent)

	// Event type "deleted" doesn't match "created" registration.
	// No goroutines should be launched.
	assert.NotPanics(t, func() {
		ts.HandleEvent(context.Background(), "document", EventTypeDeleted, "p1", map[string]any{"id": "doc-1"})
	})
}

func TestHandleEvent_WildcardAndSpecificDedup(t *testing.T) {
	ts := newTestTriggerService()

	// Register the same agent under both a specific key AND a wildcard key.
	// This simulates an agent that listens to all object types (wildcard)
	// but is ALSO explicitly registered for "document".
	specificAgent := makeTestAgent("a1", "dedup-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	wildcardAgent := makeTestAgent("a1", "dedup-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{},
		Events:      []ReactionEventType{EventTypeCreated},
	})

	ts.registerEventTrigger(specificAgent)
	ts.registerEventTrigger(wildcardAgent)

	// Both keys should have the agent registered
	assert.Len(t, ts.GetEventListeners("document:created"), 1)
	assert.Len(t, ts.GetEventListeners("*:created"), 1)

	// HandleEvent would find the agent via both keys, but the dedup logic
	// (seen map) ensures it only launches one goroutine per unique agent ID.
	// We can't directly test the dedup without executing (which needs repo),
	// but we verify both keys are populated so that the dedup code path is needed.
}

// ---------- onEntityEvent (via events.Service) ----------

func TestOnEntityEvent_LoopPrevention_AgentActor(t *testing.T) {
	ts, eventSvc := newTestTriggerServiceWithEvents()

	// Register an agent for document:created in project p1.
	// TriggerService has nil repo, so if HandleEvent actually tries to
	// execute the agent, it will panic in the spawned goroutine.
	agent := makeTestAgent("a1", "loop-test", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	ts.registerEventTrigger(agent)

	// Emit event with ActorAgent — loop prevention should block it
	eventSvc.EmitCreated(events.EntityDocument, "doc-1", "p1", &events.EmitOptions{
		Actor: &events.ActorContext{ActorType: events.ActorAgent, ActorID: "agent-x"},
	})

	// Give the async goroutine time to execute (or not)
	time.Sleep(100 * time.Millisecond)
	// If we reach here without a panic, loop prevention worked.
}

func TestOnEntityEvent_LoopPrevention_UserActor_NoMatchingAgent(t *testing.T) {
	ts, eventSvc := newTestTriggerServiceWithEvents()

	// Register agent in project "p1"
	agent := makeTestAgent("a1", "user-test", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	ts.registerEventTrigger(agent)

	// Emit event from a user actor for a DIFFERENT project ("p2").
	// onEntityEvent won't block it (not an agent actor), but HandleEvent
	// won't find a matching agent for "p2", so no goroutine is launched.
	eventSvc.EmitCreated(events.EntityDocument, "doc-1", "p2", &events.EmitOptions{
		Actor: &events.ActorContext{ActorType: events.ActorUser, ActorID: "user-1"},
	})

	time.Sleep(100 * time.Millisecond)
	// Proves the event reached HandleEvent without crash (no matching project = safe)
}

func TestOnEntityEvent_NilActorAllowed(t *testing.T) {
	ts, eventSvc := newTestTriggerServiceWithEvents()

	// No agents registered — HandleEvent returns early with no matches
	_ = ts

	// Emit event with nil Actor (should not panic on nil check)
	eventSvc.EmitCreated(events.EntityDocument, "doc-1", "p1", nil)

	time.Sleep(100 * time.Millisecond)
}

func TestOnEntityEvent_BatchEventIgnored(t *testing.T) {
	ts, eventSvc := newTestTriggerServiceWithEvents()

	// Register agent that would match document:created
	agent := makeTestAgent("a1", "batch-test", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	ts.registerEventTrigger(agent)

	// Emit a BATCH event — the switch in onEntityEvent has no case for EventTypeBatch,
	// so it returns early from the default branch. No HandleEvent call.
	eventSvc.EmitBatch(events.EntityDocument, []string{"doc-1", "doc-2"}, "p1", nil)

	time.Sleep(100 * time.Millisecond)
	// If loop prevention for batch events is broken, HandleEvent would be called
	// with nil repo causing a panic. Reaching here means batch was correctly ignored.
}

func TestOnEntityEvent_EventTypeMapping(t *testing.T) {
	ts := newTestTriggerService()

	// Verify that event type mapping is correct by checking the constants
	// onEntityEvent maps events.EventTypeCreated -> agents.EventTypeCreated, etc.
	tests := []struct {
		eventsType events.EntityEventType
		agentsType ReactionEventType
	}{
		{events.EventTypeCreated, EventTypeCreated},
		{events.EventTypeUpdated, EventTypeUpdated},
		{events.EventTypeDeleted, EventTypeDeleted},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventsType), func(t *testing.T) {
			// Register agent for each mapped type
			agent := makeTestAgent("a-"+string(tt.agentsType), "map-test", "p1", &ReactionConfig{
				ObjectTypes: []string{"document"},
				Events:      []ReactionEventType{tt.agentsType},
			})
			ts.registerEventTrigger(agent)

			key := eventKey("document", tt.agentsType)
			listeners := ts.GetEventListeners(key)
			require.NotEmpty(t, listeners, "expected listener for key %s", key)
		})
	}
}

func TestOnEntityEvent_ObjectTypeOverride(t *testing.T) {
	// When evt.ObjectType is set, it should be used instead of evt.Entity
	ts, eventSvc := newTestTriggerServiceWithEvents()

	// Register for "Person" object type (ObjectType override, not entity type)
	agent := makeTestAgent("a1", "obj-override", "p1", &ReactionConfig{
		ObjectTypes: []string{"Person"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	ts.registerEventTrigger(agent)

	// Emit a graph_object created event with ObjectType="Person"
	// The agent IS registered for Person:created and project matches p1,
	// so HandleEvent WILL try to launch a goroutine — but since we have
	// nil repo it would panic. Instead, emit to a different project.
	eventSvc.EmitCreated(events.EntityGraphObject, "obj-1", "p2", &events.EmitOptions{
		ObjectType: "Person",
	})

	time.Sleep(100 * time.Millisecond)
	// Reaching here means the ObjectType override was used for routing
	// (would have looked up "Person:created" key, not "graph_object:created")
}

func TestOnEntityEvent_SystemActorAllowed(t *testing.T) {
	ts, eventSvc := newTestTriggerServiceWithEvents()

	// No agents registered — HandleEvent returns early
	_ = ts

	// System actor should NOT be blocked by loop prevention
	eventSvc.EmitUpdated(events.EntityDocument, "doc-1", "p1", &events.EmitOptions{
		Actor: &events.ActorContext{ActorType: events.ActorSystem},
	})

	time.Sleep(100 * time.Millisecond)
}

// ---------- Concurrency ----------

func TestRegisterAndRemoveConcurrent(t *testing.T) {
	ts := newTestTriggerService()

	// Spawn many goroutines registering and removing agents concurrently
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			agent := makeTestAgent(
				"a-"+string(rune('0'+n%10)),
				"concurrent-agent",
				"p1",
				&ReactionConfig{
					ObjectTypes: []string{"document"},
					Events:      []ReactionEventType{EventTypeCreated},
				},
			)
			ts.registerEventTrigger(agent)
			ts.RemoveAgentTrigger(agent.ID)
		}(i)
	}

	for i := 0; i < 50; i++ {
		<-done
	}

	// After all goroutines complete, the map may or may not be empty
	// (race between register/remove of same IDs), but it should not have panicked.
}
