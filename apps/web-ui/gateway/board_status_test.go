package main

import (
	"testing"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

// TestBoardStatusIntentSeparatesWorkflows pins that every status a shipped pack
// declares resolves to a visible tone, and that sibling statuses inside one
// workflow never share one. The triage vocabulary (webpage-feedback) is the
// regression this covers: `new / triaged / needs-info / wont-fix` all fell
// through to the neutral default and rendered as four identical grey badges.
func TestBoardStatusIntentSeparatesWorkflows(t *testing.T) {
	workflows := map[string][]string{
		"task-board": {"ready", "in_progress", "review", "revision", "blocked", "done"},
		"triage":     {"new", "triaged", "needs-info", "wont-fix"},
	}
	for name, statuses := range workflows {
		seen := make(map[ui.BadgeIntent]string, len(statuses))
		for _, status := range statuses {
			intent := boardStatusIntent(status)
			if other, dup := seen[intent]; dup {
				t.Errorf("%s: %s and %s share tone %q, so they cannot be told apart", name, other, status, intent)
			}
			seen[intent] = status
		}
		// Neutral is allowed for exactly one status per workflow (a closed or
		// parked item); two neutrals is the bug this test exists for.
		if len(seen) < len(statuses) {
			t.Errorf("%s: %d statuses resolve to only %d tones", name, len(statuses), len(seen))
		}
	}

	// The triage tones themselves, pinned so a refactor cannot drift them.
	wantTriage := map[string]ui.BadgeIntent{
		"new":        ui.BadgeInfo,
		"triaged":    ui.BadgeAccent,
		"needs-info": ui.BadgeWarning,
		"wont-fix":   ui.BadgeNeutral,
	}
	for status, want := range wantTriage {
		if got := boardStatusIntent(status); got != want {
			t.Errorf("boardStatusIntent(%q) = %q, want %q", status, got, want)
		}
	}

	// Underscore spellings are the same status, so they must agree.
	for _, pair := range [][2]string{{"needs_info", "needs-info"}, {"wont_fix", "wont-fix"}} {
		if got, want := boardStatusIntent(pair[0]), boardStatusIntent(pair[1]); got != want {
			t.Errorf("boardStatusIntent(%q) = %q, want %q (same as %q)", pair[0], got, want, pair[1])
		}
	}

	// Unknown statuses stay neutral rather than inventing a colour.
	if got := boardStatusIntent("some_custom_status"); got != ui.BadgeNeutral {
		t.Errorf("unknown status intent = %q, want neutral", got)
	}
}

// TestBoardStatusLabelHumanisesHyphenatedStatuses pins the label the badge
// prints. Hyphens separate words here just like underscores, and the two
// authored exceptions keep their punctuation.
func TestBoardStatusLabelHumanisesHyphenatedStatuses(t *testing.T) {
	cases := map[string]string{
		"":            "Unknown",
		"new":         "New",
		"triaged":     "Triaged",
		"needs-info":  "Needs info",
		"wont-fix":    "Won't fix",
		"in_progress": "In progress",
		"ready":       "Ready",
		"blocked":     "Blocked",
		"-":           "Unknown",
	}
	for status, want := range cases {
		if got := boardStatusLabel(status); got != want {
			t.Errorf("boardStatusLabel(%q) = %q, want %q", status, got, want)
		}
	}
}
