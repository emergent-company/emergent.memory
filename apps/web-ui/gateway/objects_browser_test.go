package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestObjectsBrowserTableRendersSelectionSurface pins the table contract the
// bulk actions depend on: one row checkbox per object carrying pure Alpine
// selection state, and four self-contained dialog forms that each submit the
// selection once.
func TestObjectsBrowserTableRendersSelectionSurface(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))

	for _, want := range []string{
		`data-testid="objects-table"`,
		`id="objects-list"`,
		`name="ids"`,
		`toggleAll`,
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

	// Selection is Alpine-only. Row checkboxes carry no name/form, so a row can
	// never submit itself and `ids` cannot arrive twice (once per checked row,
	// once as the CSV the handler expects).
	if strings.Contains(html, `form="objects-bulk-form"`) || strings.Contains(html, `id="objects-bulk-form"`) {
		t.Error("bulk submits must come from per-dialog forms, not one shared form")
	}
	if strings.Contains(html, `<input type="checkbox" class="checkbox checkbox-sm" name=`) {
		t.Error("row checkboxes must not be submittable inputs")
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

	// Browse rows keep the status/embedding signals. The type is not a column:
	// it rides above the object id in the Object cell.
	for _, want := range []string{
		`data-status="in_progress"`,
		`uppercase">Status<`,
		`uppercase">Embedding<`,
		`>Created</span>`,
		`>task<`,
		`#9f1c0a2e`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("browse table missing %q", want)
		}
	}
	if strings.Contains(html, `uppercase">Type<`) {
		t.Error("browse table must not render a Type column; the type belongs in the Object cell")
	}
	if strings.Index(html, "#9f1c0a2e") > strings.Index(html, ">Ship the objects browser table</a>") {
		t.Error("the object's type and id must sit above its name")
	}
	if strings.Contains(html, `uppercase">Score<`) {
		t.Error("browse table must not render the search-only Score column")
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

// TestObjectsBrowserUnifiedDropsUnavailableColumns pins the mode-aware column
// set: unified search hits carry no status, embedding, or created_at, so the
// table drops those columns instead of rendering a column of dashes.
func TestObjectsBrowserUnifiedDropsUnavailableColumns(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSearchSampleData()))

	if !strings.Contains(html, `uppercase">Score<`) {
		t.Error("search table must render the Score column")
	}
	if !strings.Contains(html, "0.94") {
		t.Error("search table must render relevance scores")
	}
	for _, unwanted := range []string{`uppercase">Status<`, `uppercase">Embedding<`, `>Created</span>`, `aria-sort=`, `data-status=`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("unified table must not render %q", unwanted)
		}
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
		`data-testid="objects-table"`,
		`/js/app.js`,
		`/css/app.css`,
		`id="objects-bulk-bar"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page preview missing %q", want)
		}
	}

	// The search variant renders the reduced column set.
	rec = httptest.NewRecorder()
	newServer(true).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components/pages/objects-search", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("search page preview = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `uppercase">Score<`) {
		t.Error("search page preview must render the Score column")
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

// TestObjectsBrowserSortHeader pins the sort contract for the one column the
// list endpoint can order: Created. The header always states its sort
// (aria-sort), the indicator leads the label because the column is
// right-aligned, the link is query-only so it works on every route and inside
// the gallery preview, and the active filters ride along so sorting never drops
// a narrowing.
func TestObjectsBrowserSortHeader(t *testing.T) {
	data := objectsBrowserSampleData("")
	html := renderHTML(t, ObjectsPage(data))

	if !strings.Contains(html, `aria-sort="descending"`) {
		t.Error("the default browse order is newest-first, so Created must render aria-sort=descending")
	}
	// A query-only href: it resolves against the current document, so the same
	// header works on /objects, the agent-scoped route, and the preview iframe.
	// One href carries the active filter and the toggled order together, so
	// sorting cannot silently widen the list.
	if !regexp.MustCompile(`href="\?[^"]*actor_type=agent[^"]*order=asc`).MatchString(html) {
		t.Error("the sort link must carry the active actor filter and toggle to ascending in one href")
	}
	if !strings.Contains(html, "actor_type=agent") {
		t.Error("the sort link must carry the active actor filter so sorting never widens the list")
	}

	// Right-aligned column: the indicator leads the label, not the other way round.
	iconAt := strings.Index(html, "lucide--chevron-down")
	labelAt := strings.Index(html, ">Created</span>")
	if iconAt < 0 || labelAt < 0 {
		t.Fatalf("expected a descending indicator and the Created label (icon=%d label=%d)", iconAt, labelAt)
	}
	if iconAt > labelAt {
		t.Error("indicator must precede the label on a right-aligned column")
	}

	ascHTML := renderHTML(t, ObjectsPage(objectsBrowserSampleData("asc")))
	if !strings.Contains(ascHTML, `aria-sort="ascending"`) {
		t.Error("ascending order must render aria-sort=ascending")
	}
	if !strings.Contains(ascHTML, "lucide--chevron-up") {
		t.Error("ascending order must render the up indicator")
	}
	if strings.Contains(ascHTML, "?order=asc") {
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

// TestObjectsBrowserSearchHeaderIsNotSortable pins the mode distinction: search
// hits are relevance-ranked, so ordering by created_at is meaningless there and
// the column must not advertise a sort it cannot honour.
func TestObjectsBrowserSearchHeaderIsNotSortable(t *testing.T) {
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
	if !strings.Contains(html, `>Created</th>`) {
		t.Error("search results must still show the Created column")
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
		`id="objects-table"`,
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

// TestObjectsBrowserRowsPartialAppendsBothLists pins the HTMX fragment: the
// table is hidden on small screens, so a page of results has to append table
// rows AND mobile cards, or a phone's "Load more" would load invisible rows.
func TestObjectsBrowserRowsPartialAppendsBothLists(t *testing.T) {
	data := objectsBrowserSampleData("")
	data.Preview = false
	html := renderHTML(t, objectsBrowserRowsPartial(data))

	for _, want := range []string{
		`<tr`,
		`id="objects-mobile-list" hx-swap-oob="beforeend"`,
		`id="objects-load-more" hx-swap-oob="true"`,
		`hx-get="/objects/partial`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rows partial missing %q", want)
		}
	}
}

// TestObjectsBrowserMobileRowsAreSelectable pins the mobile counterpart of the
// table selection surface (#2237): each mobile card carries a checkbox that
// feeds the same Alpine `sel` model, and the checkbox is a SIBLING of the card
// anchor (interactive content nested in a link is invalid and would navigate).
func TestObjectsBrowserMobileRowsAreSelectable(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsBrowserSampleData("")))

	start := strings.Index(html, `id="objects-mobile-list"`)
	if start < 0 {
		t.Fatal("mobile object list missing")
	}
	end := strings.Index(html[start:], `id="objects-load-more"`)
	if end < 0 {
		end = len(html) - start
	}
	mobile := html[start : start+end]

	if !strings.Contains(mobile, `data-testid="objects-mobile-row"`) {
		t.Error("mobile rows missing their row marker")
	}
	if !strings.Contains(mobile, `data-object-select`) || !strings.Contains(mobile, `x-model="sel"`) {
		t.Error("mobile rows must feed the shared Alpine selection model")
	}
	// The checkbox must come before the card anchor closes over the row: check
	// the row's label/input precedes the anchor and that no anchor wraps it.
	row := strings.Index(mobile, `data-testid="objects-mobile-row"`)
	checkbox := strings.Index(mobile[row:], `data-object-select`)
	labelClose := strings.Index(mobile[row:], `</label>`)
	anchor := strings.Index(mobile[row+labelClose:], `<a `)
	if row < 0 || checkbox < 0 || labelClose < 0 || anchor < 0 {
		t.Fatalf("unexpected mobile row structure (row=%d checkbox=%d labelClose=%d anchor=%d)", row, checkbox, labelClose, anchor)
	}
	if checkbox > labelClose {
		t.Error("mobile checkbox must live inside its own label, before the card anchor")
	}
	if !strings.Contains(mobile, `href="/objects/`) {
		t.Error("mobile card anchor missing (tap must still open the object)")
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
