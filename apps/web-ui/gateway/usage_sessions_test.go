package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- MemoryClient run-log methods ---

func TestGetProjectUsageByAgent(t *testing.T) {
	var gotPath, gotSince, gotUntil string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSince = r.URL.Query().Get("since")
		gotUntil = r.URL.Query().Get("until")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"note":"Costs are estimates.","runCount":19,"data":[
			{"agentId":"a1","agentName":"coding","runCount":17,"totalText":1000,"totalOutput":200,"totalCached":50,"estimatedCostUsd":0.92},
			{"agentId":"","agentName":"","runCount":0,"totalText":20,"estimatedCostUsd":0.02}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	got, err := m.GetProjectUsageByAgent(context.Background(), since, until)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/projects/proj-1/usage/by-agent" {
		t.Errorf("path = %q", gotPath)
	}
	if gotSince != "2026-11-01T00:00:00Z" || gotUntil != "2026-11-15T00:00:00Z" {
		t.Errorf("since/until = %q / %q", gotSince, gotUntil)
	}
	if got.RunCount != 19 {
		t.Errorf("runCount = %d, want 19", got.RunCount)
	}
	if len(got.Data) != 2 {
		t.Fatalf("got %d rows, want 2", len(got.Data))
	}
	if r := got.Data[0]; r.AgentID != "a1" || r.AgentName != "coding" || r.RunCount != 17 || r.TokenCount() != 1200 || r.EstimatedCostUSD != 0.92 {
		t.Errorf("row0 = %+v", r)
	}
	if !got.Data[0].Attributed() {
		t.Error("row0 must be attributed")
	}
	if r := got.Data[1]; r.Attributed() {
		t.Error("row1 is the unattributed bucket")
	}
}

func TestGetProjectUsageRuns(t *testing.T) {
	var gotPath, gotAgent, gotLimit, gotOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAgent = r.URL.Query().Get("agentId")
		gotLimit = r.URL.Query().Get("limit")
		gotOffset = r.URL.Query().Get("offset")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"note":"Costs are estimates.","total":120,"limit":50,"offset":50,"data":[
			{"runId":"r1","agentId":"a1","agentName":"coding","status":"completed","model":"deepseek-v4-pro","startedAt":"2026-11-14T09:30:00Z","lastEventAt":"2026-11-14T09:31:00Z","durationMs":420000,"eventCount":4,"totalText":1200,"totalOutput":300,"totalCached":100,"estimatedCostUsd":0.8123}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	got, err := m.GetProjectUsageRuns(context.Background(), since, until, "a1", 50, 50)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/projects/proj-1/usage/runs" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAgent != "a1" || gotLimit != "50" || gotOffset != "50" {
		t.Errorf("agent/limit/offset = %q / %q / %q", gotAgent, gotLimit, gotOffset)
	}
	if got.Total != 120 || got.Limit != 50 || got.Offset != 50 {
		t.Errorf("paging = %+v", got)
	}
	if len(got.Data) != 1 {
		t.Fatalf("got %d rows, want 1", len(got.Data))
	}
	r := got.Data[0]
	if r.RunID != "r1" || r.AgentName != "coding" || r.Status != "completed" || r.Model == nil || *r.Model != "deepseek-v4-pro" {
		t.Errorf("row = %+v", r)
	}
	if r.TokenCount() != 1500 {
		t.Errorf("tokens = %d, want 1500", r.TokenCount())
	}
	if r.DurationMs == nil || *r.DurationMs != 420000 {
		t.Errorf("duration = %v", r.DurationMs)
	}
}

// TestGetProjectUsageRunsOmitsEmptyFilter pins the query shape: with no agent
// filter and the first page, only the range is sent.
func TestGetProjectUsageRunsOmitsEmptyFilter(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	if _, err := m.GetProjectUsageRuns(context.Background(), time.Time{}, time.Time{}, "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty", gotQuery)
	}
}

// --- paging parse ---

func TestUsageRunOffset(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{"", 0},
		{"0", 0},
		{"50", 50},
		{" 50 ", 50},
		{"-5", 0},
		{"abc", 0},
		{"1.5", 0},
	} {
		if got := usageRunOffset(tc.raw); got != tc.want {
			t.Errorf("usageRunOffset(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestUsageTriggerLabel(t *testing.T) {
	for raw, want := range map[string]string{
		"chat":            "chat",
		"schedule":        "schedule",
		"webhook:abc-123": "webhook",
		"a2a":             "a2a",
		"":                "",
	} {
		if got := usageTriggerLabel(raw); got != want {
			t.Errorf("usageTriggerLabel(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestUsageSessionsHref(t *testing.T) {
	for _, tc := range []struct {
		days   int
		agent  string
		offset int
		want   string
	}{
		{30, "", 0, "/usage/sessions?days=30"},
		{7, "a1", 0, "/usage/sessions?agent=a1&days=7"},
		{90, "a1", 100, "/usage/sessions?agent=a1&days=90&offset=100"},
	} {
		if got := usageSessionsHref(tc.days, tc.agent, tc.offset); got != tc.want {
			t.Errorf("usageSessionsHref(%d, %q, %d) = %q, want %q", tc.days, tc.agent, tc.offset, got, tc.want)
		}
	}
}

// --- page ---

func newUsageSessionsEcho(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/usage/sessions", s.uiUsageSessions)
	return s, e
}

func sampleUsageRuns() *UsageRunListResponse {
	started := time.Date(2026, 11, 14, 9, 30, 0, 0, time.UTC)
	model := "deepseek-v4-pro"
	dur := 420000
	scheduled := "schedule"
	chat := "chat"
	return &UsageRunListResponse{
		Note: "Costs shown are estimates.",
		Data: []UsageRunRow{
			{RunID: "run-1", AgentID: "agent-1", AgentName: "coding", Status: "completed", TriggerSource: &scheduled, Model: &model, StartedAt: started, LastEventAt: started.Add(time.Minute), DurationMs: &dur, EventCount: 4, TotalText: 1200, TotalOutput: 300, TotalCached: 100, EstimatedCostUSD: 0.8123},
			{RunID: "run-2", AgentID: "agent-2", AgentName: "ui-auditor", Status: "failed", TriggerSource: &chat, StartedAt: started.Add(-2 * time.Hour), LastEventAt: started, EstimatedCostUSD: 0.1},
		},
		Total:  2,
		Limit:  50,
		Offset: 0,
	}
}

func TestUsageSessionsRoute(t *testing.T) {
	f := &fakeMemory{usageRuns: sampleUsageRuns(), usageByAgent: sampleUsageByAgent()}
	_, e := newUsageSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Sessions &amp; runs", "Run log",
		`data-testid="sessions-table"`,
		// stacked-table belongs on the wrapper's class list, not in a second
		// class attribute (a duplicate drops it and mobile loses the card layout).
		`class="overflow-x-auto stacked-table"`,
		`href="/runs/run-1"`, `href="/runs/run-2"`,
		"coding", "ui-auditor", "completed", "failed", "deepseek-v4-pro",
		// Per-row cost, plus the page total (0.8123 + 0.1).
		"$0.8123", "$0.9123",
		// Totals strip: filtered total, page count, page tokens.
		`data-testid="sessions-total-runs"`, `data-testid="sessions-page-runs"`,
		`data-testid="sessions-page-cost"`, `data-testid="sessions-page-tokens"`,
		// The origin of a run with no conversation behind it is named; a chat
		// run is the default and is not.
		`data-testid="sessions-trigger-run-1"`, "schedule",
		// Range links and agent chips.
		`data-testid="sessions-range-30"`, `data-testid="sessions-agent-agent-1"`,
		`data-testid="sessions-agent-all"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("run log missing %q", want)
		}
	}
	// One page of a two-row log needs no pager.
	if strings.Contains(body, `data-testid="sessions-pager"`) {
		t.Error("single-page log must not render the pager")
	}
	if f.usageRunsLimit != usageSessionsPageSize || f.usageRunsOffset != 0 || f.usageRunsAgentID != "" {
		t.Errorf("fetch = limit %d offset %d agent %q", f.usageRunsLimit, f.usageRunsOffset, f.usageRunsAgentID)
	}
}

func TestUsageSessionsPagingKeepsFilter(t *testing.T) {
	runs := sampleUsageRuns()
	runs.Total, runs.Offset = 130, 50
	f := &fakeMemory{usageRuns: runs, usageByAgent: sampleUsageByAgent()}
	_, e := newUsageSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions?days=7&agent=agent-1&offset=50", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.usageRunsAgentID != "agent-1" || f.usageRunsLimit != usageSessionsPageSize || f.usageRunsOffset != 50 {
		t.Errorf("fetch = limit %d offset %d agent %q", f.usageRunsLimit, f.usageRunsOffset, f.usageRunsAgentID)
	}
	body := rec.Body.String()
	// Newer goes back to offset 0 (omitted), older to offset 100; both keep the
	// range and the agent filter.
	for _, want := range []string{
		`data-testid="sessions-newer"`,
		`data-testid="sessions-older"`,
		`href="/usage/sessions?agent=agent-1&amp;days=7"`,
		`href="/usage/sessions?agent=agent-1&amp;days=7&amp;offset=100"`,
		// The range selector drops the offset but keeps the filter.
		`href="/usage/sessions?agent=agent-1&amp;days=90"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("paged run log missing %q", want)
		}
	}
}

// usageAgentPickerBlock returns the markup of the run log's agent picker: its
// wrapper through the close of its dropdown menu. Assertions are scoped to it
// because the page around the picker owns markup of its own, and a page-wide
// count or tag lookup would let that markup satisfy an assertion about the
// control (the actor picker's test scopes the same way).
func usageAgentPickerBlock(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, `data-testid="sessions-agent-filter"`)
	if start < 0 {
		t.Fatal("agent picker not found")
	}
	open := strings.LastIndex(html[:start], "<div")
	if open < 0 {
		t.Fatal("agent picker has no opening tag")
	}
	end := strings.Index(html[open:], `</ul>`)
	if end < 0 {
		t.Fatal("agent picker menu is not closed")
	}
	return html[open : open+end]
}

// usageAgentPickerTrigger returns the picker's closed control: everything before
// the menu, which is the trigger's mark, label and chevron.
func usageAgentPickerTrigger(t *testing.T, picker string) string {
	t.Helper()
	end := strings.Index(picker, "<ul")
	if end < 0 {
		t.Fatal("agent picker has no menu")
	}
	return picker[:end]
}

// usageAgentPickerRow returns the markup of one picker row: its <a>, from the
// opening tag to the close. A glyph assertion has to name the row that should
// wear it — two rows and a trigger share one menu, so "the menu contains
// lucide--database" would pass even if the wrong row wore it.
func usageAgentPickerRow(t *testing.T, picker, agentID string) string {
	t.Helper()
	marker := `data-testid="sessions-agent-` + agentID + `"`
	i := strings.Index(picker, marker)
	if i < 0 {
		t.Fatalf("picker has no row for %q:\n%s", agentID, picker)
	}
	start := strings.LastIndex(picker[:i], "<a")
	if start < 0 {
		t.Fatalf("row %q has no opening tag:\n%s", agentID, picker)
	}
	end := strings.Index(picker[i:], "</a>")
	if end < 0 {
		t.Fatalf("row %q is not closed:\n%s", agentID, picker)
	}
	return picker[start : i+end]
}

// TestUsageSessionsAgentPicker pins the run log's agent filter as the avatar
// dropdown #856 asked for, in the shape the objects page's actor picker and the
// chat rail's agent filter already ship: a menu of identities behind a
// select-styled trigger, one row per agent wearing its own mark, and an "All
// agents" row that clears the filter.
//
// The fixtures keep the two id spaces apart, as production does: the breakdown
// reports kb.agents ids (agent-1, agent-2 — the ?agent= filter values), while
// the definitions it draws appearance from are kb.agent_definitions rows
// (def-coding, def-auditor). Only kb.agents.agent_definition_id links them, so a
// picker that looked appearance up by the row's own id would render every row
// with the bot glyph and fail here.
func TestUsageSessionsAgentPicker(t *testing.T) {
	f := &fakeMemory{
		usageRuns:    sampleUsageRuns(),
		usageByAgent: sampleUsageByAgent(),
		agents: []AgentDefinitionSummary{
			{ID: "def-coding", Name: "coding", UIConfig: json.RawMessage(`{"icon":"database","color":"#ff0000"}`)},
			{ID: "def-auditor", Name: "ui-auditor", UIConfig: json.RawMessage(`{"icon":"eye","color":"#00ff00"}`)},
		},
		runtimeAgents: []RuntimeAgent{
			{ID: "agent-1", Name: "coding", AgentDefinitionID: "def-coding"},
			{ID: "agent-2", Name: "ui-auditor", AgentDefinitionID: "def-auditor"},
		},
	}
	_, e := newUsageSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions?days=30&offset=50", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	picker := usageAgentPickerBlock(t, body)

	// One render, one runtime-agent read: the picker's appearance join is a
	// single fetch for the whole menu, not one per row. (The sibling definitions
	// fetch cannot be counted this way — the page shell reads that catalog too.)
	if got := f.catalogCallCount("ListRuntimeAgents"); got != 1 {
		t.Errorf("runtime agents fetched %d times, want 1", got)
	}

	// The feedback overlay anchors on this wrapper, so it has to survive the
	// change from a chip row to a menu (the issue's anchor_stable contract).
	if !strings.Contains(body, `data-testid="sessions-agent-filter"`) {
		t.Fatal("the agent control must keep the sessions-agent-filter anchor")
	}
	if !strings.Contains(tagAround(t, body, `data-testid="sessions-agent-picker"`), "data-gd-menu-root") {
		t.Errorf("the picker is not a go-daisy menu root:\n%s", body)
	}
	// The closed trigger is a menu button that names the selection. data-gd-menu-
	// trigger is the hook go-daisy's runtime binds to open the menu and run
	// roving focus, so without it the control is decorative: clicking or pressing
	// Enter does nothing. It is asserted here because deleting it renders
	// byte-identical visible markup.
	trigger := usageAgentPickerTrigger(t, picker)
	for _, want := range []string{
		"data-gd-menu-trigger",
		`role="button"`,
		`aria-haspopup="menu"`,
		`aria-label="Filter runs by agent: All agents"`,
	} {
		if !strings.Contains(trigger, want) {
			t.Errorf("picker trigger missing %q: %s", want, trigger)
		}
	}
	// A real menu, with one radio row per identity. A long project lists one row
	// per agent that recorded usage, so the menu scrolls instead of pushing the
	// rest of the page down.
	if tag := tagAround(t, picker, `role="menu"`); !strings.Contains(tag, "overflow-y-auto") || !strings.Contains(tag, "max-h-96") {
		t.Errorf("the agent menu must cap its height and scroll: %s", tag)
	}
	if !strings.Contains(body, `role="menu"`) || !strings.Contains(body, `aria-label="Filter runs by agent"`) {
		t.Error("the picker does not render a labelled menu")
	}
	if got := strings.Count(body, `role="menuitemradio"`); got != 3 {
		t.Errorf("menu rows = %d, want 3 (All agents + two agents that recorded usage)", got)
	}
	if got := strings.Count(body, "data-gd-menu-item"); got != 3 {
		t.Errorf("rows wired for keyboard navigation = %d, want 3", got)
	}

	// Rows are links: the filter survives a refresh and can be shared, and
	// picking one keeps the range while dropping the page offset.
	for _, want := range []string{
		`href="/usage/sessions?days=30"`,
		`href="/usage/sessions?agent=agent-1&amp;days=30"`,
		`href="/usage/sessions?agent=agent-2&amp;days=30"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("picker row missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "agent=agent-1&amp;days=30&amp;offset=50") {
		t.Error("picking an agent must start at the first page, not keep the offset")
	}

	// Every row wears its own mark, and the mark is the one its DEFINITION
	// declares — reached through the runtime agent's agent_definition_id, not by
	// looking the row's own id up in the definition list. Asserting the glyph and
	// the colour inside the row keeps one row's appearance from paying for
	// another's, and the bot check catches the silent fallback the id-space mixup
	// produced on real data.
	if got := strings.Count(picker, `data-agent-avatar="all"`); got != 2 {
		t.Errorf("agent-all marks in the picker = %d, want 2 (trigger + All agents row)", got)
	}
	if got := strings.Count(picker, `data-agent-avatar="agent"`); got != 2 {
		t.Errorf("agent marks in the picker = %d, want 2 (one per agent row)", got)
	}
	codingRow := usageAgentPickerRow(t, picker, "agent-1")
	for _, want := range []string{`data-agent-avatar="agent"`, "lucide--database", `style="color:#ff0000`} {
		if !strings.Contains(codingRow, want) {
			t.Errorf("coding row missing its declared mark %q:\n%s", want, codingRow)
		}
	}
	if strings.Contains(codingRow, "lucide--bot") {
		t.Errorf("coding row fell back to the bot glyph, so its definition was not resolved:\n%s", codingRow)
	}
	auditorRow := usageAgentPickerRow(t, picker, "agent-2")
	for _, want := range []string{`data-agent-avatar="agent"`, "lucide--eye", `style="color:#00ff00`} {
		if !strings.Contains(auditorRow, want) {
			t.Errorf("ui-auditor row missing its declared mark %q:\n%s", want, auditorRow)
		}
	}

	// With no filter, "All agents" is the checked row and nothing else is.
	if !strings.Contains(tagAround(t, body, `data-testid="sessions-agent-all"`), `aria-checked="true"`) {
		t.Error("the unfiltered picker must check the All agents row")
	}
	if !strings.Contains(tagAround(t, body, `data-testid="sessions-agent-agent-1"`), `aria-checked="false"`) {
		t.Error("an unselected agent row must not be checked")
	}

	// Filtered: the trigger and the checked row follow the selection, the trigger
	// wears the selected agent's mark, and the rows still carry the request's
	// range.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions?days=7&agent=agent-1", nil))
	filtered := rec.Body.String()
	filteredPicker := usageAgentPickerBlock(t, filtered)
	if !strings.Contains(filtered, `aria-label="Filter runs by agent: coding"`) {
		t.Errorf("the trigger must name the selected agent:\n%s", filtered)
	}
	// The closed control mirrors the active row, so a narrowed log is readable
	// without opening the menu.
	if tag := usageAgentPickerTrigger(t, filteredPicker); !strings.Contains(tag, "lucide--database") || !strings.Contains(tag, `style="color:#ff0000`) {
		t.Errorf("the trigger must wear the selected agent's declared mark:\n%s", tag)
	}
	if !strings.Contains(tagAround(t, filteredPicker, `data-testid="sessions-agent-agent-1"`), `aria-checked="true"`) {
		t.Error("the selected agent's row must be checked")
	}
	if strings.Contains(tagAround(t, filteredPicker, `data-testid="sessions-agent-all"`), `aria-checked="true"`) {
		t.Error("a filtered picker must not also check All agents")
	}
	if !strings.Contains(filtered, `href="/usage/sessions?agent=agent-2&amp;days=7"`) {
		t.Errorf("rows must keep the selected range:\n%s", filtered)
	}

	// A filter that names an agent with no usage in the range stays selected, but
	// the control says "unknown agent" rather than resetting to "All agents" and
	// rather than showing the operator a bare id (these are UUIDs in production).
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions?agent=9f1c0a2e-6b41-4c8d-9a2f-0e7d3b5a1c22", nil))
	ghost := rec.Body.String()
	if !strings.Contains(ghost, `aria-label="Filter runs by agent: unknown agent"`) {
		t.Errorf("an unlisted active agent must be labelled in words:\n%s", ghost)
	}
	if strings.Contains(ghost, "Filter runs by agent: 9f1c0a2e") {
		t.Errorf("an unlisted active agent must not show its raw id as the label:\n%s", ghost)
	}

	// No breakdown, no picker: a menu with nothing to pick cannot narrow.
	_, emptyEcho := newUsageSessionsEcho(&fakeMemory{})
	rec = httptest.NewRecorder()
	emptyEcho.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions", nil))
	if emptyBody := rec.Body.String(); strings.Contains(emptyBody, `data-testid="sessions-agent-picker"`) {
		t.Error("the picker must not render without an agent breakdown")
	}
}

// TestUsageSessionsSkipsAppearanceFetchesWhenThePickerCannotRender pins the cost
// side of the appearance join: both fetches exist only to paint picker rows, so
// a failed log (whole-page error state) or a breakdown with no attributed agent
// (no picker) must not spend two round trips on markup that cannot render.
func TestUsageSessionsSkipsAppearanceFetchesWhenThePickerCannotRender(t *testing.T) {
	for name, f := range map[string]*fakeMemory{
		"failed log":        {usageRunsErr: errors.New("boom"), usageByAgent: sampleUsageByAgent()},
		"unattributed only": {usageByAgent: &UsageByAgentResponse{Data: []UsageByAgentRow{{TotalText: 5, EstimatedCostUSD: 0.01}}}},
	} {
		_, e := newUsageSessionsEcho(f)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", name, rec.Code)
		}
		if got := f.catalogCallCount("ListRuntimeAgents"); got != 0 {
			t.Errorf("%s: runtime agents fetched %d times, want 0", name, got)
		}
	}
}

// TestUsageSessionsAgentOptions pins the picker's row source: attributed agents
// in the breakdown's order, each carrying the appearance its definition
// declares, with the unattributed bucket dropped and a missing definition left
// on the generic mark.
//
// The breakdown ids and the definition ids are deliberately different strings:
// the breakdown reports kb.agents.id while appearance is keyed by
// kb.agent_definitions.id, and only the runtime agent's agent_definition_id
// joins them. A lookup keyed by the row's own id finds nothing here, exactly as
// it found nothing in production.
func TestUsageSessionsAgentOptions(t *testing.T) {
	defs := []AgentDefinitionSummary{
		{ID: "def-coding", Name: "coding", UIConfig: json.RawMessage(`{"icon":"database","color":"#ff0000"}`)},
		{ID: "def-auditor", Name: "ui-auditor", UIConfig: json.RawMessage(`{"icon":"eye","color":"#00ff00"}`)},
	}
	runtime := []RuntimeAgent{
		{ID: "agent-1", Name: "coding", AgentDefinitionID: "def-coding"},
		{ID: "agent-2", Name: "ui-auditor", AgentDefinitionID: "def-auditor"},
	}
	got := usageSessionsAgentOptions(sampleUsageByAgent(), defs, runtime)
	if len(got) != 2 {
		t.Fatalf("options = %d, want 2 (the unattributed bucket is not an agent): %+v", len(got), got)
	}
	// The row keeps the runtime id — it is the ?agent= filter value the run log
	// matches — while the appearance comes from the bound definition.
	if o := got[0]; o.ID != "agent-1" || o.Name != "coding" || o.Icon != "database" || o.Color != "#ff0000" {
		t.Errorf("got[0] = %+v, want agent-1/coding with its bound definition's appearance", o)
	}
	if o := got[1]; o.ID != "agent-2" || o.Name != "ui-auditor" || o.Icon != "eye" || o.Color != "#00ff00" {
		t.Errorf("got[1] = %+v, want agent-2/ui-auditor with its bound definition's appearance", o)
	}

	// An agent that binds no definition, or binds one the definitions fetch did
	// not return, keeps the bot mark rather than borrowing someone else's.
	for name, rt := range map[string][]RuntimeAgent{
		"unbound":         {{ID: "agent-1"}, {ID: "agent-2"}},
		"definition gone": {{ID: "agent-1", AgentDefinitionID: "def-gone"}, {ID: "agent-2", AgentDefinitionID: "def-gone"}},
		"fetch failed":    nil,
	} {
		rows := usageSessionsAgentOptions(sampleUsageByAgent(), defs, rt)
		if len(rows) != 2 {
			t.Fatalf("%s: options = %d, want 2", name, len(rows))
		}
		for _, o := range rows {
			if o.Icon != "" || o.Color != "" {
				t.Errorf("%s: row %+v must fall back to the bot mark, not a definition's", name, o)
			}
		}
	}
	if got := usageSessionsAgentOptions(nil, defs, runtime); got != nil {
		t.Errorf("a missing breakdown must yield no options, got %+v", got)
	}
}

func TestUsageSessionsEmpty(t *testing.T) {
	f := &fakeMemory{}
	_, e := newUsageSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "No agent runs in this range") {
		t.Errorf("empty state missing:\n%s", body)
	}
}

// TestUsageSessionsEmptyFiltered names the filter, so "no runs for this agent"
// is not mistaken for "this project never ran anything".
func TestUsageSessionsEmptyFiltered(t *testing.T) {
	f := &fakeMemory{usageByAgent: sampleUsageByAgent()}
	_, e := newUsageSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions?agent=agent-1", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "No runs for coding") {
		t.Errorf("filtered empty state missing:\n%s", body)
	}
}

func TestUsageSessionsBackendFailure(t *testing.T) {
	f := &fakeMemory{usageRunsErr: errors.New("boom")}
	_, e := newUsageSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (error state renders the page, not a crash)", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Failed to load the run log") {
		t.Errorf("error state missing:\n%s", body)
	}
}

// TestUsageSessionsBackToUsageIsPrimary pins #857: the run log's one header
// action renders with the system's primary (brand-yellow) button variant
// instead of the steel secondary link it used to be. The feedback overlay
// anchors on data-testid, so that selector must survive the variant change.
func TestUsageSessionsBackToUsageIsPrimary(t *testing.T) {
	f := &fakeMemory{usageRuns: sampleUsageRuns(), usageByAgent: sampleUsageByAgent()}
	_, e := newUsageSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	tag := tagAround(t, rec.Body.String(), `data-testid="sessions-back-to-usage"`)
	for _, want := range []string{"btn btn-primary btn-sm", `href="/usage"`} {
		if !strings.Contains(tag, want) {
			t.Errorf("back-to-usage tag missing %q: %s", want, tag)
		}
	}
	if strings.Contains(tag, "btn-secondary") {
		t.Errorf("back-to-usage must not render the steel secondary variant: %s", tag)
	}
}
