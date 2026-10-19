package main

import (
	"context"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/a-h/templ"
	"github.com/emergent-company/emergent.memory/apps/web-ui/webui"
	alpine "github.com/emergent-company/go-daisy/components/alpine"
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
	// Interactive marks an entry whose preview needs client scripts (htmx,
	// Alpine, app.js) to be reviewable — dialogs, dropdowns, toasts. The
	// default is render-only: no scripts, so the preview cannot drive the app.
	// An interactive preview still runs in an opaque-origin frame under
	// sandbox="allow-scripts" (never allow-same-origin or allow-forms).
	Interactive bool
	// DefaultVariant optionally names the story variant the gallery's preview
	// iframe opens with, e.g. "open" to show an overlay in its open state. Empty
	// selects the default Render closure.
	DefaultVariant string
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
// resolved from the generated graph, plus on-demand L0 go-daisy entries for
// every go-daisy component the app renders — whether by a cataloged entry or by
// a page/page-local helper.
func componentGallery() []GalleryEntry {
	seed := gallerySeed()
	seen := map[string]bool{}
	result := make([]GalleryEntry, 0, len(seed)+64)
	for _, e := range seed {
		seen[e.ID] = true
		result = append(result, e)
	}
	// Add an L0 entry on demand for every go-daisy component the gateway renders
	// — whether by a cataloged entry or by a page/page-local helper. The graph
	// records a go-daisy reference per render call site, so the union of its
	// values is the complete set of primitives the app renders. Sorted so the
	// listing order is deterministic.
	var daisyIDs []string
	for _, ids := range graphDaisyComponents {
		daisyIDs = append(daisyIDs, ids...)
	}
	for _, id := range sortedUnique(daisyIDs) {
		if seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, daisyEntry(id))
	}
	// The set of ids a uses edge may resolve to.
	catalog := make(map[string]bool, len(result))
	for _, e := range result {
		catalog[e.ID] = true
	}
	// Resolve uses/used-by from the generated graph, folding in the manual
	// escape hatch for dynamic renders the static walk cannot see. Uses edges
	// are filtered to catalog ids so a same-package helper, page, or package
	// leaf the generator recorded never surfaces as a dangling target (spec R3).
	for i := range result {
		e := &result[i]
		e.Uses = filterToCatalog(sortedUnique(append(graphUses[e.ID], e.AdditionalUses...)), catalog)
		e.UsedBy = sortedUnique(graphUsedBy[e.ID])
	}
	return result
}

// filterToCatalog drops uses targets that are not catalog entries.
func filterToCatalog(uses []string, catalog map[string]bool) []string {
	out := make([]string, 0, len(uses))
	for _, u := range uses {
		if catalog[u] {
			out = append(out, u)
		}
	}
	return out
}

// daisyEntry builds the on-demand L0 entry for a go-daisy component that the
// app renders but that has no curated seed entry. When the id has an authored
// fixture it fills in the render, props type, description, and optional
// interactive/story fields; otherwise it keeps the empty render so an
// un-curated primitive is flagged "needs fixture" rather than silently showing
// a blank preview.
func daisyEntry(id string) GalleryEntry {
	name, slug := daisyDisplay(id)
	e := GalleryEntry{
		ID:          id,
		Slug:        slug,
		Name:        name,
		Layer:       LayerDaisy,
		Category:    "go-daisy",
		Subcategory: "primitive",
		SourceFile:  "<go-daisy>",
		PropsType:   "—", // no props type is known without a fixture
		Description: "go-daisy primitive rendered by the app",
		Render:      emptyRender(),
	}
	if spec, ok := daisyFixture(id); ok {
		e.Render = spec.Render
		e.PropsType = spec.PropsType
		e.Description = spec.Description
		e.Interactive = spec.Interactive
		e.DefaultVariant = spec.DefaultVariant
		e.Stories = spec.Stories
	}
	return e
}

// daisyDisplay derives the human-readable name and URL-safe slug for a go-daisy
// id. A package-qualified id ("go-daisy/<pkg>#<Name>", used when a bare name
// collides across packages) displays as "<pkg>.<Name>" with slug "<pkg>-<name>";
// a bare id passes through unchanged.
func daisyDisplay(id string) (name, slug string) {
	if rest, ok := strings.CutPrefix(id, "go-daisy/"); ok {
		if pkg, n, ok := strings.Cut(rest, "#"); ok {
			return pkg + "." + n, slugify(pkg) + "-" + slugify(n)
		}
	}
	return id, slugify(id)
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

// previewSrc is the preview iframe's src: the entry's preview document, with the
// DefaultVariant story appended as a query param when set.
func (e GalleryEntry) previewSrc() string {
	return e.previewURL(nil)
}

// previewStandaloneSrc is the "Open in new tab" URL: the same route in report
// mode (standalone=1), preserving the entry's DefaultVariant so the opened tab
// shows the same story as the embedded preview.
func (e GalleryEntry) previewStandaloneSrc() string {
	return e.previewURL(url.Values{"standalone": {"1"}})
}

// previewURL builds the preview route URL with the DefaultVariant story plus any
// extra query values, encoded once so both previewSrc and previewStandaloneSrc
// share the same shape.
func (e GalleryEntry) previewURL(extra url.Values) string {
	q := url.Values{}
	if e.DefaultVariant != "" {
		q.Set("variant", e.DefaultVariant)
	}
	for k, vs := range extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	src := "/dev/components/preview/" + e.Slug
	if enc := q.Encode(); enc != "" {
		src += "?" + enc
	}
	return src
}

// previewClientScripts returns the client-asset script block for an interactive
// preview document, in the shell's canonical load order (ui.templ): htmx, the
// guarded htmx v2-compat config, the htmx/Alpine compatibility extension,
// deferred Alpine, and app.js. It is a hand-written factory rather than a templ
// component so the generated dependency graph is unchanged: the graph scanner
// walks package-level templ definitions and same-package component callees, not
// methods, and the templ call site (entry.previewClientScripts()) is a method
// expression the scanner skips.
func (e GalleryEntry) previewClientScripts() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, `<script src="`+webui.AssetPath("/js/htmx.min.js")+`"></script>`); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<script>
			if (window.htmx) {
				htmx.config.implicitInheritance = true;
				htmx.config.noSwap = [204, 304, "4xx", "5xx"];
				htmx.config.defaultTimeout = 0;
			}
		</script>`); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<script src="`+webui.AssetPath("/js/hx-alpine-compat.js")+`"></script>`); err != nil {
			return err
		}
		if err := alpine.Tag().Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, `<script src="`+webui.AssetPath("/js/app.js")+`"></script>`)
		return err
	})
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
// the handler 404s when the flag is off so both states are httptest-able. The
// page is rendered as its own chromeless document (no app shell): the gallery
// body is composed into ComponentGalleryDocument and served via RenderPage.
func (s *Server) uiComponentGallery(c echo.Context) error {
	if !s.cfg.ComponentGallery {
		return echo.ErrNotFound
	}
	render.RenderPage(c.Response().Writer, c.Request(), ComponentGalleryDocument(ComponentGalleryPage(componentGallery(), adoptionGaps()), s.cfg.FeedbackOverlayURL))
	return nil
}

// uiComponentPreview serves one component's preview document. A `standalone=1`
// query param switches it to report mode: the top-level document opened by the
// card's "Open in new tab" control, which loads the feedback overlay. The iframe
// embed never sets the param, so it stays script-free and chrome-free.
func (s *Server) uiComponentPreview(c echo.Context) error {
	if !s.cfg.ComponentGallery {
		return echo.ErrNotFound
	}
	entry, ok := galleryEntryBySlug(c.Param("slug"))
	if !ok {
		return echo.ErrNotFound
	}
	standalone := c.QueryParam("standalone") == "1"
	render.RenderPage(c.Response().Writer, c.Request(), componentPreviewDoc(entry, c.QueryParam("variant"), standalone, s.cfg.FeedbackOverlayURL))
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
	// dataAttrRE matches a data-* marker at an attribute boundary: either the
	// attribute form `data-foo="…"` or the Go map-key form `"data-foo": "…"`.
	// The \b word boundary rejects `metadata-`-style substrings.
	dataAttrRE = regexp.MustCompile(`\b(data-[a-zA-Z0-9-]+)(?:\s*=|"\s*:)`)
)

// nonWiringDataAttrs are data-* markers that are not client wiring: the test
// anchor, the daisyUI theme/tooltip helpers, and Stimulus's data-controller
// (reported separately). They are skipped so they never surface as external deps.
var nonWiringDataAttrs = map[string]bool{
	"data-testid":     true,
	"data-theme":      true,
	"data-tip":        true,
	"data-controller": true,
}

// scanClientWiring reads a component's source file and reports the client-wiring
// markers in its body. Best-effort and file-granular: the whole source file is
// scanned, so co-located components in one file share a single wiring list.
// SourceFile is read relative to the process working directory, so it resolves
// only when the gateway runs from the source tree (task dev); a binary running
// with a different CWD (run-e2e.sh, Docker) finds no file and this returns nil.
// That is acceptable — the gallery is a dev tool — and keeps zero new deps.
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
// the distinct custom data-* markers, skipping the non-wiring data-* attributes.
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
	for _, m := range dataAttrRE.FindAllStringSubmatch(src, -1) {
		name := m[1]
		if nonWiringDataAttrs[name] {
			continue
		}
		out = append(out, name)
	}
	return sortedUnique(out)
}
