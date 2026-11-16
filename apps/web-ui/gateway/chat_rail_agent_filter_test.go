package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestChatAgentFilterMenuRendersAvatarRows pins the rail's agent filter as the
// menu variant of the objects page's actor picker: one menuitemradio row per
// agent definition, each led by that agent's avatar (its declared glyph, or the
// bot fallback), with "All agents" first and checked. The trigger shows the
// active pick so the closed control is readable without opening the menu.
func TestChatAgentFilterMenuRendersAvatarRows(t *testing.T) {
	agents := []AgentDefinitionSummary{
		{ID: "ad-coding", Name: "coding", UIConfig: json.RawMessage(`{"icon":"database","color":"#2563EB"}`)},
		{ID: "ad-review", Name: "review"},
	}
	html := renderHTML(t, chatAgentFilter(agents))

	for _, want := range []string{
		`data-gd-menu-trigger`,
		`aria-haspopup="menu"`,
		`aria-label="Filter sessions by agent: All agents"`,
		`id="chat-agent-filter-glyph"`,
		`id="chat-agent-filter-label"`,
		`>All agents<`,
		`data-agent-avatar="all"`,
		`data-agent-avatar="agent"`,
		`data-agent-id=""`,
		`data-agent-label="All agents"`,
		`data-agent-id="ad-coding"`,
		`data-agent-label="coding"`,
		`data-agent-id="ad-review"`,
		`data-agent-label="review"`,
		// The agent's declared appearance reaches the row avatar: the glyph as
		// an iconify class and the colour as the glyph's text tint.
		`lucide--database`,
		`style="color:#2563EB`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("agent filter menu missing %q\n%s", want, html)
		}
	}

	// One row per option ("All agents" + 2 agents). Every row is a radio item
	// and a roving-focus item (go-daisy-a11y's arrow-key contract), and every
	// row carries a check mark chat.js toggles — otherwise the closed menu could
	// not move its check without rewriting markup.
	if got, want := strings.Count(html, `role="menuitemradio"`), 3; got != want {
		t.Errorf("menuitemradio rows = %d, want %d", got, want)
	}
	if got, want := strings.Count(html, `data-gd-menu-item`), 3; got != want {
		t.Errorf("roving-focus items = %d, want %d", got, want)
	}
	if got, want := strings.Count(html, `data-agent-check="true"`), 3; got != want {
		t.Errorf("row check marks = %d, want %d", got, want)
	}

	// The server renders the initial state: exactly "All agents" is checked.
	if got := strings.Count(html, `aria-checked="true"`); got != 1 {
		t.Errorf("checked rows = %d, want 1 (All agents)\n%s", got, html)
	}
	allRow := agentFilterRow(html, "")
	if !strings.Contains(allRow, `aria-checked="true"`) {
		t.Errorf("All agents row must start checked\n%s", allRow)
	}
	if !strings.Contains(allRow, `menu-active`) {
		t.Errorf("All agents row must start active\n%s", allRow)
	}
	for _, id := range []string{"ad-coding", "ad-review"} {
		row := agentFilterRow(html, id)
		if strings.Contains(row, `aria-checked="true"`) || strings.Contains(row, `menu-active`) {
			t.Errorf("row %q must start unchecked\n%s", id, row)
		}
	}
}

// agentFilterRow returns the markup of the filter row for one agent id ("" is
// "All agents"), from its opening <li> to the matching closer. The rendered menu
// is small and flat, so a slice from the row's data-agent-id to the next row's
// <li role="none"> is enough to assert on one row without an HTML parser.
func agentFilterRow(html, id string) string {
	marker := `data-agent-id="` + id + `"`
	i := strings.Index(html, marker)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(html[:i], `<li`)
	if start < 0 {
		start = i
	}
	rest := html[i:]
	if next := strings.Index(rest, `<li role="none">`); next > 0 {
		return html[start : i+next]
	}
	return html[start:]
}

// TestChatRailAttributesBoardRunsToTheirAgentDefinition drives the real rail
// route with an object-driven (board) run: the run names the agent definition it
// executed under while its AgentID is the runtime agent (a kb.agents row, an
// independent id space). The row must carry the definition id in data-agent —
// the id space the rail's filter and the conversation rows use — and take its
// glyph/colour from the definition's declared appearance, not the runtime id.
// This is the reported bug: filtering by an agent matched nothing because no
// board row carried an agent id, and every board row fell back to the bot glyph.
func TestChatRailAttributesBoardRunsToTheirAgentDefinition(t *testing.T) {
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{{
			ID:       "ad-coding",
			Name:     "coding",
			UIConfig: json.RawMessage(`{"icon":"database","color":"#2563EB"}`),
		}},
		boardRuns: []BoardRun{{
			ID:                "run-board-1",
			AgentID:           "runtime-coding",
			AgentName:         "coding",
			AgentDefinitionID: "ad-coding",
			Status:            "completed",
			StartedAt:         "2026-11-03T10:00:00Z",
		}},
	}
	_, e := newChatRailEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-rail", nil))
	body := rec.Body.String()

	if !strings.Contains(body, `data-agent="ad-coding"`) {
		t.Fatalf("board row must carry its agent-definition id in data-agent\n%s", body)
	}
	if strings.Contains(body, `data-agent="runtime-coding"`) {
		t.Errorf("data-agent must be the definition id, not the runtime agent id\n%s", body)
	}
	if !strings.Contains(body, `lucide--database`) {
		t.Errorf("board row must wear its definition's declared glyph\n%s", body)
	}
}

// TestChatWorkRunsResolveAppearanceByDefinitionID pins the producer side of the
// bug: appearances is keyed by agent-definition id, so a board run must resolve
// its glyph through AgentDefinitionID. Looking it up by AgentID (the runtime
// agent) never matches and silently painted every board row with the bot glyph.
func TestChatWorkRunsResolveAppearanceByDefinitionID(t *testing.T) {
	f := &fakeMemory{boardRuns: []BoardRun{
		{
			ID: "run-attributed", AgentID: "runtime-coding", AgentName: "coding",
			AgentDefinitionID: "ad-coding", Status: "completed", StartedAt: "2026-11-03T10:00:00Z",
		},
		{
			ID: "run-unbound", AgentID: "runtime-legacy", AgentName: "legacy chat",
			Status: "completed", StartedAt: "2026-11-03T09:00:00Z",
		},
	}}
	s := &Server{cfg: Config{}, memory: f}
	appearances := map[string]agentAppearance{
		"ad-coding": {Name: "coding", Icon: "database", Color: "#2563EB"},
	}

	rows := s.chatWorkRuns(context.Background(), appearances)
	byID := make(map[string]workRunRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}

	attributed, ok := byID["run-attributed"]
	if !ok {
		t.Fatalf("missing attributed row, got %+v", rows)
	}
	if attributed.AgentDefinitionID != "ad-coding" {
		t.Errorf("AgentDefinitionID = %q, want ad-coding", attributed.AgentDefinitionID)
	}
	if attributed.Icon != "database" || attributed.Color != "#2563EB" {
		t.Errorf("appearance = %q/%q, want database/#2563EB (resolved by definition id)",
			attributed.Icon, attributed.Color)
	}

	// A run through a runtime agent that binds no definition stays unattributed:
	// it is reachable under "All agents" and nowhere else.
	unbound, ok := byID["run-unbound"]
	if !ok {
		t.Fatalf("missing unbound row, got %+v", rows)
	}
	if unbound.AgentDefinitionID != "" || unbound.Icon != "" || unbound.Color != "" {
		t.Errorf("unbound row = %+v, want no agent id and no appearance", unbound)
	}
}

// TestChatScheduledRunsCarryAgentDefinitionID covers the scheduled producer:
// the row must expose the definition its runtime agent binds, so the agent
// filter can isolate scheduled runs like conversations.
func TestChatScheduledRunsCarryAgentDefinitionID(t *testing.T) {
	defID := "ad-sched"
	f := &fakeMemory{
		scheduledAgents: []ScheduledAgent{
			{ID: "a1", Name: "morning brief", TriggerType: "schedule", AgentDefinitionID: &defID},
			{ID: "a2", Name: "legacy chat", TriggerType: "schedule"},
		},
		scheduledRuns: map[string][]ScheduledAgentRun{
			"a1": {{ID: "sr1", AgentID: "a1", Status: "completed", StartedAt: "2026-11-03T10:00:00Z"}},
			"a2": {{ID: "sr2", AgentID: "a2", Status: "completed", StartedAt: "2026-11-03T09:00:00Z"}},
		},
	}
	s := &Server{cfg: Config{}, memory: f}
	appearances := map[string]agentAppearance{
		"ad-sched": {Name: "morning brief", Icon: "sun", Color: "#F59E0B"},
	}

	rows := s.chatScheduledRuns(context.Background(), appearances)
	byID := make(map[string]scheduledRunRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}

	bound, ok := byID["sr1"]
	if !ok {
		t.Fatalf("missing scheduled row, got %+v", rows)
	}
	if bound.AgentDefinitionID != defID {
		t.Errorf("AgentDefinitionID = %q, want %q", bound.AgentDefinitionID, defID)
	}
	if bound.Icon != "sun" || bound.Color != "#F59E0B" {
		t.Errorf("appearance = %q/%q, want sun/#F59E0B", bound.Icon, bound.Color)
	}

	unbound, ok := byID["sr2"]
	if !ok {
		t.Fatalf("missing unbound scheduled row, got %+v", rows)
	}
	if unbound.AgentDefinitionID != "" {
		t.Errorf("unbound scheduled row AgentDefinitionID = %q, want empty", unbound.AgentDefinitionID)
	}
}
