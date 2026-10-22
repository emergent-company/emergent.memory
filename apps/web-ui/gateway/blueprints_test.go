package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// --- available list (published blueprints in scope) ---

// TestBuildAvailableList verifies the available list maps published blueprint
// records to items: built-ins surface as Source "builtin" (Origin "builtin"),
// everything else as Source "registry" (Origin user/import), drafts are
// excluded, and applied blueprints are excluded by name+version or id.
func TestBuildAvailableList(t *testing.T) {
	records := []BlueprintRecord{
		{ID: "b1", Name: "agent-notes", Version: "1.0.0", Origin: "builtin", Status: "published"},
		{ID: "b2", Name: "agent-notes", Version: "2.0.0", Origin: "builtin", Status: "published"},
		{ID: "u1", Name: "custom-flow", Version: "1.0.0", Origin: "user", Status: "published"},
		{ID: "d1", Name: "draft-thing", Version: "1.0.0", Origin: "user", Status: "draft"},
		{ID: "i1", Name: "imported", Version: "1.0.0", Origin: "import", Status: "published"},
	}
	applied := []AppliedBlueprint{
		{BlueprintID: "u1", Name: "custom-flow", Version: "1.0.0"},
	}
	out := buildAvailableList(records, applied)
	byName := map[string]AvailableSchemaItem{}
	for _, a := range out {
		byName[a.Name] = a
	}

	if _, ok := byName["draft-thing"]; ok {
		t.Error("draft blueprint must not surface in the available list")
	}
	if _, ok := byName["custom-flow"]; ok {
		t.Error("applied blueprint must be excluded")
	}

	an, ok := byName["agent-notes"]
	if !ok || an.ID != "b2" || an.Version != "2.0.0" || an.Source != "builtin" || an.Origin != "builtin" {
		t.Errorf("agent-notes = %+v, want latest built-in v2.0.0", an)
	}

	im, ok := byName["imported"]
	if !ok || im.Source != "registry" || im.Origin != "import" {
		t.Errorf("imported = %+v, want registry/import", im)
	}
}

// TestBuildAvailableListUpgrade verifies a published version strictly newer than
// the applied one surfaces with Upgrade=true, while same-or-older versions are
// filtered out.
func TestBuildAvailableListUpgrade(t *testing.T) {
	records := []BlueprintRecord{
		{ID: "b2", Name: "agent-notes", Version: "2.0.0", Origin: "builtin", Status: "published"},
		{ID: "p1", Name: "personal-memory", Version: "2.0.0", Origin: "user", Status: "published"},
	}
	applied := []AppliedBlueprint{
		{BlueprintID: "b1", Name: "agent-notes", Version: "1.0.0"},
		{BlueprintID: "p0", Name: "personal-memory", Version: "2.0.0"},
	}
	out := buildAvailableList(records, applied)
	if len(out) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(out), out)
	}
	if out[0].Name != "agent-notes" || !out[0].Upgrade {
		t.Errorf("agent-notes should surface as an upgrade: %+v", out[0])
	}
}

// TestBuildAvailableListExcludesAppliedByID verifies a blueprint whose name no
// longer matches the applied record is still excluded by blueprint id.
func TestBuildAvailableListExcludesAppliedByID(t *testing.T) {
	records := []BlueprintRecord{
		{ID: "u1", Name: "renamed-pack", Version: "1.0.0", Origin: "user", Status: "published"},
	}
	applied := []AppliedBlueprint{
		{BlueprintID: "u1", Name: "old-name", Version: "1.0.0"},
	}
	if out := buildAvailableList(records, applied); len(out) != 0 {
		t.Fatalf("applied-by-id blueprint must be excluded: %+v", out)
	}
}

// --- API handlers ---

func newBlueprintServer(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
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
	_, e := newBlueprintServer(f)
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
	f := &fakeMemory{blueprints: []BlueprintRecord{
		{ID: "b1", Name: "multi-agent", Version: "1.0.0", Origin: "builtin", Status: "published"},
		{ID: "u1", Name: "custom-pack", Version: "1.0.0", Origin: "user", Status: "published"},
	}}
	_, e := newBlueprintServer(f)
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
		t.Fatalf("got %d available, want 2: %+v", len(got), got)
	}
	sources := map[string]string{}
	for _, a := range got {
		sources[a.Name] = a.Source
	}
	if sources["multi-agent"] != "builtin" {
		t.Errorf("multi-agent source = %q, want builtin", sources["multi-agent"])
	}
	if sources["custom-pack"] != "registry" {
		t.Errorf("custom-pack source = %q, want registry", sources["custom-pack"])
	}
}

func TestGetCompiledTypesHandler(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", Label: "Person"}}}}
	_, e := newBlueprintServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/blueprints/compiled-types", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"person"`) {
		t.Errorf("compiled types missing person: %s", rec.Body.String())
	}
}

func TestInstallBlueprintHandlerAppliesByID(t *testing.T) {
	f := &fakeMemory{}
	_, e := newBlueprintServer(f)
	req := httptest.NewRequest(http.MethodPost, "/api/blueprints/install", strings.NewReader(`{"schemaId":"b1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "b1" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}
	if f.createdBlueprint != nil {
		t.Errorf("install by id must not create a blueprint definition: %+v", f.createdBlueprint)
	}
}

func TestInstallBlueprintValidation(t *testing.T) {
	_, e := newBlueprintServer(&fakeMemory{})
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
	_, e := newBlueprintServer(f)
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
	}
	upgrades := []AvailableSchemaItem{
		{ID: "b2", Name: "agent-notes", Version: "2.0.0", Source: "builtin", Origin: "builtin", Upgrade: true},
	}
	available := []AvailableSchemaItem{
		{ID: "b3", Name: "session-message-types", Version: "1.0.0", Source: "builtin", Origin: "builtin"},
		{ID: "u1", Name: "code-memory", Version: "1.7.0", Source: "registry", Origin: "user"},
	}
	drafts := []BlueprintRecord{
		{ID: "bp-draft", Name: "custom-flow", Version: "0.3.0", Status: "draft"},
	}
	html := renderHTML(t, BlueprintsPage(applied, upgrades, available, drafts, nil, "", nil))
	for _, want := range []string{
		"Blueprints", "Installed", "agent-notes",
		"Upgrades", "Upgrade",
		"Available", "session-message-types", "code-memory", "registry", "builtin",
		"Built-in",
		`/blueprints/bp1/unapply`,
		`name="schemaId"`,
		// drafts section
		"Drafts", "custom-flow", "v0.3.0 · draft", `/blueprints/bp-draft/install`,
		// install action always applies by id
		`action="/blueprints/install"`, "Install",
		// remove action rendered only for the registry (non-built-in) row
		`data-testid="blueprint-remove-u1"`,
		`action="/blueprints/u1/remove"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("blueprints page missing %q", want)
		}
	}
	// built-in rows must not offer the remove action
	if strings.Contains(html, `blueprint-remove-b3`) {
		t.Error("built-in blueprint must not render a remove action")
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
// affordance — an "Update available" badge plus an Update action that installs
// by blueprint id — and hides it (while keeping Remove) when no upgrade exists.
func TestRenderAppliedRowUpdateCTA(t *testing.T) {
	b := AppliedBlueprint{BlueprintID: "bp1", Name: "agent-notes", Version: "1.0.0", AppliedAt: "2026-08-26T10:00:00Z"}

	upgrade := renderHTML(t, appliedRow(b, AvailableSchemaItem{ID: "b2", Name: "agent-notes", Version: "2.0.0", Source: "builtin", Upgrade: true}))
	for _, want := range []string{"Update available", "Update", `action="/blueprints/install"`, `name="schemaId"`, `value="b2"`, "Remove"} {
		if !strings.Contains(upgrade, want) {
			t.Errorf("upgrade row missing %q", want)
		}
	}
	if strings.Contains(upgrade, `name="name"`) {
		t.Error("upgrade row must install by schemaId (blueprint id), not name")
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
// backend, including the install/remove/unapply PRG redirects.
func TestUIRouteBlueprints(t *testing.T) {
	f := &fakeMemory{appliedBlueprints: []AppliedBlueprint{{BlueprintID: "bp1", Name: "personal-memory", Version: "1.0.0"}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/blueprints", s.uiBlueprints)
	e.POST("/blueprints/install", s.uiInstallBlueprint)
	e.POST("/blueprints/:id/install", s.uiInstallBlueprintById)
	e.POST("/blueprints/:id/remove", s.uiRemoveBlueprint)
	e.POST("/blueprints/:id/unapply", s.uiUnapplyBlueprint)

	// page render
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blueprints", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "personal-memory") {
		t.Fatalf("page: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// install redirect (by blueprint id)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=b9", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("install redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("install location = %q", loc)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "b9" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}

	// install-by-id redirect
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

	// remove redirect
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/u1/remove", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("remove redirect status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?removed=1" {
		t.Errorf("remove location = %q", loc)
	}
	if len(f.deletedBlueprints) != 1 || f.deletedBlueprints[0] != "u1" {
		t.Fatalf("deletedBlueprints = %v", f.deletedBlueprints)
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

// TestUIRouteRemoveBlueprintConflict verifies a 409 from DeleteBlueprint maps to
// the friendly "uninstall first" flash.
func TestUIRouteRemoveBlueprintConflict(t *testing.T) {
	f := &fakeMemory{deleteBlueprintErr: &memoryHTTPError{Status: http.StatusConflict, Code: "conflict", Message: "applied"}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/blueprints/:id/remove", s.uiRemoveBlueprint)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/u1/remove", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/blueprints?err=") {
		t.Fatalf("location = %q, want /blueprints?err=…", loc)
	}
	msg, err := url.QueryUnescape(strings.TrimPrefix(loc, "/blueprints?err="))
	if err != nil {
		t.Fatalf("unescape err: %v", err)
	}
	if !strings.Contains(msg, "Uninstall the blueprint before removing it") {
		t.Errorf("409 flash = %q, want the uninstall-first copy", msg)
	}
}

// TestUIRouteInstallSchemaRedirectsOnIncreasedDrift installs a blueprint that
// raises the stale-object count (before low, after high). The install caused
// the drift, so the redirect must go to the migrations page.
func TestUIRouteInstallSchemaRedirectsOnIncreasedDrift(t *testing.T) {
	f := &fakeMemory{}
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
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=b9", nil))
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
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "b9" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}
}

// TestUIRouteInstallSchemaStaysWhenDriftUnchanged installs a blueprint that
// leaves the stale count equal. No new drift was introduced, so the redirect
// goes back to /blueprints.
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
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=b9", nil))
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

// --- install model-review step ---

// newUIInstallRouteServer builds a bare server exposing the install PRG routes
// (list install, install-by-id, and the review confirm).
func newUIInstallRouteServer(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/blueprints/install", s.uiInstallBlueprint)
	e.POST("/blueprints/:id/install", s.uiInstallBlueprintById)
	e.POST("/blueprints/install/confirm", s.uiInstallBlueprintConfirm)
	return s, e
}

// pinnedBlueprint returns a published blueprint record whose manifest pins the
// given model on a single agent named "operator".
func pinnedBlueprint(id, model string) *BlueprintRecord {
	return &BlueprintRecord{
		ID:       id,
		Name:     "operator",
		Version:  "1.0.0",
		Status:   "published",
		Origin:   "builtin",
		Manifest: json.RawMessage(`{"agents":[{"name":"operator","model":{"name":"` + model + `"}}]}`),
	}
}

// TestUIRouteInstallBlueprintReviewWhenPinned verifies an install of a
// blueprint that pins a model renders the review page (one row per pinned
// agent) and does NOT apply yet.
func TestUIRouteInstallBlueprintReviewWhenPinned(t *testing.T) {
	f := &fakeMemory{blueprint: pinnedBlueprint("bp1", "openai/deepseek-v4-pro")}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=bp1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="blueprint-import-map"`,
		`name="model.operator"`,
		`action="/blueprints/install/confirm"`,
		`name="id"`,
		`value="bp1"`,
		"Blueprint: openai/deepseek-v4-pro",
		// No provider configured → the pinned model's prefix is unresolved.
		"is not configured in this project.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("review page missing %q", want)
		}
	}
	if len(f.appliedBlueprint) != 0 {
		t.Errorf("review step must not apply, applied = %v", f.appliedBlueprint)
	}
}

// TestUIRouteInstallBlueprintReviewPrefillServable verifies a servable pinned
// model (provider configured) is pre-filled and carries no issue, while a
// saved override still wins over the pinned model.
func TestUIRouteInstallBlueprintReviewPrefill(t *testing.T) {
	f := &fakeMemory{
		blueprint:        pinnedBlueprint("bp1", "openai/deepseek-v4-pro"),
		projectProviders: []ProjectProviderConfig{{Provider: "openai", Slug: "openai"}},
		modelsByProvider: map[string][]ProviderSupportedModel{
			"openai": {{ModelName: "deepseek-v4-pro", ModelType: "generative"}},
		},
	}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=bp1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Servable → the pinned model pre-fills and no issue is raised.
	if !strings.Contains(body, `value="openai/deepseek-v4-pro" selected`) {
		t.Errorf("servable pinned model must be pre-filled: %s", body)
	}
	if strings.Contains(body, "Configure a provider") {
		t.Errorf("servable pinned model must not show the configure-provider link: %s", body)
	}
}

// TestUIRouteInstallBlueprintReviewPrefillSavedOverride verifies a saved
// per-project override wins over the (servable) pinned model, rendering as a
// "(current)" fallback when it is not in the configured catalog.
func TestUIRouteInstallBlueprintReviewPrefillSavedOverride(t *testing.T) {
	f := &fakeMemory{
		blueprint:        pinnedBlueprint("bp1", "openai/deepseek-v4-pro"),
		projectProviders: []ProjectProviderConfig{{Provider: "openai", Slug: "openai"}},
		modelsByProvider: map[string][]ProviderSupportedModel{
			"openai": {{ModelName: "deepseek-v4-pro", ModelType: "generative"}},
		},
		modelOverrides: map[string]string{"operator": "openai/gpt-4o"},
	}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=bp1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="openai/gpt-4o" selected>openai/gpt-4o (current)`) {
		t.Errorf("saved override must pre-fill (with a (current) fallback): %s", body)
	}
	if f.modelOverridesName != "operator" {
		t.Errorf("GetModelOverrides name = %q, want operator (blueprint name)", f.modelOverridesName)
	}
}

// TestUIRouteInstallBlueprintDirectWhenNoPinnedModel verifies a blueprint with
// no pinned agents applies directly (no review step).
func TestUIRouteInstallBlueprintDirectWhenNoPinnedModel(t *testing.T) {
	f := &fakeMemory{blueprint: &BlueprintRecord{
		ID: "bp1", Name: "agent-notes", Version: "1.0.0", Status: "published",
		Manifest: json.RawMessage(`{"agents":[{"name":"operator"}]}`),
	}}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=bp1", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("location = %q, want /blueprints?installed=1", loc)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp1" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}
}

// TestUIRouteInstallBlueprintByIdReview verifies the draft/registry install-by-
// id path shows the review step for a pinned blueprint.
func TestUIRouteInstallBlueprintByIdReview(t *testing.T) {
	f := &fakeMemory{blueprint: pinnedBlueprint("bp1", "openai/deepseek-v4-pro")}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/bp1/install", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `data-testid="blueprint-import-map"`) {
		t.Errorf("install-by-id must show the review page: %s", body)
	}
	if len(f.appliedBlueprint) != 0 {
		t.Errorf("review step must not apply, applied = %v", f.appliedBlueprint)
	}
}

// TestUIRouteInstallBlueprintConfirm verifies the review confirm applies the
// blueprint with the chosen per-agent overrides.
func TestUIRouteInstallBlueprintConfirm(t *testing.T) {
	f := &fakeMemory{}
	_, e := newUIInstallRouteServer(f)

	values := url.Values{"id": {"bp1"}, "model.operator": {"openai/gpt-4o"}, "model.editor": {""}}
	req := httptest.NewRequest(http.MethodPost, "/blueprints/install/confirm", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("location = %q, want /blueprints?installed=1", loc)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp1" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}
	want := map[string]string{"operator": "openai/gpt-4o", "editor": ""}
	if len(f.applyOverrides) != len(want) {
		t.Fatalf("applyOverrides = %v, want %v", f.applyOverrides, want)
	}
	for k, v := range want {
		if got := f.applyOverrides[k]; got != v {
			t.Errorf("applyOverrides[%q] = %q, want %q", k, got, v)
		}
	}
}

// TestUIRouteInstallBlueprintConfirmMissingID verifies the confirm route
// short-circuits without a backend call when the id is missing.
func TestUIRouteInstallBlueprintConfirmMissingID(t *testing.T) {
	f := &fakeMemory{}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install/confirm", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?err=1" {
		t.Errorf("location = %q, want /blueprints?err=1", loc)
	}
	if len(f.appliedBlueprint) != 0 || f.applyOverrides != nil {
		t.Errorf("missing id must not call the backend: applied=%v overrides=%v", f.appliedBlueprint, f.applyOverrides)
	}
}

// TestBuildModelMapRows pins the review-row resolution: issue classification and
// the saved > servable-pinned > empty pre-fill precedence.
func TestBuildModelMapRows(t *testing.T) {
	manifest := blueprintManifest{
		Agents: []blueprintAgent{
			{Name: "bare", Model: &blueprintModel{Name: "gemini-2.5-flash"}},
			{Name: "servable", Model: &blueprintModel{Name: "openai/deepseek-v4-pro"}},
			{Name: "unconfigured", Model: &blueprintModel{Name: "anthropic/claude-3"}},
			{Name: "saved", Model: &blueprintModel{Name: "openai/deepseek-v4-pro"}},
			{Name: "cleared", Model: &blueprintModel{Name: "openai/deepseek-v4-pro"}},
			{Name: "nopin"},
		},
	}
	providerNames := []string{"openai"}
	saved := map[string]string{"saved": "openai/gpt-4o", "cleared": ""}

	rows := buildModelMapRows(manifest, providerNames, saved)
	byName := map[string]BlueprintModelMapRow{}
	for _, r := range rows {
		byName[r.AgentName] = r
	}

	if _, ok := byName["nopin"]; ok {
		t.Error("agent without a pinned model must not produce a row")
	}
	if len(rows) != 5 {
		t.Fatalf("row count = %d, want 5: %+v", len(rows), rows)
	}

	if r := byName["bare"]; r.Issue != "Set a provider for this model (expected provider/model)." || r.Selected != "" {
		t.Errorf("bare = %+v, want issue + empty selected", r)
	}
	if r := byName["servable"]; r.Issue != "" || r.Selected != "openai/deepseek-v4-pro" {
		t.Errorf("servable = %+v, want no issue + pinned pre-fill", r)
	}
	if r := byName["unconfigured"]; r.Issue != "The 'anthropic' provider is not configured in this project." || r.Selected != "" {
		t.Errorf("unconfigured = %+v, want issue + empty selected", r)
	}
	if r := byName["saved"]; r.Selected != "openai/gpt-4o" {
		t.Errorf("saved = %+v, want the saved override to win", r)
	}
	if r := byName["cleared"]; r.Selected != "" {
		t.Errorf("cleared = %+v, want an empty saved override to clear to project default", r)
	}
}

// TestModelOverridesFromForm pins the per-agent form parsing: `model.<agent>`
// fields map to overrides (empty value = project default), everything else is
// ignored.
func TestModelOverridesFromForm(t *testing.T) {
	form := url.Values{
		"model.writer": {"openai/gpt-4o"},
		"model.editor": {""},
		"id":           {"bp1"},
		"url":          {"https://github.com/org/repo"},
	}
	got := modelOverridesFromForm(form)
	want := map[string]string{"writer": "openai/gpt-4o", "editor": ""}
	if len(got) != len(want) {
		t.Fatalf("overrides = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("overrides[%q] = %q, want %q", k, got, v)
		}
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

// TestObjectTypesFromMapsBoardFields pins the parser mapping for the board
// work-status config (present and absent).
func TestObjectTypesFromMapsBoardFields(t *testing.T) {
	types := objectTypesFromMaps([]map[string]any{
		{"name": "Task", "boardEnabled": true, "allowedStatuses": []any{"ready", "done"}},
		{"name": "Note"},
	})
	if len(types) != 2 {
		t.Fatalf("types = %+v", types)
	}
	if !types[0].BoardEnabled {
		t.Error("Task should be board-enabled")
	}
	if got := types[0].AllowedStatuses; !slices.Equal(got, []string{"ready", "done"}) {
		t.Errorf("Task allowed statuses = %v, want [ready done]", got)
	}
	if types[1].BoardEnabled || len(types[1].AllowedStatuses) != 0 {
		t.Errorf("Note should carry no board data, got %+v", types[1])
	}
}

// TestBlueprintDetailBoardWorkStatuses pins the rendered contract for a
// board-enabled object type row.
func TestBlueprintDetailBoardWorkStatuses(t *testing.T) {
	board := &BlueprintDetail{
		ObjectTypes: []ObjectTypeDetail{
			{Name: "Task", Label: "Task", BoardEnabled: true, AllowedStatuses: []string{"ready", "done"}},
		},
	}
	html := renderHTML(t, BlueprintDetailPage(board, nil))
	for _, want := range []string{
		`data-testid="object-type-work"`,
		`data-status="ready"`,
		`data-status="done"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("board object type missing %q", want)
		}
	}

	plain := &BlueprintDetail{ObjectTypes: []ObjectTypeDetail{{Name: "Note", Label: "Note"}}}
	if got := renderHTML(t, BlueprintDetailPage(plain, nil)); strings.Contains(got, `data-testid="object-type-work"`) {
		t.Error("non-board object type should not render a work-status summary")
	}
}

func TestRenderBlueprintDetailPage(t *testing.T) {
	d := &BlueprintDetail{
		ID:          "bp1",
		Name:        "personal-memory",
		Version:     "1.0.0",
		Description: "Personal knowledge graph",
		Author:      "diane",
		Source:      "blueprint",
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
		"personal-memory", "v1.0.0", "Personal knowledge graph", "diane", "blueprint",
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

func TestUIRouteBlueprintDetail(t *testing.T) {
	// applied blueprint record detail (resolves by blueprint id)
	f := &fakeMemory{blueprint: &BlueprintRecord{ID: "bp1", Name: "agent-notes", Version: "1.0.0", Status: "published",
		Manifest: json.RawMessage(`{"packs":[{"name":"agent-notes","objectTypes":[{"name":"Note","label":"Note"}]}]}`)}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/blueprints/:id", s.uiBlueprint)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blueprints/bp1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Note") {
		t.Fatalf("blueprint detail: status=%d body=%s", rec.Code, rec.Body.String())
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

// TestModelProviderOf covers the provider-prefix extraction used to judge
// whether a blueprint-pinned model is servable by the configured providers.
func TestModelProviderOf(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"openai/deepseek-v4-pro", "openai"},
		{"deepseek/deepseek-v4-flash", "deepseek"},
		{"google/gemini-3-pro", "google"},
		{"gpt-4o", ""},
		{"", ""},
		{"/leading", ""},
		{"trailing/", "trailing"},
	}
	for _, tc := range cases {
		if got := modelProviderOf(tc.model); got != tc.want {
			t.Errorf("modelProviderOf(%q) = %q, want %q", tc.model, got, tc.want)
		}
	}
}

// TestRenderBlueprintDetailVersionsAndDiff locks the version-history + diff-card
// markup contract.
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
		`href="/blueprints/v3"`,
		"v2.0.0",
		"lucide--tag",
		`href="/blueprints/v1"`,
		`href="/blueprints/v2"`,
		`href="/blueprints/v3"`,
		"Installed",
		"Viewing",
		`aria-current="true"`,
		"Changes",
		"vs",
		`name="compare"`,
		"bg-success/10",
		"bg-error/10",
		"bg-warning/10",
		"Added",
		"Removed",
		"Changed",
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
		`value="2.0.1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("version/diff detail missing %q", want)
		}
	}

	// Negative case: a registry-schema detail (no history, not a draft) must
	// render neither the diff card nor its comparison selector.
	plain := &BlueprintDetail{
		ID:      "s9",
		Name:    "agent-notes",
		Version: "1.0.0",
		Source:  "registry",
	}
	plainHTML := renderHTML(t, BlueprintDetailPage(plain, nil))
	for _, gone := range []string{"Changes", `name="compare"`, "Viewing"} {
		if strings.Contains(plainHTML, gone) {
			t.Errorf("registry detail must not render %q", gone)
		}
	}
}

// propertyTypeIntent is the schema property-type colour scheme.
func TestPropertyTypeIntent(t *testing.T) {
	cases := map[string]ui.BadgeIntent{
		"string":  ui.BadgeInfo,
		"number":  ui.BadgeSuccess,
		"integer": ui.BadgeSuccess,
		"boolean": propertyTypeBadgeBoolean,
		"date":    ui.BadgeWarning,
		"array":   propertyTypeBadgeArray,
		"object":  propertyTypeBadgeObject,
		"":        ui.BadgeNeutral,
		"future":  ui.BadgeNeutral,
	}
	for in, want := range cases {
		if got := propertyTypeIntent(in); got != want {
			t.Errorf("propertyTypeIntent(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPropertyTypeToneHuesSeparated guards the scannability of the type colour
// scheme.
func TestPropertyTypeToneHuesSeparated(t *testing.T) {
	hues := []float64{
		240, // string  — --color-info
		152, // number/integer — --color-success
		40,  // boolean — local: error 75% + warning
		82,  // date    — --color-warning
		196, // array   — local: info + success
		313, // object  — local: error + info
	}
	slices.Sort(hues)
	const minSeparation = 40.0
	minSep := 360.0
	for i, h := range hues {
		gap := hues[(i+1)%len(hues)] - h
		if gap < 0 {
			gap += 360
		}
		minSep = min(minSep, gap)
	}
	if minSep < minSeparation {
		t.Fatalf("minimum type-tone hue separation %.1f° is below the %.0f° floor (hues %v)", minSep, minSeparation, hues)
	}
}
