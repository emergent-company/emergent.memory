package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/a-h/templ"
	components "github.com/emergent-company/emergent.memory/apps/web-ui/components"
	"github.com/labstack/echo/v4"
)

// renderComp renders a component to a string for assertion.
func renderComp(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// --- 1.2 registry integrity ---

func TestComponentGalleryRegistry(t *testing.T) {
	entries := componentGallery()
	bySlug := map[string]string{}
	byID := map[string]GalleryEntry{}
	for _, e := range entries {
		byID[e.ID] = e
		if prev, ok := bySlug[e.Slug]; ok {
			t.Errorf("duplicate slug %q for %s and %s", e.Slug, prev, e.ID)
		}
		bySlug[e.Slug] = e.ID
		if e.ID == "" || e.Name == "" || e.Layer == "" || e.SourceFile == "" || e.Description == "" || e.PropsType == "" {
			t.Errorf("%s: incomplete entry (id=%q name=%q layer=%q source=%q props=%q desc=%q)",
				e.ID, e.ID, e.Name, e.Layer, e.SourceFile, e.PropsType, e.Description)
		}
		if e.Render == nil {
			t.Errorf("%s: nil Render closure", e.ID)
		}
	}

	// Known components present, single-page helper absent.
	for _, id := range []string{"PanelCard", "StatusBadge", "MetaGrid", "ListRow", "TableCard", "SecretRevealModal"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing known component %q", id)
		}
	}
	if _, ok := byID["BoardColumns"]; ok {
		t.Error("BoardColumns is a single-page exported helper and must be absent")
	}
}

// --- 1.x / R3: every uses target resolves to a catalog entry ---

func TestComponentGalleryUsesResolveToCatalog(t *testing.T) {
	entries := componentGallery()
	catalog := map[string]bool{}
	for _, e := range entries {
		catalog[e.ID] = true
	}
	for _, e := range entries {
		for _, u := range e.Uses {
			if !catalog[u] {
				t.Errorf("%s: Uses target %q is not a catalog entry", e.ID, u)
			}
		}
	}
}

// --- on-demand daisy entry shape ---

func TestDaisyEntry(t *testing.T) {
	e := daisyEntry("FormControl")
	if e.PropsType == "" {
		t.Error("daisyEntry must set a non-empty PropsType")
	}
	if e.Layer != LayerDaisy || e.Category != "go-daisy" {
		t.Errorf("daisyEntry layer/category = %q/%q, want L0/go-daisy", e.Layer, e.Category)
	}
	if e.Slug != "form-control" {
		t.Errorf("daisyEntry slug = %q, want %q", e.Slug, "form-control")
	}
	if e.Render == nil {
		t.Error("daisyEntry must set a Render closure")
	}
	if !e.NeedsFixture() {
		t.Error("daisyEntry with an empty render should be flagged needs fixture")
	}
}

// --- 1.3 fixtures are inert and referenced ---

func TestFixturesInert(t *testing.T) {
	for _, e := range gallerySeed() {
		html, panicked := renderSafe(e.Render())
		if panicked {
			t.Errorf("%s: fixture panicked", e.ID)
			continue
		}
		for _, marker := range []string{`action="http`, "hx-post=", "hx-put=", "hx-patch=", "hx-delete="} {
			if strings.Contains(html, marker) {
				t.Errorf("%s: fixture is not inert (contains %q)", e.ID, marker)
			}
		}
	}
}

// --- 1.4 default render + needs-fixture flag ---

func TestNeedsFixtureFlag(t *testing.T) {
	empty := GalleryEntry{ID: "empty", Render: func() templ.Component { return templ.NopComponent }}
	if !empty.NeedsFixture() {
		t.Error("empty render should be flagged needs fixture")
	}

	// ListRow dereferences its leading component; a nil slot panics.
	panicR := GalleryEntry{ID: "panic", Render: func() templ.Component {
		return components.ListRow(templ.SafeURL("#"), nil, "", "")
	}}
	if !panicR.NeedsFixture() {
		t.Error("nil-slot panic render should be flagged needs fixture")
	}

	ok := GalleryEntry{ID: "ok", Render: func() templ.Component { return components.EmptyDash() }}
	if ok.NeedsFixture() {
		t.Error("non-empty render should not be flagged needs fixture")
	}
}

// --- 1.5 render smoke ---

func TestGalleryRenderSmoke(t *testing.T) {
	for _, e := range componentGallery() {
		html, panicked := renderSafe(e.Render())
		want := panicked || isEmptyHTML(html)
		if got := e.NeedsFixture(); got != want {
			t.Errorf("%s: NeedsFixture() = %v, want %v (panicked=%v, empty=%v)", e.ID, got, want, panicked, isEmptyHTML(html))
		}
	}
}

// --- 2.4 graph edges ---

func TestComponentGalleryKnownEdges(t *testing.T) {
	// Forward edge: a shared composite -> its go-daisy primitive.
	if !containsStr(graphUses["PanelCard"], "CardRaw") {
		t.Errorf("graphUses[PanelCard] = %v, missing CardRaw", graphUses["PanelCard"])
	}
	// Used-by: a components-package entry lists at least one page caller.
	hasPage := false
	for _, caller := range graphUsedBy["PanelCard"] {
		if strings.HasSuffix(caller, "Page") {
			hasPage = true
			break
		}
	}
	if !hasPage {
		t.Errorf("graphUsedBy[PanelCard] = %v, want a package-main page caller", graphUsedBy["PanelCard"])
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// --- 2.6 cycle tolerance ---

func TestWalkGraphToleratesCycles(t *testing.T) {
	edges := map[string][]string{"A": {"B"}, "B": {"A"}}
	got := walkGraph([]string{"A"}, edges)
	want := []string{"A", "B"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("walkGraph = %v, want %v", got, want)
	}
}

// galleryTransitiveUses is the rendering-side consumer of walkGraph: it counts
// the transitive uses closure shown in the dependency summary.
func TestGalleryTransitiveUses(t *testing.T) {
	entry, ok := galleryEntryBySlug("panel-card")
	if !ok {
		t.Fatal("panel-card entry not found")
	}
	if n := galleryTransitiveUses(entry); n < 2 {
		t.Errorf("galleryTransitiveUses(panel-card) = %d, want >= 2", n)
	}
}

// --- 2.5 staleness check ---

func TestGraphStaleCheck(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "graph_gen.go")
	adoptionTmp := filepath.Join(t.TempDir(), "adoption_gen.go")
	cmd := exec.Command("go", "run", "./cmd/componentgraph", "-out", tmp, "-adoption-out", adoptionTmp)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./cmd/componentgraph: %v\n%s", err, out)
	}
	got, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	want, err := os.ReadFile("devgallery_graph_gen.go")
	if err != nil {
		t.Fatalf("read committed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("devgallery_graph_gen.go is stale; run `templ generate ./...` then `go generate ./...`")
	}
}

func TestAdoptionStaleCheck(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "adoption_gen.go")
	graphTmp := filepath.Join(t.TempDir(), "graph_gen.go")
	cmd := exec.Command("go", "run", "./cmd/componentgraph", "-out", graphTmp, "-adoption-out", tmp)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./cmd/componentgraph: %v\n%s", err, out)
	}
	got, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	want, err := os.ReadFile("devgallery_adoption_gen.go")
	if err != nil {
		t.Fatalf("read committed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("devgallery_adoption_gen.go is stale; run `templ generate ./...` then `go generate ./...`")
	}
}

// --- 4.2 external-dep scan ---

func TestClientWiringMarkers(t *testing.T) {
	got := scanClientWiring("components/secret.templ")
	for _, want := range []string{"htmx (hx-*)", "data-dialog-autoopen", "data-copy-target"} {
		if !containsStr(got, want) {
			t.Errorf("scanClientWiring(secret.templ) = %v, missing %q", got, want)
		}
	}
	for _, unwanted := range []string{"data-testid", "data-theme", "data-controller"} {
		if containsStr(got, unwanted) {
			t.Errorf("scanClientWiring(secret.templ) = %v, must not include non-wiring %q", got, unwanted)
		}
	}
}

func TestClientWiringMarkersSkipsNonWiring(t *testing.T) {
	src := `<div metadata-foo="1" data-testid="x" data-theme="dark" data-tip="tip" data-copy-target="#a" data-dialog-autoopen="true"></div>`
	got := clientWiringMarkers(src)
	for _, unwanted := range []string{"metadata-foo", "data-testid", "data-theme", "data-tip"} {
		if containsStr(got, unwanted) {
			t.Errorf("clientWiringMarkers = %v, must not include %q", got, unwanted)
		}
	}
	for _, want := range []string{"data-copy-target", "data-dialog-autoopen"} {
		if !containsStr(got, want) {
			t.Errorf("clientWiringMarkers = %v, missing %q", got, want)
		}
	}
}

// --- 3.3 story variants ---

func TestPreviewVariantChangesOutput(t *testing.T) {
	entry, ok := galleryEntryBySlug("status-badge")
	if !ok {
		t.Fatal("status-badge entry not found")
	}
	def := renderComp(t, componentPreviewDoc(entry, ""))
	variant := renderComp(t, componentPreviewDoc(entry, "warning"))
	if def == variant {
		t.Error("variant story did not change the rendered preview")
	}
	if !strings.Contains(def, "Ready") {
		t.Errorf("default preview missing expected content: %s", def)
	}
	if !strings.Contains(variant, "Pending") {
		t.Errorf("warning variant preview missing expected content: %s", variant)
	}
}

// --- interactive previews (scripted frame for overlay components) ---

// TestPreviewIsRenderOnlyByDefault pins the default: a non-interactive entry's
// preview document loads no scripts and its iframe stays under sandbox="".
func TestPreviewIsRenderOnlyByDefault(t *testing.T) {
	entry, ok := galleryEntryBySlug("panel-card")
	if !ok {
		t.Fatal("panel-card entry not found")
	}
	if entry.Interactive {
		t.Fatal("panel-card must stay render-only")
	}

	doc := renderComp(t, componentPreviewDoc(entry, ""))
	if strings.Contains(doc, "<script") {
		t.Errorf("render-only preview must load no scripts:\n%s", doc)
	}
	if !strings.Contains(doc, `data-testid="preview-canvas"`) {
		t.Error("preview missing canvas test id")
	}
	if !strings.Contains(doc, "/css/app.css") {
		t.Error("preview missing the stylesheet link")
	}

	card := renderComp(t, galleryEntryCard(entry))
	if !strings.Contains(card, `sandbox=""`) {
		t.Errorf("render-only iframe should carry sandbox=\"\", got:\n%s", card)
	}
	if strings.Contains(card, "allow-scripts") {
		t.Error("render-only iframe must not enable scripts")
	}
	if strings.Contains(card, "data-interactive") {
		t.Error("render-only iframe must not be marked interactive")
	}
}

// TestInteractivePreviewLoadsScriptsInOrder pins the canonical shell load order
// for an interactive preview.
func TestInteractivePreviewLoadsScriptsInOrder(t *testing.T) {
	entry, ok := galleryEntryBySlug("secret-reveal-modal")
	if !ok {
		t.Fatal("secret-reveal-modal entry not found")
	}
	if !entry.Interactive {
		t.Fatal("secret-reveal-modal must be marked interactive")
	}

	doc := renderComp(t, componentPreviewDoc(entry, "open"))
	if !strings.Contains(doc, "window.htmx") {
		t.Errorf("interactive preview missing guarded htmx config:\n%s", doc)
	}
	order := []string{"htmx.min.js", "hx-alpine-compat.js", "alpine.js", "app.js"}
	last := -1
	for _, name := range order {
		i := strings.Index(doc, name)
		if i < 0 {
			t.Fatalf("interactive preview missing %s:\n%s", name, doc)
		}
		if i <= last {
			t.Fatalf("%s is out of order in the preview document", name)
		}
		last = i
	}
}

// TestInteractivePreviewSandbox pins the iframe sandbox: allow-scripts only,
// never allow-same-origin or allow-forms, on the entry card and the whole page.
func TestInteractivePreviewSandbox(t *testing.T) {
	entry, ok := galleryEntryBySlug("secret-reveal-modal")
	if !ok {
		t.Fatal("secret-reveal-modal entry not found")
	}
	card := renderComp(t, galleryEntryCard(entry))
	if !strings.Contains(card, `sandbox="allow-scripts"`) {
		t.Errorf("interactive iframe should carry sandbox=\"allow-scripts\", got:\n%s", card)
	}
	if !strings.Contains(card, `data-interactive="true"`) {
		t.Error("interactive iframe missing data-interactive=\"true\"")
	}
	for _, forbidden := range []string{"allow-same-origin", "allow-forms"} {
		if strings.Contains(card, forbidden) {
			t.Errorf("interactive iframe must never carry %q", forbidden)
		}
	}

	// The rendered gallery page as a whole never emits a more permissive sandbox.
	_, e := newDevGalleryServer(true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("gallery = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-interactive="true"`) {
		t.Error("gallery page missing the interactive preview iframe")
	}
	for _, forbidden := range []string{"allow-same-origin", "allow-forms"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("gallery page must never emit %q", forbidden)
		}
	}
}

// TestInteractivePreviewOpenStory pins how the open state is selected: the open
// story sets AutoOpen, the default render stays closed, and the card links the
// iframe at the open variant.
func TestInteractivePreviewOpenStory(t *testing.T) {
	entry, ok := galleryEntryBySlug("secret-reveal-modal")
	if !ok {
		t.Fatal("secret-reveal-modal entry not found")
	}
	openDoc := renderComp(t, componentPreviewDoc(entry, "open"))
	defDoc := renderComp(t, componentPreviewDoc(entry, ""))
	if !strings.Contains(openDoc, "data-dialog-autoopen") {
		t.Errorf("open story should mark the dialog for auto-open:\n%s", openDoc)
	}
	if strings.Contains(defDoc, "data-dialog-autoopen") {
		t.Error("default render should stay closed (no data-dialog-autoopen)")
	}
	if entry.DefaultVariant != "open" {
		t.Errorf("DefaultVariant = %q, want %q", entry.DefaultVariant, "open")
	}
	card := renderComp(t, galleryEntryCard(entry))
	if !strings.Contains(card, "/dev/components/preview/secret-reveal-modal?variant=open") {
		t.Errorf("card iframe src should select the open variant, got:\n%s", card)
	}
}

// --- 5.2 routing + gating ---

func newDevGalleryServer(flag bool) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{ComponentGallery: flag}, memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/dev/components", s.uiComponentGallery)
	e.GET("/dev/components/preview/:slug", s.uiComponentPreview)
	return s, e
}

func TestUIComponentGalleryFlagOff(t *testing.T) {
	_, e := newDevGalleryServer(false)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("flag-off gallery = %d, want 404", rec.Code)
	}
}

func TestUIComponentGalleryRenders(t *testing.T) {
	_, e := newDevGalleryServer(true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("gallery = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="component-gallery"`, "PanelCard", "StatusBadge"} {
		if !strings.Contains(body, want) {
			t.Errorf("gallery body missing %q", want)
		}
	}
}

func TestUIComponentPreviewKnown(t *testing.T) {
	_, e := newDevGalleryServer(true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components/preview/panel-card", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Sample panel content") {
		t.Errorf("preview body missing component markup: %s", body)
	}
	if !strings.Contains(body, `data-testid="preview-canvas"`) {
		t.Errorf("preview body missing canvas test id")
	}
}

func TestUIComponentPreviewUnknown(t *testing.T) {
	_, e := newDevGalleryServer(true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components/preview/does-not-exist", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown preview = %d, want 404", rec.Code)
	}
}

func TestUIComponentPreviewFlagOff(t *testing.T) {
	_, e := newDevGalleryServer(false)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components/preview/panel-card", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("flag-off preview = %d, want 404", rec.Code)
	}
}

func TestProjectScopePathExemptsDev(t *testing.T) {
	for _, p := range []string{"/dev", "/dev/", "/dev/components", "/dev/components/preview/panel-card"} {
		if projectScopePath(p) {
			t.Errorf("projectScopePath(%q) = true, want false (dev routes are not project-scoped)", p)
		}
	}
	if !projectScopePath("/objects") {
		t.Error("projectScopePath(/objects) = false, want true")
	}
}

// --- 5.3 no sidebar nav entry ---

func TestNoGalleryNavLink(t *testing.T) {
	for _, group := range sidebarGroups("", false) {
		for _, item := range group.Items {
			if strings.Contains(item.Href, "/dev/") {
				t.Errorf("sidebar navigation contains a gallery link %q", item.Href)
			}
		}
	}
}

// --- adoption gaps ---

func TestAdoptionGapsDeterministic(t *testing.T) {
	first := adoptionGaps()
	for range 3 {
		again := adoptionGaps()
		if !reflect.DeepEqual(first, again) {
			t.Fatal("adoptionGaps is not deterministic across calls")
		}
	}
	// Findings are already sorted by (File, Line, Marker); verify each summary
	// is internally ordered and that files appear in first-seen order.
	for _, g := range first {
		for i := 1; i < len(g.Findings); i++ {
			prev, cur := g.Findings[i-1], g.Findings[i]
			if prev.Line > cur.Line || (prev.Line == cur.Line && prev.Marker > cur.Marker) {
				t.Errorf("%s: findings out of order at index %d", g.File, i)
			}
		}
	}
}

// adoptionFixtures returns a synthetic finding set so the allowlist and
// aggregation tests are independent of the generated data (and pass on a clean
// tree, where there are no findings at all). Both files carry two/one findings.
func adoptionFixtures() []AdoptionFinding {
	return []AdoptionFinding{
		{File: "some_page.templ", Line: 10, Marker: "class:badge", Owners: []string{"Badge", "StatusBadge"}},
		{File: "some_page.templ", Line: 12, Marker: "class:btn", Owners: []string{"Button", "SaveChangesButton"}},
		{File: "other_page.templ", Line: 3, Marker: "tag:dialog", Owners: []string{"Dialog", "SecretRevealModal"}},
	}
}

func TestAdoptionGapsAggregation(t *testing.T) {
	origFindings := adoptionFindings
	adoptionFindings = adoptionFixtures()
	t.Cleanup(func() { adoptionFindings = origFindings })

	gaps := adoptionGaps()
	if len(gaps) != 2 {
		t.Fatalf("adoptionGaps files = %d, want 2", len(gaps))
	}
	if gaps[0].File != "some_page.templ" || gaps[0].Count != 2 {
		t.Errorf("first gap = %+v, want some_page.templ count 2", gaps[0])
	}
	if gaps[1].File != "other_page.templ" || gaps[1].Count != 1 {
		t.Errorf("second gap = %+v, want other_page.templ count 1", gaps[1])
	}
	if n := adoptionFindingCount(gaps); n != 3 {
		t.Errorf("adoptionFindingCount = %d, want 3", n)
	}
}

func TestAdoptionAllowlist(t *testing.T) {
	origFindings := adoptionFindings
	origAllow := adoptionAllowlist
	adoptionFindings = adoptionFixtures()
	defer func() {
		adoptionFindings = origFindings
		adoptionAllowlist = origAllow
	}()

	// A bare marker name (the adoption:ignore form) is exempted; the file
	// survives with its other finding.
	adoptionAllowlist = map[string][]string{"some_page.templ": {"badge"}}
	gaps := adoptionGaps()
	if len(gaps) != 2 || gaps[0].File != "some_page.templ" || gaps[0].Count != 1 {
		t.Fatalf("after bare-marker allowlist gaps = %+v, want 2 with some_page.templ count 1", gaps)
	}
	if gaps[0].Findings[0].Marker != "class:btn" {
		t.Errorf("remaining some_page finding = %q, want class:btn", gaps[0].Findings[0].Marker)
	}

	// The fully-qualified marker form ("class:<name>") matches too.
	adoptionAllowlist = map[string][]string{"some_page.templ": {"class:badge"}}
	gaps = adoptionGaps()
	if len(gaps) != 2 || gaps[0].File != "some_page.templ" || gaps[0].Findings[0].Marker != "class:btn" {
		t.Errorf("after class:badge allowlist gaps = %+v, want only class:btn on some_page.templ", gaps)
	}

	// Exempting every marker drops the file entirely.
	adoptionAllowlist = map[string][]string{"some_page.templ": {"badge", "btn"}}
	for _, g := range adoptionGaps() {
		if g.File == "some_page.templ" {
			t.Errorf("fully allowlisted file %q still surfaces", g.File)
		}
	}

	// A clean tree (no findings) yields no gaps and no panic.
	adoptionFindings = nil
	if got := adoptionGaps(); len(got) != 0 {
		t.Errorf("adoptionGaps with no findings = %+v, want empty", got)
	}
}

func TestAdoptionSectionEmptyState(t *testing.T) {
	html := renderComp(t, adoptionGapSection(nil))
	if !strings.Contains(html, `data-testid="adoption-gaps-empty"`) {
		t.Errorf("empty adoption section missing empty state")
	}
	if !strings.Contains(html, `data-testid="adoption-gap-total"`) {
		t.Errorf("empty adoption section missing total badge")
	}
}

func TestUIComponentGalleryRendersAdoption(t *testing.T) {
	_, e := newDevGalleryServer(true)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("gallery = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="adoption-gaps"`, `data-testid="adoption-gap-total"`} {
		if !strings.Contains(body, want) {
			t.Errorf("gallery body missing %q", want)
		}
	}
}

func TestAdoptionSectionFlagOff(t *testing.T) {
	_, e := newDevGalleryServer(false)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/components", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("flag-off gallery = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `data-testid="adoption-gaps"`) {
		t.Error("flag-off gallery must not render the adoption-gaps section")
	}
}
