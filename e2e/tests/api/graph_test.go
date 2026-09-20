// Package api_test — graph_test.go
//
// Tests for the graph objects and relationships API endpoints.
// Ported from emergent.memory/apps/server/tests/e2e/graph_test.go
package api_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ============ Create Object Tests ============

func TestGraph_CreateObject_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "Requirement",
		"status": "draft",
		"properties": map[string]any{
			"title":       "User Authentication",
			"description": "Implement user auth flow",
		},
		"labels": []string{"security", "mvp"},
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["id"] == nil || response["id"] == "" {
		t.Error("expected non-empty id")
	}
	if response["type"] != "Requirement" {
		t.Errorf("expected type=Requirement, got %v", response["type"])
	}
	if response["status"] != "draft" {
		t.Errorf("expected status=draft, got %v", response["status"])
	}
	props, _ := response["properties"].(map[string]any)
	if props["title"] != "User Authentication" {
		t.Errorf("expected title=User Authentication, got %v", props["title"])
	}
	if v, _ := response["version"].(float64); int(v) != 1 {
		t.Errorf("expected version=1, got %v", response["version"])
	}
	if response["canonical_id"] == nil || response["canonical_id"] == "" {
		t.Error("expected non-empty canonical_id")
	}
	if response["supersedes_id"] != nil {
		t.Errorf("expected supersedes_id=nil, got %v", response["supersedes_id"])
	}
	rl.Printf("created graph object id=%v", response["id"])
}

func TestGraph_CreateObject_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/objects", "", projectID, jsonBody(map[string]any{
		"type": "Requirement",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestGraph_CreateObject_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), "", jsonBody(map[string]any{
		"type": "Requirement",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraph_CreateObject_MissingType(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"status": "draft",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraph_CreateObject_MinimalFields(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type": "Task",
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["type"] != "Task" {
		t.Errorf("expected type=Task, got %v", response["type"])
	}
	if response["properties"] == nil {
		t.Error("expected non-nil properties")
	}
	if response["labels"] == nil {
		t.Error("expected non-nil labels")
	}
}

// ============ Get Object Tests ============

func TestGraph_GetObject_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create an object first
	createResp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "Decision",
		"status": "approved",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	objID := created["id"].(string)
	t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })

	// Get the object
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/"+objID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["id"] != objID {
		t.Errorf("expected id=%s, got %v", objID, response["id"])
	}
	if response["type"] != "Decision" {
		t.Errorf("expected type=Decision, got %v", response["type"])
	}
	if response["status"] != "approved" {
		t.Errorf("expected status=approved, got %v", response["status"])
	}
	rl.Printf("fetched graph object id=%s", objID)
}

func TestGraph_GetObject_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/"+uuid.New().String(), e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// ============ List Objects Tests ============

func TestGraph_ListObjects_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty items, got %d", len(items))
	}
	if total, _ := response["total"].(float64); int(total) != 0 {
		t.Errorf("expected total=0, got %v", response["total"])
	}
}

func TestGraph_ListObjects_WithObjects(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create 3 objects
	for i := 0; i < 3; i++ {
		resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
			"type": "Requirement",
		}))
		b := mustStatus(t, resp, http.StatusCreated)
		var obj map[string]any
		parseBodyJSON(t, b, &obj)
		objID := obj["id"].(string)
		t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })
	}

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
	rl.Printf("listed %d objects", len(items))
}

func TestGraph_ListObjects_FilterByType(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	for _, objType := range []string{"Requirement", "Decision", "Requirement"} {
		resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
			"type": objType,
		}))
		b := mustStatus(t, resp, http.StatusCreated)
		var obj map[string]any
		parseBodyJSON(t, b, &obj)
		objID := obj["id"].(string)
		t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })
	}

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search?types=Requirement", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 Requirement items, got %d", len(items))
	}
	for _, item := range items {
		obj, _ := item.(map[string]any)
		if obj["type"] != "Requirement" {
			t.Errorf("expected type=Requirement, got %v", obj["type"])
		}
	}
}

func TestGraph_ListObjects_Pagination_Limit(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create 5 objects
	for i := 0; i < 5; i++ {
		resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
			"type": "Requirement",
			"properties": map[string]any{
				"index": i,
			},
		}))
		b := mustStatus(t, resp, http.StatusCreated)
		var obj map[string]any
		parseBodyJSON(t, b, &obj)
		objID := obj["id"].(string)
		t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })
	}

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search?limit=2", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	if total, _ := response["total"].(float64); int(total) <= 2 {
		t.Errorf("expected total > 2, got %v", response["total"])
	}
	if response["next_cursor"] == nil || response["next_cursor"] == "" {
		t.Error("expected non-nil next_cursor")
	}
	rl.Printf("pagination limit=2 returned 2 items, total=%v", response["total"])
}

func TestGraph_ListObjects_Pagination_Cursor(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create 5 objects
	for i := 0; i < 5; i++ {
		resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
			"type": "Requirement",
			"properties": map[string]any{
				"index": i,
			},
		}))
		b := mustStatus(t, resp, http.StatusCreated)
		var obj map[string]any
		parseBodyJSON(t, b, &obj)
		objID := obj["id"].(string)
		t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })
	}

	// First page
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search?limit=2", e2eTestToken(), projectID, nil)
	page1Body := mustStatus(t, resp, http.StatusOK)

	var page1 map[string]any
	parseBodyJSON(t, page1Body, &page1)

	items1, _ := page1["items"].([]any)
	if len(items1) != 2 {
		t.Errorf("page1: expected 2 items, got %d", len(items1))
	}
	cursor1, _ := page1["next_cursor"].(string)
	if cursor1 == "" {
		t.Fatal("page1: expected non-empty next_cursor")
	}

	// Second page
	resp = doAPILogged(t, rl, "GET", "/api/graph/objects/search?limit=2&cursor="+url.QueryEscape(cursor1), e2eTestToken(), projectID, nil)
	page2Body := mustStatus(t, resp, http.StatusOK)

	var page2 map[string]any
	parseBodyJSON(t, page2Body, &page2)

	items2, _ := page2["items"].([]any)
	if len(items2) != 2 {
		t.Errorf("page2: expected 2 items, got %d", len(items2))
	}
	cursor2, _ := page2["next_cursor"].(string)
	if cursor2 == "" {
		t.Fatal("page2: expected non-empty next_cursor")
	}

	// Third page
	resp = doAPILogged(t, rl, "GET", "/api/graph/objects/search?limit=2&cursor="+url.QueryEscape(cursor2), e2eTestToken(), projectID, nil)
	page3Body := mustStatus(t, resp, http.StatusOK)

	var page3 map[string]any
	parseBodyJSON(t, page3Body, &page3)

	items3, _ := page3["items"].([]any)
	if len(items3) != 1 {
		t.Errorf("page3: expected 1 item, got %d", len(items3))
	}
	if page3["next_cursor"] != nil && page3["next_cursor"] != "" {
		t.Errorf("page3: expected nil next_cursor, got %v", page3["next_cursor"])
	}

	// Verify no duplicates across pages
	allIDs := make(map[string]bool)
	for _, item := range append(append(items1, items2...), items3...) {
		obj, _ := item.(map[string]any)
		id, _ := obj["id"].(string)
		if allIDs[id] {
			t.Errorf("duplicate id found: %s", id)
		}
		allIDs[id] = true
	}
	if len(allIDs) != 5 {
		t.Errorf("expected 5 unique IDs across pages, got %d", len(allIDs))
	}
	rl.Printf("cursor pagination returned 5 unique objects across 3 pages")
}

func TestGraph_ListObjects_InvalidCursor(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search?cursor=invalid-cursor-format", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

// ============ Patch Object Tests ============

func TestGraph_PatchObject_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create an object
	createResp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "Requirement",
		"status": "draft",
		"properties": map[string]any{
			"title": "Original Title",
		},
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	origID := created["id"].(string)
	origCanonicalID := created["canonical_id"].(string)
	t.Cleanup(func() { deleteGraphObject(t, projectID, origID) })

	// Patch the object
	resp := doAPILogged(t, rl, "PATCH", "/api/graph/objects/"+origID, e2eTestToken(), projectID, jsonBody(map[string]any{
		"status": "approved",
		"properties": map[string]any{
			"title": "Updated Title",
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	// New version should be created
	if response["id"] == origID {
		t.Error("expected new ID after patch")
	}
	if response["canonical_id"] != origCanonicalID {
		t.Errorf("expected canonical_id=%s, got %v", origCanonicalID, response["canonical_id"])
	}
	if v, _ := response["version"].(float64); int(v) != 2 {
		t.Errorf("expected version=2, got %v", response["version"])
	}
	if response["status"] != "approved" {
		t.Errorf("expected status=approved, got %v", response["status"])
	}
	props, _ := response["properties"].(map[string]any)
	if props["title"] != "Updated Title" {
		t.Errorf("expected title=Updated Title, got %v", props["title"])
	}
	rl.Printf("patched object, new version id=%v", response["id"])
}

func TestGraph_PatchObject_MergesProperties(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create an object with multiple properties
	createResp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type": "Requirement",
		"properties": map[string]any{
			"title":       "Original Title",
			"description": "Original Description",
			"priority":    "high",
		},
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	objID := created["id"].(string)
	t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })

	// Patch only the title
	resp := doAPILogged(t, rl, "PATCH", "/api/graph/objects/"+objID, e2eTestToken(), projectID, jsonBody(map[string]any{
		"properties": map[string]any{
			"title": "Updated Title",
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	props, _ := response["properties"].(map[string]any)
	if props["title"] != "Updated Title" {
		t.Errorf("expected title=Updated Title, got %v", props["title"])
	}
	if props["description"] != "Original Description" {
		t.Errorf("expected description=Original Description, got %v", props["description"])
	}
	if props["priority"] != "high" {
		t.Errorf("expected priority=high, got %v", props["priority"])
	}
}

// ============ Delete Object Tests ============

func TestGraph_DeleteObject_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create an object
	createResp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type": "Requirement",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	objID := created["id"].(string)

	// Delete the object
	resp := doAPILogged(t, rl, "DELETE", "/api/graph/objects/"+objID, e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusOK)

	// Object should not appear in list
	listResp := doAPILogged(t, rl, "GET", "/api/graph/objects/search", e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)

	var listResponse map[string]any
	parseBodyJSON(t, listBody, &listResponse)
	items, _ := listResponse["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty list after delete, got %d items", len(items))
	}
	rl.Printf("deleted graph object id=%s", objID)
}

func TestGraph_DeleteObject_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/graph/objects/"+uuid.New().String(), e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// ============ Restore Object Tests ============

func TestGraph_RestoreObject_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create and delete an object
	createResp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type": "Requirement",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	objID := created["id"].(string)
	origCanonicalID := created["canonical_id"].(string)

	deleteResp := doAPILogged(t, rl, "DELETE", "/api/graph/objects/"+objID, e2eTestToken(), projectID, nil)
	mustStatus(t, deleteResp, http.StatusOK)

	// Get the deleted object to find its new ID (tombstone version)
	listResp := doAPILogged(t, rl, "GET", "/api/graph/objects/search?include_deleted=true", e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)

	var listResponse map[string]any
	parseBodyJSON(t, listBody, &listResponse)
	items, _ := listResponse["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 deleted item, got %d", len(items))
	}
	deletedObj, _ := items[0].(map[string]any)
	deletedID := deletedObj["id"].(string)

	// Restore the object
	resp := doAPILogged(t, rl, "POST", "/api/graph/objects/"+deletedID+"/restore", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["deleted_at"] != nil {
		t.Errorf("expected deleted_at=nil after restore, got %v", response["deleted_at"])
	}
	if response["canonical_id"] != origCanonicalID {
		t.Errorf("expected canonical_id=%s, got %v", origCanonicalID, response["canonical_id"])
	}
	rl.Printf("restored graph object, canonical_id=%s", origCanonicalID)
}

// ============ History Tests ============

func TestGraph_GetObjectHistory_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create an object
	createResp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "Requirement",
		"status": "draft",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	objID := created["id"].(string)
	t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })

	// Update the object
	doAPILogged(t, rl, "PATCH", "/api/graph/objects/"+objID, e2eTestToken(), projectID, jsonBody(map[string]any{
		"status": "approved",
	}))

	// Get history
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/"+objID+"/history", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	versions, _ := response["versions"].([]any)
	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}
	// Versions should be in descending order
	if len(versions) >= 2 {
		v1, _ := versions[0].(map[string]any)
		v2, _ := versions[1].(map[string]any)
		ver1, _ := v1["version"].(float64)
		ver2, _ := v2["version"].(float64)
		if int(ver1) != 2 || int(ver2) != 1 {
			t.Errorf("expected versions [2,1], got [%v,%v]", ver1, ver2)
		}
	}
	rl.Printf("history has 2 versions")
}

// ============ Edges Tests ============

func TestGraph_GetObjectEdges_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create an object
	createResp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type": "Requirement",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	objID := created["id"].(string)
	t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })

	// Get edges
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/"+objID+"/edges", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	incoming, _ := response["incoming"].([]any)
	outgoing, _ := response["outgoing"].([]any)
	if len(incoming) != 0 {
		t.Errorf("expected empty incoming, got %d", len(incoming))
	}
	if len(outgoing) != 0 {
		t.Errorf("expected empty outgoing, got %d", len(outgoing))
	}
}

// ============ Relationship Tests ============

func TestGraph_CreateRelationship_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	resp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
		"properties": map[string]any{
			"reason": "Decision informs requirement",
		},
		"weight": 0.8,
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["id"] == nil || response["id"] == "" {
		t.Error("expected non-empty id")
	}
	if rl, ok := response["type"].(string); !ok || !strings.EqualFold(rl, "DEPENDS_ON") {
		t.Errorf("expected type=DEPENDS_ON (case-insensitive), got %v", response["type"])
	}
	if response["src_id"] != srcID {
		t.Errorf("expected src_id=%s, got %v", srcID, response["src_id"])
	}
	if response["dst_id"] != dstID {
		t.Errorf("expected dst_id=%s, got %v", dstID, response["dst_id"])
	}
	props, _ := response["properties"].(map[string]any)
	if props["reason"] != "Decision informs requirement" {
		t.Errorf("expected reason=Decision informs requirement, got %v", props["reason"])
	}
	if v, _ := response["version"].(float64); int(v) != 1 {
		t.Errorf("expected version=1, got %v", response["version"])
	}
	rl.Printf("created relationship id=%v", response["id"])
}

func TestGraph_CreateRelationship_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	resp := doAPILogged(t, rl, "POST", "/api/graph/relationships", "", projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestGraph_CreateRelationship_MissingType(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	resp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"src_id": srcID,
		"dst_id": dstID,
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraph_CreateRelationship_SelfLoopNotAllowed(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	objID := createGraphObject(t, projectID, "Requirement", "", nil)

	resp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": objID,
		"dst_id": objID,
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraph_CreateRelationship_EndpointNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)

	resp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": uuid.New().String(),
	}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestGraph_CreateRelationship_Idempotent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	body := jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
		"properties": map[string]any{
			"reason": "test",
		},
	})

	// First creation
	resp1 := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, body)
	b1 := mustStatus(t, resp1, http.StatusCreated)
	var first map[string]any
	parseBodyJSON(t, b1, &first)

	// Second creation with same properties
	resp2 := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, body)
	b2 := mustStatus(t, resp2, http.StatusCreated)
	var second map[string]any
	parseBodyJSON(t, b2, &second)

	// Should return the same relationship (no new version)
	if first["id"] != second["id"] {
		t.Errorf("expected idempotent: same id, got first=%v second=%v", first["id"], second["id"])
	}
	if first["version"] != second["version"] {
		t.Errorf("expected same version, got first=%v second=%v", first["version"], second["version"])
	}
	rl.Printf("idempotent relationship creation confirmed, id=%v", first["id"])
}

func TestGraph_GetRelationship_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	createResp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	relID := created["id"].(string)
	t.Cleanup(func() { deleteRelationship(t, projectID, relID) })

	resp := doAPILogged(t, rl, "GET", "/api/graph/relationships/"+relID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["id"] != relID {
		t.Errorf("expected id=%s, got %v", relID, response["id"])
	}
	if rl, ok := response["type"].(string); !ok || !strings.EqualFold(rl, "DEPENDS_ON") {
		t.Errorf("expected type=DEPENDS_ON (case-insensitive), got %v", response["type"])
	}
}

func TestGraph_GetRelationship_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/relationships/"+uuid.New().String(), e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestGraph_ListRelationships_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/relationships/search", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty items, got %d", len(items))
	}
	if total, _ := response["total"].(float64); int(total) != 0 {
		t.Errorf("expected total=0, got %v", response["total"])
	}
}

func TestGraph_ListRelationships_FilterByType(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)
	dst2ID := createGraphObject(t, projectID, "Task", "", nil)

	rel1ID := createRelationship(t, projectID, srcID, dstID, "DEPENDS_ON", nil)
	rel2ID := createRelationship(t, projectID, srcID, dst2ID, "IMPLEMENTS", nil)
	_ = rel1ID
	_ = rel2ID

	resp := doAPILogged(t, rl, "GET", "/api/graph/relationships/search?type=DEPENDS_ON", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 DEPENDS_ON relationship, got %d", len(items))
	}
	if len(items) > 0 {
		item, _ := items[0].(map[string]any)
		if rl, ok := item["type"].(string); !ok || !strings.EqualFold(rl, "DEPENDS_ON") {
			t.Errorf("expected type=DEPENDS_ON (case-insensitive), got %v", item["type"])
		}
	}
}

func TestGraph_ListRelationships_FilterBySrcID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	src1ID := createGraphObject(t, projectID, "Requirement", "", nil)
	src2ID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	createRelationship(t, projectID, src1ID, dstID, "DEPENDS_ON", nil)
	createRelationship(t, projectID, src2ID, dstID, "DEPENDS_ON", nil)

	resp := doAPILogged(t, rl, "GET", "/api/graph/relationships/search?src_id="+src1ID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 relationship from src1, got %d", len(items))
	}
	if len(items) > 0 {
		item, _ := items[0].(map[string]any)
		if item["src_id"] != src1ID {
			t.Errorf("expected src_id=%s, got %v", src1ID, item["src_id"])
		}
	}
}

func TestGraph_PatchRelationship_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	createResp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
		"properties": map[string]any{
			"reason": "Initial reason",
		},
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	relID := created["id"].(string)
	origCanonicalID := created["canonical_id"].(string)
	t.Cleanup(func() { deleteRelationship(t, projectID, relID) })

	// Patch the relationship
	resp := doAPILogged(t, rl, "PATCH", "/api/graph/relationships/"+relID, e2eTestToken(), projectID, jsonBody(map[string]any{
		"properties": map[string]any{
			"reason": "Updated reason",
		},
		"weight": 0.9,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["id"] == relID {
		t.Error("expected new ID after patch")
	}
	if response["canonical_id"] != origCanonicalID {
		t.Errorf("expected canonical_id=%s, got %v", origCanonicalID, response["canonical_id"])
	}
	if v, _ := response["version"].(float64); int(v) != 2 {
		t.Errorf("expected version=2, got %v", response["version"])
	}
	props, _ := response["properties"].(map[string]any)
	if props["reason"] != "Updated reason" {
		t.Errorf("expected reason=Updated reason, got %v", props["reason"])
	}
}

func TestGraph_PatchRelationship_MergesProperties(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	createResp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
		"properties": map[string]any{
			"reason":   "Initial reason",
			"priority": "high",
		},
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	relID := created["id"].(string)
	t.Cleanup(func() { deleteRelationship(t, projectID, relID) })

	resp := doAPILogged(t, rl, "PATCH", "/api/graph/relationships/"+relID, e2eTestToken(), projectID, jsonBody(map[string]any{
		"properties": map[string]any{
			"reason": "Updated reason",
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	props, _ := response["properties"].(map[string]any)
	if props["reason"] != "Updated reason" {
		t.Errorf("expected reason=Updated reason, got %v", props["reason"])
	}
	if props["priority"] != "high" {
		t.Errorf("expected priority=high, got %v", props["priority"])
	}
}

func TestGraph_DeleteRelationship_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	createResp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	relID := created["id"].(string)

	// Delete the relationship
	resp := doAPILogged(t, rl, "DELETE", "/api/graph/relationships/"+relID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)
	if response["deleted_at"] == nil {
		t.Error("expected non-nil deleted_at on tombstone")
	}

	// Relationship should not appear in list
	listResp := doAPILogged(t, rl, "GET", "/api/graph/relationships/search", e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)
	var listResponse map[string]any
	parseBodyJSON(t, listBody, &listResponse)
	items, _ := listResponse["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty list after delete, got %d", len(items))
	}
}

func TestGraph_RestoreRelationship_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	createResp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	relID := created["id"].(string)
	origCanonicalID := created["canonical_id"].(string)

	// Delete
	deleteResp := doAPILogged(t, rl, "DELETE", "/api/graph/relationships/"+relID, e2eTestToken(), projectID, nil)
	deleteBody := mustStatus(t, deleteResp, http.StatusOK)
	var deleted map[string]any
	parseBodyJSON(t, deleteBody, &deleted)
	deletedID := deleted["id"].(string)

	// Restore
	resp := doAPILogged(t, rl, "POST", "/api/graph/relationships/"+deletedID+"/restore", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusCreated)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["deleted_at"] != nil {
		t.Errorf("expected deleted_at=nil after restore, got %v", response["deleted_at"])
	}
	if response["canonical_id"] != origCanonicalID {
		t.Errorf("expected canonical_id=%s, got %v", origCanonicalID, response["canonical_id"])
	}
}

func TestGraph_GetRelationshipHistory_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	createResp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
		"properties": map[string]any{
			"reason": "Version 1",
		},
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	relID := created["id"].(string)
	t.Cleanup(func() { deleteRelationship(t, projectID, relID) })

	// Update to create version 2
	doAPILogged(t, rl, "PATCH", "/api/graph/relationships/"+relID, e2eTestToken(), projectID, jsonBody(map[string]any{
		"properties": map[string]any{
			"reason": "Version 2",
		},
	}))

	resp := doAPILogged(t, rl, "GET", "/api/graph/relationships/"+relID+"/history", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var versions []map[string]any
	parseBodyJSON(t, body, &versions)

	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}
	if len(versions) >= 2 {
		if v, _ := versions[0]["version"].(float64); int(v) != 2 {
			t.Errorf("expected first version=2, got %v", versions[0]["version"])
		}
		if v, _ := versions[1]["version"].(float64); int(v) != 1 {
			t.Errorf("expected second version=1, got %v", versions[1]["version"])
		}
	}
}

func TestGraph_GetObjectEdges_WithRelationships(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	srcID := createGraphObject(t, projectID, "Requirement", "", nil)
	dstID := createGraphObject(t, projectID, "Decision", "", nil)

	createRelationship(t, projectID, srcID, dstID, "DEPENDS_ON", nil)

	// Get edges for source object
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/"+srcID+"/edges", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	outgoing, _ := response["outgoing"].([]any)
	incoming, _ := response["incoming"].([]any)
	if len(outgoing) != 1 {
		t.Errorf("expected 1 outgoing edge for src, got %d", len(outgoing))
	}
	if len(incoming) != 0 {
		t.Errorf("expected 0 incoming edges for src, got %d", len(incoming))
	}
	if len(outgoing) > 0 {
		edge, _ := outgoing[0].(map[string]any)
		if et, ok := edge["type"].(string); !ok || !strings.EqualFold(et, "DEPENDS_ON") {
			t.Errorf("expected edge type=DEPENDS_ON, got %v", edge["type"])
		}
	}

	// Get edges for destination object
	resp = doAPILogged(t, rl, "GET", "/api/graph/objects/"+dstID+"/edges", e2eTestToken(), projectID, nil)
	body = mustStatus(t, resp, http.StatusOK)

	var response2 map[string]any
	parseBodyJSON(t, body, &response2)

	outgoing2, _ := response2["outgoing"].([]any)
	incoming2, _ := response2["incoming"].([]any)
	if len(outgoing2) != 0 {
		t.Errorf("expected 0 outgoing edges for dst, got %d", len(outgoing2))
	}
	if len(incoming2) != 1 {
		t.Errorf("expected 1 incoming edge for dst, got %d", len(incoming2))
	}
	rl.Printf("edges verified for src/dst objects")
}

// ============ Search Tests ============

func TestGraph_FTSSearch_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/fts?q=test", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestGraph_FTSSearch_RequiresQuery(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/fts", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraph_FTSSearch_EmptyResults(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/fts?q=nonexistent", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	data, _ := response["data"].([]any)
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d", len(data))
	}
	if total, _ := response["total"].(float64); int(total) != 0 {
		t.Errorf("expected total=0, got %v", response["total"])
	}
	if hasMore, _ := response["has_more"].(bool); hasMore {
		t.Error("expected has_more=false")
	}
}

func TestGraph_FTSSearch_WithFilters(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "Requirement",
		"labels": []string{"important"},
	}))

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/fts?q=test&types=Requirement", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)
	if response["data"] == nil {
		t.Error("expected non-nil data")
	}
}

func TestGraph_VectorSearch_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/objects/vector-search", "", projectID, jsonBody(map[string]any{
		"vector": []float32{0.1, 0.2, 0.3},
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestGraph_VectorSearch_RequiresVector(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/objects/vector-search", e2eTestToken(), projectID, jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraph_VectorSearch_EmptyResults(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) * 0.001
	}

	resp := doAPILogged(t, rl, "POST", "/api/graph/objects/vector-search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"vector": vector,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	data, _ := response["data"].([]any)
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d", len(data))
	}
}

func TestGraph_VectorSearch_WithFilters(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) * 0.001
	}

	resp := doAPILogged(t, rl, "POST", "/api/graph/objects/vector-search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"vector":      vector,
		"types":       []string{"Requirement"},
		"limit":       10,
		"maxDistance": 0.5,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)
	if response["data"] == nil {
		t.Error("expected non-nil data")
	}
}

func TestGraph_HybridSearch_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", "", projectID, jsonBody(map[string]any{
		"query": "test",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestGraph_HybridSearch_RequiresQueryOrVector(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraph_HybridSearch_QueryOnly(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "authentication",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)
	if response["data"] == nil {
		t.Error("expected non-nil data")
	}
}

func TestGraph_HybridSearch_VectorOnly(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) * 0.001
	}

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"vector": vector,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)
	if response["data"] == nil {
		t.Error("expected non-nil data")
	}
}

func TestGraph_HybridSearch_QueryAndVector(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) * 0.001
	}

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":         "authentication",
		"vector":        vector,
		"lexicalWeight": 0.7,
		"vectorWeight":  0.3,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)
	if response["data"] == nil {
		t.Error("expected non-nil data")
	}
}

func TestGraph_HybridSearch_WithFilters(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":  "authentication",
		"types":  []string{"Requirement", "Decision"},
		"labels": []string{"security"},
		"limit":  10,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)
	if response["data"] == nil {
		t.Error("expected non-nil data")
	}
}

// stringPtr returns a pointer to the given string (for use in JSON payloads).
func stringPtrGraph(s string) *string { return &s }

// suppress unused warning
var _ = fmt.Sprintf
var _ = stringPtrGraph
