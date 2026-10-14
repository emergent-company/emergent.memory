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
	e.POST("/board/items/:canonicalId/comments", s.uiBoardComment)
	e.POST("/board/items/:canonicalId/retry", s.uiBoardRetry)
	e.POST("/board/items/:canonicalId/reassign", s.uiBoardReassign)
	e.POST("/board/items/:canonicalId/status", s.uiBoardSetStatus)
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
	// data-canonical-id on the dialog is the #1439 focus-restore contract:
	// object-preview.js reads it from the source element's closest
	// [data-canonical-id] to re-find the refreshed card after the OOB drawer
	// clear. Dropping it silently sends focus to <body>, so pin it here — the
	// hermetic js-dom spec fabricates its own DOM and cannot catch it.
	for _, want := range []string{`data-testid="board-item-dialog"`, `data-canonical-id="w1"`, `/board/items/w1/approve`, `name="feedback"`} {
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
// object summary carries the work item's status-gated action routes AND the
// merged comments thread in the shared #object-preview-actions-slot.
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
		// The merged thread + inline form are reachable from the card-click
		// preview path (no comments seeded, so the muted empty state renders).
		`data-testid="board-preview-comment-empty"`,
		`data-testid="board-preview-comment-form"`,
		`data-testid="board-preview-comment-input"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board preview missing %q", want)
		}
	}
	// The preview emits only its own comment test ids; the drawer's ids stay in
	// the drawer so a Playwright locator is unambiguous when both surfaces are in
	// the DOM (the dialog's content persists in #board-drawer after it closes).
	if strings.Contains(body, `data-testid="board-drawer-comment-`) {
		t.Errorf("preview must not emit the drawer's comment test ids:\n%s", body)
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
		`"visibility":"project","isDefault":false,"enabled":true,` +
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

func TestBoardDeliverables(t *testing.T) {
	run := func(deliverables any) WorkItemRun {
		return WorkItemRun{Summary: map[string]any{"deliverables": deliverables}}
	}

	t.Run("newest-wins dedupe", func(t *testing.T) {
		runs := []WorkItemRun{
			run([]any{
				map[string]any{"type": "Note", "key": "n1"},
				map[string]any{"type": "Report", "key": "r1"},
			}),
			run([]any{
				map[string]any{"type": "Note", "key": "n1"},
				map[string]any{"type": "Note", "key": "n2"},
			}),
		}
		got := boardDeliverables(runs)
		want := []boardDeliverable{
			{Type: "Note", Key: "n1"},
			{Type: "Report", Key: "r1"},
			{Type: "Note", Key: "n2"},
		}
		if len(got) != len(want) {
			t.Fatalf("got %d deliverables, want %d: %+v", len(got), len(want), got)
		}
		for i := range want {
			if got[i].Type != want[i].Type || got[i].Key != want[i].Key {
				t.Errorf("got[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("malformed entries dropped", func(t *testing.T) {
		runs := []WorkItemRun{run([]any{
			"not-a-map",
			map[string]any{"type": "Note"},            // missing key
			map[string]any{"type": 123, "key": "x"},   // non-string type
			map[string]any{"type": "Note", "key": ""}, // empty key
			map[string]any{"type": "Note", "key": "n1"},
			nil,
		})}
		got := boardDeliverables(runs)
		if len(got) != 1 || got[0].Type != "Note" || got[0].Key != "n1" {
			t.Errorf("boardDeliverables = %+v, want one valid entry", got)
		}
	})

	t.Run("missing key tolerated", func(t *testing.T) {
		cases := []WorkItemRun{
			{Summary: nil},
			{Summary: map[string]any{}},
			{Summary: map[string]any{"deliverables": "not-an-array"}},
		}
		if got := boardDeliverables(cases); len(got) != 0 {
			t.Errorf("boardDeliverables = %+v, want empty", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		if got := boardDeliverables(nil); len(got) != 0 {
			t.Errorf("boardDeliverables(nil) = %+v, want empty", got)
		}
		if got := boardDeliverables([]WorkItemRun{}); len(got) != 0 {
			t.Errorf("boardDeliverables([]) = %+v, want empty", got)
		}
	})
}

func TestBoardCompletionSummary(t *testing.T) {
	if got := boardCompletionSummary(nil); got != "" {
		t.Errorf("boardCompletionSummary(nil) = %q, want empty", got)
	}
	runs := []WorkItemRun{
		{Summary: map[string]any{"summary": "newest"}},
		{Summary: map[string]any{"summary": "older"}},
	}
	if got := boardCompletionSummary(runs); got != "newest" {
		t.Errorf("boardCompletionSummary = %q, want newest", got)
	}
	// A non-string summary value is ignored.
	if got := boardCompletionSummary([]WorkItemRun{{Summary: map[string]any{"summary": 42}}}); got != "" {
		t.Errorf("boardCompletionSummary(non-string) = %q, want empty", got)
	}
}

func TestBoardDrawerDeliverables(t *testing.T) {
	t.Run("resolved deliverable links to object", func(t *testing.T) {
		f := &fakeMemory{
			workItemDetail: &WorkItemDetail{
				Item: &WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "done", RunCount: 1, LatestRunStatus: "success"},
				Runs: []WorkItemRun{{ID: "run-1", Status: "success", Summary: map[string]any{
					"summary":      "Shipped the thing",
					"deliverables": []any{map[string]any{"type": "Note", "key": "release-notes"}},
				}}},
			},
			lookupObjects: []GraphObject{{ID: "obj-9", CanonicalID: "canon-9", Type: "Note", Key: "release-notes"}},
		}
		_, e := newBoardEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{
			`data-testid="board-deliverable"`,
			`/objects/canon-9`,
			"Completion note",
			"Shipped the thing",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("resolved drawer missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "board-deliverable-missing") {
			t.Error("resolved deliverable must not render the missing marker")
		}
	})

	t.Run("missing deliverable renders muted, no link", func(t *testing.T) {
		f := &fakeMemory{
			workItemDetail: &WorkItemDetail{
				Item: &WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "done", RunCount: 1, LatestRunStatus: "success"},
				Runs: []WorkItemRun{{ID: "run-1", Status: "success", Summary: map[string]any{
					"deliverables": []any{map[string]any{"type": "Note", "key": "ghost"}},
				}}},
			},
		}
		_, e := newBoardEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-testid="board-deliverable-missing"`) {
			t.Errorf("missing marker absent:\n%s", body)
		}
		if !strings.Contains(body, "not found") {
			t.Errorf("missing hint absent:\n%s", body)
		}
		if strings.Contains(body, `/objects/`) {
			t.Errorf("missing deliverable must not render a link:\n%s", body)
		}
	})

	t.Run("no deliverables shows empty state", func(t *testing.T) {
		f := &fakeMemory{
			workItemDetail: &WorkItemDetail{
				Item: &WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "done", RunCount: 1, LatestRunStatus: "success"},
				Runs: []WorkItemRun{{ID: "run-1", Status: "success", Summary: map[string]any{"summary": "done"}}},
			},
		}
		_, e := newBoardEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "No deliverables declared") {
			t.Errorf("empty state missing:\n%s", rec.Body.String())
		}
	})
}

// TestBoardCommentsMerge verifies the merged-thread helper combines durable
// comments and feedback rounds into one ascending createdAt order, carrying each
// entry's kind/round/actorType fields.
func TestBoardCommentsMerge(t *testing.T) {
	comments := []WorkItemComment{
		{ID: "c1", Author: "alice", Body: "note a", ActorType: "user", CreatedAt: "2026-10-14T10:00:00Z"},
		{ID: "c2", Author: "researcher", Body: "agent note", ActorType: "agent", CreatedAt: "2026-10-14T12:00:00Z"},
	}
	feedback := []WorkItemFeedback{
		{Round: 1, Author: "bob", Text: "round one", CreatedAt: "2026-10-14T11:00:00Z"},
	}

	got := boardComments(comments, feedback)
	require := func(cond bool, msg string) {
		if !cond {
			t.Errorf("%s: got %+v", msg, got)
		}
	}
	require(len(got) == 3, "want 3 merged entries")
	// Ascending by createdAt: note a (10:00), round one (11:00), agent note (12:00).
	require(got[0].Kind == "comment" && got[0].Body == "note a", "first entry wrong")
	require(got[1].Kind == "feedback" && got[1].Round == 1 && got[1].Body == "round one", "second entry wrong")
	require(got[2].Kind == "comment" && got[2].ActorType == "agent" && got[2].Body == "agent note", "third entry wrong")
}

// TestBoardCommentsMergeTieBreak pins the deterministic same-second order: with
// createdAt equal, entries resolve by (kind, round, author, body) — not by the
// order they were appended to the merge (comments first, then feedback). A
// same-second comment sorts before a same-second feedback round
// ("comment" < "feedback"), and feedback rounds order by round number.
func TestBoardCommentsMergeTieBreak(t *testing.T) {
	const at = "2026-10-14T10:00:00Z"
	comments := []WorkItemComment{
		{ID: "c1", Author: "zoe", Body: "zzz comment", ActorType: "user", CreatedAt: at},
		{ID: "c2", Author: "amy", Body: "aaa comment", ActorType: "user", CreatedAt: at},
	}
	feedback := []WorkItemFeedback{
		{Round: 2, Author: "bob", Text: "round two", CreatedAt: at},
		{Round: 1, Author: "bob", Text: "round one", CreatedAt: at},
	}

	got := boardComments(comments, feedback)
	if len(got) != 4 {
		t.Fatalf("want 4 merged entries, got %+v", got)
	}
	// Comments (kind "comment") precede feedback rounds regardless of the merge
	// append order; within a kind, author then body (comments) / round (feedback).
	want := []string{"aaa comment", "zzz comment", "round one", "round two"}
	for i, w := range want {
		if got[i].Body != w {
			t.Errorf("entry %d = %q, want %q (got %+v)", i, got[i].Body, w, got)
		}
	}

	// The order is independent of the input slices' own ordering.
	comments[0], comments[1] = comments[1], comments[0]
	feedback[0], feedback[1] = feedback[1], feedback[0]
	again := boardComments(comments, feedback)
	for i, w := range want {
		if again[i].Body != w {
			t.Errorf("reordered input: entry %d = %q, want %q", i, again[i].Body, w)
		}
	}
}

// TestBoardDrawerComments verifies the merged thread renders comments and
// feedback rounds in ascending order, marking feedback rounds and agent-authored
// comments with their badges.
func TestBoardDrawerComments(t *testing.T) {
	f := &fakeMemory{
		workItemDetail: &WorkItemDetail{
			Item: &WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "done", RunCount: 1, LatestRunStatus: "success"},
			Comments: []WorkItemComment{
				{ID: "c1", Author: "alice", Body: "human note", ActorType: "user", CreatedAt: "2026-10-14T10:00:00Z"},
				{ID: "c2", Author: "researcher", Body: "agent note", ActorType: "agent", CreatedAt: "2026-10-14T12:00:00Z"},
			},
			Feedback: []WorkItemFeedback{
				{Round: 1, Author: "bob", Text: "round one", CreatedAt: "2026-10-14T11:00:00Z"},
			},
			Runs: []WorkItemRun{{ID: "run-1", Status: "success"}},
		},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="board-drawer-comment-row"`,
		">Activity<",
		"human note",
		"agent note",
		"round one",
		"rework round 1",
		"agent",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("comments thread missing %q:\n%s", want, body)
		}
	}
	// The drawer surface uses the drawer-scoped test ids only: the preview's ids
	// must not leak here, or a Playwright locator would match both surfaces.
	if strings.Contains(body, `data-testid="board-preview-comment-`) {
		t.Errorf("drawer must not emit the preview's comment test ids:\n%s", body)
	}
	// Ascending order: human note before round one before agent note.
	hi := strings.Index(body, "human note")
	ri := strings.Index(body, "round one")
	ai := strings.Index(body, "agent note")
	if hi >= ri || ri >= ai {
		t.Errorf("comments not in ascending order: human=%d round=%d agent=%d", hi, ri, ai)
	}
	// The inline form posts to the comment route.
	if !strings.Contains(body, `/board/items/w1/comments`) || !strings.Contains(body, `name="body"`) {
		t.Errorf("comment form missing route/field:\n%s", body)
	}
}

// TestBoardDrawerCommentsEmpty verifies an empty thread renders the muted empty
// state (no comment rows).
func TestBoardDrawerCommentsEmpty(t *testing.T) {
	f := &fakeMemory{
		workItemDetail: &WorkItemDetail{
			Item: &WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "done", RunCount: 1, LatestRunStatus: "success"},
			Runs: []WorkItemRun{{ID: "run-1", Status: "success"}},
		},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="board-drawer-comment-empty"`) || !strings.Contains(body, "No activity yet") {
		t.Errorf("empty comments state missing:\n%s", body)
	}
	if strings.Contains(body, `data-testid="board-drawer-comment-row"`) {
		t.Errorf("empty thread must not render comment rows:\n%s", body)
	}
}

// TestBoardCommentPosts verifies the comment form POST calls AddWorkComment with
// the submitted body and re-renders the board columns.
func TestBoardCommentPosts(t *testing.T) {
	f := &fakeMemory{
		workAction: &WorkItemAction{Item: &WorkItemActionItem{CanonicalID: "w1", Status: "done"}},
		workItems:  []WorkItem{},
	}
	_, e := newBoardEcho(f)
	req := httptest.NewRequest(http.MethodPost, "/board/items/w1/comments", strings.NewReader("body=a%20note"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.lastWorkItemID != "w1" {
		t.Errorf("comment called with %q, want w1", f.lastWorkItemID)
	}
	if f.lastCommentBody != "a note" {
		t.Errorf("comment body = %q, want %q", f.lastCommentBody, "a note")
	}
	if !strings.Contains(rec.Body.String(), `id="board"`) {
		t.Errorf("comment must re-render the board:\n%s", rec.Body.String())
	}
}

// TestBoardAssigneePicker pins the ticket-style assignee dropdown: the drawer
// renders a picker seeded with the project's runtime agents (by exact Name) and
// members (by DisplayName, falling back to Email), each option posting the
// chosen assignee to the existing reassign route. The "Unassigned" entry is
// always present and posts an empty assignee.
func TestBoardAssigneePicker(t *testing.T) {
	f := &fakeMemory{
		scheduledAgents: []ScheduledAgent{
			{ID: "a1", Name: "researcher"},
			{ID: "a2", Name: "summarizer"},
			{ID: "a3", Name: ""}, // unnamed agent dropped
		},
		members: []ProjectMemberDto{
			{ID: "u1", DisplayName: "Ada Lovelace", Email: "ada@example.com", AvatarURL: "/avatars/ada.png"},
			{ID: "u2", Email: "grace@example.com"}, // no display name -> email fallback
		},
		workItemDetail: &WorkItemDetail{
			Item: &WorkItem{CanonicalID: "w1", Type: "BoardTask", Key: "k", Status: "ready"},
		},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, `data-testid="board-assignee-picker"`) {
		t.Errorf("assignee picker missing:\n%s", body)
	}
	// Candidates render with the reassign route and their exact assignee value.
	for _, want := range []string{
		`/board/items/w1/reassign`,
		`name="assignee"`,
		`value="researcher"`,
		`value="summarizer"`,
		`value="Ada Lovelace"`,
		`value="grace@example.com"`,
		`value=""`,                                // Unassigned
		`>agent</p>`,                              // agent sublabel (agents stay distinct from members)
		"won't be picked up",                      // member hint: non-routable consequence
		"Only agents that handle work are woken.", // menu footer (per-agent routability not determinable)
	} {
		if !strings.Contains(body, want) {
			t.Errorf("assignee picker missing %q:\n%s", want, body)
		}
	}
	// The unnamed agent must be dropped (its empty value would collide with
	// Unassigned). Assert directly against the builder, since the rendered
	// "Unassigned" option also carries an empty value.
	cands := buildBoardAssigneeCandidates(f.scheduledAgents, f.members)
	for _, c := range cands {
		if c.Value == "" {
			t.Errorf("candidate with empty value leaked through: %+v", c)
		}
	}
	if len(cands) != 4 { // researcher, summarizer, Ada, grace
		t.Errorf("buildBoardAssigneeCandidates = %d candidates, want 4: %+v", len(cands), cands)
	}
}

// TestBoardStatusControl pins the "Set status" dropdown in the drawer metadata
// row: it offers the work-path vocabulary minus the item's current status, each
// option posting to the set-status route.
func TestBoardStatusControl(t *testing.T) {
	f := &fakeMemory{
		workItemDetail: &WorkItemDetail{
			Item: &WorkItem{CanonicalID: "w1", Type: "BoardTask", Key: "k", Status: "ready"},
		},
	}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/items/w1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, `data-testid="board-status-control"`) {
		t.Errorf("status control missing:\n%s", body)
	}
	for _, want := range []string{
		`/board/items/w1/status`,
		`name="status"`,
		`value="in_progress"`,
		`value="review"`,
		`value="revision"`,
		`value="blocked"`,
		`value="done"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status control missing %q:\n%s", want, body)
		}
	}
	// The current status is excluded.
	if strings.Contains(body, `value="ready"`) {
		t.Errorf("status control must exclude the current status:\n%s", body)
	}
}

// TestBoardSetStatusHandler pins the set-status route: it forwards the posted
// status to the backend and re-renders the board columns.
func TestBoardSetStatusHandler(t *testing.T) {
	f := &fakeMemory{
		workAction: &WorkItemAction{Item: &WorkItemActionItem{CanonicalID: "w1", Status: "done"}},
		workItems:  []WorkItem{},
	}
	_, e := newBoardEcho(f)
	req := httptest.NewRequest(http.MethodPost, "/board/items/w1/status", strings.NewReader("status=done"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.lastWorkItemID != "w1" {
		t.Errorf("set status called with %q, want w1", f.lastWorkItemID)
	}
	if f.lastStatusTo != "done" {
		t.Errorf("set status called with %q, want done", f.lastStatusTo)
	}
	if !strings.Contains(rec.Body.String(), `id="board"`) {
		t.Errorf("set status must re-render the board:\n%s", rec.Body.String())
	}
}

// TestBoardReadOnlyPreviewSkipsCandidateFetch pins that a chat preview (no
// ?actions=board) never fetches the assignee picker candidates: neither
// ListScheduledAgents nor ListMembers may be called.
func TestBoardReadOnlyPreviewSkipsCandidateFetch(t *testing.T) {
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
	if f.listScheduledAgentsCalls != 0 {
		t.Errorf("read-only preview called ListScheduledAgents %d times, want 0", f.listScheduledAgentsCalls)
	}
	if f.listMembersCalls != 0 {
		t.Errorf("read-only preview called ListMembers %d times, want 0", f.listMembersCalls)
	}
	if strings.Contains(rec.Body.String(), "board-assignee-picker") {
		t.Errorf("read-only preview must not render the assignee picker")
	}
}
