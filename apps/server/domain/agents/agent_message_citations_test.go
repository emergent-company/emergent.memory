package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/chat/citations"
)

// TestAnnotateRunMessageCitations locks the run-full contract the gateway's
// run-history path consumes: an agent reply that references a retrieved object
// carries that grounded citation on its DTO, user/tool turns carry none, and a
// hallucinated (unretrieved) id is dropped — the same intersection rule the live
// turn and the conversation-history path apply.
func TestAnnotateRunMessageCitations(t *testing.T) {
	const (
		retrieved    = "11111111-1111-1111-1111-111111111111"
		hallucinated = "33333333-3333-3333-3333-333333333333"
	)
	messages := []*AgentRunMessage{
		{Role: "user", Content: map[string]any{"text": "hi"}},
		{Role: "assistant", Content: map[string]any{
			"text": "See [Acme](/objects/" + retrieved + ") but not [Ghost](/objects/" + hallucinated + ")",
		}},
		{Role: "tool", Content: map[string]any{"text": "raw tool output"}},
	}
	msgDTOs := make([]*AgentRunMessageDTO, len(messages))
	for i, m := range messages {
		msgDTOs[i] = m.ToDTO()
	}
	toolCalls := []*AgentRunToolCall{
		{Output: map[string]any{"data": []map[string]any{
			{"object": map[string]any{"id": retrieved, "type": "Company", "name": "Acme"}},
		}}},
	}

	annotateRunMessageCitations(msgDTOs, messages, toolCalls)

	if len(msgDTOs[1].Citations) != 1 {
		t.Fatalf("assistant citations = %+v, want exactly the retrieved object", msgDTOs[1].Citations)
	}
	if msgDTOs[1].Citations[0].ID != retrieved || msgDTOs[1].Citations[0].Label != "Acme" {
		t.Errorf("assistant citation = %+v, want id %s label Acme", msgDTOs[1].Citations[0], retrieved)
	}
	for _, m := range msgDTOs[1].Citations {
		if m.ID == hallucinated {
			t.Errorf("hallucinated id must not be cited: %+v", msgDTOs[1].Citations)
		}
	}
	if len(msgDTOs[0].Citations) != 0 {
		t.Errorf("user message must carry no citations: %+v", msgDTOs[0].Citations)
	}
	if len(msgDTOs[2].Citations) != 0 {
		t.Errorf("tool message must carry no citations: %+v", msgDTOs[2].Citations)
	}
}

// TestAgentRunMessageDTOWireShape proves the citations field serialises under
// the `citations` key the gateway mirrors and is omitted when empty.
func TestAgentRunMessageDTOWireShape(t *testing.T) {
	const id = "11111111-1111-1111-1111-111111111111"
	dto := &AgentRunMessageDTO{ID: "m1", Role: "assistant", Content: map[string]any{"text": "x"}}
	messages := []*AgentRunMessage{{Role: "assistant", Content: map[string]any{"text": "See [Acme](/objects/" + id + ")"}}}
	annotateRunMessageCitations([]*AgentRunMessageDTO{dto}, messages, []*AgentRunToolCall{
		{Output: map[string]any{"data": []map[string]any{
			{"object": map[string]any{"id": id, "type": "Company", "name": "Acme"}},
		}}},
	})
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"citations":[`) {
		t.Errorf("populated citations missing from wire shape: %s", raw)
	}

	empty := &AgentRunMessageDTO{ID: "m1", Role: "assistant", Content: map[string]any{"text": "x"}}
	raw, err = json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"citations"`) {
		t.Errorf("empty citations must be omitted, got %s", raw)
	}
}

// TestDeriveMessageCitationsTypeLabels proves a transcript citation re-derived
// on read (the reload/history path) carries the schema's display label and
// color, not just the raw type name. The live SSE frame and the replayed
// transcript must agree.
func TestDeriveMessageCitationsTypeLabels(t *testing.T) {
	const id = "11111111-1111-1111-1111-111111111111"
	candidates := citations.Candidates([]citations.ToolCall{{Output: map[string]any{"data": []map[string]any{
		{"object": map[string]any{"id": id, "type": "Company", "name": "Acme"}},
	}}}})
	content := map[string]any{"text": "See [Acme](/objects/" + id + ")"}

	cits := deriveMessageCitations(content, candidates, map[string]citations.TypeMeta{
		"Company": {Label: "Organisation", Color: "#4F46E5"},
	})
	if len(cits) != 1 {
		t.Fatalf("deriveMessageCitations() = %+v, want 1 citation", cits)
	}
	if cits[0].TypeLabel != "Organisation" || cits[0].TypeColor != "#4F46E5" {
		t.Errorf("history citation = %+v, want TypeLabel Organisation TypeColor #4F46E5", cits[0])
	}
}

// TestAnnotateRunMessageCitationsTypeLabels locks the run-full transcript path:
// annotateRunMessageCitations receives the project's type map and the derived
// citation carries it.
func TestAnnotateRunMessageCitationsTypeLabels(t *testing.T) {
	const id = "11111111-1111-1111-1111-111111111111"
	messages := []*AgentRunMessage{
		{Role: "assistant", Content: map[string]any{"text": "See [Acme](/objects/" + id + ")"}},
	}
	msgDTOs := []*AgentRunMessageDTO{messages[0].ToDTO()}
	toolCalls := []*AgentRunToolCall{{Output: map[string]any{"data": []map[string]any{
		{"object": map[string]any{"id": id, "type": "Company", "name": "Acme"}},
	}}}}

	annotateRunMessageCitations(msgDTOs, messages, toolCalls, map[string]citations.TypeMeta{
		"Company": {Label: "Organisation", Color: "#4F46E5"},
	})
	if len(msgDTOs[0].Citations) != 1 {
		t.Fatalf("run transcript citations = %+v, want 1", msgDTOs[0].Citations)
	}
	if got := msgDTOs[0].Citations[0]; got.TypeLabel != "Organisation" || got.TypeColor != "#4F46E5" {
		t.Errorf("run transcript citation = %+v, want TypeLabel Organisation TypeColor #4F46E5", got)
	}
}
