package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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
	SelectedType      string
	Items             []WorkItem
	Matrix            workMatrix
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

	if data.Section != workBoardSection {
		return nil
	}
	items, err := s.memory.ListWorkItems(ctx, "", "", boardItemLimit)
	if err != nil {
		return err
	}
	data.Items = items
	data.SelectedType = c.QueryParam("type")
	if data.SelectedType == "" {
		data.SelectedType = firstTypeWithItems(data.BoardTypes, items)
	}
	data.Matrix = buildWorkMatrix(data.SelectedType, items, compiled)
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

// firstTypeWithItems picks the first type that has items, else the first type.
func firstTypeWithItems(types []CompiledType, items []WorkItem) string {
	have := map[string]bool{}
	for _, it := range items {
		have[it.Type] = true
	}
	for _, t := range types {
		if have[t.Name] {
			return t.Name
		}
	}
	if len(types) > 0 {
		return types[0].Name
	}
	return ""
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
	byStatus := map[string]int{}
	rows := map[string]map[string][]WorkItem{
		"working": {}, "queued": {}, "retrying": {}, "blocked": {}, "review": {}, "idle": {},
	}
	for _, it := range items {
		if it.Type != typeName {
			continue
		}
		m.Total++
		byStatus[it.Status]++
		rows[bucketFor(it)][it.Status] = append(rows[bucketFor(it)][it.Status], it)
	}
	// Keep item-only statuses as extra columns.
	seen := map[string]bool{}
	for _, s := range statuses {
		seen[s] = true
	}
	var extra []string
	for s := range byStatus {
		if !seen[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(extra)
	statuses = append(append([]string{}, statuses...), extra...)
	for _, s := range statuses {
		m.Columns = append(m.Columns, workColumn{Status: s, Count: byStatus[s]})
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

// workTabClass returns the active/inactive tab class for a Work subpage.
func workTabClass(current, target string) string {
	if current == target {
		return "btn btn-sm btn-primary"
	}
	return "btn btn-sm btn-ghost"
}

// workTypeTabClass returns the active/inactive tab class for a type.
func workTypeTabClass(name, selected string) string {
	if name == selected {
		return "btn btn-sm btn-primary"
	}
	return "btn btn-sm btn-ghost"
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

// uiWorkRuleSave creates or replaces one (agent, object type) rule.
func (s *Server) uiWorkRuleSave(c echo.Context) error {
	ctx := c.Request().Context()
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	agentID := strings.TrimSpace(c.FormValue("agentId"))
	objectType := strings.TrimSpace(c.FormValue("ruleObjectType"))
	if agentID == "" || objectType == "" {
		return redirectWithError(c, "/work/"+workSettingsSection, fmt.Errorf("agent and object type are required"))
	}
	def, err := s.memory.GetAgentDefinition(ctx, agentID)
	if err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if def.WorkConfig == nil {
		def.WorkConfig = &AgentWorkConfig{}
	}
	rule := parseRuleRow(c, objectType)
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
	if compiled, err := s.memory.GetCompiledTypes(ctx); err == nil {
		if err := validateGatewayWorkRules(def, compiled); err != nil {
			return redirectWithError(c, "/work/"+workSettingsSection, err)
		}
	}
	deriveDelegation(def)
	if err := applyDelegation(def); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	if _, err := s.memory.UpdateAgentDefinition(ctx, agentID, def); err != nil {
		return redirectWithError(c, "/work/"+workSettingsSection, err)
	}
	return c.Redirect(http.StatusSeeOther, "/work/"+workSettingsSection+"?agent="+url.QueryEscape(agentID)+"&updated=1")
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
	u, ok := s.memory.(unblocker)
	if !ok {
		return redirectWithError(c, "/work/"+workBoardSection, fmt.Errorf("unblock is not supported by this backend"))
	}
	if _, err := u.UnblockWorkItem(c.Request().Context(), c.Param("canonicalId"), note); err != nil {
		return redirectWithError(c, "/work/"+workBoardSection+"?type="+url.QueryEscape(c.QueryParam("type")), err)
	}
	return c.Redirect(http.StatusSeeOther, "/work/"+workBoardSection+"?type="+url.QueryEscape(c.QueryParam("type")))
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
