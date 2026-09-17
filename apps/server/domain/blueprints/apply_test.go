package blueprints

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/skills"
)

// newApplyService wires the blueprints repository plus the real skills and
// agents repositories (the only cross-domain deps an agents-only manifest
// touches: applySkills lists global skills; applyAgents needs the agents repo).
// schemas/graph deps stay nil because an agents-only manifest never reaches
// them (empty packs loop no-ops, seed is nil).
func newApplyService(t *testing.T, db *bun.DB) *Service {
	t.Helper()
	repo := NewRepository(db, testLogger())
	skillsRepo := skills.NewRepository(db, testLogger())
	svc := NewService(ServiceParams{Repo: repo, SkillsRepo: skillsRepo, Log: testLogger()})
	svc.agentRepo = agents.NewRepository(db)
	return svc
}

// TestApply_AgentMaterialization_NonDestructiveUpdate is the apply integration
// test: a published blueprint with an agents-only manifest materializes an
// agent definition, and re-applying a newer version with a changed system
// prompt updates the prompt WITHOUT clobbering unmanaged fields (Enabled).
func TestApply_AgentMaterialization_NonDestructiveUpdate(t *testing.T) {
	db := connectTestDB(t)
	svc := newApplyService(t, db)
	ctx := context.Background()

	_, projectID := seedProject(t, db)
	agentName := uniqueName("test-agent")

	makeManifest := func(prompt string) json.RawMessage {
		raw, err := json.Marshal(BlueprintManifest{
			Agents: []AgentManifest{{
				Name:         agentName,
				Description:  "apply test agent",
				SystemPrompt: prompt,
				Tools:        []string{"skill-list"},
				BannedTools:  []string{"memory-wipe"},
			}},
		})
		require.NoError(t, err)
		return raw
	}

	// Create + publish the blueprint (private to the project so it can be
	// published — global built-ins are immutable).
	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name:      uniqueName("apply-bp"),
		Version:   "1.0.0",
		Manifest:  makeManifest("hi"),
		ProjectID: &projectID,
	})
	require.NoError(t, err)
	_, err = svc.PublishBlueprint(ctx, projectID, bp.ID)
	require.NoError(t, err)

	// First apply creates the agent definition (Enabled defaults to true).
	res, err := svc.Apply(ctx, bp.ID, projectID, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Agents.Created)
	assert.Equal(t, 0, res.Agents.Updated)

	agentRepo := agents.NewRepository(db)
	def, err := agentRepo.FindDefinitionByName(ctx, projectID, agentName)
	require.NoError(t, err)
	require.NotNil(t, def, "agent definition must exist after apply")
	assert.Equal(t, "hi", derefString(def.SystemPrompt))
	assert.True(t, def.Enabled, "new agents must default to enabled")
	assert.Equal(t, []string{"memory-wipe"}, def.BannedTools, "bannedTools must be applied on create")

	// Simulate a runtime change: disable the agent behind the blueprint's back.
	def.Enabled = false
	require.NoError(t, agentRepo.UpdateDefinition(ctx, def))

	// Newer blueprint version with a changed prompt, published and applied.
	clone, err := svc.NewVersion(ctx, projectID, bp.ID, "1.1.0")
	require.NoError(t, err)
	clone, err = svc.UpdateBlueprint(ctx, projectID, clone.ID, &UpdateBlueprintRequest{Manifest: makeManifest("hi v2")})
	require.NoError(t, err)
	_, err = svc.PublishBlueprint(ctx, projectID, clone.ID)
	require.NoError(t, err)

	res, err = svc.Apply(ctx, clone.ID, projectID, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Agents.Updated)
	assert.Equal(t, 0, res.Agents.Created)

	// Prompt updated, Enabled preserved (non-destructive update).
	def2, err := agentRepo.FindDefinitionByName(ctx, projectID, agentName)
	require.NoError(t, err)
	require.NotNil(t, def2)
	assert.Equal(t, "hi v2", derefString(def2.SystemPrompt), "manifest-driven field must update")
	assert.False(t, def2.Enabled, "Enabled is not manifest-driven and must be preserved")
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// TestApplyAgentManifestToExisting_BannedTools verifies bannedTools is
// preserve-when-omitted on the update path: a manifest that omits the field
// keeps the user's existing value, while a manifest that provides it overwrites.
func TestApplyAgentManifestToExisting_BannedTools(t *testing.T) {
	def := &agents.AgentDefinition{BannedTools: []string{"user-set"}}

	// Omitted -> preserve user's value.
	applyAgentManifestToExisting(def, &AgentManifest{Name: "x"})
	assert.Equal(t, []string{"user-set"}, def.BannedTools)

	// Provided -> overwrite from manifest.
	applyAgentManifestToExisting(def, &AgentManifest{Name: "x", BannedTools: []string{"blueprint-set"}})
	assert.Equal(t, []string{"blueprint-set"}, def.BannedTools)
}

// TestAgentUI_ManifestToUIConfig locks the appearance contract: an inline
// `ui: {icon, color}` block is marshalled into the definition's opaque
// UIConfig JSONB blob, and an absent/empty block leaves ui_config untouched.
func TestAgentUI_ManifestToUIConfig(t *testing.T) {
	// Create path: full icon+color block lands in UIConfig.
	def := buildAgentDefinition(&AgentManifest{
		Name: "x",
		UI:   &AgentUIManifest{Icon: "bot", Color: "#FF00AA"},
	}, "proj-1")
	require.NotNil(t, def.UIConfig)
	assert.JSONEq(t, `{"icon":"bot","color":"#FF00AA"}`, string(def.UIConfig))

	// Icon-only block omits the empty color key.
	def = buildAgentDefinition(&AgentManifest{
		Name: "x",
		UI:   &AgentUIManifest{Icon: "bot"},
	}, "proj-1")
	assert.JSONEq(t, `{"icon":"bot"}`, string(def.UIConfig))

	// Empty block -> no UIConfig (DB default '{}').
	def = buildAgentDefinition(&AgentManifest{Name: "x", UI: &AgentUIManifest{}}, "proj-1")
	assert.Nil(t, def.UIConfig)

	// Update path: provided block overwrites; omitted block preserves.
	existing := &agents.AgentDefinition{UIConfig: json.RawMessage(`{"icon":"old"}`)}
	applyAgentManifestToExisting(existing, &AgentManifest{Name: "x", UI: &AgentUIManifest{Icon: "new", Color: "#000000"}})
	assert.JSONEq(t, `{"icon":"new","color":"#000000"}`, string(existing.UIConfig))

	existing = &agents.AgentDefinition{UIConfig: json.RawMessage(`{"icon":"keep"}`)}
	applyAgentManifestToExisting(existing, &AgentManifest{Name: "x"})
	assert.JSONEq(t, `{"icon":"keep"}`, string(existing.UIConfig))
}
