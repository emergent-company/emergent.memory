package main

import (
	"encoding/json"
	"testing"
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
