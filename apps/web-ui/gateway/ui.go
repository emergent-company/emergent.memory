package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/components/layout"
	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/emergent-company/go-daisy/render"
	"github.com/emergent-company/go-daisy/shared"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

// appBrand is the product name rendered in the browser title, the standalone
// web-app title, and the navigation brand label.
const appBrand = "Memory"

// pageTitle builds the browser <title> for a page. It trims and drops blank
// segments, appends the brand last, and joins everything with " — " so every
// page ends with the same pageTitle("<label>") shape.
func pageTitle(segments ...string) string {
	parts := make([]string, 0, len(segments)+1)
	for _, s := range segments {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	parts = append(parts, appBrand)
	return strings.Join(parts, " — ")
}

// pageTestID derives a stable data-testid anchor for the main content root from
// the page title's first segment: "Agents — Memory" → "page-agents". Used by the
// e2e suite to navigate deterministically (see tests/e2e/README.md).
func pageTestID(title string) string {
	seg := title
	if i := strings.Index(title, " — "); i >= 0 {
		seg = title[:i]
	}
	var b strings.Builder
	b.WriteString("page-")
	dash := false
	for _, r := range strings.ToLower(seg) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// sidebarGroups returns the app navigation. Active state is derived per
// request via layout.ActiveSidebarGroups.
func sidebarGroups() []layout.SidebarGroup {
	return []layout.SidebarGroup{
		{
			// Ungrouped top-level entries: pulled out of their groups to sit
			// at the very top of the sidebar.
			Items: []layout.SidebarItem{
				{Label: "Approvals", Href: "/settings/approvals", Icon: "lucide--shield-check"},
				{Label: "Chat", Href: "/chat", Icon: "lucide--messages-square"},
				{Label: "Sessions", Href: "/sessions", Icon: "lucide--history"},
				{Label: "Usage", Href: "/usage", Icon: "lucide--chart-column"},
			},
		},
		{
			Label: "Agents",
			Items: []layout.SidebarItem{
				{Label: "Agents", Href: "/agents", Icon: "lucide--bot"},
				{Label: "Schedules", Href: "/schedules", Icon: "lucide--calendar-clock"},
			},
		},
		{
			Label: "Memory Browser",
			Items: []layout.SidebarItem{
				{Label: "Objects", Href: "/objects", Icon: "lucide--box"},
				{Label: "Schema", Href: "/schema", Icon: "lucide--git-branch"},
				{Label: "Documents", Href: "/documents", Icon: "lucide--file-text"},
				{Label: "Backups", Href: "/backups", Icon: "lucide--archive"},
			},
		},
		{
			Label: "Settings",
			Items: []layout.SidebarItem{
				{Label: "Project", Href: "/settings", Icon: "lucide--settings"},
				{Label: "API Tokens", Href: "/settings/tokens", Icon: "lucide--key-round"},
				{Label: "MCP Servers", Href: "/settings/mcp-servers", Icon: "lucide--server"},
				{Label: "Blueprints", Href: "/blueprints", Icon: "lucide--library"},
				{Label: "Skills", Href: "/skills", Icon: "lucide--sparkles"},
			},
		},
	}
}

// orgSidebarGroups returns the organization-scoped navigation shown when an
// organization (not a project) is the active context. Links are path-scoped to
// the active org; the Settings hub holds the tool overrides and the
// delete-org danger zone.
func orgSidebarGroups(orgID string) []layout.SidebarGroup {
	base := "/orgs/" + url.PathEscape(orgID)
	return []layout.SidebarGroup{
		{
			Label: "Organization",
			Items: []layout.SidebarItem{
				{Label: "Projects", Href: base, Icon: "lucide--folder"},
				{Label: "Members", Href: base + "/members", Icon: "lucide--users"},
				{Label: "Settings", Href: base + "/settings", Icon: "lucide--settings"},
			},
		},
	}
}

// partialWithTitle prefixes an hx-boost partial fragment with a <title> element
// so htmx keeps document.title in sync during in-page navigation. Partial
// responses have no <head> (the shell's title lives there), and htmx extracts
// the first <title> from a swapped response and applies it to document.title
// before the innerHTML swap — without this, tab titles go stale on every
// boosted navigation. The title is HTML-escaped (labels can carry user text).
func partialWithTitle(title string, content templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, "<title>"+template.HTMLEscapeString(title)+"</title>"); err != nil {
			return err
		}
		return content.Render(ctx, w)
	})
}

// flashToast renders PRG feedback (create/update/delete flows) as an
// auto-dismissing toast. Navigation is hx-boosted (in-place swaps), so the
// message must reach the Alpine toast queue two ways:
//  1. when the shell is already loaded (boosted swap), push straight into the
//     #toast-container Alpine queue;
//  2. on a full page load the Alpine runtime is not up yet when this inline
//     script runs, so fall back to stashing under the "memory-toast" key that
//     app.js's flushStashedToast reads on DOMContentLoaded.
//
// Go's encoding/json escapes < as \u003c, so message text cannot break out of
// the <script> block.
func flashToast(kind, message string) templ.Component {
	payload, err := json.Marshal(map[string]string{"kind": kind, "message": message})
	if err != nil {
		payload = []byte(`{"kind":"info","message":""}`)
	}
	encoded, err := json.Marshal(string(payload))
	if err != nil {
		encoded = []byte(`""`)
	}
	script := `(function(){try{var p=JSON.parse(` + string(encoded) + `);var c=document.getElementById("toast-container");if(c&&window.Alpine){var q=window.Alpine.$data(c);if(q&&q.add){q.add({type:p.kind,message:p.message,duration:4200});return;}}sessionStorage.setItem("memory-toast",JSON.stringify(p));}catch(e){}})();`
	return templ.Raw(`<script type="text/javascript">` + script + `</script>`)
}

// redirectWithError redirects to path, carrying the real error message as ?err=
// so the target page can surface it instead of a generic "operation failed".
func redirectWithError(c echo.Context, path string, err error) error {
	return c.Redirect(http.StatusSeeOther, path+"?err="+url.QueryEscape(err.Error()))
}

// flashError decodes the ?err= query param into an error for PRG flash feedback.
// "1" is the legacy sentinel for a generic failure; any other value is the
// actual error message surfaced by redirectWithError.
func flashError(c echo.Context) error {
	msg := c.QueryParam("err")
	if msg == "" {
		return nil
	}
	if msg == "1" {
		return fmt.Errorf("operation failed")
	}
	return fmt.Errorf("%s", msg)
}

// page renders content through the go-daisy render package. Full browser
// loads and htmx history restores (browser back/forward) get the sidebar
// shell; inline HTMX partials (sidebar navigation, boosted links) get the bare
// content for swapping into #main-content.
//
// History restores must return the full shell. htmx v4 restores by selecting
// the [hx-history-elt] element out of the response (see hx-history-elt on
// <main>) and outerSync-swapping it into #main-content. A bare fragment has no
// such element, so the swap would fail; without an hx-history-elt at all htmx
// falls back to targeting document.body, whose outerSync swap replaces the
// stylesheet <link> and scripts that live in <body> (ui.templ), leaving a
// partial page with no CSS or JS.
func (s *Server) page(c echo.Context, title string, content templ.Component) error {
	w, r := c.Response().Writer, c.Request()
	if render.IsPartial(r) {
		// The shell navigates via hx-boost (in-place swaps), so the <title>
		// lives only in the full page's <head> — partials would otherwise leave
		// document.title stale on every navigation. htmx extracts the first
		// <title> from a swapped response and applies it to document.title, so
		// the partial carries one alongside the content.
		render.RenderPartial(w, r, partialWithTitle(title, content))
		return nil
	}
	agents, err := s.memory.ListAgentDefinitions(c.Request().Context())
	captureError(err)
	assistant := s.assistantAgentID(c.Request().Context(), agents)
	// Tenancy chrome for the shell's project switcher (best-effort, same
	// style as the agents fetch above): projects/orgs for the dropdown, the
	// active project resolved against the list — from the session when one is
	// attached, else the server's static MemoryProjectID (dev/API-key mode) —
	// and its org name for the "Org / Project" trigger label.
	projects, err := s.memory.ListProjects(c.Request().Context())
	captureError(err)
	orgs, err := s.memory.ListOrgs(c.Request().Context())
	captureError(err)
	activeProjectID := ""
	var activeOrgID string
	if sc, ok := sessionContextFrom(c.Request().Context()); ok {
		activeProjectID = sc.ProjectID
		activeOrgID = sc.OrgID
	} else {
		activeProjectID = s.cfg.MemoryProjectID
	}
	var current *ProjectRef
	if activeProjectID != "" {
		for i := range projects {
			if projects[i].ID == activeProjectID {
				current = &projects[i]
				break
			}
		}
	}
	currentOrgName := ""
	if current != nil {
		currentOrgName = orgNameFor(orgs, current.OrgID)
	}
	// Active org for the org-context shell (picker trigger + org sidebar) and
	// the new-project modal's default org. Resolve from the session org whenever
	// it is set — including when a project is active (its org is the session
	// org) — so the modal defaults to the current project's org instead of
	// "Select an organization".
	var activeOrg *Org
	if activeOrgID != "" {
		for i := range orgs {
			if orgs[i].ID == activeOrgID {
				activeOrg = &orgs[i]
				break
			}
		}
	}
	// Sidebar selection by context: project → project nav; org → org nav;
	// none → no sidebar (wizard / account pages).
	var groups []layout.SidebarGroup
	providersMissing := false
	if activeProjectID != "" {
		groups = layout.ActiveSidebarGroups(sidebarGroups(), r.URL.Path)
		// Warn in the settings nav when the active project has zero configured
		// LLM providers. Fail-safe helper: false on lookup errors.
		providersMissing = s.projectHasNoProviders(c.Request().Context())
	} else if activeOrgID != "" {
		groups = layout.ActiveSidebarGroups(orgSidebarGroups(activeOrgID), r.URL.Path)
	}
	// Recent projects for the picker's "Recent" section (only when the user
	// has more than ten projects). Recent ids come from the durable cookie,
	// filtered to still-accessible projects.
	var recent []ProjectRef
	showRecent := len(projects) > 10
	if showRecent {
		if rc, ok := s.readRecentProjects(c); ok {
			var sub string
			if sc, ok := sessionContextFrom(c.Request().Context()); ok {
				sub = sc.Sub
			}
			for _, id := range rc.Recent[sub] {
				for i := range projects {
					if projects[i].ID == id {
						recent = append(recent, projects[i])
						break
					}
				}
			}
		}
	}
	// Signed-in identity for the account menu: show the menu for any session
	// with a known account sub, even when the session lacks a name/email
	// (Zitadel may not emit them). Fill the display name and email lazily from
	// the Memory profile when the IdP token omits them. Dev/API-key requests
	// have no session → no menu.
	var user *currentUser
	var accounts []currentUser
	if sc, ok := sessionContextFrom(c.Request().Context()); ok && sc.Sub != "" {
		user = &currentUser{Name: sc.Name, Email: sc.Email, Picture: resolveAvatar(sc.AvatarOverrideURL, sc.Picture), Sub: sc.Sub}
		if user.Name == "" || user.Email == "" {
			if p, err := s.memory.GetProfile(c.Request().Context()); err == nil && p != nil {
				if user.Name == "" {
					user.Name = profileDisplayNameOf(p)
				}
				if user.Email == "" {
					user.Email = p.Email
				}
			}
		}
		if installID, ok := s.readInstallID(c); ok {
			for _, a := range s.reg().list(installID) {
				accounts = append(accounts, currentUser{Name: a.Name, Email: a.Email, Picture: resolveAvatar(a.AvatarOverrideURL, a.Picture), Sub: a.Sub})
			}
		}
	}
	render.RenderPage(w, r, appShell(title, groups, providersMissing, agents, assistant, current, currentOrgName, activeOrg, groupProjectsByOrg(projects, orgs), orgs, recent, showRecent, user, accounts, content, s.cfg.SentryDSN, s.cfg.SentryEnvironment, s.cfg.SentryTracesSampleRate, s.cfg.SentryReplaySessionSampleRate, s.cfg.SentryReplayOnErrorSampleRate))
	return nil
}

// currentUser is the signed-in identity shown in the account menu.
type currentUser struct {
	Name    string
	Email   string
	Picture string // avatar URL, may be empty
	Sub     string // stable account id (Zitadel subject)
}

// initials returns the first letters of up to two space-separated words of
// name, uppercased ("Ada Lovelace" → "AL", "ada" → "A", "" → ""). Rune-safe:
// a multi-byte first character is kept whole.
func initials(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	first, _ := utf8.DecodeRuneInString(fields[0])
	out := strings.ToUpper(string(first))
	if len(fields) > 1 {
		second, _ := utf8.DecodeRuneInString(fields[1])
		out += strings.ToUpper(string(second))
	}
	return out
}

// resolveAvatar picks the avatar URL to display: the uploaded Memory photo
// (override) wins when present, else the IdP picture, else "" (initials).
func resolveAvatar(override, picture string) string {
	if override != "" {
		return override
	}
	return picture
}

// uiAgents renders the agents management page (list + create/edit/delete).
// The model catalog and the skill list are best-effort: the create/edit
// dialog falls back to a free-choice model list and an empty skill picker
// when either is unreachable.
func (s *Server) uiAgents(c echo.Context) error {
	ctx := c.Request().Context()
	var (
		agents    []AgentDefinitionSummary
		agentsErr error
		models    []Model
		modelsErr error
		skills    []Skill
		skillsErr error
	)
	var g errgroup.Group
	g.Go(func() error { agents, agentsErr = s.memory.ListAgentDefinitions(ctx); return nil })
	g.Go(func() error { models, modelsErr = s.memory.ListModels(ctx); return nil })
	g.Go(func() error { skills, skillsErr = s.memory.ListSkills(ctx); return nil })
	_ = g.Wait()
	if agentsErr != nil {
		return s.page(c, pageTitle("Agents"), AgentsPage(nil, nil, nil, agentsErr))
	}
	captureError(modelsErr)
	captureError(skillsErr)

	// Per-agent model info rides on the list response now (memory reports
	// effectiveModel per summary), so there is no per-agent GET round-trip
	// here — the agents page renders the card grid straight from the list.
	return s.page(c, pageTitle("Agents"), AgentsPage(agents, models, skills, nil))
}

// chatRailData loads the session-rail data (agents, agent name map, past
// conversations, and scheduled-run rows) shared by the full chat page and the
// /partial/chat-rail refresh endpoint.
func (s *Server) chatRailData(ctx context.Context) (agents []AgentDefinitionSummary, agentNames map[string]string, convs *ConversationList, schedRuns []scheduledRunRow, err error) {
	agents, err = s.memory.ListAgentDefinitions(ctx)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	agentNames = map[string]string{}
	for _, a := range agents {
		agentNames[a.ID] = a.Name
	}
	convs, cerr := s.memory.ListConversations(ctx)
	if cerr != nil {
		convs = &ConversationList{}
	}
	schedRuns = s.chatScheduledRuns(ctx, agentNames)
	return agents, agentNames, convs, schedRuns, nil
}

// chatModelWarnings returns agentID → message for every agent whose chats
// cannot currently run (the model-availability error state), or an empty map
// when none apply. Best-effort: a failed default-model or provider fetch
// degrades to "no default / no providers" (captured, mirroring uiAgent); an
// agent whose definition can't be fetched is skipped. The message text comes
// from agentModelDashboardIssue so the chat banner matches the agent
// dashboard/settings warnings exactly.
func (s *Server) chatModelWarnings(ctx context.Context, agents []AgentDefinitionSummary) map[string]string {
	warnings := map[string]string{}
	if len(agents) == 0 {
		return warnings
	}
	var defaultModel string
	if mc, err := s.memory.GetProjectModelConfig(ctx); err == nil && mc != nil {
		defaultModel = mc.GenerativeModel
	} else if err != nil {
		captureError(err)
	}
	providers := []string{}
	if ps, err := s.memory.ListProjectProviders(ctx); err == nil {
		providers = projectProviderNames(ps)
	} else {
		captureError(err)
	}
	defs := make([]*AgentDefinition, len(agents))
	runConcurrently(len(agents), func(i int) {
		def, err := s.memory.GetAgentDefinition(ctx, agents[i].ID)
		if err != nil {
			return // agent vanished between the rail list and here
		}
		defs[i] = def
	})
	for i, def := range defs {
		if def == nil {
			continue
		}
		if sev, msg := agentModelDashboardIssue(def, defaultModel, len(providers) > 0, providers); sev == "error" {
			warnings[agents[i].ID] = msg
		}
	}
	return warnings
}

// uiChat renders the chat workspace (the primary sessions surface):
// agent picker, streaming log, and a resumable session list.
// ?agent=<id> preselects an agent, ?c=<conversationId> resumes a conversation,
// ?prompt=<msg> pre-fills (and auto-sends) a first message.
func (s *Server) uiChat(c echo.Context) error {
	ctx := c.Request().Context()
	agents, agentNames, convs, schedRuns, err := s.chatRailData(ctx)
	if err != nil {
		return s.page(c, pageTitle("Chat"), ChatPage(nil, nil, nil, nil, "", "", "", err, true, nil))
	}
	modelWarnings := s.chatModelWarnings(ctx, agents)
	return s.page(c, pageTitle("Chat"), ChatPage(agents, convs, agentNames, schedRuns, c.QueryParam("agent"), c.QueryParam("c"), c.QueryParam("prompt"), nil, s.voiceEnabled(c.Request().Context()), modelWarnings))
}

// uiChatRail returns the session-rail list HTML as a fragment, so chat.js can
// refresh the rail after a new conversation appears without rebuilding the
// row markup client-side.
func (s *Server) uiChatRail(c echo.Context) error {
	_, agentNames, convs, schedRuns, err := s.chatRailData(c.Request().Context())
	if err != nil {
		convs = &ConversationList{}
		agentNames = map[string]string{}
		schedRuns = nil
	}
	render.RenderPartial(c.Response().Writer, c.Request(), chatRailList(convs, agentNames, schedRuns, c.QueryParam("c")))
	return nil
}

// scheduledRunRow is one scheduled-agent run rendered in the chat session
// rail (best-effort; a fetch failure yields an empty list).
type scheduledRunRow struct {
	ID        string
	AgentName string
	Status    string
	StartedAt string
	Summary   string
}

// chatScheduledRuns collects recent runs of schedule-triggered agents for the
// chat session rail. Only agents with triggerType "schedule" contribute; runs
// of manual/reaction/webhook agents are not on-demand sessions.
func (s *Server) chatScheduledRuns(ctx context.Context, agentNames map[string]string) []scheduledRunRow {
	agents, err := s.memory.ListScheduledAgents(ctx)
	if err != nil {
		return nil
	}
	var out []scheduledRunRow
	for _, a := range agents {
		if a.TriggerType != "schedule" {
			continue
		}
		runs, err := s.memory.ListScheduledAgentRuns(ctx, a.ID)
		if err != nil {
			continue
		}
		for _, r := range runs {
			out = append(out, scheduledRunRow{
				ID:        r.ID,
				AgentName: a.Name,
				Status:    r.Status,
				StartedAt: r.StartedAt,
				Summary:   scheduledRunPreview(r, a.Name),
			})
		}
	}
	return out
}

// modelGroups buckets the model catalog by provider for optgroup rendering.
func modelGroups(models []Model) []modelGroup {
	order := []string{}
	seen := map[string]bool{}
	for _, m := range models {
		if !seen[m.Provider] {
			seen[m.Provider] = true
			order = append(order, m.Provider)
		}
	}
	out := make([]modelGroup, 0, len(order))
	for _, p := range order {
		g := modelGroup{Provider: p}
		for _, m := range models {
			if m.Provider == p {
				g.Models = append(g.Models, m)
			}
		}
		out = append(out, g)
	}
	return out
}

// modelDisplay prefers the catalog's display name, falling back to
// provider/modelName for models without one.
func modelDisplay(m Model) string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	if m.Provider != "" {
		return m.Provider + "/" + m.ModelName
	}
	return m.ModelName
}

// modelGroup is one provider bucket of catalog models.
type modelGroup struct {
	Provider string
	Models   []Model
}

// --- display helpers (shared by templates) ---

// plural returns "s" for counts other than 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// countLabel renders a count with correct pluralisation ("1 memory" /
// "7 memories"). singular/plural are the noun forms.
func countLabel(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}

// shortID truncates an opaque ID for table display.
func shortID(id string) string {
	if len(id) <= 10 {
		return id
	}
	return id[:10]
}

// relTime humanizes an RFC3339 timestamp via shared.RelativeTime. Unlike the
// upstream helper (which returns "—" on a parse error), the app preserves the
// raw timestamp when it can't be parsed, so the wrapper falls back to iso when
// RelativeTime reports "—" for a non-empty input.
func relTime(iso string) string {
	if iso == "" {
		return "—"
	}
	if out := shared.RelativeTime(iso); out != "—" {
		return out
	}
	return iso
}

// flowLabel maps a flow type to a display label.
func flowLabel(flow string) string {
	switch strings.ToLower(flow) {
	case "", "flow", "agentic":
		return "agentic"
	case "linear":
		return "linear"
	default:
		return flow
	}
}

// visibilityIntent maps visibility to a badge color. Valid memory values are
// external (ACP + admin UI), project (admin UI only), and internal (other
// agents only); the widest reach reads as the strongest intent.
func visibilityIntent(v string) ui.BadgeIntent {
	switch strings.ToLower(v) {
	case "external":
		return ui.BadgeSuccess
	case "project":
		return ui.BadgeInfo
	case "internal":
		return ui.BadgeNeutral
	default:
		return ui.BadgeGhost
	}
}

// visibilityLabel displays a friendly visibility label. Memory defaults an
// agent to project visibility, so an empty value renders as "project".
func visibilityLabel(v string) string {
	if v == "" {
		return "project"
	}
	return v
}

// chatPlaceholder picks a placeholder mentioning the active agent name.
func chatPlaceholder(agents []AgentDefinitionSummary, preselect string) string {
	name := "Memory"
	id := defaultAgentID(agents, preselect)
	for _, a := range agents {
		if a.ID == id {
			name = a.Name
			break
		}
	}
	return "Message " + name + "…"
}

// defaultAgentID returns the preselect id if valid, else the first agent id.
func defaultAgentID(agents []AgentDefinitionSummary, preselect string) string {
	if preselect != "" {
		for _, a := range agents {
			if a.ID == preselect {
				return preselect
			}
		}
	}
	if len(agents) > 0 {
		return agents[0].ID
	}
	return ""
}

// assistantAgentID returns the configured assistant agent's ID, or "" when
// unset or the referenced agent no longer exists.
func (s *Server) assistantAgentID(ctx context.Context, agents []AgentDefinitionSummary) string {
	ps, err := s.memory.GetProjectSetting(ctx, settingsAssistantCategory, settingsAssistantKey)
	if err != nil || ps == nil {
		return ""
	}
	id, _ := ps.Value["agentId"].(string)
	if id == "" {
		return ""
	}
	for _, a := range agents {
		if a.ID == id {
			return id
		}
	}
	return ""
}

// --- memory browser helpers ---

// confidenceLabel renders a memory confidence (0..1) as a percentage.
func confidenceLabel(c float64) string {
	return strconv.Itoa(int(math.Round(c*100))) + "%"
}

// matchLabel renders a memory search relevance score (0..1) as a match
// indicator, e.g. "match 17%".
func matchLabel(score float64) string {
	return "match " + strconv.Itoa(int(math.Round(score*100))) + "%"
}

// memoryCategoryIntent maps a memory category to a badge colour. Unknown
// categories stay neutral.
func memoryCategoryIntent(c string) ui.BadgeIntent {
	switch strings.ToLower(c) {
	case "person":
		return ui.BadgePrimary
	case "contact":
		return ui.BadgeSecondary
	case "note":
		return ui.BadgeInfo
	case "task":
		return ui.BadgeWarning
	case "preference", "fact":
		return ui.BadgeAccent
	default:
		return ui.BadgeNeutral
	}
}

// memoryDetailURL builds the detail link for one memory, preserving the
// active search query.
func memoryDetailURL(id, query, memoryID string) string {
	u := "/agents/" + url.PathEscape(id) + "/memories?memory=" + url.QueryEscape(memoryID)
	if query != "" {
		u += "&q=" + url.QueryEscape(query)
	}
	return u
}

// memoryListURL builds the back-to-list link, preserving the search query.
func memoryListURL(id, query string) string {
	u := "/agents/" + url.PathEscape(id) + "/memories"
	if query != "" {
		u += "?q=" + url.QueryEscape(query)
	}
	return u
}

// chatTitle is the display title of a conversation, falling back to a
// shortened id for untitled chats.
func chatTitle(c Conversation) string {
	if c.Title != "" {
		return c.Title
	}
	return "Chat " + shortID(c.ID)
}

// --- document browser helpers ---

// documentName is the display name of a document, falling back to a shortened
// id for untitled documents.
func documentName(d Document) string {
	if d.Filename != "" {
		return d.Filename
	}
	return "Document " + shortID(d.ID)
}

// documentStatusIntent maps an extraction/processing status to a badge colour.
// Unknown statuses stay neutral.
func documentStatusIntent(status string) ui.BadgeIntent {
	switch strings.ToLower(status) {
	case "completed":
		return ui.BadgeSuccess
	case "failed", "dead_letter":
		return ui.BadgeError
	case "running", "processing":
		return ui.BadgeInfo
	case "pending", "queued":
		return ui.BadgeWarning
	default:
		return ui.BadgeNeutral
	}
}

// uiDocuments renders the documents page: a list of ingested documents plus an
// upload form. ?uploaded=1 surfaces an upload success message, ?err=1 an upload
// failure.
func (s *Server) uiDocuments(c echo.Context) error {
	docs, cursor, err := s.memory.ListDocuments(c.Request().Context(), "")
	var flashMsg string
	if c.QueryParam("uploaded") != "" {
		flashMsg = "Document uploaded."
	} else if c.QueryParam("duplicate") != "" {
		flashMsg = "Document already exists (identical content)."
	} else if c.QueryParam("deleted") != "" {
		flashMsg = "Document deleted."
	}
	flashErr := flashError(c)
	return s.page(c, pageTitle("Documents"), DocumentsPage(docs, cursor, err, flashMsg, flashErr))
}

// uiDocument renders one document's detail: metadata, extraction trigger, and
// a chunk preview. Chunks are best-effort (a chunk fetch failure still renders
// the page with the metadata and an empty chunk list).
func (s *Server) uiDocument(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	doc, err := s.memory.GetDocument(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Document"), DocumentDetailPage(nil, nil, extractionResults{}, err, "", nil))
	}

	chunks, err := s.memory.ListChunks(ctx, id)
	captureError(err)
	results := s.loadExtractionResults(ctx, id)
	var flashMsg string
	if c.QueryParam("extracted") != "" {
		flashMsg = "Extraction triggered."
	}
	flashErr := flashError(c)
	return s.page(c, pageTitle(documentName(*doc)), DocumentDetailPage(doc, chunks, results, nil, flashMsg, flashErr))
}

// uiUploadDocument handles the documents page upload form (PRG): it forwards
// the file to memory and redirects back to the list, or to ?err=1 on failure.
func (s *Server) uiUploadDocument(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return redirectWithError(c, "/documents", fmt.Errorf("upload: select a file"))
	}
	if file.Size > maxUploadSize {
		return redirectWithError(c, "/documents", fmt.Errorf("upload: file too large (max %d bytes)", maxUploadSize))
	}
	src, err := file.Open()
	if err != nil {
		return redirectWithError(c, "/documents", err)
	}
	defer func() { _ = src.Close() }()

	res, err := s.memory.UploadDocument(c.Request().Context(), file.Filename, src)
	if err != nil {
		return redirectWithError(c, "/documents", err)
	}
	if res.IsDuplicate {
		return c.Redirect(http.StatusSeeOther, "/documents?duplicate=1")
	}
	return c.Redirect(http.StatusSeeOther, "/documents?uploaded=1")
}

// uiTriggerExtraction handles the extraction trigger form (PRG): it creates an
// extraction job and redirects back to the document detail view.
func (s *Server) uiTriggerExtraction(c echo.Context) error {
	id := c.Param("id")
	if _, err := s.memory.CreateExtractionJob(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/documents/"+url.PathEscape(id), err)
	}
	return c.Redirect(http.StatusSeeOther, "/documents/"+url.PathEscape(id)+"?extracted=1")
}

// errDocumentIDRequired guards the document mutation routes: every one of them
// is project-scoped by the memory client (X-Project-ID from the session or the
// static project id), and none of them can act without a document id.
var errDocumentIDRequired = errors.New("document id is required")

// uiDeleteDocument handles the delete form on the document detail header and
// the per-row delete on the documents list (PRG, same shape as the project
// deletes): it forwards to memory and redirects back to the list with a
// ?deleted=1 flash, or to ?err=1 when memory rejects the deletion. HTMX
// submits (the boosted form / hx-confirm path) get an HX-Redirect; plain form
// posts get the 303.
func (s *Server) uiDeleteDocument(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return redirectWithError(c, "/documents", errDocumentIDRequired)
	}
	if err := s.memory.DeleteDocument(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/documents", err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/documents?deleted=1")
	return nil
}

// uiBlueprints renders the blueprint gallery: applied blueprints (unapply),
// the project's private drafts (install), and available packs (enable/install).
// Available packs are best-effort (a failure still renders the page).
func (s *Server) uiBlueprints(c echo.Context) error {
	ctx := c.Request().Context()
	var flashErr error
	var flashMsg string
	flashErr = flashError(c)
	if c.QueryParam("installed") != "" {
		flashMsg = "Blueprint installed."
	}
	if c.QueryParam("enabled") != "" {
		flashMsg = "Blueprint enabled."
	}
	if d := c.QueryParam("derived"); d != "" {
		flashMsg = "Draft " + d + " created."
	}

	var (
		applied    []AppliedBlueprint
		appliedErr error
		schemas    []SchemaInfo
		schemasErr error
		drafts     []BlueprintRecord
		existing   map[string]bool
	)
	var g errgroup.Group
	g.Go(func() error { applied, appliedErr = s.memory.ListAppliedBlueprints(ctx); return nil })
	g.Go(func() error { schemas, schemasErr = s.memory.ListAllSchemas(ctx); return nil })
	g.Go(func() error { drafts, _ = s.listPrivateDrafts(ctx); return nil })
	g.Go(func() error { existing = s.existingAgents(ctx); return nil })
	_ = g.Wait()

	if appliedErr != nil {
		return s.page(c, pageTitle("Blueprints"), BlueprintsPage(nil, nil, nil, nil, appliedErr, flashMsg, flashErr))
	}
	captureError(schemasErr)
	bundled, _ := s.bundledBlueprints()
	available := buildAvailableList(schemas, bundled, applied, existing)
	var upgrades, avail []AvailableSchemaItem
	for _, a := range available {
		if a.Upgrade {
			upgrades = append(upgrades, a)
		} else {
			avail = append(avail, a)
		}
	}
	return s.page(c, pageTitle("Blueprints"), BlueprintsPage(applied, upgrades, avail, drafts, nil, flashMsg, flashErr))
}

// --- skill browser helpers ---

// scopeIntent maps a skill scope to a badge colour, mirroring
// visibilityIntent: the widest reach reads as the strongest intent.
func scopeIntent(scope string) ui.BadgeIntent {
	switch strings.ToLower(scope) {
	case "global":
		return ui.BadgeSuccess
	case "org":
		return ui.BadgeInfo
	case "project":
		return ui.BadgeNeutral
	default:
		return ui.BadgeGhost
	}
}

// scopeLabel displays a friendly scope label. Skills created from the gateway
// are project-scoped, so an empty scope from an older service renders as
// "project".
func scopeLabel(scope string) string {
	if scope == "" {
		return "project"
	}
	return scope
}

// provenanceLabel renders a skill's source/license/version provenance as one
// "source · license · vX" string, or "" when nothing is recorded.
func provenanceLabel(m *SkillMetadata) string {
	if m == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if m.Source != "" {
		parts = append(parts, m.Source)
	}
	if m.License != "" {
		parts = append(parts, m.License)
	}
	if m.Version != "" {
		parts = append(parts, "v"+strings.TrimPrefix(m.Version, "v"))
	}
	return strings.Join(parts, " · ")
}

// skillUsage counts, for each skill name, how many agent definitions list it
// in their Skills. The list summary now carries Skills, so no per-agent fetch.
func (s *Server) skillUsage(agents []AgentDefinitionSummary) map[string]int {
	counts := map[string]int{}
	for _, a := range agents {
		for _, name := range a.Skills {
			counts[name]++
		}
	}
	return counts
}

// usageLabel renders a skill's agent-usage count as a short label.
func usageLabel(n int) string {
	switch n {
	case 0:
		return "Unused"
	case 1:
		return "1 agent"
	default:
		return strconv.Itoa(n) + " agents"
	}
}

// usageIntent maps a skill's agent-usage count to a badge colour.
func usageIntent(n int) ui.BadgeIntent {
	if n == 0 {
		return ui.BadgeGhost
	}
	return ui.BadgeInfo
}

// uiSkills renders the skills management page: the create form plus the list
// of skills. A failed list fetch renders the whole-page error state;
// ?created=1 / ?deleted=1 / ?err=1 surface PRG feedback from the
// create/delete flows.
func (s *Server) uiSkills(c echo.Context) error {
	ctx := c.Request().Context()
	var (
		skills    []Skill
		skillsErr error
		agents    []AgentDefinitionSummary
		agentErr  error
	)
	var g errgroup.Group
	g.Go(func() error { skills, skillsErr = s.memory.ListSkills(ctx); return nil })
	g.Go(func() error { agents, agentErr = s.memory.ListAgentDefinitions(ctx); return nil })
	_ = g.Wait()

	var flashMsg string
	flashErr := flashError(c)
	switch {
	case c.QueryParam("created") != "":
		flashMsg = "Skill created."
	case c.QueryParam("deleted") != "":
		flashMsg = "Skill deleted."
	}
	captureError(agentErr)
	assistant := s.assistantAgentID(ctx, agents)
	usedBy := s.skillUsage(agents)
	return s.page(c, pageTitle("Skills"), SkillsPage(skills, skillsErr, flashMsg, flashErr, assistant, usedBy))
}

// uiSkill renders one skill: scope + provenance badges, the full content, the
// edit form (description + content only — name is immutable), and a delete
// button with a confirm dialog. ?updated=1 / ?err=1 surface PRG feedback from
// the update flow.
func (s *Server) uiSkill(c echo.Context) error {
	id := c.Param("id")
	skill, err := s.memory.GetSkill(c.Request().Context(), id)
	var flashMsg string
	flashErr := flashError(c)
	if c.QueryParam("updated") != "" {
		flashMsg = "Skill updated."
	}
	if err != nil {
		return s.page(c, pageTitle("Skill"), SkillDetailPage(nil, err, "", nil))
	}
	return s.page(c, pageTitle(skill.Name), SkillDetailPage(skill, nil, flashMsg, flashErr))
}

// uiNewSkill renders the create form page. ?err=1 surfaces PRG feedback from
// the create flow.
func (s *Server) uiNewSkill(c echo.Context) error {
	return s.page(c, pageTitle("New skill"), SkillNewPage(flashError(c)))
}

// uiCreateSkill handles the create form on the new-skill page (PRG): it
// forwards the fields to memory and redirects back to the list, or to
// ?err=1 when memory rejects the values (invalid slug, empty
// description/content, content over 1 MiB).
func (s *Server) uiCreateSkill(c echo.Context) error {
	ctx := c.Request().Context()
	name := c.FormValue("name")
	description := c.FormValue("description")
	content := c.FormValue("content")
	if _, err := s.memory.CreateSkill(ctx, name, description, content); err != nil {
		return redirectWithError(c, "/skills/new", err)
	}
	return c.Redirect(http.StatusSeeOther, "/skills?created=1")
}

// uiUpdateSkill handles the edit form on the detail page (PRG). Only
// description and content are sent — name and scope are not updatable.
func (s *Server) uiUpdateSkill(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	description := c.FormValue("description")
	content := c.FormValue("content")
	if _, err := s.memory.UpdateSkill(ctx, id, description, content); err != nil {
		return redirectWithError(c, "/skills/"+url.PathEscape(id), err)
	}
	return c.Redirect(http.StatusSeeOther, "/skills/"+url.PathEscape(id)+"?updated=1")
}

// uiDeleteSkill handles the delete confirm form on the detail page (PRG).
func (s *Server) uiDeleteSkill(c echo.Context) error {
	id := c.Param("id")
	if err := s.memory.DeleteSkill(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/skills", err)
	}
	return c.Redirect(http.StatusSeeOther, "/skills?deleted=1")
}
