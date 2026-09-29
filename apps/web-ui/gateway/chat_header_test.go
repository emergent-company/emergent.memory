package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestActiveChatAgent pins the header's server-side active-agent resolution:
// the conversation's agent when ?c= names a known conversation, else the
// preselected/default agent; a conversation whose agent is gone falls back to
// the default rather than leaving the header blank; no agents at all = no agent.
func TestActiveChatAgent(t *testing.T) {
	agents := []AgentDefinitionSummary{
		{ID: "a1", Name: "diane"},
		{ID: "a2", Name: "milo"},
	}
	convs := &ConversationList{Conversations: []Conversation{
		{ID: "c1", Title: "Morning", AgentDefinitionID: "a2"},
		{ID: "c2", Title: "No agent", AgentDefinitionID: ""},
	}}

	tests := []struct {
		name      string
		agents    []AgentDefinitionSummary
		convs     *ConversationList
		preselect string
		convID    string
		want      string
		wantOK    bool
	}{
		{"default first agent", agents, nil, "", "", "a1", true},
		{"preselect wins over first", agents, nil, "a2", "", "a2", true},
		{"unknown preselect falls back to first", agents, nil, "nope", "", "a1", true},
		{"conversation agent wins", agents, convs, "", "c1", "a2", true},
		{"conversation agent wins over preselect", agents, convs, "a1", "c1", "a2", true},
		{"unknown conversation falls back to default", agents, convs, "", "gone", "a1", true},
		{"conversation with empty agent falls back to default", agents, convs, "", "c2", "a1", true},
		{"conversation agent not in list falls back to default", agents, &ConversationList{Conversations: []Conversation{{ID: "c3", AgentDefinitionID: "ghost"}}}, "", "c3", "a1", true},
		{"no agents", nil, nil, "", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := activeChatAgent(tc.agents, tc.convs, tc.preselect, tc.convID)
			if ok != tc.wantOK {
				t.Fatalf("activeChatAgent ok = %v, want %v", ok, tc.wantOK)
			}
			if got.ID != tc.want {
				t.Errorf("activeChatAgent id = %q, want %q", got.ID, tc.want)
			}
		})
	}
}

// headerTag returns the opening tag carrying id="<id>", or "" when absent.
func headerTag(html, id string) string {
	needle := `id="` + id + `"`
	i := strings.Index(html, needle)
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

// TestChatHeaderRender asserts the server-rendered chat pane header for the
// active agent: the contract hooks, the agent's name + description + icon, and
// the data-description carried on the picker options.
func TestChatHeaderRender(t *testing.T) {
	agents := []AgentDefinitionSummary{
		{ID: "a1", Name: "diane", Description: "Keeps the household running", UIConfig: json.RawMessage(`{"icon":"database","color":"#2563EB"}`)},
		{ID: "a2", Name: "milo"},
	}
	html := renderHTML(t, ChatPage(agents, nil, nil, nil, nil, "", "", "", nil, false, nil))

	for _, want := range []string{
		`data-testid="chat-agent-header"`,
		`id="chat-header-icon"`,
		`id="chat-header-title"`,
		`>diane</h1>`,
		`id="chat-header-desc"`,
		"Keeps the household running",
		"lucide--database",
		`data-description="Keeps the household running"`,
		`data-description=""`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chat header missing %q", want)
		}
	}
}

// TestChatHeaderResolvesConversationAgent asserts the header reflects the
// conversation's agent (not the default) when ?c= names it.
func TestChatHeaderResolvesConversationAgent(t *testing.T) {
	agents := []AgentDefinitionSummary{
		{ID: "a1", Name: "diane"},
		{ID: "a2", Name: "milo", Description: "Sorts the mail"},
	}
	convs := &ConversationList{Conversations: []Conversation{{ID: "c1", Title: "Morning", AgentDefinitionID: "a2"}}}
	html := renderHTML(t, ChatPage(agents, convs, nil, nil, nil, "", "c1", "", nil, false, nil))
	if !strings.Contains(html, `id="chat-header-title"`) || !strings.Contains(html, ">milo</h1>") {
		t.Errorf("header must show the conversation's agent; html=%s", html)
	}
	if !strings.Contains(html, "Sorts the mail") {
		t.Error("header must show the conversation agent's description")
	}
}

// TestChatHeaderHidesEmptyDescription asserts the description line is omitted
// (hidden) when the active agent has none, and rendered when it does not.
func TestChatHeaderHidesEmptyDescription(t *testing.T) {
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "diane"}}
	html := renderHTML(t, ChatPage(agents, nil, nil, nil, nil, "", "", "", nil, false, nil))

	tag := headerTag(html, "chat-header-desc")
	if tag == "" {
		t.Fatal("header missing the description mount")
	}
	if !strings.Contains(tag, "hidden") {
		t.Errorf("description line must be hidden when empty: %q", tag)
	}

	withDesc := []AgentDefinitionSummary{{ID: "a1", Name: "diane", Description: "Hi"}}
	html = renderHTML(t, ChatPage(withDesc, nil, nil, nil, nil, "", "", "", nil, false, nil))
	tag = headerTag(html, "chat-header-desc")
	if strings.Contains(tag, "hidden") {
		t.Errorf("description line must be visible when set: %q", tag)
	}
}

// TestChatHeaderNeutralBotTile asserts an agent without a ui_config icon/color
// renders the neutral bot tile.
func TestChatHeaderNeutralBotTile(t *testing.T) {
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "diane"}}
	html := renderHTML(t, ChatPage(agents, nil, nil, nil, nil, "", "", "", nil, false, nil))
	if !strings.Contains(html, "lucide--bot") {
		t.Error("agent without an appearance must render the neutral bot tile")
	}
}

// TestChatHeaderEscapesDescription asserts the description is rendered through
// templ's escaping (untrusted agent/user text must not become markup).
func TestChatHeaderEscapesDescription(t *testing.T) {
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "diane", Description: "<b>bold</b>"}}
	html := renderHTML(t, ChatPage(agents, nil, nil, nil, nil, "", "", "", nil, false, nil))
	if strings.Contains(html, "<b>bold</b>") {
		t.Error("description must be escaped, not rendered as markup")
	}
	if !strings.Contains(html, "&lt;b&gt;bold&lt;/b&gt;") {
		t.Errorf("description missing escaped form: %s", html)
	}
}

// TestChatHeaderFallbackCopy covers the fallback header (no active agent): the
// generic "Chat with Memory" title and the voice-aware subtitle, which is the
// only place that copy now lives.
func TestChatHeaderFallbackCopy(t *testing.T) {
	voiceOn := renderHTML(t, chatWorkspace(nil, nil, nil, nil, nil, "", "", "", true, nil, chatRunControl{}))
	if !strings.Contains(voiceOn, `id="chat-header-title"`) || !strings.Contains(voiceOn, ">Chat with Memory</h1>") {
		t.Error("fallback header must keep the generic title")
	}
	if !strings.Contains(voiceOn, "Chat by text or voice.") {
		t.Error("voice-enabled fallback must keep the voice-aware subtitle")
	}
	if !strings.Contains(voiceOn, "lucide--messages-square") {
		t.Error("fallback header must keep the generic messages tile")
	}

	voiceOff := renderHTML(t, chatWorkspace(nil, nil, nil, nil, nil, "", "", "", false, nil, chatRunControl{}))
	if !strings.Contains(voiceOff, "Chat by text.") {
		t.Error("voice-disabled fallback must say 'Chat by text.'")
	}
	if strings.Contains(voiceOff, "Chat by text or voice.") {
		t.Error("voice-disabled fallback must not mention voice")
	}
}

// TestChatHeaderJSWritesDescriptionWithTextContent is a static guard on the
// client mirror: updateChatHeader must exist and write the description with
// textContent, never innerHTML (agent description is untrusted).
func TestChatHeaderJSWritesDescriptionWithTextContent(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("webui", "static", "js", "chat.js"))
	if err != nil {
		t.Fatalf("read chat.js: %v", err)
	}
	js := string(b)

	const marker = "function updateChatHeader()"
	start := strings.Index(js, marker)
	if start < 0 {
		t.Fatal("chat.js: updateChatHeader is missing")
	}
	rest := js[start:]
	end := strings.Index(rest, "\n  }")
	if end < 0 {
		t.Fatal("chat.js: could not delimit updateChatHeader body")
	}
	body := rest[:end]

	if !strings.Contains(body, `descEl.textContent = desc`) {
		t.Error("updateChatHeader must write the description with textContent")
	}
	if strings.Contains(body, "innerHTML") {
		t.Error("updateChatHeader must never use innerHTML")
	}
	if !strings.Contains(body, `"data-description"`) {
		t.Error("updateChatHeader must read the option's data-description")
	}
}

// --- agent General settings form: short description ---

func generalSectionContext(t *testing.T, form url.Values) echo.Context {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/agents/a1/settings/general", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	return echo.New().NewContext(req, httptest.NewRecorder())
}

// TestApplyAgentGeneralSectionDescription pins the description mapping: trimmed
// on save, empty clears it, and a value carried through unchanged round-trips.
func TestApplyAgentGeneralSectionDescription(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		form     url.Values
		want     string
	}{
		{"set and trim", "Old", url.Values{"name": {"diane"}, "description": {"  New desc  "}}, "New desc"},
		{"empty clears", "Old", url.Values{"name": {"diane"}, "description": {""}}, ""},
		{"round-trips untouched value", "Old", url.Values{"name": {"diane"}, "description": {"Old"}}, "Old"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def := &AgentDefinition{ID: "a1", Name: "diane", Description: tc.existing}
			if err := applyAgentGeneralSection(def, generalSectionContext(t, tc.form)); err != nil {
				t.Fatalf("applyAgentGeneralSection: %v", err)
			}
			if def.Description != tc.want {
				t.Errorf("Description = %q, want %q", def.Description, tc.want)
			}
		})
	}
}

// TestApplyAgentModelSectionKeepsDescription asserts saving a different section
// never clobbers the description (only the General applier owns it).
func TestApplyAgentModelSectionKeepsDescription(t *testing.T) {
	def := &AgentDefinition{ID: "a1", Name: "diane", Description: "Keep me"}
	form := url.Values{"modelName": {"openai/gpt-4o"}, "temperature": {"0.7"}, "maxTokens": {"4096"}}
	if err := applyAgentModelSection(def, generalSectionContext(t, form)); err != nil {
		t.Fatalf("applyAgentModelSection: %v", err)
	}
	if def.Description != "Keep me" {
		t.Errorf("Description = %q, want untouched", def.Description)
	}
}

// TestAgentGeneralSettingsDescriptionField asserts the General form exposes the
// editable short-description input bound to the stored value, with the chat
// hint.
func TestAgentGeneralSettingsDescriptionField(t *testing.T) {
	data := agentSettingsData{Agent: &AgentDefinition{ID: "a1", Name: "diane", Description: "Keeps the household running"}}
	html := renderHTML(t, agentGeneralSettingsForm(data))

	for _, want := range []string{
		`id="agent-settings-description"`,
		`name="description"`,
		`value="Keeps the household running"`,
		"Shown under the agent&#39;s name in chat.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("general settings form missing %q", want)
		}
	}
}
