package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func newBoardEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/board", s.uiBoard)
	e.GET("/board/partial", s.uiBoardPartial)
	e.GET("/board/items/:canonicalId", s.uiBoardItem)
	e.POST("/board/items/:canonicalId/approve", s.uiBoardApprove)
	e.POST("/board/items/:canonicalId/request-changes", s.uiBoardRequestChanges)
	e.POST("/board/items/:canonicalId/retry", s.uiBoardRetry)
	e.POST("/board/items/:canonicalId/reassign", s.uiBoardReassign)
	e.POST("/board/items/:canonicalId/cancel", s.uiBoardCancel)
	return s, e
}

func TestBoardPageRendersColumnsAndCards(t *testing.T) {
	f := &fakeMemory{workItems: []WorkItem{
		{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready", Assignee: "researcher", RunCount: 1, LatestRunStatus: "success"},
		{CanonicalID: "w2", Type: "BoardTask", Key: "blocked-1", Status: "blocked", RunCount: 2, LatestRunStatus: "error", LatestRunFailureClass: "deterministic"},
		{CanonicalID: "w3", Type: "BoardTask", Key: "orphan-1", Status: "ready", Unroutable: true},
	}}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="page-board"`,
		`data-testid="board"`,
		`data-testid="board-card-w1"`,
		`data-testid="board-card-w2"`,
		`data-testid="board-drawer"`,
		`data-board-column="ready"`,
		`data-board-column="blocked"`,
		`data-board-column="done"`,
		`data-board-card`,
		`draggable="true"`,
		`hx-target="#board-drawer"`,
		`data-canonical-id="w2"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board page missing %q", want)
		}
	}
}

func TestBoardPageEmptyState(t *testing.T) {
	f := &fakeMemory{workItems: []WorkItem{}}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No work items") {
		t.Errorf("empty state missing:\n%s", rec.Body.String())
	}
}

func TestBoardPageLoadError(t *testing.T) {
	f := &fakeMemory{workItemsErr: errTestBoard}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Board unavailable") {
		t.Errorf("error state missing:\n%s", rec.Body.String())
	}
}

func TestBoardPageSchemaDerivedLanes(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{
			ObjectTypes: []CompiledType{
				{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"todo", "doing"}},
			},
		},
		workItems: []WorkItem{
			{CanonicalID: "w1", Type: "Task", Key: "k", Status: "parked"},
		},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-board-column="todo"`,
		`data-board-column="doing"`,
		`data-board-column="parked"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board missing %q", want)
		}
	}
	if strings.Contains(body, `data-board-column="done"`) {
		t.Error("canonical status not declared in schema must be absent")
	}
}

func TestBoardPageFallbackLanes(t *testing.T) {
	f := &fakeMemory{
		compiled:  &CompiledSchemaTypes{},
		workItems: []WorkItem{{CanonicalID: "w1", Type: "Task", Key: "k", Status: "ready"}},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-board-column="done"`) {
		t.Error("fallback must render the canonical done lane")
	}
}

func TestBoardStatusesFromCompiled(t *testing.T) {
	compiled := &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			{Name: "NonBoard", BoardEnabled: false, AllowedStatuses: []string{"x"}},
			{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"todo", "doing", "todo", "", "done"}},
			{Name: "Other", BoardEnabled: true, AllowedStatuses: []string{"doing", "blocked"}},
		},
	}
	got := boardStatusesFromCompiled(compiled)
	want := []string{"todo", "doing", "done", "blocked"}
	if !slices.Equal(got, want) {
		t.Errorf("boardStatusesFromCompiled = %v, want %v", got, want)
	}
	if boardStatusesFromCompiled(nil) != nil {
		t.Error("nil compiled must return nil")
	}
	if boardStatusesFromCompiled(&CompiledSchemaTypes{}) != nil {
		t.Error("empty compiled must return nil")
	}
}

// TestBoardStatusesFromCompiledSkipsShadowed pins that a shadowed (losing)
// duplicate board type cannot leak stale lane statuses: only the effective
// winner's allowed statuses are returned.
func TestBoardStatusesFromCompiledSkipsShadowed(t *testing.T) {
	compiled := &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"backlog", "shipped"}, Shadowed: true},
			{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"todo", "doing"}},
		},
	}
	got := boardStatusesFromCompiled(compiled)
	want := []string{"todo", "doing"}
	if !slices.Equal(got, want) {
		t.Errorf("boardStatusesFromCompiled = %v, want %v (shadowed statuses must not leak)", got, want)
	}
}

func TestBoardAgentHealthReadout(t *testing.T) {
	f := &fakeMemory{
		workItems: []WorkItem{{CanonicalID: "w1", Type: "BoardTask", Key: "k", Status: "ready"}},
		// ListScheduledAgents returns runtime agents (the fake uses `agents`
		// for definitions; ListScheduledAgents is a separate field below).
	}
	f.scheduledAgents = []ScheduledAgent{
		{Name: "researcher", TriggerType: "reaction", Enabled: true, ConsecutiveFailures: 3},
		{Name: "coder", TriggerType: "schedule", Enabled: true, ConsecutiveFailures: 0},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "board-agent-health") {
		t.Errorf("agent health readout missing")
	}
	if !strings.Contains(body, "researcher") {
		t.Errorf("unhealthy agent not listed")
	}
	if strings.Contains(body, "coder") {
		t.Errorf("healthy schedule agent must not be listed as unhealthy")
	}
}

func TestBoardDrawerGatesActionsByStatus(t *testing.T) {
	f := &fakeMemory{workItemDetail: &WorkItemDetail{
		Item: &WorkItem{CanonicalID: "w1", Type: "BoardTask", Key: "k", Status: "review", RunCount: 1, LatestRunStatus: "success"},
		Runs: []WorkItemRun{{ID: "run-1", Status: "success"}},
	}}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Review status gates Approve + Request changes, hides Retry.
	for _, want := range []string{`data-testid="board-item-dialog"`, `/board/items/w1/approve`, `name="feedback"`} {
		if !strings.Contains(body, want) {
			t.Errorf("drawer missing %q", want)
		}
	}
	if strings.Contains(body, "/board/items/w1/retry") {
		t.Error("retry must be hidden for a review item")
	}
}

func TestBoardApprovePostsAndRefreshes(t *testing.T) {
	f := &fakeMemory{
		workAction: &WorkItemAction{Item: &WorkItemActionItem{CanonicalID: "w1", Status: "done"}},
		workItems:  []WorkItem{},
	}
	_, e := newBoardEcho(f)
	req := httptest.NewRequest(http.MethodPost, "/board/items/w1/approve", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.lastWorkItemID != "w1" {
		t.Errorf("approve called with %q, want w1", f.lastWorkItemID)
	}
	if !strings.Contains(rec.Body.String(), `id="board"`) {
		t.Errorf("action must re-render the board:\n%s", rec.Body.String())
	}
}

var errTestBoard = &memoryHTTPError{Status: 503, Message: "down"}
