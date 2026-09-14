package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- flattened span fixtures (as memory returns them inline in the run DTO) ---

// sampleRunSpans mirrors the span shape of GET /agent-runs/:runId: a root
// span, a call_llm child with token counts + model, a tool child, and an
// orphan whose parent id is absent from the set.
func sampleRunSpans() []TraceSpan {
	return []TraceSpan{
		{SpanID: "sp_root", ParentSpanID: "", Name: "run_agent", StartUnixNano: 1700000000000000000, EndUnixNano: 1700000001230000000},
		{SpanID: "sp_llm", ParentSpanID: "sp_root", Name: "call_llm", StartUnixNano: 1700000001000000000, EndUnixNano: 1700000001045000000, InputTokens: 123, OutputTokens: 456, Model: "deepseek-v4-flash"},
		{SpanID: "sp_tool", ParentSpanID: "sp_llm", Name: "call_tool", StartUnixNano: 1700000001100000000, EndUnixNano: 1700000001300000000},
		{SpanID: "sp_orphan", ParentSpanID: "sp_nope", Name: "stray_span", StartUnixNano: 1700000001400000000, EndUnixNano: 1700000001490000000},
	}
}

// --- client tests ---

func TestMemoryGetAgentRunDecodesSpans(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"run-1","status":"completed","traceId":"tr_abc","spans":[
			{"spanId":"sp_root","parentSpanId":"","name":"run_agent","startUnixNano":1700000000000000000,"endUnixNano":1700000001230000000},
			{"spanId":"sp_llm","parentSpanId":"sp_root","name":"call_llm","startUnixNano":1700000001000000000,"endUnixNano":1700000001045000000,"inputTokens":123,"outputTokens":456,"model":"deepseek-v4-flash"}
		]},"error":null}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok123", "proj-1")
	run, err := m.GetAgentRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/proj-1/agent-runs/run-1" {
		t.Errorf("path = %q, want /api/projects/proj-1/agent-runs/run-1", gotPath)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("auth = %q", gotAuth)
	}
	if run == nil || run.TraceID != "tr_abc" {
		t.Errorf("run = %+v, want traceId tr_abc", run)
	}
	if len(run.Spans) != 2 {
		t.Fatalf("len(spans) = %d, want 2", len(run.Spans))
	}
	root := run.Spans[0]
	if root.SpanID != "sp_root" || root.ParentSpanID != "" || root.Name != "run_agent" {
		t.Errorf("root = %+v", root)
	}
	if root.StartUnixNano != 1700000000000000000 || root.EndUnixNano != 1700000001230000000 {
		t.Errorf("root times = %d..%d", root.StartUnixNano, root.EndUnixNano)
	}
	llm := run.Spans[1]
	if llm.Name != "call_llm" || llm.ParentSpanID != "sp_root" {
		t.Errorf("llm = %+v", llm)
	}
	if llm.InputTokens != 123 || llm.OutputTokens != 456 {
		t.Errorf("tokens = %d/%d, want 123/456", llm.InputTokens, llm.OutputTokens)
	}
	if llm.Model != "deepseek-v4-flash" {
		t.Errorf("model = %q", llm.Model)
	}
}

func TestMemoryGetAgentRunWithoutSpans(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"run-1","status":"completed"},"error":null}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	run, err := m.GetAgentRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.TraceID != "" {
		t.Errorf("traceId = %q, want empty", run.TraceID)
	}
	if len(run.Spans) != 0 {
		t.Errorf("spans = %+v, want none", run.Spans)
	}
}

// --- traceRows depth/ordering ---

func TestTraceRowsDepthAndOrder(t *testing.T) {
	spans := []TraceSpan{
		{SpanID: "a", ParentSpanID: "", Name: "root", StartUnixNano: 10, EndUnixNano: 100},
		{SpanID: "b", ParentSpanID: "a", Name: "child", StartUnixNano: 20, EndUnixNano: 90},
		{SpanID: "d", ParentSpanID: "b", Name: "grandchild", StartUnixNano: 30, EndUnixNano: 80},
		{SpanID: "c", ParentSpanID: "missing", Name: "orphan", StartUnixNano: 15, EndUnixNano: 95},
	}
	rows := traceRows(spans)
	// Roots in start-time order; each root's subtree follows depth-first, so
	// root(10) → child(20) → grandchild(30), then orphan(15) (its parent is not
	// in the set, so it surfaces as its own root at depth 1).
	wantOrder := []string{"root", "child", "grandchild", "orphan"}
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(rows))
	}
	for i, w := range wantOrder {
		if rows[i].span.Name != w {
			t.Errorf("rows[%d] = %q, want %q (order %v)", i, rows[i].span.Name, w, wantOrder)
		}
	}
	wantDepth := map[string]int{"root": 0, "orphan": 1, "child": 1, "grandchild": 2}
	for _, r := range rows {
		if wantDepth[r.span.Name] != r.depth {
			t.Errorf("%s depth = %d, want %d", r.span.Name, r.depth, wantDepth[r.span.Name])
		}
	}
}

func TestTraceRowsSingleRoot(t *testing.T) {
	rows := traceRows(sampleRunSpans())
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d", len(rows))
	}
	// depth-first: root(0) → call_llm(1) → call_tool(2), then stray orphan(1).
	wantDepth := []int{0, 1, 2, 1}
	for i, w := range wantDepth {
		if rows[i].depth != w {
			t.Errorf("rows[%d].depth = %d, want %d (%s)", i, rows[i].depth, w, rows[i].span.Name)
		}
	}
}

func TestFormatSpanDuration(t *testing.T) {
	cases := []struct {
		start, end int64
		want       string
	}{
		{1700000000000000000, 1700000001230000000, "1.23s"},
		{1700000001000000000, 1700000001045000000, "45ms"},
		{0, 999000000, "999ms"},
		{0, 0, "0ms"},
		{0, -1, "0ms"},         // negative duration degrades
		{0, 60000000000, "1m"}, // >= 60s escalates to minutes
		{0, 75000000000, "1m"}, // 75s → "1m", not "75.00s"
		{0, 9000000000000, "2h 30m"},
		{0, 93600000000000, "1d 2h"},
	}
	for _, c := range cases {
		if got := formatSpanDuration(c.start, c.end); got != c.want {
			t.Errorf("formatSpanDuration(%d,%d) = %q, want %q", c.start, c.end, got, c.want)
		}
	}
}

func TestLLMTokenLabel(t *testing.T) {
	cases := []struct {
		name    string
		in, out int64
		want    string
		wantOK  bool
	}{
		{"both present", 123, 456, "123 → 456 tokens", true},
		{"input only", 10, 0, "10 → – tokens", true},
		{"output only", 0, 7, "– → 7 tokens", true},
		{"neither (absent)", 0, 0, "", false},
	}
	for _, c := range cases {
		got, ok := llmTokenLabel(c.in, c.out)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("%s: llmTokenLabel(%d,%d) = (%q,%v), want (%q,%v)", c.name, c.in, c.out, got, ok, c.want, c.wantOK)
		}
	}
}

// --- handler tests ---

// newSessionHistory returns a conversation history with two runs: run_traced
// (a user turn + assistant reply) and run_untraced (a tool call), so handler
// tests can exercise the trace section per run.
func newSessionHistory() *ConversationHistory {
	return &ConversationHistory{
		ConversationID: "conv_1",
		Items: []json.RawMessage{
			json.RawMessage(`{"kind":"run_start","run_id":"run_traced","step_number":0,"created_at":"2026-08-26T10:00:00Z","run_status":"completed","run_model":"deepseek-v4-flash"}`),
			json.RawMessage(`{"kind":"message","run_id":"run_traced","step_number":1,"created_at":"2026-08-26T10:00:01Z","role":"user","content":{"text":"Trace me?"}}`),
			json.RawMessage(`{"kind":"message","run_id":"run_traced","step_number":2,"created_at":"2026-08-26T10:00:02Z","role":"assistant","content":{"text":"Traced."}}`),
			json.RawMessage(`{"kind":"run_start","run_id":"run_untraced","step_number":3,"created_at":"2026-08-26T10:01:00Z","run_status":"completed"}`),
			json.RawMessage(`{"kind":"tool_call","run_id":"run_untraced","step_number":4,"created_at":"2026-08-26T10:01:01Z","tool_name":"entity-create","tool_input":{"name":"X"},"tool_output":{"success":true},"tool_status":"completed"}`),
		},
	}
}

// TestSessionDetailTraceRendersWaterfall: a run whose DTO carries spans renders
// the waterfall (span name, duration, tokens, indentation); the run without
// spans renders none.
func TestSessionDetailTraceRendersWaterfall(t *testing.T) {
	f := &fakeMemory{
		histories: map[string]*ConversationHistory{"conv_1": newSessionHistory()},
		agentRuns: map[string]*AgentRun{
			"run_traced":   {Spans: sampleRunSpans()},
			"run_untraced": {},
		},
	}
	_, e := newSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/conv_1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Trace", "4 span", "run_agent", "call_llm", "call_tool", "stray_span",
		"1.23s", "45ms", "123 → 456 tokens",
		"padding-left: 0px", "padding-left: 16px", "padding-left: 32px",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("waterfall missing %q", want)
		}
	}
	// Timeline content is untouched.
	if !strings.Contains(body, "Trace me?") {
		t.Errorf("timeline message missing:\n%s", body)
	}
	// run_untraced carries no spans → exactly one trace section, for run 1.
	if got := strings.Count(body, ">Trace<"); got != 1 {
		t.Errorf("trace sections = %d, want exactly 1:\n%s", got, body)
	}
}

// TestSessionDetailRunWithoutSpans: neither run DTO carries spans → no trace
// section anywhere, timeline intact.
func TestSessionDetailRunWithoutSpans(t *testing.T) {
	f := &fakeMemory{
		histories: map[string]*ConversationHistory{"conv_1": newSessionHistory()},
		agentRuns: map[string]*AgentRun{"run_traced": {}, "run_untraced": {}},
	}
	_, e := newSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/conv_1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, ">Trace<") || strings.Contains(body, "run_agent") {
		t.Errorf("trace waterfall rendered for runs without spans:\n%s", body)
	}
	for _, want := range []string{"Trace me?", "run 1", "run 2", "entity-create"} {
		if !strings.Contains(body, want) {
			t.Errorf("timeline missing %q:\n%s", want, body)
		}
	}
}

func TestSessionDetailAgentRunErrorDoesNotBreakPage(t *testing.T) {
	f := &fakeMemory{
		histories:   map[string]*ConversationHistory{"conv_1": newSessionHistory()},
		agentRunErr: errTest,
	}
	_, e := newSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/conv_1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("agent-run fetch error must not break the page, status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Trace me?") {
		t.Errorf("timeline missing after agent-run fetch error:\n%s", rec.Body.String())
	}
}
