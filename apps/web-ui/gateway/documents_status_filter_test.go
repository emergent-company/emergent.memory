package main

import (
	"strings"
	"testing"
)

// TestDocumentsStatusFilterIsSelect pins #2313: the status filter is a <select>
// inside the existing filter form rather than a row of link buttons, and it
// submits with that form so a status change carries the search, source and sort
// state (the same view `requestURL` preserved).
func TestDocumentsStatusFilterIsSelect(t *testing.T) {
	data := documentsTestPage()
	data.Query = "spec"
	data.Status = documentStatusFailed
	data.SourceID = "ds-1"
	data.Sort = documentSortUpdated
	data.Order = documentOrderAsc

	html := renderHTML(t, DocumentsPage(data))
	start := strings.Index(html, `data-testid="documents-filter-form"`)
	if start < 0 {
		t.Fatal("filter form not found")
	}
	end := strings.Index(html[start:], "</form>")
	if end < 0 {
		t.Fatal("filter form closing tag not found")
	}
	form := html[start : start+end]

	if strings.Contains(html, `data-testid="documents-status-tabs"`) {
		t.Error("the old status tab strip must be gone")
	}
	if !strings.Contains(form, `<select id="documents-filter-status" name="status"`) {
		t.Errorf("status select must live inside the filter form:\n%s", form)
	}
	if !strings.Contains(form, `onchange="this.form.submit()"`) {
		t.Errorf("status select must submit the filter form:\n%s", form)
	}
	// The active bucket is selected and the counts the tabs showed survive.
	for _, want := range []string{
		`<option value="failed" selected`,
		"All (37)",
		"Processing (4)",
		"Ready (20)",
		"Extracted (10)",
		"Failed (3)",
	} {
		if !strings.Contains(form, want) {
			t.Errorf("status select missing %q:\n%s", want, form)
		}
	}
	// Every other narrowing control travels in the same GET submission.
	for _, want := range []string{
		`name="q" value="spec"`,
		`name="sourceId"`,
		`name="sort"`,
		`name="order"`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("filter form missing %q", want)
		}
	}
	// Without scripting the form still submits through the visible button.
	if !strings.Contains(form, "<noscript>") || !strings.Contains(form, "Apply filters") {
		t.Errorf("filter form has no no-JS submit control:\n%s", form)
	}
}
