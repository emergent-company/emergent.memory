package main

import (
	"strings"
	"testing"
	"time"
)

// TestRenderProjectSettingsPage asserts the General page renders the project
// record and the remember/dedup values with the settings sub-nav.
func TestRenderProjectSettingsPage(t *testing.T) {
	data := projectSettingsData{
		Project: &Project{
			ID:                          "p1",
			Name:                        "Home",
			ProjectInfo:                 "family",
			ChatPromptTemplate:          "be helpful",
			AutoExtractObjects:          boolPtr(true),
			AutoMergeExtractionBranches: boolPtr(false),
			BudgetUSD:                   floatPtr(25.5),
		},
		RememberAgent:  "memory",
		DedupThreshold: "0.8",
	}
	html := renderHTML(t, ProjectSettingsPage(data))
	for _, want := range []string{
		"Project Settings", "Project info", "Remember &amp; dedup",
		`name="name"`, `value="Home"`, `name="project_info"`, "family",
		`name="chat_prompt_template"`, "be helpful",
		`name="auto_extract_objects"`, `name="auto_merge_extraction_branches"`,
		`name="budget_usd"`, `value="25.5"`,
		`hx-post="/settings/project/name"`,
		`name="remember_agent"`, `value="memory"`,
		`name="dedup_threshold"`, `value="0.8"`,
		`hx-post="/settings/remember/agent"`, `hx-post="/settings/remember/dedup"`,
		// settings sub-nav is present on the General page
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("project settings page missing %q", want)
		}
	}
	// regression: boolean attributes render bare, never checked="false"
	if strings.Contains(html, `checked="false"`) {
		t.Error("toggles must not render checked=\"false\" (boolean-attribute bug)")
	}
	if !strings.Contains(html, `name="auto_extract_objects" class="toggle" checked`) {
		t.Error("on toggle should render checked")
	}
	if strings.Contains(html, `name="auto_merge_extraction_branches" class="toggle" checked`) {
		t.Error("off toggle must not render checked")
	}
}

// TestRenderSettingsSubNav covers the Settings sub-navigation rail: all six
// section links render and exactly one (the active section) is highlighted.
func TestRenderSettingsSubNav(t *testing.T) {
	html := renderHTML(t, settingsSubNav("voice", false))
	for _, want := range []string{
		`href="/settings"`, "General",
		`href="/settings/assistant"`, "Assistant",
		`href="/settings/overrides"`, "Overrides",
		`href="/settings/providers"`, "Providers",
		`href="/settings/voice"`, "Voice",
		`href="/settings/devices"`, "Devices",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("settings sub-nav missing %q", want)
		}
	}
	// Approvals is a top-level menu item, not a settings section
	if strings.Contains(html, `href="/settings/approvals"`) {
		t.Error("Approvals must not appear in the settings sub-nav")
	}
	if got := strings.Count(html, `aria-current="page"`); got != 1 {
		t.Errorf("exactly one active item expected, got %d", got)
	}
	if !strings.Contains(html, "bg-base-200 font-medium text-base-content") {
		t.Error("active item should carry the active highlight class")
	}
}

// TestRenderVoiceSettingsPage covers the Voice sub-page: header, sub-nav, and
// the inline auto-save panel (no whole-form Save button).
func TestRenderVoiceSettingsPage(t *testing.T) {
	data := voiceSettingsPageData{Voice: voiceSettings{Enabled: true, TTSProvider: "cartesia"}}
	html := renderHTML(t, VoiceSettingsPage(data))
	for _, want := range []string{
		"Voice",
		`hx-post="/settings/voice/enabled"`, `hx-swap="none"`,
		`hx-post="/settings/voice/group/tts"`, `hx-post="/settings/voice/group/endpoint_delays"`,
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("voice settings page missing %q", want)
		}
	}
	for _, gone := range []string{`action="/settings/voice"`, `name="voice_enabled"`, "Save voice", `-feedback`} {
		if strings.Contains(html, gone) {
			t.Errorf("voice settings page must not contain %q", gone)
		}
	}
}

// TestRenderAssistantSettingsPage covers the Assistant sub-page: header,
// sub-nav, and the assistant agent dropdown.
func TestRenderAssistantSettingsPage(t *testing.T) {
	data := assistantSettingsPageData{
		Agents:         []AgentDefinitionSummary{{ID: "a1", Name: "memory"}, {ID: "a2", Name: "diane"}},
		AssistantAgent: "a2",
	}
	html := renderHTML(t, AssistantSettingsPage(data))
	for _, want := range []string{
		"Assistant", `hx-post="/settings/assistant"`, `name="assistant_agent"`,
		`<option value="a2" selected>diane</option>`, `hx-swap="none"`,
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("assistant settings page missing %q", want)
		}
	}
	for _, gone := range []string{`action="/settings/assistant"`, "Save assistant"} {
		if strings.Contains(html, gone) {
			t.Errorf("assistant settings page must not contain %q", gone)
		}
	}
}

// TestRenderOverridesSettingsPage covers the Overrides sub-page: header,
// sub-nav, and the override list + form.
func TestRenderOverridesSettingsPage(t *testing.T) {
	data := overridesSettingsPageData{
		Overrides: []AgentOverrideEntry{
			{AgentName: "memory", Override: &AgentOverrideInput{
				SystemPrompt: strPtr("be terse"),
				Model:        &ModelConfig{Name: "gpt-4o"},
				Tools:        []string{"web_search", "memory_lookup"},
				MaxSteps:     intPtr(10),
			}},
		},
	}
	html := renderHTML(t, OverridesSettingsPage(data))
	for _, want := range []string{
		"Agent overrides", "memory", "system prompt", "model gpt-4o", "2 tools", "max 10 steps",
		`action="/settings/overrides/memory/delete"`,
		`name="agentName"`, `name="systemPrompt"`, `name="model"`, `name="tools"`, `name="maxSteps"`,
		`action="/settings/overrides"`,
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("overrides settings page missing %q", want)
		}
	}
}

// TestRenderProvidersSettingsPage covers the Providers sub-page: header,
// sub-nav, and the provider rate panel.
func TestRenderProvidersSettingsPage(t *testing.T) {
	data := providersSettingsPageData{Providers: providersTestData()}
	html := renderHTML(t, ProvidersSettingsPage(data))
	for _, want := range []string{
		"Providers", "openai", "gpt-4o", "custom", "auto",
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("providers settings page missing %q", want)
		}
	}
}

// TestRenderDevicesSettingsPage covers the Devices sub-page: header, sub-nav,
// the iOS setup panel, and the devices list empty state.
func TestRenderDevicesSettingsPage(t *testing.T) {
	html := renderHTML(t, DevicesSettingsPage(devicesSettingsPageData{}))
	for _, want := range []string{
		"Devices", "iOS setup", "No devices registered yet.",
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("devices settings page missing %q", want)
		}
	}
}

// TestRenderProjectSettingsPageAbsent covers the "not set" empty states when
// optional fields and settings are absent.
func TestRenderProjectSettingsPageAbsent(t *testing.T) {
	data := projectSettingsData{Project: &Project{ID: "p1", Name: "Home"}}
	html := renderHTML(t, ProjectSettingsPage(data))
	for _, want := range []string{"Budget not set.", "not set"} {
		if !strings.Contains(html, want) {
			t.Errorf("absent page missing %q", want)
		}
	}
}

// TestRenderOverridesSettingsPageNoFields renders an override with no fields
// set — the summary falls back to "no overridden fields".
func TestRenderOverridesSettingsPageNoFields(t *testing.T) {
	data := overridesSettingsPageData{
		Overrides: []AgentOverrideEntry{{AgentName: "diane", Override: &AgentOverrideInput{}}},
	}
	html := renderHTML(t, OverridesSettingsPage(data))
	if !strings.Contains(html, "no overridden fields") {
		t.Error("empty override should render 'no overridden fields'")
	}
}

// TestRenderProjectSettingsPageError asserts the whole-page error state and
// the flash states.
func TestRenderProjectSettingsPageError(t *testing.T) {
	if h := renderHTML(t, ProjectSettingsPage(projectSettingsData{LoadErr: errTest})); !strings.Contains(h, "Project settings unavailable") {
		t.Error("load-error state missing")
	}
	if h := renderHTML(t, ProjectSettingsPage(projectSettingsData{Project: &Project{Name: "Home"}, FlashMsg: "Project settings saved."})); !strings.Contains(h, "Project settings saved.") {
		t.Error("flash success missing")
	}
	if h := renderHTML(t, ProjectSettingsPage(projectSettingsData{Project: &Project{Name: "Home"}, FlashErr: errTest})); !strings.Contains(h, "backend unreachable") {
		t.Error("flash error should render the real error, missing")
	}
}

// TestOverrideSummary covers the override summary label builder.
func TestOverrideSummary(t *testing.T) {
	cases := []struct {
		in   AgentOverrideInput
		want string
	}{
		{AgentOverrideInput{}, "no overridden fields"},
		{AgentOverrideInput{Model: &ModelConfig{Name: "gpt-4o"}}, "model gpt-4o"},
		{AgentOverrideInput{Tools: []string{"a"}}, "1 tool"},
		{AgentOverrideInput{Tools: []string{"a", "b"}}, "2 tools"},
		{AgentOverrideInput{MaxSteps: intPtr(10)}, "max 10 steps"},
		{AgentOverrideInput{SystemPrompt: strPtr("x")}, "system prompt"},
		{AgentOverrideInput{SandboxConfig: map[string]any{"image": "x"}}, "sandbox config"},
		{AgentOverrideInput{Model: &ModelConfig{Name: "gpt-4o"}, Tools: []string{"a", "b"}, MaxSteps: intPtr(5)}, "model gpt-4o · 2 tools · max 5 steps"},
	}
	for _, c := range cases {
		if got := overrideSummary(c.in); got != c.want {
			t.Errorf("overrideSummary(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRenderDevicesPanel covers the Devices panel: empty state, a metadata
// device row (name + platform + OS version), a legacy device row (masked key +
// registration time fallback), and the per-device revoke forms.
func TestRenderDevicesPanel(t *testing.T) {
	// empty state
	html := renderHTML(t, devicesPanel(nil))
	if !strings.Contains(html, "No devices registered yet.") {
		t.Error("empty devices panel missing the empty-state text")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// metadata device: self-reported name/platform/OS shown, key stays masked
	mdKey := strings.Repeat("cd", 32) // 64 hex chars
	html = renderHTML(t, devicesPanel([]device{
		{
			Key:          mdKey,
			CreatedAt:    now,
			Platform:     "macos",
			Name:         "MacBook Pro",
			ModelID:      "Mac14,2",
			ModelDisplay: "MacBook Pro (M2)",
			OSName:       "macOS",
			OSVersion:    "15.0",
		},
	}))
	for _, want := range []string{
		"Devices",
		"MacBook Pro",        // name shown as the row title
		"macos · macOS 15.0", // platform + OS version
		"Registered", "just now",
		`action="/settings/devices/` + mdKey + `/revoke"`,
		"Revoke",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("metadata device panel missing %q", want)
		}
	}
	// the metadata device's full key appears only in the revoke form action
	// (which needs it to target the route), never in the display row
	mdDisplay := strings.SplitN(html, "<form", 2)[0]
	if strings.Contains(mdDisplay, mdKey[:10]) {
		t.Error("metadata device full key must not leak into the display row")
	}

	// legacy device (no manifest): masked key + registration time fallback
	key := strings.Repeat("ab", 32) // 64 hex chars
	html = renderHTML(t, devicesPanel([]device{
		{Key: key, CreatedAt: now},
	}))
	for _, want := range []string{
		"Devices",
		"••••••" + key[len(key)-6:],
		"Registered", "just now",
		`action="/settings/devices/` + key + `/revoke"`,
		"Revoke",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("legacy device panel missing %q", want)
		}
	}
	// the key is masked in the display row; it appears in full only in the
	// revoke form action (which needs it to target the route)
	display := strings.SplitN(html, "<form", 2)[0]
	if strings.Contains(display, key[:10]) {
		t.Error("device key must be masked in display, full key must not leak")
	}
}

// TestMaskDeviceKey covers the key-masking helper.
func TestMaskDeviceKey(t *testing.T) {
	if got := maskDeviceKey("0123456789abcdef"); got != "••••••abcdef" {
		t.Errorf("maskDeviceKey = %q, want ••••••abcdef", got)
	}
	if got := maskDeviceKey("short"); got != "short" {
		t.Errorf("short key should pass through unmasked, got %q", got)
	}
	if got := maskDeviceKey(""); got != "" {
		t.Errorf("empty key should stay empty, got %q", got)
	}
}

// TestRenderVoicePanel covers the editable Voice form: the master enable
// toggle, the TTS/STT/LiveKit/turn-tuning fields, the secrets shown
// set/not-set, the "Not set." note for an absent public URL, and the
// checked/unchecked toggle states.
func TestRenderVoicePanel(t *testing.T) {
	v := voiceSettings{
		Enabled:              true,
		TTSProvider:          "cartesia",
		TTSModel:             "sonic-3.5",
		TTSVoice:             "9626c31c-bec5-4cca-baa8-f8ba9e84c8bc",
		STTProvider:          "deepgram",
		STTModel:             "nova-3",
		STTLanguage:          "multi",
		LiveKitURL:           "ws://localhost:7880",
		LiveKitPublicURL:     "wss://gw.example.com",
		ExitKeywords:         "stop,goodbye,end",
		GoodbyeText:          "Goodbye.",
		AwayTimeout:          "20",
		EndpointMinDelay:     "0.4",
		EndpointMaxDelay:     "3",
		AllowInterruptions:   true,
		PreemptiveGeneration: false,
		CartesiaKeySet:       true,
		DeepgramKeySet:       false,
		LiveKitSecretSet:     true,
	}
	html := renderHTML(t, voicePanel(v))
	for _, want := range []string{
		"Voice",
		`name="enabled"`, `name="tts_provider"`,
		`name="tts_model"`, `name="tts_voice"`,
		`name="stt_provider"`, `name="stt_model"`, `name="stt_language"`,
		`name="livekit_url"`, `name="livekit_public_url"`,
		`name="exit_keywords"`, `name="goodbye_text"`,
		`name="away_timeout"`, `name="endpoint_min_delay"`, `name="endpoint_max_delay"`,
		`name="allow_interruptions"`, `name="preemptive_generation"`,
		"Cartesia API key", `class="text-success">set</span>`,
		"Deepgram API key", `class="text-base-content/45">not set</span>`,
		"LiveKit API secret", `class="text-success">set</span>`,
		`hx-post="/settings/voice/group/tts"`,
		`hx-post="/settings/voice/group/endpoint_delays"`,
		`hx-swap="none"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("voice panel missing %q", want)
		}
	}
	// no whole-form Save button or inline feedback regions remain
	for _, gone := range []string{`action="/settings/voice"`, "Save voice", `name="voice_enabled"`, `-feedback`} {
		if strings.Contains(html, gone) {
			t.Errorf("voice panel must not contain %q", gone)
		}
	}
	// enabled toggle renders a bare `checked` attribute, never checked="false"
	if !strings.Contains(html, `name="enabled" class="toggle" checked`) {
		t.Error("enabled toggle should render checked")
	}
	if strings.Contains(html, `checked="false"`) {
		t.Error("toggles must not render checked=\"false\" (boolean-attribute bug)")
	}
	// a set public URL must not render the "not set" note
	if strings.Contains(html, "Not set.") {
		t.Error("'Not set.' should not render when the public URL is set")
	}

	// empty public URL → "not set" note; disabled toggle → no checked
	empty := voiceSettings{Enabled: false, LiveKitPublicURL: ""}
	h := renderHTML(t, voicePanel(empty))
	if !strings.Contains(h, "Not set.") {
		t.Error("'Not set.' should render when the public URL is empty")
	}
	if strings.Contains(h, `name="enabled" class="toggle" checked`) {
		t.Error("disabled toggle must not render checked")
	}
	if strings.Contains(h, `checked="false"`) {
		t.Error("disabled toggle must not render checked=\"false\"")
	}
}

// TestRenderChatPageVoiceGating asserts the chat page omits the voice
// controls, audio element, and voice scripts when voice is disabled, and
// includes them when enabled. The kept <style> block mentions the voice
// controls in CSS comments/selectors, so absence is asserted on the element
// ids and the script src rather than the bare names.
func TestRenderChatPageVoiceGating(t *testing.T) {
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "memory"}}

	disabled := renderHTML(t, ChatPage(agents, nil, nil, nil, "", "", "", nil, false, nil))
	for _, absent := range []string{`id="voice-call-btn"`, `id="voice-mute-btn"`, `id="voice-status"`, `id="agent-audio"`, `js/voice.js`, "Chat by text or voice."} {
		if strings.Contains(disabled, absent) {
			t.Errorf("disabled chat page must not contain %q", absent)
		}
	}
	if !strings.Contains(disabled, "Chat by text.") {
		t.Error("disabled chat page should say 'Chat by text.'")
	}

	enabled := renderHTML(t, ChatPage(agents, nil, nil, nil, "", "", "", nil, true, nil))
	for _, want := range []string{`id="voice-call-btn"`, `id="voice-mute-btn"`, `id="voice-status"`, `id="agent-audio"`, `js/voice.js`, "Chat by text or voice."} {
		if !strings.Contains(enabled, want) {
			t.Errorf("enabled chat page missing %q", want)
		}
	}
	if strings.Contains(enabled, "Chat by text.") {
		t.Error("enabled chat page must not render the text-only subtitle")
	}
}

// TestRenderRememberAgentDropdown asserts the remember agent renders as a
// dropdown of agent definitions with the stored value selected and the
// "Use default agent" option preselected when nothing is stored.
func TestRenderRememberAgentDropdown(t *testing.T) {
	data := projectSettingsData{
		Project:       &Project{ID: "p1", Name: "Home"},
		Agents:        []AgentDefinitionSummary{{Name: "memory"}, {Name: "diane"}},
		RememberAgent: "diane",
	}
	html := renderHTML(t, ProjectSettingsPage(data))
	for _, want := range []string{
		`name="remember_agent" class="select w-full"`,
		`<option value=""`, defaultRememberAgent + " (default)",
		`<option value="memory">memory</option>`,
		`<option value="diane" selected>diane</option>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dropdown missing %q", want)
		}
	}
	// only the stored value is selected (scoped to the remember dropdown —
	// the assistant panel has its own empty option that is legitimately
	// selected when no assistant is configured)
	if strings.Contains(html, `<option value="" selected>`+defaultRememberAgent) {
		t.Error("default option should not be selected when a value is stored")
	}

	// nothing stored → default selected, no agent selected
	empty := projectSettingsData{Project: &Project{ID: "p1", Name: "Home"}, Agents: []AgentDefinitionSummary{{Name: "memory"}}}
	h := renderHTML(t, ProjectSettingsPage(empty))
	if !strings.Contains(h, `<option value="" selected>`) {
		t.Error("default option should be selected when nothing is stored")
	}
	if strings.Contains(h, `value="memory" selected`) {
		t.Error("agent option must not be selected when nothing is stored")
	}
}

// TestRenderRememberAgentUnlisted covers the fallback that keeps a stored
// remember agent visible when it no longer exists in the agent list.
func TestRenderRememberAgentUnlisted(t *testing.T) {
	data := projectSettingsData{
		Project:       &Project{ID: "p1", Name: "Home"},
		Agents:        []AgentDefinitionSummary{{Name: "memory"}},
		RememberAgent: "legacy-agent",
	}
	html := renderHTML(t, ProjectSettingsPage(data))
	if !strings.Contains(html, `<option value="legacy-agent" selected>legacy-agent (unlisted)</option>`) {
		t.Error("stored value not in the list should render as an (unlisted) selected option")
	}
	if !strings.Contains(html, `<option value="memory">memory</option>`) {
		t.Error("listed agents should still render")
	}
}

// TestRenderSettingsFieldTooltips asserts every field label carries the
// info-tip helper with the tooltip class + data-tip (CSS-only, no JS).
func TestRenderSettingsFieldTooltips(t *testing.T) {
	data := projectSettingsData{
		Project: &Project{ID: "p1", Name: "Home"},
		Agents:  []AgentDefinitionSummary{{Name: "memory"}},
	}
	html := renderHTML(t, ProjectSettingsPage(data))
	for _, want := range []string{
		`class="tooltip tooltip-top"`,
		`data-tip="Display name shown in the UI and briefings. Purely cosmetic — the project ID is stable, so renaming never breaks stored data or links."`,
		`data-tip="Free-form background about this project. Surfaces to agents via the project briefing, so a good summary here noticeably improves agent context and answer quality."`,
		`data-tip="Prepended to every chat prompt in this project. Re-primes the model for all conversations — a wrong template degrades every response, not just one."`,
		`data-tip="When on, newly uploaded documents run LLM extraction automatically. Keeps the graph current but spends tokens on every upload; when off you trigger extraction manually."`,
		`data-tip="When on, extraction staging branches merge into the main graph automatically once extraction completes. Saves a manual merge but skips the review step — bad extractions reach the main graph unchecked."`,
		`data-tip="Monthly LLM spend cap. Once exceeded, agent runs are rejected (402 budget exceeded) until the cycle resets. Empty means no cap."`,
		`data-tip="The agent whose prompt, model, and tools run the remember pipeline that turns conversations into memories. A wrong choice yields low-quality or mis-structured memories."`,
		`data-tip="Cosine-similarity cutoff (0.0–1.0) for near-duplicate detection when creating entities. Higher merges more aggressively (fewer duplicates, risk of combining distinct similar items); lower keeps more near-duplicates."`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("tooltip missing %q", want)
		}
	}
	// hints are preserved alongside the tooltips (templ escapes apostrophes)
	for _, want := range []string{
		"Required.",
		"Free-form context about this project.",
		"Monthly budget cap, if any.",
		"The agent definition that powers the remember pipeline. Empty clears it.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("hint missing %q", want)
		}
	}
}

// TestAgentListed covers the dropdown membership helper.
func TestAgentListed(t *testing.T) {
	agents := []AgentDefinitionSummary{{Name: "memory"}, {Name: "diane"}}
	if !agentListed(agents, "memory") {
		t.Error("memory should be listed")
	}
	if agentListed(agents, "legacy-agent") {
		t.Error("legacy-agent should not be listed")
	}
	if agentListed(nil, "memory") {
		t.Error("empty list should not match")
	}
}

// TestRenderAssistantPanel covers the assistant agent dropdown: the stored
// agent selected, the "None — hide assistant" default when unset, the hidden
// note, and the (unlisted) fallback for a stored agent that no longer exists.
func TestRenderAssistantPanel(t *testing.T) {
	// stored agent selected
	data := assistantSettingsPageData{
		Agents:         []AgentDefinitionSummary{{ID: "a1", Name: "memory"}, {ID: "a2", Name: "diane"}},
		AssistantAgent: "a2",
	}
	html := renderHTML(t, AssistantSettingsPage(data))
	for _, want := range []string{
		`name="assistant_agent" class="select w-full"`,
		`<option value="">None — hide assistant</option>`,
		`<option value="a1">memory</option>`,
		`<option value="a2" selected>diane</option>`,
		`hx-post="/settings/assistant"`, `hx-swap="none"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("assistant panel missing %q", want)
		}
	}
	if strings.Contains(html, `<option value="" selected>None`) {
		t.Error("default option should not be selected when an assistant is configured")
	}
	if strings.Contains(html, "Assistant is hidden") {
		t.Error("hidden note should not render when an assistant is configured")
	}

	// unset → default selected + hidden note
	unset := assistantSettingsPageData{Agents: []AgentDefinitionSummary{{ID: "a1", Name: "memory"}}}
	h := renderHTML(t, AssistantSettingsPage(unset))
	if !strings.Contains(h, `<option value="" selected>None — hide assistant</option>`) {
		t.Error("default option should be selected when no assistant is configured")
	}
	if !strings.Contains(h, "Assistant is hidden until you pick an agent.") {
		t.Error("hidden note missing when no assistant is configured")
	}

	// stored agent no longer exists → (unlisted) option keeps it visible
	unlisted := assistantSettingsPageData{
		Agents:         []AgentDefinitionSummary{{ID: "a1", Name: "memory"}},
		AssistantAgent: "legacy-agent",
	}
	lu := renderHTML(t, AssistantSettingsPage(unlisted))
	if !strings.Contains(lu, `<option value="legacy-agent" selected>legacy-agent (unlisted)</option>`) {
		t.Error("stored assistant not in the list should render as an (unlisted) selected option")
	}
	if !strings.Contains(lu, `<option value="a1">memory</option>`) {
		t.Error("listed agents should still render")
	}
}
