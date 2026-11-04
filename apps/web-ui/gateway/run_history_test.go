package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// sampleRunHistoryFull is an AgentRunFull spanning a full scheduled-run life
// cycle: user turn, assistant work (with a tool call), an ask_user pause, and
// an ask_user question that was answered later (no recorded tool call — the
// answer annotation must come from the agent-question row).
func sampleRunHistoryFull() *AgentRunFull {
	return &AgentRunFull{
		Run: &ScheduledAgentRun{
			ID:        "run-9",
			AgentID:   "a1",
			AgentName: "Daily briefing",
			Status:    "completed",
			StartedAt: "2026-08-26T08:00:00Z",
		},
		Messages: []*AgentRunMessage{
			{Role: "user", Content: map[string]any{"text": "Summarize yesterday"}, CreatedAt: "2026-08-26T08:00:01Z", StepNumber: 0},
			{Role: "coder", Content: map[string]any{"text": "Let me check the logs"}, CreatedAt: "2026-08-26T08:00:02Z", StepNumber: 1},
			{Role: "coder", Content: map[string]any{"text": "Here is the digest"}, CreatedAt: "2026-08-26T08:00:05Z", StepNumber: 4},
			{Role: "tool", Content: map[string]any{"function_responses": []any{map[string]any{"name": "web_search"}}}, CreatedAt: "2026-08-26T08:00:03Z", StepNumber: 2},
		},
		ToolCalls: []*AgentRunToolCall{
			{ID: "tc1", ToolName: "web_search", Status: "completed", Input: map[string]any{"q": "logs"}, Output: map[string]any{"result": "3 hits"}, CreatedAt: "2026-08-26T08:00:02Z", StepNumber: 1},
			{ID: "tc2", ToolName: "ask_user", Status: "running", Input: map[string]any{"question": "Approve the deploy?", "options": []any{map[string]any{"label": "Yes", "value": "yes"}, map[string]any{"label": "No", "value": "no"}}, "interaction_type": "buttons"}, Output: map[string]any{"question_id": "q-1"}, CreatedAt: "2026-08-26T08:00:03Z", StepNumber: 3},
		},
	}
}

func runHistoryQuestions() []AgentQuestionItem {
	answered := "sure"
	return []AgentQuestionItem{
		{ID: "q-1", RunID: "run-9", Question: "Approve the deploy?", Options: []AgentQuestionOption{{Label: "Yes", Value: "yes"}, {Label: "No", Value: "no"}}, Status: "pending"},
		{ID: "q-2", RunID: "run-9", Question: "Which region?", Placeholder: "e.g. us-east-1", Status: "answered", Response: &answered},
	}
}

func TestRunTimelineItems(t *testing.T) {
	items := runTimelineItems(sampleRunHistoryFull(), runHistoryQuestions())

	var msgs []map[string]any
	var ask []map[string]any
	var tools []map[string]any
	for _, raw := range items {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("item is not a JSON object: %v", err)
		}
		switch m["kind"] {
		case "message":
			msgs = append(msgs, m)
		case "tool_call":
			if m["tool_name"] == "ask_user" {
				ask = append(ask, m)
			} else {
				tools = append(tools, m)
			}
		}
	}

	if len(msgs) != 3 {
		t.Fatalf("got %d message items, want 3 (user + 2 agent-authored; tool-role message dropped): %v", len(msgs), msgs)
	}
	// user message shape
	u := msgs[0]
	if u["role"] != "user" {
		t.Errorf("first message role = %v, want user", u["role"])
	}
	uc, _ := u["content"].(map[string]any)
	if uc["text"] != "Summarize yesterday" {
		t.Errorf("user message text = %v", uc["text"])
	}
	// chronological: agent tool-work before the reply. The gateway relays the
	// persisted author role verbatim (the sanitized agent name, not "assistant",
	// see #1832) — it must not drop or normalize it, so a non-"assistant" author
	// must survive here.
	if msgs[1]["role"] != "coder" || msgs[2]["role"] != "coder" {
		t.Errorf("messages 2+3 roles = %v, want coder/coder", []any{msgs[1]["role"], msgs[2]["role"]})
	}
	// no function_calls leaked into the agent message content
	if a2, _ := msgs[1]["content"].(map[string]any); a2["function_calls"] != nil {
		t.Errorf("agent content must not carry function_calls: %v", a2)
	}

	if len(tools) != 1 {
		t.Fatalf("got %d tool chips, want 1 (web_search)", len(tools))
	}
	tool := tools[0]
	if tool["tool_name"] != "web_search" || tool["tool_status"] != "completed" {
		t.Errorf("chip = %v, want web_search/completed", tool)
	}
	if _, ok := tool["created_at"]; !ok {
		t.Error("chip missing created_at (client sortTimeline needs it)")
	}

	// Exactly one ask_user card per question, even though q-1 has a recorded
	// ask_user tool call AND an agent-question row (no duplication).
	if len(ask) != 2 {
		t.Fatalf("got %d ask_user items, want 2 (q-1 via tool call, q-2 synthesized)", len(ask))
	}
	byQ := map[string]map[string]any{}
	for _, a := range ask {
		out, _ := a["tool_output"].(map[string]any)
		byQ[out["question_id"].(string)] = a
	}

	q1, ok := byQ["q-1"]
	if !ok {
		t.Fatal("missing ask_user card for pending question q-1")
	}
	q1in, _ := q1["tool_input"].(map[string]any)
	if q1in["question"] != "Approve the deploy?" {
		t.Errorf("q-1 question = %v", q1in["question"])
	}
	q1out, _ := q1["tool_output"].(map[string]any)
	if q1out["response"] != nil {
		t.Errorf("pending q-1 must not carry a response: %v", q1out)
	}
	if q1["tool_status"] != "completed" {
		t.Errorf("q-1 status = %v, want completed (card replaces the chip)", q1["tool_status"])
	}

	q2, ok := byQ["q-2"]
	if !ok {
		t.Fatal("missing ask_user card for answered question q-2")
	}
	q2in, _ := q2["tool_input"].(map[string]any)
	if q2in["question"] != "Which region?" || q2in["interaction_type"] != "text" {
		t.Errorf("q-2 synthesized input = %v, want question + text interaction", q2in)
	}
	if q2in["placeholder"] != "e.g. us-east-1" {
		t.Errorf("q-2 placeholder = %v", q2in["placeholder"])
	}
	q2out, _ := q2["tool_output"].(map[string]any)
	if q2out["response"] != "sure" {
		t.Errorf("answered q-2 response = %v, want \"sure\"", q2out["response"])
	}
}

func TestRunTimelineItemsOrder(t *testing.T) {
	full := sampleRunHistoryFull()
	// Reverse message/tool order in storage — synthesis must re-sort
	// chronologically.
	full.Messages[0], full.Messages[1] = full.Messages[1], full.Messages[0]
	full.ToolCalls[0], full.ToolCalls[1] = full.ToolCalls[1], full.ToolCalls[0]
	items := runTimelineItems(full, nil)

	var order []string
	for _, raw := range items {
		var m struct {
			Kind    string `json:"kind"`
			Created string `json:"created_at"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		order = append(order, m.Created)
	}
	if len(order) != len(items) {
		t.Fatalf("all items must carry created_at, got %v", order)
	}
	for i := 1; i < len(order); i++ {
		if order[i] < order[i-1] {
			t.Fatalf("items out of order at %d: %v", i, order)
		}
	}
}

func TestRunTimelineEmpty(t *testing.T) {
	if items := runTimelineItems(nil, nil); items != nil {
		t.Errorf("nil run must yield no items, got %v", items)
	}
	items := runTimelineItems(&AgentRunFull{Run: &ScheduledAgentRun{ID: "r", StartedAt: "2026-08-26T08:00:00Z"}}, nil)
	if len(items) != 0 {
		t.Errorf("empty transcript must yield no items, got %d", len(items))
	}
}

// TestQuestionToolInputInteractionType asserts questionToolInput passes an
// explicit InteractionType through (multi_select etc.) while the defaults stay
// buttons-with-options / text-without.
func TestQuestionToolInputInteractionType(t *testing.T) {
	options := []AgentQuestionOption{{Label: "Red", Value: "red"}, {Label: "Blue", Value: "blue"}}
	cases := []struct {
		name    string
		q       AgentQuestionItem
		want    string
		wantOpt bool
	}{
		{"multi_select with options passes through", AgentQuestionItem{Question: "Pick", InteractionType: "multi_select", Options: options}, "multi_select", true},
		{"select with options passes through", AgentQuestionItem{Question: "Pick", InteractionType: "select", Options: options}, "select", true},
		{"options default to buttons", AgentQuestionItem{Question: "Approve?", Options: options}, "buttons", true},
		{"text interaction with placeholder passes through", AgentQuestionItem{Question: "Where?", InteractionType: "text", Placeholder: "e.g. us-east-1"}, "text", false},
		{"no options default to text", AgentQuestionItem{Question: "Where?", Placeholder: "e.g. us-east-1"}, "text", false},
	}
	for _, c := range cases {
		in := questionToolInput(c.q)
		if in["interaction_type"] != c.want {
			t.Errorf("%s: interaction_type = %v, want %q", c.name, in["interaction_type"], c.want)
		}
		if c.wantOpt && in["options"] == nil {
			t.Errorf("%s: options must be present", c.name)
		}
		if in["question"] != c.q.Question {
			t.Errorf("%s: question = %v, want %q", c.name, in["question"], c.q.Question)
		}
	}
}

// TestGetRunHistoryRoute exercises GET /api/runs/:runId/history end to end:
// markdown rendering of assistant messages, ask_user question annotation, and
// pending tool approvals scoped to the run.
func TestGetRunHistoryRoute(t *testing.T) {
	f := &fakeMemory{
		runFull: sampleRunHistoryFull(),
		runQuestions: []AgentQuestionItem{
			{ID: "q-1", RunID: "run-9", Question: "Approve the deploy?", Status: "pending"},
			{ID: "q-2", RunID: "run-9", Question: "Which region?", Status: "answered", Response: strPtr("sure")},
		},
		approvals: []ToolApprovalItem{
			{RunID: "run-9", QuestionID: "appr-1", ToolName: "deploy", ArgsSummary: map[string]any{"env": "prod"}, Decision: "pending"},
			{RunID: "run-other", QuestionID: "appr-2", ToolName: "deploy", ArgsSummary: map[string]any{}, Decision: "pending"}, // different run → excluded
			{RunID: "run-9", QuestionID: "appr-3", ToolName: "cleanup", ArgsSummary: map[string]any{}, Decision: "approved"},   // decided → excluded
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/api/runs/:runId/history", s.getRunHistory)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/run-9/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, want := range []string{
		`"kind":"message"`,
		`"kind":"tool_call"`,
		`"tool_name":"web_search"`,
		`"tool_name":"ask_user"`,
		`"question_id":"q-1"`,
		`"response":"sure"`,
		`"html"`,          // assistant markdown rendered
		`"question_html"`, // pending question markdown rendered
		// The run's own agent heads the transcript: the chat workspace has no
		// other source, and without it the pane attributes the run to whichever
		// chat agent happens to be selected.
		`"agent_name":"Daily briefing"`,
		`"agent_id":"a1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("run history body missing %q", want)
		}
	}

	// pending approvals for this run only
	var out struct {
		Items            []json.RawMessage     `json:"items"`
		PendingApprovals []PendingApprovalItem `json:"pending_approvals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.PendingApprovals) != 1 || out.PendingApprovals[0].QuestionID != "appr-1" {
		t.Errorf("pending approvals = %+v, want only appr-1", out.PendingApprovals)
	}
	if out.PendingApprovals[0].Tool != "deploy" {
		t.Errorf("approval tool = %q", out.PendingApprovals[0].Tool)
	}
}

func TestRunTimelineCarriesStageProvenance(t *testing.T) {
	// A pipeline transcript records the producing stage on the message and keeps
	// the label out of the text. The history must pass that provenance through
	// untouched, and the legacy chain-of-thought heuristic must not steal a
	// first line as "reasoning" — it used to turn a stage's status line into a
	// Thinking block that contained nothing but its own label.
	full := &AgentRunFull{
		Run: &ScheduledAgentRun{ID: "run-x", AgentID: "a2", AgentName: "Extraction", Status: "completed"},
		Messages: []*AgentRunMessage{
			{Role: "assistant", Content: map[string]any{"text": "Processed 55 entities with temp_ids", "stage": "EntityProcessor", "stage_kind": "output"}, CreatedAt: "2026-08-26T08:00:02Z", StepNumber: 2},
			{Role: "user", Content: map[string]any{"text": "Extract of document doc-1", "stage": "Extraction", "stage_kind": "header"}, CreatedAt: "2026-08-26T08:00:01Z", StepNumber: 1},
		},
	}
	hist, run, err := (&Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{runFull: full}}).runTimelineWithRun(context.Background(), "run-x")
	if err != nil {
		t.Fatalf("runTimelineWithRun: %v", err)
	}
	if run == nil || hist.AgentName != "Extraction" || hist.AgentID != "a2" {
		t.Errorf("agent = %q/%q, want the run's own agent (Extraction/a2)", hist.AgentID, hist.AgentName)
	}

	rendered := renderHistoryHTML(hist.Items)
	if len(rendered) != 2 {
		t.Fatalf("items = %d, want 2", len(rendered))
	}
	for i, wantStage := range []string{"Extraction", "EntityProcessor"} {
		var m map[string]any
		if err := json.Unmarshal(rendered[i], &m); err != nil {
			t.Fatalf("decode item %d: %v", i, err)
		}
		content, _ := m["content"].(map[string]any)
		if content["stage"] != wantStage {
			t.Errorf("item %d stage = %v, want %q", i, content["stage"], wantStage)
		}
		if _, ok := content["stage_kind"]; !ok {
			t.Errorf("item %d lost stage_kind", i)
		}
		if reasoning, ok := content["reasoning"]; ok && reasoning != "" {
			t.Errorf("item %d reasoning = %v, want none (a stage label is not chain-of-thought)", i, reasoning)
		}
	}
}

func TestRunTimelineLiftsLegacyStageLabel(t *testing.T) {
	// Transcripts written before stage provenance was structured carry the label
	// inside the text. Those rows are immutable, but the label must still be
	// lifted out: it rendered as literal "**…** · stage output", and the history
	// renderer's first-line heuristic turned it into a Thinking block containing
	// nothing but the label. The run's opening header predates the provenance
	// too, and was recorded as a plain USER message — without lifting it the
	// transcript shows a "You" bubble the operator never typed.
	full := &AgentRunFull{
		Run: &ScheduledAgentRun{ID: "run-l", AgentName: "Extraction", Status: "completed"},
		Messages: []*AgentRunMessage{
			{Role: "user", Content: map[string]any{"text": "Extraction of document doc-1 · job `job-1`"}, CreatedAt: "2026-08-26T08:00:01Z", StepNumber: 0},
			{Role: "assistant", Content: map[string]any{"text": "**EntityProcessor** · stage output\n\nProcessed 55 entities"}, CreatedAt: "2026-08-26T08:00:02Z", StepNumber: 1},
			// A genuine reply that opens with a bold run is not a label.
			{Role: "assistant", Content: map[string]any{"text": "**Bold claim** · a normal reply\n\nbody"}, CreatedAt: "2026-08-26T08:00:03Z", StepNumber: 2},
		},
	}
	rendered := renderHistoryHTML(runTimelineItems(full, nil))
	if len(rendered) != 3 {
		t.Fatalf("items = %d, want 3", len(rendered))
	}

	decode := func(i int) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(rendered[i], &m); err != nil {
			t.Fatalf("decode item %d: %v", i, err)
		}
		content, _ := m["content"].(map[string]any)
		return content
	}

	// The header belongs to the run, not to the operator who never typed it.
	hc := decode(0)
	if hc["stage"] != "Extraction" || hc["stage_kind"] != "header" {
		t.Errorf("legacy header = %v/%v, want Extraction/header", hc["stage"], hc["stage_kind"])
	}
	if hc["text"] != "Extraction of document doc-1 · job `job-1`" {
		t.Errorf("legacy header text = %v, want it unchanged", hc["text"])
	}

	lc := decode(1)
	if lc["stage"] != "EntityProcessor" || lc["stage_kind"] != "output" {
		t.Errorf("legacy label = %v/%v, want EntityProcessor/output", lc["stage"], lc["stage_kind"])
	}
	if lc["text"] != "Processed 55 entities" {
		t.Errorf("legacy text = %v, want the text without the label line", lc["text"])
	}
	if r, ok := lc["reasoning"]; ok && r != "" {
		t.Errorf("legacy reasoning = %v, want none (the label is not chain-of-thought)", r)
	}

	pc := decode(2)
	if _, has := pc["stage"]; has {
		t.Errorf("a genuine bold opener was read as a stage label: %v", pc["stage"])
	}
	// The pre-existing first-line heuristic may still re-split such a reply (that
	// is what lifts the label in the first place); the point here is only that it
	// was not mistaken for recorded stage provenance.
}

func TestGetRunHistoryRouteMemoryError(t *testing.T) {
	f := &fakeMemory{runFullErr: errTest}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/api/runs/:runId/history", s.getRunHistory)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs/run-9/history", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// TestRunTimelineQuestionsFetchFailure ensures a questions failure degrades to
// an unannotated transcript instead of failing the whole fetch (mirrors the
// run page's best-effort questions load).
func TestRunTimelineQuestionsFetchFailure(t *testing.T) {
	f := &fakeMemory{
		runFull:         sampleRunHistoryFull(),
		runQuestionsErr: errTest,
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	hist, err := s.runTimeline(context.Background(), "run-9")
	if err != nil {
		t.Fatalf("runTimeline failed: %v", err)
	}
	found := false
	for _, raw := range hist.Items {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m["kind"] == "tool_call" && m["tool_name"] == "web_search" {
			found = true
		}
	}
	if !found {
		t.Error("transcript items missing when the questions fetch fails")
	}
}

// TestRunTimelineItemsToolMetadataAndSystemPrompt verifies the run transcript
// carries the tool-call id + execution duration and the composed system
// instruction, so both chat renderers can surface them.
func TestRunTimelineItemsToolMetadataAndSystemPrompt(t *testing.T) {
	dur := 1250
	full := &AgentRunFull{
		Run: &ScheduledAgentRun{ID: "run-1", Status: "completed", StartedAt: "2026-08-26T08:00:00Z"},
		Messages: []*AgentRunMessage{
			{Role: "system", Content: map[string]any{"text": "You are a careful agent."}, CreatedAt: "2026-08-26T08:00:00Z", StepNumber: 0},
			{Role: "user", Content: map[string]any{"text": "Do the thing"}, CreatedAt: "2026-08-26T08:00:01Z", StepNumber: 0},
		},
		ToolCalls: []*AgentRunToolCall{
			{ID: "call-abc", ToolName: "web_search", Status: "completed", DurationMs: &dur, Input: map[string]any{"q": "x"}, Output: map[string]any{"n": 1}, CreatedAt: "2026-08-26T08:00:02Z", StepNumber: 1},
		},
	}
	items := runTimelineItems(full, nil)

	var sys map[string]any
	var tool map[string]any
	for _, raw := range items {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode item: %v", err)
		}
		switch {
		case m["kind"] == "message" && m["role"] == "system":
			sys = m
		case m["kind"] == "tool_call" && m["tool_name"] == "web_search":
			tool = m
		}
	}
	if sys == nil {
		t.Fatal("system instruction message missing from run transcript")
	}
	if got, _ := sys["content"].(map[string]any)["text"].(string); got != "You are a careful agent." {
		t.Errorf("system text = %q", got)
	}
	if tool == nil {
		t.Fatal("tool chip missing")
	}
	if tool["id"] != "call-abc" {
		t.Errorf("tool id = %v, want call-abc", tool["id"])
	}
	if tool["duration_ms"] != float64(1250) {
		t.Errorf("tool duration_ms = %v, want 1250", tool["duration_ms"])
	}
}

// TestRunTimelineItemsCitations asserts a run message's citations survive onto
// the synthesized timeline item (from either the message DTO field or its
// content), so the shared client renderer can surface the Sources block.
func TestRunTimelineItemsCitations(t *testing.T) {
	const cites = `[{"kind":"object","id":"11111111-1111-1111-1111-111111111111","type":"Person","label":"Acme","url":"/objects/11111111-1111-1111-1111-111111111111"}]`
	full := &AgentRunFull{
		Run: &ScheduledAgentRun{ID: "run-1", Status: "completed", StartedAt: "2026-08-26T08:00:00Z"},
		Messages: []*AgentRunMessage{
			{Role: "assistant", Content: map[string]any{"text": "See [Acme]."}, Citations: json.RawMessage(cites), CreatedAt: "2026-08-26T08:00:02Z", StepNumber: 1},
			{Role: "assistant", Content: map[string]any{"text": "Also.", "citations": []any{map[string]any{"kind": "object", "id": "22222222-2222-2222-2222-222222222222", "label": "Beta", "type": "Org", "url": "/objects/22222222-2222-2222-2222-222222222222"}}}, CreatedAt: "2026-08-26T08:00:03Z", StepNumber: 2},
		},
	}
	items := runTimelineItems(full, nil)

	var withDTO, withContent map[string]any
	for _, raw := range items {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode item: %v", err)
		}
		if m["kind"] != "message" {
			continue
		}
		switch m["content"].(map[string]any)["text"] {
		case "See [Acme].":
			withDTO = m
		case "Also.":
			withContent = m
		}
	}
	if withDTO == nil || withDTO["citations"] == nil {
		t.Fatalf("DTO citations missing on item: %v", withDTO)
	}
	if withContent == nil || withContent["citations"] == nil {
		t.Fatalf("content citations missing on item: %v", withContent)
	}

	// The citations must also survive renderHistoryHTML (passthrough) so the
	// client receives them on the history render.
	out := renderHistoryHTML(items)
	for _, raw := range out {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode rendered item: %v", err)
		}
		if m["kind"] != "message" {
			continue
		}
		if txt, _ := m["content"].(map[string]any)["text"].(string); txt == "See [Acme]." && m["citations"] == nil {
			t.Error("citations dropped by renderHistoryHTML")
		}
	}
}

// TestRunTimelineCarriesReasoning is the run-history regression test for issue
// #1263. A run message persisted after the server fix carries a clean
// multi-paragraph content.text plus content.reasoning. The synthesized run
// timeline must forward that reasoning so renderHistoryHTML keeps the reply
// intact instead of running the legacy first-line heuristic and splitting the
// answer mid-paragraph.
func TestRunTimelineCarriesReasoning(t *testing.T) {
	const (
		cot    = "first CoT paragraph\n\nsecond CoT paragraph"
		answer = "## Short answer\n\nYes, the answer is 42."
	)
	full := &AgentRunFull{
		Run: &ScheduledAgentRun{ID: "run-r", AgentName: "A", Status: "completed"},
		Messages: []*AgentRunMessage{
			{Role: "assistant", Content: map[string]any{"text": answer, "reasoning": cot}, CreatedAt: "2026-08-26T08:00:02Z", StepNumber: 1},
		},
	}
	items := runTimelineItems(full, nil)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1: %v", len(items), items)
	}
	rendered := renderHistoryHTML(items)
	var m map[string]any
	if err := json.Unmarshal(rendered[0], &m); err != nil {
		t.Fatalf("decode rendered item: %v", err)
	}
	content, _ := m["content"].(map[string]any)
	if content["reasoning"] != cot {
		t.Errorf("reasoning = %v, want %q (heuristic must not overwrite it)", content["reasoning"], cot)
	}
	if content["text"] != answer {
		t.Errorf("text = %v, want %q (reply must not be re-split)", content["text"], answer)
	}
	html, _ := content["html"].(string)
	if !strings.Contains(html, "Short answer") || !strings.Contains(html, "answer is 42") {
		t.Errorf("reply html missing full answer: %s", html)
	}
}
