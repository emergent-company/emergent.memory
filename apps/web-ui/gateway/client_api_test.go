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

// newClientEcho registers the client-facing API routes against a fake backend.
func newClientEcho(f MemoryBackend, cfg Config) (*Server, *echo.Echo) {
	s := &Server{cfg: cfg, memory: f}
	e := echo.New()
	api := e.Group("/api")
	api.GET("/sessions", s.listSessions)
	api.GET("/session", s.getSession)
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

// TestListSessionsIncludeArchived pins the archived-visibility query and the
// summary flag the iOS Chats list uses to offer Unarchive instead of Archive.
func TestListSessionsIncludeArchived(t *testing.T) {
	f := &fakeMemory{
		convs: []Conversation{
			{ID: "conv_live", Title: "Live", CreatedAt: "2026-08-26T10:00:00Z", UpdatedAt: "2026-08-26T10:05:00Z"},
			{ID: "conv_old", Title: "Old", IsArchived: true, CreatedAt: "2026-08-26T09:00:00Z", UpdatedAt: "2026-08-26T09:30:00Z"},
		},
		histories: map[string]*ConversationHistory{
			"conv_live": {ConversationID: "conv_live", Items: []json.RawMessage{}},
			"conv_old":  {ConversationID: "conv_old", Items: []json.RawMessage{}},
		},
	}
	_, e := newClientEcho(f, Config{})

	decode := func(rec *httptest.ResponseRecorder) map[string]bool {
		t.Helper()
		var got struct {
			Sessions []struct {
				Room       string `json:"room"`
				IsArchived bool   `json:"is_archived"`
			} `json:"sessions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		out := map[string]bool{}
		for _, s := range got.Sessions {
			out[s.Room] = s.IsArchived
		}
		return out
	}

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if got := decode(rec); len(got) != 1 || got["conv_live"] != false {
		t.Fatalf("default list = %+v, want only live", got)
	}
	if f.convIncludeArchived {
		t.Fatal("default list asked memory to include archived conversations")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions?includeArchived=true", nil))
	got := decode(rec)
	if len(got) != 2 || !got["conv_old"] || got["conv_live"] != false {
		t.Fatalf("includeArchived list = %+v, want both with old marked archived", got)
	}
	if !f.convIncludeArchived {
		t.Fatal("includeArchived list did not ask memory for archived conversations")
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

// TestListSessionsBoardOrigin verifies that object-driven (board/work) runs are
// surfaced as sessions with origin "board", that their preview is derived from
// the matching work item's key (joined by subject_object_id == canonicalId),
// and that the ?origin= filter isolates them.
func TestListSessionsBoardOrigin(t *testing.T) {
	subjectID := "item-1"
	runStarted := "2026-08-27T09:00:00Z"
	runCompleted := "2026-08-27T09:01:00Z"
	f := &fakeMemory{
		boardRuns: []BoardRun{{
			ID:              "board_run_1",
			AgentID:         "agent-1",
			AgentName:       "Kanban agent",
			Status:          "completed",
			StartedAt:       runStarted,
			CompletedAt:     &runCompleted,
			Summary:         map[string]any{"summary": "raw summary text"},
			SubjectObjectID: &subjectID,
		}},
		workItems: []WorkItem{
			{CanonicalID: "item-1", Key: "ACME-42"},
		},
	}

	decode := func(rec *httptest.ResponseRecorder) []struct {
		Room    string  `json:"room"`
		Origin  string  `json:"origin"`
		Preview *string `json:"preview"`
	} {
		t.Helper()
		var got struct {
			Sessions []struct {
				Room    string  `json:"room"`
				Origin  string  `json:"origin"`
				Preview *string `json:"preview"`
			} `json:"sessions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
		}
		return got.Sessions
	}

	_, e := newClientEcho(f, Config{})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	all := decode(rec)
	if len(all) != 1 {
		t.Fatalf("want 1 session, got %d (%+v)", len(all), all)
	}
	if all[0].Room != "board_run_1" || all[0].Origin != "board" {
		t.Errorf("session = %q/%q, want board_run_1/board", all[0].Room, all[0].Origin)
	}
	if all[0].Preview == nil || *all[0].Preview != "ACME-42" {
		t.Errorf("preview = %v, want ACME-42", all[0].Preview)
	}

	// ?origin=board → only the board run
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions?origin=board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered status = %d, body=%s", rec.Code, rec.Body.String())
	}
	board := decode(rec)
	if len(board) != 1 || board[0].Room != "board_run_1" {
		t.Fatalf("?origin=board = %+v, want [board_run_1]", board)
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

// TestListSessionsAgentMetadata pins agent_definition_id/title on both
// conversation- and scheduled-derived summaries, and the ?agentId= filter.
func TestListSessionsAgentMetadata(t *testing.T) {
	agentA := "agent-a"
	f := &fakeMemory{
		convs: []Conversation{
			{ID: "conv_1", Title: "Fix the bug", AgentDefinitionID: "agent-a", CreatedAt: "2026-08-26T09:00:00Z", UpdatedAt: "2026-08-26T09:30:00Z"},
			{ID: "conv_2", AgentDefinitionID: "agent-b", CreatedAt: "2026-08-26T08:00:00Z", UpdatedAt: "2026-08-26T08:30:00Z"},
			{ID: "conv_3", CreatedAt: "2026-08-26T07:00:00Z", UpdatedAt: "2026-08-26T07:30:00Z"},
		},
		scheduledAgents: []ScheduledAgent{
			{ID: "sa1", Name: "Daily", TriggerType: "schedule", AgentDefinitionID: &agentA},
		},
		scheduledRuns: map[string][]ScheduledAgentRun{
			"sa1": {{ID: "run_1", AgentID: "sa1", Status: "completed", StartedAt: "2026-08-27T08:00:00Z"}},
		},
	}

	type summary struct {
		Room              string `json:"room"`
		AgentDefinitionID string `json:"agent_definition_id"`
		Title             string `json:"title"`
	}
	decode := func(rec *httptest.ResponseRecorder) []summary {
		t.Helper()
		var got struct {
			Sessions []summary `json:"sessions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
		}
		return got.Sessions
	}

	_, e := newClientEcho(f, Config{})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	byRoom := map[string]summary{}
	for _, s := range decode(rec) {
		byRoom[s.Room] = s
	}
	if got := byRoom["conv_1"]; got.AgentDefinitionID != "agent-a" || got.Title != "Fix the bug" {
		t.Errorf("conv_1 metadata = %+v, want agent-a/Fix the bug", got)
	}
	if got := byRoom["conv_2"]; got.AgentDefinitionID != "agent-b" || got.Title != "" {
		t.Errorf("conv_2 metadata = %+v, want agent-b/empty title", got)
	}
	if got := byRoom["conv_3"]; got.AgentDefinitionID != "" || got.Title != "" {
		t.Errorf("conv_3 metadata = %+v, want empty", got)
	}
	if got := byRoom["run_1"]; got.AgentDefinitionID != "agent-a" {
		t.Errorf("run_1 agent_definition_id = %q, want agent-a", got.AgentDefinitionID)
	}

	// ?agentId= filters to the matching agent definition across origins.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions?agentId=agent-a", nil))
	filtered := decode(rec)
	if len(filtered) != 2 {
		t.Fatalf("?agentId=agent-a = %+v, want [conv_1 run_1]", filtered)
	}
	for _, s := range filtered {
		if s.AgentDefinitionID != "agent-a" {
			t.Errorf("filtered session %q has agent %q", s.Room, s.AgentDefinitionID)
		}
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions?agentId=agent-b", nil))
	if got := decode(rec); len(got) != 1 || got[0].Room != "conv_2" {
		t.Fatalf("?agentId=agent-b = %+v, want [conv_2]", got)
	}

	// agentId composes with origin.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions?agentId=agent-a&origin=manual", nil))
	if got := decode(rec); len(got) != 1 || got[0].Room != "conv_1" {
		t.Fatalf("?agentId=agent-a&origin=manual = %+v, want [conv_1]", got)
	}
}

// TestGetSessionMetadataAndPendingApprovals pins the detail response's
// agent_definition_id/title and the pending-approval projection (only pending
// rows for this room), while records keep their existing shape.
func TestGetSessionMetadataAndPendingApprovals(t *testing.T) {
	f := &fakeMemory{
		histories: map[string]*ConversationHistory{"conv_1": sampleHistory()},
		details: map[string]*ConversationDetail{
			"conv_1": {ID: "conv_1", Title: "Fix the bug", AgentDefinitionID: "agent-a"},
		},
		approvals: []ToolApprovalItem{
			{ID: "a1", ConversationID: "conv_1", Decision: "pending", QuestionID: "q1", ToolName: "delete_all", ArgsSummary: map[string]any{"scope": "all"}},
			{ID: "a2", ConversationID: "conv_1", Decision: "approved", QuestionID: "q2", ToolName: "deploy"},
			{ID: "a3", ConversationID: "conv_2", Decision: "pending", QuestionID: "q3", ToolName: "other_room"},
		},
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session?room=conv_1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Room              string `json:"room"`
		AgentDefinitionID string `json:"agent_definition_id"`
		Title             string `json:"title"`
		PendingApprovals  []struct {
			QuestionID string         `json:"questionId"`
			Tool       string         `json:"tool"`
			Input      map[string]any `json:"input"`
		} `json:"pending_approvals"`
		Records []sessionRecord `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AgentDefinitionID != "agent-a" || got.Title != "Fix the bug" {
		t.Errorf("metadata = %q/%q, want agent-a/Fix the bug", got.AgentDefinitionID, got.Title)
	}
	if len(got.PendingApprovals) != 1 {
		t.Fatalf("pending_approvals = %+v, want exactly the one pending row for conv_1", got.PendingApprovals)
	}
	pa := got.PendingApprovals[0]
	if pa.QuestionID != "q1" || pa.Tool != "delete_all" || pa.Input["scope"] != "all" {
		t.Errorf("pending approval = %+v", pa)
	}
	if len(got.Records) != 3 {
		t.Errorf("records length = %d, want unchanged 3", len(got.Records))
	}
}

// TestGetSessionExtrasFailuresDoNotFailResponse pins best-effort metadata and
// approvals: a failing lookup omits the field but still returns the timeline.
func TestGetSessionExtrasFailuresDoNotFailResponse(t *testing.T) {
	f := &fakeMemory{
		histories:    map[string]*ConversationHistory{"conv_1": sampleHistory()},
		detailErr:    fmt.Errorf("memory 503: lookup down"),
		approvalsErr: fmt.Errorf("memory 503: approvals down"),
	}
	_, e := newClientEcho(f, Config{})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session?room=conv_1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		AgentDefinitionID string           `json:"agent_definition_id"`
		Title             string           `json:"title"`
		PendingApprovals  []map[string]any `json:"pending_approvals"`
		Records           []sessionRecord  `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AgentDefinitionID != "" || got.Title != "" || got.PendingApprovals != nil {
		t.Errorf("expected omitted extras, got %+v", got)
	}
	if len(got.Records) != 3 {
		t.Errorf("records length = %d, want 3", len(got.Records))
	}
}

// --- project configuration ---
// TestProjectConfig pins the device-facing voice flag: stored true/false, an
// absent setting (a fresh project) as disabled, and a backend read failure as
// 502 so the device keeps its cached value instead of silently hiding voice.
func TestProjectConfig(t *testing.T) {
	cases := []struct {
		name       string
		f          *fakeMemory
		wantStatus int
		wantVoice  bool
	}{
		{
			name: "voice enabled",
			f: &fakeMemory{settings: map[string]map[string]map[string]any{
				voiceCategory: {voiceKeyEnabled: {"value": true}},
			}},
			wantStatus: http.StatusOK,
			wantVoice:  true,
		},
		{
			name: "voice disabled",
			f: &fakeMemory{settings: map[string]map[string]map[string]any{
				voiceCategory: {voiceKeyEnabled: {"value": false}},
			}},
			wantStatus: http.StatusOK,
			wantVoice:  false,
		},
		{
			name:       "unset defaults to off",
			f:          &fakeMemory{},
			wantStatus: http.StatusOK,
			wantVoice:  false,
		},
		{
			name:       "non-bool value defaults to off",
			f:          &fakeMemory{settings: map[string]map[string]map[string]any{voiceCategory: {voiceKeyEnabled: {"value": "yes"}}}},
			wantStatus: http.StatusOK,
			wantVoice:  false,
		},
		{
			name:       "backend read failure",
			f:          &fakeMemory{settingErr: context.DeadlineExceeded},
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{memory: tc.f}
			e := echo.New()
			e.GET("/api/project/config", s.projectConfig)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/project/config", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var got map[string]bool
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
			}
			if got["voiceEnabled"] != tc.wantVoice {
				t.Errorf("voiceEnabled = %v, want %v", got["voiceEnabled"], tc.wantVoice)
			}
			if len(got) != 1 {
				t.Errorf("payload = %v, want exactly voiceEnabled (no secrets/endpoints)", got)
			}
		})
	}
}

// An assistant turn carries its citations through to the session record so the
// iOS chat can draw the Sources footer for recorded turns; a user turn, and an
// assistant turn that cited nothing (or carried an unreadable value), carries
// none.
func TestMessageRecordCarriesAssistantCitations(t *testing.T) {
	cites := json.RawMessage(`[{"kind":"object","id":"11111111-1111-1111-1111-111111111111","type":"person","label":"Sam","url":"/objects/11111111-1111-1111-1111-111111111111"}]`)

	rec := messageRecord(TimelineItem{Kind: "message", Role: "diane", Content: &MessageContent{Text: "Sam works here."}, Citations: cites})
	if string(rec.Citations) != string(cites) {
		t.Errorf("assistant citations = %s, want %s", rec.Citations, cites)
	}
	if user := messageRecord(TimelineItem{Kind: "message", Role: "user", Content: &MessageContent{Text: "who?"}, Citations: cites}); user.Citations != nil {
		t.Errorf("user turn citations = %s, want none", user.Citations)
	}
	for _, raw := range []string{"", "null", "[]", `"oops"`} {
		got := messageRecord(TimelineItem{Kind: "message", Role: "diane", Content: &MessageContent{Text: "x"}, Citations: json.RawMessage(raw)})
		if got.Citations != nil {
			t.Errorf("citations %q: record carries %s, want none", raw, got.Citations)
		}
	}

	// Omitted on the wire when absent, so older clients see no new key.
	out, err := json.Marshal(messageRecord(TimelineItem{Kind: "message", Role: "diane", Content: &MessageContent{Text: "x"}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "citations") {
		t.Errorf("record without citations marshals %s", out)
	}
}
