package main

import (
	"strings"
	"testing"
)

// TestWorkRunStatusLabel pins the humanisation of the run wire vocabulary. The
// canonical values are machine-shaped and must never be printed verbatim.
func TestWorkRunStatusLabel(t *testing.T) {
	cases := map[string]string{
		"input-required": "needs input",
		"input_required": "needs input",
		"paused":         "needs input",
		"submitted":      "queued",
		"queued":         "queued",
		"working":        "running",
		"running":        "running",
		"completed":      "completed",
		"failed":         "failed",
		"cancelled":      "cancelled",
		"skipped":        "skipped",
		"":               "unknown",
		"awaiting-qa":    "Awaiting qa",
	}
	for in, want := range cases {
		if got := workRunStatusLabel(in); got != want {
			t.Errorf("workRunStatusLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBoardRunRows covers the row projection: agent name, human status, steps
// against the limit, duration, outcome and the discovered PR link. The server's
// runs arrive newest first and that order must survive.
func TestBoardRunRows(t *testing.T) {
	runs := []WorkItemRun{
		{
			ID:        "run-2",
			AgentName: "coding",
			Status:    "input-required",
			StartedAt: "2026-11-18T10:00:00Z",
			StepCount: 120,
			MaxSteps:  intPtr(120),
			Summary:   map[string]any{"reason": "step_limit_reached"},
		},
		{
			ID:          "run-1",
			AgentName:   "researcher",
			Status:      "completed",
			StartedAt:   "2026-11-18T09:00:00Z",
			CompletedAt: strPtr("2026-11-18T09:01:24Z"),
			DurationMs:  intPtr(84000),
			StepCount:   12,
			MaxSteps:    intPtr(30),
			Summary: map[string]any{
				"summary": "Opened https://github.com/acme/repo/pull/42.",
			},
		},
	}
	rows := boardRunRows(runs)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	first := rows[0]
	if first.ID != "run-2" || first.AgentName != "coding" || first.StatusLabel != "needs input" {
		t.Errorf("first row = %+v", first)
	}
	if first.Steps != "120/120 steps" {
		t.Errorf("first row steps = %q", first.Steps)
	}
	if first.Outcome != "Step limit reached" || first.OutcomeTone != "warning" {
		t.Errorf("first row outcome = %q/%q", first.Outcome, first.OutcomeTone)
	}
	second := rows[1]
	if second.Steps != "12/30 steps" || second.Duration != "1m" {
		t.Errorf("second row steps/duration = %q/%q", second.Steps, second.Duration)
	}
	if second.PRURL != "https://github.com/acme/repo/pull/42" {
		t.Errorf("second row PR = %q", second.PRURL)
	}

	// A run with no recorded limit still shows its step count; an unfinished run
	// has no duration.
	noLimit := boardRunRows([]WorkItemRun{{ID: "r", Status: "running", StartedAt: "2026-11-18T10:00:00Z", StepCount: 7}})[0]
	if noLimit.Steps != "7 steps" || noLimit.Duration != "" || noLimit.Started == "" {
		t.Errorf("no-limit row = %+v", noLimit)
	}
}

// TestBoardRunPRURL pins the best-effort PR discovery: known summary keys,
// nested pull_request objects, artefacts, and free-text summaries. It must not
// invent a link from an unrelated URL.
func TestBoardRunPRURL(t *testing.T) {
	cases := []struct {
		name string
		run  WorkItemRun
		want string
	}{
		{"plain key", WorkItemRun{Summary: map[string]any{"pr_url": "https://github.com/acme/repo/pull/9"}}, "https://github.com/acme/repo/pull/9"},
		{"nested", WorkItemRun{Summary: map[string]any{"pull_request": map[string]any{"html_url": "https://github.com/acme/repo/pull/10"}}}, "https://github.com/acme/repo/pull/10"},
		{"artifact", WorkItemRun{Summary: map[string]any{"artifacts": []any{"https://github.com/acme/repo/pull/11"}}}, "https://github.com/acme/repo/pull/11"},
		{"text punctuation", WorkItemRun{Summary: map[string]any{"final_response": "PR: https://github.com/acme/repo/pull/12."}}, "https://github.com/acme/repo/pull/12"},
		{"markdown link", WorkItemRun{Summary: map[string]any{"summary": "Opened [the PR](https://github.com/acme/repo/pull/14)."}}, "https://github.com/acme/repo/pull/14"},
		{"error text", WorkItemRun{ErrorMessage: strPtr("see https://github.com/acme/repo/pull/13")}, "https://github.com/acme/repo/pull/13"},
		{"unrelated", WorkItemRun{Summary: map[string]any{"summary": "Read https://example.com/docs"}}, ""},
		{"missing", WorkItemRun{}, ""},
	}
	for _, tc := range cases {
		if got := boardRunPRURL(tc.run); got != tc.want {
			t.Errorf("%s: boardRunPRURL = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestWorkPreviewBoardSlotRendersWorkRuns pins the visibility half of the issue:
// the preview lists one row per run above Activity, naming the agent, humanising
// the status, showing step usage and the outcome, and linking to the run page.
func TestWorkPreviewBoardSlotRendersWorkRuns(t *testing.T) {
	item := &WorkItem{CanonicalID: "w1", Type: "Task", Key: "ship-1", Status: "ready", Assignee: "coding"}
	runs := []WorkItemRun{
		{
			ID:        "run-2",
			AgentName: "coding",
			Status:    "input-required",
			StartedAt: "2026-11-18T10:00:00Z",
			StepCount: 120,
			MaxSteps:  intPtr(120),
			Summary:   map[string]any{"reason": "step_limit_reached"},
		},
		{
			ID:          "run-1",
			AgentName:   "researcher",
			Status:      "completed",
			StartedAt:   "2026-11-18T09:00:00Z",
			CompletedAt: strPtr("2026-11-18T09:01:24Z"),
			StepCount:   12,
			MaxSteps:    intPtr(30),
			Summary: map[string]any{
				"summary": "Opened https://github.com/acme/repo/pull/42.",
			},
		},
	}
	body := renderHTML(t, workPreviewBoardSlot(item, defaultBoardStatusMap(), nil, nil, runs, nil, nil))

	for _, want := range []string{
		"Work runs",
		`data-testid="board-run-row"`,
		`data-testid="board-run-attention"`,
		"Run paused at 120/120 steps",
		"Continue",
		`href="/runs/run-2"`,
		`/work/items/w1/cancel`,
		"coding",
		"needs input",
		"120/120 steps",
		"Step limit reached",
		"researcher",
		"completed",
		"12/30 steps",
		"1m",
		`href="https://github.com/acme/repo/pull/42"`,
		"Opened https://github.com/acme/repo/pull/42.",
		`href="/runs/run-1"`,
		`data-testid="board-run-link"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("preview work runs missing %q:\n%s", want, body)
		}
	}
	if strings.Count(body, `data-testid="board-run-row"`) != 2 {
		t.Errorf("expected two run rows:\n%s", body)
	}
	if strings.Contains(body, ">input-required<") {
		t.Errorf("raw wire status leaked into the preview:\n%s", body)
	}
	// Work runs must precede Activity, and the newest run its older sibling.
	if wi, ai := strings.Index(body, "Work runs"), strings.Index(body, ">Activity<"); wi < 0 || ai < 0 || wi > ai {
		t.Errorf("Work runs (%d) must render above Activity (%d)", wi, ai)
	}
	if r2, r1 := strings.Index(body, "run-2"), strings.Index(body, "run-1"); r2 < 0 || r1 < 0 || r2 > r1 {
		t.Errorf("runs must render newest first: run-2=%d run-1=%d", r2, r1)
	}
	// The Activity empty state no longer claims nothing happened at all.
	if !strings.Contains(body, `data-testid="board-preview-comment-empty"`) || !strings.Contains(body, "No comments yet") {
		t.Errorf("activity empty state must say comments, not activity:\n%s", body)
	}
}

// TestWorkPreviewBoardSlotRunsEmptyState pins the explicit empty state: a
// never-run object states it, rather than leaving the section looking missing.
func TestWorkPreviewBoardSlotRunsEmptyState(t *testing.T) {
	body := renderHTML(t, workPreviewBoardSlot(
		&WorkItem{CanonicalID: "w1", Type: "Task", Status: "ready"},
		defaultBoardStatusMap(), nil, nil, nil, nil, nil,
	))
	if !strings.Contains(body, `data-testid="board-run-empty"`) || !strings.Contains(body, "No runs yet") {
		t.Errorf("missing runs empty state:\n%s", body)
	}
	if strings.Contains(body, `data-testid="board-run-attention"`) {
		t.Errorf("an object with no runs must not render an attention strip:\n%s", body)
	}
}

// TestWorkPreviewRunAttentionOnlyForLatestPausedRun pins that the strip is
// derived from the latest run and that an older paused run does not resurrect
// the alarm once a newer run has run.
func TestWorkPreviewRunAttentionOnlyForLatestPausedRun(t *testing.T) {
	olderPaused := WorkItemRun{ID: "run-old", AgentName: "coding", Status: "input-required", StartedAt: "2026-11-18T08:00:00Z"}
	newerDone := WorkItemRun{ID: "run-new", AgentName: "coding", Status: "completed", StartedAt: "2026-11-18T09:00:00Z", CompletedAt: strPtr("2026-11-18T09:01:00Z"), Summary: map[string]any{"summary": "done"}}
	if got := boardRunAttention([]WorkItemRun{newerDone, olderPaused}); got != nil {
		t.Errorf("newest run is completed, attention must be nil, got %+v", got)
	}
	if got := boardRunAttention([]WorkItemRun{olderPaused, newerDone}); got == nil || got.ID != "run-old" {
		t.Errorf("newest run is paused, attention = %+v", got)
	}
	if got := boardRunAttention(nil); got != nil {
		t.Errorf("no runs must produce no attention, got %+v", got)
	}
}

// TestWorkPreviewRunAttentionForQuestion pins the other park reason: a run
// paused for input says so instead of citing a step limit it never hit.
func TestWorkPreviewRunAttentionForQuestion(t *testing.T) {
	run := WorkItemRun{
		ID: "run-q", AgentName: "coding", Status: "input-required", StartedAt: "2026-11-18T10:00:00Z",
		StepCount: 3, MaxSteps: intPtr(30),
		Summary: map[string]any{"reason": "awaiting_user_input"},
	}
	row := boardRunRows([]WorkItemRun{run})[0]
	if got := boardRunAttentionTitle(run, row); !strings.Contains(got, "waiting for your response") {
		t.Errorf("attention title = %q", got)
	}
}

// TestWorkCardSurfacesPausedRun pins the card-level honesty: a paused run is
// visible even though IsWorking is false, the chip says "paused" rather than the
// wire value, and the card carries the latest run identity and count.
func TestWorkCardSurfacesPausedRun(t *testing.T) {
	html := renderHTML(t, workCard(WorkItem{
		CanonicalID:     "w1",
		Type:            "Task",
		Key:             "ship-1",
		Status:          "ready",
		Assignee:        "coding",
		LatestRunID:     "run-9",
		LatestRunStatus: "input-required",
		RunCount:        3,
	}, workData{}))
	for _, want := range []string{
		`data-latest-run-id="run-9"`,
		`data-run-count="3"`,
		">paused<",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("work card missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "input-required") {
		t.Errorf("card leaked the raw wire status:\n%s", html)
	}
}
