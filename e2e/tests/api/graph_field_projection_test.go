// Package api_test — graph_field_projection_test.go
//
// Tests for the graph objects field projection (fields= query parameter).
// Ported from emergent.memory/apps/server/tests/e2e/graph_field_projection_test.go
package api_test

import (
	"net/http"
	"testing"
)

// TestGraphFieldProjection_ListObjects_FieldProjection verifies that the fields
// parameter filters properties in the list response.
func TestGraphFieldProjection_ListObjects_FieldProjection(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create an object with multiple properties
	resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type": "Task",
		"properties": map[string]any{
			"title":       "Build Feature",
			"description": "Build the new feature for the product",
			"priority":    "high",
			"estimate":    5,
		},
	}))
	createBody := mustStatus(t, resp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	t.Cleanup(func() { deleteGraphObject(t, projectID, created["id"].(string)) })

	// List with field projection — only request title and priority
	listResp := doAPILogged(t, rl, "GET", "/api/graph/objects/search?type=Task&fields=title,priority", e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, listBody, &response)

	items, _ := response["items"].([]any)
	if len(items) < 1 {
		t.Fatal("expected at least 1 item")
	}
	item, _ := items[0].(map[string]any)
	props, _ := item["properties"].(map[string]any)

	// Should include requested fields
	if _, ok := props["title"]; !ok {
		t.Error("expected 'title' to be present in projected properties")
	}
	if _, ok := props["priority"]; !ok {
		t.Error("expected 'priority' to be present in projected properties")
	}

	// Should NOT include non-requested fields
	if _, ok := props["description"]; ok {
		t.Error("expected 'description' NOT to be present in projected properties")
	}
	if _, ok := props["estimate"]; ok {
		t.Error("expected 'estimate' NOT to be present in projected properties")
	}

	// Metadata fields should still be present
	if item["id"] == nil || item["id"] == "" {
		t.Error("expected non-empty id in item")
	}
	if item["type"] != "Task" {
		t.Errorf("expected type=Task, got %v", item["type"])
	}
	rl.Printf("field projection returned only title and priority")
}

// TestGraphFieldProjection_ListObjects_NoFieldProjection returns all properties
// when fields is omitted.
func TestGraphFieldProjection_ListObjects_NoFieldProjection(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Create an object with multiple properties
	resp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type": "Requirement",
		"properties": map[string]any{
			"title":       "Auth Requirement",
			"description": "Users must authenticate",
		},
	}))
	createBody := mustStatus(t, resp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, createBody, &created)
	t.Cleanup(func() { deleteGraphObject(t, projectID, created["id"].(string)) })

	// List without field projection
	listResp := doAPILogged(t, rl, "GET", "/api/graph/objects/search?type=Requirement", e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, listBody, &response)

	items, _ := response["items"].([]any)
	if len(items) < 1 {
		t.Fatal("expected at least 1 item")
	}
	item, _ := items[0].(map[string]any)
	props, _ := item["properties"].(map[string]any)

	// All properties should be present
	if _, ok := props["title"]; !ok {
		t.Error("expected 'title' to be present in properties")
	}
	if _, ok := props["description"]; !ok {
		t.Error("expected 'description' to be present in properties")
	}
	rl.Printf("no field projection returned all properties")
}
