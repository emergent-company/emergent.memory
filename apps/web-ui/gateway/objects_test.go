package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
		Branches:   []Branch{{ID: "b1", Name: "plan/next-gen"}},
		Types:      []string{"note"},
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
	// The chosen mode is filled with the primary colour. A `btn-active` shade is
	// a 7% darkening that disappears beside its two neighbours, which is what
	// made the toggle read as three unrelated buttons (#121).
	if got := strings.Count(html, "btn join-item btn-primary"); got != 1 {
		t.Errorf("active provenance mode should be the only filled segment: %d", got)
	}

	// Branch, type and actor are one control family: the actor picker's closed
	// trigger wears the same select chrome as the two <select>s instead of its
	// own input box and hand-rolled chevron (#123).
	filterForm := formBlock(t, html, "objects-filters-form")
	if got := strings.Count(filterForm, `class="select w-56`); got != 3 {
		t.Errorf("filter controls sharing the select chrome = %d, want 3 (branch, type, actor):\n%s", got, filterForm)
	}
	if strings.Contains(filterForm, "input input-bordered input-sm") {
		t.Error("actor trigger should wear the select chrome, not the input chrome")
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

// TestObjectsFilterSelectsTruncateTheirValue pins the filter row's select markup.
// The native <select> stays in charge — it keeps the field name, the id, the
// auto-apply hook and the whole option list — but Chromium hard-clips a select's
// chosen value even with appearance:none and text-overflow:ellipsis, so the
// visible chrome renders the label and the select sits transparent on top of it.
// Without that layering a long branch name runs into the chevron instead of
// ending in an ellipsis.
func TestObjectsFilterSelectsTruncateTheirValue(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		Objects:    []GraphObject{{ID: "o1", Type: "note"}},
		Types:      []string{"note"},
		Branches:   []Branch{{ID: "b1", Name: "plan/next-gen"}},
		BranchID:   "b1",
		TypeFilter: "",
	}))
	form := formBlock(t, html, "objects-filters-form")

	// The chosen value is rendered in the visible layer, where CSS can truncate it.
	for _, want := range []string{
		`<span class="min-w-0 truncate" aria-hidden="true">plan/next-gen</span>`,
		`<span class="min-w-0 truncate" aria-hidden="true">All types</span>`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("filter row should render the selected value in the truncating layer; missing %s", want)
		}
	}
	// Only the two native <select>s are overlaid: the actor picker is a menu of
	// submit buttons, not a native control, so it must not grow a third.
	if got := strings.Count(form, `class="absolute inset-0 size-full cursor-pointer opacity-0"`); got != 2 {
		t.Errorf("transparent native selects layered over the chrome = %d, want 2 (branch, type)", got)
	}
	// The overlay must not cost the control its identity or its behaviour.
	for _, want := range []string{
		`id="objects-filter-branch"`, `name="branch"`,
		`id="objects-filter-type"`, `name="type"`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("overlaid select must keep %s", want)
		}
	}
}

// TestObjectBranchOptionLabel keeps the closed control's wording identical to the
// option list's, including the two cases where a guess would lie: an empty id is
// the main line, and an id that is not in the loaded list shows as itself rather
// than as "Main branch", which would claim a filter that is not applied.
func TestObjectBranchOptionLabel(t *testing.T) {
	branches := []Branch{{ID: "b1", Name: "plan/next-gen"}}
	for _, tc := range []struct {
		name string
		id   string
		want string
	}{
		{"main line", "", "Main branch"},
		{"loaded branch", "b1", "plan/next-gen"},
		{"unknown id", "gone", "gone"},
	} {
		if got := objectBranchOptionLabel(branches, tc.id); got != tc.want {
			t.Errorf("%s: label = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := objectTypeOptionLabel(""); got != "All types" {
		t.Errorf("empty type filter labels as %q, want All types", got)
	}
	if got := objectTypeOptionLabel("person"); got != "person" {
		t.Errorf("type filter labels as %q, want person", got)
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

// TestRenderObjectsProvenanceDefaultsToAny pins the toggle's rendered
// invariant: the template normalizes the mode before comparing, so an absent or
// unrecognised ?provenance= still renders the group with exactly one selected
// segment (Any) — never a join of three buttons with nothing chosen (#121).
func TestRenderObjectsProvenanceDefaultsToAny(t *testing.T) {
	for _, raw := range []string{"", "sideways"} {
		html := renderHTML(t, ObjectsPage(objectsPageData{
			Objects:    []GraphObject{{ID: "o1", Type: "note", Key: "agent-note"}},
			Provenance: raw,
			ActorType:  "agent",
			ActorID:    "11111111-1111-1111-1111-111111111111",
			ActorOptions: []objectActorOption{
				{Type: "agent", ID: "11111111-1111-1111-1111-111111111111", Label: "diane"},
			},
		}))
		if got := strings.Count(html, "btn join-item btn-primary"); got != 1 {
			t.Errorf("provenance %q: filled segments = %d, want 1", raw, got)
		}
		if want := `name="provenance" value="any" checked`; !strings.Contains(html, want) {
			t.Errorf("provenance %q: Any should be the selected segment", raw)
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
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, nil, nil, nil, nil, "", 0))
	for _, want := range []string{
		"sam-lee", "person", "first_name", "Sam", "relationship", "colleague",
		`href="/objects"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("object detail missing %q", want)
		}
	}
	for _, bad := range []string{"No relationships", "Relationships</h2>", `id="object-connect-modal"`} {
		if strings.Contains(html, bad) {
			t.Errorf("overview must not render relationships (%q):\n%s", bad, html)
		}
	}

	htmlErr := renderHTML(t, ObjectDetailPage(nil, objectBranchRef{}, nil, nil, errTest, "", nil, nil, nil, nil, nil, nil, "", 0))
	if !strings.Contains(htmlErr, "Object unavailable") {
		t.Error("error state missing")
	}

	// flash feedback renders an auto-dismissing toast carrying the message
	htmlFlash := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", errTest, nil, nil, nil, nil, nil, "", 0))
	if !strings.Contains(htmlFlash, "memory-toast") || !strings.Contains(htmlFlash, errTest.Error()) {
		t.Error("flash toast missing")
	}
}

// TestRenderRelationshipsSubpage pins the moved Relationships surface: the
// empty state with its Connect CTA/dialog, the populated edge list, and the
// active rail tab.
func TestRenderRelationshipsSubpage(t *testing.T) {
	obj := &GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}
	relTypes := []CompiledType{
		{Name: "assigned_to", Label: "Assigned to", SourceType: "task", TargetType: "person"},
		{Name: "works_with", Label: "Works with", SourceType: "person", TargetType: "person"},
		{Name: "located_in", Label: "Located in", SourceType: "place", TargetType: "place"},
	}

	empty := renderHTML(t, objectRelationshipsPage(objectDetailShellData{Obj: obj, Active: "relationships"}, relTypes, nil, nil, nil))
	for _, want := range []string{
		"No relationships", "Connect", `data-testid="object-connect-modal"`,
		`name="type"`, "Assigned to", "Works with",
		`name="src_id"`, `value="o1"`, `name="dst_id"`,
		`action="/objects/o1/relationships"`, `aria-current="page"`,
	} {
		if !strings.Contains(empty, want) {
			t.Errorf("relationships page missing %q:\n%s", want, empty)
		}
	}
	if strings.Contains(empty, "Located in") {
		t.Error("relationship modal should filter types not touching the object's type")
	}

	edges := []GraphRelationship{{ID: "r1", Type: "works_with", SrcID: "o1", DstID: "c2"}}
	related := []GraphObject{{ID: "o2", CanonicalID: "c2", Type: "person", Key: "jamie-doe"}}
	populated := renderHTML(t, objectRelationshipsPage(objectDetailShellData{Obj: obj, Active: "relationships"}, relTypes, edges, related, nil))
	for _, want := range []string{"works with", "jamie-doe", "Relationships"} {
		if !strings.Contains(populated, want) {
			t.Errorf("relationship list missing %q:\n%s", want, populated)
		}
	}
	if strings.Contains(populated, "No relationships") {
		t.Error("non-empty relationship list should not show the empty state")
	}
}

func TestRenderObjectDetailSimilar(t *testing.T) {
	obj := &GraphObject{ID: "o1", Type: "person", Key: "sam-lee"}

	// The Overview page no longer renders Similar objects inline; it is a rail
	// tab with its own route.
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, nil, nil, nil, nil, "", 0))
	if strings.Contains(html, `id="object-similar"`) || strings.Contains(html, "Finding similar objects") {
		t.Errorf("overview must not render the Similar objects section:\n%s", html)
	}
	if !strings.Contains(html, `href="/objects/o1/similar"`) {
		t.Errorf("overview rail missing the Similar objects tab:\n%s", html)
	}
	if !strings.Contains(html, `aria-current="page"`) {
		t.Errorf("overview rail missing its active state:\n%s", html)
	}

	// The Similar page paints the shared shell and defers the slow vector read.
	similarHTML := renderHTML(t, objectSimilarPage(objectDetailShellData{Obj: obj, Active: "similar"}))
	for _, want := range []string{"Similar objects", `hx-get="/objects/o1/similar/partial"`, "Finding similar objects", `aria-current="page"`} {
		if !strings.Contains(similarHTML, want) {
			t.Errorf("similar page missing %q:\n%s", want, similarHTML)
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
	// person derives from no compiled type here, so a board-enabled control is
	// supplied explicitly to exercise the Overview Workflow panel.
	status := objectStatusControlFor([]CompiledType{{Name: "person", BoardEnabled: true}}, "person")
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, nil, nil, status, nil, "", 0))

	// The Overview Workflow panel carries labelled status/assignee controls;
	// the properties form carries the key, labels, and schema fields.
	for _, want := range []string{
		`name="key"`, `value="sam-lee"`,
		`data-testid="object-workflow-panel"`,
		`data-testid="object-status-control"`, `data-status="suggested"`,
		`data-testid="object-assignee-control"`,
		">Status<", ">Assignee<",
		`id="object-labels"`, `placeholder="Add a label..."`, "contact", "colleague",
		`name="prop_first_name"`, `>Sam</textarea>`,
		`name="prop_age"`, `>42</textarea>`,
		`name="prop_nicknames"`, "Save changes",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("properties form missing %q", want)
		}
	}
	if strings.Contains(html, `id="object-status"`) {
		t.Errorf("detail properties card must not repeat the workflow status select")
	}

	// Relationships moved off the Overview entirely.
	for _, bad := range []string{"No relationships", `id="object-connect-modal"`} {
		if strings.Contains(html, bad) {
			t.Errorf("overview must not render relationships (%q):\n%s", bad, html)
		}
	}

	// chat deep link in the header
	for _, want := range []string{
		"Chat about object", `href="/objects/o1/chat"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("object detail header missing %q", want)
		}
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
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, propDefs, nil, status, nil, "Person", 0))

	if got := strings.Count(html, ">Properties</h2>"); got != 1 {
		t.Errorf("built-in Properties heading rendered %d times, want 1:\n%s", got, html)
	}
	if !strings.Contains(html, ">Person Properties</h2>") {
		t.Fatalf("schema card heading missing:\n%s", html)
	}
	if got := strings.Count(html, "Save changes"); got != 1 {
		t.Errorf("Save rendered %d times, want a single terminal bar: %s", got, html)
	}

	detailsStart := strings.Index(html, `data-testid="object-record-facts"`)
	builtinStart := strings.Index(html, `data-testid="object-builtin-properties"`)
	schemaStart := strings.Index(html, `data-testid="object-type-properties"`)
	if detailsStart < 0 || builtinStart < 0 || schemaStart < 0 || builtinStart < detailsStart || schemaStart < builtinStart {
		t.Fatalf("details/property panels missing or out of order:\n%s", html)
	}
	if !strings.Contains(html, ">Details</h2>") {
		t.Errorf("record facts should be their own Details panel:\n%s", html)
	}
	details, builtin, schema := html[detailsStart:builtinStart], html[builtinStart:schemaStart], html[schemaStart:]
	// Details panel: the read-only record facts, above Properties.
	for _, want := range []string{
		"Version", "v3", "Created",
		`11111111-1111-1111-1111-111111111111`,
	} {
		if !strings.Contains(details, want) {
			t.Errorf("details panel missing %q:\n%s", want, details)
		}
	}
	// Card 1: the editable platform fields only.
	for _, want := range []string{
		`id="object-key"`, `id="object-labels"`,
	} {
		if !strings.Contains(builtin, want) {
			t.Errorf("built-in card missing %q:\n%s", want, builtin)
		}
	}
	for _, bad := range []string{`id="object-status"`, `>Assignee</dt>`, "jamie", "Version", `data-testid="object-record-facts"`} {
		if strings.Contains(builtin, bad) {
			t.Errorf("built-in card must not repeat %q:\n%s", bad, builtin)
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
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, propDefs, nil, nil, nil, "", 0))

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
	e.GET("/objects/:id/relationships", s.uiObjectRelationshipsPage)

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

	// detail page links to the Relationships subpage but no longer renders it
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1", nil))
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "sam-lee") || !strings.Contains(body, `href="/objects/o1/relationships"`) {
		t.Fatalf("detail page: status=%d body=%s", rec.Code, body)
	}
	if strings.Contains(body, `id="object-connect-modal"`) {
		t.Fatalf("detail page must not render the relationship dialog: %s", body)
	}

	// Relationships subpage shows the resolved list
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1/relationships", nil))
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Relationships") || !strings.Contains(body, "call dentist") {
		t.Fatalf("relationships page: status=%d body=%s", rec.Code, body)
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

	t.Run("json document field submits one parsed object", func(t *testing.T) {
		f := &fakeMemory{updateResult: &GraphObject{ID: "o1"}}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		// The object property is edited in a jsonTextarea, which submits the
		// whole document as one scalar field — not prop_spec_json[0], the chip
		// shape that serialized the document as a one-element list.
		form := url.Values{}
		form.Set("prop_spec_json", "{\n  \"version\": \"1.1.0\",\n  \"terms\": [{\"key\": \"a\"}]\n}")
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
		}
		got := f.lastUpdateReq
		if got == nil {
			t.Fatal("UpdateObject not called")
		}
		want := map[string]any{"version": "1.1.0", "terms": []any{map[string]any{"key": "a"}}}
		if !reflect.DeepEqual(got.Properties["spec_json"], want) {
			t.Errorf("spec_json = %#v, want a parsed object %#v", got.Properties["spec_json"], want)
		}
	})

	t.Run("array JSON field submits one element as a list", func(t *testing.T) {
		f := &fakeMemory{updateResult: &GraphObject{ID: "o1"}}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		// An array that cannot be a chip round-trips through the JSON editor.
		// Its ONE element must still be submitted as a one-element list: a bare
		// document fails validation with "expected array, got string".
		element := strings.Repeat("a", jsonEditorElementLimit+1)
		form := url.Values{}
		form.Set("prop_checks", jsonArrayEditorValue([]string{element}, nil, false, false))
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
		}
		got := f.lastUpdateReq
		if got == nil {
			t.Fatal("UpdateObject not called")
		}
		want := []any{element}
		if !reflect.DeepEqual(got.Properties["checks"], want) {
			t.Errorf("checks = %#v, want a one-element list %#v", got.Properties["checks"], want)
		}
	})

	t.Run("stored typed array round-trips through the JSON field", func(t *testing.T) {
		f := &fakeMemory{updateResult: &GraphObject{ID: "o1"}}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		// The detail page renders a stored typed array as JSON exactly because a
		// chip would stringify it; saving must give the types back (#2419).
		stored := []any{map[string]any{"option": "a", "pros": []any{"x"}}, float64(2)}
		form := url.Values{}
		form.Set("propm_present_alternatives", "1")
		form.Set("prop_alternatives", jsonArrayEditorValue(propValues(stored), stored, false, false))
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
		}
		got := f.lastUpdateReq
		if got == nil {
			t.Fatal("UpdateObject not called")
		}
		if !reflect.DeepEqual(got.Properties["alternatives"], stored) {
			t.Errorf("alternatives = %#v, want the typed list %#v", got.Properties["alternatives"], stored)
		}
	})

	t.Run("null is rejected, not treated as a delete", func(t *testing.T) {
		f := &fakeMemory{updateResult: &GraphObject{ID: "o1"}}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		// nil is the server's delete sentinel, so accepting "null" would erase a
		// property on a save the user meant as an edit. Clearing the field (the
		// presence marker with no value) is how a property is removed.
		form := url.Values{}
		form.Set("propm_present_meta", "1")
		form.Set("prop_meta", "null")
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		if got := rec.Header().Get("Location"); !strings.Contains(got, "err=") || !strings.Contains(got, "null+is+not+a+value") {
			t.Errorf("location = %q, want an error flash about null", got)
		}
		if f.lastUpdateReq != nil {
			t.Errorf("UpdateObject called with %v, want the save rejected", f.lastUpdateReq.Properties)
		}
	})

	t.Run("clearing the field still deletes the property", func(t *testing.T) {
		f := &fakeMemory{updateResult: &GraphObject{ID: "o1"}}
		s := newServer(f)
		e := echo.New()
		e.POST("/objects/:id", s.uiObjectUpdate)

		form := url.Values{}
		form.Set("propm_present_meta", "1")
		req := httptest.NewRequest(http.MethodPost, "/objects/o1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		got := f.lastUpdateReq
		if got == nil {
			t.Fatal("UpdateObject not called")
		}
		v, ok := got.Properties["meta"]
		if !ok || v != nil {
			t.Errorf("meta = %#v (present=%v), want an explicit nil delete", v, ok)
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
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1/relationships" {
		t.Fatalf("empty type: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastCreateRelReq != nil {
		t.Fatalf("relationship created with empty type: %+v", f.lastCreateRelReq)
	}

	// empty src: redirect back, nothing created
	rec = post("type=assigned_to&dst_id=o1")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1/relationships" {
		t.Fatalf("empty src: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastCreateRelReq != nil {
		t.Fatalf("relationship created with empty src: %+v", f.lastCreateRelReq)
	}

	// valid: created and redirected back
	rec = post("type=assigned_to&src_id=o2&dst_id=o1")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1/relationships" {
		t.Fatalf("valid: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastCreateRelReq == nil {
		t.Fatal("CreateRelationship not called")
	}
	if f.lastCreateRelReq.Type != "assigned_to" || f.lastCreateRelReq.SrcID != "o2" || f.lastCreateRelReq.DstID != "o1" {
		t.Errorf("req = %+v", f.lastCreateRelReq)
	}

	// backend failure: redirect back to the Relationships subpage with the flash
	ferr := &fakeMemory{createRelErr: fmt.Errorf("boom")}
	serr := &Server{cfg: Config{DefaultAgent: "memory"}, memory: ferr}
	ee := echo.New()
	ee.POST("/objects/:id/relationships", serr.uiObjectRelationshipCreate)
	req := httptest.NewRequest(http.MethodPost, "/objects/o1/relationships", strings.NewReader("type=assigned_to&src_id=o2&dst_id=o1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	ee.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1/relationships?err=boom" {
		t.Fatalf("backend failure: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
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

	t.Run("shadowed duplicate types render one option", func(t *testing.T) {
		withShadow := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "person", Label: "Person (overridden)", Shadowed: true},
			{Name: "person", Label: "Person"},
		}}
		f := &fakeMemory{compiled: withShadow}
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.GET("/objects/new", s.uiObjectNew)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/new", nil))

		body := rec.Body.String()
		if got := strings.Count(body, `value="person"`); got != 1 {
			t.Errorf("create form rendered %d person options, want 1 (shadowed duplicate leaked)", got)
		}
		if strings.Contains(body, "Person (overridden)") {
			t.Error("create form rendered the shadowed declaration's label")
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

	t.Run("detail page renders the board status picker", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-testid="object-workflow-panel"`) || !strings.Contains(body, `data-testid="object-status-control"`) {
			t.Fatalf("detail page missing the workflow status control in:\n%s", body)
		}
		for _, opt := range []string{"ready", "done"} {
			if !strings.Contains(body, `name="status" value="`+opt+`"`) {
				t.Errorf("status picker missing option %q in:\n%s", opt, body)
			}
		}
		if strings.Contains(body, `name="status" value="in_progress"`) {
			t.Errorf("status picker must omit the current status in:\n%s", body)
		}
		if !strings.Contains(body, "In progress") {
			t.Errorf("detail status trigger should show the current status in:\n%s", body)
		}
		if strings.Contains(body, `id="object-status"`) {
			t.Errorf("detail page must not render the old built-in status select in:\n%s", body)
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

	t.Run("type with no workflow renders no workflow controls (user report)", func(t *testing.T) {
		f.objects = append(f.objects, GraphObject{ID: "o2", CanonicalID: "o2", Type: "Note", Key: "n1", Status: "anything"})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o2", nil))
		body := rec.Body.String()
		if strings.Contains(body, `data-testid="object-workflow-panel"`) || strings.Contains(body, `id="object-status"`) {
			t.Fatalf("no-workflow type must not render workflow controls in:\n%s", body)
		}
		if strings.Contains(body, "Workflow status") {
			t.Errorf("no-workflow type must not render the workflow-status label in:\n%s", body)
		}
	})
}

func TestUIObjectCreate(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", BoardEnabled: true, Properties: json.RawMessage(`{"first_name":{"type":"string"},"active":{"type":"boolean"},"tags":{"type":"array"},"meta":{"type":"object"},"checks":{"type":"array"},"authors":{"type":"array","items":{"type":"string"}},"scores":{"type":"array","items":{"type":"integer"}}}`)}}}
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

	t.Run("object property is stored as a typed map", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-obj"}}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "person")
		form.Set("prop_meta", `{"version":"1.1.0","terms":[{"key":"a"}]}`)
		rec := post(e, form)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
		}
		got := f.lastCreateObjectReq
		if got == nil {
			t.Fatal("CreateObject not called")
		}
		// The JSON text field submits ONE document. It must reach the server as
		// a map — wrapping it in an array made the server reject the object
		// ("expected object, got []interface {}").
		want := map[string]any{"version": "1.1.0", "terms": []any{map[string]any{"key": "a"}}}
		if !reflect.DeepEqual(got.Properties["meta"], want) {
			t.Errorf("meta = %#v, want a parsed object %#v", got.Properties["meta"], want)
		}
	})

	t.Run("array JSON text field stores the parsed list", func(t *testing.T) {
		f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-arr"}}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "person")
		// an array whose elements do not fit a chip is submitted as one JSON
		// array document, not as prop_checks[0], prop_checks[1], …
		form.Set("prop_checks", `[{"n":1},{"n":2}]`)
		rec := post(e, form)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
		}
		got := f.lastCreateObjectReq
		if got == nil {
			t.Fatal("CreateObject not called")
		}
		want := []any{map[string]any{"n": float64(1)}, map[string]any{"n": float64(2)}}
		if !reflect.DeepEqual(got.Properties["checks"], want) {
			t.Errorf("checks = %#v, want the parsed list %#v", got.Properties["checks"], want)
		}
	})

	t.Run("declared element type stores chip text verbatim", func(t *testing.T) {
		// A schema that declares items is a contract: the chip's text IS the
		// value and the server coerces it to the declared element type. Parsing
		// it as JSON here would turn an array-of-strings' "123" into a number
		// (which the server rejects as the wrong element type) and "null" into
		// the delete sentinel.
		f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-items"}}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "person")
		form.Add("prop_authors[0]", "123")
		form.Add("prop_authors[1]", "true")
		form.Add("prop_authors[2]", "null")
		form.Add("prop_scores[0]", "3")
		rec := post(e, form)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
		}
		got := f.lastCreateObjectReq
		if got == nil {
			t.Fatal("CreateObject not called")
		}
		if want := []any{"123", "true", "null"}; !reflect.DeepEqual(got.Properties["authors"], want) {
			t.Errorf("authors = %#v, want the chip text %#v", got.Properties["authors"], want)
		}
		// A declared numeric element type still receives the chip text; the
		// server's items coercion turns "3" into the stored number.
		if want := []any{"3"}; !reflect.DeepEqual(got.Properties["scores"], want) {
			t.Errorf("scores = %#v, want the raw chip text %#v", got.Properties["scores"], want)
		}
	})

	t.Run("no items schema keeps parsing chip text as JSON", func(t *testing.T) {
		// Backward compatibility: an array with no declared element type keeps
		// the long-standing create-form rule (a JSON literal becomes a typed
		// value).
		f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-tags"}}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "person")
		form.Add("prop_tags[0]", "123")
		rec := post(e, form)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
		}
		got := f.lastCreateObjectReq
		if got == nil {
			t.Fatal("CreateObject not called")
		}
		if want := []any{float64(123)}; !reflect.DeepEqual(got.Properties["tags"], want) {
			t.Errorf("tags = %#v, want %#v", got.Properties["tags"], want)
		}
	})

	t.Run("JSON field keeps the document's shape for the server to validate", func(t *testing.T) {
		// The JSON text field is the only unindexed prop_* shape, and for an
		// array property its document must NOT be wrapped into a one-element
		// list: that is how a map or a bare string used to be stored as JSON of
		// the wrong type. The gateway sends what the text parses to, and the
		// server's type validation rejects anything that is not an array (or,
		// for an object property, not a map) — the same contract the update path
		// has had all along.
		cases := []struct {
			name  string
			field string
			value string
			want  any
		}{
			{"array gets an object", "prop_checks", `{"a":1}`, map[string]any{"a": float64(1)}},
			{"array gets a bare string", "prop_checks", "abc", "abc"},
			{"object gets an array", "prop_meta", `[1,2]`, []any{float64(1), float64(2)}},
			{"object gets a bare string", "prop_meta", "abc", "abc"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-new"}}
				e := echo.New()
				e.POST("/objects", newServer(f).uiObjectCreate)

				form := url.Values{}
				form.Set("type", "person")
				form.Set(tc.field, tc.value)
				rec := post(e, form)

				if rec.Code != http.StatusSeeOther {
					t.Fatalf("status = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
				}
				got := f.lastCreateObjectReq
				if got == nil {
					t.Fatal("CreateObject not called")
				}
				name := strings.TrimPrefix(tc.field, "prop_")
				if !reflect.DeepEqual(got.Properties[name], tc.want) {
					t.Errorf("%s = %#v, want %#v (never a one-element list)", name, got.Properties[name], tc.want)
				}
			})
		}
	})

	t.Run("null is not a property value", func(t *testing.T) {
		// On create "null" would be stored as a null property; the detail page
		// treats nil as the delete sentinel. Neither is what a user typing JSON
		// means, so it is rejected on both surfaces.
		for _, field := range []string{"prop_meta", "prop_checks", "prop_nickname", "prop_tags[0]"} {
			t.Run(field, func(t *testing.T) {
				f := &fakeMemory{compiled: compiled, createObjectResult: &GraphObject{ID: "o-new"}}
				e := echo.New()
				e.POST("/objects", newServer(f).uiObjectCreate)

				form := url.Values{}
				form.Set("type", "person")
				form.Set(field, "null")
				rec := post(e, form)

				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want a 200 re-render", rec.Code)
				}
				if f.lastCreateObjectReq != nil {
					t.Errorf("CreateObject called with %v, want a rejected submission", f.lastCreateObjectReq.Properties)
				}
				if body := rec.Body.String(); !strings.Contains(body, "null is not a value") {
					t.Errorf("body missing the null error in:\n%s", body)
				}
			})
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

	t.Run("server validation message is echoed on the create form", func(t *testing.T) {
		// The backend names the property and element that failed (the items
		// element type, for example); showing the generic line would hide the
		// one thing the user needs to correct.
		f := &fakeMemory{compiled: compiled, createObjectErr: &memoryHTTPError{
			Status:  http.StatusBadRequest,
			Code:    "validation_error",
			Message: "property validation failed: authors: element 0: expected string, got float64",
		}}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "person")
		form.Add("prop_authors[0]", "123")
		form.Add("prop_authors[1]", "x")
		rec := post(e, form)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 re-render", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{"property validation failed", "authors", "element 0", `id="prop-authors"`} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q", want)
			}
		}
	})

	t.Run("upstream failure is not echoed", func(t *testing.T) {
		// A 5xx/transport failure is an outage, not a user mistake: its detail
		// must stay out of the form.
		f := &fakeMemory{compiled: compiled, createObjectErr: &memoryHTTPError{
			Status:  http.StatusBadGateway,
			Message: "dial tcp 10.0.0.1:8080: connection refused",
		}}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		form := url.Values{}
		form.Set("type", "person")
		rec := post(e, form)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 re-render", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Could not create the object.") {
			t.Errorf("expected the generic failure message in:\n%s", body)
		}
		if strings.Contains(body, "10.0.0.1") {
			t.Errorf("backend detail leaked into the form:\n%s", body)
		}
	})

	t.Run("rejected JSON document is re-rendered as the same editor and text", func(t *testing.T) {
		// The save failed on the server ("expected array, got object"), and the
		// create form has no stored value to rebuild the field from. The hidden
		// marker is what keeps the JSON editor — and the exact text — instead of
		// degrading to a chip or wrapping the document in a one-element list.
		f := &fakeMemory{compiled: compiled, createObjectErr: fmt.Errorf("property validation failed: checks: expected array, got map[string]interface {}")}
		e := echo.New()
		e.POST("/objects", newServer(f).uiObjectCreate)

		doc := "{\n  \"n\": 1\n}"
		form := url.Values{}
		form.Set("type", "person")
		form.Set("prop_checks", doc)
		form.Set("jsonprop_checks", "1")
		rec := post(e, form)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 re-render", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `<textarea name="prop_checks" rows="1" data-autogrow`) {
			t.Errorf("expected the JSON editor back in:\n%s", body)
		}
		if strings.Contains(body, `id="prop-checks"`) {
			t.Errorf("array document degraded to a chip input in:\n%s", body)
		}
		if !strings.Contains(body, "{\n  &#34;n&#34;: 1\n}") {
			t.Errorf("expected the submitted document verbatim in:\n%s", body)
		}
		if strings.Contains(body, "[\n  {\n") {
			t.Errorf("submitted document was wrapped in a list in:\n%s", body)
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
			// array → TagList chip input (no suggestions); object → JSON text
			// field, because an object is one document, not a set of chips
			`id="prop-tags"`, `placeholder="Add an item..."`,
			`<textarea name="prop_meta" rows="1" data-autogrow`,
			// string → auto-growing textarea (free-form text may be long)
			`<textarea name="prop_name" rows="1" data-autogrow`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("widget HTML missing %q in:\n%s", want, html)
			}
		}
		// old widgets are gone: the array chip field is never a JSON textarea,
		// the object field is never a chip input, and the checkbox + "true"
		// label is retired
		for _, gone := range []string{"<textarea name=\"prop_tags\"", `id="prop-meta"`, `class="checkbox checkbox-sm"`, `>true</span>`} {
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

// TestTagListWrapperMatchesInputBorder pins #2311: the chip field (go-daisy
// form/TagList) must draw the same outline as every other form field instead of
// the component's near-black border-line-strong. daisyUI's .input/.select/
// .textarea border with --input-color, which defaults to var(--color-base-content)
// at 20%; app.css overrides the TagList wrapper with that same token. The
// override is keyed on the `.memory-chip-list` class the call sites pass, so
// this test pins both halves: the hook is still rendered, and the rule still
// paints the input token (not the ladder's strong step).
func TestTagListWrapperMatchesInputBorder(t *testing.T) {
	const (
		hook     = "memory-chip-list"
		selector = ".memory-chip-list > div > div:not(.absolute):not(:focus-within)"
		wantDecl = "border-color: color-mix(in oklab, var(--color-base-content) 20%, transparent)"
	)

	objectTypes := []CompiledType{{
		Name:       "person",
		Properties: json.RawMessage(`{"tags":{"type":"array"}}`),
	}}
	propDefs := []objectPropertyDef{{Name: "tags", Type: "array"}}
	createHTML := renderHTML(t, ObjectCreatePage(objectTypes, propDefs, objectCreateForm{Type: "person"}, nil, nil, nil))
	editHTML := renderHTML(t, objectBuiltinPropertiesCard(&GraphObject{Key: "k", Labels: []string{"vip"}}, nil))
	for name, html := range map[string]string{"create": createHTML, "edit": editHTML} {
		if !strings.Contains(html, hook) {
			t.Errorf("%s page TagList must carry the %q hook that app.css styles", name, hook)
		}
	}

	css, err := os.ReadFile("webui/css/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	block := cssRuleBlock(t, string(css), selector)
	if !strings.Contains(block, wantDecl) {
		t.Errorf("%s must border the field with daisyUI's input token (%s); got:\n%s", selector, wantDecl, block)
	}
	if strings.Contains(block, "--color-line-strong") {
		t.Errorf("%s must not use the ladder's strong step — that is the #2311 divergence; got:\n%s", selector, block)
	}
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

// TestObjectStatusField pins the rendered controls: a type that declares a
// workflow gets the ticket-style status picker at the top of the detail page,
// with the current status as its trigger and the other statuses in the menu. A
// type that declares no workflow renders no workflow controls at all — its own
// `status` schema property (when it has one) is the status editor. The create
// form keeps its native select.
func TestObjectStatusField(t *testing.T) {
	boardStatuses := []string{"ready", "in_progress", "review", "revision", "blocked", "done"}

	renderControls := func(current string) string {
		obj := &GraphObject{ID: "o1", Type: "Task", Key: "t1", Status: current}
		objectTypes := []CompiledType{{Name: "Task", BoardEnabled: true, AllowedStatuses: boardStatuses}}
		return renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, nil, nil, objectStatusControlFor(objectTypes, "Task"), nil, "", 0))
	}

	t.Run("detail page renders the allowed statuses in the picker", func(t *testing.T) {
		html := renderControls("review")
		if !strings.Contains(html, `data-testid="object-status-control"`) {
			t.Fatalf("detail page missing the status picker in:\n%s", html)
		}
		for _, opt := range boardStatuses {
			if opt == "review" {
				if strings.Contains(html, `name="status" value="`+opt+`"`) {
					t.Errorf("current status should be omitted from the menu in:\n%s", html)
				}
				continue
			}
			if !strings.Contains(html, `name="status" value="`+opt+`"`) {
				t.Errorf("status picker missing option %q in:\n%s", opt, html)
			}
		}
		if !strings.Contains(html, "Review") {
			t.Errorf("status trigger should show the humanised current status in:\n%s", html)
		}
		if strings.Contains(html, `id="object-status"`) {
			t.Errorf("detail page must not render the old built-in status select in:\n%s", html)
		}
	})

	t.Run("empty status renders a Set status trigger and every option", func(t *testing.T) {
		html := renderControls("")
		if !strings.Contains(html, "Set status") {
			t.Errorf("empty status should render the Set status trigger in:\n%s", html)
		}
		for _, opt := range boardStatuses {
			if !strings.Contains(html, `name="status" value="`+opt+`"`) {
				t.Errorf("empty status should offer %q in:\n%s", opt, html)
			}
		}
	})

	t.Run("stored status outside the allowed set stays visible on the trigger", func(t *testing.T) {
		html := renderControls("shipped")
		if !strings.Contains(html, "Shipped") {
			t.Errorf("off-list status should stay visible on the trigger in:\n%s", html)
		}
		for _, opt := range boardStatuses {
			if !strings.Contains(html, `name="status" value="`+opt+`"`) {
				t.Errorf("off-list status should still offer %q in:\n%s", opt, html)
			}
		}
	})

	t.Run("create page renders the native status select", func(t *testing.T) {
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

	t.Run("type with no workflow renders no workflow controls (user report)", func(t *testing.T) {
		// A Scenario declares a typed `status` enum property but no board config:
		// the built-in workflow controls must be absent, and the typed property is
		// the status editor.
		obj := &GraphObject{ID: "o1", Type: "Scenario", Key: "S-005", Properties: map[string]any{"status": "proposed"}}
		objectTypes := []CompiledType{{
			Name:       "Scenario",
			Properties: json.RawMessage(`{"status":{"type":"string","enum":["proposed","active","archived"]}}`),
		}}
		html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, nil, nil, objectStatusControlFor(objectTypes, "Scenario"), nil, "", 0))
		if strings.Contains(html, `data-testid="object-workflow-panel"`) || strings.Contains(html, `id="object-status"`) {
			t.Errorf("no-workflow type must not render workflow controls in:\n%s", html)
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

	t.Run("detail handler degrades to the header badge when compiled types fail", func(t *testing.T) {
		// The schema cannot say whether the type declares a workflow, so no
		// dropdown is rendered; the stored status stays visible as the header
		// badge rather than disappearing.
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
		if strings.Contains(body, `data-testid="object-workflow-panel"`) || strings.Contains(body, `name="status" type="text"`) {
			t.Fatalf("degraded schema should not render editable workflow controls:\n%s", body)
		}
		if !strings.Contains(body, "In progress") {
			t.Errorf("degraded control should keep the stored status visible:\n%s", body)
		}
	})

	t.Run("degraded control renders the header badge only", func(t *testing.T) {
		obj := &GraphObject{ID: "o1", Type: "Task", Key: "t1", Status: "in_progress"}
		html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, nil, nil, objectStatusControlUnavailable(), nil, "", 0))
		if strings.Contains(html, `data-testid="object-workflow-panel"`) || strings.Contains(html, `name="status" type="text"`) {
			t.Errorf("unavailable schema should not render editable workflow controls in:\n%s", html)
		}
		if !strings.Contains(html, "In progress") {
			t.Errorf("degraded control should keep the stored status visible in:\n%s", html)
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

func TestCompiledTypePropertyDefsArrayItems(t *testing.T) {
	objectTypes := []CompiledType{{Name: "paper", Properties: json.RawMessage(`{
		"authors":{"type":"array","items":{"type":"string"}},
		"alternatives":{"type":"array","items":{"type":"object","properties":{"option":{"type":"string"}}}},
		"matrix":{"type":"array","items":{"type":"array","items":{"type":"number"}}},
		"plain":{"type":"array"},
		"weird":{"type":"array","items":true,"title":"Weird array"}
	}`)}}

	got := compiledTypePropertyDefs(objectTypes, "paper")
	want := map[string]string{
		"authors":      "string",
		"alternatives": "object",
		"matrix":       "array",
		"plain":        "",
		"weird":        "",
	}
	if len(got) != len(want) {
		t.Fatalf("defs = %+v, want %d", got, len(want))
	}
	for _, def := range got {
		if def.ItemType != want[def.Name] {
			t.Errorf("%s item type = %q, want %q", def.Name, def.ItemType, want[def.Name])
		}
		// A non-object items form must not take the rest of the metadata with
		// it: the property still renders from its array type and title.
		if def.Name == "weird" && (def.Type != "array" || def.Title != "Weird array") {
			t.Errorf("weird = %+v, want the array type and title preserved", def)
		}
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

func TestJSONEditorValues(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		// an object property is one document, shown pretty-printed when it
		// parses and unchanged when it does not
		{"object empty", jsonDocumentEditorValue(nil), ""},
		{"object compact", jsonDocumentEditorValue([]string{`{"a":{"b":[1,2]}}`}), "{\n  \"a\": {\n    \"b\": [\n      1,\n      2\n    ]\n  }\n}"},
		{"object plain string", jsonDocumentEditorValue([]string{"not json"}), "not json"},
		// an array property is always one JSON list, even with one element
		{"array empty", jsonArrayEditorValue(nil, nil, false, false), "[]"},
		{"array one string", jsonArrayEditorValue([]string{"x"}, nil, false, false), "[\n  \"x\"\n]"},
		{"array two strings", jsonArrayEditorValue([]string{"a", "b"}, nil, false, false), "[\n  \"a\",\n  \"b\"\n]"},
		{"array one document", jsonArrayEditorValue([]string{`{"n":1}`}, nil, false, false), "[\n  {\n    \"n\": 1\n  }\n]"},
		{"array one invalid", jsonArrayEditorValue([]string{"not json"}, nil, false, false), "[\n  \"not json\"\n]"},
		// a create-form document comes back verbatim: a rejected save must not
		// silently reshape what the user typed into a one-element list
		{"array submitted document", jsonArrayEditorValue([]string{"{\n  \"n\": 1\n}"}, nil, true, false), "{\n  \"n\": 1\n}"},
		// a stored array is rendered from the stored value, so element types are
		// exact: a number stays a number and a string that looks like JSON stays
		// a quoted string (#2419 — a chip would stringify both).
		{"array stored strings", jsonArrayEditorValue([]string{"a", "b"}, []any{"a", "b"}, false, false), "[\n  \"a\",\n  \"b\"\n]"},
		{"array stored json-looking string", jsonArrayEditorValue([]string{`{"a":1}`}, []any{`{"a":1}`}, false, false), "[\n  \"{\\\"a\\\":1}\"\n]"},
		{"array stored typed", jsonArrayEditorValue([]string{`{"option":"a","pros":["x"]}`, "2"}, []any{map[string]any{"option": "a", "pros": []any{"x"}}, float64(2)}, false, false),
			"[\n  {\n    \"option\": \"a\",\n    \"pros\": [\n      \"x\"\n    ]\n  },\n  2\n]"},
		// a declared element type makes the chip text the value: a retry after a
		// rejected create must not turn "123"/"null" into a number/null (the
		// server would then reject them as the wrong element type).
		{"array declared items keeps chip text", jsonArrayEditorValue([]string{"123", "null"}, nil, false, true), "[\n  \"123\",\n  \"null\"\n]"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestArrayUsesJSONEditor(t *testing.T) {
	cases := []struct {
		name          string
		def           objectPropertyDef
		vals          []string
		stored        any
		submittedJSON bool
		want          bool
	}{
		{"no items schema, short strings", objectPropertyDef{Name: "tags", Type: "array"}, []string{"a"}, nil, false, false},
		{"items string", objectPropertyDef{Name: "authors", Type: "array", ItemType: "string"}, []string{"a"}, nil, false, false},
		{"items number", objectPropertyDef{Name: "scores", Type: "array", ItemType: "number"}, []string{"1"}, nil, false, false},
		{"items object with no values", objectPropertyDef{Name: "alternatives", Type: "array", ItemType: "object"}, nil, nil, false, true},
		{"items array with no values", objectPropertyDef{Name: "matrix", Type: "array", ItemType: "array"}, nil, nil, false, true},
		{"long element", objectPropertyDef{Name: "tags", Type: "array", ItemType: "string"}, []string{strings.Repeat("a", jsonEditorElementLimit+1)}, nil, false, true},
		{"stored typed element", objectPropertyDef{Name: "scores", Type: "array", ItemType: "number"}, []string{"1"}, []any{float64(1)}, false, true},
		{"submitted JSON document", objectPropertyDef{Name: "tags", Type: "array", ItemType: "string"}, []string{"[]"}, nil, true, true},
	}
	for _, c := range cases {
		if got := arrayUsesJSONEditor(c.def, c.vals, c.stored, c.submittedJSON); got != c.want {
			t.Errorf("%s: arrayUsesJSONEditor = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestArrayStoredNeedsJSONEditor(t *testing.T) {
	cases := []struct {
		name   string
		stored any
		want   bool
	}{
		{"no stored value", nil, false},
		{"stored strings", []any{"a", "b"}, false},
		{"stored []string", []string{"a", "b"}, false},
		{"stored number", []any{"a", float64(2)}, true},
		{"stored object", []any{map[string]any{"option": "a"}}, true},
		{"stored list", []any{[]any{"a"}}, true},
		{"stored null element", []any{nil}, true},
		{"stored non-array value", "abc", false},
	}
	for _, c := range cases {
		if got := arrayStoredNeedsJSONEditor(c.stored); got != c.want {
			t.Errorf("%s: arrayStoredNeedsJSONEditor(%#v) = %v, want %v", c.name, c.stored, got, c.want)
		}
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
		{objectPropertyDef{Name: "meta", Type: "object"}, `<textarea name="prop_meta" rows="1" data-autogrow`},
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
		html := renderHTML(t, propertyInput(c.def, nil, nil, false))
		if !strings.Contains(html, c.want) {
			t.Errorf("%s: HTML missing %q in:\n%s", c.def.Name, c.want, html)
		}
	}

	// enum select lists the allowed values
	htmlSel := renderHTML(t, propertyInput(objectPropertyDef{Name: "category", Type: "string", Enum: []string{"preference", "fact", "correction"}}, nil, nil, false))
	for _, want := range []string{`<select name="prop_category"`, `value="preference"`, `value="correction"`} {
		if !strings.Contains(htmlSel, want) {
			t.Errorf("enum select missing %q in:\n%s", want, htmlSel)
		}
	}

	// enum select keeps a stored value that is outside the declared enum
	htmlOff := renderHTML(t, propertyInput(objectPropertyDef{Name: "category", Type: "string", Enum: []string{"preference", "fact"}}, []string{"custom_value"}, nil, false))
	if !strings.Contains(htmlOff, `value="custom_value" selected`) {
		t.Errorf("off-enum value not preserved in:\n%s", htmlOff)
	}
	// enum select marks the matching option selected for a known value
	htmlOn := renderHTML(t, propertyInput(objectPropertyDef{Name: "category", Type: "string", Enum: []string{"preference", "fact"}}, []string{"fact"}, nil, false))
	if !strings.Contains(htmlOn, `value="fact" selected`) {
		t.Errorf("matching enum value not selected in:\n%s", htmlOn)
	}

	// array → chip input while every value is short and single-line (the text
	// input itself carries no name=: values submit via Alpine-rendered hidden
	// inputs); a long or multi-line element cannot be a chip, so the whole
	// array is edited as JSON instead.
	htmlChips := renderHTML(t, propertyInput(objectPropertyDef{Name: "tags", Type: "array"}, []string{"x", "y"}, nil, false))
	if !strings.Contains(htmlChips, `placeholder="Add an item..."`) || !strings.Contains(htmlChips, `memory-chip-list`) || strings.Contains(htmlChips, `name="prop_tags" type="text"`) {
		t.Errorf("short array: expected TagList input in:\n%s", htmlChips)
	}
	long := strings.Repeat("a", jsonEditorElementLimit+1)
	htmlLong := renderHTML(t, propertyInput(objectPropertyDef{Name: "tags", Type: "array"}, []string{long}, nil, false))
	if !strings.Contains(htmlLong, `<textarea name="prop_tags" rows="1" data-autogrow`) || strings.Contains(htmlLong, `placeholder="Add an item..."`) {
		t.Errorf("long array element: expected the JSON editor in:\n%s", htmlLong)
	}
	// ONE long element is still a list: a bare document would be submitted for
	// an array property and rejected as "expected array, got string".
	if !strings.Contains(htmlLong, ">[\n  &#34;"+long+"&#34;\n]</textarea>") {
		t.Errorf("single-element array: expected a one-element JSON array in:\n%s", htmlLong)
	}
	htmlMultiline := renderHTML(t, propertyInput(objectPropertyDef{Name: "tags", Type: "array"}, []string{"a\nb"}, nil, false))
	if !strings.Contains(htmlMultiline, `<textarea name="prop_tags" rows="1" data-autogrow`) {
		t.Errorf("multi-line array element: expected the JSON editor in:\n%s", htmlMultiline)
	}
	if !strings.Contains(htmlMultiline, ">[\n  &#34;a\\nb&#34;\n]</textarea>") {
		t.Errorf("multi-line array element: expected one escaped JSON string in the array:\n%s", htmlMultiline)
	}
	// an array of one JSON document is a one-element list of that document
	htmlDocArr := renderHTML(t, propertyInput(objectPropertyDef{Name: "checks", Type: "array"}, []string{"{\n  \"n\": 1\n}"}, nil, false))
	if !strings.Contains(htmlDocArr, ">[\n  {\n    &#34;n&#34;: 1\n  }\n]</textarea>") {
		t.Errorf("array of one document: expected a one-element JSON array in:\n%s", htmlDocArr)
	}

	// a stored typed array is edited as JSON even though every element is short:
	// a chip can only submit text, so saving it would stringify the object and
	// the number (#2419).
	storedTyped := []any{map[string]any{"option": "a", "pros": []any{"x"}}, float64(2)}
	htmlTyped := renderHTML(t, propertyInput(objectPropertyDef{Name: "alternatives", Type: "array"}, propValues(storedTyped), storedTyped, false))
	if !strings.Contains(htmlTyped, `<textarea name="prop_alternatives" rows="1" data-autogrow`) || strings.Contains(htmlTyped, `placeholder="Add an item..."`) {
		t.Errorf("stored typed array: expected the JSON editor in:\n%s", htmlTyped)
	}
	if !strings.Contains(htmlTyped, "&#34;option&#34;: &#34;a&#34;") || !strings.Contains(htmlTyped, "\n  2\n]") {
		t.Errorf("stored typed array: expected the typed document in:\n%s", htmlTyped)
	}
	// a stored string array stays on chips however short the values are, so a
	// numeric-looking string is never reinterpreted as a number
	htmlStoredStrings := renderHTML(t, propertyInput(objectPropertyDef{Name: "tags", Type: "array"}, []string{"1", "true"}, []any{"1", "true"}, false))
	if !strings.Contains(htmlStoredStrings, `placeholder="Add an item..."`) {
		t.Errorf("stored string array: expected chips in:\n%s", htmlStoredStrings)
	}

	// a declared object element type takes the JSON editor even when the array
	// is empty: there is no stored element to sniff, and a chip submits text, so
	// it could never represent the element.
	htmlObjectItems := renderHTML(t, propertyInput(objectPropertyDef{Name: "alternatives", Type: "array", ItemType: "object"}, nil, nil, false))
	if !strings.Contains(htmlObjectItems, `<textarea name="prop_alternatives" rows="1" data-autogrow`) || strings.Contains(htmlObjectItems, `placeholder="Add an item..."`) {
		t.Errorf("object items: expected the JSON editor in:\n%s", htmlObjectItems)
	}
	// a scalar element type keeps the chip editor: each chip's text is coerced
	// to the declared type by the server, so a string array's numeric-looking
	// value is not reinterpreted by the form.
	htmlStringItems := renderHTML(t, propertyInput(objectPropertyDef{Name: "authors", Type: "array", ItemType: "string"}, []string{"a", "123"}, nil, false))
	if !strings.Contains(htmlStringItems, `placeholder="Add an item..."`) {
		t.Errorf("string items: expected chips in:\n%s", htmlStringItems)
	}

	// object → a JSON document is one value, never chips: a chip is a
	// fixed-height badge and a multi-KB document painted over the page.
	objectJSON := renderHTML(t, propertyInput(objectPropertyDef{Name: "meta", Type: "object"}, []string{`{"a":{"b":[1,2]}}`}, nil, false))
	if !strings.Contains(objectJSON, `<textarea name="prop_meta" rows="1" data-autogrow`) || strings.Contains(objectJSON, `placeholder="Add an item..."`) {
		t.Errorf("object: expected the JSON editor in:\n%s", objectJSON)
	}
	// the stored document is pretty-printed so a compact snapshot stays readable
	if !strings.Contains(objectJSON, "{\n  &#34;a&#34;: {") {
		t.Errorf("object: expected pretty-printed JSON in:\n%s", objectJSON)
	}
	// a plain string value is shown unchanged rather than hidden
	plain := renderHTML(t, propertyInput(objectPropertyDef{Name: "meta", Type: "object"}, []string{"not json"}, nil, false))
	if !strings.Contains(plain, "not json") {
		t.Errorf("object: plain value must be preserved in:\n%s", plain)
	}

	// scalar inputs reflect the first value
	htmlVal := renderHTML(t, propertyInput(objectPropertyDef{Name: "born", Type: "date"}, []string{"2026-01-02"}, nil, false))
	if !strings.Contains(htmlVal, `value="2026-01-02"`) {
		t.Errorf("date input missing value in:\n%s", htmlVal)
	}

	// boolean renders a three-state select and never posts a hidden false
	html := renderHTML(t, propertyInput(objectPropertyDef{Name: "active", Type: "boolean"}, []string{"true"}, nil, false))
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
	htmlUnset := renderHTML(t, propertyInput(objectPropertyDef{Name: "active", Type: "boolean"}, nil, nil, false))
	if !strings.Contains(htmlUnset, `<option value="" selected>Not set</option>`) {
		t.Errorf("unset boolean must default to Not set:\n%s", htmlUnset)
	}
	htmlFalse := renderHTML(t, propertyInput(objectPropertyDef{Name: "active", Type: "boolean"}, []string{"false"}, nil, false))
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
		html := renderHTML(t, propertyInput(c.def, []string{c.val}, nil, false))
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
		html := renderHTML(t, propertyInput(def, []string{"x"}, nil, false))
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
	html := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, nil, nil, nil, trail, "", 0))

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
	const keyHintText = `>owner_notes</span>`

	form := renderHTML(t, ObjectCreatePage([]CompiledType{{Name: "Person"}}, defs, objectCreateForm{Type: "Person"}, nil, nil, nil))
	if !strings.Contains(form, labelledLegend) {
		t.Errorf("create form must label the property by its title:\n%s", form)
	}
	if !strings.Contains(form, keyHintText) {
		t.Errorf("create form must keep the machine key visible as text:\n%s", form)
	}
	if !strings.Contains(form, `<legend class="fieldset-legend">plain</legend>`) {
		t.Errorf("unlabelled property must keep its key as the label:\n%s", form)
	}
	if strings.Contains(form, `>plain</span>`) {
		t.Errorf("unlabelled property must not repeat its key as a hint:\n%s", form)
	}

	obj := &GraphObject{ID: "o1", Type: "Person", Key: "p1", Properties: map[string]any{"owner_notes": "hello"}}
	detail := renderHTML(t, ObjectDetailPage(obj, objectBranchRef{}, nil, nil, nil, "", nil, nil, defs, nil, nil, nil, "", 0))
	if !strings.Contains(detail, labelledLegend) {
		t.Errorf("detail properties card must label the property by its title:\n%s", detail)
	}
	if !strings.Contains(detail, keyHintText) {
		t.Errorf("detail properties card must keep the machine key as text:\n%s", detail)
	}
}

// TestRelationshipOptionsSkipsShadowed pins that the connect-object dialog never
// offers an overridden relationship declaration.
func TestRelationshipOptionsSkipsShadowed(t *testing.T) {
	rels := []CompiledType{
		{Name: "works_for", Label: "Old label", SourceType: "person", Shadowed: true},
		{Name: "works_for", Label: "Works for", SourceType: "person"},
	}
	got := relationshipOptions(rels, "person")
	if len(got) != 1 {
		t.Fatalf("options = %+v, want one visible works_for", got)
	}
	if got[0].Shadowed || got[0].Label != "Works for" {
		t.Errorf("option = %+v, want the non-shadowed winner", got[0])
	}
}

func TestUIObjectStatusAndAssignee(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Task", Label: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "in_progress", "done"}},
	}}
	newServer := func(f *fakeMemory) (*Server, *echo.Echo) {
		s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
		e := echo.New()
		e.POST("/objects/:id/status", s.uiObjectStatus)
		e.POST("/objects/:id/assignee", s.uiObjectAssignee)
		e.GET("/objects/:id/preview", s.uiObjectPreviewPartial)
		e.GET("/objects/:id/history", s.uiObjectHistoryPage)
		e.GET("/objects/:id/history/partial", s.uiObjectHistoryPartial)
		e.GET("/objects/:id/similar", s.uiObjectSimilarPage)
		e.GET("/objects/:id/similar/partial", s.uiObjectSimilarPartial)
		return s, e
	}
	post := func(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	t.Run("work-item status and assignee redirect to the object page", func(t *testing.T) {
		f := &fakeMemory{
			compiled: compiled,
			objects:  []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "Task", Key: "t1", Status: "ready"}},
			members:  []ProjectMemberDto{{DisplayName: "Alice"}},
		}
		_, e := newServer(f)

		rec := post(e, "/objects/o1/status", "status=done")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1?updated=1" {
			t.Fatalf("status redirect = %d %q, want 303 /objects/o1?updated=1", rec.Code, rec.Header().Get("Location"))
		}
		if f.lastWorkItemID != "c1" || f.lastStatusTo != "done" {
			t.Errorf("SetWorkItemStatus got id=%q status=%q, want c1/done", f.lastWorkItemID, f.lastStatusTo)
		}

		rec = post(e, "/objects/o1/assignee", "assignee=Alice")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1?updated=1" {
			t.Fatalf("assignee redirect = %d %q, want 303 /objects/o1?updated=1", rec.Code, rec.Header().Get("Location"))
		}
		if f.lastWorkItemID != "c1" || f.lastReassignTo != "Alice" {
			t.Errorf("ReassignWorkItem got id=%q assignee=%q, want c1/Alice", f.lastWorkItemID, f.lastReassignTo)
		}
	})

	t.Run("from trail is preserved on page redirects", func(t *testing.T) {
		f := &fakeMemory{
			compiled: compiled,
			objects:  []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "Task", Key: "t1", Status: "ready"}},
		}
		_, e := newServer(f)
		from := "/documents/d1/extractions/j1"
		rec := post(e, "/objects/o1/status", "status=done&from="+url.QueryEscape(from))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		loc, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if loc.Path != "/objects/o1" || loc.Query().Get("from") != from || loc.Query().Get("updated") != "1" {
			t.Errorf("location = %q, want from preserved with updated=1", rec.Header().Get("Location"))
		}
	})

	t.Run("object without a key falls back to UpdateObject", func(t *testing.T) {
		f := &fakeMemory{
			compiled: compiled,
			objects:  []GraphObject{{ID: "o1", Type: "Note"}},
		}
		_, e := newServer(f)

		post(e, "/objects/o1/status", "status=done")
		if f.lastStatusTo != "" {
			t.Errorf("SetWorkItemStatus called for a keyless object: %q", f.lastStatusTo)
		}
		if f.lastUpdateReq == nil || f.lastUpdateReq.Status == nil || *f.lastUpdateReq.Status != "done" {
			t.Errorf("UpdateObject status = %+v, want done", f.lastUpdateReq)
		}

		post(e, "/objects/o1/assignee", "assignee=Alice")
		if f.lastReassignTo != "" {
			t.Errorf("ReassignWorkItem called for a keyless object: %q", f.lastReassignTo)
		}
		if f.lastUpdateReq == nil || f.lastUpdateReq.Assignee == nil || *f.lastUpdateReq.Assignee != "Alice" {
			t.Errorf("UpdateObject assignee = %+v, want Alice", f.lastUpdateReq)
		}
	})

	t.Run("failure redirects with an err flash", func(t *testing.T) {
		f := &fakeMemory{
			compiled:      compiled,
			objects:       []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "Task", Key: "t1", Status: "ready"}},
			workActionErr: fmt.Errorf("boom"),
		}
		_, e := newServer(f)
		rec := post(e, "/objects/o1/status", "status=done")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/objects/o1?err=boom" {
			t.Fatalf("failure redirect = %d %q, want 303 /objects/o1?err=boom", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("preview surface returns refreshed HTML and keeps board context", func(t *testing.T) {
		f := &fakeMemory{
			compiled: compiled,
			objects:  []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "Task", Key: "t1", Status: "ready"}},
			workItemDetail: &WorkItemDetail{Item: &WorkItem{
				CanonicalID: "c1", Type: "Task", Key: "t1", Status: "ready",
			}},
		}
		_, e := newServer(f)
		rec := post(e, "/objects/o1/status", "status=done&surface=preview&actions=board&rel=r1")
		if rec.Code != http.StatusOK {
			t.Fatalf("preview status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{
			`id="object-preview-heading"`,
			`data-testid="object-work-controls"`,
			`data-testid="object-status-control"`,
			`name="surface" value="preview"`,
			`name="actions" value="board"`,
			`name="rel" value="r1"`,
			`id="object-preview-actions-slot"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("preview response missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("preview surface renders a visible action error", func(t *testing.T) {
		f := &fakeMemory{
			compiled:      compiled,
			objects:       []GraphObject{{ID: "o1", CanonicalID: "c1", Type: "Task", Key: "t1", Status: "ready"}},
			workActionErr: fmt.Errorf("boom"),
		}
		_, e := newServer(f)
		rec := post(e, "/objects/o1/assignee", "assignee=Alice&surface=preview&actions=board")
		if rec.Code != http.StatusOK {
			t.Fatalf("preview error status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-testid="object-preview-action-error"`) || !strings.Contains(body, "boom") {
			t.Errorf("preview action error not rendered:\n%s", body)
		}
		if !strings.Contains(body, `data-testid="object-work-controls"`) {
			t.Errorf("preview should keep its controls after an error:\n%s", body)
		}
	})
}

func TestObjectSubpages(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "Task", Label: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "done"}},
		}},
		objects: []GraphObject{
			{ID: "o1", CanonicalID: "o1", Type: "Task", Key: "t1", Status: "ready"},
			{ID: "o2", CanonicalID: "o2", Type: "Task", Key: "t2", Status: "ready"},
		},
		relns: []GraphRelationship{{ID: "r1", Type: "related_to", SrcID: "o1", DstID: "o2"}},
		objectHistory: []GraphObject{
			{ID: "o1", Version: 2, Type: "Task", Key: "t1", Status: "done"},
			{ID: "o0", Version: 1, Type: "Task", Key: "t1", Status: "ready"},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/:id", s.uiObject)
	e.GET("/objects/:id/history", s.uiObjectHistoryPage)
	e.GET("/objects/:id/history/partial", s.uiObjectHistoryPartial)
	e.GET("/objects/:id/relationships", s.uiObjectRelationshipsPage)
	e.GET("/objects/:id/similar", s.uiObjectSimilarPage)
	e.GET("/objects/:id/similar/partial", s.uiObjectSimilarPartial)
	e.GET("/objects/:id/similar/count", s.uiObjectSimilarCountPartial)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	t.Run("history page renders the timeline and active rail tab", func(t *testing.T) {
		rec := get("/objects/o1/history")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{
			"History",
			`href="/objects/o1"`, `href="/objects/o1/history"`, `href="/objects/o1/similar"`,
			`data-testid="object-history-page"`,
			`data-testid="object-preview-history"`,
			`aria-current="page"`,
			"v2", "v1",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("history page missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "Finding similar objects") {
			t.Errorf("history page must not render the similar placeholder:\n%s", body)
		}
	})

	t.Run("history partial remains the drawer fragment", func(t *testing.T) {
		rec := get("/objects/o1/history/partial")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-testid="object-preview-history"`) || !strings.Contains(body, "v2") {
			t.Errorf("history partial missing timeline:\n%s", body)
		}
		if strings.Contains(body, "<!doctype html>") {
			t.Error("history partial must not render the full page shell")
		}
	})

	t.Run("similar page defers the vector search and marks its rail tab", func(t *testing.T) {
		rec := get("/objects/o1/similar")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{
			"Similar objects",
			`hx-get="/objects/o1/similar/partial"`,
			"Finding similar objects",
			`aria-current="page"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("similar page missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, `data-testid="object-preview-history"`) {
			t.Errorf("similar page must not render the history timeline:\n%s", body)
		}
	})

	t.Run("similar partial renders the deferred results", func(t *testing.T) {
		f.similar = []SimilarObject{{ID: "o2", Type: "Task", Key: "t2", Distance: 0.1}}
		rec := get("/objects/o1/similar/partial")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "t2") || !strings.Contains(rec.Body.String(), "Merge") {
			t.Errorf("similar partial missing results:\n%s", rec.Body.String())
		}
	})

	t.Run("subpages keep the read-only header status badge", func(t *testing.T) {
		data := objectDetailShellData{
			Obj:    &GraphObject{ID: "o1", Type: "Task", Key: "t1", Status: "ready"},
			Status: &objectStatusControl{Show: true, Options: []string{"ready", "done"}},
			Active: "history",
		}
		body := renderHTML(t, objectHistoryPage(data, nil, nil))
		if !strings.Contains(body, `data-status="ready"`) {
			t.Errorf("history subpage should keep the read-only header status badge:\n%s", body)
		}
		if strings.Contains(body, `data-testid="object-workflow-panel"`) {
			t.Errorf("the Workflow panel belongs to Overview only:\n%s", body)
		}
	})

	t.Run("relationships page renders the moved list and rail badge", func(t *testing.T) {
		rec := get("/objects/o1/relationships")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{
			`href="/objects/o1/relationships"`, `aria-current="page"`,
			`data-testid="object-relationships-count"`, "related to", "t2",
			`data-testid="object-connect-modal"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("relationships page missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("rail badges relationships count and defers the similar count", func(t *testing.T) {
		rec := get("/objects/o1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{
			`data-testid="object-relationships-count"`,
			`data-testid="object-similar-count-slot"`,
			`hx-get="/objects/o1/similar/count"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("overview rail missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("similar count badge renders the filtered count and saturation suffix", func(t *testing.T) {
		prev := f.similar
		defer func() { f.similar = prev }()

		f.similar = nil
		if body := strings.TrimSpace(get("/objects/o1/similar/count").Body.String()); body != "" {
			t.Errorf("zero similar count should render no badge, got %q", body)
		}

		f.similar = []SimilarObject{
			{ID: "s1", Distance: 0.1},
			{ID: "s2", Distance: 0.1},
			{ID: "s3", Distance: 0.1},
		}
		body := get("/objects/o1/similar/count").Body.String()
		if !strings.Contains(body, `data-testid="object-similar-count"`) || !strings.Contains(body, ">3</span>") {
			t.Errorf("similar count badge missing count: %s", body)
		}

		f.similar = make([]SimilarObject, objectSimilarFetchLimit)
		for i := range f.similar {
			f.similar[i] = SimilarObject{ID: "s", Distance: 0.1}
		}
		body = get("/objects/o1/similar/count").Body.String()
		if !strings.Contains(body, "10+") {
			t.Errorf("saturated similar count should carry +: %s", body)
		}
	})
}
