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
	"github.com/emergent-company/emergent.memory/pkg/apperror"
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

// TestValidateManifest_StaticErrors locks the validate-before-materialize
// contract: a malformed agent model reference, bad visibility, missing pack
// name/version, or invalid skill all surface a 400 WITHOUT any database write.
// An empty model name is allowed (provider default).
func TestValidateManifest_StaticErrors(t *testing.T) {
	svc := &Service{skillsRepo: skills.NewRepository(nil, testLogger())}

	t.Run("bare model name rejected", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Agents: []AgentManifest{{Name: "a", Model: &AgentModelManifest{Name: "gemini-2.5-flash"}}},
		})
		require.Error(t, err)
		assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
		assert.Contains(t, err.Error(), `agent "a"`)
	})

	t.Run("empty model name allowed", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Agents: []AgentManifest{{Name: "a", Model: &AgentModelManifest{}}},
		})
		require.NoError(t, err)
	})

	t.Run("invalid visibility rejected", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Agents: []AgentManifest{{Name: "a", Visibility: "public"}},
		})
		require.Error(t, err)
		assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
	})

	t.Run("missing pack version rejected", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Packs: []PackManifest{{Name: "p"}},
		})
		require.Error(t, err)
		assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
	})

	t.Run("skill missing description rejected", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Skills: []SkillManifest{{Name: "s", Content: "body"}},
		})
		require.Error(t, err)
	})

	// #354: the server-side apply path must reject a cron the trigger service
	// refuses, with the agent name and value in the error, before materializing
	// anything.
	t.Run("6-field cron on schedule agent rejected", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Agents: []AgentManifest{{Name: "nightly", TriggerType: "schedule", CronSchedule: "0 0 5 * * *"}},
		})
		require.Error(t, err)
		assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
		assert.Contains(t, err.Error(), `agent "nightly"`)
		assert.Contains(t, err.Error(), "0 0 5 * * *")
		assert.Contains(t, err.Error(), "expected exactly 5 fields")
	})

	t.Run("implicit schedule cron rejected", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Agents: []AgentManifest{{Name: "nightly", CronSchedule: "not-a-cron"}},
		})
		require.Error(t, err)
		assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
	})

	t.Run("sub-minimum cron rejected", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Agents: []AgentManifest{{Name: "chatty", TriggerType: "schedule", CronSchedule: "* * * * *"}},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "minimum")
	})

	t.Run("cron on reaction agent ignored", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Agents: []AgentManifest{{
				Name:           "triager",
				TriggerType:    "reaction",
				ReactionConfig: &agents.ReactionConfig{Events: []agents.ReactionEventType{agents.EventTypeCreated}},
				CronSchedule:   "0 0 5 * * *",
			}},
		})
		require.NoError(t, err, "reaction agents store the cron unused; the agents API skips it too")
	})

	t.Run("valid 5-field cron accepted", func(t *testing.T) {
		err := svc.validateManifest(&BlueprintManifest{
			Agents: []AgentManifest{{Name: "nightly", TriggerType: "schedule", CronSchedule: "0 3 * * *"}},
		})
		require.NoError(t, err)
	})
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// TestApplyModelOverrides locks the in-memory override helper: a matching agent
// name sets/replaces the model name (creating Model when nil) and preserves
// temperature/maxTokens/enableThinking; an empty value clears the name (project
// default); non-matching agents are untouched; an empty map is a no-op.
func TestApplyModelOverrides(t *testing.T) {
	t.Run("sets name and creates model when nil", func(t *testing.T) {
		m := &BlueprintManifest{Agents: []AgentManifest{{Name: "a"}}}
		applyModelOverrides(m, map[string]string{"a": "openai/gpt-4o"})
		require.NotNil(t, m.Agents[0].Model)
		assert.Equal(t, "openai/gpt-4o", m.Agents[0].Model.Name)
	})

	t.Run("empty override clears name", func(t *testing.T) {
		m := &BlueprintManifest{Agents: []AgentManifest{{Name: "a", Model: &AgentModelManifest{Name: "google/gemini-2.5-flash"}}}}
		applyModelOverrides(m, map[string]string{"a": ""})
		require.NotNil(t, m.Agents[0].Model)
		assert.Empty(t, m.Agents[0].Model.Name)
	})

	t.Run("preserves tuning fields", func(t *testing.T) {
		temp := float32(0.7)
		maxTok := 1000
		thinking := true
		m := &BlueprintManifest{Agents: []AgentManifest{{Name: "a", Model: &AgentModelManifest{
			Name: "google/gemini-2.5-flash", Temperature: &temp, MaxTokens: &maxTok, EnableThinking: &thinking,
		}}}}
		applyModelOverrides(m, map[string]string{"a": "openai/gpt-4o"})
		require.NotNil(t, m.Agents[0].Model)
		assert.Equal(t, "openai/gpt-4o", m.Agents[0].Model.Name)
		assert.Equal(t, temp, *m.Agents[0].Model.Temperature)
		assert.Equal(t, maxTok, *m.Agents[0].Model.MaxTokens)
		assert.Equal(t, thinking, *m.Agents[0].Model.EnableThinking)
	})

	t.Run("non-matching agent untouched", func(t *testing.T) {
		m := &BlueprintManifest{Agents: []AgentManifest{{Name: "other", Model: &AgentModelManifest{Name: "google/gemini-2.5-flash"}}}}
		applyModelOverrides(m, map[string]string{"a": "openai/gpt-4o"})
		assert.Equal(t, "google/gemini-2.5-flash", m.Agents[0].Model.Name)
	})

	t.Run("empty map is a no-op", func(t *testing.T) {
		m := &BlueprintManifest{Agents: []AgentManifest{{Name: "a"}}}
		applyModelOverrides(m, map[string]string{})
		assert.Nil(t, m.Agents[0].Model)
	})
}

// TestApply_ModelOverrides_PrecedenceAndPersistence is the apply-time resolution
// integration test: the manifest model is used with no override, a stored
// per-project override wins on a later apply, an explicit request override wins
// over the stored one, and the explicit override is persisted after a successful
// apply.
func TestApply_ModelOverrides_PrecedenceAndPersistence(t *testing.T) {
	db := connectTestDB(t)
	svc := newApplyService(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	agentName := uniqueName("mo-agent")
	makeManifest := func(model string) json.RawMessage {
		raw, err := json.Marshal(BlueprintManifest{
			Agents: []AgentManifest{{Name: agentName, Model: &AgentModelManifest{Name: model}}},
		})
		require.NoError(t, err)
		return raw
	}

	bp, err := svc.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name: uniqueName("mo-bp"), Version: "1.0.0", Manifest: makeManifest("google/gemini-2.5-flash"), ProjectID: &proj,
	})
	require.NoError(t, err)
	_, err = svc.PublishBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)

	agentRepo := agents.NewRepository(db)
	modelOf := func() string {
		def, err := agentRepo.FindDefinitionByName(ctx, proj, agentName)
		require.NoError(t, err)
		require.NotNil(t, def)
		return def.Model.Name
	}

	// No overrides: the manifest-pinned model is used.
	res, err := svc.Apply(ctx, bp.ID, proj, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Agents.Created)
	assert.Equal(t, "google/gemini-2.5-flash", modelOf())

	// Store a per-project override; re-apply a newer version with no explicit
	// overrides: the STORED override wins over the manifest pin.
	_, err = svc.UpsertModelOverrides(ctx, proj, bp.Name, map[string]string{agentName: "openai/gpt-4o"})
	require.NoError(t, err)

	clone, err := svc.NewVersion(ctx, proj, bp.ID, "1.1.0")
	require.NoError(t, err)
	_, err = svc.PublishBlueprint(ctx, proj, clone.ID)
	require.NoError(t, err)

	_, err = svc.Apply(ctx, clone.ID, proj, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err)
	assert.Equal(t, "openai/gpt-4o", modelOf(), "stored override must win over the manifest pin")

	// An explicit request override wins over the stored one, and is persisted.
	_, err = svc.Apply(ctx, clone.ID, proj, uuid.NewString(), ApplyOptions{
		ModelOverrides: map[string]string{agentName: "anthropic/claude-sonnet"},
	})
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-sonnet", modelOf(), "explicit override must win over stored")

	overrides, err := svc.GetModelOverrides(ctx, proj, bp.Name)
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-sonnet", overrides[agentName], "explicit override must be persisted")
}

// TestApplyAgentManifestToExisting_BannedTools verifies bannedTools is
// preserve-when-omitted on the update path: a manifest that omits the field
// keeps the user's existing value, while a manifest that provides it overwrites.
func TestApplyAgentManifestToExisting_BannedTools(t *testing.T) {
	def := &agents.AgentDefinition{BannedTools: []string{"user-set"}}

	// Omitted -> preserve user's value.
	require.NoError(t, applyAgentManifestToExisting(def, &AgentManifest{Name: "x"}))
	assert.Equal(t, []string{"user-set"}, def.BannedTools)

	// Provided -> overwrite from manifest.
	require.NoError(t, applyAgentManifestToExisting(def, &AgentManifest{Name: "x", BannedTools: []string{"blueprint-set"}}))
	assert.Equal(t, []string{"blueprint-set"}, def.BannedTools)
}

// TestAgentModel_ManifestValidatedAgainstFunnel proves the blueprint apply
// write path routes model.name through the shared agent model-reference funnel
// (#1465) instead of copying it verbatim: a malformed manifest model is rejected
// before persist, a valid reference is canonicalized, and an empty model is
// allowed to select the provider default. The update path must not clobber the
// existing model when the manifest value is malformed.
func TestAgentModel_ManifestValidatedAgainstFunnel(t *testing.T) {
	// Create path: malformed references are rejected.
	for _, bad := range []string{"/deepseek-v4-flash", "openai//gpt-4o", "gpt-4o", "openai/"} {
		_, err := buildAgentDefinition(&AgentManifest{Name: "x", Model: &AgentModelManifest{Name: bad}}, "proj-1")
		require.Error(t, err, "manifest model %q must be rejected", bad)
	}

	// Create path: a valid reference is canonicalized (whitespace collapsed).
	def, err := buildAgentDefinition(&AgentManifest{Name: "x", Model: &AgentModelManifest{Name: " openai / gpt-4o "}}, "proj-1")
	require.NoError(t, err)
	require.NotNil(t, def.Model)
	assert.Equal(t, "openai/gpt-4o", def.Model.Name)

	// Create path: empty model selects the provider default.
	def, err = buildAgentDefinition(&AgentManifest{Name: "x", Model: &AgentModelManifest{}}, "proj-1")
	require.NoError(t, err)
	require.NotNil(t, def.Model)
	assert.Empty(t, def.Model.Name)

	// Update path: malformed value is rejected and leaves the existing model intact.
	existing := &agents.AgentDefinition{Model: &agents.ModelConfig{Name: "google/gemini-2.5-flash"}}
	err = applyAgentManifestToExisting(existing, &AgentManifest{Name: "x", Model: &AgentModelManifest{Name: "/bad"}})
	require.Error(t, err)
	assert.Equal(t, "google/gemini-2.5-flash", existing.Model.Name, "malformed manifest must not clobber the existing model")

	// Update path: valid value is applied and canonicalized.
	existing = &agents.AgentDefinition{}
	require.NoError(t, applyAgentManifestToExisting(existing, &AgentManifest{Name: "x", Model: &AgentModelManifest{Name: " openai / gpt-4o "}}))
	assert.Equal(t, "openai/gpt-4o", existing.Model.Name)
}

// TestAgentUI_ManifestToUIConfig locks the appearance contract: an inline
// `ui: {icon, color}` block is marshalled into the definition's opaque
// UIConfig JSONB blob, and an absent/empty block leaves ui_config untouched.
func TestAgentUI_ManifestToUIConfig(t *testing.T) {
	// Create path: full icon+color block lands in UIConfig.
	def, err := buildAgentDefinition(&AgentManifest{
		Name: "x",
		UI:   &AgentUIManifest{Icon: "bot", Color: "#FF00AA"},
	}, "proj-1")
	require.NoError(t, err)
	require.NotNil(t, def.UIConfig)
	assert.JSONEq(t, `{"icon":"bot","color":"#FF00AA"}`, string(def.UIConfig))

	// Icon-only block omits the empty color key.
	def, err = buildAgentDefinition(&AgentManifest{
		Name: "x",
		UI:   &AgentUIManifest{Icon: "bot"},
	}, "proj-1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"icon":"bot"}`, string(def.UIConfig))

	// Empty block -> no UIConfig (DB default '{}').
	def, err = buildAgentDefinition(&AgentManifest{Name: "x", UI: &AgentUIManifest{}}, "proj-1")
	require.NoError(t, err)
	assert.Nil(t, def.UIConfig)

	// Update path: provided block overwrites; omitted block preserves.
	existing := &agents.AgentDefinition{UIConfig: json.RawMessage(`{"icon":"old"}`)}
	require.NoError(t, applyAgentManifestToExisting(existing, &AgentManifest{Name: "x", UI: &AgentUIManifest{Icon: "new", Color: "#000000"}}))
	assert.JSONEq(t, `{"icon":"new","color":"#000000"}`, string(existing.UIConfig))

	existing = &agents.AgentDefinition{UIConfig: json.RawMessage(`{"icon":"keep"}`)}
	require.NoError(t, applyAgentManifestToExisting(existing, &AgentManifest{Name: "x"}))
	assert.JSONEq(t, `{"icon":"keep"}`, string(existing.UIConfig))
}

// fakeAgentRepo is an in-memory AgentRepo for unit-testing applyAgents and
// Unapply without a database. It records the runtime agents it created/updated
// and stubs definition CRUD.
type fakeAgentRepo struct {
	defsByName   map[string]*agents.AgentDefinition
	agentsByName map[string]*agents.Agent

	createdAgents []*agents.Agent
	updatedAgents []*agents.Agent
}

func newFakeAgentRepo() *fakeAgentRepo {
	return &fakeAgentRepo{
		defsByName:   map[string]*agents.AgentDefinition{},
		agentsByName: map[string]*agents.Agent{},
	}
}

func (f *fakeAgentRepo) FindDefinitionByName(ctx context.Context, projectID, name string) (*agents.AgentDefinition, error) {
	return f.defsByName[name], nil
}
func (f *fakeAgentRepo) CreateDefinition(ctx context.Context, def *agents.AgentDefinition) error {
	if def.ID == "" {
		def.ID = uuid.NewString()
	}
	f.defsByName[def.Name] = def
	return nil
}
func (f *fakeAgentRepo) UpdateDefinition(ctx context.Context, def *agents.AgentDefinition) error {
	f.defsByName[def.Name] = def
	return nil
}
func (f *fakeAgentRepo) DeleteDefinition(ctx context.Context, id string) error {
	for n, d := range f.defsByName {
		if d.ID == id {
			delete(f.defsByName, n)
		}
	}
	return nil
}
func (f *fakeAgentRepo) FindAgentByName(ctx context.Context, projectID, name string) (*agents.Agent, error) {
	return f.agentsByName[name], nil
}
func (f *fakeAgentRepo) CreateAgent(ctx context.Context, a *agents.Agent) error {
	f.agentsByName[a.Name] = a
	f.createdAgents = append(f.createdAgents, a)
	return nil
}
func (f *fakeAgentRepo) UpdateAgent(ctx context.Context, a *agents.Agent) error {
	f.agentsByName[a.Name] = a
	f.updatedAgents = append(f.updatedAgents, a)
	return nil
}
func (f *fakeAgentRepo) DeleteAgentsBySourceBlueprint(ctx context.Context, projectID, blueprintID string) (int, error) {
	n := 0
	for name, a := range f.agentsByName {
		if a.ProjectID != projectID {
			continue
		}
		if v, ok := a.Config["sourceBlueprintId"]; ok && v == blueprintID {
			delete(f.agentsByName, name)
			n++
		}
	}
	return n, nil
}

// TestApplyAgents_CreatesRuntimeAgentWithWorkConfig verifies a manifest that
// declares object-driven work maps its workConfig onto the definition and
// creates a runtime kb.agents row bound to it, stamped with blueprint ownership.
func TestApplyAgents_CreatesRuntimeAgentWithWorkConfig(t *testing.T) {
	fake := newFakeAgentRepo()
	svc := &Service{agentRepo: fake, log: testLogger()}
	ctx := context.Background()

	am := AgentManifest{
		Name: "board-agent",
		WorkConfig: map[string]any{
			"status":         map[string]any{"ready": "todo", "inProgress": "doing", "done": "done"},
			"requiresReview": true,
		},
		TriggerType: "reaction",
		ReactionConfig: &agents.ReactionConfig{
			ObjectTypes:         []string{"Task"},
			Events:              []agents.ReactionEventType{agents.EventTypeCreated},
			ConcurrencyStrategy: agents.ConcurrencySkip,
		},
	}

	counts, err := svc.applyAgents(ctx, "proj-1", "bp-1", "bp", []AgentManifest{am})
	require.NoError(t, err)
	assert.Equal(t, 1, counts.Created)

	def := fake.defsByName["board-agent"]
	require.NotNil(t, def, "definition must be created")
	assert.Equal(t, "todo", def.WorkConfig.Status.Ready)
	assert.Equal(t, "doing", def.WorkConfig.Status.InProgress)
	assert.Equal(t, "done", def.WorkConfig.Status.Done)
	assert.True(t, def.WorkConfig.RequiresReview)

	rt := fake.agentsByName["board-agent"]
	require.NotNil(t, rt, "runtime agent must be created")
	assert.Equal(t, "proj-1", rt.ProjectID)
	assert.Equal(t, "board-agent", rt.Name)
	assert.Equal(t, agents.TriggerTypeReaction, rt.TriggerType)
	assert.Equal(t, "agent-def:"+def.ID, rt.StrategyType)
	assert.True(t, rt.Enabled)
	assert.NotEmpty(t, rt.CronSchedule, "cron_schedule is NOT NULL")
	require.NotNil(t, rt.ReactionConfig)
	assert.Equal(t, []string{"Task"}, rt.ReactionConfig.ObjectTypes)
	require.NotNil(t, rt.AgentDefinitionID)
	assert.Equal(t, def.ID, *rt.AgentDefinitionID)
	assert.Equal(t, "bp-1", rt.Config["sourceBlueprintId"], "ownership stamp must be merged into config")
}

// TestApplyAgents_UpdatesExistingRuntimeAgent verifies the update path applies
// workConfig and re-points an existing runtime agent instead of duplicating it.
func TestApplyAgents_UpdatesExistingRuntimeAgent(t *testing.T) {
	fake := newFakeAgentRepo()
	ctx := context.Background()

	existing := &agents.AgentDefinition{Name: "board-agent", ProjectID: "proj-1"}
	existing.ID = uuid.NewString()
	fake.defsByName["board-agent"] = existing
	fake.agentsByName["board-agent"] = &agents.Agent{
		ProjectID:         "proj-1",
		Name:              "board-agent",
		Config:            map[string]any{"queue": "fast", "sourceBlueprintId": "bp-1"},
		AgentDefinitionID: &existing.ID,
	}

	svc := &Service{agentRepo: fake, log: testLogger()}
	am := AgentManifest{
		Name:       "board-agent",
		WorkConfig: map[string]any{"requiresReview": true},
		ReactionConfig: &agents.ReactionConfig{
			ObjectTypes:         []string{"Task"},
			Events:              []agents.ReactionEventType{agents.EventTypeUpdated},
			ConcurrencyStrategy: agents.ConcurrencySkip,
		},
	}

	counts, err := svc.applyAgents(ctx, "proj-1", "bp-1", "bp", []AgentManifest{am})
	require.NoError(t, err)
	assert.Equal(t, 1, counts.Updated)

	def := fake.defsByName["board-agent"]
	assert.True(t, def.WorkConfig.RequiresReview, "update path must apply workConfig")

	rt := fake.agentsByName["board-agent"]
	require.NotNil(t, rt)
	assert.Equal(t, "fast", rt.Config["queue"], "existing config must be preserved")
	assert.Equal(t, "bp-1", rt.Config["sourceBlueprintId"])
	assert.Len(t, fake.updatedAgents, 1, "existing runtime agent updated, not duplicated")
	assert.Len(t, fake.createdAgents, 0)
}

// TestApplyAgents_InvalidCronRejectedAndNotPersisted is the #354 regression test:
// a blueprint that declares a cron the trigger service refuses (a 6-field
// expression, the form the docs used to show) must fail the apply with a clear
// BadRequest naming the agent and the value, and must NOT persist a scheduled
// runtime agent that would silently never fire.
func TestApplyAgents_InvalidCronRejectedAndNotPersisted(t *testing.T) {
	for _, tc := range []struct {
		name string
		am   AgentManifest
		want string
	}{
		{
			name: "6-field cron",
			am:   AgentManifest{Name: "nightly", TriggerType: "schedule", CronSchedule: "0 0 5 * * *"},
			want: "expected exactly 5 fields",
		},
		{
			name: "implicit schedule, malformed",
			am:   AgentManifest{Name: "nightly", CronSchedule: "not-a-cron"},
			want: "invalid cron expression",
		},
		{
			name: "below minimum interval",
			am:   AgentManifest{Name: "nightly", TriggerType: "schedule", CronSchedule: "* * * * *"},
			want: "below minimum",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeAgentRepo()
			svc := &Service{agentRepo: fake, log: testLogger()}

			counts, err := svc.applyAgents(context.Background(), "proj-1", "bp-1", "bp", []AgentManifest{tc.am})
			require.Error(t, err)
			assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
			assert.Contains(t, err.Error(), `agent "nightly"`, "error must name the agent")
			assert.Contains(t, err.Error(), tc.am.CronSchedule, "error must quote the cron value")
			assert.Contains(t, err.Error(), tc.want)

			assert.Empty(t, fake.createdAgents, "no scheduled agent may be persisted")
			assert.Empty(t, fake.agentsByName)
			assert.Empty(t, fake.updatedAgents)
			assert.Zero(t, counts.Created)
		})
	}
}

// TestApplyAgents_ValidCronPersistsScheduledAgent verifies the accepted path: a
// standard 5-field cron still materializes an enabled scheduled runtime agent.
func TestApplyAgents_ValidCronPersistsScheduledAgent(t *testing.T) {
	fake := newFakeAgentRepo()
	svc := &Service{agentRepo: fake, log: testLogger()}

	counts, err := svc.applyAgents(context.Background(), "proj-1", "bp-1", "bp", []AgentManifest{{
		Name:         "nightly",
		TriggerType:  "schedule",
		CronSchedule: "0 3 * * *",
	}})
	require.NoError(t, err)
	assert.Equal(t, 1, counts.Created)

	rt := fake.agentsByName["nightly"]
	require.NotNil(t, rt, "runtime agent must be created")
	assert.Equal(t, "0 3 * * *", rt.CronSchedule)
	assert.Equal(t, agents.TriggerTypeSchedule, rt.TriggerType)
	assert.True(t, rt.Enabled)
}

// TestApplyWorkConfig_MalformedInput verifies a malformed workConfig map returns
// a BadRequest error naming the agent (not a 500).
func TestApplyWorkConfig_MalformedInput(t *testing.T) {
	def := &agents.AgentDefinition{}
	err := applyWorkConfig(def, &AgentManifest{
		Name:       "broken",
		WorkConfig: map[string]any{"status": "not-an-object"},
	})
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)
	assert.Contains(t, err.Error(), "broken")
}

// TestBuildSeedObjectRequest_Assignee verifies the seed object request forwards
// a non-empty assignee and leaves it nil when absent.
func TestBuildSeedObjectRequest_Assignee(t *testing.T) {
	req := buildSeedObjectRequest(SeedObjectRecord{Type: "Task", Key: "k1", Assignee: "agent-x"})
	require.NotNil(t, req.Assignee)
	assert.Equal(t, "agent-x", *req.Assignee)

	req = buildSeedObjectRequest(SeedObjectRecord{Type: "Task", Key: "k1"})
	assert.Nil(t, req.Assignee)
}

// TestEnsureRuntimeAgent_ForeignOwnerNoHijack verifies a runtime agent that is
// not stamped with this blueprint's ownership is never repurposed, even when it
// happens to point at the same definition.
func TestEnsureRuntimeAgent_ForeignOwnerNoHijack(t *testing.T) {
	defID := uuid.NewString()

	tests := []struct {
		name     string
		existing *agents.Agent
	}{
		{
			name: "foreign blueprint owner",
			existing: &agents.Agent{
				ProjectID:         "proj-1",
				Name:              "board-agent",
				Config:            map[string]any{"sourceBlueprintId": "other-bp"},
				AgentDefinitionID: &defID,
			},
		},
		{
			name: "manual agent no ownership",
			existing: &agents.Agent{
				ProjectID: "proj-1",
				Name:      "board-agent",
				Config:    map[string]any{"queue": "fast"},
			},
		},
		{
			name: "manual agent matching definition",
			existing: &agents.Agent{
				ProjectID:         "proj-1",
				Name:              "board-agent",
				Config:            map[string]any{"queue": "fast"},
				AgentDefinitionID: &defID,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeAgentRepo()
			fake.agentsByName["board-agent"] = tt.existing
			svc := &Service{agentRepo: fake, log: testLogger()}

			def := &agents.AgentDefinition{ID: defID, Name: "board-agent", ProjectID: "proj-1"}
			am := AgentManifest{
				Name:           "board-agent",
				TriggerType:    "reaction",
				ReactionConfig: &agents.ReactionConfig{ObjectTypes: []string{"Task"}},
			}

			require.NoError(t, svc.ensureRuntimeAgent(context.Background(), "proj-1", "bp-1", def, &am))
			assert.Len(t, fake.updatedAgents, 0, "must not mutate a non-owned runtime agent")
			assert.Len(t, fake.createdAgents, 0)
		})
	}
}

// TestEnsureRuntimeAgent_OwnBlueprintUpdates verifies a runtime agent owned by
// this blueprint is updated in place (preserving existing config keys).
func TestEnsureRuntimeAgent_OwnBlueprintUpdates(t *testing.T) {
	defID := uuid.NewString()
	fake := newFakeAgentRepo()
	fake.agentsByName["board-agent"] = &agents.Agent{
		ProjectID: "proj-1",
		Name:      "board-agent",
		Config:    map[string]any{"sourceBlueprintId": "bp-1", "queue": "fast"},
	}
	svc := &Service{agentRepo: fake, log: testLogger()}

	def := &agents.AgentDefinition{ID: defID, Name: "board-agent", ProjectID: "proj-1"}
	am := AgentManifest{Name: "board-agent", TriggerType: "reaction", ReactionConfig: &agents.ReactionConfig{ObjectTypes: []string{"Task"}}}

	require.NoError(t, svc.ensureRuntimeAgent(context.Background(), "proj-1", "bp-1", def, &am))
	require.Len(t, fake.updatedAgents, 1, "owned agent must be updated")
	assert.Equal(t, "fast", fake.updatedAgents[0].Config["queue"], "existing config preserved")
	assert.Equal(t, "bp-1", fake.updatedAgents[0].Config["sourceBlueprintId"])
	assert.Equal(t, agents.TriggerTypeReaction, fake.updatedAgents[0].TriggerType)
}
