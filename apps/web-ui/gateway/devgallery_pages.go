package main

import (
	"net/url"
	"sort"
	"time"

	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// --- Page previews ---
//
// The component gallery catalogs components; this section previews whole pages.
// A page preview is a dev-gallery-only surface: it renders a page body with
// authored sample data inside an isolated iframe, so a layout can be reviewed
// (and clicked through) before it is wired to a handler. Page previews are not
// catalog entries — they are not reusable components, and the component graph
// deliberately does not model them.

// GalleryPagePreview is one page preview: identity plus a body renderer. Body is
// a function so a preview re-renders its sample data per request (relative
// timestamps stay fresh) without the gallery retaining a component tree.
type GalleryPagePreview struct {
	Slug        string
	Name        string
	Description string
	// Height is the embedded iframe's CSS height, e.g. "1100px".
	Height string
	// Body renders the page body (chromeless — the document supplies <head>).
	// It receives the preview document's query values so a preview can react to
	// its own controls (the objects browser's sort links) without a backend:
	// clicking a sort header reloads the iframe with ?order=… and the sample
	// data re-renders in that order.
	Body func(q url.Values) templ.Component
}

// galleryPagePreviews returns the preview registry in display order.
func galleryPagePreviews() []GalleryPagePreview {
	return []GalleryPagePreview{
		{
			Slug:        "objects",
			Name:        "Objects browser",
			Description: "Wiki-style card grid with per-card selection and grouped actions, a one-line search/filter toolbar with the sort control and the wiki view switch, a single stats strip, and \"Ask the graph\" behind a header dialog. Cards link into an object's wiki page. Sample data; selection and the bulk bar are live.",
			Height:      "1080px",
			Body: func(q url.Values) templ.Component {
				data := objectsBrowserSampleData(q.Get("order"))
				// The switch is query-only, like the sort control, so the preview
				// can show both variants without a backend.
				data.WikiView = q.Get("wiki_view")
				return ObjectsPage(data)
			},
		},
		{
			Slug:        "objects-search",
			Name:        "Objects browser — search results",
			Description: "The same card grid in unified search mode: cards keep their type accent, excerpt and labels, gain a relevance score, and drop the status/embedding badges the unified endpoint cannot fill.",
			Height:      "760px",
			Body:        func(url.Values) templ.Component { return ObjectsPage(objectsBrowserSearchSampleData()) },
		},
		{
			Slug:        "objects-wiki",
			Name:        "Object wiki",
			Description: "The reading surface a card opens: a content head (type tile, name, identity chips) with the variant switch and a small Edit link, no breadcrumb trail and no action row. \u201cArticle\u201d reads the object's prose with its properties beside it; \u201cObject page\u201d is the structured variant with key facts and relationship groups. Both carry History in the details rail. Sample data.",
			Height:      "1120px",
			Body: func(q url.Values) templ.Component {
				return objectWikiPage(galleryObjectWikiSampleData(q.Get("wiki_view")))
			},
		},
	}
}

// galleryPagePreviewBySlug resolves one page preview.
func galleryPagePreviewBySlug(slug string) (GalleryPagePreview, bool) {
	for _, p := range galleryPagePreviews() {
		if p.Slug == slug {
			return p, true
		}
	}
	return GalleryPagePreview{}, false
}

// uiComponentPagePreview serves one page preview document. Like the component
// previews it 404s when the gallery flag is off.
func (s *Server) uiComponentPagePreview(c echo.Context) error {
	if !s.cfg.ComponentGallery {
		return echo.ErrNotFound
	}
	page, ok := galleryPagePreviewBySlug(c.Param("slug"))
	if !ok {
		return echo.ErrNotFound
	}
	render.RenderPage(c.Response().Writer, c.Request(), GalleryPagePreviewDocument(page, c.QueryParams(), s.cfg.FeedbackOverlayURL))
	return nil
}

// objectsBrowserSearchSampleData is the unified-search variant of the preview:
// ranked hits with relevance scores and no status/embedding values, which is
// exactly what the unified endpoint returns. It exercises the card's
// search-mode badges (score, no status/embedding) in the gallery.
func objectsBrowserSearchSampleData() objectsPageData {
	return objectsPageData{
		Query: "acme",
		Mode:  "unified",
		Types: []string{"company", "document", "event", "note", "person", "project", "task"},
		Results: []ObjectSearchResult{
			{Score: 0.942, Object: GraphObject{
				ID: "5a3c9f68-1d27-4e0b-8c45-3f6b9a2d1e77", Type: "company", Key: "Acme Robotics",
				Labels: []string{"customer", "enterprise", "emea"},
			}},
			{Score: 0.887, Object: GraphObject{
				ID: "c8e2d5a1-4b96-4f30-a7d8-9e1c2b6f4a03", Type: "note", Key: "Acme onboarding call — notes",
				Labels: []string{"meeting", "customer"},
			}},
			{Score: 0.741, Object: GraphObject{
				ID: "4c9a7f31-6e28-4d05-b3a1-8f2c7e5b9d16", Type: "document", Key: "Acme Robotics — platform RFC",
				Labels: []string{"architecture", "rfc"},
			}},
			{Score: 0.662, Object: GraphObject{
				ID: "2b7d3e14-8f05-4a6e-b1c9-5d2a8e4f7b90", Type: "person", Key: "Priya Raman",
				Labels: []string{"acme", "champion"},
			}},
			{Score: 0.518, Object: GraphObject{
				ID: "7d4b1e93-2c58-4a07-9f6e-1b3a5c8d2e64", Type: "event", Key: "Acme quarterly business review",
			}},
			{Score: 0.407, Object: GraphObject{
				ID: "9f1c0a2e-6b41-4c8d-9a2f-0e7d3b5a1c22", Type: "task", Key: "Send Acme the security questionnaire",
				Labels: []string{"q3"},
			}},
		},
		TypeUIByType: map[string]typeUI{
			"task":     {Icon: "check-circle", Color: "#0EA5E9"},
			"person":   {Icon: "user", Color: "#8B5CF6"},
			"company":  {Icon: "briefcase", Color: "#F59E0B"},
			"note":     {Icon: "file-text", Color: "#10B981"},
			"event":    {Icon: "calendar", Color: "#EC4899"},
			"document": {Icon: "book-open", Color: "#6366F1"},
			"project":  {Icon: "layers", Color: "#14B8A6"},
		},
		Preview: true,
	}
}

// objectsBrowserSampleData is the authored sample the objects browser preview
// renders: a page of varied object types, statuses, embedding states, labels,
// and ages, plus the stats and filter sources the real page would receive.
//
// It is shaped exactly like a live objectsPageData (Preview=true suppresses the
// live wiring), so the preview exercises the same components the page will use.
func objectsBrowserSampleData(order string) objectsPageData {
	order = normalizeObjectOrder(order)
	now := time.Now()
	ago := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	irev := func(n int) *int { return &n }

	objects := []GraphObject{
		{
			ID: "9f1c0a2e-6b41-4c8d-9a2f-0e7d3b5a1c22", Type: "task", Key: "Ship the objects browser table",
			Status: "in_progress", EmbeddingStatus: "embedded", Labels: []string{"q3", "ui"},
			CreatedAt: ago(35 * time.Minute),
			ActorType: "agent", ActorID: "a-1", CredentialKind: "mcp_link",
			RevisionCount: irev(4),
			Properties: map[string]any{
				"summary":     "Replace the objects table with a card grid that reads like a wiki and gives each object an excerpt.",
				"description": "## Why\n\nThe table packed four facts into a row and pushed the object's actual content off-screen. A card gives each object a title, a summary, and room to breathe.\n\n## Scope\n\n- Responsive grid from one column to four\n- Per-card selection feeding the existing bulk actions\n- Writer and version count in the footer\n\n### Open questions\n\nWhether four columns is too dense at very wide widths.",
			},
		},
		{
			ID: "2b7d3e14-8f05-4a6e-b1c9-5d2a8e4f7b90", Type: "person", Key: "Ada Lovelace",
			EmbeddingStatus: "embedded", Labels: []string{"founder", "advisor"},
			CreatedAt: ago(2 * time.Hour),
			ActorType: "user", ActorID: "u-2", CredentialKind: "session",
			Properties: map[string]any{
				"description": "Mathematician and writer, chiefly known for her work on Charles Babbage's proposed mechanical general-purpose computer, the Analytical Engine.",
			},
		},
		{
			ID: "5a3c9f68-1d27-4e0b-8c45-3f6b9a2d1e77", Type: "company", Key: "Acme Robotics",
			EmbeddingStatus: "embedded", Labels: []string{"customer", "enterprise", "emea"},
			CreatedAt: ago(5 * time.Hour),
			ActorType: "agent", ActorID: "a-1",
			Properties: map[string]any{
				"excerpt": "Warehouse automation vendor in the EMEA region; enterprise customer since 2024.",
			},
		},
		{
			ID: "c8e2d5a1-4b96-4f30-a7d8-9e1c2b6f4a03", Type: "note", Key: "Kickoff meeting notes",
			EmbeddingStatus: "pending", Labels: []string{"meeting"},
			CreatedAt: ago(26 * time.Hour),
			ActorType: "user", ActorID: "u-1",
			Properties: map[string]any{
				"content": "The kickoff covered scope, owners, and the review cadence.\n\n## Decisions\n\n- Cards over a table\n- Ship the excerpt resolver with the grid\n\n## Follow-ups\n\nThe audit trail moves behind the history tab.",
			},
		},
		{
			ID: "7d4b1e93-2c58-4a07-9f6e-1b3a5c8d2e64", Type: "event", Key: "Design review — objects table",
			EmbeddingStatus: "processing", Labels: []string{"design"},
			CreatedAt: ago(29 * time.Hour),
		},
		{
			ID: "1e6f8a2c-5d39-4b71-8a04-7c2e9f1b3d58", Type: "task", Key: "Migrate the embeddings queue",
			Status: "blocked", EmbeddingStatus: "failed", Labels: []string{"infra", "blocked"},
			CreatedAt: ago(2 * 24 * time.Hour),
		},
		{
			ID: "4c9a7f31-6e28-4d05-b3a1-8f2c7e5b9d16", Type: "document", Key: "Platform RFC: knowledge graph v2",
			EmbeddingStatus: "embedded", Labels: []string{"architecture", "rfc", "draft"},
			CreatedAt: ago(3 * 24 * time.Hour),
		},
		{
			ID: "8b2d6c47-9a13-4e82-95f0-2c7d4a1b6e39", Type: "person", Key: "Grace Hopper",
			EmbeddingStatus: "dead_letter", Labels: []string{"advisor"},
			CreatedAt: ago(4 * 24 * time.Hour),
		},
		{
			ID: "6f3e1b58-7c94-4d26-a8b3-5e0f2d9c4a71", Type: "project", Key: "Atlas",
			Status: "review", EmbeddingStatus: "embedded", Labels: []string{"internal"},
			CreatedAt: ago(6 * 24 * time.Hour),
		},
		{
			ID: "a1d5e8f2-3b70-4c69-8d24-6f9a1b7c3e05", Type: "task", Key: "Publish schema pack notes",
			Status: "done", EmbeddingStatus: "missing",
			CreatedAt: ago(9 * 24 * time.Hour),
		},
	}

	typeSet := map[string]bool{}
	for _, o := range objects {
		typeSet[o.Type] = true
	}
	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	sort.Strings(types)

	// Mirror the endpoint's created_at ordering so the preview's sort header
	// actually flips the rows (the live list is newest-first by default).
	sort.SliceStable(objects, func(i, j int) bool {
		ti, errI := time.Parse(time.RFC3339, objects[i].CreatedAt)
		tj, errJ := time.Parse(time.RFC3339, objects[j].CreatedAt)
		if errI != nil || errJ != nil {
			return objects[i].CreatedAt > objects[j].CreatedAt
		}
		if order == "asc" {
			return ti.Before(tj)
		}
		return ti.After(tj)
	})

	return objectsPageData{
		Mode:       defaultObjectsSearchMode,
		Order:      order,
		Types:      types,
		BranchID:   "",
		Branches:   []Branch{{ID: "b-plan", Name: "plan/next-gen"}, {ID: "b-objs", Name: "feature/objects-table"}},
		Objects:    objects,
		HasMore:    true,
		NextCursor: "c2FtcGxlLWN1cnNvcg==",
		Stats:      &objectsStats{TotalObjects: 1284, PendingEmbed: 12, FailedEmbed: 3},
		TypeUIByType: map[string]typeUI{
			"task":     {Icon: "check-circle", Color: "#0EA5E9"},
			"person":   {Icon: "user", Color: "#8B5CF6"},
			"company":  {Icon: "briefcase", Color: "#F59E0B"},
			"note":     {Icon: "file-text", Color: "#10B981"},
			"event":    {Icon: "calendar", Color: "#EC4899"},
			"document": {Icon: "book-open", Color: "#6366F1"},
			"project":  {Icon: "layers", Color: "#14B8A6"},
		},
		ActorOptions: []objectActorOption{
			{Type: "user", ID: "u-1", Label: "Dana Whitfield"},
			{Type: "user", ID: "u-2", Label: "Sam Lee"},
			{Type: "agent", ID: "a-1", Label: "memory-agent", Icon: "database", Color: "#2563EB"},
		},
		// Render the picker in its active state so the preview shows an actor's
		// avatar in the closed control and the provenance toggle beside it.
		// Provenance carries the same default a live handler normalizes to: an
		// empty mode left the toggle with no selected segment, so the preview
		// showed a radio group that looked like three unrelated buttons (#121).
		ActorType:  "agent",
		ActorID:    "a-1",
		Provenance: defaultObjectProvenance,
		Preview:    true,
	}
}

// galleryObjectWikiSampleData is the wiki preview's sample object: a note with
// prose, declared properties, relationships and an extraction source, so both
// variants have something real to render. view mirrors the live page's choice
// (cookie or ?view=), so the preview's switch actually switches.
func galleryObjectWikiSampleData(view string) objectWikiPageData {
	rev := 4
	obj := &GraphObject{
		ID: "c8e2d5a1-4b96-4f30-a7d8-9e1c2b6f4a03", Type: "note", Key: "Kickoff meeting notes",
		Status: "in_progress", Labels: []string{"meeting", "q3"}, Version: 4,
		CreatedAt: "2026-11-10T09:12:00Z",
		ActorType: "user", ActorID: "u-2", CredentialKind: "session",
		RevisionCount: &rev,
		Properties: map[string]any{
			"summary":  "Scope, owners and review cadence agreed for the objects-browser rewrite.",
			"content":  "## Decisions\n\nThe table packed four facts into a row and pushed the object's content off-screen. We agreed on a card grid, four columns at desktop, collapsing to one.\n\n## Follow-ups\n\nThe excerpt resolver ships with the grid. The audit trail moves behind the history rail, and the wiki view switch lands as a per-reader preference.",
			"owner":    "Dana Whitfield",
			"priority": "high",
			"due":      "2026-11-20",
		},
	}
	return objectWikiPageData{
		Obj:       obj,
		View:      normalizeObjectWikiView(view),
		TypeLabel: "Note",
		PropDefs: []objectPropertyDef{
			{Name: "owner", Title: "Owner", Type: "string"},
			{Name: "priority", Title: "Priority", Type: "string"},
			{Name: "due", Title: "Due", Type: "date"},
		},
		Sources: []objectSource{{
			DocumentID: "d-1", DocumentName: "Objects browser kickoff.pdf",
			DocumentVersion: "v3", JobID: "job-1", ExtractedAt: "2026-11-10T09:20:00Z",
		}},
		EdgeCount: 3,
		Sections: []objectViewSection{
			{
				Title: "People", Total: 2, Limit: objectWikiSectionLimit,
				Rows: []objectViewItem{
					{RelID: "r1", RelType: "owned_by", Resolved: true, Neighbour: GraphObject{
						ID: "2b7d3e14-8f05-4a6e-b1c9-5d2a8e4f7b90", Type: "person", Key: "Dana Whitfield",
						Properties: map[string]any{"summary": "Staff engineer, owns the browse surface."},
					}},
					{RelID: "r2", RelType: "mentions", Resolved: true, Neighbour: GraphObject{
						ID: "8b2d6c47-9a13-4e82-95f0-2c7d4a1b6e39", Type: "person", Key: "Sam Lee",
						Properties: map[string]any{"summary": "Reviews the card density at narrow widths."},
					}},
				},
			},
			{
				Title: "Tasks", Total: 1, Limit: objectWikiSectionLimit,
				Rows: []objectViewItem{
					{RelID: "r3", RelType: "assigned_to", Resolved: true, Neighbour: GraphObject{
						ID: "9f1c0a2e-6b41-4c8d-9a2f-0e7d3b5a1c22", Type: "task", Key: "Ship the objects browser card grid",
						Properties: map[string]any{"summary": "Replace the table; keep the bulk actions."},
					}},
				},
			},
		},
		RelationCounts: []objectWikiRelationCount{
			{Type: "owned_by", Label: "owned by", Count: 1},
			{Type: "mentions", Label: "mentions", Count: 1},
			{Type: "assigned_to", Label: "assigned to", Count: 1},
		},
		TypeUIByType: map[string]typeUI{
			"task":    {Icon: "check-circle", Color: "#0EA5E9"},
			"person":  {Icon: "user", Color: "#8B5CF6"},
			"note":    {Icon: "file-text", Color: "#10B981"},
			"project": {Icon: "layers", Color: "#14B8A6"},
		},
	}
}
