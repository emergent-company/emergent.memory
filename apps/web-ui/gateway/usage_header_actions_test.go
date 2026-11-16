package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUsageHeaderActions pins the #861 feedback on the usage page head: the two
// header buttons share one right-aligned action row (so they get the action-gap
// instead of the header's title/actions gap), Manage budget renders first as
// the quiet ghost action, and Run log renders second as the brand-yellow
// primary call to action. Both data-testids survive so the feedback overlay's
// anchors stay stable.
func TestUsageHeaderActions(t *testing.T) {
	f := &fakeMemory{usageSummary: sampleUsageSummary(), usageSeries: sampleUsageSeries()}
	_, e := newUsageEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	manageAt := strings.Index(body, `data-testid="usage-manage-budget"`)
	runAt := strings.Index(body, `data-testid="usage-run-log"`)
	if manageAt < 0 || runAt < 0 {
		t.Fatalf("header actions missing (manage=%d run=%d):\n%s", manageAt, runAt, body)
	}
	if manageAt > runAt {
		t.Error("Manage budget must render before Run log")
	}

	manageTag := tagAround(t, body, `data-testid="usage-manage-budget"`)
	for _, want := range []string{"btn btn-ghost btn-sm", `href="/settings/budget"`} {
		if !strings.Contains(manageTag, want) {
			t.Errorf("manage-budget tag missing %q: %s", want, manageTag)
		}
	}
	if strings.Contains(manageTag, "btn-primary") {
		t.Errorf("Manage budget must stay the ghost action: %s", manageTag)
	}

	runTag := tagAround(t, body, `data-testid="usage-run-log"`)
	for _, want := range []string{"btn btn-primary btn-sm", `href="/usage/sessions?days=30"`} {
		if !strings.Contains(runTag, want) {
			t.Errorf("run-log tag missing %q: %s", want, runTag)
		}
	}

	// Both buttons live in the shared action row, not as bare children of the
	// header's outer flex (which would space them with the title/actions gap).
	if !strings.Contains(body, `<div class="flex flex-wrap items-center justify-end gap-2"><a href="/settings/budget"`) {
		t.Errorf("header actions are not wrapped in the right-aligned action row:\n%s", body)
	}
}
