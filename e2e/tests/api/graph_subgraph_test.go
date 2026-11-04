// Package api_test — graph_subgraph_test.go
//
// Tests for the graph subgraph API endpoint.
// Ported from emergent.memory/apps/server/tests/e2e/graph_subgraph_test.go
package api_test

import (
	"net/http"
	"testing"
)

func TestGraphSubgraph_CreateSubgraph_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "spec1", "type": "Spec", "key": strPtr("auth-spec"), "properties": map[string]any{"title": "Auth Spec"}},
			{"_ref": "req1", "type": "Requirement", "key": strPtr("req-login"), "properties": map[string]any{"title": "Login"}},
			{"_ref": "req2", "type": "Requirement", "key": strPtr("req-logout"), "properties": map[string]any{"title": "Logout"}},
		},
		"relationships": []map[string]any{
			{"type": "has_requirement", "src_ref": "spec1", "dst_ref": "req1"},
			{"type": "has_requirement", "src_ref": "spec1", "dst_ref": "req2"},
		},
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	objects, _ := response["objects"].([]any)
	if len(objects) != 3 {
		t.Errorf("expected 3 objects, got %d", len(objects))
	}
	relationships, _ := response["relationships"].([]any)
	if len(relationships) != 2 {
		t.Errorf("expected 2 relationships, got %d", len(relationships))
	}
	refMap, _ := response["ref_map"].(map[string]any)
	if len(refMap) != 3 {
		t.Errorf("expected ref_map with 3 entries, got %d", len(refMap))
	}
	for _, key := range []string{"spec1", "req1", "req2"} {
		if refMap[key] == nil {
			t.Errorf("expected ref_map[%s] to be set", key)
		}
	}

	// Verify object types
	if len(objects) >= 3 {
		obj0, _ := objects[0].(map[string]any)
		if obj0["type"] != "Spec" {
			t.Errorf("expected first object type=Spec, got %v", obj0["type"])
		}
	}

	// Cleanup objects created by the subgraph
	for _, obj := range objects {
		o, _ := obj.(map[string]any)
		if id, ok := o["id"].(string); ok {
			t.Cleanup(func() { deleteGraphObject(t, projectID, id) })
		}
	}
	rl.Printf("subgraph created with %d objects and %d relationships", len(objects), len(relationships))
}

func TestGraphSubgraph_CreateSubgraph_ObjectsOnly(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "a", "type": "Task", "key": strPtr("subgraph-objects-only-a"), "properties": map[string]any{"title": "Task A"}},
			{"_ref": "b", "type": "Task", "key": strPtr("subgraph-objects-only-b"), "properties": map[string]any{"title": "Task B"}},
		},
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	objects, _ := response["objects"].([]any)
	if len(objects) != 2 {
		t.Errorf("expected 2 objects, got %d", len(objects))
	}
	relationships, _ := response["relationships"].([]any)
	if len(relationships) != 0 {
		t.Errorf("expected 0 relationships, got %d", len(relationships))
	}
	refMap, _ := response["ref_map"].(map[string]any)
	if len(refMap) != 2 {
		t.Errorf("expected ref_map with 2 entries, got %d", len(refMap))
	}

	for _, obj := range objects {
		o, _ := obj.(map[string]any)
		if id, ok := o["id"].(string); ok {
			t.Cleanup(func() { deleteGraphObject(t, projectID, id) })
		}
	}
}

func TestGraphSubgraph_CreateSubgraph_EmptyObjects(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraphSubgraph_CreateSubgraph_DuplicateRef(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "dup", "type": "Task"},
			{"_ref": "dup", "type": "Task"},
		},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraphSubgraph_CreateSubgraph_InvalidRef(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "obj1", "type": "Task"},
		},
		"relationships": []map[string]any{
			{"type": "depends_on", "src_ref": "obj1", "dst_ref": "nonexistent"},
		},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraphSubgraph_CreateSubgraph_SelfLoop(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "obj1", "type": "Task"},
		},
		"relationships": []map[string]any{
			{"type": "depends_on", "src_ref": "obj1", "dst_ref": "obj1"},
		},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraphSubgraph_CreateSubgraph_Unauthorized(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", "", projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "obj1", "type": "Task"},
		},
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestGraphSubgraph_CreateSubgraph_ObjectsRetrievable(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "r1", "type": "Requirement", "properties": map[string]any{"title": "Test Requirement"}},
		},
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	refMap, _ := response["ref_map"].(map[string]any)
	objID, _ := refMap["r1"].(string)
	if objID == "" {
		t.Fatal("expected ref_map[r1] to contain an ID")
	}
	t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })

	// Retrieve the created object
	getResp := doAPILogged(t, rl, "GET", "/api/graph/objects/"+objID, e2eTestToken(), projectID, nil)
	getBody := mustStatus(t, getResp, http.StatusOK)

	var obj map[string]any
	parseBodyJSON(t, getBody, &obj)

	if obj["type"] != "Requirement" {
		t.Errorf("expected type=Requirement, got %v", obj["type"])
	}
	props, _ := obj["properties"].(map[string]any)
	if props["title"] != "Test Requirement" {
		t.Errorf("expected title=Test Requirement, got %v", props["title"])
	}
	rl.Printf("subgraph object retrievable by id=%s", objID)
}

func TestGraphSubgraph_CreateSubgraph_SameBranch(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	branchID := "00000000-0000-0000-0000-000000000099"

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "a", "type": "Task", "key": strPtr("subgraph-same-branch-a"), "branch_id": branchID, "properties": map[string]any{"title": "Task A"}},
			{"_ref": "b", "type": "Task", "key": strPtr("subgraph-same-branch-b"), "branch_id": branchID, "properties": map[string]any{"title": "Task B"}},
		},
		"relationships": []map[string]any{
			{"type": "depends_on", "src_ref": "a", "dst_ref": "b"},
		},
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	objects, _ := response["objects"].([]any)
	if len(objects) != 2 {
		t.Errorf("expected 2 objects, got %d", len(objects))
	}
	relationships, _ := response["relationships"].([]any)
	if len(relationships) != 1 {
		t.Errorf("expected 1 relationship, got %d", len(relationships))
	}

	for _, obj := range objects {
		o, _ := obj.(map[string]any)
		if id, ok := o["id"].(string); ok {
			t.Cleanup(func() { deleteGraphObject(t, projectID, id) })
		}
	}
}

func TestGraphSubgraph_CreateSubgraph_DifferentBranches(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	branchA := "00000000-0000-0000-0000-000000000001"
	branchB := "00000000-0000-0000-0000-000000000002"

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "a", "type": "Task", "key": strPtr("subgraph-diff-branches-a"), "branch_id": branchA, "properties": map[string]any{"title": "Task A"}},
			{"_ref": "b", "type": "Task", "key": strPtr("subgraph-diff-branches-b"), "branch_id": branchB, "properties": map[string]any{"title": "Task B"}},
		},
		"relationships": []map[string]any{
			{"type": "depends_on", "src_ref": "a", "dst_ref": "b"},
		},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraphSubgraph_CreateSubgraph_NilBranches(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "a", "type": "Task", "key": strPtr("subgraph-nil-branches-a"), "properties": map[string]any{"title": "Task A"}},
			{"_ref": "b", "type": "Task", "key": strPtr("subgraph-nil-branches-b"), "properties": map[string]any{"title": "Task B"}},
		},
		"relationships": []map[string]any{
			{"type": "depends_on", "src_ref": "a", "dst_ref": "b"},
		},
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	objects, _ := response["objects"].([]any)
	if len(objects) != 2 {
		t.Errorf("expected 2 objects, got %d", len(objects))
	}
	relationships, _ := response["relationships"].([]any)
	if len(relationships) != 1 {
		t.Errorf("expected 1 relationship, got %d", len(relationships))
	}

	for _, obj := range objects {
		o, _ := obj.(map[string]any)
		if id, ok := o["id"].(string); ok {
			t.Cleanup(func() { deleteGraphObject(t, projectID, id) })
		}
	}
}

func TestGraphSubgraph_CreateSubgraph_KeylessBoardEnabledRejected(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Task is board-enabled only after the built-in task-board blueprint is
	// applied; builtin packs are no longer auto-provisioned into fresh projects.
	// Apply it explicitly so the keyless-rejection precondition actually holds,
	// and assert the install happened via the compiled-types endpoint.
	applyTaskBoardBlueprint(t, rl, projectID)

	// The subgraph/bulk path must reject a keyless board-enabled object with
	// work_status_key_required, just like the single-create path (issue #1655),
	// and create nothing.
	resp := doAPILogged(t, rl, "POST", "/api/graph/subgraph", e2eTestToken(), projectID, jsonBody(map[string]any{
		"objects": []map[string]any{
			{"_ref": "a", "type": "Task", "properties": map[string]any{"title": "Keyless Task"}},
		},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

// applyTaskBoardBlueprint finds the globally-seeded built-in task-board
// blueprint, applies it to the project, and asserts the project's compiled
// types now contain a board-enabled Task. This replaces the auto-provisioning
// that previously made Task board-enabled in every fresh project.
func applyTaskBoardBlueprint(t *testing.T, rl *runLog, projectID string) {
	t.Helper()

	// Find the globally-seeded built-in task-board blueprint.
	listResp := doAPILogged(t, rl, "GET", "/api/blueprints?name=task-board", e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)
	var blueprints []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Origin string `json:"origin"`
	}
	parseBodyJSON(t, listBody, &blueprints)

	var bpID string
	for _, b := range blueprints {
		if b.Name == "task-board" && b.Origin == "builtin" {
			bpID = b.ID
			break
		}
	}
	if bpID == "" {
		t.Fatalf("expected a built-in task-board blueprint, got %+v", blueprints)
	}

	// Apply it to this project.
	applyResp := doAPILogged(t, rl, "POST", "/api/blueprints/"+bpID+"/apply", e2eTestToken(), projectID, jsonBody(map[string]any{}))
	mustStatus(t, applyResp, http.StatusOK)

	// Assert the project's compiled types now contain a board-enabled Task, so
	// the test cannot silently pass if the install did not happen.
	compiledResp := doAPILogged(t, rl, "GET", "/api/schemas/projects/"+projectID+"/compiled-types", e2eTestToken(), "", nil)
	compiledBody := mustStatus(t, compiledResp, http.StatusOK)
	var compiled struct {
		ObjectTypes []struct {
			Name         string `json:"name"`
			BoardEnabled bool   `json:"boardEnabled"`
		} `json:"objectTypes"`
	}
	parseBodyJSON(t, compiledBody, &compiled)
	for _, ot := range compiled.ObjectTypes {
		if ot.Name == "Task" && ot.BoardEnabled {
			return
		}
	}
	t.Fatalf("expected compiled types to contain board-enabled Task after applying task-board, got %+v", compiled.ObjectTypes)
}

// strPtr returns a pointer to a string.
func strPtr(s string) *string { return &s }
