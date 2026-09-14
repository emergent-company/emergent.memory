package main

import (
	"sort"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

// --- usage dashboard ---

// usageRangeDays maps the ?days= selector to a day count. Anything outside
// 7/30/90 falls back to the 30-day default.
func usageRangeDays(raw string) int {
	switch raw {
	case "7":
		return 7
	case "30":
		return 30
	case "90":
		return 90
	default:
		return 30
	}
}

// usageChartPoint is one day in the dashboard chart payload. Day is the
// calendar-day key ("2006-01-02", UTC).
type usageChartPoint struct {
	Day   string `json:"day"`
	Value int64  `json:"value"`
}

// usageChartPayload is embedded in the page as the #usage-timeseries JSON
// script block consumed by usage-charts.js.
type usageChartPayload struct {
	Tokens   []usageChartPoint `json:"tokens"`
	Sessions []usageChartPoint `json:"sessions"`
}

// dayKey returns the UTC calendar-day key for a time.Time.
func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// fillDaySeries returns one zero-valued point per day in [since, until]
// (inclusive), then adds value per day. Keeps the chart's x-axis continuous.
func fillDaySeries(since, until time.Time, add func(day string) int64) []usageChartPoint {
	start := since.UTC()
	end := until.UTC()
	if start.After(end) {
		return nil
	}
	days := int(end.Sub(start).Hours()/24) + 1
	points := make([]usageChartPoint, 0, days)
	cur := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	for range days {
		key := dayKey(cur)
		points = append(points, usageChartPoint{Day: key, Value: add(key)})
		cur = cur.AddDate(0, 0, 1)
	}
	return points
}

// tokenSeries aggregates the daily time-series rows into per-day token totals
// (input text/image/video/audio + output).
func tokenSeries(rows []UsageTimeSeriesRow, since, until time.Time) []usageChartPoint {
	totals := map[string]int64{}
	for _, r := range rows {
		t, err := time.Parse(time.RFC3339, r.Period)
		if err != nil {
			continue
		}
		key := dayKey(t)
		totals[key] += r.TotalText + r.TotalImage + r.TotalVideo + r.TotalAudio + r.TotalOutput
	}
	return fillDaySeries(since, until, func(day string) int64 { return totals[day] })
}

// sessionSeries counts conversations created per calendar day within the range.
func sessionSeries(convs []Conversation, since, until time.Time) []usageChartPoint {
	counts := map[string]int64{}
	sinceDay := dayKey(since)
	untilDay := dayKey(until)
	for _, c := range convs {
		t, err := time.Parse(time.RFC3339, c.CreatedAt)
		if err != nil {
			continue
		}
		key := dayKey(t)
		if key < sinceDay || key > untilDay {
			continue
		}
		counts[key]++
	}
	return fillDaySeries(since, until, func(day string) int64 { return counts[day] })
}

// totalTokens sums all token buckets across the summary rows.
func totalTokens(rows []UsageSummaryRow) int64 {
	var total int64
	for _, r := range rows {
		total += r.TotalText + r.TotalImage + r.TotalVideo + r.TotalAudio + r.TotalOutput
	}
	return total
}

// totalCost sums the estimated cost across the summary rows.
func totalCost(rows []UsageSummaryRow) float64 {
	var total float64
	for _, r := range rows {
		total += r.EstimatedCostUSD
	}
	return total
}

// uiUsage renders the usage dashboard: summary cards (total tokens + current
// month spend when the backend reports it), a daily token chart, a
// sessions-per-day chart, a per-model table, and a 7/30/90-day range selector
// (?days=, default 30). A failed summary fetch renders the whole-page error
// state; the current-month spend call is best-effort and omits the card on
// failure.
func (s *Server) uiUsage(c echo.Context) error {
	ctx := c.Request().Context()
	days := usageRangeDays(c.QueryParam("days"))
	now := time.Now().UTC()
	until := now
	since := now.AddDate(0, 0, -(days - 1))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var (
		summary    *UsageSummaryResponse
		summaryErr error
		series     *UsageTimeSeriesResponse
		seriesErr  error
		convs      *ConversationList
		convsErr   error
		monthSum   *UsageSummaryResponse
		monthErr   error
	)
	var g errgroup.Group
	g.Go(func() error { summary, summaryErr = s.memory.GetProjectUsageSummary(ctx, since, until); return nil })
	g.Go(func() error {
		series, seriesErr = s.memory.GetProjectUsageTimeSeries(ctx, "day", since, until)
		return nil
	})
	g.Go(func() error { convs, convsErr = s.memory.ListConversations(ctx); return nil })
	g.Go(func() error { monthSum, monthErr = s.memory.GetProjectUsageSummary(ctx, monthStart, now); return nil })
	_ = g.Wait()

	if summaryErr != nil {
		return s.page(c, pageTitle("Usage"), UsagePage(nil, nil, usageChartPayload{}, 0, false, days, summaryErr))
	}
	if seriesErr != nil {
		return s.page(c, pageTitle("Usage"), UsagePage(summary, nil, usageChartPayload{}, 0, false, days, seriesErr))
	}
	if convsErr != nil {
		return s.page(c, pageTitle("Usage"), UsagePage(summary, series, usageChartPayload{}, 0, false, days, convsErr))
	}

	payload := usageChartPayload{
		Tokens:   tokenSeries(series.Data, since, until),
		Sessions: sessionSeries(convs.Conversations, since, until),
	}

	monthSpend := 0.0
	hasMonthSpend := false
	if monthErr == nil && monthSum != nil {
		monthSpend = totalCost(monthSum.Data)
		hasMonthSpend = true
	}

	return s.page(c, pageTitle("Usage"), UsagePage(summary, series, payload, monthSpend, hasMonthSpend, days, nil))
}

// usageModels returns the per-model rows for the table, sorted by cost
// descending (stable — ties keep provider/model order).
func usageModels(rows []UsageSummaryRow) []UsageSummaryRow {
	out := make([]UsageSummaryRow, len(rows))
	copy(out, rows)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].EstimatedCostUSD > out[j].EstimatedCostUSD
	})
	return out
}

// modelTokenCount sums the token buckets of one usage row.
func modelTokenCount(r UsageSummaryRow) int64 {
	return r.TotalText + r.TotalImage + r.TotalVideo + r.TotalAudio + r.TotalOutput
}

// formatCost renders a USD figure as "$1.23".
func formatCost(v float64) string {
	return "$" + strconv.FormatFloat(v, 'f', 4, 64)
}

// compactNum renders a count as a short human-friendly figure: 1500 → "1.5K",
// 2_000_000 → "2.0M", small counts stay exact.
func compactNum(n int64) string {
	switch {
	case n >= 1e9:
		return strconv.FormatFloat(float64(n)/1e9, 'f', 1, 64) + "B"
	case n >= 1e6:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	case n >= 1e3:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(n, 10)
	}
}

// chartSeriesTotal sums the values of a (zero-filled) chart series.
func chartSeriesTotal(points []usageChartPoint) int64 {
	var total int64
	for _, p := range points {
		total += p.Value
	}
	return total
}

// hasUsageData reports whether the page has anything to show: summary rows or
// time-series rows.
func hasUsageData(summary *UsageSummaryResponse, series *UsageTimeSeriesResponse) bool {
	if summary != nil && len(summary.Data) > 0 {
		return true
	}
	if series != nil && len(series.Data) > 0 {
		return true
	}
	return false
}

// usageNote returns the backend's "costs are estimates" disclaimer, preferring
// the summary response's note.
func usageNote(summary *UsageSummaryResponse, series *UsageTimeSeriesResponse) string {
	if summary != nil && summary.Note != "" {
		return summary.Note
	}
	if series != nil && series.Note != "" {
		return series.Note
	}
	return ""
}
