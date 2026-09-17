package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

func renderHTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(t.Context(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// selectShowsValue reports whether the <select name=...> block renders the
// given option value as selected, scoped to that select so an unrelated select
// on the page cannot satisfy the assertion.
func selectShowsValue(html, name, value string) bool {
	i := strings.Index(html, `name="`+name+`"`)
	if i < 0 {
		return false
	}
	seg := html[i:]
	if len(seg) > 400 {
		seg = seg[:400]
	}
	return strings.Contains(seg, `value="`+value+`" selected`)
}

func TestRenderAgentDashboard(t *testing.T) {
	agent := &AgentDefinition{
		ID:          "a1",
		Name:        "diane",
		Description: "household assistant with a dry wit",
		Model:       &ModelConfig{Name: "gpt-4o", Temperature: 0.7},
		FlowType:    "agentic",
		Visibility:  "project",
		ToolCount:   2,
		Tools:       []string{"web_search", "memory_lookup"},
		BannedTools: []string{"code_exec"},
		UpdatedAt:   "2026-08-25T10:00:00Z",
	}
	data := agentDashboardData{
		Agent: agent,
		Chats: []Conversation{
			{ID: "c1", Title: "Morning briefing", AgentDefinitionID: "a1", UpdatedAt: "2026-08-26T09:00:00Z"},
			{ID: "c2", AgentDefinitionID: "a1", UpdatedAt: "2026-08-25T09:00:00Z"},
		},
	}
	html := renderHTML(t, AgentDashboardPage(data))
	for _, want := range []string{
		"diane", "household assistant with a dry wit",
		"gpt-4o", "agentic", "project", "2 tools",
		"web_search", "memory_lookup", "code_exec",
		"Morning briefing", "Chat " + shortID("c2"),
		`href="/chat?c=c1"`, `href="/chat?c=c2"`,
		`href="/agents/a1/memories"`, "Memories",
		`href="/chat?agent=a1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}

	// no tools → whole-section empty state
	noTools := agentDashboardData{Agent: &AgentDefinition{ID: "a1", Name: "diane"}}
	htmlNoTools := renderHTML(t, AgentDashboardPage(noTools))
	if !strings.Contains(htmlNoTools, "No tools configured") {
		t.Error("no-tools state missing")
	}

	// no chats → empty state
	noChats := agentDashboardData{Agent: agent}
	htmlNoChats := renderHTML(t, AgentDashboardPage(noChats))
	if !strings.Contains(htmlNoChats, "No chats yet") {
		t.Error("no-chats state missing")
	}

	// conversations failure degrades the section, keeps the rest
	broken := agentDashboardData{Agent: agent, ChatsErr: errTest}
	htmlBroken := renderHTML(t, AgentDashboardPage(broken))
	if !strings.Contains(htmlBroken, "Failed to load conversations") || !strings.Contains(htmlBroken, "diane") {
		t.Error("section error should surface while the rest stays usable")
	}

	// agent fetch failure → whole-page error
	htmlErr := renderHTML(t, AgentDashboardPage(agentDashboardData{LoadErr: errTest}))
	if !strings.Contains(htmlErr, "Agent unavailable") {
		t.Error("agent fetch error state missing")
	}
}

func TestRenderMemoriesPage(t *testing.T) {
	memories := []Memory{
		{ID: "m1", Content: "prefers dark roast coffee", Category: "preference", Confidence: 0.92},
		{ID: "m2", Content: "meeting with Sam on Tuesday", Category: "calendar_event", Confidence: 1},
	}
	html := renderHTML(t, MemoriesPage("a1", "diane", "", memories, nil, nil))
	for _, want := range []string{
		"Memories", "diane",
		"prefers dark roast coffee", "preference", "92%",
		"calendar_event", "100%",
		`name="q"`, `href="/agents/a1"`,
		`href="/agents/a1/memories?memory=m1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("memories page missing %q", want)
		}
	}

	// search form carries the active query; detail links preserve it
	htmlSearch := renderHTML(t, MemoriesPage("a1", "diane", "dark", memories, nil, nil))
	if !strings.Contains(htmlSearch, `value="dark"`) {
		t.Error("search query not preserved in form")
	}
	if !strings.Contains(htmlSearch, `href="/agents/a1/memories?memory=m1&amp;q=dark"`) {
		t.Error("detail link should preserve query")
	}

	// detail view shows full content + back-to-list link
	sel := memories[0]
	htmlDetail := renderHTML(t, MemoriesPage("a1", "diane", "dark", memories, &sel, nil))
	if !strings.Contains(htmlDetail, "prefers dark roast coffee") || !strings.Contains(htmlDetail, "Back to list") {
		t.Error("detail view missing content or back link")
	}
	if !strings.Contains(htmlDetail, `href="/agents/a1/memories?q=dark"`) {
		t.Error("back-to-list link should preserve query")
	}

	// empty, no-match, and error states
	htmlEmpty := renderHTML(t, MemoriesPage("a1", "diane", "", nil, nil, nil))
	if !strings.Contains(htmlEmpty, "No memories yet") {
		t.Error("empty state missing")
	}
	htmlNoMatch := renderHTML(t, MemoriesPage("a1", "diane", "zzz", nil, nil, nil))
	if !strings.Contains(htmlNoMatch, "No matching memories") {
		t.Error("no-match state missing")
	}
	htmlErr := renderHTML(t, MemoriesPage("a1", "diane", "", nil, nil, errTest))
	if !strings.Contains(htmlErr, "Failed to load memories") {
		t.Error("error state missing")
	}
}

// TestRenderMemoryBadges covers the relevance/confidence badge rules in both
// the list row and the detail view: a search result (Score > 0) shows the
// match percentage, a list memory (Confidence > 0) shows confidence, and a
// memory with neither shows no percentage badge at all.
func TestRenderMemoryBadges(t *testing.T) {
	searchHit := Memory{ID: "m1", Content: "dentist appointment", Category: "calendar_event", Score: 0.168}
	listHit := Memory{ID: "m2", Content: "prefers dark roast", Category: "preference", Confidence: 0.92}
	bare := Memory{ID: "m3", Content: "orphan note"}

	// list row
	htmlRow := renderHTML(t, MemoriesPage("a1", "diane", "dentist", []Memory{searchHit, listHit, bare}, nil, nil))
	for _, want := range []string{"match 17%", "92%"} {
		if !strings.Contains(htmlRow, want) {
			t.Errorf("list row missing %q", want)
		}
	}
	if strings.Contains(htmlRow, "match 92%") {
		t.Error("confidence should not be rendered as a match badge")
	}
	if strings.Contains(htmlRow, "0%</span>") {
		t.Error("bare memory must not render a bogus 0% badge")
	}

	// detail view
	htmlDetail := renderHTML(t, MemoriesPage("a1", "diane", "dentist", []Memory{searchHit}, &searchHit, nil))
	if !strings.Contains(htmlDetail, "match 17%") {
		t.Error("detail view missing match badge")
	}
	if strings.Contains(htmlDetail, "0%</span>") {
		t.Error("detail view must not render a bogus 0% badge")
	}
	htmlBare := renderHTML(t, MemoriesPage("a1", "diane", "", []Memory{bare}, &bare, nil))
	if strings.Contains(htmlBare, "%</span>") {
		t.Errorf("bare memory detail should render no percentage badge, got: %s", htmlBare)
	}
}

// TestUIAgentRoutes exercises both UI routes against the fake backend:
// dashboard data assembly (agent + filtered conversations) and the memories
// subpage (search + detail), plus the unknown-agent error path.
func TestUIAgentRoutes(t *testing.T) {
	f := &fakeMemory{
		defs: map[string]*AgentDefinition{
			"a1": {ID: "a1", Name: "diane", Model: &ModelConfig{Name: "gpt-4o"}, Tools: []string{"web_search"}, FlowType: "agentic", Visibility: "project"},
			"a2": {ID: "a2", Name: "milo", Tools: []string{"calendar"}},
		},
		convs: []Conversation{
			{ID: "c1", Title: "Briefing", AgentDefinitionID: "a1", UpdatedAt: "2026-08-26T09:00:00Z"},
			{ID: "c2", Title: "Other agent's chat", AgentDefinitionID: "a2", UpdatedAt: "2026-08-26T10:00:00Z"},
		},
		memories: []Memory{
			{ID: "m1", Content: "likes dark roast", Category: "preference", Confidence: 0.9},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/agents/:id", s.uiAgent)
	e.GET("/agents/:id/memories", s.uiAgentMemories)

	// dashboard: agent summary + only its own conversations
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"diane", "gpt-4o", "web_search", "Briefing", `/agents/a1/memories`} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard route missing %q", want)
		}
	}
	if strings.Contains(body, "Other agent's chat") {
		t.Error("dashboard should not list another agent's conversations")
	}

	// memories subpage: search + detail
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/memories?q=dark&memory=m1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("memories status %d", rec.Code)
	}
	body = rec.Body.String()
	for _, want := range []string{"Memories for diane", "likes dark roast", "preference", "90%", "Back to list", `href="/agents/a1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("memories route missing %q", want)
		}
	}

	// unknown agent id → whole-page error, not a broken render
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/nope", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("error page status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Agent unavailable") {
		t.Error("unknown agent should render the error state")
	}
}

// TestVisibilityIntent covers the badge mapping for the memory visibility
// enum (external/project/internal) and the default label.
func TestVisibilityIntent(t *testing.T) {
	if visibilityIntent("external") == visibilityIntent("project") {
		t.Error("external and project must map to different intents")
	}
	if visibilityIntent("project") == visibilityIntent("internal") {
		t.Error("project and internal must map to different intents")
	}
	if visibilityIntent("external") == visibilityIntent("internal") {
		t.Error("external and internal must map to different intents")
	}
	if visibilityIntent("mystery") == visibilityIntent("external") {
		t.Error("unknown visibility should not map to external")
	}
	if visibilityLabel("") != "project" {
		t.Errorf("empty visibility should display as project, got %q", visibilityLabel(""))
	}
	if visibilityLabel("internal") != "internal" {
		t.Errorf("internal label should pass through, got %q", visibilityLabel("internal"))
	}
}

var errTest = errTestType{}

type errTestType struct{}

func (errTestType) Error() string { return "backend unreachable" }

// TestRenderAgentsPageSkillsPicker covers the skill picker in the agent
// create/edit dialog: checkboxes per skill (name + description), the empty
// state with a link to the Skills page, and the section sitting between Tools
// and Delegation.
func TestRenderAgentsPageSkillsPicker(t *testing.T) {
	skills := []Skill{
		{Name: "summarize-email", Description: "Condenses threads"},
		{Name: "recall-memory"},
	}
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "diane"}}
	html := renderHTML(t, AgentsPage(agents, nil, skills, "", nil))
	for _, want := range []string{
		`name="skill"`, `value="summarize-email"`, `value="recall-memory"`,
		"summarize-email", "Condenses threads", "recall-memory",
		`href="/skills"`, "Skills",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("agents page missing %q", want)
		}
	}
	// one checkbox per skill
	if got := strings.Count(html, `name="skill" type="checkbox"`); got != 2 {
		t.Errorf("skill checkboxes = %d, want 2", got)
	}
	// skills section sits after Tools and before Delegation
	if strings.Index(html, `id="agent-tools"`) >= strings.Index(html, `name="skill"`) ||
		strings.Index(html, `name="skill"`) >= strings.Index(html, "agent-delegation") {
		t.Error("skills picker must render between Tools and Delegation")
	}

	// no skills → empty note with a link, no checkboxes
	htmlEmpty := renderHTML(t, AgentsPage(agents, nil, nil, "", nil))
	if !strings.Contains(htmlEmpty, "No skills yet") || !strings.Contains(htmlEmpty, `href="/skills"`) {
		t.Error("empty skill state missing")
	}
	if strings.Contains(htmlEmpty, `name="skill" type="checkbox"`) {
		t.Error("empty skill state must not render checkboxes")
	}
}

// TestAgentModelDisplay asserts the model shown for an agent is the resolved
// one: the explicit override when present, else memory's EffectiveModel, else
// the project default — each default suffixed with a muted "(default)" — and
// the legacy "auto — default model" fallback when nothing is known.
func TestAgentModelDisplay(t *testing.T) {
	dashboard := func(agent *AgentDefinition, defaultModel string) string {
		return renderHTML(t, AgentDashboardPage(agentDashboardData{Agent: agent, DefaultModel: defaultModel}))
	}

	// 1. no override, no effective model → project default + (default)
	h := dashboard(&AgentDefinition{ID: "a1", Name: "diane"}, "deepseek/deepseek-v4-flash")
	for _, want := range []string{"deepseek/deepseek-v4-flash", "(default)"} {
		if !strings.Contains(h, want) {
			t.Errorf("default-model dashboard missing %q", want)
		}
	}

	// 2. no override → memory's effective model + (default)
	h = dashboard(&AgentDefinition{ID: "a1", Name: "diane", EffectiveModel: "google/gemini-3-pro"}, "")
	for _, want := range []string{"google/gemini-3-pro", "(default)"} {
		if !strings.Contains(h, want) {
			t.Errorf("effective-model dashboard missing %q", want)
		}
	}

	// 3. explicit override wins, no (default) suffix
	h = dashboard(&AgentDefinition{ID: "a1", Name: "diane", Model: &ModelConfig{Name: "openai/gpt-4o"}}, "")
	if !strings.Contains(h, "openai/gpt-4o") {
		t.Error("explicit-model dashboard missing model name")
	}
	if strings.Contains(h, "(default)") {
		t.Error("explicit override must not render the (default) suffix")
	}

	// 4. nothing resolvable → legacy fallback text
	h = dashboard(&AgentDefinition{ID: "a1", Name: "diane"}, "")
	if !strings.Contains(h, "auto — default model") {
		t.Error("unresolvable model should keep the auto — default model fallback")
	}

	// 5. agents list is responsive: a desktop table (md and up) restores the
	// metadata columns and per-row edit/delete actions, while below md the
	// mobile card grid keeps the bot icon + name + chat affordance with the
	// whole card (data-href) opening the agent dashboard.
	agents := []AgentDefinitionSummary{
		{ID: "a1", Name: "diane", EffectiveModel: "openai/gpt-4o"},
		{ID: "a2", Name: "milo"},
		{ID: "a3", Name: "reggie"},
	}
	html := renderHTML(t, AgentsPage(agents, nil, nil, "", nil))

	// both breakpoint wrappers present: table desktop-only, cards mobile-only.
	for _, want := range []string{`class="hidden md:block"`, `class="md:hidden"`} {
		if !strings.Contains(html, want) {
			t.Errorf("agents page missing responsive wrapper %q", want)
		}
	}

	// mobile cards: name + data-href details target + chat <a> per agent.
	for _, a := range agents {
		if !strings.Contains(html, a.Name) {
			t.Errorf("agents grid missing agent %q", a.Name)
		}
		if want := `data-href="/agents/` + a.ID + `"`; !strings.Contains(html, want) {
			t.Errorf("agents grid missing details data-href %q", want)
		}
		if want := `href="/chat?agent=` + a.ID + `"`; !strings.Contains(html, want) {
			t.Errorf("agents grid missing chat link %q", want)
		}
	}
	if got := strings.Count(html, "lucide--messages-square"); got != len(agents) {
		t.Errorf("chat icons = %d, want %d (one per card)", got, len(agents))
	}

	// desktop table headers restored.
	for _, want := range []string{">Agent</th>", ">Model</th>", ">Tools</th>", ">Flow</th>", ">Visibility</th>", ">Updated</th>"} {
		if !strings.Contains(html, want) {
			t.Errorf("agents table missing header %q", want)
		}
	}

	// desktop table rows: resolved model text plus per-row edit/delete actions.
	if !strings.Contains(html, "openai/gpt-4o") {
		t.Error("agents table should render the agent's effective model")
	}
	if strings.Contains(html, "(default)") {
		t.Error("agents page must not render a (default) model suffix")
	}
	for _, a := range agents {
		if want := `href="/agents/` + a.ID + `/settings"`; !strings.Contains(html, want) {
			t.Errorf("agents table missing edit link %q", want)
		}
		if want := `aria-label="Delete ` + a.Name + `"`; !strings.Contains(html, want) {
			t.Errorf("agents table missing delete action %q", want)
		}
	}
	if got := strings.Count(html, "lucide--pencil"); got != len(agents) {
		t.Errorf("edit icons = %d, want %d (one per row)", got, len(agents))
	}
	// the confirm dialog also carries a trash icon, so assert presence rather
	// than an exact count for the row delete buttons.
	if !strings.Contains(html, "lucide--trash-2") {
		t.Error("agents table missing delete icon lucide--trash-2")
	}
}

// TestAgentModelWarnings asserts the config warnings rendered when an agent's
// model can't be served: legacy no-model rules (error when the project has no
// configured provider, warning when providers exist but nothing pins the
// model) plus the explicit-model rules (error when no provider is configured
// or the model's provider prefix isn't among the configured ones, none when
// the prefix matches). Both pages link to /settings/providers.
func TestAgentModelWarnings(t *testing.T) {
	noModel := &AgentDefinition{ID: "a1", Name: "diane"}
	pinned := &AgentDefinition{ID: "a1", Name: "diane", Model: &ModelConfig{Name: "openai/gpt-4o"}}
	foreign := &AgentDefinition{ID: "a1", Name: "diane", Model: &ModelConfig{Name: "anthropic/claude-sonnet-4"}}
	bare := &AgentDefinition{ID: "a1", Name: "diane", Model: &ModelConfig{Name: "gpt-4o"}}

	const (
		errDash      = "No provider or default model is configured"
		warnDash     = "No project default model is set"
		errSettings  = "This agent has no model. The project has no configured provider"
		warnSettings = "This agent has no explicit model and the project has no default"
		providersURL = `href="/settings/providers"`
	)

	// ModelConfigKnown mirrors production: the project model-config fetch
	// succeeded, so DefaultModel is the authoritative PINNED default.
	dash := func(agent *AgentDefinition, defaultModel string, hasProviders bool, providerNames []string) string {
		return renderHTML(t, AgentDashboardPage(agentDashboardData{Agent: agent, DefaultModel: defaultModel, ModelConfigKnown: true, HasProviders: hasProviders, ProviderNames: providerNames}))
	}

	// 1. no provider, no default → error alert on the dashboard
	h := dash(noModel, "", false, nil)
	for _, want := range []string{errDash, "can&#39;t run chats", providersURL, "Configure a provider"} {
		if !strings.Contains(h, want) {
			t.Errorf("dashboard error warning missing %q", want)
		}
	}
	if strings.Contains(h, warnDash) {
		t.Error("dashboard must not show the warning copy when no provider is configured")
	}

	// 2. providers configured but no default → warning alert
	h = dash(noModel, "", true, []string{"openai"})
	for _, want := range []string{warnDash, "isn&#39;t pinned", providersURL, "Set a default model"} {
		if !strings.Contains(h, want) {
			t.Errorf("dashboard warning missing %q", want)
		}
	}
	if strings.Contains(h, errDash) {
		t.Error("dashboard must not show the error copy when providers exist")
	}

	// 2b. providers configured but no pinned default, while memory resolves a
	//     provider-credential fallback into EffectiveModel → still the unpinned
	//     warning (the fallback must not mask an unpinned default).
	fallback := &AgentDefinition{ID: "a1", Name: "diane", EffectiveModel: "openai/deepseek-v4-flash"}
	h = dash(fallback, "", true, []string{"openai"})
	for _, want := range []string{warnDash, "isn&#39;t pinned", providersURL, "Set a default model"} {
		if !strings.Contains(h, want) {
			t.Errorf("provider-fallback warning missing %q", want)
		}
	}
	if strings.Contains(h, errDash) {
		t.Error("provider fallback must not show the error copy")
	}

	// 3. providers + pinned default → no warning, model shown
	h = dash(noModel, "openai/gpt-4o", true, []string{"openai"})
	for _, bad := range []string{errDash, warnDash} {
		if strings.Contains(h, bad) {
			t.Error("resolvable default must suppress the model warning")
		}
	}
	if !strings.Contains(h, "openai/gpt-4o") {
		t.Error("default model name missing on the dashboard")
	}

	// 4. explicit model, no providers → error: no provider configured
	h = dash(pinned, "", false, nil)
	for _, want := range []string{
		"No provider is configured, so this agent&#39;s model openai/gpt-4o can&#39;t run yet",
		"Configure a provider or change the agent&#39;s model",
		providersURL, "Configure a provider",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("dashboard zero-provider error warning missing %q", want)
		}
	}
	for _, bad := range []string{errDash, warnDash} {
		if strings.Contains(h, bad) {
			t.Error("explicit-model error must use the new copy, not the legacy no-model copy")
		}
	}

	// 5. explicit model whose provider IS configured → no warning
	h = dash(pinned, "", true, []string{"openai"})
	for _, bad := range []string{errDash, warnDash, "No provider is configured", "isn&#39;t configured"} {
		if strings.Contains(h, bad) {
			t.Error("explicit model with a matching configured provider must not warn")
		}
	}
	if !strings.Contains(h, "openai/gpt-4o") {
		t.Error("explicit model name missing on the dashboard")
	}

	// 6. explicit model whose provider is NOT configured → prefix-missing error
	h = dash(foreign, "", true, []string{"openai"})
	for _, want := range []string{
		"This agent&#39;s model anthropic/claude-sonnet-4 needs the anthropic provider, which isn&#39;t configured",
		"Add it or choose another model",
		providersURL, "Configure a provider",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("dashboard prefix-missing error warning missing %q", want)
		}
	}

	// 7. bare explicit model + providers configured → treated as satisfied
	h = dash(bare, "", true, []string{"openai"})
	for _, bad := range []string{errDash, warnDash, "No provider is configured", "isn&#39;t configured"} {
		if strings.Contains(h, bad) {
			t.Error("bare explicit model with providers configured must not warn")
		}
	}

	// settings page: warning renders above the model picker
	settings := func(agent *AgentDefinition, defaultModel string, hasProviders bool, providerNames []string) string {
		return renderHTML(t, AgentSettingsPage(agentSettingsData{Section: "model", Agent: agent, DefaultModel: defaultModel, ModelConfigKnown: true, HasProviders: hasProviders, ProviderNames: providerNames}))
	}
	hs := settings(noModel, "", false, nil)
	for _, want := range []string{errSettings, "so chats will fail", providersURL, "Configure a provider"} {
		if !strings.Contains(hs, want) {
			t.Errorf("settings error warning missing %q", want)
		}
	}
	hs = settings(noModel, "openai/gpt-4o", true, []string{"openai"})
	for _, bad := range []string{errSettings, warnSettings} {
		if strings.Contains(hs, bad) {
			t.Error("settings must not warn when a default model resolves")
		}
	}

	// settings: the Auto option names the resolved default when the agent has
	// no explicit model, and stays generic when nothing resolves.
	if !strings.Contains(hs, `<option value="">Auto — openai/gpt-4o (default)</option>`) {
		t.Error("settings Auto option should name the resolved default model")
	}
	hs = settings(noModel, "", true, []string{"openai"})
	if !strings.Contains(hs, `<option value="">Auto — default model</option>`) {
		t.Error("settings Auto option should stay generic when no default resolves")
	}
	// an explicitly pinned agent keeps the generic label (its model is the
	// selected catalog option, not the Auto fallback).
	hs = settings(pinned, "openai/gpt-4o", true, []string{"openai"})
	if !strings.Contains(hs, `<option value="">Auto — default model</option>`) {
		t.Error("settings Auto option should stay generic for a pinned agent")
	}

	// settings, explicit model, no providers → pinned zero-provider error
	hs = settings(pinned, "", false, nil)
	for _, want := range []string{
		"This agent is pinned to openai/gpt-4o, but the project has no configured provider",
		"until one is added or the model is changed",
		providersURL, "Configure a provider",
	} {
		if !strings.Contains(hs, want) {
			t.Errorf("settings zero-provider error warning missing %q", want)
		}
	}
	for _, bad := range []string{errSettings, warnSettings} {
		if strings.Contains(hs, bad) {
			t.Error("settings explicit-model error must not use the legacy no-model copy")
		}
	}

	// settings, explicit model, provider prefix not configured → error
	hs = settings(foreign, "", true, []string{"openai"})
	for _, want := range []string{
		"This agent&#39;s model anthropic/claude-sonnet-4 uses the anthropic provider, which isn&#39;t configured",
		"until it is added or the model is changed",
		providersURL, "Configure a provider",
	} {
		if !strings.Contains(hs, want) {
			t.Errorf("settings prefix-missing error warning missing %q", want)
		}
	}

	// settings, explicit model, matching provider configured → no warning
	hs = settings(pinned, "", true, []string{"openai"})
	for _, bad := range []string{errSettings, warnSettings, "No provider is configured", "isn&#39;t configured"} {
		if strings.Contains(hs, bad) {
			t.Error("settings must not warn when the explicit model's provider is configured")
		}
	}
}

// TestUIAgentsRouteSkills asserts uiAgents threads the fetched skills through
// to the dialog.
func TestUIAgentsRouteSkills(t *testing.T) {
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		defs: map[string]*AgentDefinition{
			"a1": {ID: "a1", Name: "diane", EffectiveModel: "deepseek/deepseek-v4-flash"},
		},
		skills: []Skill{
			{Name: "summarize-email", Description: "Condenses threads"},
			{Name: "recall-memory"},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/agents", s.uiAgents)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`name="skill"`, "summarize-email", "Condenses threads", "recall-memory", `data-href="/agents/a1"`, `href="/chat?agent=a1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("agents route missing %q", want)
		}
	}
}

// TestRenderAgentSettingsPage covers the split in-page edit subpages: each
// section renders its own panel with the current values, the settings nav
// renders as a group with all six children (active child highlighted), and the
// per-section regression guards hold (bare boolean attrs, unlisted tools,
// delegation-managed tool exclusion, synthetic current model).
func TestRenderAgentSettingsPage(t *testing.T) {
	agent := &AgentDefinition{
		ID:           "a1",
		Name:         "diane",
		SystemPrompt: "be terse",
		Model:        &ModelConfig{Name: "openai/gpt-4o", Temperature: 0.7, MaxTokens: 4096},
		Tools:        []string{"web_search", "memory_lookup"},
		Skills:       []string{"recall-memory"},
		Delegation:   &Delegation{Enabled: true, Targets: []string{"milo"}},
	}
	data := agentSettingsData{
		Agent:  agent,
		Agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}, {ID: "a2", Name: "milo"}},
		Models: []Model{{Provider: "openai", ModelName: "gpt-4o", DisplayName: "GPT-4o"}},
		MCPServers: []MCPServer{
			{Name: "builtin", ToolCount: 3, Tools: []MCPTool{
				{ToolName: "web_search", Description: "search the web"},
				{ToolName: "memory_lookup", Description: "look up memory"},
				{ToolName: "code_exec"},
			}},
		},
		Skills: []Skill{{Name: "recall-memory", Description: "recall stuff"}, {Name: "summarize-email"}},
	}
	render := func(section string) string {
		d := data
		d.Section = section
		return renderHTML(t, AgentSettingsPage(d))
	}

	// Every settings subpage renders the six-item settings nav group (General /
	// Model / Tools / Skills / Delegation / MCP sharing) plus the sibling rail
	// items (Dashboard / Settings / Sandbox / Sessions).
	settingsNavHrefs := []string{
		`href="/agents/a1"`, `href="/agents/a1/settings"`,
		`href="/agents/a1/settings/model"`, `href="/agents/a1/settings/tools"`,
		`href="/agents/a1/settings/skills"`, `href="/agents/a1/settings/delegation"`,
		`href="/agents/a1/settings/mcp"`,
		`href="/agents/a1/sandbox"`, `href="/agents/a1/sessions"`,
	}
	for _, section := range []string{"", "general", "model", "tools", "skills", "delegation", "mcp"} {
		h := render(section)
		for _, want := range settingsNavHrefs {
			if !strings.Contains(h, want) {
				t.Errorf("settings section %q nav missing %q", section, want)
			}
		}
	}

	// General page: name, system prompt, language, and its POST target.
	general := render("general")
	for _, want := range []string{
		`name="name"`, `value="diane"`,
		`name="systemPrompt"`, "be terse",
		`name="language"`,
		`/agents/a1/settings/general`,
	} {
		if !strings.Contains(general, want) {
			t.Errorf("general settings page missing %q", want)
		}
	}

	// Model page: model picker + temperature + max tokens.
	model := render("model")
	for _, want := range []string{
		`name="modelName"`, `value="openai/gpt-4o"`,
		`name="temperature"`, `value="0.7"`,
		`name="maxTokens"`, `value="4096"`,
		`/agents/a1/settings/model`,
	} {
		if !strings.Contains(model, want) {
			t.Errorf("model settings page missing %q", want)
		}
	}

	// Tools page: grouped tool checkboxes reflect stored state.
	tools := render("tools")
	for _, want := range []string{
		`name="tool"`, `value="web_search"`, `value="memory_lookup"`, `value="code_exec"`,
		`value="web_search" checked`,
		`/agents/a1/settings/tools`,
	} {
		if !strings.Contains(tools, want) {
			t.Errorf("tools settings page missing %q", want)
		}
	}
	if strings.Contains(tools, `value="code_exec" checked`) {
		t.Error("unselected tool must not render checked")
	}

	// Skills page: skill checkboxes reflect stored state.
	skills := render("skills")
	for _, want := range []string{
		`name="skill"`, `value="recall-memory"`, `value="summarize-email"`,
		`value="recall-memory" checked`,
		`/agents/a1/settings/skills`,
	} {
		if !strings.Contains(skills, want) {
			t.Errorf("skills settings page missing %q", want)
		}
	}
	if strings.Contains(skills, `value="summarize-email" checked`) {
		t.Error("unselected skill must not render checked")
	}

	// Delegation page: enable toggle + target picker; self is excluded.
	delegation := render("delegation")
	for _, want := range []string{
		`name="delegationEnabled"`,
		`name="delegation-target"`, `value="milo"`,
		`/agents/a1/settings/delegation`,
	} {
		if !strings.Contains(delegation, want) {
			t.Errorf("delegation settings page missing %q", want)
		}
	}
	if strings.Contains(delegation, `name="delegation-target" type="checkbox" value="diane"`) {
		t.Error("self must not appear in delegation targets")
	}

	// MCP page: the non-form MCP section block.
	if mcp := render("mcp"); !strings.Contains(mcp, `data-testid="agent-mcp-section"`) {
		t.Error("mcp settings page missing the MCP section")
	}

	// regression: boolean attrs must render bare `selected`/`checked`, never
	// `selected="false"`/`checked="false"` (HTML treats the latter as true).
	// The leading space avoids matching the pickers' aria-selected="false".
	for _, section := range []string{"model", "tools", "skills"} {
		h := render(section)
		if strings.Contains(h, ` selected="false"`) {
			t.Errorf("section %q must not render selected=\"false\" (boolean-attribute bug)", section)
		}
		if strings.Contains(h, ` checked="false"`) {
			t.Errorf("section %q must not render checked=\"false\" (boolean-attribute bug)", section)
		}
	}
	if !strings.Contains(model, `value="openai/gpt-4o" selected>`) {
		t.Error("stored model option should be selected")
	}

	// empty model → no selected attribute at all (Auto — default model stays first)
	emptyModel := agentSettingsData{
		Section: "model",
		Agent:   &AgentDefinition{ID: "a1", Name: "diane"},
		Agents:  []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		Models:  []Model{{Provider: "openai", ModelName: "gpt-4o"}},
	}
	if h := renderHTML(t, AgentSettingsPage(emptyModel)); strings.Contains(h, " selected") {
		t.Error("empty model must not render any selected option")
	}

	// tools from no registered server → "Other" group, preserved checked
	unlisted := agentSettingsData{
		Section:    "tools",
		Agent:      &AgentDefinition{ID: "a1", Name: "diane", Tools: []string{"web_search", "ha_get_state"}},
		Agents:     []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		MCPServers: []MCPServer{{Name: "builtin", ToolCount: 1, Tools: []MCPTool{{ToolName: "web_search"}}}},
	}
	if h := renderHTML(t, AgentSettingsPage(unlisted)); !strings.Contains(h, "Other") || !strings.Contains(h, `value="ha_get_state" checked`) {
		t.Error("unlisted tool should appear checked in the Other group")
	}

	// delegation-managed tools are never listed (managed by the delegation toggle)
	delOnly := agentSettingsData{
		Section: "tools",
		Agent:   &AgentDefinition{ID: "a1", Name: "diane", Tools: []string{"spawn_agents", "list_available_agents"}},
		Agents:  []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
	}
	if h := renderHTML(t, AgentSettingsPage(delOnly)); strings.Contains(h, "spawn_agents") {
		t.Error("delegation-managed tools must not render in the picker")
	}

	// model absent from catalog → synthetic "(current)" option
	unknown := agentSettingsData{
		Section: "model",
		Agent:   &AgentDefinition{ID: "a1", Name: "diane", Model: &ModelConfig{Name: "gpt-5"}},
		Agents:  []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		Models:  []Model{{Provider: "openai", ModelName: "gpt-4o"}},
	}
	if h := renderHTML(t, AgentSettingsPage(unknown)); !strings.Contains(h, "gpt-5 (current)") {
		t.Error("synthetic current-model option missing")
	}

	// flash + error + load-error states
	if h := renderHTML(t, AgentSettingsPage(agentSettingsData{Section: "general", Agent: agent, FlashMsg: "Agent updated."})); !strings.Contains(h, "Agent updated.") {
		t.Error("flash success missing")
	}
	if h := renderHTML(t, AgentSettingsPage(agentSettingsData{Section: "general", Agent: agent, FlashErr: errTest})); !strings.Contains(h, "backend unreachable") {
		t.Error("flash error should render the real error, missing")
	}
	if h := renderHTML(t, AgentSettingsPage(agentSettingsData{LoadErr: errTest})); !strings.Contains(h, "Agent unavailable") {
		t.Error("load-error state missing")
	}
}

// agentRailHTML returns the agent details sub-navigation markup — the
// aria-label="Agent sections" nav block — or "" when a page renders no rail
// (e.g. the whole-page error state).
func agentRailHTML(html string) string {
	const marker = `aria-label="Agent sections"`
	i := strings.Index(html, marker)
	if i < 0 {
		return ""
	}
	rail := html[i:]
	if j := strings.Index(rail, "</nav>"); j >= 0 {
		return rail[:j]
	}
	return rail
}

var navAnchorRe = regexp.MustCompile(`<a\b[^>]*>`)

// activeNavHrefs returns the sorted hrefs of the rail anchors marked
// aria-current="page".
func activeNavHrefs(rail string) []string {
	var hrefs []string
	for _, tag := range navAnchorRe.FindAllString(rail, -1) {
		if !strings.Contains(tag, `aria-current="page"`) {
			continue
		}
		if i := strings.Index(tag, `href="`); i >= 0 {
			rest := tag[i+len(`href="`):]
			if j := strings.Index(rest, `"`); j >= 0 {
				hrefs = append(hrefs, rest[:j])
			}
		}
	}
	slices.Sort(hrefs)
	return hrefs
}

// TestRenderAgentSubNavSettingsAlwaysVisible guards the agent details rail: the
// second level (Settings parent + its six children) stays visible on every agent
// section, not only on settings subpages. Off the settings surface the only
// active item is that surface's own entry; on a settings section exactly the
// matching child is active (the parent Settings link shares the General child's
// destination, so it is never marked current).
func TestRenderAgentSubNavSettingsAlwaysVisible(t *testing.T) {
	agent := &AgentDefinition{ID: "a1", Name: "diane"}

	const settingsParent = "/agents/a1/settings"
	children := []struct {
		section string
		href    string
	}{
		{"general", settingsParent},
		{"model", settingsParent + "/model"},
		{"tools", settingsParent + "/tools"},
		{"skills", settingsParent + "/skills"},
		{"delegation", settingsParent + "/delegation"},
		{"mcp", settingsParent + "/mcp"},
	}
	// Every agent section must expose the Settings parent plus all six children.
	allSettingsHrefs := []string{settingsParent}
	for _, c := range children {
		allSettingsHrefs = append(allSettingsHrefs, c.href)
	}

	// Off the settings surface: dashboard / sandbox / sessions each render the
	// full second level, with no settings entry marked active.
	offSurface := []struct {
		name       string
		html       string
		activeHref string
	}{
		{"dashboard", renderHTML(t, AgentDashboardPage(agentDashboardData{Agent: agent})), "/agents/a1"},
		{"sandbox", renderHTML(t, AgentSandboxPage(agentSandboxData{Agent: agent})), "/agents/a1/sandbox"},
		{"sessions", renderHTML(t, AgentSessionsPage(agentSessionsData{Agent: agent})), "/agents/a1/sessions"},
	}
	for _, tc := range offSurface {
		t.Run(tc.name, func(t *testing.T) {
			rail := agentRailHTML(tc.html)
			if rail == "" {
				t.Fatal("agent rail not rendered")
			}
			for _, href := range allSettingsHrefs {
				if !strings.Contains(rail, `href="`+href+`"`) {
					t.Errorf("%s rail missing settings link %q", tc.name, href)
				}
			}
			got := activeNavHrefs(rail)
			if !slices.Equal(got, []string{tc.activeHref}) {
				t.Errorf("%s active rail items = %v, want only %q", tc.name, got, tc.activeHref)
			}
			for _, c := range children {
				if slices.Contains(got, c.href) {
					t.Errorf("%s settings child %q must not be active off the settings surface", tc.name, c.href)
				}
			}
		})
	}

	// On a settings section: exactly the matching child is active; no other
	// section entry is. The Settings parent is never active — on General it would
	// duplicate the child's href.
	for _, c := range children {
		t.Run("settings_"+c.section, func(t *testing.T) {
			html := renderHTML(t, AgentSettingsPage(agentSettingsData{Section: c.section, Agent: agent}))
			rail := agentRailHTML(html)
			if rail == "" {
				t.Fatal("agent rail not rendered")
			}
			want := []string{c.href}
			if got := activeNavHrefs(rail); !slices.Equal(got, want) {
				t.Errorf("section %q active rail items = %v, want %v", c.section, got, want)
			}
		})
	}
}

func TestRenderAgentSessionsPage(t *testing.T) {
	agent := &AgentDefinition{ID: "a1", Name: "diane"}
	data := agentSessionsData{
		Agent: agent,
		Chats: []Conversation{{ID: "c1", Title: "Briefing", AgentDefinitionID: "a1", UpdatedAt: "2026-08-26T09:00:00Z"}},
	}
	html := renderHTML(t, AgentSessionsPage(data))
	for _, want := range []string{
		"Sessions", "Briefing", `href="/chat?c=c1"`,
		`href="/agents/a1/sessions"`, `href="/agents/a1/settings"`, `href="/agents/a1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("sessions page missing %q", want)
		}
	}
	if h := renderHTML(t, AgentSessionsPage(agentSessionsData{Agent: agent})); !strings.Contains(h, "No sessions yet") {
		t.Error("empty state missing")
	}
	if h := renderHTML(t, AgentSessionsPage(agentSessionsData{Agent: agent, ChatsErr: errTest})); !strings.Contains(h, "Failed to load conversations") {
		t.Error("chats error state missing")
	}
	if h := renderHTML(t, AgentSessionsPage(agentSessionsData{LoadErr: errTest})); !strings.Contains(h, "Agent unavailable") {
		t.Error("load-error state missing")
	}
}

// TestUIAgentSettingsRoute exercises the settings GET routes against the fake
// backend: the General landing route (name/prompt/language) and each subpage
// route (model/tools/skills/delegation), including deriveDelegation
// reconstructing delegation from Tools + Config.spawnPolicy (the memory
// representation) and an unknown section returning 404.
func TestUIAgentSettingsRoute(t *testing.T) {
	f := &fakeMemory{
		defs: map[string]*AgentDefinition{
			"a1": {
				ID: "a1", Name: "diane", SystemPrompt: "hi",
				Model:  &ModelConfig{Name: "gpt-4o", Temperature: 0.7},
				Tools:  []string{"web_search", "spawn_agents", "list_available_agents"},
				Config: map[string]any{"spawnPolicy": map[string]any{"allow": []any{"milo"}}},
				Skills: []string{"recall-memory"},
			},
		},
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}, {ID: "a2", Name: "milo"}},
		skills: []Skill{{Name: "recall-memory"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/agents/:id/settings", s.uiAgentSettings)
	e.GET("/agents/:id/settings/:section", s.uiAgentSettingsSection)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// General landing page: name, system prompt, language — no model/tools/etc.
	rec := get("/agents/a1/settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("settings status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`value="diane"`, "hi", `name="language"`} {
		if !strings.Contains(body, want) {
			t.Errorf("general settings route missing %q", want)
		}
	}
	if strings.Contains(body, `name="modelName"`) {
		t.Error("general settings page must not render the model picker")
	}

	// Model subpage carries the stored model.
	body = get("/agents/a1/settings/model").Body.String()
	for _, want := range []string{`value="gpt-4o"`, `value="0.7"`} {
		if !strings.Contains(body, want) {
			t.Errorf("model settings route missing %q", want)
		}
	}

	// Delegation subpage reconstructs the persisted delegation targets.
	body = get("/agents/a1/settings/delegation").Body.String()
	if !strings.Contains(body, `name="delegation-target" type="checkbox" value="milo"`) {
		t.Error("delegation route missing derived target milo")
	}

	// Skills subpage renders the stored skill.
	body = get("/agents/a1/settings/skills").Body.String()
	if !strings.Contains(body, "recall-memory") {
		t.Error("skills route missing stored skill")
	}

	// Unknown section → 404.
	if rec := get("/agents/a1/settings/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown section status = %d, want 404", rec.Code)
	}
}

// TestLoadAgentSettingsScopesCatalogFetches proves the settings loader fetches
// only the catalogs the requested subpage renders (see loadAgentSettings):
// "tools" is the only subpage that loads the MCP server catalog + relay nodes,
// "model" loads the model config/providers/catalog, "skills" loads skills,
// "delegation" loads the other agents, and "general"/"mcp" load none of those.
// GetAgentDefinition (+ deriveDelegation) always run for every section.
func TestLoadAgentSettingsScopesCatalogFetches(t *testing.T) {
	agent := &AgentDefinition{ID: "a1", Name: "diane"}
	newFake := func() *fakeMemory {
		return &fakeMemory{
			defs:             map[string]*AgentDefinition{"a1": agent},
			agents:           []AgentDefinitionSummary{{ID: "a1", Name: "diane"}, {ID: "a2", Name: "milo"}},
			servers:          []MCPServer{{Name: "builtin", Tools: []MCPTool{{ToolName: "web_search"}}}},
			skills:           []Skill{{Name: "recall-memory"}},
			relaySessions:    []RelaySession{{InstanceID: "mac-ada", ToolCount: 1}},
			relayTools:       map[string][]RelayTool{"mac-ada": {{Name: "notes_search"}}},
			modelConfig:      &ProjectModelConfig{GenerativeModel: "openai/gpt-4o"},
			projectProviders: []ProjectProviderConfig{{Provider: "openai"}},
		}
	}

	// Every catalog method the loader can call (see loadAgentSettings).
	allCatalogs := []string{
		"GetProjectModelConfig", "ListProjectProviders", "ListModels",
		"ListMCPServers", "ListRelaySessions", "GetRelaySessionTools",
		"ListSkills", "ListAgentDefinitions",
	}

	cases := []struct {
		section string
		loads   []string // methods that MUST be fetched
	}{
		{section: "general"},
		{section: "model", loads: []string{"GetProjectModelConfig", "ListProjectProviders", "ListModels"}},
		{section: "tools", loads: []string{"ListMCPServers", "ListRelaySessions", "GetRelaySessionTools"}},
		{section: "skills", loads: []string{"ListSkills"}},
		{section: "delegation", loads: []string{"ListAgentDefinitions"}},
		{section: "mcp"},
	}

	for _, tc := range cases {
		t.Run(tc.section, func(t *testing.T) {
			f := newFake()
			s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
			data := agentSettingsData{Section: tc.section}
			if err := s.loadAgentSettings(t.Context(), "a1", &data); err != nil {
				t.Fatalf("loadAgentSettings(%q): %v", tc.section, err)
			}
			loaded := map[string]bool{}
			for _, m := range tc.loads {
				loaded[m] = true
				if got := f.catalogCallCount(m); got != 1 {
					t.Errorf("%s section: expected %s to be fetched exactly once, got %d calls", tc.section, m, got)
				}
			}
			for _, m := range allCatalogs {
				if got := f.catalogCallCount(m); got != 0 && !loaded[m] {
					t.Errorf("%s section: %s fetched %d times, want 0", tc.section, m, got)
				}
			}
		})
	}
}

// TestUIAgentSessionsRoute exercises the sessions GET route: only the agent's
// own conversations are listed.
func TestUIAgentSessionsRoute(t *testing.T) {
	f := &fakeMemory{
		defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
		convs: []Conversation{
			{ID: "c1", Title: "Briefing", AgentDefinitionID: "a1", UpdatedAt: "2026-08-26T09:00:00Z"},
			{ID: "c2", Title: "Other", AgentDefinitionID: "a2", UpdatedAt: "2026-08-26T10:00:00Z"},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/agents/:id/sessions", s.uiAgentSessions)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Briefing") {
		t.Error("sessions route missing own conversation")
	}
	if strings.Contains(body, "Other") {
		t.Error("sessions route should not list another agent's conversations")
	}
}

// TestUIAgentUpdateRoute exercises the split per-section POST handlers (PRG):
// each handler maps only its own fields onto the definition, delegation is
// applied via applyDelegation, and success redirects to the section's ?updated=1
// while validation failures redirect to ?err=<msg>. The back-compat
// POST /agents/:id/update still works as the General alias.
func TestUIAgentUpdateRoute(t *testing.T) {
	newServer := func(f *fakeMemory) *echo.Echo {
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.POST("/agents/:id/update", s.uiAgentUpdateGeneral)
		e.POST("/agents/:id/settings/general", s.uiAgentUpdateGeneral)
		e.POST("/agents/:id/settings/model", s.uiAgentUpdateModel)
		e.POST("/agents/:id/settings/tools", s.uiAgentUpdateTools)
		e.POST("/agents/:id/settings/skills", s.uiAgentUpdateSkills)
		e.POST("/agents/:id/settings/delegation", s.uiAgentUpdateDelegation)
		return e
	}
	post := func(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("general maps name/prompt/language", func(t *testing.T) {
		f := &fakeMemory{
			defs:   map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
			agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}, {ID: "a2", Name: "milo"}},
		}
		e := newServer(f)
		rec := post(e, "/agents/a1/settings/general", "name=diane&systemPrompt=be+terse&language=Spanish")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/agents/a1/settings?updated=1" {
			t.Errorf("redirect = %q, want general?updated=1", loc)
		}
		u := f.updatedAgent
		if u == nil {
			t.Fatal("UpdateAgentDefinition not called")
		}
		if u.Name != "diane" || u.SystemPrompt != "be terse" || u.Config["language"] != "Spanish" {
			t.Errorf("general mapping wrong: %+v", u)
		}
	})

	t.Run("general clears empty language and preserves delegation", func(t *testing.T) {
		f := &fakeMemory{
			defs: map[string]*AgentDefinition{"a1": {
				ID: "a1", Name: "diane",
				Tools:  []string{"spawn_agents", "list_available_agents"},
				Config: map[string]any{"spawnPolicy": map[string]any{"allow": []any{"milo"}}, "language": "Spanish"},
			}},
			agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}, {ID: "a2", Name: "milo"}},
		}
		e := newServer(f)
		rec := post(e, "/agents/a1/settings/general", "name=diane&systemPrompt=be+terse&language=+++")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status %d, want 303", rec.Code)
		}
		u := f.updatedAgent
		if _, ok := u.Config["language"]; ok {
			t.Errorf("Config[language] should be deleted on empty value: %+v", u.Config)
		}
		if _, ok := u.Config["spawnPolicy"]; !ok {
			t.Errorf("spawnPolicy should survive language clear: %+v", u.Config)
		}
		if !containsString(u.Tools, "spawn_agents") {
			t.Errorf("delegation tools should survive a general save: %v", u.Tools)
		}
	})

	t.Run("update alias targets general", func(t *testing.T) {
		f := &fakeMemory{
			defs:   map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
			agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		}
		e := newServer(f)
		rec := post(e, "/agents/a1/update", "name=renamed&systemPrompt=x")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents/a1/settings?updated=1" {
			t.Fatalf("alias redirect = %d %q", rec.Code, rec.Header().Get("Location"))
		}
		if f.updatedAgent == nil || f.updatedAgent.Name != "renamed" {
			t.Errorf("alias should map the name, got %+v", f.updatedAgent)
		}
	})

	t.Run("empty name errors", func(t *testing.T) {
		f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}}}
		e := newServer(f)
		rec := post(e, "/agents/a1/settings/general", "name=&systemPrompt=x")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents/a1/settings?err=name+is+required" {
			t.Errorf("empty name redirect = %d %q", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("model maps explicit and auto", func(t *testing.T) {
		f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}}}
		e := newServer(f)
		rec := post(e, "/agents/a1/settings/model", "modelName=gpt-4o&temperature=0.7&maxTokens=4096")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents/a1/settings/model?updated=1" {
			t.Fatalf("model redirect = %d %q", rec.Code, rec.Header().Get("Location"))
		}
		u := f.updatedAgent
		if u.Model == nil || u.Model.Name != "gpt-4o" || u.Model.Temperature != 0.7 || u.Model.MaxTokens != 4096 {
			t.Errorf("model mapping wrong: %+v", u.Model)
		}
		// empty model name clears the override
		_ = post(e, "/agents/a1/settings/model", "modelName=")
		if f.updatedAgent.Model != nil {
			t.Errorf("empty model name should clear the override, got %+v", f.updatedAgent.Model)
		}
	})

	t.Run("tools preserves name/model/skills", func(t *testing.T) {
		f := &fakeMemory{
			defs: map[string]*AgentDefinition{"a1": {
				ID: "a1", Name: "diane", SystemPrompt: "be terse",
				Model:  &ModelConfig{Name: "gpt-4o"},
				Tools:  []string{"web_search"},
				Skills: []string{"recall-memory"},
			}},
			agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		}
		e := newServer(f)
		rec := post(e, "/agents/a1/settings/tools", "defaultToolPolicy=ask&tool=web_search&tool=memory_lookup&toolPolicy.web_search=deny")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents/a1/settings/tools?updated=1" {
			t.Fatalf("tools redirect = %d %q", rec.Code, rec.Header().Get("Location"))
		}
		u := f.updatedAgent
		// the section only touches tools/policies — name/model/skills survive
		if u.Name != "diane" || u.SystemPrompt != "be terse" {
			t.Errorf("tools save must not clear name/prompt: %+v", u)
		}
		if u.Model == nil || u.Model.Name != "gpt-4o" {
			t.Errorf("tools save must not clear the model: %+v", u.Model)
		}
		if len(u.Skills) != 1 || u.Skills[0] != "recall-memory" {
			t.Errorf("tools save must not clear skills: %v", u.Skills)
		}
		if !containsString(u.Tools, "web_search") || !containsString(u.Tools, "memory_lookup") {
			t.Errorf("tools mapping wrong: %v", u.Tools)
		}
		if u.DefaultToolPolicy != "ask" {
			t.Errorf("defaultToolPolicy = %q, want ask", u.DefaultToolPolicy)
		}
		if p := u.ToolPolicies["web_search"]; !p.Disabled {
			t.Errorf("web_search policy should be deny (Disabled), got %+v", p)
		}
	})

	t.Run("skills preserves tools", func(t *testing.T) {
		f := &fakeMemory{
			defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane", Tools: []string{"web_search"}}},
		}
		e := newServer(f)
		rec := post(e, "/agents/a1/settings/skills", "skill=recall-memory")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents/a1/settings/skills?updated=1" {
			t.Fatalf("skills redirect = %d %q", rec.Code, rec.Header().Get("Location"))
		}
		u := f.updatedAgent
		if len(u.Skills) != 1 || u.Skills[0] != "recall-memory" {
			t.Errorf("skills mapping wrong: %v", u.Skills)
		}
		if !containsString(u.Tools, "web_search") {
			t.Errorf("skills save must not clear tools: %v", u.Tools)
		}
		// empty skills list clears skills
		_ = post(e, "/agents/a1/settings/skills", "")
		if len(f.updatedAgent.Skills) != 0 {
			t.Errorf("empty skills should clear, got %v", f.updatedAgent.Skills)
		}
	})

	t.Run("delegation validates and applies", func(t *testing.T) {
		f := &fakeMemory{
			defs:   map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
			agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}, {ID: "a2", Name: "milo"}},
		}
		e := newServer(f)
		// enabled without targets → error redirect
		rec := post(e, "/agents/a1/settings/delegation", "delegationEnabled=on")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents/a1/settings/delegation?err=delegation+requires+at+least+one+target" {
			t.Errorf("delegation without targets redirect = %d %q", rec.Code, rec.Header().Get("Location"))
		}
		// enabled with a target → success + spawnPolicy + delegation tools
		rec = post(e, "/agents/a1/settings/delegation", "delegationEnabled=on&delegation-target=milo")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents/a1/settings/delegation?updated=1" {
			t.Fatalf("delegation redirect = %d %q", rec.Code, rec.Header().Get("Location"))
		}
		u := f.updatedAgent
		if u.Delegation != nil {
			t.Errorf("Delegation should be cleared after applyDelegation: %+v", u.Delegation)
		}
		if !containsString(u.Tools, "spawn_agents") || !containsString(u.Tools, "list_available_agents") {
			t.Errorf("delegation tools missing: %v", u.Tools)
		}
		sp, ok := u.Config["spawnPolicy"].(map[string]any)
		if !ok {
			t.Fatalf("spawnPolicy missing: %+v", u.Config)
		}
		if allow, _ := sp["allow"].([]string); len(allow) != 1 || allow[0] != "milo" {
			t.Errorf("spawnPolicy.allow = %v, want [milo]", allow)
		}
	})
}

// TestRenderAgentSandboxPage covers the sandbox config form: the enabled
// toggle, provider/base-image/repo/resource fields reflecting stored state,
// the provider availability list (enabled healthy options, disabled
// unavailable ones with their reason), the tool whitelist checkboxes, and the
// section layout.
func TestRenderAgentSandboxPage(t *testing.T) {
	cfg := &AgentSandboxConfig{
		Enabled:  true,
		Provider: "gvisor",
		RepoSource: &RepoSourceConfig{
			Type:   "fixed",
			URL:    "https://github.com/org/repo",
			Branch: "main",
		},
		Tools:          []string{"bash", "read"},
		ResourceLimits: &ResourceLimits{CPU: "2", Memory: "4G", Disk: "10G"},
		BaseImage:      "memory-workspace:latest",
		SetupCommands:  []string{"pip install -r requirements.txt"},
		EnvVars:        map[string]string{"FOO": "bar"},
	}
	data := agentSandboxData{
		Agent:  &AgentDefinition{ID: "a1", Name: "diane"},
		Config: cfg,
		Providers: []SandboxProvider{
			{Name: "gVisor (Docker)", Type: "gvisor", Registered: true, Healthy: true},
			{Name: "E2B", Type: "e2b", Healthy: false, Message: "E2B_API_KEY not set"},
		},
		Images: []SandboxImage{{ID: "img-1", Name: "memory-workspace:latest"}},
	}
	html := renderHTML(t, AgentSandboxPage(data))
	for _, want := range []string{
		`name="enabled" type="checkbox" checked`, "Enable sandbox",
		`name="provider"`, `value="gvisor" selected`, "gVisor (Docker)",
		`name="baseImage"`, `value="memory-workspace:latest"`,
		`name="repoSourceType"`, `value="fixed" selected`, `value="task_context"`,
		`name="repoSourceUrl"`, `value="https://github.com/org/repo"`,
		`name="repoSourceBranch"`, `value="main"`,
		`name="sandboxTool"`, `value="bash" checked`, `value="ast_grep"`,
		`name="cpu"`, `value="2"`, `name="memory"`, `value="4G"`, `name="disk"`, `value="10G"`,
		`name="setupCommands"`, "pip install -r requirements.txt",
		`name="envVars"`, "FOO=bar",
		"If none selected, all tools are allowed.",
		// availability list: healthy enabled, unavailable disabled + reason
		`value="gvisor" selected`, `>available<`,
		`value="e2b" disabled`, "E2B — unavailable", "E2B_API_KEY not set",
		`/agents/a1/sandbox/update`, `href="/agents/a1/sandbox"`, "Sandbox",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("sandbox page missing %q", want)
		}
	}
	// a healthy provider must not be marked unavailable
	if strings.Contains(html, "gVisor (Docker) — unavailable") {
		t.Error("healthy provider must not be marked unavailable")
	}

	// empty config → Auto selected by default, nothing checked, no
	// provider list → availability-unknown warning, no env content
	empty := agentSandboxData{Agent: &AgentDefinition{ID: "a1", Name: "diane"}}
	h := renderHTML(t, AgentSandboxPage(empty))
	if strings.Contains(h, `value="gvisor" selected`) || strings.Contains(h, `value="firecracker" selected`) || strings.Contains(h, `value="e2b" selected`) {
		t.Error("empty config must not select a concrete provider")
	}
	if !strings.Contains(h, `value="" selected`) {
		t.Error("empty config should default to Auto selected")
	}
	if strings.Contains(h, "checked") {
		t.Error("empty config must not render checked attributes")
	}
	if !strings.Contains(h, "No sandbox providers were reported") {
		t.Error("empty provider list must render the availability warning")
	}
	if strings.Contains(h, "FOO=bar</textarea>") {
		t.Error("empty config must not render env vars as content")
	}

	// failed provider fetch → same warning, but explaining the fetch failure
	warnErr := agentSandboxData{Agent: &AgentDefinition{ID: "a1", Name: "diane"}, ProviderListErr: true}
	if h := renderHTML(t, AgentSandboxPage(warnErr)); !strings.Contains(h, "Could not load sandbox provider availability") {
		t.Error("provider fetch failure must render the availability warning")
	}

	// saved provider missing from the API list → synthetic option stays
	// selected and selectable, so the form still submits it
	missing := agentSandboxData{
		Agent:     &AgentDefinition{ID: "a1", Name: "diane"},
		Config:    &AgentSandboxConfig{Provider: "firecracker"},
		Providers: []SandboxProvider{{Name: "gVisor (Docker)", Type: "gvisor", Registered: true, Healthy: true}},
	}
	hm := renderHTML(t, AgentSandboxPage(missing))
	if !strings.Contains(hm, `value="firecracker" selected`) {
		t.Error("saved provider missing from the list must render selected")
	}
	if strings.Contains(hm, "firecracker\" selected disabled") || strings.Contains(hm, "firecracker\" disabled") {
		t.Error("saved provider option must stay selectable so it round-trips on submit")
	}
	if !strings.Contains(hm, "Firecracker — unavailable") {
		t.Error("saved provider missing from the list must be marked unavailable")
	}

	// stored but reported-unavailable provider → its option stays selectable
	// (a disabled selected option is omitted from form data); a different
	// unavailable provider is still disabled.
	storedUnavailable := agentSandboxData{
		Agent:  &AgentDefinition{ID: "a1", Name: "diane"},
		Config: &AgentSandboxConfig{Provider: "firecracker"},
		Providers: []SandboxProvider{
			{Name: "E2B", Type: "e2b", Healthy: false, Message: "E2B_API_KEY not set"},
			{Name: "Firecracker", Type: "firecracker", Registered: false, Healthy: false, Message: "KVM not available on this host"},
		},
	}
	su := renderHTML(t, AgentSandboxPage(storedUnavailable))
	if !strings.Contains(su, `value="firecracker" selected`) {
		t.Error("stored unavailable provider must stay selected")
	}
	if strings.Contains(su, "firecracker\" selected disabled") || strings.Contains(su, "firecracker\" disabled") {
		t.Error("stored unavailable provider option must stay selectable so it round-trips on submit")
	}
	if !strings.Contains(su, `value="e2b" disabled`) {
		t.Error("a non-stored unavailable provider must still be disabled")
	}

	// load-error → whole-page error state
	if h := renderHTML(t, AgentSandboxPage(agentSandboxData{LoadErr: errTest})); !strings.Contains(h, "Agent unavailable") {
		t.Error("load-error state missing")
	}

	// flash states
	if h := renderHTML(t, AgentSandboxPage(agentSandboxData{Agent: &AgentDefinition{ID: "a1", Name: "diane"}, FlashMsg: "Sandbox updated."})); !strings.Contains(h, "Sandbox updated.") {
		t.Error("flash success missing")
	}
	if h := renderHTML(t, AgentSandboxPage(agentSandboxData{Agent: &AgentDefinition{ID: "a1", Name: "diane"}, FlashErr: errTest})); !strings.Contains(h, "backend unreachable") {
		t.Error("flash error should render the real error, missing")
	}
}

// TestUIAgentSandboxRoutes exercises the sandbox GET + POST routes against the
// fake backend: config prefill, best-effort provider/image lists, and the PRG
// update flow (success redirect + stored config, fixed-source URL validation).
func TestUIAgentSandboxRoutes(t *testing.T) {
	f := &fakeMemory{
		defs:      map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
		providers: []SandboxProvider{{Name: "gVisor (Docker)", Type: "gvisor", Healthy: true}},
		images:    []SandboxImage{{ID: "img-1", Name: "memory-workspace:latest"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/agents/:id/sandbox", s.uiAgentSandbox)
	e.POST("/agents/:id/sandbox/update", s.uiAgentSandboxUpdate)

	// GET: default config renders the form with catalog data
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/sandbox", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("sandbox GET status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"diane", `name="enabled"`, `value="gvisor"`, "gVisor (Docker)", ">available<", `value="memory-workspace:latest"`} {
		if !strings.Contains(body, want) {
			t.Errorf("sandbox GET missing %q", want)
		}
	}
	if strings.Contains(body, "gVisor (Docker) — unavailable") {
		t.Error("healthy provider must not be marked unavailable")
	}

	// GET with a failing provider fetch: the page still renders (no whole-page
	// error) and shows the availability warning.
	f.providersErr = errTest
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/sandbox", nil))
	f.providersErr = nil
	if rec.Code != http.StatusOK {
		t.Fatalf("sandbox GET with provider error status %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "Could not load sandbox provider availability") {
		t.Error("provider fetch failure should render the availability warning")
	}

	// POST: full form maps onto AgentSandboxConfig, redirects to ?updated=1
	form := "enabled=on&provider=gvisor&baseImage=memory-workspace%3Alatest" +
		"&repoSourceType=fixed&repoSourceUrl=https%3A%2F%2Fgithub.com%2Forg%2Frepo&repoSourceBranch=main" +
		"&sandboxTool=bash&sandboxTool=read" +
		"&cpu=2&memory=4G&disk=10G" +
		"&setupCommands=pip+install+-r+requirements.txt" +
		"&envVars=FOO%3Dbar%0ABAZ%3Dqux"
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agents/a1/sandbox/update", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sandbox update status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/agents/a1/sandbox?updated=1" {
		t.Errorf("redirect = %q, want /agents/a1/sandbox?updated=1", loc)
	}
	c := f.sandboxConfig
	if c == nil {
		t.Fatal("SetAgentSandboxConfig not called")
	}
	if !c.Enabled || c.Provider != "gvisor" || c.BaseImage != "memory-workspace:latest" {
		t.Errorf("provider mapping wrong: %+v", c)
	}
	if c.RepoSource == nil || c.RepoSource.Type != "fixed" || c.RepoSource.URL != "https://github.com/org/repo" || c.RepoSource.Branch != "main" {
		t.Errorf("repo source mapping wrong: %+v", c.RepoSource)
	}
	if len(c.Tools) != 2 || c.Tools[0] != "bash" || c.Tools[1] != "read" {
		t.Errorf("tools mapping wrong: %v", c.Tools)
	}
	if c.ResourceLimits == nil || c.ResourceLimits.CPU != "2" || c.ResourceLimits.Memory != "4G" || c.ResourceLimits.Disk != "10G" {
		t.Errorf("resource limits mapping wrong: %+v", c.ResourceLimits)
	}
	if len(c.SetupCommands) != 1 || c.SetupCommands[0] != "pip install -r requirements.txt" {
		t.Errorf("setup commands mapping wrong: %v", c.SetupCommands)
	}
	if len(c.EnvVars) != 2 || c.EnvVars["FOO"] != "bar" || c.EnvVars["BAZ"] != "qux" {
		t.Errorf("env vars mapping wrong: %v", c.EnvVars)
	}

	// POST: fixed source with empty URL → error redirect
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/agents/a1/sandbox/update", strings.NewReader("repoSourceType=fixed"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents/a1/sandbox?err=repository+URL+is+required+for+a+fixed+source" {
		t.Errorf("fixed without URL should redirect with error, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// POST: malformed env var line → error redirect
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/agents/a1/sandbox/update", strings.NewReader("envVars=NOPE"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=env+vars%3A+each+line+must+be+KEY%3DVALUE") {
		t.Errorf("bad env var should redirect with error, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// POST: task_context source (no URL needed), no tools → all allowed (empty list)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/agents/a1/sandbox/update", strings.NewReader("repoSourceType=task_context&repoSourceBranch=dev"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("task_context update status %d", rec.Code)
	}
	c = f.sandboxConfig
	if c.RepoSource == nil || c.RepoSource.Type != "task_context" || c.RepoSource.URL != "" || c.RepoSource.Branch != "dev" {
		t.Errorf("task_context repo source mapping wrong: %+v", c.RepoSource)
	}
	if len(c.Tools) != 0 {
		t.Errorf("no tools selected → empty list (all allowed), got %v", c.Tools)
	}
	if c.ResourceLimits != nil {
		t.Errorf("empty resource fields → nil limits, got %+v", c.ResourceLimits)
	}

	// unknown agent id → whole-page error
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/nope/sandbox", nil))
	if !strings.Contains(rec.Body.String(), "Agent unavailable") {
		t.Error("unknown agent should render the error state")
	}
}

// TestApplyAgentToolsSectionGroups covers the capability-group form mapping:
// group policies land under reserved "@group:<id>" keys, group enable/disable
// fans out to Tools + BannedTools, per-tool overrides still win, delegation
// tools are never fanned out, and a group the form did not render is untouched.
func TestApplyAgentToolsSectionGroups(t *testing.T) {
	newServer := func(f *fakeMemory) *echo.Echo {
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.POST("/agents/:id/settings/tools", s.uiAgentUpdateTools)
		return e
	}
	post := func(e *echo.Echo, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agents/a1/settings/tools", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		return rec
	}
	groups := []ToolGroup{
		{ID: "graph-write", Label: "Graph · Write", Tools: []string{"entity-create", "entity-delete"}},
		{ID: "web", Label: "Web", Tools: []string{"web_search"}},
		{ID: "agents", Label: "Agents", Tools: []string{"spawn_agents", "list_available_agents"}},
	}
	newFake := func(def *AgentDefinition) *fakeMemory {
		return &fakeMemory{defs: map[string]*AgentDefinition{"a1": def}}
	}

	t.Run("group ask writes a group entry", func(t *testing.T) {
		f := newFake(&AgentDefinition{ID: "a1", Name: "diane", ToolGroups: groups})
		post(newServer(f), "groupPolicy.graph-write=ask&groupEnabled.graph-write=on")
		p, ok := f.updatedAgent.ToolPolicies["@group:graph-write"]
		if !ok || !p.Confirm || p.Disabled {
			t.Errorf("@group:graph-write = %+v (ok=%v), want confirm", p, ok)
		}
	})

	t.Run("group inherit writes no group entry", func(t *testing.T) {
		f := newFake(&AgentDefinition{ID: "a1", Name: "diane", ToolGroups: groups})
		post(newServer(f), "groupPolicy.graph-write=inherit&groupEnabled.graph-write=on")
		if _, ok := f.updatedAgent.ToolPolicies["@group:graph-write"]; ok {
			t.Errorf("inherit must not write a group entry: %+v", f.updatedAgent.ToolPolicies)
		}
	})

	t.Run("group deny writes a disabled group entry", func(t *testing.T) {
		f := newFake(&AgentDefinition{ID: "a1", Name: "diane", ToolGroups: groups})
		post(newServer(f), "groupPolicy.graph-write=deny&groupEnabled.graph-write=on")
		p, ok := f.updatedAgent.ToolPolicies["@group:graph-write"]
		if !ok || !p.Disabled {
			t.Errorf("@group:graph-write = %+v (ok=%v), want disabled", p, ok)
		}
	})

	t.Run("disabling a group removes and bans its members", func(t *testing.T) {
		f := newFake(&AgentDefinition{
			ID: "a1", Name: "diane", ToolGroups: groups,
			Tools: []string{"entity-create", "entity-delete"},
		})
		// Rendered ON (both members enabled) then switched off: the policy field
		// is present, the enable switch is absent, and the baseline is "true".
		post(newServer(f), "tool=entity-create&tool=entity-delete&groupPolicy.graph-write=inherit&groupWasEnabled.graph-write=true")
		u := f.updatedAgent
		if containsString(u.Tools, "entity-create") || containsString(u.Tools, "entity-delete") {
			t.Errorf("disabled group members must leave Tools: %v", u.Tools)
		}
		if !containsString(u.BannedTools, "entity-create") || !containsString(u.BannedTools, "entity-delete") {
			t.Errorf("disabled group members must be banned: %v", u.BannedTools)
		}
	})

	t.Run("enabling a group restores its members", func(t *testing.T) {
		f := newFake(&AgentDefinition{
			ID: "a1", Name: "diane", ToolGroups: groups,
			BannedTools: []string{"entity-create", "entity-delete"},
		})
		post(newServer(f), "groupPolicy.graph-write=allow&groupEnabled.graph-write=on&groupWasEnabled.graph-write=false")
		u := f.updatedAgent
		if !containsString(u.Tools, "entity-create") || !containsString(u.Tools, "entity-delete") {
			t.Errorf("enabled group members must be in Tools: %v", u.Tools)
		}
		if containsString(u.BannedTools, "entity-create") || containsString(u.BannedTools, "entity-delete") {
			t.Errorf("enabled group members must be un-banned: %v", u.BannedTools)
		}
	})

	t.Run("per-tool override still wins alongside a group policy", func(t *testing.T) {
		f := newFake(&AgentDefinition{ID: "a1", Name: "diane", ToolGroups: groups})
		post(newServer(f), "tool=entity-create&toolPolicy.entity-create=deny&groupPolicy.graph-write=ask&groupEnabled.graph-write=on&groupWasEnabled.graph-write=false")
		u := f.updatedAgent
		if p := u.ToolPolicies["entity-create"]; !p.Disabled {
			t.Errorf("explicit tool override must survive: %+v", u.ToolPolicies)
		}
		if p, ok := u.ToolPolicies["@group:graph-write"]; !ok || !p.Confirm {
			t.Errorf("group policy must also be written: %+v", u.ToolPolicies)
		}
		if !containsString(u.Tools, "entity-delete") {
			t.Errorf("enabling the group must fan out its other member: %v", u.Tools)
		}
	})

	t.Run("delegation tools are never fanned out", func(t *testing.T) {
		// enabling a group that (wrongly) lists delegation tools must not add
		// them, and disabling must not ban them.
		f := newFake(&AgentDefinition{ID: "a1", Name: "diane", ToolGroups: groups})
		post(newServer(f), "groupPolicy.agents=ask&groupEnabled.agents=on&groupWasEnabled.agents=false&groupPolicy.graph-write=ask&groupEnabled.graph-write=on&groupWasEnabled.graph-write=false")
		u := f.updatedAgent
		if containsString(u.Tools, "spawn_agents") || containsString(u.Tools, "list_available_agents") {
			t.Errorf("group enable must not add delegation tools: %v", u.Tools)
		}
		if containsString(u.BannedTools, "spawn_agents") || containsString(u.BannedTools, "list_available_agents") {
			t.Errorf("group enable must not ban delegation tools: %v", u.BannedTools)
		}

		f2 := newFake(&AgentDefinition{
			ID: "a1", Name: "diane", ToolGroups: groups,
			Tools: []string{"spawn_agents", "list_available_agents"},
		})
		post(newServer(f2), "groupPolicy.agents=inherit&groupWasEnabled.agents=true")
		u2 := f2.updatedAgent
		if containsString(u2.BannedTools, "spawn_agents") || containsString(u2.BannedTools, "list_available_agents") {
			t.Errorf("group disable must not ban delegation tools: %v", u2.BannedTools)
		}
	})

	t.Run("a group absent from the form is left alone", func(t *testing.T) {
		f := newFake(&AgentDefinition{
			ID: "a1", Name: "diane", ToolGroups: groups,
			Tools: []string{"entity-create"},
		})
		post(newServer(f), "tool=entity-create&groupPolicy.graph-write=allow&groupEnabled.graph-write=on&groupWasEnabled.graph-write=false")
		u := f.updatedAgent
		if containsString(u.BannedTools, "web_search") {
			t.Errorf("an unrendered group must not be disabled: %v", u.BannedTools)
		}
		if containsString(u.Tools, "web_search") {
			t.Errorf("an unrendered group must not be enabled: %v", u.Tools)
		}
		if _, ok := u.ToolPolicies["@group:web"]; ok {
			t.Errorf("an unrendered group must write no policy: %+v", u.ToolPolicies)
		}
	})

	t.Run("no-op group save does not fan out the full group", func(t *testing.T) {
		f := newFake(&AgentDefinition{
			ID: "a1", Name: "diane", ToolGroups: groups,
			Tools: []string{"entity-create"}, // partial: entity-delete not enabled
		})
		// The switch is rendered ON (entity-create is enabled) and submitted
		// unchanged; a no-op save must not add the full-catalog remainder.
		post(newServer(f), "tool=entity-create&groupEnabled.graph-write=on&groupWasEnabled.graph-write=true")
		u := f.updatedAgent
		if containsString(u.Tools, "entity-delete") {
			t.Errorf("a no-op group save must not fan out the full group: %v", u.Tools)
		}
		if !containsString(u.Tools, "entity-create") {
			t.Errorf("the enabled member must survive a no-op save: %v", u.Tools)
		}
	})

	t.Run("toggling a partially-enabled group off bans its members", func(t *testing.T) {
		f := newFake(&AgentDefinition{
			ID: "a1", Name: "diane", ToolGroups: groups,
			Tools: []string{"entity-create"},
		})
		// Switch rendered ON (entity-create enabled) but submitted OFF: every
		// member (including the not-currently-enabled entity-delete) is banned.
		post(newServer(f), "tool=entity-create&groupWasEnabled.graph-write=true")
		u := f.updatedAgent
		if containsString(u.Tools, "entity-create") {
			t.Errorf("toggling off must remove enabled members: %v", u.Tools)
		}
		if !containsString(u.BannedTools, "entity-create") || !containsString(u.BannedTools, "entity-delete") {
			t.Errorf("toggling off must ban all group members: %v", u.BannedTools)
		}
	})

	t.Run("save preserves existing per-tool and group policies", func(t *testing.T) {
		f := newFake(&AgentDefinition{
			ID: "a1", Name: "diane", ToolGroups: groups,
			Tools: []string{"entity-create"},
			ToolPolicies: map[string]ToolPolicy{
				"entity-delete":   {Confirm: true},
				"some_other_tool": {Disabled: true},
				"@group:web":      {Disabled: true},
			},
		})
		// A no-touch save: no toolPolicy.* or groupPolicy.* control submitted.
		post(newServer(f), "tool=entity-create&groupEnabled.graph-write=on&groupWasEnabled.graph-write=true&groupWasEnabled.web=false&groupWasEnabled.agents=false")
		u := f.updatedAgent
		if p, ok := u.ToolPolicies["entity-delete"]; !ok || !p.Confirm {
			t.Errorf("existing per-tool override must survive a no-touch save: %+v", u.ToolPolicies)
		}
		if p, ok := u.ToolPolicies["some_other_tool"]; !ok || !p.Disabled {
			t.Errorf("unrendered per-tool entry must survive: %+v", u.ToolPolicies)
		}
		if p, ok := u.ToolPolicies["@group:web"]; !ok || !p.Disabled {
			t.Errorf("existing @group: entry must survive a no-touch save: %+v", u.ToolPolicies)
		}
	})
}

// TestRenderAgentSettingsToolGroups covers the capability-group panel: a
// collapsible header per group with its enable switch + policy select, the MCP
// server nested under the group that owns its tools, inheritance hints on rows
// without an override and the explicit value on rows with one, an "Other" group
// for uncovered tools, and no header for a group with no member tools.
func TestRenderAgentSettingsToolGroups(t *testing.T) {
	data := agentSettingsData{
		Section: "tools",
		Agent: &AgentDefinition{
			ID: "a1", Name: "diane",
			Tools:        []string{"entity-create", "ha_get_state"},
			ToolPolicies: map[string]ToolPolicy{"entity-create": {Confirm: true}},
			ToolGroups: []ToolGroup{
				{ID: "graph-write", Label: "Graph · Write", Description: "Create, update, or delete graph objects.", Policy: "ask", Enabled: true, Tools: []string{"entity-create", "entity-delete"}},
				{ID: "web", Label: "Web", Description: "Search and fetch web content.", Tools: []string{"web_search"}},
				{ID: "empty", Label: "Empty group"},
			},
		},
		Agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		MCPServers: []MCPServer{{Name: "builtin", ToolCount: 3, Tools: []MCPTool{
			{ToolName: "entity-create", Description: "create an object"},
			{ToolName: "entity-delete", Description: "delete an object"},
			{ToolName: "web_search", Description: "search the web"},
		}}},
	}
	html := renderHTML(t, AgentSettingsPage(data))
	for _, want := range []string{
		"Graph · Write", "Create, update, or delete graph objects.",
		`data-testid="tool-group-header-graph-write"`,
		`data-tool-group="graph-write"`,
		`name="groupPolicy.graph-write"`,
		`name="groupEnabled.graph-write" value="on" checked`,
		`data-testid="tool-group-policy-graph-write"`,
		`data-testid="tool-group-enabled-graph-write"`,
		`name="tool" type="checkbox" value="entity-create" checked`,
		`name="tool" type="checkbox" value="entity-delete"`,
		"Override · Ask",               // entity-create has an explicit entry
		"Inherits Graph · Write · Ask", // entity-delete has none
		"builtin",                      // MCP server nested in the group that owns it
		"Web", `name="groupPolicy.web"`,
		`name="tool" type="checkbox" value="web_search"`,
		`data-testid="tool-group-other"`,
		`name="tool" type="checkbox" value="ha_get_state" checked`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("grouped tools page missing %q", want)
		}
	}
	// the policy select reflects the stored group policy (scoped to each select)
	if !selectShowsValue(html, "groupPolicy.graph-write", "ask") {
		t.Error("graph-write policy select should show Ask")
	}
	if !selectShowsValue(html, "groupPolicy.web", "inherit") {
		t.Error("web policy select should show Inherit")
	}
	if strings.Contains(html, "Empty group") {
		t.Error("a group with no member tools must not render a header")
	}
	// an enabled group opens by default; an idle group stays collapsed
	if !strings.Contains(html, `open data-testid="tool-group" data-tool-group="graph-write"`) {
		t.Error("a group with enabled members should default open")
	}
	if strings.Contains(html, `open data-testid="tool-group" data-tool-group="web"`) {
		t.Error("a group with no enabled members should stay collapsed")
	}

	// no groups reported → fall back to the source-only grouping unchanged
	fallback := agentSettingsData{
		Section: "tools",
		Agent:   &AgentDefinition{ID: "a1", Name: "diane", Tools: []string{"web_search"}},
		Agents:  []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		MCPServers: []MCPServer{{Name: "builtin", ToolCount: 1, Tools: []MCPTool{
			{ToolName: "web_search", Description: "search the web"},
		}}},
	}
	fh := renderHTML(t, AgentSettingsPage(fallback))
	if !strings.Contains(fh, `name="tool" type="checkbox" value="web_search" checked`) {
		t.Error("fallback source-only grouping should still render server tools")
	}
	if strings.Contains(fh, `data-testid="tool-groups"`) {
		t.Error("fallback must not render the capability-group wrapper")
	}
}

// TestApplyAgentToolsSectionGroupsFullMembership covers the form mapping against
// full-membership toolGroups (the server's reconciled contract): a group the
// agent has fully disabled arrives with enabled:false and its whole member
// list, so switching it on restores every member; a relay tool's per-tool
// policy persists under its agent-facing <instance>_<tool> name.
func TestApplyAgentToolsSectionGroupsFullMembership(t *testing.T) {
	newServer := func(f *fakeMemory) *echo.Echo {
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.POST("/agents/:id/settings/tools", s.uiAgentUpdateTools)
		return e
	}
	post := func(e *echo.Echo, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agents/a1/settings/tools", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("a fully disabled group can be switched back on", func(t *testing.T) {
		f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {
			ID: "a1", Name: "diane",
			// full membership; the agent currently allows none of it
			ToolGroups: []ToolGroup{{ID: "graph-write", Label: "Graph · Write", Enabled: false, Tools: []string{"entity-create", "entity-delete"}}},
		}}}
		post(newServer(f), "groupPolicy.graph-write=allow&groupEnabled.graph-write=on&groupWasEnabled.graph-write=false")
		u := f.updatedAgent
		if !containsString(u.Tools, "entity-create") || !containsString(u.Tools, "entity-delete") {
			t.Errorf("enabling a disabled group must restore its full membership: %v", u.Tools)
		}
		if len(u.BannedTools) != 0 {
			t.Errorf("restored members must not stay banned: %v", u.BannedTools)
		}
	})

	t.Run("relay tool per-tool policy persists under its instance name", func(t *testing.T) {
		f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {
			ID: "a1", Name: "diane",
			ToolGroups: []ToolGroup{{ID: "mac", Label: "Mac", Tools: []string{"mac-ada_notes_search"}}},
		}}}
		post(newServer(f), "tool=mac-ada_notes_search&toolPolicy.mac-ada_notes_search=ask&groupPolicy.mac=inherit&groupEnabled.mac=on")
		p, ok := f.updatedAgent.ToolPolicies["mac-ada_notes_search"]
		if !ok || !p.Confirm {
			t.Errorf("relay tool override must persist: %+v", f.updatedAgent.ToolPolicies)
		}
	})
}

// TestGroupWritePathIgnoresToolGroups asserts the write path never depends on
// the read-only toolGroups field: with toolGroups populated, a save makes only
// the intended Tools / ToolPolicies / BannedTools changes.
func TestGroupWritePathIgnoresToolGroups(t *testing.T) {
	groups := []ToolGroup{
		{ID: "graph-write", Label: "Graph · Write", Enabled: true, Tools: []string{"entity-create", "entity-delete"}},
		{ID: "web", Label: "Web", Enabled: false, Tools: []string{"web_search"}},
	}
	f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {
		ID: "a1", Name: "diane",
		Tools:        []string{"entity-create"},
		BannedTools:  []string{"web_search"},
		ToolPolicies: map[string]ToolPolicy{"@group:web": {Confirm: true}},
		ToolGroups:   groups,
	}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/agents/:id/settings/tools", s.uiAgentUpdateTools)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agents/a1/settings/tools", strings.NewReader("tool=entity-create&groupPolicy.web=deny&groupEnabled.web=on&groupWasEnabled.web=false"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	u := f.updatedAgent
	if len(u.Tools) != 2 || !containsString(u.Tools, "entity-create") || !containsString(u.Tools, "web_search") {
		t.Errorf("Tools = %v, want [entity-create web_search] only", u.Tools)
	}
	if len(u.BannedTools) != 0 {
		t.Errorf("BannedTools = %v, want empty after enabling the web group", u.BannedTools)
	}
	if p, ok := u.ToolPolicies["@group:web"]; !ok || !p.Disabled {
		t.Errorf("@group:web = %+v (ok=%v), want disabled", p, ok)
	}
	if _, ok := u.ToolPolicies["web_search"]; ok {
		t.Errorf("enabling a group must not write per-tool entries: %+v", u.ToolPolicies)
	}
	if len(u.ToolGroups) != len(groups) {
		t.Errorf("the applier must not depend on toolGroups: %+v", u.ToolGroups)
	}
}

// TestSanitizeAgentWriteDropsToolGroups asserts the client write path strips the
// server-computed toolGroups field before serializing an agent definition.
func TestSanitizeAgentWriteDropsToolGroups(t *testing.T) {
	def := &AgentDefinition{ID: "a1", ToolGroups: []ToolGroup{{ID: "g", Tools: []string{"t"}}}}
	sanitizeAgentWrite(def)
	if def.ToolGroups != nil {
		t.Errorf("sanitizeAgentWrite must clear toolGroups, got %+v", def.ToolGroups)
	}
	sanitizeAgentWrite(nil) // must not panic
}

// TestRenderAgentSettingsToolGroupsFullMembership covers the picker against
// full-membership toolGroups: a fully disabled group still renders with its
// switch off and every member row unchecked (so it can be switched on), each
// member renders exactly once and never leaks into Other, a banned-only member
// still appears in its group, an uncovered relay tool lands in Other, and the
// inheritance hint stays visible at all widths.
func TestRenderAgentSettingsToolGroupsFullMembership(t *testing.T) {
	data := agentSettingsData{
		Section: "tools",
		Agent: &AgentDefinition{
			ID: "a1", Name: "diane",
			Tools:       []string{"mac-ada_notes_search"},
			BannedTools: []string{"entity-delete"},
			ToolGroups: []ToolGroup{
				{ID: "graph-write", Label: "Graph · Write", Description: "Create, update, or delete graph objects.", Enabled: false, Tools: []string{"entity-create", "entity-delete"}},
				{ID: "web", Label: "Web", Description: "Search and fetch web content.", Enabled: false, Tools: []string{"web_search"}},
			},
		},
		Agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		MCPServers: []MCPServer{{Name: "builtin", ToolCount: 3, Tools: []MCPTool{
			{ToolName: "entity-create"},
			{ToolName: "entity-delete"},
			{ToolName: "web_search"},
		}}},
		RelayNodes: []relayNode{{Session: RelaySession{InstanceID: "mac-ada", ToolCount: 1}, Tools: []RelayTool{{Name: "notes_search"}}}},
	}
	html := renderHTML(t, AgentSettingsPage(data))

	// a fully disabled group is still rendered (not dropped) with its switch off
	for _, want := range []string{
		`data-testid="tool-group-header-graph-write"`,
		`data-testid="tool-group-header-web"`,
		`name="groupEnabled.graph-write"`,
		`name="groupEnabled.web"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("grouped picker missing %q", want)
		}
	}
	for _, group := range []string{"graph-write", "web"} {
		if strings.Contains(html, `name="groupEnabled.`+group+`" value="on" checked`) {
			t.Errorf("group %s has no enabled members, switch must be off", group)
		}
	}

	// every member renders exactly once, unchecked (banned-only included)
	for _, name := range []string{"entity-create", "entity-delete", "web_search"} {
		if got := strings.Count(html, `value="`+name+`"`); got != 1 {
			t.Errorf("%s rendered %d times, want 1 (no duplicate, no Other leak)", name, got)
		}
		if strings.Contains(html, `value="`+name+`" checked`) {
			t.Errorf("%s must render unchecked", name)
		}
	}

	// the uncovered relay tool lands in Other, checked, so it is never lost
	if !strings.Contains(html, `data-testid="tool-group-other"`) {
		t.Error("uncovered tools should render the Other group")
	}
	if !strings.Contains(html, `name="tool" type="checkbox" value="mac-ada_notes_search" checked`) {
		t.Error("uncovered relay tool should appear checked in Other")
	}

	// inheritance stays visible at all widths: compact form alongside the full one
	if !strings.Contains(html, "→ Default") {
		t.Error("compact inheritance hint should render for inherited rows")
	}
	if !strings.Contains(html, "Inherits Graph · Write · Default") {
		t.Error("full inheritance hint should render for inherited rows")
	}
}

// goldenToolGroupsPath resolves the server-lane golden fixture relative to this
// test file, so it works from any working directory and never needs an absolute
// machine path.
func goldenToolGroupsPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the test file path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..",
		"openspec", "changes", "add-agent-tool-groups", "fixtures", "tool-groups.golden.json")
}

// TestGoldenToolGroupsFixtureContract is the cross-lane contract test: it parses
// the server-lane golden fixture — the real serialized AgentDefinition.toolGroups
// payload — into the gateway's local ToolGroup type and drives the real Tools
// panel render path with it. Deterministic: file + in-process render only.
func TestGoldenToolGroupsFixtureContract(t *testing.T) {
	raw, err := os.ReadFile(goldenToolGroupsPath(t))
	if err != nil {
		t.Fatalf("read golden tool-groups fixture: %v", err)
	}
	var groups []ToolGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatalf("unmarshal golden fixture into []ToolGroup via the gateway JSON tags: %v", err)
	}
	if len(groups) == 0 {
		t.Fatal("golden fixture has no tool groups")
	}

	// Frozen shape: every entry must carry all six D6 keys, with valid values.
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("unmarshal golden fixture as raw entries: %v", err)
	}
	if len(entries) != len(groups) {
		t.Fatalf("raw entry count %d != decoded group count %d", len(entries), len(groups))
	}
	ids := map[string]bool{}
	for i, g := range groups {
		for _, key := range []string{"id", "label", "description", "policy", "enabled", "tools"} {
			if _, ok := entries[i][key]; !ok {
				t.Errorf("group %q is missing the %q field", g.ID, key)
			}
		}
		if g.ID == "" || g.Label == "" || g.Description == "" {
			t.Errorf("group %d has an empty id/label/description: %+v", i, g)
		}
		if ids[g.ID] {
			t.Errorf("duplicate group id %q", g.ID)
		}
		ids[g.ID] = true
		switch g.Policy {
		case "", "allow", "ask", "deny":
		default:
			t.Errorf("group %q has invalid policy %q", g.ID, g.Policy)
		}
		if g.Tools == nil {
			t.Errorf("group %q has a null tools list (want an array)", g.ID)
		}
	}

	// The fixture must actually exercise every case this contract depends on.
	var (
		disabledWithMembers bool
		askGroupWithMembers bool
		relayStyleMember    bool
	)
	for _, g := range groups {
		if !g.Enabled && len(g.Tools) > 0 {
			disabledWithMembers = true
		}
		if g.Policy == "ask" && len(g.Tools) > 0 {
			askGroupWithMembers = true
		}
		for _, tool := range g.Tools {
			if strings.Contains(tool, "_") {
				relayStyleMember = true
			}
		}
	}
	if !disabledWithMembers {
		t.Fatal("fixture is missing case: a group with enabled:false and a non-empty tools list")
	}
	if !askGroupWithMembers {
		t.Fatal("fixture is missing case: a group with policy:\"ask\" and at least one member (needed to exercise an explicit override alongside a group policy)")
	}
	if !relayStyleMember {
		t.Fatal("fixture is missing case: a relay-style <instance>_<tool> member name")
	}

	// Reconstruct the representative agent + catalog from the fixture. Every
	// group member is either allowed (its group is enabled) or banned (disabled),
	// names with an underscore are relay-style, the rest come from one registry
	// server. The explicit overrides mirror the README: entity-create has a
	// per-tool entry, and document-list overrides its group's ask policy.
	var (
		tools       []string
		banned      []string
		serverTools []MCPTool
		relayTools  = map[string][]RelayTool{}
		relayOrder  []string
	)
	for _, g := range groups {
		for _, name := range g.Tools {
			if g.Enabled {
				tools = append(tools, name)
			} else {
				banned = append(banned, name)
			}
			instance, tool, isRelay := strings.Cut(name, "_")
			if isRelay {
				if _, ok := relayTools[instance]; !ok {
					relayOrder = append(relayOrder, instance)
				}
				relayTools[instance] = append(relayTools[instance], RelayTool{Name: tool})
				continue
			}
			serverTools = append(serverTools, MCPTool{ToolName: name})
		}
	}
	var relayNodes []relayNode
	for _, instance := range relayOrder {
		relayNodes = append(relayNodes, relayNode{
			Session: RelaySession{InstanceID: instance, ToolCount: len(relayTools[instance])},
			Tools:   relayTools[instance],
		})
	}

	data := agentSettingsData{
		Section: "tools",
		Agent: &AgentDefinition{
			ID: "a1", Name: "fixture-agent",
			Tools:       tools,
			BannedTools: banned,
			ToolPolicies: map[string]ToolPolicy{
				"entity-create": {Confirm: true},
				"document-list": {Disabled: true},
			},
			ToolGroups: groups,
		},
		Agents:     []AgentDefinitionSummary{{ID: "a1", Name: "fixture-agent"}},
		MCPServers: []MCPServer{{Name: "builtin", ToolCount: len(serverTools), Tools: serverTools}},
		RelayNodes: relayNodes,
	}
	html := renderHTML(t, AgentSettingsPage(data))

	// every fixture group renders a header (no group dropped) and the relay-style
	// member renders inside its group's relay sub-group.
	for _, g := range groups {
		if !strings.Contains(html, `data-testid="tool-group-header-`+g.ID+`"`) {
			t.Errorf("group %q header is missing from the panel", g.ID)
		}
	}
	if !strings.Contains(html, "relay1") {
		t.Error("relay-style group member should render under its relay node sub-group")
	}
	if !strings.Contains(html, `name="tool" type="checkbox" value="relay1_reminders_list" checked`) {
		t.Error("relay-style member should render checked in its relay sub-group")
	}

	// (a) a disabled group still renders its rows unchecked and offers the
	//     controls needed to switch it on.
	if !strings.Contains(html, `data-testid="tool-group-enabled-schema-write"`) ||
		!strings.Contains(html, `data-testid="tool-group-policy-schema-write"`) {
		t.Error("a fully disabled group must still render its enable switch and policy select")
	}
	if strings.Contains(html, `name="groupEnabled.schema-write" value="on" checked`) {
		t.Error("schema-write has no enabled members, its switch must be off")
	}
	for _, name := range []string{"schema-create", "schema-delete"} {
		if !strings.Contains(html, `name="tool" type="checkbox" value="`+name+`"`) {
			t.Errorf("disabled group member %s is missing from the panel", name)
		}
		if strings.Contains(html, `value="`+name+`" checked`) {
			t.Errorf("disabled group member %s must render unchecked", name)
		}
	}

	// (b) the stored ask policy renders on the group's policy select.
	if !selectShowsValue(html, "groupPolicy.documents", "ask") {
		t.Error("documents group policy select should show Ask")
	}

	// (c) an explicit per-tool override renders its own value; a tool without one
	//     renders the inherited hint for its group.
	for _, want := range []string{
		"Override · Ask",                   // entity-create (README's per-tool entry)
		"Override · Deny",                  // document-list overrides its ask group
		"Inherits Documents · Ask",         // document-create inherits ask
		"Inherits Graph · Write · Default", // entity-update inherits the graph-write group
	} {
		if !strings.Contains(html, want) {
			t.Errorf("hint %q is missing from the rendered panel", want)
		}
	}

	// (d) every fixture member renders exactly once (so no duplicate row inside a
	//     group) and none leaks into the Other fallback block.
	for _, g := range groups {
		for _, name := range g.Tools {
			if got := strings.Count(html, `value="`+name+`"`); got != 1 {
				t.Errorf("fixture member %s rendered %d times, want exactly 1", name, got)
			}
		}
	}
	if strings.Contains(html, `data-testid="tool-group-other"`) {
		t.Error("no fixture member may leak into the Other fallback block")
	}
}

// sandboxSelectedProvider parses the rendered provider <select> and returns the
// value a browser would submit: the first selected option that is not disabled,
// or "" when no such option exists (a disabled selected option contributes
// nothing to the form data set).
func sandboxSelectedProvider(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, `id="agent-sandbox-provider"`)
	if start < 0 {
		t.Fatal("provider select not found")
	}
	end := strings.Index(html[start:], "</select>")
	if end < 0 {
		t.Fatal("provider select not closed")
	}
	block := html[start : start+end]
	for {
		i := strings.Index(block, "<option")
		if i < 0 {
			return ""
		}
		j := strings.Index(block[i:], ">")
		if j < 0 {
			return ""
		}
		tag := block[i : i+j]
		block = block[i+j+1:]
		if !strings.Contains(tag, "selected") || strings.Contains(tag, "disabled") {
			continue
		}
		v := strings.Index(tag, `value="`)
		if v < 0 {
			continue
		}
		v += len(`value="`)
		e := strings.Index(tag[v:], `"`)
		if e < 0 {
			continue
		}
		return tag[v : v+e]
	}
}

// TestUIAgentSandboxProviderRoundTripSubmit is the submit regression for the
// disabled-option defect: saving an unchanged sandbox form must never drop a
// stored-but-unavailable provider into Auto. The test mirrors what a browser
// submits (selected option that is not disabled) and asserts the handler
// persists that value.
func TestUIAgentSandboxProviderRoundTripSubmit(t *testing.T) {
	cases := []struct {
		name       string
		cfg        *AgentSandboxConfig
		providers  []SandboxProvider
		wantSubmit string // value a browser would send for the provider field
		wantStored string // provider the handler must persist
	}{
		{
			name: "stored unavailable provider round-trips",
			cfg:  &AgentSandboxConfig{Enabled: true, Provider: "firecracker"},
			providers: []SandboxProvider{
				{Name: "gVisor (Docker)", Type: "gvisor", Registered: true, Healthy: true},
				{Name: "Firecracker", Type: "firecracker", Registered: false, Healthy: false, Message: "KVM not available on this host"},
				{Name: "E2B", Type: "e2b", Registered: false, Healthy: false, Message: "E2B_API_KEY not set"},
			},
			wantSubmit: "firecracker",
			wantStored: "firecracker",
		},
		{
			name: "stored provider absent from the list round-trips",
			cfg:  &AgentSandboxConfig{Enabled: true, Provider: "firecracker"},
			providers: []SandboxProvider{
				{Name: "gVisor (Docker)", Type: "gvisor", Registered: true, Healthy: true},
			},
			wantSubmit: "firecracker",
			wantStored: "firecracker",
		},
		{
			name: "stored Auto round-trips as Auto",
			cfg:  &AgentSandboxConfig{Enabled: true},
			providers: []SandboxProvider{
				{Name: "gVisor (Docker)", Type: "gvisor", Registered: true, Healthy: false, Message: "docker daemon unreachable"},
			},
			wantSubmit: "",
			wantStored: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{
				defs:          map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
				sandboxConfig: tc.cfg,
				providers:     tc.providers,
			}
			s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
			e := echo.New()
			e.GET("/agents/:id/sandbox", s.uiAgentSandbox)
			e.POST("/agents/:id/sandbox/update", s.uiAgentSandboxUpdate)

			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/sandbox", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("sandbox GET status %d", rec.Code)
			}
			html := rec.Body.String()

			got := sandboxSelectedProvider(t, html)
			if got != tc.wantSubmit {
				t.Errorf("browser would submit provider %q, want %q", got, tc.wantSubmit)
			}
			// exactly one provider control: no hidden fallback that could
			// override an explicit Auto choice
			if n := strings.Count(html, `name="provider"`); n != 1 {
				t.Errorf("expected exactly one provider control, got %d", n)
			}

			// submit the form unchanged (only the provider field matters here)
			rec = httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/agents/a1/sandbox/update", strings.NewReader("enabled=on&provider="+got))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("sandbox update status %d", rec.Code)
			}
			if f.sandboxConfig == nil {
				t.Fatal("SetAgentSandboxConfig not called")
			}
			if f.sandboxConfig.Provider != tc.wantStored {
				t.Errorf("stored provider = %q, want %q", f.sandboxConfig.Provider, tc.wantStored)
			}
		})
	}
}

// TestUIAgentSandboxExplicitAutoSubmit is the control: a user who explicitly
// picks Auto submits provider="" and Auto persists (nothing re-injects the
// stored provider).
func TestUIAgentSandboxExplicitAutoSubmit(t *testing.T) {
	f := &fakeMemory{
		defs:          map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane"}},
		sandboxConfig: &AgentSandboxConfig{Enabled: true, Provider: "firecracker"},
		providers: []SandboxProvider{
			{Name: "Firecracker", Type: "firecracker", Registered: false, Healthy: false, Message: "KVM not available on this host"},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/agents/:id/sandbox/update", s.uiAgentSandboxUpdate)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agents/a1/sandbox/update", strings.NewReader("enabled=on&provider="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sandbox update status %d", rec.Code)
	}
	if f.sandboxConfig == nil || f.sandboxConfig.Provider != "" {
		t.Errorf("explicit Auto must persist an empty provider, got %+v", f.sandboxConfig)
	}
}
