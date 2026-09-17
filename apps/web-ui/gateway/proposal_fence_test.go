package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// fenceManifestJSON is the real-incident shape: a single-pack blueprint manifest
// (two object types, three relationship types) embedded as a ```json fence in
// the question text.
const fenceManifestJSON = `{"packs":[{"name":"personal-inventory","version":"1.0.0","description":"Track owned devices and accessories","objectTypes":[{"name":"Device","label":"Device","description":"An owned device","properties":{"serial":{"type":"string"}}},{"name":"Accessory","label":"Accessory"}],"relationshipTypes":[{"name":"compatible_with","sourceType":"Device","targetType":"Device"},{"name":"works_with","sourceType":"Accessory","targetType":"Device"},{"name":"bundled_with","sourceType":"Accessory","targetType":"Device"}]}]}`

// fenceManifestYAML is the same manifest as a ```yaml fence.
const fenceManifestYAML = `packs:
  - name: personal-inventory
    version: "1.0.0"
    description: Track owned devices and accessories
    objectTypes:
      - name: Device
        properties:
          serial: { type: string }
      - name: Accessory
    relationshipTypes:
      - name: compatible_with
        sourceType: Device
        targetType: Device
      - name: works_with
        sourceType: Accessory
        targetType: Device
      - name: bundled_with
        sourceType: Accessory
        targetType: Device`

func fenceQuestion(lang, body string) string {
	return "I propose the following change:\n```" + lang + "\n" + body + "\n```"
}

func TestProposalFromQuestionTextJSONFence(t *testing.T) {
	card := proposalFromQuestionText(fenceQuestion("json", fenceManifestJSON))
	if card == nil {
		t.Fatal("proposalFromQuestionText returned nil for a fenced manifest")
	}
	if card.Kind != "blueprint" {
		t.Errorf("kind = %q, want blueprint", card.Kind)
	}
	if len(card.ObjectTypes) != 2 || card.ObjectTypes[0].Name != "Device" || card.ObjectTypes[1].Name != "Accessory" {
		t.Errorf("object types wrong: %+v", card.ObjectTypes)
	}
	if len(card.RelationshipTypes) != 3 {
		t.Errorf("relationship types = %d, want 3: %+v", len(card.RelationshipTypes), card.RelationshipTypes)
	}
	if card.AddedObjectTypes != 2 || card.AddedRelationshipTypes != 3 {
		t.Errorf("diff counts wrong: +%d object types, +%d relationship types", card.AddedObjectTypes, card.AddedRelationshipTypes)
	}
	if !strings.Contains(card.Summary, "personal-inventory 1.0.0") || !strings.Contains(card.Summary, "Track owned devices") {
		t.Errorf("summary = %q", card.Summary)
	}
}

func TestProposalFromQuestionTextYAMLFence(t *testing.T) {
	card := proposalFromQuestionText(fenceQuestion("yaml", fenceManifestYAML))
	if card == nil {
		t.Fatal("proposalFromQuestionText returned nil for a fenced YAML manifest")
	}
	if card.Kind != "blueprint" {
		t.Errorf("kind = %q, want blueprint", card.Kind)
	}
	if len(card.ObjectTypes) != 2 || len(card.RelationshipTypes) != 3 {
		t.Errorf("types wrong: %+v / %+v", card.ObjectTypes, card.RelationshipTypes)
	}
	if card.ObjectTypes[0].Name != "Device" || card.ObjectTypes[1].Name != "Accessory" {
		t.Errorf("object type names wrong: %+v", card.ObjectTypes)
	}
}

func TestProposalFromQuestionTextBarePack(t *testing.T) {
	// Bare pack object: top-level objectTypes/relationshipTypes, no `packs` array.
	const bare = `{"name":"personal-inventory","version":"1.0.0","objectTypes":[{"name":"Device"},{"name":"Accessory"}],"relationshipTypes":[{"name":"compatible_with","sourceType":"Device","targetType":"Device"}]}`
	card := proposalFromQuestionText(fenceQuestion("json", bare))
	if card == nil {
		t.Fatal("proposalFromQuestionText returned nil for a bare pack manifest")
	}
	if len(card.ObjectTypes) != 2 || len(card.RelationshipTypes) != 1 {
		t.Errorf("types wrong: %+v / %+v", card.ObjectTypes, card.RelationshipTypes)
	}
	if !strings.Contains(card.Summary, "personal-inventory 1.0.0") {
		t.Errorf("summary = %q", card.Summary)
	}
}

func TestProposalFromQuestionTextMultiPackSummary(t *testing.T) {
	const multi = `{"packs":[{"name":"a","version":"1.0.0","objectTypes":[{"name":"One"}]},{"name":"b","version":"2.0.0","objectTypes":[{"name":"Two"}]}]}`
	card := proposalFromQuestionText(fenceQuestion("json", multi))
	if card == nil {
		t.Fatal("multi-pack manifest should produce a card")
	}
	if len(card.ObjectTypes) != 2 {
		t.Errorf("multi-pack object types = %d, want 2 (merged)", len(card.ObjectTypes))
	}
	if !strings.Contains(card.Summary, "+1 more packs") {
		t.Errorf("multi-pack summary missing note: %q", card.Summary)
	}
}

func TestProposalFromQuestionTextDegrades(t *testing.T) {
	cases := map[string]string{
		"non-manifest-json": fenceQuestion("json", `{"foo":1}`),
		"malformed-json":    fenceQuestion("json", `{not json`),
		"prose-only":        "No manifest here, just prose.",
		"unknown-lang":      fenceQuestion("python", `{"packs":[{"name":"a","objectTypes":[{"name":"One"}]}]}`),
		"bare-fence":        "```\n{\"packs\":[]}\n```",
		"empty-packs":       fenceQuestion("json", `{"packs":[]}`),
		"unterminated":      "```json\n{\"packs\":[]}",
	}
	for name, question := range cases {
		if card := proposalFromQuestionText(question); card != nil {
			t.Errorf("%s: want nil, got %+v", name, card)
		}
	}
}

func TestProposalFromQuestionTextFirstManifestFence(t *testing.T) {
	// A non-manifest fence first, then the real manifest: only the manifest is
	// recognized.
	question := fenceQuestion("python", `print("hi")`) + "\n\n" + fenceQuestion("json", fenceManifestJSON)
	card := proposalFromQuestionText(question)
	if card == nil || card.Kind != "blueprint" {
		t.Fatalf("second fence (json) should be recognized, got %+v", card)
	}
}

func TestRenderProposalCardHTMLFenceExactlyOnce(t *testing.T) {
	card := proposalFromQuestionText(fenceQuestion("json", fenceManifestJSON))
	if card == nil {
		t.Fatal("nil card")
	}
	html := renderProposalCardHTML(card)
	if html == "" {
		t.Fatal("empty HTML")
	}
	for _, want := range []string{"proposal-card", `data-proposal-kind="blueprint"`, "Device", "Accessory", "compatible_with", "bundled_with"} {
		if !strings.Contains(html, want) {
			t.Errorf("card HTML missing %q:\n%s", want, html)
		}
	}
	if n := strings.Count(html, "proposal-card"); n != 1 {
		t.Errorf("card markup rendered %d times, want 1:\n%s", n, html)
	}
}

// askUserStream builds a two-event ask_user SSE stream carrying the question
// (and, when non-nil, a structured proposal) in the started event.
func askUserStream(t *testing.T, question string, proposal json.RawMessage) string {
	t.Helper()
	result := map[string]any{"question": question}
	if proposal != nil {
		var p any
		if err := json.Unmarshal(proposal, &p); err != nil {
			t.Fatalf("proposal not JSON: %v", err)
		}
		result["proposal"] = p
	}
	started, err := json.Marshal(map[string]any{
		"type": "mcp_tool", "tool": "ask_user", "status": "started", "result": result,
	})
	if err != nil {
		t.Fatal(err)
	}
	completed := `{"type":"mcp_tool","tool":"ask_user","status":"completed","result":{"question_id":"q1"}}`
	return "data: " + string(started) + "\n\n" + "data: " + completed + "\n\n"
}

// questionProposalHTML extracts the proposalHtml field of the synthesized
// question event, or "" when none is present.
func questionProposalHTML(t *testing.T, out string) string {
	t.Helper()
	for _, frame := range strings.Split(out, "\n\n") {
		frame = strings.TrimSpace(frame)
		if !strings.HasPrefix(frame, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(frame, "data:"))
		var ev struct {
			Type         string `json:"type"`
			ProposalHTML string `json:"proposalHtml"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		if ev.Type == "question" {
			return ev.ProposalHTML
		}
	}
	return ""
}

func TestRewriteChatStreamAskUserFenceFallback(t *testing.T) {
	question := fenceQuestion("json", fenceManifestJSON)
	out := rewrite(t, askUserStream(t, question, nil))
	html := questionProposalHTML(t, out)
	if html == "" {
		t.Fatalf("proposalHtml not emitted for a fenced manifest: %s", out)
	}
	for _, want := range []string{"Device", "Accessory", "bundled_with"} {
		if !strings.Contains(html, want) {
			t.Errorf("proposalHtml missing %q:\n%s", want, html)
		}
	}
	if n := strings.Count(html, "proposal-card"); n != 1 {
		t.Errorf("card markup rendered %d times, want 1:\n%s", n, html)
	}
}

func TestRewriteChatStreamAskUserFenceFallbackPlainText(t *testing.T) {
	out := rewrite(t, askUserStream(t, "Just a plain question?", nil))
	if html := questionProposalHTML(t, out); html != "" {
		t.Errorf("plain-text question must not emit proposalHtml, got %q", html)
	}
	if !strings.Contains(out, "questionHtml") {
		t.Errorf("markdown questionHtml path should remain: %s", out)
	}
}

func TestRewriteChatStreamAskUserStructuredProposalWins(t *testing.T) {
	question := fenceQuestion("json", fenceManifestJSON)
	structured := json.RawMessage(`{"kind":"skill","summary":"Add a summarize skill","body":{"name":"summarize","description":"d","prompt":"Summarize."}}`)
	out := rewrite(t, askUserStream(t, question, structured))
	html := questionProposalHTML(t, out)
	if !strings.Contains(html, "summarize") || !strings.Contains(html, `data-proposal-kind="skill"`) {
		t.Fatalf("structured proposal should render its own card, got:\n%s", html)
	}
	if strings.Contains(html, "Device") {
		t.Errorf("fence fallback must not override the structured proposal:\n%s", html)
	}
	if n := strings.Count(html, "proposal-card"); n != 1 {
		t.Errorf("card markup rendered %d times, want 1:\n%s", n, html)
	}
}

func TestRenderHistoryHTMLFenceFallback(t *testing.T) {
	question := fenceQuestion("json", fenceManifestJSON)
	items := []json.RawMessage{mustJSON(map[string]any{
		"kind":       "tool_call",
		"tool_name":  "ask_user",
		"tool_input": map[string]any{"question": question},
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
	if !strings.Contains(ph, "Device") || !strings.Contains(ph, "Accessory") {
		t.Errorf("proposal_html missing card content: %q", ph)
	}
	if n := strings.Count(ph, "proposal-card"); n != 1 {
		t.Errorf("card markup rendered %d times, want 1:\n%s", n, ph)
	}
}

func TestRenderHistoryHTMLFenceFallbackPlainText(t *testing.T) {
	items := []json.RawMessage{mustJSON(map[string]any{
		"kind":       "tool_call",
		"tool_name":  "ask_user",
		"tool_input": map[string]any{"question": "Just a question?"},
	})}
	out := renderHistoryHTML(items)
	var m map[string]any
	if err := json.Unmarshal(out[0], &m); err != nil {
		t.Fatal(err)
	}
	in, _ := m["tool_input"].(map[string]any)
	if _, ok := in["proposal_html"]; ok {
		t.Errorf("plain-text question must not gain proposal_html: %v", in)
	}
	if _, ok := in["question_html"]; !ok {
		t.Errorf("question_html missing: %v", in)
	}
}

func TestRenderHistoryHTMLFenceFallbackStructuredWins(t *testing.T) {
	var proposal map[string]any
	if err := json.Unmarshal([]byte(`{"kind":"skill","summary":"Add a summarize skill","body":{"name":"summarize","prompt":"Summarize."}}`), &proposal); err != nil {
		t.Fatal(err)
	}
	items := []json.RawMessage{mustJSON(map[string]any{
		"kind":       "tool_call",
		"tool_name":  "ask_user",
		"tool_input": map[string]any{"question": fenceQuestion("json", fenceManifestJSON), "proposal": proposal},
	})}
	out := renderHistoryHTML(items)
	var m map[string]any
	if err := json.Unmarshal(out[0], &m); err != nil {
		t.Fatal(err)
	}
	in, _ := m["tool_input"].(map[string]any)
	ph, _ := in["proposal_html"].(string)
	if !strings.Contains(ph, "summarize") {
		t.Errorf("structured proposal should render its own card, got %q", ph)
	}
	if strings.Contains(ph, "Device") {
		t.Errorf("fence fallback must not override the structured proposal: %q", ph)
	}
	if n := strings.Count(ph, "proposal-card"); n != 1 {
		t.Errorf("card markup rendered %d times, want 1:\n%s", n, ph)
	}
}
