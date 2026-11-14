package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestObjectsBrowserCardGridRendersSelectionSurface pins the card-grid contract
// the bulk actions depend on: one card per object carrying pure Alpine selection
// state, and four self-contained dialog forms that each submit the selection
// once.
func TestObjectsBrowserCardGridRendersSelectionSurface(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))

	for _, want := range []string{
		`data-testid="objects-cards"`,
		`data-testid="object-card"`,
		`data-testid="objects-results-bar"`,
		`name="ids"`,
		`toggleAll`,
		`data-object-select`,
		`x-model="sel"`,
		`id="objects-bulk-delete-dialog"`,
		`id="objects-bulk-status-dialog"`,
		`id="objects-bulk-label-add-dialog"`,
		`id="objects-bulk-label-remove-dialog"`,
		`MemoryApp.openDialog(&#39;objects-knowledge-dialog&#39;)`,
		`id="objects-knowledge-dialog"`,
		`id="objects-load-more"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("objects browser missing %q", want)
		}
	}

	// Selection is Alpine-only. Card checkboxes carry no name/form, so a card can
	// never submit itself and `ids` cannot arrive twice (once per checked card,
	// once as the CSV the handler expects).
	if strings.Contains(html, `form="objects-bulk-form"`) || strings.Contains(html, `id="objects-bulk-form"`) {
		t.Error("bulk submits must come from per-dialog forms, not one shared form")
	}
	if strings.Contains(html, `<input type="checkbox" class="checkbox checkbox-sm" name=`) {
		t.Error("card checkboxes must not be submittable inputs")
	}

	// Exactly one hidden ids field per bulk dialog, each synced by the model.
	if got := strings.Count(html, `name="ids"`); got != 4 {
		t.Errorf("want one ids field per bulk dialog (4), got %d", got)
	}
	if got := strings.Count(html, `:value="sel.join(',')"`); got != 4 {
		t.Errorf("want every dialog to submit the selected ids, got %d", got)
	}
	for _, action := range []string{"label_add", "label_remove", "status", "delete"} {
		if want := `name="action" value="` + action + `"`; !strings.Contains(html, want) {
			t.Errorf("missing per-dialog action field %q", want)
		}
	}

	// Cards keep the browse facts: the type and ref ride above the name as one
	// identity line, and the workflow state, embedding state, writer and version
	// count each render as their own signal.
	for _, want := range []string{
		`data-status="in_progress"`,
		`>task<`,
		`#9f1c0a2e`,
		`>4 versions<`,
		`via agent MCP link`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("browse card missing %q", want)
		}
	}
	if strings.Index(html, "#9f1c0a2e") > strings.Index(html, ">Ship the objects browser table<") {
		t.Error("the object's type and id must sit above its name")
	}
	if strings.Contains(html, ">Embedded<") {
		t.Error("an embedded object must not earn a badge; only a not-embedded state is worth flagging")
	}
	if strings.Contains(html, `uppercase">Score<`) {
		t.Error("browse cards must not render the search-only score")
	}

	// Every preview form discards its submit instead of posting: the gallery has
	// no project behind it, and "Delete selected" must not navigate away.
	for _, id := range []string{
		"objects-search-form",
		"objects-filters-form",
	} {
		if block := formBlock(t, html, id); !strings.Contains(block, `onsubmit="return false"`) {
			t.Errorf("preview form %q must discard its submit", id)
		}
	}
	for _, id := range []string{
		"objects-bulk-delete-dialog",
		"objects-bulk-status-dialog",
		"objects-bulk-label-add-dialog",
		"objects-bulk-label-remove-dialog",
	} {
		if block := dialogBlock(t, html, id); !strings.Contains(block, `onsubmit="return false"`) {
			t.Errorf("preview dialog %q must discard its submit", id)
		}
	}

	// …and the live page keeps submitting for real.
	live := objectsBrowserSampleData("")
	live.Preview = false
	liveHTML := renderHTML(t, ObjectsPage(live))
	if strings.Contains(liveHTML, `onsubmit="return false"`) {
		t.Error("live page must not carry preview submit guards")
	}
}

// resultsBar returns the HTML of the objects results bar (select-all + sort),
// so sort assertions talk about the control rather than any hidden filter input
// that legitimately carries the same order elsewhere on the page.
func resultsBar(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, `data-testid="objects-results-bar"`)
	if start < 0 {
		t.Fatal("objects results bar not found")
	}
	end := strings.Index(html[start:], `id="objects-cards"`)
	if end < 0 {
		t.Fatal("objects card grid not found after the results bar")
	}
	return html[start : start+end]
}

// formBlock returns the HTML of one <form id=...> element.
func formBlock(t *testing.T, html, id string) string {
	t.Helper()
	start := strings.Index(html, `id="`+id+`"`)
	if start < 0 {
		t.Fatalf("form %q not found", id)
	}
	// Walk back to the opening tag, then forward to its close.
	open := strings.LastIndex(html[:start], "<form")
	if open < 0 {
		t.Fatalf("form %q has no opening tag", id)
	}
	end := strings.Index(html[open:], "</form>")
	if end < 0 {
		t.Fatalf("form %q is not closed", id)
	}
	return html[open : open+end]
}

// TestObjectsBrowserBulkDialogsValidateIndependently guards the regression that
// made every bulk action unusable: one shared form meant browser constraint
// validation inspected the closed dialogs too, so the empty required label and
// status fields blocked Delete.
func TestObjectsBrowserBulkDialogsValidateIndependently(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))

	deleteBlock := dialogBlock(t, html, "objects-bulk-delete-dialog")
	if strings.Contains(deleteBlock, "required") {
		t.Errorf("delete dialog must not carry a required control, got:\n%s", deleteBlock)
	}

	statusBlock := dialogBlock(t, html, "objects-bulk-status-dialog")
	if strings.Contains(statusBlock, `name="label"`) {
		t.Errorf("status dialog must not carry the label field, got:\n%s", statusBlock)
	}
	if !strings.Contains(statusBlock, `name="status"`) || !strings.Contains(statusBlock, "required") {
		t.Error("status dialog must require its own status select")
	}

	addBlock := dialogBlock(t, html, "objects-bulk-label-add-dialog")
	removeBlock := dialogBlock(t, html, "objects-bulk-label-remove-dialog")
	if strings.Contains(addBlock, `name="status"`) || strings.Contains(removeBlock, `name="status"`) {
		t.Error("label dialogs must not carry the status field")
	}
	for name, block := range map[string]string{"add": addBlock, "remove": removeBlock} {
		if !strings.Contains(block, `name="label"`) || !strings.Contains(block, "required") {
			t.Errorf("%s-label dialog must require its own label input", name)
		}
	}
}

// dialogBlock returns the HTML of one <dialog id=...> element, so assertions can
// talk about a single dialog's controls instead of the whole page.
func dialogBlock(t *testing.T, html, id string) string {
	t.Helper()
	start := strings.Index(html, `id="`+id+`"`)
	if start < 0 {
		t.Fatalf("dialog %q not found", id)
	}
	end := strings.Index(html[start:], "</dialog>")
	if end < 0 {
		t.Fatalf("dialog %q is not closed", id)
	}
	return html[start : start+end]
}

// TestObjectsBrowserPreviewIsInert pins the gallery preview contract: the
// sample render must not carry live pagination or mutation wiring, so clicking
// around the preview cannot reach a backend.
func TestObjectsBrowserPreviewIsInert(t *testing.T) {
	data := objectsBrowserSampleData("")
	html := renderHTML(t, ObjectsPage(data))

	if !data.Preview {
		t.Fatal("sample data must set Preview")
	}
	for _, unwanted := range []string{
		`hx-get=`,
		`hx-post=`,
		`action="/objects/bulk"`,
		`action="/objects"`,
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("preview must not carry live wiring %q", unwanted)
		}
	}

	// The same components keep their live wiring outside preview mode.
	live := objectsBrowserSampleData("")
	live.Preview = false
	liveHTML := renderHTML(t, ObjectsPage(live))
	for _, want := range []string{
		`hx-get="/objects/partial`,
		`hx-post="/objects/knowledge"`,
		`action="/objects/bulk"`,
	} {
		if !strings.Contains(liveHTML, want) {
			t.Errorf("live render missing %q", want)
		}
	}
}

// TestObjectsBrowserUnifiedDropsUnavailableBadges pins the mode-aware badge
// set: unified search hits carry no status, embedding, or created_at, so their
// cards drop those badges instead of rendering empty ones — and carry the
// relevance score instead.
func TestObjectsBrowserUnifiedDropsUnavailableBadges(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSearchSampleData()))

	if !strings.Contains(html, `title="Relevance score"`) {
		t.Error("search cards must render the relevance score")
	}
	if !strings.Contains(html, "0.94") {
		t.Error("search cards must render relevance scores")
	}
	for _, unwanted := range []string{`aria-sort=`, `data-status=`, `title="Embedding`, `>Created<`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("unified search cards must not render %q", unwanted)
		}
	}
	// Search mode states its order instead of offering one the result set ignores.
	if !strings.Contains(html, "Best match first") {
		t.Error("search results must state that they are relevance-ranked")
	}
}

// TestObjectsBrowserStatsStrip pins the collapsed overview: one stats strip with
// grouped digits instead of three stat cards, and the unavailable state as an
// em dash rather than a number.
func TestObjectsBrowserStatsStrip(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))
	if !strings.Contains(html, ">1,284<") {
		t.Errorf("stats strip must group digits, got:\n%s", html)
	}

	unavailable := objectsBrowserSampleData("")
	unavailable.Stats = &objectsStats{
		TotalErr: errTestUnavailable{},
		EmbedErr: errTestUnavailable{},
	}
	html = renderHTML(t, ObjectsPage(unavailable))
	if strings.Count(html, `>—</div>`) < 3 {
		t.Errorf("unavailable stats must render three em dashes, got:\n%s", html)
	}
	if strings.Contains(html, ">0<") {
		t.Error("unavailable stats must not render a numeric 0")
	}
}

// TestObjectsLayoutContracts pins #699.3: the responsive card grid and the stats
// strip are the two places where a class edit silently breaks the phone layout,
// and the claim ("four per row, two at sm, one below, nothing scrolls at 320px")
// otherwise rests on a manual browser check. ui-patterns §8 says a test should
// assert the contract, not a class string; this is the contract, and the markup
// is asserted to use these constants rather than a drifting literal.
func TestObjectsLayoutContracts(t *testing.T) {
	// The ramp is ordered and starts at one column. A `max-sm:grid-cols-*` is
	// the classic regression: it lets the phone grid grow past the viewport.
	ramp := []string{"grid-cols-1", "sm:grid-cols-2", "lg:grid-cols-3", "2xl:grid-cols-4"}
	prev := -1
	for _, token := range ramp {
		at := strings.Index(objectsCardsGridClass, token)
		if at < 0 {
			t.Fatalf("card grid ramp is missing %q: %q", token, objectsCardsGridClass)
		}
		if at < prev {
			t.Fatalf("card grid ramp breaks at %q — the columns must only grow: %q", token, objectsCardsGridClass)
		}
		prev = at
	}
	for _, field := range strings.Fields(objectsCardsGridClass) {
		if !strings.HasPrefix(field, "grid-cols-") {
			continue
		}
		// Every unprefixed column count must be the single-column base; a
		// wider base is what overflows a 320px viewport.
		if field != "grid-cols-1" {
			t.Errorf("card grid has a multi-column base %q; a phone must start at one column", field)
		}
	}
	if strings.Contains(objectsCardsGridClass, "max-sm:grid-cols-") {
		t.Errorf("card grid narrows on max-sm instead of widening on min-width breakpoints: %q", objectsCardsGridClass)
	}

	// The stats strip keeps its three figures inside the viewport: the group is
	// full width and each stat may shrink with a smaller title/value on phones.
	for _, want := range []string{
		"[&>.stats]:w-full",
		"max-sm:[&_.stat]:min-w-0",
	} {
		if !strings.Contains(objectsStatsStripClass, want) {
			t.Errorf("stats strip lost its narrow-viewport guard %q: %q", want, objectsStatsStripClass)
		}
	}

	// The page renders the constants, not a parallel literal.
	html := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))
	if !strings.Contains(html, objectsCardsGridClass) {
		t.Errorf("card grid markup does not use objectsCardsGridClass:\n%s", html)
	}
	if !strings.Contains(html, templAttrEscaped(objectsStatsStripClass)) {
		t.Errorf("stats strip markup does not use objectsStatsStripClass:\n%s", html)
	}

	// #699.1: the chip row is raised above the card's stretched link, or neither
	// tooltip can open — the pointer is on the overlay instead of the chip.
	chipsAt := strings.Index(html, `data-testid="object-card-chips"`)
	if chipsAt < 0 {
		t.Fatalf("card chip row is not identifiable:\n%s", html)
	}
	tag := html[:chipsAt]
	if open := strings.LastIndex(tag, "<div"); open >= 0 {
		tag = tag[open:]
	}
	if !strings.Contains(tag, "relative z-10") {
		t.Errorf("card chip row must stack above the stretched link to expose its tooltips, got tag %q", tag)
	}
}

// templAttrEscaped is templ's attribute escaping for the bits these constants
// contain. The stats class is carried in a templ.Attributes map, so its
// child-selector ampersands and angle brackets arrive escaped in the output.
func templAttrEscaped(v string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(v)
}

// errTestUnavailable is a stand-in for a failed stats source.
type errTestUnavailable struct{}

func (errTestUnavailable) Error() string { return "unavailable" }

// TestUIComponentPagePreview covers the dev-gallery page-preview route: served
// only with the gallery flag, 404 for unknown slugs, and the bodies render the
// expected page surfaces.
func TestUIComponentPagePreview(t *testing.T) {
	newServer := func(flag bool) *echo.Echo {
		s := &Server{cfg: Config{ComponentGallery: flag}, memory: &fakeMemory{}}
		e := echo.New()
		e.GET("/dev/components/pages/:slug", s.uiComponentPagePreview)
		return e
	}

	rec := httptest.NewRecorder()
	newServer(true).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components/pages/objects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("page preview = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="objects-cards"`,
		`/js/app.js`,
		`/css/app.css`,
		`id="objects-bulk-bar"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page preview missing %q", want)
		}
	}

	// The search variant renders the score instead of browse-only badges.
	rec = httptest.NewRecorder()
	newServer(true).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components/pages/objects-search", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("search page preview = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `title="Relevance score"`) {
		t.Error("search page preview must render the relevance score")
	}

	rec = httptest.NewRecorder()
	newServer(true).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components/pages/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown page preview = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	newServer(false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components/pages/objects", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("flag-off page preview = %d, want 404", rec.Code)
	}
}

// TestObjectsBrowserSortControl pins the sort contract for the one order the
// list endpoint supports: Created. With no table there is no column header to
// click, so the control lives in the results bar and always states the current
// order. The link is query-only so it works on every route and inside the
// gallery preview, and the active filters ride along so sorting never drops a
// narrowing.
func TestObjectsBrowserSortControl(t *testing.T) {
	data := objectsBrowserSampleData("")
	html := renderHTML(t, ObjectsPage(data))

	if !strings.Contains(html, "Newest first") {
		t.Error("the default browse order must render as Newest first")
	}
	if !strings.Contains(html, "lucide--arrow-down") {
		t.Error("newest-first must render a descending indicator")
	}
	if !strings.Contains(html, `aria-label="Sort: Newest first"`) {
		t.Error("the sort control must state the current order for assistive tech")
	}
	// A query-only href: it resolves against the current document, so the same
	// control works on /objects, the agent-scoped route, and the preview iframe.
	// One href carries the active filter and the toggled order together, so
	// sorting cannot silently widen the list.
	if !regexp.MustCompile(`href="\?[^"]*actor_type=agent[^"]*order=asc`).MatchString(resultsBar(t, html)) {
		t.Error("the sort link must carry the active actor filter and toggle to ascending in one href")
	}

	ascHTML := renderHTML(t, ObjectsPage(objectsBrowserSampleData("asc")))
	if !strings.Contains(ascHTML, "Oldest first") {
		t.Error("ascending order must render as Oldest first")
	}
	if !strings.Contains(ascHTML, "lucide--arrow-up") {
		t.Error("ascending order must render the up indicator")
	}
	// The sort link's own href, not the whole bar: the sort control is not the
	// only link in the results bar any more (the wiki view switch carries the
	// current order forward too).
	sortHref := func(html string) string {
		t.Helper()
		m := regexp.MustCompile(`href="([^"]*)"[^>]*aria-label="Sort: `).FindStringSubmatch(resultsBar(t, html))
		if m == nil {
			t.Fatal("sort link not found in the results bar")
		}
		return m[1]
	}
	if strings.Contains(sortHref(ascHTML), "order=asc") {
		t.Error("ascending order must link back to the default (newest-first) order")
	}

	// Toggling keeps the active filters, so a sorted view stays narrowed.
	filtered := objectsBrowserSampleData("")
	filtered.TypeFilter = "task"
	filtered.BranchID = "b-objs"
	href := objectsSortHref(filtered, objectsNextSortDir(filtered))
	if !strings.HasPrefix(href, "?") {
		t.Errorf("sort href must be query-only, got %q", href)
	}
	for _, want := range []string{"order=asc", "type=task", "branch=b-objs"} {
		if !strings.Contains(href, want) {
			t.Errorf("sort href dropped %q: %q", want, href)
		}
	}
}

// TestObjectsBrowserSampleDataHonoursOrder keeps the gallery preview honest: the
// sample rows re-sort from the query string, so the sort header is reviewable
// without a backend behind the iframe.
func TestObjectsBrowserSampleDataHonoursOrder(t *testing.T) {
	desc := objectsBrowserSampleData("")
	asc := objectsBrowserSampleData("asc")

	if len(desc.Objects) < 2 || len(asc.Objects) != len(desc.Objects) {
		t.Fatalf("sample data must hold a stable page of rows (desc=%d asc=%d)", len(desc.Objects), len(asc.Objects))
	}
	if desc.Order != "" || asc.Order != "asc" {
		t.Fatalf("sample order must round-trip (desc=%q asc=%q)", desc.Order, asc.Order)
	}
	if desc.Objects[0].ID == asc.Objects[0].ID {
		t.Error("ascending order must not start with the same row as the default")
	}
	// RFC3339 with a shared offset compares lexicographically.
	if desc.Objects[0].CreatedAt < desc.Objects[1].CreatedAt {
		t.Error("default sample order must be newest first")
	}
	if asc.Objects[0].CreatedAt > asc.Objects[1].CreatedAt {
		t.Error("ascending sample order must be oldest first")
	}
}

// TestObjectsFiltersApplyInPlace pins the filter interaction: every control
// applies itself — so no Apply button is needed (or left behind as the only way
// to submit) — and applies it by refreshing the results region, not by loading
// the document.
//
// The regression this guards: a control that calls form.submit() never fires a
// `submit` event, so htmx's boosted form never sees it and the browser performs
// a full page load. The form's own hx-get is what keeps the page (header, stats,
// controls) in place while hx-push-url keeps the address bar truthful.
func TestObjectsFiltersApplyInPlace(t *testing.T) {
	live := objectsBrowserSampleData("asc")
	live.Preview = false
	html := renderHTML(t, ObjectsPage(live))
	form := formBlock(t, html, "objects-filters-form")

	// No Apply button: the only submit controls are the actor menu's options,
	// which carry the value they set.
	if strings.Contains(strings.Replace(form, actorFilterBlock(t, html), "", 1), `type="submit"`) {
		t.Error("filter form must not carry a submit button; every control applies itself")
	}
	// The actor picker's options are the one submit path, and each carries the
	// whole pair as its value.
	if !strings.Contains(form, `name="actor" value="user:u-1"`) {
		t.Error("the actor picker must submit its actor as one value")
	}
	for _, want := range []string{
		`hx-get="/objects"`,
		`hx-target="#objects-results"`,
		`hx-swap="outerHTML"`,
		`hx-push-url="true"`,
		`hx-trigger="change, submit"`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("filter form missing %q:\n%s", want, form)
		}
	}
	if strings.Contains(form, `this.form.submit()`) {
		t.Error("a filter control must not call form.submit(); it bypasses htmx and reloads the page")
	}
	// The active actor travels as hidden inputs, so narrowing by branch, type, or
	// provenance cannot silently drop it (#131).
	for _, want := range []string{`name="actor_type" value="agent"`, `name="actor_id" value="a-1"`} {
		if !strings.Contains(form, want) {
			t.Errorf("filter form missing the active actor (%q):\n%s", want, form)
		}
	}
	if !strings.Contains(form, `name="order" value="asc"`) {
		t.Error("filter form must carry the active sort so narrowing keeps it")
	}

	// The gallery preview keeps every control live but must not navigate its
	// iframe: no request wiring at all, and the inert submit guard stays.
	previewForm := formBlock(t, renderHTML(t, ObjectsPage(objectsBrowserSampleData("asc"))), "objects-filters-form")
	if strings.Contains(previewForm, "hx-get=") || strings.Contains(previewForm, "hx-target=") {
		t.Error("preview filters must not carry request wiring; a request would navigate the iframe")
	}
	if !strings.Contains(previewForm, `onsubmit="return false"`) {
		t.Error("preview filters must keep the inert submit guard")
	}
}

// TestObjectsBrowserFilterControlsAreLabelled pins the filter row's shape: every
// control is named by its own legend instead of a shared "Filter" caption, so
// the bar explains which control narrows what.
func TestObjectsBrowserFilterControlsAreLabelled(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))
	form := formBlock(t, html, "objects-filters-form")

	for _, legend := range []string{"Branch", "Type", "Provenance", "Actor"} {
		if want := `<legend class="fieldset-legend">` + legend + `</legend>`; !strings.Contains(form, want) {
			t.Errorf("filter row missing the %q label (%s)", legend, want)
		}
	}
	if strings.Contains(form, ">Filter<") {
		t.Error("filter row must not carry a shared Filter caption once every control is labelled")
	}
	// The label sits above the control it names.
	labelAt := strings.Index(form, `<legend class="fieldset-legend">Branch</legend>`)
	selectAt := strings.Index(form, `name="branch"`)
	if labelAt < 0 || selectAt < 0 || labelAt > selectAt {
		t.Error("the Branch label must precede the branch control")
	}
}

// TestObjectsActorFilterListsIdentities pins the actor picker: real identities
// grouped under Users/Agents with their avatars, "Any actor" set apart by a
// divider, and every row a submit button carrying the whole (type, id) pair.
func TestObjectsActorFilterListsIdentities(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))
	picker := actorFilterBlock(t, html)

	// The closed control shows the active actor's identity, not just a key.
	if !strings.Contains(picker, "memory-agent") {
		t.Error("the actor control must show the active actor's label")
	}
	if !strings.Contains(picker, "lucide--database") {
		t.Error("an agent's avatar is its declared glyph")
	}

	// Every option submits the pair as one value.
	for _, want := range []string{
		`name="actor" value="agent:a-1"`,
		`name="actor" value="user:u-1"`,
		// "Any actor" submits the sentinel rather than an empty value: the form
		// always carries the active actor as hidden inputs (#131), so an empty
		// submit value could not be told apart from that pair.
		`name="actor" value="clear"`,
	} {
		if !strings.Contains(picker, want) {
			t.Errorf("actor picker missing option %q", want)
		}
	}
	// Groups are announced as groups (not just visual captions), and a separator
	// marks where "Any actor" ends and the identities begin.
	for _, want := range []string{
		`role="group" aria-label="Users"`,
		`role="group" aria-label="Agents"`,
		`role="separator"`,
	} {
		if !strings.Contains(picker, want) {
			t.Errorf("actor picker missing %q", want)
		}
	}
	// Members are people: they get an avatar (initials here), not a glyph. Both
	// marks are decorative — the row's label already names the actor.
	for _, want := range []string{
		`data-actor-avatar="user"`,
		`data-actor-avatar="agent"`,
	} {
		if !strings.Contains(picker, want) {
			t.Errorf("actor picker missing the %q mark", want)
		}
	}
	// Attribute order is not the contract; the pairing is.
	decorativeUser := regexp.MustCompile(`<[^>]*data-actor-avatar="user"[^>]*aria-hidden="true"|<[^>]*aria-hidden="true"[^>]*data-actor-avatar="user"`)
	if !decorativeUser.MatchString(picker) {
		t.Error("the member avatar must be decorative: its label already names the actor")
	}
	if !strings.Contains(picker, `aria-checked="true"`) {
		t.Error("the active actor must render as checked")
	}

	// An actor that is not in the loaded lists still shows as the active filter.
	unknown := objectsBrowserSampleData("")
	unknown.ActorOptions = nil
	if got := renderHTML(t, ObjectsPage(unknown)); !strings.Contains(got, "Agent · a-1") {
		t.Error("an unknown active actor must still render its type and short id")
	}
}

// actorFilterBlock returns the HTML of the actor picker (its dropdown root).
func actorFilterBlock(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, `data-testid="objects-actor-filter"`)
	if start < 0 {
		t.Fatal("actor picker not found")
	}
	open := strings.LastIndex(html[:start], "<div")
	if open < 0 {
		t.Fatal("actor picker has no opening tag")
	}
	end := strings.Index(html[open:], `</ul>`)
	if end < 0 {
		t.Fatal("actor picker menu is not closed")
	}
	return html[open : open+end]
}

// TestObjectShortID pins the id fragment above an object name: a UUID shows its
// first segment instead of a flat cut that ends mid-segment with a hyphen.
func TestObjectShortID(t *testing.T) {
	for in, want := range map[string]string{
		"9f1c0a2e-6b41-4c8d-9a2f-0e7d3b5a1c22": "9f1c0a2e",
		"c8e2d5a1-4b96-4f30-a7d8-9e1c2b6f4a03": "c8e2d5a1",
		"short":                                "short",
		"project-atlas-2026":                   "project-at",
	} {
		if got := objectShortID(in); got != want {
			t.Errorf("objectShortID(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestObjectsBrowserSearchResultsAreRelevanceRanked pins the mode distinction:
// search hits are relevance-ranked, so ordering by created_at is meaningless
// there and the results bar must not offer a sort the result set cannot honour.
func TestObjectsBrowserSearchResultsAreRelevanceRanked(t *testing.T) {
	data := objectsBrowserSampleData("")
	data.Query = "acme"
	data.Mode = "hybrid"
	for _, o := range data.Objects {
		data.Results = append(data.Results, ObjectSearchResult{Score: 0.5, Object: o})
	}
	data.Objects = nil

	html := renderHTML(t, ObjectsPage(data))
	if strings.Contains(html, "aria-sort") {
		t.Error("search results must not render a sortable header")
	}
	if strings.Contains(html, "Newest first") || strings.Contains(html, "Oldest first") {
		t.Error("search results must not offer a created_at order")
	}
	if !strings.Contains(html, "Best match first") {
		t.Error("search results must state that they are relevance-ranked")
	}
	if !strings.Contains(html, `title="Relevance score"`) {
		t.Error("search cards must render the relevance score")
	}
}

// TestObjectsPartialURLKeepsSort pins pagination: "Load more" must fetch the next
// page in the same order, not silently fall back to newest-first.
func TestObjectsPartialURLKeepsSort(t *testing.T) {
	if got := objectsPartialURL(objectsBrowserSampleData("asc")); !strings.Contains(got, "order=asc") {
		t.Errorf("partial URL must carry the sort, got %q", got)
	}
	if got := objectsPartialURL(objectsBrowserSampleData("")); strings.Contains(got, "order=") {
		t.Errorf("partial URL must not carry the default order, got %q", got)
	}
}

// TestObjectsPageAgentScopeOmitsProjectSurfaces pins the agent-scoped view: it
// reuses the same table and filters, but the project-wide surfaces (global
// search, the stats strip, the Q&A dialog) are absent because the server's
// search endpoints cannot be provenance-scoped to one agent.
func TestObjectsPageAgentScopeOmitsProjectSurfaces(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		AgentID:   "a1",
		AgentName: "diane",
		Objects:   []GraphObject{{ID: "o1", Type: "person", Key: "Sam"}},
		Types:     []string{"person"},
		Stats:     &objectsStats{TotalObjects: 3},
	}))

	for _, unwanted := range []string{
		`id="objects-search-form"`,
		`id="objects-stats"`,
		`id="objects-knowledge-answer"`,
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("agent-scoped view must omit %q", unwanted)
		}
	}
	for _, want := range []string{
		`id="objects-filters-form"`,
		`data-testid="objects-cards"`,
		`href="/agents/a1"`,
		`name="actor_type"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("agent-scoped view missing %q", want)
		}
	}
}

// TestObjectsPageBulkFormsCarryReturnPath pins the redirect target each bulk
// dialog posts: the filtered page the selection was made on, so a bulk action
// reloads the same view instead of resetting to the unfiltered list.
func TestObjectsPageBulkFormsCarryReturnPath(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{
		TypeFilter: "task",
		Order:      "asc",
		BranchID:   "b-objs",
		Objects:    []GraphObject{{ID: "o1", Type: "task", Key: "One"}},
		Types:      []string{"task"},
	}))

	if got := strings.Count(html, `name="back"`); got != 4 {
		t.Fatalf("back inputs = %d, want one per bulk dialog (4)", got)
	}
	if !strings.Contains(html, `value="/objects?branch=b-objs&amp;order=asc&amp;type=task"`) {
		t.Errorf("back input must carry the current filters, got:\n%s", html)
	}
	// Delete resolves the object on the page's branch; without this field it
	// would tombstone the main head and leave the object in the branch view. The
	// filter forms carry a `branch` hidden input too, so assert inside each dialog.
	for _, id := range []string{
		"objects-bulk-label-add-dialog",
		"objects-bulk-label-remove-dialog",
		"objects-bulk-status-dialog",
		"objects-bulk-delete-dialog",
	} {
		if block := dialogBlock(t, html, id); !strings.Contains(block, `name="branch" value="b-objs"`) {
			t.Errorf("%s must carry the page's branch", id)
		}
	}
}

// TestObjectsBrowserRowsPartialAppendsToTheGrid pins the HTMX fragment: one grid
// at every width means one append target, plus an out-of-band replacement of the
// load-more slot. The old fragment had to duplicate the page into a mobile list
// that was hidden on desktop.
func TestObjectsBrowserRowsPartialAppendsToTheGrid(t *testing.T) {
	data := objectsBrowserSampleData("")
	data.Preview = false
	html := renderHTML(t, objectsBrowserRowsPartial(data))

	for _, want := range []string{
		`data-testid="object-card"`,
		`id="objects-load-more" hx-swap-oob="true"`,
		`hx-get="/objects/partial`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rows partial missing %q", want)
		}
	}
	for _, unwanted := range []string{`<tr`, `objects-mobile-list`, `objects-mobile-row`, `objects-list`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("rows partial must not carry the retired table/mobile markup %q", unwanted)
		}
	}
}

// TestObjectsBrowserCardsAreSelectable pins the selection surface on the card
// itself (#2237): each card carries a checkbox that feeds the shared Alpine `sel`
// model, and the checkbox is a SIBLING of the card anchor (interactive content
// nested in a link is invalid and would navigate instead of selecting).
func TestObjectsBrowserCardsAreSelectable(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))

	gridAt := strings.Index(html, `data-testid="objects-cards"`)
	if gridAt < 0 {
		t.Fatal("objects card grid missing")
	}
	end := strings.Index(html[gridAt:], `id="objects-load-more"`)
	if end < 0 {
		end = len(html) - gridAt
	}
	grid := html[gridAt : gridAt+end]

	if got, want := strings.Count(grid, `data-testid="object-card"`), len(objectsBrowserSampleData("").Objects); got != want {
		t.Errorf("grid cards = %d, want one per object (%d)", got, want)
	}
	if !strings.Contains(grid, `data-object-select`) || !strings.Contains(grid, `x-model="sel"`) {
		t.Error("cards must feed the shared Alpine selection model")
	}
	// The checkbox lives in the card header, before the anchor that carries the
	// title: no anchor may wrap a form control.
	card := strings.Index(grid, `data-testid="object-card"`)
	checkbox := strings.Index(grid[card:], `data-object-select`)
	labelClose := strings.Index(grid[card:], `</label>`)
	anchor := strings.Index(grid[card:], `<a `)
	if card < 0 || checkbox < 0 || labelClose < 0 || anchor < 0 {
		t.Fatalf("unexpected card structure (card=%d checkbox=%d labelClose=%d anchor=%d)", card, checkbox, labelClose, anchor)
	}
	if checkbox > labelClose {
		t.Error("card checkbox must live inside its own label")
	}
	if anchor < labelClose {
		t.Error("the card anchor must not wrap the selection checkbox")
	}
	if !strings.Contains(grid, `href="/objects/`) {
		t.Error("card anchor missing (the card must open the object)")
	}
}

// TestObjectsBulkStatusDialogUsesPerTypeOptions pins #2242: the bulk Set-status
// dialog derives its options from the selected types (Alpine) instead of always
// offering the canonical board order, and is disabled when the selection has no
// common workflow status.
func TestObjectsBulkStatusDialogUsesPerTypeOptions(t *testing.T) {
	data := objectsBrowserSampleData("")
	data.StatusOptionsJSON = `{"Task":["ready","done"],"Note":["draft"]}`
	html := renderHTML(t, ObjectsPage(data))

	for _, want := range []string{
		`data-status-options=`,
		`data-object-type=`,
		`x-for="s in statusOptions"`,
		`:disabled="!statusActionable"`,
		`data-testid="objects-bulk-status-unavailable"`,
		`Options are the statuses every selected type declares.`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("bulk status dialog missing %q", want)
		}
	}
	// The old static board-order option list is gone from the status dialog.
	if strings.Contains(html, `<option value="in_progress">`) {
		t.Error("status dialog still renders the static canonical board order")
	}
}

// TestObjectsBrowserWikiViewSwitch pins the browse-side variant switch: the card
// grid opens an object's wiki in the chosen variant, the switch carries that
// choice into the query (so a shared or reloaded URL keeps it), and it survives
// the active filters.
func TestObjectsBrowserWikiViewSwitch(t *testing.T) {
	// The default variant is the article, and the default card link stays short:
	// no view parameter when nothing but the article is meant.
	data := objectsBrowserSampleData("")
	data.Preview = false
	html := renderHTML(t, ObjectsPage(data))
	bar := resultsBar(t, html)
	if !strings.Contains(bar, `data-testid="objects-wiki-view-article"`) ||
		!strings.Contains(bar, `data-testid="objects-wiki-view-page"`) {
		t.Fatal("the results bar must offer both wiki variants")
	}
	if !strings.Contains(bar, `aria-current="page"`) {
		t.Error("the active variant must be marked for assistive tech")
	}
	if !regexp.MustCompile(`href="\?[^"]*wiki_view=page`).MatchString(bar) {
		t.Error("the switch must link to the object-page variant as a query-only href")
	}
	// A card opens the wiki, and only a non-default variant is spelled out.
	if !regexp.MustCompile(`href="/objects/[^"]+/wiki"`).MatchString(html) {
		t.Error("a card must link into the object's wiki")
	}
	if strings.Contains(html, "/wiki?view=article") {
		t.Error("the default article variant must not be spelled into card URLs")
	}

	// Chosen object-page variant: the cards carry it, so opening one does not
	// fall back to the article.
	page := objectsBrowserSampleData("")
	page.Preview = false
	page.WikiView = objectWikiViewPage
	page.TypeFilter = "task"
	pageHTML := renderHTML(t, ObjectsPage(page))
	if !regexp.MustCompile(`href="/objects/[^"]+/wiki\?view=page"`).MatchString(pageHTML) {
		t.Error("cards must open the wiki in the variant the reader chose")
	}
	pageBar := resultsBar(t, pageHTML)
	if !regexp.MustCompile(`href="\?[^"]*wiki_view=article`).MatchString(pageBar) {
		t.Error("the switch back to the article must be offered once page is active")
	}
	// Switching the variant keeps the active filter, so it cannot widen the list.
	if !strings.Contains(pageBar, "wiki_view=article") || !strings.Contains(pageBar, "type=task") {
		t.Errorf("the switch href dropped the active filter: %s", pageBar)
	}

	// The gallery preview stays inert: its cards and switch cannot navigate the
	// preview iframe away from the sample page.
	preview := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))
	if strings.Contains(preview, "/wiki") {
		t.Error("a preview card must not link to a real object")
	}
}
