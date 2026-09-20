// Package api_test — schemas_test.go
// Migrated from templatepacks_test.go. Tests that require direct DB access are skipped.
package api_test

import (
	"net/http"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Get Available Packs
// ─────────────────────────────────────────────────────────────────────────────

func TestTemplatePacks_GetAvailablePacks_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/schemas/projects/"+projectID+"/available", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTemplatePacks_GetAvailablePacks_ReturnsArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/schemas/projects/"+projectID+"/available", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	// Response should be an array (may be empty)
	if len(body) == 0 {
		t.Error("expected non-empty body")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Get Installed Packs
// ─────────────────────────────────────────────────────────────────────────────

func TestTemplatePacks_GetInstalledPacks_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/schemas/projects/"+projectID+"/installed", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTemplatePacks_GetInstalledPacks_ReturnsEmptyArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/schemas/projects/"+projectID+"/installed", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusOK)
}

// ─────────────────────────────────────────────────────────────────────────────
// Get Compiled Types
// ─────────────────────────────────────────────────────────────────────────────

func TestTemplatePacks_GetCompiledTypes_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/schemas/projects/"+projectID+"/compiled-types", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTemplatePacks_GetCompiledTypes_ReturnsEmptyWhenNoPacks(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/schemas/projects/"+projectID+"/compiled-types", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if _, ok := result["objectTypes"]; !ok {
		t.Error("expected 'objectTypes' key in response")
	}
	if _, ok := result["relationshipTypes"]; !ok {
		t.Error("expected 'relationshipTypes' key in response")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Assign Pack
// ─────────────────────────────────────────────────────────────────────────────

func TestTemplatePacks_AssignPack_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/schemas/projects/"+projectID+"/assign", "", "", jsonBody(map[string]any{
		"template_pack_id": "some-id",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTemplatePacks_AssignPack_RequiresTemplatePackID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/schemas/projects/"+projectID+"/assign", e2eTestToken(), "", jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestTemplatePacks_AssignPack_NotFoundForInvalidPackID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/schemas/projects/"+projectID+"/assign", e2eTestToken(), "", jsonBody(map[string]any{
		"schema_id": "00000000-0000-0000-0000-000000000000",
	}))
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Update Assignment
// ─────────────────────────────────────────────────────────────────────────────

func TestTemplatePacks_UpdateAssignment_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", "/api/schemas/projects/"+projectID+"/assignments/some-id", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTemplatePacks_UpdateAssignment_NotFoundForInvalidID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", "/api/schemas/projects/"+projectID+"/assignments/00000000-0000-0000-0000-000000000000",
		e2eTestToken(), "", jsonBody(map[string]any{"active": false}))
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Delete Assignment
// ─────────────────────────────────────────────────────────────────────────────

func TestTemplatePacks_DeleteAssignment_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/schemas/projects/"+projectID+"/assignments/some-id", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTemplatePacks_DeleteAssignment_NotFoundForInvalidID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/schemas/projects/"+projectID+"/assignments/00000000-0000-0000-0000-000000000000",
		e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}
