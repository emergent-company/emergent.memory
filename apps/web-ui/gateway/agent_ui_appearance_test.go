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
