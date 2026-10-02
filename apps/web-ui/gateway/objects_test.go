package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestDistinctObjectTypes(t *testing.T) {
	objects := []GraphObject{
		{ID: "1", Type: "person"},
		{ID: "2", Type: "task"},
		{ID: "3", Type: "person"},
		{ID: "4", Type: ""},
	}
	types := distinctObjectTypes(objects)
	if len(types) != 2 || types[0] != "person" || types[1] != "task" {
		t.Fatalf("types = %v", types)
	}
}

func TestGraphObjectProperties(t *testing.T) {
	o := GraphObject{Properties: map[string]any{
		"name":       "Sam",
		"first_name": "Sam",
		"_internal":  "skip",
		"empty":      "",
	}}
	props := graphObjectProperties(o)
	if len(props) != 2 {
		t.Fatalf("props = %+v", props)
	}
	if props[0].Key != "first_name" || props[1].Key != "name" {
		t.Fatalf("props not sorted: %+v", props)
	}
}

func TestObjectEndpointLabel(t *testing.T) {
	obj := &GraphObject{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam-lee"}
	related := []GraphObject{{ID: "o2", CanonicalID: "c2", Type: "task", Key: "call dentist"}}
	if got := objectEndpointLabel(obj, related, "c1"); got != "sam-lee" {
		t.Errorf("self = %q, want sam", got)
	}
	if got := objectEndpointLabel(obj, related, "c2"); got != "call dentist" {
		t.Errorf("related = %q, want call dentist", got)
	}
	if got := objectEndpointLabel(obj, related, "c9"); got == "" {
		t.Error("unknown id should fall back to a shortened id")
	}
}

func TestRenderObjectsPage(t *testing.T) {
	objects := []GraphObject{
		{ID: "o1", Type: "person", Key: "sam-lee", Status: "suggested", CreatedAt: "2026-08-26T10:00:00Z"},
		{ID: "o2", Type: "task", Key: "call dentist", CreatedAt: "2026-08-26T11:00:00Z"},
	}
	branches := []Branch{{ID: "b1", Name: "plan/next-gen"}}
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Objects:  objects,
		Types:    []string{"person", "task"},
		Branches: branches,
	}))
	for _, want := range []string{
		"Objects", "sam-lee", "call dentist", "person", "task",
		`href="/objects/o1"`, `name="type"`, "All types",
		`name="branch"`, "Main branch", "plan/next-gen",
		"New object", `href="/objects/new"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("objects page missing %q", want)
		}
	}

	htmlEmpty := renderHTML(t, ObjectsPage(objectsPageData{}))
	if !strings.Contains(htmlEmpty, "No objects yet") {
		t.Error("empty state missing")
	}

	htmlErr := renderHTML(t, ObjectsPage(objectsPageData{LoadErr: errTest}))
	if !strings.Contains(htmlErr, "Failed to load objects") {
		t.Error("error state missing")
	}
}

// TestRenderObjectsProvenanceFilter covers the actor provenance controls on the
// generic browser: a "Created by / Updated by / Any" mode toggle and a single
// actor select listing real project members (Users) and agent definitions
// (Agents), whose option value encodes the whole (actor_type, actor_id) pair.
// Copy is plain object wording — never a memory concept.
func TestRenderObjectsProvenanceFilter(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Objects:    []GraphObject{{ID: "o1", Type: "note", Key: "agent-note"}},
		Provenance: "created",
		ActorType:  "agent",
		ActorID:    "11111111-1111-1111-1111-111111111111",
		ActorOptions: []objectActorOption{
			{Type: "user", ID: "u1", Label: "Sam Lee"},
			{Type: "agent", ID: "11111111-1111-1111-1111-111111111111", Label: "diane"},
		},
	}))
	for _, want := range []string{
		`name="provenance"`, `value="any"`, `value="created"`, `value="updated"`,
		"Any", "Created by", "Updated by",
		`aria-label="Provenance"`,
		`name="actor"`, `aria-label="Actor"`, "Any actor",
		`<optgroup label="Users">`, `<optgroup label="Agents">`,
		"Sam Lee", "diane",
		`value="user:u1"`,
		`value="agent:11111111-1111-1111-1111-111111111111" selected`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("provenance filter missing %q", want)
		}
	}
	// exactly one provenance mode is checked, and it is the active one.
	if got := strings.Count(html, `name="provenance"`); got != 3 {
		t.Errorf("provenance radios = %d, want 3", got)
	}
	if got := strings.Count(html, `name="provenance" value="created" checked`); got != 1 {
		t.Errorf("active provenance mode not checked: %d", got)
	}
	// The free-text actor-id input is gone: actors are chosen from the dropdown.
	if strings.Contains(html, `aria-label="Actor id"`) {
		t.Error("actor id text input should be replaced by the actor dropdown")
	}

	// The actor select is always available; without a selection it stays on
	// "Any actor" and applies nothing.
	noActor := renderHTML(t, ObjectsPage(objectsPageData{
		Objects: []GraphObject{{ID: "o1", Type: "note"}},
	}))
	if !strings.Contains(noActor, `aria-label="Actor"`) {
		t.Error("actor select should always render in the browse filter")
	}
	if !strings.Contains(noActor, `value="" selected`) {
		t.Error("empty actor select should select the Any actor option")
	}
}

// TestRenderObjectsActorFilterFallbackOption pins that an active actor pair
// which is not among the loaded options (a former member, or one passed by URL)
// still renders as a selected option, so the control reflects the active filter
// instead of silently resetting to "Any actor".
func TestRenderObjectsActorFilterFallbackOption(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Objects:   []GraphObject{{ID: "o1", Type: "note"}},
		ActorType: "user",
		ActorID:   "deadbeef-0000-0000-0000-000000000000",
	}))
	for _, want := range []string{`value="user:deadbeef-0000-0000-0000-000000000000" selected`, "User"} {
		if !strings.Contains(html, want) {
			t.Errorf("fallback actor option missing %q", want)
		}
	}
}

// TestRenderObjectsProvenanceFilterHiddenInSearch pins that the provenance and
// actor controls are browse-only: the server's search endpoints cannot apply the
// actor filter, so the controls are omitted (not silently ignored) in search
// mode.
func TestRenderObjectsProvenanceFilterHiddenInSearch(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Query: "sam",
		Mode:  "unified",
	}))
	for _, bad := range []string{`name="provenance"`, `aria-label="Provenance"`, `name="actor"`, `aria-label="Actor"`} {
		if strings.Contains(html, bad) {
			t.Errorf("search mode must not render provenance control %q", bad)
		}
	}
}

// TestRenderAgentScopedObjectsPage covers the agent-scoped view: an
// agent-focused header, the provenance mode toggle, the actor pair fixed to the
// agent (hidden inputs, no editable actor controls), and the filter form posting
// back to the agent's own route.
func TestRenderAgentScopedObjectsPage(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		AgentID:    "a1",
		AgentName:  "diane",
		ActorType:  "agent",
		ActorID:    "a1",
		Provenance: "any",
		Objects:    []GraphObject{{ID: "o1", Type: "note", Key: "agent-note"}},
	}))
	for _, want := range []string{
		"Objects", "Objects created or updated by diane",
		`href="/agents/a1"`,
		`action="/agents/a1/objects"`,
		`name="provenance"`, "Created by", "Updated by", "Any",
		// actor pair fixed and carried as hidden inputs
		`type="hidden" name="actor_type" value="agent"`,
		`type="hidden" name="actor_id" value="a1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("agent-scoped objects page missing %q", want)
		}
	}
	// No editable actor controls: the pair is fixed to the agent.
	for _, bad := range []string{`aria-label="Actor"`, `name="actor"`} {
		if strings.Contains(html, bad) {
			t.Errorf("agent-scoped view must not render editable actor control %q", bad)
		}
	}
	// The global search and Q&A are omitted (they cannot be provenance-scoped).
	for _, bad := range []string{`id="objects-search-form"`, "Ask the graph"} {
		if strings.Contains(html, bad) {
			t.Errorf("agent-scoped view must not render %q", bad)
		}
	}
}

// TestRenderObjectsLoadMoreCarriesProvenance pins that cursor pagination keeps
// the active actor-provenance filter, so "Load more" appends scoped rows.
func TestRenderObjectsLoadMoreCarriesProvenance(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Objects:    []GraphObject{{ID: "o1", Type: "note", Key: "agent-note"}},
		HasMore:    true,
		NextCursor: "cur-1",
		ActorType:  "agent",
		ActorID:    "a1",
		Provenance: "created",
	}))
	for _, want := range []string{"actor_type=agent", "actor_id=a1", "provenance=created", "cursor=cur-1"} {
		if !strings.Contains(html, want) {
			t.Errorf("load more URL missing %q", want)
		}
	}
}

// TestUIObjectsProvenanceParams exercises query-param parsing at the handler:
// the actor pair and provenance mode reach ListGraphObjectsPage, an unknown
// provenance normalizes to any, and a stray actor_id without actor_type is
// dropped (the server rejects it).
func TestUIObjectsProvenanceParams(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects", s.uiObjects)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?actor_type=agent&actor_id=abc-123&provenance=created", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.lastPageActorType != "agent" || f.lastPageActorID != "abc-123" || f.lastPageProvenance != "created" {
		t.Errorf("parsed params = %q/%q/%q, want agent/abc-123/created", f.lastPageActorType, f.lastPageActorID, f.lastPageProvenance)
	}

	// unknown provenance normalizes to the default
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?provenance=bogus", nil))
	if f.lastPageProvenance != defaultObjectProvenance {
		t.Errorf("unknown provenance = %q, want %q", f.lastPageProvenance, defaultObjectProvenance)
	}

	// actor_id without actor_type is dropped (the server rejects the pair)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?actor_id=abc-123", nil))
	if f.lastPageActorType != "" || f.lastPageActorID != "" {
		t.Errorf("stray actor_id must be dropped, got %q/%q", f.lastPageActorType, f.lastPageActorID)
	}

	// actor_type without actor_id is dropped too: it would narrow to NULL-actor
	// rows, not a usable any-actor filter.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?actor_type=agent", nil))
	if f.lastPageActorType != "" || f.lastPageActorID != "" {
		t.Errorf("actor_type without actor_id must be dropped, got %q/%q", f.lastPageActorType, f.lastPageActorID)
	}
}

// TestUIObjectsCombinedActorParam pins the actor dropdown's submission contract:
// the single select submits the pair as `actor=<type>:<id>`, which parses back to
// the (actor_type, actor_id) pair, while malformed or incomplete combined values
// are dropped exactly like an incomplete explicit pair.
func TestUIObjectsCombinedActorParam(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects", s.uiObjects)

	t.Run("user pair", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?actor=user:u-1&provenance=updated", nil))
		if f.lastPageActorType != "user" || f.lastPageActorID != "u-1" || f.lastPageProvenance != "updated" {
			t.Errorf("parsed pair = %q/%q/%q, want user/u-1/updated", f.lastPageActorType, f.lastPageActorID, f.lastPageProvenance)
		}
	})
	t.Run("agent pair", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?actor=agent:a-1", nil))
		if f.lastPageActorType != "agent" || f.lastPageActorID != "a-1" {
			t.Errorf("parsed pair = %q/%q, want agent/a-1", f.lastPageActorType, f.lastPageActorID)
		}
	})
	for name, query := range map[string]string{
		"missing separator": "actor=bogus",
		"empty id":          "actor=agent:",
		"unknown type":      "actor=robot:x",
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?"+query, nil))
			if f.lastPageActorType != "" || f.lastPageActorID != "" {
				t.Errorf("%s must drop the pair, got %q/%q", query, f.lastPageActorType, f.lastPageActorID)
			}
		})
	}
}

// TestEffectiveObjectProvenance pins the defense-in-depth helper contract: an
// empty actor type clears any provenance mode to "any" (the server rejects
// provenance without actor_type), while a complete actor pair keeps the
// normalized mode.
func TestEffectiveObjectProvenance(t *testing.T) {
	if got := effectiveObjectProvenance("created", ""); got != defaultObjectProvenance {
		t.Errorf("effectiveObjectProvenance(created, \"\") = %q, want %q", got, defaultObjectProvenance)
	}
	if got := effectiveObjectProvenance("created", "agent"); got != "created" {
		t.Errorf("effectiveObjectProvenance(created, agent) = %q, want created", got)
	}
}

// TestUIObjectsBareProvenanceCleared pins the fix: a `provenance` param with no
// actor pair is cleared to "any" before it reaches ListGraphObjectsPage (the
// server 400s on provenance without actor_type), and the rendered control is
// omitted so the filter bar cannot build the broken URL.
func TestUIObjectsBareProvenanceCleared(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects", s.uiObjects)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?provenance=created", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.lastPageProvenance != defaultObjectProvenance {
		t.Errorf("bare provenance forwarded = %q, want %q", f.lastPageProvenance, defaultObjectProvenance)
	}
	if f.lastPageActorType != "" || f.lastPageActorID != "" {
		t.Errorf("bare provenance set actor pair = %q/%q, want empty", f.lastPageActorType, f.lastPageActorID)
	}
	if strings.Contains(rec.Body.String(), `name="provenance"`) {
		t.Error("provenance control must be hidden when no actor is selected")
	}
}

// TestUIObjectsPartialCarriesProvenance pins that the "Load more" partial
// forwards the actor-provenance filter to ListGraphObjectsPage, so appended
// pages stay scoped.
func TestUIObjectsPartialCarriesProvenance(t *testing.T) {
	f := &fakeMemory{pageObjects: []GraphObject{{ID: "o2", Type: "note", Key: "next-note"}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/partial", s.uiObjectsPartial)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/partial?cursor=c1&actor_type=agent&actor_id=a1&provenance=updated", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.lastPageActorType != "agent" || f.lastPageActorID != "a1" || f.lastPageProvenance != "updated" {
		t.Errorf("partial params = %q/%q/%q, want agent/a1/updated", f.lastPageActorType, f.lastPageActorID, f.lastPageProvenance)
	}
}

func TestRenderObjectsPageSearchStatsLoadMore(t *testing.T) {
	// search mode: query input, mode selector, and result rows with a score badge.
	searchHTML := renderHTML(t, ObjectsPage(objectsPageData{
		Query: "sam",
		Mode:  "hybrid",
		Results: []ObjectSearchResult{
			{Object: GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}, Score: 0.87},
		},
	}))
	for _, want := range []string{
		`name="q"`, `value="sam"`, `name="mode"`, "Full-text", "Hybrid",
		"sam-lee", "0.87",
	} {
		if !strings.Contains(searchHTML, want) {
			t.Errorf("search page missing %q", want)
		}
	}
	// split button: the primary submit carries the SELECTED mode, and each
	// dropdown option is its own submit button that announces its selected
	// state (role=menuitemradio + aria-checked).
	searchForm := objectSearchFormHTML(t, searchHTML)
	if !strings.Contains(searchForm, "Hybrid Search") {
		t.Errorf("primary label must reflect the selected mode:\n%s", searchForm)
	}
	if tag := firstSubmitTag(searchForm); !strings.Contains(tag, `name="mode"`) || !strings.Contains(tag, `value="hybrid"`) {
		t.Errorf("primary submit must carry the selected mode, got %s", tag)
	}
	for _, want := range []string{
		"Fuses graph and text ranking for the best overall matches.",
		"Blends keyword and semantic (vector) matching.",
		"Keyword matching only — fast and exact.",
	} {
		if !strings.Contains(searchForm, want) {
			t.Errorf("search form missing mode description %q", want)
		}
	}
	if got := strings.Count(searchForm, `role="menuitemradio"`); got != 3 {
		t.Errorf("want 3 mode menuitemradio options, got %d", got)
	}
	if got := strings.Count(searchForm, `aria-checked="true"`); got != 1 {
		t.Errorf("want exactly one checked mode, got %d", got)
	}
	if got := strings.Count(searchForm, `aria-checked="false"`); got != 2 {
		t.Errorf("want two unchecked modes, got %d", got)
	}
	// Truthful ARIA: the dropdown is daisyUI's CSS-only focus dropdown, so it
	// must not advertise a static expanded state, and mode selection must be
	// plain form submits rather than relying on inline JS.
	if strings.Contains(searchForm, "aria-expanded") || strings.Contains(searchForm, "aria-haspopup") {
		t.Error("CSS-only dropdown must not claim aria-expanded/aria-haspopup")
	}
	if strings.Contains(searchForm, "onclick=") {
		t.Error("mode selection must work without inline JS (plain submit buttons)")
	}
	if strings.Contains(searchHTML, "Load more") {
		t.Error("search mode must not render a Load more button")
	}

	// search no-hits state
	noHits := renderHTML(t, ObjectsPage(objectsPageData{Query: "zzz", Mode: "fulltext"}))
	if !strings.Contains(noHits, "No matching objects") {
		t.Error("search no-hits state missing")
	}

	// browse mode with stats + load-more
	browseHTML := renderHTML(t, ObjectsPage(objectsPageData{
		Objects:    []GraphObject{{ID: "o1", Type: "person", Key: "sam-lee"}},
		HasMore:    true,
		NextCursor: "cur-1",
		Stats:      &objectsStats{TotalObjects: 42, PendingEmbed: 3, FailedEmbed: 1},
	}))
	for _, want := range []string{"42", "objects", "pending", "failed", "Load more", `hx-get="/objects/partial?cursor=cur-1"`, `hx-target="#objects-list"`} {
		if !strings.Contains(browseHTML, want) {
			t.Errorf("browse page missing %q", want)
		}
	}
}

// TestRenderObjectsPageSearchUnified renders the unified search mode: the
// Unified mode option and a unified result row (labels + score) that omits the
// status/embedding badges unavailable in unified results.
func TestRenderObjectsPageSearchUnified(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Query: "sam",
		Mode:  "unified",
		Results: []ObjectSearchResult{
			{Object: GraphObject{ID: "c1", Type: "person", Key: "sam-lee", Status: "confirmed", EmbeddingStatus: "embedded", Labels: []string{"contact"}}, Score: 0.88},
		},
	}))
	for _, want := range []string{
		"Unified", "sam-lee", "0.88", "contact", "person",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("unified search page missing %q", want)
		}
	}
	if strings.Contains(html, "confirmed") || strings.Contains(html, "Embedded") {
		t.Error("unified row must not render status/embedding badges")
	}
}

// TestRenderObjectsSearchDefaultUnified pins the new default: an absent mode
// selects Unified, so the primary submit button reads "Unified Search" and
// carries mode=unified (not fulltext). The mode rides on the submit buttons,
// not a hidden input, so it works without JS.
func TestRenderObjectsSearchDefaultUnified(t *testing.T) {
	form := objectSearchFormHTML(t, renderHTML(t, ObjectsPage(objectsPageData{Query: "sam"})))
	if !strings.Contains(form, "Unified Search") {
		t.Errorf("default primary label missing:\n%s", form)
	}
	if tag := firstSubmitTag(form); !strings.Contains(tag, `name="mode"`) || !strings.Contains(tag, `value="unified"`) {
		t.Errorf("default primary submit must carry mode=unified, got %s", tag)
	}
	if strings.Contains(form, `<input type="hidden" name="mode"`) {
		t.Error("mode must be carried by the submit buttons, not a hidden input")
	}
}

// TestRenderObjectsSearchUnknownModeNormalized pins label/dispatch agreement:
// an unrecognised mode value normalizes to the default, so the button reads
// "Unified Search" and no raw unknown value leaks into the control.
func TestRenderObjectsSearchUnknownModeNormalized(t *testing.T) {
	form := objectSearchFormHTML(t, renderHTML(t, ObjectsPage(objectsPageData{Query: "sam", Mode: "bogus"})))
	if !strings.Contains(form, "Unified Search") {
		t.Errorf("unknown mode must render the normalized default label:\n%s", form)
	}
	if tag := firstSubmitTag(form); !strings.Contains(tag, `value="unified"`) {
		t.Errorf("unknown mode must normalize the submitted value to unified, got %s", tag)
	}
	if strings.Contains(form, `value="bogus"`) {
		t.Error("raw unknown mode value must not leak into the control")
	}
	if got := strings.Count(form, `aria-checked="true"`); got != 1 {
		t.Errorf("want exactly one checked mode after normalization, got %d", got)
	}
}

// TestNormalizeObjectsSearchMode pins the normalization contract directly.
func TestNormalizeObjectsSearchMode(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "unified"},
		{"unified", "unified"},
		{"hybrid", "hybrid"},
		{"fulltext", "fulltext"},
		{"bogus", "unified"},
		{"FULLTEXT", "unified"}, // case-sensitive; unknown → default
	} {
		if got := normalizeObjectsSearchMode(tc.in); got != tc.want {
			t.Errorf("normalizeObjectsSearchMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderObjectDetailPage(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", Type: "person", Key: "sam-lee", Status: "suggested",
		Properties: map[string]any{"first_name": "Sam", "relationship": "colleague"},
	}
	edges := []GraphRelationship{{ID: "r1", Type: "assigned_to", SrcID: "c2", DstID: "c1"}}
	related := []GraphObject{{ID: "o2", CanonicalID: "c2", Type: "task", Key: "call dentist"}}
	html := renderHTML(t, ObjectDetailPage(obj, nil, edges, related, nil, nil, "", nil, nil, nil, nil))
	for _, want := range []string{
		"sam-lee", "person", "first_name", "Sam", "relationship", "colleague",
		"assigned to", "call dentist", `href="/objects"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("object detail missing %q", want)
		}
	}

	htmlNoEdges := renderHTML(t, ObjectDetailPage(obj, nil, nil, nil, nil, nil, "", nil, nil, nil, nil))
	if !strings.Contains(htmlNoEdges, "No relationships") {
		t.Error("no-relationships state missing")
	}

	htmlErr := renderHTML(t, ObjectDetailPage(nil, nil, nil, nil, nil, errTest, "", nil, nil, nil, nil))
	if !strings.Contains(htmlErr, "Object unavailable") {
		t.Error("error state missing")
	}

	// flash feedback renders an auto-dismissing toast carrying the message
	htmlFlash := renderHTML(t, ObjectDetailPage(obj, nil, nil, nil, nil, nil, "", errTest, nil, nil, nil))
	if !strings.Contains(htmlFlash, "memory-toast") || !strings.Contains(htmlFlash, errTest.Error()) {
		t.Error("flash toast missing")
	}
}

func TestRenderObjectDetailSimilar(t *testing.T) {
	obj := &GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}
	similar := []SimilarObject{
		{ID: "o2", Type: "person", Key: "samuel-lee", Distance: 0.12},
	}
	html := renderHTML(t, ObjectDetailPage(obj, nil, nil, nil, similar, nil, "", nil, nil, nil, nil))
	for _, want := range []string{
		"Similar objects", "samuel-lee", "88% match", "Merge", `href="/objects/o1/merge?with=o2"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("similar objects section missing %q", want)
		}
	}

	// Without similar data the page defers the section to /objects/:id/similar
	// instead of rendering it inline, so a slow /similar cannot hold the render
	// (issue #1096).
	htmlDeferred := renderHTML(t, ObjectDetailPage(obj, nil, nil, nil, nil, nil, "", nil, nil, nil, nil))
	for _, want := range []string{`hx-get="/objects/o1/similar"`, "Finding similar objects", `hx-target="this"`} {
		if !strings.Contains(htmlDeferred, want) {
			t.Errorf("deferred similar placeholder missing %q", want)
		}
	}

	// The empty state itself still renders from the partial's data path.
	htmlEmpty := renderHTML(t, objectSimilarObjects(obj, nil))
	if !strings.Contains(htmlEmpty, "No similar objects") {
		t.Error("no-similar-objects state missing")
	}
}

func TestRenderObjectDetailEditable(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", Type: "person", Key: "sam-lee", Status: "suggested",
		Labels: []string{"contact", "colleague"},
		Properties: map[string]any{
			"first_name": "Sam",
			"age":        float64(42),
			"nicknames":  []any{"Sammy", "SL"},
		},
	}
	relTypes := []CompiledType{
		{Name: "assigned_to", Label: "Assigned to", SourceType: "task", TargetType: "person"},
		{Name: "works_with", Label: "Works with", SourceType: "person", TargetType: "person"},
		{Name: "located_in", Label: "Located in", SourceType: "place", TargetType: "place"},
	}
	html := renderHTML(t, ObjectDetailPage(obj, relTypes, nil, nil, nil, nil, "", nil, nil, nil, nil))

	// properties form: common fields + one input per property
	for _, want := range []string{
		`name="key"`, `value="sam-lee"`,
		`name="status"`, `value="suggested"`,
		`id="object-labels"`, `placeholder="Add a label..."`, "contact", "colleague",
		`name="prop_first_name"`, `>Sam</textarea>`,
		`name="prop_age"`, `>42</textarea>`,
		`name="prop_nicknames"`, "Save changes",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("properties form missing %q", want)
		}
	}

	// relationship empty state: Connect CTA + modal with type/src/dst fields
	for _, want := range []string{
		"No relationships", "Connect",
		`name="type"`, "Assigned to", "Works with",
		`name="src_id"`, `value="o1"`,
		`name="dst_id"`,
		`action="/objects/o1/relationships"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("relationship modal missing %q", want)
		}
	}
	// rel types that can't touch a person are filtered out when matches exist
	if strings.Contains(html, "Located in") {
		t.Error("relationship modal should filter types not touching the object's type")
	}

	// chat deep link in the header
	for _, want := range []string{
		"Chat about object", `href="/objects/o1/chat"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("object detail header missing %q", want)
		}
	}

	// non-empty relationships still render the list rows
	edges := []GraphRelationship{{ID: "r1", Type: "works_with", SrcID: "o1", DstID: "c2"}}
	related := []GraphObject{{ID: "o2", CanonicalID: "c2", Type: "person", Key: "jamie-doe"}}
	htmlEdges := renderHTML(t, ObjectDetailPage(obj, relTypes, edges, related, nil, nil, "", nil, nil, nil, nil))
	for _, want := range []string{"works with", "jamie-doe", "Relationships"} {
		if !strings.Contains(htmlEdges, want) {
			t.Errorf("relationship list missing %q", want)
		}
	}
	if strings.Contains(htmlEdges, "No relationships") {
		t.Error("non-empty relationship list should not show the empty state")
	}
}

func TestSimilarObjectLabel(t *testing.T) {
	if got := similarObjectLabel(SimilarObject{ID: "o1", Type: "person", Key: "sam-lee"}); got != "sam-lee" {
		t.Errorf("key label = %q, want sam-lee", got)
	}
	// A type-specific property (name) must NOT be used as the label — titles
	// only show fields common to all types (key / type+id).
	if got := similarObjectLabel(SimilarObject{ID: "o1", Type: "person", Properties: map[string]any{"name": "Sam Lee"}}); !strings.Contains(got, "person") || strings.Contains(got, "Sam Lee") {
		t.Errorf("name property label = %q, want type fallback (not the name property)", got)
	}
	if got := similarObjectLabel(SimilarObject{ID: "abcdef123456", Type: "task"}); !strings.Contains(got, "task") {
		t.Errorf("fallback label = %q, want type fallback", got)
	}
}

func TestGraphObjectLabel(t *testing.T) {
	longContent := strings.Repeat("x", 500) // would blow up a title if leaked
	cases := []struct {
		name string
		obj  GraphObject
		want string
	}{
		{"key wins", GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}, "sam-lee"},
		{"type+short id fallback", GraphObject{ID: "abcdef123456", Type: "task"}, "task abcdef1234"},
		{"short id only", GraphObject{ID: "o1"}, "o1"},
		// Type-specific properties must never become the label.
		{"content not used", GraphObject{ID: "o1", Type: "note", Properties: map[string]any{"content": longContent}}, "note o1"},
		{"name property not used", GraphObject{ID: "o1", Type: "person", Properties: map[string]any{"name": "Sam Lee"}}, "person o1"},
		{"title property not used", GraphObject{ID: "o1", Type: "event", Properties: map[string]any{"title": "Team sync"}}, "event o1"},
	}
	for _, tc := range cases {
		if got := graphObjectLabel(tc.obj); got != tc.want {
			t.Errorf("%s: label = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRenderObjectDetailTypeAwareWidgets(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", Type: "note", Key: "sam-note",
		Properties: map[string]any{
			"cleared":  true,
			"amount":   1.5,
			"priority": "high",
			"title":    "a title",
		},
	}
	propDefs := []objectPropertyDef{
		{Name: "amount", Type: "number"},
		{Name: "cleared", Type: "boolean"},
		{Name: "priority", Type: "string", Enum: []string{"low", "high"}},
		{Name: "title", Type: "string"},
	}
	html := renderHTML(t, ObjectDetailPage(obj, nil, nil, nil, nil, nil, "", nil, nil, propDefs, nil))

	// boolean → hidden false + checked toggle (never a text input)
	if !strings.Contains(html, `name="prop_cleared" value="false"`) {
		t.Error("boolean prop: missing hidden false input")
	}
	// Pins ToggleInput's canonical attribute order (class before checked) —
	// semantically identical to the former hand-rolled markup.
	if !strings.Contains(html, `type="checkbox" name="prop_cleared" value="true" class="toggle" checked`) {
		t.Error("boolean prop: expected a checked toggle, got something else")
	}
	if strings.Contains(html, `type="text" name="prop_cleared"`) {
		t.Error("boolean prop must not render as a text input")
	}
	// number → number input with the stored value
	if !strings.Contains(html, `name="prop_amount" type="number" step="any" value="1.5"`) {
		t.Error("number prop: expected a number input carrying the value")
	}
	// enum string → native select with the stored value selected
	if !strings.Contains(html, `<select name="prop_priority"`) || !strings.Contains(html, `<option value="high" selected>high</option>`) {
		t.Error("enum prop: expected a select with the stored option selected")
	}
	// plain string → auto-growing textarea (free-form, may hold long text)
	if !strings.Contains(html, `<textarea name="prop_title"`) || !strings.Contains(html, "a title") {
		t.Error("string prop: expected a textarea carrying the value")
	}
}

func TestDateInputValue(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"2019-03-07T00:00:00Z", "2019-03-07"}, // RFC3339
		{"2019-03-07T00:00:00+01:00", "2019-03-07"},
		{"2019-03-08 14:30", "2019-03-08"}, // space-separated date-time
		{"2019-03-09", "2019-03-09"},       // already yyyy-mm-dd
		{"", ""},                           // empty
		{"not-a-date", ""},                 // no yyyy-mm-dd prefix
		{"2019-03", ""},                    // too short
		{"2019-03-7", ""},                  // malformed, single-digit day
		{"2019-99-99", ""},                 // yyyy-mm-dd-shaped but not a real date
		{"2019-13-01", ""},                 // invalid month
		{"2019-02-30", ""},                 // invalid day for month
	}
	for _, c := range cases {
		if got := dateInputValue(c.in); got != c.want {
			t.Errorf("dateInputValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFilterSimilarMatches(t *testing.T) {
	in := []SimilarObject{
		{ID: "o1", Distance: 0.05}, // 95% — keep
		{ID: "o2", Distance: 0.19}, // 81% — keep
		{ID: "o3", Distance: 0.20}, // 80% — drop (not "over 80%")
		{ID: "o4", Distance: 1.50}, // 0% — drop
	}
	out := filterSimilarMatches(in)
	if len(out) != 2 || out[0].ID != "o1" || out[1].ID != "o2" {
		t.Fatalf("filter = %+v, want [o1 o2]", out)
	}
}

func TestSimilarityLabel(t *testing.T) {
	cases := []struct {
		d    float32
		want string
	}{
		{0.0, "100% match"},
		{0.12, "88% match"},
		{0.5, "50% match"},
		{1.0, "0% match"},
		{1.5, "0% match"},    // clamped
		{-0.2, "100% match"}, // clamped
	}
	for _, c := range cases {
		if got := similarityLabel(c.d); got != c.want {
			t.Errorf("similarityLabel(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestMergeAgentID(t *testing.T) {
	ctx := context.Background()
	newServer := func(f *fakeMemory) *Server { return &Server{memory: f} }

	f := &fakeMemory{agents: []AgentDefinitionSummary{
		{ID: "a1", Name: "one", Enabled: true},
		{ID: "a2", Name: "two", IsDefault: true, Enabled: true},
	}}
	if got := newServer(f).mergeAgentID(ctx); got != "a2" {
		t.Errorf("default agent = %q, want a2", got)
	}
	f = &fakeMemory{agents: []AgentDefinitionSummary{
		{ID: "a1", Name: "one", Enabled: false},
		{ID: "a2", Name: "two", Enabled: true},
	}}
	if got := newServer(f).mergeAgentID(ctx); got != "a2" {
		t.Errorf("first enabled agent = %q, want a2", got)
	}
	f = &fakeMemory{agents: []AgentDefinitionSummary{
		{ID: "a1", Name: "one", Enabled: false},
	}}
	if got := newServer(f).mergeAgentID(ctx); got != "a1" {
		t.Errorf("first agent = %q, want a1", got)
	}
	f = &fakeMemory{}
	if got := newServer(f).mergeAgentID(ctx); got != "" {
		t.Errorf("no agents = %q, want empty", got)
	}
}

func TestResolveEditorAgentID(t *testing.T) {
	ctx := context.Background()
	newServer := func(f *fakeMemory) *Server { return &Server{memory: f} }

	// configured editor agent wins
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "one", IsDefault: true, Enabled: true}},
		settings: map[string]map[string]map[string]any{
			"editor": {"agent_id": {"agentId": "a9"}},
		},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a9" {
		t.Errorf("configured editor agent = %q, want a9", got)
	}

	// empty configured value falls back like an unset one
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "one", IsDefault: true, Enabled: true}},
		settings: map[string]map[string]map[string]any{
			"editor": {"agent_id": {"agentId": ""}},
		},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a1" {
		t.Errorf("empty configured value = %q, want fallback a1", got)
	}

	// unconfigured falls back to mergeAgentID (default → first enabled → first)
	f = &fakeMemory{agents: []AgentDefinitionSummary{
		{ID: "a1", Name: "one", Enabled: true},
		{ID: "a2", Name: "two", IsDefault: true, Enabled: true},
	}}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a2" {
		t.Errorf("fallback = %q, want default agent a2", got)
	}
	f = &fakeMemory{agents: []AgentDefinitionSummary{
		{ID: "a1", Name: "one", Enabled: false},
		{ID: "a2", Name: "two", Enabled: true},
	}}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a2" {
		t.Errorf("fallback = %q, want first enabled agent a2", got)
	}
	f = &fakeMemory{}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "" {
		t.Errorf("no agents = %q, want empty", got)
	}
}

func TestObjectChatInstruction(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", Type: "person", Key: "sam-lee", Status: "suggested",
		Properties: map[string]any{"first_name": "Sam", "_internal": "skip", "empty": ""},
	}
	got := objectChatInstruction(obj)
	for _, want := range []string{
		"Help me work with this knowledge-graph object.",
		"sam-lee", "o1", "person", "suggested",
		"first_name", "Sam",
		"Steps:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("instruction missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "_internal") {
		t.Error("instruction must skip internal (underscore) properties")
	}
}

func TestObjectMergeRedirect(t *testing.T) {
	f := &fakeMemory{
		objects: []GraphObject{
			{ID: "o1", Type: "person", Key: "sam-lee"},
			{ID: "o2", Type: "person", Key: "samuel-lee"},
		},
		similar: []SimilarObject{
			{ID: "o2", Type: "person", Key: "samuel-lee", Distance: 0.12},
		},
		agents: []AgentDefinitionSummary{
			{ID: "a1", Name: "diane", IsDefault: true, Enabled: true},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/:id/merge", s.uiObjectMerge)

	// merge deep link: 303 to /chat with agent + prompt, both labels present
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1/merge?with=o2", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("merge status = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/chat?agent=a1&prompt=") {
		t.Errorf("location = %q, want /chat?agent=a1&prompt=", loc)
	}
	if !strings.Contains(loc, "sam-lee") || !strings.Contains(loc, "samuel-lee") {
		t.Errorf("location missing object labels: %q", loc)
	}

	// missing dst: fail-safe redirect back to the object page
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1/merge", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1" {
		t.Fatalf("missing dst: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	// unknown object: fail-safe redirect back, no 500
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/nope/merge?with=o2", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/nope" {
		t.Fatalf("unknown src: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestObjectsRoutes(t *testing.T) {
	f := &fakeMemory{
		objects: []GraphObject{
			{ID: "o1", CanonicalID: "o1", Type: "person", Key: "sam-lee"},
			{ID: "o2", CanonicalID: "o2", Type: "task", Key: "call dentist"},
			{ID: "o3", CanonicalID: "o3", BranchID: "b1", Type: "person", Key: "branch-person"},
		},
		pageObjects: []GraphObject{
			{ID: "o1", CanonicalID: "o1", Type: "person", Key: "sam-lee"},
			{ID: "o2", CanonicalID: "o2", Type: "task", Key: "call dentist"},
			{ID: "o3", CanonicalID: "o3", BranchID: "b1", Type: "person", Key: "branch-person"},
		},
		relns: []GraphRelationship{
			{ID: "r1", Type: "assigned_to", SrcID: "o2", DstID: "o1"},
		},
		branches: []Branch{{ID: "b1", Name: "plan/next-gen"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects", s.uiObjects)
	e.GET("/objects/:id", s.uiObject)

	// list page (main branch — the b1 object is excluded)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "sam-lee") {
		t.Fatalf("list page: status=%d body=%s", rec.Code, body)
	}
	if strings.Contains(body, "branch-person") {
		t.Fatalf("main branch should exclude branch objects: %s", body)
	}

	// type filter keeps only the matching type
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?type=task", nil))
	body = rec.Body.String()
	if !strings.Contains(body, "call dentist") || strings.Contains(body, "sam-lee") {
		t.Fatalf("type filter: body=%s", body)
	}

	// branch filter shows only that branch's objects
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?branch=b1", nil))
	body = rec.Body.String()
	if !strings.Contains(body, "branch-person") || strings.Contains(body, "sam-lee") {
		t.Fatalf("branch filter: body=%s", body)
	}

	// detail page shows relationships with resolved labels
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1", nil))
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Relationships") || !strings.Contains(body, "call dentist") {
		t.Fatalf("detail page: status=%d body=%s", rec.Code, body)
	}

	// ?updated=1 (PRG from the edit form) surfaces a success toast via the
	// inline flash script.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1?updated=1", nil))
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Object updated.") {
		t.Fatalf("detail page after update: status=%d body=%s", rec.Code, body)
	}

	// ?err= (PRG from a failed edit) surfaces an error toast.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1?err=boom", nil))
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "boom") {
		t.Fatalf("detail page after failed update: status=%d body=%s", rec.Code, body)
	}
}

func TestUIObjectPartialIncludesTitle(t *testing.T) {
	f := &fakeMemory{objects: []GraphObject{{ID: "o1", Type: "person", Key: "sam-lee"}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/:id", s.uiObject)

	// hx-boost navigations fetch partials (no <head>). htmx keeps
	// document.title in sync by extracting a <title> from the swapped
	// fragment, so partials must carry one — otherwise tab titles go stale on
	// every in-page navigation.
	req := httptest.NewRequest(http.MethodGet, "/objects/o1", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Request-Type", "partial")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	if !strings.HasPrefix(body, "<title>") || !strings.Contains(body, "sam-lee") {
		t.Fatalf("partial must open with a <title>, got: %.200s", body)
	}
	if strings.Count(body, "<title>") != 1 {
		t.Fatalf("expected exactly one <title> in partial, got: %s", body)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "<head") {
		t.Fatalf("partial must not include the full HTML shell, got: %.200s", body)
	}
}

func TestUIObjectsPartial(t *testing.T) {
	f := &fakeMemory{
		pageObjects: []GraphObject{
			{ID: "o2", Type: "task", Key: "call dentist"},
			{ID: "o3", Type: "task", Key: "write report"},
		},
		nextPageCursor: "nc-2",
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/partial", s.uiObjectsPartial)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/partial?cursor=nc-1", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	for _, want := range []string{"call dentist", "write report", `id="objects-load-more"`, "hx-swap-oob", "Load more", "cursor=nc-2"} {
		if !strings.Contains(body, want) {
			t.Errorf("partial missing %q in:\n%s", want, body)
		}
	}
	if f.lastPageCursor != "nc-1" {
		t.Errorf("cursor = %q, want nc-1", f.lastPageCursor)
	}
}

func TestUIObjectsPartialFailure(t *testing.T) {
	f := &fakeMemory{pageErr: fmt.Errorf("boom")}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/partial", s.uiObjectsPartial)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/partial?cursor=nc-1", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestRenderObjectsPageStatsUnavailable(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Objects: []GraphObject{{ID: "o1", Type: "person", Key: "sam-lee"}},
		Stats:   &objectsStats{TotalErr: errTest, EmbedErr: errTest},
	}))
	for _, want := range []string{"—", `title="Unavailable"`} {
		if !strings.Contains(html, want) {
			t.Errorf("unavailable stats missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, ">0</p>") {
		t.Error("unavailable stats must not render a numeric 0 value")
	}
}

// TestObjectsStatsDeferredCarriesBranch pins the branch-scoping regression guard
// (issue #1098): the deferred stats placeholder must carry the active branch so
// the object count is scoped the same way the pre-deferral synchronous call was.
func TestObjectsStatsDeferredCarriesBranch(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Objects:  []GraphObject{{ID: "o1", Type: "person", Key: "sam-lee"}},
		BranchID: "b1",
	}))
	if !strings.Contains(html, `hx-get="/objects/stats?branch=b1"`) {
		t.Errorf("deferred stats URL must carry the branch, got:\n%s", html)
	}

	// Without a branch the URL is bare.
	htmlMain := renderHTML(t, ObjectsPage(objectsPageData{
		Objects: []GraphObject{{ID: "o1", Type: "person", Key: "sam-lee"}},
	}))
	if !strings.Contains(htmlMain, `hx-get="/objects/stats"`) {
		t.Errorf("deferred stats URL should omit an empty branch, got:\n%s", htmlMain)
	}
}

// TestObjectsStatsDeferredTargetsItself pins the swap-target regression: the
// page shell's <main> carries hx-target="#main-content", which htmx inherits
// into descendants (implicitInheritance=true). Without an explicit
// hx-target="this" the deferred stats fetch would swap #main-content itself,
// leaving the page showing only the three stat cards with the object browser
// gone.
func TestObjectsStatsDeferredTargetsItself(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Objects: []GraphObject{{ID: "o1", Type: "person", Key: "sam-lee"}},
	}))
	if !strings.Contains(html, `id="objects-stats"`) {
		t.Errorf("deferred stats placeholder id missing, got:\n%s", html)
	}
	if !strings.Contains(html, `hx-target="this"`) {
		t.Errorf("deferred stats placeholder must carry hx-target=\"this\" so it does not inherit #main-content, got:\n%s", html)
	}
}

// TestUIObjectsStatsPartialBranchScoped asserts the deferred stats endpoint
// forwards the branch to CountObjects (the assertion whose absence let the
// unscoped-count regression through).
func TestUIObjectsStatsPartialBranchScoped(t *testing.T) {
	f := &fakeMemory{objectCount: 7}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/stats", s.uiObjectsStatsPartial)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/stats?branch=b1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if f.lastCountBranch != "b1" {
		t.Fatalf("CountObjects branch = %q, want b1", f.lastCountBranch)
	}
	if !strings.Contains(rec.Body.String(), "7") {
		t.Errorf("stats partial missing branch-scoped count 7: %s", rec.Body.String())
	}
}

func TestUIObjectsSearchRoute(t *testing.T) {
	f := &fakeMemory{
		searchResults: []ObjectSearchResult{
			{Object: GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}, Score: 0.9},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects", s.uiObjects)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?q=sam&mode=hybrid", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	if !strings.Contains(body, "sam-lee") || !strings.Contains(body, "0.90") {
		t.Errorf("search body missing result: %s", body)
	}
	if f.lastSearchMode != "hybrid" || f.lastSearchQuery != "sam" {
		t.Errorf("search = %q/%q, want hybrid/sam", f.lastSearchMode, f.lastSearchQuery)
	}
	if strings.Contains(body, "Load more") {
		t.Error("search route must not render Load more")
	}
}

// TestUIObjectsUnifiedRoute exercises GET /objects?mode=unified: it dispatches
// to SearchObjectsUnified and renders the unified row.
func TestUIObjectsUnifiedRoute(t *testing.T) {
	f := &fakeMemory{
		unifiedResults: []ObjectSearchResult{
			{Object: GraphObject{ID: "c1", Type: "person", Key: "sam-lee"}, Score: 0.9},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects", s.uiObjects)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?q=sam&mode=unified", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	if !strings.Contains(body, "sam-lee") || !strings.Contains(body, "0.90") {
		t.Errorf("unified search body missing result: %s", body)
	}
	if f.lastUnifiedQuery != "sam" || f.lastUnifiedLimit != 25 {
		t.Errorf("unified search = %q/%d, want sam/25", f.lastUnifiedQuery, f.lastUnifiedLimit)
	}
	if f.lastSearchMode != "" {
		t.Errorf("fulltext/hybrid SearchObjects must not run for unified mode, got mode %q", f.lastSearchMode)
	}
}

// TestUIObjectsDefaultModeUnified pins the handler default: GET /objects?q=…
// with no mode must dispatch the unified search (the split button's default),
// not full-text. Previously an absent mode fell back to fulltext.
func TestUIObjectsDefaultModeUnified(t *testing.T) {
	f := &fakeMemory{
		unifiedResults: []ObjectSearchResult{
			{Object: GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}, Score: 0.9},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects", s.uiObjects)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?q=sam", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	if f.lastUnifiedQuery != "sam" {
		t.Errorf("omitted mode should dispatch unified search, got unified query %q", f.lastUnifiedQuery)
	}
	if f.lastSearchMode != "" {
		t.Errorf("omitted mode must not dispatch fulltext/hybrid SearchObjects, got mode %q", f.lastSearchMode)
	}
	if !strings.Contains(body, "Unified Search") {
		t.Errorf("default search body missing Unified Search label: %s", body)
	}
}

// TestUIObjectsUnknownModeNormalized pins label/dispatch agreement at the
// handler: an unsupported `mode` normalizes to unified, so the request does NOT
// fall through to the full-text SearchObjects path and the rendered label says
// "Unified Search".
func TestUIObjectsUnknownModeNormalized(t *testing.T) {
	f := &fakeMemory{
		unifiedResults: []ObjectSearchResult{
			{Object: GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}, Score: 0.9},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects", s.uiObjects)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects?q=sam&mode=bogus", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	if f.lastUnifiedQuery != "sam" {
		t.Errorf("unsupported mode must normalize and dispatch unified, got unified query %q", f.lastUnifiedQuery)
	}
	if f.lastSearchMode != "" {
		t.Errorf("unsupported mode must not fall through to fulltext SearchObjects, got mode %q", f.lastSearchMode)
	}
	if !strings.Contains(body, "Unified Search") {
		t.Errorf("unsupported mode body missing normalized label: %s", body)
	}
}

// objectSearchFormHTML returns the objects search form region of the rendered
// page, so split-button assertions cannot accidentally match markup elsewhere.
func objectSearchFormHTML(t *testing.T, html string) string {
	t.Helper()
	i := strings.Index(html, `id="objects-search-form"`)
	if i < 0 {
		t.Fatalf("objects search form not found in:\n%s", html)
	}
	rest := html[i:]
	j := strings.Index(rest, "</form>")
	if j < 0 {
		t.Fatal("objects search form close tag not found")
	}
	return rest[:j]
}

// firstSubmitTag returns the opening tag of the first submit button in html, so
// tests can assert which mode the primary action submits without pinning the
// full attribute order.
func firstSubmitTag(html string) string {
	const marker = `<button type="submit"`
	i := strings.Index(html, marker)
	if i < 0 {
		return ""
	}
	rest := html[i:]
	if j := strings.Index(rest, ">"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// TestRenderObjectsSearchFormLayout pins the /objects search row's layout
// contract (#1377): the search field occupies the remaining row width and the
// submit group sits at the far right of the content area. The grow utility on
// the input wrapper is the mechanism for that, so it is the one class this test
// asserts — the resolved narrow the field previously carried must be gone.
func TestRenderObjectsSearchFormLayout(t *testing.T) {
	form := objectSearchFormHTML(t, renderHTML(t, ObjectsPage(objectsPageData{})))
	if !strings.Contains(form, `name="q"`) {
		t.Fatal("search form missing query input")
	}
	if !strings.Contains(form, "flex-1") {
		t.Error("search input wrapper must grow to fill the row (flex-1)")
	}
	if strings.Contains(form, "sm:max-w-md") {
		t.Error("search input wrapper must not cap its width; the field fills the row (#1377)")
	}
	if qi, si := strings.Index(form, `name="q"`), strings.Index(form, `<button type="submit"`); qi < 0 || si < 0 || qi > si {
		t.Errorf("search field must precede the submit group: input@%d submit@%d", qi, si)
	}
}

// TestUIObjectsKnowledge exercises POST /objects/knowledge: a question renders
// the answer partial, a backend error renders the error state, and an empty
// question redirects back to the browser.
func TestUIObjectsKnowledge(t *testing.T) {
	s := func(f *fakeMemory) *Server { return &Server{cfg: Config{DefaultAgent: "memory"}, memory: f} }

	t.Run("answer renders", func(t *testing.T) {
		f := &fakeMemory{knowledgeAnswer: "Sam is a colleague.", knowledgeSession: "conv-1"}
		e := echo.New()
		e.POST("/objects/knowledge", s(f).uiObjectsKnowledge)

		form := url.Values{}
		form.Set("question", "who is sam?")
		form.Set("branch", "b1")
		req := httptest.NewRequest(http.MethodPost, "/objects/knowledge", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Sam is a colleague.") || !strings.Contains(rec.Body.String(), "conv-1") {
			t.Errorf("answer body = %s", rec.Body.String())
		}
		if f.lastKnowledgeQ != "who is sam?" || f.lastKnowledgeBr != "b1" {
			t.Errorf("query = %q/%q, want who is sam?/b1", f.lastKnowledgeQ, f.lastKnowledgeBr)
		}
	})

	t.Run("error renders", func(t *testing.T) {
		f := &fakeMemory{knowledgeErr: fmt.Errorf("boom")}
		e := echo.New()
		e.POST("/objects/knowledge", s(f).uiObjectsKnowledge)

		form := url.Values{}
		form.Set("question", "who is sam?")
		req := httptest.NewRequest(http.MethodPost, "/objects/knowledge", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Could not get an answer.") {
			t.Errorf("error body = %s", rec.Body.String())
		}
	})

	t.Run("empty question redirects", func(t *testing.T) {
		f := &fakeMemory{}
		e := echo.New()
		e.POST("/objects/knowledge", s(f).uiObjectsKnowledge)

		req := httptest.NewRequest(http.MethodPost, "/objects/knowledge", strings.NewReader("question="))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects" {
			t.Fatalf("status=%d location=%q, want 303 /objects", rec.Code, rec.Header().Get("Location"))
		}
		if f.lastKnowledgeQ != "" {
			t.Errorf("QueryKnowledge called for empty question: %q", f.lastKnowledgeQ)
		}
	})

	t.Run("whitespace-only question redirects", func(t *testing.T) {
		f := &fakeMemory{}
		e := echo.New()
		e.POST("/objects/knowledge", s(f).uiObjectsKnowledge)

		req := httptest.NewRequest(http.MethodPost, "/objects/knowledge", strings.NewReader("question=+++"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects" {
			t.Fatalf("status=%d location=%q, want 303 /objects", rec.Code, rec.Header().Get("Location"))
		}
		if f.lastKnowledgeQ != "" {
			t.Errorf("QueryKnowledge called for whitespace-only question: %q", f.lastKnowledgeQ)
		}
	})
}

func TestUIObjectUpdate(t *testing.T) {
	newServer := func(f *fakeMemory) *Server { return &Server{cfg: Config{DefaultAgent: "memory"}, memory: f} }

	t.Run("full edit redirects to the updated object id", func(t *testing.T) {
		f := &fakeMemory{updateResult: &GraphObject{ID: "o2-new"}}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		form := url.Values{}
		form.Set("key", "sam-lee-v2")
		form.Set("status", "confirmed")
		form.Add("labels[0]", "person")
		form.Add("labels[1]", "vip")
		form.Set("prop_first_name", "Sam")
		form.Set("prop_age", "42")
		form.Set("prop_score", "4.5")
		form.Set("prop_flag", "true")
		form.Set("prop_summary", `"quoted text"`)
		form.Set("prop_empty", "")
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o2-new?updated=1" {
			t.Fatalf("status=%d location=%q, want 303 /objects/o2-new?updated=1", rec.Code, rec.Header().Get("Location"))
		}
		if f.lastUpdateID != "o1" {
			t.Errorf("update id = %q, want o1", f.lastUpdateID)
		}
		got := f.lastUpdateReq
		if got == nil {
			t.Fatal("UpdateObject not called")
		}
		if got.Key == nil || *got.Key != "sam-lee-v2" {
			t.Errorf("key = %v, want sam-lee-v2", got.Key)
		}
		if got.Status == nil || *got.Status != "confirmed" {
			t.Errorf("status = %v, want confirmed", got.Status)
		}
		if !got.ReplaceLabels {
			t.Error("replaceLabels = false, want true")
		}
		if len(got.Labels) != 2 || got.Labels[0] != "person" || got.Labels[1] != "vip" {
			t.Errorf("labels = %v, want [person vip]", got.Labels)
		}
		want := map[string]any{
			"first_name": "Sam",       // bare word → raw string
			"age":        float64(42), // number literal → parsed
			"score":      float64(4.5),
			"flag":       true,
			"summary":    "quoted text", // JSON string → unquoted
		}
		if len(got.Properties) != len(want) {
			t.Fatalf("properties = %v, want %v", got.Properties, want)
		}
		for k, v := range want {
			if got.Properties[k] != v {
				t.Errorf("property %q = %#v, want %#v", k, got.Properties[k], v)
			}
		}
	})

	t.Run("blank key and status stay nil", func(t *testing.T) {
		f := &fakeMemory{}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		form := url.Values{}
		form.Set("labels", "")
		form.Set("prop_title", "Hello")
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		got := f.lastUpdateReq
		if got.Key != nil || got.Status != nil {
			t.Errorf("key/status = %v/%v, want nil", got.Key, got.Status)
		}
		if len(got.Properties) != 1 || got.Properties["title"] != "Hello" {
			t.Errorf("properties = %v, want {title: Hello}", got.Properties)
		}
	})

	t.Run("update error redirects back to the submitted object with err flash", func(t *testing.T) {
		f := &fakeMemory{updateObjectErr: fmt.Errorf("boom")}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		form := url.Values{}
		form.Set("key", "k")
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1?err=boom" {
			t.Fatalf("status=%d location=%q, want 303 /objects/o1?err=boom", rec.Code, rec.Header().Get("Location"))
		}
	})
}

func TestUIObjectRelationshipCreate(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/objects/:id/relationships", s.uiObjectRelationshipCreate)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/objects/o1/relationships", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	// empty type: redirect back, nothing created
	rec := post("src_id=o2&dst_id=o1")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1" {
		t.Fatalf("empty type: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastCreateRelReq != nil {
		t.Fatalf("relationship created with empty type: %+v", f.lastCreateRelReq)
	}

	// empty src: redirect back, nothing created
	rec = post("type=assigned_to&dst_id=o1")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1" {
		t.Fatalf("empty src: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastCreateRelReq != nil {
		t.Fatalf("relationship created with empty src: %+v", f.lastCreateRelReq)
	}

	// valid: created and redirected back
	rec = post("type=assigned_to&src_id=o2&dst_id=o1")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1" {
		t.Fatalf("valid: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastCreateRelReq == nil {
		t.Fatal("CreateRelationship not called")
	}
	if f.lastCreateRelReq.Type != "assigned_to" || f.lastCreateRelReq.SrcID != "o2" || f.lastCreateRelReq.DstID != "o1" {
		t.Errorf("req = %+v", f.lastCreateRelReq)
	}
}

func TestUIObjectSearch(t *testing.T) {
	f := &fakeMemory{
		ftsResults: []GraphObject{
			{ID: "o1", Type: "person", Key: "sam-lee"},
			{ID: "o2", Type: "task", Key: "call dentist"},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/search", s.uiObjectSearch)

	// empty query → 200 with an empty JSON array
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/search?q=", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty q: status=%d body=%q, want 200 []", rec.Code, rec.Body.String())
	}
	if f.ftsQuery != "" {
		t.Errorf("fts called for empty query: %q", f.ftsQuery)
	}

	// non-empty → objects with id/label/type, type filter forwarded
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/search?q=sam&type=person", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if f.ftsQuery != "sam" || f.ftsTypeFilter != "person" {
		t.Errorf("fts = %q/%q, want sam/person", f.ftsQuery, f.ftsTypeFilter)
	}
	var results []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].ID != "o1" || results[0].Label != "sam-lee" || results[0].Type != "person" {
		t.Errorf("results = %+v, want sam-lee + call dentist", results)
	}
}

func TestUIObjectNew(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person", Label: "Person", Properties: json.RawMessage(`{"first_name":{"type":"string"},"_internal":{"type":"string"}}`)},
		{Name: "task"},
	}}

	t.Run("renders the type selector from compiled types", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled}
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.GET("/objects/new", s.uiObjectNew)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/new", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{"New object", `method="get"`, `name="type"`, "Pick a type…", "Person", "task", "Pick a type to see its fields."} {
			if !strings.Contains(body, want) {
				t.Errorf("new page missing %q", want)
			}
		}
		if !strings.Contains(body, `action="/objects/new"`) {
			t.Error("form action missing")
		}
	})

	t.Run("selected type reveals its property inputs", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled}
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.GET("/objects/new", s.uiObjectNew)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/new?type=person", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `name="prop_first_name"`) {
			t.Errorf("person property input missing: %s", body)
		}
		if strings.Contains(body, "prop__internal") {
			t.Error("underscore-prefixed internal property should be hidden")
		}
		if strings.Contains(body, "Pick a type to see its fields.") {
			t.Error("type pick hint should be hidden once a type is selected")
		}
	})

	t.Run("compiled types failure renders the error state", func(t *testing.T) {
		f := &fakeMemory{blueprintErr: fmt.Errorf("boom")}
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.GET("/objects/new", s.uiObjectNew)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/new", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Failed to load object types") {
			t.Errorf("error state missing: %s", rec.Body.String())
		}
	})

	t.Run("no object types renders schema-install guidance", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.GET("/objects/new", s.uiObjectNew)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/new", nil))

		body := rec.Body.String()
		if !strings.Contains(body, "No object types available") || !strings.Contains(body, "Install a schema pack") {
			t.Errorf("empty-types guidance missing: %s", body)
		}
	})
}

func TestUIObjectCreate(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", Properties: json.RawMessage(`{"first_name":{"type":"string"},"active":{"type":"boolean"},"tags":{"type":"array"}}`)}}}
	newServer := func(f *fakeMemory) *Server { return &Server{cfg: Config{DefaultAgent: "memory"}, memory: f} }
	post := func(e *echo.Echo, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/objects", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("missing type rejected with values preserved", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("key", "sam-lee")
		form.Add("labels[0]", "vip")
		rec := post(e, form)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 re-render (not redirect)", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Select an object type.") {
			t.Errorf("body missing validation message: %s", body)
		}
		if f.lastCreateObjectReq != nil {
			t.Errorf("CreateObject called with no type: %+v", f.lastCreateObjectReq)
		}
	})

	t.Run("unknown type rejected", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "alien")
		rec := post(e, form)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 re-render", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Unknown object type.") {
			t.Errorf("body missing unknown-type message: %s", rec.Body.String())
		}
		if f.lastCreateObjectReq != nil {
			t.Errorf("CreateObject called with unknown type: %+v", f.lastCreateObjectReq)
		}
	})

	t.Run("valid submit builds the payload and redirects", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-new"}}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "person")
		form.Set("key", "sam-lee")
		form.Set("status", "confirmed")
		form.Add("labels[0]", "a")
		form.Add("labels[1]", "b")
		form.Set("prop_first_name", "Sam")
		form.Set("prop_age", "42")
		form.Set("prop_summary", `"quoted text"`)
		form.Add("prop_tags[0]", "x")
		form.Add("prop_tags[1]", "y")
		form.Set("prop_empty", "")
		rec := post(e, form)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o-new" {
			t.Fatalf("status=%d location=%q, want 303 /objects/o-new", rec.Code, rec.Header().Get("Location"))
		}
		got := f.lastCreateObjectReq
		if got == nil {
			t.Fatal("CreateObject not called")
		}
		if got.Type != "person" {
			t.Errorf("type = %q, want person", got.Type)
		}
		if got.Key == nil || *got.Key != "sam-lee" {
			t.Errorf("key = %v, want sam-lee", got.Key)
		}
		if got.Status == nil || *got.Status != "confirmed" {
			t.Errorf("status = %v, want confirmed", got.Status)
		}
		if len(got.Labels) != 2 || got.Labels[0] != "a" || got.Labels[1] != "b" {
			t.Errorf("labels = %v, want [a b]", got.Labels)
		}
		want := map[string]any{
			"first_name": "Sam",           // bare word → raw string
			"age":        float64(42),     // number literal → parsed
			"summary":    "quoted text",   // JSON string → unquoted
			"tags":       []any{"x", "y"}, // TagList values → JSON elements in order
		}
		if len(got.Properties) != len(want) {
			t.Fatalf("properties = %v, want %v", got.Properties, want)
		}
		for k, v := range want {
			if !reflect.DeepEqual(got.Properties[k], v) {
				t.Errorf("property %q = %#v, want %#v", k, got.Properties[k], v)
			}
		}
	})

	t.Run("boolean property unchecked submits false, checked submits true", func(t *testing.T) {
		postType := func(val string) map[string]any {
			f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-bool"}}
			e := echo.New()
			e.POST("/objects", newServer(f).uiObjectCreate)

			form := url.Values{}
			form.Set("type", "person")
			form.Set("prop_active", "false") // the hidden input the form always sends
			if val == "true" {
				form.Add("prop_active", "true") // checked toggle, submitted after the hidden
			}
			rec := post(e, form)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", rec.Code)
			}
			return f.lastCreateObjectReq.Properties
		}

		props := postType("false")
		if v, ok := props["active"].(bool); !ok || v != false {
			t.Errorf("unchecked active = %#v, want bool false", props["active"])
		}
		props = postType("true")
		if v, ok := props["active"].(bool); !ok || v != true {
			t.Errorf("checked active = %#v, want bool true", props["active"])
		}
	})

	t.Run("minimal submit sends only the type", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-min"}}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "person")
		rec := post(e, form)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o-min" {
			t.Fatalf("status=%d location=%q, want 303 /objects/o-min", rec.Code, rec.Header().Get("Location"))
		}
		got := f.lastCreateObjectReq
		if got.Type != "person" || got.Key != nil || got.Status != nil || len(got.Labels) != 0 || len(got.Properties) != 0 {
			t.Errorf("req = %+v, want only type set", got)
		}
	})

	t.Run("create failure re-renders with error and preserved values", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled, createObjectErr: fmt.Errorf("boom")}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "person")
		form.Set("key", "sam-lee")
		form.Set("status", "confirmed")
		form.Add("labels[0]", "vip")
		form.Set("prop_first_name", "Sam")
		rec := post(e, form)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 re-render", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{"Could not create the object.", `value="sam-lee"`, `value="confirmed"`, `>Sam</textarea>`, `placeholder="Add a label..."`, "vip"} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q", want)
			}
		}
	})
}

func TestRenderObjectCreatePage(t *testing.T) {
	objectTypes := []CompiledType{
		{Name: "person", Label: "Person", Properties: json.RawMessage(`{"born":{"type":"date"},"age":{"type":"number"},"active":{"type":"boolean"},"tags":{"type":"array"},"name":{"type":"string"},"meta":{"type":"object"},"_internal":{"type":"string"}}`)},
		{Name: "task"},
	}
	propDefs := []objectPropertyDef{
		{Name: "active", Type: "boolean"},
		{Name: "age", Type: "number"},
		{Name: "born", Type: "date"},
		{Name: "meta", Type: "object"},
		{Name: "name", Type: "string"},
		{Name: "tags", Type: "array"},
	}

	t.Run("renders the type selector and TagList labels input", func(t *testing.T) {
		form := objectCreateForm{Type: "person", Key: "k", Status: "s", Labels: []string{"vip"}, Props: map[string][]string{"first_name": {"Sam"}}}
		html := renderHTML(t, ObjectCreatePage(objectTypes, propDefs, form, []string{"vip", "urgent"}, nil))
		for _, want := range []string{
			"New object", `name="type"`, "Pick a type…", "Person", "task",
			`name="key"`, `name="status"`, `value="k"`, "Create object", `action="/objects"`,
			// labels are a chip TagList, not a comma-separated text input
			"Labels/Tags", "Comma-separated labels.",
			`id="object-labels"`, `placeholder="Add a label..."`,
			// suggestions feed a combobox listbox dropdown, not a <datalist>
			`role="listbox"`,
			`x-show="open && filteredSuggestions.length > 0"`,
			`role="option"`,
			`x-for="(s, i) in filteredSuggestions"`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("create page missing %q", want)
			}
		}
		if strings.Contains(html, "No object types available") {
			t.Error("empty-types guidance should not render when types exist")
		}
	})

	t.Run("renders type-aware property widgets", func(t *testing.T) {
		form := objectCreateForm{Type: "person", Props: map[string][]string{}}
		html := renderHTML(t, ObjectCreatePage(objectTypes, propDefs, form, nil, nil))
		for _, want := range []string{
			// date → date picker
			`name="prop_born" type="date"`,
			// number → numeric input
			`name="prop_age" type="number" step="any"`,
			// boolean → hidden false + daisyUI toggle switch
			`type="hidden" name="prop_active" value="false"`,
			`type="checkbox" name="prop_active" value="true"`,
			`class="toggle"`,
			// array/object → TagList chip inputs (no suggestions)
			`id="prop-tags"`, `placeholder="Add an item..."`,
			`id="prop-meta"`,
			// string → auto-growing textarea (free-form text may be long)
			`<textarea name="prop_name" rows="1" data-autogrow`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("widget HTML missing %q in:\n%s", want, html)
			}
		}
		// old widgets are gone: JSON textareas and the checkbox + "true" label
		for _, gone := range []string{"<textarea name=\"prop_tags\"", "<textarea name=\"prop_meta\"", `class="checkbox checkbox-sm"`, `>true</span>`} {
			if strings.Contains(html, gone) {
				t.Errorf("widget HTML should not contain %q in:\n%s", gone, html)
			}
		}
		// no suggestions passed → no listbox dropdown (nor datalist) anywhere
		for _, gone := range []string{`role="listbox"`, "<datalist"} {
			if strings.Contains(html, gone) {
				t.Errorf("no %q expected without suggestions in:\n%s", gone, html)
			}
		}
		// internal properties are never surfaced as inputs
		if strings.Contains(html, "prop__internal") {
			t.Error("underscore-prefixed internal property should be hidden")
		}
	})

	t.Run("boolean toggle reflects a true form value", func(t *testing.T) {
		form := objectCreateForm{Type: "person", Props: map[string][]string{"active": {"true"}}}
		html := renderHTML(t, ObjectCreatePage(objectTypes, propDefs, form, nil, nil))
		// Pins ToggleInput's canonical attribute order (class before checked).
		if !strings.Contains(html, `type="checkbox" name="prop_active" value="true" class="toggle" checked`) {
			t.Errorf("checked toggle missing in:\n%s", html)
		}
	})

	t.Run("renders schema-install guidance when no types", func(t *testing.T) {
		html := renderHTML(t, ObjectCreatePage(nil, nil, objectCreateForm{}, nil, nil))
		if !strings.Contains(html, "No object types available") {
			t.Error("empty-types guidance missing")
		}
	})

	t.Run("renders the load error state", func(t *testing.T) {
		html := renderHTML(t, ObjectCreatePage(nil, nil, objectCreateForm{}, nil, errTest))
		if !strings.Contains(html, "Failed to load object types") {
			t.Error("load error state missing")
		}
	})
}

func TestCompiledTypePropertyDefs(t *testing.T) {
	objectTypes := []CompiledType{
		{Name: "person", Properties: json.RawMessage(`{"zeta":{"type":"string"},"first_name":{"type":"string","description":"First name"},"age":{"type":"number"},"_internal":{"type":"string"}}`)},
		{Name: "note", Properties: json.RawMessage(`{"content":{"type":"string","widget":"textarea"},"category":{"type":"string","enum":["preference","fact","correction"]},"source":{"type":"string","widget":"input"}}`)},
		{Name: "plain", Properties: json.RawMessage(`{}`)},
	}

	// parses type per property; sorted; underscore-prefixed skipped
	got := compiledTypePropertyDefs(objectTypes, "person")
	want := []objectPropertyDef{
		{Name: "age", Type: "number"},
		{Name: "first_name", Type: "string"},
		{Name: "zeta", Type: "string"},
	}
	if len(got) != len(want) {
		t.Fatalf("defs = %+v, want %+v", got, want)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("defs[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// unknown type → nil
	if got := compiledTypePropertyDefs(objectTypes, "nope"); got != nil {
		t.Errorf("unknown type = %v, want nil", got)
	}

	// widget and enum hints are preserved from the raw property schema
	noteDefs := compiledTypePropertyDefs(objectTypes, "note")
	wantNote := []objectPropertyDef{
		{Name: "category", Type: "string", Enum: []string{"preference", "fact", "correction"}},
		{Name: "content", Type: "string", Widget: "textarea"},
		{Name: "source", Type: "string", Widget: "input"},
	}
	if len(noteDefs) != len(wantNote) {
		t.Fatalf("note defs = %+v, want %+v", noteDefs, wantNote)
	}
	for i := range wantNote {
		if !reflect.DeepEqual(noteDefs[i], wantNote[i]) {
			t.Errorf("note defs[%d] = %+v, want %+v", i, noteDefs[i], wantNote[i])
		}
	}

	// type with no properties → empty (not nil)
	if got := compiledTypePropertyDefs(objectTypes, "plain"); len(got) != 0 {
		t.Errorf("empty props = %v, want empty", got)
	}

	// property schema without a type field → def with empty Type
	notyped := []CompiledType{{Name: "t", Properties: json.RawMessage(`{"title":{"description":"x"}}`)}}
	got2 := compiledTypePropertyDefs(notyped, "t")
	if len(got2) != 1 || got2[0].Name != "title" || got2[0].Type != "" {
		t.Errorf("untyped prop = %+v, want {title }", got2)
	}

	// non-object Properties JSON → nil
	bad := []CompiledType{{Name: "bad", Properties: json.RawMessage(`["a","b"]`)}, {Name: "bad2", Properties: json.RawMessage(`not json`)}}
	if got := compiledTypePropertyDefs(bad, "bad"); got != nil {
		t.Errorf("array props = %v, want nil", got)
	}
	if got := compiledTypePropertyDefs(bad, "bad2"); got != nil {
		t.Errorf("invalid props = %v, want nil", got)
	}

	// empty type list → nil
	if got := compiledTypePropertyDefs(nil, "person"); got != nil {
		t.Errorf("no types = %v, want nil", got)
	}
}

func TestPropertyInput(t *testing.T) {
	cases := []struct {
		def  objectPropertyDef
		want string
	}{
		{objectPropertyDef{Name: "born", Type: "date"}, `name="prop_born" type="date"`},
		{objectPropertyDef{Name: "age", Type: "number"}, `name="prop_age" type="number" step="any"`},
		{objectPropertyDef{Name: "streak", Type: "integer"}, `name="prop_streak" type="number" step="1"`},
		{objectPropertyDef{Name: "tags", Type: "array"}, `id="prop-tags"`},
		{objectPropertyDef{Name: "meta", Type: "object"}, `id="prop-meta"`},
		{objectPropertyDef{Name: "name", Type: "string"}, `<textarea name="prop_name" rows="1" data-autogrow`},
		{objectPropertyDef{Name: "other", Type: "unknown"}, `<textarea name="prop_other" rows="1" data-autogrow`},
		{objectPropertyDef{Name: "note", Type: ""}, `<textarea name="prop_note" rows="1" data-autogrow`},
		{objectPropertyDef{Name: "summary", Type: "string", Widget: "textarea"}, `<textarea name="prop_summary" rows="1" data-autogrow`},
		{objectPropertyDef{Name: "source", Type: "string", Widget: "input"}, `<input name="prop_source" type="text"`},
		{objectPropertyDef{Name: "category", Type: "string", Enum: []string{"preference", "fact"}}, `<select name="prop_category"`},
		{objectPropertyDef{Name: "notes", Type: "string", Widget: "bogus"}, `<textarea name="prop_notes" rows="1" data-autogrow`},
	}
	for _, c := range cases {
		html := renderHTML(t, propertyInput(c.def, nil))
		if !strings.Contains(html, c.want) {
			t.Errorf("%s: HTML missing %q in:\n%s", c.def.Name, c.want, html)
		}
	}

	// enum select lists the allowed values
	htmlSel := renderHTML(t, propertyInput(objectPropertyDef{Name: "category", Type: "string", Enum: []string{"preference", "fact", "correction"}}, nil))
	for _, want := range []string{`<select name="prop_category"`, `value="preference"`, `value="correction"`} {
		if !strings.Contains(htmlSel, want) {
			t.Errorf("enum select missing %q in:\n%s", want, htmlSel)
		}
	}

	// enum select keeps a stored value that is outside the declared enum
	htmlOff := renderHTML(t, propertyInput(objectPropertyDef{Name: "category", Type: "string", Enum: []string{"preference", "fact"}}, []string{"custom_value"}))
	if !strings.Contains(htmlOff, `value="custom_value" selected`) {
		t.Errorf("off-enum value not preserved in:\n%s", htmlOff)
	}
	// enum select marks the matching option selected for a known value
	htmlOn := renderHTML(t, propertyInput(objectPropertyDef{Name: "category", Type: "string", Enum: []string{"preference", "fact"}}, []string{"fact"}))
	if !strings.Contains(htmlOn, `value="fact" selected`) {
		t.Errorf("matching enum value not selected in:\n%s", htmlOn)
	}

	// array/object render a TagList chip input (no name= prop field on the
	// text input itself — values submit via Alpine-rendered hidden inputs)
	for _, name := range []string{"tags", "meta"} {
		html := renderHTML(t, propertyInput(objectPropertyDef{Name: name, Type: "array"}, []string{"x"}))
		if !strings.Contains(html, `placeholder="Add an item..."`) || strings.Contains(html, `name="prop_`+name+`" type="text"`) {
			t.Errorf("%s: expected TagList input in:\n%s", name, html)
		}
	}

	// scalar inputs reflect the first value
	htmlVal := renderHTML(t, propertyInput(objectPropertyDef{Name: "born", Type: "date"}, []string{"2026-01-02"}))
	if !strings.Contains(htmlVal, `value="2026-01-02"`) {
		t.Errorf("date input missing value in:\n%s", htmlVal)
	}

	// boolean renders the hidden false + daisyUI toggle pair, reflecting the value
	html := renderHTML(t, propertyInput(objectPropertyDef{Name: "active", Type: "boolean"}, []string{"true"}))
	for _, want := range []string{
		`type="hidden" name="prop_active" value="false"`,
		// Pins ToggleInput's canonical attribute order (class before checked).
		`type="checkbox" name="prop_active" value="true" class="toggle" checked`,
		`class="toggle"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("boolean HTML missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "checkbox checkbox-sm") || strings.Contains(html, ">true</span>") {
		t.Errorf("boolean should render a toggle, not a labeled checkbox:\n%s", html)
	}
	htmlUnchecked := renderHTML(t, propertyInput(objectPropertyDef{Name: "active", Type: "boolean"}, []string{"false"}))
	if strings.Contains(htmlUnchecked, "checked") {
		t.Errorf("unchecked boolean should not render checked:\n%s", htmlUnchecked)
	}

	// multi-line string fields carry a live character counter, seeded with the
	// server-rendered count and grouped for readability.
	multiLine := []struct {
		def  objectPropertyDef
		val  string
		want string
	}{
		{objectPropertyDef{Name: "name", Type: "string"}, "abc", "3 characters"},
		{objectPropertyDef{Name: "name", Type: "string"}, "", "0 characters"},
		{objectPropertyDef{Name: "name", Type: "string"}, "x", "1 character"},
		{objectPropertyDef{Name: "other", Type: "unknown"}, strings.Repeat("x", 1234), "1,234 characters"},
		{objectPropertyDef{Name: "note", Type: ""}, "hello", "5 characters"},
		{objectPropertyDef{Name: "summary", Type: "string", Widget: "textarea"}, "hi", "2 characters"},
		{objectPropertyDef{Name: "notes", Type: "string", Widget: "bogus"}, "ok", "2 characters"},
	}
	for _, c := range multiLine {
		html := renderHTML(t, propertyInput(c.def, []string{c.val}))
		for _, want := range []string{`data-char-counter`, `data-testid="char-count"`, `data-char-count-value`, c.want} {
			if !strings.Contains(html, want) {
				t.Errorf("%s: counter HTML missing %q in:\n%s", c.def.Name, want, html)
			}
		}
	}

	// every other widget type omits the counter — a count on a date, select,
	// number, toggle, or chip input is noise.
	notMultiLine := []objectPropertyDef{
		{Name: "born", Type: "date"},
		{Name: "age", Type: "number"},
		{Name: "streak", Type: "integer"},
		{Name: "tags", Type: "array"},
		{Name: "meta", Type: "object"},
		{Name: "source", Type: "string", Widget: "input"},
		{Name: "category", Type: "string", Enum: []string{"preference", "fact"}},
		{Name: "active", Type: "boolean"},
	}
	for _, def := range notMultiLine {
		html := renderHTML(t, propertyInput(def, []string{"x"}))
		if strings.Contains(html, `data-testid="char-count"`) {
			t.Errorf("%s: counter must not render on non-multi-line fields:\n%s", def.Name, html)
		}
	}
}

func TestCharCountLabel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "0 characters"},
		{"a", "1 character"},
		{"abc", "3 characters"},
		{"abcd", "4 characters"},
		{strings.Repeat("x", 1234), "1,234 characters"},
		{strings.Repeat("x", 41594), "41,594 characters"},
		// Line-ending normalization: a browser rewrites a textarea's CRLF/CR to
		// LF, so the server-rendered count must match the client's LF count.
		{"a\r\nb", "3 characters"},
		{"a\rb", "3 characters"},
		{"\r\n", "1 character"},
		{"\r", "1 character"},
		{"a\r\nb\r\nc", "5 characters"},
	}
	for _, c := range cases {
		if got := charCountLabel(c.in); got != c.want {
			t.Errorf("charCountLabel(len=%d) = %q, want %q", len(c.in), got, c.want)
		}
	}
}

func TestObjectPropertiesCardLongTextFallback(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", Type: "note", Key: "n1",
		Properties: map[string]any{
			"content":  "a long body", // no schema def → fallback multi-line field
			"source":   "https://example.com",
			"category": "fact",
			"amount":   1.5,
		},
	}
	propDefs := []objectPropertyDef{
		{Name: "source", Type: "string", Widget: "input"},
		{Name: "category", Type: "string", Enum: []string{"fact", "opinion"}},
		{Name: "amount", Type: "number"},
	}
	html := renderHTML(t, objectPropertiesCard(obj, propDefs, nil))

	// the un-schema'd stored property renders the long-text field with a counter
	if !strings.Contains(html, `name="prop_content"`) || !strings.Contains(html, `data-autogrow`) {
		t.Errorf("fallback property missing auto-growing textarea in:\n%s", html)
	}
	if !strings.Contains(html, "11 characters") {
		t.Errorf("fallback property missing server-rendered counter in:\n%s", html)
	}
	// exactly one counter across the card: only the multi-line field has one
	if n := strings.Count(html, `data-testid="char-count"`); n != 1 {
		t.Errorf("counter rendered %d times, want 1 (multi-line only):\n%s", n, html)
	}
}

func TestIndexedFormValues(t *testing.T) {
	// contiguous indices collect in ascending order
	v := url.Values{}
	v.Add("labels[0]", "a")
	v.Add("labels[1]", "b")
	v.Add("labels[2]", "c")
	if got := indexedFormValues(v, "labels"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("indexed = %v, want [a b c]", got)
	}

	// a gap stops collection at the first missing index
	v2 := url.Values{}
	v2.Add("labels[0]", "a")
	v2.Add("labels[2]", "c")
	if got := indexedFormValues(v2, "labels"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("gapped = %v, want [a]", got)
	}

	// no indexed fields → nil
	if got := indexedFormValues(url.Values{}, "labels"); got != nil {
		t.Errorf("empty = %v, want nil", got)
	}
	if got := indexedFormValues(url.Values{"labels": {"x"}}, "labels"); got != nil {
		t.Errorf("scalar only = %v, want nil", got)
	}

	// bases are independent
	v3 := url.Values{}
	v3.Add("labels[0]", "a")
	v3.Add("props[0]", "p")
	if got := indexedFormValues(v3, "labels"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("labels = %v, want [a]", got)
	}
	if got := indexedFormValues(v3, "props"); !reflect.DeepEqual(got, []string{"p"}) {
		t.Errorf("props = %v, want [p]", got)
	}
}

func TestDistinctObjectLabels(t *testing.T) {
	objects := []GraphObject{
		{ID: "1", Labels: []string{"vip", "contact"}},
		{ID: "2", Labels: []string{"contact"}},
		{ID: "3", Labels: []string{"", "vip"}},
		{ID: "4"},
	}
	labels := distinctObjectLabels(objects)
	if len(labels) != 2 || labels[0] != "contact" || labels[1] != "vip" {
		t.Fatalf("labels = %v, want [contact vip]", labels)
	}
	if got := distinctObjectLabels(nil); got != nil {
		t.Errorf("nil objects = %v, want nil", got)
	}
}

func TestUIObjectChat(t *testing.T) {
	newServer := func(f *fakeMemory) *Server { return &Server{cfg: Config{DefaultAgent: "memory"}, memory: f} }
	newRouter := func(f *fakeMemory) (*Server, *echo.Echo) {
		s := newServer(f)
		e := echo.New()
		e.GET("/objects/:id/chat", s.uiObjectChat)
		return s, e
	}
	get := func(e *echo.Echo, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	f := &fakeMemory{
		objects: []GraphObject{
			{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam-lee", Status: "suggested"},
		},
		agents: []AgentDefinitionSummary{
			{ID: "a1", Name: "diane", IsDefault: true, Enabled: true},
		},
		createConvID: "conv-9",
	}
	_, e := newRouter(f)

	rec := get(e, "/objects/o1/chat")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/chat?c=conv-9&agent=a1" {
		t.Errorf("location = %q, want /chat?c=conv-9&agent=a1", loc)
	}
	if f.convCanonicalID != "c1" {
		t.Errorf("canonicalId = %q, want c1", f.convCanonicalID)
	}
	if f.convTitle != "sam-lee" {
		t.Errorf("title = %q, want sam-lee", f.convTitle)
	}
	if !strings.Contains(f.convMessage, "Help me work with this knowledge-graph object.") {
		t.Errorf("message = %q, want object instruction", f.convMessage)
	}

	// unknown object: fail-safe redirect back
	rec = get(e, "/objects/nope/chat")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/nope" {
		t.Fatalf("unknown object: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	// no agent resolves: redirect back with an ?err= flash explaining to
	// create an agent first; no conversation is created.
	f2 := &fakeMemory{
		objects: []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam-lee"}},
	}
	_, e2 := newRouter(f2)
	rec = get(e2, "/objects/o1/chat")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("no agent: status=%d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/objects/o1?err=") {
		t.Fatalf("no agent: location=%q, want /objects/o1?err=...", loc)
	}
	if !strings.Contains(loc, "agent") {
		t.Errorf("no agent flash should mention creating an agent: location=%q", loc)
	}
	if f2.convCanonicalID != "" || f2.convTitle != "" {
		t.Errorf("conversation should not be created without an agent: canonical=%q title=%q", f2.convCanonicalID, f2.convTitle)
	}

	// conversation creation fails: redirect back, no 500
	f3 := &fakeMemory{
		objects:       []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam-lee"}},
		agents:        []AgentDefinitionSummary{{ID: "a1", Name: "diane", IsDefault: true, Enabled: true}},
		createConvErr: fmt.Errorf("boom"),
	}
	_, e3 := newRouter(f3)
	rec = get(e3, "/objects/o1/chat")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1" {
		t.Fatalf("create err: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}
