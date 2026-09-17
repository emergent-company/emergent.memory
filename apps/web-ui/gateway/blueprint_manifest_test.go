package main

import (
	"encoding/json"
	"testing"
	"testing/fstest"
)

// TestBuildBlueprintManifest verifies a schema-carrying blueprint with agents
// and migrations maps to the manifest JSON shape, and that behavioural keys
// (labels/embedding) on object types survive the raw-map pass-through.
func TestBuildBlueprintManifest(t *testing.T) {
	bp := &BundledBlueprint{
		Name:        "agent-notes",
		Version:     "2.0.0",
		Description: "annotation layer",
		Author:      "memory",
		HasSchema:   true,
		ObjectTypeSchemas: []map[string]any{{
			"name":        "Note",
			"label":       "Note",
			"description": "an observation",
			"properties": map[string]any{
				"content": map[string]any{"type": "string", "required": true},
			},
			"labels": []string{"note", "{category}"},
			"embedding": map[string]any{
				"mode":  "field",
				"field": "content",
			},
		}},
		RelationshipTypeSchemas: []map[string]any{{
			"name":       "annotates",
			"sourceType": "Note",
			"targetType": "*",
		}},
		Migrations: &SchemaMigrationHints{
			FromVersion: "1.0.0",
			TypeRenames: []TypeRename{{From: "ANNOTATES", To: "annotates"}},
		},
		Agents: []BundledAgent{{
			Name:         "operator",
			SystemPrompt: "assist",
			Model:        "deepseek-v4-pro",
			Tools:        []string{"agent-def-*"},
			BannedTools:  []string{"memory-wipe"},
			FlowType:     "agentic",
			Visibility:   "project",
			Config:       map[string]any{"agentType": "operator"},
		}},
	}

	raw, err := buildBlueprintManifest(bp)
	if err != nil {
		t.Fatalf("buildBlueprintManifest: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	packs, ok := m["packs"].([]any)
	if !ok || len(packs) != 1 {
		t.Fatalf("packs = %#v, want 1 pack", m["packs"])
	}
	pack := packs[0].(map[string]any)
	if pack["name"] != "agent-notes" || pack["version"] != "2.0.0" || pack["author"] != "memory" {
		t.Errorf("pack meta = %#v", pack)
	}
	migrations := pack["migrations"].(map[string]any)
	if migrations["from_version"] != "1.0.0" {
		t.Errorf("migrations.from_version = %#v", migrations["from_version"])
	}
	ot := pack["objectTypes"].([]any)[0].(map[string]any)
	if _, ok := ot["labels"]; !ok {
		t.Error("object type labels dropped")
	}
	if _, ok := ot["embedding"]; !ok {
		t.Error("object type embedding dropped")
	}

	agents, ok := m["agents"].([]any)
	if !ok || len(agents) != 1 {
		t.Fatalf("agents = %#v, want 1", m["agents"])
	}
	ag := agents[0].(map[string]any)
	if ag["name"] != "operator" {
		t.Errorf("agent name = %#v", ag["name"])
	}
	model := ag["model"].(map[string]any)
	if model["name"] != "deepseek-v4-pro" {
		t.Errorf("model.name = %#v", model["name"])
	}
	bt := ag["bannedTools"].([]any)
	if len(bt) != 1 || bt[0] != "memory-wipe" {
		t.Errorf("bannedTools = %#v", bt)
	}
}

// TestBuildBlueprintManifest_AgentOnly verifies a pure-agent blueprint (no
// schema) emits no pack entry and only agents.
func TestBuildBlueprintManifest_AgentOnly(t *testing.T) {
	bp := &BundledBlueprint{
		Name:    "operator",
		Version: "1.0.0",
		Author:  "memory",
		Agents: []BundledAgent{{
			Name:         "operator",
			SystemPrompt: "assist",
		}},
	}
	raw, err := buildBlueprintManifest(bp)
	if err != nil {
		t.Fatalf("buildBlueprintManifest: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["packs"]; ok {
		t.Errorf("agent-only blueprint must not emit packs: %s", raw)
	}
	if _, ok := m["agents"]; !ok {
		t.Errorf("agent-only blueprint must emit agents: %s", raw)
	}
}

// TestBuildBlueprintManifest_AgentUI verifies an agent's inline `ui` block
// (icon + color) survives into the manifest JSON posted to memory, and that a
// nil/empty block is omitted rather than serialised as a meaningless `{}`.
func TestBuildBlueprintManifest_AgentUI(t *testing.T) {
	bp := &BundledBlueprint{
		Name:    "operator",
		Version: "1.0.0",
		Agents: []BundledAgent{
			{Name: "with-ui", UI: &BundledAgentUI{Icon: "database", Color: "#2563EB"}},
			{Name: "empty-ui", UI: &BundledAgentUI{}},
			{Name: "no-ui"},
		},
	}
	raw, err := buildBlueprintManifest(bp)
	if err != nil {
		t.Fatalf("buildBlueprintManifest: %v", err)
	}
	var m struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if len(m.Agents) != 3 {
		t.Fatalf("agents = %d, want 3", len(m.Agents))
	}
	ui, ok := m.Agents[0]["ui"].(map[string]any)
	if !ok {
		t.Fatalf("declared agent ui dropped: %#v", m.Agents[0])
	}
	if ui["icon"] != "database" || ui["color"] != "#2563EB" {
		t.Errorf("agent ui = %#v, want icon=database color=#2563EB", ui)
	}
	if _, ok := m.Agents[1]["ui"]; ok {
		t.Errorf("empty agent ui block must be omitted, got %#v", m.Agents[1]["ui"])
	}
	if _, ok := m.Agents[2]["ui"]; ok {
		t.Errorf("nil agent ui block must be omitted, got %#v", m.Agents[2]["ui"])
	}
}

// TestBundledAgentsFromManifestPreservesUI round-trips an agent's declared
// appearance from manifest JSON back into the detail view's BundledAgent shape.
func TestBundledAgentsFromManifestPreservesUI(t *testing.T) {
	manifest := json.RawMessage(`{"agents":[{"name":"operator","ui":{"icon":"database","color":"#2563EB"}},{"name":"plain"}]}`)
	var m blueprintManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	out := bundledAgentsFromManifest(m.Agents)
	if len(out) != 2 {
		t.Fatalf("agents = %d, want 2", len(out))
	}
	if out[0].UI == nil || out[0].UI.Icon != "database" || out[0].UI.Color != "#2563EB" {
		t.Errorf("ui not preserved: %#v", out[0].UI)
	}
	if out[1].UI != nil {
		t.Errorf("agent without ui must have a nil UI, got %#v", out[1].UI)
	}
}

// TestBundledAgentsFromFSDecodesUI verifies an agents/*.yaml `ui` block decodes
// into BundledAgent so a blueprint's declared appearance is not silently lost
// on load.
func TestBundledAgentsFromFSDecodesUI(t *testing.T) {
	fsys := fstest.MapFS{
		"agents/operator.yaml": &fstest.MapFile{
			Data: []byte("name: operator\nsystemPrompt: assist\nui:\n  icon: database\n  color: \"#2563EB\"\n"),
		},
	}
	out, err := bundledAgentsFromFS(fsys, "agents")
	if err != nil {
		t.Fatalf("bundledAgentsFromFS: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("agents = %d, want 1", len(out))
	}
	if out[0].UI == nil || out[0].UI.Icon != "database" || out[0].UI.Color != "#2563EB" {
		t.Errorf("yaml ui not decoded: %#v", out[0].UI)
	}
}
