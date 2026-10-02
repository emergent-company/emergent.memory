package main

import (
	"encoding/json"
	"sort"
)

// blueprintManifest mirrors Emergent Memory's BlueprintManifest JSON shape
// (apps/server/domain/blueprints/manifest.go) as consumed by POST
// /api/blueprints. ObjectTypes and RelationshipTypes are kept as raw maps so
// behavioural keys (labels, embedding, extraction, ui, and relationship-level
// properties) survive the round-trip instead of being dropped by a typed
// struct. Migrations reuses SchemaMigrationHints (snake_case JSON, matching
// memory's schemas domain). Skills and seed mirror the server's SkillManifest
// and SeedManifest so a bundled pack's workflow skill and keyed seed objects
// install out of the box.
type blueprintManifest struct {
	Packs  []blueprintPack  `json:"packs,omitempty"`
	Agents []blueprintAgent `json:"agents,omitempty"`
	Skills []blueprintSkill `json:"skills,omitempty"`
	Seed   *blueprintSeed   `json:"seed,omitempty"`
}

// blueprintPack is the manifest form of a schema pack. It mirrors memory's
// PackManifest but keeps the type definitions as raw maps for lossless pass-
// through of extra keys.
type blueprintPack struct {
	Name              string                `json:"name"`
	Version           string                `json:"version"`
	Description       string                `json:"description,omitempty"`
	Author            string                `json:"author,omitempty"`
	Migrations        *SchemaMigrationHints `json:"migrations,omitempty"`
	ObjectTypes       []map[string]any      `json:"objectTypes,omitempty"`
	RelationshipTypes []map[string]any      `json:"relationshipTypes,omitempty"`
}

// blueprintAgent is the manifest form of an agent definition. It mirrors
// memory's AgentManifest. Model is a single-name struct (Memory's bundled
// agents pin only a model name; temperature/maxTokens/etc. are unmanaged). UI
// is the optional inline appearance block (BundledAgentUI already carries the
// json tags AgentUIManifest expects).
type blueprintAgent struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	SystemPrompt string          `json:"systemPrompt,omitempty"`
	Model        *blueprintModel `json:"model,omitempty"`
	Tools        []string        `json:"tools,omitempty"`
	BannedTools  []string        `json:"bannedTools,omitempty"`
	Skills       []string        `json:"skills,omitempty"`
	FlowType     string          `json:"flowType,omitempty"`
	Visibility   string          `json:"visibility,omitempty"`
	Config       map[string]any  `json:"config,omitempty"`
	UI           *BundledAgentUI `json:"ui,omitempty"`
	// WorkConfig is the object-driven work configuration, matching the server's
	// AgentManifest so a board worker's work contract is not dropped.
	WorkConfig map[string]any `json:"workConfig,omitempty"`
	// TriggerType/ReactionConfig/CronSchedule configure the runtime agent that
	// picks object-driven work up.
	TriggerType    string                 `json:"triggerType,omitempty"`
	ReactionConfig *BundledReactionConfig `json:"reactionConfig,omitempty"`
	CronSchedule   string                 `json:"cronSchedule,omitempty"`
	// DispatchMode/DefaultQueue/MaxSteps are the runtime scheduling knobs a
	// board worker declares.
	DispatchMode string `json:"dispatchMode,omitempty"`
	DefaultQueue string `json:"defaultQueue,omitempty"`
	MaxSteps     *int   `json:"maxSteps,omitempty"`
}

type blueprintModel struct {
	Name string `json:"name"`
}

// blueprintSkill mirrors the server's SkillManifest.
type blueprintSkill struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// blueprintSeed mirrors the server's SeedManifest.
type blueprintSeed struct {
	Objects       []blueprintSeedObject       `json:"objects,omitempty"`
	Relationships []blueprintSeedRelationship `json:"relationships,omitempty"`
}

// blueprintSeedObject mirrors the server's SeedObjectRecord (a keyed object
// with an optional assignee for board-enabled types).
type blueprintSeedObject struct {
	Type       string         `json:"type"`
	Key        string         `json:"key,omitempty"`
	Status     string         `json:"status,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Labels     []string       `json:"labels,omitempty"`
	Assignee   string         `json:"assignee,omitempty"`
}

// blueprintSeedRelationship mirrors the server's SeedRelationshipRecord.
type blueprintSeedRelationship struct {
	Type       string         `json:"type"`
	SrcKey     string         `json:"srcKey,omitempty"`
	DstKey     string         `json:"dstKey,omitempty"`
	SrcType    string         `json:"srcType,omitempty"`
	DstType    string         `json:"dstType,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

// nonEmptyAgentUI drops an appearance block that declares neither icon nor
// color, so the manifest never carries a meaningless `"ui":{}` (nil/empty both
// mean "no appearance").
func nonEmptyAgentUI(ui *BundledAgentUI) *BundledAgentUI {
	if ui == nil || (ui.Icon == "" && ui.Color == "") {
		return nil
	}
	return ui
}

// buildBlueprintManifest converts a bundled blueprint pack into the manifest
// JSON consumed by Emergent Memory's /api/blueprints create+apply flow. A
// schema-carrying pack contributes one pack entry (with its raw object and
// relationship type maps so behavioural fields are preserved); agents are
// always included. A pure-agent blueprint (no schema) contributes no pack.
// Skills and seed are included when present so a bundled pack's workflow skill
// and keyed seed objects install alongside its schema and agents.
func buildBlueprintManifest(bp *BundledBlueprint) (json.RawMessage, error) {
	m := blueprintManifest{}
	if bp.HasSchema {
		m.Packs = []blueprintPack{{
			Name:              bp.Name,
			Version:           bp.Version,
			Description:       bp.Description,
			Author:            bp.Author,
			Migrations:        bp.Migrations,
			ObjectTypes:       bp.ObjectTypeSchemas,
			RelationshipTypes: bp.RelationshipTypeSchemas,
		}}
	}
	for _, a := range bp.Agents {
		am := blueprintAgent{
			Name:           a.Name,
			Description:    a.Description,
			SystemPrompt:   a.SystemPrompt,
			Tools:          a.Tools,
			BannedTools:    a.BannedTools,
			Skills:         a.Skills,
			FlowType:       a.FlowType,
			Visibility:     a.Visibility,
			Config:         a.Config,
			UI:             nonEmptyAgentUI(a.UI),
			WorkConfig:     a.WorkConfig,
			TriggerType:    a.TriggerType,
			ReactionConfig: a.ReactionConfig,
			CronSchedule:   a.CronSchedule,
			DispatchMode:   a.DispatchMode,
			DefaultQueue:   a.DefaultQueue,
			MaxSteps:       a.MaxSteps,
		}
		if a.Model != "" {
			am.Model = &blueprintModel{Name: a.Model}
		}
		m.Agents = append(m.Agents, am)
	}
	if len(bp.Skills) > 0 {
		m.Skills = make([]blueprintSkill, 0, len(bp.Skills))
		for _, s := range bp.Skills {
			m.Skills = append(m.Skills, blueprintSkill(s))
		}
	}
	if len(bp.SeedObjects) > 0 || len(bp.SeedRelationships) > 0 {
		m.Seed = &blueprintSeed{
			Objects:       make([]blueprintSeedObject, 0, len(bp.SeedObjects)),
			Relationships: make([]blueprintSeedRelationship, 0, len(bp.SeedRelationships)),
		}
		for _, o := range bp.SeedObjects {
			m.Seed.Objects = append(m.Seed.Objects, blueprintSeedObject(o))
		}
		for _, r := range bp.SeedRelationships {
			m.Seed.Relationships = append(m.Seed.Relationships, blueprintSeedRelationship(r))
		}
	}
	return json.Marshal(m)
}

// buildRegistryManifest builds a blueprint manifest from a registry schema
// (GetSchema), so a registry pack can be installed through the blueprint API
// (create → publish → apply) instead of the legacy schema-assignment path.
func buildRegistryManifest(sch *BlueprintSchema) (json.RawMessage, error) {
	m := blueprintManifest{
		Packs: []blueprintPack{{
			Name:              sch.Name,
			Version:           sch.Version,
			Description:       sch.Description,
			Author:            sch.Author,
			ObjectTypes:       schemaTypesToMaps(sch.ObjectTypeSchemas),
			RelationshipTypes: schemaTypesToMaps(sch.RelationshipTypeSchemas),
		}},
	}
	return json.Marshal(m)
}

// schemaTypesToMaps parses a raw objectTypeSchemas/relationshipTypeSchemas jsonb
// (array or map form) into a slice of raw maps suitable for a blueprint
// manifest. The map form is normalized so each entry carries its type name.
func schemaTypesToMaps(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		arr = arr[:0]
		for name, v := range m {
			vm, _ := v.(map[string]any)
			if vm == nil {
				vm = map[string]any{}
			}
			vm["name"] = name
			arr = append(arr, vm)
		}
		sort.Slice(arr, func(i, j int) bool { return strAny(arr[i]["name"]) < strAny(arr[j]["name"]) })
		return arr
	}
	return nil
}
