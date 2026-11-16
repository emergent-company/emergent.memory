package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUsageSessionsBackToUsageIsPrimaryButton pins #857: yellow is the system's
// button colour, so a filled action button uses the brand's brass primary
// variant — the run log's page action was the steel secondary variant, which
// rendered as a blue button.
//
// The feedback's other contract is anchor stability. The change must not move
// the element: it stays an <a> carrying the captured selector
// (data-testid="sessions-back-to-usage"), the same /usage href, the
// lucide--chart-column icon and the "Usage dashboard" label, so it is still
// findable by selector and by fingerprint.
func TestUsageSessionsBackToUsageIsPrimaryButton(t *testing.T) {
	f := &fakeMemory{usageRuns: sampleUsageRuns(), usageByAgent: sampleUsageByAgent()}
	_, e := newUsageSessionsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := parseDOM(t, rec.Body.String())

	anchor := findElementByAttr(doc, "data-testid", "sessions-back-to-usage")
	if anchor == nil {
		t.Fatal("sessions-back-to-usage anchor not found")
	}
	if anchor.Data != "a" {
		t.Fatalf("selector must stay on an <a>, got <%s>", anchor.Data)
	}

	// Yellow: daisyUI's primary (brass) fill, not the steel secondary one.
	if !hasClasses(anchor, "btn", "btn-primary", "btn-sm") {
		t.Errorf("back-to-usage must render the yellow primary button, got class %q", classAttr(anchor))
	}
	if hasClasses(anchor, "btn-secondary") {
		t.Errorf("back-to-usage must not render the steel secondary button, got class %q", classAttr(anchor))
	}

	if got := attrValue(anchor, "href"); got != "/usage" {
		t.Errorf("href = %q, want /usage", got)
	}
	// Fingerprint: the icon and label that identify the captured element.
	if icon := findElementByClass(anchor, "lucide--chart-column"); icon == nil {
		t.Error("back-to-usage lost its lucide--chart-column icon")
	}
	if got := elementText(anchor); got != "Usage dashboard" {
		t.Errorf("label = %q, want %q", got, "Usage dashboard")
	}
}
