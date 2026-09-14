package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- task 3.1: Add schema affordances ---

func TestRenderSchemaPageAddAndDeriveAffordances(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}}
	html := renderHTML(t, SchemaPage(&SchemaListView{Compiled: compiled}, nil))
	for _, want := range []string{
		`href="/schema/add"`,
		"Add schema",
		"Save as blueprint",
		`data-dialog-open="derive-blueprint-modal"`,
		`id="derive-blueprint-modal"`,
		`action="/schema/blueprints/derive"`,
		`name="_ui"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("schema page missing %q", want)
		}
	}
}

func TestRenderSchemaPageEmptyStateHasAddSchema(t *testing.T) {
	html := renderHTML(t, SchemaPage(nil, nil))
	if !strings.Contains(html, "No compiled types") {
		t.Fatal("empty state missing")
	}
	if !strings.Contains(html, "Add schema") {
		t.Error("empty state should offer the Add schema action")
	}
}

func TestRenderSchemaAddPageStates(t *testing.T) {
	available := renderHTML(t, SchemaAddPage(&SchemaAddView{Available: []AvailableSchemaItem{
		{ID: "s1", Name: "agent-notes", Version: "1.0.0", Description: "notes", Source: "registry"},
		{ID: "personal-memory", Name: "personal-memory", Version: "1.0.0", Source: "bundled"},
	}}))
	for _, want := range []string{
		"agent-notes", "personal-memory",
		`action="/schema/add"`,
		`name="schemaId" value="s1"`,
		`name="name" value="personal-memory"`,
		"Install",
	} {
		if !strings.Contains(available, want) {
			t.Errorf("available schema page missing %q", want)
		}
	}

	empty := renderHTML(t, SchemaAddPage(&SchemaAddView{}))
	for _, want := range []string{"No schemas available", "Back to Schema"} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty add-schema page missing %q", want)
		}
	}

	errHTML := renderHTML(t, SchemaAddPage(&SchemaAddView{Err: errTest}))
	if !strings.Contains(errHTML, "backend unreachable") {
		t.Error("add-schema error state missing the message")
	}
}

// --- task 3.2: object-type editor ---

func TestRenderSchemaEditorRowsAndTemplate(t *testing.T) {
	view := &SchemaEditView{Detail: &ObjectTypeDetail{
		Name:        "person",
		Description: "a person",
		Properties: []PropertyDetail{
			{Name: "first_name", Type: "string", Description: "First", Required: true},
			{Name: "age", Type: "number"},
		},
	}}
	html := renderHTML(t, SchemaObjectTypeEditPage(view))
	for _, want := range []string{
		`id="property-rows"`,
		`id="property-row-template"`,
		`data-add-property="property-row-template"`,
		`data-remove-property="1"`,
		`name="property_name"`,
		`name="property_type"`,
		`name="property_required"`,
		`name="property_description"`,
		`id="object-type-name"`,
		`action="/schema/object-types/person"`,
		`name="_ui"`,
		"first_name", "age", "number", "Optional", "Required", "Add property", "Save changes",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("editor missing %q", want)
		}
	}
	// name is immutable
	if !strings.Contains(html, `id="object-type-name"`) || !strings.Contains(html, "disabled") {
		t.Error("editor name field must be disabled")
	}
	// one remove button per existing row plus the template row
	if n := strings.Count(html, `data-remove-property="1"`); n != 3 {
		t.Errorf("remove buttons = %d, want 3 (2 rows + template)", n)
	}
	// the template row defaults to the string type
	if !strings.Contains(html, `value="string" selected`) {
		t.Error("template/select should default the string type selected")
	}
}

func TestRenderSchemaEditorUnknownType(t *testing.T) {
	html := renderHTML(t, SchemaObjectTypeEditPage(&SchemaEditView{}))
	for _, want := range []string{"Object type not found", "Back to Schema", `href="/schema"`} {
		if !strings.Contains(html, want) {
			t.Errorf("unknown-type editor missing %q", want)
		}
	}
}

// --- task 3.3 + 3.4: provenance marker and derived gate ---

func TestRenderSchemaEditorDerivedGate(t *testing.T) {
	derived := &SchemaEditView{
		Detail:     &ObjectTypeDetail{Name: "person"},
		Provenance: &ProvenanceInfo{BlueprintID: "bp1", BlueprintName: "personal-memory", BlueprintVersion: "1.0.0"},
	}
	html := renderHTML(t, SchemaObjectTypeEditPage(derived))
	for _, want := range []string{
		`data-requires-confirm="derive-warning-modal"`,
		`id="derive-warning-modal"`,
		"personal-memory v1.0.0",
		"Save anyway",
		"Keep editing",
		"may be overwritten",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("derived editor missing %q", want)
		}
	}

	authored := &SchemaEditView{Detail: &ObjectTypeDetail{Name: "note"}}
	authoredHTML := renderHTML(t, SchemaObjectTypeEditPage(authored))
	if strings.Contains(authoredHTML, "derive-warning-modal") {
		t.Error("project-authored editor must not arm the derived confirmation gate")
	}
}

func TestRenderSchemaListProvenanceLink(t *testing.T) {
	compiled := &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person", SchemaName: "personal-memory", SchemaVersion: "1.0.0"},
	}}
	prov := map[string]*ProvenanceInfo{
		"person": {BlueprintID: "bp1", BlueprintName: "personal-memory", BlueprintVersion: "1.0.0"},
	}
	html := renderHTML(t, SchemaPage(&SchemaListView{Compiled: compiled, Provenance: prov}, nil))
	for _, want := range []string{"Blueprint-derived", `href="/blueprints/bp1"`, "personal-memory v1.0.0"} {
		if !strings.Contains(html, want) {
			t.Errorf("schema list provenance missing %q", want)
		}
	}
}

func TestRenderSchemaObjectTypeOriginStates(t *testing.T) {
	derivedHTML := renderHTML(t, SchemaObjectTypePage(&SchemaObjectTypeView{
		Detail:     &ObjectTypeDetail{Name: "person"},
		Provenance: &ProvenanceInfo{BlueprintID: "bp1", BlueprintName: "personal-memory", BlueprintVersion: "1.0.0"},
		SchemaName: "personal-memory", SchemaVersion: "1.0.0",
	}, nil))
	for _, want := range []string{"Blueprint-derived", "personal-memory v1.0.0", `href="/blueprints/bp1"`} {
		if !strings.Contains(derivedHTML, want) {
			t.Errorf("derived origin missing %q", want)
		}
	}

	authoredHTML := renderHTML(t, SchemaObjectTypePage(&SchemaObjectTypeView{
		Detail: &ObjectTypeDetail{Name: "note"}, SchemaName: "agent-notes", SchemaVersion: "1.0.0",
	}, nil))
	if strings.Contains(authoredHTML, "Blueprint-derived") {
		t.Error("project-authored origin must not carry a blueprint marker")
	}
	for _, want := range []string{"Project-authored", "agent-notes v1.0.0"} {
		if !strings.Contains(authoredHTML, want) {
			t.Errorf("authored origin missing %q", want)
		}
	}
}

// --- task 3.2/3.4: editor handler interaction ---

func TestUISchemaObjectTypeUpdateUIFormSuccess(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", SchemaID: "pack-1"}}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate)

	form := url.Values{}
	form.Set("_ui", "1")
	form.Set("description", "updated")
	form.Add("property_name", "name")
	form.Add("property_type", "string")
	form.Add("property_required", "1")
	rec := schemaFormPost(e, "/schema/object-types/person", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "?updated=1") {
		t.Errorf("location = %q", loc)
	}
	if f.updatedSchemaPackID != "pack-1" {
		t.Errorf("updated pack = %q", f.updatedSchemaPackID)
	}
}

func TestUISchemaObjectTypeUpdateUIFormPreservesValuesOnError(t *testing.T) {
	f := &fakeMemory{
		compiled:      &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", SchemaID: "pack-1"}}},
		schemaPackErr: errors.New("memory 500 boom"),
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate)

	form := url.Values{}
	form.Set("_ui", "1")
	form.Set("description", "edited description")
	form.Add("property_name", "first")
	form.Add("property_type", "string")
	form.Add("property_description", "given")
	rec := schemaFormPost(e, "/schema/object-types/person", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (editor re-render)", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"edited description", "first", "given", "memory 500 boom", `value="first"`} {
		if !strings.Contains(body, want) {
			t.Errorf("preserved editor missing %q", want)
		}
	}
}

func TestUISchemaObjectTypeUpdateUIFormValidation(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", SchemaID: "pack-1"}}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate)

	form := url.Values{}
	form.Set("_ui", "1")
	form.Add("property_name", "dup")
	form.Add("property_type", "string")
	form.Add("property_name", "dup")
	form.Add("property_type", "number")
	rec := schemaFormPost(e, "/schema/object-types/person", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "duplicate property") {
		t.Errorf("validation error missing: %s", body)
	}
	if n := strings.Count(body, `value="dup"`); n != 2 {
		t.Errorf("duplicate rows should both be preserved, got %d", n)
	}
	if f.updatedSchemaPackID != "" {
		t.Error("validation failure must not reach the backend")
	}
}

func TestUISchemaObjectTypeUpdateRemovedPropertyPersistsExactSet(t *testing.T) {
	// Submitting only the surviving rows (as the browser does after removing a
	// row) must persist exactly those properties.
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person", SchemaID: "pack-1"}}}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate)

	form := url.Values{}
	form.Set("_ui", "1")
	form.Set("description", "kept")
	form.Add("property_name", "kept")
	form.Add("property_type", "boolean")
	rec := schemaFormPost(e, "/schema/object-types/person", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.updatedSchemaPackEdit == nil {
		t.Fatal("expected an edit")
	}
	props := f.updatedSchemaPackEdit.Properties
	if len(props) != 1 {
		t.Fatalf("properties = %v, want exactly the submitted row", props)
	}
	if _, ok := props["kept"]; !ok {
		t.Errorf("properties = %v, want kept", props)
	}
}

// --- task 3.5: derive UI (Schema area) ---

func TestDeriveSchemaBlueprintUIFormSuccess(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{
		{Name: "person", Properties: json.RawMessage(`{"name":{"type":"string"}}`)},
	}}}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/derive", s.deriveSchemaBlueprint)

	rec := schemaFormPost(e, "/schema/blueprints/derive", url.Values{
		"_ui": {"1"}, "name": {"my-bp"}, "version": {"1.0.0"}, "description": {"derived"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/blueprints?derived=") {
		t.Errorf("location = %q, want a redirect to the drafts list", loc)
	}
	if f.createdBlueprint == nil || f.createdBlueprint.Name != "my-bp" || f.createdBlueprint.Version != "1.0.0" {
		t.Fatalf("create request = %+v", f.createdBlueprint)
	}
}

func TestDeriveSchemaBlueprintUIFormNothingToDerive(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{}}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/derive", s.deriveSchemaBlueprint)

	rec := schemaFormPost(e, "/schema/blueprints/derive", url.Values{
		"_ui": {"1"}, "name": {"my-bp"}, "version": {"1.0.0"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/schema?err=") {
		t.Errorf("location = %q, want an error flash back to /schema", loc)
	}
	if f.createdBlueprint != nil {
		t.Errorf("no blueprint should be created: %+v", f.createdBlueprint)
	}
}

func TestDeriveSchemaBlueprintUIFormMissingFields(t *testing.T) {
	f := &fakeMemory{compiled: &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}}}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/derive", s.deriveSchemaBlueprint)

	rec := schemaFormPost(e, "/schema/blueprints/derive", url.Values{"_ui": {"1"}, "name": {"only-name"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("location = %q, want an error flash", loc)
	}
	if f.createdBlueprint != nil {
		t.Error("no blueprint should be created without a version")
	}
}

func TestDeriveSchemaBlueprintVersionUIForm(t *testing.T) {
	baseManifest, _ := json.Marshal(blueprintManifest{Packs: []blueprintPack{{Name: "personal-memory", Version: "1.0.0"}}})
	f := &fakeMemory{
		compiled:                   &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}},
		blueprint:                  &BlueprintRecord{ID: "bp1", Name: "personal-memory", Version: "1.0.0", Manifest: baseManifest},
		createdBlueprintVersionRes: &BlueprintRecord{ID: "bp2", Name: "personal-memory", Version: "2.0.0", Status: "draft"},
	}
	s := &Server{memory: f}
	e := echo.New()
	e.POST("/schema/blueprints/:id/versions/derive", s.deriveSchemaBlueprintVersion)

	rec := schemaFormPost(e, "/schema/blueprints/bp1/versions/derive", url.Values{
		"_ui": {"1"}, "version": {"2.0.0"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/blueprints/bp1?derived=") {
		t.Errorf("location = %q, want the blueprint detail", loc)
	}
	if f.createdBlueprintVersionID != "bp1" {
		t.Errorf("clone base = %q, want bp1", f.createdBlueprintVersionID)
	}
}

// --- task 3.5: derive UI (blueprints gallery) ---

func TestRenderBlueprintDetailDeriveVersion(t *testing.T) {
	d := &BlueprintDetail{
		ID:               "bp1",
		Name:             "personal-memory",
		Version:          "1.0.0",
		Source:           "blueprint",
		CanDeriveVersion: true,
		Versions: []BlueprintVersionView{
			{ID: "bp1", Version: "1.0.0", Status: "published"},
			{ID: "bp2", Version: "2.0.0", Status: "draft"},
		},
	}
	html := renderHTML(t, BlueprintDetailPage(d, nil))
	for _, want := range []string{
		"Save as new version",
		`data-dialog-open="derive-version-modal"`,
		`id="derive-version-modal"`,
		`action="/schema/blueprints/bp1/versions/derive"`,
		"Versions", "2.0.0", "draft",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("derivable blueprint detail missing %q", want)
		}
	}

	plain := &BlueprintDetail{ID: "agent-notes", Name: "agent-notes", Version: "1.0.0", Source: "bundled"}
	plainHTML := renderHTML(t, BlueprintDetailPage(plain, nil))
	if strings.Contains(plainHTML, "Save as new version") || strings.Contains(plainHTML, "derive-version-modal") {
		t.Error("non-record blueprint must not offer version derivation")
	}
}

func TestRenderBlueprintsPageDeriveBlueprint(t *testing.T) {
	html := renderHTML(t, BlueprintsPage(nil, nil, nil, nil, nil, "", nil))
	for _, want := range []string{"Save as blueprint", `data-dialog-open="derive-blueprint-modal"`, `id="derive-blueprint-modal"`} {
		if !strings.Contains(html, want) {
			t.Errorf("blueprints page missing %q", want)
		}
	}
}

func TestBlueprintDetailShowsDerivedFlash(t *testing.T) {
	d := &BlueprintDetail{ID: "bp1", Name: "personal-memory", Version: "2.0.0", CanDeriveVersion: true, FlashMsg: "Draft version personal-memory 2.0.0 created."}
	html := renderHTML(t, BlueprintDetailPage(d, nil))
	if !strings.Contains(html, "personal-memory 2.0.0") {
		t.Error("derived flash message missing from the detail page")
	}
}

// renderUnavailable is a tiny helper so the history-unavailable view can be
// exercised without a fake server.
func renderUnavailableHistory(t *testing.T) string {
	t.Helper()
	return renderHTML(t, SchemaObjectTypePage(&SchemaObjectTypeView{
		Detail:     &ObjectTypeDetail{Name: "person"},
		HistoryErr: true,
	}, nil))
}

func TestRenderSchemaHistoryStates(t *testing.T) {
	available := renderHTML(t, SchemaObjectTypePage(&SchemaObjectTypeView{
		Detail: &ObjectTypeDetail{Name: "person"},
		History: []SchemaHistoryItem{
			{SchemaID: "pack-1", Version: "1.0.0", Active: true, InstalledAt: "2026-08-26T10:00:00Z"},
			{SchemaID: "pack-1", Version: "0.9.0", RemovedAt: "2026-08-26T09:00:00Z"},
		},
	}, nil))
	for _, want := range []string{"History", "v1.0.0", "active", "v0.9.0", "inactive", "2026-08-26T10:00:00Z"} {
		if !strings.Contains(available, want) {
			t.Errorf("available history missing %q", want)
		}
	}

	unavailable := renderUnavailableHistory(t)
	if !strings.Contains(unavailable, "history is unavailable") {
		t.Error("unavailable history notice missing")
	}
}
