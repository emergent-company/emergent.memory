package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

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
// actor picker listing real project members (Users) and agent definitions
// (Agents), where every row submits the whole (actor_type, actor_id) pair.
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
		// Each filter control is named by its own legend.
		`>Provenance</legend>`, `>Actor</legend>`,
		`name="actor"`, `aria-haspopup="menu"`, "Any actor",
		`role="group" aria-label="Users"`, `role="group" aria-label="Agents"`,
		"Sam Lee", "diane",
		`name="actor" value="user:u1"`,
		`name="actor" value="agent:11111111-1111-1111-1111-111111111111"`,
		// The active actor is checked and shown in the closed control.
		`aria-checked="true"`,
		`aria-label="Actor: diane"`,
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
	// The free-text actor-id input is gone: actors are chosen from the picker.
	if strings.Contains(html, `aria-label="Actor id"`) {
		t.Error("actor id text input should be replaced by the actor picker")
	}

	// The actor picker is always available; without a selection it stays on
	// "Any actor" and applies nothing.
	noActor := renderHTML(t, ObjectsPage(objectsPageData{
		Objects: []GraphObject{{ID: "o1", Type: "note"}},
	}))
	if !strings.Contains(noActor, `aria-label="Actor: Any actor"`) {
		t.Error("actor picker should always render in the browse filter")
	}
	if !strings.Contains(noActor, `name="actor" value=""`) {
		t.Error("empty actor picker must offer the Any actor row")
	}
	if !strings.Contains(noActor, `aria-checked="true"`) {
		t.Error("Any actor must render as the checked row when no actor is active")
	}
}

// TestRenderObjectsActorFilterFallbackOption pins that an active actor pair
// which is not among the loaded options (a former member, or one passed by URL)
// still renders in the closed control, so the filter reflects the active state
// instead of silently resetting to "Any actor".
func TestRenderObjectsActorFilterFallbackOption(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Objects:   []GraphObject{{ID: "o1", Type: "note"}},
		ActorType: "user",
		ActorID:   "deadbeef-0000-0000-0000-000000000000",
	}))
	for _, want := range []string{
		// The closed control states the active identity…
		"User · deadbeef",
		`aria-label="Actor: User · deadbeef`,
		// …and clearing the filter stays available.
		`name="actor" value=""`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("fallback actor state missing %q", want)
		}
	}
	// An identity that is not in the loaded list has no row to pick (it is
	// already active); only the closed control names it.
	if strings.Contains(html, `value="user:deadbeef-0000-0000-0000-000000000000"`) {
		t.Error("an unknown actor must not be offered as a pickable row")
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
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, edges, related, nil, nil, "", nil, nil, nil, nil, nil, nil, ""))
	for _, want := range []string{
		"sam-lee", "person", "first_name", "Sam", "relationship", "colleague",
		"assigned to", "call dentist", `href="/objects"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("object detail missing %q", want)
		}
	}

	htmlNoEdges := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", nil, nil, nil, nil, nil, nil, ""))
	if !strings.Contains(htmlNoEdges, "No relationships") {
		t.Error("no-relationships state missing")
	}

	htmlErr := renderHTML(t, ObjectDetailPage(nil, objectBranchRef{}, nil, nil, nil, nil, nil, errTest, "", nil, nil, nil, nil, nil, nil, ""))
	if !strings.Contains(htmlErr, "Object unavailable") {
		t.Error("error state missing")
	}

	// flash feedback renders an auto-dismissing toast carrying the message
	htmlFlash := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", errTest, nil, nil, nil, nil, nil, ""))
	if !strings.Contains(htmlFlash, "memory-toast") || !strings.Contains(htmlFlash, errTest.Error()) {
		t.Error("flash toast missing")
	}
}

func TestRenderObjectDetailSimilar(t *testing.T) {
	obj := &GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}
	similar := []SimilarObject{
		{ID: "o2", Type: "person", Key: "samuel-lee", Distance: 0.12},
	}
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, similar, nil, "", nil, nil, nil, nil, nil, nil, ""))
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
	htmlDeferred := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", nil, nil, nil, nil, nil, nil, ""))
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
	// person derives from no compiled type here, so a board-enabled control is
	// supplied explicitly to exercise the workflow-status field (the only status
	// editor for a type that declares a workflow).
	status := objectStatusControlFor([]CompiledType{{Name: "person", BoardEnabled: true}}, "person")
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, relTypes, nil, nil, nil, nil, "", nil, nil, nil, nil, status, nil, ""))

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
	htmlEdges := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, relTypes, edges, related, nil, nil, "", nil, nil, nil, nil, nil, nil, ""))
	for _, want := range []string{"works with", "jamie-doe", "Relationships"} {
		if !strings.Contains(htmlEdges, want) {
			t.Errorf("relationship list missing %q", want)
		}
	}
	if strings.Contains(htmlEdges, "No relationships") {
		t.Error("non-empty relationship list should not show the empty state")
	}
}

// TestRenderObjectDetailTwoPropertyCards pins the detail page's split into a
// built-in "Properties" card and the type's "<Type> Properties" card — the same
// two-panel rule the read-only preview drawer follows, applied to the editable
// surface: platform fields never blur into schema fields.
func TestRenderObjectDetailTwoPropertyCards(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", Type: "Person", Key: "sam-lee", Assignee: "jamie",
		Version: 3, CreatedAt: "2026-10-01T09:00:00Z",
		CanonicalID: "11111111-1111-1111-1111-111111111111",
		Labels:      []string{"contact"},
		Properties:  map[string]any{"first_name": "Sam", "entry_point": "the door"},
	}
	propDefs := []objectPropertyDef{
		{Name: "first_name", Type: "string", Title: "First name"},
		{Name: "notes", Type: "string", Description: "Free-form notes."},
	}
	status := objectStatusControlFor([]CompiledType{{Name: "Person", BoardEnabled: true}}, "Person")
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", nil, nil, propDefs, nil, status, nil, "Person"))

	if got := strings.Count(html, ">Properties</h2>"); got != 1 {
		t.Errorf("built-in Properties heading rendered %d times, want 1:\n%s", got, html)
	}
	if !strings.Contains(html, ">Person Properties</h2>") {
		t.Fatalf("schema card heading missing:\n%s", html)
	}
	if got := strings.Count(html, "Save changes"); got != 1 {
		t.Errorf("Save rendered %d times, want a single terminal bar: %s", got, html)
	}

	builtin, schema, ok := strings.Cut(html, `data-testid="object-type-properties"`)
	if !ok {
		t.Fatalf("schema card container missing:\n%s", html)
	}
	// Card 1: the platform fields plus the read-only record facts.
	for _, want := range []string{
		`id="object-key"`, `id="object-status"`, `id="object-labels"`,
		"Assignee", "jamie", "Version", "v3", "Created",
		`11111111-1111-1111-1111-111111111111`,
		`data-testid="object-record-facts"`,
	} {
		if !strings.Contains(builtin, want) {
			t.Errorf("built-in card missing %q:\n%s", want, builtin)
		}
	}
	for _, bad := range []string{`name="prop_first_name"`, `name="prop_notes"`, `name="prop_entry_point"`} {
		if strings.Contains(builtin, bad) {
			t.Errorf("built-in card must not carry the schema field %q", bad)
		}
	}

	// Card 2: declared properties in schema order (including one with no stored
	// value), then the stored key the schema does not declare, humanised with the
	// raw key as its hint.
	for _, want := range []string{
		`name="prop_first_name"`, "First name", "first_name",
		`name="prop_notes"`, "Free-form notes.",
		`name="prop_entry_point"`, ">Entry point<", ">entry_point<",
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema card missing %q:\n%s", want, schema)
		}
	}
	if strings.Contains(schema, `id="object-key"`) {
		t.Errorf("schema card must not carry the platform key field:\n%s", schema)
	}
}

// TestObjectTypePropertiesCardOmitted pins the card's absence rule: nothing to
// edit means no card at all (matching the drawer), and an unresolved type label
// falls back to a neutral heading rather than an unnamed one.
func TestObjectTypePropertiesCardOmitted(t *testing.T) {
	empty := renderHTML(t, objectTypePropertiesCard(&GraphObject{ID: "o1", Type: "Person", Key: "p1"}, nil, "Person"))
	if strings.Contains(empty, "Properties") {
		t.Errorf("schema card with no rows must not render:\n%s", empty)
	}

	obj := &GraphObject{ID: "o2", Type: "Person", Key: "p2", Properties: map[string]any{"nick": "sam"}}
	html := renderHTML(t, objectTypePropertiesCard(obj, nil, ""))
	if !strings.Contains(html, ">Type Properties</h2>") {
		t.Errorf("unresolved type label should fall back to a neutral heading:\n%s", html)
	}
}

// TestObjectTypePropertyHint pins the single hint line under a schema field: the
// machine key when the label is an authored title (property-labels requires the
// name stay visible), otherwise the schema's own description.
func TestObjectTypePropertyHint(t *testing.T) {
	cases := []struct {
		name string
		def  objectPropertyDef
		want string
	}{
		{"authored title keeps the key visible", objectPropertyDef{Name: "first_name", Title: "First name"}, "first_name"},
		{"title equal to the name adds no hint", objectPropertyDef{Name: "title", Title: "title"}, ""},
		{"description stands in for an untitled field", objectPropertyDef{Name: "notes", Description: "Free-form notes."}, "Free-form notes."},
		{"nothing to say", objectPropertyDef{Name: "notes"}, ""},
	}
	for _, tc := range cases {
		if got := objectTypePropertyHint(tc.def); got != tc.want {
			t.Errorf("%s: objectTypePropertyHint = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestObjectTypePropertyRows pins the merge order the schema card renders:
// declared properties first in schema order (a declared-but-unstored one still
// gets a row so it can be filled in), then stored keys the schema does not
// declare, in key order, humanised with the raw key as the hint.
func TestObjectTypePropertyRows(t *testing.T) {
	obj := GraphObject{Properties: map[string]any{"zeta": "z", "entry_point": "the door", "b_declared": "b"}}
	defs := []objectPropertyDef{{Name: "b_declared"}, {Name: "a_declared"}}
	rows := objectTypePropertyRows(obj, defs)

	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, r.Name)
	}
	want := []string{"b_declared", "a_declared", "entry_point", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if !rows[0].Declared || !reflect.DeepEqual(rows[0].Values, []string{"b"}) {
		t.Errorf("declared stored row = %+v, want Declared with the stored value", rows[0])
	}
	if !rows[1].Declared || len(rows[1].Values) != 0 {
		t.Errorf("declared unstored row = %+v, want Declared and empty", rows[1])
	}
	if rows[2].Declared || rows[2].Label != "Entry point" || rows[2].Hint != "entry_point" {
		t.Errorf("undeclared row = %+v, want humanised label with the raw key as hint", rows[2])
	}
	if !reflect.DeepEqual(rows[2].Values, []string{"the door"}) {
		t.Errorf("undeclared row values = %v, want [the door]", rows[2].Values)
	}
	// A key whose humanised label only differs by case needs no hint line.
	if label := objectPreviewFieldLabel("zeta"); label != "Zeta" {
		t.Errorf("humanised label = %q, want Zeta", label)
	}
	if rows[3].Hint != "" {
		t.Errorf("case-only difference must not add a hint, got %q", rows[3].Hint)
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
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", nil, nil, propDefs, nil, nil, nil, ""))

	// boolean → three-state select (unset/true/false), never a text input
	if !strings.Contains(html, `<select name="prop_cleared"`) {
		t.Error("boolean prop: expected a select")
	}
	if !strings.Contains(html, `<option value="true" selected>Yes</option>`) {
		t.Error("boolean prop: expected Yes selected for a stored true")
	}
	if strings.Contains(html, `name="prop_cleared" value="false"`) {
		t.Error("boolean prop must not post a hidden false (that invents a value)")
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

func TestDefaultAgentID(t *testing.T) {
	ctx := context.Background()
	newServer := func(f *fakeMemory) *Server { return &Server{memory: f} }

	f := &fakeMemory{agents: []AgentDefinitionSummary{
		{ID: "a1", Name: "one", Enabled: true},
		{ID: "a2", Name: "two", IsDefault: true, Enabled: true},
	}}
	if got := newServer(f).defaultAgentID(ctx); got != "a2" {
		t.Errorf("default agent = %q, want a2", got)
	}
	f = &fakeMemory{agents: []AgentDefinitionSummary{
		{ID: "a1", Name: "one", Enabled: false},
		{ID: "a2", Name: "two", Enabled: true},
	}}
	if got := newServer(f).defaultAgentID(ctx); got != "a2" {
		t.Errorf("first enabled agent = %q, want a2", got)
	}
	f = &fakeMemory{agents: []AgentDefinitionSummary{
		{ID: "a1", Name: "one", Enabled: false},
	}}
	if got := newServer(f).defaultAgentID(ctx); got != "a1" {
		t.Errorf("first agent = %q, want a1", got)
	}
	f = &fakeMemory{}
	if got := newServer(f).defaultAgentID(ctx); got != "" {
		t.Errorf("no agents = %q, want empty", got)
	}
}

func TestResolveEditorAgentID(t *testing.T) {
	ctx := context.Background()
	newServer := func(f *fakeMemory) *Server { return &Server{memory: f} }
	// The tool set an agent needs to run object edits (see editorAgentEligibility).
	editTools := []string{"entity-update", "relationship-create"}
	editor := func(id, name string, tools ...string) *AgentDefinition {
		return &AgentDefinition{ID: id, Name: name, Enabled: true, Tools: tools}
	}

	// the configured editor agent wins when it is editor-capable
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{
			{ID: "a1", Name: "one", IsDefault: true, Enabled: true},
			{ID: "a9", Name: "editor", Enabled: true},
		},
		defs: map[string]*AgentDefinition{
			"a1": editor("a1", "one", editTools...),
			"a9": editor("a9", "editor", editTools...),
		},
		settings: map[string]map[string]map[string]any{
			"editor": {"agent_id": {"agentId": "a9"}},
		},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a9" {
		t.Errorf("configured editor agent = %q, want a9", got)
	}

	// a configured agent that cannot edit is skipped, not blindly trusted
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{
			{ID: "a1", Name: "one", IsDefault: true, Enabled: true},
			{ID: "a9", Name: "toolless", Enabled: true},
		},
		defs: map[string]*AgentDefinition{
			"a1": editor("a1", "one", editTools...),
			"a9": editor("a9", "toolless", "entity-query"),
		},
		settings: map[string]map[string]map[string]any{
			"editor": {"agent_id": {"agentId": "a9"}},
		},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a1" {
		t.Errorf("incapable configured agent = %q, want the capable default a1", got)
	}

	// a stored id whose definition is gone falls back the same way
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "one", Enabled: true}},
		defs:   map[string]*AgentDefinition{"a1": editor("a1", "one", editTools...)},
		settings: map[string]map[string]map[string]any{
			"editor": {"agent_id": {"agentId": "deleted"}},
		},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a1" {
		t.Errorf("deleted configured agent = %q, want fallback a1", got)
	}

	// empty configured value falls back like an unset one
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "one", IsDefault: true, Enabled: true}},
		defs:   map[string]*AgentDefinition{"a1": editor("a1", "one", editTools...)},
		settings: map[string]map[string]map[string]any{
			"editor": {"agent_id": {"agentId": ""}},
		},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a1" {
		t.Errorf("empty configured value = %q, want fallback a1", got)
	}

	// unconfigured: default-if-capable, then first enabled capable, then first capable
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{
			{ID: "a1", Name: "one", Enabled: true},
			{ID: "a2", Name: "two", IsDefault: true, Enabled: true},
		},
		defs: map[string]*AgentDefinition{
			"a1": editor("a1", "one", editTools...),
			"a2": editor("a2", "two", editTools...),
		},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a2" {
		t.Errorf("fallback = %q, want default agent a2", got)
	}
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{
			{ID: "a1", Name: "one", Enabled: false},
			{ID: "a2", Name: "two", Enabled: true},
		},
		defs: map[string]*AgentDefinition{
			"a1": editor("a1", "one", editTools...),
			"a2": editor("a2", "two", editTools...),
		},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a2" {
		t.Errorf("fallback = %q, want first enabled agent a2", got)
	}
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "one", Enabled: false}},
		defs:   map[string]*AgentDefinition{"a1": editor("a1", "one", editTools...)},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "a1" {
		t.Errorf("fallback = %q, want the first capable agent a1", got)
	}

	// no capable agent at all: empty, so the chat surfaces an error instead of
	// starting a run the agent cannot finish
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "one", IsDefault: true, Enabled: true}},
		defs:   map[string]*AgentDefinition{"a1": editor("a1", "one", "entity-query")},
	}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "" {
		t.Errorf("no capable agent = %q, want empty", got)
	}
	f = &fakeMemory{}
	if got := newServer(f).resolveEditorAgentID(ctx); got != "" {
		t.Errorf("no agents = %q, want empty", got)
	}
}

func TestResolveMergeAgentID(t *testing.T) {
	ctx := context.Background()
	newServer := func(f *fakeMemory) *Server { return &Server{memory: f} }
	// The tool set an agent needs to run a merge (see agentToolEligibility).
	mergeTools := []string{"entity-create", "entity-delete", "entity-edges-get", "relationship-create"}
	def := func(id, name string, enabled bool, tools ...string) *AgentDefinition {
		return &AgentDefinition{ID: id, Name: name, Enabled: enabled, Tools: tools}
	}

	// the configured merge agent wins when it is merge-capable
	f := &fakeMemory{
		agents: []AgentDefinitionSummary{
			{ID: "a1", Name: "one", IsDefault: true, Enabled: true},
			{ID: "a2", Name: "two", Enabled: true},
		},
		defs: map[string]*AgentDefinition{
			"a1": def("a1", "one", true, mergeTools...),
			"a2": def("a2", "two", true, mergeTools...),
		},
		settings: map[string]map[string]map[string]any{
			"merge": {"agent_id": {"agentId": "a2"}},
		},
	}
	if got := newServer(f).resolveMergeAgentID(ctx); got != "a2" {
		t.Errorf("configured merge agent = %q, want a2", got)
	}

	// a configured agent that lost a required tool is skipped in favour of the
	// default agent — never run a merge with an incapable agent
	f.defs["a2"] = def("a2", "two", true, "entity-create", "entity-edges-get", "relationship-create")
	if got := newServer(f).resolveMergeAgentID(ctx); got != "a1" {
		t.Errorf("incapable configured merge agent = %q, want fallback a1", got)
	}

	// the default agent is skipped when it cannot merge
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{
			{ID: "a1", Name: "one", IsDefault: true, Enabled: true},
			{ID: "a2", Name: "two", Enabled: true},
		},
		defs: map[string]*AgentDefinition{
			"a1": def("a1", "one", true),
			"a2": def("a2", "two", true, mergeTools...),
		},
	}
	if got := newServer(f).resolveMergeAgentID(ctx); got != "a2" {
		t.Errorf("incapable default agent = %q, want first capable enabled a2", got)
	}

	// a disabled but capable agent is still the only candidate
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "one", Enabled: false}},
		defs:   map[string]*AgentDefinition{"a1": def("a1", "one", false, mergeTools...)},
	}
	if got := newServer(f).resolveMergeAgentID(ctx); got != "a1" {
		t.Errorf("only capable agent (disabled) = %q, want a1", got)
	}

	// no merge-capable agent resolves to empty, so the deep link can explain
	f = &fakeMemory{
		agents: []AgentDefinitionSummary{{ID: "a1", Name: "one", IsDefault: true, Enabled: true}},
		defs:   map[string]*AgentDefinition{"a1": def("a1", "one", true, "entity-query")},
	}
	if got := newServer(f).resolveMergeAgentID(ctx); got != "" {
		t.Errorf("no merge-capable agent = %q, want empty", got)
	}

	// no agents at all
	if got := newServer(&fakeMemory{}).resolveMergeAgentID(ctx); got != "" {
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
	// Searches are recommended, not required (editorRequirement): the edit and
	// relationship creation stay unconditional steps, and searching is marked
	// conditional. A regression to the old "or search for related objects as
	// needed" wording must red this test.
	if !strings.Contains(got, "create or update the object as needed, and create relationships") {
		t.Errorf("instruction must keep the edit and relationship creation unconditional:\n%s", got)
	}
	if strings.Contains(got, "or search for related objects as needed") {
		t.Error("instruction must not present searching as an unconditional step")
	}
	if !strings.Contains(got, "when your tools allow it") {
		t.Errorf("instruction must mark searching as conditional:\n%s", got)
	}
}

func TestMergeInstruction(t *testing.T) {
	src := &GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}
	dst := &GraphObject{ID: "o2", Type: "person", Key: "samuel-lee"}
	got := mergeInstruction(src, dst)
	for _, want := range []string{
		"Merge the following two knowledge-graph objects",
		"sam-lee", "samuel-lee",
		"propose the merged shape",
		"retire the source",
		"entity-edges-get", "relationship-create", "relationship-delete",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("merge instruction missing %q:\n%s", want, got)
		}
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
		defs: map[string]*AgentDefinition{
			"a1": {ID: "a1", Name: "diane", Enabled: true, Tools: []string{
				"entity-create", "entity-delete", "entity-edges-get", "relationship-create",
			}},
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

	// no merge-capable agent: the deep link must explain the configuration gap
	// instead of starting a run the agent cannot complete
	f.defs["a1"] = &AgentDefinition{ID: "a1", Name: "diane", Enabled: true, Tools: []string{"entity-query"}}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1/merge?with=o2", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("no capable agent: status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "no+merge-capable+agent") && !strings.Contains(loc, "no%20merge-capable%20agent") {
		t.Errorf("no capable agent: location = %q, want an error redirect", loc)
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
	for _, want := range []string{"—", "unavailable"} {
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

	t.Run("clearing a stored property submits an explicit delete", func(t *testing.T) {
		f := &fakeMemory{}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		form := url.Values{}
		form.Set("propm_present_title", "1")
		form.Set("prop_title", "")
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		got := f.lastUpdateReq
		if got == nil {
			t.Fatal("UpdateObject not called")
		}
		v, ok := got.Properties["title"]
		if !ok || v != nil {
			t.Errorf("emptied title = %#v (present=%v), want an explicit nil delete", v, ok)
		}
	})

	t.Run("untouched boolean stays unset instead of persisting false", func(t *testing.T) {
		f := &fakeMemory{}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		form := url.Values{}
		form.Set("key", "renamed")
		form.Set("propm_present_active", "1")
		form.Set("prop_active", "") // Not set
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		got := f.lastUpdateReq
		v, ok := got.Properties["active"]
		if !ok || v != nil {
			t.Errorf("unset boolean = %#v (present=%v), want nil delete, not false", v, ok)
		}
	})

	t.Run("emptied tag list deletes the stored array", func(t *testing.T) {
		f := &fakeMemory{}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		form := url.Values{}
		form.Set("propm_present_tags", "1")
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		got := f.lastUpdateReq
		v, ok := got.Properties["tags"]
		if !ok || v != nil {
			t.Errorf("emptied tags = %#v (present=%v), want nil delete", v, ok)
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

// TestUIObjectStatusDropdownWiring proves the handlers plumb the compiled
// type's board config through to the rendered status control, for both the
// detail (edit) page and the create page.
func TestUIObjectStatusDropdownWiring(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Task", Label: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "in_progress", "done"}},
		{Name: "Note", Label: "Note"},
	}}
	f := &fakeMemory{
		compiled: compiled,
		objects:  []GraphObject{{ID: "o1", CanonicalID: "o1", Type: "Task", Key: "t1", Status: "in_progress"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/:id", s.uiObject)
	e.GET("/objects/new", s.uiObjectNew)

	t.Run("detail page selects the board statuses", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `<select id="object-status" name="status"`) {
			t.Fatalf("detail page missing status select in:\n%s", body)
		}
		for _, opt := range []string{"ready", "in_progress", "done"} {
			if !strings.Contains(body, `value="`+opt+`"`) {
				t.Errorf("detail select missing option %q in:\n%s", opt, body)
			}
		}
		if !strings.Contains(body, `value="in_progress" selected`) {
			t.Errorf("detail current status not selected in:\n%s", body)
		}
		if !strings.Contains(body, "Workflow status") {
			t.Errorf("detail status field should be labelled Workflow status in:\n%s", body)
		}
	})

	t.Run("create page selects the board statuses", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/new?type=Task", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `<select id="object-status" name="status"`) {
			t.Fatalf("create page missing status select in:\n%s", body)
		}
		for _, opt := range []string{"ready", "in_progress", "done"} {
			if !strings.Contains(body, `value="`+opt+`"`) {
				t.Errorf("create select missing option %q in:\n%s", opt, body)
			}
		}
	})

	t.Run("type with no workflow renders no status field (user report)", func(t *testing.T) {
		f.objects = append(f.objects, GraphObject{ID: "o2", CanonicalID: "o2", Type: "Note", Key: "n1", Status: "anything"})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o2", nil))
		body := rec.Body.String()
		if strings.Contains(body, `id="object-status"`) || strings.Contains(body, `name="status"`) {
			t.Fatalf("no-workflow type must not render a built-in status field in:\n%s", body)
		}
		if strings.Contains(body, "Workflow status") {
			t.Errorf("no-workflow type must not render the workflow-status label in:\n%s", body)
		}
	})
}

func TestUIObjectCreate(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", BoardEnabled: true, Properties: json.RawMessage(`{"first_name":{"type":"string"},"active":{"type":"boolean"},"tags":{"type":"array"}}`)}}}
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
		html := renderHTML(t, ObjectCreatePage(objectTypes, propDefs, form, []string{"vip", "urgent"}, nil, objectStatusControlFor(objectTypes, "person")))
		for _, want := range []string{
			"New object", `name="type"`, "Pick a type…", "Person", "task",
			`name="key"`, `value="k"`, "Create object", `action="/objects"`,
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
		// The person type declares no workflow, so no built-in status field is
		// rendered — its schema has no status property either.
		if strings.Contains(html, `id="object-status"`) {
			t.Error("a type with no workflow must not render the built-in status field")
		}
		if strings.Contains(html, "No object types available") {
			t.Error("empty-types guidance should not render when types exist")
		}
	})

	t.Run("renders type-aware property widgets", func(t *testing.T) {
		form := objectCreateForm{Type: "person", Props: map[string][]string{}}
		html := renderHTML(t, ObjectCreatePage(objectTypes, propDefs, form, nil, nil, nil))
		for _, want := range []string{
			// date → date picker
			`name="prop_born" type="date"`,
			// number → numeric input
			`name="prop_age" type="number" step="any"`,
			// boolean → three-state select
			`<select name="prop_active"`,
			`<option value="true"`,
			`Not set`,
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
		html := renderHTML(t, ObjectCreatePage(objectTypes, propDefs, form, nil, nil, nil))
		if !strings.Contains(html, `<option value="true" selected>Yes</option>`) {
			t.Errorf("stored true must select Yes in:\n%s", html)
		}
	})

	t.Run("renders schema-install guidance when no types", func(t *testing.T) {
		html := renderHTML(t, ObjectCreatePage(nil, nil, objectCreateForm{}, nil, nil, nil))
		if !strings.Contains(html, "No object types available") {
			t.Error("empty-types guidance missing")
		}
	})

	t.Run("renders the load error state", func(t *testing.T) {
		html := renderHTML(t, ObjectCreatePage(nil, nil, objectCreateForm{}, nil, errTest, nil))
		if !strings.Contains(html, "Failed to load object types") {
			t.Error("load error state missing")
		}
	})
}

// TestObjectStatusControl pins the visibility and option-source rules for the
// built-in workflow-status field: a type gets the field only when it declares a
// workflow (boardEnabled or allowedStatuses). Declared allowedStatuses win; a
// board-enabled type with none falls back to the canonical board order. A type
// with no declaration has no workflow status — its own `status` schema property,
// when it has one, is edited with the rest of its properties — so no field is
// rendered for it, for an unknown type, or before a type is picked.
func TestObjectStatusControl(t *testing.T) {
	boardWithStatuses := []CompiledType{{
		Name: "Task", BoardEnabled: true,
		AllowedStatuses: []string{"triage", "doing", "done"},
	}}
	boardWithoutStatuses := []CompiledType{{Name: "Task", BoardEnabled: true}}
	// The user-reported shape: a type whose registry row declares neither
	// boardEnabled nor allowedStatuses, so it has no workflow status to set.
	noWorkflow := []CompiledType{{Name: "Scenario"}}

	ctrl := objectStatusControlFor(boardWithStatuses, "Task")
	if !ctrl.Show || !slices.Equal(ctrl.Options, []string{"triage", "doing", "done"}) {
		t.Errorf("declared statuses control = %+v, want shown with [triage doing done]", ctrl)
	}
	ctrl = objectStatusControlFor(boardWithoutStatuses, "Task")
	if !ctrl.Show || !slices.Equal(ctrl.Options, boardStatusOrder) {
		t.Errorf("board canonical fallback control = %+v, want shown with %v", ctrl, boardStatusOrder)
	}
	if ctrl = objectStatusControlFor(noWorkflow, "Scenario"); ctrl.Show || len(ctrl.Options) > 0 {
		t.Errorf("no-workflow type control = %+v, want hidden", ctrl)
	}
	if ctrl = objectStatusControlFor(noWorkflow, "Unknown"); ctrl.Show {
		t.Errorf("unknown type control = %+v, want hidden", ctrl)
	}
	if ctrl = objectStatusControlFor(boardWithStatuses, ""); ctrl.Show {
		t.Errorf("no type selected control = %+v, want hidden", ctrl)
	}
	if ctrl = objectStatusControlUnavailable(); !ctrl.Show || !ctrl.Free {
		t.Errorf("degraded control = %+v, want shown as free text", ctrl)
	}

	// A shadowed (losing) declaration listed before the effective override must
	// not leak its statuses; the winner's declared statuses win.
	shadowedFirst := []CompiledType{
		{Name: "Task", Shadowed: true, AllowedStatuses: []string{"stale"}},
		{Name: "Task", AllowedStatuses: []string{"triage", "doing"}},
	}
	if ctrl = objectStatusControlFor(shadowedFirst, "Task"); !slices.Equal(ctrl.Options, []string{"triage", "doing"}) {
		t.Errorf("shadowed declaration leaked: options = %v, want [triage doing]", ctrl.Options)
	}
}

// TestObjectStatusField pins the rendered control: a type that declares a
// workflow gets a native select labelled "Workflow status", with the current
// value selected and a stored value outside the set preserved. A type that
// declares no workflow renders no status field at all — its own `status` schema
// property (when it has one) is the status editor.
func TestObjectStatusField(t *testing.T) {
	boardStatuses := []string{"ready", "in_progress", "review", "revision", "blocked", "done"}

	renderSelect := func(current string) string {
		obj := &GraphObject{ID: "o1", Type: "Task", Key: "t1", Status: current}
		objectTypes := []CompiledType{{Name: "Task", BoardEnabled: true, AllowedStatuses: boardStatuses}}
		return renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", nil, nil, nil, nil, objectStatusControlFor(objectTypes, "Task"), nil, ""))
	}

	t.Run("edit page renders a select of the allowed statuses", func(t *testing.T) {
		html := renderSelect("review")
		if !strings.Contains(html, `<select id="object-status" name="status"`) {
			t.Fatalf("edit page missing status select in:\n%s", html)
		}
		for _, opt := range boardStatuses {
			if !strings.Contains(html, `value="`+opt+`"`) {
				t.Errorf("status select missing option %q in:\n%s", opt, html)
			}
		}
		if !strings.Contains(html, `value="review" selected`) {
			t.Errorf("current status not marked selected in:\n%s", html)
		}
		if !strings.Contains(html, "In progress") {
			t.Errorf("status labels should be humanised in:\n%s", html)
		}
		if !strings.Contains(html, "Workflow status") {
			t.Errorf("status field should be labelled Workflow status in:\n%s", html)
		}
		if strings.Contains(html, `name="status" type="text"`) {
			t.Errorf("board type should not render a text input in:\n%s", html)
		}
	})

	t.Run("blank pick option only when the current value is empty", func(t *testing.T) {
		html := renderSelect("")
		if !strings.Contains(html, `<option value="" selected>Pick…</option>`) {
			t.Errorf("empty status should render a blank selected option in:\n%s", html)
		}
		for _, opt := range boardStatuses {
			if strings.Contains(html, `value="`+opt+`" selected`) {
				t.Errorf("no status should be selected when empty; %q was in:\n%s", opt, html)
			}
		}
	})

	t.Run("stored status outside the allowed set is preserved", func(t *testing.T) {
		html := renderSelect("shipped")
		if !strings.Contains(html, `<option value="shipped" selected>`) {
			t.Errorf("off-list status should be kept as a selected option in:\n%s", html)
		}
	})

	t.Run("create page renders the same status select", func(t *testing.T) {
		objectTypes := []CompiledType{{Name: "Task", BoardEnabled: true, AllowedStatuses: boardStatuses}}
		form := objectCreateForm{Type: "Task", Status: "in_progress"}
		html := renderHTML(t, ObjectCreatePage(objectTypes, nil, form, nil, nil, objectStatusControlFor(objectTypes, "Task")))
		if !strings.Contains(html, `<select id="object-status" name="status"`) {
			t.Fatalf("create page missing status select in:\n%s", html)
		}
		for _, opt := range boardStatuses {
			if !strings.Contains(html, `value="`+opt+`"`) {
				t.Errorf("create status select missing option %q in:\n%s", opt, html)
			}
		}
		if !strings.Contains(html, `value="in_progress" selected`) {
			t.Errorf("create current status not marked selected in:\n%s", html)
		}
		if !strings.Contains(html, "Workflow status") {
			t.Errorf("create status field should be labelled Workflow status in:\n%s", html)
		}
	})

	t.Run("type with no workflow renders no status field (user report)", func(t *testing.T) {
		// A Scenario declares a typed `status` enum property but no board config:
		// the built-in workflow field must be absent, and the typed property is
		// the status editor.
		obj := &GraphObject{ID: "o1", Type: "Scenario", Key: "S-005", Properties: map[string]any{"status": "proposed"}}
		objectTypes := []CompiledType{{
			Name:       "Scenario",
			Properties: json.RawMessage(`{"status":{"type":"string","enum":["proposed","active","archived"]}}`),
		}}
		html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", nil, nil, nil, nil, objectStatusControlFor(objectTypes, "Scenario"), nil, ""))
		if strings.Contains(html, `id="object-status"`) {
			t.Errorf("no-workflow type must not render the built-in status field in:\n%s", html)
		}
		if strings.Contains(html, "Workflow status") {
			t.Errorf("no-workflow type must not render the workflow-status label in:\n%s", html)
		}
		if !strings.Contains(html, `name="prop_status"`) {
			t.Errorf("the declared status property should render as the status editor in:\n%s", html)
		}

		createHTML := renderHTML(t, ObjectCreatePage(objectTypes, compiledTypePropertyDefs(objectTypes, "Scenario"), objectCreateForm{Type: "Scenario"}, nil, nil, objectStatusControlFor(objectTypes, "Scenario")))
		if strings.Contains(createHTML, `id="object-status"`) {
			t.Errorf("no-workflow type must not render the built-in status field on create in:\n%s", createHTML)
		}
	})

	t.Run("detail handler degrades to text when compiled types fail to load", func(t *testing.T) {
		// The handler (not just the component) must pick the degraded control
		// when GetCompiledTypes fails, otherwise a stored status silently
		// disappears from the edit form.
		f := &fakeMemory{
			blueprintErr: errTest,
			objects:      []GraphObject{{ID: "o1", CanonicalID: "o1", Type: "Task", Key: "t1", Status: "in_progress"}},
		}
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.GET("/objects/:id", s.uiObject)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1", nil))
		body := rec.Body.String()
		if !strings.Contains(body, `name="status" type="text"`) {
			t.Fatalf("unavailable schema should fall back to a text status input:\n%s", body)
		}
		if !strings.Contains(body, `value="in_progress"`) {
			t.Errorf("degraded control should keep the stored value:\n%s", body)
		}
	})

	t.Run("degraded edit form keeps the value editable as text", func(t *testing.T) {
		obj := &GraphObject{ID: "o1", Type: "Task", Key: "t1", Status: "in_progress"}
		html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", nil, nil, nil, nil, objectStatusControlUnavailable(), nil, ""))
		if !strings.Contains(html, `name="status" type="text"`) {
			t.Errorf("unavailable schema should fall back to a text input in:\n%s", html)
		}
		if !strings.Contains(html, `value="in_progress"`) {
			t.Errorf("degraded control should keep the stored value in:\n%s", html)
		}
	})
}

func TestCompiledTypePropertyDefs(t *testing.T) {
	objectTypes := []CompiledType{
		{Name: "person", Properties: json.RawMessage(`{"zeta":{"type":"string"},"first_name":{"type":"string","title":"First name","description":"First name"},"age":{"type":"number"},"_internal":{"type":"string"}}`)},
		{Name: "note", Properties: json.RawMessage(`{"content":{"type":"string","widget":"textarea"},"category":{"type":"string","enum":["preference","fact","correction"]},"source":{"type":"string","widget":"input"}}`)},
		{Name: "plain", Properties: json.RawMessage(`{}`)},
	}

	// parses type and display title per property; sorted; underscore-prefixed skipped
	got := compiledTypePropertyDefs(objectTypes, "person")
	want := []objectPropertyDef{
		{Name: "age", Type: "number"},
		{Name: "first_name", Title: "First name", Type: "string", Description: "First name"},
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
	if len(got2) != 1 || got2[0].Name != "title" || got2[0].Type != "" || got2[0].Description != "x" {
		t.Errorf("untyped prop = %+v, want {title  description x}", got2)
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

func TestCompiledTypePropertyDefsSkipsShadowed(t *testing.T) {
	// The status field, the preview panel title and the forms all resolve the
	// winning declaration; the property rows must come from that same one, in
	// whichever order the compiled list carries winner and loser.
	winner := CompiledType{Name: "person", Properties: json.RawMessage(`{"fresh":{"type":"string"}}`)}
	loser := CompiledType{Name: "person", Shadowed: true, Properties: json.RawMessage(`{"stale":{"type":"string"}}`)}

	for _, tc := range []struct {
		name  string
		types []CompiledType
	}{
		{name: "shadowed after winner", types: []CompiledType{winner, loser}},
		{name: "shadowed before winner", types: []CompiledType{loser, winner}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := compiledTypePropertyDefs(tc.types, "person")
			if len(got) != 1 || got[0].Name != "fresh" {
				t.Fatalf("defs = %+v, want only the winning declaration's fresh property", got)
			}
		})
	}

	// A name that survives only as a shadowed loser has no properties to render.
	only := []CompiledType{{Name: "gone", Shadowed: true, Properties: json.RawMessage(`{"stale":{"type":"string"}}`)}}
	if got := compiledTypePropertyDefs(only, "gone"); got != nil {
		t.Errorf("all-shadowed input = %+v, want nil", got)
	}
}

func TestLoadObjectFilterOptionsSkipsShadowed(t *testing.T) {
	// A shadowed duplicate would list the same type name twice in the filter
	// dropdown, and a name that survives only as a loser is not selectable.
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person"},
		{Name: "note"},
		{Name: "person", Shadowed: true},
		{Name: "gone", Shadowed: true},
	}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{compiled: compiled}}

	var data objectsPageData
	s.loadObjectFilterOptions(context.Background(), &data)

	if want := []string{"person", "note"}; !reflect.DeepEqual(data.Types, want) {
		t.Errorf("types = %v, want %v (winner once, shadowed-only name absent)", data.Types, want)
	}
}

func TestObjectPropertyDefLabel(t *testing.T) {
	labelled := objectPropertyDef{Name: "owner_notes", Title: "Owner notes"}
	if got := labelled.Label(); got != "Owner notes" {
		t.Errorf("label = %q, want the title", got)
	}
	if got := labelled.KeyHint(); got != "owner_notes" {
		t.Errorf("key hint = %q, want the machine key", got)
	}

	unlabelled := objectPropertyDef{Name: "owner_notes"}
	if got := unlabelled.Label(); got != "owner_notes" {
		t.Errorf("unlabelled label = %q, want the key", got)
	}
	if got := unlabelled.KeyHint(); got != "" {
		t.Errorf("unlabelled key hint = %q, want empty", got)
	}

	same := objectPropertyDef{Name: "title", Title: "title"}
	if got := same.KeyHint(); got != "" {
		t.Errorf("identical label/key hint = %q, want empty", got)
	}

	// The comparison ignores case: a label that differs only in case is not a
	// second, machine-readable name for the same property.
	mixed := objectPropertyDef{Name: "owner_notes", Title: "Owner_Notes"}
	if got := mixed.Label(); got != "Owner_Notes" {
		t.Errorf("case-only label = %q, want the title", got)
	}
	if got := mixed.KeyHint(); got != "" {
		t.Errorf("case-only difference hint = %q, want empty", got)
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
		{objectPropertyDef{Name: "homepage", Type: "url"}, `name="prop_homepage" type="url"`},
		{objectPropertyDef{Name: "checksum", Type: "sha256"}, `name="prop_checksum" type="text"`},
		{objectPropertyDef{Name: "digest", Type: "md5"}, `pattern="[0-9a-fA-F]{32}"`},
		{objectPropertyDef{Name: "digest", Type: "md5"}, `maxlength="32"`},
		{objectPropertyDef{Name: "digest", Type: "sha512"}, `maxlength="128"`},
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

	// boolean renders a three-state select and never posts a hidden false
	html := renderHTML(t, propertyInput(objectPropertyDef{Name: "active", Type: "boolean"}, []string{"true"}))
	for _, want := range []string{
		`name="propm_present_active" value="1"`,
		`<select name="prop_active"`,
		`<option value="true" selected>Yes</option>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("boolean HTML missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, `name="prop_active" value="false"`) {
		t.Errorf("boolean must not post an implicit false:\n%s", html)
	}
	htmlUnset := renderHTML(t, propertyInput(objectPropertyDef{Name: "active", Type: "boolean"}, nil))
	if !strings.Contains(htmlUnset, `<option value="" selected>Not set</option>`) {
		t.Errorf("unset boolean must default to Not set:\n%s", htmlUnset)
	}
	htmlFalse := renderHTML(t, propertyInput(objectPropertyDef{Name: "active", Type: "boolean"}, []string{"false"}))
	if !strings.Contains(htmlFalse, `<option value="false" selected>No</option>`) {
		t.Errorf("stored false must select No:\n%s", htmlFalse)
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
		{Name: "homepage", Type: "url"},
		{Name: "checksum", Type: "sha256"},
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

func TestObjectTypePropertiesCardLongTextFallback(t *testing.T) {
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
	html := renderHTML(t, objectTypePropertiesCard(obj, propDefs, "Note"))

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

func TestInvalidateObjectLabelSuggestions(t *testing.T) {
	seed := func() map[string]labelSuggestionsEntry {
		return map[string]labelSuggestionsEntry{
			"proj-1": {labels: []string{"vip", "contact"}, expiresAt: time.Now().Add(time.Hour)},
		}
	}

	// Session project scope: the session's active project wins over the static id.
	s := &Server{cfg: Config{MemoryProjectID: "static-proj"}, memory: &fakeMemory{}}
	s.labelCache = seed()
	s.invalidateObjectLabelSuggestions(withSessionContext(context.Background(), &sessionContext{ProjectID: "proj-1"}))
	if _, ok := s.labelCache["proj-1"]; ok {
		t.Errorf("session project entry not removed after invalidation")
	}

	// Static project id (dev/API-key mode, no session context).
	s2 := &Server{cfg: Config{MemoryProjectID: "proj-1"}, memory: &fakeMemory{}}
	s2.labelCache = seed()
	s2.invalidateObjectLabelSuggestions(context.Background())
	if _, ok := s2.labelCache["proj-1"]; ok {
		t.Errorf("static project entry not removed after invalidation")
	}

	// No project scope: invalidation is a no-op and leaves other entries intact.
	s3 := &Server{cfg: Config{}, memory: &fakeMemory{}}
	s3.labelCache = seed()
	s3.invalidateObjectLabelSuggestions(context.Background())
	if _, ok := s3.labelCache["proj-1"]; !ok {
		t.Errorf("entry removed despite no project scope; invalidation should be a no-op")
	}
}

// TestObjectMutationInvalidatesLabelSuggestions drives the real create/update
// routes rather than the invalidateObjectLabelSuggestions helper. The #1504 fix
// lives at the call sites, and TestInvalidateObjectLabelSuggestions alone passes
// with both calls deleted (mutation-verified), so it does not guard the wiring.
// Removing either invalidateObjectLabelSuggestions call must fail here.
func TestObjectMutationInvalidatesLabelSuggestions(t *testing.T) {
	const pid = "proj-inval"
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", Properties: json.RawMessage(`{"first_name":{"type":"string"}}`)}}}
	seed := func() map[string]labelSuggestionsEntry {
		return map[string]labelSuggestionsEntry{
			pid: {labels: []string{"stale"}, expiresAt: time.Now().Add(time.Hour)},
		}
	}
	newServer := func(f *fakeMemory) *Server {
		return &Server{cfg: Config{DefaultAgent: "memory", MemoryProjectID: pid}, memory: f}
	}
	post := func(e *echo.Echo, path string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("successful create clears the cache", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-new"}}
		s := newServer(f)
		s.labelCache = seed()
		e := echo.New()
		e.POST("/objects", s.uiObjectCreate)

		rec := post(e, "/objects", url.Values{"type": {"person"}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
		}
		if _, ok := s.labelCache[pid]; ok {
			t.Error("successful create must invalidate the label-suggestion cache")
		}
	})

	t.Run("successful update clears the cache", func(t *testing.T) {
		f := &fakeMemory{updateResult: &GraphObject{ID: "o2-new"}}
		s := newServer(f)
		s.labelCache = seed()
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		rec := post(e, "/objects/o1", url.Values{"key": {"k"}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
		}
		if _, ok := s.labelCache[pid]; ok {
			t.Error("successful update must invalidate the label-suggestion cache")
		}
	})
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
		defs: map[string]*AgentDefinition{
			"a1": {ID: "a1", Name: "diane", Enabled: true, Tools: []string{"entity-update", "relationship-create"}},
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
	if !strings.Contains(loc, "editor-capable") {
		t.Errorf("no agent flash should say what kind of agent is needed: location=%q", loc)
	}
	if !strings.Contains(loc, "relationship-create") || !strings.Contains(loc, "Project+settings") {
		t.Errorf("no agent flash should name the required tools and point at settings: location=%q", loc)
	}
	if f2.convCanonicalID != "" || f2.convTitle != "" {
		t.Errorf("conversation should not be created without an agent: canonical=%q title=%q", f2.convCanonicalID, f2.convTitle)
	}

	// conversation creation fails: redirect back, no 500
	f3 := &fakeMemory{
		objects: []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "person", Key: "sam-lee"}},
		agents:  []AgentDefinitionSummary{{ID: "a1", Name: "diane", IsDefault: true, Enabled: true}},
		defs: map[string]*AgentDefinition{
			"a1": {ID: "a1", Name: "diane", Enabled: true, Tools: []string{"entity-update", "relationship-create"}},
		},
		createConvErr: fmt.Errorf("boom"),
	}
	_, e3 := newRouter(f3)
	rec = get(e3, "/objects/o1/chat")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1" {
		t.Fatalf("create err: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestResolveObjectBranch(t *testing.T) {
	branches := []Branch{
		{ID: "b1", Name: "extract-1"},
		{ID: "b2", Name: "extract-2", MergedAt: "2026-08-27T00:00:00Z"},
	}
	cases := []struct {
		name string
		id   string
		want objectBranchRef
	}{
		{"main", "", objectBranchRef{}},
		{"open", "b1", objectBranchRef{ID: "b1", Name: "extract-1"}},
		{"merged", "b2", objectBranchRef{ID: "b2", Name: "extract-2", MergedAt: "2026-08-27T00:00:00Z"}},
		{"unknown", "nope", objectBranchRef{ID: "nope"}},
	}
	for _, tc := range cases {
		if got := resolveObjectBranch(branches, tc.id); got != tc.want {
			t.Errorf("resolveObjectBranch(%q) = %+v, want %+v", tc.id, got, tc.want)
		}
	}
}

func TestRenderObjectBranchChip(t *testing.T) {
	cases := []struct {
		name   string
		branch objectBranchRef
		want   []string
		absent []string
	}{
		{
			name:   "main",
			branch: objectBranchRef{},
			want:   []string{`data-testid="object-branch-chip"`, "main"},
			absent: []string{`href="/objects?branch=`, "lucide--git-branch"},
		},
		{
			name:   "open",
			branch: objectBranchRef{ID: "b1", Name: "extract-1"},
			want:   []string{`data-testid="object-branch-chip"`, "extract-1", `href="/objects?branch=b1"`, "lucide--git-branch"},
		},
		{
			name:   "merged",
			branch: objectBranchRef{ID: "b2", Name: "extract-2", MergedAt: "2026-08-27T00:00:00Z"},
			want:   []string{`data-testid="object-branch-chip"`, "extract-2", "merged", `href="/objects?branch=b2"`},
		},
		{
			name:   "unknown",
			branch: objectBranchRef{ID: "missing"},
			want:   []string{`data-testid="object-branch-chip"`, "missing", `href="/objects?branch=missing`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := renderHTML(t, objectBranchChip(tc.branch))
			for _, w := range tc.want {
				if !strings.Contains(html, w) {
					t.Errorf("chip missing %q:\n%s", w, html)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(html, a) {
					t.Errorf("chip should not contain %q:\n%s", a, html)
				}
			}
		})
	}
}

func TestResolveObjectSourceNames(t *testing.T) {
	f := &fakeMemory{
		documents: []Document{
			{ID: "doc1", Filename: "Spec.md"},
			{ID: "doc2", Filename: "Notes.md"},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	sources := []objectSource{
		{DocumentID: "doc1"},
		{DocumentID: "doc1"}, // duplicate → cached, no extra lookup
		{DocumentID: "doc2"},
		{DocumentID: "gone"}, // missing → empty name, card falls back to short id
		{DocumentID: ""},     // no id → untouched
	}
	s.resolveObjectSourceNames(t.Context(), sources)
	if sources[0].DocumentName != "Spec.md" || sources[1].DocumentName != "Spec.md" {
		t.Errorf("doc1 name not resolved/cached: %+v", sources)
	}
	if sources[2].DocumentName != "Notes.md" {
		t.Errorf("doc2 name not resolved: %+v", sources)
	}
	if sources[3].DocumentName != "" {
		t.Errorf("missing doc should leave an empty name: %+v", sources[3])
	}

	// Fan-out is capped: beyond objectSourceDocLookupCap distinct ids, lookups
	// stop so a pathological _sources array cannot trigger unbounded calls.
	t.Run("cap", func(t *testing.T) {
		docs := make([]Document, objectSourceDocLookupCap+5)
		for i := range docs {
			docs[i] = Document{ID: fmt.Sprintf("d%d", i), Filename: fmt.Sprintf("F%d", i)}
		}
		f := &fakeMemory{documents: docs}
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		sources := make([]objectSource, objectSourceDocLookupCap+5)
		for i := range sources {
			sources[i] = objectSource{DocumentID: fmt.Sprintf("d%d", i)}
		}
		s.resolveObjectSourceNames(t.Context(), sources)
		for i := range sources {
			if i < objectSourceDocLookupCap && sources[i].DocumentName == "" {
				t.Errorf("source %d within cap should resolve", i)
			}
			if i >= objectSourceDocLookupCap && sources[i].DocumentName != "" {
				t.Errorf("source %d beyond cap should not resolve", i)
			}
		}
	})
}

func TestRenderObjectSourceCard(t *testing.T) {
	sources := []objectSource{
		{
			DocumentID:      "doc1",
			DocumentName:    "Spec.md",
			DocumentVersion: "3",
			JobID:           "jobid1234567890",
			ExtractedAt:     "2026-08-27T00:00:00Z",
		},
		{
			DocumentID:      "doc2",
			DocumentName:    "", // legacy fallback → short id
			DocumentVersion: "1",
			JobID:           "",
		},
	}
	html := renderHTML(t, objectSourceCard(sources))
	for _, want := range []string{
		`data-testid="object-source-card"`,
		"Spec.md",
		`href="/documents/doc1"`,
		"v3",
		`href="/documents/doc1/extractions"`,
		`title="jobid1234567890"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("source card missing %q:\n%s", want, html)
		}
	}
	// legacy fallback: unresolved name renders the short document id
	if !strings.Contains(html, "doc2") {
		t.Errorf("legacy source should show the short document id:\n%s", html)
	}
	// no job → no job link on the legacy row
	if strings.Contains(html, `href="/documents/doc2/extractions"`) {
		t.Errorf("source without a job must not render a job link:\n%s", html)
	}
}

// --- object breadcrumb origin ---

func TestParseExtractionOrigin(t *testing.T) {
	cases := []struct {
		in           string
		docID, jobID string
		ok           bool
	}{
		{"/documents/d1/extractions/job1", "d1", "job1", true},
		{"", "", "", false},
		{"/documents/d1", "", "", false},
		{"/documents/d1/extractions", "", "", false},
		{"/documents/d1/extractions/job1/extra", "", "", false},
		{"/objects/o1", "", "", false},
		{"documents/d1/extractions/job1", "", "", false},
		{"/documents//extractions/job1", "", "", false},
		{"/documents/d1/extractions/", "", "", false},
		{"/documents/d1/extractions/job1?x=1", "", "", false},
		{"/documents/d1/extractions/job1#frag", "", "", false},
		// A percent-encoded slash decodes into an extra segment and is refused.
		{"/documents/d%2F1/extractions/job1", "", "", false},
	}
	for _, tc := range cases {
		docID, jobID, ok := parseExtractionOrigin(tc.in)
		if ok != tc.ok || docID != tc.docID || jobID != tc.jobID {
			t.Errorf("parseExtractionOrigin(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, docID, jobID, ok, tc.docID, tc.jobID, tc.ok)
		}
	}
}

func TestObjectOriginTrail(t *testing.T) {
	doc := &Document{ID: "d1", Filename: "Spec.md"}
	getDoc := func(ctx context.Context, id string) (*Document, error) {
		if id != "d1" {
			return nil, fmt.Errorf("not found")
		}
		return doc, nil
	}

	trail := objectOriginTrail(context.Background(), "/documents/d1/extractions/job1", getDoc)
	want := []struct{ label, href string }{
		{"Documents", "/documents"},
		{"Spec.md", "/documents/d1"},
		{"Extraction history", "/documents/d1/extractions"},
	}
	if len(trail) != len(want) {
		t.Fatalf("trail = %+v, want %d crumbs", trail, len(want))
	}
	for i, w := range want {
		if trail[i].Label != w.label || trail[i].Href != w.href {
			t.Errorf("crumb %d = %+v, want label=%q href=%q", i, trail[i], w.label, w.href)
		}
	}
	if trail[len(trail)-1].Active {
		t.Error("objectOriginTrail must leave the active marker to the header")
	}

	// Best-effort: a missing document or backend failure drops the trail.
	if got := objectOriginTrail(context.Background(), "/documents/d2/extractions/job1", getDoc); got != nil {
		t.Errorf("unknown document trail = %+v, want nil", got)
	}
	errDoc := func(context.Context, string) (*Document, error) { return nil, errTest }
	if got := objectOriginTrail(context.Background(), "/documents/d1/extractions/job1", errDoc); got != nil {
		t.Errorf("backend error trail = %+v, want nil", got)
	}
	// An unrecognised origin shape drops the trail.
	if got := objectOriginTrail(context.Background(), "/objects/o1", getDoc); got != nil {
		t.Errorf("unknown origin trail = %+v, want nil", got)
	}
}

// TestRenderObjectDetailOriginTrail pins that a supplied origin replaces the
// default Objects > <object> trail and ends at the document's Extraction
// history — the object label is the page title, not a crumb, and the section
// remains a navigable ancestor rather than the active item.
func TestRenderObjectDetailOriginTrail(t *testing.T) {
	obj := &GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}
	doc := &Document{ID: "d1", Filename: "Spec.md"}
	getDoc := func(context.Context, string) (*Document, error) { return doc, nil }
	trail := objectOriginTrail(context.Background(), "/documents/d1/extractions/job1", getDoc)
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", nil, nil, nil, nil, nil, trail, ""))

	region := breadcrumbRegion(html)
	if region == "" {
		t.Fatal("no breadcrumb trail rendered")
	}
	last := -1
	for _, label := range []string{"Documents", "Spec.md", "Extraction history"} {
		i := strings.Index(region, ">"+label+"<")
		if i < 0 {
			t.Fatalf("missing crumb %q in %s", label, region)
		}
		if i <= last {
			t.Fatalf("crumb %q out of order in %s", label, region)
		}
		last = i
	}
	if strings.Contains(region, "sam-lee") {
		t.Errorf("object label must not appear in an origin trail: %s", region)
	}
	if strings.Contains(region, "Run job1") || strings.Contains(region, "job1") {
		t.Errorf("run short id must not repeat in the trail: %s", region)
	}
	if !strings.Contains(region, `href="/documents/d1/extractions"`) {
		t.Errorf("extraction history crumb must link back: %s", region)
	}
	if !strings.Contains(region, ">Extraction history</a>") {
		t.Errorf("extraction history crumb must stay a link, not the current item: %s", region)
	}
	if strings.Contains(region, "font-medium text-base-content") {
		t.Errorf("origin trail must not mark a crumb active: %s", region)
	}
	if strings.Contains(region, `href="/objects"`) {
		t.Errorf("origin trail must not keep the default Objects crumb: %s", region)
	}
}

// TestObjectDetailFromHandler asserts the ?from= origin survives the handler
// and bounds the fallback: an invalid origin renders the page normally.
func TestObjectDetailFromHandler(t *testing.T) {
	f := &fakeMemory{
		objects:   []GraphObject{{ID: "o1", CanonicalID: "o1", Type: "person", Key: "sam-lee"}},
		documents: []Document{{ID: "d1", Filename: "Spec.md"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/:id", s.uiObject)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1?from=%2Fdocuments%2Fd1%2Fextractions%2Fjob1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	region := breadcrumbRegion(rec.Body.String())
	for _, want := range []string{"Documents", "Spec.md", "Extraction history"} {
		if !strings.Contains(region, ">"+want+"<") {
			t.Errorf("handler origin trail missing %q: %s", want, region)
		}
	}
	if strings.Contains(region, "job1") {
		t.Errorf("handler origin trail must not repeat the run short id: %s", region)
	}

	// A stale/invalid origin degrades to the default trail, never an error.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1?from=%2Fobjects%2Fo9", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid origin status = %d, want 200", rec.Code)
	}
	region = breadcrumbRegion(rec.Body.String())
	if !strings.Contains(region, `href="/objects"`) || !strings.Contains(region, ">sam-lee<") {
		t.Errorf("invalid origin must fall back to Objects > <object>: %s", region)
	}
}

// TestObjectFormPropertyLabels pins the label-first mapping on both object form
// surfaces: the create form and the detail properties card name the property by
// its display label and keep the machine key as the hint. An unlabelled property
// keeps the key as its label and shows no hint.
func TestObjectFormPropertyLabels(t *testing.T) {
	defs := []objectPropertyDef{
		{Name: "owner_notes", Title: "Owner notes", Type: "string"},
		{Name: "plain", Type: "string"},
	}
	const labelledLegend = `<legend class="fieldset-legend">Owner notes</legend>`
	const keyHint = `<span class="label-text-alt text-base-content/60">owner_notes</span>`

	form := renderHTML(t, ObjectCreatePage([]CompiledType{{Name: "Person"}}, defs, objectCreateForm{Type: "Person"}, nil, nil, nil))
	if !strings.Contains(form, labelledLegend) {
		t.Errorf("create form must label the property by its title:\n%s", form)
	}
	if !strings.Contains(form, keyHint) {
		t.Errorf("create form must keep the machine key visible as the hint:\n%s", form)
	}
	if !strings.Contains(form, `<legend class="fieldset-legend">plain</legend>`) {
		t.Errorf("unlabelled property must keep its key as the label:\n%s", form)
	}
	if strings.Contains(form, `<span class="label-text-alt text-base-content/60">plain</span>`) {
		t.Errorf("unlabelled property must not repeat its key as a hint:\n%s", form)
	}

	obj := &GraphObject{ID: "o1", Type: "Person", Key: "p1", Properties: map[string]any{"owner_notes": "hello"}}
	detail := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, nil, nil, nil, "", nil, nil, defs, nil, nil, nil, ""))
	if !strings.Contains(detail, labelledLegend) {
		t.Errorf("detail properties card must label the property by its title:\n%s", detail)
	}
	if !strings.Contains(detail, keyHint) {
		t.Errorf("detail properties card must keep the machine key as the hint:\n%s", detail)
	}
}
