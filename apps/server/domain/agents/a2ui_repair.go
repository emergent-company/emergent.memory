package agents

import (
	"encoding/json"
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
	attempts  int
	surfaceID string
}

// Next returns the repair prompt to send back when the final answer carried an
// invalid A2UI block and no plain text to fall back to. The second call after a
// repair returns ok=false even if the repair failed again.
func (s *a2uiRepairState) Next(parts []*genai.Part) (prompt string, ok bool) {
	if s.attempts >= maxA2UIRepairs {
		return "", false
	}
	surfaceID := failedSurfaceID(parts)
	prompt = a2uiRepairPrompt(parts, surfaceID)
	if prompt == "" {
		return "", false
	}
	s.attempts++
	s.surfaceID = surfaceID
	return prompt, true
}

// Attempted reports whether a repair turn was issued.
func (s *a2uiRepairState) Attempted() bool { return s.attempts > 0 }

// SurfaceID is the failed block's surface id, captured when the repair turn was
// issued. A repaired emission that omits createSurface is stamped with it so
// the surface keeps its identity and ordering (#417).
func (s *a2uiRepairState) SurfaceID() string { return s.surfaceID }

// Outcome classifies the run for the log/metric: "repaired" when the final
// answer now carries a valid surface, "degraded" when it still does not.
func (s *a2uiRepairState) Outcome(parts []*genai.Part) string {
	if eventHasValidA2UI(parts) {
		return "repaired"
	}
	return "degraded"
}

// a2uiRepairFor is the executor's single repair decision point. It honors the
// RenderA2UI gate and never spends the last remaining step: a repair model call
// would trip beforeModelCb's step-limit check and turn a completed answer into
// "Execution paused" (executor.go beforeModelCb). Returns ok=false when no
// repair should run.
func a2uiRepairFor(req ExecuteRequest, tracker *stepTracker, state *a2uiRepairState, parts []*genai.Part) (string, bool) {
	if state == nil || !req.RenderA2UI {
		return "", false
	}
	if tracker != nil && tracker.nextStepExceedsBudget() {
		return "", false
	}
	return state.Next(parts)
}

// a2uiRepairPrompt builds the single repair turn for an answer that emitted an
// A2UI fence but no valid surface. It returns "" when there is nothing worth
// repairing: no fence, a valid surface, or plain prose the user can still read
// (a card failure next to a good answer is not worth a second model call).
func a2uiRepairPrompt(parts []*genai.Part, surfaceID string) string {
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
	if surfaceID == "" {
		surfaceID = a2ui.DefaultSurfaceID
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
	b.WriteString("Keep the same surfaceId. The block MUST begin with a createSurface line for surfaceId ")
	b.WriteString(surfaceID)
	b.WriteString(" (catalogId ")
	b.WriteString(a2ui.CatalogID)
	b.WriteString(") before any updateComponents line.")
	return b.String()
}

// ensureRepairedCreateSurface prepends a CreateSurface message when a repaired
// answer carries an a2ui block but no createSurface. The failed attempt's
// surface was already torn down, so without this the repaired card would be
// re-published under a default id with no opening message, breaking #417's
// ordering/stability guarantee. Blocks that already mention createSurface (even
// an invalid one) are left untouched so the validator's issues stay honest.
func ensureRepairedCreateSurface(parts []*genai.Part, surfaceID string) []*genai.Part {
	if surfaceID == "" {
		return parts
	}
	text := partsPlainText(parts)
	_, _, hasBlock, _ := a2ui.ExtractWithIssues(text)
	if !hasBlock || blockHasCreateSurface(text) {
		return parts
	}

	line, err := json.Marshal(map[string]any{
		"createSurface": map[string]string{
			"surfaceId": surfaceID,
			"catalogId": a2ui.CatalogID,
		},
	})
	if err != nil {
		return parts
	}

	injected := injectCreateSurfaceLine(text, string(line))
	if injected == text {
		return parts
	}

	// Collapse the non-Thought text into the first such part so the injected
	// line sits at the start of the block. Thought parts keep their position.
	out := make([]*genai.Part, 0, len(parts))
	inserted := false
	for _, part := range parts {
		if part == nil || part.Text == "" || part.Thought {
			out = append(out, part)
			continue
		}
		if !inserted {
			out = append(out, &genai.Part{Text: injected})
			inserted = true
		}
	}
	return out
}

// blockHasCreateSurface reports whether the first a2ui block mentions a
// createSurface envelope. It scans raw lines because an invalid block's
// messages are discarded by ExtractWithIssues; the string fallback catches a
// malformed opening message too.
func blockHasCreateSurface(text string) bool {
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if trimmed == "```a2ui" {
				inFence = true
			}
			continue
		}
		if trimmed == "```" {
			break
		}
		if strings.Contains(trimmed, `"createSurface"`) {
			return true
		}
	}
	return false
}

// injectCreateSurfaceLine inserts a createSurface JSONL line immediately after
// the first ```a2ui fence opener.
func injectCreateSurfaceLine(text, line string) string {
	const marker = "```a2ui"
	idx := strings.Index(text, marker)
	if idx < 0 {
		return text
	}
	at := idx + len(marker)
	if at < len(text) && text[at] == '\r' {
		at++
	}
	if at < len(text) && text[at] == '\n' {
		at++
	}
	return text[:at] + line + "\n" + text[at:]
}

// failedSurfaceID returns the surface id named anywhere in the failed block —
// including an updateComponents line that never reached a valid surface. It
// falls back to DefaultSurfaceID when the block names none.
func failedSurfaceID(parts []*genai.Part) string {
	inFence := false
	for _, line := range strings.Split(partsPlainText(parts), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if trimmed == "```a2ui" {
				inFence = true
			}
			continue
		}
		if trimmed == "```" {
			break
		}
		var msg a2ui.Message
		if err := json.Unmarshal([]byte(trimmed), &msg); err != nil {
			continue
		}
		if sid := messageSurfaceID(msg); sid != "" {
			return sid
		}
	}
	return a2ui.DefaultSurfaceID
}

// messageSurfaceID returns the surfaceId an envelope operates on, if any.
func messageSurfaceID(msg a2ui.Message) string {
	switch {
	case msg.CreateSurface != nil:
		return msg.CreateSurface.SurfaceID
	case msg.UpdateComponents != nil:
		return msg.UpdateComponents.SurfaceID
	case msg.UpdateDataModel != nil:
		return msg.UpdateDataModel.SurfaceID
	case msg.DeleteSurface != nil:
		return msg.DeleteSurface.SurfaceID
	}
	return ""
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
