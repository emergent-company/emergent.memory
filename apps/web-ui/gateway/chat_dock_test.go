package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- helpers ---

func runItem(kind, runID, status string) json.RawMessage {
	return mustJSON(TimelineItem{Kind: kind, RunID: runID, RunStatus: status})
}

func askUserCall(qid string) json.RawMessage {
	return mustJSON(TimelineItem{Kind: "tool_call", ToolName: "ask_user", ToolOutput: mustJSON(map[string]string{"question_id": qid})})
}

func convStateServer(histItems []json.RawMessage) *Server {
	f := &fakeMemory{}
	if histItems != nil {
		f.histories = map[string]*ConversationHistory{"c1": {ConversationID: "c1", Items: histItems}}
	}
	return &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
}

// --- bucket derivation ---

func TestDeriveRunBucket(t *testing.T) {
	pending := []string{"q1"}
	tests := []struct {
		name   string
		status string
		appr   []string
		quests []string
		want   string
	}{
		{"no runs", "", nil, nil, runBucketDone},
		{"working", "working", nil, nil, runBucketRunning},
		{"submitted", "submitted", nil, nil, runBucketRunning},
		{"cancelling", "cancelling", nil, nil, runBucketRunning},
		{"completed", "completed", nil, nil, runBucketDone},
		{"cancelled", "cancelled", nil, nil, runBucketDone},
		{"skipped", "skipped", nil, nil, runBucketDone},
		{"failed", "failed", nil, nil, runBucketFailed},
		{"input-required", "input-required", nil, nil, runBucketNeedsInput},
		{"pending approval outranks failed", "failed", pending, nil, runBucketNeedsInput},
		{"pending question outranks failed", "failed", nil, pending, runBucketNeedsInput},
		{"pending approval outranks running", "working", pending, nil, runBucketNeedsInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveRunBucket(tt.status, tt.appr, tt.quests); got != tt.want {
				t.Errorf("deriveRunBucket(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// TestConversationStateBucket asserts the full bucket derivation through
// conversationState (history + project snapshots) — the pending-approval
// priority over a failed run, the working→running mapping, the no-runs case,
// and the active-run id/status from the newest run lifecycle item.
func TestConversationStateBucket(t *testing.T) {
	unanswered := &AgentQuestionItem{ID: "q1"}
	answered := &AgentQuestionItem{ID: "q1", Response: strPtr("yes")}
	tests := []struct {
		name       string
		items      []json.RawMessage
		approvals  []ToolApprovalItem
		questions  []AgentQuestionItem
		wantBucket string
		wantRunID  string
		wantStatus string
	}{
		{
			name:       "no runs",
			items:      []json.RawMessage{},
			wantBucket: runBucketDone,
		},
		{
			name:       "working run is running",
			items:      []json.RawMessage{runItem("run_start", "r1", "working")},
			wantBucket: runBucketRunning,
			wantRunID:  "r1",
			wantStatus: "working",
		},
		{
			name:       "failed run is failed",
			items:      []json.RawMessage{runItem("run_end", "r1", "failed")},
			wantBucket: runBucketFailed,
			wantRunID:  "r1",
			wantStatus: "failed",
		},
		{
			name:       "completed run is done",
			items:      []json.RawMessage{runItem("run_end", "r1", "completed")},
			wantBucket: runBucketDone,
			wantRunID:  "r1",
			wantStatus: "completed",
		},
		{
			name:       "pending approval outranks failed",
			items:      []json.RawMessage{runItem("run_end", "r1", "failed")},
			approvals:  []ToolApprovalItem{{ConversationID: "c1", Decision: "pending", QuestionID: "a1"}},
			wantBucket: runBucketNeedsInput,
			wantRunID:  "r1",
			wantStatus: "failed",
		},
		{
			name:       "input-required run is needs_input",
			items:      []json.RawMessage{runItem("run_end", "r1", "input-required")},
			wantBucket: runBucketNeedsInput,
			wantRunID:  "r1",
			wantStatus: "input-required",
		},
		{
			name:       "unanswered ask_user forces needs_input",
			items:      []json.RawMessage{runItem("run_start", "r1", "working"), askUserCall("q1")},
			questions:  []AgentQuestionItem{*unanswered},
			wantBucket: runBucketNeedsInput,
			wantRunID:  "r1",
			wantStatus: "working",
		},
		{
			name:       "answered ask_user does not force needs_input",
			items:      []json.RawMessage{runItem("run_start", "r1", "working"), askUserCall("q1")},
			questions:  []AgentQuestionItem{*answered},
			wantBucket: runBucketRunning,
			wantRunID:  "r1",
			wantStatus: "working",
		},
		{
			name:       "newest lifecycle item decides",
			items:      []json.RawMessage{runItem("run_start", "r1", "working"), runItem("run_end", "r2", "completed")},
			wantBucket: runBucketDone,
			wantRunID:  "r2",
			wantStatus: "completed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := convStateServer(tt.items)
			st, err := s.conversationState(t.Context(), "c1", tt.approvals, tt.questions)
			if err != nil {
				t.Fatalf("conversationState: %v", err)
			}
			if st.bucket != tt.wantBucket {
				t.Errorf("bucket = %q, want %q", st.bucket, tt.wantBucket)
			}
			if st.activeRunID != tt.wantRunID {
				t.Errorf("activeRunID = %q, want %q", st.activeRunID, tt.wantRunID)
			}
			if st.activeRunStatus != tt.wantStatus {
				t.Errorf("activeRunStatus = %q, want %q", st.activeRunStatus, tt.wantStatus)
			}
		})
	}
}

// TestConversationStateSingleHistoryFetch asserts the bucket derivation reuses
// the poller's single history fetch — conversationState must call
// GetConversationHistory exactly once and not add an upstream lookup.
func TestConversationStateSingleHistoryFetch(t *testing.T) {
	m := &hubRecordingMemory{fakeMemory: &fakeMemory{}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: m}
	if _, err := s.conversationState(t.Context(), "c1", nil, nil); err != nil {
		t.Fatalf("conversationState: %v", err)
	}
	history, _, _ := m.contexts()
	if len(history) != 1 {
		t.Fatalf("conversationState fetched history %d times, want exactly 1", len(history))
	}
}

// TestRefreshFramePayload asserts the published refresh payload shape and that
// a consumer decoding only {"type":"refresh"} still sees the type field (extra
// fields are additive and ignorable).
func TestRefreshFramePayload(t *testing.T) {
	st := &conversationRunState{
		bucket:           runBucketNeedsInput,
		activeRunID:      "r1",
		activeRunStatus:  "input-required",
		pendingApprovals: []string{"a1", "a2"},
		pendingQuestions: []string{"q1"},
	}
	var full refreshPayload
	if err := json.Unmarshal(refreshFrame(st), &full); err != nil {
		t.Fatalf("refresh frame is not valid JSON: %v", err)
	}
	if full.Type != "refresh" || full.Bucket != "needs_input" || full.RunID != "r1" || full.RunStatus != "input-required" {
		t.Errorf("refresh payload = %+v", full)
	}
	if full.PendingApprovals != 2 || full.PendingQuestions != 1 {
		t.Errorf("pending counts = approvals %d questions %d, want 2/1", full.PendingApprovals, full.PendingQuestions)
	}
	// A minimal consumer ignoring unknown fields still reads type=refresh.
	var minimal struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(refreshFrame(st), &minimal); err != nil || minimal.Type != "refresh" {
		t.Errorf("minimal consumer must still decode type=refresh, got %q err %v", minimal.Type, err)
	}
	// A nil state degrades to the bare frame.
	if got := string(refreshFrame(nil)); got != `{"type":"refresh"}` {
		t.Errorf("nil-state frame = %q, want bare refresh", got)
	}
}

// --- cancel ---

func newCancelEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/api/chat/runs/:runId/cancel", s.cancelAgentRun)
	return s, e
}

func cancelBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("cancel response is not JSON: %v (%s)", err, rec.Body.String())
	}
	return out
}

func TestCancelAgentRunHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeMemory{agentRuns: map[string]*AgentRun{"r1": {AgentID: "a1"}}}
		_, e := newCancelEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat/runs/r1/cancel", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		body := cancelBody(t, rec)
		if body["ok"] != true || body["runId"] != "r1" {
			t.Errorf("body = %+v, want ok:true runId:r1", body)
		}
		if f.cancelAgentID != "a1" || f.cancelRunID != "r1" {
			t.Errorf("cancel forwarded as agent=%q run=%q, want a1/r1", f.cancelAgentID, f.cancelRunID)
		}
	})
	t.Run("already-terminal is non-error", func(t *testing.T) {
		// The upstream cancel is idempotent: a terminal run still returns 200,
		// so CancelAgentRun succeeds and the gateway reports ok:true.
		f := &fakeMemory{agentRuns: map[string]*AgentRun{"r1": {AgentID: "a1"}}}
		_, e := newCancelEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat/runs/r1/cancel", nil))
		body := cancelBody(t, rec)
		if body["ok"] != true {
			t.Errorf("terminal-run cancel body = %+v, want ok:true", body)
		}
	})
	t.Run("unresolvable run id", func(t *testing.T) {
		f := &fakeMemory{}
		_, e := newCancelEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat/runs/missing/cancel", nil))
		body := cancelBody(t, rec)
		if body["ok"] != false || body["reason"] == "" {
			t.Errorf("body = %+v, want ok:false with a reason", body)
		}
	})
	t.Run("unresolvable agent id", func(t *testing.T) {
		f := &fakeMemory{agentRuns: map[string]*AgentRun{"r1": {}}}
		_, e := newCancelEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat/runs/r1/cancel", nil))
		body := cancelBody(t, rec)
		if body["ok"] != false || body["reason"] == "" {
			t.Errorf("body = %+v, want ok:false with a reason", body)
		}
	})
	t.Run("upstream failure", func(t *testing.T) {
		f := &fakeMemory{agentRuns: map[string]*AgentRun{"r1": {AgentID: "a1"}}, cancelAgentRunErr: errTestCancel}
		_, e := newCancelEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat/runs/r1/cancel", nil))
		body := cancelBody(t, rec)
		if body["ok"] != false || body["reason"] == "" {
			t.Errorf("body = %+v, want ok:false with a reason", body)
		}
	})
}

// errTestCancel is a sentinel error for the upstream-failure cancel case.
var errTestCancel = &memoryHTTPError{Status: http.StatusBadGateway, Message: "memory unavailable"}

func TestCancelAgentRunClient(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"message":"Run cancelled successfully"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "static-proj")
	if err := m.CancelAgentRun(sessCtx("static-token"), "a1", "r1"); err != nil {
		t.Fatalf("CancelAgentRun: %v", err)
	}
	if gotPath != "/api/projects/static-proj/agents/a1/runs/r1/cancel" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/projects/static-proj/agents/a1/runs/r1/cancel", gotMethod, gotPath)
	}
	if gotAuth != "Bearer static-token" {
		t.Errorf("auth = %q, want static bearer", gotAuth)
	}
}

func TestCancelAgentRunClientNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"run not found"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	err := m.CancelAgentRun(context.Background(), "a1", "r1")
	if err == nil {
		t.Fatal("non-2xx cancel must return an error")
	}
	if !isMemoryStatus(err, http.StatusNotFound) {
		t.Errorf("error status = %d, want 404", memoryStatus(err))
	}
}

// --- session todos ---

func TestListSessionTodosClient(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"t1","sessionId":"s1","content":"Write tests","status":"in_progress","order":1,"createdAt":"2026-09-01T10:00:00Z","updatedAt":"2026-09-01T10:05:00Z"},{"id":"t2","sessionId":"s1","content":"Ship","status":"pending","order":2,"createdAt":"2026-09-01T10:01:00Z","updatedAt":"2026-09-01T10:01:00Z"}]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	todos, err := m.ListSessionTodos(context.Background(), "s1")
	if err != nil {
		t.Fatalf("ListSessionTodos: %v", err)
	}
	if gotPath != "/api/v1/agent/sessions/s1/todos" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/v1/agent/sessions/s1/todos", gotMethod, gotPath)
	}
	if len(todos) != 2 {
		t.Fatalf("got %d todos, want 2", len(todos))
	}
	if todos[0].ID != "t1" || todos[0].Content != "Write tests" || todos[0].Status != "in_progress" || todos[0].Order != 1 {
		t.Errorf("todo[0] = %+v", todos[0])
	}
	if todos[1].Status != "pending" || todos[1].Order != 2 {
		t.Errorf("todo[1] = %+v", todos[1])
	}
}

func TestListSessionTodosClientEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "proj")
	todos, err := m.ListSessionTodos(context.Background(), "s1")
	if err != nil {
		t.Fatalf("ListSessionTodos: %v", err)
	}
	if len(todos) != 0 {
		t.Fatalf("got %d todos, want 0", len(todos))
	}
}

func newTodosEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/partial/chat-todos", s.uiChatTodos)
	return s, e
}

func TestUiChatTodos(t *testing.T) {
	t.Run("populated", func(t *testing.T) {
		f := &fakeMemory{
			details:      map[string]*ConversationDetail{"c1": {ID: "c1", ACPSessionID: "s1"}},
			sessionTodos: []SessionTodo{{ID: "t1", Content: "Write tests", Status: "in_progress", Order: 1}},
		}
		_, e := newTodosEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-todos?c=c1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-testid="todo-card"`) || !strings.Contains(body, "Write tests") {
			t.Errorf("body missing todo card/content: %s", body)
		}
		if f.lastTodosSessionID != "s1" {
			t.Errorf("todos fetched for session %q, want s1", f.lastTodosSessionID)
		}
	})
	t.Run("empty todos", func(t *testing.T) {
		f := &fakeMemory{details: map[string]*ConversationDetail{"c1": {ID: "c1", ACPSessionID: "s1"}}}
		_, e := newTodosEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-todos?c=c1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("empty todos must render an empty fragment, got %q", rec.Body.String())
		}
	})
	t.Run("missing ACP session", func(t *testing.T) {
		f := &fakeMemory{details: map[string]*ConversationDetail{"c1": {ID: "c1"}}}
		_, e := newTodosEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-todos?c=c1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("missing session must render an empty fragment, got %q", rec.Body.String())
		}
		if f.lastTodosSessionID != "" {
			t.Errorf("no session → no todos fetch, got session %q", f.lastTodosSessionID)
		}
	})
}

// --- dock ---

func newDockEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/partial/chat-dock", s.uiChatDock)
	return s, e
}

func TestUiChatDock(t *testing.T) {
	t.Run("populated approvals and questions", func(t *testing.T) {
		f := &fakeMemory{
			approvals: []ToolApprovalItem{
				{QuestionID: "a1", ToolName: "web_search", ArgsSummary: map[string]any{"q": "x"}, ConversationID: "c1", Decision: "pending"},
				{QuestionID: "a2", ToolName: "web_search", ArgsSummary: map[string]any{"q": "y"}, ConversationID: "c2", Decision: "pending"},
			},
			histories: map[string]*ConversationHistory{"c1": {ConversationID: "c1", Items: []json.RawMessage{askUserCall("q1")}}},
		}
		_, e := newDockEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-dock?c=c1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-testid="chat-dock"`) || !strings.Contains(body, "web_search") {
			t.Errorf("dock body missing container/approval: %s", body)
		}
		// The c2 approval is not this conversation's, so it must not render.
		if strings.Contains(body, `data-question-id="a2"`) {
			t.Errorf("dock rendered another conversation's approval: %s", body)
		}
	})
	t.Run("empty dock", func(t *testing.T) {
		f := &fakeMemory{}
		_, e := newDockEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-dock?c=c1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("empty dock must render an empty fragment, got %q", rec.Body.String())
		}
	})
}

// --- templ components ---

func TestDockPanelRender(t *testing.T) {
	html := renderHTML(t, DockPanel("c1",
		[]ApprovalCard{{QuestionID: "a1", ToolName: "web_search", ArgsSummary: `{"q":"x"}`, ConversationID: "c1"}},
		[]QuestionCard{{QuestionID: "q1", Prompt: "Pick one", Kind: "buttons", Options: []AgentQuestionOption{
			{Label: "Alpha", Value: "alpha"},
			{Label: "Beta", Value: "beta"},
		}}},
	))
	for _, want := range []string{
		`data-testid="chat-dock"`,
		`data-testid="dock-approval"`,
		`data-question-id="a1"`,
		"web_search",
		`data-testid="dock-question"`,
		"Pick one",
		"Alpha",
		"Beta",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("DockPanel missing %q:\n%s", want, html)
		}
	}
	if got := renderHTML(t, DockPanel("c1", nil, nil)); got != "" {
		t.Errorf("empty DockPanel must render nothing, got %q", got)
	}
}

// TestDockPanelOptionExposesValue pins the label-vs-value fix: an ask_user
// option whose label differs from its value must render the label as the button
// text and expose the value on data-dock-option-value — never the label as the
// submitted value. This fails on the old behaviour, which exposed the label in
// data-dock-option and had no value attribute.
func TestDockPanelOptionExposesValue(t *testing.T) {
	html := renderHTML(t, DockPanel("c1", nil, []QuestionCard{{
		QuestionID: "q1",
		Prompt:     "Pick one",
		Kind:       "buttons",
		Options: []AgentQuestionOption{
			{Label: "Yes, please", Value: "yes"},
			{Label: "No thanks", Value: "no"},
		},
	}}))

	if !strings.Contains(html, `data-dock-option-value="yes"`) {
		t.Errorf("option value must be exposed on data-dock-option-value:\n%s", html)
	}
	if !strings.Contains(html, `data-dock-option-value="no"`) {
		t.Errorf("second option value must be exposed on data-dock-option-value:\n%s", html)
	}
	if strings.Contains(html, `data-dock-option-value="Yes, please"`) {
		t.Errorf("option label leaked into the value attribute:\n%s", html)
	}
	if strings.Contains(html, `data-dock-option="`) {
		t.Errorf("legacy data-dock-option (label-as-value) attribute still rendered:\n%s", html)
	}
	// The label is still the visible button text.
	if !strings.Contains(html, "Yes, please") || !strings.Contains(html, "No thanks") {
		t.Errorf("option labels must render as button text:\n%s", html)
	}
}

func TestTodoCardRender(t *testing.T) {
	html := renderHTML(t, TodoCard([]TodoItem{
		{ID: "t1", Content: "Write tests", Status: "in_progress", Order: 1},
		{ID: "t2", Content: "Ship", Status: "pending", Order: 2},
	}))
	for _, want := range []string{
		`data-testid="todo-card"`,
		`data-testid="todo-card-toggle"`,
		`aria-expanded="false"`,
		`data-status="in_progress"`,
		`data-order="1"`,
		"Write tests",
		"Ship",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("TodoCard missing %q:\n%s", want, html)
		}
	}
	if got := renderHTML(t, TodoCard(nil)); got != "" {
		t.Errorf("empty TodoCard must render nothing, got %q", got)
	}
}

func TestRailStatusBadgeRender(t *testing.T) {
	tests := []struct {
		bucket string
		label  string
	}{
		{runBucketNeedsInput, "Needs input"},
		{runBucketFailed, "Failed"},
		{runBucketRunning, "Running"},
		{runBucketDone, ""},
	}
	for _, tt := range tests {
		t.Run(tt.bucket, func(t *testing.T) {
			html := renderHTML(t, RailStatusBadge(tt.bucket, 2, 1))
			for _, want := range []string{
				`data-bucket="` + tt.bucket + `"`,
				`data-pending-approvals="2"`,
				`data-pending-questions="1"`,
			} {
				if !strings.Contains(html, want) {
					t.Errorf("RailStatusBadge(%q) missing %q:\n%s", tt.bucket, want, html)
				}
			}
			if tt.label != "" && !strings.Contains(html, tt.label) {
				t.Errorf("RailStatusBadge(%q) missing label %q:\n%s", tt.bucket, tt.label, html)
			}
		})
	}
}

// --- rail row data ---

func TestChatRailDataCarriesBucket(t *testing.T) {
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		convs: []Conversation{
			{ID: "c1", Title: "failed", AgentDefinitionID: "a1"},
			{ID: "c2", Title: "needs input", AgentDefinitionID: "a1"},
		},
		histories: map[string]*ConversationHistory{
			"c1": {ConversationID: "c1", Items: []json.RawMessage{runItem("run_end", "r1", "failed")}},
			"c2": {ConversationID: "c2", Items: []json.RawMessage{runItem("run_end", "r2", "working")}},
		},
		approvals: []ToolApprovalItem{{ConversationID: "c2", Decision: "pending", QuestionID: "a1"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	_, _, convs, _, _, err := s.chatRailData(t.Context())
	if err != nil {
		t.Fatalf("chatRailData: %v", err)
	}
	byID := map[string]Conversation{}
	for _, c := range convs.Conversations {
		byID[c.ID] = c
	}
	if got := byID["c1"]; got.Bucket != runBucketFailed || got.PendingApprovals != 0 || got.PendingQuestions != 0 {
		t.Errorf("c1 row = bucket %q approvals %d questions %d, want failed/0/0", got.Bucket, got.PendingApprovals, got.PendingQuestions)
	}
	if got := byID["c2"]; got.Bucket != runBucketNeedsInput || got.PendingApprovals != 1 || got.PendingQuestions != 0 {
		t.Errorf("c2 row = bucket %q approvals %d questions %d, want needs_input/1/0", got.Bucket, got.PendingApprovals, got.PendingQuestions)
	}
}

// TestChatScheduledRunsResolveAppearance asserts the scheduled-run plumbing in
// chatScheduledRuns resolves each run row's Icon/Color from its agent
// definition id. A broken lookup here would silently fall back to the neutral
// bot glyph while the render-level tests (which pass Icon/Color in directly)
// still pass — so this exercises the production data path end to end.
func TestChatScheduledRunsResolveAppearance(t *testing.T) {
	defID := "a1"
	f := &fakeMemory{
		scheduledAgents: []ScheduledAgent{
			{ID: "s1", Name: "Daily briefing", TriggerType: "schedule", AgentDefinitionID: &defID},
			{ID: "s2", Name: "Legacy scheduled", TriggerType: "schedule"}, // no definition link
		},
		scheduledRuns: map[string][]ScheduledAgentRun{
			"s1": {{ID: "r1", Status: "completed", StartedAt: "2026-08-27T08:00:00Z"}},
			"s2": {{ID: "r2", Status: "completed", StartedAt: "2026-08-27T09:00:00Z"}},
		},
	}
	s := &Server{memory: f}
	appearances := map[string]agentAppearance{"a1": {Name: "memory", Icon: "database", Color: "#2563EB"}}

	rows := s.chatScheduledRuns(t.Context(), appearances)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	byID := map[string]scheduledRunRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	got, ok := byID["r1"]
	if !ok {
		t.Fatal("missing r1 run row")
	}
	if got.Icon != "database" || got.Color != "#2563EB" {
		t.Errorf("linked run appearance = %q/%q, want database/#2563EB", got.Icon, got.Color)
	}
	got, ok = byID["r2"]
	if !ok {
		t.Fatal("missing r2 run row")
	}
	if got.Icon != "" || got.Color != "" {
		t.Errorf("unlinked run appearance = %q/%q, want empty (neutral bot)", got.Icon, got.Color)
	}
}
