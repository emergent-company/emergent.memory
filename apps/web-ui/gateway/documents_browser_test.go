package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// documentsTestServer wires the documents routes onto a server backed by f, the
// way main.go does, so the handler's own parameter parsing is under test.
func documentsTestServer(f *fakeMemory) *echo.Echo {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/documents", s.uiDocuments)
	e.GET("/documents/partial", s.uiDocumentsPartial)
	return e
}

// TestUIDocumentsListParams pins the query-parameter contract between the page
// and the API: the filters, search, sort and cursor the user selected reach the
// listing request, and unrecognised values normalize to the defaults instead of
// being forwarded (the API rejects them).
func TestUIDocumentsListParams(t *testing.T) {
	f := &fakeMemory{
		documents:            []Document{{ID: "d1", Filename: "Spec.md", ProcessingStatus: "completed", Chunks: 2, CreatedAt: "2026-08-26T10:00:00Z"}},
		documentsTotal:       37,
		documentStatusCounts: DocumentStatusCounts{All: 37, Ready: 20, Extracted: 10, Processing: 4, Failed: 3},
	}
	e := documentsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents?q=spec&status=failed&sourceType=drive&sort=updated&order=asc&cursor=cur-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	want := DocumentListParams{
		Query:      "spec",
		Status:     "failed",
		SourceType: "drive",
		Sort:       "updated",
		Order:      "asc",
		Cursor:     "cur-1",
		Limit:      documentPageSize,
	}
	if f.lastDocumentListParams != want {
		t.Errorf("list params = %+v, want %+v", f.lastDocumentListParams, want)
	}

	body := rec.Body.String()
	// The header count is the project total, not the one row the page showed.
	if !strings.Contains(body, "37 documents") {
		t.Error("header should report the project total")
	}
	if !strings.Contains(body, `data-testid="documents-stats"`) {
		t.Error("stats strip missing")
	}
	if got := f.catalogCallCount("ListDocumentSourceTypes"); got != 1 {
		t.Errorf("page fetched source types %d times, want 1", got)
	}

	// Unknown vocabulary falls back to the defaults rather than a 400: these
	// come from a bookmark or a hand-edited URL, not from the controls.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents?status=bogus&sort=filename&order=sideways", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	want = DocumentListParams{Sort: documentSortCreated, Order: documentOrderDesc, Limit: documentPageSize}
	if f.lastDocumentListParams != want {
		t.Errorf("normalized params = %+v, want %+v", f.lastDocumentListParams, want)
	}
}

// TestUIDocumentsLoadError pins that a failed listing renders the page's error
// state and no statistics, instead of a page full of zero counts.
func TestUIDocumentsLoadError(t *testing.T) {
	f := &fakeMemory{docErr: errors.New("upstream down")}
	e := documentsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Failed to load documents") {
		t.Error("error state missing")
	}
	if strings.Contains(body, `data-testid="documents-stats"`) {
		t.Error("statistics must not render zeroed counts on a failed fetch")
	}
}

// TestUIDocumentsPartial pins the pagination fragment endpoint: it carries the
// active view into the listing request and returns rows plus the OOB slots, not
// a whole page.
func TestUIDocumentsPartial(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{
			{ID: "d2", Filename: "notes.txt", ProcessingStatus: "ready_for_extraction", Chunks: 3, CreatedAt: "2026-08-26T11:00:00Z"},
		},
		documentsTotal: 37,
	}
	e := documentsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/partial?cursor=cur-2&q=note&status=ready&sourceType=upload&sort=updated&order=asc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	want := DocumentListParams{
		Query:      "note",
		Status:     "ready",
		SourceType: "upload",
		Sort:       "updated",
		Order:      "asc",
		Cursor:     "cur-2",
		Limit:      documentPageSize,
	}
	if f.lastDocumentListParams != want {
		t.Errorf("partial params = %+v, want %+v", f.lastDocumentListParams, want)
	}

	body := rec.Body.String()
	for _, wantHTML := range []string{
		`data-document-id="d2"`,
		`id="documents-mobile-list"`,
		`hx-swap-oob="beforeend"`,
		`id="documents-load-more"`,
		`hx-swap-oob="true"`,
	} {
		if !strings.Contains(body, wantHTML) {
			t.Errorf("partial missing %q", wantHTML)
		}
	}
	// A fragment, not a page: no shell, no stats strip.
	if strings.Contains(body, `data-testid="documents-stats"`) {
		t.Error("partial should not render the stats strip")
	}
	// It renders rows, not the filter select, so it must not pay for the
	// source-type catalog on every "Load more".
	if got := f.catalogCallCount("ListDocumentSourceTypes"); got != 0 {
		t.Errorf("partial fetched source types %d times, want 0", got)
	}

	// A failure is a 502 for the HTMX swap, not a silently empty append.
	f = &fakeMemory{docErr: errors.New("upstream down")}
	e = documentsTestServer(f)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/partial?cursor=cur-2", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// TestDocumentSourceTypeLabel pins the provenance labels: uploads read as
// "Uploaded", not as an internal "upload" token, and an unknown type is
// prettified rather than shown raw.
func TestDocumentSourceTypeLabel(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"upload":        "Uploaded",
		"drive":         "Google Drive",
		"repo":          "Repository",
		"email":         "Email",
		"custom_source": "Custom source",
	}
	for in, want := range cases {
		if got := documentSourceTypeLabel(in); got != want {
			t.Errorf("documentSourceTypeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDocumentProcessingLabel pins the status vocabulary shown to users: the
// six derived states map onto readable labels, and an unknown state surfaces
// as-is rather than rendering blank.
func TestDocumentProcessingLabel(t *testing.T) {
	cases := map[string]string{
		"converting":           "Converting",
		"conversion_failed":    "Conversion failed",
		"extracting":           "Extracting",
		"extraction_failed":    "Extraction failed",
		"completed":            "Extracted",
		"ready_for_extraction": "Ready",
		"some_new_state":       "some_new_state",
	}
	for in, want := range cases {
		if got := documentProcessingLabel(in); got != want {
			t.Errorf("documentProcessingLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestUIDocumentsSortLinks pins the sort control's URL contract: switching keys
// keeps the reader's direction, clicking the active column flips it, and the
// defaults stay out of the URL.
func TestUIDocumentsSortLinks(t *testing.T) {
	d := documentPageData{Sort: documentSortCreated, Order: documentOrderDesc}
	if got := documentsSortHref(d, documentSortUpdated); got != "/documents?sort=updated" {
		t.Errorf("switch key = %q", got)
	}
	if got := documentsSortHref(d, documentSortCreated); got != "/documents?order=asc" {
		t.Errorf("flip active key = %q", got)
	}

	d = documentPageData{Sort: documentSortUpdated, Order: documentOrderAsc, Query: "spec"}
	if got := documentsSortHref(d, documentSortUpdated); got != "/documents?q=spec&sort=updated" {
		t.Errorf("flip to desc = %q", got)
	}
	if got := documentsSortHref(d, documentSortCreated); got != "/documents?order=asc&q=spec" {
		t.Errorf("switch keeping direction = %q", got)
	}
}

// TestUIDocumentsPartialURL pins that "Load more" carries the whole view: the
// next cursor alone would silently reset the filters on the appended page.
func TestUIDocumentsPartialURL(t *testing.T) {
	d := documentPageData{
		Query:      "spec",
		Status:     documentStatusFailed,
		SourceType: "drive",
		Sort:       documentSortUpdated,
		Order:      documentOrderAsc,
		NextCursor: "cur-3",
	}
	got := documentsPartialURL(d)
	for _, want := range []string{"cursor=cur-3", "q=spec", "status=failed", "sourceType=drive", "sort=updated", "order=asc"} {
		if !strings.Contains(got, want) {
			t.Errorf("partial URL %q missing %q", got, want)
		}
	}
}
