package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

// Work section keys (global, project-scoped).
const (
	workBoardSection    = "board"
	workSettingsSection = "settings"
)

var workSections = []string{workBoardSection, workSettingsSection}

// workData is the payload for the global Work Board / Settings pages.
type workData struct {
	Section           string
	Agents            []AgentDefinitionSummary
	Definitions       map[string]*AgentDefinition
	ObjectTypes       []CompiledType
	RelationshipTypes []CompiledType
	BoardTypes        []CompiledType
	SelectedTypes     []string
	TypeCounts        map[string]int
	Matrices          []workMatrix
	FilterExplicit    bool
	ScopeBase         string
	LaneOpen          map[string]bool
	Items             []WorkItem
	Allowances        boardAllowances
	Rules             []workRuleRow
	FlashMsg          string
	FlashErr          error
	LoadErr           error
}

// workRuleRow is one (agent, object type) rule in the aggregated table.
type workRuleRow struct {
	AgentID    string
	AgentName  string
	ObjectType string
	Rule       AgentObjectRule
}

// uiWork redirects the bare Work route to Board.
func (s *Server) uiWork(c echo.Context) error {
	return c.Redirect(http.StatusSeeOther, "/work/"+workBoardSection)
}

// uiWorkSection renders Work → Board or Work → Settings.
func (s *Server) uiWorkSection(c echo.Context) error {
	section := c.Param("section")
	if !containsString(workSections, section) {
		return c.NoContent(http.StatusNotFound)
	}
	data := workData{Section: section}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Work updated."
	}
	data.FlashErr = flashError(c)
	if err := s.loadWorkData(c.Request().Context(), c, &data); err != nil {
		data.LoadErr = userFacingError(err)
	}
	return s.page(c, pageTitle("Work"), WorkPage(data))
}

// loadWorkData loads agents, compiled types, rules, and (for Board) items.
func (s *Server) loadWorkData(ctx context.Context, c echo.Context, data *workData) error {
	defs, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return err
	}
	data.Agents = defs
	data.Definitions = map[string]*AgentDefinition{}
	for _, d := range defs {
		full, derr := s.memory.GetAgentDefinition(ctx, d.ID)
		if derr != nil || full == nil {
			continue
		}
		data.Definitions[d.ID] = full
		data.Rules = append(data.Rules, rulesForDefinition(full)...)
	}
	sort.Slice(data.Rules, func(i, j int) bool {
		if data.Rules[i].AgentName != data.Rules[j].AgentName {
			return data.Rules[i].AgentName < data.Rules[j].AgentName
		}
		return data.Rules[i].ObjectType < data.Rules[j].ObjectType
	})

	var compiled *CompiledSchemaTypes
	if c, err := s.memory.GetCompiledTypes(ctx); err != nil {
		captureError(err)
	} else {
		compiled = c
	}
	if compiled != nil {
		data.ObjectTypes = compiled.ObjectTypes
		data.RelationshipTypes = compiled.RelationshipTypes
	}
	data.BoardTypes = boardTypesFromCompiled(compiled)
	data.Allowances = boardAllowancesFromCompiled(compiled)
	data.ScopeBase = linkBaseFromContext(ctx)
	data.LaneOpen = workLanePrefs(c, data.ScopeBase)

	if data.Section != workBoardSection {
		return nil
	}
	items, err := s.memory.ListWorkItems(ctx, "", "", boardItemLimit)
	if err != nil {
		return err
	}
	data.Items = items
	q := c.Request().URL.Query()
	data.FilterExplicit = q.Get("filter") == "1"
	data.SelectedTypes = resolveWorkTypeFilter(data.BoardTypes, q["type"], data.FilterExplicit)
	data.TypeCounts = map[string]int{}
	for _, it := range items {
		data.TypeCounts[it.Type]++
	}
	selected := map[string]bool{}
	for _, name := range data.SelectedTypes {
		selected[name] = true
	}
	for _, t := range data.BoardTypes {
		if !selected[t.Name] {
			continue
		}
		if !data.FilterExplicit && data.TypeCounts[t.Name] == 0 {
			continue // default view hides empty types
		}
		data.Matrices = append(data.Matrices, buildWorkMatrix(t.Name, items, compiled))
	}
	return nil
}

// rulesForDefinition flattens one definition's rules.
func rulesForDefinition(d *AgentDefinition) []workRuleRow {
	if d == nil || d.WorkConfig == nil {
		return nil
	}
	out := make([]workRuleRow, 0, len(d.WorkConfig.ObjectRules))
	for _, r := range d.WorkConfig.ObjectRules {
		out = append(out, workRuleRow{AgentID: d.ID, AgentName: d.Name, ObjectType: r.ObjectType, Rule: r})
	}
	return out
}

// resolveWorkTypeFilter resolves the board's type selection from the query.
// With no explicit filter the board shows every board type (checked in the
// dropdown); an explicit filter is the validated, de-duplicated subset. An
// explicit filter with no type params selects nothing.
func resolveWorkTypeFilter(boardTypes []CompiledType, raw []string, explicit bool) []string {
	if !explicit {
		out := make([]string, 0, len(boardTypes))
		for _, t := range boardTypes {
			out = append(out, t.Name)
		}
		return out
	}
	known := make(map[string]bool, len(boardTypes))
	for _, t := range boardTypes {
		known[t.Name] = true
	}
	seen := make(map[string]bool, len(raw))
	var out []string
	for _, name := range raw {
		if name == "" || !known[name] || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// --- matrix ---

type workMatrix struct {
	Type    string
	Columns []workColumn
	Groups  []workGroup
	Total   int
}

type workColumn struct {
	Status string
	Label  string
	Count  int
}

type workGroup struct {
	Key   string // active | attention | idle
	Label string
	Rows  []workRow
}

type workRow struct {
	Key   string // working | queued | retrying | blocked | review
	Label string
	// Cells is keyed by status.
	Cells map[string][]WorkItem
	Count int
}

// buildWorkMatrix buckets one type's items into status columns and
// active/attention/idle rows. Every item appears once.
func buildWorkMatrix(typeName string, items []WorkItem, compiled *CompiledSchemaTypes) workMatrix {
	m := workMatrix{Type: typeName}
	if typeName == "" {
		return m
	}
	var typeDef *CompiledType
	if compiled != nil {
		for i := range compiled.ObjectTypes {
			t := compiled.ObjectTypes[i]
			if t.Name == typeName && !t.Shadowed {
				typeDef = &t
				break
			}
		}
	}
	statuses := boardStatusOrder
	if typeDef != nil {
		statuses = boardLanesForType(*typeDef)
	}
	known := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		known[s] = true
	}
	byStatus := map[string]int{}
	rows := map[string]map[string][]WorkItem{
		"working": {}, "queued": {}, "retrying": {}, "blocked": {}, "review": {}, "idle": {},
	}
	for _, it := range items {
		if it.Type != typeName {
			continue
		}
		// Statuses outside the type's declared vocabulary (including an empty
		// status) collapse into the first "No status" column rather than
		// inventing new lanes.
		key := it.Status
		if !known[key] {
			key = ""
		}
		m.Total++
		byStatus[key]++
		rows[bucketFor(it)][key] = append(rows[bucketFor(it)][key], it)
	}
	if byStatus[""] > 0 {
		m.Columns = append(m.Columns, workColumn{Status: "", Label: "No status", Count: byStatus[""]})
	}
	for _, s := range statuses {
		m.Columns = append(m.Columns, workColumn{Status: s, Label: s, Count: byStatus[s]})
	}
	m.Groups = []workGroup{
		{Key: "active", Label: "Active", Rows: []workRow{
			{Key: "working", Label: "Working", Cells: rows["working"]},
			{Key: "queued", Label: "Queued", Cells: rows["queued"]},
			{Key: "retrying", Label: "Retrying", Cells: rows["retrying"]},
		}},
		{Key: "attention", Label: "Attention", Rows: []workRow{
			{Key: "blocked", Label: "Blocked", Cells: rows["blocked"]},
			{Key: "review", Label: "Needs review", Cells: rows["review"]},
		}},
		{Key: "idle", Label: "Idle", Rows: []workRow{
			{Key: "idle", Label: "Idle", Cells: rows["idle"]},
		}},
	}
	for gi := range m.Groups {
		for ri := range m.Groups[gi].Rows {
			r := &m.Groups[gi].Rows[ri]
			for _, cell := range r.Cells {
				r.Count += len(cell)
			}
		}
	}
	return m
}

// workLanePrefs reads the persisted open/closed matrix lane preferences from
// the work_lanes cookie. Keys are "<scope>|<type>|<group>"; a missing key uses
// the default (Active/Attention open, Idle closed).
func workLanePrefs(c echo.Context, scope string) map[string]bool {
	out := map[string]bool{}
	ck, err := c.Cookie(workLanesCookie)
	if err != nil || ck.Value == "" {
		return out
	}
	raw, err := url.QueryUnescape(ck.Value)
	if err != nil {
		return out
	}
	var stored map[string]bool
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return out
	}
	prefix := scope + "|"
	for k, v := range stored {
		if strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out
}

// workLaneKey is the stable preference key for one type's execution group.
func workLaneKey(data workData, objectType, group string) string {
	return data.ScopeBase + "|" + objectType + "|" + group
}

// workLaneOpen reports whether one matrix group should render expanded.
func workLaneOpen(data workData, objectType, group string) bool {
	if v, ok := data.LaneOpen[workLaneKey(data, objectType, group)]; ok {
		return v
	}
	return group != "idle"
}

// workLanesCookie persists per-user matrix lane open/closed preferences.
const workLanesCookie = "memory_work_lanes"

// workStatusAccentClass is the card's left-border color for a workflow status.
func workStatusAccentClass(status string) string {
	switch status {
	case "done":
		return "border-l-success"
	case "review":
		return "border-l-warning"
	case "revision":
		return "border-l-accent"
	case "in_progress":
		return "border-l-info"
	case "ready":
		return "border-l-primary"
	case "blocked":
		return "border-l-error"
	default:
		return "border-l-base-content/20"
	}
}

// workStatusDotClass is the column-header dot color for a workflow status.
func workStatusDotClass(status string) string {
	switch status {
	case "done":
		return "bg-success"
	case "review":
		return "bg-warning"
	case "revision":
		return "bg-accent"
	case "in_progress":
		return "bg-info"
	case "ready":
		return "bg-primary"
	case "blocked":
		return "bg-error"
	default:
		return "bg-base-content/25"
	}
}

// workTypeByName returns a board type by name, or a bare CompiledType.
func workTypeByName(types []CompiledType, name string) CompiledType {
	for _, t := range types {
		if t.Name == name {
			return t
		}
	}
	return CompiledType{Name: name}
}

// workTypeLabel returns a type's display label, falling back to its name.
func workTypeLabel(t CompiledType, fallback string) string {
	if t.Label != "" {
		return t.Label
	}
	if t.Name != "" {
		return t.Name
	}
	return fallback
}

// workTypeChecked reports whether a type is selected in the filter. With no
// explicit filter the dropdown shows every type checked.
func workTypeChecked(data workData, name string) bool {
	if !data.FilterExplicit {
		return true
	}
	return slices.Contains(data.SelectedTypes, name)
}

// workFilterTypes returns the explicit selection for action forms; nil when the
// board is showing all types.
func workFilterTypes(data workData) []string {
	if !data.FilterExplicit {
		return nil
	}
	return data.SelectedTypes
}

// workRuleEditHref builds the edit-page URL for one rule.
func workRuleEditHref(r workRuleRow) string {
	q := url.Values{}
	q.Set("agent", r.AgentID)
	q.Set("type", r.ObjectType)
	return "/work/settings/rules/edit?" + q.Encode()
}

// workAgentNameByID resolves an agent's display name, falling back to the id.
func workAgentNameByID(agents []AgentDefinitionSummary, id string) string {
	for _, a := range agents {
		if a.ID == id {
			return a.Name
		}
	}
	return id
}

// workGroupCount sums a group's row counts for its header.
func workGroupCount(g workGroup) int {
	n := 0
	for _, r := range g.Rows {
		n += r.Count
	}
	return n
}

// workColumnItems flattens one status column across the matrix rows, preserving
// group/row order.
func workColumnItems(m workMatrix, status string) []WorkItem {
	var out []WorkItem
	for _, g := range m.Groups {
		for _, row := range g.Rows {
			out = append(out, row.Cells[status]...)
		}
	}
	return out
}

// bucketFor assigns an item to exactly one execution/attention row.
func bucketFor(it WorkItem) string {
	if it.Blocked {
		return "blocked"
	}
	if it.IsWorking {
		if it.LatestRunStatus == "queued" && it.NextRunAt != "" {
			return "retrying"
		}
		if it.LatestRunStatus == "queued" {
			return "queued"
		}
		return "working"
	}
	if it.NeedsReview {
		return "review"
	}
	return "idle"
}

// --- actions ---

// workRuleFormData is the payload for the New/Edit rule pages.
type workRuleFormData struct {
	Mode            string // new | edit
	Agents          []AgentDefinitionSummary
	ObjectTypes     []CompiledType
	SelectedAgentID string
	SelectedType    string
	Rule            AgentObjectRule
	FlashErr        error
}

// uiWorkRuleNew renders the create-rule page.
func (s *Server) uiWorkRuleNew(c echo.Context) error {
	rule := AgentObjectRule{
		CanCreate: boolPtr(true),
		CanUpdate: boolPtr(true),
		CanDelete: boolPtr(true),
	}
	return s.renderWorkRuleForm(c, "new", c.QueryParam("agent"), c.QueryParam("type"), rule, nil)
}

// uiWorkRuleEdit renders the edit-rule page for an existing (agent, type) rule.
func (s *Server) uiWorkRuleEdit(c echo.Context) error {
	ctx := c.Request().Context()
	agentID := c.QueryParam("agent")
	objectType := c.QueryParam("type")
	if agentID == "" || objectType == "" {
		return redirectWithError(c, "/work/"+workSettingsSection, fmt.Errorf("agent and object type are required"))
	}
	def, err := s.memory.GetAgentDefinition(ctx, agentID)
	if err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	rule := def.WorkConfig.RuleFor(objectType)
	if rule == nil {
		return c.NoContent(http.StatusNotFound)
	}
	return s.renderWorkRuleForm(c, "edit", agentID, objectType, *rule, nil)
}

// renderWorkRuleForm loads the selects and renders the shared rule form.
func (s *Server) renderWorkRuleForm(c echo.Context, mode, agentID, objectType string, rule AgentObjectRule, flashErr error) error {
	ctx := c.Request().Context()
	agents, err := s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	var types []CompiledType
	if compiled, err := s.memory.GetCompiledTypes(ctx); err == nil && compiled != nil {
		types = visibleCompiledTypes(compiled.ObjectTypes)
	}
	// Keep an existing selection visible even if the type is no longer compiled.
	if objectType != "" && !slices.ContainsFunc(types, func(t CompiledType) bool { return t.Name == objectType }) {
		types = append(types, CompiledType{Name: objectType})
	}
	title := "New rule"
	if mode == "edit" {
		title = "Edit rule"
	}
	return s.page(c, pageTitle(title), WorkRulePage(workRuleFormData{
		Mode:            mode,
		Agents:          agents,
		ObjectTypes:     types,
		SelectedAgentID: agentID,
		SelectedType:    objectType,
		Rule:            rule,
		FlashErr:        flashErr,
	}))
}

// uiWorkRuleSave creates or replaces one (agent, object type) rule.
func (s *Server) uiWorkRuleSave(c echo.Context) error {
	ctx := c.Request().Context()
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	mode := strings.TrimSpace(c.FormValue("mode"))
	if mode == "" {
		mode = "edit"
	}
	agentID := strings.TrimSpace(c.FormValue("agentId"))
	objectType := strings.TrimSpace(c.FormValue("ruleObjectType"))
	rule := parseRuleRow(c, objectType)
	if agentID == "" || objectType == "" {
		return s.renderWorkRuleForm(c, mode, agentID, objectType, rule, fmt.Errorf("agent and object type are required"))
	}
	def, err := s.memory.GetAgentDefinition(ctx, agentID)
	if err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if def.WorkConfig == nil {
		def.WorkConfig = &AgentWorkConfig{}
	}
	existing := def.WorkConfig.RuleFor(objectType)
	if (mode == "create" || mode == "new") && existing != nil {
		return s.renderWorkRuleForm(c, mode, agentID, objectType, rule, fmt.Errorf("a rule for %s and %s already exists — edit it instead", def.Name, objectType))
	}
	if mode == "edit" && existing == nil {
		return s.renderWorkRuleForm(c, mode, agentID, objectType, rule, fmt.Errorf("the rule no longer exists"))
	}
	replaced := false
	for i := range def.WorkConfig.ObjectRules {
		if def.WorkConfig.ObjectRules[i].ObjectType == objectType {
			def.WorkConfig.ObjectRules[i] = rule
			replaced = true
			break
		}
	}
	if !replaced {
		def.WorkConfig.ObjectRules = append(def.WorkConfig.ObjectRules, rule)
	}
	if compiled, cerr := s.memory.GetCompiledTypes(ctx); cerr == nil {
		if verr := validateGatewayWorkRules(def, compiled); verr != nil {
			return s.renderWorkRuleForm(c, mode, agentID, objectType, rule, verr)
		}
	}
	deriveDelegation(def)
	if err := applyDelegation(def); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if _, err := s.memory.UpdateAgentDefinition(ctx, agentID, def); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	return c.Redirect(http.StatusSeeOther, "/work/"+workSettingsSection+"?updated=1")
}

// uiWorkRuleDelete removes one rule row.
func (s *Server) uiWorkRuleDelete(c echo.Context) error {
	ctx := c.Request().Context()
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	agentID := strings.TrimSpace(c.FormValue("agentId"))
	objectType := strings.TrimSpace(c.FormValue("objectType"))
	def, err := s.memory.GetAgentDefinition(ctx, agentID)
	if err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if def.WorkConfig != nil {
		out := def.WorkConfig.ObjectRules[:0]
		for _, r := range def.WorkConfig.ObjectRules {
			if r.ObjectType != objectType {
				out = append(out, r)
			}
		}
		def.WorkConfig.ObjectRules = out
	}
	deriveDelegation(def)
	if err := applyDelegation(def); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if _, err := s.memory.UpdateAgentDefinition(ctx, agentID, def); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	return c.Redirect(http.StatusSeeOther, "/work/"+workSettingsSection+"?updated=1")
}

// uiWorkAgentDefaults saves scope + guardrails for one agent without touching
// its per-type rules.
func (s *Server) uiWorkAgentDefaults(c echo.Context) error {
	ctx := c.Request().Context()
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	agentID := strings.TrimSpace(c.FormValue("agentId"))
	def, err := s.memory.GetAgentDefinition(ctx, agentID)
	if err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if def.WorkConfig == nil {
		def.WorkConfig = &AgentWorkConfig{}
	}
	def.WorkConfig.RequiresReview = lastFormBool(c, "workRequiresReview")
	if def.WorkConfig.FailureLimit, err = workIntField(c, "failureLimit", "Failure limit", 0); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if def.WorkConfig.RevisionLimit, err = workIntField(c, "revisionLimit", "Revision limit", 0); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if def.WorkConfig.RetryPolicy.MaxAttempts, err = workIntField(c, "maxAttempts", "Max attempts", 1); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	def.WorkConfig.WorkContract.RequireArtifacts = lastFormBool(c, "workRequireArtifacts")
	def.WorkConfig.WorkContract.RequiredDeliverableTypes = splitCSV(c.FormValue("requiredDeliverableType"))
	if err := applyAgentGraphSection(def, c); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	deriveDelegation(def)
	if err := applyDelegation(def); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if _, err := s.memory.UpdateAgentDefinition(ctx, agentID, def); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	return c.Redirect(http.StatusSeeOther, "/work/"+workSettingsSection+"?updated=1")
}

// uiWorkUnblock clears a block from the board.
func (s *Server) uiWorkUnblock(c echo.Context) error {
	type unblocker interface {
		UnblockWorkItem(ctx context.Context, canonicalID, note string) (*WorkItemAction, error)
	}
	note := strings.TrimSpace(c.FormValue("note"))
	_ = c.Request().ParseForm()
	returnTo := workBoardReturnPath(c.Request().Form["type"])
	u, ok := s.memory.(unblocker)
	if !ok {
		return redirectWithError(c, returnTo, fmt.Errorf("unblock is not supported by this backend"))
	}
	if _, err := u.UnblockWorkItem(c.Request().Context(), c.Param("canonicalId"), note); err != nil {
		return redirectWithError(c, returnTo, err)
	}
	return c.Redirect(http.StatusSeeOther, returnTo)
}

// uiWorkSetStatus performs an exact-status move from the Work board drag/drop.
// The matrix reloads itself after a successful move.
func (s *Server) uiWorkSetStatus(c echo.Context) error {
	status := strings.TrimSpace(c.FormValue("status"))
	if status == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "status is required"})
	}
	if _, err := s.memory.SetWorkItemStatus(c.Request().Context(), c.Param("canonicalId"), status); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

// workBoardReturnPath builds the board URL for a set of selected type names,
// preserving an explicit filter; no types means the all-types board.
func workBoardReturnPath(types []string) string {
	var kept []string
	for _, t := range types {
		if t = strings.TrimSpace(t); t != "" {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		return "/work/" + workBoardSection
	}
	q := url.Values{"filter": {"1"}}
	for _, t := range kept {
		q.Add("type", t)
	}
	return "/work/" + workBoardSection + "?" + q.Encode()
}

// parseRuleRow reads one rule's fields from the form.
func parseRuleRow(c echo.Context, objectType string) AgentObjectRule {
	create := lastFormBool(c, "ruleCreate")
	update := lastFormBool(c, "ruleUpdate")
	del := lastFormBool(c, "ruleDelete")
	return AgentObjectRule{
		ObjectType:              objectType,
		CanCreate:               &create,
		CanUpdate:               &update,
		CanDelete:               &del,
		Fields:                  splitCSV(c.FormValue("ruleFields")),
		Triggers:                parseRuleTriggers(c.FormValue("ruleTriggers")),
		Transitions:             parseRuleTransitions(c.FormValue("ruleTransitions")),
		ClearAssigneeOnComplete: lastFormBool(c, "ruleClearAssignee"),
	}
}
