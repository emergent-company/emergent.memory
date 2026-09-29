package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActorAgentID verifies the actor provenance id is the agent DEFINITION id
// (kb.agent_definitions.id) — the canonical actor_id for actor_type='agent' —
// not the kb.agents run entity id, and is nil when no definition is present.
func TestActorAgentID(t *testing.T) {
	ae := &AgentExecutor{}
	defID := uuid.NewString()

	t.Run("definition id preferred over runtime agent id", func(t *testing.T) {
		req := ExecuteRequest{
			Agent:           &Agent{ID: uuid.NewString()},
			AgentDefinition: &AgentDefinition{ID: defID},
		}
		got := ae.actorAgentID(req)
		require.NotNil(t, got)
		assert.Equal(t, uuid.MustParse(defID), *got)
	})

	t.Run("nil when no definition even with a runtime agent", func(t *testing.T) {
		req := ExecuteRequest{
			Agent:           &Agent{ID: uuid.NewString()},
			AgentDefinition: nil,
		}
		assert.Nil(t, ae.actorAgentID(req))
	})

	t.Run("nil when definition id empty", func(t *testing.T) {
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{ID: ""}}
		assert.Nil(t, ae.actorAgentID(req))
	})

	t.Run("nil when definition id unparseable", func(t *testing.T) {
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{ID: "not-a-uuid"}}
		assert.Nil(t, ae.actorAgentID(req))
	})
}

// TestResolveAgentDefinitionID verifies the per-agent MCP endpoint path maps a
// kb.agents id to its definition id and returns "" when unresolvable (agent
// missing, definition missing, or repo unavailable), so the caller skips
// stamping and leaves the write unattributed.
func TestResolveAgentDefinitionID(t *testing.T) {
	defID := uuid.NewString()

	t.Run("resolves definition id", func(t *testing.T) {
		h := &MCPToolHandler{onceRepo: &fakeOnceRepo{
			agent: &Agent{ID: uuid.NewString()},
			def:   &AgentDefinition{ID: defID},
		}}
		assert.Equal(t, defID, h.ResolveAgentDefinitionID(context.Background(), "proj", "agent-id"))
	})

	t.Run("empty when agent not found", func(t *testing.T) {
		h := &MCPToolHandler{onceRepo: &fakeOnceRepo{
			agent: nil,
			def:   &AgentDefinition{ID: defID},
		}}
		assert.Equal(t, "", h.ResolveAgentDefinitionID(context.Background(), "proj", "agent-id"))
	})

	t.Run("empty when definition unresolvable", func(t *testing.T) {
		h := &MCPToolHandler{onceRepo: &fakeOnceRepo{
			agent: &Agent{ID: uuid.NewString()},
			def:   nil,
		}}
		assert.Equal(t, "", h.ResolveAgentDefinitionID(context.Background(), "proj", "agent-id"))
	})

	t.Run("empty when repo unavailable", func(t *testing.T) {
		h := &MCPToolHandler{}
		assert.Equal(t, "", h.ResolveAgentDefinitionID(context.Background(), "proj", "agent-id"))
	})
}
