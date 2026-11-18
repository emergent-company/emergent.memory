package main

import (
	"encoding/json"
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
			page:   func(t *testing.T) string { return renderHTML(t, SchemaPackPage(nil, nil, "")) },
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
		`data-testid="schema-pack-card"`, `data-schema-pack-section="manual"`, "Manual",
		`href="/schema/object-types/person"`, `data-testid="schema-pack-type-link"`,
		`data-testid="schema-pack-title"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("packs list missing %q", want)
		}
	}
	// A manual pack's title is not a link; the type chip and the explicit
	// Details link are what navigate.
	if strings.Contains(body, `data-schema-pack-title-href`) {
		t.Error("manual pack title must not be a link")
	}
}

// TestGroupSchemaPacks pins the Blueprints section's grouping rules: only
// blueprint-owned packs appear, one group per blueprint, ordered
// case-insensitively by the blueprint label, with a heading that links to the
// blueprint.
func TestGroupSchemaPacks(t *testing.T) {
	groups := groupSchemaPacks([]schemaPackSummary{
		{ID: "cars", Name: "cars", Version: "1.0.0"},
		{ID: "memory", Name: "personal-memory", Version: "3.0.0", Provenance: &ProvenanceInfo{BlueprintID: "bp1", BlueprintName: "personal-memory", BlueprintVersion: "3.0.0"}},
		{ID: "notes", Name: "agent-notes", Version: "2.0.0", Provenance: &ProvenanceInfo{BlueprintID: "bp2", BlueprintName: "agent-notes", BlueprintVersion: "2.0.0"}},
		{ID: "overrides", Name: "project-schema-overrides", Version: "1.0.0"},
	})
	var keys []string
	for _, g := range groups {
		keys = append(keys, g.Key)
	}
	if want := []string{"blueprint:bp2", "blueprint:bp1"}; strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("group order = %v, want %v", keys, want)
	}
	if groups[0].BlueprintID() != "bp2" || groups[1].BlueprintID() != "bp1" {
		t.Errorf("blueprint ids = %q / %q, want bp2 / bp1", groups[0].BlueprintID(), groups[1].BlueprintID())
	}
	if groups[1].Href != "/blueprints/bp1" {
		t.Errorf("blueprint heading href = %q, want /blueprints/bp1", groups[1].Href)
	}
	var owned []string
	for _, r := range groups[0].Rows {
		owned = append(owned, r.Pack.ID)
	}
	if strings.Join(owned, ",") != "notes" {
		t.Errorf("blueprint bp2 rows = %v, want notes", owned)
	}
}

// TestAssignPackCategories pins the four-way classification and its precedence:
// an owning blueprint wins, then a provider token, then the provider catalog's
// declared pack name (the pre-reconcile fallback for an existing row), then a
// global/builtin row; the rest is manual.
func TestAssignPackCategories(t *testing.T) {
	providers := []DSProvider{
		{Type: "github_app", Name: "GitHub App / Bot", SchemaPack: "github-app-workflow"},
	}
	sources := []DSIntegration{
		{ID: "src-1", Name: "emergent-company/emergent.memory", ProviderType: "github_app"},
		{ID: "src-2", Name: "mkucharz/memory", ProviderType: "github_app"},
	}
	packs := []schemaPackSummary{
		{ID: "bp", Name: "pack", Provenance: &ProvenanceInfo{BlueprintID: "bp1"}},
		{ID: "token", Name: "github-app-workflow", ProjectID: "p1", Source: "provider:github_app"},
		{ID: "catalog", Name: "github-app-workflow", ProjectID: "p1", Source: "manual"},
		{ID: "builtin", Name: "session-message-types", Source: "builtin"},
		{ID: "manual", Name: "cars", ProjectID: "p1", Source: "manual"},
	}
	assignPackCategories(packs, providers, sources)

	if packs[0].Category != packCategoryBlueprints {
		t.Errorf("blueprint-owned category = %q", packs[0].Category)
	}
	for _, i := range []int{1, 2} {
		if packs[i].Category != packCategoryDataSource {
			t.Errorf("pack %d category = %q, want data-sources", i, packs[i].Category)
		}
		if packs[i].ProviderLabel != "GitHub App / Bot" {
			t.Errorf("pack %d provider label = %q", i, packs[i].ProviderLabel)
		}
		if len(packs[i].LinkedSources) != 2 || packs[i].LinkedSources[0].ID != "src-1" {
			t.Errorf("pack %d linked sources = %+v", i, packs[i].LinkedSources)
		}
	}
	if packs[3].Category != packCategoryBuiltIn {
		t.Errorf("global builtin category = %q", packs[3].Category)
	}
	if packs[4].Category != packCategoryManual {
		t.Errorf("manual category = %q", packs[4].Category)
	}
}

// TestSchemaPackSections pins the fixed section order and the data-source rows:
// one row per installed app, each linking to that app's detail page, and a
// provider with no installed app still gets a plain row.
func TestSchemaPackSections(t *testing.T) {
	packs := []schemaPackSummary{
		{ID: "m1", Name: "cars", Category: packCategoryManual},
		{ID: "b1", Name: "personal-memory", Category: packCategoryBlueprints, Provenance: &ProvenanceInfo{BlueprintID: "bp1", BlueprintName: "personal-memory", BlueprintVersion: "1.0.0"}},
		{ID: "d1", Name: "github-app-workflow", Version: "1.3.0", Category: packCategoryDataSource, ProviderType: "github_app", ProviderLabel: "GitHub App / Bot", LinkedSources: []packLinkedSource{{ID: "src-1", Name: "org/repo"}, {ID: "src-2", Name: "me/repo"}}},
		{ID: "d2", Name: "email-core", Version: "1.0.0", Category: packCategoryDataSource, ProviderType: "gmail", ProviderLabel: "Gmail"},
		{ID: "g1", Name: "session-message-types", Category: packCategoryBuiltIn},
	}
	sections := schemaPackSections(packs)
	var keys []string
	for _, sec := range sections {
		keys = append(keys, sec.Key)
	}
	if want := []string{"manual", "blueprints", "data-sources", "built-in"}; strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("section order = %v, want %v", keys, want)
	}
	if sections[0].Groups[0].Rows[0].Title != "cars" || sections[0].Groups[0].Rows[0].Href != "" {
		t.Errorf("manual row = %+v, want plain title", sections[0].Groups[0].Rows[0])
	}
	if sections[1].Groups[0].Href != "/blueprints/bp1" {
		t.Errorf("blueprint group href = %q", sections[1].Groups[0].Href)
	}
	dsRows := sections[2].Groups[0].Rows
	if sections[2].Groups[0].Label != "GitHub App / Bot" || len(dsRows) != 2 {
		t.Fatalf("data-source group = %+v", sections[2].Groups[0])
	}
	if dsRows[0].Title != "org/repo" || dsRows[0].Href != "/data-sources/src-1" || dsRows[0].Subtitle != "github-app-workflow v1.3.0" {
		t.Errorf("data-source row = %+v", dsRows[0])
	}
	emailRows := sections[2].Groups[1].Rows
	if len(emailRows) != 1 || emailRows[0].Href != "" || emailRows[0].Title != "Gmail" {
		t.Errorf("unlinked provider row = %+v, want one plain Gmail row", emailRows)
	}
	if sections[3].Groups[0].Rows[0].Title != "session-message-types" || sections[3].Groups[0].Rows[0].Href != "" {
		t.Errorf("built-in row = %+v", sections[3].Groups[0].Rows[0])
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
	for _, want := range []string{"session-message-types", "Global", `href="/schema/packs/global-1"`, `data-schema-pack-section="built-in"`, "Built-in"} {
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
		"Source", "Manual",
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

// TestUISchemaPackDetailDataSource pins the data-source provenance on the pack
// detail: the friendly provider label replaces the raw token, and each installed
// app is a link to its data-source page.
func TestUISchemaPackDetailDataSource(t *testing.T) {
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			{Name: "GitHubIssue", SchemaID: "pack-gh", SchemaName: "github-app-workflow", SchemaVersion: "1.3.0"},
		}},
		schemas: []SchemaInfo{{ID: "pack-gh", Name: "github-app-workflow", Version: "1.3.0", ProjectID: "p1", Source: "provider:github_app"}},
		dsProviders: []DSProvider{
			{Type: "github_app", Name: "GitHub App / Bot", SchemaPack: "github-app-workflow"},
		},
		dsIntegrations: []DSIntegration{
			{ID: "src-1", Name: "emergent-company/emergent.memory", ProviderType: "github_app"},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/packs/:id", s.uiSchemaPack)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs/pack-gh", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"GitHub App / Bot", "Installed apps", `href="/data-sources/src-1"`, "emergent-company/emergent.memory",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("data-source pack detail missing %q", want)
		}
	}
	if strings.Contains(body, "provider:github_app") {
		t.Error("data-source pack detail must not print the raw provenance token")
	}
}

func TestUISchemaPackDetailNotFound(t *testing.T) {
	f := packDetailFake()
	// The catalog holds no pack by this id, so GetSchema fails and the page must
	// render the not-found state (not a pending-pack page with a blank name).
	f.schemasByID = map[string]*BlueprintSchema{}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
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

// TestUISchemaPackDetailPendingReview pins the state a discovery run leaves
// behind: the pack exists but is not installed, so it contributes no compiled
// types. The Schema page links to it as "Review pack" — that link must render
// what the run proposed and the install action, never a not-found page.
func TestUISchemaPackDetailPendingReview(t *testing.T) {
	f := &fakeMemory{
		schemasByID: map[string]*BlueprintSchema{
			"discovered-1": {
				ID:                      "discovered-1",
				Name:                    "Discovered Types - 2026-11-04",
				Version:                 "1.0.0",
				Description:             "Auto-discovered types from 4 entities",
				Author:                  "Auto-Discovery System",
				Source:                  "discovered",
				PendingReview:           true,
				ObjectTypeSchemas:       json.RawMessage(`{"Customer":{"type":"object"},"Order":{"type":"object"}}`),
				RelationshipTypeSchemas: json.RawMessage(`{"places":{"sourceTypes":["Customer"],"targetTypes":["Order"]}}`),
			},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/packs/:id", s.uiSchemaPack)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs/discovered-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Discovered Types - 2026-11-04",
		"Pending review",
		"not installed",
		"Customer", "Order",
		"places (Customer → Order)",
		`action="/schema/packs/discovered-1/install"`,
		"Install pack",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pending pack page missing %q", want)
		}
	}
	if strings.Contains(body, "Schema pack not found") {
		t.Error("a pending pack must not render the not-found state")
	}
}

// TestUISchemaPackDetailNotInstalled pins the other half of the badge: a pack
// that exists but was not proposed by a run reads as "Not installed", not as
// "Pending review" — the flag the server sets is what distinguishes them.
func TestUISchemaPackDetailNotInstalled(t *testing.T) {
	f := &fakeMemory{
		schemasByID: map[string]*BlueprintSchema{
			"orphan-1": {
				ID:                "orphan-1",
				Name:              "Orphan pack",
				Version:           "1.0.0",
				ObjectTypeSchemas: json.RawMessage(`{"Customer":{"type":"object"}}`),
			},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/packs/:id", s.uiSchemaPack)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs/orphan-1", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "Not installed") {
		t.Error("an unassigned pack that is not awaiting review must say so")
	}
	if strings.Contains(body, "Pending review") {
		t.Error("a pack with pending_review=false must not claim to be pending review")
	}
}

// TestUISchemaPackInstall pins that installing a reviewed pack is what takes a
// discovery result live, that it lands back on the pack, and that the landing
// page confirms it — a redirect that says nothing would read as a no-op.
func TestUISchemaPackInstall(t *testing.T) {
	f := packDetailFake()
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/packs/:id", s.uiSchemaPack)
	e.POST("/schema/packs/:id/install", s.uiSchemaPackInstall)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schema/packs/pack-1/install", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/schema/packs/pack-1?installed=1" {
		t.Errorf("install location = %q", loc)
	}
	if len(f.assignedSchemaPacks) != 1 || f.assignedSchemaPacks[0] != "pack-1" {
		t.Fatalf("assigned packs = %v, want [pack-1]", f.assignedSchemaPacks)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs/pack-1?installed=1", nil))
	if !strings.Contains(rec.Body.String(), "Pack installed") {
		t.Error("the landing page must confirm the install")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/packs/pack-1", nil))
	if strings.Contains(rec.Body.String(), "Pack installed") {
		t.Error("a plain page load must not repeat the install confirmation")
	}
}

// TestUISchemaPackInstallFailureStaysOnThePack pins the error path: a refused
// install (no write permission, pack vanished) reports on the pack page.
func TestUISchemaPackInstallFailureStaysOnThePack(t *testing.T) {
	f := &fakeMemory{schemaPackErr: errors.New("permission denied")}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/schema/packs/:id/install", s.uiSchemaPackInstall)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schema/packs/discovered-1/install", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/schema/packs/discovered-1?err=") {
		t.Errorf("install location = %q, want the pack page with ?err=", loc)
	}
	if len(f.assignedSchemaPacks) != 0 {
		t.Fatalf("assigned packs = %v, want none", f.assignedSchemaPacks)
	}
}

// TestPendingPackFromSchema pins the parse: type maps are keyed by name, and a
// relationship shows its endpoints so the review is meaningful.
func TestPendingPackFromSchema(t *testing.T) {
	pack := pendingPackFromSchema(&BlueprintSchema{
		ID:                      "p1",
		Name:                    "discovered",
		Version:                 "1.0.0",
		ObjectTypeSchemas:       json.RawMessage(`{"Zebra":{"type":"object"},"Alpha":{"type":"object"}}`),
		RelationshipTypeSchemas: json.RawMessage(`{"owns":{"sourceTypes":["Alpha"],"targetTypes":["Zebra"]},"links":{}}`),
	})
	if got, want := strings.Join(pack.ObjectTypes, ","), "Alpha,Zebra"; got != want {
		t.Errorf("object types = %q, want %q (sorted)", got, want)
	}
	if got, want := strings.Join(pack.RelationTypes, ","), "links,owns (Alpha → Zebra)"; got != want {
		t.Errorf("relationship types = %q, want %q", got, want)
	}
	if pack.PendingReview {
		t.Error("pending_review must come from the schema, not be assumed")
	}

	// The registry's array form (and a scalar endpoint pair) must render too:
	// a pack stored the other way round used to review as an empty page with an
	// install button.
	registry := pendingPackFromSchema(&BlueprintSchema{
		ID:                      "p2",
		Name:                    "registry",
		ObjectTypeSchemas:       json.RawMessage(`[{"name":"Widgit","description":"thing"},{"name":"Gadget"}]`),
		RelationshipTypeSchemas: json.RawMessage(`[{"name":"fits","sourceType":"Widgit","targetType":"Gadget"},{"name":"loose"}]`),
	})
	if got, want := strings.Join(registry.ObjectTypes, ","), "Gadget,Widgit"; got != want {
		t.Errorf("array-form object types = %q, want %q", got, want)
	}
	if got, want := strings.Join(registry.RelationTypes, ","), "fits (Widgit → Gadget),loose"; got != want {
		t.Errorf("array-form relationship types = %q, want %q", got, want)
	}
}
