package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- card excerpts -----------------------------------------------------------

func TestObjectExcerptPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"summary wins", map[string]any{"summary": "Short", "description": "Long"}, "Short"},
		{"description before excerpt", map[string]any{"description": "Desc", "excerpt": "Exc"}, "Desc"},
		{"excerpt before abstract", map[string]any{"excerpt": "Exc", "abstract": "Abs"}, "Exc"},
		{"abstract before content", map[string]any{"abstract": "Abs", "content": "Body"}, "Abs"},
		{"content before body", map[string]any{"content": "Body", "body": "B"}, "Body"},
		{"body before notes", map[string]any{"body": "Body", "notes": "N"}, "Body"},
		{"notes last", map[string]any{"notes": "Noted"}, "Noted"},
		{"case-insensitive key", map[string]any{"Summary": "Title case"}, "Title case"},
		{"non-string summary is skipped", map[string]any{"summary": []any{"a"}, "description": "Desc"}, "Desc"},
		{"no prose", map[string]any{"count": 3}, ""},
		{"empty map", map[string]any{}, ""},
		{"blank prose falls through", map[string]any{"summary": "  \n ", "description": "Desc"}, "Desc"},
		// Markdown flattens to the first paragraph's words: headings, list
		// bullets, emphasis, images and link targets all disappear.
		{"markdown flattened", map[string]any{
			"content": "# Title\n\n## Heading\n\n**Bold** text with a [link](https://example.com) and `code`.\nStill the same paragraph.\n\nSecond paragraph is dropped.",
		}, "Bold text with a link and code. Still the same paragraph."},
		{"images dropped", map[string]any{"description": "![diagram](https://x/y.png) After the image."}, "After the image."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := objectExcerpt(c.props); got != c.want {
				t.Errorf("objectExcerpt(%v) = %q, want %q", c.props, got, c.want)
			}
		})
	}
}

// TestObjectExcerptScalarProse pins #699.5: a prose field posted as valid JSON
// becomes its typed value (parseFormJSON), so a description of 2024 arrives as a
// float64. The resolver must coerce scalars to the text the author typed rather
// than treating the field as absent and rendering an empty wiki.
func TestObjectExcerptScalarProse(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"integer", map[string]any{"description": float64(2024)}, "2024"},
		// json.Number is the forward-compatible arm: today's decode path yields
		// float64, so this pins the literal-preserving branch itself.
		{"decimal keeps its literal", map[string]any{"description": json.Number("1.50")}, "1.50"},
		{"boolean", map[string]any{"description": true}, "true"},
		{"array is still not prose", map[string]any{"description": []any{"2024"}}, ""},
		{"object is still not prose", map[string]any{"description": map[string]any{"text": "2024"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := objectExcerpt(c.props); got != c.want {
				t.Errorf("objectExcerpt(%v) = %q, want %q", c.props, got, c.want)
			}
		})
	}

	prose := objectArticleFor(GraphObject{Properties: map[string]any{"description": float64(2024)}})
	if prose.Lead != "2024" {
		t.Errorf("numeric-looking description must become the article lead, got lead=%q body=%q", prose.Lead, prose.BodyHTML)
	}
}

func TestObjectExcerptIsWordBoundTruncated(t *testing.T) {
	long := strings.Repeat("word ", 100)
	got := objectExcerpt(map[string]any{"summary": long})
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("long excerpt must end with an ellipsis, got %q", got)
	}
	if strings.Contains(got, "wor…") {
		t.Errorf("excerpt must cut at a word boundary, got %q", got)
	}
	if len([]rune(got)) > objectExcerptMaxRunes+1 {
		t.Errorf("excerpt = %d runes, want at most %d plus the ellipsis", len([]rune(got)), objectExcerptMaxRunes)
	}
}

// TestObjectExcerptCardRenders pins the card's use of the resolver: the excerpt
// appears in the clamp, and an object with no prose renders the card without a
// fabricated empty line.
func TestObjectExcerptCardRenders(t *testing.T) {
	html := renderHTML(t, ObjectsPage(objectsPageData{Objects: []GraphObject{
		{ID: "o1", Type: "note", Key: "With prose", Properties: map[string]any{"summary": "A one-line summary."}},
		{ID: "o2", Type: "note", Key: "Without prose"},
	}}))

	if !strings.Contains(html, "A one-line summary.") {
		t.Error("card must render the resolved excerpt")
	}
	if strings.Count(html, `line-clamp-3`) != 1 {
		t.Errorf("excerpt clamp = %d, want only the object with prose to carry one", strings.Count(html, "line-clamp-3"))
	}
}

// --- article prose -----------------------------------------------------------

func TestObjectArticleLeadAndBodySplit(t *testing.T) {
	t.Run("summary leads, content bodies", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"summary": "The lead sentence.",
			"content": "## History\n\nSigned in 1901.\n\n## Notes\n\nFiled under land.",
		}})
		if prose.Lead != "The lead sentence." {
			t.Errorf("lead = %q", prose.Lead)
		}
		if prose.LeadKey != "summary" || prose.BodyKey != "content" {
			t.Errorf("consumed keys = %q/%q", prose.LeadKey, prose.BodyKey)
		}
		if !strings.Contains(prose.BodyHTML, "<h2 id=\"history\">History</h2>") {
			t.Errorf("body must render markdown headings with ids: %q", prose.BodyHTML)
		}
		if len(prose.Headings) != 2 || prose.Headings[0].Text != "History" || prose.Headings[1].Text != "Notes" {
			t.Errorf("headings = %+v", prose.Headings)
		}
	})

	t.Run("summary leads, description bodies", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"summary":     "The short version.",
			"description": "The long version.\n\n## Detail\n\nMore of it.",
		}})
		if prose.Lead != "The short version." {
			t.Errorf("lead = %q", prose.Lead)
		}
		if prose.LeadKey != "summary" || prose.BodyKey != "description" {
			t.Errorf("consumed keys = %q/%q", prose.LeadKey, prose.BodyKey)
		}
		if !strings.Contains(prose.BodyHTML, "More of it.") {
			t.Errorf("description must render as the body: %q", prose.BodyHTML)
		}
	})

	t.Run("body only promotes its first paragraph", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"content": "The opening paragraph.\n\n## Details\n\nMore text.",
		}})
		if prose.Lead != "The opening paragraph." {
			t.Errorf("lead = %q", prose.Lead)
		}
		if strings.Contains(prose.BodyHTML, "The opening paragraph.") {
			t.Error("the promoted lead must be removed from the body, not duplicated")
		}
		if !strings.Contains(prose.BodyHTML, "More text.") {
			t.Errorf("body must keep the rest: %q", prose.BodyHTML)
		}
		if len(prose.Headings) != 1 || prose.Headings[0].ID != "details" {
			t.Errorf("headings = %+v", prose.Headings)
		}
	})

	t.Run("single-paragraph body has no body left", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{"content": "Only one paragraph."}})
		if prose.Lead != "Only one paragraph." {
			t.Errorf("lead = %q", prose.Lead)
		}
		if prose.BodyHTML != "" {
			t.Errorf("body must be empty when the whole body became the lead, got %q", prose.BodyHTML)
		}
	})

	t.Run("a lead field and body field repeating it do not duplicate", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"summary": "Same opening.",
			"content": "Same opening.\n\n## Rest\n\nAfter.",
		}})
		if prose.Lead != "Same opening." {
			t.Errorf("lead = %q", prose.Lead)
		}
		if strings.Contains(prose.BodyHTML, "Same opening.") {
			t.Error("the body must not repeat the lead's paragraph")
		}
	})

	t.Run("lead-only prose keeps its second paragraph", func(t *testing.T) {
		// The reported bug: an object whose only prose key is `summary` kept P1 as
		// the lead and dropped P2 entirely, because no body key resolved and the
		// lead property was never split.
		for _, key := range []string{"summary", "excerpt", "abstract", "description"} {
			prose := objectArticleFor(GraphObject{Properties: map[string]any{
				key: "The opening paragraph.\n\n## Detail\n\nMore text.",
			}})
			if prose.Lead != "The opening paragraph." {
				t.Errorf("%s: lead = %q", key, prose.Lead)
			}
			if !strings.Contains(prose.BodyHTML, "More text.") {
				t.Errorf("%s: the lead property's remaining paragraphs must become the body, got %q", key, prose.BodyHTML)
			}
			if strings.Contains(prose.BodyHTML, "The opening paragraph.") {
				t.Errorf("%s: the promoted lead must not repeat in the body: %q", key, prose.BodyHTML)
			}
			if len(prose.Headings) != 1 || prose.Headings[0].ID != "detail" {
				t.Errorf("%s: headings = %+v", key, prose.Headings)
			}
		}
	})

	t.Run("a lead key that is also the body key is not duplicated", func(t *testing.T) {
		// description is in both precedence lists on purpose.
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"description": "The opening.\n\n## Detail\n\nMore text.",
		}})
		if prose.Lead != "The opening." {
			t.Errorf("lead = %q", prose.Lead)
		}
		if strings.Count(prose.BodyHTML, "The opening.") != 0 {
			t.Errorf("the body must not repeat the lead: %q", prose.BodyHTML)
		}
		if !strings.Contains(prose.BodyHTML, "More text.") {
			t.Errorf("the rest of the document must survive: %q", prose.BodyHTML)
		}
	})

	t.Run("no prose", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{"count": 1}})
		if prose.Lead != "" || prose.BodyHTML != "" || len(prose.Headings) != 0 {
			t.Errorf("expected empty prose, got %+v", prose)
		}
	})
}

func TestRenderArticleMarkdownHeadingIDs(t *testing.T) {
	html, headings := renderArticleMarkdown("## Why\n\nA.\n\n## Why\n\nB.\n\n### Deep dive\n\nC.")

	if len(headings) != 3 {
		t.Fatalf("headings = %+v, want 3", headings)
	}
	wantIDs := []string{"why", "why-2", "deep-dive"}
	for i, want := range wantIDs {
		if headings[i].ID != want {
			t.Errorf("heading %d id = %q, want %q", i, headings[i].ID, want)
		}
		if !strings.Contains(html, `id="`+want+`"`) {
			t.Errorf("rendered body missing id %q: %s", want, html)
		}
	}
	if headings[2].Level != 3 {
		t.Errorf("level = %d, want 3", headings[2].Level)
	}
	// Headings keep their text; only the tag gains an id.
	if !strings.Contains(html, ">Why<") || !strings.Contains(html, ">Deep dive<") {
		t.Errorf("heading text lost: %s", html)
	}

	// Empty markdown renders nothing rather than an empty wrapper.
	if html, headings := renderArticleMarkdown("   \n"); html != "" || headings != nil {
		t.Errorf("blank body = %q / %+v, want empty", html, headings)
	}
}

// --- wiki page ---------------------------------------------------------------

func TestObjectWikiPageArticleVariant(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", CanonicalID: "o1", Type: "note", Key: "Deed", Version: 3,
		Labels:     []string{"land"},
		Properties: map[string]any{"summary": "A short lead.", "content": "## History\n\nSigned in 1901.\n\n## Notes\n\nFiled under land.", "owner": "Ada Lovelace"},
	}
	data := objectWikiPageData{
		Obj:       obj,
		View:      objectWikiViewArticle,
		PropDefs:  []objectPropertyDef{{Name: "owner", Title: "Owner", Type: "string"}},
		TypeLabel: "Note",
		EdgeCount: 2,
	}
	html := renderHTML(t, objectWikiPage(data))

	for _, want := range []string{
		// The page head is content only: icon tile, name, identity chips.
		`data-testid="object-wiki-head"`,
		`<h1`,
		`Deed`,
		`data-testid="object-ref"`,
		`data-testid="object-wiki-article"`,
		`data-testid="object-lead">A short lead.<`,
		`data-testid="object-body"`,
		`<h2 id="history">History</h2>`,
		`<h2 id="notes">Notes</h2>`,
		`On this page`,
		`href="#history"`,
		`href="#notes"`,
		// The article variant is reading-only: Edit opens the object's own page.
		`href="/objects/o1"`,
		`data-testid="object-wiki-edit"`,
		// Chat is the third read-only link out of the wiki (with Edit and
		// History): the object's own chat page, not an embedded composer.
		`data-testid="object-wiki-chat"`,
		`href="/objects/o1/chat"`,
		// History lives in the shared details rail, not a page-action row, so
		// both variants carry it exactly once.
		`>History</h2>`,
		`href="/objects/o1/history"`,
		`Note Properties`,
		`>Owner<`,
		`Ada Lovelace`,
		`2 relationships`,
		// Both variants are always offered by the switch.
		`data-testid="object-wiki-view-article"`,
		`data-testid="object-wiki-view-page"`,
		`href="/objects/o1/wiki?view=page"`,
		// The switch is icon-only, so each option carries an accessible name and
		// the active one announces itself.
		`aria-label="Article view"`,
		`aria-label="Object page view"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("article variant missing %q", want)
		}
	}

	// The wiki has no page chrome: no breadcrumb trail (the reader arrives from
	// a list, not from a hierarchy), no action button row.
	if strings.Contains(html, "breadcrumbs") {
		t.Error("the wiki must not render a breadcrumb trail")
	}
	// Exactly one option is current, and the article variant renders in it.
	if got := strings.Count(html, `aria-current="page"`); got != 1 {
		t.Errorf("aria-current rendered %d times, want once (one active variant)", got)
	}
	// The head reads like a browse card: type icon on the left, the type/ref
	// identity line first, the object's name second, and no separate type badge.
	head, _, ok := strings.Cut(html, `data-testid="object-wiki-article"`)
	if !ok {
		t.Fatal("missing the article variant marker")
	}
	if got := strings.Count(head, `data-testid="object-wiki-identity"`); got != 1 {
		t.Errorf("identity line rendered %d times in the head, want 1", got)
	}
	if id, title := strings.Index(head, `data-testid="object-wiki-identity"`), strings.Index(head, "<h1"); id < 0 || title < 0 || id > title {
		t.Errorf("identity line must precede the title (identity=%d title=%d)", id, title)
	}
	if !strings.Contains(head, `>note<`) {
		t.Error("identity line must name the object's type")
	}
	if strings.Contains(head, "badge-primary") {
		t.Error("the wiki head must not render the type as a badge")
	}

	// The article's prose is not repeated in the details rail.
	if got := strings.Count(html, "A short lead."); got != 1 {
		t.Errorf("lead rendered %d times, want once (the rail must skip the consumed property)", got)
	}
	// The article variant does not render the object-page layout.
	if strings.Contains(html, `data-testid="object-wiki-page"`) {
		t.Error("the article variant must not render the object-page layout")
	}
	if strings.Contains(html, `action="/objects/o1"`) {
		t.Error("the wiki is read-only and must never render the edit form")
	}
}

func TestObjectWikiPageObjectVariant(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", CanonicalID: "o1", Type: "note", Key: "Deed", Version: 3,
		Properties: map[string]any{"summary": "A short lead.", "content": "## History\n\nSigned in 1901.", "owner": "Ada Lovelace"},
	}
	neighbour := GraphObject{
		ID: "n1", Type: "task", Key: "Fix the roof",
		Properties: map[string]any{"summary": "Leaky after the storm."},
	}
	data := objectWikiPageData{
		Obj:       obj,
		View:      objectWikiViewPage,
		TypeLabel: "Note",
		PropDefs:  []objectPropertyDef{{Name: "owner", Title: "Owner", Type: "string"}},
		EdgeCount: 1,
		Sections: []objectViewSection{{
			Title: "Tasks",
			Rows:  []objectViewItem{{RelID: "r1", RelType: "relates_to", Neighbour: neighbour, Resolved: true}},
			Total: 1,
			Limit: objectWikiSectionLimit,
		}},
		RelationCounts: []objectWikiRelationCount{{Type: "relates_to", Label: "relates to", Count: 1}},
		Sources: []objectSource{{
			DocumentID: "d1", DocumentName: "Kickoff.pdf", DocumentVersion: "v3",
			JobID: "job-1", ExtractedAt: "2026-11-10T09:20:00Z",
		}},
	}
	html := renderHTML(t, objectWikiPage(data))

	for _, want := range []string{
		`data-testid="object-wiki-head"`,
		`data-testid="object-wiki-page"`,
		`data-testid="object-wiki-key-facts"`,
		`Key facts`,
		`>Owner<`,
		`Ada Lovelace`,
		`Overview`,
		`A short lead.`,
		`data-testid="object-wiki-section"`,
		`>Tasks<`,
		`data-testid="object-wiki-relation-row"`,
		`Fix the roof`,
		// Neighbour links stay in the wiki, in the variant being read.
		`href="/objects/n1/wiki?view=page"`,
		`Leaky after the storm.`,
		`Relationships by type`,
		`All 1 relationships`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("object-page variant missing %q", want)
		}
	}
	// History moved into the shared details rail, so it is present here too —
	// once, not twice.
	if got := strings.Count(html, `href="/objects/o1/history"`); got != 1 {
		t.Errorf("History rail link rendered %d times, want once", got)
	}
	// The Source rail labels itself: the row renders inside the rail, not as a
	// second "Source" card with its own heading.
	if got := strings.Count(html, ">Source</h2>"); got != 1 {
		t.Errorf("Source rail rendered %d times, want once", got)
	}
	if !strings.Contains(html, "Kickoff.pdf") {
		t.Error("object-page variant missing the source document row")
	}
	// A related object wears the same layout as the page head: its own identity
	// line (type · ref) above its name, so the head plus the one neighbour give
	// two identity lines.
	if got := strings.Count(html, `data-testid="object-wiki-identity"`); got != 2 {
		t.Errorf("identity lines rendered %d times, want 2 (head + one related object)", got)
	}
	if !strings.Contains(html, `>task<`) {
		t.Error("related object must name its type on the identity line")
	}
	if strings.Contains(html, "breadcrumbs") {
		t.Error("the wiki must not render a breadcrumb trail")
	}
	if strings.Contains(html, `data-testid="object-wiki-article"`) {
		t.Error("the object-page variant must not render the article layout")
	}
}

func TestObjectWikiPageEmptyProseState(t *testing.T) {
	obj := &GraphObject{ID: "o1", Type: "note", Key: "Bare"}
	html := renderHTML(t, objectWikiPage(objectWikiPageData{Obj: obj, View: objectWikiViewArticle}))

	if !strings.Contains(html, `data-testid="object-wiki-empty"`) {
		t.Error("an object with no prose must render the empty state")
	}
	if !strings.Contains(html, "No description yet") {
		t.Error("the empty state must say what is missing")
	}
	if strings.Contains(html, "On this page") {
		t.Error("a page with no headings must not render an empty table of contents")
	}
}

// TestObjectWikiPageOverviewLeads pins the object-page variant's order: the
// overview opens the page (a reader arriving from a list wants to know what the
// object is before reading its fields), key facts follow it, and relationship
// sections come last.
func TestObjectWikiPageOverviewLeads(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", Type: "note", Key: "Deed",
		Properties: map[string]any{"summary": "A short lead.", "owner": "Ada Lovelace"},
	}
	data := objectWikiPageData{
		Obj:       obj,
		View:      objectWikiViewPage,
		PropDefs:  []objectPropertyDef{{Name: "owner", Title: "Owner", Type: "string"}},
		EdgeCount: 1,
		Sections: []objectViewSection{{
			Title: "Tasks",
			Rows:  []objectViewItem{{RelID: "r1", RelType: "relates_to", Neighbour: GraphObject{ID: "n1", Type: "task", Key: "Fix the roof"}, Resolved: true}},
			Total: 1,
			Limit: objectWikiSectionLimit,
		}},
	}
	html := renderHTML(t, objectWikiPage(data))

	overview := strings.Index(html, `>Overview<`)
	facts := strings.Index(html, `>Key facts<`)
	relations := strings.Index(html, `data-testid="object-wiki-section"`)
	if overview < 0 || facts < 0 || relations < 0 {
		t.Fatalf("object-page variant missing a block (overview=%d facts=%d relations=%d)", overview, facts, relations)
	}
	if overview >= facts || facts >= relations {
		t.Errorf("order must be overview → key facts → relationships, got overview=%d facts=%d relations=%d", overview, facts, relations)
	}
	if !strings.Contains(html, `data-testid="object-lead">A short lead.<`) {
		t.Error("the overview must carry the object's prose")
	}

	// No prose: the overview is omitted entirely rather than showing an empty
	// heading over the "no description yet" prompt, and the record starts the
	// page instead.
	bare := &GraphObject{ID: "o2", Type: "note", Key: "Bare", Properties: map[string]any{"owner": "Ada Lovelace"}}
	bareHTML := renderHTML(t, objectWikiPage(objectWikiPageData{
		Obj:      bare,
		View:     objectWikiViewPage,
		PropDefs: data.PropDefs,
	}))
	if strings.Contains(bareHTML, `>Overview<`) {
		t.Errorf("an object with no prose must not render an Overview heading:\n%s", bareHTML)
	}
	if strings.Contains(bareHTML, `data-testid="object-wiki-empty"`) {
		t.Error("the object-page variant must omit the overview, not render its empty state")
	}
	if !strings.Contains(bareHTML, `>Key facts<`) {
		t.Error("the page must still start with the record's facts")
	}
}

// TestUIObjectWikiVariants pins the handler wiring: the wiki defaults to the
// article, ?view=page renders the object page and persists the choice, and a
// later request without the query reads the persisted variant from the cookie.
func TestUIObjectWikiVariants(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "Note", Label: "Note"}}}
	f := &fakeMemory{
		compiled: compiled,
		objects: []GraphObject{{
			ID: "o1", CanonicalID: "o1", Type: "Note", Key: "Deed", Version: 2,
			Properties: map[string]any{"summary": "A short lead."},
		}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/:id/wiki", s.uiObjectWiki)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1/wiki", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="object-wiki-article"`) {
		t.Error("the wiki must default to the article variant")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1/wiki?view=page", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="object-wiki-page"`) {
		t.Error("?view=page must render the object-page variant")
	}
	persisted := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == objectWikiViewCookie && cookie.Value == objectWikiViewPage {
			persisted = true
		}
	}
	if !persisted {
		t.Error("an explicit variant must be persisted as a cookie")
	}

	req := httptest.NewRequest(http.MethodGet, "/objects/o1/wiki", nil)
	req.AddCookie(&http.Cookie{Name: objectWikiViewCookie, Value: objectWikiViewPage})
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `data-testid="object-wiki-page"`) {
		t.Error("the persisted variant must be used when the query omits it")
	}
}

// TestUIObjectPageStaysClassic pins the revert: the object's own route is still
// the editable page, with the form on screen and no wiki layout.
func TestUIObjectPageStaysClassic(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "Note", Label: "Note"}}}
	f := &fakeMemory{
		compiled: compiled,
		objects: []GraphObject{{
			ID: "o1", CanonicalID: "o1", Type: "Note", Key: "Deed", Version: 2,
			Properties: map[string]any{"summary": "A short lead."},
		}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/:id", s.uiObject)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/o1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `action="/objects/o1"`) {
		t.Error("the object page must render the editable form")
	}
	if strings.Contains(body, `data-testid="object-wiki-article"`) || strings.Contains(body, `data-testid="object-wiki-page"`) {
		t.Error("the object page must not render the wiki layout")
	}
}

// TestUIObjectWikiSwitchRoundTrip pins the variant switch as a *control*: going
// to the object page and back to the article must both work, even though the
// chosen variant is persisted as a cookie.
//
// A control link that omits the default variant is read back from that cookie,
// which made the switch look one-way: the article option's href was
// /objects/{id}/wiki, the cookie still said "page", and clicking it re-rendered
// the page. So control links spell the variant out (objectWikiControlHref) while
// card links keep the short default form (objectWikiHref).
func TestUIObjectWikiSwitchRoundTrip(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "Note", Label: "Note"}}}
	f := &fakeMemory{
		compiled: compiled,
		objects: []GraphObject{{
			ID: "o1", CanonicalID: "o1", Type: "Note", Key: "Deed", Version: 2,
			Properties: map[string]any{"summary": "A short lead."},
		}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/objects/:id/wiki", s.uiObjectWiki)

	var cookie *http.Cookie
	get := func(url string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", url, rec.Code)
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == objectWikiViewCookie {
				cookie = c
			}
		}
		return rec.Body.String()
	}
	switchHref := func(html, view string) string {
		t.Helper()
		re := regexp.MustCompile(`href="([^"]*)"[^>]*data-testid="object-wiki-view-` + view + `"`)
		m := re.FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("no %s switch link", view)
		}
		return m[1]
	}

	// Default article, then to the object page.
	if body := get("/objects/o1/wiki"); !strings.Contains(body, `data-testid="object-wiki-article"`) {
		t.Fatal("the wiki must default to the article variant")
	}
	body := get("/objects/o1/wiki?view=page")
	if !strings.Contains(body, `data-testid="object-wiki-page"`) {
		t.Fatal("?view=page must render the object page")
	}

	// Back to the article. The persisted cookie must not win over the control.
	articleHref := switchHref(body, "article")
	if !strings.Contains(articleHref, "view=article") {
		t.Errorf("the article control must spell its variant out, got %q", articleHref)
	}
	body = get(articleHref)
	if !strings.Contains(body, `data-testid="object-wiki-article"`) {
		t.Error("switching back to the article re-rendered the object page")
	}
	if strings.Contains(body, `data-testid="object-wiki-page"`) {
		t.Error("the article variant must not render the object-page layout")
	}

	// And forward again, so neither direction is one-way.
	pageHref := switchHref(body, "page")
	if !strings.Contains(pageHref, "view=page") {
		t.Errorf("the object-page control must spell its variant out, got %q", pageHref)
	}
	if body = get(pageHref); !strings.Contains(body, `data-testid="object-wiki-page"`) {
		t.Error("switching to the object page did not render it")
	}
}

// TestObjectWikiViewPreference pins the resolution order the switch depends on:
// an explicit query parameter beats the persisted cookie, and an unrecognised
// value never renders an empty page.
func TestObjectWikiViewPreference(t *testing.T) {
	pref := func(url string, cookie string) (string, bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: objectWikiViewCookie, Value: cookie})
		}
		e := echo.New()
		var got string
		var explicit bool
		e.GET("/w", func(c echo.Context) error {
			got, explicit = objectWikiViewPreference(c)
			return c.NoContent(http.StatusNoContent)
		})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("handler not reached for %s", url)
		}
		return got, explicit
	}

	cases := []struct {
		name     string
		url      string
		cookie   string
		want     string
		explicit bool
	}{
		{"no preference", "/w", "", objectWikiViewArticle, false},
		{"cookie page", "/w", objectWikiViewPage, objectWikiViewPage, false},
		{"query page wins over cookie", "/w?view=page", objectWikiViewArticle, objectWikiViewPage, true},
		{"query article wins over cookie", "/w?view=article", objectWikiViewPage, objectWikiViewArticle, true},
		{"browse-side parameter", "/w?wiki_view=page", "", objectWikiViewPage, true},
		{"query beats the browse-side parameter", "/w?view=article&wiki_view=page", "", objectWikiViewArticle, true},
		{"unknown value falls back to the article", "/w?view=nonsense", "", objectWikiViewArticle, true},
		{"case-insensitive", "/w?view=PAGE", "", objectWikiViewPage, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, explicit := pref(tc.url, tc.cookie)
			if got != tc.want || explicit != tc.explicit {
				t.Errorf("preference = (%q, %v), want (%q, %v)", got, explicit, tc.want, tc.explicit)
			}
		})
	}
}

// TestObjectWikiHrefForms pins the two href shapes: a card link omits the default
// variant, a control link always names one.
func TestObjectWikiHrefForms(t *testing.T) {
	if got := objectWikiHref("o1", objectWikiViewArticle); got != "/objects/o1/wiki" {
		t.Errorf("default card link = %q, want the short form", got)
	}
	if got := objectWikiHref("o1", ""); got != "/objects/o1/wiki" {
		t.Errorf("no-opinion link = %q, want the reader's variant to decide", got)
	}
	if got := objectWikiHref("o1", objectWikiViewPage); got != "/objects/o1/wiki?view=page" {
		t.Errorf("object-page card link = %q", got)
	}
	if got := objectWikiControlHref("o1", objectWikiViewArticle); got != "/objects/o1/wiki?view=article" {
		t.Errorf("article control link = %q, want the variant spelled out", got)
	}
	if got := objectWikiControlHref("o1", objectWikiViewPage); got != "/objects/o1/wiki?view=page" {
		t.Errorf("object-page control link = %q", got)
	}

	// The browse-side switch always names its variant, so switching back to the
	// article persists a choice instead of reading the old one back.
	data := objectsPageData{TypeFilter: "task"}
	if got := objectsWikiViewHref(data, objectWikiViewArticle); !strings.Contains(got, "wiki_view=article") || !strings.Contains(got, "type=task") {
		t.Errorf("browse switch link = %q, want wiki_view=article and the active filter", got)
	}
	if got := objectsWikiViewHref(objectsPageData{}, objectWikiViewPage); got != "?wiki_view=page" {
		t.Errorf("bare browse switch link = %q", got)
	}
}
