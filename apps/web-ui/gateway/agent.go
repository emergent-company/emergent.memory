package main

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// agentDashboardData is the assembled payload for AgentDashboardPage. The
// agent fetch failure renders the whole-page error state (LoadErr); a
// conversations failure degrades only the recent-chats section (ChatsErr).
type agentDashboardData struct {
	Agent    *AgentDefinition
	Chats    []Conversation
	ChatsErr error
	LoadErr  error

	// DefaultModel is the project's default generative model (best-effort
	// fallback when memory does not report EffectiveModel).
	DefaultModel string

	// HasProviders reports whether the project has at least one configured
	// provider (drives the "agent can't chat" warning for default-less agents).
	HasProviders bool

	// ProviderNames holds the configured provider keys of the project, e.g.
	// "openai" (drives the pinned-model unservable warning).
	ProviderNames []string
}

// uiAgent renders the per-agent dashboard: summary, configured tools, and
// recent chats, with a link to the memories subpage.
func (s *Server) uiAgent(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Agent"), AgentDashboardPage(agentDashboardData{LoadErr: err}))
	}
	data := agentDashboardData{Agent: agent}

	if mc, err := s.memory.GetProjectModelConfig(ctx); err == nil && mc != nil {
		data.DefaultModel = mc.GenerativeModel
	} else if err != nil {
		captureError(err)
	}

	if ps, err := s.memory.ListProjectProviders(ctx); err == nil {
		data.HasProviders = len(ps) > 0
		data.ProviderNames = projectProviderNames(ps)
	} else {
		captureError(err)
	}

	convs, cerr := s.memory.ListConversations(ctx)
	if cerr != nil {
		data.ChatsErr = cerr
	} else {
		for _, conv := range convs.Conversations {
			if conv.AgentDefinitionID == id {
				data.Chats = append(data.Chats, conv)
			}
		}
		// Most recently updated first (conversation UpdatedAt is RFC3339,
		// so plain string comparison orders correctly).
		sort.SliceStable(data.Chats, func(i, j int) bool {
			return data.Chats[i].UpdatedAt > data.Chats[j].UpdatedAt
		})
	}
	return s.page(c, pageTitle(agent.Name), AgentDashboardPage(data))
}

// uiAgentMemories renders the memory browser for one agent: a searchable
// list with a detail view when ?memory=<id> selects an item from the fetched
// list (no extra fetch needed). Empty query lists all memories, a query
// searches — mirroring the iOS memory browser.
func (s *Server) uiAgentMemories(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	// The agent name is only for display; memory is project-scoped, not
	// per-agent, so the agent fetch failure is the whole-page error.
	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Memories"), MemoriesPage(id, "", "", nil, nil, err))
	}
	query := c.QueryParam("q")

	var memories []Memory
	if query != "" {
		memories, err = s.memory.SearchMemories(ctx, query)
	} else {
		memories, err = s.memory.ListMemories(ctx)
	}
	if err != nil {
		return s.page(c, pageTitle("Memories"), MemoriesPage(id, agent.Name, query, nil, nil, err))
	}

	var selected *Memory
	if mid := c.QueryParam("memory"); mid != "" {
		for i := range memories {
			if memories[i].ID == mid {
				selected = &memories[i]
				break
			}
		}
	}
	return s.page(c, pageTitle(agent.Name, "Memories"), MemoriesPage(id, agent.Name, query, memories, selected, nil))
}

// agentSettingsData is the payload for AgentSettingsPage: the agent being
// edited, the model catalog, the MCP servers + their tools (the tools picker),
// the skill list, and every other agent (the delegation-target picker). A
// failed agent fetch renders the whole-page error state (LoadErr).
type agentSettingsData struct {
	Agent      *AgentDefinition
	Agents     []AgentDefinitionSummary
	Models     []Model
	MCPServers []MCPServer
	// RelayNodes are the connected external MCP relay nodes + their tools,
	// rendered as extra labelled groups in the tool picker. Best-effort: a
	// relay API failure during load leaves this empty (RelayNodes absent) and
	// the rest of the page intact.
	RelayNodes []relayNode
	Skills     []Skill
	LoadErr    error
	FlashMsg   string
	FlashErr   error

	// DefaultModel is the project's default generative model (best-effort).
	DefaultModel string
	// HasProviders reports whether the project has a configured provider.
	HasProviders bool
	// ProviderNames holds the configured provider keys of the project, e.g.
	// "openai" (drives the pinned-model unservable warning).
	ProviderNames []string
}

// agentSessionsData is the payload for AgentSessionsPage: the agent plus its
// full conversation list, mirroring agentDashboardData.
type agentSessionsData struct {
	Agent    *AgentDefinition
	Chats    []Conversation
	ChatsErr error
	LoadErr  error
}

// agentSandboxData is the payload for AgentSandboxPage: the agent being
// edited, its current sandbox config, and the provider/image catalogs
// (best-effort; their fetch errors are ignored, only the agent/config
// failures surface as the whole-page LoadErr).
type agentSandboxData struct {
	Agent     *AgentDefinition
	Config    *AgentSandboxConfig
	Providers []SandboxProvider
	Images    []SandboxImage
	LoadErr   error
	FlashMsg  string
	FlashErr  error
}

// uiAgentSettings renders the in-page agent edit form (moved out of the
// create/edit modal). ?updated=1 / ?err=1 surface PRG feedback from the
// update flow, mirroring uiSkill.
func (s *Server) uiAgentSettings(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	data := agentSettingsData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Agent updated."
	}
	data.FlashErr = flashError(c)

	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Agent settings"), AgentSettingsPage(data))
	}
	deriveDelegation(agent) // reconstruct Delegation for the form's prefill
	data.Agent = agent

	if mc, err := s.memory.GetProjectModelConfig(ctx); err == nil && mc != nil {
		data.DefaultModel = mc.GenerativeModel
	} else if err != nil {
		captureError(err)
	}
	if ps, err := s.memory.ListProjectProviders(ctx); err == nil {
		data.HasProviders = len(ps) > 0
		data.ProviderNames = projectProviderNames(ps)
	} else {
		captureError(err)
	}

	agents, err := s.memory.ListAgentDefinitions(ctx)
	captureError(err)
	data.Agents = agents

	models, err := s.memory.ListModels(ctx)
	captureError(err)
	data.Models = models

	mcps, err := s.memory.ListMCPServers(ctx)
	captureError(err)
	data.MCPServers = mcps

	// Relay nodes are best-effort: when the backend relay API is unreachable
	// the relay groups are simply absent (see loadRelayNodes).
	data.RelayNodes = s.loadRelayNodes(ctx)

	skills, err := s.memory.ListSkills(ctx)
	captureError(err)
	data.Skills = skills

	return s.page(c, pageTitle(agent.Name, "Settings"), AgentSettingsPage(data))
}

// uiAgentUpdate handles the Settings edit form (PRG). It maps form fields onto
// the existing AgentDefinition — mirroring the old modal's submitAgentForm
// JSON mapping exactly — then persists via UpdateAgentDefinition.
func (s *Server) uiAgentUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	def, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/settings", err)
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/settings", fmt.Errorf("name is required"))
	}
	def.Name = name
	def.SystemPrompt = c.FormValue("systemPrompt")
	// Language persists to Config["language"]; the memory service reads it at
	// runtime. Empty clears it so the model default applies.
	lang := strings.TrimSpace(c.FormValue("language"))
	if def.Config == nil {
		def.Config = map[string]any{}
	}
	if lang != "" {
		def.Config["language"] = lang
	} else {
		delete(def.Config, "language")
	}
	// Tools come from the checkbox picker (name="tool"), not a free-text string.
	def.Tools = c.Request().Form["tool"]
	// A tool can't be both allowed and banned; un-ban any newly allowed tool.
	def.BannedTools = removeItems(def.BannedTools, def.Tools...)

	// Always set skills (even empty) so clearing them reaches memory.
	def.Skills = c.Request().Form["skill"]

	// Tool approval policy: default + per-tool overrides.
	def.DefaultToolPolicy = strings.TrimSpace(c.FormValue("defaultToolPolicy"))
	policies := map[string]ToolPolicy{}
	for _, tool := range def.Tools {
		switch c.FormValue("toolPolicy." + tool) {
		case "ask":
			policies[tool] = ToolPolicy{Confirm: true}
		case "deny":
			policies[tool] = ToolPolicy{Disabled: true}
		case "allow":
			policies[tool] = ToolPolicy{}
		}
	}
	def.ToolPolicies = policies

	if modelName := strings.TrimSpace(c.FormValue("modelName")); modelName != "" {
		temp, terr := strconv.ParseFloat(c.FormValue("temperature"), 64)
		if terr != nil {
			temp = 0.7
		}
		maxTok, merr := strconv.Atoi(c.FormValue("maxTokens"))
		if merr != nil || maxTok <= 0 {
			maxTok = 4096
		}
		def.Model = &ModelConfig{Name: modelName, Temperature: temp, MaxTokens: maxTok}
	} else {
		def.Model = nil
	}

	if c.FormValue("delegationEnabled") == "on" {
		targets := c.Request().Form["delegation-target"]
		if len(targets) == 0 {
			return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/settings", fmt.Errorf("delegation requires at least one target"))
		}
		def.Delegation = &Delegation{Enabled: true, Targets: targets}
	} else {
		def.Delegation = &Delegation{Enabled: false}
	}

	if err := applyDelegation(def); err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/settings", err)
	}
	if _, err := s.memory.UpdateAgentDefinition(ctx, id, def); err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/settings", err)
	}
	return c.Redirect(http.StatusSeeOther, "/agents/"+url.PathEscape(id)+"/settings?updated=1")
}

// uiAgentSessions renders the full conversation list for one agent, mirroring
// the recent-chats query in uiAgent but as a dedicated sub-page.
func (s *Server) uiAgentSessions(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	data := agentSessionsData{}
	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Agent sessions"), AgentSessionsPage(data))
	}
	data.Agent = agent

	convs, cerr := s.memory.ListConversations(ctx)
	if cerr != nil {
		data.ChatsErr = cerr
	} else {
		for _, conv := range convs.Conversations {
			if conv.AgentDefinitionID == id {
				data.Chats = append(data.Chats, conv)
			}
		}
		sort.SliceStable(data.Chats, func(i, j int) bool {
			return data.Chats[i].UpdatedAt > data.Chats[j].UpdatedAt
		})
	}
	return s.page(c, pageTitle(agent.Name, "Sessions"), AgentSessionsPage(data))
}

// --- settings-form helpers (used by AgentSettingsPage) ---

// delegationToolNames are the A2A tools applyDelegation manages on
// AgentDefinition.Tools. They're driven by the delegation toggle, not the
// manual tools picker, so the picker neither renders nor submits them.
var delegationToolNames = []string{"spawn_agents", "list_available_agents"}

// isDelegationTool reports whether t is delegation-managed.
func isDelegationTool(t string) bool {
	return containsString(delegationToolNames, t)
}

// unlistedTools returns the agent's current tools that no registered MCP
// server or connected relay node offers (and that aren't delegation-managed) —
// surfaced as an "Other" group so the picker never silently drops them (e.g.
// the bridge's Home Assistant tools, or a relay tool whose node has
// disconnected since it was whitelisted).
func unlistedTools(agent *AgentDefinition, servers []MCPServer, relayNodes []relayNode) []string {
	if agent == nil {
		return nil
	}
	listed := map[string]bool{}
	for _, srv := range servers {
		for _, t := range srv.Tools {
			listed[t.ToolName] = true
		}
	}
	for _, n := range relayNodes {
		for _, t := range n.Tools {
			listed[relayAgentToolName(n.Session.InstanceID, t.Name)] = true
		}
	}
	var out []string
	for _, t := range agent.Tools {
		if !listed[t] && !isDelegationTool(t) {
			out = append(out, t)
		}
	}
	return out
}

// mcpServerInUse reports whether the agent already has a tool from the
// server — drives the default open/collapsed state of the settings tools
// picker so active picks stay visible.
func mcpServerInUse(srv MCPServer, agent *AgentDefinition) bool {
	if agent == nil {
		return false
	}
	for _, t := range srv.Tools {
		if containsString(agent.Tools, t.ToolName) {
			return true
		}
	}
	return false
}

// containsString reports whether list contains s.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// toolPolicyValue returns the current approval policy for a tool as one of
// "allow", "ask", "deny", or "" (inherit the default). It maps the memory
// ToolPolicy booleans onto the form's value space.
func toolPolicyValue(agent *AgentDefinition, tool string) string {
	if agent == nil || agent.ToolPolicies == nil {
		return ""
	}
	p, ok := agent.ToolPolicies[tool]
	if !ok {
		return ""
	}
	switch {
	case p.Disabled:
		return "deny"
	case p.Confirm:
		return "ask"
	default:
		return "allow"
	}
}

// modelInCatalog reports whether a prefixed provider/model name is present in
// the catalog.
func modelInCatalog(models []Model, name string) bool {
	for _, m := range models {
		if m.Provider+"/"+m.ModelName == name {
			return true
		}
	}
	return false
}

// agentModelName returns the model an agent runs with and whether that model
// is its (resolved) default rather than an explicit override. name is ""
// when nothing is known — callers keep rendering the "auto — default model"
// fallback. fallbackDefault is the project's default generative model, used
// only when memory reported no EffectiveModel (older memory versions).
func agentModelName(agent *AgentDefinition, fallbackDefault string) (name string, isDefault bool) {
	if agent == nil {
		return "", false
	}
	if agent.Model != nil && agent.Model.Name != "" {
		return agent.Model.Name, false
	}
	if agent.EffectiveModel != "" {
		return agent.EffectiveModel, true
	}
	if fallbackDefault != "" {
		return fallbackDefault, true
	}
	return "", false
}

// projectProviderNames returns the provider keys of the project's configured
// providers (ps[i].Provider), e.g. "openai" for openai/deepseek-v4-pro, in
// list order.
func projectProviderNames(ps []ProjectProviderConfig) []string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Provider)
	}
	return names
}

// modelProviderOf returns the provider key of a model string — the substring
// before the first "/" — or "" when the model is bare (no "/").
func modelProviderOf(model string) string {
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i]
	}
	return ""
}

// agentModelIssue classifies an agent's model availability for the warnings:
// the severity ("" = none) plus, when the problem is an explicit pinned model,
// that model and its unconfigured provider prefix.
type agentModelIssue struct {
	sev      string
	explicit bool
	model    string
	provider string
}

// classifyAgentModelIssue decides whether (and why) an agent's model can't be
// served. Legacy no-explicit-model rules are unchanged: "error" when the
// project has no configured provider and no default resolves, "warning" when
// providers exist but nothing pins the model, none when a model resolves. An
// explicit model is "error" when the project has no configured provider or its
// provider prefix matches no configured provider; a bare model (no "/") with
// providers configured is treated as satisfied, and a matching provider is
// satisfied even when the model name isn't in its catalog (custom base URLs).
func classifyAgentModelIssue(agent *AgentDefinition, defaultModel string, hasProviders bool, providerNames []string) agentModelIssue {
	if agent == nil {
		return agentModelIssue{}
	}
	if agent.Model == nil || agent.Model.Name == "" {
		if resolved, _ := agentModelName(agent, defaultModel); resolved != "" {
			return agentModelIssue{}
		}
		if !hasProviders {
			return agentModelIssue{sev: "error"}
		}
		return agentModelIssue{sev: "warning"}
	}
	model := agent.Model.Name
	if !hasProviders {
		return agentModelIssue{sev: "error", explicit: true, model: model}
	}
	prov := modelProviderOf(model)
	if prov == "" || containsString(providerNames, prov) {
		return agentModelIssue{}
	}
	return agentModelIssue{sev: "error", explicit: true, model: model, provider: prov}
}

// agentModelDashboardIssue returns the severity and copy for the agent
// dashboard's model-availability notice, or ("", "") when no warning applies.
func agentModelDashboardIssue(agent *AgentDefinition, defaultModel string, hasProviders bool, providerNames []string) (sev, msg string) {
	issue := classifyAgentModelIssue(agent, defaultModel, hasProviders, providerNames)
	switch {
	case issue.sev == "":
		return "", ""
	case !issue.explicit:
		if issue.sev == "error" {
			return "error", "No provider or default model is configured — this agent can't run chats yet."
		}
		return "warning", "No project default model is set — the model this agent uses isn't pinned."
	case issue.provider != "":
		return "error", "This agent's model " + issue.model + " needs the " + issue.provider + " provider, which isn't configured. Add it or choose another model."
	default:
		return "error", "No provider is configured, so this agent's model " + issue.model + " can't run yet. Configure a provider or change the agent's model."
	}
}

// agentModelSettingsIssue returns the severity and copy for the agent-settings
// Model section's availability warning, or ("", "") when none applies. Copy
// mirrors the dashboard's but uses "pinned" wording.
func agentModelSettingsIssue(agent *AgentDefinition, defaultModel string, hasProviders bool, providerNames []string) (sev, msg string) {
	issue := classifyAgentModelIssue(agent, defaultModel, hasProviders, providerNames)
	switch {
	case issue.sev == "":
		return "", ""
	case !issue.explicit:
		if issue.sev == "error" {
			return "error", "This agent has no model. The project has no configured provider and no default generative model, so chats will fail."
		}
		return "warning", "This agent has no explicit model and the project has no default generative model, so the exact model isn't pinned. Chats fall back to a configured provider's default."
	case issue.provider != "":
		return "error", "This agent's model " + issue.model + " uses the " + issue.provider + " provider, which isn't configured — chats will fail until it is added or the model is changed."
	default:
		return "error", "This agent is pinned to " + issue.model + ", but the project has no configured provider — chats will fail until one is added or the model is changed."
	}
}

// agentPinnedModelIssue returns the severity and copy for a blueprint detail
// agent row whose pinned model can't be served, or ("", "") when the model is
// empty or satisfiable by the project's configured providers.
func agentPinnedModelIssue(agentName, model string, providerNames []string) (sev, msg string) {
	if model == "" {
		return "", ""
	}
	if len(providerNames) == 0 {
		return "error", "This blueprint defines agent " + agentName + " pinned to " + model + ", but no provider is configured in this project. Chats with it will fail until a provider is added or the agent's model is changed in Agent settings."
	}
	prov := modelProviderOf(model)
	if prov == "" || containsString(providerNames, prov) {
		return "", ""
	}
	return "error", "This blueprint defines agent " + agentName + " pinned to " + model + ", which needs the " + prov + " provider — not configured in this project. Chats with it will fail until a provider is added or the agent's model is changed in Agent settings."
}

// modelWarningAlertType maps the warning severity ("error"/"warning") to the
// go-daisy alert type used for the banner.
func modelWarningAlertType(sev string) ui.AlertType {
	if sev == "error" {
		return ui.AlertError
	}
	return ui.AlertWarning
}

// agentLanguageValue reads the agent's configured response language from
// Config["language"], returning "" when unset.
func agentLanguageValue(a *AgentDefinition) string {
	if a == nil || a.Config == nil {
		return ""
	}
	if v, ok := a.Config["language"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// languageOption is one selectable agent language.
type languageOption struct {
	Code  string
	Label string
}

// agentLanguages is the canonical language list (ISO 639-1 codes), shared with
// the bridge's LANGUAGES table. Codes are the single source of truth.
var agentLanguages = []languageOption{
	{"en", "English"},
	{"pl", "Polski"},
	{"de", "Deutsch"},
	{"es", "Español"},
	{"fr", "Français"},
	{"it", "Italiano"},
	{"ja", "日本語"},
	{"pt", "Português"},
	{"nl", "Nederlands"},
}

// agentLanguageCode normalizes a stored language value — a code like "pl" or a
// legacy free-form label like "Spanish" — to the canonical ISO 639-1 code.
// Returns "" for empty or unknown values.
func agentLanguageCode(v string) string {
	code := strings.ToLower(strings.TrimSpace(v))
	if code == "" {
		return ""
	}
	for _, l := range agentLanguages {
		if code == l.Code {
			return l.Code
		}
	}
	aliases := map[string]string{
		"english": "en", "polish": "pl", "german": "de", "spanish": "es",
		"french": "fr", "italian": "it", "japanese": "ja", "portuguese": "pt",
		"dutch": "nl",
	}
	if c, ok := aliases[code]; ok {
		return c
	}
	return ""
}

// temperatureInputValue returns the form value for the temperature field, or
// "" when unset (zero value is treated as unset, matching the JSON omitempty
// round-trip of the old modal).
func temperatureInputValue(m *ModelConfig) string {
	if m == nil || m.Temperature == 0 {
		return ""
	}
	return strconv.FormatFloat(m.Temperature, 'f', -1, 64)
}

// maxTokensInputValue returns the form value for the max-tokens field, or ""
// when unset.
func maxTokensInputValue(m *ModelConfig) string {
	if m == nil || m.MaxTokens == 0 {
		return ""
	}
	return strconv.Itoa(m.MaxTokens)
}

// --- sandbox ---

// sandboxToolNames are the only workspace tools memory accepts.
var sandboxToolNames = []string{"bash", "read", "write", "edit", "glob", "grep", "git", "run_python", "run_go", "ast_grep"}

// uiAgentSandbox renders the per-agent sandbox config page. ?updated=1 /
// ?err=1 surface PRG feedback from the update flow, mirroring uiAgentSettings.
// The provider and image catalogs are best-effort — their failures degrade to
// empty lists, not the whole-page error.
func (s *Server) uiAgentSandbox(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	data := agentSandboxData{}
	if c.QueryParam("updated") != "" {
		data.FlashMsg = "Sandbox updated."
	}
	data.FlashErr = flashError(c)

	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Sandbox"), AgentSandboxPage(data))
	}
	data.Agent = agent

	cfg, err := s.memory.GetAgentSandboxConfig(ctx, id)
	if err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Sandbox"), AgentSandboxPage(data))
	}
	data.Config = cfg

	providers, err := s.memory.ListSandboxProviders(ctx)
	captureError(err)
	data.Providers = providers

	images, err := s.memory.ListSandboxImages(ctx)
	captureError(err)
	data.Images = images

	return s.page(c, pageTitle(agent.Name, "Sandbox"), AgentSandboxPage(data))
}

// uiAgentSandboxUpdate handles the sandbox form (PRG). It maps form fields
// onto AgentSandboxConfig — empty tools means "all allowed", repo_source is
// only set for fixed/task_context, URL is only valid for fixed — then persists
// via SetAgentSandboxConfig.
func (s *Server) uiAgentSandboxUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	cfg := &AgentSandboxConfig{
		Enabled:   c.FormValue("enabled") == "on",
		Provider:  strings.TrimSpace(c.FormValue("provider")),
		BaseImage: strings.TrimSpace(c.FormValue("baseImage")),
	}

	switch repoType := strings.TrimSpace(c.FormValue("repoSourceType")); repoType {
	case "fixed":
		urlStr := strings.TrimSpace(c.FormValue("repoSourceUrl"))
		if urlStr == "" {
			return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/sandbox", fmt.Errorf("repository URL is required for a fixed source"))
		}
		cfg.RepoSource = &RepoSourceConfig{Type: "fixed", URL: urlStr, Branch: strings.TrimSpace(c.FormValue("repoSourceBranch"))}
	case "task_context":
		cfg.RepoSource = &RepoSourceConfig{Type: "task_context", Branch: strings.TrimSpace(c.FormValue("repoSourceBranch"))}
	default: // "none" / empty → no repo source
		cfg.RepoSource = nil
	}

	if tools := c.Request().Form["sandboxTool"]; len(tools) > 0 {
		cfg.Tools = tools
	}

	cpu := strings.TrimSpace(c.FormValue("cpu"))
	mem := strings.TrimSpace(c.FormValue("memory"))
	disk := strings.TrimSpace(c.FormValue("disk"))
	if cpu != "" || mem != "" || disk != "" {
		cfg.ResourceLimits = &ResourceLimits{CPU: cpu, Memory: mem, Disk: disk}
	}

	for _, line := range strings.Split(c.FormValue("setupCommands"), "\n") {
		if cmd := strings.TrimSpace(line); cmd != "" {
			cfg.SetupCommands = append(cfg.SetupCommands, cmd)
		}
	}

	cfg.EnvVars = map[string]string{}
	for _, line := range strings.Split(c.FormValue("envVars"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/sandbox", fmt.Errorf("env vars: each line must be KEY=VALUE"))
		}
		cfg.EnvVars[k] = strings.TrimSpace(v)
	}

	if _, err := s.memory.SetAgentSandboxConfig(ctx, id, cfg); err != nil {
		return redirectWithError(c, "/agents/"+url.PathEscape(id)+"/sandbox", err)
	}
	return c.Redirect(http.StatusSeeOther, "/agents/"+url.PathEscape(id)+"/sandbox?updated=1")
}

// --- sandbox form helpers (used by AgentSandboxPage) ---

// sandboxConfigEnabled reports whether the sandbox is enabled.
func sandboxConfigEnabled(cfg *AgentSandboxConfig) bool {
	return cfg != nil && cfg.Enabled
}

// sandboxConfigProvider returns the configured sandbox provider, or "" when
// unset (memory's auto).
func sandboxConfigProvider(cfg *AgentSandboxConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.Provider
}

// sandboxRepoSourceType returns the repo-source type, defaulting the display
// to "none" when unset.
func sandboxRepoSourceType(cfg *AgentSandboxConfig) string {
	if cfg == nil || cfg.RepoSource == nil {
		return ""
	}
	t := cfg.RepoSource.Type
	if t == "" {
		return "none"
	}
	return t
}

func sandboxRepoSourceURL(cfg *AgentSandboxConfig) string {
	if cfg == nil || cfg.RepoSource == nil {
		return ""
	}
	return cfg.RepoSource.URL
}

func sandboxRepoSourceBranch(cfg *AgentSandboxConfig) string {
	if cfg == nil || cfg.RepoSource == nil {
		return ""
	}
	return cfg.RepoSource.Branch
}

// sandboxResourceValue returns one of cpu/memory/disk from the configured
// resource limits ("" when unset). kind must be "cpu", "memory", or "disk".
func sandboxResourceValue(cfg *AgentSandboxConfig, kind string) string {
	if cfg == nil || cfg.ResourceLimits == nil {
		return ""
	}
	switch kind {
	case "cpu":
		return cfg.ResourceLimits.CPU
	case "memory":
		return cfg.ResourceLimits.Memory
	case "disk":
		return cfg.ResourceLimits.Disk
	}
	return ""
}

func sandboxBaseImage(cfg *AgentSandboxConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.BaseImage
}

// sandboxSetupCommands joins the setup commands one-per-line for the textarea.
func sandboxSetupCommands(cfg *AgentSandboxConfig) string {
	if cfg == nil {
		return ""
	}
	return strings.Join(cfg.SetupCommands, "\n")
}

// sandboxEnvVars joins the env vars as KEY=VALUE lines (sorted for
// deterministic rendering).
func sandboxEnvVars(cfg *AgentSandboxConfig) string {
	if cfg == nil || len(cfg.EnvVars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(cfg.EnvVars))
	for k := range cfg.EnvVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+cfg.EnvVars[k])
	}
	return strings.Join(lines, "\n")
}

// sandboxToolEnabled reports whether tool is in the config's tool allowlist
// (empty list = all tools allowed, but unchecked).
func sandboxToolEnabled(cfg *AgentSandboxConfig, tool string) bool {
	return cfg != nil && containsString(cfg.Tools, tool)
}

// sandboxHealthyProviders lists the names of healthy sandbox providers,
// comma-separated, or "" when none are healthy (the template omits the note).
func sandboxHealthyProviders(providers []SandboxProvider) string {
	names := []string{}
	for _, p := range providers {
		if p.Healthy {
			names = append(names, p.Name)
		}
	}
	return strings.Join(names, ", ")
}
