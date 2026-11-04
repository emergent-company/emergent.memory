package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newSearchServer wires the Search page onto a bare echo router backed by a
// fake memory client, mirroring the objects-page tests: s.page fetches the shell
// data (agents/projects/orgs) from the same fake and renders the full page.
func newSearchServer(f *fakeMemory) *echo.Echo {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/search", s.uiSearch)
	return e
}

func searchPageBody(t *testing.T, f *fakeMemory, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	newSearchServer(f).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestUISearchEmptyQuerySkipsBackend pins the pre-query state: an empty or
// whitespace-only query renders the explainer and never calls the search API.
func TestUISearchEmptyQuerySkipsBackend(t *testing.T) {
	for _, target := range []string{"/search", "/search?q=", "/search?q=%20%20"} {
		f := &fakeMemory{}
		body := searchPageBody(t, f, target)
		if !strings.Contains(body, `data-testid="search-intro"`) {
			t.Errorf("%s: body missing the intro state", target)
		}
		if strings.Contains(body, `data-testid="search-results-summary"`) {
			t.Errorf("%s: empty query rendered results", target)
		}
		if f.lastSearchUnified.Query != "" {
			t.Errorf("%s: empty query dispatched a search (%+v)", target, f.lastSearchUnified)
		}
	}
}

// TestUISearchDocumentsFilterRendersChunkHits covers the core case: a
// documents-only search asks the endpoint for text results, resolves the
// document name, and renders the passage with its match mode and score.
func TestUISearchDocumentsFilterRendersChunkHits(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{{ID: "doc-1", Filename: "quarterly-report.md"}},
		searchUnifiedResp: &UnifiedSearchResponse{
			Results: []UnifiedSearchHit{
				{Type: "text", ID: "chunk-1", DocumentID: "doc-1", Snippet: "revenue grew 12%", Mode: "hybrid", Score: 0.87},
				{Type: "graph", ID: "obj-1", ObjectType: "Person", Key: "sam-lee", Score: 0.4},
			},
			Metadata: UnifiedSearchMetadata{TotalResults: 2, TextResultCount: 1, GraphResultCount: 1},
		},
	}

	body := searchPageBody(t, f, "/search?q=revenue&type=documents")

	if f.lastSearchUnified.Query != "revenue" {
		t.Errorf("query = %q, want revenue", f.lastSearchUnified.Query)
	}
	if f.lastSearchUnified.ResultTypes != "text" {
		t.Errorf("resultTypes = %q, want text", f.lastSearchUnified.ResultTypes)
	}
	if f.lastSearchUnified.Limit != defaultSearchLimit {
		t.Errorf("limit = %d, want %d", f.lastSearchUnified.Limit, defaultSearchLimit)
	}
	for _, want := range []string{
		"quarterly-report.md",
		"/documents/doc-1",
		"revenue grew 12%",
		"Keyword + semantic",
		"0.87",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	// The filter excludes objects, so that section must not render at all — not
	// even as an empty state.
	if strings.Contains(body, `data-testid="search-hit-object"`) {
		t.Error("documents filter rendered an object hit")
	}
	if strings.Contains(body, `data-testid="search-hit-document"`) == false {
		t.Error("documents filter did not render a document hit")
	}
}

// TestUISearchDefaultFilterIsEverything pins the default: no type param searches
// both result types and renders both sections.
func TestUISearchDefaultFilterIsEverything(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{{ID: "doc-1", Filename: "notes.md"}},
		searchUnifiedResp: &UnifiedSearchResponse{
			Results: []UnifiedSearchHit{
				{Type: "text", ID: "chunk-1", DocumentID: "doc-1", Snippet: "alpha", Mode: "lexical", Score: 0.9},
				{Type: "graph", ID: "obj-1", CanonicalID: "canon-1", ObjectType: "Note", Key: "alpha-note", Score: 0.5},
				{Type: "relationship", ID: "rel-1", Score: 0.1},
			},
		},
	}

	body := searchPageBody(t, f, "/search?q=alpha")

	if f.lastSearchUnified.ResultTypes != "both" {
		t.Errorf("resultTypes = %q, want both", f.lastSearchUnified.ResultTypes)
	}
	if !strings.Contains(body, `data-testid="search-hit-document"`) {
		t.Error("default filter did not render the document hit")
	}
	if !strings.Contains(body, `data-testid="search-hit-object"`) {
		t.Error("default filter did not render the object hit")
	}
	// Objects link through their canonical id when the endpoint supplies one.
	if !strings.Contains(body, "/objects/canon-1") {
		t.Error("object hit did not link through its canonical id")
	}
	if strings.Contains(body, "/objects/obj-1") {
		t.Error("object hit linked through the version id instead of the canonical id")
	}
}

// TestUISearchObjectsFilter pins the objects-only filter: graph results only,
// and no documents section.
func TestUISearchObjectsFilter(t *testing.T) {
	f := &fakeMemory{
		searchUnifiedResp: &UnifiedSearchResponse{
			Results: []UnifiedSearchHit{
				{Type: "graph", ID: "obj-1", ObjectType: "Person", Key: "sam-lee", Score: 0.6},
				{Type: "text", ID: "chunk-1", DocumentID: "doc-1", Snippet: "ignored", Score: 0.9},
			},
		},
	}

	body := searchPageBody(t, f, "/search?q=sam&type=objects")

	if f.lastSearchUnified.ResultTypes != "graph" {
		t.Errorf("resultTypes = %q, want graph", f.lastSearchUnified.ResultTypes)
	}
	if !strings.Contains(body, "sam-lee") {
		t.Error("objects filter did not render the object hit")
	}
	if strings.Contains(body, `data-testid="search-hit-document"`) {
		t.Error("objects filter rendered a document hit")
	}
}

// TestUISearchLimitNormalization pins the limit contract: offered values pass
// through, anything else falls back to the default (label, submitted value and
// dispatched limit stay in agreement).
func TestUISearchLimitNormalization(t *testing.T) {
	cases := map[string]int{
		"/search?q=a&limit=10":  searchLimitMin,
		"/search?q=a&limit=50":  searchLimitMax,
		"/search?q=a&limit=25":  searchLimitMid,
		"/search?q=a":           defaultSearchLimit,
		"/search?q=a&limit=999": defaultSearchLimit,
		"/search?q=a&limit=no":  defaultSearchLimit,
	}
	for target, want := range cases {
		f := &fakeMemory{}
		searchPageBody(t, f, target)
		if f.lastSearchUnified.Limit != want {
			t.Errorf("%s: limit = %d, want %d", target, f.lastSearchUnified.Limit, want)
		}
	}
}

// TestUISearchUnknownTypeNormalizes pins the filter contract: an unsupported
// type searches everything rather than narrowing to nothing.
func TestUISearchUnknownTypeNormalizes(t *testing.T) {
	f := &fakeMemory{}
	body := searchPageBody(t, f, "/search?q=a&type=bogus")

	if f.lastSearchUnified.ResultTypes != "both" {
		t.Errorf("resultTypes = %q, want both", f.lastSearchUnified.ResultTypes)
	}
	if !strings.Contains(body, "Everything") {
		t.Error("unknown type did not render the normalized 'Everything' label")
	}
}

// TestUISearchBackendErrorRendersPageError keeps a failed search a page-level
// error, not a blank page.
func TestUISearchBackendErrorRendersPageError(t *testing.T) {
	f := &fakeMemory{searchUnifiedErr: errAnySearchFailure{}}
	body := searchPageBody(t, f, "/search?q=alpha")

	if !strings.Contains(body, "Search failed") {
		t.Errorf("body missing the page error: %s", body[:min(len(body), 400)])
	}
}

// TestUISearchZeroHitsRendersOnlyTheQueryEmptyState pins the empty-state shape:
// a query with no hits anywhere gets one page-level empty state, not that plus
// two section empty states stacked underneath it.
func TestUISearchZeroHitsRendersOnlyTheQueryEmptyState(t *testing.T) {
	f := &fakeMemory{}
	body := searchPageBody(t, f, "/search?q=nothinghere")

	if !strings.Contains(body, "No matches for") {
		t.Error("zero-hit page did not render the query empty state")
	}
	for _, unwanted := range []string{"No document text matched", "No objects matched"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("zero-hit page also rendered the section empty state %q", unwanted)
		}
	}
}

// TestUISearchZeroHitsExplainsMissingEmbeddingModel covers the actionability
// hint: a query with no document hits in a project without an embedding model
// says so and links to the page that fixes it.
func TestUISearchZeroHitsExplainsMissingEmbeddingModel(t *testing.T) {
	f := &fakeMemory{effectiveModel: &EffectiveModelConfig{EmbeddingModel: ""}}
	body := searchPageBody(t, f, "/search?q=alpha")

	if !strings.Contains(body, "no embedding model configured") {
		t.Error("zero-hit state did not explain the missing embedding model")
	}
	if !strings.Contains(body, "/embeddings") {
		t.Error("zero-hit state did not link to the embeddings page")
	}
}

// TestUISearchZeroHitsWithEmbeddingModelHasNoHint keeps the warning off when the
// project is configured: an empty result is then simply an empty result.
func TestUISearchZeroHitsWithEmbeddingModelHasNoHint(t *testing.T) {
	f := &fakeMemory{effectiveModel: &EffectiveModelConfig{EmbeddingModel: "text-embedding-3-small"}}
	body := searchPageBody(t, f, "/search?q=alpha")

	if strings.Contains(body, "no embedding model configured") {
		t.Error("configured project rendered the missing-embedding-model hint")
	}
}

// TestUISearchObjectsOnlySkipsModelLookup pins the laziness rule: the model
// config is only fetched when a zero-hit search could have matched document
// text, so an objects-only search never pays for it.
func TestUISearchObjectsOnlySkipsModelLookup(t *testing.T) {
	f := &fakeMemory{}
	searchPageBody(t, f, "/search?q=alpha&type=objects")

	if f.effectiveModelCalls != 0 {
		t.Errorf("effective-model lookups = %d, want 0 for an objects-only search", f.effectiveModelCalls)
	}
}

// TestUISearchObjectHitsSkipModelLookup covers the other half of that rule: a
// search that did return results renders no empty-state hint, so it does not
// fetch the model config either.
func TestUISearchObjectHitsSkipModelLookup(t *testing.T) {
	f := &fakeMemory{
		searchUnifiedResp: &UnifiedSearchResponse{
			Results: []UnifiedSearchHit{{Type: "graph", ID: "obj-1", ObjectType: "Note", Key: "a-note", Score: 0.6}},
		},
	}
	searchPageBody(t, f, "/search?q=a")

	if f.effectiveModelCalls != 0 {
		t.Errorf("effective-model lookups = %d, want 0 when results exist", f.effectiveModelCalls)
	}
}

// TestUISearchDocumentNameFallback covers the degraded name: when the document
// lookup fails the row still names something usable instead of rendering a bare
// UUID.
func TestUISearchDocumentNameFallback(t *testing.T) {
	f := &fakeMemory{
		searchUnifiedResp: &UnifiedSearchResponse{
			Results: []UnifiedSearchHit{{Type: "text", ID: "c1", DocumentID: "doc-1", Snippet: "x", Score: 0.5}},
		},
	}
	body := searchPageBody(t, f, "/search?q=x")

	if !strings.Contains(body, "Document doc-1") {
		t.Errorf("body missing the document fallback label")
	}
}

// TestSplitSearchHits covers the union normalization directly: text and graph
// items are kept, relationship items are dropped, and a graph hit with no
// canonical id falls back to its object id.
func TestSplitSearchHits(t *testing.T) {
	docs, objs := splitSearchHits([]UnifiedSearchHit{
		{Type: "text", ID: "c1", DocumentID: "d1", Snippet: "a\n\n  b  ", Mode: "lexical", Score: 0.4},
		{Type: "graph", ID: "obj-1", ObjectID: "obj-1", ObjectType: "Note", Key: "k", Score: 0.3},
		{Type: "graph", ID: "obj-2", ObjectType: "Note", Key: "", Score: 0.2},
		{Type: "relationship", ID: "rel-1", Score: 0.1},
	})

	if len(docs) != 1 {
		t.Fatalf("documents = %d, want 1", len(docs))
	}
	if docs[0].Snippet != "a b" {
		t.Errorf("snippet = %q, want whitespace collapsed", docs[0].Snippet)
	}
	if len(objs) != 2 {
		t.Fatalf("objects = %d, want 2 (relationship dropped)", len(objs))
	}
	if objs[0].ID != "obj-1" {
		t.Errorf("object id = %q, want the object id when no canonical id is set", objs[0].ID)
	}
	if searchObjectLabel(objs[1]) != "Object obj-2" {
		t.Errorf("keyless object label = %q, want the id fallback", searchObjectLabel(objs[1]))
	}
}

// TestTrimSearchSnippet bounds a rendered chunk and keeps it readable.
func TestTrimSearchSnippet(t *testing.T) {
	long := strings.Repeat("x", searchSnippetRunes+50)
	got := trimSearchSnippet("  " + long + "  ")

	if !strings.HasSuffix(got, "…") {
		t.Error("long snippet was not truncated with an ellipsis")
	}
	if n := len([]rune(strings.TrimSuffix(got, "…"))); n != searchSnippetRunes {
		t.Errorf("truncated snippet runes = %d, want %d", n, searchSnippetRunes)
	}
	if short := trimSearchSnippet(" a\n\tb "); short != "a b" {
		t.Errorf("short snippet = %q, want whitespace collapsed", short)
	}
}

// TestSearchModeLabel pins the vocabulary the badge uses, so a mode the server
// adds later renders no badge rather than an empty word.
func TestSearchModeLabel(t *testing.T) {
	cases := map[string]string{
		"hybrid":  "Keyword + semantic",
		"lexical": "Keyword",
		"vector":  "Semantic",
		"":        "",
		"other":   "",
	}
	for mode, want := range cases {
		if got := searchModeLabel(mode); got != want {
			t.Errorf("searchModeLabel(%q) = %q, want %q", mode, got, want)
		}
	}
}

// errAnySearchFailure is a representative backend failure (the fake only needs
// a non-nil error to exercise the page's error state).
type errAnySearchFailure struct{}

func (errAnySearchFailure) Error() string { return "search backend unavailable" }
