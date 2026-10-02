package main

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/a-h/templ"
	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// GalleryLayer is a component's layering tier. The catalog holds L0 go-daisy
// primitives, L1 domain adapters, and L2 app composites; L3 page-local helpers
// and L4 pages are call sites, not catalog entries.
type GalleryLayer string

const (
	LayerDaisy     GalleryLayer = "L0"
	LayerAdapter   GalleryLayer = "L1"
	LayerComposite GalleryLayer = "L2"
)

// GalleryEntry is one catalog entry. ID matches the generator's canonical id
// (the Go identifier as written). Render is the fixture closure — a curated
// fixture for covered components, an authored zero-value closure otherwise —
// and Stories is an optional set of variant fixtures selected by query param.
type GalleryEntry struct {
	ID             string
	Slug           string
	Name           string
	Layer          GalleryLayer
	Category       string
	Subcategory    string
	SourceFile     string
	PropsType      string
	Description    string
	Uses           []string
	UsedBy         []string
	AdditionalUses []string
	Render         func() templ.Component
	Stories        map[string]func() templ.Component
}

// GalleryDependency is a single row in a component's dependency panel.
type GalleryDependency struct {
	Kind   string // "uses", "used-by", or "external"
	Target string // canonical id, go-daisy import path, or client-wiring marker
	Label  string // human-readable description
}

// componentGallery returns the full catalog: the hand-maintained seed (the
// components/ package entries plus a curated go-daisy subset) with uses/used-by
// resolved from the generated graph, plus on-demand L0 go-daisy entries for any
// go-daisy component the listed entries render.
func componentGallery() []GalleryEntry {
	seed := gallerySeed()
	seen := map[string]bool{}
	result := make([]GalleryEntry, 0, len(seed)+16)
	for _, e := range seed {
		seen[e.ID] = true
		result = append(result, e)
	}
	// Add go-daisy L0 entries on demand for every referenced go-daisy component
	// that is not already cataloged.
	for i := range result {
		for _, id := range graphDaisyComponents[result[i].ID] {
			if seen[id] {
				continue
			}
			seen[id] = true
			result = append(result, daisyEntry(id))
		}
	}
	// Resolve uses/used-by from the generated graph, folding in the manual
	// escape hatch for dynamic renders the static walk cannot see.
	for i := range result {
		e := &result[i]
		e.Uses = sortedUnique(append(graphUses[e.ID], e.AdditionalUses...))
		e.UsedBy = sortedUnique(graphUsedBy[e.ID])
	}
	return result
}

// daisyEntry builds the on-demand L0 entry for a go-daisy component that the
// app renders but that has no curated fixture. Its render is empty, so it is
// flagged "needs fixture" rather than silently showing a blank preview.
func daisyEntry(id string) GalleryEntry {
	return GalleryEntry{
		ID:          id,
		Slug:        slugify(id),
		Name:        id,
		Layer:       LayerDaisy,
		Category:    "go-daisy",
		Subcategory: "primitive",
		SourceFile:  "<go-daisy>",
		PropsType:   "",
		Description: "go-daisy primitive rendered by the app",
		Render:      emptyRender(),
	}
}

// galleryEntryBySlug finds a catalog entry by slug, for the preview route.
func galleryEntryBySlug(slug string) (GalleryEntry, bool) {
	for _, e := range componentGallery() {
		if e.Slug == slug {
			return e, true
		}
	}
	return GalleryEntry{}, false
}

// renderFor selects the fixture closure for a story variant, falling back to
// the default Render closure.
func (e GalleryEntry) renderFor(variant string) templ.Component {
	if variant != "" && e.Stories != nil {
		if r, ok := e.Stories[variant]; ok {
			return r()
		}
	}
	return e.Render()
}

// NeedsFixture reports whether the entry's default render is empty (no
// non-whitespace text and no element) or panics. A panic on a nil slot is
// caught so the gallery never crashes rendering an un-fixtured component.
func (e GalleryEntry) NeedsFixture() bool {
	html, panicked := renderSafe(e.Render())
	return panicked || isEmptyHTML(html)
}

// ExternalDeps returns the component's external dependencies: the go-daisy
// packages it renders (from the generated graph) plus any client wiring (Alpine
// x-*, Stimulus data-controller, htmx hx-*, and custom data-* markers) detected
// by scanning its source body.
func (e GalleryEntry) ExternalDeps() []string {
	deps := append([]string{}, graphDaisyPackages[e.ID]...)
	deps = append(deps, scanClientWiring(e.SourceFile)...)
	return sortedUnique(deps)
}

// uiComponentGallery serves the catalog page. The route is always registered;
// the handler 404s when the flag is off so both states are httptest-able.
func (s *Server) uiComponentGallery(c echo.Context) error {
	if !s.cfg.ComponentGallery {
		return echo.ErrNotFound
	}
	return s.page(c, pageTitle("Component gallery"), ComponentGalleryPage(componentGallery()))
}

// uiComponentPreview serves one component's isolated preview document.
func (s *Server) uiComponentPreview(c echo.Context) error {
	if !s.cfg.ComponentGallery {
		return echo.ErrNotFound
	}
	entry, ok := galleryEntryBySlug(c.Param("slug"))
	if !ok {
		return echo.ErrNotFound
	}
	render.RenderPage(c.Response().Writer, c.Request(), componentPreviewDoc(entry, c.QueryParam("variant")))
	return nil
}

// --- rendering helpers ---

// renderSafe renders c into a string, recovering from a panic (a nil-slot
// fixture panics rather than returning an error). The second return reports
// whether a panic occurred.
func renderSafe(c templ.Component) (html string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	var b strings.Builder
	if c != nil {
		_ = c.Render(context.Background(), &b)
	}
	return b.String(), false
}

// isEmptyHTML reports whether html has no non-whitespace text and no element.
func isEmptyHTML(html string) bool {
	return !strings.ContainsAny(html, "<") && strings.TrimSpace(html) == ""
}

// emptyRender returns a closure that renders nothing, used as the default
// render for on-demand daisy entries without a curated fixture.
func emptyRender() func() templ.Component {
	return func() templ.Component { return templ.NopComponent }
}

// sortedUnique de-duplicates and sorts a string slice, dropping empties.
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// walkGraph returns the set of nodes reachable from roots via edges, including
// the roots themselves, using a visited set so cycles terminate without
// topological sorting.
func walkGraph(roots []string, edges map[string][]string) []string {
	seen := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		if seen[n] {
			return
		}
		seen[n] = true
		for _, next := range edges[n] {
			visit(next)
		}
	}
	for _, r := range roots {
		visit(r)
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// slugify converts a PascalCase identifier to a kebab-case slug.
func slugify(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// galleryLayerOrder is the fixed display order of the catalog's layers.
func galleryLayerOrder() []GalleryLayer {
	return []GalleryLayer{LayerDaisy, LayerAdapter, LayerComposite}
}

// layerLabel returns a human-readable label for a layer.
func layerLabel(l GalleryLayer) string {
	switch l {
	case LayerDaisy:
		return "L0 — go-daisy primitives"
	case LayerAdapter:
		return "L1 — domain adapters"
	case LayerComposite:
		return "L2 — app composites"
	default:
		return string(l)
	}
}

// entriesInLayer filters entries to those of the given layer, preserving order.
func entriesInLayer(entries []GalleryEntry, layer GalleryLayer) []GalleryEntry {
	out := make([]GalleryEntry, 0, len(entries))
	for _, e := range entries {
		if e.Layer == layer {
			out = append(out, e)
		}
	}
	return out
}

// joinText renders a string slice as a comma-separated string for the gallery
// dependency panels.
func joinText(ss []string) string {
	return strings.Join(ss, ", ")
}

// --- client-wiring scan ---

var (
	alpineAttrRE = regexp.MustCompile(`\sx-[a-zA-Z0-9:_-]+\s*=`)
	htmxAttrRE   = regexp.MustCompile(`\shx-[a-zA-Z0-9-]+\s*=`)
	dataAttrRE   = regexp.MustCompile(`data-[a-zA-Z0-9-]+`)
)

// scanClientWiring reads a component's source file and reports the client-wiring
// markers in its body. Best-effort: a missing source file (binary-only deploy)
// yields no markers rather than an error.
func scanClientWiring(sourceFile string) []string {
	if sourceFile == "" || strings.HasPrefix(sourceFile, "<") {
		return nil
	}
	data, err := os.ReadFile(sourceFile)
	if err != nil {
		return nil
	}
	return clientWiringMarkers(string(data))
}

// clientWiringMarkers classifies the client-wiring markers in a source body:
// Alpine x-* attributes, Stimulus data-controller, htmx hx-* attributes, and
// the distinct custom data-* markers (excluding the daisyUI tooltip helper).
func clientWiringMarkers(src string) []string {
	var out []string
	if alpineAttrRE.MatchString(src) {
		out = append(out, "alpine (x-*)")
	}
	if strings.Contains(src, "data-controller") {
		out = append(out, "stimulus (data-controller)")
	}
	if htmxAttrRE.MatchString(src) {
		out = append(out, "htmx (hx-*)")
	}
	for _, m := range dataAttrRE.FindAllString(src, -1) {
		if m == "data-controller" || m == "data-tip" {
			continue
		}
		out = append(out, m)
	}
	return sortedUnique(out)
}
