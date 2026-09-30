package agents

import (
	"strings"
	"testing"

	"google.golang.org/genai"
)

// TestPersistedEventTextExcludesThoughtFromReply is the server-side regression
// test for issue #1263: persistEventContent must not merge Thought
// (chain-of-thought) parts into the assistant reply text. It must keep them in
// the separate reasoning bucket so the gateway can render them as a Thinking
// block and the reply bubble stays clean.
func TestPersistedEventTextExcludesThoughtFromReply(t *testing.T) {
	parts := []*genai.Part{
		{Text: "Let me reason about this."},
		{Text: "first CoT paragraph\n\nsecond CoT paragraph", Thought: true},
		{Text: "## Short answer\n\nYes, it is 42."},
	}

	text, reasoning := persistedEventText(parts)

	if strings.Contains(text, "CoT") {
		t.Fatalf("chain-of-thought leaked into persisted reply text: %q", text)
	}
	wantText := "Let me reason about this.\n## Short answer\n\nYes, it is 42."
	if text != wantText {
		t.Errorf("reply text = %q, want %q", text, wantText)
	}
	wantReasoning := "first CoT paragraph\n\nsecond CoT paragraph"
	if reasoning != wantReasoning {
		t.Errorf("reasoning = %q, want %q", reasoning, wantReasoning)
	}
}

// TestPersistedEventTextReasoningOnly documents that a Thought-only event keeps
// the Thought text out of the reply text as well; the gateway surfaces it via
// content.reasoning rather than the answer bubble.
func TestPersistedEventTextReasoningOnly(t *testing.T) {
	parts := []*genai.Part{
		nil,
		{Text: ""},
		{Text: "covert reasoning", Thought: true},
	}

	text, reasoning := persistedEventText(parts)

	if text != "" {
		t.Errorf("reply text = %q, want empty", text)
	}
	if reasoning != "covert reasoning" {
		t.Errorf("reasoning = %q, want %q", reasoning, "covert reasoning")
	}
}
