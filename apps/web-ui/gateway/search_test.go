package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newSearchServer wires the merged search page onto a bare echo router backed by
// a fake memory client, mirroring the objects-page tests: s.page fetches the
// shell data (agents/projects/orgs) from the same fake and renders the full page.
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
// whitespace-only query renders the browse list and never calls a search
// endpoint. Browse is the rest state of the page, not a prompt to type.
func TestUISearchEmptyQuerySkipsBackend(t *testing.T) {
	for _, target := range []string{"/search", "/search?q=", "/search?q=%20%20"} {
		f := &fakeMemory{}
		body := searchPageBody(t, f, target)
		if !strings.Contains(body, "No objects yet") {
			t.Errorf("%s: body missing the browse empty state", target)
		}
		if strings.Contains(body, `data-testid="search-results-summary"`) {
			t.Errorf("%s: empty query rendered a query summary", target)
		}
		if f.lastSearchUnified.Query != "" {
			t.Errorf("%s: empty query dispatched a text search (%+v)", target, f.lastSearchUnified)
		}
		if f.lastUnifiedQuery != "" {
			t.Errorf("%s: empty query dispatched an object search (%q)", target, f.lastUnifiedQuery)
		}
	}
}

// TestUISearchDocumentsScopeRendersChunkHits covers the documents-only scope: it
// asks the text leg for text results, resolves the document name, and renders
// the passage with its match mode and score — and does not run the object leg.
func TestUISearchDocumentsScopeRendersChunkHits(t *testing.T) {
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

	body := searchPageBody(t, f, "/search?q=revenue&in=documents")

	if f.lastSearchUnified.Query != "revenue" {
		t.Errorf("query = %q, want revenue", f.lastSearchUnified.Query)
	}
	if f.lastSearchUnified.ResultTypes != "text" {
		t.Errorf("resultTypes = %q, want text", f.lastSearchUnified.ResultTypes)
	}
	if f.lastSearchUnified.Limit != defaultSearchLimit {
		t.Errorf("limit = %d, want %d", f.lastSearchUnified.Limit, defaultSearchLimit)
	}
	if f.lastUnifiedQuery != "" {
		t.Errorf("documents scope ran the object leg (%q)", f.lastUnifiedQuery)
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
	if strings.Contains(body, `data-testid="object-card"`) {
		t.Error("documents scope rendered an object card")
	}
	if !strings.Contains(body, `data-testid="search-hit-document"`) {
		t.Error("documents scope did not render a document hit")
	}
}

// TestUISearchDefaultScopeIsEverything pins the default: no `in` runs both legs
// and renders the document section above the object cards. The object card links
// through the id the object endpoint resolved (canonical-first).
func TestUISearchDefaultScopeIsEverything(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{{ID: "doc-1", Filename: "notes.md"}},
		searchUnifiedResp: &UnifiedSearchResponse{
			Results: []UnifiedSearchHit{
				{Type: "text", ID: "chunk-1", DocumentID: "doc-1", Snippet: "alpha", Mode: "lexical", Score: 0.9},
				{Type: "relationship", ID: "rel-1", Score: 0.1},
			},
		},
		unifiedResults: []ObjectSearchResult{
			{Object: GraphObject{ID: "canon-1", Type: "Note", Key: "alpha-note"}, Score: 0.5},
		},
	}

	body := searchPageBody(t, f, "/search?q=alpha")

	if f.lastSearchUnified.ResultTypes != "text" {
		t.Errorf("text leg resultTypes = %q, want text", f.lastSearchUnified.ResultTypes)
	}
	if f.lastUnifiedQuery != "alpha" {
		t.Errorf("object leg query = %q, want alpha", f.lastUnifiedQuery)
	}
	if !strings.Contains(body, `data-testid="search-hit-document"`) {
		t.Error("default scope did not render the document hit")
	}
	if !strings.Contains(body, `data-testid="object-card"`) {
		t.Error("default scope did not render the object card")
	}
	if !strings.Contains(body, "/objects/canon-1/wiki") {
		t.Errorf("object card did not link through the resolved id:\n%s", body)
	}
}

// TestUISearchObjectsScope pins the objects-only scope: the object leg runs, the
// text leg does not, and no documents prompt appears.
func TestUISearchObjectsScope(t *testing.T) {
	f := &fakeMemory{
		unifiedResults: []ObjectSearchResult{
			{Object: GraphObject{ID: "obj-1", Type: "Person", Key: "sam-lee"}, Score: 0.6},
		},
	}

	body := searchPageBody(t, f, "/search?q=sam&in=objects")

	if f.lastSearchUnified.Query != "" {
		t.Errorf("objects scope ran the text leg (%q)", f.lastSearchUnified.Query)
	}
	if f.lastUnifiedQuery != "sam" {
		t.Errorf("object leg query = %q, want sam", f.lastUnifiedQuery)
	}
	if !strings.Contains(body, "sam-lee") {
		t.Error("objects scope did not render the object card")
	}
	if strings.Contains(body, `data-testid="search-hit-document"`) {
		t.Error("objects scope rendered a document hit")
	}
	if strings.Contains(body, `data-testid="search-documents-prompt"`) {
		t.Error("objects scope rendered the documents prompt")
	}
}

// TestUISearchLimitNormalization pins the limit contract: offered values pass
// through both legs, anything else falls back to the default (label, submitted
// value and dispatched limit stay in agreement).
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
			t.Errorf("%s: text-leg limit = %d, want %d", target, f.lastSearchUnified.Limit, want)
		}
		if f.lastUnifiedLimit != want {
			t.Errorf("%s: object-leg limit = %d, want %d", target, f.lastUnifiedLimit, want)
		}
	}
}

// TestUISearchUnknownScopeNormalizes pins the scope contract: an unsupported
// `in` searches everything rather than narrowing to nothing, and the control the
// reader sees matches the legs that ran.
func TestUISearchUnknownScopeNormalizes(t *testing.T) {
	f := &fakeMemory{}
	body := searchPageBody(t, f, "/search?q=a&in=bogus")

	if f.lastSearchUnified.Query != "a" {
		t.Errorf("unknown scope skipped the text leg (%+v)", f.lastSearchUnified)
	}
	if f.lastUnifiedQuery != "a" {
		t.Errorf("unknown scope skipped the object leg (%q)", f.lastUnifiedQuery)
	}
	if !strings.Contains(body, `value="all" selected`) {
		t.Error("unknown scope did not render the normalized 'all' selection")
	}
}

// TestUISearchObjectHitOpensWiki pins the link target of an object card: search
// is a list surface, so a hit opens the wiki rather than the editable object
// page. The href carries no variant, so the reader's persisted wiki variant
// decides how it opens.
func TestUISearchObjectHitOpensWiki(t *testing.T) {
	f := &fakeMemory{
		unifiedResults: []ObjectSearchResult{
			{Object: GraphObject{ID: "obj-1", Type: "Person", Key: "sam-lee"}, Score: 0.6},
		},
	}

	body := searchPageBody(t, f, "/search?q=sam&in=objects")

	if !strings.Contains(body, `href="/objects/obj-1/wiki"`) {
		t.Errorf("object card must open its wiki page:\n%s", body)
	}
	if strings.Contains(body, `href="/objects/obj-1"`) {
		t.Error("object card must not land on the editable object page")
	}
	if strings.Contains(body, "/objects/obj-1/wiki?view=") {
		t.Error("the wiki link must not pin a variant; the reader's preference decides")
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

// TestUISearchZeroHitsRendersOneEmptyState pins the empty-state shape: a query
// with no hits anywhere gets one empty state — the object grid's — and neither a
// document empty card nor the documents prompt beside it.
func TestUISearchZeroHitsRendersOneEmptyState(t *testing.T) {
	f := &fakeMemory{}
	body := searchPageBody(t, f, "/search?q=nothinghere")

	if !strings.Contains(body, "No matches for") {
		t.Error("zero-hit page did not render the query empty state")
	}
	if strings.Contains(body, "No document text matched") {
		t.Error("zero-hit page also rendered the document section empty state")
	}
	if strings.Contains(body, `data-testid="search-documents-prompt"`) {
		t.Error("zero-hit page rendered the documents prompt")
	}
}

// TestUISearchDocumentsPromptWhenOnlyObjectsMatch pins the quiet text-leg miss:
// document text matched nothing but objects did, so the page says so in one line
// with the escape to a documents-only search, instead of a full empty card.
func TestUISearchDocumentsPromptWhenOnlyObjectsMatch(t *testing.T) {
	f := &fakeMemory{
		unifiedResults: []ObjectSearchResult{
			{Object: GraphObject{ID: "obj-1", Type: "Note", Key: "a-note"}, Score: 0.6},
		},
	}
	body := searchPageBody(t, f, "/search?q=a")

	if !strings.Contains(body, `data-testid="search-documents-prompt"`) {
		t.Error("missing the documents prompt when only objects matched")
	}
	if !strings.Contains(body, "in=documents") {
		t.Error("the documents prompt must link to a documents-only search")
	}
	if strings.Contains(body, "No chunk of an ingested document") {
		t.Error("the prompt must not render the full document empty card")
	}
}

// TestUISearchDocumentsHaveHitsObjectsDoNot pins the opposite shape: documents
// matched and objects did not, so the document rows stand alone with no object
// empty state implying the whole search failed.
func TestUISearchDocumentsHaveHitsObjectsDoNot(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{{ID: "doc-1", Filename: "notes.md"}},
		searchUnifiedResp: &UnifiedSearchResponse{
			Results: []UnifiedSearchHit{{Type: "text", ID: "c1", DocumentID: "doc-1", Snippet: "alpha", Score: 0.8}},
		},
	}
	body := searchPageBody(t, f, "/search?q=alpha")

	if !strings.Contains(body, `data-testid="search-hit-document"`) {
		t.Error("missing the document hit")
	}
	if strings.Contains(body, "No matches for") {
		t.Error("a document-only result must not render the whole-query empty state")
	}
	if strings.Contains(body, `data-testid="objects-cards"`) {
		t.Error("an empty object leg must not render the object grid")
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
	searchPageBody(t, f, "/search?q=alpha&in=objects")

	if f.effectiveModelCalls != 0 {
		t.Errorf("effective-model lookups = %d, want 0 for an objects-only search", f.effectiveModelCalls)
	}
}

// TestUISearchObjectHitsSkipModelLookup covers the other half of that rule: a
// search that did return results renders no empty-state hint, so it does not
// fetch the model config either.
func TestUISearchObjectHitsSkipModelLookup(t *testing.T) {
	f := &fakeMemory{
		unifiedResults: []ObjectSearchResult{
			{Object: GraphObject{ID: "obj-1", Type: "Note", Key: "a-note"}, Score: 0.6},
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

// TestSplitDocumentHits covers the text-leg normalization directly: text items
// are kept, graph and relationship items are dropped (the object leg renders
// those), and a snippet's whitespace is collapsed.
func TestSplitDocumentHits(t *testing.T) {
	docs := splitDocumentHits([]UnifiedSearchHit{
		{Type: "text", ID: "c1", DocumentID: "d1", Snippet: "a\n\n  b  ", Mode: "lexical", Score: 0.4},
		{Type: "graph", ID: "obj-1", ObjectID: "obj-1", ObjectType: "Note", Key: "k", Score: 0.3},
		{Type: "relationship", ID: "rel-1", Score: 0.1},
	})

	if len(docs) != 1 {
		t.Fatalf("documents = %d, want 1", len(docs))
	}
	if docs[0].Snippet != "a b" {
		t.Errorf("snippet = %q, want whitespace collapsed", docs[0].Snippet)
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
