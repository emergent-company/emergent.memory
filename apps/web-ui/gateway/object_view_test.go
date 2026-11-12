package main

import (
	"strconv"
	"testing"
)

// viewTestObject builds a resolved neighbour as the gateway holds it: canonical
// id set, a type, and a display name.
func viewTestObject(id, typ, name string, props map[string]any) GraphObject {
	return GraphObject{ID: id, CanonicalID: id, Type: typ, Name: name, Properties: props}
}

// viewTestEdge builds an edge from src to dst. The real endpoint returns
// canonical ids, which is what these tests reproduce.
func viewTestEdge(id, typ, src, dst string) GraphRelationship {
	return GraphRelationship{ID: id, Type: typ, SrcID: src, DstID: dst}
}

// TestResolveObjectViewSections is the core contract of a type-declared view:
// declared sections collect the right edges in the right direction, every group
// is bounded and reports what it truncated, and edges no section claimed stay
// visible in the trailing group instead of disappearing.
func TestResolveObjectViewSections(t *testing.T) {
	prop := GraphObject{ID: "prop-v4", CanonicalID: "prop", Type: "Property", Name: "Home — Waliły 29"}
	related := []GraphObject{
		viewTestObject("doc-1", "Document", "Tax decision", map[string]any{"document_kind": "tax_decision", "document_date": "2026-01-26"}),
		viewTestObject("doc-2", "Document", "Circuit map", nil),
		viewTestObject("doc-3", "Document", "Roofing contract", nil),
		viewTestObject("util-1", "UtilityService", "Water — ZGK", map[string]any{"status": "active", "provider": "ZGK"}),
		viewTestObject("util-2", "UtilityService", "Electricity — PGE", nil),
		viewTestObject("item-1", "Item", "Heat pump", map[string]any{"status": "active"}),
	}
	edges := []GraphRelationship{
		viewTestEdge("e1", "concerns", "doc-1", "prop"),
		viewTestEdge("e2", "concerns", "doc-2", "prop"),
		viewTestEdge("e3", "concerns", "doc-3", "prop"),
		viewTestEdge("e4", "serves", "util-1", "prop"),
		// An outgoing edge of a sectioned type: the section asks for incoming
		// only, so this one must not appear under Utilities.
		viewTestEdge("e5", "serves", "prop", "util-2"),
		// A type no section declares: it must survive into "More connections".
		viewTestEdge("e6", "kept_at", "item-1", "prop"),
	}
	view := &typeUIView{
		More: true,
		Sections: []typeUISection{
			{Title: "Documents", Relation: []string{"concerns"}, Limit: 2},
			{Title: "Utilities", Relation: []string{"serves"}, Dir: "in"},
		},
	}
	typeUIByType := map[string]typeUI{
		"Document": {View: &typeUIView{Inline: []string{"document_kind", "document_date"}}},
	}

	sections := resolveObjectViewSections(view, &prop, edges, related, typeUIByType)
	if len(sections) != 3 {
		t.Fatalf("sections = %d (%v), want declared Documents, Utilities and the trailing group", len(sections), sectionTitles(sections))
	}
	if sections[0].Title != "Documents" || sections[1].Title != "Utilities" || sections[2].Title != "More connections" {
		t.Fatalf("section order = %v, want declared order then the trailing group", sectionTitles(sections))
	}

	docs := sections[0]
	if docs.Total != 3 {
		t.Errorf("Documents.Total = %d, want 3", docs.Total)
	}
	if len(docs.Rows) != 2 {
		t.Fatalf("Documents.Rows = %d, want the declared limit of 2", len(docs.Rows))
	}

	util := sections[1]
	if util.Total != 1 || len(util.Rows) != 1 {
		t.Fatalf("Utilities = %d rows / %d total, want the incoming edge only (dir: in)", len(util.Rows), util.Total)
	}
	if util.Rows[0].Neighbour.ID != "util-1" {
		t.Errorf("Utilities row = %q, want util-1", util.Rows[0].Neighbour.ID)
	}

	more := sections[2]
	if more.Total != 2 {
		t.Fatalf("More connections = %d, want the outgoing serves edge and kept_at", more.Total)
	}
	gotMore := map[string]bool{}
	for _, row := range more.Rows {
		gotMore[row.RelID] = true
	}
	if !gotMore["e5"] || !gotMore["e6"] {
		t.Errorf("More connections rows = %v, want e5 (undeclared direction) and e6 (undeclared type)", gotMore)
	}
}

// TestResolveObjectViewSectionsMatchesVersionRow pins the canonical-id rule.
// Relationships are stored against the canonical id, while the object a page
// loads can be any version row; matching on ID alone silently produces an empty
// page for every object that has ever been edited.
func TestResolveObjectViewSectionsMatchesVersionRow(t *testing.T) {
	versionRow := GraphObject{ID: "prop-v4", CanonicalID: "prop-canonical", Type: "Property", Name: "Home"}
	related := []GraphObject{viewTestObject("doc-1", "Document", "Tax decision", nil)}
	edges := []GraphRelationship{viewTestEdge("e1", "concerns", "doc-1", "prop-canonical")}
	view := &typeUIView{Sections: []typeUISection{{Title: "Documents", Relation: []string{"concerns"}}}}

	sections := resolveObjectViewSections(view, &versionRow, edges, related, nil)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("sections = %v, want the edge matched through CanonicalID", sectionTitles(sections))
	}
	if got := sections[0].Rows[0].Neighbour.ID; got != "doc-1" {
		t.Errorf("neighbour = %q, want doc-1", got)
	}
}

// TestResolveObjectViewSectionsEmptyAndNil pins the two "nothing to render"
// cases: a type with no view falls back to the generic list (nil sections), and
// a declared section with no matching edges is hidden rather than shown empty.
func TestResolveObjectViewSectionsEmptyAndNil(t *testing.T) {
	prop := GraphObject{ID: "prop", CanonicalID: "prop", Type: "Property"}
	edges := []GraphRelationship{viewTestEdge("e1", "concerns", "doc-1", "prop")}

	if got := resolveObjectViewSections(nil, &prop, edges, nil, nil); got != nil {
		t.Errorf("nil view = %v, want nil so the caller keeps the generic list", got)
	}

	view := &typeUIView{More: false, Sections: []typeUISection{
		{Title: "Utilities", Relation: []string{"serves"}},
		{Title: "Documents", Relation: []string{"concerns"}},
	}}
	sections := resolveObjectViewSections(view, &prop, edges, nil, nil)
	if len(sections) != 1 || sections[0].Title != "Documents" {
		t.Fatalf("sections = %v, want only the section that matched", sectionTitles(sections))
	}
	if got := resolveObjectViewSections(&typeUIView{More: true}, &prop, nil, nil, nil); got != nil {
		t.Errorf("no edges = %v, want nil", got)
	}
}

// TestResolveObjectViewEdgeNotTouchingObject guards the resolver against an
// edge list that mentions somebody else's edge: it must be ignored, not
// rendered as a self-reference.
func TestResolveObjectViewEdgeNotTouchingObject(t *testing.T) {
	prop := GraphObject{ID: "prop", CanonicalID: "prop", Type: "Property"}
	edges := []GraphRelationship{
		viewTestEdge("e1", "concerns", "doc-1", "prop"),
		viewTestEdge("e2", "concerns", "doc-9", "doc-8"),
	}
	view := &typeUIView{Sections: []typeUISection{{Title: "Documents", Relation: []string{"concerns"}}}}

	sections := resolveObjectViewSections(view, &prop, edges, nil, nil)
	if len(sections) != 1 || sections[0].Total != 1 {
		t.Fatalf("sections = %v, want the single touching edge", sectionTitles(sections))
	}
	for _, row := range sections[0].Rows {
		if row.Neighbour.ID == "" || row.Neighbour.ID == "prop" {
			t.Errorf("row neighbour = %+v, want the document endpoint", row.Neighbour)
		}
	}
}

// TestObjectViewFacts covers the depth-1 preview budget: a type that declares
// `inline` chooses its own facts in order, a type that does not falls back to
// status plus the first scalar properties, prose fields are never used, and
// every value is bounded.
func TestObjectViewFacts(t *testing.T) {
	typeUIByType := map[string]typeUI{
		"Document": {View: &typeUIView{Inline: []string{"document_kind", "document_date", "missing"}}},
	}

	declared := viewTestObject("doc-1", "Document", "Tax decision", map[string]any{
		"document_kind": "tax_decision",
		"document_date": "2026-01-26",
		"notes":         "should never be a fact",
	})
	facts := objectViewFacts(declared, typeUIByType)
	if len(facts) != 2 {
		t.Fatalf("facts = %v, want the two declared properties that exist", facts)
	}
	if facts[0].Label != "Document kind" || facts[0].Value != "tax_decision" {
		t.Errorf("first fact = %+v, want the declared order", facts[0])
	}

	fallback := viewTestObject("obl-1", "Obligation", "Property tax", map[string]any{
		"status":   "active",
		"amount":   263,
		"currency": "PLN",
		"notes":    "long prose that must not fill a preview row",
		"tags":     []any{"a", "b"},
	})
	fallback.Status = "active"
	facts = objectViewFacts(fallback, nil)
	if len(facts) == 0 || facts[0].Label != "Status" || facts[0].Value != "active" {
		t.Fatalf("facts = %+v, want status first", facts)
	}
	if len(facts) > viewMaxFactsPerRow {
		t.Errorf("facts = %d, want at most %d", len(facts), viewMaxFactsPerRow)
	}
	statusFacts := 0
	for _, fact := range facts {
		if fact.Label == "Status" {
			statusFacts++
		}
		if fact.Label == "Notes" {
			t.Error("notes must not be used as a preview fact")
		}
		if len([]rune(fact.Value)) > viewFactMaxRunes+1 {
			t.Errorf("fact %q = %d runes, want it truncated", fact.Label, len([]rune(fact.Value)))
		}
	}
	if statusFacts != 1 {
		t.Errorf("status rendered %d times, want exactly once", statusFacts)
	}

	long := viewTestObject("x-1", "Note", "Long", map[string]any{
		"summary_long": "aaaa bbbb cccc dddd eeee ffff gggg hhhh iiii jjjj kkkk llll mmmm nnnn oooo pppp",
	})
	facts = objectViewFacts(long, nil)
	if len(facts) != 1 {
		t.Fatalf("facts = %v, want one truncated fact", facts)
	}
	if got := len([]rune(facts[0].Value)); got > viewFactMaxRunes+1 {
		t.Errorf("truncated value = %d runes, want <= %d", got, viewFactMaxRunes+1)
	}
}

// TestResolveObjectViewUnresolvedNeighbour pins the degraded row: a neighbour
// that was not fetched renders as an unresolved reference with no preview,
// rather than as a link to a page that may not exist.
func TestResolveObjectViewUnresolvedNeighbour(t *testing.T) {
	prop := GraphObject{ID: "prop", CanonicalID: "prop", Type: "Property"}
	edges := []GraphRelationship{viewTestEdge("e1", "concerns", "doc-gone", "prop")}
	view := &typeUIView{Sections: []typeUISection{{Title: "Documents", Relation: []string{"concerns"}}}}

	sections := resolveObjectViewSections(view, &prop, edges, nil, nil)
	if len(sections) != 1 || len(sections[0].Rows) != 1 {
		t.Fatalf("sections = %v, want one row", sectionTitles(sections))
	}
	row := sections[0].Rows[0]
	if row.Resolved {
		t.Error("Resolved = true, want false for a neighbour that was not fetched")
	}
	if len(row.Facts) != 0 {
		t.Errorf("facts = %v, want none for an unresolved neighbour", row.Facts)
	}
	if row.Neighbour.ID != "doc-gone" {
		t.Errorf("neighbour id = %q, want the edge's far endpoint", row.Neighbour.ID)
	}
}

// TestResolveObjectViewRowsAreStable pins the ordering rule: the same page
// renders in the same order twice, so a screenshot diff or a test can rely on
// it.
func TestResolveObjectViewRowsAreStable(t *testing.T) {
	prop := GraphObject{ID: "prop", CanonicalID: "prop", Type: "Property"}
	related := []GraphObject{
		viewTestObject("doc-b", "Document", "beta", nil),
		viewTestObject("doc-a", "Document", "Alpha", nil),
	}
	edges := []GraphRelationship{
		viewTestEdge("e2", "concerns", "doc-b", "prop"),
		viewTestEdge("e1", "concerns", "doc-a", "prop"),
	}
	view := &typeUIView{Sections: []typeUISection{{Title: "Documents", Relation: []string{"concerns"}}}}

	sections := resolveObjectViewSections(view, &prop, edges, related, nil)
	if len(sections) != 1 {
		t.Fatalf("sections = %v", sectionTitles(sections))
	}
	rows := sections[0].Rows
	if len(rows) != 2 || rows[0].Neighbour.Name != "Alpha" {
		t.Fatalf("rows = %+v, want case-insensitive label order", rows)
	}
}

// sectionTitles is the failure-message helper.
func sectionTitles(sections []objectViewSection) []string {
	titles := make([]string, 0, len(sections))
	for _, s := range sections {
		titles = append(titles, s.Title)
	}
	return titles
}

// TestExpandedViewSection pins the escape hatch from a truncated section: the
// same group re-resolved at the expanded budget, addressed by its index in the
// page's own render, including the trailing "More connections" group. An index
// that names no group reports false — the route must not render an empty card.
func TestExpandedViewSection(t *testing.T) {
	prop := GraphObject{ID: "prop", CanonicalID: "prop", Type: "Property"}
	var edges []GraphRelationship
	var related []GraphObject
	for i := 0; i < 30; i++ {
		id := "doc-" + strconv.Itoa(i)
		related = append(related, viewTestObject(id, "Document", "Doc "+strconv.Itoa(i), nil))
		edges = append(edges, viewTestEdge("e"+strconv.Itoa(i), "concerns", id, "prop"))
	}
	related = append(related, viewTestObject("svc", "UtilityService", "Water", nil))
	edges = append(edges, viewTestEdge("e-svc", "serves", "svc", "prop"))

	view := &typeUIView{
		More:     true,
		Sections: []typeUISection{{Title: "Documents", Relation: []string{"concerns"}, Limit: 3}},
	}
	sections := resolveObjectViewSections(view, &prop, edges, related, nil)
	if len(sections) != 2 {
		t.Fatalf("sections = %v", sectionTitles(sections))
	}
	if len(sections[0].Rows) != 3 || sections[0].Total != 30 {
		t.Fatalf("collapsed documents = %d/%d, want 3/30", len(sections[0].Rows), sections[0].Total)
	}
	if sections[0].Limit != 3 {
		t.Errorf("collapsed section Limit = %d, want the declared 3", sections[0].Limit)
	}
	// The fallback group is bounded by the default limit and reachable by index.
	if len(sections[1].Rows) != 1 || sections[1].Title != viewMoreTitle {
		t.Fatalf("fallback group = %q/%d rows, want %q/1", sections[1].Title, len(sections[1].Rows), viewMoreTitle)
	}

	expanded, ok := expandedViewSection(view, 0, &prop, edges, related, nil)
	if !ok {
		t.Fatal("expandedViewSection(0) = false")
	}
	if !expanded.Expanded {
		t.Error("expanded section must report Expanded")
	}
	if len(expanded.Rows) != 30 || expanded.Total != 30 {
		t.Errorf("expanded rows = %d/%d, want all 30", len(expanded.Rows), expanded.Total)
	}
	// Expansion keeps the declared budget so "show fewer" can return to the
	// page the reader left, not to a larger default.
	if expanded.Limit != 3 {
		t.Errorf("expanded section Limit = %d, want the declared 3", expanded.Limit)
	}
	// The trailing group is addressable too, so nothing on the page is dead.
	if more, ok := expandedViewSection(view, 1, &prop, edges, related, nil); !ok || more.Title != viewMoreTitle {
		t.Errorf("expandedViewSection(1) = %+v/%v, want the fallback group", more, ok)
	}
	// Out-of-range indexes are a miss, not an empty section.
	for _, index := range []int{-1, 2, 99} {
		if _, ok := expandedViewSection(view, index, &prop, edges, related, nil); ok {
			t.Errorf("expandedViewSection(%d) = true, want false", index)
		}
	}
}

// TestExpandedViewSectionCapsRows pins the expanded budget: a hub larger than it
// still returns a bounded response, with Total naming what is missing so the page
// can offer a filtered browse instead of pretending the list is complete.
func TestExpandedViewSectionCapsRows(t *testing.T) {
	prop := GraphObject{ID: "prop", CanonicalID: "prop", Type: "Property"}
	var edges []GraphRelationship
	var related []GraphObject
	for i := 0; i < viewExpandLimit+5; i++ {
		id := "doc-" + strconv.Itoa(i)
		related = append(related, viewTestObject(id, "Document", "Doc "+strconv.Itoa(i), nil))
		edges = append(edges, viewTestEdge("e"+strconv.Itoa(i), "concerns", id, "prop"))
	}
	view := &typeUIView{Sections: []typeUISection{{Title: "Documents", Relation: []string{"concerns"}, Limit: 2}}}

	expanded, ok := expandedViewSection(view, 0, &prop, edges, related, nil)
	if !ok {
		t.Fatal("expandedViewSection = false")
	}
	if len(expanded.Rows) != viewExpandLimit || expanded.Total != viewExpandLimit+5 {
		t.Fatalf("expanded = %d/%d rows, want %d/%d", len(expanded.Rows), expanded.Total, viewExpandLimit, viewExpandLimit+5)
	}
}

// TestObjectViewSectionDirectionCue pins the ShowDirection signal: a `dir: both`
// section (and the fallback group, which mixes whatever the author did not
// claim) tells each row to render its arrow, while a single-direction section
// does not repeat what its heading already says.
func TestObjectViewSectionDirectionCue(t *testing.T) {
	prop := GraphObject{ID: "prop", CanonicalID: "prop", Type: "Property"}
	related := []GraphObject{
		viewTestObject("doc", "Document", "Deed", nil),
		viewTestObject("svc", "UtilityService", "Water", nil),
	}
	edges := []GraphRelationship{
		viewTestEdge("e1", "concerns", "doc", "prop"),
		viewTestEdge("e2", "serves", "svc", "prop"),
		viewTestEdge("e3", "related_to", "prop", "doc"),
	}
	view := &typeUIView{Sections: []typeUISection{
		{Title: "Both", Relation: []string{"concerns", "related_to"}},
		{Title: "Incoming", Relation: []string{"serves"}, Dir: "in"},
	}}
	sections := resolveObjectViewSections(view, &prop, edges, related, nil)
	if len(sections) != 2 {
		t.Fatalf("sections = %v", sectionTitles(sections))
	}
	if !sections[0].ShowDirection {
		t.Error("a dir:both section must ask its rows for the direction cue")
	}
	if sections[1].ShowDirection {
		t.Error("a dir:in section must not repeat the heading's direction per row")
	}

	fallback := resolveObjectViewSections(&typeUIView{More: true}, &prop, edges, related, nil)
	if len(fallback) != 1 || !fallback[0].ShowDirection {
		t.Errorf("fallback group = %+v, want one group with the direction cue", fallback)
	}
}

// TestDominantNeighbourType pins the browse-all hint: the most common neighbour
// type wins, ties break alphabetically (so the link is deterministic), and an
// empty group names nothing.
func TestDominantNeighbourType(t *testing.T) {
	rows := []objectViewItem{
		{Neighbour: GraphObject{Type: "Document"}},
		{Neighbour: GraphObject{Type: "Document"}},
		{Neighbour: GraphObject{Type: "Item"}},
		{Neighbour: GraphObject{Type: "Item"}},
		{Neighbour: GraphObject{Type: "Task"}},
	}
	if got := dominantNeighbourType(rows); got != "Document" {
		t.Errorf("dominantNeighbourType = %q, want Document (Document and Item tie, D < I)", got)
	}
	if got := dominantNeighbourType(nil); got != "" {
		t.Errorf("dominantNeighbourType(nil) = %q, want empty", got)
	}
	if got := dominantNeighbourType([]objectViewItem{{Neighbour: GraphObject{}}}); got != "" {
		t.Errorf("dominantNeighbourType(untyped) = %q, want empty", got)
	}
}
