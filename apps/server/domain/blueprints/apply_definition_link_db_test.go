package blueprints

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

// TestReapplyAfterDefinitionDeleteAndRecreate is the #789 regression: the
// definition-link unique index (kb.uq_agents_project_agent_definition) rejects a
// second linked runtime row, and the blueprint apply's EXISTING-row branch used
// to write agent_definition_id without handling that conflict — so the raw 23505
// surfaced as a 500.
//
// The four-step sequence from the issue, driven through the real apply path:
//
//  1. apply a blueprint that binds a runtime agent to a definition;
//  2. delete the definition (the FK is ON DELETE SET NULL, so the
//     blueprint-owned runtime row survives, unlinked);
//  3. re-create the definition through the agents API with object rules, which
//     materializes a second, linked runtime row that now owns the definition;
//  4. re-apply the blueprint.
//
// Step 4 must not answer 500. The apply recovers exactly like the create path:
// it resolves the row that owns the link, and because that row is not stamped
// with this blueprint's ownership it is left alone rather than repurposed (the
// same ownership gate the create path applies — repurposing it would rebind its
// trigger and let Unapply delete a row this blueprint never created). The
// definition therefore ends with exactly one linked runtime row: the API one.
func TestReapplyAfterDefinitionDeleteAndRecreate(t *testing.T) {
	db := connectTestDB(t)
	svc := newApplyService(t, db)
	ctx := context.Background()
	_, projectID := seedProject(t, db)

	agentName := uniqueName("deflink-agent")
	manifest := BlueprintManifest{Agents: []AgentManifest{{
		Name:        agentName,
		Description: "definition-link re-apply agent",
		WorkConfig: map[string]any{
			"status": map[string]any{"ready": "todo", "inProgress": "doing", "done": "done"},
		},
		TriggerType: "reaction",
		ReactionConfig: &agents.ReactionConfig{
			ObjectTypes:         []string{"Task"},
			Events:              []agents.ReactionEventType{agents.EventTypeCreated},
			ConcurrencyStrategy: agents.ConcurrencySkip,
		},
	}}}
	rawManifest, err := json.Marshal(manifest)
	require.NoError(t, err)

	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name:      uniqueName("deflink-bp"),
		Version:   "1.0.0",
		ProjectID: &projectID,
		Manifest:  rawManifest,
	})
	require.NoError(t, err)
	_, err = svc.PublishBlueprint(ctx, projectID, bp.ID)
	require.NoError(t, err)

	// Step 1: apply — the blueprint-owned runtime row owns the definition link.
	_, err = svc.Apply(ctx, bp.ID, projectID, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)

	agentRepo := agents.NewRepository(db)

	firstDef, err := agentRepo.FindDefinitionByName(ctx, projectID, agentName)
	require.NoError(t, err)
	require.NotNil(t, firstDef, "apply must materialize the definition")

	linkedIDs := func(definitionID string) []string {
		t.Helper()
		var ids []string
		require.NoError(t, db.NewRaw(
			`SELECT id FROM kb.agents WHERE project_id = ? AND agent_definition_id = ? ORDER BY created_at, id`,
			projectID, definitionID).Scan(ctx, &ids))
		return ids
	}

	owned := linkedIDs(firstDef.ID)
	require.Len(t, owned, 1, "apply must bind exactly one runtime row to the definition")
	blueprintRowID := owned[0]

	// Step 2: delete the definition. ON DELETE SET NULL leaves the
	// blueprint-owned row in place, unlinked.
	_, err = db.ExecContext(ctx, `DELETE FROM kb.agent_definitions WHERE id = ?`, firstDef.ID)
	require.NoError(t, err)

	var unlinked *string
	require.NoError(t, db.NewRaw(
		`SELECT agent_definition_id FROM kb.agents WHERE id = ?`, blueprintRowID).Scan(ctx, &unlinked))
	require.Nil(t, unlinked, "deleting the definition must leave the runtime row unlinked")

	// Step 3: re-create the definition through the agents API path with object
	// rules. The API's write shape is a project-visibility definition plus the
	// reaction runtime row syncDefinitionWorkTriggers materializes for a
	// definition with object rules — the row that now owns the link.
	secondDefID := uuid.NewString()
	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.agent_definitions (id, project_id, name, visibility, enabled, work_config)
		VALUES (?, ?, ?, 'project', true, CAST(? AS jsonb))`,
		secondDefID, projectID, agentName, `{
			"status": {"ready": "todo", "inProgress": "doing", "done": "done"},
			"objectRules": [{"objectType": "Task", "triggers": [{"objectType": "Task", "status": "ready"}]}]
		}`)
	require.NoError(t, err)

	apiRowID := uuid.NewString()
	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.agents (id, project_id, name, strategy_type, cron_schedule, enabled, trigger_type, reaction_config, agent_definition_id)
		VALUES (?, ?, ?, ?, '0 0 * * *', true, 'reaction', CAST(? AS jsonb), ?)`,
		apiRowID, projectID, agentName, "agent-def:"+secondDefID,
		`{"objectTypes":["Task"],"events":["created","status_changed"],"concurrencyStrategy":"skip"}`,
		secondDefID)
	require.NoError(t, err)
	require.Len(t, linkedIDs(secondDefID), 1, "the re-created definition must own exactly one runtime row")

	// Step 4: re-apply. Before the fix this returned the raw 23505 from the
	// existing-row update branch (a 500 at the HTTP layer).
	_, err = svc.Apply(ctx, bp.ID, projectID, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err, "re-apply after a definition delete/re-create must not fail")

	// Exactly one row carries the definition link, and it is the one the
	// agents API created — the blueprint-owned row was not repurposed.
	linked := linkedIDs(secondDefID)
	require.Len(t, linked, 1, "re-apply must leave exactly one row carrying the definition link")
	assert.Equal(t, apiRowID, linked[0], "re-apply repurposed the API-created definition row")

	// The orphaned blueprint row survives, unlinked, and is still owned by the
	// blueprint (Unapply deletes it).
	var orphanLink *string
	var orphanBlueprint *string
	require.NoError(t, db.NewRaw(
		`SELECT agent_definition_id, config->>'sourceBlueprintId' FROM kb.agents WHERE id = ?`,
		blueprintRowID).Scan(ctx, &orphanLink, &orphanBlueprint))
	assert.Nil(t, orphanLink, "the blueprint row must stay unlinked")
	require.NotNil(t, orphanBlueprint)
	assert.Equal(t, bp.ID, *orphanBlueprint)

	// Two runtime rows, one link — no duplicate was minted by the re-apply.
	var total int
	require.NoError(t, db.NewRaw(
		`SELECT count(*) FROM kb.agents WHERE project_id = ? AND name = ?`, projectID, agentName).Scan(ctx, &total))
	assert.Equal(t, 2, total, "re-apply must not mint another runtime row")
}
