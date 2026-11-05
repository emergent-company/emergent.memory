package mcp

import (
	"context"
	"testing"
)

func TestAgentWriteRules(t *testing.T) {
	rules := NewAgentWriteRules(map[string]ObjectWriteRule{
		"Task": {CanCreate: true, CanUpdate: true, CanDelete: false, Fields: []string{"title"}},
	})
	if !rules.CreateAllowed("Task") || !rules.UpdateAllowed("Task") || rules.DeleteAllowed("Task") {
		t.Fatal("action flags not enforced")
	}
	if !rules.CreateAllowed("Document") {
		t.Fatal("unlisted type must be unrestricted")
	}
	if err := rules.CheckFields("Task", map[string]any{"title": "x"}); err != nil {
		t.Fatalf("allowed field rejected: %v", err)
	}
	if err := rules.CheckFields("Task", map[string]any{"description": "x"}); err == nil {
		t.Fatal("disallowed field accepted")
	}
	if err := rules.CheckFields("Document", map[string]any{"anything": "x"}); err != nil {
		t.Fatalf("unlisted type field rejected: %v", err)
	}
}

func TestAgentWriteRulesContext(t *testing.T) {
	r := NewAgentWriteRules(map[string]ObjectWriteRule{"Task": {CanCreate: true}})
	got := AgentWriteRulesFromContext(ContextWithAgentWriteRules(context.Background(), r))
	if got != r {
		t.Fatal("rules did not round-trip through context")
	}
}
