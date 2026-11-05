package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// widgetConflict is one representative object-type collision: an existing
// Widget type gains an "email" property and keeps its existing "name"
// definition.
func widgetConflict() BlueprintTypeConflict {
	return BlueprintTypeConflict{
		TypeName:              "Widget",
		IncomingPackName:      "widgets-pro",
		ExistingSchemaID:      "schema-widget",
		ExistingSchemaName:    "widgets-core",
		ExistingSchemaVersion: "1.2.0",
		ExistingSchema:        json.RawMessage(`{"type":"object"}`),
		IncomingSchema:        json.RawMessage(`{"type":"object"}`),
		AddedProperties:       []string{"email"},
		ConflictingProperties: []BlueprintPropertyConflict{{
			Property:    "name",
			ExistingDef: json.RawMessage(`{"type":"string","maxLength":120}`),
			IncomingDef: json.RawMessage(`{"type":"string","maxLength":64}`),
			Resolution:  "existing_wins",
		}},
	}
}

// TestUIRouteInstallTypeConflictRendersReview verifies an install whose apply
// preview reports object-type collisions renders the merge review page and does
// NOT apply anything.
func TestUIRouteInstallTypeConflictRendersReview(t *testing.T) {
	f := &fakeMemory{
		blueprint:       pinnedBlueprint("bp1", "openai/deepseek-v4-pro"),
		previewApplyRes: &BlueprintApplyPreview{RequiresMerge: true, Conflicts: []BlueprintTypeConflict{widgetConflict()}},
	}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=bp1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="blueprint-type-conflict-page"`,
		`data-testid="blueprint-type-conflict-Widget"`,
		`data-testid="blueprint-conflict-added-Widget-email"`,
		`data-testid="blueprint-conflict-overridden-Widget-name"`,
		`data-testid="blueprint-type-conflict-merge"`,
		`data-testid="blueprint-type-conflict-cancel"`,
		`action="/blueprints/install/confirm"`,
		`name="id"`,
		`value="bp1"`,
		`name="mergeTypeConflicts"`,
		`value="1"`,
		"widgets-pro",
		"widgets-core v1.2.0",
		"definition wins",
		"Merge types and install",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("conflict page missing %q", want)
		}
	}
	if len(f.appliedBlueprint) != 0 {
		t.Errorf("conflict review must not apply, applied = %v", f.appliedBlueprint)
	}
	if f.previewApplyCalls != 1 {
		t.Errorf("PreviewApplyBlueprint calls = %d, want 1", f.previewApplyCalls)
	}
}

// TestUIRouteInstallTypeConflictConfirmMerge verifies the merge confirm applies
// with the merge flag and preserves the chosen model overrides without
// re-previewing.
func TestUIRouteInstallTypeConflictConfirmMerge(t *testing.T) {
	f := &fakeMemory{}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/install/confirm", url.Values{
		"id":                 {"bp1"},
		"mergeTypeConflicts": {"1"},
		"model.operator":     {"openai/gpt-4o"},
	}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("location = %q, want /blueprints?installed=1", loc)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp1" {
		t.Fatalf("appliedBlueprint = %v, want [bp1]", f.appliedBlueprint)
	}
	if len(f.applyOptions) == 0 {
		t.Fatal("apply options were not recorded")
	}
	got := f.applyOptions[len(f.applyOptions)-1]
	if !got.MergeTypeConflicts {
		t.Error("merge flag was not forwarded to the apply")
	}
	if got.ModelOverrides["operator"] != "openai/gpt-4o" {
		t.Errorf("model overrides = %v, want operator=openai/gpt-4o", got.ModelOverrides)
	}
	if f.previewApplyCalls != 0 {
		t.Errorf("PreviewApplyBlueprint calls = %d, want 0 (merge already confirmed)", f.previewApplyCalls)
	}
}

// TestUIRouteInstallTypeConflictConfirmRepreviews verifies the plain confirm
// path re-checks the preview and routes back to the conflict page when
// collisions remain, carrying the model overrides through.
func TestUIRouteInstallTypeConflictConfirmRepreviews(t *testing.T) {
	f := &fakeMemory{
		previewApplyRes: &BlueprintApplyPreview{RequiresMerge: true, Conflicts: []BlueprintTypeConflict{widgetConflict()}},
	}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/install/confirm", url.Values{
		"id":             {"bp1"},
		"model.operator": {"openai/gpt-4o"},
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="blueprint-type-conflict-page"`) {
		t.Errorf("confirm must render the conflict page: %s", body)
	}
	if !strings.Contains(body, `name="model.operator"`) || !strings.Contains(body, `value="openai/gpt-4o"`) {
		t.Errorf("conflict page must preserve the chosen model override: %s", body)
	}
	if len(f.appliedBlueprint) != 0 {
		t.Errorf("re-preview conflict must not apply, applied = %v", f.appliedBlueprint)
	}
}

// TestUIRouteImportTypeConflictRendersReview verifies an import preview with
// object-type collisions renders the merge review page, carrying the GitHub
// coordinates and applying nothing.
func TestUIRouteImportTypeConflictRendersReview(t *testing.T) {
	f := &fakeMemory{
		previewImportBlueprintRes: &BlueprintPreview{
			Name:          "repo",
			Manifest:      json.RawMessage(`{"agents":[]}`),
			TypeConflicts: []BlueprintTypeConflict{widgetConflict()},
		},
	}
	_, e := newImportRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, importFormRequest(t, url.Values{
		"url":   {"https://github.com/org/repo"},
		"ref":   {"v1.2.3"},
		"token": {"ghp_secret"},
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="blueprint-type-conflict-page"`,
		`data-testid="blueprint-type-conflict-Widget"`,
		`action="/blueprints/import/confirm"`,
		`name="url"`,
		`value="https://github.com/org/repo"`,
		`name="ref"`,
		`value="v1.2.3"`,
		`name="token"`,
		`value="ghp_secret"`,
		"Merge types and install",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("import conflict page missing %q", want)
		}
	}
	if f.importAndApplyCalls != 0 {
		t.Errorf("ImportAndApplyBlueprint calls = %d, want 0", f.importAndApplyCalls)
	}
}

// TestUIRouteImportTypeConflictConfirmPreservesSource verifies a confirm that
// still carries collisions re-renders the conflict page with the URL/ref/token
// and the chosen model overrides intact, and imports nothing.
func TestUIRouteImportTypeConflictConfirmPreservesSource(t *testing.T) {
	f := &fakeMemory{
		previewImportBlueprintRes: &BlueprintPreview{
			Name:          "repo",
			Manifest:      json.RawMessage(`{"agents":[]}`),
			TypeConflicts: []BlueprintTypeConflict{widgetConflict()},
		},
	}
	_, e := newImportRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/import/confirm", url.Values{
		"url":          {"https://github.com/org/repo"},
		"ref":          {"v1.2.3"},
		"token":        {"ghp_secret"},
		"model.writer": {"openai/gpt-4o"},
		"model.editor": {""},
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="blueprint-type-conflict-page"`,
		`value="https://github.com/org/repo"`,
		`value="v1.2.3"`,
		`value="ghp_secret"`,
		`name="model.writer"`,
		`value="openai/gpt-4o"`,
		`name="model.editor"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("import conflict confirm missing %q", want)
		}
	}
	if f.importAndApplyCalls != 0 {
		t.Errorf("ImportAndApplyBlueprint calls = %d, want 0", f.importAndApplyCalls)
	}
}

// TestUIRouteImportTypeConflictConfirmMerge verifies the merge confirm imports
// and applies with the merge flag.
func TestUIRouteImportTypeConflictConfirmMerge(t *testing.T) {
	f := &fakeMemory{
		// The merge confirm re-previews to load the manifest for a possible model
		// review; this fixture pins no models, so it applies directly.
		previewImportBlueprintRes: &BlueprintPreview{Manifest: json.RawMessage(`{"agents":[]}`)},
	}
	_, e := newImportRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/import/confirm", url.Values{
		"url":                {"https://github.com/org/repo"},
		"ref":                {"v1.2.3"},
		"token":              {"ghp_secret"},
		"mergeTypeConflicts": {"1"},
		"model.writer":       {"openai/gpt-4o"},
	}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("location = %q, want /blueprints?installed=1", loc)
	}
	if f.importAndApplyCalls != 1 {
		t.Fatalf("ImportAndApplyBlueprint calls = %d, want 1", f.importAndApplyCalls)
	}
	req := f.importAndApplyReq
	if req == nil {
		t.Fatal("import+apply was not called")
	}
	if !req.MergeTypeConflicts {
		t.Error("merge flag was not forwarded to the import")
	}
	if req.ModelOverrides["writer"] != "openai/gpt-4o" {
		t.Errorf("model overrides = %v, want writer=openai/gpt-4o", req.ModelOverrides)
	}
}

// TestUIRouteUpdateTypeConflictRendersReview verifies the Update action previews
// first and renders the merge review page for collisions without enqueuing.
func TestUIRouteUpdateTypeConflictRendersReview(t *testing.T) {
	f := &fakeMemory{
		previewUpdateRes: &BlueprintUpdatePreview{
			BlueprintName:  "vendor-pack",
			CurrentVersion: "1.0.0",
			LatestVersion:  "2.0.0",
			RequiresMerge:  true,
			Conflicts:      []BlueprintTypeConflict{widgetConflict()},
		},
	}
	_, e := newUpdateRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/vendor-pack/updates/apply", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="blueprint-type-conflict-page"`,
		`data-testid="blueprint-type-conflict-Widget"`,
		`action="/blueprints/vendor-pack/updates/apply"`,
		"Merge types and update",
		"1.0.0",
		"2.0.0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("update conflict page missing %q", want)
		}
	}
	if len(f.appliedUpdates) != 0 {
		t.Errorf("conflict review must not enqueue an update, got %v", f.appliedUpdates)
	}
	if f.previewUpdateCalls != 1 {
		t.Errorf("PreviewBlueprintUpdate calls = %d, want 1", f.previewUpdateCalls)
	}
}

// TestUIRouteUpdateTypeConflictConfirmMerge verifies the merge confirm enqueues
// the update with the merge flag.
func TestUIRouteUpdateTypeConflictConfirmMerge(t *testing.T) {
	f := &fakeMemory{}
	_, e := newUpdateRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/vendor-pack/updates/apply", url.Values{
		"mergeTypeConflicts": {"1"},
	}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?updateQueued=1" {
		t.Errorf("location = %q, want /blueprints?updateQueued=1", loc)
	}
	if len(f.appliedUpdates) != 1 || f.appliedUpdates[0] != "vendor-pack" {
		t.Fatalf("appliedUpdates = %v, want [vendor-pack]", f.appliedUpdates)
	}
	if len(f.appliedUpdatesMerge) != 1 || !f.appliedUpdatesMerge[0] {
		t.Errorf("merge flags = %v, want [true]", f.appliedUpdatesMerge)
	}
	if f.previewUpdateCalls != 0 {
		t.Errorf("PreviewBlueprintUpdate calls = %d, want 0 (merge already confirmed)", f.previewUpdateCalls)
	}
}

// TestUIRouteConflictCancel verifies the conflict page's Cancel control is a
// plain link back to the list with no form/backend side effect.
func TestUIRouteConflictCancel(t *testing.T) {
	f := &fakeMemory{
		previewApplyRes: &BlueprintApplyPreview{RequiresMerge: true, Conflicts: []BlueprintTypeConflict{widgetConflict()}},
		blueprint:       pinnedBlueprint("bp1", "openai/deepseek-v4-pro"),
	}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=bp1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="blueprint-type-conflict-cancel"`) {
		t.Fatal("cancel control missing")
	}
	if !strings.Contains(body, `href="/blueprints"`) {
		t.Error("cancel must link back to /blueprints")
	}
	if len(f.appliedBlueprint) != 0 {
		t.Errorf("cancel path must not apply, applied = %v", f.appliedBlueprint)
	}
}

// --- client wire shapes ---

// TestPreviewApplyBlueprintClientPostsRequest verifies the apply-preview client
// posts to /api/blueprints/:id/apply/preview and decodes the conflicts.
func TestPreviewApplyBlueprintClientPostsRequest(t *testing.T) {
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"requiresMerge":true,"conflicts":[{"typeName":"Widget","incomingPackName":"p","addedProperties":["email"],"conflictingProperties":[{"property":"name","existing_def":{"type":"string"},"incoming_def":{"type":"string"},"resolution":"existing_wins"}]}]}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	preview, err := m.PreviewApplyBlueprint(t.Context(), "bp1")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/blueprints/bp1/apply/preview" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if preview == nil || !preview.RequiresMerge || len(preview.Conflicts) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	c := preview.Conflicts[0]
	if c.TypeName != "Widget" || len(c.AddedProperties) != 1 || len(c.ConflictingProperties) != 1 {
		t.Errorf("conflict = %+v", c)
	}
	if c.ConflictingProperties[0].Resolution != "existing_wins" {
		t.Errorf("resolution = %q", c.ConflictingProperties[0].Resolution)
	}
}

// TestApplyBlueprintWithOptionsClientPostsMerge verifies the apply client posts
// both the model overrides and the merge flag.
func TestApplyBlueprintWithOptionsClientPostsMerge(t *testing.T) {
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"blueprintId":"bp1"}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	_, err := m.ApplyBlueprintWithOptions(t.Context(), "bp1", BlueprintApplyOptions{
		ModelOverrides:     map[string]string{"operator": "openai/gpt-4o"},
		MergeTypeConflicts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, gotBody)
	}
	if sent["mergeTypeConflicts"] != true {
		t.Errorf("mergeTypeConflicts = %v, want true", sent["mergeTypeConflicts"])
	}
	overrides, _ := sent["modelOverrides"].(map[string]any)
	if overrides["operator"] != "openai/gpt-4o" {
		t.Errorf("modelOverrides = %v", sent["modelOverrides"])
	}
}

// TestPreviewBlueprintUpdateClientPostsRequest verifies the update-preview client
// posts to /api/blueprints/:name/updates/preview and decodes the versions.
func TestPreviewBlueprintUpdateClientPostsRequest(t *testing.T) {
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"blueprintName":"vendor-pack","currentVersion":"1.0.0","latestVersion":"2.0.0","requiresMerge":false,"conflicts":[]}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	preview, err := m.PreviewBlueprintUpdate(t.Context(), "vendor-pack")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/blueprints/vendor-pack/updates/preview" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if preview == nil || preview.CurrentVersion != "1.0.0" || preview.LatestVersion != "2.0.0" {
		t.Fatalf("preview = %+v", preview)
	}
}

// TestApplyBlueprintUpdateWithMergeClientPostsFlag verifies the update-apply
// client posts the merge flag when set.
func TestApplyBlueprintUpdateWithMergeClientPostsFlag(t *testing.T) {
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"job-2","action":"update","status":"pending"}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	if _, err := m.ApplyBlueprintUpdateWithMerge(t.Context(), "vendor-pack", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"mergeTypeConflicts":true`) {
		t.Errorf("body = %q, want mergeTypeConflicts:true", gotBody)
	}
}

// TestImportAndApplyBlueprintWithOptionsClientPostsMerge verifies the import
// client posts apply:true plus the merge flag.
func TestImportAndApplyBlueprintWithOptionsClientPostsMerge(t *testing.T) {
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"bp-9","status":"published"}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	if _, err := m.ImportAndApplyBlueprintWithOptions(t.Context(), "https://github.com/org/repo", "", "", nil, true); err != nil {
		t.Fatal(err)
	}
	var sent importBlueprintRequest
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, gotBody)
	}
	if !sent.Apply || !sent.MergeTypeConflicts {
		t.Errorf("sent = %+v, want apply+merge", sent)
	}
}

// pinnedConflictManifest is a blueprint manifest that both collides on object
// types (Thing/Gadget) and pins a model on one agent, so the conflict merge must
// still route through the model-review step.
func pinnedConflictManifest(agentName string) json.RawMessage {
	return json.RawMessage(`{"packs":[{"name":"conflict-pack","version":"1.0.0","objectTypes":[` +
		`{"name":"Thing","label":"Thing","properties":{"title":{"type":"string","title":"Title"}}}]}],` +
		`"agents":[{"name":"` + agentName + `","model":{"name":"openai/deepseek-v4-pro"}}]}`)
}

func pinnedConflictBlueprint(id, agentName string) *BlueprintRecord {
	return &BlueprintRecord{
		ID:       id,
		Name:     "pinned-conflict",
		Version:  "1.0.0",
		Status:   "published",
		Origin:   "user",
		Manifest: pinnedConflictManifest(agentName),
	}
}

// TestUIRouteInstallConflictThenModelReviewThenMerge verifies a conflicting
// install that also pins models goes conflict review -> model review -> apply
// with the merge flag and the chosen overrides.
func TestUIRouteInstallConflictThenModelReviewThenMerge(t *testing.T) {
	conflict := widgetConflict()
	f := &fakeMemory{
		blueprint:       pinnedConflictBlueprint("bp1", "operator"),
		previewApplyRes: &BlueprintApplyPreview{RequiresMerge: true, Conflicts: []BlueprintTypeConflict{conflict}},
	}
	_, e := newUIInstallRouteServer(f)

	// 1) Install click -> conflict review, nothing applied.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=bp1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("conflict page status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-testid="blueprint-type-conflict-page"`) {
		t.Fatalf("conflict page missing: %s", rec.Body.String())
	}
	if len(f.appliedBlueprint) != 0 {
		t.Fatalf("conflict review must not apply, got %v", f.appliedBlueprint)
	}

	// 2) Merge confirm -> model review, still nothing applied.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/install/confirm", url.Values{
		"id":                 {"bp1"},
		"mergeTypeConflicts": {"1"},
		"model.operator":     {"openai/gpt-4o"},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("model review status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="blueprint-import-map"`,
		`name="id"`,
		`value="bp1"`,
		`name="model.operator"`,
		`name="mergeTypeConflicts"`,
		`name="modelReviewed"`,
		// A model choice carried by the conflict page survives into the review.
		`value="openai/gpt-4o" selected`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("model review missing %q", want)
		}
	}
	if len(f.appliedBlueprint) != 0 {
		t.Fatalf("model review must not apply, got %v", f.appliedBlueprint)
	}

	// 3) Model review submit -> applies with merge + override.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/install/confirm", url.Values{
		"id":                 {"bp1"},
		"mergeTypeConflicts": {"1"},
		"modelReviewed":      {"1"},
		"model.operator":     {"openai/gpt-4o"},
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("final status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp1" {
		t.Fatalf("appliedBlueprint = %v", f.appliedBlueprint)
	}
	if len(f.applyOptions) != 1 {
		t.Fatalf("applyOptions = %v", f.applyOptions)
	}
	got := f.applyOptions[0]
	if !got.MergeTypeConflicts {
		t.Error("final apply must carry mergeTypeConflicts")
	}
	if got.ModelOverrides["operator"] != "openai/gpt-4o" {
		t.Errorf("model overrides = %v, want operator=openai/gpt-4o", got.ModelOverrides)
	}
}

// TestUIRouteImportConflictThenModelReviewThenMerge is the import equivalent:
// conflict review -> model review (source + merge carried) -> import+apply merge.
func TestUIRouteImportConflictThenModelReviewThenMerge(t *testing.T) {
	f := &fakeMemory{
		previewImportBlueprintRes: &BlueprintPreview{
			Name:          "repo",
			Manifest:      pinnedConflictManifest("writer"),
			TypeConflicts: []BlueprintTypeConflict{widgetConflict()},
		},
	}
	_, e := newImportRouteServer(f)

	// 1) Import -> conflict review, nothing imported.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, importFormRequest(t, url.Values{
		"url":   {"https://github.com/org/repo"},
		"ref":   {"v1.2.3"},
		"token": {"ghp_secret"},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("conflict page status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-testid="blueprint-type-conflict-page"`) {
		t.Fatalf("conflict page missing: %s", rec.Body.String())
	}
	if f.importAndApplyCalls != 0 {
		t.Fatalf("conflict review must not import, calls=%d", f.importAndApplyCalls)
	}

	// 2) Merge confirm -> model review carrying source + merge.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/import/confirm", url.Values{
		"url":                {"https://github.com/org/repo"},
		"ref":                {"v1.2.3"},
		"token":              {"ghp_secret"},
		"mergeTypeConflicts": {"1"},
		"model.writer":       {"openai/gpt-4o"},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("model review status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="blueprint-import-map"`,
		`name="url"`,
		`value="https://github.com/org/repo"`,
		`name="ref"`,
		`value="v1.2.3"`,
		`name="token"`,
		`value="ghp_secret"`,
		`name="model.writer"`,
		`name="mergeTypeConflicts"`,
		`name="modelReviewed"`,
		// A model choice carried by the conflict page survives into the review.
		`value="openai/gpt-4o" selected`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("model review missing %q", want)
		}
	}
	if f.importAndApplyCalls != 0 {
		t.Fatalf("model review must not import, calls=%d", f.importAndApplyCalls)
	}

	// 3) Model review submit -> import+apply with merge + override.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/import/confirm", url.Values{
		"url":                {"https://github.com/org/repo"},
		"ref":                {"v1.2.3"},
		"token":              {"ghp_secret"},
		"mergeTypeConflicts": {"1"},
		"modelReviewed":      {"1"},
		"model.writer":       {"openai/gpt-4o"},
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("final status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.importAndApplyCalls != 1 || f.importAndApplyReq == nil {
		t.Fatalf("import+apply calls=%d req=%+v", f.importAndApplyCalls, f.importAndApplyReq)
	}
	if !f.importAndApplyReq.MergeTypeConflicts {
		t.Error("final import must carry mergeTypeConflicts")
	}
	if f.importAndApplyReq.ModelOverrides["writer"] != "openai/gpt-4o" {
		t.Errorf("model overrides = %v, want writer=openai/gpt-4o", f.importAndApplyReq.ModelOverrides)
	}
}

// TestUIRouteInstallPinnedNoConflictUnchanged verifies the no-conflict
// pinned-model path still renders the model review without a merge flag and
// applies with the chosen overrides.
func TestUIRouteInstallPinnedNoConflictUnchanged(t *testing.T) {
	f := &fakeMemory{blueprint: pinnedBlueprint("bp1", "openai/deepseek-v4-pro")}
	_, e := newUIInstallRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/blueprints/install?schemaId=bp1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("model review status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="blueprint-import-map"`) {
		t.Fatalf("model review missing: %s", body)
	}
	if strings.Contains(body, `name="mergeTypeConflicts"`) {
		t.Error("conflict-free model review must not carry a merge flag")
	}
	if !strings.Contains(body, `name="modelReviewed"`) {
		t.Error("model review must mark its submission as reviewed")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/install/confirm", url.Values{
		"id":             {"bp1"},
		"modelReviewed":  {"1"},
		"model.operator": {"openai/gpt-4o"},
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("final status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(f.applyOptions) != 1 {
		t.Fatalf("applyOptions = %v", f.applyOptions)
	}
	if f.applyOptions[0].MergeTypeConflicts {
		t.Error("conflict-free apply must not carry mergeTypeConflicts")
	}
	if f.applyOptions[0].ModelOverrides["operator"] != "openai/gpt-4o" {
		t.Errorf("model overrides = %v", f.applyOptions[0].ModelOverrides)
	}
}
