package main

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

// --- session / run cost log ---

// usageSessionsPageSize is one page of the run log. The page also shows the
// filtered total, so a truncated log says so instead of looking complete.
const usageSessionsPageSize = 50

// usageRunOffset parses the ?offset= paging parameter, treating anything that
// is not a positive integer as the first page.
func usageRunOffset(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// uiUsageSessions renders the per-run cost log: every agent run that recorded
// token usage in the selected range, most recent activity first, with the
// tokens and estimated cost it accumulated.
//
// This is the detail view behind the usage dashboard's "Agent runs" card and
// its per-agent breakdown. It is deliberately per RUN rather than per chat
// conversation: a conversation can hold many runs (and a scheduled or delegated
// run belongs to no conversation at all), so counting conversations is what
// made a $10 month look like a single session.
func (s *Server) uiUsageSessions(c echo.Context) error {
	ctx := c.Request().Context()
	days := usageRangeDays(c.QueryParam("days"))
	agentID := strings.TrimSpace(c.QueryParam("agent"))
	offset := usageRunOffset(c.QueryParam("offset"))
	now := time.Now().UTC()
	until := now
	since := now.AddDate(0, 0, -(days - 1))

	var (
		runs      *UsageRunListResponse
		runsErr   error
		byAgent   *UsageByAgentResponse
		agentsErr error
	)
	var g errgroup.Group
	g.Go(func() error {
		runs, runsErr = s.memory.GetProjectUsageRuns(ctx, since, until, agentID, usageSessionsPageSize, offset)
		return nil
	})
	// The agent filter chips come from the same breakdown the dashboard shows;
	// a failure only costs the chips, not the log.
	g.Go(func() error {
		byAgent, agentsErr = s.memory.GetProjectUsageByAgent(ctx, since, until)
		return nil
	})
	_ = g.Wait()

	data := usageSessionsPageData{
		Days:      days,
		AgentID:   agentID,
		Agents:    byAgent,
		AgentsErr: agentsErr,
		Runs:      runs,
		LoadErr:   runsErr,
		Limit:     usageSessionsPageSize,
		Offset:    offset,
	}
	if runs != nil {
		data.HasPrev = offset > 0
		data.HasNext = offset+len(runs.Data) < runs.Total
	}
	return s.page(c, pageTitle("Sessions"), UsageSessionsPage(data))
}

// usageSessionsHref renders a page URL for the log, preserving the range and
// agent filter and omitting the offset for the first page.
func usageSessionsHref(days int, agentID string, offset int) string {
	q := url.Values{}
	q.Set("days", strconv.Itoa(days))
	if agentID != "" {
		q.Set("agent", agentID)
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	return "/usage/sessions?" + q.Encode()
}

// usageAgentFilterLabel names the filter's current selection, falling back to
// the id when the breakdown could not name it (a filter carried in a URL from a
// range where the agent has no usage).
func usageAgentFilterLabel(byAgent *UsageByAgentResponse, agentID string) string {
	if agentID == "" {
		return "All agents"
	}
	if byAgent != nil {
		for _, row := range byAgent.Data {
			if row.AgentID == agentID && row.AgentName != "" {
				return row.AgentName
			}
		}
	}
	return agentID
}

// usageRunsCost sums the estimated cost of the rows on the page.
func usageRunsCost(rows []UsageRunRow) float64 {
	var total float64
	for _, r := range rows {
		total += r.EstimatedCostUSD
	}
	return total
}

// usageRunsTokens sums the token count of the rows on the page.
func usageRunsTokens(rows []UsageRunRow) int64 {
	var total int64
	for _, r := range rows {
		total += r.TokenCount()
	}
	return total
}
