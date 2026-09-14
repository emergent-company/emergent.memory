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
// memory's schemas domain). Skills and seed are intentionally omitted — the
// bundled packs carry neither today.
type blueprintManifest struct {
	Packs  []blueprintPack  `json:"packs,omitempty"`
	Agents []blueprintAgent `json:"agents,omitempty"`
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
// agents pin only a model name; temperature/maxTokens/etc. are unmanaged).
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
}

type blueprintModel struct {
	Name string `json:"name"`
}

// buildBlueprintManifest converts a bundled blueprint pack into the manifest
// JSON consumed by Emergent Memory's /api/blueprints create+apply flow. A
// schema-carrying pack contributes one pack entry (with its raw object and
// relationship type maps so behavioural fields are preserved); agents are
// always included. A pure-agent blueprint (no schema) contributes no pack.
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
			Name:         a.Name,
			Description:  a.Description,
			SystemPrompt: a.SystemPrompt,
			Tools:        a.Tools,
			BannedTools:  a.BannedTools,
			Skills:       a.Skills,
			FlowType:     a.FlowType,
			Visibility:   a.Visibility,
			Config:       a.Config,
		}
		if a.Model != "" {
			am.Model = &blueprintModel{Name: a.Model}
		}
		m.Agents = append(m.Agents, am)
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
