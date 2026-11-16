package main

import (
	"encoding/json"
	"net/url"
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

func TestApplyWorkBoardItemsPerType(t *testing.T) {
	types := []CompiledType{
		{Name: "Bug", BoardEnabled: true, AllowedStatuses: []string{"open", "triaged", "closed"}},
		{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "in_progress", "done"}},
		{Name: "Empty", BoardEnabled: true, AllowedStatuses: []string{"a", "b"}},
	}
	items := []WorkItem{
		{Type: "Task", Status: "ready", CanonicalID: "t1"},
		{Type: "Bug", Status: "open", CanonicalID: "b1"},
		{Type: "Bug", Status: "open", CanonicalID: "b2"},
	}
	data := workData{
		ObjectTypes: types,
		BoardTypes:  boardTypesFromCompiled(&CompiledSchemaTypes{ObjectTypes: types}),
	}
	applyWorkBoardItems(&data, items, nil)
	if len(data.Matrices) != 2 {
		t.Fatalf("matrices = %d, want 2 (empty type hidden)", len(data.Matrices))
	}
	if data.Matrices[0].Type != "Bug" || data.Matrices[1].Type != "Task" {
		t.Fatalf("matrix order = %s, %s", data.Matrices[0].Type, data.Matrices[1].Type)
	}
	if len(data.Matrices[0].Columns) != 3 || data.Matrices[0].Columns[0].Status != "open" {
		t.Fatalf("Bug columns = %#v", data.Matrices[0].Columns)
	}
	if len(data.Matrices[1].Columns) != 3 || data.Matrices[1].Columns[0].Status != "ready" {
		t.Fatalf("Task columns = %#v", data.Matrices[1].Columns)
	}

	// An explicit filter that selects the empty type renders it with its lanes
	// and no items (the empty type is hidden by the default view, not by the
	// projection).
	filtered := workData{
		ObjectTypes: types,
		BoardTypes:  data.BoardTypes,
	}
	applyWorkBoardItems(&filtered, items, url.Values{"filter": {"1"}, "type": {"Empty"}})
	if len(filtered.Matrices) != 1 || filtered.Matrices[0].Type != "Empty" {
		t.Fatalf("explicit filter matrices = %+v, want the Empty type", filtered.Matrices)
	}
	if len(filtered.Matrices[0].Columns) != 2 || filtered.Matrices[0].Total != 0 {
		t.Errorf("Empty matrix = %+v, want 2 empty lanes", filtered.Matrices[0])
	}
}

func TestParseRuleInvocation(t *testing.T) {
	inv, err := parseRuleInvocation(`{"model":"provider/model","credential":{"kind":"github_app","permissions":{"contents":"write"}}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if inv["model"] != "provider/model" {
		t.Fatalf("inv = %#v", inv)
	}
	if _, err := parseRuleInvocation(`[]`); err == nil {
		t.Fatal("array accepted as invocation")
	}
	if inv, err := parseRuleInvocation("  "); err != nil || inv != nil {
		t.Fatalf("empty = %#v, %v", inv, err)
	}
}
