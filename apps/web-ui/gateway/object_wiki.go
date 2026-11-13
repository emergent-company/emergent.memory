package main

import (
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- object wiki --------------------------------------------------------------
//
// The wiki is reading-only surface, separate from the object's editable page.
// Browsing (the card grid and its search results) links here, and the page
// renders in one of two variants:
//
//   - article — the prose-led layout: a lead paragraph, the markdown body, and a
//     details rail.
//   - page — the object-page layout: key facts, an overview, and one section per
//     relationship group.
//
// The variant is a reader preference, not a property of the object: it is a
// cookie so it follows the reader from one object to the next, and a `?view=`
// query parameter so a shared link opens in the variant the sender was reading.
// The editable page keeps its own route (/objects/:id) and is what the wiki's
// Edit action opens.

const (
	objectWikiViewArticle = "article"
	objectWikiViewPage    = "page"

	// objectWikiViewCookie stores the reader's variant. It is HttpOnly: only the
	// server reads it, and every control that changes it is a link.
	objectWikiViewCookie = "memory_object_wiki_view"
	// objectWikiViewCookieMaxAge is one year, in seconds.
	objectWikiViewCookieMaxAge = 365 * 24 * 60 * 60
)

// objectWikiViewOption is one entry of the variant switch, in render order.
type objectWikiViewOption struct {
	Value string
	Label string
	Icon  string
}

// objectWikiViewOptions is the switch's entries. The default variant is first.
func objectWikiViewOptions() []objectWikiViewOption {
	return []objectWikiViewOption{
		{Value: objectWikiViewArticle, Label: "Article", Icon: "lucide--file-text"},
		{Value: objectWikiViewPage, Label: "Object page", Icon: "lucide--layout-grid"},
	}
}

// normalizeObjectWikiView maps any input to a known variant, defaulting to the
// article. An unrecognised value must never render an empty page.
func normalizeObjectWikiView(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), objectWikiViewPage) {
		return objectWikiViewPage
	}
	return objectWikiViewArticle
}

// objectWikiViewPreference resolves the reader's variant: an explicit value on
// the query wins, then the cookie, then the default. explicit reports whether the
// request named a variant, which is when the handler should persist it.
func objectWikiViewPreference(c echo.Context) (view string, explicit bool) {
	for _, param := range []string{"view", "wiki_view"} {
		if v := strings.TrimSpace(c.QueryParam(param)); v != "" {
			return normalizeObjectWikiView(v), true
		}
	}
	if cookie, err := c.Cookie(objectWikiViewCookie); err == nil && cookie.Value != "" {
		return normalizeObjectWikiView(cookie.Value), false
	}
	return objectWikiViewArticle, false
}

// setObjectWikiViewPreference persists the variant for later pages. The write is
// best-effort: a refused cookie (a browser with cookies off) still renders the
// requested variant for this request.
func setObjectWikiViewPreference(c echo.Context, view string) {
	c.SetCookie(&http.Cookie{
		Name:     objectWikiViewCookie,
		Value:    normalizeObjectWikiView(view),
		Path:     "/",
		MaxAge:   objectWikiViewCookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// objectWikiHref is the wiki URL for one object in one variant.
//
// The variant is omitted when it is the default article (the article is what
// /wiki renders when nothing says otherwise), so a plain link stays short and a
// link that has no opinion opens whatever the reader last chose. Pass "" for
// exactly that: no opinion, the reader's persisted variant decides.
func objectWikiHref(id, view string) string {
	q := url.Values{}
	if v := normalizeObjectWikiView(view); v != objectWikiViewArticle {
		q.Set("view", v)
	}
	if len(q) == 0 {
		return "/objects/" + url.PathEscape(id) + "/wiki"
	}
	return "/objects/" + url.PathEscape(id) + "/wiki?" + q.Encode()
}

// objectWikiControlHref is objectWikiHref for a variant control (the switch, a
// relationship link): it always spells the variant out, even the default one.
//
// An omitted default would be read back from the reader's cookie, so a reader
// whose stored variant is "page" could never switch back to the article — the
// link would look identical to the page they are already on. Card links keep the
// short form (objectWikiHref) because they are not controls: they open whatever
// the reader last chose.
func objectWikiControlHref(id, view string) string {
	q := url.Values{}
	q.Set("view", normalizeObjectWikiView(view))
	return "/objects/" + url.PathEscape(id) + "/wiki?" + q.Encode()
}

// objectsWikiViewHref is the browse page's switch target: the current filter
// query with the chosen variant added. It is query-only (no path), which keeps it
// correct on /objects, on the agent-scoped route, and in the gallery preview —
// the same rule objectsSortHref follows. The handler persists the variant as a
// cookie, so the choice survives the next visit to a bare /objects.
func objectsWikiViewHref(data objectsPageData, view string) string {
	q, _ := url.ParseQuery(strings.TrimPrefix(objectsSortHref(data, data.Order), "?"))
	q.Set("wiki_view", normalizeObjectWikiView(view))
	return "?" + q.Encode()
}

// objectWikiRelationCount is one row of the rail's "Relationships by type"
// summary: the relationship type, its human label, and how many edges carry it.
type objectWikiRelationCount struct {
	Type  string
	Label string
	Count int
}

// objectWikiRelationCounts counts edges by relationship type, most frequent
// first, ties broken by label so the rail is stable between renders.
func objectWikiRelationCounts(edges []GraphRelationship) []objectWikiRelationCount {
	counts := make(map[string]int, len(edges))
	for _, edge := range edges {
		if edge.Type == "" {
			continue
		}
		counts[edge.Type]++
	}
	out := make([]objectWikiRelationCount, 0, len(counts))
	for typ, n := range counts {
		out = append(out, objectWikiRelationCount{Type: typ, Label: relationshipTypeLabel(typ), Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// objectWikiRelationSections groups edges by relationship type for a type that
// declares no ui.view. The object-page variant should still show its
// relationships: without a declared view the page groups by the edge type
// itself, which is the honest heading available. Each group is bounded so a hub
// object with a hundred edges cannot make the page unreadable; the section's
// Total keeps the real count visible.
func objectWikiRelationSections(obj *GraphObject, edges []GraphRelationship, related []GraphObject, typeUIByType map[string]typeUI) []objectViewSection {
	if obj == nil || len(edges) == 0 {
		return nil
	}
	groups := make(map[string][]objectViewItem, len(edges))
	order := make([]string, 0, len(edges))
	for _, edge := range edges {
		item, ok := objectViewItemFor(edge, obj, related, typeUIByType)
		if !ok {
			continue
		}
		if _, seen := groups[edge.Type]; !seen {
			order = append(order, edge.Type)
		}
		groups[edge.Type] = append(groups[edge.Type], item)
	}
	sort.SliceStable(order, func(i, j int) bool {
		if len(groups[order[i]]) != len(groups[order[j]]) {
			return len(groups[order[i]]) > len(groups[order[j]])
		}
		return relationshipTypeLabel(order[i]) < relationshipTypeLabel(order[j])
	})
	out := make([]objectViewSection, 0, len(order))
	for _, typ := range order {
		rows := groups[typ]
		out = append(out, objectViewGroup{
			Title: relationshipTypeLabel(typ),
			Rows:  sortViewRows(rows, 0),
			Limit: objectWikiSectionLimit,
		}.section(objectWikiSectionLimit))
	}
	return out
}

// objectWikiSectionLimit is the collapsed row budget of an undeclared section in
// the object-page variant: enough rows to read, few enough that a hub page stays
// affordable (the mockup's own 187-edge example is why this needs a bound).
const objectWikiSectionLimit = 8

// objectWikiPageData is everything the wiki page renders. It is deliberately not
// objectDetailShellData: the wiki has no workflow editor, no sub-nav and no
// mutation controls, so reusing the editor's shell would carry dead weight.
type objectWikiPageData struct {
	Obj          *GraphObject
	Branch       objectBranchRef
	TypeUIByType map[string]typeUI
	Status       *objectStatusControl
	TypeLabel    string
	PropDefs     []objectPropertyDef
	Sources      []objectSource
	// Sections are the relationship groups: the type's declared ui.view when it
	// has one, otherwise one group per relationship type.
	Sections []objectViewSection
	// RelationCounts summarises every edge by type for the rail.
	RelationCounts []objectWikiRelationCount
	EdgeCount      int
	// View is the variant being rendered ("article" | "page").
	View    string
	LoadErr error
}

// uiObjectWiki renders the wiki page in the reader's chosen variant.
func (s *Server) uiObjectWiki(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	view, explicit := objectWikiViewPreference(c)
	if explicit {
		setObjectWikiViewPreference(c, view)
	}

	data := objectWikiPageData{View: view}
	shell := s.loadObjectDetailShell(ctx, id, "")
	if shell.LoadErr != nil {
		data.LoadErr = shell.LoadErr
		return s.page(c, pageTitle("Object"), objectWikiPage(data))
	}

	data.Obj = shell.Obj
	data.Branch = shell.Branch
	data.TypeUIByType = shell.TypeUIByType
	data.Status = shell.Status
	data.TypeLabel = shell.TypeLabel
	data.Sources = shell.Sources
	data.EdgeCount = shell.EdgeCount
	data.RelationCounts = objectWikiRelationCounts(shell.Edges)
	if shell.Compiled != nil {
		data.PropDefs = compiledTypePropertyDefs(shell.Compiled.ObjectTypes, shell.Obj.Type)
	}

	if shell.EdgesErr == nil && len(shell.Edges) > 0 {
		related := s.loadRelatedObjects(ctx, shell.Obj, shell.Edges)
		if viewCfg := typeUIOf(shell.TypeUIByType, shell.Obj.Type).View; viewCfg != nil {
			data.Sections = resolveObjectViewSections(viewCfg, shell.Obj, shell.Edges, related, shell.TypeUIByType)
		}
		if len(data.Sections) == 0 {
			data.Sections = objectWikiRelationSections(shell.Obj, shell.Edges, related, shell.TypeUIByType)
		}
	}

	return s.page(c, pageTitle(graphObjectLabel(*data.Obj)+" · Wiki"), objectWikiPage(data))
}

// objectRailPropertyRows is objectTypePropertyRows filtered for a reading rail:
// the properties the prose rendered as its lead or body are dropped, because
// repeating them beside the prose makes the page twice as long and teaches the
// reader nothing.
func objectRailPropertyRows(o GraphObject, propDefs []objectPropertyDef, prose objectArticleProse) []objectTypePropertyRow {
	rows := objectTypePropertyRows(o, propDefs)
	skip := make(map[string]bool, 2)
	for _, k := range []string{prose.LeadKey, prose.BodyKey} {
		if k != "" {
			skip[normalizeProseKey(k)] = true
		}
	}
	if len(skip) == 0 {
		return rows
	}
	out := make([]objectTypePropertyRow, 0, len(rows))
	for _, r := range rows {
		if skip[normalizeProseKey(r.Name)] {
			continue
		}
		out = append(out, r)
	}
	return out
}

// objectPropertyDisplay renders a property row's value on one line for a
// details rail: a scalar as written, an array as a comma-separated list, capped
// so a long value cannot dominate the rail. The full value stays reachable on
// the editable page, which is where a value is meant to be edited.
func objectPropertyDisplay(r objectTypePropertyRow) string {
	if len(r.Values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.Values))
	for _, v := range r.Values {
		if v = strings.TrimSpace(v); v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return truncateProseRunes(proseSpaces.ReplaceAllString(strings.Join(parts, ", "), " "), 300)
}
