package agents

import (
	"strings"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

// maxA2UIRepairs bounds the repair pass: exactly one extra model turn per run.
// Never recurse, never loop — a second failure degrades like before.
const maxA2UIRepairs = 1

// a2uiRepairState owns the one-shot repair decision so the executor loop stays
// small and the bound is testable on its own.
type a2uiRepairState struct {
	attempts int
}

// Next returns the repair prompt to send back when the final answer carried an
// invalid A2UI block and no plain text to fall back to. The second call after a
// repair returns ok=false even if the repair failed again.
func (s *a2uiRepairState) Next(parts []*genai.Part) (prompt string, ok bool) {
	if s.attempts >= maxA2UIRepairs {
		return "", false
	}
	prompt = a2uiRepairPrompt(parts)
	if prompt == "" {
		return "", false
	}
	s.attempts++
	return prompt, true
}

// Attempted reports whether a repair turn was issued.
func (s *a2uiRepairState) Attempted() bool { return s.attempts > 0 }

// Outcome classifies the run for the log/metric: "repaired" when the final
// answer now carries a valid surface, "degraded" when it still does not.
func (s *a2uiRepairState) Outcome(parts []*genai.Part) string {
	if eventHasValidA2UI(parts) {
		return "repaired"
	}
	return "degraded"
}

// a2uiRepairPrompt builds the single repair turn for an answer that emitted an
// A2UI fence but no valid surface. It returns "" when there is nothing worth
// repairing: no fence, a valid surface, or plain prose the user can still read
// (a card failure next to a good answer is not worth a second model call).
func a2uiRepairPrompt(parts []*genai.Part) string {
	text := partsPlainText(parts)
	if strings.TrimSpace(text) == "" {
		return ""
	}
	msgs, rest, hasBlock, issues := a2ui.ExtractWithIssues(text)
	if !hasBlock || len(msgs) > 0 {
		return ""
	}
	if strings.TrimSpace(rest) != "" {
		return ""
	}
	if len(issues) == 0 {
		issues = a2ui.Issues{{Detail: "the A2UI block did not produce a valid surface"}}
	}

	var b strings.Builder
	b.WriteString("Your previous answer's A2UI emission failed validation. ")
	b.WriteString("Fix exactly these issues:\n")
	for _, issue := range issues {
		b.WriteString("- ")
		b.WriteString(issue.Error())
		b.WriteString("\n")
	}
	b.WriteString("Reply with only a corrected ```a2ui fenced code block (one JSON message per line) and nothing else. ")
	b.WriteString("Keep the same surfaceId.")
	return b.String()
}

// eventHasValidA2UI reports whether the parts carry at least one valid A2UI
// surface. Used only to classify a repair outcome.
func eventHasValidA2UI(parts []*genai.Part) bool {
	text := partsPlainText(parts)
	if text == "" {
		return false
	}
	msgs, _, _, _ := a2ui.ExtractWithIssues(text)
	return len(msgs) > 0
}

// partsPlainText joins the non-Thought text parts. It mirrors
// extractA2UIFromParts so the repair decision and the extraction see the same
// answer text.
func partsPlainText(parts []*genai.Part) string {
	var b strings.Builder
	for _, part := range parts {
		if part == nil || part.Text == "" || part.Thought {
			continue
		}
		b.WriteString(part.Text)
	}
	return b.String()
}

// eventParts returns an event's content parts, tolerating a nil event/content.
func eventParts(ev *session.Event) []*genai.Part {
	if ev == nil || ev.Content == nil {
		return nil
	}
	return ev.Content.Parts
}
