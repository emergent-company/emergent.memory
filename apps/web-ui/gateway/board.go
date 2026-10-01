package main

import (
	"net/url"
	"sort"
	"strings"

	"github.com/emergent-company/go-daisy/components/ui"
	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// boardStatusOrder is the canonical column order for the board. Statuses not in
// this list (custom per-agent mappings) are appended after it, sorted, so the
// board always renders a stable, predictable set of lanes.
var boardStatusOrder = []string{"ready", "in_progress", "review", "revision", "blocked", "done"}

// boardStatusLabel humanises a work status for the column header.
func boardStatusLabel(status string) string {
	if status == "in_progress" {
		return "In progress"
	}
	if status == "" {
		return "Unknown"
	}
	parts := strings.Split(status, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// boardStatusIntent maps a work status to its badge tone (L1 adapter).
func boardStatusIntent(status string) ui.BadgeIntent {
	switch status {
	case "done":
		return ui.BadgeSuccess
	case "review":
		return ui.BadgeWarning
	case "revision":
		return ui.BadgeAccent
	case "in_progress":
		return ui.BadgeInfo
	case "blocked":
		return ui.BadgeError
	default:
		return ui.BadgeNeutral
	}
}

// boardRunIntent maps a run execution status to its badge tone (L1 adapter).
func boardRunIntent(status string) ui.BadgeIntent {
	switch status {
	case "success", "completed":
		return ui.BadgeSuccess
	case "error", "failed":
		return ui.BadgeError
	case "running", "queued":
		return ui.BadgeInfo
	case "paused":
		return ui.BadgeWarning
	case "skipped", "cancelled", "cancelling":
		return ui.BadgeNeutral
	default:
		return ui.BadgeGhost
	}
}

// boardLane groups work items under one status lane.
type boardLane struct {
	Status string
	Items  []WorkItem
}

// buildBoardColumns groups items into columns by status in the canonical order,
// appending any custom statuses (sorted) after the known ones. Canonical lanes
// render even when empty so the board's drag targets are stable.
func buildBoardColumns(items []WorkItem) []boardLane {
	byStatus := make(map[string][]WorkItem)
	for _, it := range items {
		byStatus[it.Status] = append(byStatus[it.Status], it)
	}
	seen := make(map[string]bool)
	cols := make([]boardLane, 0, len(boardStatusOrder)+len(byStatus))
	for _, s := range boardStatusOrder {
		seen[s] = true
		cols = append(cols, boardLane{Status: s, Items: byStatus[s]})
	}
	var extra []string
	for s := range byStatus {
		if !seen[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(extra)
	for _, s := range extra {
		cols = append(cols, boardLane{Status: s, Items: byStatus[s]})
	}
	return cols
}

// boardUnhealthyAgents returns the runtime reaction agents that are unhealthy
// (disabled, or carrying consecutive failures) — the circuit-breaker readout.
func boardUnhealthyAgents(agents []ScheduledAgent) []ScheduledAgent {
	var out []ScheduledAgent
	for _, a := range agents {
		if a.TriggerType == "reaction" && (!a.Enabled || a.ConsecutiveFailures > 0) {
			out = append(out, a)
		}
	}
	return out
}

// uiBoard renders the Kanban board page. A failed fetch renders the whole-page
// error state.
func (s *Server) uiBoard(c echo.Context) error {
	ctx := c.Request().Context()
	items, err := s.memory.ListWorkItems(ctx, "", "", 200)
	if err != nil {
		return s.page(c, pageTitle("Board"), BoardPage(nil, nil, err))
	}
	agents, aerr := s.memory.ListScheduledAgents(ctx)
	if aerr != nil {
		captureError(aerr)
		agents = nil
	}
	return s.page(c, pageTitle("Board"), BoardPage(items, agents, nil))
}

// uiBoardPartial renders just the board columns (the htmx swap region), used by
// the drag/action refresh.
func (s *Server) uiBoardPartial(c echo.Context) error {
	return s.renderBoardColumns(c, nil)
}

// renderBoardColumns fetches the current work items and renders the columns
// partial, prepending an error banner when actionErr is non-nil.
func (s *Server) renderBoardColumns(c echo.Context, actionErr error) error {
	ctx := c.Request().Context()
	items, err := s.memory.ListWorkItems(ctx, "", "", 200)
	errMsg := ""
	if actionErr != nil {
		errMsg = actionErr.Error()
	} else if err != nil {
		errMsg = err.Error()
	}
	render.RenderPartial(c.Response().Writer, c.Request(), BoardRefresh(items, errMsg))
	return nil
}

// uiBoardItem renders the card drawer (a native dialog) for one work item.
func (s *Server) uiBoardItem(c echo.Context) error {
	ctx := c.Request().Context()
	canonicalID := c.Param("canonicalId")
	detail, err := s.memory.GetWorkItem(ctx, canonicalID)
	if err != nil {
		render.RenderPartial(c.Response().Writer, c.Request(), BoardDrawerError(err))
		return nil
	}
	render.RenderPartial(c.Response().Writer, c.Request(), BoardDrawer(detail))
	return nil
}

func (s *Server) uiBoardApprove(c echo.Context) error {
	_, err := s.memory.ApproveWorkItem(c.Request().Context(), c.Param("canonicalId"))
	return s.renderBoardColumns(c, err)
}

func (s *Server) uiBoardRequestChanges(c echo.Context) error {
	_, err := s.memory.RequestChangesWorkItem(c.Request().Context(), c.Param("canonicalId"), c.FormValue("feedback"))
	return s.renderBoardColumns(c, err)
}

func (s *Server) uiBoardRetry(c echo.Context) error {
	_, err := s.memory.RetryWorkItem(c.Request().Context(), c.Param("canonicalId"))
	return s.renderBoardColumns(c, err)
}

func (s *Server) uiBoardReassign(c echo.Context) error {
	_, err := s.memory.ReassignWorkItem(c.Request().Context(), c.Param("canonicalId"), c.FormValue("assignee"))
	return s.renderBoardColumns(c, err)
}

func (s *Server) uiBoardCancel(c echo.Context) error {
	_, err := s.memory.CancelWorkItem(c.Request().Context(), c.Param("canonicalId"))
	return s.renderBoardColumns(c, err)
}

// boardItemPath builds the drawer route for a work item.
func boardItemPath(canonicalID string) string {
	return "/board/items/" + url.PathEscape(canonicalID)
}

// boardActionPath builds an action route for a work item.
func boardActionPath(canonicalID, action string) string {
	return "/board/items/" + url.PathEscape(canonicalID) + "/" + action
}
