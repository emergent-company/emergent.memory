package main

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// boardStatusMapAttr extracts and decodes the #board[data-board-status-map]
// value from a rendered board response (templ HTML-escapes the JSON quotes).
func boardStatusMapAttr(t *testing.T, body string) boardStatusMap {
	t.Helper()
	const marker = `data-board-status-map="`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("missing data-board-status-map:\n%s", body)
	}
	rest := body[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatalf("unterminated data-board-status-map: %q", rest)
	}
	raw := html.UnescapeString(rest[:j])
	var m boardStatusMap
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("data-board-status-map %q is not JSON: %v", raw, err)
	}
	return m
}

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
	e.GET("/objects/:id/preview", s.uiObjectPreviewPartial)
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
		// #board is the stable post-swap focus target (object-preview.js).
		`tabindex="-1"`,
		`data-testid="board-card-w1"`,
		`data-testid="board-card-w2"`,
		`data-testid="board-drawer"`,
		`data-board-column="ready"`,
		`data-board-column="blocked"`,
		`data-board-column="done"`,
		`data-board-card`,
		`draggable="true"`,
		`data-board-open-preview`,
		`aria-haspopup="dialog"`,
		`data-canonical-id="w2"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board page missing %q", want)
		}
	}
	// Clicking a card opens the shared object preview, not the board dialog.
	if strings.Contains(body, `hx-target="#board-drawer"`) {
		t.Error("board card must not load the board dialog on click")
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

// TestObjectPreviewBoardActionsSlot pins the ?actions=board extension: the
// object summary carries the work item's status-gated action routes in the
// shared #object-preview-actions-slot.
func TestObjectPreviewBoardActionsSlot(t *testing.T) {
	f := &fakeMemory{
		objects: []GraphObject{{ID: "w1", Type: "BoardTask", Key: "k", Status: "review"}},
		workItemDetail: &WorkItemDetail{
			Item: &WorkItem{CanonicalID: "w1", Type: "BoardTask", Key: "k", Status: "review"},
		},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/w1/preview?actions=board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="object-preview-actions-slot"`,
		`/board/items/w1/approve`,
		`/board/items/w1/request-changes`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board preview missing %q", want)
		}
	}
}

// TestObjectPreviewWithoutActionsStaysReadOnly pins that chat previews (no
// actions param) render the empty action slot and never fetch or render a work
// item's board actions.
func TestObjectPreviewWithoutActionsStaysReadOnly(t *testing.T) {
	f := &fakeMemory{
		objects:        []GraphObject{{ID: "w1", Type: "BoardTask", Key: "k", Status: "review"}},
		workItemDetail: &WorkItemDetail{Item: &WorkItem{CanonicalID: "w1", Status: "review"}},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/w1/preview", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="object-preview-actions-slot"`) {
		t.Errorf("preview missing action slot:\n%s", body)
	}
	if strings.Contains(body, "/board/items/") {
		t.Errorf("read-only preview must not render board actions:\n%s", body)
	}
}

// TestBoardItemPath pins the drawer route builder retained for the
// review->revision drag path (app.js GETs this route into #board-drawer).
func TestBoardItemPath(t *testing.T) {
	if got := boardItemPath("w1"); got != "/board/items/w1" {
		t.Errorf("boardItemPath = %q, want /board/items/w1", got)
	}
}

// TestBoardStatusMapFromDefinitions pins the work-path mapping derivation:
// unanimous explicit phases win, unset/ambiguous phases fall back to the
// built-in defaults, and a zero config contributes nothing.
func TestBoardStatusMapFromDefinitions(t *testing.T) {
	def := func(status AgentWorkStatusConfig) AgentDefinitionSummary {
		return AgentDefinitionSummary{WorkConfig: &AgentWorkConfig{Status: status}}
	}

	// No definitions -> canonical defaults.
	if got := boardStatusMapFromDefinitions(nil); got != defaultBoardStatusMap() {
		t.Errorf("no definitions = %+v, want defaults", got)
	}

	// A zero work config (WorkConfig present, all phases empty) contributes
	// nothing — the default board must be unaffected.
	zero := []AgentDefinitionSummary{{WorkConfig: &AgentWorkConfig{}}}
	if got := boardStatusMapFromDefinitions(zero); got != defaultBoardStatusMap() {
		t.Errorf("zero config = %+v, want defaults", got)
	}

	// Two agreeing agents resolve the custom statuses; unset phases stay default.
	agreeing := []AgentDefinitionSummary{
		def(AgentWorkStatusConfig{Ready: "todo", InProgress: "doing", Review: "checking"}),
		def(AgentWorkStatusConfig{Ready: "todo", InProgress: "doing", Review: "checking"}),
	}
	want := defaultBoardStatusMap()
	want.Ready = "todo"
	want.InProgress = "doing"
	want.Review = "checking"
	if got := boardStatusMapFromDefinitions(agreeing); got != want {
		t.Errorf("agreeing defs = %+v, want %+v", got, want)
	}

	// Conflicting declarations for one phase keep the default for that phase
	// only (a stray agent cannot hijack the board).
	conflicting := []AgentDefinitionSummary{
		def(AgentWorkStatusConfig{Ready: "todo", Done: "shipped"}),
		def(AgentWorkStatusConfig{Ready: "backlog", Done: "shipped"}),
	}
	want = defaultBoardStatusMap()
	want.Done = "shipped"
	if got := boardStatusMapFromDefinitions(conflicting); got != want {
		t.Errorf("conflicting defs = %+v, want %+v (ambiguous ready keeps default)", got, want)
	}

	// A merge across agents is per-phase: each phase is independent.
	merged := []AgentDefinitionSummary{
		def(AgentWorkStatusConfig{Ready: "todo"}),
		def(AgentWorkStatusConfig{Blocked: "rejected"}),
	}
	want = defaultBoardStatusMap()
	want.Ready = "todo"
	want.Blocked = "rejected"
	if got := boardStatusMapFromDefinitions(merged); got != want {
		t.Errorf("merged defs = %+v, want %+v", got, want)
	}
}

// TestBoardPageEmitsStatusMap pins that the board carries the project's
// work-path status map for app.js to derive drag transitions from.
func TestBoardPageEmitsStatusMap(t *testing.T) {
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{
			{WorkConfig: &AgentWorkConfig{Status: AgentWorkStatusConfig{Ready: "todo", InProgress: "doing"}}},
		},
		workItems: []WorkItem{{CanonicalID: "w1", Type: "Task", Key: "k", Status: "todo"}},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := boardStatusMapAttr(t, rec.Body.String())
	want := defaultBoardStatusMap()
	want.Ready = "todo"
	want.InProgress = "doing"
	if got != want {
		t.Errorf("board status map = %+v, want %+v", got, want)
	}

	// An unconfigured project emits the canonical defaults.
	def := &fakeMemory{workItems: []WorkItem{{CanonicalID: "w1", Status: "ready"}}}
	_, e2 := newBoardEcho(def)
	rec2 := httptest.NewRecorder()
	e2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/board", nil))
	if got := boardStatusMapAttr(t, rec2.Body.String()); got != defaultBoardStatusMap() {
		t.Errorf("default board status map = %+v, want %+v", got, defaultBoardStatusMap())
	}
}

// TestBoardDrawerGatesActionsByCustomStatus pins that the drawer actions are
// selected through the project's work-path mapping, not the literal statuses.
func TestBoardDrawerGatesActionsByCustomStatus(t *testing.T) {
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{
			{WorkConfig: &AgentWorkConfig{Status: AgentWorkStatusConfig{
				Ready: "todo", InProgress: "doing", Review: "checking",
				Revision: "rework", Blocked: "rejected", Done: "shipped",
			}}},
		},
		workItemDetail: &WorkItemDetail{
			Item: &WorkItem{CanonicalID: "w1", Type: "BoardTask", Key: "k", Status: "checking"},
		},
	}
	_, e := newBoardEcho(f)

	// "checking" is the mapped review status -> approve + request changes.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
	body := rec.Body.String()
	for _, want := range []string{`/board/items/w1/approve`, `name="feedback"`} {
		if !strings.Contains(body, want) {
			t.Errorf("custom review status missing %q", want)
		}
	}
	if strings.Contains(body, "/board/items/w1/retry") {
		t.Error("retry must be hidden for a custom-review item")
	}
	// review != done, so reassign + cancel stay available.
	if !strings.Contains(body, "/board/items/w1/cancel") {
		t.Error("cancel must be available for a custom-review item")
	}

	// "rejected" is the mapped blocked status -> retry only.
	f.workItemDetail.Item.Status = "rejected"
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
	body = rec.Body.String()
	if !strings.Contains(body, "/board/items/w1/retry") {
		t.Error("custom blocked status must expose retry")
	}
	if strings.Contains(body, "/board/items/w1/approve") {
		t.Error("approve must be hidden for a custom-blocked item")
	}

	// "shipped" is the mapped done status -> no cancel/reassign.
	f.workItemDetail.Item.Status = "shipped"
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
	body = rec.Body.String()
	if strings.Contains(body, "/board/items/w1/cancel") {
		t.Error("cancel must be hidden for a custom-done item")
	}
	if strings.Contains(body, `name="assignee"`) {
		t.Error("reassign must be hidden for a custom-done item")
	}
}

// TestListAgentDefinitionsWorkConfigFlowsToStatusMap is the cross-boundary
// regression for #1428. It feeds the success envelope the memory server emits
// for GET /agent-definitions (an AgentDefinitionSummaryDTO carrying a custom
// workConfig.status) through the real MemoryClient HTTP unmarshal, then derives
// the board's status map. Unlike TestBoardStatusMapFromDefinitions, which
// hand-builds AgentDefinitionSummary values, this exercises the wire field
// names and the gateway unmarshal path, so it fails if any layer drops
// workConfig instead of silently defaulting.
func TestListAgentDefinitionsWorkConfigFlowsToStatusMap(t *testing.T) {
	const body = `{"success":true,"data":[{"id":"ad-1","projectId":"p1","name":"diane",` +
		`"flowType":"single","visibility":"project","isDefault":false,"enabled":true,` +
		`"toolCount":0,"skills":[],"createdAt":"2026-01-01T00:00:00Z",` +
		`"updatedAt":"2026-01-01T00:00:00Z",` +
		`"workConfig":{"status":{"ready":"todo","inProgress":"doing","review":"checking",` +
		`"revision":"rework","blocked":"rejected","done":"shipped"}}}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/projects/p1/agent-definitions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	defs, err := NewMemoryClient(srv.URL, "p1").ListAgentDefinitions(t.Context())
	if err != nil {
		t.Fatalf("ListAgentDefinitions: %v", err)
	}
	if len(defs) != 1 || defs[0].WorkConfig == nil {
		t.Fatalf("workConfig dropped between server payload and summary DTO: %+v", defs)
	}

	want := boardStatusMap{
		Ready: "todo", InProgress: "doing", Review: "checking",
		Revision: "rework", Blocked: "rejected", Done: "shipped",
	}
	if got := boardStatusMapFromDefinitions(defs); got != want {
		t.Errorf("status map = %+v, want %+v", got, want)
	}
	// The derived map must be serialisable into the data attribute app.js reads.
	if j := want.statusMapJSON(); !strings.Contains(j, `"ready":"todo"`) || !strings.Contains(j, `"done":"shipped"`) {
		t.Errorf("statusMapJSON = %s", j)
	}
}

var errTestBoard = &memoryHTTPError{Status: 503, Message: "down"}
