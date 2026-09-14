package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- MemoryClient usage methods ---

func TestGetProjectUsageSummary(t *testing.T) {
	var gotPath, gotSince, gotUntil, gotGranularity string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSince = r.URL.Query().Get("since")
		gotUntil = r.URL.Query().Get("until")
		gotGranularity = r.URL.Query().Get("granularity")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"note":"Costs shown are estimates based on retail pricing.","data":[
			{"Provider":"openai","Model":"deepseek-v4-pro","TotalText":123,"TotalImage":0,"TotalVideo":0,"TotalAudio":0,"TotalOutput":456,"TotalCached":0,"EstimatedCostUSD":0.0123},
			{"Provider":"anthropic","Model":"claude-sonnet-4","TotalText":10,"TotalOutput":5,"EstimatedCostUSD":0.001}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)
	got, err := m.GetProjectUsageSummary(context.Background(), since, until)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/projects/proj-1/usage" {
		t.Errorf("path = %q", gotPath)
	}
	if gotSince != "2026-09-01T00:00:00Z" {
		t.Errorf("since = %q", gotSince)
	}
	if gotUntil != "2026-09-30T23:59:59Z" {
		t.Errorf("until = %q", gotUntil)
	}
	if gotGranularity != "" {
		t.Errorf("summary must not send granularity, got %q", gotGranularity)
	}
	if len(got.Data) != 2 {
		t.Fatalf("got %d rows, want 2", len(got.Data))
	}
	r := got.Data[0]
	if r.Provider != "openai" || r.Model != "deepseek-v4-pro" || r.TotalText != 123 || r.TotalOutput != 456 || r.EstimatedCostUSD != 0.0123 {
		t.Errorf("row0 = %+v", r)
	}
	if got.Note == "" {
		t.Error("note missing")
	}
}

func TestGetProjectUsageSummaryOmitsZeroBounds(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"note":"","data":[]}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.GetProjectUsageSummary(context.Background(), time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty", gotQuery)
	}
}

func TestGetProjectUsageTimeSeries(t *testing.T) {
	var gotPath, gotGranularity, gotSince, gotUntil string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotGranularity = r.URL.Query().Get("granularity")
		gotSince = r.URL.Query().Get("since")
		gotUntil = r.URL.Query().Get("until")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"note":"Costs shown are estimates.","data":[
			{"Period":"2026-09-01T00:00:00Z","Provider":"openai","Model":"deepseek-v4-pro","TotalText":1,"TotalImage":0,"TotalVideo":0,"TotalAudio":0,"TotalOutput":2,"TotalCached":0,"EstimatedCostUSD":0.0001}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	got, err := m.GetProjectUsageTimeSeries(context.Background(), "day", since, until)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/projects/proj-1/usage/timeseries" {
		t.Errorf("path = %q", gotPath)
	}
	if gotGranularity != "day" {
		t.Errorf("granularity = %q, want day", gotGranularity)
	}
	if gotSince != "2026-09-01T00:00:00Z" || gotUntil != "2026-09-07T00:00:00Z" {
		t.Errorf("since/until = %q / %q", gotSince, gotUntil)
	}
	if len(got.Data) != 1 {
		t.Fatalf("got %d rows, want 1", len(got.Data))
	}
	r := got.Data[0]
	if r.Period != "2026-09-01T00:00:00Z" || r.TotalText != 1 || r.TotalOutput != 2 || r.EstimatedCostUSD != 0.0001 {
		t.Errorf("row = %+v", r)
	}
}

// --- handler ---

func newUsageEcho(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/usage", s.uiUsage)
	return s, e
}

func sampleUsageSummary() *UsageSummaryResponse {
	return &UsageSummaryResponse{
		Note: "Costs shown are estimates.",
		Data: []UsageSummaryRow{
			{Provider: "openai", Model: "deepseek-v4-pro", TotalText: 100, TotalImage: 0, TotalVideo: 0, TotalAudio: 0, TotalOutput: 50, EstimatedCostUSD: 0.0123},
			{Provider: "anthropic", Model: "claude-sonnet-4", TotalText: 10, TotalOutput: 5, EstimatedCostUSD: 0.001},
		},
	}
}

// sampleUsageSeries builds a time-series covering today so the dashboard's
// since/until window (computed from time.Now in the handler) includes it.
func sampleUsageSeries() *UsageTimeSeriesResponse {
	now := time.Now().UTC()
	return &UsageTimeSeriesResponse{
		Note: "Costs shown are estimates.",
		Data: []UsageTimeSeriesRow{
			{Period: now.Format(time.RFC3339), Provider: "openai", Model: "deepseek-v4-pro", TotalText: 100, TotalOutput: 50, EstimatedCostUSD: 0.0123},
		},
	}
}

func TestUsageRoute(t *testing.T) {
	f := &fakeMemory{
		usageSummary: sampleUsageSummary(),
		usageSeries:  sampleUsageSeries(),
		convs: []Conversation{
			{ID: "c1", Title: "Morning", CreatedAt: time.Now().UTC().Format(time.RFC3339)},
			{ID: "c2", Title: "Evening", CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		},
	}
	_, e := newUsageEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Usage", "Total tokens", "165", // 100+50+10+5
		"$0.0123", "$0.0133", // per-row cost + range total (0.0123+0.001)
		"Current month spend", "deepseek-v4-pro", "claude-sonnet-4",
		"openai", "anthropic", "Costs shown are estimates.",
		`id="usage-timeseries"`, `type="application/json"`, `"tokens":`, `"sessions":`,
		"Tokens per day", "Sessions per day", "Per-model usage",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("usage page missing %q", want)
		}
	}
}

func TestUsageRouteRangeSelector(t *testing.T) {
	f := &fakeMemory{usageSummary: sampleUsageSummary(), usageSeries: sampleUsageSeries()}
	_, e := newUsageEcho(f)
	for _, days := range []string{"7", "30", "90"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage?days="+days, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("days=%s status = %d", days, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), days+" day range") {
			t.Errorf("days=%s range badge missing", days)
		}
	}
	// Invalid / absent days fall back to the 30-day default.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage?days=5", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "30 day range") {
		t.Errorf("fallback days: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUsageRouteEmptyState(t *testing.T) {
	f := &fakeMemory{} // no usage rows, no series rows, no conversations
	_, e := newUsageEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No usage data") {
		t.Errorf("empty state missing:\n%s", body)
	}
	if strings.Contains(body, "Failed to load usage") {
		t.Errorf("empty data must not render the error state:\n%s", body)
	}
}

func TestUsageRouteBackendFailure(t *testing.T) {
	f := &fakeMemory{usageErr: fmt.Errorf("memory 503: service down")}
	_, e := newUsageEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (error state renders the page, not a crash)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Failed to load usage") || !strings.Contains(body, "service down") {
		t.Errorf("error state missing:\n%s", body)
	}
}

func TestUsageChartPayloadEmbedded(t *testing.T) {
	payload := usageChartPayload{
		Tokens:   []usageChartPoint{{Day: "2026-08-30", Value: 150}, {Day: "2026-08-31", Value: 15}},
		Sessions: []usageChartPoint{{Day: "2026-08-30", Value: 3}},
	}
	html := renderHTML(t, UsagePage(sampleUsageSummary(), sampleUsageSeries(), payload, 0.0133, true, 30, nil))
	for _, want := range []string{
		`id="usage-timeseries"`, `type="application/json"`,
		`"tokens"`, `"sessions"`, `"day":"2026-08-30"`, `"value":150`,
		`data-testid="usage-token-chart"`, `data-testid="usage-session-chart"`,
		"/assets/js/usage-charts.js",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chart payload missing %q", want)
		}
	}
	// The JSON payload must be parseable as our payload shape.
	var decoded usageChartPayload
	if err := json.Unmarshal([]byte(extractScriptJSON(t, html)), &decoded); err != nil {
		t.Fatalf("payload JSON invalid: %v", err)
	}
	if len(decoded.Tokens) != 2 || decoded.Tokens[1].Value != 15 || decoded.Sessions[0].Value != 3 {
		t.Errorf("decoded payload = %+v", decoded)
	}
}

// extractScriptJSON pulls the content of the #usage-timeseries script block.
func extractScriptJSON(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, `id="usage-timeseries"`)
	if start < 0 {
		t.Fatal("usage-timeseries script not found")
	}
	open := strings.Index(html[start:], ">")
	body := html[start+open+1:]
	end := strings.Index(body, "</script>")
	if end < 0 {
		t.Fatal("closing script tag not found")
	}
	return strings.TrimSpace(body[:end])
}

func TestUsageSessionSeriesGroupsByDay(t *testing.T) {
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	convs := []Conversation{
		{ID: "a", CreatedAt: "2026-09-01T08:00:00Z"},
		{ID: "b", CreatedAt: "2026-09-01T22:30:00Z"},
		{ID: "c", CreatedAt: "2026-09-02T01:00:00Z"},
		{ID: "d", CreatedAt: "2026-08-31T23:00:00Z"}, // outside range
		{ID: "e", CreatedAt: "not-a-time"},
	}
	points := sessionSeries(convs, since, until)
	if len(points) != 3 {
		t.Fatalf("want 3 days, got %+v", points)
	}
	if points[0].Value != 2 || points[1].Value != 1 || points[2].Value != 0 {
		t.Errorf("points = %+v", points)
	}
}

func TestUsageTokenSeriesAggregatesByDay(t *testing.T) {
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	rows := []UsageTimeSeriesRow{
		{Period: "2026-09-01T00:00:00Z", TotalText: 100, TotalOutput: 50},
		{Period: "2026-09-01T12:00:00Z", TotalText: 10}, // same day, different provider/model
		{Period: "2026-09-02T00:00:00Z", TotalText: 5},
		{Period: "garbage"},
	}
	points := tokenSeries(rows, since, until)
	if len(points) != 2 {
		t.Fatalf("want 2 days, got %+v", points)
	}
	if points[0].Value != 160 || points[1].Value != 5 {
		t.Errorf("points = %+v", points)
	}
}
