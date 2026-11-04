// Package api_test — embedding_policies_test.go
//
// Tests for the embedding policies API (/api/graph/embedding-policies).
// Ported from emergent.memory/apps/server/tests/e2e/embedding_policies_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

const (
	nonExistentPolicyID = "00000000-0000-0000-0000-000000000099"
	fakePolicyID        = "00000000-0000-0000-0000-000000000001"
)

// ─────────────────────────────────────────────────────────────────────────────
// List Embedding Policies
// ─────────────────────────────────────────────────────────────────────────────

func TestEmbeddingPolicies_ListRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/graph/embedding-policies?project_id="+projectID, "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestEmbeddingPolicies_ListRequiresGraphReadScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	// "with-scope" has documents:read, documents:write, project:read but NOT graph:read
	resp := doAPILogged(t, rl, "GET", "/api/graph/embedding-policies?project_id="+projectID, "with-scope", "", nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestEmbeddingPolicies_ListRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/embedding-policies", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "project_id")
}

func TestEmbeddingPolicies_ListReturnsEmptyArrayForNewProject(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/graph/embedding-policies?project_id="+projectID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var policies []any
	parseBodyJSON(t, body, &policies)
	if len(policies) != 0 {
		t.Errorf("expected 0 policies, got %d", len(policies))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Create Embedding Policy
// ─────────────────────────────────────────────────────────────────────────────

func TestEmbeddingPolicies_CreateRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/graph/embedding-policies", "", "",
		jsonBody(map[string]any{"projectId": projectID, "objectType": "TestType"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestEmbeddingPolicies_CreateRequiresGraphWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/graph/embedding-policies", "read-only", "",
		jsonBody(map[string]any{"projectId": projectID, "objectType": "TestType"}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestEmbeddingPolicies_CreateRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/embedding-policies", e2eTestToken(), "",
		jsonBody(map[string]any{"objectType": "TestType"}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "projectId")
}

func TestEmbeddingPolicies_CreateRequiresObjectType(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/graph/embedding-policies", e2eTestToken(), "",
		jsonBody(map[string]any{"projectId": projectID}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "objectType")
}

func TestEmbeddingPolicies_CreateRejectsInvalidProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/embedding-policies", e2eTestToken(), "",
		jsonBody(map[string]any{"projectId": "not-a-uuid", "objectType": "TestType"}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestEmbeddingPolicies_CreateRejectsNegativeMaxPropertySize(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/graph/embedding-policies", e2eTestToken(), "",
		jsonBody(map[string]any{"projectId": projectID, "objectType": "TestType", "maxPropertySize": -1}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "maxPropertySize")
}

// ─────────────────────────────────────────────────────────────────────────────
// Get Embedding Policy by ID
// ─────────────────────────────────────────────────────────────────────────────

func TestEmbeddingPolicies_GetByIDRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/graph/embedding-policies/"+fakePolicyID+"?project_id="+projectID, "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestEmbeddingPolicies_GetByIDRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/embedding-policies/"+fakePolicyID, e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestEmbeddingPolicies_GetByIDReturns404ForNonExistent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/graph/embedding-policies/"+nonExistentPolicyID+"?project_id="+projectID, e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestEmbeddingPolicies_GetByIDRejectsInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/graph/embedding-policies/not-a-uuid?project_id="+projectID, e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

// ─────────────────────────────────────────────────────────────────────────────
// Update Embedding Policy
// ─────────────────────────────────────────────────────────────────────────────

func TestEmbeddingPolicies_UpdateRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", "/api/graph/embedding-policies/"+fakePolicyID+"?project_id="+projectID, "", "",
		jsonBody(map[string]any{"enabled": false}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestEmbeddingPolicies_UpdateRequiresGraphWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", "/api/graph/embedding-policies/"+fakePolicyID+"?project_id="+projectID, "read-only", "",
		jsonBody(map[string]any{"enabled": false}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestEmbeddingPolicies_UpdateRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PATCH", "/api/graph/embedding-policies/"+fakePolicyID, e2eTestToken(), "",
		jsonBody(map[string]any{"enabled": false}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestEmbeddingPolicies_UpdateReturns404ForNonExistent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", "/api/graph/embedding-policies/"+nonExistentPolicyID+"?project_id="+projectID, e2eTestToken(), "",
		jsonBody(map[string]any{"enabled": false}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestEmbeddingPolicies_UpdateRejectsInvalidMaxPropertySize(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", "/api/graph/embedding-policies/"+fakePolicyID+"?project_id="+projectID, e2eTestToken(), "",
		jsonBody(map[string]any{"maxPropertySize": 0}))
	mustStatus(t, resp, http.StatusBadRequest)
}

// ─────────────────────────────────────────────────────────────────────────────
// Delete Embedding Policy
// ─────────────────────────────────────────────────────────────────────────────

func TestEmbeddingPolicies_DeleteRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/graph/embedding-policies/"+fakePolicyID+"?project_id="+projectID, "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestEmbeddingPolicies_DeleteRequiresGraphWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/graph/embedding-policies/"+fakePolicyID+"?project_id="+projectID, "read-only", "", nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestEmbeddingPolicies_DeleteRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/graph/embedding-policies/"+fakePolicyID, e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestEmbeddingPolicies_DeleteReturns404ForNonExistent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/graph/embedding-policies/"+nonExistentPolicyID+"?project_id="+projectID, e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}
