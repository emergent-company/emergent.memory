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
// The wiki is the reading surface, separate from the object's editable page.
// Browsing (the card grid and its search results) links here. There is exactly
// one layout — the object page: key facts, an overview, and one section per
// relationship group — and the editable page keeps its own route (/objects/:id),
// which is what the wiki's Edit action opens.

// objectWikiHref is the wiki URL for one object.
//
// It is the one link helper every surface uses (browse cards, search hits,
// relation rows, document and work rows), so a card and the page it opens can
// never disagree about the URL.
func objectWikiHref(id string) string {
	return "/objects/" + url.PathEscape(id) + "/wiki"
}

// objectWikiLegacyViewParams are the retired variant selectors. A request that
// still carries one is redirected to the bare URL rather than silently served
// the page view: `?view=article` used to mean something, and answering it with
// different content than the sender expects is worse than a clean 301.
var objectWikiLegacyViewParams = []string{"view", "wiki_view"}

// objectWikiRelationCount is one row of the rail's "Relationships by type"
// summary: the relationship type, its human label, and how many edges carry it.
type objectWikiRelationCount struct {
	Type  string
	Label string
	Count int
}

// objectWikiRailOptions is retired: with a single wiki layout the rail's blocks
// are fixed (no property dump — the body's key-facts table lists them — and the
// per-type relationship breakdown, because this is the whole-object view).

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
	// Provenance is the write stamp of the version on screen — who wrote it, how
	// they were authenticated, and when — resolved against Identities. It is the
	// last entry of the rail's provenance list; every field degrades to "".
	Provenance objectVersionProvenance
	// Identities resolves actor ids to names for Provenance. Best-effort: a
	// failed lookup leaves the entry naming the actor by type and short id.
	Identities []objectActorOption
	LoadErr    error
}

// uiObjectWiki renders the wiki page. It is the reading surface, so it takes no
// query: a request that still carries a retired variant selector (view=,
// wiki_view=) is redirected to the bare URL instead of being served a layout it
// did not ask for.
func (s *Server) uiObjectWiki(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	for _, param := range objectWikiLegacyViewParams {
		if strings.TrimSpace(c.QueryParam(param)) != "" {
			return c.Redirect(http.StatusMovedPermanently, objectWikiHref(id))
		}
	}

	var data objectWikiPageData
	// The wiki reads: its Workflow rail group is read-only and shows the stored
	// assignee name, so there is no workflow panel and no assignee-candidate
	// fetch (the pickers live on the object page it links to).
	shell := s.loadObjectDetailShell(ctx, id, "", objectDetailSurfaceReader)
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
	// The rail names the version's writer, so resolve the actor names the read
	// path cannot: one best-effort lookup, and only when the version carries a
	// user or agent actor (a system write needs no lookup).
	identities, orderers := s.objectProvenanceInputs(ctx, []GraphObject{*shell.Obj})
	data.Identities = identities
	data.Provenance = objectProvenanceOf(*shell.Obj, identities, orderers)
	if shell.Compiled != nil {
		data.PropDefs = compiledTypePropertyDefs(shell.Compiled.ObjectTypes, shell.Obj.Type)
	}

	if objectEdgesReadable(shell) {
		// The wiki's own question is the edge half, not objectViewRenderable:
		// a type that declares no ui.view still gets one section per
		// relationship type (objectWikiRelationSections), so gating on a
		// declared view would empty the Relationships area on every type that
		// has not adopted one. The declared view only chooses the grouping.
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

// objectPropertyRowsSplit partitions property rows by whether they render a
// value. Both property surfaces on the wiki page (the Key facts table and the
// rail) fold the second group behind the eye: a field the type declares and
// nobody filled is worth knowing about, but as a wall of em dashes it buries the
// fields that do carry a value.
func objectPropertyRowsSplit(rows []objectTypePropertyRow) (valued, unset []objectTypePropertyRow) {
	valued = make([]objectTypePropertyRow, 0, len(rows))
	unset = make([]objectTypePropertyRow, 0, len(rows))
	for _, r := range rows {
		if objectPropertyDisplay(r) == "" {
			unset = append(unset, r)
			continue
		}
		valued = append(valued, r)
	}
	return valued, unset
}
