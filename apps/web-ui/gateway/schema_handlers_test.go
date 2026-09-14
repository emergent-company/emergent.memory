package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// schemaFormPost submits an application/x-www-form-urlencoded body to a route.
func schemaFormPost(e *echo.Echo, target string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// schemaJSONPost submits an application/json body to a route (derivation API).
func schemaJSONPost(e *echo.Echo, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// --- task 2.1: edit handlers ---

func TestUISchemaObjectTypeUpdateProjectAuthored(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person", SchemaID: "pack-1"},
	}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate)

	form := url.Values{}
	form.Set("description", "updated")
	form.Set("property_name", "name")
	form.Set("property_type", "string")
	rec := schemaFormPost(e, "/schema/object-types/person", form)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/schema/object-types/person") {
		t.Errorf("location = %q", loc)
	}
	if f.updatedSchemaPackID != "pack-1" {
		t.Errorf("updated pack = %q, want pack-1", f.updatedSchemaPackID)
	}
	if f.updatedSchemaPackEdit == nil || f.updatedSchemaPackEdit.Description != "updated" {
		t.Errorf("edit = %+v", f.updatedSchemaPackEdit)
	}
	if _, ok := f.updatedSchemaPackEdit.Properties["name"]; !ok {
		t.Errorf("properties = %v", f.updatedSchemaPackEdit.Properties)
	}
}

func TestUISchemaObjectTypeUpdateUnknownType(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{}}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate)

	rec := schemaFormPost(e, "/schema/object-types/ghost", url.Values{"description": {"x"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if f.updatedSchemaPackID != "" {
		t.Errorf("no pack update expected, got %q", f.updatedSchemaPackID)
	}
}

func TestUISchemaObjectTypeUpdateMemoryError(t *testing.T) {
	f := &fakeMemory{blueprintErr: errors.New("connection refused")}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate)

	rec := schemaFormPost(e, "/schema/object-types/person", url.Values{"description": {"x"}})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body = %s", rec.Code, rec.Body.String())
	}
}

func TestUISchemaObjectTypeEditPage(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person", Description: "a person", Properties: json.RawMessage(`{"name":{"type":"string","description":"full name"}}`)},
	}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/object-types/:name/edit", s.uiSchemaObjectTypeEdit)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/object-types/person/edit", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"person", "a person", "name", `action="/schema/object-types/person"`, "Save"} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
}

func TestUISchemaAddPage(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/schema/add", s.uiSchemaAdd)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/add", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Add schema") {
		t.Errorf("add schema page missing heading: %s", rec.Body.String())
	}
}

func TestUISchemaInstallNoSelection(t *testing.T) {
	s := &Server{memory: &fakeMemory{}}
	e := echo.New()
	e.POST("/schema/add", s.uiSchemaInstall)

	rec := schemaFormPost(e, "/schema/add", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/schema/add?err=1") {
		t.Errorf("location = %q", loc)
	}
}

// --- task 2.2: derivation handlers ---

func TestDeriveSchemaBlueprint(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person", Properties: json.RawMessage(`{"name":{"type":"string"}}`)},
	}}}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/derive", s.deriveSchemaBlueprint)

	rec := schemaJSONPost(e, "/schema/blueprints/derive", `{"name":"my-bp","version":"1.0.0","description":"derived"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.createdBlueprint == nil || f.createdBlueprint.Name != "my-bp" || f.createdBlueprint.Version != "1.0.0" {
		t.Fatalf("create request = %+v", f.createdBlueprint)
	}
	if !strings.Contains(string(f.createdBlueprint.Manifest), `"person"`) {
		t.Errorf("manifest missing person: %s", f.createdBlueprint.Manifest)
	}
}

func TestDeriveSchemaBlueprintNothingToDerive(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{}}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/derive", s.deriveSchemaBlueprint)

	rec := schemaJSONPost(e, "/schema/blueprints/derive", `{"name":"my-bp","version":"1.0.0"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	if f.createdBlueprint != nil {
		t.Errorf("no blueprint should be created: %+v", f.createdBlueprint)
	}
}

func TestDeriveSchemaBlueprintDuplicate(t *testing.T) {
	f := &fakeMemory{
		compiled:     &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}},
		blueprintErr: errors.New("memory 409 conflict: name and version already exist"),
	}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/derive", s.deriveSchemaBlueprint)

	rec := schemaJSONPost(e, "/schema/blueprints/derive", `{"name":"dup","version":"1.0.0"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
}

func TestDeriveSchemaBlueprintVersion(t *testing.T) {
	baseManifest, _ := json.Marshal(blueprintManifest{Packs: []blueprintPack{{Name: "personal-memory", Version: "1.0.0"}}})
	f := &fakeMemory{
		compiled:                   &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}},
		blueprint:                  &BlueprintRecord{ID: "bp1", Name: "personal-memory", Version: "1.0.0", Manifest: baseManifest},
		createdBlueprintVersionRes: &BlueprintRecord{ID: "bp2", Name: "personal-memory", Version: "2.0.0", Status: "draft"},
	}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/:id/versions/derive", s.deriveSchemaBlueprintVersion)

	rec := schemaJSONPost(e, "/schema/blueprints/bp1/versions/derive", `{"version":"2.0.0"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.createdBlueprintVersionID != "bp1" {
		t.Errorf("clone base = %q, want bp1", f.createdBlueprintVersionID)
	}
	if f.createdBlueprintVersion == nil || f.createdBlueprintVersion.Version != "2.0.0" {
		t.Errorf("create version req = %+v", f.createdBlueprintVersion)
	}
	if f.updatedBlueprintID != "bp2" {
		t.Errorf("manifest replace id = %q, want bp2", f.updatedBlueprintID)
	}
	if f.updatedBlueprintReq == nil || !strings.Contains(string(f.updatedBlueprintReq.Manifest), `"person"`) {
		t.Errorf("replace manifest = %+v", f.updatedBlueprintReq)
	}
}

// TestDeriveSchemaBlueprintVersionCapturesEditedSchema proves the "next version"
// derivation captures a manually edited schema: the project's effective compiled
// types (a project-owned override pack shadowing the blueprint's type) become the
// new version's manifest, the pack name is inherited from the base manifest, and
// the prior version is never touched — only the forked draft is manifest-replaced.
func TestDeriveSchemaBlueprintVersionCapturesEditedSchema(t *testing.T) {
	baseManifest, _ := json.Marshal(blueprintManifest{Packs: []blueprintPack{{Name: "personal-memory", Version: "1.0.0"}}})
	f := &fakeMemory{
		compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
			// The blueprint's definition of person, shadowed by the manual edit.
			{Name: "person", Description: "blueprint person", Shadowed: true, SchemaName: "personal-memory", SchemaVersion: "1.0.0"},
			// The effective, manually edited person from the project override pack.
			{Name: "person", Description: "edited manually", Properties: json.RawMessage(`{"name":{"type":"string"},"nickname":{"type":"string","widget":"input"}}`)},
		}},
		// Display name differs from the manifest pack name on purpose: the derived
		// version must inherit the pack name, not the blueprint's display name.
		blueprint:                  &BlueprintRecord{ID: "bp1", Name: "Personal Memory", Version: "1.0.0", Manifest: baseManifest},
		createdBlueprintVersionRes: &BlueprintRecord{ID: "bp2", Name: "Personal Memory", Version: "2.0.0", Status: "draft"},
	}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/:id/versions/derive", s.deriveSchemaBlueprintVersion)

	rec := schemaJSONPost(e, "/schema/blueprints/bp1/versions/derive", `{"version":"2.0.0","description":"captured edits"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.createdBlueprintVersionID != "bp1" {
		t.Errorf("clone base = %q, want bp1", f.createdBlueprintVersionID)
	}
	if f.updatedBlueprintID != "bp2" {
		t.Fatalf("manifest replace id = %q, want the forked draft bp2 (prior version must stay unchanged)", f.updatedBlueprintID)
	}
	if f.updatedBlueprintReq == nil {
		t.Fatal("expected a manifest replace on the forked draft")
	}
	var m blueprintManifest
	if err := json.Unmarshal(f.updatedBlueprintReq.Manifest, &m); err != nil {
		t.Fatalf("manifest decode: %v", err)
	}
	if len(m.Packs) != 1 {
		t.Fatalf("packs = %d, want 1", len(m.Packs))
	}
	pack := m.Packs[0]
	if pack.Name != "personal-memory" || pack.Version != "2.0.0" {
		t.Errorf("pack identity = %s@%s, want inherited personal-memory@2.0.0", pack.Name, pack.Version)
	}
	persons := 0
	for _, o := range pack.ObjectTypes {
		if strAny(o["name"]) != "person" {
			continue
		}
		persons++
		if got := strAny(o["description"]); got != "edited manually" {
			t.Errorf("derived person description = %q, want the manually edited value", got)
		}
		props, _ := o["properties"].(map[string]any)
		if _, ok := props["nickname"]; !ok {
			t.Errorf("derived person properties missing the manual edit: %v", props)
		}
	}
	if persons != 1 {
		t.Errorf("derived person entries = %d, want exactly 1 (shadowed blueprint definition excluded)", persons)
	}
}

func TestDeriveSchemaBlueprintVersionUnknownBase(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}}}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/:id/versions/derive", s.deriveSchemaBlueprintVersion)

	rec := schemaJSONPost(e, "/schema/blueprints/missing/versions/derive", `{"version":"2.0.0"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
}

// --- task 2.3: provenance + history in view models ---

func TestUISchemaObjectTypeDerivedProvenanceAndHistory(t *testing.T) {
	f := provenanceFakeMemory(t, blueprintWithPack(t, "bp1", "personal-memory", "1.0.0", "personal-memory", "1.0.0"))
	f.compiled = &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person", SchemaID: "pack-1", SchemaName: "personal-memory", SchemaVersion: "1.0.0"},
	}}
	f.history = []SchemaHistoryItem{
		{SchemaID: "pack-1", Version: "1.0.0", Active: true, InstalledAt: "2026-08-26T10:00:00Z"},
		{SchemaID: "pack-1", Version: "0.9.0", InstalledAt: "2026-08-01T10:00:00Z", RemovedAt: "2026-08-26T09:00:00Z"},
		{SchemaID: "other", Version: "9.9.9", Active: true},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/object-types/:name", s.uiSchemaObjectType)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/object-types/person", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Blueprint-derived", "personal-memory", "1.0.0",
		"active", "2026-08-26T10:00:00Z", "2026-08-01T10:00:00Z",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("derived detail page missing %q", want)
		}
	}
	if strings.Contains(body, "9.9.9") {
		t.Errorf("history from an unrelated schema leaked into the page")
	}
}

func TestUISchemaObjectTypeProjectAuthoredNoBadge(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "note"}}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/object-types/:name", s.uiSchemaObjectType)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/object-types/note", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Blueprint-derived") {
		t.Errorf("project-authored type must not carry a blueprint badge: %s", rec.Body.String())
	}
}

func TestUISchemaHistoryUnavailable(t *testing.T) {
	f := &fakeMemory{
		compiled:     &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", SchemaID: "pack-1"}}},
		migrationErr: errors.New("history unavailable"),
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/object-types/:name", s.uiSchemaObjectType)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/object-types/person", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "history is unavailable") {
		t.Errorf("expected non-blocking history notice: %s", rec.Body.String())
	}
}

// --- task 2.4: schema write capability ---

func TestRequireSchemaWriteDeniesWithoutCapability(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", SchemaID: "pack-1"}}}}
	s := &Server{memory: f, schemaWritePolicy: func(echo.Context) bool { return false }}
	e := echo.New()
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate, s.requireSchemaWrite)

	rec := schemaFormPost(e, "/schema/object-types/person", url.Values{"description": {"x"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
	if f.updatedSchemaPackID != "" || f.createdSchemaPack != nil || len(f.assignedSchemaPacks) != 0 {
		t.Errorf("no backend mutation should occur when denied: %+v", f)
	}
}

func TestRequireSchemaWriteAllowsWithCapability(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", SchemaID: "pack-1"}}}}
	s := &Server{memory: f, schemaWritePolicy: func(echo.Context) bool { return true }}
	e := echo.New()
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate, s.requireSchemaWrite)

	rec := schemaFormPost(e, "/schema/object-types/person", url.Values{"description": {"x"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303, body = %s", rec.Code, rec.Body.String())
	}
	if f.updatedSchemaPackID != "pack-1" {
		t.Errorf("updated pack = %q, want pack-1", f.updatedSchemaPackID)
	}
}
