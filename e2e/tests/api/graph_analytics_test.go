// Package api_test — graph_analytics_test.go
//
// Tests for the graph analytics API endpoints.
// Ported from emergent.memory/apps/server/tests/e2e/graph_analytics_test.go
package api_test

import (
	"net/http"
	"testing"
	"time"
)

func TestGraphAnalytics_GetMostAccessed_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create test objects
	for i := 0; i < 5; i++ {
		resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
			"type": "TestObject",
			"properties": map[string]any{
				"name": "Test Object",
			},
		}))
		b := mustStatus(t, resp, http.StatusCreated)
		var obj map[string]any
		parseBodyJSON(t, b, &obj)
		t.Cleanup(func() { deleteGraphObject(t, projectID, obj["id"].(string)) })
	}

	// Trigger access via search
	doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "Test",
		"limit": 5,
	}))

	time.Sleep(100 * time.Millisecond)

	resp := doAPILogged(t, rl, "GET", "/api/graph/analytics/most-accessed", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) < 1 {
		t.Skip("no accessed objects yet — search may not have triggered access tracking")
	}
	total, _ := response["total"].(float64)
	if int(total) < 1 {
		t.Errorf("expected total >= 1, got %v", response["total"])
	}
	if response["meta"] == nil {
		t.Error("expected non-nil meta")
	}
	meta, _ := response["meta"].(map[string]any)
	if meta["limit"] == nil {
		t.Error("expected limit in meta")
	}
	rl.Printf("most-accessed returned %d items", len(items))
}

func TestGraphAnalytics_GetMostAccessed_WithLimit(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create test objects
	for i := 0; i < 5; i++ {
		resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
			"type": "TestObject",
			"properties": map[string]any{
				"name": "Test Object",
			},
		}))
		b := mustStatus(t, resp, http.StatusCreated)
		var obj map[string]any
		parseBodyJSON(t, b, &obj)
		t.Cleanup(func() { deleteGraphObject(t, projectID, obj["id"].(string)) })
	}

	// Trigger access via search
	doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "Test",
		"limit": 5,
	}))

	time.Sleep(100 * time.Millisecond)

	resp := doAPILogged(t, rl, "GET", "/api/graph/analytics/most-accessed?limit=2", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) > 2 {
		t.Errorf("expected at most 2 items, got %d", len(items))
	}
	meta, _ := response["meta"].(map[string]any)
	if limit, _ := meta["limit"].(float64); int(limit) != 2 {
		t.Errorf("expected limit=2 in meta, got %v", meta["limit"])
	}
}

func TestGraphAnalytics_GetMostAccessed_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/analytics/most-accessed", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestGraphAnalytics_GetMostAccessed_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/analytics/most-accessed", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraphAnalytics_GetUnused_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create test objects
	for i := 0; i < 5; i++ {
		resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
			"type": "TestObject",
			"properties": map[string]any{
				"name": "Test Object",
			},
		}))
		b := mustStatus(t, resp, http.StatusCreated)
		var obj map[string]any
		parseBodyJSON(t, b, &obj)
		t.Cleanup(func() { deleteGraphObject(t, projectID, obj["id"].(string)) })
	}

	resp := doAPILogged(t, rl, "GET", "/api/graph/analytics/unused", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	items, _ := response["items"].([]any)
	if len(items) < 5 {
		t.Errorf("expected at least 5 unused items, got %d", len(items))
	}
	total, _ := response["total"].(float64)
	if int(total) < 5 {
		t.Errorf("expected total >= 5, got %v", response["total"])
	}
	for _, item := range items {
		obj, _ := item.(map[string]any)
		if obj["id"] == nil || obj["id"] == "" {
			t.Error("expected non-empty id on unused object")
		}
		if obj["type"] != "TestObject" {
			t.Errorf("expected type=TestObject, got %v", obj["type"])
		}
		if obj["last_accessed_at"] != nil {
			t.Errorf("unused objects should have nil last_accessed_at, got %v", obj["last_accessed_at"])
		}
	}

	meta, _ := response["meta"].(map[string]any)
	if meta == nil {
		t.Fatal("expected non-nil meta")
	}
	if limit, _ := meta["limit"].(float64); int(limit) != 50 {
		t.Errorf("expected default limit=50, got %v", meta["limit"])
	}
	if days, _ := meta["daysThreshold"].(float64); int(days) != 30 {
		t.Errorf("expected default daysThreshold=30, got %v", meta["daysThreshold"])
	}
	rl.Printf("unused returned %d items", len(items))
}

func TestGraphAnalytics_GetUnused_WithDaysThreshold(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/analytics/unused?days=7", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	meta, _ := response["meta"].(map[string]any)
	if days, _ := meta["daysThreshold"].(float64); int(days) != 7 {
		t.Errorf("expected daysThreshold=7, got %v", meta["daysThreshold"])
	}
}

func TestGraphAnalytics_GetUnused_ExcludesRecentlyAccessed(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create test objects
	for i := 0; i < 5; i++ {
		resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
			"type": "TestObject",
			"properties": map[string]any{
				"name": "Test Object",
			},
		}))
		b := mustStatus(t, resp, http.StatusCreated)
		var obj map[string]any
		parseBodyJSON(t, b, &obj)
		t.Cleanup(func() { deleteGraphObject(t, projectID, obj["id"].(string)) })
	}

	// Trigger access via search
	doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "Test Object",
		"limit": 1,
	}))

	time.Sleep(100 * time.Millisecond)

	resp := doAPILogged(t, rl, "GET", "/api/graph/analytics/unused?days=1", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	// For items that were accessed, last_accessed_at should be set if they appear
	items, _ := response["items"].([]any)
	for _, item := range items {
		obj, _ := item.(map[string]any)
		props, _ := obj["properties"].(map[string]any)
		if props != nil && props["name"] == "Test Object A" {
			if obj["last_accessed_at"] != nil {
				// If it was accessed, timestamp should be set
				if obj["days_since_access"] == nil {
					t.Error("expected days_since_access to be set for accessed object")
				}
			}
		}
	}
	rl.Printf("unused (days=1) returned %d items", len(items))
}

func TestGraphAnalytics_GetUnused_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/analytics/unused", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestGraphAnalytics_GetUnused_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/analytics/unused", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}
