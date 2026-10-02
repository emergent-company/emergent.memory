package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	cmd := exec.Command("go", "run", "./cmd/componentgraph", "-out", tmp)
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
	for _, group := range sidebarGroups(false) {
		for _, item := range group.Items {
			if strings.Contains(item.Href, "/dev/") {
				t.Errorf("sidebar navigation contains a gallery link %q", item.Href)
			}
		}
	}
}
