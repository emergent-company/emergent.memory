// Package api_test — graph_search_test.go
//
// Tests for the graph hybrid search API.
// Ported from emergent.memory/apps/server/tests/e2e/graph_search_test.go
package api_test

import (
	"net/http"
	"testing"
)

// createTestGraphObjectForSearch creates a graph object for search tests.
func createTestGraphObjectForSearch(t *testing.T, projectID, objType, key string, properties map[string]any) string {
	t.Helper()
	token := e2eTestToken()
	body := map[string]any{
		"type":       objType,
		"properties": properties,
	}
	if key != "" {
		body["key"] = key
	}
	resp := doAPI(t, "POST", "/api/graph/objects", token, projectID, jsonBody(body))
	b := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, b, &result)
	objID := result["id"].(string)
	t.Cleanup(func() { deleteGraphObject(t, projectID, objID) })
	return objID
}

// ============ Hybrid Search Basic Tests ============

func TestGraphSearch_HybridSearch_BasicQuery(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	createTestGraphObjectForSearch(t, projectID, "Requirement", "req-001", map[string]any{
		"title":       "User Authentication",
		"description": "Implement OAuth2 login flow",
	})

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

func TestGraphSearch_HybridSearch_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", "", projectID, jsonBody(map[string]any{
		"query": "test",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestGraphSearch_HybridSearch_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), "", jsonBody(map[string]any{
		"query": "test",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestGraphSearch_HybridSearch_RequiresQueryOrVector(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}
	if errObj["code"] != "bad_request" {
		t.Errorf("expected code=bad_request, got %v", errObj["code"])
	}
}

// ============ Debug Mode Tests ============

func TestGraphSearch_DebugModeViaBodyField(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	createTestGraphObjectForSearch(t, projectID, "Task", "task-001", map[string]any{
		"title": "Debug test task",
	})

	// e2e-test-user has graph:search:debug scope
	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":        "debug",
		"includeDebug": true,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	meta, ok := response["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected meta to be present")
	}
	if meta["elapsed_ms"] == nil {
		t.Error("expected elapsed_ms in meta")
	}
	timing, ok := meta["timing"].(map[string]any)
	if !ok {
		t.Fatal("expected timing to be present in debug mode")
	}
	if timing["total_ms"] == nil {
		t.Error("expected total_ms in timing")
	}
	if meta["channel_stats"] == nil {
		t.Error("expected channel_stats in meta in debug mode")
	}
	rl.Printf("debug mode returned timing and channel_stats")
}

func TestGraphSearch_DebugModeViaQueryParam(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	createTestGraphObjectForSearch(t, projectID, "Task", "task-002", map[string]any{
		"title": "Query param debug task",
	})

	resp := doAPILogged(t, rl, "POST", "/api/graph/search?debug=true", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "query param",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	meta, ok := response["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected meta to be present")
	}
	if meta["timing"] == nil {
		t.Error("expected timing in meta when debug=true via query param")
	}
	if meta["channel_stats"] == nil {
		t.Error("expected channel_stats when debug=true")
	}
}

func TestGraphSearch_DebugModeRequiresScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)

	// 'graph-read' user has graph:read and graph:search:read but NOT graph:search:debug
	resp := doAPILogged(t, rl, "POST", "/api/graph/search", "graph-read", projectID, jsonBody(map[string]any{
		"query":        "test",
		"includeDebug": true,
	}))
	body := mustStatus(t, resp, http.StatusForbidden)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object")
	}
	if errObj["code"] != "forbidden" {
		t.Errorf("expected code=forbidden, got %v", errObj["code"])
	}
}

func TestGraphSearch_DebugModeRequiresScopeViaQueryParam(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search?debug=true", "graph-read", projectID, jsonBody(map[string]any{
		"query": "test",
	}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestGraphSearch_NoDebugWithoutFlag(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	createTestGraphObjectForSearch(t, projectID, "Task", "task-003", map[string]any{
		"title": "No debug task",
	})

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "no debug",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	meta, ok := response["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected meta to be present even without debug")
	}
	if meta["timing"] != nil {
		t.Error("timing should NOT be present when debug is not requested")
	}
	if meta["channel_stats"] != nil {
		t.Error("channel_stats should NOT be present when debug is not requested")
	}
}

// ============ Search Parameter Tests ============

func TestGraphSearch_WithLimit(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	for i := 0; i < 5; i++ {
		createTestGraphObjectForSearch(t, projectID, "Item", "", map[string]any{
			"name": "Limit Test Item",
		})
	}

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "Limit Test",
		"limit": 2,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	data, _ := response["data"].([]any)
	if len(data) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(data))
	}
}

func TestGraphSearch_WithWeights(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	createTestGraphObjectForSearch(t, projectID, "WeightTest", "wt-001", map[string]any{
		"title": "Weight test object",
	})

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":         "weight test",
		"lexicalWeight": 0.8,
		"vectorWeight":  0.2,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)
	if response["data"] == nil {
		t.Error("expected non-nil data")
	}
}

func TestGraphSearch_WithBranchID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":    "test",
		"branchId": "00000000-0000-0000-0000-000000000000",
	}))
	mustStatus(t, resp, http.StatusOK)
}

func TestGraphSearch_WithTypes(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	createTestGraphObjectForSearch(t, projectID, "TypeA", "ta-001", map[string]any{"title": "Type A"})
	createTestGraphObjectForSearch(t, projectID, "TypeB", "tb-001", map[string]any{"title": "Type B"})

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "Type",
		"types": []string{"TypeA"},
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	// All results should be of type TypeA (if any results returned)
	data, _ := response["data"].([]any)
	for _, item := range data {
		obj, _ := item.(map[string]any)
		object, _ := obj["object"].(map[string]any)
		if object != nil && object["type"] != "TypeA" {
			t.Errorf("expected type=TypeA, got %v", object["type"])
		}
	}
}

func TestGraphSearch_WithLabels(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	createTestGraphObjectForSearch(t, projectID, "LabelTest", "lt-001", map[string]any{"title": "With Label"})

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":  "Label",
		"labels": []string{"important"},
	}))
	mustStatus(t, resp, http.StatusOK)
}

func TestGraphSearch_WithStatus(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp2 := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":       "StatusTest",
		"status":     "active",
		"properties": map[string]any{"title": "Active Status"},
	}))
	b2 := mustStatus(t, resp2, http.StatusCreated)
	var obj map[string]any
	parseBodyJSON(t, b2, &obj)
	t.Cleanup(func() { deleteGraphObject(t, projectID, obj["id"].(string)) })

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":  "Status",
		"status": "active",
	}))
	mustStatus(t, resp, http.StatusOK)
}

// ============ Empty Results Tests ============

func TestGraphSearch_EmptyResults(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "xyznonexistentquery123456",
	}))
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

func TestGraphSearch_EmptyResultsWithDebug(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/graph/search", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":        "xyznonexistentquery123456",
		"includeDebug": true,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	data, _ := response["data"].([]any)
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d", len(data))
	}
	meta, ok := response["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected meta to be present")
	}
	if meta["timing"] == nil {
		t.Error("timing should be present even with empty results")
	}
	if meta["channel_stats"] == nil {
		t.Error("channel_stats should be present even with empty results")
	}
	channelStats, _ := meta["channel_stats"].(map[string]any)
	if channelStats != nil {
		lexical, _ := channelStats["lexical"].(map[string]any)
		if lexical != nil {
			if count, _ := lexical["count"].(float64); int(count) != 0 {
				t.Errorf("expected lexical count=0, got %v", lexical["count"])
			}
		}
	}
	rl.Printf("empty results with debug returned timing and channel_stats")
}
