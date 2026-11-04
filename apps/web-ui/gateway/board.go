package main

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// boardStatusOrder is the canonical column order for the board. Statuses not in
// this list (custom per-agent mappings) are appended after it, sorted, so the
// board always renders a stable, predictable set of lanes.
var boardStatusOrder = []string{"ready", "in_progress", "review", "revision", "blocked", "done"}

// boardItemLimit caps how many work items the board fetches in one page. There
// is no pagination yet, so a capped board renders an explicit notice
// (board.templ) rather than silently hiding older items.
const boardItemLimit = 200

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

// boardAllowances maps an object type to the work statuses its type config
// allows a move to. Cards carry their own type's set (data-board-allowed), so the
// drag affordance can leave out a lane the type would refuse instead of offering
// a target that always fails: the lanes are the union of every board-enabled
// type's statuses, while a write is validated against the item's own type.
// Types with no declared set are absent, and their cards stay unconstrained.
type boardAllowances map[string][]string

// boardAllowancesFromCompiled derives the per-type allowed status sets from the
// compiled schema, under the same visibility rule as the lane order (a shadowed
// declaration cannot leak a stale set). It mirrors the write side: only a
// board-enabled type with at least one declared status constrains anything,
// because an unconstrained type accepts any status.
func boardAllowancesFromCompiled(compiled *CompiledSchemaTypes) boardAllowances {
	if compiled == nil {
		return nil
	}
	out := make(boardAllowances, len(compiled.ObjectTypes))
	for _, t := range visibleCompiledTypes(compiled.ObjectTypes) {
		if !t.BoardEnabled || len(t.AllowedStatuses) == 0 {
			continue
		}
		allowed := make([]string, 0, len(t.AllowedStatuses))
		seen := make(map[string]struct{}, len(t.AllowedStatuses))
		for _, s := range t.AllowedStatuses {
			if s == "" {
				continue
			}
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			allowed = append(allowed, s)
		}
		if len(allowed) > 0 {
			out[t.Name] = allowed
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// allowed returns the statuses an object of typeName may be moved to, or nil when
// its type is unconstrained — every lane then offers a move, as before.
func (a boardAllowances) allowed(typeName string) []string {
	if len(a) == 0 {
		return nil
	}
	return a[typeName]
}

// lanes filters the board lane order down to the statuses the item's type allows.
// It backs the drawer's "Set status" menu: offering a status the type refuses is
// the same dead end the drop targets avoid. When nothing intersects — a type whose
// declared set the board does not render — the list is returned unchanged rather
// than emptied, so a rendering gap cannot silently remove the control.
func (a boardAllowances) lanes(statuses []string, typeName string) []string {
	allowed := a.allowed(typeName)
	if len(allowed) == 0 {
		return statuses
	}
	set := make(map[string]struct{}, len(allowed))
	for _, s := range allowed {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		if _, ok := set[s]; ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return statuses
	}
	return out
}

// allowedJSON renders the statuses an object of typeName may be moved to as a
// JSON array for the card's data-board-allowed attribute, or "" when its type is
// unconstrained (the attribute is then omitted). JSON rather than a comma list so
// a custom status containing a comma or a space survives the trip.
func (a boardAllowances) allowedJSON(typeName string) string {
	allowed := a.allowed(typeName)
	if len(allowed) == 0 {
		return ""
	}
	raw, err := json.Marshal(allowed)
	if err != nil {
		return ""
	}
	return string(raw)
}

// boardSchema best-effort derives the board's lane order and per-type allowed
// statuses from one compiled-schema fetch. A fetch failure is captured and yields
// nil for both (canonical lanes, unconstrained cards), so the board never breaks
// on a schema outage.
func (s *Server) boardSchema(ctx context.Context) ([]string, boardAllowances) {
	compiled, err := s.memory.GetCompiledTypes(ctx)
	if err != nil {
		captureError(err)
		return nil, nil
	}
	return boardStatusesFromCompiled(compiled), boardAllowancesFromCompiled(compiled)
}

// uiBoard renders the Kanban board page. A failed fetch renders the whole-page
// error state.
func (s *Server) uiBoard(c echo.Context) error {
	ctx := c.Request().Context()
	items, err := s.memory.ListWorkItems(ctx, "", "", boardItemLimit)
	if err != nil {
		return s.page(c, pageTitle("Board"), BoardPage(nil, nil, nil, nil, defaultBoardStatusMap(), userFacingError(err)))
	}
	agents, aerr := s.memory.ListScheduledAgents(ctx)
	if aerr != nil {
		captureError(aerr)
		agents = nil
	}
	statuses, allowances := s.boardSchema(ctx)
	return s.page(c, pageTitle("Board"), BoardPage(items, agents, statuses, allowances, s.boardStatusMap(ctx), nil))
}

// uiBoardPartial renders just the board columns (the htmx swap region), used by
// background refreshes (drag cancellation replay) and manual reloads. It does
// NOT include the drawer out-of-band clear: a background refresh must never
// close a drawer the user is typing in.
func (s *Server) uiBoardPartial(c echo.Context) error {
	return s.renderBoardPartial(c, nil)
}

// renderBoardColumns fetches the current work items and renders the action
// response: the columns plus the out-of-band drawer clear, so an action taken
// from the drawer closes it. actionErr, when non-nil, becomes the error banner.
func (s *Server) renderBoardColumns(c echo.Context, actionErr error) error {
	ctx := c.Request().Context()
	items, err := s.memory.ListWorkItems(ctx, "", "", boardItemLimit)
	errMsg := ""
	if actionErr != nil {
		errMsg = actionErr.Error()
	} else if err != nil {
		errMsg = err.Error()
	}
	statuses, allowances := s.boardSchema(ctx)
	renderPartial(c.Response().Writer, c.Request(), BoardRefresh(items, statuses, allowances, s.boardStatusMap(ctx), errMsg))
	return nil
}

// renderBoardPartial is renderBoardColumns without the drawer swap: the
// response for background refreshes (the live stream's catch-up and a replay
// after a cancelled drag).
func (s *Server) renderBoardPartial(c echo.Context, actionErr error) error {
	ctx := c.Request().Context()
	items, err := s.memory.ListWorkItems(ctx, "", "", boardItemLimit)
	errMsg := ""
	if actionErr != nil {
		errMsg = actionErr.Error()
	} else if err != nil {
		errMsg = err.Error()
	}
	statuses, allowances := s.boardSchema(ctx)
	renderPartial(c.Response().Writer, c.Request(), BoardColumnsFrame(items, statuses, allowances, s.boardStatusMap(ctx), errMsg))
	return nil
}

// boardDeliverable is one declared deliverable on a completed run, resolved to
// its graph object when a lookup succeeds. Type/Key come straight from the run
// summary; ObjectID/Resolved are filled by the resolution pass.
type boardDeliverable struct {
	Type     string
	Key      string
	ObjectID string
	Resolved bool
}

// boardDeliverables flattens a work item's runs (newest first, as the server
// returns them) into de-duplicated deliverables, newest declaration first. Each
// run's Summary["deliverables"] is decoded defensively from raw JSON ([]any of
// map[string]any with string type/key); malformed entries are dropped and a
// missing/renamed key is tolerated, so the helper never panics.
func boardDeliverables(runs []WorkItemRun) []boardDeliverable {
	seen := make(map[string]bool)
	var out []boardDeliverable
	for _, run := range runs {
		for _, d := range decodeBoardDeliverables(run.Summary) {
			key := d.Type + "\x00" + d.Key
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, d)
		}
	}
	return out
}

// decodeBoardDeliverables decodes one run summary's deliverables array into
// boardDeliverable values, dropping entries that are not {type, key} string
// pairs.
func decodeBoardDeliverables(summary map[string]any) []boardDeliverable {
	raw, ok := summary["deliverables"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]boardDeliverable, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		k, _ := m["key"].(string)
		if t == "" || k == "" {
			continue
		}
		out = append(out, boardDeliverable{Type: t, Key: k})
	}
	return out
}

// boardCompletionSummary returns the newest run's completion note (its
// Summary["summary"] prose), or "" when absent.
func boardCompletionSummary(runs []WorkItemRun) string {
	if len(runs) == 0 {
		return ""
	}
	s, _ := runs[0].Summary["summary"].(string)
	return s
}

// boardComment is one entry in the merged "Activity" thread: a durable comment
// (Kind == "comment") or a review feedback round (Kind == "feedback").
type boardComment struct {
	Kind      string
	Author    string
	Body      string
	Round     int    // feedback rounds only
	ActorType string // comments only
	CreatedAt string
}

// boardComments merges a work item's durable comments and review feedback
// rounds into one thread sorted by createdAt ascending. Both createdAt values
// are RFC3339 strings, whose lexicographic order equals chronological order.
//
// createdAt is second-precision, so a comment and a feedback round recorded in
// the same second tie. The sort is fully ordered — not merely stable — by falling
// back to (kind, round, author, body), so a tie resolves by the entry's own
// content rather than by which slice it came from; two entries with identical
// content are indistinguishable anyway. "comment" < "feedback" lexicographically,
// so a same-second comment precedes a feedback round.
func boardComments(comments []WorkItemComment, feedback []WorkItemFeedback) []boardComment {
	out := make([]boardComment, 0, len(comments)+len(feedback))
	for _, c := range comments {
		out = append(out, boardComment{Kind: "comment", Author: c.Author, Body: c.Body, ActorType: c.ActorType, CreatedAt: c.CreatedAt})
	}
	for _, f := range feedback {
		out = append(out, boardComment{Kind: "feedback", Author: f.Author, Body: f.Text, Round: f.Round, CreatedAt: f.CreatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return boardCommentLess(out[i], out[j]) })
	return out
}

// boardCommentLess is the merged thread's total order: createdAt first, then
// (kind, round, author, body) so equal-second entries still have one
// deterministic position.
func boardCommentLess(a, b boardComment) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt < b.CreatedAt
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Round != b.Round {
		return a.Round < b.Round
	}
	if a.Author != b.Author {
		return a.Author < b.Author
	}
	return a.Body < b.Body
}

// uiBoardItem renders the card drawer (a native dialog) for one work item. It
// resolves each declared deliverable to its object (best-effort: a miss or
// lookup failure leaves that row as missing) and passes the result plus the
// newest run's completion note into the drawer.
func (s *Server) uiBoardItem(c echo.Context) error {
	ctx := c.Request().Context()
	canonicalID := c.Param("canonicalId")
	detail, err := s.memory.GetWorkItem(ctx, canonicalID)
	if err != nil {
		renderPartial(c.Response().Writer, c.Request(), BoardDrawerError(err))
		return nil
	}
	deliverables := boardDeliverables(detail.Runs)
	for i := range deliverables {
		obj, lerr := s.memory.LookupGraphObjectByKey(ctx, deliverables[i].Type, deliverables[i].Key)
		if lerr != nil {
			captureError(lerr)
			continue
		}
		if obj == nil {
			continue
		}
		deliverables[i].Resolved = true
		deliverables[i].ObjectID = firstNonEmpty(obj.CanonicalID, obj.ID)
	}
	statuses, allowances := s.boardSchema(ctx)
	renderPartial(c.Response().Writer, c.Request(), BoardDrawer(detail, s.boardStatusMap(ctx), allowances.lanes(statuses, detail.Item.Type), deliverables, boardCompletionSummary(detail.Runs), s.boardAssigneeCandidates(ctx)))
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

func (s *Server) uiBoardComment(c echo.Context) error {
	_, err := s.memory.AddWorkComment(c.Request().Context(), c.Param("canonicalId"), c.FormValue("body"))
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

func (s *Server) uiBoardSetStatus(c echo.Context) error {
	_, err := s.memory.SetWorkItemStatus(c.Request().Context(), c.Param("canonicalId"), c.FormValue("status"))
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

// boardAssigneeCandidate is one option in the ticket-style assignee picker.
type boardAssigneeCandidate struct {
	Value     string // the exact assignee value posted to the reassign endpoint
	Label     string // display name
	AvatarURL string // member avatar URL; empty for agents (initials fallback)
	IsAgent   bool   // agents tint secondary; members use their avatar
}

// buildBoardAssigneeCandidates builds the assignee picker options: runtime
// agents by exact Name, then project members by DisplayName (falling back to
// Email). The "Unassigned" (empty) entry is rendered by the picker itself, not
// here.
func buildBoardAssigneeCandidates(agents []ScheduledAgent, members []ProjectMemberDto) []boardAssigneeCandidate {
	out := make([]boardAssigneeCandidate, 0, len(agents)+len(members))
	for _, a := range agents {
		if a.Name == "" {
			continue
		}
		out = append(out, boardAssigneeCandidate{Value: a.Name, Label: a.Name, IsAgent: true})
	}
	for _, m := range members {
		label := m.DisplayName
		if label == "" {
			label = m.Email
		}
		if label == "" {
			continue
		}
		out = append(out, boardAssigneeCandidate{Value: label, Label: label, AvatarURL: m.AvatarURL})
	}
	return out
}

// boardAssigneeCandidates fetches the assignee picker candidates: runtime agents
// and project members. Failures degrade to empty lists so the picker never
// breaks the board.
func (s *Server) boardAssigneeCandidates(ctx context.Context) []boardAssigneeCandidate {
	agents, aerr := s.memory.ListScheduledAgents(ctx)
	if aerr != nil {
		captureError(aerr)
		agents = nil
	}
	members, merr := s.memory.ListMembers(ctx)
	if merr != nil {
		captureError(merr)
		members = nil
	}
	return buildBoardAssigneeCandidates(agents, members)
}

// boardStatusOptions returns the work-path status values (the statusMap
// vocabulary) a user may set an item to, in canonical phase order, excluding the
// item's current status.
func boardStatusOptions(statusMap boardStatusMap, lanes []string, current string) []string {
	out := make([]string, 0, len(lanes)+6)
	seen := make(map[string]bool, len(lanes)+6)
	add := func(v string) {
		if v == "" || v == current || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	// Prefer the board's rendered lanes: they are the statuses this project
	// actually uses, so the menu offers exactly the columns a user can see (and
	// drag to). The canonical vocabulary is only a fallback for a board that
	// rendered no lanes.
	if len(lanes) > 0 {
		for _, v := range lanes {
			add(v)
		}
		return out
	}
	for _, v := range []string{
		statusMap.Ready,
		statusMap.InProgress,
		statusMap.Review,
		statusMap.Revision,
		statusMap.Blocked,
		statusMap.Done,
	} {
		add(v)
	}
	return out
}
