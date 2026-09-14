package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newClientEcho registers the client-facing API routes against a fake backend.
func newClientEcho(f MemoryBackend, cfg Config) (*Server, *echo.Echo) {
	s := &Server{cfg: cfg, memory: f}
	e := echo.New()
	api := e.Group("/api")
	api.GET("/sessions", s.listSessions)
	api.GET("/session", s.getSession)
	api.GET("/memories/capability", s.memoryCapability)
	api.GET("/memories", s.listMemories)
	return s, e
}

// --- session log ---

func TestListSessions(t *testing.T) {
	f := &fakeMemory{
		convs: []Conversation{
			{ID: "conv_old", CreatedAt: "2026-08-26T09:00:00Z", UpdatedAt: "2026-08-26T09:30:00Z"},
			{ID: "conv_new", CreatedAt: "2026-08-26T10:00:00Z", UpdatedAt: "2026-08-26T10:05:00Z"},
		},
		histories: map[string]*ConversationHistory{
			// 2 messages + 1 tool call (run_start/run_end skipped)
			"conv_new": sampleHistory(),
			// no recorded activity
			"conv_old": {ConversationID: "conv_old", Items: []json.RawMessage{}},
		},
	}

	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Sessions []struct {
			Room      string  `json:"room"`
			StartedAt *string `json:"started_at"`
			EndedAt   *string `json:"ended_at"`
			Turns     int     `json:"turns"`
			ToolCalls int     `json:"tool_calls"`
			Preview   *string `json:"preview"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(got.Sessions))
	}
	// most-recent-first
	if got.Sessions[0].Room != "conv_new" || got.Sessions[1].Room != "conv_old" {
		t.Fatalf("order = %s, %s", got.Sessions[0].Room, got.Sessions[1].Room)
	}
	n := got.Sessions[0]
	if n.Turns != 2 || n.ToolCalls != 1 {
		t.Errorf("conv_new turns/tool_calls = %d/%d, want 2/1", n.Turns, n.ToolCalls)
	}
	if n.StartedAt == nil || *n.StartedAt != "2026-08-26T10:00:00Z" {
		t.Errorf("conv_new started_at = %v", n.StartedAt)
	}
	if n.EndedAt == nil || *n.EndedAt != "2026-08-26T10:05:00Z" {
		t.Errorf("conv_new ended_at = %v", n.EndedAt)
	}
	if n.Preview == nil || *n.Preview != "What is the capital of France?" {
		t.Errorf("conv_new preview = %v", n.Preview)
	}
	o := got.Sessions[1]
	if o.Turns != 0 || o.ToolCalls != 0 || o.Preview != nil {
		t.Errorf("conv_old zeros = %d/%d/%v", o.Turns, o.ToolCalls, o.Preview)
	}
}

func TestListSessionsHistoryFailureStillListed(t *testing.T) {
	f := &fakeMemory{
		convs:   []Conversation{{ID: "conv_x", CreatedAt: "2026-08-26T09:00:00Z", UpdatedAt: "2026-08-26T09:00:01Z"}},
		histErr: fmt.Errorf("memory 503: boom"),
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	var got struct {
		Sessions []struct {
			Room      string  `json:"room"`
			Turns     int     `json:"turns"`
			ToolCalls int     `json:"tool_calls"`
			Preview   *string `json:"preview"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].Turns != 0 || got.Sessions[0].ToolCalls != 0 || got.Sessions[0].Preview != nil {
		t.Fatalf("want one zeroed session, got %+v", got.Sessions)
	}
}

func TestListSessionsPreviewTruncated(t *testing.T) {
	long := strings.Repeat("é", 200) // 200 runes
	f := &fakeMemory{
		convs: []Conversation{{ID: "c1", CreatedAt: "2026-08-26T09:00:00Z", UpdatedAt: "2026-08-26T09:00:01Z"}},
		histories: map[string]*ConversationHistory{
			"c1": {Items: []json.RawMessage{
				json.RawMessage(`{"kind":"message","role":"user","content":{"text":"` + long + `"}}`),
			}},
		},
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	var got struct {
		Sessions []struct {
			Preview *string `json:"preview"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	p := got.Sessions[0].Preview
	if p == nil {
		t.Fatal("preview = nil")
	}
	if len([]rune(*p)) != 161 || !strings.HasSuffix(*p, "…") {
		t.Errorf("preview = %q (len %d), want 160 runes + …", *p, len([]rune(*p)))
	}
	if !strings.HasPrefix(*p, long[:160]) {
		t.Errorf("preview prefix mismatch: %q", *p)
	}
}

// TestListSessionsOrigin verifies the origin field on sessions and the
// ?origin= filter: conversations are "manual", scheduled-agent runs are
// "scheduled", filtering isolates one origin, and an unknown origin yields an
// empty list rather than an error.
func TestListSessionsOrigin(t *testing.T) {
	runStarted := "2026-08-27T08:00:00Z"
	runCompleted := "2026-08-27T08:02:00Z"
	f := &fakeMemory{
		convs: []Conversation{{ID: "conv_1", CreatedAt: "2026-08-26T09:00:00Z", UpdatedAt: "2026-08-26T09:30:00Z"}},
		histories: map[string]*ConversationHistory{
			"conv_1": {Items: []json.RawMessage{
				json.RawMessage(`{"kind":"message","role":"user","content":{"text":"hello"}}`),
			}},
		},
		scheduledAgents: []ScheduledAgent{
			{ID: "sa1", Name: "Daily briefing", TriggerType: "schedule"},
			{ID: "sa2", Name: "Manual only", TriggerType: "manual"}, // must not merge runs
		},
		scheduledRuns: map[string][]ScheduledAgentRun{
			"sa1": {{ID: "run_1", AgentID: "sa1", Status: "completed", StartedAt: runStarted, CompletedAt: &runCompleted, Summary: map[string]any{"summary": "digest ready"}}},
			"sa2": {{ID: "run_2", AgentID: "sa2", Status: "completed", StartedAt: runStarted}},
		},
	}

	decode := func(rec *httptest.ResponseRecorder) []struct {
		Room      string  `json:"room"`
		Origin    string  `json:"origin"`
		StartedAt *string `json:"started_at"`
		Preview   *string `json:"preview"`
	} {
		t.Helper()
		var got struct {
			Sessions []struct {
				Room      string  `json:"room"`
				Origin    string  `json:"origin"`
				StartedAt *string `json:"started_at"`
				Preview   *string `json:"preview"`
			} `json:"sessions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
		}
		return got.Sessions
	}

	_, e := newClientEcho(f, Config{})

	// all sessions: manual conversation + scheduled run (manual-trigger agent
	// must NOT contribute its run).
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	all := decode(rec)
	if len(all) != 2 {
		t.Fatalf("want 2 sessions, got %d (%+v)", len(all), all)
	}
	byRoom := map[string]string{}
	for _, s := range all {
		byRoom[s.Room] = s.Origin
	}
	if byRoom["conv_1"] != "manual" {
		t.Errorf("conv_1 origin = %q, want manual", byRoom["conv_1"])
	}
	if byRoom["run_1"] != "scheduled" {
		t.Errorf("run_1 origin = %q, want scheduled", byRoom["run_1"])
	}

	// ?origin=scheduled → only the scheduled run
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions?origin=scheduled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered status = %d, body=%s", rec.Code, rec.Body.String())
	}
	sched := decode(rec)
	if len(sched) != 1 || sched[0].Room != "run_1" {
		t.Fatalf("?origin=scheduled = %+v, want [run_1]", sched)
	}
	if sched[0].Origin != "scheduled" {
		t.Errorf("scheduled origin = %q", sched[0].Origin)
	}
	if sched[0].Preview == nil || *sched[0].Preview != "digest ready" {
		t.Errorf("scheduled preview = %v, want digest ready", sched[0].Preview)
	}

	// ?origin=manual → only the conversation
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions?origin=manual", nil))
	manual := decode(rec)
	if len(manual) != 1 || manual[0].Room != "conv_1" {
		t.Fatalf("?origin=manual = %+v, want [conv_1]", manual)
	}

	// unknown origin → empty list, 200, no error
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions?origin=bogus", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("bogus origin status = %d, want 200", rec.Code)
	}
	if got := decode(rec); len(got) != 0 {
		t.Fatalf("?origin=bogus = %+v, want empty", got)
	}
}

// TestListSessionsScheduledMergeFailureStillListsConversations verifies that a
// scheduled-agent fetch failure degrades to just the conversations instead of
// failing the whole list.
func TestListSessionsScheduledMergeFailureStillListsConversations(t *testing.T) {
	f := &fakeMemory{
		convs:    []Conversation{{ID: "conv_1", CreatedAt: "2026-08-26T09:00:00Z", UpdatedAt: "2026-08-26T09:30:00Z"}},
		schedErr: fmt.Errorf("memory 503: boom"),
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Sessions []struct {
			Room   string `json:"room"`
			Origin string `json:"origin"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].Room != "conv_1" || got.Sessions[0].Origin != "manual" {
		t.Fatalf("sessions = %+v, want [conv_1/manual]", got.Sessions)
	}
}

func TestGetSessionRecords(t *testing.T) {
	f := &fakeMemory{histories: map[string]*ConversationHistory{"conv_1": sampleHistory()}}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session?room=conv_1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Room    string `json:"room"`
		Records []struct {
			Kind  string  `json:"kind"`
			Role  *string `json:"role"`
			Text  *string `json:"text"`
			Calls []struct {
				Name    string  `json:"name"`
				Args    *string `json:"args"`
				Result  *string `json:"result"`
				IsError bool    `json:"is_error"`
			} `json:"calls"`
			Outputs []string `json:"outputs"`
			IsError *bool    `json:"is_error"`
		} `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Room != "conv_1" {
		t.Errorf("room = %q", got.Room)
	}
	// run_start/run_end skipped → 2 turns + 1 tools_executed
	if len(got.Records) != 3 {
		t.Fatalf("want 3 records, got %d: %+v", len(got.Records), got.Records)
	}
	u := got.Records[0]
	if u.Kind != "turn" || u.Role == nil || *u.Role != "user" || u.Text == nil || *u.Text != "What is the capital of France?" {
		t.Errorf("user turn wrong: %+v", u)
	}
	if u.Calls != nil || u.Outputs != nil || u.IsError != nil {
		t.Errorf("user turn should have null calls/outputs/is_error: %+v", u)
	}
	a := got.Records[1]
	if a.Kind != "turn" || a.Role == nil || *a.Role != "diane" || a.Text == nil || *a.Text != "(calls: entity-create)" {
		t.Errorf("assistant calls turn wrong: %+v", a)
	}
	tc := got.Records[2]
	if tc.Kind != "tools_executed" || tc.Role != nil || tc.Text != nil {
		t.Errorf("tools_executed record wrong: %+v", tc)
	}
	if len(tc.Calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(tc.Calls))
	}
	call := tc.Calls[0]
	if call.Name != "entity-create" {
		t.Errorf("call name = %q", call.Name)
	}
	if call.Args == nil || *call.Args != `{"name":"France"}` {
		t.Errorf("call args = %v", call.Args)
	}
	if call.Result == nil || !strings.Contains(*call.Result, `"failed":1`) {
		t.Errorf("call result = %v", call.Result)
	}
	if !call.IsError || tc.IsError == nil || !*tc.IsError {
		t.Errorf("failed tool should set is_error: call=%+v rec=%+v", call, tc)
	}
	if len(tc.Outputs) != 1 || !strings.Contains(tc.Outputs[0], `"failed":1`) {
		t.Errorf("outputs = %v", tc.Outputs)
	}
}

func TestGetSessionFallsBackToMessages(t *testing.T) {
	f := &fakeMemory{details: map[string]*ConversationDetail{
		"conv_2": {ID: "conv_2", Messages: []Message{{Role: "user", Content: "The user really likes ice cream."}}},
	}}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session?room=conv_2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Records []sessionRecord `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 1 {
		t.Fatalf("want 1 record, got %d: %+v", len(got.Records), got.Records)
	}
	r := got.Records[0]
	if r.Kind != "turn" || r.Role == nil || *r.Role != "user" || r.Text == nil || *r.Text != "The user really likes ice cream." {
		t.Errorf("record wrong: %+v", r)
	}
}

func TestListSessionsFallsBackToMessages(t *testing.T) {
	f := &fakeMemory{
		convs: []Conversation{{ID: "conv_2", CreatedAt: "2026-08-26T09:00:00Z", UpdatedAt: "2026-08-26T09:00:01Z"}},
		details: map[string]*ConversationDetail{
			"conv_2": {ID: "conv_2", Messages: []Message{{Role: "user", Content: "The user really likes ice cream."}}},
		},
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	var got struct {
		Sessions []struct {
			Room    string  `json:"room"`
			Turns   int     `json:"turns"`
			Preview *string `json:"preview"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(got.Sessions))
	}
	s := got.Sessions[0]
	if s.Turns != 1 {
		t.Errorf("turns = %d, want 1", s.Turns)
	}
	if s.Preview == nil || *s.Preview != "The user really likes ice cream." {
		t.Errorf("preview = %v", s.Preview)
	}
}

func TestGetSessionUnknownRoomEmpty(t *testing.T) {
	for _, e := range []string{
		"conversation 404 not found",
		"memory 404 not_found: conversation not found",
		"memory 400 bad_request: invalid conversation id",
	} {
		f := &fakeMemory{histErr: fmt.Errorf("%s", e)}
		_, srv := newClientEcho(f, Config{})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session?room=missing", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("err=%q want 200 empty, got %d (%s)", e, rec.Code, rec.Body.String())
		}
		var got struct {
			Records []sessionRecord `json:"records"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Records) != 0 {
			t.Errorf("err=%q want empty records, got %+v", e, got.Records)
		}
	}
}

func TestGetSessionMemoryDown(t *testing.T) {
	f := &fakeMemory{histErr: fmt.Errorf("memory 503: service down")}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session?room=x", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "memory 503") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestGetSessionMissingRoom(t *testing.T) {
	_, e := newClientEcho(&fakeMemory{}, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// --- memory proxy ---

func memoryAgentDefs() map[string]*AgentDefinition {
	return map[string]*AgentDefinition{
		"a1": {ID: "a1", Name: "diane", Tools: []string{"remember", "entity-query", "search-hybrid"}},
		"a2": {ID: "a2", Name: "memory", Tools: []string{"ha_get_state"}},
	}
}

func TestMemoryCapability(t *testing.T) {
	f := &fakeMemory{
		agents:  []AgentDefinitionSummary{{ID: "a1", Name: "diane"}, {ID: "a2", Name: "memory"}},
		defs:    memoryAgentDefs(),
		servers: []MCPServer{{ID: "mem-1", Name: "memory", ToolCount: 20}},
	}
	_, e := newClientEcho(f, Config{})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories/capability?agent=diane", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("diane status = %d (%s)", rec.Code, rec.Body.String())
	}
	var got struct {
		Agent     string `json:"agent"`
		HasMemory bool   `json:"hasMemory"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Agent != "diane" || !got.HasMemory {
		t.Errorf("diane capability = %+v, want hasMemory true", got)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories/capability?agent=memory", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.HasMemory {
		t.Errorf("memory capability = %+v, want hasMemory false", got)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories/capability?agent=ghost", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown agent status = %d, want 404", rec.Code)
	}
}

func TestMemoryCapabilityNoMemoryServer(t *testing.T) {
	// No "memory" MCP server registered → fail closed even if the agent names
	// memory tools.
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		defs:   memoryAgentDefs(),
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories/capability?agent=diane", nil))
	var got struct {
		HasMemory bool `json:"hasMemory"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.HasMemory {
		t.Error("want hasMemory false with no memory server registered")
	}
}

func TestMemoryCapabilityGlob(t *testing.T) {
	f := &fakeMemory{
		agents:  []AgentDefinitionSummary{{ID: "a3", Name: "milo"}},
		defs:    map[string]*AgentDefinition{"a3": {ID: "a3", Name: "milo", Tools: []string{"entity-*"}}},
		servers: []MCPServer{{Name: "memory"}},
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories/capability?agent=milo", nil))
	var got struct {
		HasMemory bool `json:"hasMemory"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.HasMemory {
		t.Error("glob entity-* should match a memory tool")
	}
}

func TestListMemoriesSearch(t *testing.T) {
	f := &fakeMemory{
		agents:  []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		defs:    memoryAgentDefs(),
		servers: []MCPServer{{Name: "memory"}},
		memories: []Memory{
			{ID: "m1", Content: "likes dark roast", Category: "preference", Confidence: 0.92},
			{ID: "m2", Content: "Sam (colleague)", Category: "person", Confidence: 1},
		},
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories?agent=diane&query=sam", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var got struct {
		Memories []memoryItem `json:"memories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Memories) != 2 {
		t.Fatalf("want 2 memories, got %+v", got.Memories)
	}
	if got.Memories[0] != (memoryItem{ID: "m1", Content: "likes dark roast", Category: "preference", Confidence: 0.92}) {
		t.Errorf("memory 0 = %+v", got.Memories[0])
	}
	// wire keys must be exactly {id, content, category, confidence}
	for _, key := range []string{`"id"`, `"content"`, `"category"`, `"confidence"`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("response missing key %s: %s", key, rec.Body.String())
		}
	}
}

func TestListMemoriesNoQueryListsAll(t *testing.T) {
	f := &fakeMemory{
		agents:  []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		defs:    memoryAgentDefs(),
		servers: []MCPServer{{Name: "memory"}},
		memories: []Memory{
			{ID: "e1", Content: "prefers dark roast", Category: "preference", Confidence: 0.9},
		},
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories?agent=diane", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var got struct {
		Memories []memoryItem `json:"memories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Memories) != 1 || got.Memories[0].ID != "e1" {
		t.Errorf("memories = %+v", got.Memories)
	}
}

func TestListMemoriesNoMemoryAgent(t *testing.T) {
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a2", Name: "memory"}},
		defs:   memoryAgentDefs(),
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories?agent=memory", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"memories":[]`) {
		t.Errorf("body = %s, want empty memories", rec.Body.String())
	}
}

func TestListMemoriesUnknownAgent(t *testing.T) {
	f := &fakeMemory{agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}}, defs: memoryAgentDefs()}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories?agent=ghost", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestListMemoriesMemoryDown(t *testing.T) {
	f := &fakeMemory{
		agents:  []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		defs:    memoryAgentDefs(),
		servers: []MCPServer{{Name: "memory"}},
		memErr:  fmt.Errorf("memory 503: search down"),
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memories?agent=diane&query=x", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "memory service unavailable") {
		t.Errorf("body = %s", rec.Body.String())
	}
}
