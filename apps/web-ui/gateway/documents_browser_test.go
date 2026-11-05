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

// TestUIDocumentsSourceProvenance pins the per-row provenance wiring: a
// document's data-source id resolves to that source's name and to a link to its
// page, uploads stay plain text, an id the source list cannot resolve falls back
// to the source-kind label but still links, and a row with no provenance renders
// nothing at all.
func TestUIDocumentsSourceProvenance(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{
			{ID: "d1", Filename: "spec.md", ProcessingStatus: "completed", SourceType: "drive", SourceID: "ds-1"},
			{ID: "d2", Filename: "notes.txt", ProcessingStatus: "completed", SourceType: "upload"},
			{ID: "d3", Filename: "orphan.md", ProcessingStatus: "completed", SourceType: "repo", SourceID: "ds-gone"},
			{ID: "d4", Filename: "bare.md", ProcessingStatus: "completed"},
		},
		documentsTotal: 4,
		dsIntegrations: []DSIntegration{{ID: "ds-1", Name: "Product Drive", SourceType: "drive"}},
	}
	e := documentsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	// The connected source is named and linked, in the table and in the card.
	if !strings.Contains(body, `title="From Product Drive"`) {
		t.Error("row should name the data source it came from")
	}
	if got := strings.Count(body, `href="/data-sources/ds-1"`); got != 2 {
		t.Errorf("links to the resolved source = %d, want 2 (table row + card)", got)
	}
	// An id the list cannot resolve keeps the kind label and still links.
	if got := strings.Count(body, `href="/data-sources/ds-gone"`); got != 2 {
		t.Errorf("links to the unresolved source = %d, want 2 (table row + card)", got)
	}
	if !strings.Contains(body, "Repository") {
		t.Error("unresolved source should fall back to its kind label")
	}
	// Uploads and provenance-less rows link nowhere.
	for _, unwanted := range []string{`href="/data-sources/"`, `href="/data-sources/ds-2"`, `href="/data-sources/ds-4"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("unexpected source link %q", unwanted)
		}
	}
}

// TestUIDocumentsSourceFilterOptions pins the source filter's per-source
// wiring: each connected data source is named (not its kind), uploads get their
// own bucket, a source the integration list no longer knows stays selectable
// with an honest label, and counts come from the source-counts endpoint.
func TestUIDocumentsSourceFilterOptions(t *testing.T) {
	f := &fakeMemory{
		documents:      []Document{{ID: "d1", Filename: "meeting.md", ProcessingStatus: "completed", SourceType: "meeting", SourceID: "ds-trale"}},
		documentsTotal: 941,
		dsIntegrations: []DSIntegration{
			{ID: "ds-gh", Name: "Github Jon", SourceType: "repo"},
			{ID: "ds-trale", Name: "Trale", SourceType: "meeting"},
			{ID: "ds-web", Name: "Huma Website", SourceType: "web"},
		},
		docSourceCounts: []DocumentSourceCount{
			{SourceID: "ds-gh", SourceType: "repo", Count: 933},
			{SourceID: "ds-trale", SourceType: "meeting", Count: 7},
			{SourceID: "ds-web", SourceType: "web", Count: 1},
			{SourceType: "upload", Count: 2},
			{SourceID: "ds-gone", SourceType: "drive", Count: 4},
		},
	}
	e := documentsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents?sourceId=ds-trale", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{
		`<option value="ds-gh">Github Jon (933)</option>`,
		`<option value="ds-trale" selected>Trale (7)</option>`,
		`<option value="ds-web">Huma Website (1)</option>`,
		`<option value="none">Uploaded (2)</option>`,
		`<option value="ds-gone">Google Drive (removed source) (4)</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("source filter missing %q", want)
		}
	}
	if strings.Contains(body, `<option value="repo"`) || strings.Contains(body, `<option value="meeting"`) {
		t.Error("source filter must list data sources, not source kinds")
	}
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
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents?q=spec&status=failed&sourceId=ds-1&sort=updated&order=asc&cursor=cur-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	want := DocumentListParams{
		Query:    "spec",
		Status:   "failed",
		SourceID: "ds-1",
		Sort:     "updated",
		Order:    "asc",
		Cursor:   "cur-1",
		Limit:    documentPageSize,
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
	if got := f.catalogCallCount("ListDocumentSourceCounts"); got != 1 {
		t.Errorf("page fetched source counts %d times, want 1", got)
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
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/partial?cursor=cur-2&q=note&status=ready&sourceId=none&sort=updated&order=asc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	want := DocumentListParams{
		Query:    "note",
		Status:   "ready",
		SourceID: "none",
		Sort:     "updated",
		Order:    "asc",
		Cursor:   "cur-2",
		Limit:    documentPageSize,
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
	// source catalog on every "Load more".
	if got := f.catalogCallCount("ListDocumentSourceCounts"); got != 0 {
		t.Errorf("partial fetched source counts %d times, want 0", got)
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
		SourceID:   "ds-1",
		Sort:       documentSortUpdated,
		Order:      documentOrderAsc,
		NextCursor: "cur-3",
	}
	got := documentsPartialURL(d)
	for _, want := range []string{"cursor=cur-3", "q=spec", "status=failed", "sourceId=ds-1", "sort=updated", "order=asc"} {
		if !strings.Contains(got, want) {
			t.Errorf("partial URL %q missing %q", got, want)
		}
	}
}

// TestUIDocumentsUploader pins the per-row uploader wiring (#2260): a document
// with an uploader renders that name in both the table row and the mobile card,
// and a document without one (source-synced, or written before the column
// existed) renders the empty dash instead of a blank cell.
func TestUIDocumentsUploader(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{
			{ID: "d1", Filename: "uploaded.md", ProcessingStatus: "completed", SourceType: "upload", CreatedByUserID: "u-1", CreatedByDisplayName: "Ada Lovelace"},
			{ID: "d2", Filename: "synced.md", ProcessingStatus: "completed", SourceType: "drive", SourceID: "ds-1"},
		},
		documentsTotal: 2,
		dsIntegrations: []DSIntegration{{ID: "ds-1", Name: "Product Drive", SourceType: "drive"}},
	}
	e := documentsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	// One table cell and one card line, both naming the uploader (each also
	// carries the name in a title attribute, so count the titles rather than the
	// bare string).
	if got := strings.Count(body, `title="Ada Lovelace"`); got != 1 {
		t.Errorf("uploader table cells = %d, want 1", got)
	}
	if got := strings.Count(body, `title="Uploaded by Ada Lovelace"`); got != 1 {
		t.Errorf("uploader card lines = %d, want 1 (table cell + card line)", got)
	}
	if got := strings.Count(body, `data-testid="documents-uploader"`); got != 1 {
		t.Errorf("uploader table cells = %d, want 1 (the row without one must not render a name)", got)
	}
	if got := strings.Count(body, `data-testid="documents-uploader-card"`); got != 1 {
		t.Errorf("uploader card lines = %d, want 1", got)
	}
	// The source-synced row keeps its provenance and gains no invented uploader.
	if !strings.Contains(body, `title="From Product Drive"`) {
		t.Error("the source-synced row must still name its source")
	}
}
