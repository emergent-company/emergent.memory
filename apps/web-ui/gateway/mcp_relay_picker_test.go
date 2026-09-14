package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- Agent tool picker: relay node groups (tasks 4.1-4.3) ---

// relayGroupBorder is the <details> class marker of a real relay group in the
// tool picker — it distinguishes a live relay group from a relay tool that
// fell back into the Other group (its node disconnected).
const relayGroupBorder = "border-primary/25"

func relaySettingsData() agentSettingsData {
	return agentSettingsData{
		Agent: &AgentDefinition{
			ID:    "a1",
			Name:  "diane",
			Tools: []string{"web_search", "mac-ada_notes_search"},
		},
		Agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		MCPServers: []MCPServer{
			{Name: "builtin", ToolCount: 1, Tools: []MCPTool{{ToolName: "web_search", Description: "search the web"}}},
		},
		RelayNodes: []relayNode{
			{
				Session: RelaySession{InstanceID: "mac-ada", Version: "1.2.0", ToolCount: 2},
				Tools: []RelayTool{
					{Name: "notes_search", Description: "Search Apple Notes"},
					{Name: "reminders_add"},
				},
			},
			// a second node to assert per-node grouping
			{Session: RelaySession{InstanceID: "studio-linux"}, Tools: []RelayTool{{Name: "files_list"}}},
		},
	}
}

// TestRenderAgentSettingsRelayTools covers the picker relay groups (spec
// "Relay node tools listed by node"): one group per connected node, labelled
// with the instance id + a visually distinct remote badge, each tool a
// checkbox carrying its agent-facing <instance>_<tool> value, checked when the
// agent's whitelist contains it, and no per-tool policy select in relay groups.
func TestRenderAgentSettingsRelayTools(t *testing.T) {
	html := renderHTML(t, AgentSettingsPage(relaySettingsData()))
	for _, want := range []string{
		"mac-ada", "studio-linux", "remote",
		`name="tool" type="checkbox" value="mac-ada_notes_search" checked`,
		`name="tool" type="checkbox" value="mac-ada_reminders_add"`,
		`name="tool" type="checkbox" value="studio-linux_files_list"`,
		"Search Apple Notes",
		`name="tool" type="checkbox" value="web_search" checked`, // registry group untouched
	} {
		if !strings.Contains(html, want) {
			t.Errorf("relay picker missing %q", want)
		}
	}
	// no per-tool policy selects inside relay groups (allow-only v1)
	if strings.Contains(html, `name="toolPolicy.mac-ada_notes_search"`) ||
		strings.Contains(html, `name="toolPolicy.studio-linux_files_list"`) {
		t.Error("relay groups must not render per-tool policy selects (allow-only v1)")
	}
	// registry group keeps its policy select
	if !strings.Contains(html, `name="toolPolicy.web_search"`) {
		t.Error("registry server group must keep its per-tool policy select")
	}
	// whitelisted relay tool appears exactly once (relay group, not Other)
	if got := strings.Count(html, `value="mac-ada_notes_search"`); got != 1 {
		t.Errorf("whitelisted relay tool rendered %d times, want 1 (no Other duplicate)", got)
	}
	// checked boxes are bare `checked`, never `checked="false"`
	if strings.Contains(html, `checked="false"`) {
		t.Error("must not render checked=\"false\" (boolean-attribute bug)")
	}
}

// TestRenderAgentSettingsRelayOnlyTools covers the empty-state boundary: when
// only relay nodes offer tools (no registry servers, no unlisted tools) the
// picker renders the relay groups instead of the "No tools available" state.
func TestRenderAgentSettingsRelayOnlyTools(t *testing.T) {
	data := agentSettingsData{
		Agent:  &AgentDefinition{ID: "a1", Name: "diane", Tools: []string{"mac-ada_notes_search"}},
		Agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		RelayNodes: []relayNode{
			{Session: RelaySession{InstanceID: "mac-ada"}, Tools: []RelayTool{{Name: "notes_search"}}},
		},
	}
	html := renderHTML(t, AgentSettingsPage(data))
	if strings.Contains(html, "No tools available") {
		t.Error("relay-only tools must not trigger the empty state")
	}
	if !strings.Contains(html, `value="mac-ada_notes_search" checked`) {
		t.Error("relay tool should render checked")
	}

	// empty node (no tools) renders no group and no empty state regression
	data = agentSettingsData{
		Agent:      &AgentDefinition{ID: "a1", Name: "diane"},
		Agents:     []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		RelayNodes: []relayNode{{Session: RelaySession{InstanceID: "empty-node"}}},
	}
	html = renderHTML(t, AgentSettingsPage(data))
	if strings.Contains(html, "empty-node") {
		t.Error("a node serving no tools must not render an empty group")
	}
	if !strings.Contains(html, "No tools available") {
		t.Error("no servers, no relay tools, no unlisted tools → empty state")
	}
}

// TestRenderAgentSettingsNoRelayNodes asserts the picker is unchanged when no
// relay nodes are connected (spec "No relay nodes connected"): no remote group,
// registry + Other groups intact.
func TestRenderAgentSettingsNoRelayNodes(t *testing.T) {
	data := relaySettingsData()
	data.RelayNodes = nil
	html := renderHTML(t, AgentSettingsPage(data))
	if strings.Contains(html, "remote") {
		t.Error("no relay nodes → no remote group")
	}
	for _, want := range []string{
		`name="tool" type="checkbox" value="web_search" checked`,
		`name="toolPolicy.web_search"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("registry group missing %q without relay nodes", want)
		}
	}
}

// TestRelayToolPreservedWhenNodeOffline is task 4.3: an agent that whitelisted
// <instance>_<tool> while its node was connected keeps the tool after the node
// disconnects — it falls into the existing "Other" group, checked, so saving
// never drops it.
func TestRelayToolPreservedWhenNodeOffline(t *testing.T) {
	data := agentSettingsData{
		Agent:  &AgentDefinition{ID: "a1", Name: "diane", Tools: []string{"web_search", "mac-ada_notes_search", "ha_get_state"}},
		Agents: []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		MCPServers: []MCPServer{
			{Name: "builtin", ToolCount: 1, Tools: []MCPTool{{ToolName: "web_search"}}},
		},
		// mac-ada disconnected: not in RelayNodes anymore
	}
	html := renderHTML(t, AgentSettingsPage(data))
	for _, want := range []string{"Other", `value="mac-ada_notes_search" checked`, `value="ha_get_state" checked`} {
		if !strings.Contains(html, want) {
			t.Errorf("offline relay tool should survive in the Other group, missing %q", want)
		}
	}
	// no relay group is rendered for the disconnected node
	if got := strings.Count(html, `value="mac-ada_notes_search"`); got != 1 {
		t.Errorf("offline relay tool must render exactly once (Other group only), got %d", got)
	}
}

// TestUIAgentSettingsRouteRelayNodes is task 4.1's loader coverage: the agent
// settings loader fetches relay sessions + per-node tools alongside the MCP
// servers (nodes present), renders no relay group when none are connected
// (nodes empty), and degrades to no relay groups on a relay API error while
// the rest of the page stays intact.
func TestUIAgentSettingsRouteRelayNodes(t *testing.T) {
	newServer := func(f *fakeMemory) *echo.Echo {
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.GET("/agents/:id/settings", s.uiAgentSettings)
		return e
	}
	agent := &AgentDefinition{ID: "a1", Name: "diane", Tools: []string{"mac-ada_notes_search"}}

	// nodes present: loader threads relay sessions + tools into the picker
	f := &fakeMemory{
		defs:          map[string]*AgentDefinition{"a1": agent},
		agents:        []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		relaySessions: []RelaySession{{InstanceID: "mac-ada", Version: "1.2.0", ToolCount: 2}},
		relayTools:    map[string][]RelayTool{"mac-ada": {{Name: "notes_search"}, {Name: "reminders_add"}}},
	}
	e := newServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("nodes-present status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`value="mac-ada_notes_search" checked`, `value="mac-ada_reminders_add"`, relayGroupBorder} {
		if !strings.Contains(body, want) {
			t.Errorf("nodes-present settings page missing %q", want)
		}
	}

	// nodes empty: no relay group, page otherwise normal
	f = &fakeMemory{
		defs:          map[string]*AgentDefinition{"a1": agent},
		agents:        []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		relaySessions: []RelaySession{},
	}
	e = newServer(f)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/settings", nil))
	body = rec.Body.String()
	if strings.Contains(body, relayGroupBorder) {
		t.Error("no connected nodes → no relay group")
	}
	if !strings.Contains(body, "diane") {
		t.Error("rest of the settings page should still render")
	}
	// the whitelisted relay tool survives via the Other group (never dropped)
	if !strings.Contains(body, `value="mac-ada_notes_search" checked`) {
		t.Error("whitelisted relay tool should persist in the Other group when its node is offline")
	}

	// relay API error: degrades to no relay groups, rest of the page intact
	f = &fakeMemory{
		defs:             map[string]*AgentDefinition{"a1": agent},
		agents:           []AgentDefinitionSummary{{ID: "a1", Name: "diane"}},
		relaySessionsErr: errTest,
	}
	e = newServer(f)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents/a1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("relay-error status %d", rec.Code)
	}
	body = rec.Body.String()
	if strings.Contains(body, relayGroupBorder) {
		t.Error("relay API error must not render relay groups")
	}
	if !strings.Contains(body, "diane") || strings.Contains(body, "Settings unavailable") {
		t.Error("relay API error must leave the rest of the settings page intact")
	}
}
