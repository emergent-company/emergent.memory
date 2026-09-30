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

	text, reasoning := persistedEventText(parts, true)

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

// TestPersistedEventTextReasoningOnly is the server-side regression test for the
// reasoning-only final response: a reasoner in thinking mode answers entirely in
// reasoning_content, so the final event carries only Thought parts. Streaming
// (finalResponseStreamEvents) promotes that Thought to the answer text delta, so
// the persisted reply must do the same — content.text carries the answer and
// content.reasoning stays empty. Persisting it as reasoning-only left the reply
// bubble empty and flipped a correctly-streamed answer to nothing on history
// re-render.
func TestPersistedEventTextReasoningOnly(t *testing.T) {
	parts := []*genai.Part{
		nil,
		{Text: ""},
		{Text: "covert reasoning", Thought: true},
	}

	text, reasoning := persistedEventText(parts, true)

	if text != "covert reasoning" {
		t.Errorf("reply text = %q, want %q (reasoning-only final must promote Thought to the answer)", text, "covert reasoning")
	}
	if reasoning != "" {
		t.Errorf("reasoning = %q, want empty (Thought became the answer, matching streaming)", reasoning)
	}
}

// TestPersistedEventTextReasoningOnlyNonFinal locks the scoping of the guard:
// an intermediate (non-final) event that carries only Thought text must keep it
// in the reasoning bucket, because the executor streams intermediate Thought as
// thinking events, not answer tokens. Promoting it here would surface mid-run
// chain-of-thought as a reply bubble.
func TestPersistedEventTextReasoningOnlyNonFinal(t *testing.T) {
	parts := []*genai.Part{
		{Text: "intermediate planning", Thought: true},
	}

	text, reasoning := persistedEventText(parts, false)

	if text != "" {
		t.Errorf("reply text = %q, want empty for a non-final Thought-only event", text)
	}
	if reasoning != "intermediate planning" {
		t.Errorf("reasoning = %q, want %q", reasoning, "intermediate planning")
	}
}
