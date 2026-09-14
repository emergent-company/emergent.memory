package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// writeBundledPack writes a pack/schema YAML file under dir/<sub>/pack.yaml so
// ListBundledBlueprints can discover it.
func writeBundledPack(t *testing.T, dir, sub, content string) {
	t.Helper()
	packs := filepath.Join(dir, sub)
	if err := os.MkdirAll(packs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packs, "pack.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- bundled blueprint discovery ---

func TestListBundledBlueprints(t *testing.T) {
	dir := t.TempDir()
	writeBundledPack(t, filepath.Join(dir, "personal-memory"), "packs",
		"name: personal-memory\nversion: 1.0.0\ndescription: Personal graph\nauthor: diane\nobjectTypes:\n  - name: person\n    label: Person\n")
	writeBundledPack(t, filepath.Join(dir, "agent-notes"), "schemas",
		"name: agent-notes\nversion: 1.0.0\nobjectTypeSchemas:\n  - name: Note\n")
	// A directory with no parseable pack must be skipped, not fail the list.
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := ListBundledBlueprints(dir)
	if err != nil {
		t.Fatal(err)
	}
	// sorted by name: agent-notes, personal-memory
	if len(out) != 2 {
		t.Fatalf("got %d bundled packs, want 2: %+v", len(out), out)
	}
	if out[0].Name != "agent-notes" || out[0].Source != "bundled" {
		t.Errorf("item0 = %+v", out[0])
	}
	if out[1].Name != "personal-memory" || out[1].Version != "1.0.0" || out[1].Author != "diane" {
		t.Errorf("item1 = %+v", out[1])
	}
}

func TestListBundledBlueprintsMissing(t *testing.T) {
	if _, err := ListBundledBlueprints(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("want error for missing dir, got nil")
	}
}

// TestListEmbeddedBlueprints verifies the packs compiled into the binary are
// discovered (personal-memory + agent-notes).
func TestListEmbeddedBlueprints(t *testing.T) {
	out, err := listEmbeddedBlueprints()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, b := range out {
		if b.Source != "bundled" {
			t.Errorf("source = %q, want bundled", b.Source)
		}
		names[b.Name] = true
	}
	if !names["personal-memory"] || !names["agent-notes"] {
		t.Fatalf("missing embedded packs, got: %+v", out)
	}
	if !names["operator"] {
		t.Fatalf("missing embedded operator pack, got: %+v", out)
	}
}

// TestOperatorBlueprintDefinesOperatorAgent loads the embedded operator
// blueprint and asserts it defines an agent whose tools include ask_user (the
// proposal/confirm checkpoint) and whose system prompt mandates proposing a
// change and pausing for acceptance before any write. This is the definitional
// contract of the general operator instructions: given an operator, the chat
// loop must be able to emit a "question with a suggestion to confirm".
func TestOperatorBlueprintDefinesOperatorAgent(t *testing.T) {
	bp, err := loadBundledFromFS(bundledFS, "blueprints/operator")
	if err != nil {
		t.Fatal(err)
	}
	if bp.Name != "operator" {
		t.Fatalf("name = %q, want operator", bp.Name)
	}
	if bp.HasSchema {
		t.Error("operator is a pure-agent blueprint; must not carry a schema")
	}
	if len(bp.Agents) != 1 {
		t.Fatalf("agents = %d, want 1", len(bp.Agents))
	}
	a := bp.Agents[0]
	if a.Name != "operator" {
		t.Errorf("agent name = %q", a.Name)
	}
	if a.Model != "openai/deepseek-v4-pro" {
		t.Errorf("model = %q, want openai/deepseek-v4-pro", a.Model)
	}
	if !containsString(a.Tools, "ask_user") {
		t.Error("operator tools must include ask_user (the proposal checkpoint)")
	}
	// The operator instructions must encode the propose → accept contract so
	// the agent asks for confirmation (a question) before it writes.
	for _, want := range []string{"ask_user", "Accept", "Reject", "propose"} {
		if !strings.Contains(a.SystemPrompt, want) {
			t.Errorf("operator systemPrompt missing %q", want)
		}
	}
}

// TestInstallOperatorBlueprintViaBlueprintAPI installs the operator blueprint
// through the gateway and asserts it is applied via memory's blueprint API
// (create → publish → apply) rather than the legacy schema/agent path. No
// schema is registered and no agent is created directly — the agent is
// materialized by memory's blueprint apply.
func TestInstallOperatorBlueprintViaBlueprintAPI(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}

	res, err := s.installBundledBlueprint(context.Background(), "operator")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("install returned nil result")
	}
	if len(f.created) != 0 {
		t.Errorf("operator install must not create an agent directly, got %d", len(f.created))
	}
	if f.createdBlueprint == nil {
		t.Fatal("expected a blueprint definition to be created")
	}
	if f.createdBlueprint.Name != "operator" {
		t.Errorf("created blueprint name = %q, want operator", f.createdBlueprint.Name)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp-new" {
		t.Errorf("applied = %v, want [bp-new]", f.appliedBlueprint)
	}
}

// TestInstallOperatorBlueprintIdempotent verifies a second install of the
// operator blueprint succeeds without error. Real idempotency (no duplicate
// agent) is memory's apply contract (create-or-update by name); the gateway's
// resolve-or-create adopts an existing version on re-install.
func TestInstallOperatorBlueprintIdempotent(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}

	for range 2 {
		if _, err := s.installBundledBlueprint(context.Background(), "operator"); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.appliedBlueprint) != 2 {
		t.Fatalf("applied = %d, want 2 (both installs reach apply)", len(f.appliedBlueprint))
	}
}

// TestBuildAvailableListExcludesInstalledAgentBlueprint verifies an agent-only
// blueprint is excluded from the available list once its agent exists, and is
// available again when the agent is absent.
func TestBuildAvailableListExcludesInstalledAgentBlueprint(t *testing.T) {
	bundled := []AvailableSchemaItem{
		{ID: "operator", Name: "operator", Version: "1.0.0", Source: "bundled", Agents: []string{"operator"}},
		{ID: "agent-notes", Name: "agent-notes", Version: "1.0.0", Source: "bundled", HasSchema: true},
	}

	// operator agent exists → operator excluded from available (pre-cutover
	// agent fallback)
	out := buildAvailableList(nil, bundled, nil, map[string]bool{"operator": true})
	if len(out) != 1 || out[0].Name != "agent-notes" {
		t.Fatalf("available = %+v, want only agent-notes", out)
	}

	// no operator agent → both available
	out2 := buildAvailableList(nil, bundled, nil, nil)
	if len(out2) != 2 {
		t.Fatalf("available = %+v, want both (operator agent absent)", out2)
	}
}

// TestBuildAvailableList verifies the available list merges project-owned
// registry packs with bundled packs, excludes installed packs, dedupes by name,
// and skips global (unfetchable) schemas.
func TestBuildAvailableList(t *testing.T) {
	schemas := []SchemaInfo{
		{ID: "s1", Name: "multi-agent", Version: "1.0.0", ProjectID: "proj"},
		{ID: "s2", Name: "session-message-types", Version: "1.0.0"}, // global — must be skipped
	}
	bundled := []AvailableSchemaItem{
		{ID: "agent-notes", Name: "agent-notes", Version: "1.0.0", Source: "bundled"},
		{ID: "personal-memory", Name: "personal-memory", Version: "1.0.0", Source: "bundled"},
	}
	installed := []AppliedBlueprint{{Name: "agent-notes", Version: "1.0.0"}}
	out := buildAvailableList(schemas, bundled, installed, nil)
	if len(out) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(out), out)
	}
	got := map[string]string{}
	for _, a := range out {
		got[a.Name] = a.Source
	}
	if got["multi-agent"] != "registry" {
		t.Errorf("multi-agent source = %q", got["multi-agent"])
	}
	if got["personal-memory"] != "bundled" {
		t.Errorf("personal-memory source = %q", got["personal-memory"])
	}
	if _, ok := got["session-message-types"]; ok {
		t.Errorf("global schema should be skipped (unfetchable via GetPack)")
	}
	if _, ok := got["agent-notes"]; ok {
		t.Errorf("agent-notes should be excluded (installed at same version)")
	}
}

// TestBuildAvailableListUpgrade verifies a bundled pack with a higher version
// than the installed pack surfaces as an "upgrade" (kept, Upgrade=true), while
// same-or-older versions are filtered out.
func TestBuildAvailableListUpgrade(t *testing.T) {
	bundled := []AvailableSchemaItem{
		{ID: "agent-notes", Name: "agent-notes", Version: "2.0.0", Source: "bundled"},
		{ID: "personal-memory", Name: "personal-memory", Version: "2.0.0", Source: "bundled"},
	}
	installed := []AppliedBlueprint{
		{Name: "agent-notes", Version: "1.0.0"},
		{Name: "personal-memory", Version: "2.0.0"}, // already at v2 — not an upgrade
	}
	out := buildAvailableList(nil, bundled, installed, nil)
	if len(out) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(out), out)
	}
	if out[0].Name != "agent-notes" || !out[0].Upgrade {
		t.Errorf("agent-notes should be an upgrade: %+v", out[0])
	}
}

// TestBuildAvailableListRegistryUpgrade verifies project-scoped registry packs
// collapse to their highest version per name and surface as an upgrade only
// when that version is strictly newer than the applied one. Applied-at-same (or
// newer) registry packs must stay hidden, non-applied ones stay available, and
// the bundled path is unchanged.
func TestBuildAvailableListRegistryUpgrade(t *testing.T) {
	applied := []AppliedBlueprint{
		{Name: "multi-agent", Version: "1.0.0"},
		{Name: "session-message-types", Version: "2.0.0"},
		{Name: "vendor-pack", Version: "3.0.0"},
		{Name: "agent-notes", Version: "1.0.0"},
	}
	schemas := []SchemaInfo{
		// multi-agent has two project-scoped rows: only the highest may surface.
		{ID: "s1-old", Name: "multi-agent", Version: "1.0.0", ProjectID: "proj"},
		{ID: "s1-new", Name: "multi-agent", Version: "2.0.0", ProjectID: "proj"},
		// applied at the same version → hidden
		{ID: "s2", Name: "session-message-types", Version: "2.0.0", ProjectID: "proj"},
		// registry older than applied → hidden (no regression)
		{ID: "s3", Name: "vendor-pack", Version: "1.0.0", ProjectID: "proj"},
		// not applied → available
		{ID: "s4", Name: "custom-pack", Version: "1.0.0", ProjectID: "proj"},
		// global → skipped
		{ID: "g1", Name: "global-pack", Version: "9.0.0"},
	}
	bundled := []AvailableSchemaItem{
		{ID: "agent-notes", Name: "agent-notes", Version: "2.0.0", Source: "bundled"},
	}

	out := buildAvailableList(schemas, bundled, applied, nil)
	byName := map[string]AvailableSchemaItem{}
	for _, a := range out {
		byName[a.Name] = a
	}

	// (a) applied older + registry newer → upgrade from the highest version.
	up, ok := byName["multi-agent"]
	if !ok {
		t.Fatalf("multi-agent should surface as an upgrade: %+v", out)
	}
	if !up.Upgrade || up.Source != "registry" || up.Version != "2.0.0" || up.ID != "s1-new" {
		t.Errorf("multi-agent upgrade = %+v", up)
	}

	// (b) applied at same or newer version → hidden.
	if _, ok := byName["session-message-types"]; ok {
		t.Errorf("same-version registry pack must stay hidden: %+v", out)
	}
	if _, ok := byName["vendor-pack"]; ok {
		t.Errorf("older-than-applied registry pack must stay hidden: %+v", out)
	}

	// (c) not applied → plain available (Upgrade=false).
	av, ok := byName["custom-pack"]
	if !ok || av.Upgrade || av.Source != "registry" {
		t.Errorf("custom-pack should be a plain registry available: %+v", av)
	}

	// global schemas remain skipped.
	if _, ok := byName["global-pack"]; ok {
		t.Errorf("global schema should be skipped")
	}

	// (d) bundled upgrade path unchanged.
	bup, ok := byName["agent-notes"]
	if !ok || !bup.Upgrade || bup.Source != "bundled" || bup.Version != "2.0.0" {
		t.Errorf("bundled upgrade = %+v, want bundled v2.0.0 Upgrade=true", bup)
	}
}

// TestBuildAvailableListExcludesOverridePack verifies the project-owned
// copy-on-write override pack never leaks into the "Available schemas" list,
// while a normal project-scoped registry pack still appears.
func TestBuildAvailableListExcludesOverridePack(t *testing.T) {
	schemas := []SchemaInfo{
		{ID: "ov", Name: overridePackName, Version: overridePackVersion, ProjectID: "proj"},
		{ID: "p1", Name: "custom-pack", Version: "1.0.0", ProjectID: "proj"},
	}
	out := buildAvailableList(schemas, nil, nil, nil)
	if len(out) != 1 || out[0].Name != "custom-pack" {
		t.Fatalf("available = %+v, want only custom-pack", out)
	}
}

// TestBuildBundledDetail verifies the bundled pack → detail conversion parses
// object types (with properties) and relationship types.
func TestBuildBundledDetail(t *testing.T) {
	bp := &BundledBlueprint{
		ID:      "personal-memory",
		Name:    "personal-memory",
		Version: "1.0.0",
		ObjectTypeSchemas: []map[string]any{
			{
				"name":  "person",
				"label": "Person",
				"properties": map[string]any{
					"first_name": map[string]any{"type": "string", "description": "First name"},
					"active":     map[string]any{"type": "boolean", "required": true},
				},
			},
		},
		RelationshipTypeSchemas: []map[string]any{
			{"name": "assigned_to", "sourceType": "task", "targetType": "person"},
		},
	}
	d := buildBundledDetail(bp)
	if d.Name != "personal-memory" || d.Source != "bundled" {
		t.Errorf("detail = %+v", d)
	}
	if len(d.ObjectTypes) != 1 || d.ObjectTypes[0].Name != "person" || d.ObjectTypes[0].Label != "Person" {
		t.Fatalf("object types = %+v", d.ObjectTypes)
	}
	props := d.ObjectTypes[0].Properties
	if len(props) != 2 {
		t.Fatalf("properties = %+v", props)
	}
	if props[0].Name != "active" || props[0].Type != "boolean" || !props[0].Required {
		t.Errorf("props[0] = %+v (sorted: active before first_name)", props[0])
	}
	if len(d.RelationshipTypes) != 1 || d.RelationshipTypes[0].SourceType != "task" {
		t.Errorf("relationship types = %+v", d.RelationshipTypes)
	}
}

// --- API handlers ---

func newBlueprintServer(f *fakeMemory, dir string) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory", BlueprintsDir: dir}, memory: f}
	e := echo.New()
	api := e.Group("/api")
	api.GET("/blueprints/installed", s.listInstalledBlueprints)
	api.GET("/blueprints/available", s.listAvailableBlueprints)
	api.GET("/blueprints/compiled-types", s.getCompiledTypes)
	api.POST("/blueprints/install", s.installBlueprint)
	api.POST("/blueprints/:id/unapply", s.unapplyBlueprint)
	return s, e
}

func TestListInstalledBlueprintsHandler(t *testing.T) {
	f := &fakeMemory{appliedBlueprints: []AppliedBlueprint{{BlueprintID: "bp1", Name: "personal-memory", Version: "1.0.0"}}}
	_, e := newBlueprintServer(f, t.TempDir())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blueprints/installed", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []AppliedBlueprint
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "personal-memory" {
		t.Fatalf("installed = %+v", got)
	}
}

func TestListAvailableBlueprintsHandler(t *testing.T) {
	f := &fakeMemory{schemas: []SchemaInfo{
		{ID: "s9", Name: "multi-agent", Version: "1.0.0", ProjectID: "proj"},
		{ID: "s2", Name: "session-message-types", Version: "1.0.0"}, // global — skipped
	}}
	dir := t.TempDir()
	writeBundledPack(t, filepath.Join(dir, "agent-notes"), "schemas", "name: agent-notes\nversion: 1.0.0\n")
	_, e := newBlueprintServer(f, dir)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blueprints/available", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []AvailableSchemaItem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d available, want 2 (registry + bundled): %+v", len(got), got)
	}
	sources := map[string]string{}
	for _, a := range got {
		sources[a.Name] = a.Source
	}
	if sources["multi-agent"] != "registry" {
		t.Errorf("multi-agent source = %q", sources["multi-agent"])
	}
	if sources["agent-notes"] != "bundled" {
		t.Errorf("agent-notes source = %q", sources["agent-notes"])
	}
	if _, ok := sources["session-message-types"]; ok {
		t.Errorf("global schema should be skipped")
	}
}

func TestGetCompiledTypesHandler(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", Label: "Person"}}}}
	_, e := newBlueprintServer(f, t.TempDir())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blueprints/compiled-types", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"person"`) {
		t.Errorf("compiled types missing person: %s", rec.Body.String())
	}
}

func TestInstallBlueprintRegistryHandler(t *testing.T) {
	f := &fakeMemory{}
	_, e := newBlueprintServer(f, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/blueprints/install", strings.NewReader(`{"schemaId":"s1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp-new" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}
	if f.createdBlueprint == nil || f.createdBlueprint.Name != "s1" {
		t.Fatalf("createdBlueprint = %+v, want name s1", f.createdBlueprint)
	}
}

func TestInstallBlueprintBundledHandler(t *testing.T) {
	f := &fakeMemory{}
	dir := t.TempDir()
	writeBundledPack(t, filepath.Join(dir, "agent-notes"), "schemas",
		"name: agent-notes\nversion: 1.0.0\nobjectTypeSchemas:\n  - name: Note\n")
	_, e := newBlueprintServer(f, dir)
	req := httptest.NewRequest(http.MethodPost, "/api/blueprints/install", strings.NewReader(`{"source":"bundled","name":"agent-notes"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.createdBlueprint == nil || f.createdBlueprint.Name != "agent-notes" {
		t.Fatalf("createdBlueprint = %+v, want agent-notes", f.createdBlueprint)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp-new" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}
}

func TestInstallBlueprintValidation(t *testing.T) {
	_, e := newBlueprintServer(&fakeMemory{}, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/blueprints/install", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestBlueprintMemoryError(t *testing.T) {
	f := &fakeMemory{blueprintErr: fmt.Errorf("memory 404 not_found")}
	_, e := newBlueprintServer(f, t.TempDir())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blueprints/installed", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

// --- UI render ---

func TestRenderBlueprintsPage(t *testing.T) {
	applied := []AppliedBlueprint{
		{BlueprintID: "bp1", Name: "agent-notes", Version: "1.0.0", AppliedAt: "2026-08-26T10:00:00Z"},
		{BlueprintID: "bp2", Name: "operator", Version: "1.0.0", AppliedAt: "2026-08-26T11:00:00Z"},
	}
	upgrades := []AvailableSchemaItem{
		{ID: "agent-notes", Name: "agent-notes", Version: "2.0.0", Source: "bundled", Upgrade: true},
	}
	available := []AvailableSchemaItem{
		{ID: "s9", Name: "session-message-types", Version: "1.0.0", Source: "registry"},
		{ID: "code-memory", Name: "code-memory", Version: "1.7.0", Source: "bundled"},
		{ID: "personal-memory", Name: "personal-memory", Version: "1.0.0", Source: "bundled"},
	}
	drafts := []BlueprintRecord{
		{ID: "bp-draft", Name: "custom-flow", Version: "0.3.0", Status: "draft"},
	}
	html := renderHTML(t, BlueprintsPage(applied, upgrades, available, drafts, nil, "", nil))
	for _, want := range []string{
		"Blueprints", "Installed", "agent-notes", "operator",
		"Upgrades", "Upgrade",
		"Available", "session-message-types", "code-memory", "registry", "bundled",
		`/blueprints/bp1/unapply`,
		`name="schemaId"`,
		// drafts section
		"Drafts", "custom-flow", "v0.3.0 · draft", `/blueprints/bp-draft/install`,
		// bundled + registry packs share one install form (by name / schemaId)
		`action="/blueprints/install"`, "Install", `name="name"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("blueprints page missing %q", want)
		}
	}

	htmlEmpty := renderHTML(t, BlueprintsPage(nil, nil, nil, nil, nil, "", nil))
	for _, want := range []string{"No blueprints installed", "No blueprints available"} {
		if !strings.Contains(htmlEmpty, want) {
			t.Errorf("empty state missing %q", want)
		}
	}
	for _, absent := range []string{"Drafts", "No blueprints in progress"} {
		if strings.Contains(htmlEmpty, absent) {
			t.Errorf("drafts section should be hidden when empty, found %q", absent)
		}
	}

	htmlErr := renderHTML(t, BlueprintsPage(nil, nil, nil, nil, errTest, "", nil))
	if !strings.Contains(htmlErr, "Failed to load blueprints") {
		t.Error("error state missing")
	}

	htmlFlash := renderHTML(t, BlueprintsPage(nil, nil, nil, nil, nil, "Blueprint installed.", nil))
	if !strings.Contains(htmlFlash, "Blueprint installed.") {
		t.Error("flash message missing")
	}
}

// TestRenderAppliedRowUpdateCTA verifies the installed row exposes the update
// affordance — an "Update available" badge plus an Update action wired to the
// existing /blueprints/install path — when a newer version of the pack exists,
// for both bundled (by name) and registry (by schemaId) sources, and hides it
// (while keeping Remove) when no upgrade exists.
func TestRenderAppliedRowUpdateCTA(t *testing.T) {
	b := AppliedBlueprint{BlueprintID: "bp1", Name: "agent-notes", Version: "1.0.0", AppliedAt: "2026-08-26T10:00:00Z"}

	bundled := renderHTML(t, appliedRow(b, AvailableSchemaItem{ID: "agent-notes", Name: "agent-notes", Version: "2.0.0", Source: "bundled"}))
	for _, want := range []string{"Update available", "Update", `action="/blueprints/install"`, `name="name"`, "Remove"} {
		if !strings.Contains(bundled, want) {
			t.Errorf("bundled upgrade row missing %q", want)
		}
	}

	registry := renderHTML(t, appliedRow(b, AvailableSchemaItem{ID: "s9", Name: "agent-notes", Version: "2.0.0", Source: "registry", Upgrade: true}))
	for _, want := range []string{"Update available", `name="schemaId"`, `value="s9"`} {
		if !strings.Contains(registry, want) {
			t.Errorf("registry upgrade row missing %q", want)
		}
	}
	if strings.Contains(registry, `name="name"`) {
		t.Error("registry upgrade row must install by schemaId, not name")
	}

	plain := renderHTML(t, appliedRow(b, AvailableSchemaItem{}))
	if strings.Contains(plain, "Update available") {
		t.Error("row without an upgrade must not show the update affordance")
	}
	if !strings.Contains(plain, "Remove") {
		t.Error("row without an upgrade must still offer Remove")
	}
}

// TestUIRouteBlueprints exercises the /blueprints page route against the fake
// backend, including the install/enable/unapply PRG redirects.
func TestUIRouteBlueprints(t *testing.T) {
	dir := t.TempDir()
	writeBundledPack(t, filepath.Join(dir, "agent-notes"), "schemas",
		"name: agent-notes\nversion: 1.0.0\nobjectTypeSchemas:\n  - name: Note\n")
	f := &fakeMemory{appliedBlueprints: []AppliedBlueprint{{BlueprintID: "bp1", Name: "personal-memory", Version: "1.0.0"}}}
	s := &Server{cfg: Config{DefaultAgent: "memory", BlueprintsDir: dir}, memory: f}
	e := echo.New()
	e.GET("/blueprints", s.uiBlueprints)
	e.POST("/blueprints/install", s.uiInstallBlueprint)
	e.POST("/blueprints/enable", s.uiEnableBlueprint)
	e.POST("/blueprints/:id/install", s.uiInstallBlueprintById)
	e.POST("/blueprints/:id/unapply", s.uiUnapplyBlueprint)

	// page render
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blueprints", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "personal-memory") {
		t.Fatalf("page: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// install redirect (registry)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=s9", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("install location = %q", loc)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp-new" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}

	// enable redirect (bundled seed, no apply)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/enable?name=agent-notes", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("enable redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?enabled=1" {
		t.Errorf("enable location = %q", loc)
	}
	if f.createdBlueprint == nil || f.createdBlueprint.Name != "agent-notes" {
		t.Fatalf("createdBlueprint = %+v, want agent-notes", f.createdBlueprint)
	}
	if len(f.appliedBlueprint) != 1 {
		t.Fatalf("enable must not apply, applied = %v", f.appliedBlueprint)
	}

	// install-by-id redirect (apply seeded blueprint)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/bp-new/install", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install-by-id redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("install-by-id location = %q", loc)
	}
	if len(f.appliedBlueprint) != 2 || f.appliedBlueprint[1] != "bp-new" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}

	// unapply redirect
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/bp1/unapply", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unapply redirect status = %d", rec.Code)
	}
	if len(f.unappliedBlueprint) != 1 || f.unappliedBlueprint[0] != "bp1" {
		t.Fatalf("unappliedBlueprint = %v", f.unappliedBlueprint)
	}
}

// TestUIRouteInstallPureAgentIgnoresPreExistingDrift installs the pure-agent
// operator blueprint while unrelated stale objects already exist (the drift
// scan returns the same stale count before and after). The install cannot have
// caused that drift, so it must redirect back to /blueprints — never to the
// migrations page.
func TestUIRouteInstallPureAgentIgnoresPreExistingDrift(t *testing.T) {
	f := &fakeMemory{}
	// Every ValidateSchemas call reports 3 stale objects: pre-existing drift,
	// unchanged by the operator install.
	f.validateFn = func(context.Context) (*SchemaValidationResult, error) {
		return &SchemaValidationResult{TotalObjects: 10, StaleObjects: 3}, nil
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/blueprints/install", s.uiInstallBlueprint)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?name=operator", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("operator install location = %q, want /blueprints?installed=1", loc)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp-new" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}
}

// TestUIRouteInstallSchemaRedirectsOnIncreasedDrift installs a schema-bearing
// blueprint that raises the stale-object count (before low, after high). The
// install caused the drift, so the redirect must go to the migrations page with
// the before/after counts in the message.
func TestUIRouteInstallSchemaRedirectsOnIncreasedDrift(t *testing.T) {
	f := &fakeMemory{}
	// First scan (before install): 1 stale. Second scan (after): 4 stale.
	calls := 0
	f.validateFn = func(context.Context) (*SchemaValidationResult, error) {
		calls++
		if calls == 1 {
			return &SchemaValidationResult{TotalObjects: 10, StaleObjects: 1}, nil
		}
		return &SchemaValidationResult{TotalObjects: 10, StaleObjects: 4}, nil
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/blueprints/install", s.uiInstallBlueprint)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=s9", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install redirect status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/blueprints/migrations?migrateMsg=") {
		t.Fatalf("install location = %q, want migrations redirect", loc)
	}
	msg, err := url.QueryUnescape(strings.TrimPrefix(loc, "/blueprints/migrations?migrateMsg="))
	if err != nil {
		t.Fatalf("unescape migrateMsg: %v", err)
	}
	if !strings.Contains(msg, "stale objects went from 1 to 4") {
		t.Errorf("migrateMsg missing before/after counts: %q", msg)
	}
	if calls != 2 {
		t.Errorf("ValidateSchemas calls = %d, want 2 (before + after)", calls)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp-new" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}
}

// TestUIRouteInstallSchemaStaysWhenDriftUnchanged installs a schema-bearing
// blueprint that leaves the stale count equal. No new drift was introduced, so
// the redirect goes back to /blueprints.
func TestUIRouteInstallSchemaStaysWhenDriftUnchanged(t *testing.T) {
	f := &fakeMemory{}
	calls := 0
	f.validateFn = func(context.Context) (*SchemaValidationResult, error) {
		calls++
		return &SchemaValidationResult{TotalObjects: 10, StaleObjects: 2}, nil
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/blueprints/install", s.uiInstallBlueprint)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=s9", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("install location = %q, want /blueprints?installed=1", loc)
	}
	if calls != 2 {
		t.Errorf("ValidateSchemas calls = %d, want 2 (before + after)", calls)
	}
}

// --- blueprint details ---

func TestObjectTypesFromRaw(t *testing.T) {
	raw := json.RawMessage(`[{"name":"person","label":"Person","properties":{"first_name":{"type":"string"}}}]`)
	types := objectTypesFromRaw(raw)
	if len(types) != 1 || types[0].Name != "person" {
		t.Fatalf("types = %+v", types)
	}
	if len(types[0].Properties) != 1 || types[0].Properties[0].Name != "first_name" || types[0].Properties[0].Type != "string" {
		t.Errorf("properties = %+v", types[0].Properties)
	}
	if objectTypesFromRaw(nil) != nil {
		t.Error("nil raw should return nil")
	}
	if objectTypesFromRaw(json.RawMessage(`{"person":{"label":"Person"}}`)) == nil {
		t.Error("map-form fallback should parse")
	}
}

func TestRenderBlueprintDetailPage(t *testing.T) {
	d := &BlueprintDetail{
		ID:          "personal-memory",
		Name:        "personal-memory",
		Version:     "1.0.0",
		Description: "Personal knowledge graph",
		Author:      "diane",
		Source:      "bundled",
		ObjectTypes: []ObjectTypeDetail{
			{Name: "person", Label: "Person", Description: "a person", Properties: []PropertyDetail{
				{Name: "first_name", Type: "string", Description: "First name"},
				{Name: "active", Type: "boolean", Required: true},
			}},
		},
		RelationshipTypes: []RelationshipTypeDetail{
			{Name: "assigned_to", SourceType: "task", TargetType: "person"},
		},
	}
	html := renderHTML(t, BlueprintDetailPage(d, nil))
	for _, want := range []string{
		"personal-memory", "v1.0.0", "Personal knowledge graph", "diane", "bundled",
		"person", "Person", "first_name", "string", "boolean", "required",
		"assigned_to", "task → person",
		`href="/blueprints"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page missing %q", want)
		}
	}

	htmlErr := renderHTML(t, BlueprintDetailPage(nil, errTest))
	if !strings.Contains(htmlErr, "Blueprint unavailable") {
		t.Error("error state missing")
	}
}

// TestRenderOperatorBlueprintDetail verifies the operator blueprint's detail
// page surfaces the full agent config — name, model, tool grant, and system
// prompt — alongside the (empty) object/relationship sections.
func TestRenderOperatorBlueprintDetail(t *testing.T) {
	bp, err := loadBundledFromFS(bundledFS, "blueprints/operator")
	if err != nil {
		t.Fatal(err)
	}
	d := buildBundledDetail(bp)
	if len(d.Agents) != 1 {
		t.Fatalf("agents = %d, want 1", len(d.Agents))
	}
	html := renderHTML(t, BlueprintDetailPage(d, nil))
	for _, want := range []string{
		"operator", "openai/deepseek-v4-pro", "agentType", "ask_user", "agent-def-*", "skill-*",
		"Tools", "System prompt", "Propose before write", "Accept", "Reject",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("operator detail missing %q", want)
		}
	}
}

func TestUIRouteBlueprintDetail(t *testing.T) {
	// bundled pack detail (resolves by directory name)
	f := &fakeMemory{}
	dir := t.TempDir()
	writeBundledPack(t, filepath.Join(dir, "agent-notes"), "schemas",
		"name: agent-notes\nversion: 1.0.0\nobjectTypeSchemas:\n  - name: Note\n    label: Note\n")
	s := &Server{cfg: Config{DefaultAgent: "memory", BlueprintsDir: dir}, memory: f}
	e := echo.New()
	e.GET("/blueprints/:id", s.uiBlueprint)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blueprints/agent-notes", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Note") {
		t.Fatalf("bundled detail: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// registry schema detail (resolves by schema id via GetSchema)
	f2 := &fakeMemory{schema: &BlueprintSchema{ID: "s9", Name: "multi-agent", Version: "1.0.0",
		ObjectTypeSchemas: json.RawMessage(`[{"name":"Task","label":"Task"}]`)}}
	s2 := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f2}
	e2 := echo.New()
	e2.GET("/blueprints/:id", s2.uiBlueprint)
	rec = httptest.NewRecorder()
	e2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blueprints/s9", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Task") {
		t.Fatalf("registry detail: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// unknown id → error page
	f3 := &fakeMemory{blueprintErr: fmt.Errorf("memory 404 not_found")}
	s3 := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f3}
	e3 := echo.New()
	e3.GET("/blueprints/:id", s3.uiBlueprint)
	rec = httptest.NewRecorder()
	e3.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blueprints/nope", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Blueprint unavailable") {
		t.Fatalf("error detail: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestModelProviderOf covers the provider-prefix extraction (the substring
// before the first "/") used to judge whether an agent or blueprint-pinned
// model is servable by the configured providers.
func TestModelProviderOf(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"openai/deepseek-v4-pro", "openai"},
		{"deepseek/deepseek-v4-flash", "deepseek"},
		{"google/gemini-3-pro", "google"},
		{"gpt-4o", ""},   // bare model, no provider prefix
		{"", ""},         // empty model
		{"/leading", ""}, // nothing before the slash
		{"trailing/", "trailing"},
	}
	for _, tc := range cases {
		if got := modelProviderOf(tc.model); got != tc.want {
			t.Errorf("modelProviderOf(%q) = %q, want %q", tc.model, got, tc.want)
		}
	}
}

// TestAgentPinnedModelIssue covers the blueprint-agent-row model availability
// warning: an error when the project has no configured provider or none
// matches the pinned model's provider prefix, and no warning for an empty
// model, a bare model (no prefix), or a model whose provider is configured.
func TestAgentPinnedModelIssue(t *testing.T) {
	// zero providers configured → error, zero-provider copy
	sev, msg := agentPinnedModelIssue("operator", "openai/deepseek-v4-pro", nil)
	if sev != "error" || !strings.Contains(msg, "This blueprint defines agent operator pinned to openai/deepseek-v4-pro") || !strings.Contains(msg, "no provider is configured in this project") {
		t.Errorf("zero providers: sev=%q msg=%q", sev, msg)
	}

	// provider prefix missing from the configured set → error, prefix copy
	sev, msg = agentPinnedModelIssue("operator", "anthropic/claude-sonnet-4", []string{"openai"})
	if sev != "error" || !strings.Contains(msg, "needs the anthropic provider") || !strings.Contains(msg, "not configured in this project") {
		t.Errorf("missing provider: sev=%q msg=%q", sev, msg)
	}

	// provider prefix configured → satisfied, no warning
	if sev, msg = agentPinnedModelIssue("operator", "openai/deepseek-v4-pro", []string{"openai"}); sev != "" || msg != "" {
		t.Errorf("matching provider: sev=%q msg=%q", sev, msg)
	}

	// bare model (no prefix) with providers configured → satisfied
	if sev, msg = agentPinnedModelIssue("operator", "deepseek-v4-pro", []string{"openai"}); sev != "" || msg != "" {
		t.Errorf("bare model: sev=%q msg=%q", sev, msg)
	}

	// no model → no warning
	if sev, msg = agentPinnedModelIssue("operator", "", []string{"openai"}); sev != "" || msg != "" {
		t.Errorf("empty model: sev=%q msg=%q", sev, msg)
	}
}

// TestRenderBlueprintPinnedModelWarning asserts the blueprint details page
// renders an error warning under an agent row whose pinned model's provider
// isn't configured (zero providers, or a prefix missing from the configured
// set) and renders none once a matching provider is configured.
func TestRenderBlueprintPinnedModelWarning(t *testing.T) {
	bp, err := loadBundledFromFS(bundledFS, "blueprints/operator")
	if err != nil {
		t.Fatal(err)
	}

	// zero providers → error warning under the agent row
	html := renderHTML(t, BlueprintDetailPage(buildBundledDetail(bp), nil))
	for _, want := range []string{
		"This blueprint defines agent operator pinned to openai/deepseek-v4-pro",
		"no provider is configured in this project",
		`href="/settings/providers"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("zero-provider blueprint detail missing %q", want)
		}
	}

	// provider matching the pinned model's prefix → no warning
	satisfied := buildBundledDetail(bp)
	satisfied.ProviderNames = []string{"openai"}
	htmlOK := renderHTML(t, BlueprintDetailPage(satisfied, nil))
	if strings.Contains(htmlOK, "This blueprint defines agent") {
		t.Error("satisfied pinned model must not render the blueprint warning")
	}
	// the model badge itself stays visible in both states
	if !strings.Contains(htmlOK, "openai/deepseek-v4-pro") {
		t.Error("satisfied detail missing the model badge")
	}
}

// TestRenderBlueprintDetailVersionsAndDiff locks the version-history + diff-card
// markup contract: breadcrumb canonical link, title-adornment version badge,
// version rows with installed/viewing state, the draft comparison card (selector
// and added/removed/changed tints), and the derive-version prefill.
func TestRenderBlueprintDetailVersionsAndDiff(t *testing.T) {
	d := &BlueprintDetail{
		ID:               "v2",
		Name:             "personal-memory",
		Version:          "2.0.0",
		Source:           "blueprint",
		Status:           "draft",
		IsDraft:          true,
		CanDeriveVersion: true,
		CanonicalID:      "v3",
		SuggestedVersion: "2.0.1",
		Installed:        true,
		Versions: []BlueprintVersionView{
			{ID: "v3", Version: "3.0.0", Status: "published", Installed: true},
			{ID: "v2", Version: "2.0.0", Status: "draft", Current: true},
			{ID: "v1", Version: "1.0.0", Status: "published"},
		},
		CompareOptions: []BlueprintVersionView{
			{ID: "v3", Version: "3.0.0", Status: "published"},
			{ID: "v1", Version: "1.0.0", Status: "published"},
		},
		CompareVersion: "1.0.0",
		Diff: &BlueprintDiff{
			BaseVersion:   "1.0.0",
			TargetVersion: "2.0.0",
			ObjectTypes: []ObjectTypeDiff{
				{
					Name:   "company",
					Change: DiffChangeAdded,
					Label:  FieldDiff{After: "Company", Change: DiffChangeAdded},
					Properties: []PropertyDiff{
						{Name: "ticker", Change: DiffChangeAdded, Type: FieldDiff{After: "string", Change: DiffChangeAdded}},
					},
				},
				{
					Name:   "legacy",
					Change: DiffChangeRemoved,
					Label:  FieldDiff{Before: "Legacy", Change: DiffChangeRemoved},
				},
				{
					Name:   "person",
					Change: DiffChangeChanged,
					Label:  FieldDiff{Before: "Person", After: "Person"},
					Properties: []PropertyDiff{
						{Name: "Wave", Change: DiffChangeChanged, Type: FieldDiff{Before: "number", After: "string", Change: DiffChangeChanged}},
					},
				},
			},
			RelationshipTypes: []RelationshipTypeDiff{
				{
					Name:       "assigned_to",
					Change:     DiffChangeChanged,
					Label:      FieldDiff{Before: "Assigned", After: "Assigned to", Change: DiffChangeChanged},
					SourceType: FieldDiff{Before: "task", After: "task"},
					TargetType: FieldDiff{Before: "person", After: "person"},
				},
			},
			Agents: []AgentDiff{
				{
					Name:         "operator",
					Change:       DiffChangeChanged,
					SystemPrompt: FieldDiff{Before: "old prompt", After: "new prompt", Change: DiffChangeChanged},
					Tools:        ChipDiff{Added: []string{"search"}},
					Skills:       ChipDiff{Removed: []string{"legacy-skill"}},
				},
			},
		},
	}

	html := renderHTML(t, BlueprintDetailPage(d, nil))
	for _, want := range []string{
		// 1. breadcrumb: canonical name link + active version label
		`href="/blueprints/v3"`,
		"v2.0.0",
		// 2. title-adornment version badge
		"lucide--tag",
		// 3. every version row links to its exact version
		`href="/blueprints/v1"`,
		`href="/blueprints/v2"`,
		`href="/blueprints/v3"`,
		// 4. installed + viewing state on the current row
		"Installed",
		"Viewing",
		`aria-current="true"`,
		// 5. diff card markers + tints + change chips
		"Changes",
		"vs",
		`name="compare"`,
		"bg-success/10",
		"bg-error/10",
		"bg-warning/10",
		"Added",
		"Removed",
		"Changed",
		// diff content
		"company",
		"legacy",
		"person",
		"Wave",
		"assigned_to",
		"operator",
		"old prompt",
		"new prompt",
		"legacy-skill",
		"Tools",
		"Skills",
		// 6. derive-version prefill
		`value="2.0.1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("version/diff detail missing %q", want)
		}
	}

	// Negative case: a bundled detail (no history, not a draft) must render
	// neither the diff card nor its comparison selector.
	plain := &BlueprintDetail{
		ID:      "agent-notes",
		Name:    "agent-notes",
		Version: "1.0.0",
		Source:  "bundled",
	}
	plainHTML := renderHTML(t, BlueprintDetailPage(plain, nil))
	for _, gone := range []string{"Changes", `name="compare"`, "Viewing"} {
		if strings.Contains(plainHTML, gone) {
			t.Errorf("bundled detail must not render %q", gone)
		}
	}
}
