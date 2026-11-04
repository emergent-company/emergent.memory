package chat

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

func TestStripPauseSentinels(t *testing.T) {
	cases := map[string]string{
		"Execution paused. Waiting for user response to your question.":                        "",
		"Here is my summary.\n\nExecution paused. Waiting for user response to your question.": "Here is my summary.",
		"Step limit reached. Run has been paused.":                                             "",
		"Plain text": "Plain text",
	}
	for in, want := range cases {
		if got := stripPauseSentinels(in); got != want {
			t.Errorf("stripPauseSentinels(%q) = %q, want %q", in, got, want)
		}
	}
}

// On pause, the conversation used to record only the executor's sentinel. The
// question (and its options) must reach chat_messages so the history shows what
// the user is expected to answer.
func TestFormatQuestionForChat_IncludesOptions(t *testing.T) {
	q := &agents.AgentQuestion{
		Question: "Which blueprint should I apply?",
		Options: []agents.AgentQuestionOption{
			{Label: "Webpage feedback", Value: "webpage", Description: "ships the feedback loop"},
			{Label: "Do nothing", Value: "noop"},
		},
	}

	got := formatQuestionForChat(q)

	for _, want := range []string{
		"Which blueprint should I apply?",
		"**Webpage feedback** — ships the feedback loop",
		"**Do nothing**",
		"Reply with an option or your own answer.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatQuestionForChat missing %q in:\n%s", want, got)
		}
	}
}
