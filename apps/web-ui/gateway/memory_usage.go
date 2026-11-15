package main

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// --- project usage ---

// UsageSummaryRow is one provider instance+model row of aggregated token
// usage. Provider is the dialect; ProviderSlug is the instance that served the
// request (empty for rows recorded before instances existed — the UI then
// shows the dialect). The memory service serializes usage rows WITHOUT json
// tags, so the wire keys are the Go field names in PascalCase (Provider,
// Model, TotalText, …).
type UsageSummaryRow struct {
	Provider         string  `json:"Provider"`
	ProviderSlug     string  `json:"ProviderSlug"`
	Model            string  `json:"Model"`
	TotalText        int64   `json:"TotalText"`
	TotalImage       int64   `json:"TotalImage"`
	TotalVideo       int64   `json:"TotalVideo"`
	TotalAudio       int64   `json:"TotalAudio"`
	TotalOutput      int64   `json:"TotalOutput"`
	TotalCached      int64   `json:"TotalCached"`
	EstimatedCostUSD float64 `json:"EstimatedCostUSD"`
}

// UsageSummaryResponse is the bare (unwrapped) GET /usage response. The note
// states that costs are retail-pricing estimates and is always displayed with
// spend.
type UsageSummaryResponse struct {
	Note string            `json:"note"`
	Data []UsageSummaryRow `json:"data"`
}

// UsageTimeSeriesRow is one time-bucketed usage row. Period is an RFC3339
// timestamp (start of the bucket). Provider is the dialect; ProviderSlug is
// the instance (empty for legacy rows). Like the summary rows, keys are the
// PascalCase Go field names on the wire.
type UsageTimeSeriesRow struct {
	Period           string  `json:"Period"` // RFC3339
	Provider         string  `json:"Provider"`
	ProviderSlug     string  `json:"ProviderSlug"`
	Model            string  `json:"Model"`
	TotalText        int64   `json:"TotalText"`
	TotalImage       int64   `json:"TotalImage"`
	TotalVideo       int64   `json:"TotalVideo"`
	TotalAudio       int64   `json:"TotalAudio"`
	TotalOutput      int64   `json:"TotalOutput"`
	TotalCached      int64   `json:"TotalCached"`
	EstimatedCostUSD float64 `json:"EstimatedCostUSD"`
}

// UsageTimeSeriesResponse is the bare (unwrapped) GET /usage/timeseries
// response.
type UsageTimeSeriesResponse struct {
	Note string               `json:"note"`
	Data []UsageTimeSeriesRow `json:"data"`
}

// usageQueryParams renders the ?since=&until= query string for the usage
// endpoints. Zero times are omitted so memory applies its own default range.
func usageQueryParams(since, until time.Time) url.Values {
	q := url.Values{}
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	if !until.IsZero() {
		q.Set("until", until.UTC().Format(time.RFC3339))
	}
	return q
}

// BudgetWindowState is one spending window of a project's budget, as reported
// by GET /api/v1/projects/{id}/usage/budget. The period is computed server-side
// (internal/budgetwindow) so the UI renders the same week/month boundary that
// enforces the cap.
type BudgetWindowState struct {
	// Window is "week" or "month".
	Window string `json:"window"`
	// CapUSD is nil when the window has no cap (no limit).
	CapUSD *float64 `json:"capUsd"`
	// Threshold is the alert threshold as a fraction (0.8 = 80%).
	Threshold float64 `json:"threshold"`
	SpendUSD  float64 `json:"spendUsd"`
	// RemainingUSD is nil when there is no cap.
	RemainingUSD *float64  `json:"remainingUsd"`
	PercentUsed  float64   `json:"percentUsed"`
	PeriodStart  time.Time `json:"periodStart"`
	PeriodEnd    time.Time `json:"periodEnd"`
	// Status is "ok", "warning" (threshold crossed) or "exhausted" (cap hit).
	Status string `json:"status"`
}

// Capped reports whether the window has a spend cap.
func (w *BudgetWindowState) Capped() bool {
	return w != nil && w.CapUSD != nil && *w.CapUSD > 0
}

// BudgetState is a project's budget position across both windows.
type BudgetState struct {
	Week  *BudgetWindowState `json:"week"`
	Month *BudgetWindowState `json:"month"`
}

// BudgetAlertLevel collapses both windows into the single level the nav badge
// shows: "exhausted" beats "warning", no cap anywhere is "".
func (b *BudgetState) AlertLevel() string {
	if b == nil {
		return ""
	}
	level := ""
	for _, w := range []*BudgetWindowState{b.Week, b.Month} {
		if !w.Capped() {
			continue
		}
		switch w.Status {
		case "exhausted":
			return "exhausted"
		case "warning":
			level = "warning"
		}
	}
	return level
}

// BudgetPeriod is the window returned by the reset endpoint.
type BudgetPeriod struct {
	Window      string    `json:"window"`
	PeriodStart time.Time `json:"periodStart"`
	PeriodEnd   time.Time `json:"periodEnd"`
}

// GetProjectBudgetState returns the project's position in both spending
// windows. The response is BARE JSON — no success envelope. A failure must be
// non-fatal at the call sites that decorate a page (badge, usage summary): an
// unreachable budget endpoint should hide the badge, not fail the page.
func (m *MemoryClient) GetProjectBudgetState(ctx context.Context) (*BudgetState, error) {
	var out BudgetState
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/usage/budget"
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResetProjectBudgetWindow restarts one spending window (week|month) from now,
// by moving that window's cycle anchor. The other window and all usage history
// are untouched. The backend requires project-admin authority.
func (m *MemoryClient) ResetProjectBudgetWindow(ctx context.Context, window string) (*BudgetPeriod, error) {
	var out BudgetPeriod
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/budget/reset"
	if err := m.do(ctx, http.MethodPost, path, map[string]string{"window": window}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProjectUsageSummary returns the project's aggregated token usage and
// estimated cost, grouped by provider + model, for the given range (zero times
// omit the bounds). The response is BARE JSON — no success envelope.
func (m *MemoryClient) GetProjectUsageSummary(ctx context.Context, since, until time.Time) (*UsageSummaryResponse, error) {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/usage"
	if q := usageQueryParams(since, until); len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out UsageSummaryResponse
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProjectUsageTimeSeries returns the project's usage bucketed by
// granularity ("day" | "week" | "month") for the given range (zero times omit
// the bounds). The response is BARE JSON — no success envelope.
func (m *MemoryClient) GetProjectUsageTimeSeries(ctx context.Context, granularity string, since, until time.Time) (*UsageTimeSeriesResponse, error) {
	path := "/api/v1/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/usage/timeseries"
	q := usageQueryParams(since, until)
	if granularity != "" {
		q.Set("granularity", granularity)
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out UsageTimeSeriesResponse
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
