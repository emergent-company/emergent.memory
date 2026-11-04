package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// Work section keys. Work is a top-level agent section with two subpages.
const (
	workSectionBoard    = "board"
	workSectionSettings = "settings"
)

var agentWorkSections = []string{workSectionBoard, workSectionSettings}

// agentWorkData is the payload for the Work section pages.
type agentWorkData struct {
	Agent             *AgentDefinition
	Section           string
	ObjectTypes       []CompiledType
	RelationshipTypes []CompiledType
	BoardTypes        []CompiledType
	View              boardView
	Items             []WorkItem
	Agents            []ScheduledAgent
	Allowances        boardAllowances
	FlashMsg          string
	FlashErr          error
	LoadErr           error
}

// uiAgentWork redirects the bare Work route to its Board subpage.
func (s *Server) uiAgentWork(c echo.Context) error {
	return c.Redirect(http.StatusSeeOther, "/agents/"+url.PathEscape(c.Param("id"))+"/work/"+workSectionBoard)
}

// uiAgentWorkSection renders Work → Board or Work → Settings.
func (s *Server) uiAgentWorkSection(c echo.Context) error {
	section := c.Param("section")
	if !containsString(agentWorkSections, section) {
		return c.NoContent(http.StatusNotFound)
	}
	data := agentWorkData{Section: section}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Agent work settings updated."
	}
	data.FlashErr = flashError(c)
	if err := s.loadAgentWorkData(c.Request().Context(), c.Param("id"), &data); err != nil {
		data.LoadErr = userFacingError(err)
	}
	return s.page(c, pageTitle(data.AgentName(), "Work"), AgentWorkPage(data))
}

// AgentName returns the agent name or a fallback for the page title.
func (d agentWorkData) AgentName() string {
	if d.Agent != nil && d.Agent.Name != "" {
		return d.Agent.Name
	}
	return "Agent"
}

// loadAgentWorkData loads the definition, compiled types, and (for Board) the
// agent-scoped work items plus the per-type swimlanes.
func (s *Server) loadAgentWorkData(ctx context.Context, id string, data *agentWorkData) error {
	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return err
	}
	deriveDelegation(agent)
	data.Agent = agent

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
	data.BoardTypes = agentRuleTypes(agent, compiled)
	data.Allowances = boardAllowancesFromCompiled(compiled)

	if data.Section != workSectionBoard {
		return nil
	}
	items, err := s.memory.ListWorkItems(ctx, "", "", boardItemLimit)
	if err != nil {
		return err
	}
	items = filterWorkItemsForAgent(items, agent)
	data.Items = items
	defs, derr := s.memory.ListAgentDefinitions(ctx)
	if derr != nil {
		captureError(derr)
	}
	view := buildBoardView(items, compiled, defs)
	data.View = filterBoardViewForTypes(view, agentRuleTypes(agent, compiled))
	agents, aerr := s.memory.ListScheduledAgents(ctx)
	if aerr != nil {
		captureError(aerr)
	}
	data.Agents = agents
	return nil
}

// agentRuleTypes returns the object types the Settings editor should render:
// board-enabled types plus any type already carrying a rule, sorted by name.
func agentRuleTypes(def *AgentDefinition, compiled *CompiledSchemaTypes) []CompiledType {
	byName := map[string]CompiledType{}
	if compiled != nil {
		for _, t := range visibleCompiledTypes(compiled.ObjectTypes) {
			if t.BoardEnabled {
				byName[t.Name] = t
			}
		}
	}
	if def != nil && def.WorkConfig != nil {
		for _, r := range def.WorkConfig.ObjectRules {
			if _, ok := byName[r.ObjectType]; ok {
				continue
			}
			var found *CompiledType
			if compiled != nil {
				for _, t := range visibleCompiledTypes(compiled.ObjectTypes) {
					if t.Name == r.ObjectType {
						tt := t
						found = &tt
						break
					}
				}
			}
			if found != nil {
				byName[r.ObjectType] = *found
			} else {
				byName[r.ObjectType] = CompiledType{Name: r.ObjectType}
			}
		}
	}
	out := make([]CompiledType, 0, len(byName))
	for _, t := range byName {
		out = append(out, t)
	}
	sortCompiledTypesByName(out)
	return out
}

// filterBoardViewForTypes narrows swimlanes to the given types, preserving the
// view's order.
func filterBoardViewForTypes(v boardView, types []CompiledType) boardView {
	if len(types) == 0 {
		return v
	}
	allow := map[string]bool{}
	for _, t := range types {
		allow[t.Name] = true
	}
	out := boardView{StatusMaps: map[string]boardStatusMap{}}
	for _, sl := range v.Swimlanes {
		if allow[sl.Type] {
			out.Swimlanes = append(out.Swimlanes, sl)
			out.StatusMaps[sl.Type] = v.StatusMaps[sl.Type]
		}
	}
	return out
}

// EffectiveStatusForType resolves the phase map for an object type from the
// per-type rule, then the agent-level map, then the built-in defaults.
func (w *AgentWorkConfig) EffectiveStatusForType(objectType string) AgentWorkStatusConfig {
	s := AgentWorkStatusConfig{}
	if w != nil {
		s = w.Status
		if r := w.RuleFor(objectType); r != nil && r.Status != nil {
			s = *r.Status
		}
	}
	if s.Ready == "" {
		s.Ready = "ready"
	}
	if s.InProgress == "" {
		s.InProgress = "in_progress"
	}
	if s.Review == "" {
		s.Review = "review"
	}
	if s.Revision == "" {
		s.Revision = "revision"
	}
	if s.Blocked == "" {
		s.Blocked = "blocked"
	}
	if s.Done == "" {
		s.Done = "done"
	}
	return s
}

// uiAgentWorkUpdateSettings persists the Work Settings form: scope, per-type
// rules, and guardrails.
func (s *Server) uiAgentWorkUpdateSettings(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	def, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/work/"+workSectionSettings, err)
	}
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/work/"+workSectionSettings, err)
	}
	deriveDelegation(def)
	if err := applyAgentGraphSection(def, c); err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/work/"+workSectionSettings, err)
	}
	if err := applyAgentWorkRulesSection(def, c); err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/work/"+workSectionSettings, err)
	}
	if compiled, err := s.memory.GetCompiledTypes(ctx); err == nil {
		if err := validateGatewayWorkRules(def, compiled); err != nil {
			return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/work/"+workSectionSettings, err)
		}
	}
	if err := applyDelegation(def); err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/work/"+workSectionSettings, err)
	}
	if _, err := s.memory.UpdateAgentDefinition(ctx, id, def); err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/work/"+workSectionSettings, err)
	}
	return c.Redirect(http.StatusSeeOther, "/agents/"+url.PathEscape(id)+"/work/"+workSectionSettings+"?updated=1")
}

// applyAgentWorkRulesSection maps the Work Settings form onto the definition:
// agent-level guardrails/default phase map plus per-type rules.
func applyAgentWorkRulesSection(def *AgentDefinition, c echo.Context) error {
	if def.WorkConfig == nil {
		def.WorkConfig = &AgentWorkConfig{}
	}
	wc := def.WorkConfig

	wc.Status.Ready = strings.TrimSpace(c.FormValue("workStatus.ready"))
	wc.Status.InProgress = strings.TrimSpace(c.FormValue("workStatus.inProgress"))
	wc.Status.Review = strings.TrimSpace(c.FormValue("workStatus.review"))
	wc.Status.Revision = strings.TrimSpace(c.FormValue("workStatus.revision"))
	wc.Status.Blocked = strings.TrimSpace(c.FormValue("workStatus.blocked"))
	wc.Status.Done = strings.TrimSpace(c.FormValue("workStatus.done"))

	wc.RequiresReview = lastFormBool(c, "workRequiresReview")
	var err error
	if wc.FailureLimit, err = workIntField(c, "failureLimit", "Failure limit", 0); err != nil {
		return err
	}
	if wc.RevisionLimit, err = workIntField(c, "revisionLimit", "Revision limit", 0); err != nil {
		return err
	}
	if wc.RetryPolicy.MaxAttempts, err = workIntField(c, "maxAttempts", "Max attempts", 1); err != nil {
		return err
	}
	if wc.RetryPolicy.InitialIntervalMS, err = workIntField(c, "initialIntervalMs", "Initial interval", 0); err != nil {
		return err
	}
	if wc.RetryPolicy.BackoffCoefficient, err = workFloatField(c, "backoffCoefficient", "Backoff coefficient", 1); err != nil {
		return err
	}
	if wc.RetryPolicy.MaxIntervalMS, err = workIntField(c, "maxIntervalMs", "Max interval", 0); err != nil {
		return err
	}
	wc.WorkContract.RequireArtifacts = lastFormBool(c, "workRequireArtifacts")
	wc.WorkContract.RequiredDeliverableTypes = splitCSV(c.FormValue("requiredDeliverableType"))

	rules, err := parseObjectRules(c)
	if err != nil {
		return err
	}
	wc.ObjectRules = rules
	if len(rules) > 0 {
		wc.TriggerStatuses = nil
	} else {
		wc.TriggerStatuses = splitCSV(c.FormValue("triggerStatus"))
	}
	return nil
}

// parseObjectRules reads the repeated rule rows. Each rendered type submits a
// hidden ruleType marker plus its rule-<type>-* fields.
func parseObjectRules(c echo.Context) ([]AgentObjectRule, error) {
	types := c.Request().Form["ruleType"]
	var out []AgentObjectRule
	seen := map[string]bool{}
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		r := AgentObjectRule{ObjectType: t}
		r.Fields = splitCSV(c.FormValue("rule-" + t + "-fields"))
		create := lastFormBool(c, "rule-"+t+"-create")
		update := lastFormBool(c, "rule-"+t+"-update")
		del := lastFormBool(c, "rule-"+t+"-delete")
		r.CanCreate, r.CanUpdate, r.CanDelete = &create, &update, &del
		if st := parseRuleStatus(c, t); st != nil {
			r.Status = st
		}
		r.Triggers = parseRuleTriggers(c.FormValue("rule-" + t + "-triggers"))
		r.Transitions = parseRuleTransitions(c.FormValue("rule-" + t + "-transitions"))
		if ruleHasConfig(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// ruleHasConfig reports whether a parsed row carries non-default configuration.
// Untouched rows are dropped so saving the form cannot silently flip a legacy
// agent into rules-based pickup with no triggers.
func ruleHasConfig(r AgentObjectRule) bool {
	if r.Status != nil || len(r.Fields) > 0 || len(r.Triggers) > 0 || len(r.Transitions) > 0 {
		return true
	}
	for _, f := range []*bool{r.CanCreate, r.CanUpdate, r.CanDelete} {
		if f != nil && !*f {
			return true
		}
	}
	return false
}

// parseRuleStatus builds a per-type phase map override, or nil when every field
// is blank (inherit the agent-level map).
func parseRuleStatus(c echo.Context, objectType string) *AgentWorkStatusConfig {
	prefix := "rule-" + objectType + "-status-"
	vals := AgentWorkStatusConfig{
		Ready:      strings.TrimSpace(c.FormValue(prefix + "ready")),
		InProgress: strings.TrimSpace(c.FormValue(prefix + "inProgress")),
		Review:     strings.TrimSpace(c.FormValue(prefix + "review")),
		Revision:   strings.TrimSpace(c.FormValue(prefix + "revision")),
		Blocked:    strings.TrimSpace(c.FormValue(prefix + "blocked")),
		Done:       strings.TrimSpace(c.FormValue(prefix + "done")),
	}
	if vals == (AgentWorkStatusConfig{}) {
		return nil
	}
	return &vals
}

// parseRuleTriggers parses "status | prompt" lines, one trigger per line.
func parseRuleTriggers(raw string) []AgentWorkTrigger {
	var out []AgentWorkTrigger
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		status, prompt, _ := strings.Cut(line, "|")
		status = strings.TrimSpace(status)
		if status == "" {
			continue
		}
		out = append(out, AgentWorkTrigger{Status: status, Prompt: strings.TrimSpace(prompt)})
	}
	return out
}

// parseRuleTransitions parses "from -> to" lines.
func parseRuleTransitions(raw string) []AgentTransition {
	var out []AgentTransition
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		from, to, ok := strings.Cut(line, "->")
		if !ok {
			continue
		}
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if from == "" || to == "" {
			continue
		}
		out = append(out, AgentTransition{From: from, To: to})
	}
	return out
}

// validateGatewayWorkRules checks rules against the compiled type vocabulary and
// rejects non-pickup trigger statuses.
func validateGatewayWorkRules(def *AgentDefinition, compiled *CompiledSchemaTypes) error {
	if def == nil || def.WorkConfig == nil || len(def.WorkConfig.ObjectRules) == 0 {
		return nil
	}
	types := map[string]CompiledType{}
	if compiled != nil {
		for _, t := range visibleCompiledTypes(compiled.ObjectTypes) {
			types[t.Name] = t
		}
	}
	for _, r := range def.WorkConfig.ObjectRules {
		if r.ObjectType == "" {
			return fmt.Errorf("object type is required for every rule row")
		}
		ct, ok := types[r.ObjectType]
		if !ok {
			return fmt.Errorf("%s: unknown object type", r.ObjectType)
		}
		allowed := map[string]bool{}
		for _, s := range ct.AllowedStatuses {
			allowed[s] = true
		}
		st := def.WorkConfig.EffectiveStatusForType(r.ObjectType)
		nonPickup := map[string]string{
			st.InProgress: "in-progress",
			st.Review:     "review",
			st.Revision:   "revision",
			st.Blocked:    "blocked",
			st.Done:       "done",
		}
		seen := map[string]bool{}
		for _, tr := range r.Triggers {
			if phase, bad := nonPickup[tr.Status]; bad {
				return fmt.Errorf("%s: %q is the agent's %s status and cannot be a pickup trigger", r.ObjectType, tr.Status, phase)
			}
			if len(allowed) > 0 && !allowed[tr.Status] {
				return fmt.Errorf("%s: trigger status %q is not in the type's allowed statuses", r.ObjectType, tr.Status)
			}
			if seen[tr.Status] {
				return fmt.Errorf("%s: trigger status %q is declared more than once", r.ObjectType, tr.Status)
			}
			seen[tr.Status] = true
		}
		for _, t := range r.Transitions {
			if t.From == "" || t.To == "" || t.From == t.To {
				return fmt.Errorf("%s: transition edges require two different statuses", r.ObjectType)
			}
			if len(allowed) > 0 && (!allowed[t.From] || !allowed[t.To]) {
				return fmt.Errorf("%s: transition %q → %q uses a status outside the type's allowed statuses", r.ObjectType, t.From, t.To)
			}
		}
		if len(r.Fields) > 0 {
			props := compiledTypePropertyNames(ct)
			for _, f := range r.Fields {
				if len(props) > 0 && !props[f] {
					return fmt.Errorf("%s: writable field %q is not a declared property of the type", r.ObjectType, f)
				}
			}
		}
	}
	return nil
}

// compiledTypePropertyNames extracts the top-level property names from a
// compiled type's Properties JSON.
func compiledTypePropertyNames(t CompiledType) map[string]bool {
	out := map[string]bool{}
	if len(t.Properties) == 0 {
		return out
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(t.Properties, &m); err != nil {
		return out
	}
	for k := range m {
		out[k] = true
	}
	return out
}

// effectiveStatusForType is a nil-safe prefill wrapper.
func effectiveStatusForType(def *AgentDefinition, typeName string) AgentWorkStatusConfig {
	if def == nil || def.WorkConfig == nil {
		return (&AgentWorkConfig{}).EffectiveStatusForType(typeName)
	}
	return def.WorkConfig.EffectiveStatusForType(typeName)
}

// workConfigOf returns the definition's work config, or a zero value.
func workConfigOf(def *AgentDefinition) AgentWorkConfig {
	if def == nil || def.WorkConfig == nil {
		return AgentWorkConfig{}
	}
	return *def.WorkConfig
}

// --- form prefill helpers (used by the templ templates) ---

// ruleFor returns the prefill rule row for a type, or a zero value.
func ruleFor(def *AgentDefinition, objectType string) AgentObjectRule {
	if def == nil || def.WorkConfig == nil {
		return AgentObjectRule{ObjectType: objectType}
	}
	if r := def.WorkConfig.RuleFor(objectType); r != nil {
		return *r
	}
	return AgentObjectRule{ObjectType: objectType}
}

// ruleStatusValue returns the per-type override value, or "" when inherited.
func ruleStatusValue(r AgentObjectRule, phase string) string {
	if r.Status == nil {
		return ""
	}
	switch phase {
	case "ready":
		return r.Status.Ready
	case "inProgress":
		return r.Status.InProgress
	case "review":
		return r.Status.Review
	case "revision":
		return r.Status.Revision
	case "blocked":
		return r.Status.Blocked
	case "done":
		return r.Status.Done
	}
	return ""
}

// ruleAllows resolves the tri-state action flag for the form: nil reads as
// checked (allowed).
func ruleAllows(flag *bool) bool {
	return flag == nil || *flag
}

// ruleTriggersText renders trigger rows as "status | prompt" lines.
func ruleTriggersText(r AgentObjectRule) string {
	var lines []string
	for _, t := range r.Triggers {
		if t.Prompt != "" {
			lines = append(lines, t.Status+" | "+t.Prompt)
		} else {
			lines = append(lines, t.Status)
		}
	}
	return strings.Join(lines, "\n")
}

// ruleTransitionsText renders transition edges as "from -> to" lines.
func ruleTransitionsText(r AgentObjectRule) string {
	var lines []string
	for _, t := range r.Transitions {
		lines = append(lines, t.From+" -> "+t.To)
	}
	return strings.Join(lines, "\n")
}

// ruleFieldsText renders the writable-field allowlist as a comma list.
func ruleFieldsText(r AgentObjectRule) string {
	return strings.Join(r.Fields, ", ")
}

// compiledTypeStatusesText renders a type's lane vocabulary for the UI hint.
func compiledTypeStatusesText(t CompiledType) string {
	if len(t.AllowedStatuses) == 0 {
		return strings.Join(boardStatusOrder, ", ")
	}
	return strings.Join(t.AllowedStatuses, ", ")
}
