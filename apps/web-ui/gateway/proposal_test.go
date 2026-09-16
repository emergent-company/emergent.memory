package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// proposalBlueprintJSON is a valid blueprint proposal envelope: two object
// types (one with properties) and one relationship type.
const proposalBlueprintJSON = `{
	"kind": "blueprint",
	"summary": "Add person and task types plus an assignment edge",
	"body": {
		"objectTypes": [
			{"name": "person", "label": "Person", "description": "a person", "properties": {"name": {"type": "string", "required": true}}},
			{"name": "task", "label": "Task", "properties": {"title": {"type": "string"}}}
		],
		"relationshipTypes": [
			{"name": "assigned_to", "sourceType": "task", "targetType": "person"}
		]
	}
}`

func TestBuildProposalCardBlueprint(t *testing.T) {
	card := buildProposalCard(json.RawMessage(proposalBlueprintJSON))
	if card == nil {
		t.Fatal("buildProposalCard returned nil for a valid blueprint proposal")
	}
	if card.Kind != "blueprint" || card.Summary == "" {
		t.Errorf("kind/summary lost: %+v", card)
	}
	if len(card.ObjectTypes) != 2 || card.ObjectTypes[0].Name != "person" {
		t.Errorf("object types not parsed: %+v", card.ObjectTypes)
	}
	if len(card.RelationshipTypes) != 1 || card.RelationshipTypes[0].Name != "assigned_to" {
		t.Errorf("relationship types not parsed: %+v", card.RelationshipTypes)
	}
	if card.AddedObjectTypes != 2 || card.AddedRelationshipTypes != 1 {
		t.Errorf("diff counts wrong: +%d object types, +%d relationship types",
			card.AddedObjectTypes, card.AddedRelationshipTypes)
	}
	if got := proposalSummaryLabel(card); got != "Adds 2 object types, 1 relationship type" {
		t.Errorf("summary label = %q", got)
	}
}

func TestBuildProposalCardInvalid(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":           "",
		"not-json":        `{not json`,
		"missing-kind":    `{"summary":"s","body":{}}`,
		"empty-blueprint": `{"kind":"blueprint","summary":"s","body":{"objectTypes":[],"relationshipTypes":[]}}`,
		"bad-body":        `{"kind":"blueprint","summary":"s","body":"nope"}`,
	} {
		if card := buildProposalCard(json.RawMessage(raw)); card != nil {
			t.Errorf("%s: want nil, got %+v", name, card)
		}
	}
}

func TestBuildProposalCardUnknownKind(t *testing.T) {
	card := buildProposalCard(json.RawMessage(`{"kind":"skill","summary":"a skill","body":{"name":"x"}}`))
	if card == nil || card.Kind != "skill" || card.Summary != "a skill" {
		t.Fatalf("unknown kind should degrade to summary-only card, got %+v", card)
	}
	if proposalSummaryLabel(card) != "" {
		t.Errorf("summary-only card should have no diff summary, got %q", proposalSummaryLabel(card))
	}
}

func TestRenderProposalHTML(t *testing.T) {
	html := renderProposalHTML(json.RawMessage(proposalBlueprintJSON))
	for _, want := range []string{
		"proposal-card", "Proposed changes", "blueprint",
		"Adds 2 object types, 1 relationship type",
		"person", "task", "assigned_to", "task → person",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("proposal HTML missing %q:\n%s", want, html)
		}
	}
	if got := renderProposalHTML(json.RawMessage(`{"kind":"blueprint","summary":"s","body":{}}`)); got != "" {
		t.Errorf("empty blueprint body should render nothing, got %q", got)
	}
}

// TestRewriteChatStreamAskUserProposal asserts a proposal-carrying ask_user
// emits proposalHtml in the synthesized question event.
func TestRewriteChatStreamAskUserProposal(t *testing.T) {
	proposal := strings.ReplaceAll(proposalBlueprintJSON, "\n", "")
	proposal = strings.ReplaceAll(proposal, "\t", "")
	stream := strings.Join([]string{
		`data: {"type":"mcp_tool","tool":"ask_user","status":"started","result":{"question":"Add these types?","proposal":` + proposal + `}}`,
		`data: {"type":"mcp_tool","tool":"ask_user","status":"completed","result":{"question_id":"q1"}}`,
	}, "\n\n") + "\n\n"

	out := rewrite(t, stream)
	if !strings.Contains(out, `"proposalHtml"`) {
		t.Fatalf("proposalHtml not emitted: %s", out)
	}
	if !strings.Contains(out, "Proposed changes") || !strings.Contains(out, "assigned_to") {
		t.Errorf("proposal card HTML missing from question event: %s", out)
	}
}

// TestRewriteChatStreamAskUserNoProposal asserts a proposal-less question keeps
// the markdown questionHtml path with no proposalHtml field.
func TestRewriteChatStreamAskUserNoProposal(t *testing.T) {
	stream := `data: {"type":"mcp_tool","tool":"ask_user","status":"running","result":{"question":"Q?"}}` + "\n\n" +
		`data: {"type":"mcp_tool","tool":"ask_user","status":"completed","result":{"question_id":"q2"}}` + "\n\n"
	out := rewrite(t, stream)
	if strings.Contains(out, "proposalHtml") {
		t.Errorf("proposal-less question must not emit proposalHtml: %s", out)
	}
	if !strings.Contains(out, "questionHtml") {
		t.Errorf("markdown questionHtml path should remain: %s", out)
	}
}

// TestRenderHistoryHTMLProposal asserts an ask_user tool_call with a proposal
// gains proposal_html alongside question_html.
func TestRenderHistoryHTMLProposal(t *testing.T) {
	var input map[string]any
	if err := json.Unmarshal([]byte(proposalBlueprintJSON), &input); err != nil {
		t.Fatal(err)
	}
	items := []json.RawMessage{mustJSON(map[string]any{
		"kind":       "tool_call",
		"tool_name":  "ask_user",
		"tool_input": map[string]any{"question": "Add these?", "proposal": input},
	})}
	out := renderHistoryHTML(items)
	var m map[string]any
	if err := json.Unmarshal(out[0], &m); err != nil {
		t.Fatal(err)
	}
	in, _ := m["tool_input"].(map[string]any)
	if _, ok := in["question_html"]; !ok {
		t.Errorf("question_html missing: %v", in)
	}
	ph, _ := in["proposal_html"].(string)
	if !strings.Contains(ph, "Proposed changes") || !strings.Contains(ph, "assigned_to") {
		t.Errorf("proposal_html missing card content: %q", ph)
	}
}
