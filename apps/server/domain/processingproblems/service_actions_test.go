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

// TestActionForDiscoveryStaysAnOpenUntilItsControlsLand pins main's behaviour for
// a failing discovery run: the panel navigates to the schema area, which is where
// add-schema-discovery-controls puts the retry and the controls (issue #52, #53).
//
// This yielded change deliberately dropped its own discovery retry, which moved
// the queue out of its own action case; losing it from the shared case as well
// would be silent — the row renders Drilldown, so a fall-through to
// {open, "Open", ""} only shows up as an empty action payload. Hence the pin.
func TestActionForDiscoveryStaysAnOpenUntilItsControlsLand(t *testing.T) {
	row := problemRow{Queue: QueueDiscovery, JobID: "job-1"}

	action := actionFor(QueueDiscovery, row)
	if action.Kind != ActionOpen {
		t.Fatalf("kind = %q, want %q", action.Kind, ActionOpen)
	}
	if want := "/schema"; action.Href != want {
		t.Fatalf("href = %q, want %q", action.Href, want)
	}
	if want := "Open schema"; action.Label != want {
		t.Fatalf("label = %q, want %q", action.Label, want)
	}
	if got := drilldownFor(QueueDiscovery, row); got != "/schema" {
		t.Fatalf("drilldown = %q, want /schema", got)
	}
}
