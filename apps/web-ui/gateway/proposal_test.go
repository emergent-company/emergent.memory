package main

import (
	"encoding/json"
	"slices"
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
	card := buildProposalCard(json.RawMessage(`{"kind":"token","summary":"a token","body":{"name":"x"}}`))
	if card == nil || card.Kind != "token" || card.Summary != "a token" {
		t.Fatalf("unknown kind should degrade to summary-only card, got %+v", card)
	}
	if proposalSummaryLabel(card) != "" {
		t.Errorf("summary-only card should have no diff summary, got %q", proposalSummaryLabel(card))
	}
}

func TestBuildProposalCardSkill(t *testing.T) {
	card := buildProposalCard(json.RawMessage(`{"kind":"skill","summary":"Add a summarize skill","body":{"name":"summarize","description":"summarizes text","prompt":"Summarize the input.","tools":["web-fetch"],"bannedTools":["ask_user"]}}`))
	if card == nil || card.Skill == nil {
		t.Fatalf("skill proposal should parse, got %+v", card)
	}
	if card.Skill.Name != "summarize" || card.Skill.Description == "" || card.Skill.Prompt == "" {
		t.Errorf("skill fields lost: %+v", card.Skill)
	}
	if !slices.Equal(card.Skill.Tools, []string{"web-fetch"}) || !slices.Equal(card.Skill.BannedTools, []string{"ask_user"}) {
		t.Errorf("skill tool lists wrong: %+v", card.Skill)
	}
	if proposalSummaryLabel(card) != "" {
		t.Errorf("skill card should have no diff summary, got %q", proposalSummaryLabel(card))
	}
}

func TestBuildProposalCardAgent(t *testing.T) {
	card := buildProposalCard(json.RawMessage(`{"kind":"agent","summary":"Add an editor agent","body":{"name":"editor","model":"openai/deepseek-v4-flash","systemPrompt":"You edit objects.","tools":["entity-create"],"skills":["editing"],"bannedTools":["ask_user"],"flowType":"agentic","visibility":"project"}}`))
	if card == nil || card.Agent == nil {
		t.Fatalf("agent proposal should parse, got %+v", card)
	}
	a := card.Agent
	if a.Name != "editor" || a.Model != "openai/deepseek-v4-flash" || a.SystemPrompt == "" {
		t.Errorf("agent fields lost: %+v", a)
	}
	if !slices.Equal(a.Tools, []string{"entity-create"}) || !slices.Equal(a.Skills, []string{"editing"}) || !slices.Equal(a.BannedTools, []string{"ask_user"}) {
		t.Errorf("agent lists wrong: %+v", a)
	}
}

func TestBuildProposalCardMCPServer(t *testing.T) {
	card := buildProposalCard(json.RawMessage(`{"kind":"mcp_server","summary":"Add a search server","body":{"name":"exa","type":"http","url":"https://mcp.exa.ai/mcp","enabled":true,"enabledTools":["search"],"disabledTools":["crawl"]}}`))
	if card == nil || card.MCPServer == nil {
		t.Fatalf("mcp_server proposal should parse, got %+v", card)
	}
	m := card.MCPServer
	if m.Name != "exa" || m.Type != "http" || m.URL == "" || !m.Enabled {
		t.Errorf("mcp_server fields lost: %+v", m)
	}
	if !slices.Equal(m.EnabledTools, []string{"search"}) || !slices.Equal(m.DisabledTools, []string{"crawl"}) {
		t.Errorf("mcp_server tool lists wrong: %+v", m)
	}
}

func TestBuildProposalCardMCPServerMasksHeaders(t *testing.T) {
	card := buildProposalCard(json.RawMessage(`{"kind":"mcp_server","summary":"Add exa with auth","body":{"name":"exa","type":"http","url":"https://mcp.exa.ai/mcp","enabled":true,"enabledTools":["search"],"headers":{"Authorization":"Bearer sk-super-secret-token"}}}`))
	if card == nil || card.MCPServer == nil {
		t.Fatalf("mcp_server proposal should parse, got %+v", card)
	}
	m := card.MCPServer
	if m.Name != "exa" || m.Type != "http" || m.URL == "" {
		t.Errorf("mcp_server fields lost: %+v", m)
	}
	if html := renderProposalHTML(json.RawMessage(`{"kind":"mcp_server","summary":"x","body":{"name":"exa","type":"http","url":"https://mcp.exa.ai/mcp","enabled":true,"enabledTools":["search"],"headers":{"Authorization":"Bearer sk-super-secret-token"}}}`)); strings.Contains(html, "sk-super-secret-token") || strings.Contains(html, "Authorization") || strings.Contains(html, "headers") {
		t.Errorf("mcp_server card must not render auth headers:\n%s", html)
	}
}

func TestBuildProposalCardProviderMasksSecret(t *testing.T) {
	card := buildProposalCard(json.RawMessage(`{"kind":"provider","summary":"Add the openai provider","body":{"provider":"openai","baseUrl":"http://litellm:4000/v1","models":["deepseek-v4-flash"],"api_key":"SUPERSECRET"}}`))
	if card == nil || card.Provider == nil {
		t.Fatalf("provider proposal should parse, got %+v", card)
	}
	p := card.Provider
	if p.Provider != "openai" || p.BaseURL != "http://litellm:4000/v1" {
		t.Errorf("provider fields lost: %+v", p)
	}
	if !slices.Equal(p.Models, []string{"deepseek-v4-flash"}) {
		t.Errorf("provider models wrong: %+v", p.Models)
	}
	if html := renderProposalHTML(json.RawMessage(`{"kind":"provider","summary":"x","body":{"provider":"openai","baseUrl":"http://litellm:4000/v1","models":["deepseek-v4-flash"],"api_key":"SUPERSECRET"}}`)); strings.Contains(html, "SUPERSECRET") || strings.Contains(html, "api_key") {
		t.Errorf("provider card must not render the API key:\n%s", html)
	}
}

func TestBuildProposalCardObject(t *testing.T) {
	card := buildProposalCard(json.RawMessage(`{"kind":"object","summary":"Create a person and a task","body":{"entities":[{"type":"person","key":"alice","properties":{"name":"Alice","age":30},"tags":["vip"]},{"type":"task","key":"t1"}],"relationships":[{"type":"assigned_to","source":"t1","target":"alice"}]}}`))
	if card == nil {
		t.Fatalf("object proposal should parse")
	}
	if len(card.Entities) != 2 || card.Entities[0].Type != "person" || card.Entities[0].Key != "alice" {
		t.Errorf("entities wrong: %+v", card.Entities)
	}
	if card.Entities[0].Properties["age"] != "30" {
		t.Errorf("entity properties not stringified: %+v", card.Entities[0].Properties)
	}
	if len(card.Relationships) != 1 || card.Relationships[0].Source != "t1" || card.Relationships[0].Target != "alice" {
		t.Errorf("relationships wrong: %+v", card.Relationships)
	}
}

func TestBuildProposalCardObjectNilProperty(t *testing.T) {
	card := buildProposalCard(json.RawMessage(`{"kind":"object","summary":"Create a person","body":{"entities":[{"type":"person","key":"alice","properties":{"name":"Alice","nickname":null}}]}}`))
	if card == nil || len(card.Entities) != 1 {
		t.Fatalf("object proposal should parse, got %+v", card)
	}
	props := card.Entities[0].Properties
	if props["name"] != "Alice" {
		t.Errorf("string property lost: %+v", props)
	}
	if got, ok := props["nickname"]; !ok || got != "" {
		t.Errorf("nil property should render as empty string, got %q (present=%v)", got, ok)
	}
	if html := renderProposalHTML(json.RawMessage(`{"kind":"object","summary":"Create a person","body":{"entities":[{"type":"person","key":"alice","properties":{"name":"Alice","nickname":null}}]}}`)); strings.Contains(html, "null") {
		t.Errorf("nil property must not render as the literal \"null\":\n%s", html)
	}
}

func TestBuildProposalCardEmptyKindBodies(t *testing.T) {
	for name, raw := range map[string]string{
		"skill-no-name":  `{"kind":"skill","summary":"s","body":{"description":"d"}}`,
		"agent-no-name":  `{"kind":"agent","summary":"s","body":{"model":"m"}}`,
		"mcp-no-name":    `{"kind":"mcp_server","summary":"s","body":{"url":"u"}}`,
		"provider-empty": `{"kind":"provider","summary":"s","body":{}}`,
		"object-empty":   `{"kind":"object","summary":"s","body":{"entities":[],"relationships":[]}}`,
	} {
		if card := buildProposalCard(json.RawMessage(raw)); card != nil {
			t.Errorf("%s: want nil (degrade to markdown), got %+v", name, card)
		}
	}
}

func TestRenderProposalHTMLNewKinds(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want []string
	}{
		"skill": {
			`{"kind":"skill","summary":"Add a summarize skill","body":{"name":"summarize","description":"d","prompt":"Summarize.","tools":["web-fetch"],"bannedTools":["ask_user"]}}`,
			[]string{"proposal-card", "Proposed changes", "skill", `data-proposal-kind="skill"`, "summarize", "Summarize.", "web-fetch", "Banned tools"},
		},
		"agent": {
			`{"kind":"agent","summary":"Add an editor","body":{"name":"editor","model":"openai/deepseek-v4-flash","systemPrompt":"You edit."}}`,
			[]string{"proposal-card", "agent", `data-proposal-kind="agent"`, "editor", "openai/deepseek-v4-flash", "You edit."},
		},
		"mcp_server": {
			`{"kind":"mcp_server","summary":"Add exa","body":{"name":"exa","type":"http","url":"https://mcp.exa.ai/mcp","enabled":true,"enabledTools":["search"]}}`,
			[]string{"proposal-card", "mcp_server", `data-proposal-kind="mcp_server"`, "exa", "http", "https://mcp.exa.ai/mcp", "search"},
		},
		"provider": {
			`{"kind":"provider","summary":"Add openai","body":{"provider":"openai","baseUrl":"http://litellm:4000/v1","models":["deepseek-v4-flash"]}}`,
			[]string{"proposal-card", "provider", `data-proposal-kind="provider"`, "openai", "http://litellm:4000/v1", "deepseek-v4-flash"},
		},
		"object": {
			`{"kind":"object","summary":"Create a person","body":{"entities":[{"type":"person","key":"alice","properties":{"name":"Alice"}}],"relationships":[{"type":"assigned_to","source":"t1","target":"alice"}]}}`,
			[]string{"proposal-card", "object", `data-proposal-kind="object"`, "Entities", "person", "alice", "Relationships", "assigned_to", "t1 → alice"},
		},
	}
	for name, tc := range cases {
		html := renderProposalHTML(json.RawMessage(tc.raw))
		for _, want := range tc.want {
			if !strings.Contains(html, want) {
				t.Errorf("%s: proposal HTML missing %q:\n%s", name, want, html)
			}
		}
	}
}

func TestRenderProposalHTML(t *testing.T) {
	html := renderProposalHTML(json.RawMessage(proposalBlueprintJSON))
	for _, want := range []string{
		"proposal-card", "Proposed changes", "blueprint", `data-proposal-kind="blueprint"`,
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
