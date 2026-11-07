package agents

import (
	"context"

	"github.com/emergent-company/emergent.memory/domain/skills"
)

// skillTranscriptAdapter adapts the persisted run log to the narrow transcript
// shape the skill-capture review consumes. It lives in the agents package
// because skills must not import agents.
type skillTranscriptAdapter struct {
	repo *Repository
}

// FindSkillReviewTranscript returns the chronological transcript of a run.
func (a *skillTranscriptAdapter) FindSkillReviewTranscript(ctx context.Context, runID string) ([]skills.TranscriptEntry, error) {
	if a == nil || a.repo == nil || runID == "" {
		return nil, nil
	}
	entries, err := a.repo.FindRunLogEntries(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := make([]skills.TranscriptEntry, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		entry := skills.TranscriptEntry{
			Kind:     e.Kind,
			Role:     e.Role,
			ToolName: e.ToolName,
			Status:   e.Status,
			Input:    e.Input,
			Output:   e.Output,
			Step:     e.StepNumber,
		}
		if e.Kind == "message" {
			entry.Text = messageText(e.Content)
		}
		out = append(out, entry)
	}
	return out, nil
}

// messageText extracts the human-readable text parts from a persisted message
// content map. The shape varies by provider ({"parts":[{"text":...}]}, a bare
// {"text":...}, or nested role wrappers), so walk it and collect text fields.
func messageText(content map[string]any) string {
	if len(content) == 0 {
		return ""
	}
	var parts []string
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > 6 {
			return
		}
		switch x := v.(type) {
		case string:
			// A bare string child of "parts"/"content" is itself text.
			if len(x) > 0 {
				parts = append(parts, x)
			}
		case []any:
			for _, item := range x {
				walk(item, depth+1)
			}
		case map[string]any:
			if t, ok := x["text"].(string); ok && t != "" {
				parts = append(parts, t)
				return
			}
			for _, key := range []string{"content", "parts", "message"} {
				if child, ok := x[key]; ok {
					walk(child, depth+1)
				}
			}
		}
	}
	walk(content, 0)
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n"
		}
		out += p
	}
	return out
}
