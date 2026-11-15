package blueprints

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// conflictOnUpdateRepo returns the definition-link unique violation (#789) for
// the UpdateAgent of failOnID, so ensureRuntimeAgent's bind-conflict recovery
// can be exercised without Postgres. Every other method is the in-memory fake.
//
// A failed UPDATE rolls back in Postgres, so the rejected row is restored to
// its pre-update state. Without the rollback the failed candidate keeps the
// definition FK the stamp assigned it and the recovery lookup below would
// resolve the loser as the link owner instead of the row that really holds it.
type conflictOnUpdateRepo struct {
	*fakeAgentRepo
	failOnID  string
	preUpdate map[string]*agents.Agent
}

func (f *conflictOnUpdateRepo) UpdateAgent(ctx context.Context, a *agents.Agent) error {
	if a.ID == f.failOnID {
		if saved := f.preUpdate[a.ID]; saved != nil {
			*a = *saved
		}
		return errors.New(`ERROR: duplicate key value violates unique constraint "uq_agents_project_agent_definition" (SQLSTATE 23505)`)
	}
	return f.fakeAgentRepo.UpdateAgent(ctx, a)
}

// cloneAgent copies an agent deeply enough that a restored row does not share
// its Config map with the stamp that failed.
func cloneAgent(a *agents.Agent) *agents.Agent {
	if a == nil {
		return nil
	}
	cp := *a
	if a.Config != nil {
		cp.Config = make(map[string]any, len(a.Config))
		for k, v := range a.Config {
			cp.Config[k] = v
		}
	}
	return &cp
}

// TestEnsureRuntimeAgentRecoversFromDefinitionLinkConflict pins the #789
// recovery branches of the existing-row update: when the update of the
// blueprint-owned row collides with the row that actually owns the definition
// link, the manifest config is applied to that winner when the blueprint owns
// it, the winner is left untouched when another owner holds it, and an
// unresolvable conflict is a legible 409 rather than a raw 23505.
func TestEnsureRuntimeAgentRecoversFromDefinitionLinkConflict(t *testing.T) {
	const (
		projectID  = "proj-1"
		blueprint  = "bp-1"
		definition = "def-2"
	)

	manifest := AgentManifest{
		Name: "board-agent",
		ReactionConfig: &agents.ReactionConfig{
			ObjectTypes:         []string{"Task"},
			Events:              []agents.ReactionEventType{agents.EventTypeCreated},
			ConcurrencyStrategy: agents.ConcurrencySkip,
		},
	}

	newCase := func(winnerConfig map[string]any) (*Service, *conflictOnUpdateRepo, *agents.AgentDefinition) {
		fake := newFakeAgentRepo()
		// The blueprint-owned row survived an earlier apply unlinked (its
		// definition was deleted), so the update below tries to link it and
		// collides with the row that already owns the link.
		fake.agentsByName["board-agent"] = &agents.Agent{
			ID: "bp-row", ProjectID: projectID, Name: "board-agent",
			Config: map[string]any{"sourceBlueprintId": blueprint},
		}
		if winnerConfig != nil {
			defID := definition
			fake.agentsByName["winner-row"] = &agents.Agent{
				ID: "winner-row", ProjectID: projectID, Name: "board-agent (winner)",
				Enabled: true, Config: winnerConfig, AgentDefinitionID: &defID,
			}
		}
		repo := &conflictOnUpdateRepo{
			fakeAgentRepo: fake,
			failOnID:      "bp-row",
			preUpdate:     map[string]*agents.Agent{"bp-row": cloneAgent(fake.agentsByName["board-agent"])},
		}
		return &Service{agentRepo: repo, log: testLogger()}, repo, &agents.AgentDefinition{
			ID: definition, ProjectID: projectID, Name: "board-agent",
		}
	}

	t.Run("winner owned by this blueprint is stamped", func(t *testing.T) {
		svc, fake, def := newCase(map[string]any{"sourceBlueprintId": blueprint})
		got, err := svc.ensureRuntimeAgent(context.Background(), projectID, blueprint, def, &manifest)
		require.NoError(t, err, "recovery must not surface the unique violation")
		require.NotNil(t, got)
		assert.Equal(t, "winner-row", got.ID, "the manifest config must be applied to the row that owns the link")
		assert.Equal(t, definition, *got.AgentDefinitionID)
		assert.Equal(t, agents.TriggerTypeReaction, got.TriggerType)
		assert.True(t, got.Enabled)
		assert.Len(t, fake.updatedAgents, 1)
	})

	t.Run("winner owned elsewhere is never repurposed", func(t *testing.T) {
		svc, fake, def := newCase(map[string]any{"sourceBlueprintId": "other-bp"})
		got, err := svc.ensureRuntimeAgent(context.Background(), projectID, blueprint, def, &manifest)
		require.NoError(t, err)
		assert.Nil(t, got, "another owner's row must not be adopted")
		assert.Empty(t, fake.updatedAgents, "another owner's row must not be rewritten")
	})

	t.Run("unresolvable conflict is a 409", func(t *testing.T) {
		svc, _, def := newCase(nil)
		_, err := svc.ensureRuntimeAgent(context.Background(), projectID, blueprint, def, &manifest)
		require.Error(t, err)
		assertAppErrorCode(t, err, apperror.ErrConflict.Code)
		assert.Contains(t, err.Error(), "board-agent", "the conflict must name the agent")
	})
}
