package main

import (
	"strings"
	"testing"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

// TestVisibilityBadgeMatchesSettings is the #889 regression: the dashboard
// badge label (visibilityLabel) and the Settings control value
// (agentVisibilityValue) MUST derive from the same normalized visibility for
// every input — valid, mixed-case, whitespace-padded, empty, and
// unknown/legacy — so the two surfaces can never disagree again.
func TestVisibilityBadgeMatchesSettings(t *testing.T) {
	cases := []struct {
		name   string
		stored string
		want   string
	}{
		{name: "project", stored: "project", want: "project"},
		{name: "external", stored: "external", want: "external"},
		{name: "internal", stored: "internal", want: "internal"},
		{name: "mixed case", stored: "External", want: "external"},
		{name: "whitespace padded", stored: "  internal  ", want: "internal"},
		{name: "empty", stored: "", want: "project"},
		{name: "unknown", stored: "public", want: "project"},
		{name: "legacy", stored: "legacy-public", want: "project"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			label := visibilityLabel(tc.stored)
			value := agentVisibilityValue(&AgentDefinition{Visibility: tc.stored})
			if label != tc.want {
				t.Errorf("visibilityLabel(%q) = %q, want %q", tc.stored, label, tc.want)
			}
			if value != tc.want {
				t.Errorf("agentVisibilityValue(%q) = %q, want %q", tc.stored, value, tc.want)
			}
			if label != value {
				t.Errorf("dashboard label %q and settings value %q disagree for stored %q", label, value, tc.stored)
			}
		})
	}
}

// TestVisibilityIntentMatchesLabel asserts the badge color intent resolves from
// the same normalized value as the label, so a project badge can never render
// with a ghost intent (the pre-fix empty/unknown mismatch where the label said
// "project" but the intent returned BadgeGhost).
func TestVisibilityIntentMatchesLabel(t *testing.T) {
	labelToIntent := map[string]ui.BadgeIntent{
		agentVisibilityProject:  ui.BadgeInfo,
		agentVisibilityExternal: ui.BadgeSuccess,
		agentVisibilityInternal: ui.BadgeNeutral,
	}
	for _, in := range []string{"project", "external", "internal", " External ", "  ", "", "public"} {
		label := visibilityLabel(in)
		want, ok := labelToIntent[label]
		if !ok {
			t.Fatalf("no intent mapping for normalized label %q", label)
		}
		if got := visibilityIntent(in); got != want {
			t.Errorf("visibilityIntent(%q) = %v, want %v (label %q)", in, got, want, label)
		}
	}
}

// TestDashboardBadgeDoesNotEchoRawValue is the display-side regression for the
// report: a stored "public" used to render verbatim on the dashboard badge. It
// must now render the same "project" the Settings control selects.
func TestDashboardBadgeDoesNotEchoRawValue(t *testing.T) {
	html := renderHTML(t, agentSummaryCard(agentDashboardData{
		Agent: &AgentDefinition{ID: "a1", Name: "diane", Visibility: "public"},
	}))
	if strings.Contains(html, ">public<") {
		t.Error(`dashboard badge must not echo the raw unknown value "public"`)
	}
	if !strings.Contains(html, ">project<") {
		t.Error(`dashboard badge must render the normalized "project" for an unknown value`)
	}
}
