package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- schema sub-navigation rail ---

// TestRenderSchemaRailOnEverySchemaPage asserts the two-item Schema rail renders
// on each Schema page with the correct active item. The active anchor carries
// aria-current="page" immediately after its href.
func TestRenderSchemaRailOnEverySchemaPage(t *testing.T) {
	compiledActive := `href="/schema" aria-current="page"`
	packsActive := `href="/schema/packs" aria-current="page"`

	cases := []struct {
		name   string
		page   func(t *testing.T) string
		active string
	}{
		{
			name: "compiled schema",
			page: func(t *testing.T) string {
				return renderHTML(t, SchemaPage(&SchemaListView{Compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}}}, nil))
			},
			active: compiledActive,
		},
		{
			name: "object type detail",
			page: func(t *testing.T) string {
				return renderHTML(t, SchemaObjectTypePage(&SchemaObjectTypeView{Detail: &ObjectTypeDetail{Name: "person"}}, nil))
			},
			active: compiledActive,
		},
		{
			name: "object type editor",
			page: func(t *testing.T) string {
				return renderHTML(t, SchemaObjectTypeEditPage(&SchemaEditView{Detail: &ObjectTypeDetail{Name: "person"}}))
			},
			active: compiledActive,
		},
		{
			name:   "add schema",
			page:   func(t *testing.T) string { return renderHTML(t, SchemaAddPage(&SchemaAddView{})) },
			active: packsActive,
		},
		{
			name:   "schema packs list",
			page:   func(t *testing.T) string { return renderHTML(t, SchemaPacksPage(nil, nil)) },
			active: packsActive,
		},
		{
			name:   "schema pack detail",
			page:   func(t *testing.T) string { return renderHTML(t, SchemaPackPage(nil, nil)) },
			active: packsActive,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := tc.page(t)
			for _, want := range []string{"Compiled schema", "Schema packs", tc.active} {
				if !strings.Contains(html, want) {
					t.Errorf("rail missing %q", want)
				}
			}
			// The opposite item must not be the active one.
			other := compiledActive
			if tc.active == compiledActive {
				other = packsActive
			}
			if strings.Contains(html, other) {
				t.Errorf("unexpected active state %q", other)
			}
		})
	}
}

// --- schema packs list ---

func TestUISchemaPacksRoute(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{
			ObjectTypes:       []CompiledType{{Name: "person", SchemaID: "pack-1", SchemaName: "personal-memory", SchemaVersion: "1.0.0"}},
			RelationshipTypes: []CompiledType{{Name: "knows", SchemaID: "pack-1", SchemaName: "personal-memory", SchemaVersion: "1.0.0"}},
		},
		schemas: []SchemaInfo{{ID: "pack-1", Name: "personal-memory", Version: "1.0.0", Description: "a pack", ProjectID: "p1", Source: "registry"}},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/packs", s.uiSchemaPacks)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Schema packs", "personal-memory", "v1.0.0", "a pack",
		"Project", "1 object type", "1 relationship type",
		`href="/schema/packs/pack-1"`, "Add schema", "1 pack",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("packs list missing %q", want)
		}
	}
}

// TestUISchemaPacksRouteGlobalPackWithoutCatalog asserts the derivation trap is
// avoided: a global pack that contributes compiled types but has no catalog
// entry (and no blueprint) still surfaces, labelled Global.
func TestUISchemaPacksRouteGlobalPackWithoutCatalog(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{
			ObjectTypes: []CompiledType{{Name: "message", SchemaID: "global-1", SchemaName: "session-message-types", SchemaVersion: "1.0.0"}},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/packs", s.uiSchemaPacks)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"session-message-types", "Global", `href="/schema/packs/global-1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("global pack list missing %q", want)
		}
	}
}

func TestUISchemaPacksRouteError(t *testing.T) {
	f := &fakeMemory{blueprintErr: errors.New("connection refused")}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/packs", s.uiSchemaPacks)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Failed to load schema packs") {
		t.Error("packs list load-error state missing")
	}
}

// --- schema pack detail ---

func packDetailFake() *fakeMemory {
	return &fakeMemory{
		compiled: &CompiledSchemaTypes{
			ObjectTypes: []CompiledType{
				{Name: "person", Label: "Person", SchemaID: "pack-1", SchemaName: "personal-memory", SchemaVersion: "1.0.0"},
				{Name: "note", SchemaID: "pack-2", SchemaName: "agent-notes", SchemaVersion: "1.0.0"},
			},
			RelationshipTypes: []CompiledType{{Name: "knows", SchemaID: "pack-1", SchemaName: "personal-memory", SchemaVersion: "1.0.0", SourceType: "person", TargetType: "person"}},
		},
		schemas: []SchemaInfo{{ID: "pack-1", Name: "personal-memory", Version: "1.0.0", Description: "a pack", ProjectID: "p1", Source: "registry"}},
	}
}

func TestUISchemaPackDetailRoute(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: packDetailFake()}
	e := echo.New()
	e.GET("/schema/packs/:id", s.uiSchemaPack)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs/pack-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"personal-memory", "a pack", "v1.0.0", "Project", "Active",
		"Object types", "Relationship types",
		`href="/schema/object-types/person"`, "knows",
		"Pack details", `href="/schema/packs" aria-current="page"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pack detail missing %q", want)
		}
	}
	// The pack's types are scoped to the pack: another pack's type must not leak.
	if strings.Contains(body, "agent-notes") {
		t.Error("types from another pack leaked into the detail")
	}
}

func TestUISchemaPackDetailNotFound(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: packDetailFake()}
	e := echo.New()
	e.GET("/schema/packs/:id", s.uiSchemaPack)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs/ghost", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Schema pack not found", `href="/schema/packs"`, "Compiled schema"} {
		if !strings.Contains(body, want) {
			t.Errorf("not-found detail missing %q", want)
		}
	}
}

func TestUISchemaPackDetailOwningBlueprintLink(t *testing.T) {
	f := provenanceFakeMemory(t, blueprintWithPack(t, "bp1", "personal-memory", "1.0.0", "personal-memory", "1.0.0"))
	f.compiled = &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person", SchemaID: "pack-1", SchemaName: "personal-memory", SchemaVersion: "1.0.0"},
	}}
	f.schemas = []SchemaInfo{{ID: "pack-1", Name: "personal-memory", Version: "1.0.0", ProjectID: "p1"}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/packs/:id", s.uiSchemaPack)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs/pack-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Owning blueprint", `href="/blueprints/bp1"`, "personal-memory v1.0.0"} {
		if !strings.Contains(body, want) {
			t.Errorf("blueprint-owned pack detail missing %q", want)
		}
	}
}

// --- derivation unit ---

func TestBuildSchemaPackSummariesGroupsAndSorts(t *testing.T) {
	compiled := &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			{Name: "zebra", SchemaID: "b", SchemaName: "beta", SchemaVersion: "1.0.0"},
			{Name: "person", SchemaID: "a", SchemaName: "alpha", SchemaVersion: "1.0.0"},
			{Name: "orphan"}, // no SchemaID — skipped
		},
		RelationshipTypes: []CompiledType{
			{Name: "knows", SchemaID: "a", SchemaName: "alpha", SchemaVersion: "1.0.0"},
		},
	}
	metas := []SchemaInfo{{ID: "a", Name: "alpha", Version: "2.0.0", ProjectID: "p1"}}
	packs := buildSchemaPackSummaries(compiled, metas, nil)
	if len(packs) != 2 {
		t.Fatalf("packs = %d, want 2", len(packs))
	}
	if packs[0].Name != "alpha" || packs[1].Name != "beta" {
		t.Errorf("sort order = %q, %q; want alpha, beta", packs[0].Name, packs[1].Name)
	}
	if packs[0].Version != "2.0.0" {
		t.Errorf("catalog metadata not joined: version = %q", packs[0].Version)
	}
	if packs[0].typeCountLabel() != "1 object type, 1 relationship type" {
		t.Errorf("count label = %q", packs[0].typeCountLabel())
	}
	if packs[1].typeCountLabel() != "1 object type" {
		t.Errorf("count label = %q", packs[1].typeCountLabel())
	}
}

// TestBuildSchemaPackSummariesExcludesShadowed asserts the compiled-schema
// invariant holds on the packs surface too: a shadowed (losing) duplicate is
// never listed or counted, so an override pack cannot resurrect the blueprint
// type it supersedes. It covers the three shapes that matter: a pack with only
// surviving types, a pack with a mix of surviving and superseded types (the
// realistic override case), and a pack whose every type is superseded.
func TestBuildSchemaPackSummariesExcludesShadowed(t *testing.T) {
	compiled := &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			// Blueprint pack: person survives, note is superseded by the override.
			{Name: "person", SchemaID: "personal-memory", SchemaName: "personal-memory", SchemaVersion: "1.0.0"},
			{Name: "note", SchemaID: "personal-memory", SchemaName: "personal-memory", SchemaVersion: "1.0.0", Shadowed: true},
			// Winning override declaration for note.
			{Name: "note", SchemaID: "overrides", SchemaName: "project-schema-overrides", SchemaVersion: "1.0.0"},
			// Fully superseded pack: only a losing declaration remains.
			{Name: "legacy", SchemaID: "legacy-pack", SchemaName: "legacy-pack", SchemaVersion: "1.0.0", Shadowed: true},
		},
		RelationshipTypes: []CompiledType{
			{Name: "annotates", SchemaID: "legacy-pack", SchemaName: "legacy-pack", SchemaVersion: "1.0.0", Shadowed: true},
		},
	}
	packs := buildSchemaPackSummaries(compiled, nil, nil)
	if len(packs) != 2 {
		t.Fatalf("packs = %d, want 2 (legacy-pack fully shadowed): %+v", len(packs), packs)
	}
	byName := map[string]schemaPackSummary{}
	for _, p := range packs {
		if _, dup := byName[p.Name]; dup {
			t.Fatalf("duplicate pack %q", p.Name)
		}
		byName[p.Name] = p
	}
	if _, ok := byName["legacy-pack"]; ok {
		t.Error("fully-shadowed pack legacy-pack must be omitted, not listed with 0 types")
	}
	// Partially-shadowed pack: the surviving type is listed, the superseded one is not.
	pm, ok := byName["personal-memory"]
	if !ok {
		t.Fatalf("personal-memory pack missing: %+v", byName)
	}
	if pm.typeCountLabel() != "1 object type" {
		t.Errorf("personal-memory types = %q, want 1 object type (superseded note excluded)", pm.typeCountLabel())
	}
	if slices.ContainsFunc(pm.ObjectTypes, func(t CompiledType) bool { return t.Name == "note" }) {
		t.Errorf("superseded note still listed under personal-memory: %+v", pm.ObjectTypes)
	}
	// Override pack carries the winning note.
	override, ok := byName["project-schema-overrides"]
	if !ok {
		t.Fatalf("override pack missing: %+v", byName)
	}
	if override.typeCountLabel() != "1 object type" {
		t.Errorf("override types = %q, want 1 object type", override.typeCountLabel())
	}
	if len(override.ObjectTypes) != 1 || override.ObjectTypes[0].Name != "note" {
		t.Errorf("override object types = %+v", override.ObjectTypes)
	}
}

// TestBuildSchemaPackSummariesProvenanceUsesDenormalisedIdentity asserts the
// owning-blueprint lookup keys on the compiled type's schema name/version, so a
// catalog entry whose name/version diverges from the blueprint manifest's pack
// does not silently drop the link (parity with the object-type page).
func TestBuildSchemaPackSummariesProvenanceUsesDenormalisedIdentity(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person", SchemaID: "pack-1", SchemaName: "personal-memory", SchemaVersion: "1.0.0"},
	}}
	metas := []SchemaInfo{{ID: "pack-1", Name: "Personal Memory (display)", Version: "9.9.9", ProjectID: "p1"}}
	prov := map[provenanceKey]*ProvenanceInfo{
		{name: "personal-memory", version: "1.0.0"}: {BlueprintID: "bp1", BlueprintName: "personal-memory", BlueprintVersion: "1.0.0"},
	}
	packs := buildSchemaPackSummaries(compiled, metas, prov)
	if len(packs) != 1 {
		t.Fatalf("packs = %d, want 1", len(packs))
	}
	if packs[0].Provenance == nil {
		t.Fatalf("provenance dropped when catalog name/version diverge: %+v", packs[0])
	}
	if packs[0].Provenance.BlueprintID != "bp1" {
		t.Errorf("provenance = %+v", packs[0].Provenance)
	}
}
