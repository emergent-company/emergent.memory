package main

import (
	"strings"
	"testing"
)

// TestChatModelWarnings covers chatModelWarnings, the agentID → error-message
// map that drives the chat workspace's pre-send banner. Only error-severity
// issues surface (chats genuinely cannot run); warning-severity states (a
// resolvable default that just isn't pinned) and satisfied explicit models are
// excluded, mirroring the agent dashboard rules via agentModelDashboardIssue.
func TestChatModelWarnings(t *testing.T) {
	noModel := &AgentDefinition{ID: "a1", Name: "diane"}
	pinnedOpenAI := &AgentDefinition{ID: "a2", Name: "milo", Model: &ModelConfig{Name: "openai/gpt-4o"}}
	pinnedAnthropic := &AgentDefinition{ID: "a3", Name: "reggie", Model: &ModelConfig{Name: "anthropic/claude-sonnet-4"}}
	all := []AgentDefinitionSummary{
		{ID: "a1", Name: "diane"},
		{ID: "a2", Name: "milo"},
		{ID: "a3", Name: "reggie"},
	}

	// 1. zero providers, no default → every agent errors: legacy no-model copy
	//    for the default-less agent, zero-provider pinned copy for the others.
	f := &fakeMemory{
		defs: map[string]*AgentDefinition{"a1": noModel, "a2": pinnedOpenAI, "a3": pinnedAnthropic},
	}
	s := &Server{cfg: Config{}, memory: f}
	warnings := s.chatModelWarnings(t.Context(), all)
	if len(warnings) != 3 {
		t.Fatalf("warnings = %+v, want all three agents flagged", warnings)
	}
	if got := warnings["a1"]; got != "No provider or default model is configured — this agent can't run chats yet." {
		t.Errorf("a1 warning = %q", got)
	}
	if got := warnings["a2"]; got != "No provider is configured, so this agent's model openai/gpt-4o can't run yet. Configure a provider or change the agent's model." {
		t.Errorf("a2 warning = %q", got)
	}
	if got := warnings["a3"]; got != "No provider is configured, so this agent's model anthropic/claude-sonnet-4 can't run yet. Configure a provider or change the agent's model." {
		t.Errorf("a3 warning = %q", got)
	}

	// 2. openai provider configured, no default → only the agent whose model's
	//    prefix is unconfigured errors. The default-less agent falls to
	//    warning severity (excluded) and the openai-pinned model is satisfied.
	f2 := &fakeMemory{
		defs:             map[string]*AgentDefinition{"a1": noModel, "a2": pinnedOpenAI, "a3": pinnedAnthropic},
		projectProviders: []ProjectProviderConfig{{Provider: "openai"}},
	}
	s2 := &Server{cfg: Config{}, memory: f2}
	warnings = s2.chatModelWarnings(t.Context(), all)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %+v, want only a3 flagged", warnings)
	}
	if got := warnings["a3"]; got != "This agent's model anthropic/claude-sonnet-4 needs the anthropic provider, which isn't configured. Add it or choose another model." {
		t.Errorf("a3 warning = %q", got)
	}

	// 3. agents whose definition can't be fetched are skipped silently.
	f3 := &fakeMemory{
		defs:             map[string]*AgentDefinition{"a2": pinnedOpenAI},
		projectProviders: []ProjectProviderConfig{{Provider: "openai"}},
	}
	s3 := &Server{cfg: Config{}, memory: f3}
	warnings = s3.chatModelWarnings(t.Context(), all)
	if len(warnings) != 0 {
		t.Errorf("warnings = %+v, want empty (a1/a3 unfetchable, a2 satisfied)", warnings)
	}

	// 4. no agents → empty map.
	if warnings := s.chatModelWarnings(t.Context(), nil); len(warnings) != 0 {
		t.Errorf("empty agent list must yield no warnings, got %+v", warnings)
	}
}

// TestChatModelWarningBannerRender asserts the chat workspace's pre-send
// banner: when the default (selected) agent has a model warning the banner is
// visible with the message text, error styling, and a Configure-a-provider
// link, and that agent's picker option carries the data-warn; when no agent is
// warned the banner stays hidden with an empty text span and the options carry
// empty data-warn attributes.
func TestChatModelWarningBannerRender(t *testing.T) {
	agents := []AgentDefinitionSummary{
		{ID: "a1", Name: "diane"},
		{ID: "a2", Name: "milo"},
	}
	msg := "No provider is configured, so this agent's model openai/gpt-4o can't run yet. Configure a provider or change the agent's model."
	escaped := "No provider is configured, so this agent&#39;s model openai/gpt-4o can&#39;t run yet. Configure a provider or change the agent&#39;s model."

	// default agent (a1) warned → visible banner + data-warn on its option
	html := renderHTML(t, ChatPage(agents, nil, nil, nil, "", "", "", nil, false, map[string]string{"a1": msg}))
	if !strings.Contains(html, `id="chat-model-warning" role="alert" class="alert alert-error alert-outline mx-auto w-full max-w-6xl px-4 pt-3 lg:px-6"`) {
		t.Error("warned agent must render a visible (non-hidden) error banner")
	}
	for _, want := range []string{
		`role="alert"`,
		"alert-error",
		`id="chat-model-warning-text">` + escaped + `</span>`,
		`href="/settings/providers"`,
		"Configure a provider",
		`data-warn="` + escaped + `"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("warned chat page missing %q", want)
		}
	}

	// no warnings → banner hidden, empty text span, options carry empty data-warn
	htmlClean := renderHTML(t, ChatPage(agents, nil, nil, nil, "", "", "", nil, false, nil))
	if !strings.Contains(htmlClean, `id="chat-model-warning" role="alert" class="alert alert-error alert-outline mx-auto w-full max-w-6xl px-4 pt-3 lg:px-6 hidden"`) {
		t.Error("alert-free chat page must render the banner hidden")
	}
	if !strings.Contains(htmlClean, `id="chat-model-warning-text"></span>`) {
		t.Error("hidden banner must carry an empty text span")
	}
	if !strings.Contains(htmlClean, `data-warn=""`) {
		t.Error("unwarned agent options must carry an empty data-warn")
	}

	// a warning for a non-selected agent stays hidden server-side (chat.js
	// reveals it only once that agent is picked)
	htmlOther := renderHTML(t, ChatPage(agents, nil, nil, nil, "", "", "", nil, false, map[string]string{"a2": msg}))
	if !strings.Contains(htmlOther, `id="chat-model-warning" role="alert" class="alert alert-error alert-outline mx-auto w-full max-w-6xl px-4 pt-3 lg:px-6 hidden"`) {
		t.Error("non-selected agent's warning must not pre-open the banner")
	}
	if !strings.Contains(htmlOther, `value="a2" data-warn="`) {
		t.Error("warned option missing data-warn")
	}
}
