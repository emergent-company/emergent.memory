package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- pack mutation client methods (task 1.2) ---

func TestMemoryClientCreateSchemaPack(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"pack-1","name":"my-pack","version":"2.0.0"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	raw := json.RawMessage(`[{"name":"person","properties":{"name":{"type":"string"}}}]`)
	pack, err := m.CreateSchemaPack(context.Background(), &SchemaPackWriteRequest{
		Name: "my-pack", Version: "2.0.0", ObjectTypeSchemas: raw,
	})
	if err != nil {
		t.Fatalf("CreateSchemaPack: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/schemas" {
		t.Errorf("request = %s %s, want POST /api/schemas", gotMethod, gotPath)
	}
	if gotBody["name"] != "my-pack" || gotBody["version"] != "2.0.0" {
		t.Errorf("body name/version = %v / %v", gotBody["name"], gotBody["version"])
	}
	if _, ok := gotBody["object_type_schemas"]; !ok {
		t.Errorf("body missing object_type_schemas: %v", gotBody)
	}
	if pack.ID != "pack-1" {
		t.Errorf("created pack = %+v", pack)
	}
}

func TestMemoryClientUpdateSchemaPackPreservesUnrelatedTypes(t *testing.T) {
	var gotMethod, gotPath string
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"id":"pack-1","name":"p","version":"1.0.0","objectTypeSchemas":[{"name":"person","label":"Person","description":"old","properties":{"name":{"type":"string"}}},{"name":"task","properties":{"title":{"type":"string"}}}],"relationshipTypeSchemas":[{"name":"assigned_to","sourceType":"task","targetType":"person"}]}`)
			return
		}
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&putBody)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	err := m.UpdateSchemaPack(context.Background(), "pack-1", ObjectTypeEdit{
		Name:        "person",
		Description: "new",
		Properties: map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "number"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateSchemaPack: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/schemas/pack-1" {
		t.Errorf("request = %s %s, want PUT /api/schemas/pack-1", gotMethod, gotPath)
	}
	if _, ok := putBody["relationship_type_schemas"]; ok {
		t.Errorf("partial update must not send relationship_type_schemas: %v", putBody)
	}
	if _, ok := putBody["name"]; ok {
		t.Errorf("partial update must not send pack name: %v", putBody)
	}
	raw, _ := json.Marshal(putBody["object_type_schemas"])
	var types []map[string]any
	if err := json.Unmarshal(raw, &types); err != nil {
		t.Fatalf("object_type_schemas decode: %v", err)
	}
	if len(types) != 2 {
		t.Fatalf("types = %d, want 2 (person + preserved task): %v", len(types), types)
	}
	byName := map[string]map[string]any{}
	for _, tp := range types {
		byName[strAny(tp["name"])] = tp
	}
	person := byName["person"]
	if person == nil || strAny(person["description"]) != "new" {
		t.Errorf("person = %v, want description new", person)
	}
	if strAny(person["label"]) != "Person" {
		t.Errorf("person label = %q, want preserved Person", strAny(person["label"]))
	}
	props, _ := person["properties"].(map[string]any)
	if _, ok := props["name"]; !ok {
		t.Errorf("person properties = %v, want name preserved", props)
	}
	if _, ok := props["age"]; !ok {
		t.Errorf("person properties = %v, want age added", props)
	}
	task := byName["task"]
	if task == nil || strAny(task["name"]) != "task" {
		t.Errorf("unrelated task type not preserved: %v", types)
	}
}

func TestMemoryClientAssignSchemaPack(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"assigned"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.AssignSchemaPack(context.Background(), "pack-override"); err != nil {
		t.Fatalf("AssignSchemaPack: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/schemas/projects/proj/assign" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody["schema_id"] != "pack-override" {
		t.Errorf("body = %v", gotBody)
	}
}

// --- blueprint derivation client methods (task 1.3) ---

func TestMemoryClientUpdateBlueprint(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"bp1","name":"derived","version":"2.0.0","status":"draft"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	rec, err := m.UpdateBlueprint(context.Background(), "bp1", &UpdateBlueprintRequest{
		Name: "derived", Version: "2.0.0", Manifest: json.RawMessage(`{"packs":[{"name":"derived","version":"2.0.0"}]}`),
	})
	if err != nil {
		t.Fatalf("UpdateBlueprint: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/blueprints/bp1" {
		t.Errorf("request = %s %s, want PUT /api/blueprints/bp1", gotMethod, gotPath)
	}
	if _, ok := gotBody["manifest"]; !ok {
		t.Errorf("body missing manifest: %v", gotBody)
	}
	if rec.ID != "bp1" {
		t.Errorf("record = %+v", rec)
	}
}

func TestMemoryClientCreateBlueprintVersion(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"bp2","name":"derived","version":"2.0.0","status":"draft"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	rec, err := m.CreateBlueprintVersion(context.Background(), "bp1", &CreateBlueprintVersionRequest{Version: "2.0.0"})
	if err != nil {
		t.Fatalf("CreateBlueprintVersion: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/blueprints/bp1/versions" {
		t.Errorf("request = %s %s, want POST /api/blueprints/bp1/versions", gotMethod, gotPath)
	}
	if gotBody["version"] != "2.0.0" {
		t.Errorf("body = %v", gotBody)
	}
	if rec.ID != "bp2" {
		t.Errorf("record = %+v", rec)
	}
}

// --- copy-on-write override pack (task 1.4) ---

func TestUpsertOverrideTypeFirstCreatesAndAssigns(t *testing.T) {
	f := &fakeMemory{createdSchemaPackRes: &BlueprintSchema{ID: "pack-new"}}
	s := &Server{memory: f}
	edit := ObjectTypeEdit{
		Name:        "person",
		Description: "edited",
		Properties:  map[string]any{"name": map[string]any{"type": "string"}},
	}
	if err := s.upsertOverrideType(context.Background(), edit); err != nil {
		t.Fatalf("upsertOverrideType: %v", err)
	}
	if f.createdSchemaPack == nil {
		t.Fatal("expected CreateSchemaPack on first override")
	}
	if f.createdSchemaPack.Name != overridePackName || f.createdSchemaPack.Version != overridePackVersion {
		t.Errorf("created pack = %+v, want %s@%s", f.createdSchemaPack, overridePackName, overridePackVersion)
	}
	if len(f.assignedSchemaPacks) != 1 || f.assignedSchemaPacks[0] != "pack-new" {
		t.Errorf("assigned = %v, want [pack-new]", f.assignedSchemaPacks)
	}
	if f.updatedSchemaPackID != "" {
		t.Errorf("unexpected UpdateSchemaPack on %q", f.updatedSchemaPackID)
	}
}

func TestUpsertOverrideTypeRepeatUpdatesExisting(t *testing.T) {
	// A blueprint pack is present in the catalog; the override helper must touch
	// only the override pack, never the blueprint's original pack.
	f := &fakeMemory{schemas: []SchemaInfo{
		{ID: "pack-blueprint", Name: "personal-memory", Version: "1.0.0", ProjectID: "proj"},
		{ID: "pack-override", Name: overridePackName, Version: overridePackVersion, ProjectID: "proj"},
	}}
	s := &Server{memory: f}
	if err := s.upsertOverrideType(context.Background(), ObjectTypeEdit{Name: "person", Description: "edited"}); err != nil {
		t.Fatalf("upsertOverrideType: %v", err)
	}
	if f.updatedSchemaPackID != "pack-override" {
		t.Errorf("updated pack = %q, want pack-override", f.updatedSchemaPackID)
	}
	if f.updatedSchemaPackID == "pack-blueprint" {
		t.Error("must not mutate the blueprint's original pack")
	}
	if f.createdSchemaPack != nil {
		t.Errorf("unexpected CreateSchemaPack on repeat: %+v", f.createdSchemaPack)
	}
	if len(f.assignedSchemaPacks) != 0 {
		t.Errorf("unexpected assign on repeat: %v", f.assignedSchemaPacks)
	}
}

// --- provenance resolver (task 1.5) ---

func provenanceFakeMemory(t *testing.T, records ...*BlueprintRecord) *fakeMemory {
	t.Helper()
	f := &fakeMemory{blueprintByID: map[string]*BlueprintRecord{}}
	for _, rec := range records {
		f.blueprintByID[rec.ID] = rec
		f.appliedBlueprints = append(f.appliedBlueprints, AppliedBlueprint{
			BlueprintID: rec.ID, Name: rec.Name, Version: rec.Version,
		})
	}
	return f
}

func blueprintWithPack(t *testing.T, id, name, version, packName, packVersion string) *BlueprintRecord {
	t.Helper()
	manifest, err := json.Marshal(blueprintManifest{Packs: []blueprintPack{{Name: packName, Version: packVersion}}})
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return &BlueprintRecord{ID: id, Name: name, Version: version, Manifest: manifest}
}

func TestBuildProvenanceIndex(t *testing.T) {
	f := provenanceFakeMemory(t, blueprintWithPack(t, "bp1", "personal-memory", "1.0.0", "personal-memory", "1.0.0"))
	s := &Server{memory: f}
	index := s.buildProvenanceIndex(context.Background())

	derived := provenanceFor(index, CompiledType{Name: "person", SchemaName: "personal-memory", SchemaVersion: "1.0.0"})
	if derived == nil {
		t.Fatal("expected blueprint-derived match")
	}
	if derived.BlueprintID != "bp1" || derived.BlueprintName != "personal-memory" || derived.BlueprintVersion != "1.0.0" {
		t.Errorf("derived = %+v", derived)
	}

	if miss := provenanceFor(index, CompiledType{Name: "note", SchemaName: "agent-notes", SchemaVersion: "1.0.0"}); miss != nil {
		t.Errorf("project-authored type should have no provenance, got %+v", miss)
	}

	if vm := provenanceFor(index, CompiledType{Name: "person", SchemaName: "personal-memory", SchemaVersion: "2.0.0"}); vm != nil {
		t.Errorf("version mismatch should have no provenance, got %+v", vm)
	}

	if bare := provenanceFor(index, CompiledType{Name: "bare"}); bare != nil {
		t.Errorf("empty schema name should have no provenance, got %+v", bare)
	}
}

func TestBuildProvenanceIndexAmbiguous(t *testing.T) {
	f := provenanceFakeMemory(t,
		blueprintWithPack(t, "bp1", "blueprint-a", "1.0.0", "shared-pack", "1.0.0"),
		blueprintWithPack(t, "bp2", "blueprint-b", "1.0.0", "shared-pack", "1.0.0"),
	)
	s := &Server{memory: f}
	index := s.buildProvenanceIndex(context.Background())
	p := provenanceFor(index, CompiledType{Name: "person", SchemaName: "shared-pack", SchemaVersion: "1.0.0"})
	if p == nil {
		t.Fatal("expected a provenance entry")
	}
	if !p.Ambiguous {
		t.Errorf("expected Ambiguous=true, got %+v", p)
	}
	if p.BlueprintID != "bp1" {
		t.Errorf("first blueprint should win, got %+v", p)
	}
}

// --- effective-type manifest assembly (task 1.6) ---

func TestBuildDerivedManifestEffectiveTypes(t *testing.T) {
	compiled := &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{
			{Name: "person", Label: "Person", Description: "a person", Properties: json.RawMessage(`{"name":{"type":"string"}}`), SchemaName: "personal-memory"},
			{Name: "person", Description: "shadowed until uuid", Shadowed: true},
			{Name: "task"},
		},
		RelationshipTypes: []CompiledType{
			{Name: "assigned_to", SourceType: "task", TargetType: "person"},
			{Name: "assigned_to", SourceType: "other", TargetType: "other", Shadowed: true},
		},
	}
	raw, err := buildDerivedManifest("derived", "1.0.0", "notes", compiled)
	if err != nil {
		t.Fatalf("buildDerivedManifest: %v", err)
	}
	var m blueprintManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest decode: %v", err)
	}
	if len(m.Packs) != 1 {
		t.Fatalf("packs = %d, want 1", len(m.Packs))
	}
	pack := m.Packs[0]
	if pack.Name != "derived" || pack.Version != "1.0.0" {
		t.Errorf("pack identity = %s@%s", pack.Name, pack.Version)
	}
	if len(pack.ObjectTypes) != 2 {
		t.Fatalf("object types = %d, want 2 (person + task, no shadowed): %v", len(pack.ObjectTypes), pack.ObjectTypes)
	}
	if len(pack.RelationshipTypes) != 1 {
		t.Fatalf("relationship types = %d, want 1 (no shadowed): %v", len(pack.RelationshipTypes), pack.RelationshipTypes)
	}
	if got := strAny(pack.ObjectTypes[0]["label"]); got != "Person" {
		t.Errorf("person label = %q", got)
	}
	if _, ok := pack.ObjectTypes[0]["properties"]; !ok {
		t.Errorf("person properties missing: %v", pack.ObjectTypes[0])
	}
	if strAny(pack.RelationshipTypes[0]["sourceType"]) != "task" {
		t.Errorf("relationship source = %v", pack.RelationshipTypes[0])
	}
}

func TestBuildDerivedManifestNothing(t *testing.T) {
	_, err := buildDerivedManifest("x", "1.0.0", "", &CompiledSchemaTypes{
		ObjectTypes: []CompiledType{{Name: "only", Shadowed: true}},
	})
	if !errors.Is(err, errNothingToDerive) {
		t.Fatalf("err = %v, want errNothingToDerive", err)
	}
}

// TestObjectTypeMapFromEditUI covers the new-type (override pack) form: the
// type-level ui block is emitted only when an icon or color is set.
func TestObjectTypeMapFromEditUI(t *testing.T) {
	withUI := objectTypeMapFromEdit(ObjectTypeEdit{
		Name: "person", Icon: "lucide--user", Color: "#4F46E5",
		Properties: map[string]any{"name": map[string]any{"type": "string", "widget": "input"}},
	})
	ui, ok := withUI["ui"].(map[string]any)
	if !ok || ui["icon"] != "lucide--user" || ui["color"] != "#4F46E5" {
		t.Fatalf("ui = %v, want icon+color", withUI["ui"])
	}

	plain := objectTypeMapFromEdit(ObjectTypeEdit{Name: "person"})
	if _, ok := plain["ui"]; ok {
		t.Fatalf("ui present without icon/color: %v", plain)
	}
}

// TestApplyObjectTypeEditMergesUIPreservingExtras asserts an edit sets the ui
// icon, clears a blank color, keeps unrelated ui keys and the type's other
// keys, and writes the per-property widget.
func TestApplyObjectTypeEditMergesUIPreservingExtras(t *testing.T) {
	types := []map[string]any{
		{
			"name": "person", "label": "Person", "description": "old",
			"ui":         map[string]any{"color": "#111", "size": "lg"},
			"embedding":  map[string]any{"model": "m"},
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		},
		{"name": "task", "properties": map[string]any{"title": map[string]any{"type": "string"}}},
	}
	edit := ObjectTypeEdit{
		Name: "person", Description: "new", Icon: "lucide--user",
		Properties: map[string]any{"name": map[string]any{"type": "string", "widget": "input"}},
	}
	out := applyObjectTypeEdit(types, edit)
	if len(out) != 2 {
		t.Fatalf("types = %d, want 2 (unrelated preserved)", len(out))
	}

	var person map[string]any
	for _, m := range out {
		if strAny(m["name"]) == "person" {
			person = m
		}
	}
	if person == nil {
		t.Fatal("person missing")
	}
	if strAny(person["description"]) != "new" {
		t.Errorf("description = %q", person["description"])
	}
	if _, ok := person["embedding"].(map[string]any); !ok {
		t.Errorf("embedding not preserved: %v", person)
	}
	ui, _ := person["ui"].(map[string]any)
	if ui["icon"] != "lucide--user" {
		t.Errorf("ui icon = %v, want lucide--user", ui["icon"])
	}
	if _, ok := ui["color"]; ok {
		t.Errorf("blank color must clear the ui key: %v", ui)
	}
	if ui["size"] != "lg" {
		t.Errorf("unrelated ui key not preserved: %v", ui)
	}
	props, _ := person["properties"].(map[string]any)
	nameProp, _ := props["name"].(map[string]any)
	if nameProp["widget"] != "input" {
		t.Errorf("widget = %v, want input", nameProp["widget"])
	}
}

// TestApplyObjectTypeEditDropsEmptiedUI asserts clearing both icon and color
// removes the ui key entirely rather than leaving an empty block.
func TestApplyObjectTypeEditDropsEmptiedUI(t *testing.T) {
	types := []map[string]any{
		{"name": "person", "ui": map[string]any{"icon": "lucide--user", "color": "#111"}},
	}
	out := applyObjectTypeEdit(types, ObjectTypeEdit{Name: "person"})
	if _, ok := out[0]["ui"]; ok {
		t.Fatalf("ui should be dropped when emptied: %v", out[0])
	}
}
