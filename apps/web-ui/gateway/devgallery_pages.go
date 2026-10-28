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
			Description: "Table layout with row selection and grouped actions, a one-line search/filter toolbar, a single stats strip, and \"Ask the graph\" behind a header dialog. Sample data; selection and the bulk bar are live.",
			Height:      "1080px",
			Body: func(q url.Values) templ.Component {
				return ObjectsPage(objectsBrowserSampleData(q.Get("order")))
			},
		},
		{
			Slug:        "objects-search",
			Name:        "Objects browser — search results",
			Description: "The same table in unified search mode: rows keep their type accent and labels, gain a relevance score, and drop the status/embedding/created columns the unified endpoint cannot fill.",
			Height:      "760px",
			Body:        func(url.Values) templ.Component { return ObjectsPage(objectsBrowserSearchSampleData()) },
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
// ranked hits with relevance scores and no status/embedding/created values,
// which is exactly what the unified endpoint returns. It exercises the table's
// mode-aware columns in the gallery.
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

	objects := []GraphObject{
		{
			ID: "9f1c0a2e-6b41-4c8d-9a2f-0e7d3b5a1c22", Type: "task", Key: "Ship the objects browser table",
			Status: "in_progress", EmbeddingStatus: "embedded", Labels: []string{"q3", "ui"},
			CreatedAt: ago(35 * time.Minute),
		},
		{
			ID: "2b7d3e14-8f05-4a6e-b1c9-5d2a8e4f7b90", Type: "person", Key: "Ada Lovelace",
			EmbeddingStatus: "embedded", Labels: []string{"founder", "advisor"},
			CreatedAt: ago(2 * time.Hour),
		},
		{
			ID: "5a3c9f68-1d27-4e0b-8c45-3f6b9a2d1e77", Type: "company", Key: "Acme Robotics",
			EmbeddingStatus: "embedded", Labels: []string{"customer", "enterprise", "emea"},
			CreatedAt: ago(5 * time.Hour),
		},
		{
			ID: "c8e2d5a1-4b96-4f30-a7d8-9e1c2b6f4a03", Type: "note", Key: "Kickoff meeting notes",
			EmbeddingStatus: "pending", Labels: []string{"meeting"},
			CreatedAt: ago(26 * time.Hour),
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
		ActorType: "agent",
		ActorID:   "a-1",
		Preview:   true,
	}
}
