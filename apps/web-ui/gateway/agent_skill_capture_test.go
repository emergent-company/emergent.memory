package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// skillCaptureSectionContext builds a POST context for the Skills subpage with
// the form already parsed, because applyAgentSkillsSection reads
// c.Request().Form directly (the real handler parses it first).
func skillCaptureSectionContext(t *testing.T, form url.Values) echo.Context {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/agents/a1/settings/skills", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	if err := req.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}
	return echo.New().NewContext(req, httptest.NewRecorder())
}

func modePtr(v string) *string { return &v }

func boolPtrValue(v bool) *bool { return &v }

// TestApplyAgentSkillsSectionCaptureModes pins the radio mapping: "" inherits
// (mode cleared, {} sent), off/suggest narrow, and auto is refused because
// memory would resolve it to the project value anyway.
func TestApplyAgentSkillsSectionCaptureModes(t *testing.T) {
	tests := []struct {
		name        string
		existing    *AgentSkillCapture
		form        url.Values
		wantMode    string // "" means the override mode must be cleared
		wantErr     bool
		wantAutoApp *bool
	}{
		{
			name:        "inherit clears a stored override",
			existing:    &AgentSkillCapture{Mode: modePtr("off")},
			form:        url.Values{},
			wantMode:    "",
			wantAutoApp: nil,
		},
		{
			name:        "inherit preserves API-set autoApply",
			existing:    &AgentSkillCapture{Mode: modePtr("suggest"), AutoApply: boolPtrValue(false)},
			form:        url.Values{},
			wantMode:    "",
			wantAutoApp: boolPtrValue(false),
		},
		{
			name:     "inherit on a nil override yields an empty object",
			existing: nil,
			form:     url.Values{},
			wantMode: "",
		},
		{
			name:     "suggest narrows",
			existing: nil,
			form:     url.Values{"skillCaptureMode": {"suggest"}},
			wantMode: "suggest",
		},
		{
			name:     "off disables",
			existing: &AgentSkillCapture{Mode: modePtr("suggest")},
			form:     url.Values{"skillCaptureMode": {"off"}},
			wantMode: "off",
		},
		{
			name:     "auto is refused as an override",
			existing: nil,
			form:     url.Values{"skillCaptureMode": {"auto"}},
			wantErr:  true,
		},
		{
			name:     "unknown value is refused",
			existing: nil,
			form:     url.Values{"skillCaptureMode": {"sometimes"}},
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def := &AgentDefinition{ID: "a1", SkillCapture: tc.existing}
			err := applyAgentSkillsSection(def, skillCaptureSectionContext(t, tc.form))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("applyAgentSkillsSection: %v", err)
			}
			if def.SkillCapture == nil {
				t.Fatal("SkillCapture must be non-nil so an inherited override is sent as {}")
			}
			switch {
			case tc.wantMode == "" && def.SkillCapture.Mode != nil:
				t.Fatalf("mode = %q, want cleared", *def.SkillCapture.Mode)
			case tc.wantMode != "" && (def.SkillCapture.Mode == nil || *def.SkillCapture.Mode != tc.wantMode):
				t.Fatalf("mode = %v, want %q", def.SkillCapture.Mode, tc.wantMode)
			}
			switch {
			case tc.wantAutoApp == nil && def.SkillCapture.AutoApply != nil:
				t.Fatalf("autoApply = %v, want nil", *def.SkillCapture.AutoApply)
			case tc.wantAutoApp != nil && (def.SkillCapture.AutoApply == nil || *def.SkillCapture.AutoApply != *tc.wantAutoApp):
				t.Fatalf("autoApply = %v, want %v", def.SkillCapture.AutoApply, *tc.wantAutoApp)
			}
		})
	}
}

// TestApplyAgentSkillsSectionKeepsSkillPicker pins that adding the capture
// radio did not change the existing skill checkbox mapping.
func TestApplyAgentSkillsSectionKeepsSkillPicker(t *testing.T) {
	def := &AgentDefinition{ID: "a1"}
	form := url.Values{"skill": {"alpha", "beta"}}
	if err := applyAgentSkillsSection(def, skillCaptureSectionContext(t, form)); err != nil {
		t.Fatalf("applyAgentSkillsSection: %v", err)
	}
	if got := strings.Join(def.Skills, ","); got != "alpha,beta" {
		t.Fatalf("skills = %q, want alpha,beta", got)
	}
}

// TestResolveSkillCaptureMode pins the gateway mirror of memory's
// ResolveCaptureMode: the project is the master switch and an agent override
// only narrows.
func TestResolveSkillCaptureMode(t *testing.T) {
	tests := []struct {
		project  string
		override string // "" means no override
		want     string
	}{
		{"off", "", "off"},
		{"off", "suggest", "off"},
		{"suggest", "", "suggest"},
		{"suggest", "suggest", "suggest"},
		{"suggest", "off", "off"},
		{"auto", "", "auto"},
		{"auto", "suggest", "suggest"},
		{"auto", "off", "off"},
		{"auto", "auto", "auto"}, // never widens, resolves to the project value
	}
	for _, tc := range tests {
		agent := &AgentDefinition{}
		if tc.override != "" {
			agent.SkillCapture = &AgentSkillCapture{Mode: modePtr(tc.override)}
		}
		if got := resolveSkillCaptureMode(tc.project, agent); got != tc.want {
			t.Errorf("resolveSkillCaptureMode(%q, override %q) = %q, want %q", tc.project, tc.override, got, tc.want)
		}
	}
	if got := resolveSkillCaptureMode("auto", nil); got != "auto" {
		t.Errorf("nil agent = %q, want auto", got)
	}
}

// inputIsChecked reports whether the <input> carrying id renders the checked
// attribute. templ emits attributes in source order, so the tag is read from the
// id to the next '>'.
func inputIsChecked(html, id string) bool {
	i := strings.Index(html, `id="`+id+`"`)
	if i < 0 {
		return false
	}
	end := strings.Index(html[i:], ">")
	if end < 0 {
		return false
	}
	return strings.Contains(html[i:i+end], "checked")
}

// TestAgentSkillsFormRendersCaptureControl pins the user-visible control: the
// three offered modes, the stored selection, and the effective-mode hint.
func TestAgentSkillsFormRendersCaptureControl(t *testing.T) {
	html := renderHTML(t, agentSkillsSettingsForm(agentSettingsData{
		Section:              sectionSkills,
		Agent:                &AgentDefinition{ID: "a1", Name: "diane", SkillCapture: &AgentSkillCapture{Mode: modePtr("suggest")}},
		CaptureKnown:         true,
		ProjectCaptureMode:   "auto",
		EffectiveCaptureMode: "suggest",
	}))
	for _, want := range []string{
		`name="skillCaptureMode"`,
		`value=""`,
		`value="suggest"`,
		`value="off"`,
		"Inherit project setting",
		"effective: Suggest",
		"Automatic skill capture",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("skills form missing %q", want)
		}
	}
	if !inputIsChecked(html, "agent-settings-capture-suggest") {
		t.Error("the stored suggest override must render checked")
	}
	if inputIsChecked(html, "agent-settings-capture-inherit") {
		t.Error("inherit must not render checked when an override is stored")
	}
	if inputIsChecked(html, "agent-settings-capture-off") {
		t.Error("off must not render checked when a suggest override is stored")
	}
	if strings.Contains(html, `value="auto"`) {
		t.Error("skills form must not offer auto as an override")
	}
}

// TestAgentSkillsFormWarnsWhenProjectCaptureOff pins the inert-control notice
// (and its link to the project setting) when the project master switch is off.
func TestAgentSkillsFormWarnsWhenProjectCaptureOff(t *testing.T) {
	html := renderHTML(t, agentSkillsSettingsForm(agentSettingsData{
		Section:              sectionSkills,
		Agent:                &AgentDefinition{ID: "a1", Name: "diane"},
		CaptureKnown:         true,
		ProjectCaptureMode:   "off",
		EffectiveCaptureMode: "off",
	}))
	if !strings.Contains(html, "Skill capture is off for this project") {
		t.Error("expected the project-off notice")
	}
	if !strings.Contains(html, `href="/settings"`) {
		t.Error("expected a link to the project settings")
	}
}

// TestAgentSkillsFormOmitsHintWhenProjectUnknown pins that a failed project
// fetch does not render a misleading effective-mode label.
func TestAgentSkillsFormOmitsHintWhenProjectUnknown(t *testing.T) {
	html := renderHTML(t, agentSkillsSettingsForm(agentSettingsData{
		Section: sectionSkills,
		Agent:   &AgentDefinition{ID: "a1", Name: "diane"},
	}))
	if strings.Contains(html, "effective:") {
		t.Error("expected no effective-mode hint when the project mode is unknown")
	}
	if strings.Contains(html, "Skill capture is off for this project") {
		t.Error("expected no project-off notice when the project mode is unknown")
	}
}

// TestCaptureOverrideValue pins the rendering of a stored override, including
// an API-set "auto" that resolves to the project value and renders as inherit.
func TestCaptureOverrideValue(t *testing.T) {
	tests := []struct {
		override *AgentSkillCapture
		want     string
	}{
		{nil, ""},
		{&AgentSkillCapture{}, ""},
		{&AgentSkillCapture{Mode: modePtr("auto")}, ""},
		{&AgentSkillCapture{Mode: modePtr("off")}, "off"},
		{&AgentSkillCapture{Mode: modePtr("suggest")}, "suggest"},
	}
	for _, tc := range tests {
		if got := captureOverrideValue(&AgentDefinition{SkillCapture: tc.override}); got != tc.want {
			t.Errorf("captureOverrideValue(%+v) = %q, want %q", tc.override, got, tc.want)
		}
	}
	if got := captureOverrideValue(nil); got != "" {
		t.Errorf("nil agent = %q, want \"\"", got)
	}
}

// TestCapturePolicyWithPreservesUnrenderedKeys pins the round-trip that keeps
// API-set guardrail knobs from being wiped by a mode toggle: memory replaces
// the whole policy on PATCH.
func TestCapturePolicyWithPreservesUnrenderedKeys(t *testing.T) {
	current := map[string]any{
		"mode":           "suggest",
		"nudgeInterval":  float64(5),
		"dedupThreshold": 0.9,
		"autoApply":      true,
	}
	got := capturePolicyWith(current, "mode", "auto")
	if got["mode"] != "auto" {
		t.Errorf("mode = %v, want auto", got["mode"])
	}
	for _, k := range []string{"nudgeInterval", "dedupThreshold", "autoApply"} {
		if got[k] != current[k] {
			t.Errorf("%s = %v, want %v (must be carried through)", k, got[k], current[k])
		}
	}
	if current["mode"] != "suggest" {
		t.Errorf("capturePolicyWith mutated its input: mode = %v", current["mode"])
	}
	if nilPolicy := capturePolicyWith(nil, "mode", "off"); nilPolicy["mode"] != "off" || len(nilPolicy) != 1 {
		t.Errorf("nil current = %v, want {mode: off}", nilPolicy)
	}
}

// TestSkillCapturePanelRendersModeAndAutoApply pins the project panel: the three
// modes, the stored selection, and that the auto-apply toggle only appears for
// "auto" (it is meaningless otherwise).
func TestSkillCapturePanelRendersModeAndAutoApply(t *testing.T) {
	auto := renderHTML(t, skillCapturePanel(&Project{SkillCapture: map[string]any{"mode": "auto", "autoApply": true}}))
	for _, want := range []string{
		`<option value="auto" selected`,
		`<option value="suggest"`,
		`<option value="off"`,
		`id="autoApply"`,
		`hx-post="/settings/project/skill_capture_auto_apply"`,
	} {
		if !strings.Contains(auto, want) {
			t.Errorf("auto panel missing %q", want)
		}
	}

	off := renderHTML(t, skillCapturePanel(&Project{SkillCapture: map[string]any{"mode": "off"}}))
	if !strings.Contains(off, `<option value="off" selected`) {
		t.Error("off panel must select the off option")
	}
	if strings.Contains(off, `id="autoApply"`) {
		t.Error("auto-apply toggle must not render unless the mode is auto")
	}
	if !strings.Contains(off, "Each agent can narrow this policy") {
		t.Error("expected the per-agent pointer note")
	}
}

// TestProjectSkillCaptureMode pins the read helpers used by the project panel.
func TestProjectSkillCaptureMode(t *testing.T) {
	tests := []struct {
		policy map[string]any
		mode   string
		auto   bool
	}{
		{nil, "off", false},
		{map[string]any{}, "off", false},
		{map[string]any{"mode": "SUGGEST"}, "suggest", false},
		{map[string]any{"mode": "auto", "autoApply": true}, "auto", true},
		{map[string]any{"mode": "off", "autoApply": true}, "off", true},
		{map[string]any{"mode": "nonsense"}, "off", false},
	}
	for _, tc := range tests {
		p := &Project{SkillCapture: tc.policy}
		if got := p.SkillCaptureMode(); got != tc.mode {
			t.Errorf("mode(%v) = %q, want %q", tc.policy, got, tc.mode)
		}
		if got := p.SkillCaptureAutoApply(); got != tc.auto {
			t.Errorf("autoApply(%v) = %v, want %v", tc.policy, got, tc.auto)
		}
	}
	if got := (*Project)(nil).SkillCaptureMode(); got != "off" {
		t.Errorf("nil project mode = %q, want off", got)
	}
}
