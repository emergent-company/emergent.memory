package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// dumpFake reuses fakeMemory (handlers_test.go) but overrides the conversation
// accessors so the dump handler can be exercised against realistic data.
type dumpFake struct {
	fakeMemory
	detail  *ConversationDetail
	history *ConversationHistory
	err     error
}

func (f *dumpFake) GetConversation(ctx context.Context, id string) (*ConversationDetail, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.detail, nil
}

func (f *dumpFake) GetConversationHistory(ctx context.Context, id string) (*ConversationHistory, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.history, nil
}

func sampleConversation() *ConversationDetail {
	return &ConversationDetail{
		ID:                "conv_1",
		Title:             "Capital of France",
		AgentDefinitionID: "agent_diane",
		ProjectID:         "proj_1",
		ACPSessionID:      "acp_1",
		CreatedAt:         "2026-08-26T09:59:00Z",
		UpdatedAt:         "2026-08-26T10:00:05Z",
	}
}

func sampleHistory() *ConversationHistory {
	return &ConversationHistory{
		ACPSessionID:   "acp_1",
		ConversationID: "conv_1",
		Items: []json.RawMessage{
			json.RawMessage(`{"kind":"run_start","run_id":"run_1","step_number":0,"created_at":"2026-08-26T10:00:00Z","run_status":"completed","run_model":"deepseek-v4-flash"}`),
			json.RawMessage(`{"kind":"message","run_id":"run_1","step_number":1,"created_at":"2026-08-26T10:00:01Z","role":"user","content":{"text":"What is the capital of France?"}}`),
			json.RawMessage(`{"kind":"message","run_id":"run_1","step_number":2,"created_at":"2026-08-26T10:00:02Z","role":"diane","content":{"function_calls":[{"id":"call_1","name":"entity-create","args":{}}]}}`),
			json.RawMessage(`{"kind":"tool_call","run_id":"run_1","step_number":3,"created_at":"2026-08-26T10:00:03Z","tool_name":"entity-create","tool_input":{"name":"France"},"tool_output":{"failed":1,"results":[{"error":"bad_request: key is required for upsert","success":false}]},"tool_status":"completed"}`),
			json.RawMessage(`{"kind":"run_end","run_id":"run_1","step_number":4,"created_at":"2026-08-26T10:00:05Z","run_status":"completed","completed_at":"2026-08-26T10:00:05Z"}`),
		},
	}
}

func TestTextDumpFormat(t *testing.T) {
	items := parseTimeline(sampleHistory().Items)
	text := formatTextDump(sampleConversation(), items)
	for _, want := range []string{
		"Session: Capital of France (conv_1)",
		"Agent:   agent_diane",
		"Created: 2026-08-26T09:59:00Z",
		"Updated: 2026-08-26T10:00:05Z",
		"━━ run 1 · model=deepseek-v4-flash · status=completed · 5.0s ━━",
		"[user] What is the capital of France?",
		"[diane] (calls: entity-create)",
		"[tool] entity-create — completed",
		`  input:  {"name":"France"}`,
		`  output: {"failed":1,"results":[{"error":"bad_request: key is required for upsert","success":false}]}`,
		"  error:  bad_request: key is required for upsert",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("dump missing %q\n---\n%s", want, text)
		}
	}
}

func TestJSONDump(t *testing.T) {
	items := parseTimeline(sampleHistory().Items)
	b, err := json.Marshal(dumpJSON{Meta: sampleConversation(), Timeline: items})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Meta     ConversationDetail `json:"meta"`
		Timeline []TimelineItem     `json:"timeline"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Timeline) != 5 {
		t.Fatalf("want 5 timeline items, got %d", len(got.Timeline))
	}
	tool := got.Timeline[3]
	if tool.Kind != "tool_call" || tool.ToolName != "entity-create" {
		t.Fatalf("timeline[3] = %+v", tool)
	}
	if tool.RunID != "run_1" || tool.StepNumber != 3 || tool.ToolStatus != "completed" {
		t.Fatalf("tool item fields wrong: %+v", tool)
	}
	if got.Meta.Title != "Capital of France" || got.Meta.ACPSessionID != "acp_1" {
		t.Fatalf("meta wrong: %+v", got.Meta)
	}
}

func TestToolError(t *testing.T) {
	cases := []struct {
		status, output, want string
	}{
		{"completed", `{"success":true,"results":[{"success":true}]}`, ""},
		{"failed", `{}`, "failed"},
		{"completed", `{"failed":1,"results":[{"error":"bad_request: key is required for upsert","success":false}]}`, "bad_request: key is required for upsert"},
		{"completed", `{"success":false,"message":"nope"}`, "nope"},
		{"completed", `{"error":"boom"}`, "boom"},
		{"completed", `not json`, ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := toolError(c.status, c.output); got != c.want {
			t.Errorf("toolError(%q, %q) = %q, want %q", c.status, c.output, got, c.want)
		}
	}
}

func TestGroupByRunOrder(t *testing.T) {
	items := parseTimeline([]json.RawMessage{
		json.RawMessage(`{"kind":"run_start","run_id":"a"}`),
		json.RawMessage(`{"kind":"run_start","run_id":"b"}`),
		json.RawMessage(`{"kind":"message","run_id":"a","role":"user","content":{"text":"after b"}}`),
	})
	groups := groupByRun(items)
	if len(groups) != 2 || groups[0].runID != "a" || groups[1].runID != "b" {
		t.Fatalf("unexpected groups: %+v", groups)
	}
	if len(groups[0].items) != 2 || groups[0].items[1].Role != "user" {
		t.Fatalf("group a items wrong: %+v", groups[0].items)
	}
}

// newDumpEcho builds a local echo instance registering only the dump route;
// newTestServer in handlers_test.go is left untouched.
func newDumpEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	api := e.Group("/api")
	api.GET("/conversations/:id/dump", s.getConversationDump)
	return s, e
}

func TestGetConversationDumpText(t *testing.T) {
	_, e := newDumpEcho(&dumpFake{detail: sampleConversation(), history: sampleHistory()})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/conv_1/dump", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"Capital of France", "What is the capital of France?", "[tool] entity-create", "error:  bad_request: key is required for upsert"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n---\n%s", want, body)
		}
	}
}

func TestGetConversationDumpJSON(t *testing.T) {
	_, e := newDumpEcho(&dumpFake{detail: sampleConversation(), history: sampleHistory()})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/conv_1/dump?format=json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	var got struct {
		Meta     ConversationDetail `json:"meta"`
		Timeline []TimelineItem     `json:"timeline"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Timeline) != 5 || got.Timeline[3].ToolName != "entity-create" {
		t.Fatalf("unexpected timeline: %+v", got.Timeline)
	}
}

func TestGetConversationDumpFallsBackToMessages(t *testing.T) {
	detail := sampleConversation()
	detail.Messages = []Message{{Role: "user", Content: "The user really likes ice cream."}}
	_, e := newDumpEcho(&dumpFake{detail: detail, history: &ConversationHistory{ConversationID: "conv_1"}})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/conv_1/dump", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "[user] The user really likes ice cream.") {
		t.Errorf("body missing synthesized user message:\n%s", rec.Body.String())
	}
}

func TestGetConversationDumpNotFound(t *testing.T) {
	for _, e := range []string{
		"conversation 404 not found",
		"memory 404 not_found: conversation not found",
		"memory 400 bad_request: invalid conversation id",
	} {
		_, srv := newDumpEcho(&dumpFake{err: fmt.Errorf("%s", e)})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/missing/dump", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("err=%q want 404, got %d", e, rec.Code)
		}
	}
}
