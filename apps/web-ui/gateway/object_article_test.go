package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	prose := objectArticleFor(GraphObject{Properties: map[string]any{"description": float64(2024)}}, nil)
	if prose.Lead != "2024" {
		t.Errorf("numeric-looking description must become the article lead, got lead=%q body=%q", prose.Lead, prose.BodyHTML)
	}
}

// TestObjectExcerptKeyMatching pins both halves of the excerpt matcher's key
// rule: case and separators are tolerated, extra words are not. The positive
// half (Description/DESCRIPTION/description all match) is what makes a pack's
// prose render at all; the negative half is the guard rail — `long_description`
// is a *different property*, and a "helpful" future change (substring or
// word-prefix matching) would silently start rendering it as the description
// (#761, object-browser spec).
func TestObjectExcerptKeyMatching(t *testing.T) {
	// Case is irrelevant: the same property spelled three ways is one property.
	for _, key := range []string{"Description", "DESCRIPTION", "description"} {
		t.Run("matches "+key, func(t *testing.T) {
			if got := objectExcerpt(map[string]any{key: "Body"}); got != "Body" {
				t.Errorf("objectExcerpt(%s: Body) = %q, want %q", key, got, "Body")
			}
		})
	}

	// An extra word makes it a different property, whatever the separators.
	for _, key := range []string{"long_description", "Long Description", "LONG_DESCRIPTION", "longDescription"} {
		t.Run("does not match "+key, func(t *testing.T) {
			if got := objectExcerpt(map[string]any{key: "Long body"}); got != "" {
				t.Errorf("objectExcerpt(%s: Long body) = %q, want no excerpt: %s is not a description", key, got, key)
			}
		})
	}

	// The fold drops separators and spaces but keeps case, so EqualFold is what
	// makes those spellings one property — under their own normalized name,
	// never under "description".
	longFolded := normalizeProseKey("long_description")
	for _, alt := range []string{"Long Description", "LONG_DESCRIPTION"} {
		if !strings.EqualFold(longFolded, normalizeProseKey(alt)) {
			t.Fatalf("long-form key spellings must fold together: %q vs %q", longFolded, normalizeProseKey(alt))
		}
	}
	if strings.EqualFold(longFolded, normalizeProseKey("description")) {
		t.Fatalf("a long-form key must not fold onto %q", normalizeProseKey("description"))
	}
	if _, _, ok := prosePropertyNamed(map[string]any{"long_description": "Long body"}, longFolded); !ok {
		t.Errorf("prosePropertyNamed must still resolve the folded long-form name")
	}

	// A description beside a long description still wins as the excerpt.
	if got := objectExcerpt(map[string]any{"description": "Short", "long_description": "Long body"}); got != "Short" {
		t.Errorf("description must win over long_description, got %q", got)
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
	if strings.Count(html, `line-clamp-2`) != 1 {
		t.Errorf("excerpt clamp = %d, want only the object with prose to carry one", strings.Count(html, "line-clamp-2"))
	}
}

// --- article prose -----------------------------------------------------------

func TestObjectArticleLeadAndBodySplit(t *testing.T) {
	t.Run("summary leads, content bodies", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"summary": "The lead sentence.",
			"content": "## History\n\nSigned in 1901.\n\n## Notes\n\nFiled under land.",
		}}, nil)
		if prose.Lead != "The lead sentence." {
			t.Errorf("lead = %q", prose.Lead)
		}
		if prose.LeadKey != "summary" || prose.BodyKey != "content" {
			t.Errorf("consumed keys = %q/%q", prose.LeadKey, prose.BodyKey)
		}
		if !strings.Contains(prose.BodyHTML, "<h2>History</h2>") || !strings.Contains(prose.BodyHTML, "<h2>Notes</h2>") {
			t.Errorf("body must render the markdown headings: %q", prose.BodyHTML)
		}
	})

	t.Run("summary leads, description bodies", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"summary":     "The short version.",
			"description": "The long version.\n\n## Detail\n\nMore of it.",
		}}, nil)
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
		}}, nil)
		if prose.Lead != "The opening paragraph." {
			t.Errorf("lead = %q", prose.Lead)
		}
		if strings.Contains(prose.BodyHTML, "The opening paragraph.") {
			t.Error("the promoted lead must be removed from the body, not duplicated")
		}
		if !strings.Contains(prose.BodyHTML, "More text.") {
			t.Errorf("body must keep the rest: %q", prose.BodyHTML)
		}
		if !strings.Contains(prose.BodyHTML, "<h2>Details</h2>") {
			t.Errorf("body must keep the heading tree: %q", prose.BodyHTML)
		}
	})

	t.Run("single-paragraph body has no body left", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{"content": "Only one paragraph."}}, nil)
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
		}}, nil)
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
			}}, nil)
			if prose.Lead != "The opening paragraph." {
				t.Errorf("%s: lead = %q", key, prose.Lead)
			}
			if !strings.Contains(prose.BodyHTML, "More text.") {
				t.Errorf("%s: the lead property's remaining paragraphs must become the body, got %q", key, prose.BodyHTML)
			}
			if strings.Contains(prose.BodyHTML, "The opening paragraph.") {
				t.Errorf("%s: the promoted lead must not repeat in the body: %q", key, prose.BodyHTML)
			}
			if !strings.Contains(prose.BodyHTML, "<h2>Detail</h2>") {
				t.Errorf("%s: the promoted body must keep its heading tree: %q", key, prose.BodyHTML)
			}
		}
	})

	t.Run("a lead key that is also the body key is not duplicated", func(t *testing.T) {
		// description is in both precedence lists on purpose.
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"description": "The opening.\n\n## Detail\n\nMore text.",
		}}, nil)
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
		prose := objectArticleFor(GraphObject{Properties: map[string]any{"count": 1}}, nil)
		if prose.Lead != "" || prose.BodyHTML != "" {
			t.Errorf("expected empty prose, got %+v", prose)
		}
	})
}

// --- wiki page ---------------------------------------------------------------

// TestObjectWikiPageLayout pins the wiki's one layout: a content head, the prose
// overview, the type's key facts, then one section per relationship group, with
// the facts a reader cannot find elsewhere in the details rail. There is no
// variant switch: the retired article layout must not render, in any form.
func TestObjectWikiPageLayout(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", CanonicalID: "o1", Type: "note", Key: "Deed", Version: 3,
		Labels:     []string{"land"},
		Properties: map[string]any{"summary": "A short lead.", "content": "## History\n\nSigned in 1901.\n\n## Notes\n\nFiled under land.", "owner": "Ada Lovelace"},
	}
	data := objectWikiPageData{
		Obj:       obj,
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
		`data-testid="object-wiki-page"`,
		`data-testid="object-lead">A short lead.<`,
		`data-testid="object-body"`,
		`<h2>History</h2>`,
		`<h2>Notes</h2>`,
		// The key facts are the layout's property surface, titled from the type.
		`data-testid="object-wiki-key-facts"`,
		`>Note Properties<`,
		`>Owner<`,
		`Ada Lovelace`,
		// Reading-only: Edit opens the object's own page, Chat its chat page.
		`href="/objects/o1"`,
		`data-testid="object-wiki-edit"`,
		`data-testid="object-wiki-chat"`,
		`href="/objects/o1/chat"`,
		// History lives in the details rail, exactly once.
		`href="/objects/o1/history"`,
		`2 relationships`,
		`>Labels<`,
		`land`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("wiki page missing %q", want)
		}
	}

	// The variant switch and the article layout are gone.
	for _, unwanted := range []string{
		`data-testid="object-wiki-article"`,
		`data-testid="object-wiki-view-article"`,
		`data-testid="object-wiki-view-page"`,
		`aria-label="Article view"`,
		`aria-label="Object page view"`,
		`On this page`,
		`?view=`,
		`wiki_view=`,
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("retired variant machinery rendered: %q", unwanted)
		}
	}
	if got := strings.Count(html, `aria-current="page"`); got != 0 {
		t.Errorf("aria-current rendered %d times, want none (no active variant)", got)
	}

	// The wiki has no page chrome: no breadcrumb trail (the reader arrives from
	// a list, not from a hierarchy), no action button row, no edit form.
	if strings.Contains(html, "breadcrumbs") {
		t.Error("the wiki must not render a breadcrumb trail")
	}
	if strings.Contains(html, `action="/objects/o1"`) {
		t.Error("the wiki is read-only and must never render the edit form")
	}

	// The head reads like a browse card: type icon on the left, the type/ref
	// identity line first, the object's name second, and no separate type badge.
	head, _, ok := strings.Cut(html, `data-testid="object-wiki-page"`)
	if !ok {
		t.Fatal("missing the wiki page marker")
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

	// The prose is not repeated in the details rail.
	if got := strings.Count(html, "A short lead."); got != 1 {
		t.Errorf("lead rendered %d times, want once (the rail must skip the consumed property)", got)
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
		// The property card is titled from the type label, the same heading the
		// rail and the editor's card use.
		`>Note Properties<`,
		`>Owner<`,
		`Ada Lovelace`,
		`Overview`,
		`A short lead.`,
		`data-testid="object-wiki-section"`,
		`>Tasks<`,
		`data-testid="object-wiki-relation-row"`,
		`Fix the roof`,
		// Neighbour links stay in the wiki.
		`href="/objects/n1/wiki"`,
		`Leaky after the storm.`,
		// One relationships rail in the page view: the by-type breakdown, under
		// the plain "Relationships" heading.
		`>Relationships<`,
		`All 1 relationships`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("object-page variant missing %q", want)
		}
	}
	if strings.Contains(html, "Relationships by type") {
		t.Error("the page view renders one relationships rail, so the by-type title must be gone")
	}
	// The rail must not repeat the property list the body card already carries:
	// one value, one place.
	if strings.Contains(html, `id="object-wiki-rail-facts"`) {
		t.Error("the page view's rail must not repeat the body card's properties")
	}
	if got := strings.Count(html, `data-empties-toggle=`); got > 1 {
		t.Errorf("page view carries %d fold-empty controls, want at most the body card's", got)
	}
	// Feedback on this row (#763): the type tile sits on the identity line, not
	// centred against the two-line excerpt the row carries.
	rowAt := strings.Index(html, `data-testid="object-wiki-relation-row"`)
	if rowAt < 0 {
		t.Fatal("missing the relationship row marker")
	}
	if row := html[rowAt:]; !strings.Contains(row, "flex min-w-0 items-start gap-3") {
		t.Errorf("a relationship row's type tile must align to the top of the row:\n%s", row)
	}
	// History moved into the shared details rail, so it is present here too —
	// once, not twice.
	if got := strings.Count(html, `href="/objects/o1/history"`); got != 1 {
		t.Errorf("History rail link rendered %d times, want once", got)
	}
	// The Source rail labels itself: the row renders inside the rail, not as a
	// second "Source" card with its own heading. Since the provenance group
	// absorbed the sources, that heading is the group's.
	if got := strings.Count(html, ">Provenance</h2>"); got != 1 {
		t.Errorf("provenance group rendered %d times, want once", got)
	}
	if strings.Contains(html, ">Source</h2>") {
		t.Error("the source rows belong to the provenance group, not to a second block")
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
}

// TestObjectWikiRelationRowAlignsTallRowsOnly pins the #763 delta: a row opts
// the type tile onto its first line only when it carries an excerpt, because an
// excerpt-less row is two lines tall and reads better with the tile centred. A
// mixed group must therefore render both alignments.
func TestObjectWikiRelationRowAlignsTallRowsOnly(t *testing.T) {
	data := objectWikiPageData{
		Obj: &GraphObject{ID: "o1", CanonicalID: "o1", Type: "note", Key: "Deed"},
		Sections: []objectViewSection{{
			Title: "Tasks",
			Rows: []objectViewItem{
				{
					RelID: "r1", RelType: "relates_to", Resolved: true,
					Neighbour: GraphObject{
						ID: "n1", Type: "task", Key: "With a summary",
						Properties: map[string]any{"summary": "Leaky after the storm."},
					},
				},
				{
					RelID: "r2", RelType: "relates_to", Resolved: true,
					Neighbour: GraphObject{ID: "n2", Type: "task", Key: "Bare"},
				},
			},
			Total: 2,
			Limit: objectWikiSectionLimit,
		}},
	}
	html := renderHTML(t, objectWikiPage(data))

	sectionAt := strings.Index(html, `data-testid="object-wiki-section"`)
	if sectionAt < 0 {
		t.Fatal("missing the relationship section")
	}
	rows := html[sectionAt:]
	if got := strings.Count(rows, "flex min-w-0 items-start gap-3"); got != 1 {
		t.Errorf("excerpt-bearing rows top-aligned = %d, want 1 (the tall row):\n%s", got, rows)
	}
	if got := strings.Count(rows, "flex min-w-0 items-center gap-3"); got != 1 {
		t.Errorf("excerpt-less rows centred = %d, want 1 (the two-line row):\n%s", got, rows)
	}
}

func TestObjectWikiPageOverviewLeads(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", Type: "note", Key: "Deed",
		Properties: map[string]any{"summary": "A short lead.", "owner": "Ada Lovelace"},
	}
	data := objectWikiPageData{
		Obj:       obj,
		TypeLabel: "Note",
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
	facts := strings.Index(html, `>Note Properties<`)
	relations := strings.Index(html, `data-testid="object-wiki-section"`)
	if overview < 0 || facts < 0 || relations < 0 {
		t.Fatalf("object-page variant missing a block (overview=%d facts=%d relations=%d)", overview, facts, relations)
	}
	if overview >= facts || facts >= relations {
		t.Errorf("order must be overview → type properties → relationships, got overview=%d facts=%d relations=%d", overview, facts, relations)
	}
	if !strings.Contains(html, `data-testid="object-lead">A short lead.<`) {
		t.Error("the overview must carry the object's prose")
	}

	// No prose: the overview is omitted entirely rather than showing an empty
	// heading over the "no description yet" prompt, and the record starts the
	// page instead.
	bare := &GraphObject{ID: "o2", Type: "note", Key: "Bare", Properties: map[string]any{"owner": "Ada Lovelace"}}
	bareHTML := renderHTML(t, objectWikiPage(objectWikiPageData{
		Obj:       bare,
		TypeLabel: data.TypeLabel,
		PropDefs:  data.PropDefs,
	}))
	if strings.Contains(bareHTML, `>Overview<`) {
		t.Errorf("an object with no prose must not render an Overview heading:\n%s", bareHTML)
	}
	if strings.Contains(bareHTML, `data-testid="object-wiki-empty"`) {
		t.Error("the object-page variant must omit the overview, not render its empty state")
	}
	if !strings.Contains(bareHTML, `>Note Properties<`) {
		t.Error("the page must still start with the type's properties")
	}
}

// TestUIObjectWikiVariants pins the handler wiring: the wiki defaults to the
// article, ?view=page renders the object page and persists the choice, and a
// later request without the query reads the persisted variant from the cookie.
// TestUIObjectWikiRetiredViewParams pins the wiki's handler: it renders the one
// layout, and a request that still carries a retired variant selector is
// redirected to the bare URL rather than silently served a different page.
func TestUIObjectWikiRetiredViewParams(t *testing.T) {
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
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="object-wiki-page"`) {
		t.Error("the wiki must render the object-page layout")
	}
	if strings.Contains(body, `data-testid="object-wiki-view-`) {
		t.Error("the wiki must not render a variant switch")
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("the wiki must not set a view cookie, got %v", rec.Result().Cookies())
	}

	for _, legacy := range []string{"/objects/o1/wiki?view=page", "/objects/o1/wiki?view=article", "/objects/o1/wiki?wiki_view=article"} {
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, legacy, nil))
		if rec.Code != http.StatusMovedPermanently {
			t.Errorf("GET %s = %d, want a 301", legacy, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/objects/o1/wiki" {
			t.Errorf("GET %s redirected to %q, want the bare wiki URL", legacy, loc)
		}
	}
}

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

// TestObjectWikiHref pins the one link helper every surface uses: the canonical
// wiki URL, with no retired variant selector attached.
func TestObjectWikiHref(t *testing.T) {
	if got := objectWikiHref("o1"); got != "/objects/o1/wiki" {
		t.Errorf("objectWikiHref(o1) = %q, want /objects/o1/wiki", got)
	}
	if got := objectWikiHref("a/b"); got != "/objects/a%2Fb/wiki" {
		t.Errorf("objectWikiHref must escape the id, got %q", got)
	}
}

func TestObjectArticleExplicitMarkdownBody(t *testing.T) {
	markdownDef := func(name string) objectPropertyDef {
		return objectPropertyDef{Name: name, Type: "string", Format: "markdown"}
	}

	t.Run("declared format beats the conventional body key", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"content": "The conventional body.",
			"readme":  "## Declared\n\nThe schema's choice.",
		}}, []objectPropertyDef{{Name: "content", Type: "string"}, markdownDef("readme")})
		if prose.BodyKey != "readme" {
			t.Fatalf("body key = %q, want readme", prose.BodyKey)
		}
		if !strings.Contains(prose.BodyHTML, "choice.") {
			t.Errorf("declared markdown must render as the body: %q", prose.BodyHTML)
		}
		if strings.Contains(prose.BodyHTML, "The conventional body.") {
			t.Errorf("the conventional body must not also render: %q", prose.BodyHTML)
		}
	})

	t.Run("unrelated format leaves the heuristic alone", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"content": "## History\n\nSigned in 1901.",
		}}, []objectPropertyDef{{Name: "content", Type: "string", Format: "email"}})
		if prose.BodyKey != "content" {
			t.Fatalf("body key = %q, want content", prose.BodyKey)
		}
	})

	t.Run("markdown on a non-string type is ignored", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"content": "The conventional body.",
			"tags":    "not prose",
		}}, []objectPropertyDef{
			{Name: "content", Type: "string"},
			{Name: "tags", Type: "array", Format: "markdown"},
		})
		if prose.BodyKey != "content" {
			t.Fatalf("body key = %q, want content", prose.BodyKey)
		}
	})

	t.Run("the lead property is not stolen as the body", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"summary": "The lead.",
			"notes":   "## Body\n\nFrom the notes field.",
		}}, []objectPropertyDef{markdownDef("summary"), markdownDef("notes")})
		if prose.LeadKey != "summary" || prose.BodyKey != "notes" {
			t.Fatalf("consumed keys = %q/%q, want summary/notes", prose.LeadKey, prose.BodyKey)
		}
		if prose.Lead != "The lead." {
			t.Errorf("lead = %q", prose.Lead)
		}
	})

	t.Run("an empty declared field falls back to the heuristic", func(t *testing.T) {
		prose := objectArticleFor(GraphObject{Properties: map[string]any{
			"content": "The conventional body.",
			"readme":  "",
		}}, []objectPropertyDef{markdownDef("readme")})
		if prose.BodyKey != "content" {
			t.Fatalf("body key = %q, want the conventional fallback", prose.BodyKey)
		}
	})
}

// TestObjectWikiFoldsEmptyProperties pins the property surfaces' folded state.
// Each view lists the type's properties exactly once — the page view in its body
// card, the article view in the rail, because its prose has already consumed the
// summary — so each carries one eye, one data-empty row and one folded row. The
// eye only renders when something is filled and something is not. A page whose
// fields are all unset folds nothing: the list still names the declared fields
// rather than rendering an empty table.
// TestObjectWikiFoldsEmptyProperties pins the property surface's folded state.
// The page lists the type's properties exactly once, in its key-facts table, so
// it carries one eye, one data-empty row and one folded row. The eye only
// renders when something is filled and something is not. A page whose fields are
// all unset folds nothing: the list still names the declared fields rather than
// rendering an empty table.
func TestObjectWikiFoldsEmptyProperties(t *testing.T) {
	newPage := func(props map[string]any) objectWikiPageData {
		obj := GraphObject{ID: "o1", Type: "note", Key: "the-note", Properties: props}
		return objectWikiPageData{
			Obj:       &obj,
			TypeLabel: "Note",
			PropDefs: []objectPropertyDef{
				{Name: "owner", Title: "Owner", Type: "string"},
				{Name: "reviewed_by", Title: "Reviewed by", Type: "string"},
			},
		}
	}

	html := renderHTML(t, objectWikiPage(newPage(map[string]any{
		"summary": "A short lead.",
		"owner":   "Ada Lovelace",
	})))

	if !strings.Contains(html, `<table class="table table-sm" id="object-wiki-key-facts"`) {
		t.Errorf("the properties must render as a two-column table:\n%s", html)
	}
	if got := strings.Count(html, `data-empties-toggle="object-wiki-key-facts"`); got != 1 {
		t.Errorf("the key-facts table rendered %d fold controls, want 1", got)
	}
	// One surface: the rail must not repeat the list the body already carries.
	if strings.Contains(html, "object-wiki-rail-facts") {
		t.Error("the rail must not carry a second property list")
	}
	for _, want := range []string{`data-label-show="Show all"`, `data-label-hide="Hide empty"`} {
		if got := strings.Count(html, want); got != 1 {
			t.Errorf("%s rendered %d times, want 1", want, got)
		}
	}
	if got := strings.Count(html, `data-empty`); got != 1 {
		t.Errorf("unset property tagged %d times, want 1", got)
	}
	if got := strings.Count(html, "display:none"); got != 1 {
		t.Errorf("unset property folded %d times, want 1", got)
	}
	if !strings.Contains(html, ">Ada Lovelace</td>") {
		t.Error("a filled property must render its value in the properties table")
	}

	// Nothing to unfold: no eye, and the empties stay visible rather than
	// leaving the list blank.
	allUnset := renderHTML(t, objectWikiPage(newPage(map[string]any{"summary": "A short lead."})))
	if strings.Contains(allUnset, "data-empties-toggle=") {
		t.Error("no filled field means nothing to unfold, so no eye may render")
	}
	if strings.Contains(allUnset, "display:none") {
		t.Error("with every field unset nothing is folded")
	}
	if !strings.Contains(allUnset, "Reviewed by") {
		t.Error("an all-unset list must still name its declared fields")
	}
}

func TestObjectWikiRailProvenance(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", CanonicalID: "o1", Type: "note", Key: "Deed", Version: 3,
		DataClass: "synthesized", CredentialKind: "session",
		ActorType: "user", ActorID: "u1",
		EmbeddingStatus: "embedded", EmbeddingUpdatedAt: "2026-11-12T10:01:50Z",
	}
	data := objectWikiPageData{
		Obj:       obj,
		TypeLabel: "Note",
		Identities: []objectActorOption{
			{Type: "user", ID: "u1", Label: "Ada Lovelace"},
		},
		Sources: []objectSource{{
			DocumentID: "d1", DocumentName: "Kickoff.pdf", DocumentVersion: "v3", ExtractedAt: "2026-11-10T09:20:00Z",
		}},
	}
	data.Provenance = objectProvenanceOf(*obj, data.Identities)
	html := renderHTML(t, objectWikiPage(data))

	meta := strings.Index(html, ">Metadata<")
	branch := strings.Index(html, `data-testid="object-branch-chip"`)
	embed := strings.Index(html, `data-testid="object-embedding"`)
	prov := strings.Index(html, ">Provenance<")
	if meta < 0 || branch < 0 || embed < 0 || prov < 0 {
		t.Fatalf("rail blocks missing (metadata=%d branch=%d embedding=%d provenance=%d)", meta, branch, embed, prov)
	}
	// Branch and embedding are record facts: after the Metadata heading, before
	// the group below it.
	if meta >= branch || branch >= embed || embed >= prov {
		t.Errorf("order must be metadata → branch → embedding → provenance, got %d/%d/%d/%d", meta, branch, embed, prov)
	}
	// And not in the header: everything before the Metadata heading is the head.
	if strings.Contains(html[:meta], "object-branch-chip") || strings.Contains(html[:meta], "object-embedding") {
		t.Error("the header must not repeat the branch or embedding badge the rail carries")
	}

	// The chain reads oldest first: the source document, then the class, then the
	// writer of the version on screen.
	source := strings.Index(html, "Kickoff.pdf")
	class := strings.Index(html, `data-testid="object-wiki-provenance-class"`)
	write := strings.Index(html, `data-testid="object-wiki-provenance-write"`)
	if source < 0 || class < 0 || write < 0 {
		t.Fatalf("provenance entries missing (source=%d class=%d write=%d)", source, class, write)
	}
	if source >= class || class >= write {
		t.Errorf("provenance order must be source → class → write, got %d/%d/%d", source, class, write)
	}
	for _, want := range []string{
		"Synthesized",
		"Version 3",
		"written by",
		"Ada Lovelace",
		"via web session",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("provenance list missing %q", want)
		}
	}
	// The read path's created_at is the version's timestamp, not the object's, so
	// the rail must not present it as a creation date.
	if strings.Contains(html, ">Created<") {
		t.Error("the rail must not label a version timestamp as the object's creation")
	}

	// The embedding badge is outlined (border + text colour, no fill) and carries
	// the vector's timestamp in its tooltip.
	window := html[max(0, embed-400):min(len(html), embed+400)]
	if !strings.Contains(window, "badge-outline") {
		t.Errorf("the embedding badge must be outlined:\n%s", window)
	}
	if !strings.Contains(window, `title="Vector last written `) {
		t.Errorf("the embedding badge must title the vector's timestamp:\n%s", window)
	}
	if strings.Contains(html, "Embedded <") || strings.Contains(html, "Embedded 3") {
		t.Error("the separate relative-time line is superseded by the badge tooltip")
	}

	// The History block counts versions from the version number when the server
	// reports no revision_count.
	if !strings.Contains(html, "3 versions") {
		t.Error("history must count the object's versions even without revision_count")
	}
	if strings.Contains(html, "No versions recorded") {
		t.Error("an object with a version number must never claim there are no versions")
	}
	if strings.Contains(html, "View history") {
		t.Error("a known count renders the count, not the unknown-count fallback")
	}
}

// TestObjectWikiRailProvenanceDegrades pins the two empty states: no sources and
// no write stamp renders no group at all, and a system actor is named as such
// rather than dropped from the chain.
func TestObjectWikiRailProvenanceDegrades(t *testing.T) {
	bare := &GraphObject{ID: "o1", CanonicalID: "o1", Type: "note", Key: "Deed"}
	html := renderHTML(t, objectWikiPage(objectWikiPageData{Obj: bare, TypeLabel: "Note"}))
	if strings.Contains(html, "object-wiki-provenance") || strings.Contains(html, ">Provenance<") {
		t.Error("an object with no source, class or write stamp renders no provenance group")
	}
	// No revision_count and no version number: offer the history, do not claim an
	// empty one.
	if !strings.Contains(html, "View history") {
		t.Error("an unknown version count must offer the history rather than a count")
	}

	system := &GraphObject{ID: "o2", CanonicalID: "o2", Type: "note", Key: "Deed", Version: 1, ActorType: "system", CredentialKind: "none"}
	data := objectWikiPageData{Obj: system, TypeLabel: "Note"}
	data.Provenance = objectProvenanceOf(*system, nil)
	html = renderHTML(t, objectWikiPage(data))
	if !strings.Contains(html, ">System<") {
		t.Error("a background write must be named as System")
	}
	if strings.Contains(html, "via ") {
		t.Error("a write with no credential must not render a credential")
	}
}

// TestObjectWikiRailWorkflow pins the Workflow rail group: a type that declares
// a workflow shows its status badge and assignee there, read-only, and the
// header carries no status chip. The two values are the object's position in its
// type's work process; the pickers stay on the object page it links to.
func TestObjectWikiRailWorkflow(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", CanonicalID: "o1", Type: "task", Key: "t1", Version: 2,
		Status: "in_progress", Assignee: "Ada Lovelace",
	}
	data := objectWikiPageData{
		Obj: obj, TypeLabel: "Task",
		Status: &objectStatusControl{Show: true, Options: []string{"ready", "in_progress", "done"}},
	}
	html := renderHTML(t, objectWikiPage(data))

	if got := strings.Count(html, ">Workflow</h2>"); got != 1 {
		t.Errorf("workflow group rendered %d times, want once", got)
	}
	start := strings.Index(html, `data-testid="object-wiki-workflow"`)
	if start < 0 {
		t.Fatal("missing the workflow rail group")
	}
	group := html[start:]
	if end := strings.Index(group, "</section>"); end > 0 {
		group = group[:end]
	}
	if !strings.Contains(group, "In progress") {
		t.Errorf("workflow group must show the humanised status label:\n%s", group)
	}
	if !strings.Contains(group, `data-status="in_progress"`) {
		t.Errorf("workflow group must carry the raw status:\n%s", group)
	}
	if !strings.Contains(group, "Ada Lovelace") {
		t.Errorf("workflow group must name the assignee:\n%s", group)
	}
	if strings.Contains(group, `data-testid="object-status-control"`) {
		t.Error("the wiki is read-only and must not render the status picker")
	}
	if !strings.Contains(group, `href="/objects/o1"`) {
		t.Errorf("workflow group must link to the object page:\n%s", group)
	}
	// The link is the group's pencil, inline at the end of the last row — not a
	// sentence of its own after the list.
	if dl := strings.Index(group, "</dl>"); dl > 0 {
		edit := strings.Index(group, `data-testid="object-wiki-workflow-edit"`)
		if edit < 0 || edit > dl {
			t.Errorf("the workflow edit link must sit inside the group's last row, before </dl>:\n%s", group)
		}
		if strings.Contains(group[:dl], "Change on the object page") {
			t.Error("the workflow group must not carry the old sentence link")
		}
	}
	// The header is everything before the rail: no status chip there.
	if at := strings.Index(html, ">Metadata</h2>"); at > 0 && strings.Contains(html[:at], "data-status=") {
		t.Error("the header must not repeat the workflow status badge")
	}
}

// TestObjectWikiRailWorkflowAbsent pins the other side of the separation: an
// object whose type declares no workflow has no workflow at all. A value in the
// work-status column is not a state of that type, so no group and no badge
// render, and the declared `status` property stays the one place the value is
// read — exactly once.
func TestObjectWikiRailWorkflowAbsent(t *testing.T) {
	obj := &GraphObject{
		ID: "o1", CanonicalID: "o1", Type: "obligation", Key: "ob-boiler", Version: 3,
		Status:     "active",
		Properties: map[string]any{"status": "active", "title": "Boiler service"},
	}
	data := objectWikiPageData{
		Obj: obj, TypeLabel: "Obligation",
		PropDefs: []objectPropertyDef{{Name: "status", Title: "Status", Type: "string"}},
	}
	html := renderHTML(t, objectWikiPage(data))

	if strings.Contains(html, ">Workflow</h2>") {
		t.Error("a type that declares no workflow must not render the workflow group")
	}
	if strings.Contains(html, "data-status=") {
		t.Error("a type that declares no workflow must not render a status badge")
	}
	if got := strings.Count(html, ">active</td>"); got != 1 {
		t.Errorf("the status property rendered %d times, want once", got)
	}
	if strings.Contains(html, ">Active<") {
		t.Error("the workflow label must not be derived from a property value")
	}
}
