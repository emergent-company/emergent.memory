package main

import (
	"encoding/json"
	"testing"
)

func TestParseRuleTriggersAndTransitions(t *testing.T) {
	triggers := parseRuleTriggers("ready | claim it\n\nrevision | rework")
	if len(triggers) != 2 {
		t.Fatalf("triggers = %#v", triggers)
	}
	if triggers[0].Status != "ready" || triggers[0].Prompt != "claim it" {
		t.Fatalf("first trigger = %#v", triggers[0])
	}
	if triggers[1].Status != "revision" || triggers[1].Prompt != "rework" {
		t.Fatalf("second trigger = %#v", triggers[1])
	}
	trans := parseRuleTransitions("in_progress -> review\nblocked -> ready\nnonsense")
	if len(trans) != 2 || trans[0].From != "in_progress" || trans[0].To != "review" {
		t.Fatalf("transitions = %#v", trans)
	}
}

func TestValidateGatewayWorkRules(t *testing.T) {
	props, _ := json.Marshal(map[string]any{"title": map[string]any{"type": "string"}})
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "in_progress", "review", "done"}, Properties: props},
	}}
	def := &AgentDefinition{
		Name: "worker",
		WorkConfig: &AgentWorkConfig{
			Status: AgentWorkStatusConfig{Ready: "ready", InProgress: "in_progress", Review: "review", Done: "done"},
			ObjectRules: []AgentObjectRule{{
				ObjectType: "Task",
				Triggers:   []AgentWorkTrigger{{Status: "ready"}},
				Fields:     []string{"title"},
			}},
		},
	}
	if err := validateGatewayWorkRules(def, compiled); err != nil {
		t.Fatalf("valid rules rejected: %v", err)
	}
	def.WorkConfig.ObjectRules[0].Triggers = []AgentWorkTrigger{{Status: "review"}}
	if err := validateGatewayWorkRules(def, compiled); err == nil {
		t.Fatal("review trigger accepted")
	}
	def.WorkConfig.ObjectRules[0].Triggers = []AgentWorkTrigger{{Status: "ready"}}
	def.WorkConfig.ObjectRules[0].Fields = []string{"nope"}
	if err := validateGatewayWorkRules(def, compiled); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestBuildBoardSwimlanesPerType(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Bug", BoardEnabled: true, AllowedStatuses: []string{"open", "triaged", "closed"}},
		{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "in_progress", "done"}},
		{Name: "Empty", BoardEnabled: true, AllowedStatuses: []string{"a", "b"}},
	}}
	items := []WorkItem{
		{Type: "Task", Status: "ready", CanonicalID: "t1"},
		{Type: "Bug", Status: "open", CanonicalID: "b1"},
		{Type: "Bug", Status: "open", CanonicalID: "b2"},
	}
	lanes := buildBoardSwimlanes(items, compiled)
	if len(lanes) != 2 {
		t.Fatalf("swimlanes = %d, want 2 (empty type hidden)", len(lanes))
	}
	if lanes[0].Type != "Bug" || lanes[1].Type != "Task" {
		t.Fatalf("swimlane order = %s, %s", lanes[0].Type, lanes[1].Type)
	}
	if len(lanes[0].Lanes) != 3 || lanes[0].Lanes[0].Status != "open" {
		t.Fatalf("Bug lanes = %#v", lanes[0].Lanes)
	}
	if len(lanes[1].Lanes) != 3 || lanes[1].Lanes[0].Status != "ready" {
		t.Fatalf("Task lanes = %#v", lanes[1].Lanes)
	}
}

func TestFilterWorkItemsForAgent(t *testing.T) {
	def := &AgentDefinition{Name: "worker", WorkConfig: &AgentWorkConfig{ObjectRules: []AgentObjectRule{{ObjectType: "Task"}}}}
	items := []WorkItem{
		{Type: "Task", Assignee: "worker"},
		{Type: "Task", Assignee: ""},
		{Type: "Bug", Assignee: ""},
		{Type: "Task", Assignee: "other"},
	}
	got := filterWorkItemsForAgent(items, def)
	if len(got) != 2 {
		t.Fatalf("filtered items = %d, want 2", len(got))
	}
}
