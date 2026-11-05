package main

import (
	"net/http/httptest"
	"testing"
)

// The region dispatch is what lets a filter form request the *page* URL and
// still get only the results back. It is keyed on the HX-Target htmx sends, so
// these cases pin the shapes that can reach it — and, just as importantly, the
// ones that must not (a full load, another surface's region). A wrong answer
// here means a filter swaps the whole page body into the list.
func TestTargetsResultsRegion(t *testing.T) {
	cases := []struct {
		name   string
		target string
		region string
		want   bool
	}{
		{name: "no header is a document load", target: "", region: objectsResultsRegion, want: false},
		{name: "tag and id", target: "div#objects-results", region: objectsResultsRegion, want: true},
		{name: "bare id still matches", target: objectsResultsRegion, region: objectsResultsRegion, want: true},
		{name: "surrounding space is ignored", target: "  div#search-results ", region: searchResultsRegion, want: true},
		{name: "another region", target: "div#documents-results", region: objectsResultsRegion, want: false},
		{name: "a longer id is not a prefix match", target: "div#objects-results-footer", region: objectsResultsRegion, want: false},
		{name: "the page shell is not a region", target: "div#main-content", region: objectsResultsRegion, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/objects", nil)
			if tc.target != "" {
				req.Header.Set("HX-Target", tc.target)
			}
			if got := targetsResultsRegion(req, tc.region); got != tc.want {
				t.Errorf("targetsResultsRegion(%q, %q) = %v, want %v", tc.target, tc.region, got, tc.want)
			}
		})
	}
}

// Pin the attribute contract the filter forms render with. It is the reason a
// filter change is an in-place region swap rather than a document load, and it
// is shared by every list surface — so it should fail loudly here, once, rather
// than silently in one view's markup.
func TestListFilterAttrs(t *testing.T) {
	region := regionFilterAttrs("/objects", objectsResultsRegion)
	for key, want := range map[string]string{
		"hx-get":      "/objects", // the page URL, never a fragment route
		"hx-target":   "#objects-results",
		"hx-swap":     "outerHTML", // the region is replaced, not filled
		"hx-push-url": "true",
	} {
		if got := region[key]; got != want {
			t.Errorf("regionFilterAttrs()[%q] = %q, want %q", key, got, want)
		}
	}

	// A select-only filter applies on pick and on submit (the objects actor
	// picker is a menu of submit buttons).
	if got := filterFormAttrs("/objects", objectsResultsRegion)["hx-trigger"]; got != "change, submit" {
		t.Errorf("filterFormAttrs hx-trigger = %q, want %q", got, "change, submit")
	}

	// A form that also carries a text field applies only when a <select>
	// changed, so typing a query and tabbing away does not fire a request.
	if got := selectFilterFormAttrs("/search", searchResultsRegion)["hx-trigger"]; got != "change target:select, submit" {
		t.Errorf("selectFilterFormAttrs hx-trigger = %q, want %q", got, "change target:select, submit")
	}

	// A query form runs on Enter or the button, never while typing.
	if got := submitFilterFormAttrs("/search", searchResultsRegion)["hx-trigger"]; got != "submit" {
		t.Errorf("submitFilterFormAttrs hx-trigger = %q, want %q", got, "submit")
	}
}
