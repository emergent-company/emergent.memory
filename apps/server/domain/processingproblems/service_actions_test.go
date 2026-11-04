package processingproblems

import "testing"

// TestActionForSourceSyncDeepLinksTheSource pins that a failing sync navigates
// to its own source page (where the job's error and the sync trigger live), not
// the list: the row's remediation names one source, so the action must too.
func TestActionForSourceSyncDeepLinksTheSource(t *testing.T) {
	src := "src-1"
	action := actionFor(QueueSourceSync, problemRow{SourceID: &src})
	if action.Kind != ActionOpen {
		t.Fatalf("kind = %q, want %q", action.Kind, ActionOpen)
	}
	if want := "/data-sources/src-1"; action.Href != want {
		t.Fatalf("href = %q, want %q", action.Href, want)
	}

	// Without a resolved source id the list is still better than a dead link.
	action = actionFor(QueueSourceSync, problemRow{})
	if want := "/data-sources"; action.Href != want {
		t.Fatalf("fallback href = %q, want %q", action.Href, want)
	}

	if got := drilldownFor(QueueSourceSync, problemRow{SourceID: &src}); got != "/data-sources/src-1" {
		t.Fatalf("drilldown = %q, want the source page", got)
	}
}

// TestActionForDocumentParsingIsARetry pins that a conversion failure carries a
// retry descriptor, not navigation only: the gateway renders a per-row Retry
// from it (a conversion retry resets the job row, hence the separate kind).
func TestActionForDocumentParsingIsARetry(t *testing.T) {
	doc := "doc-1"
	action := actionFor(QueueDocumentParsing, problemRow{DocumentID: &doc})
	if action.Kind != ActionRetryConversion {
		t.Fatalf("kind = %q, want %q", action.Kind, ActionRetryConversion)
	}
	if want := "/documents/doc-1"; action.Href != want {
		t.Fatalf("href = %q, want %q", action.Href, want)
	}
}

// TestActionForDiscoveryStaysAnOpenUntilItsControlsLand pins the row's shape for
// a failing discovery run: the panel navigates to the schema area instead of
// re-running the run itself, because the run's progress, the pack it produced
// and the retry control all live there (add-schema-discovery-controls, #52/#53).
//
// The retry the row offers is the panel's per-row action (the gateway renders it
// from processingProblemRetry against the same endpoint the card posts to), so
// this action case has to stay an open link rather than become a second retry.
// It is pinned because dropping the case would be silent — the row renders
// Drilldown, so a fall-through to {open, "Open", ""} only shows up as an empty
// action payload.
//
// add-schema-discovery-controls deep-links the anchor: the schema page as a
// whole is the compiled type browser, so landing at the top left the user
// hunting for the run the row is about.
func TestActionForDiscoveryStaysAnOpenUntilItsControlsLand(t *testing.T) {
	row := problemRow{Queue: QueueDiscovery, JobID: "job-1"}

	action := actionFor(QueueDiscovery, row)
	if action.Kind != ActionOpen {
		t.Fatalf("kind = %q, want %q", action.Kind, ActionOpen)
	}
	if want := "/schema#discovery"; action.Href != want {
		t.Fatalf("href = %q, want %q", action.Href, want)
	}
	if want := "Open discovery"; action.Label != want {
		t.Fatalf("label = %q, want %q", action.Label, want)
	}
	if want := "/schema#discovery"; drilldownFor(QueueDiscovery, row) != want {
		t.Fatalf("drilldown = %q, want %s", drilldownFor(QueueDiscovery, row), want)
	}
}
