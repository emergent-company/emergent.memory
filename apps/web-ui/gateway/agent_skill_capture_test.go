package main

import (
	"encoding/json"
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

// inputTagByID returns the <input> tag carrying id, or "" when absent. templ
// emits attributes in source order, so the tag is read from the id to '>'.
func inputTagByID(html, id string) string {
	i := strings.Index(html, `id="`+id+`"`)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(html[:i], "<")
	end := strings.Index(html[i:], ">")
	if start < 0 || end < 0 {
		return ""
	}
	return html[start : i+end+1]
}

// inputIsChecked reports whether the <input> carrying id renders the checked
// attribute.
func inputIsChecked(html, id string) bool {
	return strings.Contains(inputTagByID(html, id), "checked")
}

// TestApplyAgentSkillsSectionCaptureWireShape pins the serialized override:
// inherit must be `{}` (an omitted key means "leave unchanged" on memory's
// PATCH, which would make the control unable to clear a stored override), and a
// narrowed override must carry only the mode.
func TestApplyAgentSkillsSectionCaptureWireShape(t *testing.T) {
	tests := []struct {
		name string
		form url.Values
		want string
	}{
		{"inherit", url.Values{}, `"skillCapture":{}`},
		{"suggest", url.Values{"skillCaptureMode": {"suggest"}}, `"skillCapture":{"mode":"suggest"}`},
		{"off", url.Values{"skillCaptureMode": {"off"}}, `"skillCapture":{"mode":"off"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def := &AgentDefinition{ID: "a1"}
			if err := applyAgentSkillsSection(def, skillCaptureSectionContext(t, tc.form)); err != nil {
				t.Fatalf("applyAgentSkillsSection: %v", err)
			}
			body, err := json.Marshal(def)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(body), tc.want) {
				t.Errorf("body %s missing %s", body, tc.want)
			}
		})
	}
}

// TestUIAgentUpdateSkillsWritesCaptureOverride exercises the real handler path
// (POST /agents/:id/settings/skills): the PRG redirect, the skill picker, the
// override, and that an API-set autoApply rides along.
func TestUIAgentUpdateSkillsWritesCaptureOverride(t *testing.T) {
	f := &fakeMemory{defs: map[string]*AgentDefinition{
		"a1": {
			ID:           "a1",
			Name:         "diane",
			Skills:       []string{},
			SkillCapture: &AgentSkillCapture{Mode: modePtr("suggest"), AutoApply: boolPtrValue(true)},
		},
	}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/agents/:id/settings/skills", s.uiAgentUpdateSkills)

	rec := voicePost(e, "/agents/a1/settings/skills", "skill=alpha&skillCaptureMode=off")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303 (PRG)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/agents/a1/settings/skills?updated=1") {
		t.Errorf("Location = %q, want the updated redirect", loc)
	}
	got := f.updatedAgent
	if got == nil {
		t.Fatal("UpdateAgentDefinition was not called")
	}
	if got.SkillCapture == nil || got.SkillCapture.Mode == nil || *got.SkillCapture.Mode != "off" {
		t.Errorf("skillCapture = %+v, want mode off", got.SkillCapture)
	}
	if got.SkillCapture.AutoApply == nil || !*got.SkillCapture.AutoApply {
		t.Error("an API-set autoApply must be preserved by an unrelated save")
	}
	if strings.Join(got.Skills, ",") != "alpha" {
		t.Errorf("skills = %v, want [alpha]", got.Skills)
	}
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

// TestAgentSkillsFormWarnsWhenCaptureOnWithoutAutoLoad pins the honesty gap:
// with capture effectively on but auto-load off, AfterRun skips the agent, so
// the form must say so instead of only showing the effective mode.
func TestAgentSkillsFormWarnsWhenCaptureOnWithoutAutoLoad(t *testing.T) {
	html := renderHTML(t, agentSkillsSettingsForm(agentSettingsData{
		Section:              sectionSkills,
		Agent:                &AgentDefinition{ID: "a1", Name: "diane", AutoLoadSkills: false},
		CaptureKnown:         true,
		ProjectCaptureMode:   "auto",
		EffectiveCaptureMode: "auto",
	}))
	if !strings.Contains(html, "does not load its own captured skills") {
		t.Error("expected the capture-will-be-skipped warning")
	}
	if !strings.Contains(html, "Load this agent&#39;s own captured skills on each run") &&
		!strings.Contains(html, "Load this agent's own captured skills on each run") {
		t.Error("expected the auto-load toggle")
	}
	if !strings.Contains(html, `name="autoLoadSkills"`) {
		t.Error("toggle must submit autoLoadSkills")
	}
	if inputIsChecked(html, "agent-settings-autoload-skills") {
		t.Error("auto-load toggle must render unchecked when the agent does not auto-load")
	}

	loaded := renderHTML(t, agentSkillsSettingsForm(agentSettingsData{
		Section:              sectionSkills,
		Agent:                &AgentDefinition{ID: "a1", Name: "diane", AutoLoadSkills: true},
		CaptureKnown:         true,
		ProjectCaptureMode:   "auto",
		EffectiveCaptureMode: "auto",
	}))
	if strings.Contains(loaded, "does not load its own captured skills") {
		t.Error("no warning when the agent does auto-load prefix skills")
	}
	if !inputIsChecked(loaded, "agent-settings-autoload-skills") {
		t.Error("auto-load toggle must render checked when the agent auto-loads")
	}

	off := renderHTML(t, agentSkillsSettingsForm(agentSettingsData{
		Section:              sectionSkills,
		Agent:                &AgentDefinition{ID: "a1", Name: "diane"},
		CaptureKnown:         true,
		ProjectCaptureMode:   "off",
		EffectiveCaptureMode: "off",
	}))
	if strings.Contains(off, "does not load its own captured skills") {
		t.Error("no skip warning when capture is off")
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
// modes, the stored selection, and that the auto-apply toggle is always present
// but only enabled in Auto mode. Always-right is deliberate — the mode select
// saves without a re-render, and Auto without auto-apply behaves like Suggest.
func TestSkillCapturePanelRendersModeAndAutoApply(t *testing.T) {
	auto := renderHTML(t, skillCapturePanel(&Project{SkillCapture: map[string]any{"mode": "auto", "autoApply": true}}))
	for _, want := range []string{
		`<option value="auto" selected`,
		`<option value="suggest"`,
		`<option value="off"`,
		`hx-post="/settings/project/skill_capture_auto_apply"`,
	} {
		if !strings.Contains(auto, want) {
			t.Errorf("auto panel missing %q", want)
		}
	}
	autoTag := inputTagByID(auto, "autoApply")
	if autoTag == "" {
		t.Fatal("auto panel must render the auto-apply toggle")
	}
	if strings.Contains(autoTag, "disabled") {
		t.Error("auto-apply toggle must be enabled in Auto mode")
	}
	if !strings.Contains(autoTag, "checked") {
		t.Error("auto-apply toggle must reflect the stored autoApply value")
	}

	off := renderHTML(t, skillCapturePanel(&Project{SkillCapture: map[string]any{"mode": "off"}}))
	if !strings.Contains(off, `<option value="off" selected`) {
		t.Error("off panel must select the off option")
	}
	offTag := inputTagByID(off, "autoApply")
	if offTag == "" {
		t.Fatal("off panel must still render the auto-apply toggle (disabled)")
	}
	if !strings.Contains(offTag, "disabled") {
		t.Error("auto-apply toggle must be disabled outside Auto mode")
	}
	if !strings.Contains(off, "Only applies in Auto mode.") {
		t.Error("disabled toggle must say why it does not apply")
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

// TestApplyAgentSkillsSectionAutoLoadSkills pins the toggle mapping. The
// rendered form posts a hidden "false" then the checkbox's "true", so the last
// value wins; a form that omits the key entirely (an older caller) must leave a
// fetched API-set value untouched rather than clearing it.
func TestApplyAgentSkillsSectionAutoLoadSkills(t *testing.T) {
	tests := []struct {
		name     string
		form     url.Values
		existing bool
		want     bool
	}{
		{"checked turns it on", url.Values{"autoLoadSkills": {"false", "true"}}, false, true},
		{"unchecked (hidden false) turns it off", url.Values{"autoLoadSkills": {"false"}}, true, false},
		{"absent key leaves the fetched value", url.Values{}, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def := &AgentDefinition{ID: "a1", AutoLoadSkills: tc.existing}
			if err := applyAgentSkillsSection(def, skillCaptureSectionContext(t, tc.form)); err != nil {
				t.Fatalf("apply: %v", err)
			}
			if def.AutoLoadSkills != tc.want {
				t.Errorf("AutoLoadSkills = %v, want %v", def.AutoLoadSkills, tc.want)
			}
		})
	}
}

// TestAgentDefinitionAutoLoadSkillsWireShape pins that the field is always sent,
// including false: memory's PATCH treats an absent key as "leave unchanged", so
// omitempty would make the toggle unable to turn auto-load off.
func TestAgentDefinitionAutoLoadSkillsWireShape(t *testing.T) {
	for _, v := range []bool{false, true} {
		raw, err := json.Marshal(&AgentDefinition{ID: "a1", AutoLoadSkills: v})
		if err != nil {
			t.Fatal(err)
		}
		want := `"autoLoadSkills":false`
		if v {
			want = `"autoLoadSkills":true`
		}
		if !strings.Contains(string(raw), want) {
			t.Errorf("marshal(%v) = %s, want %s", v, raw, want)
		}
	}
}

// TestCaptureSkippedWithoutAutoLoad pins the warning predicate: it must follow
// the resolved mode, not the label, and only fire when the AfterRun gate will
// actually skip the agent.
func TestCaptureSkippedWithoutAutoLoad(t *testing.T) {
	tests := []struct {
		mode     string
		autoLoad bool
		want     bool
	}{
		{"suggest", false, true},
		{"auto", false, true},
		{"SUGGEST", false, true},
		{"off", false, false},
		{"", false, false},
		{"suggest", true, false},
		{"auto", true, false},
	}
	for _, tc := range tests {
		if got := captureSkippedWithoutAutoLoad(tc.mode, tc.autoLoad); got != tc.want {
			t.Errorf("captureSkippedWithoutAutoLoad(%q, %v) = %v, want %v", tc.mode, tc.autoLoad, got, tc.want)
		}
	}
}
