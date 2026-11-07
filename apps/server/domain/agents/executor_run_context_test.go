package agents

import (
	"strings"
	"testing"
	"time"
)

// frozenExecutor returns an executor whose run clock is pinned, so the injected
// date/time is deterministic (issue #330).
func frozenExecutor(now time.Time) *AgentExecutor {
	return &AgentExecutor{now: func() time.Time { return now }}
}

// TestRunContextPrefixCarriesFrozenClockOnce is the fail-first regression for
// issue #330: every run's first user message carries exactly one "Current date
// and time" line with the run's clock value and an explicit zone. Before the
// fix, runContextPrefix only contained TriggerMetadata and the model had to
// guess the date.
func TestRunContextPrefixCarriesFrozenClockOnce(t *testing.T) {
	frozen := time.Date(2026, 11, 7, 5, 0, 0, 0, time.UTC)
	got := frozenExecutor(frozen).runContextPrefix(&AgentRun{})

	if n := strings.Count(got, "Current date and time:"); n != 1 {
		t.Fatalf("runContextPrefix() = %q, want exactly one %q line, got %d", got, "Current date and time:", n)
	}
	const want = "Current date and time: 2026-11-07T05:00:00Z (UTC)"
	if !strings.Contains(got, want) {
		t.Fatalf("runContextPrefix() = %q, want it to contain %q", got, want)
	}
}

// TestRunContextPrefixUsesPerRunClock proves the value is read from the clock at
// run time, not baked in at build time: the same executor sees the new date
// once its clock advances.
func TestRunContextPrefixUsesPerRunClock(t *testing.T) {
	now := time.Date(2026, 11, 7, 5, 0, 0, 0, time.UTC)
	ae := &AgentExecutor{now: func() time.Time { return now }}

	first := ae.runContextPrefix(&AgentRun{})
	now = now.Add(24 * time.Hour)
	second := ae.runContextPrefix(&AgentRun{})

	if !strings.Contains(first, "2026-11-07T05:00:00Z") {
		t.Fatalf("first run prefix = %q, want the 2026-11-07 timestamp", first)
	}
	if !strings.Contains(second, "2026-11-08T05:00:00Z") {
		t.Fatalf("second run prefix = %q, want the 2026-11-08 timestamp", second)
	}
	if first == second {
		t.Fatalf("both runs produced the same prefix %q; the clock must be read per run", first)
	}
}

// TestRunContextPrefixPreservesTriggerMetadata pins that the pre-existing
// <context> block is byte-identical apart from the new leading date line, so
// existing surfaces keep the trigger metadata they already relied on.
func TestRunContextPrefixPreservesTriggerMetadata(t *testing.T) {
	frozen := time.Date(2026, 11, 7, 5, 0, 0, 0, time.UTC)
	run := &AgentRun{TriggerMetadata: map[string]any{
		"namespace": "kb",
		"title":     "nightly",
	}}
	got := frozenExecutor(frozen).runContextPrefix(run)

	// encoding/json sorts map keys, so the legacy block is deterministic.
	const want = "Current date and time: 2026-11-07T05:00:00Z (UTC)\n\n" +
		"<context>\n{\"namespace\":\"kb\",\"title\":\"nightly\"}\n</context>\n\n"
	if got != want {
		t.Fatalf("runContextPrefix() = %q, want %q", got, want)
	}
}

// TestRunContextPrefixZeroValueExecutorHasDate guards the real construction
// paths, which never set the clock field: a zero-value executor must still
// produce a parseable RFC3339 timestamp in UTC.
func TestRunContextPrefixZeroValueExecutorHasDate(t *testing.T) {
	got := (&AgentExecutor{}).runContextPrefix(&AgentRun{})
	const heading = "Current date and time: "
	if !strings.HasPrefix(got, heading) {
		t.Fatalf("runContextPrefix() = %q, want it to start with %q", got, heading)
	}
	rest := strings.TrimPrefix(got, heading)
	stamp, _, ok := strings.Cut(rest, " (UTC)\n\n")
	if !ok {
		t.Fatalf("runContextPrefix() = %q, want an explicit %q zone label", got, "(UTC)")
	}
	if _, err := time.Parse(time.RFC3339, stamp); err != nil {
		t.Fatalf("runContextPrefix() timestamp %q is not RFC3339: %v", stamp, err)
	}
}
