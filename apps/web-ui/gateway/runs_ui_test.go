package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestRunMessageText(t *testing.T) {
	cases := []struct {
		name    string
		content map[string]any
		want    string
	}{
		{"nil content", nil, ""},
		{"empty", map[string]any{}, ""},
		{"plain text", map[string]any{"text": "  hello world  "}, "hello world"},
		{"text with function_calls keeps the text", map[string]any{"text": "searching…", "function_calls": []any{map[string]any{"name": "web_search"}}}, "searching…"},
		{"adk parts with text", map[string]any{"parts": []any{map[string]any{"text": "part one"}, map[string]any{"text": "part two"}}}, "part one\npart two"},
		{"adk parts with functionCall", map[string]any{"parts": []any{map[string]any{"functionCall": map[string]any{"name": "read_file", "args": "{}"}}}}, "called read_file"},
		{"parts with functionResponse", map[string]any{"parts": []any{map[string]any{"functionResponse": map[string]any{"name": "read_file"}}}}, ""},
		{"non-text keys only", map[string]any{"function_responses": []any{map[string]any{"name": "read_file"}}}, ""},
	}
	for _, c := range cases {
		if got := runMessageText(c.content); got != c.want {
			t.Errorf("%s: runMessageText = %q, want %q", c.name, got, c.want)
		}
	}
}

// sampleRunFull builds a realistic AgentRunFull for render tests.
func sampleRunFull() *AgentRunFull {
	completed := time.Now().Add(-25 * time.Minute).Format(time.RFC3339)
	now := time.Now()
	t1 := now.Add(-3 * time.Minute).Format(time.RFC3339)
	t2 := now.Add(-2 * time.Minute).Format(time.RFC3339)
	t3 := now.Add(-time.Minute).Format(time.RFC3339)
	msg := "API key invalid"
	return &AgentRunFull{
		Run: &ScheduledAgentRun{
			ID:           "run-42",
			AgentID:      "a1",
			AgentName:    "Daily briefing",
			Status:       "failed",
			StartedAt:    now.Add(-30 * time.Minute).Format(time.RFC3339),
			CompletedAt:  &completed,
			Summary:      map[string]any{"summary": "digest ready"},
			ErrorMessage: &msg,
		},
		Messages: []*AgentRunMessage{
			{ID: "m1", Role: "user", Content: map[string]any{"text": "Summarize yesterday"}, CreatedAt: t1},
			{ID: "m2", Role: "assistant", Content: map[string]any{"text": "Let me check", "function_calls": []any{map[string]any{"name": "web_search"}}}, CreatedAt: t2},
			{ID: "m3", Role: "tool", Content: map[string]any{"function_responses": []any{map[string]any{"name": "web_search"}}}, CreatedAt: t3},
		},
		ToolCalls: []*AgentRunToolCall{
			{ID: "tc1", ToolName: "web_search", Status: "completed", Output: map[string]any{"result": "3 hits"}, CreatedAt: t3},
		},
	}
}

func TestRenderRunPage(t *testing.T) {
	html := renderHTML(t, RunPage(runPageData{Run: sampleRunFull()}))
	for _, want := range []string{
		"Session", "Daily briefing",
		"failed", // status badge
		"This run failed: API key invalid",
		"digest ready",
		"run-42",
		`href="/schedules/a1"`, // breadcrumb back to the agent's schedule
		`data-run="run-42"`,    // transcript shell handed to chat.js
		`id="chat-messages"`,
		`src="/assets/js/chat-components.js?v=`, `src="/assets/js/chat-stream.js?v=`,
		`src="/assets/js/chat-host.js?v=`, `src="/assets/js/chat.js?v=`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("run page missing %q", want)
		}
	}
	// The transcript body is client-rendered: no server chat bubbles, and no
	// question/respond flow or old debug transcript chrome.
	for _, gone := range []string{
		"chat chat-end", "chat-bubble", "memory-md", ">no text</p>",
		"Continue", "Answered questions", "respond",
		"Transcript", "Summary", "Run unavailable", "tracking-wide uppercase",
	} {
		if strings.Contains(html, gone) {
			t.Errorf("run page must not contain %q", gone)
		}
	}
}

// TestRenderRunPageMetadata asserts the trace-derived header metadata renders
// when present (model, tokens, trigger source, parent run, trace link) and is
// hidden when absent.
func TestRenderRunPageMetadata(t *testing.T) {
	full := sampleRunFull()
	full.Run.TriggerSource = strPtr("manual")
	full.ParentRun = &ScheduledAgentRun{ID: "run-41", AgentName: "Daily briefing"}
	data := runPageData{
		Run: full,
		Trace: runTraceMeta{
			TraceID:      "tr_1",
			Model:        "deepseek-v4",
			InputTokens:  10,
			OutputTokens: 20,
		},
	}
	html := renderHTML(t, RunPage(data))
	for _, want := range []string{
		"deepseek-v4", "10 in · 20 out",
		"manual", // trigger source
		"Resumed from", `href="/runs/run-41"`,
		"View trace", `href="/sessions/run-42"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("run metadata header missing %q", want)
		}
	}

	// Without a trace (and without trigger/parent), those badges vanish.
	htmlBare := renderHTML(t, RunPage(runPageData{Run: sampleRunFull()}))
	for _, gone := range []string{
		"deepseek-v4", " in · ", "manual", "Resumed from", "View trace",
	} {
		if strings.Contains(htmlBare, gone) {
			t.Errorf("bare run page must not contain %q", gone)
		}
	}
}

func TestRenderRunPageError(t *testing.T) {
	html := renderHTML(t, RunPage(runPageData{LoadErr: errTest}))
	if !strings.Contains(html, "Run unavailable") {
		t.Error("load error state missing")
	}
}

// TestUIRunRoute exercises GET /runs/:runId against the fake backend: the run
// page shell renders (metadata header + client-rendered transcript mount), and
// an unknown run renders the whole-page error.
func TestUIRunRoute(t *testing.T) {
	f := &fakeMemory{
		runFull: sampleRunFull(),
		agentRuns: map[string]*AgentRun{
			"run-42": {TraceID: "tr_1", Spans: []TraceSpan{{Model: "deepseek-v4", InputTokens: 10, OutputTokens: 20}}},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/runs/:runId", s.uiRun)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runs/run-42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Session", "Daily briefing",
		`data-run="run-42"`, `id="chat-messages"`,
		"deepseek-v4", "10 in · 20 out", "View trace",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("run route missing %q", want)
		}
	}

	// unknown run → whole-page error state, not a broken render
	f2 := &fakeMemory{runFullErr: errTest}
	s2 := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f2}
	e2 := echo.New()
	e2.GET("/runs/:runId", s2.uiRun)
	rec = httptest.NewRecorder()
	e2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runs/nope", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Run unavailable") {
		t.Fatalf("unknown run: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunTraceMetaFromRun(t *testing.T) {
	cases := []struct {
		name string
		run  *AgentRun
		want runTraceMeta
	}{
		{"nil run", nil, runTraceMeta{}},
		{"empty run", &AgentRun{}, runTraceMeta{}},
		{"trace id only", &AgentRun{TraceID: "tr_1"}, runTraceMeta{TraceID: "tr_1"}},
		{"model from first span naming one", &AgentRun{
			TraceID: "tr_1",
			Spans: []TraceSpan{
				{Name: "root"}, // no model
				{Name: "call_llm", Model: "deepseek-v4", InputTokens: 10, OutputTokens: 20}, // first with a model
				{Name: "call_llm", Model: "gpt-x", InputTokens: 30, OutputTokens: 40},       // later model ignored
			},
		}, runTraceMeta{TraceID: "tr_1", Model: "deepseek-v4", InputTokens: 40, OutputTokens: 60}},
	}
	for _, c := range cases {
		if got := runTraceMetaFromRun(c.run); got != c.want {
			t.Errorf("%s: runTraceMetaFromRun = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestRunDurationSeconds(t *testing.T) {
	start := "2026-08-26T08:00:00Z"
	finished := "2026-08-26T08:05:30Z"
	cases := []struct {
		name string
		run  *ScheduledAgentRun
		want float64
	}{
		{"nil run", nil, 0},
		{"valid pair", &ScheduledAgentRun{StartedAt: start, CompletedAt: &finished}, 330},
		{"missing completed", &ScheduledAgentRun{StartedAt: start}, 0},
		{"empty completed", &ScheduledAgentRun{StartedAt: start, CompletedAt: strPtr("")}, 0},
		{"missing started", &ScheduledAgentRun{CompletedAt: &finished}, 0},
		{"unparseable started", &ScheduledAgentRun{StartedAt: "yesterday", CompletedAt: &finished}, 0},
		{"unparseable completed", &ScheduledAgentRun{StartedAt: start, CompletedAt: strPtr("tomorrow")}, 0},
	}
	for _, c := range cases {
		if got := runDurationSeconds(c.run); got != c.want {
			t.Errorf("%s: runDurationSeconds = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestRenderScheduleDetailRunsSection asserts the schedule detail page lists
// recent runs linking to /runs/{id}, with an empty state before any run.
func TestRenderScheduleDetailRunsSection(t *testing.T) {
	data := scheduleDetailData{
		Agent: &ScheduledAgent{ID: "a1", Name: "Daily briefing", CronSchedule: "0 0 8 * * *", Enabled: true, UpdatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)},
		Runs: []ScheduledAgentRun{
			{ID: "run-1", AgentID: "a1", Status: "completed", StartedAt: time.Now().Add(-30 * time.Minute).Format(time.RFC3339), CompletedAt: strPtr(time.Now().Add(-28 * time.Minute).Format(time.RFC3339)), Summary: map[string]any{"summary": "digest"}},
			{ID: "run-2", AgentID: "a1", Status: "failed", StartedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339), ErrorMessage: strPtr("boom")},
		},
	}
	html := renderHTML(t, ScheduleDetailPage(data))
	for _, want := range []string{
		"Latest runs",
		`href="/runs/run-1"`, `href="/runs/run-2"`,
		"completed", "failed",
		"digest",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("runs section missing %q", want)
		}
	}

	// empty state
	htmlEmpty := renderHTML(t, ScheduleDetailPage(scheduleDetailData{Agent: data.Agent}))
	if !strings.Contains(htmlEmpty, "No runs yet") {
		t.Error("empty runs state missing")
	}
	if strings.Contains(htmlEmpty, `href="/runs/`) {
		t.Error("empty runs state must not render run rows")
	}
}
