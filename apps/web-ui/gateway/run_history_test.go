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
			{Role: "assistant", Content: map[string]any{"text": "Let me check the logs"}, CreatedAt: "2026-08-26T08:00:02Z", StepNumber: 1},
			{Role: "assistant", Content: map[string]any{"text": "Here is the digest"}, CreatedAt: "2026-08-26T08:00:05Z", StepNumber: 4},
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
		t.Fatalf("got %d message items, want 3 (user + 2 assistant; tool-role message dropped): %v", len(msgs), msgs)
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
	// chronological: assistant tool-work before the reply
	if msgs[1]["role"] != "assistant" || msgs[2]["role"] != "assistant" {
		t.Errorf("messages 2+3 roles = %v, want assistant/assistant", []any{msgs[1]["role"], msgs[2]["role"]})
	}
	// no function_calls leaked into the assistant message content
	if a2, _ := msgs[1]["content"].(map[string]any); a2["function_calls"] != nil {
		t.Errorf("assistant content must not carry function_calls: %v", a2)
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
