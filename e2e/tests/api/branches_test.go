// Package api_test — branches_test.go
//
// Tests for the graph branches API (/api/graph/branches).
// Ported from emergent.memory/apps/server/tests/e2e/branches_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// branchUniqueName generates a unique branch name for testing.
func branchUniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// =============================================================================
// Test: List Branches - Authentication & Authorization
// =============================================================================

func TestBranches_List_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/branches", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestBranches_List_RequiresGraphReadScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	// "with-scope" has documents:read, documents:write, project:read but NOT graph:read
	resp := doAPILogged(t, rl, "GET", "/api/graph/branches", "with-scope", "", nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestBranches_List_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// The branches API is always project-scoped: omitting project_id (no
	// query param and no X-Project-ID header) must be rejected.
	resp := doAPILogged(t, rl, "GET", "/api/graph/branches", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestBranches_List_FiltersByProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Branches are project-scoped and membership-gated (issue #913): listing
	// must use the caller's own project (via setupProjectLogged), not a foreign
	// project id.
	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/branches?project_id="+projectID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var branches []any
	parseBodyJSON(t, body, &branches)
	if branches == nil {
		t.Error("expected array, got nil")
	}
}

func TestBranches_List_RejectsInvalidProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/branches?project_id=not-a-uuid", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

// =============================================================================
// Test: Get Single Branch - Authentication & Validation
// =============================================================================

func TestBranches_GetByID_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/branches/00000000-0000-0000-0000-000000000001", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestBranches_GetByID_RequiresGraphReadScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	resp := doAPILogged(t, rl, "GET", "/api/graph/branches/00000000-0000-0000-0000-000000000001", "with-scope", "", nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestBranches_GetByID_Returns404ForNonExistent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/branches/00000000-0000-0000-0000-000000000099", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestBranches_GetByID_RejectsInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/branches/not-a-uuid", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

// =============================================================================
// Test: Create Branch - Authentication & Validation
// =============================================================================

func TestBranches_Create_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/branches", "", "", jsonBody(map[string]any{
		"name": "test-branch",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestBranches_Create_RequiresGraphWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	// read-only user has graph:read but NOT graph:write
	resp := doAPILogged(t, rl, "POST", "/api/graph/branches", "read-only", "", jsonBody(map[string]any{
		"name": "test-branch",
	}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestBranches_Create_RequiresName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Use the caller's own project so the membership gate (issue #913) passes
	// and the request reaches name validation.
	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object")
	}
	msg, _ := errObj["message"].(string)
	assertContains(t, msg, "name")
}

func TestBranches_Create_RejectsEmptyName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), "", jsonBody(map[string]any{
		"name": "",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestBranches_Create_RejectsInvalidProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), "", jsonBody(map[string]any{
		"name":       "test-branch",
		"project_id": "not-a-uuid",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestBranches_Create_RejectsInvalidParentBranchID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), "", jsonBody(map[string]any{
		"name":             "test-branch",
		"parent_branch_id": "not-a-uuid",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestBranches_Create_SuccessWithNameOnly(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	name := branchUniqueName("test-branch-basic")

	resp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": name,
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var branch map[string]any
	parseBodyJSON(t, body, &branch)

	if branch["name"] != name {
		t.Errorf("expected name=%s, got %v", name, branch["name"])
	}
	if branch["id"] == nil || branch["id"] == "" {
		t.Error("expected non-empty id")
	}
	if branch["created_at"] == nil {
		t.Error("expected non-nil created_at")
	}

	// Cleanup
	branchID, _ := branch["id"].(string)
	t.Cleanup(func() {
		doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+branchID, e2eTestToken(), projectID, nil)
	})
	rl.Printf("created branch id=%s", branchID)
}

func TestBranches_Create_SuccessWithProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	name := branchUniqueName("test-branch-with-project")

	resp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), "", jsonBody(map[string]any{
		"name":       name,
		"project_id": projectID,
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var branch map[string]any
	parseBodyJSON(t, body, &branch)

	if branch["name"] != name {
		t.Errorf("expected name=%s, got %v", name, branch["name"])
	}
	if branch["id"] == nil || branch["id"] == "" {
		t.Error("expected non-empty id")
	}

	branchID, _ := branch["id"].(string)
	t.Cleanup(func() {
		doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+branchID, e2eTestToken(), projectID, nil)
	})
}

func TestBranches_Create_RejectsDuplicateNameSameProject(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	name := branchUniqueName("unique-branch-name-dup-test")

	// Create first branch
	resp1 := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": name,
	}))
	body1 := mustStatus(t, resp1, http.StatusCreated)
	var b1 map[string]any
	parseBodyJSON(t, body1, &b1)
	t.Cleanup(func() {
		doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+b1["id"].(string), e2eTestToken(), projectID, nil)
	})

	// Try to create second branch with same name
	resp2 := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": name,
	}))
	mustStatus(t, resp2, http.StatusConflict)
}

// =============================================================================
// Test: Update Branch - Authentication & Validation
// =============================================================================

func TestBranches_Update_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PATCH", "/api/graph/branches/00000000-0000-0000-0000-000000000001", "", "",
		jsonBody(map[string]any{"name": "updated-name"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestBranches_Update_RequiresGraphWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	resp := doAPILogged(t, rl, "PATCH", "/api/graph/branches/00000000-0000-0000-0000-000000000001", "read-only", "",
		jsonBody(map[string]any{"name": "updated-name"}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestBranches_Update_Returns404ForNonExistent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "PATCH", "/api/graph/branches/00000000-0000-0000-0000-000000000099", e2eTestToken(), projectID,
		jsonBody(map[string]any{"name": "updated-name"}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestBranches_Update_RejectsInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PATCH", "/api/graph/branches/not-a-uuid", e2eTestToken(), "",
		jsonBody(map[string]any{"name": "updated-name"}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestBranches_Update_RejectsEmptyName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create a branch first
	createResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": branchUniqueName("branch-to-update"),
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	branchID := created["id"].(string)
	t.Cleanup(func() { doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+branchID, e2eTestToken(), projectID, nil) })

	resp := doAPILogged(t, rl, "PATCH", "/api/graph/branches/"+branchID, e2eTestToken(), projectID,
		jsonBody(map[string]any{"name": ""}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestBranches_Update_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create a branch
	originalName := branchUniqueName("original-name")
	createResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": originalName,
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	branchID := created["id"].(string)
	t.Cleanup(func() { doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+branchID, e2eTestToken(), projectID, nil) })

	// Update the branch
	updatedName := branchUniqueName("updated-name")
	resp := doAPILogged(t, rl, "PATCH", "/api/graph/branches/"+branchID, e2eTestToken(), projectID,
		jsonBody(map[string]any{"name": updatedName}))
	body := mustStatus(t, resp, http.StatusOK)

	var updated map[string]any
	parseBodyJSON(t, body, &updated)
	if updated["name"] != updatedName {
		t.Errorf("expected name=%s, got %v", updatedName, updated["name"])
	}
	if updated["id"] != branchID {
		t.Errorf("expected id=%s, got %v", branchID, updated["id"])
	}
	rl.Printf("updated branch name to %s", updatedName)
}

// =============================================================================
// Test: Delete Branch - Authentication & Validation
// =============================================================================

func TestBranches_Delete_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/graph/branches/00000000-0000-0000-0000-000000000001", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestBranches_Delete_RequiresGraphWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	resp := doAPILogged(t, rl, "DELETE", "/api/graph/branches/00000000-0000-0000-0000-000000000001", "read-only", "", nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestBranches_Delete_Returns404ForNonExistent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/graph/branches/00000000-0000-0000-0000-000000000099", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestBranches_Delete_RejectsInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/graph/branches/not-a-uuid", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestBranches_Delete_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create a branch
	createResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": branchUniqueName("branch-to-delete"),
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	branchID := created["id"].(string)

	// Delete the branch
	resp := doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+branchID, e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNoContent)

	// Verify it's deleted
	getResp := doAPILogged(t, rl, "GET", "/api/graph/branches/"+branchID, e2eTestToken(), projectID, nil)
	mustStatus(t, getResp, http.StatusNotFound)
	rl.Printf("deleted branch id=%s", branchID)
}

// =============================================================================
// Test: CRUD Flow - Full Lifecycle
// =============================================================================

func TestBranches_CRUD_FullLifecycle(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// 1. Create a branch
	branchName := branchUniqueName("lifecycle-branch")
	createResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": branchName,
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	branchID := created["id"].(string)

	if created["name"] != branchName {
		t.Errorf("expected name=%s, got %v", branchName, created["name"])
	}

	// 2. Read the branch
	getResp := doAPILogged(t, rl, "GET", "/api/graph/branches/"+branchID, e2eTestToken(), projectID, nil)
	getBody := mustStatus(t, getResp, http.StatusOK)
	var fetched map[string]any
	parseBodyJSON(t, getBody, &fetched)
	if fetched["id"] != branchID {
		t.Errorf("expected id=%s, got %v", branchID, fetched["id"])
	}
	if fetched["name"] != branchName {
		t.Errorf("expected name=%s, got %v", branchName, fetched["name"])
	}

	// 3. List and find the branch
	listResp := doAPILogged(t, rl, "GET", "/api/graph/branches", e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)
	var branches []map[string]any
	parseBodyJSON(t, listBody, &branches)
	found := false
	for _, b := range branches {
		if b["id"] == branchID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created branch %s should appear in list", branchID)
	}

	// 4. Update the branch
	updatedName := branchUniqueName("lifecycle-branch-updated")
	updateResp := doAPILogged(t, rl, "PATCH", "/api/graph/branches/"+branchID, e2eTestToken(), projectID,
		jsonBody(map[string]any{"name": updatedName}))
	updateBody := mustStatus(t, updateResp, http.StatusOK)
	var updated map[string]any
	parseBodyJSON(t, updateBody, &updated)
	if updated["name"] != updatedName {
		t.Errorf("expected name=%s, got %v", updatedName, updated["name"])
	}

	// 5. Delete the branch
	deleteResp := doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+branchID, e2eTestToken(), projectID, nil)
	mustStatus(t, deleteResp, http.StatusNoContent)

	// 6. Verify deletion
	verifyResp := doAPILogged(t, rl, "GET", "/api/graph/branches/"+branchID, e2eTestToken(), projectID, nil)
	mustStatus(t, verifyResp, http.StatusNotFound)
	rl.Printf("branch full lifecycle completed successfully")
}

// =============================================================================
// Test: Parent Branch Relationships
// =============================================================================

func TestBranches_Create_WithParentBranch(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create parent branch
	parentResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": branchUniqueName("parent-branch"),
	}))
	parentBody := mustStatus(t, parentResp, http.StatusCreated)
	var parent map[string]any
	parseBodyJSON(t, parentBody, &parent)
	parentID := parent["id"].(string)
	t.Cleanup(func() { doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+parentID, e2eTestToken(), projectID, nil) })

	// Create child branch
	childResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name":             branchUniqueName("child-branch"),
		"parent_branch_id": parentID,
	}))
	childBody := mustStatus(t, childResp, http.StatusCreated)
	var child map[string]any
	parseBodyJSON(t, childBody, &child)
	childID := child["id"].(string)
	t.Cleanup(func() { doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+childID, e2eTestToken(), projectID, nil) })

	if child["name"] == nil || child["name"] == "" {
		t.Error("expected non-empty name on child branch")
	}
	if child["parent_branch_id"] != parentID {
		t.Errorf("expected parent_branch_id=%s, got %v", parentID, child["parent_branch_id"])
	}
	rl.Printf("child branch created with parent_branch_id=%s", parentID)
}

func TestBranches_Create_RejectsNonExistentParentBranch(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	nonExistentID := "00000000-0000-0000-0000-000000000099"
	resp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name":             "orphan-branch",
		"parent_branch_id": nonExistentID,
	}))
	mustStatus(t, resp, http.StatusNotFound)
}

// =============================================================================
// Test: Merge Branch - Authentication, Validation & Dry Run
// =============================================================================

func TestBranches_Merge_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/branches/00000000-0000-0000-0000-000000000001/merge", "", "",
		jsonBody(map[string]any{
			"sourceBranchId": "00000000-0000-0000-0000-000000000002",
		}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestBranches_Merge_InvalidTargetBranchID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/branches/not-a-uuid/merge", e2eTestToken(), "",
		jsonBody(map[string]any{
			"sourceBranchId": "00000000-0000-0000-0000-000000000002",
		}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestBranches_Merge_MissingSourceBranchID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	targetID := "00000000-0000-0000-0000-000000000001"
	resp := doAPILogged(t, rl, "POST", "/api/graph/branches/"+targetID+"/merge", e2eTestToken(), "",
		jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestBranches_Merge_DryRun_EmptyBranches(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create source and target branches
	sourceResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": branchUniqueName("merge-source"),
	}))
	sourceBody := mustStatus(t, sourceResp, http.StatusCreated)
	var source map[string]any
	parseBodyJSON(t, sourceBody, &source)
	sourceID := source["id"].(string)
	t.Cleanup(func() { doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+sourceID, e2eTestToken(), projectID, nil) })

	targetResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": branchUniqueName("merge-target"),
	}))
	targetBody := mustStatus(t, targetResp, http.StatusCreated)
	var target map[string]any
	parseBodyJSON(t, targetBody, &target)
	targetID := target["id"].(string)
	t.Cleanup(func() { doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+targetID, e2eTestToken(), projectID, nil) })

	// Dry run merge
	mergeResp := doAPILogged(t, rl, "POST", "/api/graph/branches/"+targetID+"/merge", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"source_branch_id": sourceID,
		}))
	mergeBody := mustStatus(t, mergeResp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, mergeBody, &result)

	if result["target_branch_id"] != targetID {
		t.Errorf("expected target_branch_id=%s, got %v", targetID, result["target_branch_id"])
	}
	if result["source_branch_id"] != sourceID {
		t.Errorf("expected source_branch_id=%s, got %v", sourceID, result["source_branch_id"])
	}
	if result["dryRun"] != true {
		t.Errorf("expected dryRun=true, got %v", result["dryRun"])
	}
	if v, _ := result["total_objects"].(float64); int(v) != 0 {
		t.Errorf("expected total_objects=0, got %v", result["total_objects"])
	}
	rl.Printf("dry run merge of empty branches succeeded")
}

func TestBranches_Merge_DryRun_DoesNotMutate(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	sourceResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": branchUniqueName("dry-run-source"),
	}))
	sourceBody := mustStatus(t, sourceResp, http.StatusCreated)
	var source map[string]any
	parseBodyJSON(t, sourceBody, &source)
	sourceID := source["id"].(string)
	t.Cleanup(func() { doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+sourceID, e2eTestToken(), projectID, nil) })

	targetResp := doAPILogged(t, rl, "POST", "/api/graph/branches", e2eTestToken(), projectID, jsonBody(map[string]any{
		"name": branchUniqueName("dry-run-target"),
	}))
	targetBody := mustStatus(t, targetResp, http.StatusCreated)
	var target map[string]any
	parseBodyJSON(t, targetBody, &target)
	targetID := target["id"].(string)
	t.Cleanup(func() { doAPILogged(t, rl, "DELETE", "/api/graph/branches/"+targetID, e2eTestToken(), projectID, nil) })

	// Dry run
	mergeResp := doAPILogged(t, rl, "POST", "/api/graph/branches/"+targetID+"/merge", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"source_branch_id": sourceID,
			// execute omitted — defaults to false (dry run)
		}))
	mergeBody := mustStatus(t, mergeResp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, mergeBody, &result)

	// applied should be false or absent for a dry run
	if applied, hasApplied := result["applied"]; hasApplied {
		if applied != false {
			t.Errorf("expected applied=false for dry run, got %v", applied)
		}
	}
	rl.Printf("dry run merge did not mutate state")
}
