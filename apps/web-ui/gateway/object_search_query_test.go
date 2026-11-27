package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// objectLookupFake overrides the one method the id lookup's miss paths need: the
// shared fake reports an unknown id as a plain error, while the real client
// classifies the backend's status (404/400 are misses, 5xx is a page failure).
type objectLookupFake struct {
	*fakeMemory
	err error
}

func (f *objectLookupFake) GetGraphObject(ctx context.Context, id string) (*GraphObject, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.fakeMemory.GetGraphObject(ctx, id)
}

// TestParseObjectSearchQuery pins the three-way split of the search box's input:
// the explicit `id:` form, a bare uuid (which can only be an id), and everything
// else as relevance text. A malformed `id:` payload is reported rather than
// silently searched: the reader said what they meant, and it was not a query.
func TestParseObjectSearchQuery(t *testing.T) {
	const id = "d612b1f9-73b0-4d71-88d8-2e590a5943b5"

	cases := []struct {
		name    string
		in      string
		text    string
		id      string
		invalid string
	}{
		{name: "explicit id", in: "id:" + id, id: id},
		{name: "explicit id is case-insensitive", in: "ID:" + id, id: id},
		{name: "explicit id with padding", in: "  id:  " + id + "  ", id: id},
		{name: "upper-case uuid normalizes", in: "id:" + strings.ToUpper(id), id: id},
		{name: "bare uuid is a lookup", in: id, id: id},
		{name: "bare uuid with padding", in: " " + id + " ", id: id},
		{name: "uuid without hyphens", in: "d612b1f973b04d7188d82e590a5943b5", id: id},
		{name: "malformed explicit id", in: "id:d612b1f9", invalid: "d612b1f9"},
		{name: "empty explicit id", in: "id:", invalid: ""},
		{name: "plain query", in: "boiler service", text: "boiler service"},
		{name: "query that mentions an id", in: id + " boiler", text: id + " boiler"},
		{name: "empty input", in: "   ", text: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseObjectSearchQuery(tc.in)
			if got.Text != tc.text || got.ID != tc.id {
				t.Fatalf("parse(%q) = %+v, want text %q id %q", tc.in, got, tc.text, tc.id)
			}
			if tc.invalid != "" && got.InvalidID != tc.invalid {
				t.Errorf("InvalidID = %q, want %q", got.InvalidID, tc.invalid)
			}
			// "id:" alone is an explicit request with an empty payload: it is a
			// miss with a reason, never a search for the word "id:".
			if tc.in == "id:" && (got.InvalidID != "" || got.ID != "" || got.Text != "") {
				t.Errorf("empty explicit id = %+v, want an empty invalid payload", got)
			}
		})
	}
}

// TestObjectSearchIDLookupResolves pins the success path: one object, no
// ranking, and the note explains which visible filter the exact lookup bypassed.
func TestObjectSearchIDLookupResolves(t *testing.T) {
	obj := GraphObject{ID: "11111111-1111-1111-1111-111111111111", Type: "obligation", Key: "ob-boiler"}
	f := &fakeMemory{objects: []GraphObject{obj}}
	s := &Server{memory: f}

	results, lookup, err := s.objectSearchIDLookup(context.Background(), objectSearchTarget{ID: obj.ID}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Object.ID != obj.ID {
		t.Fatalf("results = %+v, want the one object", results)
	}
	if lookup == nil || lookup.ID != obj.ID || lookup.Reason != "" {
		t.Fatalf("lookup = %+v, want a found lookup naming the id", lookup)
	}
	if lookup.Detail != "" {
		t.Errorf("Detail = %q, want none with no filter set", lookup.Detail)
	}

	_, lookup, err = s.objectSearchIDLookup(context.Background(), objectSearchTarget{ID: obj.ID}, "task", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if lookup.Detail != "type and branch filters not applied" {
		t.Errorf("Detail = %q, want both filters named", lookup.Detail)
	}
}

// TestObjectSearchIDLookupMiss pins the miss paths: a 404 and a 400 are answers
// (no object, explained), while a 5xx is a page failure the caller must report
// rather than dress up as "not found".
func TestObjectSearchIDLookupMiss(t *testing.T) {
	const id = "d612b1f9-73b0-4d71-88d8-2e590a5943b5"

	for _, status := range []int{http.StatusNotFound, http.StatusBadRequest} {
		s := &Server{memory: &objectLookupFake{fakeMemory: &fakeMemory{}, err: &memoryHTTPError{Status: status, Message: "nope"}}}
		results, lookup, err := s.objectSearchIDLookup(context.Background(), objectSearchTarget{ID: id}, "", "")
		if err != nil {
			t.Fatalf("status %d: unexpected error: %v", status, err)
		}
		if len(results) != 0 {
			t.Errorf("status %d: results = %+v, want none", status, results)
		}
		if lookup == nil || lookup.ID != id || !strings.Contains(lookup.Reason, "No object with this id") {
			t.Errorf("status %d: lookup = %+v, want the id plus a reason", status, lookup)
		}
	}

	boom := errors.New("connection refused")
	s := &Server{memory: &objectLookupFake{fakeMemory: &fakeMemory{}, err: boom}}
	if _, _, err := s.objectSearchIDLookup(context.Background(), objectSearchTarget{ID: id}, "", ""); !errors.Is(err, boom) {
		t.Errorf("5xx error = %v, want it propagated", err)
	}
}

// TestObjectSearchIDLookupInvalid pins the malformed-id answer: no backend call,
// no results, and a reason that says what an object id is.
func TestObjectSearchIDLookupInvalid(t *testing.T) {
	// err is set so any backend call would fail the test as a page error.
	s := &Server{memory: &objectLookupFake{fakeMemory: &fakeMemory{}, err: errors.New("must not be called")}}
	results, lookup, err := s.objectSearchIDLookup(context.Background(), objectSearchTarget{InvalidID: "d612b1f9"}, "", "")
	if err != nil {
		t.Fatalf("invalid id must not be a page failure: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %+v, want none", results)
	}
	if lookup == nil || !strings.Contains(lookup.Reason, "UUID") {
		t.Errorf("lookup = %+v, want a reason naming a UUID", lookup)
	}
	if lookup.ID != "" {
		t.Errorf("ID = %q, want empty: there is no id to name", lookup.ID)
	}
}

// TestObjectSearchResultsShown pins the two-rule floor: a noise floor at the
// bottom of the band unrelated neighbours score in, and a relative one that
// trims the tail of a query that did match. showAll returns everything.
func TestObjectSearchResultsShown(t *testing.T) {
	matching := []ObjectSearchResult{
		{Object: GraphObject{ID: "a"}, Score: 0.9},
		{Object: GraphObject{ID: "b"}, Score: 0.6},
		{Object: GraphObject{ID: "c"}, Score: 0.3},
		{Object: GraphObject{ID: "d"}, Score: 0.2},
	}

	shown := objectSearchResultsShown(matching, false)
	if len(shown) != 2 || shown[0].Object.ID != "a" || shown[1].Object.ID != "b" {
		t.Fatalf("shown = %+v, want the two hits above 0.35 * the best hit", shown)
	}
	if got := objectSearchResultsShown(matching, true); len(got) != len(matching) {
		t.Errorf("showAll kept %d hits, want all %d", len(got), len(matching))
	}

	// The measured shape of a real query in this project: "boiler" (a live
	// Obligation's key) scored 0.22 at the top and decayed smoothly into the
	// vector leg's nearest neighbours. The three real hits stay, the tail goes
	// behind "show all" — and a floor above the top hit, which this test used to
	// enforce at 0.25, would have emptied the whole page.
	realQuery := []ObjectSearchResult{
		{Object: GraphObject{ID: "boiler"}, Score: 0.22},
		{Object: GraphObject{ID: "parts"}, Score: 0.135},
		{Object: GraphObject{ID: "engineer"}, Score: 0.121},
		{Object: GraphObject{ID: "chimney"}, Score: 0.086},
		{Object: GraphObject{ID: "contract"}, Score: 0.053},
	}
	shown = objectSearchResultsShown(realQuery, false)
	if len(shown) != 3 {
		t.Fatalf("shown = %d hits, want the three above the noise floor: %+v", len(shown), shown)
	}

	// A query whose every neighbour sits in the noise band says so rather than
	// listing the vector leg's nearest objects as if they were answers.
	junk := []ObjectSearchResult{
		{Object: GraphObject{ID: "x"}, Score: 0.099},
		{Object: GraphObject{ID: "y"}, Score: 0.09},
		{Object: GraphObject{ID: "z"}, Score: 0.081},
	}
	if shown := objectSearchResultsShown(junk, false); len(shown) != 0 {
		t.Errorf("shown = %d junk hits, want none below the noise floor", len(shown))
	}
	if shown := objectSearchResultsShown(junk, true); len(shown) != len(junk) {
		t.Errorf("showAll kept %d junk hits, want all %d", len(shown), len(junk))
	}

}

func TestObjectSearchShowsAll(t *testing.T) {
	for in, want := range map[string]bool{
		"1": true, "true": true, "YES": true, "all": true,
		"": false, "0": false, "false": false, "2": false,
	} {
		if got := objectSearchShowsAll(in); got != want {
			t.Errorf("objectSearchShowsAll(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestSearchShowAllHref pins the "show all" link: it carries the query,
// its mode and the active filters forward and adds only all=1, so following it
// shows the same search with the held-back hits included.
func TestSearchShowAllHref(t *testing.T) {
	href := objectsSearchShowAllHref(objectsPageData{Query: "boiler", TypeFilter: "obligation", BranchID: "b1"})
	u, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if got := q.Get("all"); got != "1" {
		t.Errorf("all = %q, want 1", got)
	}
	for key, want := range map[string]string{"q": "boiler", "type": "obligation", "branch": "b1"} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if strings.Contains(href, "order=") {
		t.Errorf("href = %q, want no order: search results are relevance-ranked", href)
	}
}

// TestSearchResultsNoteExactLookup pins the lookup summary: it names the
// resolved id and says which filters the exact match bypassed, instead of
// counting results as if the id had been searched for.
func TestSearchResultsNoteExactLookup(t *testing.T) {
	const id = "d612b1f9-73b0-4d71-88d8-2e590a5943b5"
	html := renderHTML(t, searchResultsNote(objectsPageData{
		Query:  "id:" + id,
		In:     searchScopeAll,
		Lookup: &objectSearchLookup{ID: id, Detail: "type filter not applied"},
	}))

	if !strings.Contains(html, "Exact id match") {
		t.Errorf("lookup note must say the row was resolved:\n%s", html)
	}
	if !strings.Contains(html, id) {
		t.Errorf("lookup note must name the id:\n%s", html)
	}
	if !strings.Contains(html, "type filter not applied") {
		t.Errorf("lookup note must explain the bypassed filter:\n%s", html)
	}
	if strings.Contains(html, "matching") {
		t.Errorf("a lookup is not a result count:\n%s", html)
	}
}

// TestSearchResultsNoteHiddenCount pins the disclosure: the held-back
// hits are counted with a link that re-runs the same search with them included.
func TestSearchResultsNoteHiddenCount(t *testing.T) {
	html := renderHTML(t, searchResultsNote(objectsPageData{
		Query:       "boiler",
		In:          searchScopeAll,
		TypeFilter:  "obligation",
		Results:     []ObjectSearchResult{{Object: GraphObject{ID: "a"}, Score: 0.4}},
		HiddenCount: 3,
	}))

	if !strings.Contains(html, "3 lower-scoring matches hidden") {
		t.Errorf("hidden count missing:\n%s", html)
	}
	if !strings.Contains(html, "all=1") || !strings.Contains(html, "type=obligation") {
		t.Errorf("show-all link must keep the query and filters:\n%s", html)
	}
	if !strings.Contains(html, "1 result matching") {
		t.Errorf("the shown count must survive the disclosure:\n%s", html)
	}
}

// TestObjectsSearchWeakEmpty pins the "nothing strong" state: it reports the
// count and offers the escape instead of claiming there are no objects.
func TestObjectsSearchWeakEmpty(t *testing.T) {
	html := renderHTML(t, objectsSearchWeakEmpty(objectsPageData{Query: "boiler", In: searchScopeAll, HiddenCount: 12}))

	if !strings.Contains(html, "No strong matches") {
		t.Errorf("weak empty state must say what happened:\n%s", html)
	}
	if !strings.Contains(html, "12 objects came") {
		t.Errorf("weak empty state must name the count:\n%s", html)
	}
	if !strings.Contains(html, "Show 12 results") || !strings.Contains(html, "all=1") {
		t.Errorf("weak empty state must offer the held-back results:\n%s", html)
	}
}

// TestObjectsSearchLookupEmpty pins the miss state: the reason is rendered, and
// it does not reuse the "No matching objects" copy — the id, not the query, is
// what did not match.
func TestObjectsSearchLookupEmpty(t *testing.T) {
	reason := "No object with this id exists in this project."
	html := renderHTML(t, objectsSearchLookupEmpty(&objectSearchLookup{Reason: reason}))

	if !strings.Contains(html, "No object with that id") {
		t.Errorf("lookup miss must name the id shape:\n%s", html)
	}
	if !strings.Contains(html, reason) {
		t.Errorf("lookup miss must render its reason:\n%s", html)
	}
	if strings.Contains(html, "No matching objects") {
		t.Errorf("a lookup is not a text search:\n%s", html)
	}
}

// TestObjectsSearchEmptyMentionsIDLookup pins discoverability: the no-hits state
// is where a reader who pasted an id into the relevance search ends up, so it is
// where the id form is explained.
func TestObjectsSearchEmptyMentionsIDLookup(t *testing.T) {
	html := renderHTML(t, objectsSearchEmpty(objectsPageData{Query: "boiler"}))
	if !strings.Contains(html, "id:") {
		t.Errorf("the no-hits state must mention the id form:\n%s", html)
	}
}
