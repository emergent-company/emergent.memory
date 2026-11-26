package main

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

// --- Project search (the /search page) ---
//
// /search is the one list page that spans the whole project: it browses the
// knowledge graph's objects, and a query runs two legs over one query string —
// document chunks (full-text + vector, fused server-side) and graph objects
// (ranked by the same unified endpoint, graph results only). /objects is retired
// as a destination and forwards here, because two pages showing the same object
// cards with one of them unable to search document text was two answers to one
// question.
//
// Scope is project-wide: chunk and object hits both come from the request's
// resolved project, so no per-hit authorization decision is made here. The
// `in` filter narrows which leg runs; the object-type `type` filter narrows the
// object leg only.

const (
	// searchScopeAll/Documents/Objects are the URL values of the `in` scope
	// filter.
	searchScopeAll       = "all"
	searchScopeDocuments = "documents"
	searchScopeObjects   = "objects"

	// defaultSearchLimit is the fused result cap the page asks for; the
	// endpoint has no offset, so the limit is the whole pagination story for a
	// query. Browsing is cursor-paginated at a fixed page size instead.
	defaultSearchLimit = 25

	// searchLimitOptions are the limits the select offers. The server clamps to
	// SEARCH_MAX_LIMIT (100 by default), so the UI stays well below it: a page
	// that renders 100 rows is a scroll, not a scan.
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

// uiSearch renders the search page: browse objects, or run one query across
// document text and graph objects. An empty query renders the browse list and
// never touches the search endpoints; a query runs the legs its scope allows and
// renders one document section above the object cards.
func (s *Server) uiSearch(c echo.Context) error {
	ctx := c.Request().Context()
	query := strings.TrimSpace(c.QueryParam("q"))
	scope := normalizeSearchScope(c.QueryParam("in"))
	limit := normalizeSearchLimit(c.QueryParam("limit"))
	typeFilter := c.QueryParam("type")
	branchID := c.QueryParam("branch")
	actorType, actorID := objectActorFilter(c)

	data := objectsPageData{
		Query:        query,
		In:           scope,
		Limit:        limit,
		TypeFilter:   typeFilter,
		BranchID:     branchID,
		ActorType:    actorType,
		ActorID:      actorID,
		Provenance:   effectiveObjectProvenance(c.QueryParam("provenance"), actorType),
		CredentialID: strings.TrimSpace(c.QueryParam("credential_id")),
		ViaShare:     objectViaShareLabel(c.QueryParam("via")),
		Order:        normalizeObjectOrder(c.QueryParam("order")),
		FlashMsg:     objectsBulkFlash(c),
		FlashErr:     flashError(c),
	}
	s.loadObjectFilterOptions(ctx, &data)

	if query == "" {
		return s.searchBrowse(c, ctx, data)
	}

	// An object id is a lookup, not a query (see parseObjectSearchQuery). The
	// ranked legs would answer the id's meaningless embedding with its nearest
	// neighbours, so resolve it exactly and render that one object. A
	// documents-only scope is left alone: it asked for document text, and an
	// object id cannot answer that.
	if target := parseObjectSearchQuery(query); scope != searchScopeDocuments && (target.ID != "" || target.InvalidID != "") {
		results, lookup, err := s.objectSearchIDLookup(ctx, target, typeFilter, branchID)
		if err != nil {
			data.LoadErr = userFacingError(err)
			return s.renderObjects(c, pageTitle("Search"), data)
		}
		data.Results, data.Lookup = results, lookup
		if len(results) > 0 {
			shown := make([]GraphObject, 0, len(results))
			for _, r := range results {
				shown = append(shown, r.Object)
			}
			data.ActorOptions, data.RunOrderers = s.objectProvenanceInputs(ctx, shown)
		}
		return s.renderObjects(c, pageTitle("Search"), data)
	}

	// Text leg: documents only. The unified endpoint's graph leg is not used
	// here because the object cards need the hydrated objects SearchObjectsUnified
	// returns; running both would fetch the graph twice, and the text leg alone
	// is what this section renders.
	if scope != searchScopeObjects {
		resp, err := s.memory.SearchUnified(ctx, UnifiedSearchParams{
			Query:       query,
			ResultTypes: "text",
			Limit:       limit,
		})
		if err != nil {
			data.LoadErr = userFacingError(err)
			return s.renderObjects(c, pageTitle("Search"), data)
		}
		data.Meta = resp.Metadata
		data.Documents = splitDocumentHits(resp.Results)
		s.resolveDocumentNames(ctx, data.Documents)
	}

	// Object leg: the same ranked, hydrated hits the objects browser rendered,
	// now on the search page. The relevance floor and the ?all=1 disclosure are
	// unchanged (see objectSearchResultsShown).
	if scope != searchScopeDocuments {
		results, err := s.memory.SearchObjectsUnified(ctx, query, typeFilter, branchID, limit)
		if err != nil {
			data.LoadErr = userFacingError(err)
			return s.renderObjects(c, pageTitle("Search"), data)
		}
		data.Results = objectSearchResultsShown(results, objectSearchShowsAll(c.QueryParam("all")))
		data.HiddenCount = len(results) - len(data.Results)
		// The enriched hits carry a real write stamp, so a user or agent writer
		// needs a name to render — and when an agent wrote the hit, the person
		// whose order it acted on needs one too. Only what the page references is
		// fetched: a page of extraction-written objects (actor "system", no
		// orderer) costs no extra request.
		if len(data.Results) > 0 {
			shown := make([]GraphObject, 0, len(data.Results))
			for _, r := range data.Results {
				shown = append(shown, r.Object)
			}
			data.ActorOptions, data.RunOrderers = s.objectProvenanceInputs(ctx, shown)
		}
	}

	// Only the empty state can act on this, so fetch it only when the whole
	// search came back empty — and only for a search that could have matched
	// document text: the objects-only scope cannot be affected by a missing
	// embedding model.
	if len(data.Documents) == 0 && len(data.Results) == 0 && scope != searchScopeObjects {
		if modelCfg, modelErr := s.memory.GetEffectiveModelConfig(ctx); modelErr == nil && modelCfg != nil {
			data.EmbeddingModelMissing = strings.TrimSpace(modelCfg.EmbeddingModel) == ""
		}
	}

	return s.renderObjects(c, pageTitle("Search"), data)
}

// searchBrowse loads the unqueried list: one page of objects, most-recent-first,
// with the actor identities the browse filter needs. Stats stay deferred (see
// ObjectsPage) because they are slow and independent of this render.
func (s *Server) searchBrowse(c echo.Context, ctx context.Context, data objectsPageData) error {
	s.loadObjectActorOptions(ctx, &data)
	if err := s.loadObjectBrowsePage(ctx, &data, c.QueryParam("cursor")); err != nil {
		data.LoadErr = userFacingError(err)
		return s.renderObjects(c, pageTitle("Search"), data)
	}
	return s.renderObjects(c, pageTitle("Search"), data)
}

// splitDocumentHits normalizes the text leg's items into document rows.
// Relationship and graph items are dropped: the request asked for text only, and
// the page renders the object leg from its own call.
func splitDocumentHits(items []UnifiedSearchHit) []searchDocumentHit {
	var documents []searchDocumentHit
	for _, it := range items {
		if it.Type != "text" {
			continue
		}
		documents = append(documents, searchDocumentHit{
			DocumentID: it.DocumentID,
			Snippet:    trimSearchSnippet(it.Snippet),
			Mode:       it.Mode,
			Score:      it.Score,
		})
	}
	return documents
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

// searchScopeOf returns the page's normalized scope. Reading it from the data
// rather than the request keeps every control and href agreeing with the leg the
// handler actually ran.
func searchScopeOf(data objectsPageData) string {
	return normalizeSearchScope(data.In)
}

// searchLimitOf returns the page's normalized result cap.
func searchLimitOf(data objectsPageData) int {
	if data.Limit <= 0 {
		return defaultSearchLimit
	}
	return normalizeSearchLimit(strconv.Itoa(data.Limit))
}

// normalizeSearchScope coerces a raw `in` value to one of the three scopes. An
// absent or unrecognised value falls back to all, so the rendered selection, the
// value the form submits and the dispatched search always agree.
func normalizeSearchScope(raw string) string {
	switch raw {
	case searchScopeDocuments, searchScopeObjects:
		return raw
	default:
		return searchScopeAll
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

// searchScopeOptions returns the scope options in display order (widest first).
func searchScopeOptions() []searchTypeOption {
	return []searchTypeOption{
		{Value: searchScopeAll, Label: "Everything"},
		{Value: searchScopeDocuments, Label: "Documents"},
		{Value: searchScopeObjects, Label: "Objects"},
	}
}

// searchTypeOption is one entry of the scope filter.
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

// searchDocumentsOnlyHref is the "search documents only" escape from the
// documents prompt: the same query, narrowed to the text leg.
func searchDocumentsOnlyHref(data objectsPageData) string {
	q := url.Values{}
	q.Set("q", data.Query)
	q.Set("in", searchScopeDocuments)
	if data.Limit > 0 && data.Limit != defaultSearchLimit {
		q.Set("limit", strconv.Itoa(data.Limit))
	}
	return "/search?" + q.Encode()
}

// searchEmptyDescription is the empty state's explanation. It names the scope
// the search ran under, and — when the project has no embedding model — says why
// the semantic half of the search could not contribute.
func searchEmptyDescription(data objectsPageData) string {
	var desc string
	switch searchScopeOf(data) {
	case searchScopeDocuments:
		desc = "No chunk of an ingested document matched. Try different words: documents become searchable once they are converted and chunked."
	case searchScopeObjects:
		desc = "No knowledge-graph entity matched. Try different words, or search everything to include document text."
	default:
		desc = "Nothing in this project matched. Try different words — or paste an object's full id (or write id:<id>) to open that object exactly."
	}
	if data.EmbeddingModelMissing {
		desc += " This project has no embedding model configured, so only keyword matching ran — set one to match by meaning as well."
	}
	return desc
}
