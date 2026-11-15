package main

import (
	"context"
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
