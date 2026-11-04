package main

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

// --- Project search ---
//
// /search is the one text-query surface that spans the whole project: document
// chunks (full-text + vector, fused server-side) and graph objects. It is the
// only place the UI renders POST /api/search/unified's text hits — the objects
// browser uses the same endpoint but asks for graph results only, so a project
// with documents and no graph returns nothing there.
//
// Scope is project-wide: chunk and object hits both come from the request's
// resolved project, so no per-hit authorization decision is made here.

const (
	// searchTypeAll/Documents/Objects are the URL values of the type filter.
	searchTypeAll       = "all"
	searchTypeDocuments = "documents"
	searchTypeObjects   = "objects"

	// defaultSearchLimit is the fused result cap the page asks for; the
	// endpoint has no offset, so the limit is the whole pagination story.
	defaultSearchLimit = 25

	// searchLimitOptions are the limits the select offers. The server clamps to
	// SEARCH_MAX_LIMIT (100 by default), so the UI stays well below it: a search
	// page that renders 100 rows is a scroll, not a scan.
	searchLimitMin = 10
	searchLimitMid = 25
	searchLimitMax = 50

	// searchSnippetRunes bounds a rendered chunk. The endpoint returns the
	// chunk's whole text, which can run to a couple of thousand characters; the
	// row keeps the first few hundred so hits stay comparable at a glance.
	searchSnippetRunes = 400

	// searchNameLookupConcurrency bounds the per-render document lookups. Names
	// are not part of the search response, so they are resolved per distinct
	// document in the page; a small pool keeps the fan-out from competing with
	// the request that produced the hits.
	searchNameLookupConcurrency = 8
)

// searchDocumentHit is one document-chunk hit, ready to render.
type searchDocumentHit struct {
	DocumentID string
	// Name is the resolved document filename. Empty when the lookup failed or
	// the document has no filename; searchDocumentLabel falls back to the id.
	Name    string
	Snippet string
	// Mode is the text leg's own label for how the chunk matched: "hybrid"
	// (keyword + vector), "lexical" (keyword only) or "vector" (vector only).
	Mode  string
	Score float32
}

// searchObjectHit is one graph-object hit, ready to render.
type searchObjectHit struct {
	ID    string
	Type  string
	Key   string
	Score float32
}

// searchPageData is the Search page's view model.
type searchPageData struct {
	Query string
	// Type is the normalized filter: all | documents | objects.
	Type  string
	Limit int

	Documents []searchDocumentHit
	Objects   []searchObjectHit
	Meta      UnifiedSearchMetadata
	LoadErr   error

	// EmbeddingModelMissing is set only when a query returned no document hits
	// and the project has no embedding model configured — the one actionable
	// reason a semantic search can come back empty. Never set on a successful
	// run that found chunks, and never set for the objects-only filter.
	EmbeddingModelMissing bool
}

// uiSearch renders the project search page. An empty query renders the
// explainer without touching the backend; a query runs one unified search and
// splits the fused hits into the document and object sections.
func (s *Server) uiSearch(c echo.Context) error {
	ctx := c.Request().Context()
	data := searchPageData{
		Query: strings.TrimSpace(c.QueryParam("q")),
		Type:  normalizeSearchType(c.QueryParam("type")),
		Limit: normalizeSearchLimit(c.QueryParam("limit")),
	}
	if data.Query == "" {
		return s.page(c, pageTitle("Search"), SearchPage(data))
	}

	resp, err := s.memory.SearchUnified(ctx, UnifiedSearchParams{
		Query:       data.Query,
		ResultTypes: searchResultTypes(data.Type),
		Limit:       data.Limit,
	})
	if err != nil {
		data.LoadErr = userFacingError(err)
		return s.page(c, pageTitle("Search"), SearchPage(data))
	}

	data.Meta = resp.Metadata
	data.Documents, data.Objects = splitSearchHits(resp.Results)
	s.resolveDocumentNames(ctx, data.Documents)

	// Only the empty state can act on this, so fetch it only when the whole
	// search came back empty — and only for a search that could have matched
	// document text: the objects-only filter cannot be affected by a missing
	// embedding model.
	if len(data.Documents) == 0 && len(data.Objects) == 0 && data.Type != searchTypeObjects {
		if modelCfg, modelErr := s.memory.GetEffectiveModelConfig(ctx); modelErr == nil && modelCfg != nil {
			data.EmbeddingModelMissing = strings.TrimSpace(modelCfg.EmbeddingModel) == ""
		}
	}

	return s.page(c, pageTitle("Search"), SearchPage(data))
}

// splitSearchHits normalizes the unified endpoint's union items into the two
// sections the page renders. Relationship items are dropped: the request
// disables that leg, and the page has no section for them.
func splitSearchHits(items []UnifiedSearchHit) (documents []searchDocumentHit, objects []searchObjectHit) {
	for _, it := range items {
		switch it.Type {
		case "text":
			documents = append(documents, searchDocumentHit{
				DocumentID: it.DocumentID,
				Snippet:    trimSearchSnippet(it.Snippet),
				Mode:       it.Mode,
				Score:      it.Score,
			})
		case "graph":
			id := firstNonEmpty(it.CanonicalID, it.ObjectID, it.ID)
			if id == "" {
				continue
			}
			objects = append(objects, searchObjectHit{
				ID:    id,
				Type:  it.ObjectType,
				Key:   it.Key,
				Score: it.Score,
			})
		}
	}
	return documents, objects
}

// resolveDocumentNames fills in each hit's document filename. Search responses
// carry a document id but no name, and a chunk row that only shows a UUID is
// not usable, so the page resolves the distinct documents it references.
// Failures are silent: the row falls back to "Document <id>".
func (s *Server) resolveDocumentNames(ctx context.Context, hits []searchDocumentHit) {
	ids := make([]string, 0, len(hits))
	seen := make(map[string]bool, len(hits))
	for _, h := range hits {
		if h.DocumentID == "" || seen[h.DocumentID] {
			continue
		}
		seen[h.DocumentID] = true
		ids = append(ids, h.DocumentID)
	}
	if len(ids) == 0 {
		return
	}

	names := make(map[string]string, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, searchNameLookupConcurrency)
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			doc, err := s.memory.GetDocument(ctx, id)
			if err != nil || doc == nil {
				return
			}
			mu.Lock()
			names[id] = documentName(*doc)
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	for i := range hits {
		if name, ok := names[hits[i].DocumentID]; ok {
			hits[i].Name = name
		}
	}
}

// trimSearchSnippet collapses a chunk's whitespace and cuts it to a readable
// length, so every hit occupies a comparable few lines.
func trimSearchSnippet(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(collapsed) <= searchSnippetRunes {
		return collapsed
	}
	runes := []rune(collapsed)
	return strings.TrimSpace(string(runes[:searchSnippetRunes])) + "…"
}

// normalizeSearchType coerces a raw `type` value to one of the three filters.
// An absent or unrecognised value falls back to all, so the rendered selection,
// the value the form submits and the dispatched search always agree.
func normalizeSearchType(raw string) string {
	switch raw {
	case searchTypeDocuments, searchTypeObjects:
		return raw
	default:
		return searchTypeAll
	}
}

// normalizeSearchLimit coerces a raw `limit` value to the offered options.
func normalizeSearchLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultSearchLimit
	}
	for _, allowed := range []int{searchLimitMin, searchLimitMid, searchLimitMax} {
		if n == allowed {
			return n
		}
	}
	return defaultSearchLimit
}

// searchResultTypes maps the page's filter onto the endpoint's result types.
func searchResultTypes(searchType string) string {
	switch searchType {
	case searchTypeDocuments:
		return "text"
	case searchTypeObjects:
		return "graph"
	default:
		return "both"
	}
}

// searchTypeOptions returns the filter options in display order (widest first).
func searchTypeOptions() []searchTypeOption {
	return []searchTypeOption{
		{Value: searchTypeAll, Label: "Everything"},
		{Value: searchTypeDocuments, Label: "Documents"},
		{Value: searchTypeObjects, Label: "Objects"},
	}
}

// searchTypeOption is one entry of the type filter.
type searchTypeOption struct {
	Value string
	Label string
}

// searchLimitOptions returns the limit choices with their labels.
func searchLimitOptions() []searchLimitOption {
	return []searchLimitOption{
		{Value: searchLimitMin, Label: "10"},
		{Value: searchLimitMid, Label: "25"},
		{Value: searchLimitMax, Label: "50"},
	}
}

// searchLimitOption is one entry of the limit select.
type searchLimitOption struct {
	Value int
	Label string
}

// searchDocumentLabel is the row's document name: the resolved filename, or a
// shortened id when the lookup failed (deleted document, transient error).
func searchDocumentLabel(hit searchDocumentHit) string {
	if hit.Name != "" {
		return hit.Name
	}
	return "Document " + shortID(hit.DocumentID)
}

// searchModeLabel explains how a chunk matched. The endpoint reports one mode
// for the whole text leg, but chunk rows carry it per hit, so the badge is
// honest about a degraded (keyword-only) search.
func searchModeLabel(mode string) string {
	switch mode {
	case "hybrid":
		return "Keyword + semantic"
	case "lexical":
		return "Keyword"
	case "vector":
		return "Semantic"
	default:
		return ""
	}
}

// searchObjectLabel is the row's object name: its key, or a shortened id when
// the object has no key.
func searchObjectLabel(hit searchObjectHit) string {
	if hit.Key != "" {
		return hit.Key
	}
	return "Object " + shortID(hit.ID)
}

// searchEmptyDescription is the empty state's explanation. It names the filter
// the search ran under, and — when the project has no embedding model — says
// why the semantic half of the search could not contribute.
func searchEmptyDescription(data searchPageData) string {
	var desc string
	switch data.Type {
	case searchTypeDocuments:
		desc = "No chunk of an ingested document matched. Try different words: documents become searchable once they are converted and chunked."
	case searchTypeObjects:
		desc = "No knowledge-graph entity matched. Try different words, or search everything to include document text."
	default:
		desc = "Nothing in this project matched. Try different words, or narrow the search to documents or objects."
	}
	if data.EmbeddingModelMissing {
		desc += " This project has no embedding model configured, so only keyword matching ran — set one to match by meaning as well."
	}
	return desc
}
