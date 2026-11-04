package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/web-ui/toolresult"
	"github.com/labstack/echo/v4"
)

// --- component rendering ---

func TestRenderSessionsPage(t *testing.T) {
	convs := []Conversation{
		{ID: "c1", Title: "Morning briefing", AgentDefinitionID: "agent_a", CreatedAt: "2026-08-26T10:00:00Z"},
		{ID: "c2", AgentDefinitionID: "agent_b", CreatedAt: "2026-08-25T10:00:00Z"}, // untitled → "Chat c2"
	}
	html := renderHTML(t, SessionsPage(nil, convs, nil))
	for _, want := range []string{
		"Sessions", "Morning briefing", "Chat c2",
		`href="/sessions/c1"`, `href="/sessions/c2"`,
		"2 sessions",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("sessions page missing %q", want)
		}
	}

	htmlEmpty := renderHTML(t, SessionsPage(nil, nil, nil))
	if !strings.Contains(htmlEmpty, "No sessions") {
		t.Error("empty state missing")
	}

	htmlErr := renderHTML(t, SessionsPage(nil, nil, errTest))
	if !strings.Contains(htmlErr, "Failed to load sessions") {
		t.Error("error state missing")
	}
}

func TestRenderSessionPage(t *testing.T) {
	items := parseTimeline(sampleHistory().Items)
	html := renderHTML(t, SessionPage(nil, sampleConversation(), groupByRun(items), nil))
	for _, want := range []string{
		"Capital of France", "What is the capital of France?",
		"entity-create", "Input", "Output",
		"bad_request: key is required for upsert",
		"run 1", "deepseek-v4-flash", "completed",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("session page missing %q", want)
		}
	}
	// Messages render as chat bubbles: right-aligned primary "You" bubble,
	// left-aligned neutral bubble under the assistant's author header.
	for _, want := range []string{
		`class="chat chat-end"`, `class="chat chat-start"`,
		"chat-bubble-primary", "chat-bubble-neutral",
		`class="chat-header text-xs text-muted">You</div>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("session page missing chat markup %q", want)
		}
	}
	// Tool input/output JSON is syntax-highlighted (inline-styled chroma spans
	// over pretty-printed JSON, quotes as HTML entities) instead of a raw
	// escaped text wall.
	if !strings.Contains(html, "style=\"color:") {
		t.Error("tool JSON not syntax-highlighted")
	}
	if !strings.Contains(html, "&#34;name&#34;") {
		t.Error("tool input JSON content missing")
	}
	if !strings.Contains(html, "&#34;failed&#34;") {
		t.Error("tool output JSON content missing")
	}
}

// TestRenderSessionPageAgentPromptAndToolMetadata covers the composed agent
// instruction card (rendered once, at the top, not as a chat bubble) and the
// tool-call id + execution duration on the tool card.
func TestRenderSessionPageAgentPromptAndToolMetadata(t *testing.T) {
	dur := 1200
	items := []TimelineItem{
		{Kind: "message", RunID: "r1", Role: "system", Content: &MessageContent{Text: "You are a careful agent."}},
		{Kind: "message", RunID: "r1", Role: "user", Content: &MessageContent{Text: "hi"}},
		{Kind: "tool_call", RunID: "r1", ID: "call-abc", ToolName: "web_search", ToolStatus: "completed", DurationMs: &dur,
			ToolInput: json.RawMessage(`{"q":"x"}`), ToolOutput: json.RawMessage(`{"n":1}`)},
	}
	html := renderHTML(t, SessionPage(nil, sampleConversation(), groupByRun(items), nil))
	for _, want := range []string{
		"Agent prompt", "You are a careful agent.", "system instruction",
		"1.2s", "#call-abc",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("session page missing %q", want)
		}
	}
	// The system instruction is a card, never an assistant/system bubble.
	if strings.Contains(html, ">System</div>") {
		t.Error("system instruction rendered as a chat bubble")
	}
	// Regression: the instruction card is only rendered once per prompt. Two
	// identical system records still yield a single card.
	twice := []TimelineItem{
		{Kind: "message", RunID: "r1", Role: "system", Content: &MessageContent{Text: "You are a careful agent."}},
		{Kind: "message", RunID: "r2", Role: "system", Content: &MessageContent{Text: "You are a careful agent."}},
	}
	htmlTwice := renderHTML(t, SessionPage(nil, sampleConversation(), groupByRun(twice), nil))
	if n := strings.Count(htmlTwice, "Agent prompt"); n != 1 {
		t.Errorf("agent prompt rendered %d times, want 1", n)
	}
}

func TestRenderSessionPageEmptyTimeline(t *testing.T) {
	html := renderHTML(t, SessionPage(nil, nil, nil, nil))
	if !strings.Contains(html, "No timeline") {
		t.Error("empty timeline state missing")
	}
	if strings.Contains(html, "Session unavailable") {
		t.Error("unknown session must not render the error state")
	}
}

// TestRenderSessionPageUsageAbsent covers the spec's "usage absent" scenario:
// run records carry no token usage on the conversation-history API, so the
// viewer omits usage and still renders the timeline.
func TestRenderSessionPageUsageAbsent(t *testing.T) {
	items := parseTimeline(sampleHistory().Items)
	html := renderHTML(t, SessionPage(nil, sampleConversation(), groupByRun(items), nil))
	for _, want := range []string{"Capital of France", "entity-create", "run 1"} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, unwanted := range []string{"input tokens", "output tokens", "estimated cost", "Token usage"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("usage must be omitted when absent, found %q", unwanted)
		}
	}
}

// --- routes ---

func newSessionsEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/sessions", s.uiSessions)
	e.GET("/sessions/:id", s.uiSession)
	return s, e
}

func TestSessionsListRoute(t *testing.T) {
	f := &fakeMemory{convs: []Conversation{
		{ID: "c_old", Title: "Older", CreatedAt: "2026-08-20T10:00:00Z"},
		{ID: "c_new", Title: "Newer", CreatedAt: "2026-08-26T10:00:00Z"},
	}}
	_, e := newSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Newer") || !strings.Contains(body, "Older") {
		t.Fatalf("sessions missing from list:\n%s", body)
	}
	// most recent first
	if strings.Index(body, "Newer") > strings.Index(body, "Older") {
		t.Error("sessions not sorted most-recent-first")
	}
	if !strings.Contains(body, `href="/sessions/c_new"`) {
		t.Error("session rows must link to their detail view")
	}
}

// TestSessionsListExcludesArchived pins the list-surface default (design D6):
// the Sessions trace page requests the default list, so archived conversations
// are not shown there.
func TestSessionsListExcludesArchived(t *testing.T) {
	f := &fakeMemory{convs: []Conversation{
		{ID: "c_active", Title: "Active session", CreatedAt: "2026-08-26T10:00:00Z"},
		{ID: "c_arch", Title: "Archived session", IsArchived: true, CreatedAt: "2026-08-25T10:00:00Z"},
	}}
	_, e := newSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.convIncludeArchived {
		t.Error("sessions list must not request includeArchived")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Active session") {
		t.Error("active session missing from the list")
	}
	if strings.Contains(body, "Archived session") {
		t.Error("archived session must be excluded from the sessions list")
	}
}

func TestSessionsListRouteError(t *testing.T) {
	f := &fakeMemory{convErr: fmt.Errorf("memory 503: service down")}
	_, e := newSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Failed to load sessions") {
		t.Errorf("error state missing:\n%s", rec.Body.String())
	}
}

func TestSessionDetailRoute(t *testing.T) {
	f := &fakeMemory{
		histories: map[string]*ConversationHistory{
			"c_new": {ConversationID: "c_new", Items: sampleHistory().Items},
		},
	}
	_, e := newSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/c_new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"What is the capital of France?", "entity-create",
		"bad_request: key is required for upsert",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
}

func TestSessionDetailUnknownSession(t *testing.T) {
	_, e := newSessionsEcho(&fakeMemory{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/nope", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (empty timeline, not error)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No timeline") {
		t.Errorf("empty timeline state missing:\n%s", body)
	}
	if strings.Contains(body, "Session unavailable") {
		t.Errorf("unknown session must not render the error state:\n%s", body)
	}
}

func TestSessionDetailMessageOnly(t *testing.T) {
	f := &fakeMemory{
		details: map[string]*ConversationDetail{
			"conv_m": {
				ID:       "conv_m",
				Title:    "Doc pipeline",
				Messages: []Message{{Role: "user", Content: "The user really likes ice cream."}},
			},
		},
	}
	_, e := newSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/conv_m", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "The user really likes ice cream.") {
		t.Errorf("message-only session should render its messages as a timeline:\n%s", body)
	}
	if strings.Contains(body, "No timeline") {
		t.Errorf("message-only session must not show an empty timeline:\n%s", body)
	}
}

func TestSessionDetailNotFoundError(t *testing.T) {
	// Memory reports the conversation missing (404) — the viewer shows an
	// empty timeline, not a crash.
	_, e := newSessionsEcho(&dumpFake{err: fmt.Errorf("memory 404 not_found: conversation not found")})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/missing", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No timeline") {
		t.Errorf("empty timeline missing:\n%s", rec.Body.String())
	}
}

func TestSessionDetailBackendFailure(t *testing.T) {
	_, e := newSessionsEcho(&dumpFake{err: &memoryHTTPError{
		Status:  http.StatusServiceUnavailable,
		Code:    "service_unavailable",
		Message: "Token validation temporarily unavailable",
	}})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/missing", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (error state renders, not a crash)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Session unavailable") || !strings.Contains(body, memoryUnavailableMessage) {
		t.Errorf("error state missing sanitized message:\n%s", body)
	}
	for _, leak := range []string{"dial tcp", "memory 5", "service_unavailable", "Token validation"} {
		if strings.Contains(body, leak) {
			t.Errorf("error state leaked %q", leak)
		}
	}
}

// --- recorded session: the tool step (phase 2) --------------------------------
//
// The recorded-session page and the chat chip must describe the same call the
// same way. Both tests below read the shared step contract
// `toolresult/testdata/envelope-search.step.json`, which the parser test pins, so
// changing a wording rule forces one deliberate update instead of two renderers
// drifting apart (spec task 3.3).

type sessionStepContract struct {
	Tool     string                   `json:"tool"`
	Input    json.RawMessage          `json:"input"`
	Category toolresult.Category      `json:"category"`
	Subject  string                   `json:"subject"`
	View     *toolresult.Presentation `json:"view"`
}

func loadSessionStepContract(t *testing.T) sessionStepContract {
	t.Helper()
	raw, err := os.ReadFile("toolresult/testdata/envelope-search.step.json")
	if err != nil {
		t.Fatalf("read step contract: %v", err)
	}
	var c sessionStepContract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("decode step contract: %v", err)
	}
	if c.Tool == "" || c.View == nil || c.Subject == "" {
		t.Fatalf("incomplete step contract: %+v", c)
	}
	return c
}

// envelopeSearchItem is the recorded tool_call item for the shared contract: the
// real payload fixture as the output, the contract's arguments as its input.
func envelopeSearchItem(t *testing.T) TimelineItem {
	t.Helper()
	c := loadSessionStepContract(t)
	payload, err := os.ReadFile("toolresult/testdata/envelope-search.json")
	if err != nil {
		t.Fatalf("read payload fixture: %v", err)
	}
	return TimelineItem{
		Kind:       "tool_call",
		RunID:      "r1",
		ID:         "call-1",
		ToolName:   c.Tool,
		ToolInput:  c.Input,
		ToolOutput: json.RawMessage(payload),
		ToolStatus: "completed",
	}
}

// TestEnrichTimelineStepsPresentation is task 3.1: the recorded page assembles
// each tool item's presentation through the same toolresult package the chip
// uses.
func TestEnrichTimelineStepsPresentation(t *testing.T) {
	contract := loadSessionStepContract(t)
	items := []TimelineItem{
		envelopeSearchItem(t),
		{Kind: "message", Role: "user", Content: &MessageContent{Text: "find me notes"}},
	}
	enrichTimelineSteps(items)

	step := items[0].ToolStep
	if step == nil {
		t.Fatal("tool_call item has no presentation")
	}
	if want := contract.Category.Verb + " · " + contract.Subject; step.Label() != want {
		t.Errorf("Label() = %q, want %q", step.Label(), want)
	}
	if step.Outcome() != contract.View.Outcome {
		t.Errorf("Outcome() = %q, want %q", step.Outcome(), contract.View.Outcome)
	}
	if !step.Records() {
		t.Error("Records() = false, want the list body")
	}
	// Non-tool items carry no presentation.
	if items[1].ToolStep != nil {
		t.Error("message item has a tool presentation")
	}
}

// TestRenderSessionToolStep is tasks 3.2 and 3.3: the template renders the
// presentation, and the strings it must show come from the same contract file the
// DOM spec reads.
func TestRenderSessionToolStep(t *testing.T) {
	contract := loadSessionStepContract(t)
	items := []TimelineItem{envelopeSearchItem(t)}
	enrichTimelineSteps(items)
	html := renderHTML(t, sessionToolItem(items[0]))

	for _, want := range []string{
		contract.Category.Verb, contract.Subject, contract.View.Outcome,
		contract.View.Items[0].Title, contract.View.Items[0].Detail,
		contract.View.Items[1].Title,
		// the wire name and both raw blocks live behind one disclosure
		"Show raw JSON", contract.Tool, ">Input<", ">Output<",
		// 44 CSS pixels of tappable height for the disclosure control
		"min-h-[44px]",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("tool step missing %q", want)
		}
	}
	// The payload is behind a closed disclosure, not in the body.
	if strings.Contains(html, "<details open") {
		t.Error("raw disclosure renders open; it must stay closed until asked for")
	}
	// Everything the reader sees before the disclosure is the readable body: the
	// payload itself must not leak into it.
	body, _, found := strings.Cut(html, "Show raw JSON")
	if !found {
		t.Fatal("raw disclosure missing")
	}
	if strings.Contains(body, `"score"`) || strings.Contains(body, "has_more") || strings.Contains(body, "0.91") {
		t.Errorf("collapsed body carries the raw payload: %s", body)
	}
}

// TestRenderSessionToolStepPaused: a call paused for approval never ran, so its
// card says so and shows its arguments, not the executor's sentinel as a result.
func TestRenderSessionToolStepPaused(t *testing.T) {
	items := []TimelineItem{{
		Kind:       "tool_call",
		ToolName:   "entity-create",
		ToolInput:  json.RawMessage(`{"title":"Note","type":"note"}`),
		ToolOutput: json.RawMessage(`{"status":"awaiting_confirmation","question_id":"q1","message":"Tool execution paused, awaiting user confirmation."}`),
		ToolStatus: "completed",
	}}
	enrichTimelineSteps(items)
	html := renderHTML(t, sessionToolItem(items[0]))

	for _, want := range []string{"Using memory · Note", "Waiting for confirmation", "has not run yet", "title", ">Input", "badge-warning"} {
		if !strings.Contains(html, want) {
			t.Errorf("paused step missing %q", want)
		}
	}
	// It says it is waiting once, and it does not wear the persisted "completed"
	// badge for a call that never ran.
	if got := strings.Count(html, "Waiting for confirmation"); got != 1 {
		t.Errorf("paused step says it is waiting %d times, want once", got)
	}
	for _, unwanted := range []string{"badge-success", ">completed<", "fields", "Show raw JSON", "Tool execution paused"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("paused step shows %q; it has no result to show", unwanted)
		}
	}
}

// TestRenderSessionToolStepEmptyList: an empty list reads as "No results", with
// the envelope behind the raw disclosure rather than in the open (#2277).
func TestRenderSessionToolStepEmptyList(t *testing.T) {
	payload, err := os.ReadFile("toolresult/testdata/empty-search.json")
	if err != nil {
		t.Fatalf("read payload fixture: %v", err)
	}
	items := []TimelineItem{{
		Kind:       "tool_call",
		ToolName:   "search-hybrid",
		ToolInput:  json.RawMessage(`{"query":"nothing"}`),
		ToolOutput: json.RawMessage(payload),
		ToolStatus: "completed",
	}}
	enrichTimelineSteps(items)
	html := renderHTML(t, sessionToolItem(items[0]))

	for _, want := range []string{"No results", `data-testid="session-tool-empty"`, "Show raw JSON"} {
		if !strings.Contains(html, want) {
			t.Errorf("empty step missing %q", want)
		}
	}
	// The copy is the whole answer, said once.
	if got := strings.Count(html, "No results"); got != 1 {
		t.Errorf("empty step says %q %d times, want once", "No results", got)
	}
	body, _, found := strings.Cut(html, "Show raw JSON")
	if !found {
		t.Fatal("raw disclosure missing")
	}
	if strings.Contains(body, "has_more") {
		t.Errorf("empty state's body carries the raw envelope: %s", body)
	}
}

// TestRenderSessionToolStepFailedBatch: a batch with a failed item leads with its
// counts in the serious tone, so the created records below do not read as a clean
// list (#2277).
func TestRenderSessionToolStepFailedBatch(t *testing.T) {
	payload, err := os.ReadFile("toolresult/testdata/entity-create-partial.json")
	if err != nil {
		t.Fatalf("read payload fixture: %v", err)
	}
	items := []TimelineItem{{
		Kind:       "tool_call",
		ToolName:   "entity-create",
		ToolInput:  json.RawMessage(`{"entities":[]}`),
		ToolOutput: json.RawMessage(payload),
		ToolStatus: "completed",
	}}
	enrichTimelineSteps(items)
	html := renderHTML(t, sessionToolItem(items[0]))

	for _, want := range []string{
		`data-testid="session-tool-batch"`,
		`data-batch-failure="true"`,
		"1 created, 1 failed",
		"lucide--triangle-alert",
		"text-warning",
		"1 of 2 entities failed to create",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("failed batch missing %q", want)
		}
	}
}

// TestRenderSessionToolStepCleanBatch: a batch with no failed item keeps the same
// counts in the quiet tone.
func TestRenderSessionToolStepCleanBatch(t *testing.T) {
	payload, err := os.ReadFile("toolresult/testdata/entity-create.json")
	if err != nil {
		t.Fatalf("read payload fixture: %v", err)
	}
	items := []TimelineItem{{
		Kind:       "tool_call",
		ToolName:   "entity-create",
		ToolInput:  json.RawMessage(`{"entities":[]}`),
		ToolOutput: json.RawMessage(payload),
		ToolStatus: "completed",
	}}
	enrichTimelineSteps(items)
	html := renderHTML(t, sessionToolItem(items[0]))

	if !strings.Contains(html, "1 created, 0 failed") {
		t.Errorf("clean batch missing its counts: %s", html)
	}
	if strings.Contains(html, `data-batch-failure`) {
		t.Error("clean batch is marked as a failure")
	}
}

// TestRenderSessionToolStepNoShape: a payload with no readable shape shows its
// bounded text in the open — there is nothing else to read.
func TestRenderSessionToolStepNoShape(t *testing.T) {
	items := []TimelineItem{{
		Kind:       "tool_call",
		ToolName:   "entity-get",
		ToolInput:  json.RawMessage(`{"id":"e1"}`),
		ToolOutput: json.RawMessage(`not JSON at all — the handler returned plain text`),
		ToolStatus: "completed",
	}}
	enrichTimelineSteps(items)
	html := renderHTML(t, sessionToolItem(items[0]))

	if !strings.Contains(html, "entity-get") {
		t.Error("no-shape step must keep the wire name beside the text")
	}
	if !strings.Contains(html, "<pre") {
		t.Error("no-shape step must show the payload text itself")
	}
	if strings.Contains(html, "Show raw JSON") {
		t.Error("no-shape step shows a second disclosure over the text it already shows")
	}
	// The result is in the open, but the call's arguments are still one tap away —
	// the card never hides what the tool was asked to do.
	if !strings.Contains(html, ">Input") {
		t.Error("no-shape step hides the call's arguments")
	}
}

// TestRenderSessionToolStepFailedStatus: a persisted failure is recorded with
// status "failed"; the reader needs the message, not the status word.
func TestRenderSessionToolStepFailedStatus(t *testing.T) {
	items := []TimelineItem{{
		Kind:       "tool_call",
		ToolName:   "document-get",
		ToolInput:  json.RawMessage(`{"id":"d1"}`),
		ToolOutput: json.RawMessage(`{"ok":false,"error":"connection refused"}`),
		ToolStatus: "failed",
	}}
	enrichTimelineSteps(items)
	html := renderHTML(t, sessionToolItem(items[0]))

	body, _, found := strings.Cut(html, "Show raw JSON")
	if !found {
		t.Fatal("raw disclosure missing")
	}
	if !strings.Contains(body, "connection refused") {
		t.Errorf("failed step does not say why it failed:\n%s", body)
	}
	// The error line carries the message, not the status word (the badge already
	// says "failed").
	i := strings.Index(html, `data-testid="session-tool-failure"`)
	if i < 0 {
		t.Fatal("failed step has no failure line")
	}
	line := html[i:]
	if end := strings.Index(line, "</p>"); end >= 0 {
		line = line[:end]
	}
	if !strings.Contains(line, "connection refused") || strings.Contains(line, ">failed<") {
		t.Errorf("failure line = %q, want the result's message", line)
	}
	if !strings.Contains(html, "badge-error") {
		t.Error("failed step must keep the failing status badge")
	}
}

// TestRenderSessionToolStepFailed: a failed result reads as its message once. The
// outcome, the body and an error line are the same sentence; printing all three
// buries the answer the card exists to give.
func TestRenderSessionToolStepFailed(t *testing.T) {
	payload, err := os.ReadFile("toolresult/testdata/failed-envelope.json")
	if err != nil {
		t.Fatalf("read payload fixture: %v", err)
	}
	items := []TimelineItem{{
		Kind:       "tool_call",
		ToolName:   "entity-get",
		ToolInput:  json.RawMessage(`{"id":"e9"}`),
		ToolOutput: json.RawMessage(payload),
		ToolStatus: "completed", // a failure is recorded as a completed call
	}}
	enrichTimelineSteps(items)
	html := renderHTML(t, sessionToolItem(items[0]))

	body, _, found := strings.Cut(html, "Show raw JSON")
	if !found {
		t.Fatal("raw disclosure missing")
	}
	if got := strings.Count(body, "entity not found: e9"); got != 1 {
		t.Errorf("failure message shown %d times before the disclosure, want once:\n%s", got, body)
	}
	if !strings.Contains(html, "text-error") {
		t.Error("failed step must keep the error line")
	}
}

// TestToolStepRemainderWording pins the cap lines to the chat chip's wording:
// the page and the chip state a hidden remainder the same way.
func TestToolStepRemainderWording(t *testing.T) {
	step := &toolStep{View: &toolresult.Presentation{
		Kind:              toolresult.KindRecords,
		MoreRecords:       2,
		MoreLines:         1,
		RawMoreLines:      3,
		RawMoreCharacters: 120,
	}}
	if got, want := step.BodyRemainder(), "2 more records · 1 more line"; got != want {
		t.Errorf("BodyRemainder() = %q, want %q", got, want)
	}
	if got, want := step.RawRemainder(), "3 more lines · 120 characters cut"; got != want {
		t.Errorf("RawRemainder() = %q, want %q", got, want)
	}
}
