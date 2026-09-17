package main

import (
	"encoding/json"
	"strings"
	"testing"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

func TestAgentUIOfTolerant(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
		want agentUI
	}{
		{"nil", nil, agentUI{}},
		{"empty", json.RawMessage(""), agentUI{}},
		{"null", json.RawMessage("null"), agentUI{}},
		{"empty object", json.RawMessage("{}"), agentUI{}},
		{"both", json.RawMessage(`{"icon":"database","color":"#2563EB"}`), agentUI{Icon: "database", Color: "#2563EB"}},
		{"icon only", json.RawMessage(`{"icon":"bot"}`), agentUI{Icon: "bot"}},
		{"color only", json.RawMessage(`{"color":"#10B981"}`), agentUI{Color: "#10B981"}},
		{"non-string ignored", json.RawMessage(`{"icon":42,"color":true}`), agentUI{}},
		{"malformed", json.RawMessage(`{"icon":`), agentUI{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentUIOf(tc.raw); got != tc.want {
				t.Errorf("agentUIOf(%q) = %+v, want %+v", string(tc.raw), got, tc.want)
			}
		})
	}

	if got := agentIcon(json.RawMessage(`{"icon":"sparkles"}`)); got != "sparkles" {
		t.Errorf("agentIcon = %q, want sparkles", got)
	}
	if got := agentColor(nil); got != "" {
		t.Errorf("agentColor(nil) = %q, want empty", got)
	}
}

func TestAgentIconNameFallback(t *testing.T) {
	cases := map[string]string{
		"":                  "bot",
		"   ":               "bot",
		"DefinitelyNotReal": "bot",
		"database":          "database",
		"lucide--database":  "lucide--database",
		"📝":                 "📝",
	}
	for in, want := range cases {
		if got := agentIconName(in); got != want {
			t.Errorf("agentIconName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := agentIconifyClass(""); got != "lucide--bot" {
		t.Errorf("agentIconifyClass(\"\") = %q, want lucide--bot", got)
	}
	if got := agentIconifyClass("database"); got != "lucide--database" {
		t.Errorf("agentIconifyClass(database) = %q, want lucide--database", got)
	}
	// raw glyphs cannot be expressed as an iconify class → bot fallback
	if got := agentIconifyClass("📝"); got != "lucide--bot" {
		t.Errorf("agentIconifyClass(emoji) = %q, want lucide--bot", got)
	}
}

func TestAgentUIConfig(t *testing.T) {
	cases := []struct {
		icon, color string
		want        string
	}{
		{"", "", "{}"},
		{"database", "", `{"icon":"database"}`},
		{"", "#2563EB", `{"color":"#2563EB"}`},
		{"database", "#2563EB", `{"color":"#2563EB","icon":"database"}`},
		{" database ", " #2563EB ", `{"color":"#2563EB","icon":"database"}`},
	}
	for _, tc := range cases {
		got := string(agentUIConfig(tc.icon, tc.color))
		// json.Marshal sorts keys, so the literal comparison below is stable.
		if got != tc.want {
			t.Errorf("agentUIConfig(%q,%q) = %s, want %s", tc.icon, tc.color, got, tc.want)
		}
	}
}

func TestAgentUIForList(t *testing.T) {
	list := []AgentDefinitionSummary{
		{ID: "a1", Name: "diane", UIConfig: json.RawMessage(`{"icon":"database","color":"#2563EB"}`)},
		{ID: "a2", Name: "milo"},
	}
	if got := agentUIForList(list, "a1"); got != (agentUI{Icon: "database", Color: "#2563EB"}) {
		t.Errorf("agentUIForList(a1) = %+v", got)
	}
	if got := agentUIForList(list, "a2"); got != (agentUI{}) {
		t.Errorf("agentUIForList(a2) = %+v, want zero", got)
	}
	if got := agentUIForList(list, "missing"); got != (agentUI{}) {
		t.Errorf("agentUIForList(missing) = %+v, want zero", got)
	}
}

func TestRenderAgentIconTile(t *testing.T) {
	// No appearance: the default primary bot tile, identical to before.
	html := renderHTML(t, agentIconTile("", ""))
	for _, want := range []string{"lucide--bot", "bg-primary/10", "text-primary", "border-primary/15"} {
		if !strings.Contains(html, want) {
			t.Errorf("default agent tile missing %q", want)
		}
	}

	// Declared icon + color: the shared type tile with the agent icon and the
	// inline color accent.
	html = renderHTML(t, agentIconTile("database", "#2563EB"))
	for _, want := range []string{"lucide--database", "color:#2563EB", "color-mix(in oklch,#2563EB 10%,transparent)"} {
		if !strings.Contains(html, want) {
			t.Errorf("accented agent tile missing %q", want)
		}
	}

	// Color only (no icon) still falls back to the bot glyph, tinted.
	html = renderHTML(t, agentIconTile("", "#2563EB"))
	if !strings.Contains(html, "lucide--bot") || !strings.Contains(html, "color:#2563EB") {
		t.Errorf("color-only agent tile = %s", html)
	}
}

func TestRenderAgentNameChip(t *testing.T) {
	html := renderHTML(t, agentNameChip("diane", ui.BadgeGhost, "", ""))
	if !strings.Contains(html, "lucide--bot") || !strings.Contains(html, "badge-ghost") {
		t.Errorf("default agent chip missing bot/ghost: %s", html)
	}

	html = renderHTML(t, agentNameChip("diane", ui.BadgeGhost, "database", "#2563EB"))
	for _, want := range []string{"lucide--database", "color:#2563EB", "diane"} {
		if !strings.Contains(html, want) {
			t.Errorf("accented agent chip missing %q", want)
		}
	}
}

func TestRenderAgentInlineGlyph(t *testing.T) {
	// No appearance: a bare muted bot glyph — no tile frame, no border, no badge.
	html := renderHTML(t, agentInlineGlyph("", ""))
	for _, want := range []string{"lucide--bot", "size-4", "text-base-content/40"} {
		if !strings.Contains(html, want) {
			t.Errorf("default inline glyph missing %q: %s", want, html)
		}
	}
	for _, bad := range []string{"rounded-lg", "badge", "border", "bg-primary"} {
		if strings.Contains(html, bad) {
			t.Errorf("inline glyph must be bare (found %q): %s", bad, html)
		}
	}

	// Declared icon + color: the agent icon tinted with its color, still bare.
	html = renderHTML(t, agentInlineGlyph("database", "#2563EB"))
	for _, want := range []string{"lucide--database", "color:#2563EB"} {
		if !strings.Contains(html, want) {
			t.Errorf("accented inline glyph missing %q: %s", want, html)
		}
	}

	// Color only falls back to the tinted bot glyph.
	html = renderHTML(t, agentInlineGlyph("", "#2563EB"))
	if !strings.Contains(html, "lucide--bot") || !strings.Contains(html, "color:#2563EB") {
		t.Errorf("color-only inline glyph = %s", html)
	}
}

// TestAgentDashboardHeaderLeadingTile asserts the agent dashboard header now
// leads with the framed agent icon tile (before the h1) — matching the summary
// card row — for both a declared appearance and the neutral bot default.
func TestAgentDashboardHeaderLeadingTile(t *testing.T) {
	declared := &AgentDefinition{
		ID: "a1", Name: "diane",
		UIConfig: json.RawMessage(`{"icon":"database","color":"#2563EB"}`),
	}
	html := renderHTML(t, AgentDashboardPage(agentDashboardData{Agent: declared}))
	tile := strings.Index(html, "lucide--database")
	h1 := strings.Index(html, `lg:text-3xl">diane</h1>`)
	if tile == -1 || h1 == -1 {
		t.Fatalf("dashboard header missing tile or title: tile=%d h1=%d", tile, h1)
	}
	if tile > h1 {
		t.Errorf("agent tile (%d) must lead the title (%d)", tile, h1)
	}

	neutral := &AgentDefinition{ID: "a1", Name: "diane"}
	html = renderHTML(t, AgentDashboardPage(agentDashboardData{Agent: neutral}))
	tile = strings.Index(html, "lucide--bot")
	h1 = strings.Index(html, `lg:text-3xl">diane</h1>`)
	if tile == -1 || h1 == -1 || tile > h1 {
		t.Errorf("neutral agent tile must lead the title: tile=%d h1=%d", tile, h1)
	}
}

// TestChatRailAgentGlyphLeading asserts the session rail leads each row with the
// agent's own bare glyph (no generic message/calendar icon, no frame) when an
// appearance is declared, and keeps the "name · time" subtitle intact.
func TestChatRailAgentGlyphLeading(t *testing.T) {
	conv := Conversation{ID: "c1", Title: "Morning chat", AgentDefinitionID: "a1", UpdatedAt: "2026-08-26T09:00:00Z"}
	ap := agentAppearance{Name: "memory", Icon: "database", Color: "#2563EB"}
	html := renderHTML(t, sessionRailItem(conv, ap, false))
	for _, want := range []string{"lucide--database", "color:#2563EB", "memory ·"} {
		if !strings.Contains(html, want) {
			t.Errorf("rail row missing %q: %s", want, html)
		}
	}
	for _, bad := range []string{"lucide--messages-square", "lucide--calendar-clock"} {
		if strings.Contains(html, bad) {
			t.Errorf("rail row must lead with the agent glyph, found generic %q", bad)
		}
	}

	// No declared appearance → the neutral bare bot glyph, name still shown.
	html = renderHTML(t, sessionRailItem(conv, agentAppearance{Name: "memory"}, false))
	if !strings.Contains(html, "lucide--bot") || !strings.Contains(html, "memory ·") {
		t.Errorf("neutral rail row missing bot glyph or name: %s", html)
	}

	// Scheduled runs lead with the same agent glyph (resolved from the run row).
	run := scheduledRunRow{ID: "run-1", AgentName: "Daily briefing", Status: "completed", StartedAt: "2026-08-27T08:00:00Z", Icon: "database", Color: "#2563EB"}
	html = renderHTML(t, scheduledRailItem(run))
	if !strings.Contains(html, "lucide--database") || strings.Contains(html, "lucide--calendar-clock") {
		t.Errorf("scheduled rail row must lead with the agent glyph: %s", html)
	}
}
