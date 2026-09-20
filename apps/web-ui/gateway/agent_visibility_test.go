package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// renderGeneralForm renders just the General settings form for an agent, so
// assertions about the Visibility control can't be satisfied by another page.
func renderGeneralForm(t *testing.T, agent *AgentDefinition) string {
	t.Helper()
	return renderHTML(t, agentGeneralSettingsForm(agentSettingsData{Agent: agent}))
}

// visibilitySelectBlock isolates the rendered Visibility <select> so a
// per-option selected-state assertion cannot be satisfied by another select.
func visibilitySelectBlock(t *testing.T, html string) string {
	t.Helper()
	i := strings.Index(html, `name="visibility"`)
	if i < 0 {
		t.Fatal("visibility select not found")
	}
	block := html[i:]
	if j := strings.Index(block, "</select>"); j >= 0 {
		return block[:j]
	}
	return block
}

// visibilitySelected reports whether the Visibility select preselects value.
func visibilitySelected(t *testing.T, html, value string) bool {
	t.Helper()
	return strings.Contains(visibilitySelectBlock(t, html), `value="`+value+`" selected`)
}

// TestRenderAgentGeneralVisibilityDropdown covers the dropdown itself: the
// three options with their exact stored values and the "Name — description"
// labels, sitting in its own Visibility section after Appearance.
func TestRenderAgentGeneralVisibilityDropdown(t *testing.T) {
	html := renderGeneralForm(t, &AgentDefinition{ID: "a1", Name: "diane"})

	if !strings.Contains(html, `id="agent-settings-visibility" name="visibility"`) {
		t.Error("general form missing the visibility select")
	}
	block := visibilitySelectBlock(t, html)
	for _, want := range []string{
		`value="project"`, `value="external"`, `value="internal"`,
		"Project — Visible in this project&#39;s UI and chat.",
		"External — Advertised in the project&#39;s A2A agent card",
		"Internal — Hidden from the agents list.",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("visibility dropdown missing %q", want)
		}
	}
	// exactly the three valid values, in project/external/internal order
	if got := strings.Count(block, "<option"); got != 3 {
		t.Errorf("visibility options = %d, want 3", got)
	}
	// its own section, carrying the section description
	if !strings.Contains(html, ">Visibility</h2>") {
		t.Error("visibility section heading missing")
	}
	if !strings.Contains(html, "Controls who can discover this agent. Changes take effect after you save.") {
		t.Error("visibility section description missing")
	}
	// appears after Appearance's controls (icon/color pickers)
	if strings.Index(html, `id="agent-settings-visibility"`) < strings.Index(html, `id="agent-settings-color"`) {
		t.Error("visibility section must render after the appearance section")
	}
}

// TestRenderAgentGeneralVisibilityPreselection covers the stored value driving
// the selected option, including the empty/unknown graceful fallback to project.
func TestRenderAgentGeneralVisibilityPreselection(t *testing.T) {
	cases := []struct {
		name        string
		stored      string
		want        string
		notSelected []string
	}{
		{name: "project", stored: "project", want: "project", notSelected: []string{"external", "internal"}},
		{name: "external", stored: "external", want: "external", notSelected: []string{"project", "internal"}},
		{name: "internal", stored: "internal", want: "internal", notSelected: []string{"project", "external"}},
		{name: "empty defaults to project", stored: "", want: "project", notSelected: []string{"external", "internal"}},
		{name: "unknown defaults to project", stored: "mystery", want: "project", notSelected: []string{"external", "internal"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := renderGeneralForm(t, &AgentDefinition{ID: "a1", Name: "diane", Visibility: tc.stored})
			if !visibilitySelected(t, html, tc.want) {
				t.Errorf("stored %q should preselect %q", tc.stored, tc.want)
			}
			for _, v := range tc.notSelected {
				if visibilitySelected(t, html, v) {
					t.Errorf("stored %q must not preselect %q", tc.stored, v)
				}
			}
		})
	}
}

// TestRenderAgentGeneralVisibilityAlerts covers the conditional warnings: the
// warning only for external, the info note only for internal, and neither for
// project (or an unset/unknown value).
func TestRenderAgentGeneralVisibilityAlerts(t *testing.T) {
	external := renderGeneralForm(t, &AgentDefinition{ID: "a1", Name: "diane", Visibility: "external"})
	if !strings.Contains(external, "External agents are listed in your project&#39;s A2A agent card.") ||
		!strings.Contains(external, "agents:read") || !strings.Contains(external, "agents:write") {
		t.Error("external visibility must render the A2A warning")
	}
	if !strings.Contains(external, "alert-warning") {
		t.Error("external visibility warning must use the warning tone")
	}
	if strings.Contains(external, "Internal agents don&#39;t appear in the agents list.") {
		t.Error("external visibility must not render the internal note")
	}

	internal := renderGeneralForm(t, &AgentDefinition{ID: "a1", Name: "diane", Visibility: "internal"})
	if !strings.Contains(internal, "Internal agents don&#39;t appear in the agents list.") {
		t.Error("internal visibility must render the info note")
	}
	if !strings.Contains(internal, "alert-info") {
		t.Error("internal visibility note must use the info tone")
	}
	if strings.Contains(internal, "External agents are listed in your project&#39;s A2A agent card.") {
		t.Error("internal visibility must not render the external warning")
	}

	for _, stored := range []string{"project", "", "mystery"} {
		html := renderGeneralForm(t, &AgentDefinition{ID: "a1", Name: "diane", Visibility: stored})
		if strings.Contains(html, "External agents are listed in your project&#39;s A2A agent card.") {
			t.Errorf("stored %q must not render the external warning", stored)
		}
		if strings.Contains(html, "Internal agents don&#39;t appear in the agents list.") {
			t.Errorf("stored %q must not render the internal note", stored)
		}
	}
}

// TestAgentVisibilityNormalize covers the applier's validation rule: only the
// three valid levels pass, empty/missing defaults to project, and anything else
// is rejected.
func TestAgentVisibilityNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "project", want: "project", ok: true},
		{in: "external", want: "external", ok: true},
		{in: "internal", want: "internal", ok: true},
		{in: "  External ", want: "external", ok: true},
		{in: "", want: "project", ok: true},
		{in: "   ", want: "project", ok: true},
		{in: "public", want: "", ok: false},
		{in: "bogus", want: "", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := agentVisibilityNormalize(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Errorf("agentVisibilityNormalize(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestAgentVisibilityDescription covers the helper line: a stored value maps to
// its option's description, and empty/unknown falls back to the project copy.
func TestAgentVisibilityDescription(t *testing.T) {
	if got := agentVisibilityDescription("external"); !strings.Contains(got, "Advertised in the project's A2A agent card") {
		t.Errorf("external description wrong: %q", got)
	}
	if got := agentVisibilityDescription("internal"); !strings.Contains(got, "Hidden from the agents list") {
		t.Errorf("internal description wrong: %q", got)
	}
	project := agentVisibilityDescription("project")
	if !strings.Contains(project, "Visible in this project's UI and chat") {
		t.Errorf("project description wrong: %q", project)
	}
	if got := agentVisibilityDescription(""); got != project {
		t.Errorf("empty should fall back to the project description, got %q", got)
	}
	if got := agentVisibilityDescription("mystery"); got != project {
		t.Errorf("unknown should fall back to the project description, got %q", got)
	}
}

// TestUIAgentGeneralVisibilityRoundTrip covers the General-settings POST: a
// valid value persists, an empty/missing value normalizes to project, and an
// invalid value is rejected without forwarding anything to the backend.
func TestUIAgentGeneralVisibilityRoundTrip(t *testing.T) {
	newServer := func(f *fakeMemory) *echo.Echo {
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.POST("/agents/:id/settings/general", s.uiAgentUpdateGeneral)
		return e
	}
	post := func(e *echo.Echo, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agents/a1/settings/general", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("valid value persists and round-trips", func(t *testing.T) {
		f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane", Visibility: "project"}}}
		e := newServer(f)
		rec := post(e, "name=diane&systemPrompt=be+terse&visibility=external")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/agents/a1/settings?updated=1" {
			t.Fatalf("redirect = %d %q", rec.Code, rec.Header().Get("Location"))
		}
		if u := f.updatedAgent; u == nil || u.Visibility != "external" {
			t.Fatalf("visibility not persisted, got %+v", f.updatedAgent)
		}
		// The stored definition now renders with external preselected.
		html := renderGeneralForm(t, f.updatedAgent)
		if !visibilitySelected(t, html, "external") {
			t.Error("saved external visibility should round-trip into the form")
		}
	})

	t.Run("empty value normalizes to project", func(t *testing.T) {
		f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane", Visibility: "external"}}}
		e := newServer(f)
		// No visibility field at all (e.g. a legacy form) → server default.
		rec := post(e, "name=diane&systemPrompt=be+terse")
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status %d, want 303", rec.Code)
		}
		if u := f.updatedAgent; u == nil || u.Visibility != "project" {
			t.Fatalf("empty visibility should normalize to project, got %+v", f.updatedAgent)
		}
	})

	t.Run("invalid value is rejected", func(t *testing.T) {
		f := &fakeMemory{defs: map[string]*AgentDefinition{"a1": {ID: "a1", Name: "diane", Visibility: "project"}}}
		e := newServer(f)
		rec := post(e, "name=diane&systemPrompt=x&visibility=bogus")
		want := "/agents/a1/settings?err=" + url.QueryEscape("visibility must be one of project, external, internal")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Fatalf("invalid visibility redirect = %d %q, want %q", rec.Code, rec.Header().Get("Location"), want)
		}
		if f.updatedAgent != nil {
			t.Errorf("invalid visibility must not reach the backend: %+v", f.updatedAgent)
		}
	})
}
