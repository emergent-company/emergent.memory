package main

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// --- project usage ---

// UsageSummaryRow is one provider+model row of aggregated token usage. The
// memory service serializes usage rows WITHOUT json tags, so the wire keys are
// the Go field names in PascalCase (Provider, Model, TotalText, …).
type UsageSummaryRow struct {
	Provider         string  `json:"Provider"`
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
// timestamp (start of the bucket). Like the summary rows, keys are the
// PascalCase Go field names on the wire.
type UsageTimeSeriesRow struct {
	Period           string  `json:"Period"` // RFC3339
	Provider         string  `json:"Provider"`
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
