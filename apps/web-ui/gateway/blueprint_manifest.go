package main

import (
	"encoding/json"
	"sort"
)

// blueprintManifest mirrors Emergent Memory's BlueprintManifest JSON shape
// (apps/server/domain/blueprints/manifest.go) as consumed by POST
// /api/blueprints and the import preview. ObjectTypes and RelationshipTypes are
// kept as raw maps so behavioural keys (labels, embedding, extraction, ui, and
// relationship-level properties) survive the round-trip instead of being
// dropped by a typed struct. Migrations reuses SchemaMigrationHints
// (snake_case JSON, matching memory's schemas domain). Skills and seed mirror
// the server's SkillManifest and SeedManifest.
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
// memory's AgentManifest. Model is a single-name struct (agents pin only a
// model name; temperature/maxTokens/etc. are unmanaged). UI is the optional
// inline appearance block (BundledAgentUI already carries the json tags
// AgentUIManifest expects).
type blueprintAgent struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	SystemPrompt string          `json:"systemPrompt,omitempty"`
	Model        *blueprintModel `json:"model,omitempty"`
	Tools        []string        `json:"tools,omitempty"`
	BannedTools  []string        `json:"bannedTools,omitempty"`
	Skills       []string        `json:"skills,omitempty"`
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
