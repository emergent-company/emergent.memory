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
		runs    *UsageRunListResponse
		runsErr error
		byAgent *UsageByAgentResponse
		defs    []AgentDefinitionSummary
		runtime []RuntimeAgent
	)
	var g errgroup.Group
	g.Go(func() error {
		runs, runsErr = s.memory.GetProjectUsageRuns(ctx, since, until, agentID, usageSessionsPageSize, offset)
		return nil
	})
	// The agent picker's rows come from the same breakdown the dashboard shows;
	// a failure only costs the picker, not the log.
	g.Go(func() error {
		byAgent, _ = s.memory.GetProjectUsageByAgent(ctx, since, until)
		return nil
	})
	// The rows wear each agent's declared glyph, so the picker reads like the
	// chat rail's and the objects page's. Appearance is decoration: a failed
	// fetch leaves every row on the generic bot mark, exactly as an agent whose
	// definition is gone renders, so this failure is not surfaced either.
	g.Go(func() error {
		defs, _ = s.memory.ListAgentDefinitions(ctx)
		return nil
	})
	// The breakdown reports kb.agents ids while a definition's appearance is
	// keyed by kb.agent_definitions id, so the two fetches above cannot be joined
	// on the row's own id; this fetch supplies the runtime agent's definition id.
	// Best-effort for the same reason as the definitions: an unreachable map only
	// costs the glyph, and every row stays on the generic bot mark.
	g.Go(func() error {
		runtime, _ = s.memory.ListRuntimeAgents(ctx, s.currentProjectID(ctx))
		return nil
	})
	_ = g.Wait()

	data := usageSessionsPageData{
		Days:         days,
		AgentID:      agentID,
		Agents:       byAgent,
		AgentOptions: usageSessionsAgentOptions(byAgent, defs, runtime),
		Runs:         runs,
		Limit:        usageSessionsPageSize,
		Offset:       offset,
	}
	// The error state renders the error's message, so a raw backend failure has
	// to be sanitized here, exactly as the dashboard does (#1671).
	if runsErr != nil {
		data.LoadErr = userFacingError(runsErr)
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

// usageSessionsAgentOptions builds the agent picker's rows from the per-agent
// breakdown, in the order the breakdown returns them, joined with each agent's
// declared appearance so a row wears the glyph the agent wears elsewhere. Only
// attributed rows become options: the unattributed bucket is not an agent and
// cannot be filtered by agent id.
//
// The row keeps the breakdown's id — that is the ?agent= filter value the run
// log matches — and only the appearance lookup crosses over to the definition
// id space (see usageRuntimeAgentDefinitionID).
func usageSessionsAgentOptions(byAgent *UsageByAgentResponse, defs []AgentDefinitionSummary, runtime []RuntimeAgent) []usageSessionsAgentOption {
	if byAgent == nil {
		return nil
	}
	out := make([]usageSessionsAgentOption, 0, len(byAgent.Data))
	for _, row := range byAgent.Data {
		if !row.Attributed() {
			continue
		}
		appearance := agentUIForList(defs, usageRuntimeAgentDefinitionID(runtime, row.AgentID))
		out = append(out, usageSessionsAgentOption{
			ID:    row.AgentID,
			Name:  row.AgentName,
			Icon:  appearance.Icon,
			Color: appearance.Color,
		})
	}
	return out
}

// usageRuntimeAgentDefinitionID maps a breakdown row's agent id to the agent
// definition whose declared appearance describes it. The two are independent id
// spaces: the breakdown projects kb.agents.id (the id the run log's ?agent=
// filter matches), while ListAgentDefinitions returns kb.agent_definitions.id.
// Looking a row up by its own id therefore never matches on real data and
// silently painted every row with the generic bot mark. An agent that binds no
// definition (or one the best-effort fetch could not see) yields "" and renders
// the bot mark, which is the correct fallback.
func usageRuntimeAgentDefinitionID(runtime []RuntimeAgent, agentID string) string {
	for _, a := range runtime {
		if a.ID == agentID {
			return a.AgentDefinitionID
		}
	}
	return ""
}

// usageSessionsAgentOptionOf returns the picker row for an agent id, so the
// closed trigger can borrow the same mark its row renders. known is false for an
// unfiltered log and for a filter that names an agent the breakdown does not
// list (see usageAgentFilterLabel).
func usageSessionsAgentOptionOf(options []usageSessionsAgentOption, agentID string) (usageSessionsAgentOption, bool) {
	if agentID == "" {
		return usageSessionsAgentOption{}, false
	}
	for _, o := range options {
		if o.ID == agentID {
			return o, true
		}
	}
	return usageSessionsAgentOption{}, false
}

// usageTriggerLabel shortens a run's trigger source for the log's agent cell.
// Webhook sources are recorded as "webhook:<id>"; the id adds nothing on this
// page, so only the kind is shown.
func usageTriggerLabel(source string) string {
	if kind, _, ok := strings.Cut(source, ":"); ok && kind != "" {
		return kind
	}
	return source
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
