package main

import (
	"strings"
	"testing"
)

// TestModelSelectLabelsFullProviderModel covers #1441: a native <select> can
// only show the selected option's text, so catalog option labels must be the
// full provider/model reference — not the short display name — for the closed
// dropdown to keep the provider visible.
func TestModelSelectLabelsFullProviderModel(t *testing.T) {
	models := []Model{
		{Provider: "openai", ModelName: "gpt-4o", DisplayName: "GPT-4o"},
		{Provider: "openai", ModelName: "deepseek-v4-flash"},
	}
	html := renderHTML(t, modelSelect("agent-settings-model", models, &ModelConfig{Name: "openai/gpt-4o"}, "", nil))

	for _, want := range []string{
		`<option value="openai/gpt-4o" selected>openai/gpt-4o</option>`,
		`<option value="openai/deepseek-v4-flash">openai/deepseek-v4-flash</option>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("modelSelect missing full-ref option %q, got:\n%s", want, html)
		}
	}
	// The catalog's short display name must no longer be the option label.
	if strings.Contains(html, ">GPT-4o</option>") {
		t.Errorf("modelSelect still labels options with the short display name:\n%s", html)
	}
}

// TestAgentFormDialogOmitsToolsConfiguration covers #1510: the create/edit
// dialog keeps its modal id and must not carry any tools-configuration
// control. Tools stay configurable on the agent's settings subpage, so the
// dialog only defines the persona/model/delegation.
func TestAgentFormDialogOmitsToolsConfiguration(t *testing.T) {
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "diane"}}
	models := []Model{{Provider: "openai", ModelName: "gpt-4o"}}
	html := renderHTML(t, agentFormDialog(agents, models, ""))

	if !strings.Contains(html, `id="agent-form-modal"`) {
		t.Errorf("agent form dialog missing its modal id:\n%s", html)
	}
	for _, gone := range []string{`name="tools"`, `id="agent-tools"`, ">Tools<"} {
		if strings.Contains(html, gone) {
			t.Errorf("agent form dialog must not render tools control %q:\n%s", gone, html)
		}
	}
}
