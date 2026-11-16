package main

import (
	"strings"
	"testing"
)

// TestConversationStartedAt pins the server-side resolution of the active
// conversation's start instant: the matching row's created_at, else empty
// (nil list, no active id, or an id the visible page does not carry).
func TestConversationStartedAt(t *testing.T) {
	convs := &ConversationList{Conversations: []Conversation{
		{ID: "c1", Title: "Morning", CreatedAt: "2026-11-16T09:05:00Z"},
		{ID: "c2", Title: "No start"},
	}}

	tests := []struct {
		name   string
		convs  *ConversationList
		id     string
		expect string
	}{
		{"matching conversation", convs, "c1", "2026-11-16T09:05:00Z"},
		{"empty start", convs, "c2", ""},
		{"unknown conversation", convs, "gone", ""},
		{"no active id", convs, "", ""},
		{"nil list", nil, "c1", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := conversationStartedAt(tc.convs, tc.id); got != tc.expect {
				t.Fatalf("conversationStartedAt = %q, want %q", got, tc.expect)
			}
		})
	}
}

// TestChatPageSessionStartRender asserts the transcript carries the start line
// and its instant for the resumed conversation, and that a new chat hides it
// (the client fills the local-time text once the conversation exists).
func TestChatPageSessionStartRender(t *testing.T) {
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "diane"}}
	convs := &ConversationList{Conversations: []Conversation{
		{ID: "c1", Title: "Morning", AgentDefinitionID: "a1", CreatedAt: "2026-11-16T09:05:00Z"},
	}}

	html := renderHTML(t, ChatPage(agents, convs, nil, nil, nil, nil, "", "c1", "", nil, false, nil))
	for _, want := range []string{
		`data-conv-started="2026-11-16T09:05:00Z"`,
		`id="chat-session-start"`,
		`data-started-at="2026-11-16T09:05:00Z"`,
		`id="chat-session-start-time"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chat page missing %q", want)
		}
	}
	start := headerTag(html, "chat-session-start")
	if start == "" {
		t.Fatal("session-start mount missing")
	}
	if strings.Contains(start, "hidden") {
		t.Errorf("session-start must be visible for a resumed conversation: %q", start)
	}

	fresh := renderHTML(t, ChatPage(agents, convs, nil, nil, nil, nil, "", "", "", nil, false, nil))
	freshStart := headerTag(fresh, "chat-session-start")
	if freshStart == "" {
		t.Fatal("session-start mount missing on a new chat")
	}
	if !strings.Contains(freshStart, "hidden") {
		t.Errorf("session-start must be hidden on a new chat: %q", freshStart)
	}
	if !strings.Contains(freshStart, `data-started-at=""`) {
		t.Errorf("new chat must not carry a start instant: %q", freshStart)
	}
}
