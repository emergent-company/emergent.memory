package main

// Tests for the follow-ups collected in #667 (typed object page) and #699
// (objects card grid + wiki). They live together because each one pins a
// behaviour a review found missing, and the pairing makes the reason obvious to
// the next reader. Component-level assertions stay next to the component tests
// they extend (objects_browser_test.go, object_article_test.go), and the
// resolver's collapse/expand parity is in object_view_test.go.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newFollowupServer wires the object routes these tests exercise: the Overview,
// the Relationships tab, and the section fragment.
func newFollowupServer(memory *fakeMemory) *echo.Echo {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: memory}
	e := echo.New()
	e.GET("/objects/:id", s.uiObject)
	e.GET("/objects/:id/relationships", s.uiObjectRelationshipsPage)
	e.GET("/objects/:id/relationships/sections/:index", s.uiObjectViewSection)
	e.GET("/objects/:id/similar/partial", s.uiObjectSimilarPartial)
	e.GET("/objects/:id/similar/count", s.uiObjectSimilarCountPartial)
	e.GET("/objects/:id/wiki", s.uiObjectWiki)
	return e
}

// viewFollowupMemory is one Property with a Documents section (limit 2) over four
// document edges, so the collapsed card hides two rows and the expanded card
// shows all four. A second pack-owned relationship type (serves) is declared by
// the view but matches no edge, which is what a view that hides its edge types
// looks like from the page's side.
func viewFollowupMemory() *fakeMemory {
	return &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{
			Name:  "Property",
			Label: "Property",
			UI:    json.RawMessage(`{"icon":"home","color":"#4F46E5","view":{"more":false,"sections":[{"title":"Documents","relation":"concerns","limit":2}]}}`),
		}}},
		objects: []GraphObject{
			{ID: "o1", CanonicalID: "o1", Type: "Property", Name: "Home — Waliły 29"},
			{ID: "d1", CanonicalID: "d1", Type: "Document", Name: "Deed"},
			{ID: "d2", CanonicalID: "d2", Type: "Document", Name: "Mortgage"},
			{ID: "d3", CanonicalID: "d3", Type: "Document", Name: "Permit"},
			{ID: "d4", CanonicalID: "d4", Type: "Document", Name: "Survey"},
		},
		relns: []GraphRelationship{
			{ID: "r1", Type: "concerns", SrcID: "d1", DstID: "o1"},
			{ID: "r2", Type: "concerns", SrcID: "d2", DstID: "o1"},
			{ID: "r3", Type: "concerns", SrcID: "d3", DstID: "o1"},
			{ID: "r4", Type: "concerns", SrcID: "d4", DstID: "o1"},
		},
	}
}

// TestObjectRelationshipsPageEscapeParams covers #667.2 at the handler level:
// ?expand=N and ?all=1 are branch decisions in uiObjectRelationshipsPage, and a
// regression there must fail here rather than only through a rendered href.
func TestObjectRelationshipsPageEscapeParams(t *testing.T) {
	e := newFollowupServer(viewFollowupMemory())
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// Collapsed by default: the declared limit of 2, with the count of what it
	// hides and a link that re-renders the page expanded.
	body := get("/objects/o1/relationships").Body.String()
	for _, want := range []string{
		`data-testid="object-view-sections"`,
		`data-testid="object-view-truncated"`,
		"+2 more not shown",
		`href="/objects/o1/relationships?expand=0"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("collapsed relationships page missing %q:\n%s", want, body)
		}
	}

	// ?expand=N opens exactly that group to the expanded budget.
	body = get("/objects/o1/relationships?expand=0").Body.String()
	for _, want := range []string{"Deed", "Mortgage", "Permit", "Survey", `data-testid="object-view-show-fewer"`} {
		if !strings.Contains(body, want) {
			t.Errorf("?expand=0 page missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `data-testid="object-view-truncated"`) {
		t.Errorf("an expanded group that fits must not claim truncation:\n%s", body)
	}

	// An out-of-range index is ignored, not a 500 or an empty page.
	if rec := get("/objects/o1/relationships?expand=99"); rec.Code != http.StatusOK {
		t.Fatalf("?expand=99 status = %d, want 200", rec.Code)
	} else if !strings.Contains(rec.Body.String(), "+2 more not shown") {
		t.Errorf("an out-of-range ?expand= must leave the page collapsed:\n%s", rec.Body.String())
	}

	// ?all=1 is the reader's escape from the view: the flat list of every edge.
	body = get("/objects/o1/relationships?all=1").Body.String()
	if !strings.Contains(body, `data-testid="object-relationship-endpoint"`) {
		t.Errorf("?all=1 must render the flat edge list:\n%s", body)
	}
	if strings.Contains(body, `data-testid="object-view-sections"`) {
		t.Errorf("?all=1 must bypass the declared view:\n%s", body)
	}
}

// TestObjectOverviewEmptyViewNote covers #667.3 through the handler: the Overview
// must set HasView, so a view that legally shows nothing says so there instead of
// only on the Relationships tab. The user guide describes the note as page-wide.
func TestObjectOverviewEmptyViewNote(t *testing.T) {
	f := viewFollowupMemory()
	// Replace the visible edges with one the view deliberately does not present:
	// the view is valid and shows none of the object's connections.
	f.relns = []GraphRelationship{{ID: "r9", Type: "related_to", SrcID: "o1", DstID: "o2"}}
	f.objects = append(f.objects, GraphObject{ID: "o2", CanonicalID: "o2", Type: "Property", Name: "Other"})

	e := newFollowupServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="object-view-empty"`,
		`data-testid="object-view-show-unfiltered"`,
		`href="/objects/o1/relationships?all=1"`,
		"1 connections",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overview missing the empty-view note %q:\n%s", want, body)
		}
	}

	// A type that declares no view keeps the Overview silent: there is nothing
	// the reader was promised, and zero edges is the ordinary empty case.
	plain := &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "Property", Label: "Property"}}},
		objects:  f.objects[:1],
		relns:    f.relns,
	}
	plainEcho := newFollowupServer(plain)
	rec = httptest.NewRecorder()
	plainEcho.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1", nil))
	if strings.Contains(rec.Body.String(), `data-testid="object-view-empty"`) {
		t.Errorf("a type without a view must not render the empty-view note:\n%s", rec.Body.String())
	}
}

// TestObjectOverviewSectionSurface covers #667.4: an Overview card's no-JS "show
// fewer" must land back on the Overview, not on the Relationships tab the reader
// never opened. The in-place swap has to carry the surface too, or the card it
// returns links away from the page it replaced.
func TestObjectOverviewSectionSurface(t *testing.T) {
	e := newFollowupServer(viewFollowupMemory())
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// The Overview's own "show all" swap must name the Overview surface.
	body := get("/objects/o1").Body.String()
	if !strings.Contains(body, `hx-get="/objects/o1/relationships/sections/0?surface=overview"`) {
		t.Errorf("overview show-all swap must carry the overview surface:\n%s", body)
	}

	// The expanded card the swap returns links back to the Overview.
	body = get("/objects/o1/relationships/sections/0?surface=overview").Body.String()
	for _, want := range []string{
		`data-testid="object-view-show-fewer"`,
		`href="/objects/o1"`,
		`hx-get="/objects/o1/relationships/sections/0?surface=overview&amp;collapsed=1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overview-sourced card missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `href="/objects/o1/relationships"`) {
		t.Errorf("the overview card's no-JS collapse must not navigate to the Relationships tab:\n%s", body)
	}

	// The Relationships tab keeps its own target.
	body = get("/objects/o1/relationships/sections/0").Body.String()
	if !strings.Contains(body, `href="/objects/o1/relationships"`) {
		t.Errorf("relationships-sourced card lost its collapse target:\n%s", body)
	}
}

// TestObjectViewSectionStaleHtmx covers #667.7: a stale ?collapsed=1 must not
// 404 an htmx swap — a 404 leaves the card frozen and dead. The swap's target no
// longer exists, so an empty 200 with outerHTML removes it. A direct link still
// 404s, because there the reader needs to know the section is gone.
func TestObjectViewSectionStaleHtmx(t *testing.T) {
	f := viewFollowupMemory()
	e := newFollowupServer(f)
	// Drop every visible edge: index 0 no longer names a group.
	f.relns = nil

	request := func(hx bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/objects/o1/relationships/sections/0?collapsed=1", nil)
		if hx {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	rec := request(true)
	if rec.Code != http.StatusOK {
		t.Fatalf("stale htmx collapse status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "" {
		t.Errorf("stale htmx collapse must return an empty body so outerHTML removes the card, got %q", body)
	}

	if rec := request(false); rec.Code != http.StatusNotFound {
		t.Errorf("direct stale section status = %d, want 404", rec.Code)
	}
}

// TestObjectSimilarOverFetch covers #667.1: the vector search returns the top ten
// neighbours, and on a dense object those can all be already linked. Filtering
// after a fetch of exactly the display limit then emptied the section while
// unlinked matches existed at #11+. The fetch must over-request and the count
// badge must not claim saturation when it did not have to.
func TestObjectSimilarOverFetch(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "Task", Label: "Task"}}},
		objects:  []GraphObject{{ID: "o1", CanonicalID: "o1", Type: "Task", Key: "t1"}},
	}
	for i := 1; i <= 10; i++ {
		id := "s" + strconv.Itoa(i)
		f.objects = append(f.objects, GraphObject{ID: id, CanonicalID: id, Type: "Task", Key: "linked-" + strconv.Itoa(i)})
		f.relns = append(f.relns, GraphRelationship{ID: "e" + strconv.Itoa(i), Type: "related_to", SrcID: "o1", DstID: id})
	}
	// The eleventh neighbour is the first unlinked match: a fetch bounded to the
	// display limit never sees it.
	f.objects = append(f.objects, GraphObject{ID: "s11", CanonicalID: "s11", Type: "Task", Key: "unlinked-match"})
	for i := 1; i <= 10; i++ {
		f.similar = append(f.similar, SimilarObject{ID: "s" + strconv.Itoa(i), Type: "Task", Key: "linked-" + strconv.Itoa(i), Distance: 0.05})
	}
	f.similar = append(f.similar, SimilarObject{ID: "s11", Type: "Task", Key: "unlinked-match", Distance: 0.05})

	e := newFollowupServer(f)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	body := get("/objects/o1/similar/partial").Body.String()
	if !strings.Contains(body, "unlinked-match") {
		t.Errorf("similar section lost the first unlinked match past the display limit:\n%s", body)
	}
	for i := 1; i <= 10; i++ {
		if strings.Contains(body, "linked-"+strconv.Itoa(i)) {
			t.Errorf("similar section re-listed already-linked object %d:\n%s", i, body)
		}
	}

	// One visible match out of a saturated raw fetch: the badge says 1, and the
	// "+" correctly says more may exist past the raw fetch.
	count := get("/objects/o1/similar/count").Body.String()
	if !strings.Contains(count, ">1</span>") {
		t.Errorf("similar count badge = %q, want the single visible match", count)
	}
	if strings.Contains(count, "1+") {
		t.Errorf("a raw fetch that was not saturated must not claim saturation: %q", count)
	}
}

// TestObjectWikiSkipsAssigneeCandidates covers #699.2: the wiki renders no
// assignee control, so its shell load must not pay the runtime-agent round trip
// the editor's Overview does.
func TestObjectWikiSkipsAssigneeCandidates(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{
			Name: "Task", Label: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "done"},
		}}},
		objects: []GraphObject{{
			ID: "o1", CanonicalID: "o1", Type: "Task", Key: "t1", Version: 1,
			Properties: map[string]any{"summary": "A short lead."},
		}},
	}
	e := newFollowupServer(f)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// The Overview is the editor surface and must still offer the picker.
	if rec := get("/objects/o1"); rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d", rec.Code)
	}
	if f.listScheduledAgentsCalls == 0 {
		t.Fatal("the Overview must load assignee candidates for its workflow panel")
	}

	f.listScheduledAgentsCalls = 0
	if rec := get("/objects/o1/wiki"); rec.Code != http.StatusOK {
		t.Fatalf("wiki status = %d", rec.Code)
	}
	if f.listScheduledAgentsCalls != 0 {
		t.Errorf("the wiki fetched assignee candidates %d times, want 0", f.listScheduledAgentsCalls)
	}
}
