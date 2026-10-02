package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/scheduler"
)

// TestNewTriggerService_WiresAgentDeletionListener proves the wiring that makes
// trigger teardown part of the delete: a zero-value Repository (no DB needed)
// receives the listener on construction, and notifying it drops both the cron
// and reaction registrations.
func TestNewTriggerService_WiresAgentDeletionListener(t *testing.T) {
	repo := &Repository{}
	sched := scheduler.NewScheduler(agentDeleteTestLogger())
	ts := NewTriggerService(sched, nil, repo, nil, agentDeleteTestLogger())

	agent := makeTestAgent("wire-1", "wire-agent", "p1", &ReactionConfig{
		ObjectTypes: []string{"document"},
		Events:      []ReactionEventType{EventTypeCreated},
	})
	ts.registerEventTrigger(agent)
	registerCronForTest(t, sched, "wire-1")

	require.Len(t, ts.GetEventListeners("document:created"), 1)
	require.Contains(t, sched.ListTasks(), triggerTaskName("wire-1"))

	// This is exactly what Repository.Delete / DeleteAgentsBySourceBlueprint
	// invoke after a successful row delete.
	repo.notifyAgentsDeleted([]string{"wire-1"})

	assert.Empty(t, ts.GetEventListeners("document:created"))
	assert.NotContains(t, sched.ListTasks(), triggerTaskName("wire-1"))
}
