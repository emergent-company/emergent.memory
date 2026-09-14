package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- component rendering ---

func TestRenderSessionsPage(t *testing.T) {
	convs := []Conversation{
		{ID: "c1", Title: "Morning briefing", AgentDefinitionID: "agent_a", CreatedAt: "2026-08-26T10:00:00Z"},
		{ID: "c2", AgentDefinitionID: "agent_b", CreatedAt: "2026-08-25T10:00:00Z"}, // untitled → "Chat c2"
	}
	html := renderHTML(t, SessionsPage(convs, nil))
	for _, want := range []string{
		"Sessions", "Morning briefing", "Chat c2",
		`href="/sessions/c1"`, `href="/sessions/c2"`,
		"2 sessions",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("sessions page missing %q", want)
		}
	}

	htmlEmpty := renderHTML(t, SessionsPage(nil, nil))
	if !strings.Contains(htmlEmpty, "No sessions") {
		t.Error("empty state missing")
	}

	htmlErr := renderHTML(t, SessionsPage(nil, errTest))
	if !strings.Contains(htmlErr, "Failed to load sessions") {
		t.Error("error state missing")
	}
}

func TestRenderSessionPage(t *testing.T) {
	items := parseTimeline(sampleHistory().Items)
	html := renderHTML(t, SessionPage(sampleConversation(), groupByRun(items), nil))
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
		`class="chat-header text-xs text-base-content/50">You</div>`,
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

func TestRenderSessionPageEmptyTimeline(t *testing.T) {
	html := renderHTML(t, SessionPage(nil, nil, nil))
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
	html := renderHTML(t, SessionPage(sampleConversation(), groupByRun(items), nil))
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
	_, e := newSessionsEcho(&dumpFake{err: fmt.Errorf("memory 503: service down")})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/missing", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (error state renders, not a crash)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Session unavailable") || !strings.Contains(body, "service down") {
		t.Errorf("error state missing:\n%s", body)
	}
}
