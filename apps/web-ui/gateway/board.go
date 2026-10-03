package main

import (
	"context"
	"encoding/json"
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

// boardStatusMap is the project's work-path status mapping: which object status
// value means each work-item lifecycle phase. It lets the board translate its
// fixed transition set (retry/approve/request-changes/cancel) into whatever
// statuses the project's agents configured, defaulting to the built-in names so
// an unconfigured project keeps the canonical behaviour. Zero value is not
// meaningful — always start from defaultBoardStatusMap.
type boardStatusMap struct {
	Ready      string `json:"ready"`
	InProgress string `json:"inProgress"`
	Review     string `json:"review"`
	Revision   string `json:"revision"`
	Blocked    string `json:"blocked"`
	Done       string `json:"done"`
}

// defaultBoardStatusMap is the canonical mapping. Its phase values match
// boardStatusOrder, so a default board renders and behaves exactly as before.
func defaultBoardStatusMap() boardStatusMap {
	return boardStatusMap{
		Ready:      "ready",
		InProgress: "in_progress",
		Review:     "review",
		Revision:   "revision",
		Blocked:    "blocked",
		Done:       "done",
	}
}

// statusMapJSON serialises the map for the #board[data-board-status-map]
// attribute app.js reads to derive drag transitions. Falls back to "" on the
// (practically impossible) marshal error.
func (m boardStatusMap) statusMapJSON() string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// boardStatusMapFromDefinitions derives the project's work-path status mapping
// from its agent definitions' workConfig.status blocks. Each phase is overridden
// only when every declaring agent agrees on one non-empty value (exactly one
// distinct value), so a stray agent cannot silently hijack the board's
// transitions; a phase no agent declares keeps its built-in default.
func boardStatusMapFromDefinitions(defs []AgentDefinitionSummary) boardStatusMap {
	ready := map[string]struct{}{}
	inProgress := map[string]struct{}{}
	review := map[string]struct{}{}
	revision := map[string]struct{}{}
	blocked := map[string]struct{}{}
	done := map[string]struct{}{}
	add := func(set map[string]struct{}, v string) {
		if v != "" {
			set[v] = struct{}{}
		}
	}
	for _, d := range defs {
		if d.WorkConfig == nil {
			continue
		}
		s := d.WorkConfig.Status
		add(ready, s.Ready)
		add(inProgress, s.InProgress)
		add(review, s.Review)
		add(revision, s.Revision)
		add(blocked, s.Blocked)
		add(done, s.Done)
	}
	m := defaultBoardStatusMap()
	if v, ok := soleValue(ready); ok {
		m.Ready = v
	}
	if v, ok := soleValue(inProgress); ok {
		m.InProgress = v
	}
	if v, ok := soleValue(review); ok {
		m.Review = v
	}
	if v, ok := soleValue(revision); ok {
		m.Revision = v
	}
	if v, ok := soleValue(blocked); ok {
		m.Blocked = v
	}
	if v, ok := soleValue(done); ok {
		m.Done = v
	}
	return m
}

// soleValue returns the one value in set, or ("", false) when the set is empty
// or carries more than one distinct value (an ambiguous declaration).
func soleValue(set map[string]struct{}) (string, bool) {
	if len(set) != 1 {
		return "", false
	}
	for v := range set {
		return v, true
	}
	return "", false
}

// boardStatusMap derives the project's work-path status mapping. A failed
// definitions fetch is captured and yields the canonical default so the board
// never breaks on a definitions outage.
func (s *Server) boardStatusMap(ctx context.Context) boardStatusMap {
	defs, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		captureError(err)
		return defaultBoardStatusMap()
	}
	return boardStatusMapFromDefinitions(defs)
}

// boardStatusesFromCompiled derives the board lane order from the compiled
// schema's board-enabled object types, in declaration order, de-duplicated
// first-seen. Shadowed (losing) duplicates are skipped — same as every other
// compiled-type consumer (visibleCompiledTypes) — so an overridden board type
// cannot leak stale lane statuses. Non-board types and empty entries are
// skipped. Returns nil when no board-enabled type declares any statuses.
func boardStatusesFromCompiled(compiled *CompiledSchemaTypes) []string {
	if compiled == nil {
		return nil
	}
	var out []string
	seen := make(map[string]struct{})
	for _, t := range visibleCompiledTypes(compiled.ObjectTypes) {
		if !t.BoardEnabled {
			continue
		}
		for _, s := range t.AllowedStatuses {
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// buildBoardColumns groups items into columns by status in the schema-declared
// order (falling back to the canonical order when schemaStatuses is empty),
// appending any custom statuses (sorted) after the known ones. Canonical lanes
// render even when empty so the board's drag targets are stable.
func buildBoardColumns(items []WorkItem, schemaStatuses []string) []boardLane {
	byStatus := make(map[string][]WorkItem)
	for _, it := range items {
		byStatus[it.Status] = append(byStatus[it.Status], it)
	}
	base := schemaStatuses
	if len(base) == 0 {
		base = boardStatusOrder
	}
	seen := make(map[string]bool)
	cols := make([]boardLane, 0, len(base)+len(byStatus))
	for _, s := range base {
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

// boardStatuses best-effort derives the board lane order from the compiled
// schema. A fetch failure is captured and falls back to nil (canonical order).
func (s *Server) boardStatuses(ctx context.Context) []string {
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		captureError(err)
		return nil
	}
	return boardStatusesFromCompiled(compiled)
}

// uiBoard renders the Kanban board page. A failed fetch renders the whole-page
// error state.
func (s *Server) uiBoard(c echo.Context) error {
	ctx := c.Request().Context()
	items, err := s.memory.ListWorkItems(ctx, "", "", 200)
	if err != nil {
		return s.page(c, pageTitle("Board"), BoardPage(nil, nil, nil, defaultBoardStatusMap(), err))
	}
	agents, aerr := s.memory.ListScheduledAgents(ctx)
	if aerr != nil {
		captureError(aerr)
		agents = nil
	}
	return s.page(c, pageTitle("Board"), BoardPage(items, agents, s.boardStatuses(ctx), s.boardStatusMap(ctx), nil))
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
	render.RenderPartial(c.Response().Writer, c.Request(), BoardRefresh(items, s.boardStatuses(ctx), s.boardStatusMap(ctx), errMsg))
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
	render.RenderPartial(c.Response().Writer, c.Request(), BoardDrawer(detail, s.boardStatusMap(ctx)))
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
