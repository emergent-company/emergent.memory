package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- External MCP nodes page: render + route tests ---

// TestSettingsSubNavMCPNodesEntry asserts the Settings rail carries the "MCP
// nodes" entry linking /settings/mcp-nodes (distinct from the sibling change's
// "MCP Servers" registry entry).
func TestSettingsSubNavMCPNodesEntry(t *testing.T) {
	html := renderHTML(t, settingsSubNav("mcp-nodes", false))
	if !strings.Contains(html, `href="/settings/mcp-nodes"`) || !strings.Contains(html, "MCP nodes") {
		t.Error("settings rail missing the MCP nodes entry")
	}
	// active state marks the current page
	if !strings.Contains(html, `aria-current="page"`) {
		t.Error("active MCP nodes entry should carry aria-current")
	}
	// the entry sits alongside the existing settings sections
	for _, want := range []string{`href="/settings"`, `href="/settings/voice"`, `href="/settings/devices"`} {
		if !strings.Contains(html, want) {
			t.Errorf("settings rail missing %q", want)
		}
	}
	// sibling's label must NOT be reused here
	if strings.Contains(html, "MCP Servers") {
		t.Error("rail entry must not be labelled \"MCP Servers\"")
	}
}

func relayPageFixture() externalMCPNodesPageData {
	return externalMCPNodesPageData{
		Nodes: []mcpNodeView{
			{InstanceID: "mac-ada", Version: "1.2.0", ToolCount: 2, Live: true, ConnectedAt: time.Now().Add(-25 * time.Minute)},
			{InstanceID: "studio-linux", Version: "0.9.1", ToolCount: 1, Live: true, ConnectedAt: time.Now().Add(-2 * time.Hour)},
		},
	}
}

// TestRenderExternalMCPNodesPage covers the happy path: both live nodes listed
// with the green Connected state, instance id / version / tool count / relative
// connection time, the selected node's tools under agent-facing
// <instance>_<tool> names with descriptions, and the instance-id-as-unique-key
// hint copy.
func TestRenderExternalMCPNodesPage(t *testing.T) {
	data := relayPageFixture()
	data.SelectedLive = true
	data.Selected = &relayNode{
		Session: RelaySession{InstanceID: "mac-ada", Version: "1.2.0", ToolCount: 2, ConnectedAt: time.Now().Add(-25 * time.Minute)},
		Tools: []RelayTool{
			{Name: "notes_search", Description: "Search Apple Notes"},
			{Name: "reminders_add"},
		},
	}
	html := renderHTML(t, ExternalMCPNodesPage(data))
	for _, want := range []string{
		"External MCP nodes",
		"mac-ada", "studio-linux",
		"v1.2.0", "v0.9.1",
		"2 tools", "1 tool",
		"connected 25m ago", "connected 2h ago",
		`href="/settings/mcp-nodes?node=mac-ada"`, `href="/settings/mcp-nodes?node=studio-linux"`,
		"mac-ada_notes_search", "Search Apple Notes", "mac-ada_reminders_add",
		"remote", "unique key",
		"Connected", "status-success",
		`href="/settings/mcp-nodes"`, "Refresh",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("nodes page missing %q", want)
		}
	}
	// live rows never render the disconnected state or a last-seen line
	if strings.Contains(html, "Disconnected") || strings.Contains(html, "last seen") {
		t.Error("live page must not render a disconnected / last-seen state")
	}
	// the settings rail renders on the page with the entry active
	if !strings.Contains(html, `href="/settings/mcp-nodes"`) {
		t.Error("page missing the settings rail entry link")
	}

	// unselected: no tools panel content, just the pick hint
	dataNoSel := relayPageFixture()
	htmlNoSel := renderHTML(t, ExternalMCPNodesPage(dataNoSel))
	if !strings.Contains(htmlNoSel, "Select a node above to inspect the tools it serves.") {
		t.Error("unselected page should prompt to select a node")
	}
	if strings.Contains(htmlNoSel, "notes_search") {
		t.Error("unselected page must not render any node's tools")
	}
}

// TestRenderExternalMCPNodesOfflineRow covers the offline rendering: muted row
// with the gray Disconnected state, "last seen <relative>" instead of
// "connected", the compact Remove button with its confirm dialog and remove
// action, and the cached tool snapshot in the tools panel with its last-seen
// note.
func TestRenderExternalMCPNodesOfflineRow(t *testing.T) {
	data := externalMCPNodesPageData{
		Nodes: []mcpNodeView{
			{InstanceID: "mac-ada", Version: "1.1.0", ToolCount: 1, LastSeen: time.Now().Add(-3 * time.Hour)},
		},
		Selected: &relayNode{
			Session: RelaySession{InstanceID: "mac-ada", Version: "1.1.0", ToolCount: 1},
			Tools:   []RelayTool{{Name: "notes_search", Description: "Search Apple Notes"}},
		},
	}
	html := renderHTML(t, ExternalMCPNodesPage(data))
	for _, want := range []string{
		"Disconnected", "status-neutral",
		"last seen 3h ago",
		"offline snapshot",
		"Snapshot recorded when this node was last seen 3h ago.",
		"mac-ada_notes_search",
		`action="/settings/mcp-nodes/remove"`,
		`name="instance_id"`,
		`value="mac-ada"`,
		"Remove node", "mcp-node-remove-mac-ada",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("offline row missing %q", want)
		}
	}
	if strings.Contains(html, "Connected") {
		t.Error("offline row must not render a connected state")
	}
	if strings.Contains(html, "connected 3h ago") {
		t.Error("offline row must not render a connected line")
	}

	// offline node with no cached tools → clear no-tools-recorded state
	data.Selected.Tools = nil
	html = renderHTML(t, ExternalMCPNodesPage(data))
	if !strings.Contains(html, "No tools recorded for this node.") {
		t.Error("offline no-tools state missing")
	}
}

// TestRenderExternalMCPNodeRemoveControl covers the compact destructive Remove
// affordance: an icon button plus its confirm dialog, both siblings of the row
// link (never nested interactive controls), whose form posts the node's
// instance_id to /settings/mcp-nodes/remove.
func TestRenderExternalMCPNodeRemoveControl(t *testing.T) {
	html := renderHTML(t, ExternalMCPNodesPage(relayPageFixture()))
	for _, want := range []string{
		`aria-label="Remove node mac-ada"`,
		`id="mcp-node-remove-mac-ada"`,
		"Remove node", "a still-connected host will reappear on refresh",
		`name="instance_id"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("remove affordance missing %q", want)
		}
	}
	if strings.Contains(html, "hx-confirm") {
		t.Error("remove must use the confirm dialog, not a raw hx-confirm stub")
	}
	// the destructive form lives in the dialog after the row link closes, so no
	// interactive control is nested inside the anchor.
	linkIdx := strings.Index(html, `href="/settings/mcp-nodes?node=mac-ada"`)
	formIdx := strings.Index(html, `action="/settings/mcp-nodes/remove"`)
	if linkIdx < 0 || formIdx < linkIdx {
		t.Fatalf("remove form should follow the row link (link=%d form=%d)", linkIdx, formIdx)
	}
	if !strings.Contains(html[linkIdx:formIdx], "</a>") {
		t.Error("row link must close before the remove form (sibling controls)")
	}
}

// TestRenderExternalMCPNodesPageEmpty covers the "no external nodes connected"
// empty state (spec: registry empty, no sessions).
func TestRenderExternalMCPNodesPageEmpty(t *testing.T) {
	html := renderHTML(t, ExternalMCPNodesPage(externalMCPNodesPageData{}))
	for _, want := range []string{"No external nodes connected", "MCP relay", "relay"} {
		if !strings.Contains(html, want) {
			t.Errorf("empty state missing %q", want)
		}
	}
	if strings.Contains(html, "0 tools") {
		t.Error("empty state must not render the count badge")
	}
	if strings.Contains(html, "/settings/mcp-nodes/remove") {
		t.Error("empty state must not render a remove control")
	}
}

// TestRenderExternalMCPNodesPageStates covers the degraded states: live-fetch
// failure rendering an error banner alongside registry rows (not blanking the
// page), the not-recorded selection state, and the per-node tools failure
// degrading only the tools panel while the node list stays usable.
func TestRenderExternalMCPNodesPageStates(t *testing.T) {
	// live-fetch failure with registry rows → banner + offline rows, not blank
	data := externalMCPNodesPageData{
		Nodes:   []mcpNodeView{{InstanceID: "mac-ada", Version: "1.1.0", ToolCount: 1, LastSeen: time.Now().Add(-3 * time.Hour)}},
		LoadErr: errTest,
	}
	html := renderHTML(t, ExternalMCPNodesPage(data))
	if !strings.Contains(html, "Couldn&#39;t load live relay sessions") || !strings.Contains(html, "mac-ada") {
		t.Error("live-fetch failure should render an error banner plus the registry, not blank the page")
	}
	if !strings.Contains(html, "Disconnected") || !strings.Contains(html, "status-neutral") {
		t.Error("live-fetch failure should render registry rows as offline")
	}

	// ?node=<id> for an instance neither live nor recorded → not-recorded state
	data = relayPageFixture()
	data.NodeNotFound = true
	html = renderHTML(t, ExternalMCPNodesPage(data))
	if !strings.Contains(html, "neither connected nor in the registry") {
		t.Error("not-recorded selection state missing")
	}

	// tools fetch failure → alert in the tools panel, node list still rendered
	data = relayPageFixture()
	data.SelectedLive = true
	data.Selected = &relayNode{Session: data.Nodes[0].session()}
	data.ToolsErr = errTest
	html = renderHTML(t, ExternalMCPNodesPage(data))
	if !strings.Contains(html, "Couldn&#39;t load this node&#39;s tools") || !strings.Contains(html, "mac-ada") {
		t.Error("tools failure should degrade the panel while keeping the node list")
	}
	if !strings.Contains(html, "studio-linux") {
		t.Error("tools failure must not hide the other connected nodes")
	}

	// selected live node serving no tools → clear no-tools state
	data = relayPageFixture()
	data.SelectedLive = true
	data.Selected = &relayNode{Session: data.Nodes[0].session(), Tools: []RelayTool{}}
	html = renderHTML(t, ExternalMCPNodesPage(data))
	if !strings.Contains(html, "connected but serves no tools") {
		t.Error("no-tools state missing")
	}
}

// session is a small helper turning a view into a Session for selected-node
// fixtures.
func (v mcpNodeView) session() RelaySession {
	return RelaySession{InstanceID: v.InstanceID, Version: v.Version, ToolCount: v.ToolCount, ConnectedAt: v.ConnectedAt}
}

// TestUIMCPNodesRoute exercises GET /settings/mcp-nodes end to end against the
// fake backend: reconciliation seeds the registry, ordering is live-first then
// offline, ?node= selection serves live tools or the cached snapshot, an
// unknown node renders the not-recorded state, a live-fetch failure falls back
// to the registry behind a banner, and the remove route deletes the record.
func TestUIMCPNodesRoute(t *testing.T) {
	now := time.Now()
	f := &fakeMemory{
		relaySessions: []RelaySession{
			{InstanceID: "older", Version: "0.9.1", ToolCount: 1, ConnectedAt: now.Add(-2 * time.Hour)},
			{InstanceID: "newer", Version: "1.2.0", ToolCount: 2, ConnectedAt: now.Add(-25 * time.Minute)},
		},
		relayTools: map[string][]RelayTool{
			"newer": {{Name: "notes_search", Description: "Search Apple Notes"}, {Name: "reminders_add"}},
			"older": {{Name: "files_list"}},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/settings/mcp-nodes", s.uiMCPNodes)
	e.POST("/settings/mcp-nodes/remove", s.uiMCPNodesRemove)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// list: both live nodes, newest (most recently connected) first
	rec := get("/settings/mcp-nodes")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"newer", "older", "2 nodes", "Connected", "Select a node above", `action="/settings/mcp-nodes/remove"`} {
		if !strings.Contains(body, want) {
			t.Errorf("route missing %q", want)
		}
	}
	if strings.Index(body, "?node=newer") > strings.Index(body, "?node=older") {
		t.Error("sessions must render most recently connected first")
	}

	// reconciliation persisted both nodes with firstSeen + lastSeen and the
	// cached tools snapshot for each.
	registry, err := s.loadRelayRegistry(t.Context())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(registry) != 2 {
		t.Fatalf("registry size = %d, want 2", len(registry))
	}
	if rec, ok := registry["newer"]; !ok || rec.Version != "1.2.0" || rec.ToolCount != 2 || len(rec.Tools) != 2 || rec.FirstSeen.IsZero() || rec.LastSeen.IsZero() {
		t.Fatalf("persisted record wrong: %+v", rec)
	}

	// select a node → its agent-facing tools render
	rec = get("/settings/mcp-nodes?node=newer")
	if rec.Code != http.StatusOK {
		t.Fatalf("select status %d", rec.Code)
	}
	body = rec.Body.String()
	for _, want := range []string{"newer_notes_search", "Search Apple Notes", "newer_reminders_add"} {
		if !strings.Contains(body, want) {
			t.Errorf("selected-node page missing %q", want)
		}
	}
	if !strings.Contains(body, `aria-current="page"`) {
		t.Error("selected node row should be highlighted")
	}

	// unknown node → not-recorded state, page still 200
	rec = get("/settings/mcp-nodes?node=gone")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "neither connected nor in the registry") {
		t.Errorf("unknown node should render the not-recorded state, got %d", rec.Code)
	}

	// tools fetch failure → panel alert, list intact
	f.relayToolsErr = errTest
	rec = get("/settings/mcp-nodes?node=newer")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Couldn&#39;t load this node&#39;s tools") {
		t.Errorf("tools failure should degrade the panel, got %d", rec.Code)
	}
	f.relayToolsErr = nil

	// node disconnects: it stays in the registry and renders offline with its
	// cached tools still selectable.
	f.relaySessions = nil
	rec = get("/settings/mcp-nodes")
	if rec.Code != http.StatusOK {
		t.Fatalf("offline status %d", rec.Code)
	}
	body = rec.Body.String()
	if !strings.Contains(body, "Disconnected") || !strings.Contains(body, "last seen") {
		t.Error("disconnected nodes should render offline with a last-seen line")
	}
	if strings.Contains(body, "No external nodes connected") {
		t.Error("disconnected nodes must not blank the page")
	}
	rec = get("/settings/mcp-nodes?node=newer")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "newer_notes_search") {
		t.Error("offline node should serve its cached tool snapshot")
	}

	// live-fetch failure → error banner plus registry rows, page still 200
	f.relaySessionsErr = errTest
	rec = get("/settings/mcp-nodes")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Couldn&#39;t load live relay sessions") {
		t.Errorf("session-list failure should render the error banner, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "newer") {
		t.Error("session-list failure must still render the registry")
	}
	f.relaySessionsErr = nil

	// remove route: 303 + Location, entry deleted, unknown id tolerated
	rec = postForm(t, e, "/settings/mcp-nodes/remove", "instance_id=newer")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-nodes?updated=1" {
		t.Fatalf("remove status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	registry, err = s.loadRelayRegistry(t.Context())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if _, ok := registry["newer"]; ok {
		t.Error("removed node must be gone from the registry")
	}
	if _, ok := registry["older"]; !ok {
		t.Error("unrelated node must survive removal")
	}
	rec = postForm(t, e, "/settings/mcp-nodes/remove", "instance_id=never-seen")
	if rec.Code != http.StatusSeeOther {
		t.Errorf("removing an unknown id should be tolerated, got %d", rec.Code)
	}

	// empty registry + no live sessions → empty state
	f.settings = nil
	f.relaySessions = nil
	f.relayTools = nil
	rec = get("/settings/mcp-nodes")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No external nodes connected") {
		t.Errorf("empty sessions should render the empty state, got %d", rec.Code)
	}
}
