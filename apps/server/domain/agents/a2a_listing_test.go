package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// These cover the pure listing helpers extracted from ListA2ARuns. The
// ListA2ARuns method itself resolves resume chains via FindLatestRunInChain
// against the database, so its full behaviour requires DB-level verification
// (no live DB is available in this test suite).
func TestA2AListingRun_UsesTailStateRootID(t *testing.T) {
	root := &AgentRun{ID: "root-1", Status: RunStatusRunning}
	tail := &AgentRun{ID: "child-1", Status: RunStatusSuccess, ACPSessionID: strPtr("sess-1")}

	effective := a2aListingRun(root, tail)

	// The task id must stay the stable root id, but the state must come from the
	// chain tail.
	assert.Equal(t, "root-1", effective.ID, "listing task id must be the stable root id")
	assert.Equal(t, RunStatusSuccess, effective.Status, "listing status must be the chain tail's status")
	assert.Equal(t, "sess-1", derefString(effective.ACPSessionID))
}

func TestA2AListingRun_NilTailFallsBackToRoot(t *testing.T) {
	root := &AgentRun{ID: "root-1", Status: RunStatusSuccess}

	effective := a2aListingRun(root, nil)

	assert.Equal(t, "root-1", effective.ID)
	assert.Equal(t, RunStatusSuccess, effective.Status)
}

func TestContainsAgentRunStatus(t *testing.T) {
	assert.True(t, containsAgentRunStatus([]AgentRunStatus{RunStatusSuccess, RunStatusError}, RunStatusError))
	assert.False(t, containsAgentRunStatus([]AgentRunStatus{RunStatusSuccess}, RunStatusError))
	assert.False(t, containsAgentRunStatus(nil, RunStatusSuccess))
}
