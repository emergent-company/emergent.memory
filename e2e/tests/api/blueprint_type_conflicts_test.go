// Package api_test — blueprint_type_conflicts_test.go
//
// End-to-end coverage for blueprint object-type collision detection: read-only
// apply preview, blocked apply without merge confirmation, merge-confirmed apply,
// same-pack re-apply/version updates, the direct schema merge-preview endpoint,
// and a conflict-free install.
package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// mergeConflictProperty is the subset of PropertyConflict asserted by tests.
type mergeConflictProperty struct {
	Property    string          `json:"property"`
	ExistingDef json.RawMessage `json:"existing_def"`
	IncomingDef json.RawMessage `json:"incoming_def"`
	Resolution  string          `json:"resolution"`
}

// mergePreviewConflict mirrors schemas.MergePreviewConflict on the wire.
type mergePreviewConflict struct {
	TypeName              string                  `json:"typeName"`
	IncomingPackName      string                  `json:"incomingPackName"`
	ExistingSchemaID      string                  `json:"existingSchemaId,omitempty"`
	ExistingSchemaName    string                  `json:"existingSchemaName,omitempty"`
	ExistingSchemaVersion string                  `json:"existingSchemaVersion,omitempty"`
	ExistingSchema        json.RawMessage         `json:"existingSchema,omitempty"`
	IncomingSchema        json.RawMessage         `json:"incomingSchema,omitempty"`
	MergedSchema          json.RawMessage         `json:"mergedSchema,omitempty"`
	AddedProperties       []string                `json:"addedProperties,omitempty"`
	ConflictingProperties []mergeConflictProperty `json:"conflictingProperties,omitempty"`
}

type mergePreviewResult struct {
	Conflicts     []mergePreviewConflict `json:"conflicts"`
	RequiresMerge bool                   `json:"requiresMerge"`
}

type applyPreviewResult struct {
	Conflicts     []mergePreviewConflict `json:"conflicts"`
	RequiresMerge bool                   `json:"requiresMerge"`
}

type applyResultWithConflicts struct {
	BlueprintID   string                 `json:"blueprintId"`
	BlueprintName string                 `json:"blueprintName"`
	Version       string                 `json:"version"`
	TypeConflicts []mergePreviewConflict `json:"typeConflicts,omitempty"`
}

type compiledObjectType struct {
	Name          string         `json:"name"`
	SchemaName    string         `json:"schemaName"`
	SchemaVersion string         `json:"schemaVersion"`
	Shadowed      bool           `json:"shadowed"`
	Properties    map[string]any `json:"properties"`
}

type compiledTypesResponse struct {
	ObjectTypes []compiledObjectType `json:"objectTypes"`
}

type appliedBlueprintItem struct {
	BlueprintID string `json:"blueprintId"`
	Name        string `json:"name"`
	Version     string `json:"version"`
}

// objectTypeProperties returns the non-obvious nested property map for a type.
func objectTypeProperties(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		t.Fatalf("decode properties: %v\nraw: %s", err, raw)
	}
	return props
}

// baseConflictManifest returns the shared base-pack fixture.
func baseConflictManifest() map[string]any {
	return map[string]any{
		"packs": []map[string]any{{
			"name":    "base-pack",
			"version": "1.0.0",
			"objectTypes": []map[string]any{{
				"name":  "Thing",
				"label": "Thing",
				"properties": map[string]any{
					"title":  map[string]any{"type": "string"},
					"status": map[string]any{"type": "string", "enum": []string{"open", "closed"}},
				},
			}},
		}},
	}
}

// conflictManifest returns a pack whose Thing collides with base-pack.
func conflictManifest(packName string) map[string]any {
	return map[string]any{
		"packs": []map[string]any{{
			"name":    packName,
			"version": "1.0.0",
			"objectTypes": []map[string]any{{
				"name":  "Thing",
				"label": "Thing",
				"properties": map[string]any{
					"title":       map[string]any{"type": "string", "description": "incoming title"},
					"description": map[string]any{"type": "string"},
					"owner":       map[string]any{"type": "string"},
				},
			}},
		}},
	}
}

// multiConflictManifest returns a pack with two colliding object types.
func multiConflictManifest(packName string) map[string]any {
	return map[string]any{
		"packs": []map[string]any{{
			"name":    packName,
			"version": "1.0.0",
			"objectTypes": []map[string]any{
				{
					"name": "Thing",
					"properties": map[string]any{
						"title":       map[string]any{"type": "string", "description": "incoming"},
						"description": map[string]any{"type": "string"},
					},
				},
				{
					"name": "Gadget",
					"properties": map[string]any{
						"title": map[string]any{"type": "string", "description": "incoming"},
						"owner": map[string]any{"type": "string"},
					},
				},
			},
		}},
	}
}

// multiBaseManifest defines Thing and Gadget so multiConflictManifest collides on both.
func multiBaseManifest() map[string]any {
	return map[string]any{
		"packs": []map[string]any{{
			"name":    "base-pack",
			"version": "1.0.0",
			"objectTypes": []map[string]any{
				{
					"name": "Thing",
					"properties": map[string]any{
						"title": map[string]any{"type": "string"},
					},
				},
				{
					"name": "Gadget",
					"properties": map[string]any{
						"title": map[string]any{"type": "string"},
					},
				},
			},
		}},
	}
}

// createPrivateBlueprint creates and publishes a project-private blueprint.
func createPrivateBlueprint(t *testing.T, rl *framework.RunLog, projectID, name, version string, manifest map[string]any) string {
	t.Helper()
	resp := doAPILogged(t, rl, "POST", "/api/blueprints", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name":     name,
		"version":  version,
		"manifest": manifest,
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	bpID := parseIDFromBody(t, body)

	pubResp := doAPILogged(t, rl, "POST", "/api/blueprints/"+bpID+"/publish", e2eTestToken(), projectID, nil)
	mustStatus(t, pubResp, http.StatusOK)
	return bpID
}

// applyBlueprint issues an apply and returns the raw response.
func applyBlueprint(t *testing.T, rl *framework.RunLog, projectID, bpID string, opts map[string]any) *http.Response {
	t.Helper()
	if opts == nil {
		opts = map[string]any{}
	}
	return doAPILogged(t, rl, "POST", "/api/blueprints/"+bpID+"/apply", e2eTestToken(), projectID, jsonBody(opts))
}

// previewBlueprintApply issues the read-only apply preview.
func previewBlueprintApply(t *testing.T, rl *framework.RunLog, projectID, bpID string) applyPreviewResult {
	t.Helper()
	resp := doAPILogged(t, rl, "POST", "/api/blueprints/"+bpID+"/apply/preview", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var preview applyPreviewResult
	parseBodyJSON(t, body, &preview)
	return preview
}

// compiledTypes fetches and parses the project's compiled object types.
func compiledTypes(t *testing.T, rl *framework.RunLog, projectID string) compiledTypesResponse {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/schemas/projects/"+projectID+"/compiled-types", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var out compiledTypesResponse
	parseBodyJSON(t, body, &out)
	return out
}

// appliedBlueprints fetches the project's applied blueprint list.
func appliedBlueprints(t *testing.T, rl *framework.RunLog, projectID string) []appliedBlueprintItem {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/blueprints/applied", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var out []appliedBlueprintItem
	parseBodyJSON(t, body, &out)
	return out
}

// findCompiledType returns the newest non-shadowed type with the given name.
func findCompiledType(t *testing.T, types compiledTypesResponse, name string) compiledObjectType {
	t.Helper()
	var found *compiledObjectType
	for i := range types.ObjectTypes {
		ot := &types.ObjectTypes[i]
		if ot.Name != name || ot.Shadowed {
			continue
		}
		found = ot
	}
	if found == nil {
		t.Fatalf("compiled type %q not found; got %+v", name, types.ObjectTypes)
	}
	return *found
}

func hasProperty(props map[string]any, name string) bool {
	_, ok := props[name]
	return ok
}

func TestBlueprintTypeConflicts_ApplyPreviewNoConflict(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	conflictBP := createPrivateBlueprint(t, rl, projectID, uniqueName("conflict-preview"), "1.0.0", conflictManifest("conflict-pack"))

	preview := previewBlueprintApply(t, rl, projectID, conflictBP)
	if preview.RequiresMerge {
		t.Fatalf("expected no conflicts on an empty registry, got %+v", preview.Conflicts)
	}
	if len(preview.Conflicts) != 0 {
		t.Fatalf("expected empty conflicts, got %+v", preview.Conflicts)
	}
}

func TestBlueprintTypeConflicts_ApplyPreviewReportsCollision(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	baseBP := createPrivateBlueprint(t, rl, projectID, uniqueName("base"), "1.0.0", baseConflictManifest())
	mustStatus(t, applyBlueprint(t, rl, projectID, baseBP, nil), http.StatusOK)

	conflictBP := createPrivateBlueprint(t, rl, projectID, uniqueName("conflict"), "1.0.0", conflictManifest("conflict-pack"))
	preview := previewBlueprintApply(t, rl, projectID, conflictBP)
	if !preview.RequiresMerge {
		t.Fatal("expected requiresMerge=true")
	}
	if len(preview.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", preview.Conflicts)
	}
	c := preview.Conflicts[0]
	if c.TypeName != "Thing" || c.IncomingPackName != "conflict-pack" {
		t.Fatalf("unexpected conflict identity: %+v", c)
	}
	if c.ExistingSchemaName != "base-pack" || c.ExistingSchemaVersion != "1.0.0" {
		t.Fatalf("unexpected existing schema: %+v", c)
	}
	if !containsString(c.AddedProperties, "description") || !containsString(c.AddedProperties, "owner") {
		t.Fatalf("expected added properties description/owner, got %v", c.AddedProperties)
	}
	if len(c.ConflictingProperties) != 1 || c.ConflictingProperties[0].Property != "title" {
		t.Fatalf("expected title conflict, got %+v", c.ConflictingProperties)
	}
	if c.ConflictingProperties[0].Resolution != "existing_wins" {
		t.Fatalf("expected existing_wins resolution, got %q", c.ConflictingProperties[0].Resolution)
	}

	var merged map[string]any
	if err := json.Unmarshal(c.MergedSchema, &merged); err != nil {
		t.Fatalf("decode merged schema: %v", err)
	}
	mergedProps, _ := merged["properties"].(map[string]any)
	titleDef, _ := mergedProps["title"].(map[string]any)
	if _, incomingMarker := titleDef["description"]; incomingMarker {
		t.Fatalf("existing title definition should win, got %v", titleDef)
	}
}

func TestBlueprintTypeConflicts_MultipleTypesReported(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	baseBP := createPrivateBlueprint(t, rl, projectID, uniqueName("multi-base"), "1.0.0", multiBaseManifest())
	mustStatus(t, applyBlueprint(t, rl, projectID, baseBP, nil), http.StatusOK)

	conflictBP := createPrivateBlueprint(t, rl, projectID, uniqueName("multi-conflict"), "1.0.0", multiConflictManifest("conflict-pack"))
	preview := previewBlueprintApply(t, rl, projectID, conflictBP)
	if !preview.RequiresMerge || len(preview.Conflicts) != 2 {
		t.Fatalf("expected 2 conflicting types, got %+v", preview.Conflicts)
	}
	names := map[string]bool{}
	for _, c := range preview.Conflicts {
		names[c.TypeName] = true
	}
	if !names["Thing"] || !names["Gadget"] {
		t.Fatalf("expected Thing and Gadget conflicts, got %+v", preview.Conflicts)
	}
}

func TestBlueprintTypeConflicts_ApplyWithoutMergeBlockedAndReadOnly(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	baseBP := createPrivateBlueprint(t, rl, projectID, uniqueName("base"), "1.0.0", baseConflictManifest())
	mustStatus(t, applyBlueprint(t, rl, projectID, baseBP, nil), http.StatusOK)

	appliedBefore := appliedBlueprints(t, rl, projectID)
	compiledBefore := compiledTypes(t, rl, projectID)

	manifest := conflictManifest("conflict-pack")
	agentName := uniqueName("conflict-agent")
	skillName := fmt.Sprintf("conflict-skill-%d", timeNowMillis())
	manifest["agents"] = []map[string]any{{
		"name":         agentName,
		"description":  "conflict agent",
		"systemPrompt": "test",
		"tools":        []string{"skill-list"},
	}}
	manifest["skills"] = []map[string]any{{
		"name":        skillName,
		"description": "conflict skill",
		"content":     "test content",
	}}
	manifest["seed"] = map[string]any{"objects": []map[string]any{{
		"type": "Thing", "key": "conflict-seed", "properties": map[string]any{"title": "seed"},
	}}}
	conflictBP := createPrivateBlueprint(t, rl, projectID, uniqueName("conflict"), "1.0.0", manifest)

	resp := applyBlueprint(t, rl, projectID, conflictBP, nil)
	body := mustStatus(t, resp, http.StatusConflict)

	var conflictErr struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details struct {
				Conflicts []mergePreviewConflict `json:"conflicts"`
			} `json:"details"`
		} `json:"error"`
	}
	parseBodyJSON(t, body, &conflictErr)
	if conflictErr.Error.Code != "conflict" {
		t.Fatalf("expected conflict error code, got %+v", conflictErr)
	}
	if len(conflictErr.Error.Details.Conflicts) == 0 {
		t.Fatalf("expected conflict details, got %s", body)
	}

	appliedAfter := appliedBlueprints(t, rl, projectID)
	if len(appliedAfter) != len(appliedBefore) {
		t.Fatalf("blocked apply changed applied blueprints: before=%+v after=%+v", appliedBefore, appliedAfter)
	}
	compiledAfter := compiledTypes(t, rl, projectID)
	if !reflect.DeepEqual(compiledBefore.ObjectTypes, compiledAfter.ObjectTypes) {
		t.Fatalf("blocked apply changed compiled types: before=%+v after=%+v", compiledBefore.ObjectTypes, compiledAfter.ObjectTypes)
	}

	assertPackNotCreated(t, rl, projectID, "conflict-pack")
	assertAgentNotCreated(t, rl, projectID, agentName)
	assertSkillNotCreated(t, rl, projectID, skillName)
	assertNoThingObjects(t, rl, projectID)
}

func TestBlueprintTypeConflicts_ApplyWithMerge(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	baseBP := createPrivateBlueprint(t, rl, projectID, uniqueName("base"), "1.0.0", baseConflictManifest())
	mustStatus(t, applyBlueprint(t, rl, projectID, baseBP, nil), http.StatusOK)

	conflictBP := createPrivateBlueprint(t, rl, projectID, uniqueName("conflict"), "1.0.0", conflictManifest("conflict-pack"))
	resp := applyBlueprint(t, rl, projectID, conflictBP, map[string]any{"mergeTypeConflicts": true})
	body := mustStatus(t, resp, http.StatusOK)

	var result applyResultWithConflicts
	parseBodyJSON(t, body, &result)
	if len(result.TypeConflicts) == 0 {
		t.Fatalf("expected accepted type conflicts in apply result, got %s", body)
	}
	if len(result.TypeConflicts[0].ConflictingProperties) == 0 {
		t.Fatalf("expected property conflict detail in apply result, got %+v", result.TypeConflicts[0])
	}

	thing := findCompiledType(t, compiledTypes(t, rl, projectID), "Thing")
	if !hasProperty(thing.Properties, "description") || !hasProperty(thing.Properties, "owner") {
		t.Fatalf("expected merged Thing properties description/owner, got %v", thing.Properties)
	}

	// Existing title definition wins in the computed merge (the conflict pack is
	// the newest compiled source, so its raw definition is the incoming one).
	var merged map[string]any
	if err := json.Unmarshal(result.TypeConflicts[0].MergedSchema, &merged); err != nil {
		t.Fatalf("decode merged schema: %v", err)
	}
	mergedProps, _ := merged["properties"].(map[string]any)
	titleDef, _ := mergedProps["title"].(map[string]any)
	if _, incomingMarker := titleDef["description"]; incomingMarker {
		t.Fatalf("existing title definition should survive merge, got %v", titleDef)
	}

	applied := appliedBlueprints(t, rl, projectID)
	found := false
	for _, item := range applied {
		if item.Name == result.BlueprintName && item.Version == "1.0.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected conflict blueprint in applied list, got %+v", applied)
	}
}

func TestBlueprintTypeConflicts_SamePackVersionUpdateIsNotConflict(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	baseBP := createPrivateBlueprint(t, rl, projectID, uniqueName("base"), "1.0.0", baseConflictManifest())
	mustStatus(t, applyBlueprint(t, rl, projectID, baseBP, nil), http.StatusOK)

	v2 := baseConflictManifest()
	pack := v2["packs"].([]map[string]any)[0]
	pack["version"] = "2.0.0"
	thing := pack["objectTypes"].([]map[string]any)[0]
	thing["properties"].(map[string]any)["extra"] = map[string]any{"type": "string"}

	v2BP := createPrivateBlueprint(t, rl, projectID, uniqueName("base"), "2.0.0", v2)
	if preview := previewBlueprintApply(t, rl, projectID, v2BP); preview.RequiresMerge || len(preview.Conflicts) != 0 {
		t.Fatalf("same pack name must not conflict, got %+v", preview.Conflicts)
	}
	mustStatus(t, applyBlueprint(t, rl, projectID, v2BP, nil), http.StatusOK)

	resolved := findCompiledType(t, compiledTypes(t, rl, projectID), "Thing")
	if !hasProperty(resolved.Properties, "extra") {
		t.Fatalf("expected same-pack v2 extra property, got %v", resolved.Properties)
	}
}

func TestBlueprintTypeConflicts_DirectSchemaMergePreview(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	baseBP := createPrivateBlueprint(t, rl, projectID, uniqueName("base"), "1.0.0", baseConflictManifest())
	mustStatus(t, applyBlueprint(t, rl, projectID, baseBP, nil), http.StatusOK)

	manifestPack := conflictManifest("conflict-pack")["packs"].([]map[string]any)[0]
	resp := doAPILogged(t, rl, "POST", "/api/schemas/projects/"+projectID+"/merge-preview", e2eTestToken(), projectID,
		jsonBody(map[string]any{"packs": []map[string]any{{
			"name":        "conflict-pack",
			"objectTypes": manifestPack["objectTypes"],
		}}}))
	body := mustStatus(t, resp, http.StatusOK)
	var result mergePreviewResult
	parseBodyJSON(t, body, &result)
	if !result.RequiresMerge || len(result.Conflicts) != 1 {
		t.Fatalf("expected one direct merge-preview conflict, got %+v", result)
	}
	if result.Conflicts[0].TypeName != "Thing" || result.Conflicts[0].ExistingSchemaName != "base-pack" {
		t.Fatalf("unexpected direct merge-preview conflict: %+v", result.Conflicts[0])
	}
}

func TestBlueprintTypeConflicts_NoConflictBlueprintInstallsDirectly(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	bp := createPrivateBlueprint(t, rl, projectID, uniqueName("standalone"), "1.0.0", conflictManifest("standalone-pack"))
	mustStatus(t, applyBlueprint(t, rl, projectID, bp, nil), http.StatusOK)

	thing := findCompiledType(t, compiledTypes(t, rl, projectID), "Thing")
	if !hasProperty(thing.Properties, "description") || !hasProperty(thing.Properties, "owner") {
		t.Fatalf("expected installed properties, got %v", thing.Properties)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Local assertions (avoid depending on unrelated endpoint helpers)
// ─────────────────────────────────────────────────────────────────────────────

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func assertPackNotCreated(t *testing.T, rl *framework.RunLog, projectID, packName string) {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/schemas/projects/"+projectID+"/available", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var packs []map[string]any
	parseBodyJSON(t, body, &packs)
	for _, p := range packs {
		if p["name"] == packName {
			t.Fatalf("pack %q was created despite blocked apply: %s", packName, body)
		}
	}
}

func assertAgentNotCreated(t *testing.T, rl *framework.RunLog, projectID, agentName string) {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/agent-definitions", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result struct {
		Data []map[string]any `json:"data"`
	}
	parseBodyJSON(t, body, &result)
	for _, def := range result.Data {
		if def["name"] == agentName {
			t.Fatalf("agent %q was created despite blocked apply", agentName)
		}
	}
}

func assertSkillNotCreated(t *testing.T, rl *framework.RunLog, projectID, skillName string) {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/skills", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result struct {
		Skills []map[string]any `json:"skills"`
	}
	parseBodyJSON(t, body, &result)
	for _, skill := range result.Skills {
		if skill["name"] == skillName {
			t.Fatalf("skill %q was created despite blocked apply", skillName)
		}
	}
}

func assertNoThingObjects(t *testing.T, rl *framework.RunLog, projectID string) {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search?type=Thing", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result struct {
		Items []any `json:"items"`
	}
	parseBodyJSON(t, body, &result)
	if len(result.Items) != 0 {
		t.Fatalf("seed object was created despite blocked apply: %s", body)
	}
}

func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}
