package main

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// workBoardStatusMapAttr extracts and decodes the
// #work-board[data-board-status-map] value from a rendered board fragment (templ
// HTML-escapes the JSON quotes).
func workBoardStatusMapAttr(t *testing.T, body string) boardStatusMap {
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

// newWorkBoardEcho wires the work board's routes on a bare echo instance: the
// board page, its live stream and partial, the work-item actions, and the shared
// object preview whose ?actions=board slot is the board's item surface.
func newWorkBoardEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/work/board/partial", s.uiWorkBoardPartial)
	e.GET("/work/board/stream", s.workBoardStream)
	e.GET("/work/:section", s.uiWorkSection)
	e.POST("/work/items/:canonicalId/approve", s.uiWorkApprove)
	e.POST("/work/items/:canonicalId/request-changes", s.uiWorkRequestChanges)
	e.POST("/work/items/:canonicalId/comments", s.uiWorkComment)
	e.POST("/work/items/:canonicalId/retry", s.uiWorkRetry)
	e.POST("/work/items/:canonicalId/cancel", s.uiWorkCancel)
	e.POST("/work/items/:canonicalId/status", s.uiWorkSetStatus)
	e.GET("/objects/:id/preview", s.uiObjectPreviewPartial)
	return s, e
}

// TestWorkBoardPartialRendersTheFrame pins the swap region's identity: the live
// stream and the drawer's action forms both address #work-board, so the region
// must exist even for an empty board (an empty state inside it, not instead of
// it) or the first live item would have nowhere to land.
func TestWorkBoardPartialRendersTheFrame(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "done"}},
		}},
		workItems: []WorkItem{
			{CanonicalID: "w1", Type: "Task", Key: "ship-1", Name: "Ship the thing", Status: "ready"},
		},
	}
	_, e := newWorkBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/work/board/partial", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`id="work-board"`, `data-work-board`, `data-testid="work-card-w1"`, "Ship the thing"} {
		if !strings.Contains(body, want) {
			t.Errorf("board frame missing %q:\n%s", want, body)
		}
	}
	// A background refresh carries the frame only: no out-of-band directives and
	// no stale drawer region (the legacy drawer no longer exists).
	for _, forbidden := range []string{"hx-swap-oob", "board-drawer"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("board partial carries %q:\n%s", forbidden, body)
		}
	}

	// An empty board still renders the region, with its empty state inside it.
	empty := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready"}},
	}}}
	_, e2 := newWorkBoardEcho(empty)
	rec2 := httptest.NewRecorder()
	e2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/work/board/partial", nil))
	if !strings.Contains(rec2.Body.String(), `id="work-board"`) {
		t.Errorf("an empty board rendered no swap region:\n%s", rec2.Body.String())
	}
}

// TestWorkBoardPartialEmitsStatusMap pins that the frame carries the project's
// work-path status map — that attribute is where app.js reads the drag
// transitions from, so a custom-mapped project would silently fall back to the
// built-in statuses without it (the legacy board's page-level attribute was the
// only source before).
func TestWorkBoardPartialEmitsStatusMap(t *testing.T) {
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{
			{WorkConfig: &AgentWorkConfig{Status: AgentWorkStatusConfig{Ready: "todo", InProgress: "doing"}}},
		},
		workItems: []WorkItem{{CanonicalID: "w1", Type: "Task", Key: "k", Status: "todo"}},
	}
	_, e := newWorkBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/work/board/partial", nil))
	got := workBoardStatusMapAttr(t, rec.Body.String())
	want := defaultBoardStatusMap()
	want.Ready = "todo"
	want.InProgress = "doing"
	if got != want {
		t.Errorf("board status map = %+v, want %+v", got, want)
	}

	// An unconfigured project emits the canonical defaults.
	def := &fakeMemory{workItems: []WorkItem{{CanonicalID: "w1", Status: "ready"}}}
	_, e2 := newWorkBoardEcho(def)
	rec2 := httptest.NewRecorder()
	e2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/work/board/partial", nil))
	if got := workBoardStatusMapAttr(t, rec2.Body.String()); got != defaultBoardStatusMap() {
		t.Errorf("default board status map = %+v, want %+v", got, defaultBoardStatusMap())
	}
}

// TestWorkBoardHealthReadout pins the circuit-breaker banner that moved off the
// retired board page: an unhealthy reaction agent is listed, a healthy one is
// not, and the region renders even when every agent is fine (so a live frame can
// clear a stale alert).
func TestWorkBoardHealthReadout(t *testing.T) {
	agents := []ScheduledAgent{
		{Name: "researcher", TriggerType: "reaction", Enabled: true, ConsecutiveFailures: 3},
		{Name: "coder", TriggerType: "schedule", Enabled: true, ConsecutiveFailures: 0},
	}
	body := renderHTML(t, workBoardHealthRegion(boardUnhealthyAgents(agents)))
	if !strings.Contains(body, `data-testid="work-board-health"`) {
		t.Errorf("health region missing:\n%s", body)
	}
	if !strings.Contains(body, "board-agent-health") || !strings.Contains(body, "researcher") {
		t.Errorf("unhealthy agent not listed:\n%s", body)
	}
	if strings.Contains(body, "coder") {
		t.Errorf("healthy schedule agent must not be listed as unhealthy:\n%s", body)
	}

	// All healthy: the region exists, the alert does not.
	healthy := renderHTML(t, workBoardHealthRegion(nil))
	if !strings.Contains(healthy, `id="work-board-health"`) {
		t.Errorf("healthy readout has no swap region:\n%s", healthy)
	}
	if strings.Contains(healthy, "board-agent-health") {
		t.Errorf("healthy readout rendered the warning:\n%s", healthy)
	}
}

// TestWorkBoardCountReadout pins the "N of M types" readout as its own swap
// region: the live stream refreshes it without touching the matrices.
func TestWorkBoardCountReadout(t *testing.T) {
	data := workData{
		BoardTypes: []CompiledType{{Name: "Bug"}, {Name: "Task"}, {Name: "Note"}},
		Matrices:   []workMatrix{{Type: "Bug"}, {Type: "Task"}},
	}
	body := renderHTML(t, workBoardCount(data))
	if !strings.Contains(body, `id="work-board-count"`) {
		t.Errorf("count readout is not a swap region:\n%s", body)
	}
	if !strings.Contains(body, "2") || !strings.Contains(body, "3 types") {
		t.Errorf("count readout = %q, want \"2 of 3 types\"", body)
	}
}

// TestBoardViewQuery pins the board's view-state reduction. The type filter is
// the only thing that has to survive a re-render, and it must survive intact:
// dropping it widens a filtered board to every type behind a control that still
// shows the subset, and carrying unrelated params would leak the rest of the
// request into every action URL.
//
// The values are also resolved against the board's own types (#878), so the
// published query and the rendered matrices come from one set: a repeat, an
// unknown type and a type the board does not enable are all dropped here rather
// than published and then ignored by the renderer.
func TestBoardViewQuery(t *testing.T) {
	boardTypes := []CompiledType{{Name: "Task", BoardEnabled: true}, {Name: "Bug", BoardEnabled: true}}
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"filter only", "filter=1", "filter=1"},
		{"types only", "type=Task", "type=Task"},
		{"filter and types", "filter=1&type=Task&type=Bug", "filter=1&type=Task&type=Bug"},
		{"blank type dropped", "filter=1&type=&type=Task", "filter=1&type=Task"},
		{"repeated type dropped", "filter=1&type=Task&type=Task", "filter=1&type=Task"},
		{"unknown type dropped", "filter=1&type=Task&type=Nope", "filter=1&type=Task"},
		{"non-board type dropped", "filter=1&type=Task&type=Note", "filter=1&type=Task"},
		{"unknown type alone leaves the filter", "filter=1&type=Nope", "filter=1"},
		{"unrelated params dropped", "filter=1&type=Task&section=board&agent=a1&q=x", "filter=1&type=Task"},
		{"filter flag must be exactly 1", "filter=0&type=Task", "type=Task"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := url.ParseQuery(tt.in)
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", tt.in, err)
			}
			if got := boardViewQuery(in, boardTypes).Encode(); got != tt.want {
				t.Errorf("boardViewQuery(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestBoardViewQueryWithoutSchema pins the preview's fail-open half: when the
// compiled schema is unavailable, boardViewQuery cannot tell a real type from a
// stale one, so it must still forward the selection it cannot validate (a
// filtered board must not widen on a schema hiccup) while still dropping the
// values that cannot mean anything.
func TestBoardViewQueryWithoutSchema(t *testing.T) {
	in, err := url.ParseQuery("filter=1&type=Task&type=Task&type=&type=Bug")
	if err != nil {
		t.Fatal(err)
	}
	if got := boardViewQuery(in, nil).Encode(); got != "filter=1&type=Task&type=Bug" {
		t.Errorf("boardViewQuery without schema = %q, want filter=1&type=Task&type=Bug", got)
	}
}

// TestWorkBoardStreamURLCarriesTheView pins the stream's address. hx-sse opens a
// bare EventSource, so the query cannot be attached by the client afterwards: if
// it is not in the rendered URL the catch-up frame renders the all-types board.
func TestWorkBoardStreamURLCarriesTheView(t *testing.T) {
	if got := workBoardStreamURL(workData{}); got != "/work/board/stream" {
		t.Errorf("unfiltered stream URL = %q, want /work/board/stream", got)
	}
	data := workData{Query: url.Values{"filter": {"1"}, "type": {"Task"}}}
	if got := workBoardStreamURL(data); got != "/work/board/stream?filter=1&type=Task" {
		t.Errorf("filtered stream URL = %q, want /work/board/stream?filter=1&type=Task", got)
	}
}

// TestWorkActionPathCarriesTheView pins the action routes. An action's response
// re-renders the board from the request's own query, so the filter has to ride on
// the action URL — the POST body carries the form's fields, not the board's view.
func TestWorkActionPathCarriesTheView(t *testing.T) {
	if got := workActionPath("w1", "approve", nil); got != "/work/items/w1/approve" {
		t.Errorf("unfiltered action path = %q, want /work/items/w1/approve", got)
	}
	view := url.Values{"filter": {"1"}, "type": {"Task"}}
	if got := workActionPath("w1", "approve", view); got != "/work/items/w1/approve?filter=1&type=Task" {
		t.Errorf("filtered action path = %q, want /work/items/w1/approve?filter=1&type=Task", got)
	}
}

// TestWorkBoardViewConnectsTheStreamWithItsView pins both ends of the wiring: the
// rendered board publishes its view state as data-board-query (which app.js
// appends to the refresh and action requests) and opens the live stream with the
// same query.
func TestWorkBoardViewConnectsTheStreamWithItsView(t *testing.T) {
	data := workData{
		BoardTypes:     []CompiledType{{Name: "Task", BoardEnabled: true}},
		SelectedTypes:  []string{"Task"},
		FilterExplicit: true,
		Query:          url.Values{"filter": {"1"}, "type": {"Task"}},
		TypeCounts:     map[string]int{"Task": 1},
		StatusMap:      defaultBoardStatusMap(),
	}
	body := renderHTML(t, workBoardView(data))
	for _, want := range []string{
		`data-board-query="filter=1&amp;type=Task"`,
		`hx-sse:connect="/work/board/stream?filter=1&amp;type=Task"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("filtered board missing %q:\n%s", want, body)
		}
	}

	unfiltered := renderHTML(t, workBoardView(workData{
		BoardTypes:    []CompiledType{{Name: "Task", BoardEnabled: true}},
		SelectedTypes: []string{"Task"},
		TypeCounts:    map[string]int{"Task": 1},
		StatusMap:     defaultBoardStatusMap(),
	}))
	if !strings.Contains(unfiltered, `hx-sse:connect="/work/board/stream"`) {
		t.Errorf("unfiltered board must connect without a query:\n%s", unfiltered)
	}
	if !strings.Contains(unfiltered, `data-board-query=""`) {
		t.Errorf("unfiltered board must publish an empty view:\n%s", unfiltered)
	}
}

// TestWorkActionRefreshKeepsTheTypeFilter is the regression for the review
// finding: the action POST answers by re-rendering the board from its own query,
// so an approve on a filtered board must come back still showing only the
// selected type. Before, the response rendered every type while the filter
// control — outside the swap region — still showed the subset.
func TestWorkActionRefreshKeepsTheTypeFilter(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "review", "done"}},
			{Name: "Bug", BoardEnabled: true, AllowedStatuses: []string{"open", "closed"}},
		}},
		workItems: []WorkItem{
			{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "review"},
			{CanonicalID: "b1", Type: "Bug", Key: "crash-1", Status: "open"},
		},
		workAction: &WorkItemAction{Item: &WorkItemActionItem{CanonicalID: "w1", Status: "done"}},
	}
	_, e := newWorkBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/work/items/w1/approve?filter=1&type=Task", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="work-type-Task"`) {
		t.Errorf("filtered action refresh dropped the selected type:\n%s", body)
	}
	if strings.Contains(body, `data-testid="work-type-Bug"`) {
		t.Errorf("filtered action refresh widened the board to every type:\n%s", body)
	}
	if !strings.Contains(body, `data-board-query="filter=1&amp;type=Task"`) {
		t.Errorf("filtered action refresh lost the view state:\n%s", body)
	}
}

// TestWorkBoardPageUsesFullBleedContainer pins the page shell the legacy board's
// layout test also pinned: a Kanban board is a horizontal surface, so it takes the
// full-bleed container (go-daisy ContainerFull = max-w-none) rather than the capped
// reading width. The width is applied from a runtime value (string(size)), so the
// compiled-stylesheet side is covered by the @source inline("max-w-none")
// safelist in webui/css/app.css.
func TestWorkBoardPageUsesFullBleedContainer(t *testing.T) {
	body := renderWorkBoardPage(t, workBoardPageMemory())

	if !strings.Contains(body, "max-w-none") {
		t.Errorf("work board page does not render the full-bleed container width (max-w-none):\n%s", body)
	}
	if strings.Contains(body, "max-w-6xl") {
		t.Error("work board page still renders the capped container width (max-w-6xl)")
	}
}

// TestWorkBoardPageWiresLiveStream pins the live-refresh wiring the retired
// board's layout test covered: the htmx hx-sse extension is loaded and the page
// carries a connection anchor pointed at the work stream. The client-side poller
// (/js/board.js) must stay retired — it listened for entity.created/updated and
// therefore never saw the entity.status_changed/entity.assigned events the work
// path emits.
func TestWorkBoardPageWiresLiveStream(t *testing.T) {
	body := renderWorkBoardPage(t, workBoardPageMemory())

	for _, want := range []string{
		"/js/hx-sse.js",
		`hx-sse:connect="/work/board/stream"`,
		`id="work-board-live"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("work board page is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/js/board.js") {
		t.Error("work board page still loads the retired client-side poller (/js/board.js)")
	}
}

// TestWorkBoardLanesShareTheRow pins the narrow-screen lane row: a column-flow
// grid whose lanes take an equal share of the row, instead of a wrapping
// auto-fill grid (which leaves a trailing empty column or strands a lone lane on
// a second row). The desktop status × execution matrix divides width by
// grid-template-columns, so this covers the fallback.
func TestWorkBoardLanesShareTheRow(t *testing.T) {
	body := renderWorkBoardPage(t, workBoardPageMemory())

	for _, want := range []string{"grid-flow-col", "auto-cols-[minmax(", "memory-board__lane"} {
		if !strings.Contains(body, want) {
			t.Errorf("work board lanes missing width-sharing member %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "auto-fill") {
		t.Error("work board lanes still use a wrapping auto-fill grid")
	}
}

// TestWorkBoardDropRingIsDrawnInsideTheLane pins the drop highlight. Lanes live in
// an overflow-x-auto row with no top padding, so an external outline (a positive
// offset) has its top edge clipped by the scroll container.
func TestWorkBoardDropRingIsDrawnInsideTheLane(t *testing.T) {
	css, err := os.ReadFile("webui/css/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	s := string(css)
	for _, selector := range []string{".memory-board__lane[data-drop-eligible]", ".memory-board__lane--drop"} {
		block := cssRuleBlock(t, s, selector)
		if !strings.Contains(block, "outline-offset: -2px") {
			t.Errorf("%s must draw its ring inside the lane (outline-offset: -2px); got:\n%s", selector, block)
		}
	}
	// The active target has to outrank the eligibility ring, which is a
	// class+attribute selector (0,2,0): with a single class (0,1,0) the hovered
	// lane kept the dashed ring and only the background tint distinguished it.
	if !strings.Contains(s, ".memory-board__lane.memory-board__lane--drop") {
		t.Error("the active drop-target rule must be two-class specific (.memory-board__lane.memory-board__lane--drop) to override the eligibility ring")
	}
	// ...and it has to come after the eligibility rule: both are (0,2,0), so only
	// source order decides which ring the hovered lane gets (#2464).
	eligibilityAt := strings.Index(s, ".memory-board__lane[data-drop-eligible] {")
	activeAt := strings.Index(s, ".memory-board__lane.memory-board__lane--drop {")
	if eligibilityAt < 0 || activeAt < 0 {
		t.Fatalf("both drop-ring rules must be present in app.css: eligibility at %d, active at %d", eligibilityAt, activeAt)
	}
	if activeAt < eligibilityAt {
		t.Error("the active drop-target rule must come after the eligibility rule: both are (0,2,0), so only source order decides")
	}
}

// TestWorkBoardLaneCarriesTheDropAffordanceHook pins both halves of the
// eligibility ring: the selector needs the class, and the lane element needs to
// carry it. A rename on either side leaves the drag affordance invisible with no
// error anywhere.
func TestWorkBoardLaneCarriesTheDropAffordanceHook(t *testing.T) {
	body := renderWorkBoardPage(t, workBoardPageMemory())

	lane := openingTagBefore(t, body, `data-board-column="ready"`)
	if !strings.Contains(lane, "memory-board__lane") {
		t.Errorf("board lane element does not carry the memory-board__lane hook, so .memory-board__lane[data-drop-eligible] never matches:\n%s", lane)
	}

	css, err := os.ReadFile("webui/css/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	if !strings.Contains(string(css), ".memory-board__lane[data-drop-eligible]") {
		t.Error("app.css no longer styles the eligibility ring through .memory-board__lane[data-drop-eligible]")
	}
}

// workBoardPageMemory is a one-type board holding one item, so the page renders a
// real matrix rather than the no-board-types empty state.
func workBoardPageMemory() *fakeMemory {
	return &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "in_progress", "done"}},
		}},
		workItems: []WorkItem{
			{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "ready"},
		},
	}
}

// renderWorkBoardPage renders the Work → Board page through its route.
func renderWorkBoardPage(t *testing.T, f MemoryBackend) string {
	t.Helper()
	_, e := newWorkBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/work/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /work/board status = %d, body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// openingTagBefore isolates the opening tag that carries attribute: it starts at
// the last `<` before the attribute, so an assertion cannot pass on a class that
// belongs to a different element earlier in the page.
func openingTagBefore(t *testing.T, s, attribute string) string {
	t.Helper()
	end := strings.Index(s, attribute)
	if end < 0 {
		t.Fatalf("attribute %q not found in rendered board", attribute)
	}
	start := strings.LastIndex(s[:end], "<")
	if start < 0 {
		t.Fatalf("no tag start before %q", attribute)
	}
	tagEnd := strings.Index(s[end:], ">")
	if tagEnd < 0 {
		t.Fatalf("unterminated tag at %q", attribute)
	}
	return s[start : end+tagEnd+1]
}

// TestWorkPreviewActionButtonsGateByStatus pins the status-gated work-item
// actions in the preview's action slot: review exposes approve + request changes
// (feedback is required, so it is a form) and hides retry; blocked exposes retry;
// done hides cancel.
func TestWorkPreviewActionButtonsGateByStatus(t *testing.T) {
	review := renderHTML(t, workPreviewActionButtons(&WorkItem{CanonicalID: "w1", Status: "review"}, defaultBoardStatusMap(), nil))
	for _, want := range []string{`/work/items/w1/approve`, `/work/items/w1/request-changes`, `name="feedback"`, `hx-target="#work-board"`, `hx-swap="outerMorph"`} {
		if !strings.Contains(review, want) {
			t.Errorf("review actions missing %q:\n%s", want, review)
		}
	}
	if strings.Contains(review, "/work/items/w1/retry") {
		t.Error("retry must be hidden for a review item")
	}

	blocked := renderHTML(t, workPreviewActionButtons(&WorkItem{CanonicalID: "w1", Status: "blocked"}, defaultBoardStatusMap(), nil))
	if !strings.Contains(blocked, "/work/items/w1/retry") {
		t.Errorf("blocked item must expose retry:\n%s", blocked)
	}

	done := renderHTML(t, workPreviewActionButtons(&WorkItem{CanonicalID: "w1", Status: "done"}, defaultBoardStatusMap(), nil))
	if strings.Contains(done, "/work/items/w1/cancel") || strings.Contains(done, "/work/items/w1/approve") {
		t.Errorf("done item must expose neither cancel nor approve:\n%s", done)
	}
}

// TestWorkPreviewActionButtonsUseCustomStatusMap pins that the actions are
// selected through the project's configured work-path mapping, not the literal
// status names.
func TestWorkPreviewActionButtonsUseCustomStatusMap(t *testing.T) {
	sm := boardStatusMap{Ready: "todo", InProgress: "doing", Review: "checking", Revision: "rework", Blocked: "rejected", Done: "shipped"}

	review := renderHTML(t, workPreviewActionButtons(&WorkItem{CanonicalID: "w1", Status: "checking"}, sm, nil))
	for _, want := range []string{`/work/items/w1/approve`, `name="feedback"`} {
		if !strings.Contains(review, want) {
			t.Errorf("custom review status missing %q:\n%s", want, review)
		}
	}
	if strings.Contains(review, "/work/items/w1/retry") {
		t.Error("retry must be hidden for a custom-review item")
	}

	blocked := renderHTML(t, workPreviewActionButtons(&WorkItem{CanonicalID: "w1", Status: "rejected"}, sm, nil))
	if !strings.Contains(blocked, "/work/items/w1/retry") || strings.Contains(blocked, "/work/items/w1/approve") {
		t.Errorf("custom blocked status must expose retry only:\n%s", blocked)
	}

	done := renderHTML(t, workPreviewActionButtons(&WorkItem{CanonicalID: "w1", Status: "shipped"}, sm, nil))
	if strings.Contains(done, "/work/items/w1/cancel") {
		t.Errorf("cancel must be hidden for a custom-done item:\n%s", done)
	}
}

// TestWorkPreviewBoardSlotRendersActivity pins the merged thread in the preview
// slot: comments and rework rounds ascend, each kind carries its badge, and the
// inline form posts to the work-item comment route.
func TestWorkPreviewBoardSlotRendersActivity(t *testing.T) {
	body := renderHTML(t, workPreviewBoardSlot(
		&WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "done"},
		defaultBoardStatusMap(),
		[]WorkItemComment{
			{ID: "c1", Author: "alice", Body: "human note", ActorType: "user", CreatedAt: "2026-10-14T10:00:00Z"},
			{ID: "c2", Author: "researcher", Body: "agent note", ActorType: "agent", CreatedAt: "2026-10-14T12:00:00Z"},
		},
		[]WorkItemFeedback{
			{Round: 1, Author: "bob", Text: "round one", CreatedAt: "2026-10-14T11:00:00Z"},
		},
		nil,
		nil,
		nil,
	))
	for _, want := range []string{
		`data-testid="board-preview-comment-row"`,
		">Activity<",
		"human note",
		"agent note",
		"round one",
		"rework round 1",
		"agent",
		`/work/items/w1/comments`,
		`name="body"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("preview activity missing %q:\n%s", want, body)
		}
	}
	// Ascending order: human note before round one before agent note.
	hi := strings.Index(body, "human note")
	ri := strings.Index(body, "round one")
	ai := strings.Index(body, "agent note")
	if hi >= ri || ri >= ai {
		t.Errorf("comments not in ascending order: human=%d round=%d agent=%d", hi, ri, ai)
	}
	// An item with no deliverables still states the section (object-driven-agent-work):
	// the preview is the human's only view of what a run produced, so "nothing
	// declared" must read as an explicit empty state rather than a missing feature.
	if !strings.Contains(body, "Deliverables") {
		t.Errorf("empty deliverables omitted the section:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="board-deliverable-empty"`) || !strings.Contains(body, "No deliverables declared") {
		t.Errorf("empty deliverables missing the explicit empty state:\n%s", body)
	}
}

// TestWorkPreviewBoardSlotRendersDeliverables pins the spec's deliverable
// rendering on the surviving surface: a resolved deliverable links to its object,
// an unresolved one renders muted with a "not found" hint and no link.
func TestWorkPreviewBoardSlotRendersDeliverables(t *testing.T) {
	item := &WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "done"}
	resolved := renderHTML(t, workPreviewBoardSlot(item, defaultBoardStatusMap(), nil, nil, nil,
		[]boardDeliverable{{Type: "Note", Key: "release-notes", ObjectID: "canon-9", Resolved: true}}, nil))
	for _, want := range []string{"Deliverables", `data-testid="board-deliverable"`, `href="/objects/canon-9/wiki"`, "release-notes"} {
		if !strings.Contains(resolved, want) {
			t.Errorf("resolved deliverable missing %q:\n%s", want, resolved)
		}
	}
	if strings.Contains(resolved, "board-deliverable-missing") {
		t.Error("resolved deliverable must not render the missing marker")
	}

	missing := renderHTML(t, workPreviewBoardSlot(item, defaultBoardStatusMap(), nil, nil, nil,
		[]boardDeliverable{{Type: "Note", Key: "ghost"}}, nil))
	if !strings.Contains(missing, `data-testid="board-deliverable-missing"`) || !strings.Contains(missing, "not found") {
		t.Errorf("unresolved deliverable not marked:\n%s", missing)
	}
	if strings.Contains(missing, `/objects/`) {
		t.Errorf("unresolved deliverable must not render a link:\n%s", missing)
	}
}

// TestObjectPreviewBoardActionsSlot pins the ?actions=board extension: the shared
// object preview carries the work item's status-gated action routes and the
// merged comments thread in #object-preview-actions-slot.
func TestObjectPreviewBoardActionsSlot(t *testing.T) {
	f := &fakeMemory{
		objects: []GraphObject{{ID: "w1", Type: "BoardTask", Key: "k", Status: "review"}},
		workItemDetail: &WorkItemDetail{
			Item: &WorkItem{CanonicalID: "w1", Type: "BoardTask", Key: "k", Status: "review"},
			// The run history rides on the same detail payload; the board slot must
			// render it, not just consume its deliverables.
			Runs: []WorkItemRun{{
				ID:          "run-1",
				AgentName:   "coding",
				Status:      "completed",
				StartedAt:   "2026-11-18T10:00:00Z",
				CompletedAt: strPtr("2026-11-18T10:01:00Z"),
				Summary:     map[string]any{"summary": "implemented the change"},
			}},
		},
	}
	_, e := newWorkBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/w1/preview?actions=board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="object-preview-actions-slot"`,
		`/work/items/w1/approve`,
		`/work/items/w1/request-changes`,
		// The run history reaches the preview through the detail payload.
		"Work runs",
		`data-testid="board-run-row"`,
		"coding",
		`href="/runs/run-1"`,
		"implemented the change",
		// The merged thread + inline form are reachable from the card-click
		// preview path (no comments seeded, so the muted empty state renders).
		`data-testid="board-preview-comment-empty"`,
		`data-testid="board-preview-comment-form"`,
		`data-testid="board-preview-comment-input"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board preview missing %q:\n%s", want, body)
		}
	}
}

// TestObjectPreviewBoardActionsSlotCarriesTheView pins the drawer's half of the
// board's view state (#878): renderObjectPreview hands boardViewQuery to the
// action slot, which is what makes an action taken from a filtered board return
// to that board still filtered. The unfiltered case above cannot catch a dropped
// argument — it expects a bare path — so the filtered case is the one that fails
// if the query stops being passed.
func TestObjectPreviewBoardActionsSlotCarriesTheView(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "BoardTask", BoardEnabled: true, AllowedStatuses: []string{"review", "done", "cancelled"}},
		}},
		objects: []GraphObject{{ID: "w1", Type: "BoardTask", Key: "k", Status: "review"}},
		workItemDetail: &WorkItemDetail{
			Item: &WorkItem{CanonicalID: "w1", Type: "BoardTask", Key: "k", Status: "review"},
		},
	}
	_, e := newWorkBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/w1/preview?actions=board&filter=1&type=BoardTask", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`/work/items/w1/approve?filter=1&amp;type=BoardTask`,
		`/work/items/w1/request-changes?filter=1&amp;type=BoardTask`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board preview action slot lost the board's view state, missing %q:\n%s", want, body)
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
	_, e := newWorkBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/w1/preview", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="object-preview-actions-slot"`) {
		t.Errorf("preview missing action slot:\n%s", body)
	}
	if strings.Contains(body, "/work/items/") {
		t.Errorf("read-only preview must not render board actions:\n%s", body)
	}
}

// TestWorkApprovePostsAndRefreshes pins the action route's contract: the POST
// reaches the memory action and answers with the refreshed board frame.
func TestWorkApprovePostsAndRefreshes(t *testing.T) {
	f := &fakeMemory{
		workAction: &WorkItemAction{Item: &WorkItemActionItem{CanonicalID: "w1", Status: "done"}},
		workItems:  []WorkItem{},
	}
	_, e := newWorkBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/work/items/w1/approve", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.lastWorkItemID != "w1" {
		t.Errorf("approve called with %q, want w1", f.lastWorkItemID)
	}
	if !strings.Contains(rec.Body.String(), `id="work-board"`) {
		t.Errorf("action must re-render the board frame:\n%s", rec.Body.String())
	}
}

// TestWorkCommentPosts verifies the comment form POST calls AddWorkComment with
// the submitted body and re-renders the board frame.
func TestWorkCommentPosts(t *testing.T) {
	f := &fakeMemory{
		workAction: &WorkItemAction{Item: &WorkItemActionItem{CanonicalID: "w1", Status: "done"}},
		workItems:  []WorkItem{},
	}
	_, e := newWorkBoardEcho(f)
	req := httptest.NewRequest(http.MethodPost, "/work/items/w1/comments", strings.NewReader("body=a%20note"))
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
	if !strings.Contains(rec.Body.String(), `id="work-board"`) {
		t.Errorf("comment must re-render the board frame:\n%s", rec.Body.String())
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

// TestBoardDeliverables pins the defensive decode of a run summary's declared
// deliverables (the preview's Deliverables section is the surviving surface).
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

// TestResolveBoardDeliverables pins the resolution pass: a deliverable whose
// (type, key) resolves links to the object, an unresolved one is kept (rendered
// muted) rather than dropped, and a lookup failure degrades that one row.
func TestResolveBoardDeliverables(t *testing.T) {
	// fakeMemory's lookup always answers with its first object, which is what a
	// permissively-wired backend must not be trusted to model here: the point of
	// the pass is that a declared key that does NOT exist stays unresolved. This
	// fake answers per (type, key).
	f := &keyLookupMemory{fakeMemory: &fakeMemory{}, objects: map[string]GraphObject{
		"Note\x00release-notes": {ID: "obj-9", CanonicalID: "canon-9", Type: "Note", Key: "release-notes"},
	}}
	s := &Server{cfg: Config{}, memory: f}
	runs := []WorkItemRun{
		{Summary: map[string]any{"deliverables": []any{map[string]any{"type": "Note", "key": "release-notes"}}}},
		{Summary: map[string]any{"deliverables": []any{map[string]any{"type": "Note", "key": "ghost"}}}},
	}
	got := s.resolveBoardDeliverables(t.Context(), runs)
	if len(got) != 2 {
		t.Fatalf("resolved %d deliverables, want 2 (an unresolved one is kept): %+v", len(got), got)
	}
	if !got[0].Resolved || got[0].ObjectID != "canon-9" {
		t.Errorf("deliverable 0 = %+v, want resolved to canon-9", got[0])
	}
	if got[1].Resolved {
		t.Errorf("deliverable 1 = %+v, want unresolved", got[1])
	}
}

// keyLookupMemory is a fakeMemory whose key lookup answers per (type, key): the
// embedded fake always returns its first object whatever key is asked for, so it
// cannot model a declared deliverable that does not exist.
type keyLookupMemory struct {
	*fakeMemory
	objects map[string]GraphObject
}

func (m *keyLookupMemory) LookupGraphObjectByKey(_ context.Context, typeFilter, key string) (*GraphObject, error) {
	if obj, ok := m.objects[typeFilter+"\x00"+key]; ok {
		return &obj, nil
	}
	return nil, nil
}
