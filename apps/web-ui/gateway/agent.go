package main

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
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

	// DefaultModel is the project's PINNED default generative model (project
	// model-config); "" when none is pinned. The provider-credential fallback is
	// deliberately NOT folded in — ModelConfigKnown + HasProviders drive the
	// "isn't pinned" warning.
	DefaultModel string

	// ModelConfigKnown reports whether the project model-config fetch succeeded,
	// so DefaultModel == "" reliably means "no pinned default" rather than
	// "signal unavailable".
	ModelConfigKnown bool

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

	if mc, err := s.memory.GetProjectModelConfig(ctx); err != nil {
		captureError(err)
	} else {
		if mc != nil {
			data.DefaultModel = mc.GenerativeModel
		}
		data.ModelConfigKnown = true
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
	Agent *AgentDefinition
	// Section is the settings subpage being rendered: "general", "model",
	// "tools", "skills", "delegation", or "mcp" ("" is treated as "general").
	Section    string
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

	// DefaultModel is the project's PINNED default generative model (project
	// model-config); "" when none is pinned.
	DefaultModel string
	// ModelConfigKnown reports whether the project model-config fetch succeeded
	// (see agentDashboardData.ModelConfigKnown).
	ModelConfigKnown bool
	// HasProviders reports whether the project has a configured provider.
	HasProviders bool
	// ProviderNames holds the configured provider keys of the project, e.g.
	// "openai" (drives the pinned-model unservable warning).
	ProviderNames []string

	// MCPEndpoint is the agent's own MCP endpoint (agent-scoped-mcp-endpoint);
	// nil means the agent has none yet. MCPKeys are its labeled credentials,
	// MCPSessions the external client sessions those keys created.
	MCPAgentID       string
	MCPEndpoint      *AgentMCPEndpoint
	MCPKeys          []AgentMCPKey
	MCPSessions      []AgentMCPSession
	MCPSessionStatus string
	// MCPKeyDraft preserves a typed-but-rejected key label across a re-render.
	MCPKeyDraft string
	// MCPEndpointErr / MCPKeysErr / MCPSessionsErr degrade only their own MCP
	// block; MCPActionErr is a create/revoke/rotate failure shown inline.
	MCPEndpointErr error
	MCPKeysErr     error
	MCPSessionsErr error
	MCPActionErr   error
	// MCPReveal carries a just-created/rotated one-time secret. It is non-nil on
	// that one response only.
	MCPReveal *agentMCPReveal
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
// edited, its current sandbox config, and the provider/image catalogs. The
// catalogs are best-effort (only the agent/config failures surface as the
// whole-page LoadErr); a provider-list failure sets ProviderListErr so the
// form can show an inline warning instead of guessing at availability.
type agentSandboxData struct {
	Agent           *AgentDefinition
	Config          *AgentSandboxConfig
	Providers       []SandboxProvider
	Images          []SandboxImage
	ProviderListErr bool // provider list fetch failed; availability is unknown
	LoadErr         error
	FlashMsg        string
	FlashErr        error
}

// Settings subpage keys. "general" is the landing page (/agents/:id/settings);
// the rest are /agents/:id/settings/<section>.
const (
	sectionGeneral    = "general"
	sectionModel      = "model"
	sectionTools      = "tools"
	sectionSkills     = "skills"
	sectionDelegation = "delegation"
	sectionMCP        = "mcp"
)

// agentSettingsSections is the ordered list of settings subpages, used by the
// nav group.
var agentSettingsSections = []string{sectionGeneral, sectionModel, sectionTools, sectionSkills, sectionDelegation, sectionMCP}

// agentSettingsSectionPath returns the settings subpage URL for one section.
// "general" maps to the bare /agents/:id/settings landing route.
func agentSettingsSectionPath(agentID, section string) string {
	base := "/agents/" + url.PathEscape(agentID) + "/settings"
	if section == "" || section == sectionGeneral {
		return base
	}
	return base + "/" + section
}

// uiAgentSettings renders the settings landing page (the General section).
func (s *Server) uiAgentSettings(c echo.Context) error {
	return s.renderAgentSettingsSection(c, sectionGeneral)
}

// uiAgentSettingsSection renders one settings subpage (Model / Tools / Skills /
// Delegation / MCP sharing). An unknown section is a 404.
func (s *Server) uiAgentSettingsSection(c echo.Context) error {
	section := c.Param("section")
	if !containsString(agentSettingsSections, section) {
		return c.NoContent(http.StatusNotFound)
	}
	return s.renderAgentSettingsSection(c, section)
}

// renderAgentSettingsSection assembles the settings payload for one subpage and
// renders the shared AgentSettingsPage, with data.Section selecting the panel.
// ?updated=1 / ?err=... surface PRG feedback; ?sessions= preselects the MCP
// session status filter (only loaded on the MCP subpage).
func (s *Server) renderAgentSettingsSection(c echo.Context, section string) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	data := agentSettingsData{Section: section}
	switch {
	case c.QueryParam("updated") != "":
		data.FlashMsg = "Agent updated."
	case c.QueryParam("mcpCreated") != "":
		data.FlashMsg = "MCP endpoint created."
	case c.QueryParam("mcpRevoked") != "":
		data.FlashMsg = "MCP endpoint revoked."
	case c.QueryParam("keyRevoked") != "":
		data.FlashMsg = "Key revoked."
	}
	data.FlashErr = flashError(c)
	data.MCPSessionStatus = normalizeAgentMCPSessionStatus(c.QueryParam("sessions"))

	if err := s.loadAgentSettings(ctx, id, &data); err != nil {
		data.LoadErr = err
		return s.page(c, pageTitle("Agent settings"), AgentSettingsPage(data))
	}
	// The MCP block (endpoint/keys/sessions) is only rendered on the MCP
	// sharing subpage; other subpages skip those backend fetches.
	if section == sectionMCP {
		s.loadAgentMCP(ctx, id, &data)
	}

	return s.page(c, pageTitle(data.Agent.Name, "Settings"), AgentSettingsPage(data))
}

// agentSectionApply mutates the freshly fetched definition with one subpage's
// form values only — it never touches fields another subpage owns, so saving
// one section can't wipe the others.
type agentSectionApply func(def *AgentDefinition, c echo.Context) error

// applyAgentSettingsSection fetches the agent definition, reconstructs the
// gateway-only Delegation field from the persisted representation, applies one
// section's fields, re-applies delegation (so delegation-managed tools and
// spawnPolicy survive even a non-delegation save), and persists. PRG redirects
// back to the section with ?updated=1 on success or ?err=<msg> on failure.
func (s *Server) applyAgentSettingsSection(c echo.Context, section string, apply agentSectionApply) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	def, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return redirectWithError(c, agentSettingsSectionPath(id, section), err)
	}
	// Parse the form once so the multi-value reads (tool[], skill[],
	// delegation-target[]) in the section appliers see the submitted values.
	if err := c.Request().ParseForm(); err != nil {
		return redirectWithError(c, agentSettingsSectionPath(id, section), err)
	}
	deriveDelegation(def)
	if err := apply(def, c); err != nil {
		return redirectWithError(c, agentSettingsSectionPath(id, section), err)
	}
	if err := applyDelegation(def); err != nil {
		return redirectWithError(c, agentSettingsSectionPath(id, section), err)
	}
	if _, err := s.memory.UpdateAgentDefinition(ctx, id, def); err != nil {
		return redirectWithError(c, agentSettingsSectionPath(id, section), err)
	}
	return c.Redirect(http.StatusSeeOther, agentSettingsSectionPath(id, section)+"?updated=1")
}

// uiAgentUpdateGeneral handles POST /agents/:id/settings/general (and the
// back-compat POST /agents/:id/update alias): name, system prompt, language,
// and visibility.
func (s *Server) uiAgentUpdateGeneral(c echo.Context) error {
	return s.applyAgentSettingsSection(c, sectionGeneral, applyAgentGeneralSection)
}

// uiAgentUpdateModel handles POST /agents/:id/settings/model.
func (s *Server) uiAgentUpdateModel(c echo.Context) error {
	return s.applyAgentSettingsSection(c, sectionModel, applyAgentModelSection)
}

// uiAgentUpdateTools handles POST /agents/:id/settings/tools.
func (s *Server) uiAgentUpdateTools(c echo.Context) error {
	return s.applyAgentSettingsSection(c, sectionTools, applyAgentToolsSection)
}

// uiAgentUpdateSkills handles POST /agents/:id/settings/skills.
func (s *Server) uiAgentUpdateSkills(c echo.Context) error {
	return s.applyAgentSettingsSection(c, sectionSkills, applyAgentSkillsSection)
}

// uiAgentUpdateDelegation handles POST /agents/:id/settings/delegation.
func (s *Server) uiAgentUpdateDelegation(c echo.Context) error {
	return s.applyAgentSettingsSection(c, sectionDelegation, applyAgentDelegationSection)
}

// applyAgentGeneralSection maps the General form (name, system prompt,
// language, appearance, visibility) onto the definition. Name is trimmed and
// required; language persists to Config["language"] (deleted when empty); the
// icon + color appearance persists to the uiConfig blob (both empty clears it);
// visibility accepts only project/external/internal, with empty/missing
// defaulting to project (the server default) and anything else rejected.
func applyAgentGeneralSection(def *AgentDefinition, c echo.Context) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return fmt.Errorf("name is required")
	}
	def.Name = name
	def.SystemPrompt = c.FormValue("systemPrompt")
	def.UIConfig = agentUIConfig(c.FormValue("icon"), c.FormValue("color"))
	lang := strings.TrimSpace(c.FormValue("language"))
	if def.Config == nil {
		def.Config = map[string]any{}
	}
	if lang != "" {
		def.Config["language"] = lang
	} else {
		delete(def.Config, "language")
	}
	visibility, ok := agentVisibilityNormalize(c.FormValue("visibility"))
	if !ok {
		return fmt.Errorf("visibility must be one of project, external, internal")
	}
	def.Visibility = visibility
	return nil
}

// applyAgentModelSection maps the Model form onto the definition: an explicit
// model (name + temperature + max tokens) or nil when "Auto — default model"
// is selected.
func applyAgentModelSection(def *AgentDefinition, c echo.Context) error {
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
	return nil
}

// applyAgentToolsSection maps the Tools form onto the definition: the allowed
// tool list, un-banning any newly allowed tool, the default approval policy,
// per-tool policy overrides (ask/deny/allow/inherit), and the capability-group
// policy + enable/disable controls.
//
// Per-tool and group policy keys are written only when their form control is
// actually present (a per-tool select submits for both checked and unchecked
// tools; a group select submits only for non-"other" groups), so keys whose
// control is absent are preserved verbatim rather than wiped.
//
// Group enable/disable is a membership operation (D4), not a policy: enabling a
// group adds every member to Tools and un-bans it; disabling removes members
// from Tools and bans them. Group policies are stored under reserved
// "@group:<id>" keys in ToolPolicies, so resolution (explicit tool → group →
// default) needs no schema change.
func applyAgentToolsSection(def *AgentDefinition, c echo.Context) error {
	form := c.Request().Form
	def.Tools = form["tool"]
	def.BannedTools = removeItems(def.BannedTools, def.Tools...)
	def.DefaultToolPolicy = strings.TrimSpace(c.FormValue("defaultToolPolicy"))

	// Start from the existing policies so per-tool overrides and @group: entries
	// for tools/groups this save does not render are preserved, not deleted.
	policies := maps.Clone(def.ToolPolicies)
	if policies == nil {
		policies = map[string]ToolPolicy{}
	}

	// Per-tool overrides are read first and win: a tool that carries an explicit
	// "toolPolicy.<tool>" entry always writes its own entry; "inherit" deletes it.
	// Iterate the submitted keys (not def.Tools) so a policy select for an
	// unchecked tool is still honored.
	for key := range form {
		tool, ok := strings.CutPrefix(key, "toolPolicy.")
		if !ok {
			continue
		}
		switch form.Get(key) {
		case "ask":
			policies[tool] = ToolPolicy{Confirm: true}
		case "deny":
			policies[tool] = ToolPolicy{Disabled: true}
		case "allow":
			policies[tool] = ToolPolicy{}
		case "inherit":
			delete(policies, tool)
		}
	}
	applyToolGroups(def, form, policies)
	def.ToolPolicies = policies
	return nil
}

// applyToolGroups writes the group-level policy entries and applies each
// rendered group's enable/disable fan-out. A group is "rendered" when the form
// carries its baseline groupWasEnabled.<id> field (emitted next to the enable
// switch). The enable/disable fan-out runs only when the submitted switch state
// differs from that baseline, so a no-op save or unchecking a single child does
// not fan out the entire full-catalog group.
//
// Delegation-managed tools are never fanned out: the enable switch must not add
// or ban spawn_agents / list_available_agents, which the delegation toggle owns.
func applyToolGroups(def *AgentDefinition, form url.Values, policies map[string]ToolPolicy) {
	// Group policy: only when the form explicitly submits a value for the group
	// (the policy select is rendered for every non-"other" group). "inherit"
	// clears the key; an absent control leaves the stored entry untouched.
	for _, g := range def.ToolGroups {
		if !form.Has("groupPolicy." + g.ID) {
			continue
		}
		switch form.Get("groupPolicy." + g.ID) {
		case "ask":
			policies[toolGroupPolicyKey(g.ID)] = ToolPolicy{Confirm: true}
		case "deny":
			policies[toolGroupPolicyKey(g.ID)] = ToolPolicy{Disabled: true}
		case "allow":
			policies[toolGroupPolicyKey(g.ID)] = ToolPolicy{}
		case "inherit":
			delete(policies, toolGroupPolicyKey(g.ID))
		}
	}

	// Enable/disable fan-out: only when the submitted switch state differs from
	// the rendered baseline.
	for _, g := range def.ToolGroups {
		if !form.Has("groupWasEnabled." + g.ID) {
			continue
		}
		wasEnabled := form.Get("groupWasEnabled."+g.ID) == "true"
		nowEnabled := form.Has("groupEnabled." + g.ID)
		if wasEnabled == nowEnabled {
			continue
		}
		members := make([]string, 0, len(g.Tools))
		for _, t := range g.Tools {
			if isDelegationTool(t) {
				continue
			}
			members = append(members, t)
		}
		if nowEnabled {
			def.Tools = appendUnique(def.Tools, members...)
			def.BannedTools = removeItems(def.BannedTools, members...)
		} else {
			def.Tools = removeItems(def.Tools, members...)
			def.BannedTools = appendUnique(def.BannedTools, members...)
		}
	}
}

// applyAgentSkillsSection maps the Skills form onto the definition, always
// setting (even to empty) so clearing skills reaches memory.
func applyAgentSkillsSection(def *AgentDefinition, c echo.Context) error {
	def.Skills = c.Request().Form["skill"]
	return nil
}

// applyAgentDelegationSection maps the Delegation form onto the definition:
// enabled with targets, or disabled. applyDelegation (run after every section)
// then syncs the spawn_agents/list_available_agents tools and spawnPolicy.
func applyAgentDelegationSection(def *AgentDefinition, c echo.Context) error {
	if c.FormValue("delegationEnabled") == "on" {
		targets := c.Request().Form["delegation-target"]
		if len(targets) == 0 {
			return fmt.Errorf("delegation requires at least one target")
		}
		def.Delegation = &Delegation{Enabled: true, Targets: targets}
	} else {
		def.Delegation = &Delegation{Enabled: false}
	}
	return nil
}

// loadAgentSettings assembles the agent settings payload for ONE subpage: the
// agent being edited plus only the catalog data that subpage renders. Each
// subpage's template is the source of truth for what must load here:
//   - general:     agent only (name / system prompt / language / appearance)
//   - model:       + project model config, providers, and the model catalog
//   - tools:       + MCP servers + relay node tools (the picker)
//   - skills:      + the skill list
//   - delegation:  + every other agent (the delegation-target picker)
//   - mcp:         agent only (endpoint/keys/sessions load via loadAgentMCP)
//
// GetAgentDefinition + deriveDelegation always run (the header/nav need the
// agent). Catalog fetches stay best-effort (captureError) so a failed fetch
// degrades only that subpage's content, never the whole page.
func (s *Server) loadAgentSettings(ctx context.Context, id string, data *agentSettingsData) error {
	agent, err := s.memory.GetAgentDefinition(ctx, id)
	if err != nil {
		return err
	}
	deriveDelegation(agent) // reconstruct Delegation for the form's prefill
	data.Agent = agent

	switch data.Section {
	case sectionModel:
		if mc, err := s.memory.GetProjectModelConfig(ctx); err != nil {
			captureError(err)
		} else {
			if mc != nil {
				data.DefaultModel = mc.GenerativeModel
			}
			data.ModelConfigKnown = true
		}
		if ps, err := s.memory.ListProjectProviders(ctx); err == nil {
			data.HasProviders = len(ps) > 0
			data.ProviderNames = projectProviderNames(ps)
			// Offer only configured providers' models — the global catalog
			// includes unconfigured providers, which would break chats.
			data.Models = s.configuredGenerativeModels(ctx, ps)
		} else {
			captureError(err)
		}
	case sectionTools:
		mcps, err := s.memory.ListMCPServers(ctx)
		captureError(err)
		data.MCPServers = mcps
		// Relay nodes are best-effort: when the backend relay API is
		// unreachable the relay groups are simply absent (see loadRelayNodes).
		data.RelayNodes = s.loadRelayNodes(ctx)
	case sectionSkills:
		skills, err := s.memory.ListSkills(ctx)
		captureError(err)
		data.Skills = skills
	case sectionDelegation:
		agents, err := s.memory.ListAgentDefinitions(ctx)
		captureError(err)
		data.Agents = agents
	}

	return nil
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

// --- capability-group Tools picker ---

// toolGroupPolicyPrefix marks a group policy entry inside ToolPolicies. Tool
// names are validated identifiers that cannot begin with "@", so the prefix
// cannot collide with a per-tool entry.
const toolGroupPolicyPrefix = "@group:"

// toolGroupOtherID is the display-only fallback group id (server's
// toolgroups.GroupOther). It is never a policy source: it gets an enable switch
// but no group policy select, and its nested relay/server sub-groups get no
// per-tool policy control.
const toolGroupOtherID = "other"

// toolGroupPolicyKey returns the ToolPolicies key for a group id.
func toolGroupPolicyKey(groupID string) string {
	return toolGroupPolicyPrefix + groupID
}

// groupPolicyValue returns a group's stored policy as "", "allow", "ask", or
// "deny" ("" = inherit the agent default).
func groupPolicyValue(agent *AgentDefinition, groupID string) string {
	return toolPolicyValue(agent, toolGroupPolicyKey(groupID))
}

// groupPolicyFormValue maps a stored group policy onto the select's value
// space, where "inherit" spells the absent policy.
func groupPolicyFormValue(v string) string {
	if v == "" {
		return "inherit"
	}
	return v
}

// groupWasEnabledValue renders the boolean baseline for the hidden
// groupWasEnabled.<id> field emitted next to each group enable switch, so the
// applier can tell whether the switch actually toggled.
func groupWasEnabledValue(enabled bool) string {
	if enabled {
		return "true"
	}
	return "false"
}

// policyTitle renders a policy value for an inheritance hint ("Ask", "Deny").
func policyTitle(v string) string {
	switch v {
	case "allow":
		return "Allow"
	case "ask":
		return "Ask"
	case "deny":
		return "Deny"
	default:
		return "Inherit"
	}
}

// toolPolicyHint explains what a tool row falls back to: its own explicit
// override when it has one, otherwise the owning capability group's policy (or
// the agent default when the group inherits too). "" means the row says
// nothing — e.g. an uncovered "Other" tool with no override.
func toolPolicyHint(agent *AgentDefinition, tool, groupLabel, groupPolicy string) string {
	if v := toolPolicyValue(agent, tool); v != "" {
		return "Override · " + policyTitle(v)
	}
	if groupLabel == "" {
		return ""
	}
	if groupPolicy != "" {
		return "Inherits " + groupLabel + " · " + policyTitle(groupPolicy)
	}
	return "Inherits " + groupLabel + " · Default"
}

// toolPolicyHintShort is the compact, all-widths form of toolPolicyHint
// ("→ Ask"), shown on narrow screens where the full sentence would crowd out
// the tool name.
func toolPolicyHintShort(agent *AgentDefinition, tool, groupLabel, groupPolicy string) string {
	if v := toolPolicyValue(agent, tool); v != "" {
		return "→ " + policyTitle(v)
	}
	if groupLabel == "" {
		return ""
	}
	if groupPolicy != "" {
		return "→ " + policyTitle(groupPolicy)
	}
	return "→ Default"
}

// toolRow resolves one tool into a picker row: its toggle state, the explicit
// per-tool policy value, and the inheritance hints for its owning group.
func toolRow(agent *AgentDefinition, name, description, groupLabel, groupPolicy string) agentToolRow {
	return agentToolRow{
		Name:        name,
		Description: description,
		Checked:     containsString(agent.Tools, name),
		PolicyValue: toolPolicyValue(agent, name),
		Hint:        toolPolicyHint(agent, name, groupLabel, groupPolicy),
		HintShort:   toolPolicyHintShort(agent, name, groupLabel, groupPolicy),
	}
}

// toolRows resolves names with their descriptions into picker rows.
func toolRows(agent *AgentDefinition, names []string, descriptions map[string]string, groupLabel, groupPolicy string) []agentToolRow {
	rows := make([]agentToolRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, toolRow(agent, name, descriptions[name], groupLabel, groupPolicy))
	}
	return rows
}

// anyChecked reports whether any name is currently allowed on the agent.
func anyChecked(agent *AgentDefinition, names []string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return containsString(agent.Tools, name) })
}

// toolGroupView is the resolved view of one capability group in the grouped
// picker: the server-supplied group, its stored policy, whether any member is
// currently enabled, and the nested source sub-groups plus direct member rows
// that make up its body.
type toolGroupView struct {
	Group        ToolGroup
	Policy       string // "inherit" | "allow" | "ask" | "deny"
	Enabled      bool
	Open         bool
	Count        int
	ServerGroups []agentToolGroupProps
	RelayGroups  []agentToolGroupProps
	Rows         []agentToolRow
}

// buildToolGroupViews resolves the server-supplied tool groups into render
// models. ToolGroup.Tools is the group's FULL membership — the project catalog
// for that group unioned with the agent's allowed and banned tools — so it
// drives the rows, the enable switch, and the enable/disable fan-out even when
// every member is currently disabled. A group with no member tools is dropped
// so the panel never renders an empty header.
//
// Each member renders exactly once: a tool offered by several sources is
// claimed by the first source (server order, then relay order) and any member
// no source offers renders as a direct row. This prevents duplicate
// input[name=tool] values inside one group and keeps a group member out of the
// "Other" fallback.
func buildToolGroupViews(data agentSettingsData, groups []ToolGroup) []toolGroupView {
	agent := data.Agent
	if agent == nil || len(groups) == 0 {
		return nil
	}
	descriptions := catalogToolDescriptions(data)
	views := make([]toolGroupView, 0, len(groups))
	for _, g := range groups {
		if len(g.Tools) == 0 {
			continue
		}
		// The server computes the group policy; prefer it, falling back to the
		// stored "@group:" entry when an older memory omits the field.
		policy := g.Policy
		if policy == "" {
			policy = groupPolicyValue(agent, g.ID)
		}
		inGroup := make(map[string]bool, len(g.Tools))
		for _, t := range g.Tools {
			inGroup[t] = true
		}
		rendered := make(map[string]bool, len(g.Tools))
		enabled := false
		for _, t := range g.Tools {
			if !isDelegationTool(t) && containsString(agent.Tools, t) {
				enabled = true
				break
			}
		}
		gv := toolGroupView{
			Group:   g,
			Policy:  groupPolicyFormValue(policy),
			Enabled: enabled,
			Open:    enabled,
			Count:   len(g.Tools),
		}
		for _, srv := range data.MCPServers {
			names := claimedServerToolNames(srv.Tools, inGroup, rendered)
			if len(names) == 0 {
				continue
			}
			markRendered(rendered, names)
			gv.ServerGroups = append(gv.ServerGroups, agentToolGroupProps{
				BorderClass:     "border-base-content/10 bg-base-100/60",
				BodyBorderClass: "border-base-content/10",
				Icon:            "lucide--server",
				IconClass:       "text-base-content/40",
				Title:           srv.Name,
				Count:           len(names),
				Open:            anyChecked(agent, names),
				Tools:           toolRows(agent, names, descriptions, g.Label, policy),
				// External MCP-server tools (the "other" group) get enable/disable
				// only; built-in capability groups keep per-tool policy controls.
				WithPolicy: g.ID != toolGroupOtherID,
			})
		}
		for _, node := range relayPickerGroups(data.RelayNodes) {
			names := claimedRelayToolNames(node, inGroup, rendered)
			if len(names) == 0 {
				continue
			}
			markRendered(rendered, names)
			gv.RelayGroups = append(gv.RelayGroups, agentToolGroupProps{
				BorderClass:     "border-primary/25 bg-primary/[0.04]",
				BodyBorderClass: "border-primary/15",
				Icon:            "lucide--radio",
				IconClass:       "text-primary/60",
				Title:           node.Session.InstanceID,
				TitleMono:       true,
				RemoteBadge:     true,
				Count:           len(names),
				Open:            anyChecked(agent, names),
				Tools:           toolRows(agent, names, descriptions, g.Label, policy),
				// Relay-node tools get enable/disable only, no policy control
				// (deferred non-goal).
				WithPolicy: false,
			})
		}
		// Direct member rows: group members no server or relay offers (native
		// tools) still belong to the group and render inline. A member is only
		// skipped here when an earlier source already claimed it, or when it is
		// delegation-managed (never rendered inside a capability group).
		for _, t := range g.Tools {
			if isDelegationTool(t) {
				continue
			}
			if rendered[t] {
				continue
			}
			rendered[t] = true
			gv.Rows = append(gv.Rows, toolRow(agent, t, descriptions[t], g.Label, policy))
		}
		views = append(views, gv)
	}
	return views
}

// claimedServerToolNames returns a registry server's tool names that belong to
// the group and have not been claimed by an earlier source, in server order.
func claimedServerToolNames(tools []MCPTool, members, rendered map[string]bool) []string {
	var names []string
	for _, t := range tools {
		if members[t.ToolName] && !rendered[t.ToolName] {
			names = append(names, t.ToolName)
		}
	}
	return names
}

// claimedRelayToolNames is claimedServerToolNames for a relay node's
// agent-facing <instance>_<tool> names.
func claimedRelayToolNames(node relayNode, members, rendered map[string]bool) []string {
	var names []string
	for _, t := range node.Tools {
		name := relayAgentToolName(node.Session.InstanceID, t.Name)
		if members[name] && !rendered[name] {
			names = append(names, name)
		}
	}
	return names
}

// markRendered records names as claimed so no later source or direct row
// repeats them inside the same group.
func markRendered(rendered map[string]bool, names []string) {
	for _, n := range names {
		rendered[n] = true
	}
}

// catalogToolDescriptions maps every catalog tool name to its description, so
// direct (source-less) group rows and the Other group can still show one.
func catalogToolDescriptions(data agentSettingsData) map[string]string {
	descriptions := map[string]string{}
	for _, srv := range data.MCPServers {
		for _, t := range srv.Tools {
			if t.ToolName == "" {
				continue
			}
			if _, ok := descriptions[t.ToolName]; !ok {
				descriptions[t.ToolName] = t.Description
			}
		}
	}
	for _, node := range data.RelayNodes {
		for _, t := range node.Tools {
			name := relayAgentToolName(node.Session.InstanceID, t.Name)
			if _, ok := descriptions[name]; !ok {
				descriptions[name] = t.Description
			}
		}
	}
	return descriptions
}

// groupedOtherRows returns rows for tools the rendered groups do not cover —
// the agent's allowed and banned tools plus any catalog tool (registry server
// or relay node) whose capability group is missing — so a taxonomy gap can
// never drop a tool from the picker on save. A tool that any group lists as a
// member is covered and stays out of Other. Delegation-managed tools are
// excluded (the delegation toggle owns them).
func groupedOtherRows(data agentSettingsData, views []toolGroupView) []agentToolRow {
	agent := data.Agent
	if agent == nil {
		return nil
	}
	covered := map[string]bool{}
	for _, gv := range views {
		for _, t := range gv.Group.Tools {
			covered[t] = true
		}
	}
	descriptions := catalogToolDescriptions(data)
	names := slices.Clone(agent.Tools)
	names = append(names, agent.BannedTools...)
	for _, srv := range data.MCPServers {
		for _, t := range srv.Tools {
			names = append(names, t.ToolName)
		}
	}
	for _, node := range data.RelayNodes {
		names = append(names, node.agentToolNames()...)
	}
	seen := map[string]bool{}
	var rows []agentToolRow
	for _, name := range names {
		if name == "" || covered[name] || seen[name] || isDelegationTool(name) {
			continue
		}
		seen[name] = true
		rows = append(rows, agentToolRow{
			Name:        name,
			Description: descriptions[name],
			Checked:     containsString(agent.Tools, name),
			PolicyValue: toolPolicyValue(agent, name),
		})
	}
	return rows
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

// agentAutoModelName returns the model the agent would fall back to when the
// Auto option is selected (i.e. only when the agent has no explicit model),
// or "" when the agent is explicitly pinned or nothing resolves. It keeps the
// model picker's Auto label identical to what the dashboard shows for auto
// agents.
func agentAutoModelName(agent *AgentDefinition, pinnedDefault string) string {
	if agent == nil {
		return ""
	}
	if agent.Model != nil && agent.Model.Name != "" {
		return ""
	}
	name, _ := agentModelName(agent, pinnedDefault)
	return name
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
// served. For an agent with no explicit model the PINNED project default
// decides: "error" when the project has no configured provider, "warning" when
// providers exist but the model is only a provider-credential fallback (no
// pinned project default), none when a pinned project default resolves. Memory
// resolves a provider-credential fallback into EffectiveModel whenever a
// configured credential carries a generative model (it stays empty otherwise),
// so that fallback must not suppress the "isn't pinned" warning. pinnedDefaultKnown
// false means the project model-config fetch failed — the legacy rule (any
// resolvable model suppresses the warning) is kept so a transient fetch error
// never fabricates a warning. An explicit model is "error" when the project has
// no configured provider or its provider prefix matches no configured provider;
// a bare model (no "/") with providers configured is treated as satisfied, and
// a matching provider is satisfied even when the model name isn't in its
// catalog (custom base URLs).
func classifyAgentModelIssue(agent *AgentDefinition, pinnedDefault string, pinnedDefaultKnown, hasProviders bool, providerNames []string) agentModelIssue {
	if agent == nil {
		return agentModelIssue{}
	}
	if agent.Model == nil || agent.Model.Name == "" {
		if !hasProviders {
			return agentModelIssue{sev: "error"}
		}
		if !pinnedDefaultKnown {
			if resolved, _ := agentModelName(agent, pinnedDefault); resolved != "" {
				return agentModelIssue{}
			}
			return agentModelIssue{sev: "warning"}
		}
		if pinnedDefault == "" {
			return agentModelIssue{sev: "warning"}
		}
		return agentModelIssue{}
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
func agentModelDashboardIssue(agent *AgentDefinition, pinnedDefault string, pinnedDefaultKnown, hasProviders bool, providerNames []string) (sev, msg string) {
	issue := classifyAgentModelIssue(agent, pinnedDefault, pinnedDefaultKnown, hasProviders, providerNames)
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
func agentModelSettingsIssue(agent *AgentDefinition, pinnedDefault string, pinnedDefaultKnown, hasProviders bool, providerNames []string) (sev, msg string) {
	issue := classifyAgentModelIssue(agent, pinnedDefault, pinnedDefaultKnown, hasProviders, providerNames)
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
// The provider and image catalogs are best-effort — a provider-list failure
// keeps the page rendering and raises an inline availability warning, not the
// whole-page error.
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
	if err != nil {
		captureError(err)
		data.ProviderListErr = true
	}
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

// sandboxProviderDisplayName is the provider name shown in the status list,
// falling back to the type when the API omits a name.
func sandboxProviderDisplayName(p SandboxProvider) string {
	if p.Name != "" {
		return p.Name
	}
	return p.Type
}

// sandboxProviderOptionLabel is the option text for a provider: the plain name
// when healthy, otherwise the name with an explicit unavailable suffix so the
// disabled state is unmistakable without relying on colour alone.
func sandboxProviderOptionLabel(p SandboxProvider) string {
	name := sandboxProviderDisplayName(p)
	if p.Healthy {
		return name
	}
	return name + " — unavailable"
}

// sandboxProviderListed reports whether typ appears in the provider list.
func sandboxProviderListed(providers []SandboxProvider, typ string) bool {
	return slices.ContainsFunc(providers, func(p SandboxProvider) bool { return p.Type == typ })
}

// sandboxSavedProviderLabel is the option text for a stored provider the API
// did not report: a friendly name when the type is known, else the raw type,
// always marked unavailable because its state cannot be confirmed.
func sandboxSavedProviderLabel(typ string) string {
	name := map[string]string{
		"gvisor":      "gVisor (Docker)",
		"firecracker": "Firecracker",
		"e2b":         "E2B",
	}[typ]
	if name == "" {
		name = typ
	}
	return name + " — unavailable"
}

// sandboxProviderWarning explains why the availability list is empty, so the
// user knows a failed fetch differs from "no providers configured".
func sandboxProviderWarning(data agentSandboxData) string {
	if data.ProviderListErr {
		return "Could not load sandbox provider availability, so Auto may pick a provider that is not available."
	}
	return "No sandbox providers were reported. Sandbox workspaces may be disabled on this server."
}
