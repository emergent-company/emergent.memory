package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/go-daisy/components/ui"
)

// The run history in the work-item preview answers the question the rest of the
// drawer cannot: has anything ever been run for this object, by which agent,
// how far did it get, and how did it end. The run rows are driven by the runs
// the detail endpoint already returns (newest first), so this file is pure
// presentation — it never fetches.

// boardRunRow is one row of the preview's "Work runs" section, pre-resolved so
// the template stays a flat render.
type boardRunRow struct {
	ID           string
	AgentName    string
	StatusLabel  string
	StatusIntent ui.BadgeIntent
	Started      string
	Duration     string
	Steps        string
	PRURL        string
	Outcome      string
	// OutcomeTone is "" for an ordinary final summary, "warning" for a parked
	// run, and "error" for a failure. The template maps it to a text class.
	OutcomeTone string
}

// boardRunRows converts the detail payload's runs into display rows, preserving
// the server's newest-first order.
func boardRunRows(runs []WorkItemRun) []boardRunRow {
	rows := make([]boardRunRow, 0, len(runs))
	for _, run := range runs {
		outcome, tone := boardRunOutcome(run)
		name := strings.TrimSpace(run.AgentName)
		if name == "" {
			name = "Unknown agent"
		}
		rows = append(rows, boardRunRow{
			ID:           run.ID,
			AgentName:    name,
			StatusLabel:  workRunStatusLabel(run.Status),
			StatusIntent: workRunStatusIntent(run.Status),
			Started:      relTime(run.StartedAt),
			Duration:     boardRunDuration(run),
			Steps:        boardRunSteps(run),
			PRURL:        boardRunPRURL(run),
			Outcome:      outcome,
			OutcomeTone:  tone,
		})
	}
	return rows
}

// workRunStatusLabel humanises a run's wire status for the UI. The canonical
// values are deliberately machine-shaped ("input-required", "submitted"), so
// they are never printed verbatim: a paused run reads "needs input", a queued
// one "queued", and a working one "running".
func workRunStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "submitted", "queued", "pending":
		return "queued"
	case "working", "running", "in_progress", "in-progress":
		return "running"
	case "input-required", "input_required", "paused", "needs_input", "needs-input":
		return "needs input"
	case "completed", "success", "succeeded":
		return "completed"
	case "failed", "error":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	case "cancelling", "canceling":
		return "cancelling"
	case "skipped":
		return "skipped"
	case "":
		return "unknown"
	default:
		return humanRunToken(status)
	}
}

// workRunStatusIntent maps a run status to its badge tone.
func workRunStatusIntent(status string) ui.BadgeIntent {
	switch workRunStatusLabel(status) {
	case "completed":
		return ui.BadgeSuccess
	case "failed":
		return ui.BadgeError
	case "needs input", "cancelling":
		return ui.BadgeWarning
	case "running", "queued":
		return ui.BadgeInfo
	default:
		return ui.BadgeNeutral
	}
}

// workRunNeedsInput reports whether a run status is the paused / needs-input
// state. Both the canonical "input-required" and the legacy "paused" spelling
// are accepted because run rows predate the vocabulary being standardised.
func workRunNeedsInput(status string) bool {
	return workRunStatusLabel(status) == "needs input"
}

// workRunLive reports whether a run status still represents work in flight. A
// card uses this to surface a run chip even when IsWorking has not caught up:
// a paused run is not "working", but it is certainly not nothing.
func workRunLive(status string) bool {
	switch workRunStatusLabel(status) {
	case "queued", "running", "needs input", "cancelling":
		return true
	default:
		return false
	}
}

// humanRunToken turns a snake/kebab token into a sentence-cased label
// ("step_limit_reached" → "Step limit reached").
func humanRunToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	parts := strings.FieldsFunc(token, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	out := strings.Join(parts, " ")
	// Sentence case the rest of the label: only the first rune is uppercased.
	if len(out) > 1 {
		out = out[:1] + strings.ToLower(out[1:])
	}
	return out
}

// boardRunSteps renders the run's step usage against its limit. The limit is
// what makes a step-limit park legible ("120/120 steps"); a run with no recorded
// limit still shows its step count rather than pretending it took none.
func boardRunSteps(run WorkItemRun) string {
	if run.MaxSteps != nil && *run.MaxSteps > 0 {
		return strconv.Itoa(run.StepCount) + "/" + strconv.Itoa(*run.MaxSteps) + " steps"
	}
	if run.StepCount > 0 {
		return strconv.Itoa(run.StepCount) + " steps"
	}
	return ""
}

// boardRunDuration renders how long the run took. The server's duration_ms is
// authoritative when present; older rows fall back to completed_at − started_at.
// An unfinished run has no duration yet, which is honest — its start time is
// shown alongside.
func boardRunDuration(run WorkItemRun) string {
	if run.DurationMs != nil && *run.DurationMs >= 0 {
		return formatRunDuration(float64(*run.DurationMs) / 1000)
	}
	if run.CompletedAt == nil || *run.CompletedAt == "" {
		return ""
	}
	start, err1 := time.Parse(time.RFC3339, run.StartedAt)
	end, err2 := time.Parse(time.RFC3339, *run.CompletedAt)
	if err1 != nil || err2 != nil || !end.After(start) {
		return ""
	}
	return formatRunDuration(end.Sub(start).Seconds())
}

// boardRunOutcome resolves the run's result line: the failure for a failed run,
// the parked reason for a paused one, and the agent's own final summary for a
// completed one. The second return value is the tone the template renders.
func boardRunOutcome(run WorkItemRun) (string, string) {
	if failure := runFailureText(run.Status, run.ErrorMessage, run.FailureClass, run.Summary); failure != "" {
		return failure, "error"
	}
	if reason, ok := run.Summary["reason"].(string); ok {
		switch reason {
		case "step_limit_reached":
			return "Step limit reached", "warning"
		case "awaiting_user_input":
			return "Waiting for your response", "warning"
		}
	}
	if summary, ok := run.Summary["summary"].(string); ok && strings.TrimSpace(summary) != "" {
		return strings.TrimSpace(summary), ""
	}
	if final, ok := run.Summary["final_response"].(string); ok && strings.TrimSpace(final) != "" {
		return strings.TrimSpace(final), ""
	}
	return "", ""
}

// boardRunPRKeys are the summary keys an agent or tool is known to use for the
// pull request a run produced. The set is deliberately a lookup rather than a
// schema: the summary is agent-authored JSON, so a missing key is normal.
var boardRunPRKeys = []string{
	"pr_url", "pull_request_url", "pullRequestUrl", "prUrl",
	"pull_request", "pullRequest",
}

// boardRunPRURL finds the run's pull-request link, best-effort. It checks the
// summary's known keys (string or {url/html_url} object), then any URL-shaped
// artefact, and finally scans the human summary text. This is presentation only:
// a run with no discoverable PR link simply omits the link.
func boardRunPRURL(run WorkItemRun) string {
	for _, key := range boardRunPRKeys {
		switch v := run.Summary[key].(type) {
		case string:
			if u := firstPullRequestURL(v); u != "" {
				return u
			}
		case map[string]any:
			for _, nested := range []string{"url", "html_url", "htmlUrl"} {
				if s, ok := v[nested].(string); ok {
					if u := firstPullRequestURL(s); u != "" {
						return u
					}
				}
			}
		}
	}
	for _, key := range []string{"artifacts", "deliverables"} {
		switch v := run.Summary[key].(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					if u := firstPullRequestURL(s); u != "" {
						return u
					}
				}
			}
		case []string:
			for _, s := range v {
				if u := firstPullRequestURL(s); u != "" {
					return u
				}
			}
		}
	}
	for _, key := range []string{"summary", "final_response", "error"} {
		if s, ok := run.Summary[key].(string); ok {
			if u := firstPullRequestURL(s); u != "" {
				return u
			}
		}
	}
	if run.ErrorMessage != nil {
		if u := firstPullRequestURL(*run.ErrorMessage); u != "" {
			return u
		}
	}
	return ""
}

// firstPullRequestURL extracts the first http(s) URL that names a pull request
// from free text, tolerating trailing punctuation ("…/pull/12."), markdown
// links, and a sentence that runs straight into the URL.
func firstPullRequestURL(s string) string {
	for _, field := range strings.Fields(s) {
		start := strings.Index(field, "https://")
		if start < 0 {
			start = strings.Index(field, "http://")
		}
		if start < 0 {
			continue
		}
		field = strings.TrimRight(field[start:], "<>()[]{}'\"`*.,;:!")
		if strings.Contains(field, "/pull/") {
			return field
		}
	}
	return ""
}

// boardRunAttention builds the preview's attention strip from the latest run,
// or nil when no run needs a human. It is deliberately not the durable `blocked`
// state: a paused run is transient and does not suppress dispatch.
func boardRunAttention(runs []WorkItemRun) *boardRunRow {
	if len(runs) == 0 || !workRunNeedsInput(runs[0].Status) {
		return nil
	}
	rows := boardRunRows(runs[:1])
	row := rows[0]
	return &row
}

// boardRunAttentionTitle is the headline of the attention strip: what the park
// actually was, in terms a human can act on.
func boardRunAttentionTitle(run WorkItemRun, row boardRunRow) string {
	reason, _ := run.Summary["reason"].(string)
	if reason == "awaiting_user_input" {
		return "Run paused — waiting for your response"
	}
	if steps := row.Steps; steps != "" {
		return fmt.Sprintf("Run paused at %s", steps)
	}
	return "Run paused — needs input"
}

// boardRunOutcomeClass maps an outcome tone to its text class. An ordinary
// summary stays muted so it reads as information, not a warning.
func boardRunOutcomeClass(tone string) string {
	switch tone {
	case "error":
		return "text-error"
	case "warning":
		return "text-warning"
	default:
		return "text-muted"
	}
}

// workCardRunStatusLabel is the board card's compact run label. A paused run is
// "paused" on the card (the issue's wording) while the preview row spells out
// "needs input"; both resolve the same wire value through workRunNeedsInput.
func workCardRunStatusLabel(status string) string {
	if workRunNeedsInput(status) {
		return "paused"
	}
	return workRunStatusLabel(status)
}
