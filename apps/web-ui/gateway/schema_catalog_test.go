package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestRenderSchemaCatalogPage(t *testing.T) {
	list := &CatalogList{
		Version: "1.0.0",
		Total:   3,
		Categories: []CatalogCategoryCount{
			{Key: "media", Count: 1},
			{Key: "identity", Count: 2},
		},
		Types: []CatalogSummary{
			{Name: "Movie", Label: "Movie", Description: "A film.", Category: "media", Parent: "CreativeWork", Labels: []string{"film"}, UI: &CatalogUI{Icon: "tabler--movie", Color: "#DB2777"}},
			{Name: "Person", Label: "Person", Category: "identity", UI: &CatalogUI{Icon: "tabler--user"}},
			{Name: "Organization", Label: "Organization", Category: "identity", UI: &CatalogUI{Icon: "tabler--building"}},
		},
	}
	view := &SchemaCatalogView{
		Catalog: list,
		States: map[string]catalogInstallState{
			"Person":       {Installed: true, ProvidedBy: "schemaorg-catalog", ProvidedVer: "1.0.0"},
			"Organization": {ProvidedBy: "personal-memory", ProvidedVer: "4.1.0"},
		},
		Groups: groupCatalogTypes(list),
	}
	html := renderHTML(t, SchemaCatalogPage(view, nil))
	for _, want := range []string{
		`data-testid="catalog-search-form"`,
		`data-testid="catalog-category-filter"`,
		`category=media`,
		`data-catalog-type="Movie"`,
		`href="/schema/catalog/Movie"`,
		`action="/schema/catalog/install"`,
		`name="type" value="Movie"`,
		"Installed",
		"Provided by personal-memory 4.1.0",
		`href="/schema/object-types/Organization"`,
		"tabler--movie",
		"film",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("catalog page missing %q", want)
		}
	}
	// Installed and provided-by names are not installable in this project: the
	// first is already ours, the second belongs to another pack's definition.
	for _, name := range []string{"Person", "Organization"} {
		if strings.Contains(html, `name="type" value="`+name+`"`) {
			t.Errorf("%s must not render an install form", name)
		}
	}
}

func TestCatalogStatesUseProvenanceNotNameAlone(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		// A same-named type from another pack: not installed, provided by it.
		{Name: "Person", SchemaName: "personal-memory", SchemaVersion: "4.1.0"},
		// Ours.
		{Name: "Movie", SchemaName: "schemaorg-catalog", SchemaVersion: "1.0.0"},
		// The loser of a cross-pack collision must not decide the owner.
		{Name: "Organization", SchemaName: "personal-memory", SchemaVersion: "4.1.0"},
		{Name: "Organization", SchemaName: "schemaorg-catalog", SchemaVersion: "1.0.0", Shadowed: true},
	}}
	names := map[string]bool{"Person": true, "Movie": true, "Organization": true, "Offer": true}
	states := catalogStates(compiled, names)

	if got := states["Person"]; got.Installed || got.ProvidedBy != "personal-memory" || got.Available() {
		t.Errorf("Person = %+v, want provided by personal-memory", got)
	}
	if got := states["Movie"]; !got.Installed || got.ProvidedLabel() != "schemaorg-catalog 1.0.0" || got.Available() {
		t.Errorf("Movie = %+v, want installed from the catalog pack", got)
	}
	if got := states["Organization"]; got.Installed || got.ProvidedBy != "personal-memory" {
		t.Errorf("Organization = %+v, want provided by the unshadowed declaration", got)
	}
	if got := states["Offer"]; !got.Available() {
		t.Errorf("Offer = %+v, want available", got)
	}
	if got := catalogStates(nil, names); len(got) != 0 {
		t.Errorf("catalogStates(nil) = %v, want empty", got)
	}
}

// TestCatalogStatesSkipsUnownedRows pins the empty-schema-name case: the
// compiled view can carry a same-named row with no providing pack, and the
// server treats such a name as unowned. Recording a state for it would claim a
// provenance (and an install) the server will not honour.
func TestCatalogStatesSkipsUnownedRows(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Offer", SchemaName: ""},
		{Name: "Movie", SchemaName: "schemaorg-catalog", SchemaVersion: "1.0.0"},
	}}
	states := catalogStates(compiled, map[string]bool{"Offer": true, "Movie": true})
	if got, ok := states["Offer"]; ok {
		t.Errorf("Offer = %+v, want no state for an unowned row", got)
	}
	if got := states["Movie"]; !got.Installed {
		t.Errorf("Movie = %+v, want installed from the catalog pack", got)
	}
}

// TestCatalogPackForFollowsCustomName pins issue #1149 item 3: a project that
// installed the catalog with a custom --name owns its catalog types under that
// pack, so the badge must read the pack the project actually uses rather than
// the hardcoded default. A pack owning any non-catalog type is not the catalog
// pack; a shadowed row does not vote.
func TestCatalogPackForFollowsCustomName(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Movie", SchemaName: "my-catalog", SchemaVersion: "1.0.0"},
		{Name: "Person", SchemaName: "my-catalog", SchemaVersion: "1.0.0"},
		{Name: "Person", SchemaName: "personal-memory", SchemaVersion: "4.1.0", Shadowed: true},
		{Name: "note", SchemaName: "agent-notes", SchemaVersion: "2.0.0"},
	}}
	names := map[string]bool{"Movie": true, "Person": true, "Organization": true}

	if got := catalogPackFor(compiled, names); got != "my-catalog" {
		t.Fatalf("catalogPackFor = %q, want my-catalog", got)
	}
	states := catalogStates(compiled, names)
	if got := states["Movie"]; !got.Installed || got.ProvidedLabel() != "my-catalog 1.0.0" {
		t.Errorf("Movie = %+v, want installed from my-catalog", got)
	}
	if got := states["Person"]; !got.Installed || got.ProvidedBy != "my-catalog" {
		t.Errorf("Person = %+v, want installed from my-catalog", got)
	}
}

// TestCatalogPackForDefaultAndFallbacks pins the pack-name derivation edges: a
// project that owns types under the default name is read as the default catalog
// pack even when another pack owns more catalog types, a pack holding any
// non-catalog type is not a catalog pack, and no candidate leaves the default.
func TestCatalogPackForDefaultAndFallbacks(t *testing.T) {
	names := map[string]bool{"Movie": true, "Person": true}
	twoPacks := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Movie", SchemaName: "schemaorg-catalog"},
		{Name: "Person", SchemaName: "my-catalog"},
	}}
	if got := catalogPackFor(twoPacks, names); got != "schemaorg-catalog" {
		t.Errorf("catalogPackFor with the default pack present = %q, want the default", got)
	}

	nonCatalogPack := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "Movie", SchemaName: "my-catalog"},
		{Name: "note", SchemaName: "my-catalog"},
	}}
	if got := catalogPackFor(nonCatalogPack, names); got != catalogPackName {
		t.Errorf("catalogPackFor with a non-catalog pack = %q, want the default", got)
	}

	if got := catalogPackFor(nil, names); got != catalogPackName {
		t.Errorf("catalogPackFor(nil) = %q, want the default", got)
	}
	if got := catalogPackFor(twoPacks, nil); got != catalogPackName {
		t.Errorf("catalogPackFor without catalog names = %q, want the default", got)
	}
}

func TestRenderSchemaCatalogPageEmpty(t *testing.T) {
	html := renderHTML(t, SchemaCatalogPage(&SchemaCatalogView{Catalog: &CatalogList{}}, nil))
	if !strings.Contains(html, "No catalog types match") {
		t.Error("empty catalog state missing")
	}
}

func TestRenderSchemaCatalogTypePage(t *testing.T) {
	detail := &CatalogTypeDetail{
		Type: CatalogType{
			Name:        "Movie",
			Label:       "Movie",
			Description: "A feature film.",
			Category:    "media",
			Parent:      "CreativeWork",
			Labels:      []string{"film"},
			UI:          &CatalogUI{Icon: "tabler--movie", Color: "#DB2777"},
			Properties: map[string]CatalogProperty{
				"duration": {Type: "integer", Description: "Runtime in minutes."},
			},
		},
		Ancestors: []string{"CreativeWork", "Thing"},
		Children:  []string{"Documentary"},
		RelatedRelationships: []CatalogRelationship{
			{Name: "directed_by", SourceType: "Movie", TargetType: "Person", Description: "Directed by a person."},
		},
	}
	html := renderHTML(t, SchemaCatalogTypePage(&SchemaCatalogTypeView{Detail: detail}, nil))
	for _, want := range []string{
		`data-testid="catalog-type-detail"`,
		"Movie",
		"CreativeWork",
		"Documentary",
		"duration",
		"directed_by",
		"Movie → Person",
		`action="/schema/catalog/install"`,
		"Install into project",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("catalog type page missing %q", want)
		}
	}
}

// TestRenderSchemaCatalogTypePageProvidedBy pins the detail page for a name the
// project already has from another pack: it names that pack and its live
// definition instead of offering an install, because installing would merge the
// catalog's properties into a type the project does not resolve from the
// catalog. Both the header badge and the panel carry the pack's version.
func TestRenderSchemaCatalogTypePageProvidedBy(t *testing.T) {
	detail := &CatalogTypeDetail{Type: CatalogType{Name: "Organization", Label: "Organization", Description: "An organization."}}
	state := catalogInstallState{ProvidedBy: "personal-memory", ProvidedVer: "4.1.0"}
	html := renderHTML(t, SchemaCatalogTypePage(&SchemaCatalogTypeView{Detail: detail, State: state}, nil))
	for _, want := range []string{
		`data-testid="catalog-provided-by"`,
		"Provided by personal-memory 4.1.0",
		"View the project type",
		`href="/schema/object-types/Organization"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("provided-by detail page missing %q", want)
		}
	}
	if strings.Contains(html, `action="/schema/catalog/install"`) {
		t.Error("a type another pack provides must not render an install form")
	}
}

// TestUISchemaCatalogFollowsCustomPackName drives the browse route end to end
// with a catalog installed under a custom pack name: its types read Installed
// instead of being offered for install as if unowned.
func TestUISchemaCatalogFollowsCustomPackName(t *testing.T) {
	f := &fakeMemory{
		catalogList: &CatalogList{
			Version: "1.0.0",
			Total:   2,
			Categories: []CatalogCategoryCount{
				{Key: "media", Count: 1},
				{Key: "identity", Count: 1},
			},
			Types: []CatalogSummary{
				{Name: "Movie", Label: "Movie", Category: "media"},
				{Name: "Person", Label: "Person", Category: "identity"},
			},
		},
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "Movie", SchemaName: "my-catalog", SchemaVersion: "1.0.0"},
			{Name: "Person", SchemaName: "personal-memory", SchemaVersion: "4.1.0"},
		}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/catalog", s.uiSchemaCatalog)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/catalog", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Installed", "Provided by personal-memory 4.1.0"} {
		if !strings.Contains(body, want) {
			t.Errorf("catalog page missing %q", want)
		}
	}
	if strings.Contains(body, `name="type" value="Movie"`) {
		t.Error("a catalog type owned under a custom pack name must not render an install form")
	}
}

// TestUISchemaCatalogTypeFollowsCustomPackName is the detail-page counterpart:
// the type it shows is read from the same custom-named catalog pack.
func TestUISchemaCatalogTypeFollowsCustomPackName(t *testing.T) {
	f := &fakeMemory{
		catalogList: &CatalogList{Types: []CatalogSummary{
			{Name: "Movie", Label: "Movie"},
			{Name: "Person", Label: "Person"},
		}},
		catalogDetail: &CatalogTypeDetail{Type: CatalogType{Name: "Movie", Label: "Movie"}},
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "Movie", SchemaName: "my-catalog", SchemaVersion: "1.0.0"},
		}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/catalog/:name", s.uiSchemaCatalogType)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/catalog/Movie", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Installed") {
		t.Error("catalog detail page missing the Installed badge for a custom-named catalog pack")
	}
	if strings.Contains(body, "Install into project") {
		t.Error("an installed catalog type must not render an install form")
	}
}

func TestGroupCatalogTypesOrder(t *testing.T) {
	list := &CatalogList{
		Categories: []CatalogCategoryCount{{Key: "media", Count: 1}, {Key: "identity", Count: 1}},
		Types: []CatalogSummary{
			{Name: "Movie", Category: "media"},
			{Name: "Person", Category: "identity"},
			{Name: "Mystery"},
		},
	}
	groups := groupCatalogTypes(list)
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if groups[0].Key != "media" || groups[1].Key != "identity" || groups[2].Key != "other" {
		t.Fatalf("group order = %v", []string{groups[0].Key, groups[1].Key, groups[2].Key})
	}
}

func TestCatalogLinks(t *testing.T) {
	if got := catalogTypeHref("Movie"); got != "/schema/catalog/Movie" {
		t.Errorf("catalogTypeHref = %q", got)
	}
	if got := catalogCategoryHref("media", "film"); !strings.Contains(got, "category=media") || !strings.Contains(got, "q=film") {
		t.Errorf("catalogCategoryHref = %q", got)
	}
}
