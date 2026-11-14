package main

import (
	"strings"
	"testing"
)

// objectHeaderLeadFixture is one object introduced at page-head scale.
func objectHeaderLeadFixture() objectHeaderProps {
	return objectHeaderProps{
		Variant:        objectHeaderLead,
		Type:           "task",
		Icon:           "zap",
		Color:          "#6366F1",
		Ref:            "TASK-1",
		Label:          "Fix the roof",
		Excerpt:        "must not leak at lead scale",
		TestID:         "object-header-probe",
		IdentityTestID: "object-identity-probe",
	}
}

// TestObjectHeaderLeadVariant pins the page-head scale: the type icon tile, the
// identity line, and the object's name as the page's h1 — the shape the object
// page and the wiki both lead with.
func TestObjectHeaderLeadVariant(t *testing.T) {
	html := renderHTMLDev(t, objectHeader(objectHeaderLeadFixture()))

	for _, want := range []string{
		`data-component="gateway/ObjectHeader"`,
		`data-testid="object-header-probe"`,
		`data-component="gateway/ObjectIdentityLine"`,
		`data-testid="object-identity-probe"`,
		`data-testid="object-ref"`,
		`>task<`,
		`>TASK-1<`,
		`<h1`,
		`Fix the roof`,
		`lucide--zap`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("lead header missing %q:\n%s", want, html)
		}
	}
	// Identity precedes the name, so the head reads type · ref, then the object.
	if id, name := strings.Index(html, `data-testid="object-identity-probe"`), strings.Index(html, "<h1"); id < 0 || name < 0 || id > name {
		t.Errorf("identity line must precede the h1 (identity=%d name=%d)", id, name)
	}
	// An excerpt belongs to a row; at lead scale the page's prose follows the
	// head and a second summary line would repeat it.
	if strings.Contains(html, "must not leak at lead scale") {
		t.Errorf("lead header rendered a row excerpt:\n%s", html)
	}
}

// TestObjectHeaderRowVariant pins the list scale: the name is a link when the
// caller supplies an href, plain text when it already owns the anchor, and a
// resolved neighbour is never dimmed.
func TestObjectHeaderRowVariant(t *testing.T) {
	linked := renderHTMLDev(t, objectHeader(objectHeaderProps{
		Variant: objectHeaderRow,
		Type:    "task",
		Ref:     "TASK-1",
		Label:   "Fix the roof",
		Href:    "/objects/o1/wiki",
		Excerpt: "Leaky after the storm.",
	}))
	if !strings.Contains(linked, `href="/objects/o1/wiki"`) {
		t.Errorf("row header must link its name:\n%s", linked)
	}
	if strings.Contains(linked, "<h1") {
		t.Error("a row must not render an h1")
	}
	if !strings.Contains(linked, "Leaky after the storm.") {
		t.Errorf("row header missing its excerpt:\n%s", linked)
	}
	if strings.Contains(linked, "data-unresolved") {
		t.Error("a resolved row must not be marked unresolved")
	}

	plain := renderHTMLDev(t, objectHeader(objectHeaderProps{
		Variant: objectHeaderRow,
		Type:    "task",
		Ref:     "TASK-1",
		Label:   "Fix the roof",
	}))
	if strings.Contains(plain, "<a ") {
		t.Errorf("a row without an href must render plain text:\n%s", plain)
	}
	if !strings.Contains(plain, "Fix the roof") {
		t.Errorf("plain row lost the object's name:\n%s", plain)
	}
	// No testid was asked for, so none is emitted — an empty data-testid would
	// match every "attribute present" assertion on an unrelated surface.
	if strings.Contains(plain, `data-testid=""`) {
		t.Errorf("row rendered an empty data-testid:\n%s", plain)
	}
}

// TestObjectHeaderUnresolvedRow pins the unresolved neighbour: dimmed, unlinked,
// and marked so a reader can tell a graph gap from a real object.
func TestObjectHeaderUnresolvedRow(t *testing.T) {
	html := renderHTMLDev(t, objectHeader(objectHeaderProps{
		Variant:    objectHeaderRow,
		Type:       "task",
		Ref:        "TASK-1",
		Label:      "Missing neighbour",
		Href:       "/objects/o1",
		Unresolved: true,
	}))

	if strings.Contains(html, "<a ") {
		t.Errorf("an unresolved neighbour must not link:\n%s", html)
	}
	if !strings.Contains(html, `data-unresolved="1"`) {
		t.Errorf("unresolved neighbour must be marked:\n%s", html)
	}
	if !strings.Contains(html, "text-muted-subtle") {
		t.Errorf("unresolved neighbour must be dimmed:\n%s", html)
	}
}

// TestObjectHeaderRowDirection pins the edge direction the row reports: an
// incoming arrow, an outgoing one, and nothing at all when the row is not inside
// a direction-bearing section.
func TestObjectHeaderRowDirection(t *testing.T) {
	in := renderHTMLDev(t, objectHeader(objectHeaderProps{Variant: objectHeaderRow, Type: "task", Label: "A", Direction: "in"}))
	if !strings.Contains(in, "lucide--arrow-left") {
		t.Errorf("incoming row missing its arrow:\n%s", in)
	}
	if !strings.Contains(in, "Incoming relationship") {
		t.Errorf("incoming arrow must be labelled for assistive tech:\n%s", in)
	}

	out := renderHTMLDev(t, objectHeader(objectHeaderProps{Variant: objectHeaderRow, Type: "task", Label: "B", Direction: "out"}))
	if !strings.Contains(out, "lucide--arrow-right") {
		t.Errorf("outgoing row missing its arrow:\n%s", out)
	}

	flat := renderHTMLDev(t, objectHeader(objectHeaderProps{Variant: objectHeaderRow, Type: "task", Label: "C"}))
	if strings.Contains(flat, "arrow-left") || strings.Contains(flat, "arrow-right") {
		t.Errorf("a row outside a directed section must not claim a direction:\n%s", flat)
	}
}

// TestObjectDisplayRef pins the identity line's ref: the declared ref, or the
// short id when the object has none yet.
func TestObjectDisplayRef(t *testing.T) {
	if got := objectDisplayRef("TASK-1", "0123456789abcdef"); got != "TASK-1" {
		t.Errorf("declared ref = %q, want TASK-1", got)
	}
	got := objectDisplayRef("", "0123456789abcdef")
	if got == "" || got[0] != '#' {
		t.Errorf("missing ref = %q, want a #-prefixed short id", got)
	}
	if strings.Contains(got, "0123456789abcdef") {
		t.Errorf("missing ref = %q, want the short id, not the full id", got)
	}
}

// TestObjectHeaderIsReusedAcrossObjectSurfaces is the point of the component: the
// wiki head, the object page head, a relationship row, a relationship endpoint
// and a browse card all introduce an object with the same two components, so no
// surface can drift into its own private head again.
func TestObjectHeaderIsReusedAcrossObjectSurfaces(t *testing.T) {
	wiki := renderHTMLDev(t, objectWikiPage(objectWikiPageData{
		Obj:  &GraphObject{ID: "o1", Type: "note", Key: "Deed", Properties: map[string]any{"summary": "A short lead."}},
		View: objectWikiViewPage,
		Sections: []objectViewSection{{
			Title: "Tasks",
			Rows:  []objectViewItem{{RelID: "r1", RelType: "relates_to", Neighbour: GraphObject{ID: "n1", Type: "task", Key: "Fix the roof"}, Resolved: true}},
			Total: 1,
			Limit: objectWikiSectionLimit,
		}},
	}))
	if got := strings.Count(wiki, `data-component="gateway/ObjectHeader"`); got != 2 {
		t.Errorf("wiki page rendered %d object headers, want 2 (head + one related object)", got)
	}
	if got := strings.Count(wiki, `data-component="gateway/ObjectIdentityLine"`); got != 2 {
		t.Errorf("wiki page rendered %d identity lines, want 2", got)
	}

	obj := &GraphObject{ID: "o1", Type: "note", Ref: "note-1", Key: "Deed", Properties: map[string]any{"summary": "A short lead."}}
	classic := renderHTMLDev(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, nil, map[string]typeUI{"note": {Icon: "notebook"}}, nil, nil, "Note", 0, nil, false))
	if !strings.Contains(classic, `data-component="gateway/ObjectHeader"`) {
		t.Error("the object page head must be the shared object header")
	}
	if !strings.Contains(classic, `data-testid="object-detail-header"`) {
		t.Error("the object page head must keep its own test hook")
	}
	// The header names the type and ref, so the chip row must not repeat them:
	// one occurrence each, from the identity line.
	if got := strings.Count(classic, ">note<"); got != 1 {
		t.Errorf("type named %d times on the object page, want 1 (the header's identity line)", got)
	}
	if got := strings.Count(classic, ">note-1<"); got != 1 {
		t.Errorf("ref rendered %d times on the object page, want 1", got)
	}

	neighbour := GraphObject{ID: "u1", Type: "UtilityService", Name: "Water — ZGK"}
	related := []GraphObject{neighbour}
	edges := []GraphRelationship{{ID: "r1", Type: "serves", SrcID: "u1", DstID: "o1"}}
	typeUIByType := map[string]typeUI{"UtilityService": {Icon: "zap", Color: "#F59E0B"}}
	relationships := renderHTMLDev(t, objectRelationshipsPage(
		objectDetailShellData{Obj: obj, Active: "relationships", TypeUIByType: typeUIByType},
		nil, edges, related, nil,
	))
	if !strings.Contains(relationships, `data-component="gateway/ObjectHeader"`) {
		t.Error("a relationship endpoint must be the shared object header")
	}
	if got := strings.Count(relationships, `data-testid="object-relationship-endpoint"`); got != 2 {
		t.Errorf("endpoint blocks rendered %d times, want 2 (both ends of one edge)", got)
	}

	cards := renderHTMLDev(t, ObjectsPage(objectsBrowserSampleData("")))
	wantCards := len(objectsBrowserSampleData("").Objects)
	if got := strings.Count(cards, `data-testid="object-card"`); got != wantCards {
		t.Fatalf("cards rendered %d, want %d", got, wantCards)
	}
	if got := strings.Count(cards, `data-component="gateway/ObjectCard"`); got != wantCards {
		t.Errorf("annotated cards = %d, want %d (the feedback overlay names the card, not its wrapper)", got, wantCards)
	}
	if got := strings.Count(cards, `data-component="gateway/ObjectIdentityLine"`); got != wantCards {
		t.Errorf("card identity lines = %d, want %d (one per card)", got, wantCards)
	}
	// The card's head is the same component, not a card-shaped copy of it: the
	// tile, the identity line and the name come from objectHeader, so a report on
	// any of them names an object component instead of the card wrapper.
	if got := strings.Count(cards, `data-component="gateway/ObjectHeader"`); got != wantCards {
		t.Errorf("card object headers = %d, want %d (one per card)", got, wantCards)
	}
	// One link per card, and it is the stretched overlay after the checkbox
	// label — the head itself carries no anchor, so a click on the head reaches
	// the overlay instead of being swallowed by it.
	if got := strings.Count(cards, `aria-label="Open `); got != wantCards {
		t.Errorf("card open links = %d, want %d (exactly one per card)", got, wantCards)
	}
	cardStart := strings.Index(cards, `data-testid="object-card"`)
	labelEnd := strings.Index(cards[cardStart:], `</label>`)
	if cardStart < 0 || labelEnd < 0 {
		t.Fatalf("unexpected card structure (card=%d labelEnd=%d)", cardStart, labelEnd)
	}
	if head := cards[cardStart : cardStart+labelEnd]; strings.Contains(head, "<a ") {
		t.Errorf("the card head must not carry its own anchor (the card has one stretched link):\n%s", head)
	}
	if got := strings.Count(cards, `line-clamp-3`); got == 0 {
		t.Error("a card excerpt must ask for the taller clamp")
	}
}
