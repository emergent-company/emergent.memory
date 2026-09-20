// Package api_test — useractivity_test.go
//
// Tests for user activity API endpoints.
// Ported from emergent.memory/apps/server/tests/e2e/useractivity_test.go
//
// Tests requiring direct DB access (createActivityViaDB) are omitted.
// Record and delete lifecycle tests use the API exclusively.
package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// ─── Record Activity Tests ────────────────────────────────────────────────────

func TestUserActivity_Record_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/user-activity/record?project_id=%s", projectID), "", "", jsonBody(map[string]any{
		"resourceType": "document",
		"resourceId":   uuid.New().String(),
		"actionType":   "viewed",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestUserActivity_Record_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/user-activity/record", e2eTestToken(), "", jsonBody(map[string]any{
		"resourceType": "document",
		"resourceId":   uuid.New().String(),
		"actionType":   "viewed",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestUserActivity_Record_RequiresFields(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	recordURL := fmt.Sprintf("/api/user-activity/record?project_id=%s", projectID)

	// Missing resourceType
	resp := doAPILogged(t, rl, "POST", recordURL, e2eTestToken(), "", jsonBody(map[string]any{
		"resourceId": uuid.New().String(),
		"actionType": "viewed",
	}))
	mustStatus(t, resp, http.StatusBadRequest)

	// Missing resourceId
	resp = doAPILogged(t, rl, "POST", recordURL, e2eTestToken(), "", jsonBody(map[string]any{
		"resourceType": "document",
		"actionType":   "viewed",
	}))
	mustStatus(t, resp, http.StatusBadRequest)

	// Missing actionType
	resp = doAPILogged(t, rl, "POST", recordURL, e2eTestToken(), "", jsonBody(map[string]any{
		"resourceType": "document",
		"resourceId":   uuid.New().String(),
	}))
	mustStatus(t, resp, http.StatusBadRequest)

	rl.Printf("missing required fields correctly rejected")
}

func TestUserActivity_Record_ValidatesResourceID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/user-activity/record?project_id=%s", projectID), e2eTestToken(), "", jsonBody(map[string]any{
		"resourceType": "document",
		"resourceId":   "not-a-uuid",
		"actionType":   "viewed",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestUserActivity_Record_ValidatesProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/user-activity/record?project_id=invalid-uuid", e2eTestToken(), "", jsonBody(map[string]any{
		"resourceType": "document",
		"resourceId":   uuid.New().String(),
		"actionType":   "viewed",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestUserActivity_Record_RecordsActivity(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resourceID := uuid.New().String()

	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/user-activity/record?project_id=%s", projectID), e2eTestToken(), "", jsonBody(map[string]any{
		"resourceType": "document",
		"resourceId":   resourceID,
		"actionType":   "viewed",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["status"] != "recorded" {
		t.Errorf("expected status=recorded, got %v", result["status"])
	}

	// Verify via GET that the item appears in recent activity
	getResp := doAPILogged(t, rl, "GET", "/api/user-activity/recent", e2eTestToken(), "", nil)
	getBody := mustStatus(t, getResp, http.StatusOK)

	var recentResult map[string]any
	parseBodyJSON(t, getBody, &recentResult)

	data, _ := recentResult["data"].([]any)
	found := false
	for _, item := range data {
		m, _ := item.(map[string]any)
		if m["resourceId"] == resourceID {
			if m["resourceType"] != "document" {
				t.Errorf("expected resourceType=document, got %v", m["resourceType"])
			}
			if m["actionType"] != "viewed" {
				t.Errorf("expected actionType=viewed, got %v", m["actionType"])
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("recorded activity %s not found in recent items", resourceID)
	}
	rl.Printf("activity recorded and visible in recent list")
}

func TestUserActivity_Record_WithOptionalFields(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resourceID := uuid.New().String()
	resourceName := "Test Document"
	resourceSubtype := "pdf"

	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/user-activity/record?project_id=%s", projectID), e2eTestToken(), "", jsonBody(map[string]any{
		"resourceType":    "document",
		"resourceId":      resourceID,
		"resourceName":    resourceName,
		"resourceSubtype": resourceSubtype,
		"actionType":      "edited",
	}))
	mustStatus(t, resp, http.StatusOK)

	// Verify optional fields were stored
	getResp := doAPILogged(t, rl, "GET", "/api/user-activity/recent", e2eTestToken(), "", nil)
	getBody := mustStatus(t, getResp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, getBody, &result)

	data, _ := result["data"].([]any)
	found := false
	for _, item := range data {
		m, _ := item.(map[string]any)
		if m["resourceId"] == resourceID {
			if m["actionType"] != "edited" {
				t.Errorf("expected actionType=edited, got %v", m["actionType"])
			}
			if m["resourceName"] != resourceName {
				t.Errorf("expected resourceName=%s, got %v", resourceName, m["resourceName"])
			}
			if m["resourceSubtype"] != resourceSubtype {
				t.Errorf("expected resourceSubtype=%s, got %v", resourceSubtype, m["resourceSubtype"])
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("activity with optional fields %s not found in recent items", resourceID)
	}
	rl.Printf("optional fields stored correctly")
}

// ─── GetRecent Tests ──────────────────────────────────────────────────────────

func TestUserActivity_GetRecent_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user-activity/recent", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// ─── GetRecentByType Tests ────────────────────────────────────────────────────

func TestUserActivity_GetRecentByType_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user-activity/recent/document", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestUserActivity_GetRecentByType_FiltersCorrectly(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Record a document activity
	docResourceID := uuid.New().String()
	doAPILogged(t, rl, "POST", fmt.Sprintf("/api/user-activity/record?project_id=%s", projectID), e2eTestToken(), "", jsonBody(map[string]any{
		"resourceType": "document",
		"resourceId":   docResourceID,
		"actionType":   "viewed",
	}))

	// Record an object activity
	doAPILogged(t, rl, "POST", fmt.Sprintf("/api/user-activity/record?project_id=%s", projectID), e2eTestToken(), "", jsonBody(map[string]any{
		"resourceType": "object",
		"resourceId":   uuid.New().String(),
		"actionType":   "viewed",
	}))

	// Get only document activity and verify all items are documents
	resp := doAPILogged(t, rl, "GET", "/api/user-activity/recent/document", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	data, _ := result["data"].([]any)
	for _, item := range data {
		m, _ := item.(map[string]any)
		if m["resourceType"] != "document" {
			t.Errorf("expected resourceType=document in filtered result, got %v", m["resourceType"])
		}
	}

	// Verify our document appears
	found := false
	for _, item := range data {
		m, _ := item.(map[string]any)
		if m["resourceId"] == docResourceID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("document activity %s not found in /recent/document", docResourceID)
	}
	rl.Printf("type filter returns only documents")
}

// ─── DeleteAll Tests ──────────────────────────────────────────────────────────

func TestUserActivity_DeleteAll_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/user-activity/recent", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestUserActivity_DeleteAll_DeletesActivity(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Record some activities first
	for i := 0; i < 2; i++ {
		doAPILogged(t, rl, "POST", fmt.Sprintf("/api/user-activity/record?project_id=%s", projectID), e2eTestToken(), "", jsonBody(map[string]any{
			"resourceType": "document",
			"resourceId":   uuid.New().String(),
			"actionType":   "viewed",
		}))
	}

	// Delete all
	resp := doAPILogged(t, rl, "DELETE", "/api/user-activity/recent", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var deleteResult map[string]any
	parseBodyJSON(t, body, &deleteResult)
	if deleteResult["status"] != "deleted" {
		t.Errorf("expected status=deleted, got %v", deleteResult["status"])
	}

	// Verify all deleted
	getResp := doAPILogged(t, rl, "GET", "/api/user-activity/recent", e2eTestToken(), "", nil)
	getBody := mustStatus(t, getResp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, getBody, &result)
	data, _ := result["data"].([]any)
	if len(data) != 0 {
		t.Errorf("expected 0 items after delete all, got %d", len(data))
	}
	rl.Printf("delete all cleared all activity")
}

func TestUserActivity_DeleteAll_SucceedsWhenEmpty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Clear any existing activity first
	doAPILogged(t, rl, "DELETE", "/api/user-activity/recent", e2eTestToken(), "", nil)

	// Delete again when already empty — should succeed
	resp := doAPILogged(t, rl, "DELETE", "/api/user-activity/recent", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusOK)
	rl.AssertionStep("delete all on empty succeeds", http.StatusOK, resp.StatusCode, nil)
}

// ─── DeleteByResource Tests ───────────────────────────────────────────────────

func TestUserActivity_DeleteByResource_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/user-activity/recent/document/"+uuid.New().String(), "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestUserActivity_DeleteByResource_ValidatesResourceID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/user-activity/recent/document/not-a-uuid", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestUserActivity_DeleteByResource_SucceedsWhenResourceNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/user-activity/recent/document/"+uuid.New().String(), e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusOK)
	rl.AssertionStep("delete non-existent succeeds", http.StatusOK, resp.StatusCode, nil)
}
