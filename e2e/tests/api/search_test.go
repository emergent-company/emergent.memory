// Package api_test — search_test.go
//
// Tests for the unified search API (/api/search/unified).
// Ported from emergent.memory/apps/server/tests/e2e/search_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// createDocumentForSearch creates a document for search tests.
func createDocumentForSearch(t *testing.T, projectID, filename, content string) string {
	t.Helper()
	token := e2eTestToken()
	body := jsonBody(map[string]any{"filename": filename, "content": content})
	resp := doAPI(t, "POST", "/api/documents", token, projectID, body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b := readRespBody(resp)
		t.Fatalf("createDocumentForSearch: expected 200 or 201, got %d: %s", resp.StatusCode, b)
	}
	b := readRespBody(resp)
	docID := parseIDFromBody(t, b)
	t.Cleanup(func() { deleteDocument(t, projectID, docID) })
	return docID
}

// createGraphObjectForSearch creates a graph object for search tests.
func createGraphObjectForSearch(t *testing.T, projectID, objType, key string, properties map[string]any) string {
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

// =============================================================================
// Test: Authentication & Authorization
// =============================================================================

func TestSearch_UnifiedSearch_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", "", projectID, jsonBody(map[string]any{
		"query": "test query",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSearch_UnifiedSearch_RequiresSearchReadScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", "no-scope", projectID, jsonBody(map[string]any{
		"query": "test query",
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

func TestSearch_UnifiedSearch_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), "", jsonBody(map[string]any{
		"query": "test query",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

// =============================================================================
// Test: Request Validation
// =============================================================================

func TestSearch_UnifiedSearch_RequiresQuery(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object")
	}
	if errObj["code"] != "bad_request" {
		t.Errorf("expected code=bad_request, got %v", errObj["code"])
	}
}

func TestSearch_UnifiedSearch_QueryTooLong(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	longQuery := make([]byte, 801)
	for i := range longQuery {
		longQuery[i] = 'a'
	}

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": string(longQuery),
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

// =============================================================================
// Test: Empty Results
// =============================================================================

func TestSearch_UnifiedSearch_EmptyResults(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "nonexistent query term",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	results, _ := response["results"].([]any)
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
	metadata, _ := response["metadata"].(map[string]any)
	if metadata == nil {
		t.Fatal("expected non-nil metadata")
	}
	if total, _ := metadata["totalResults"].(float64); int(total) != 0 {
		t.Errorf("expected totalResults=0, got %v", metadata["totalResults"])
	}
	if count, _ := metadata["graphResultCount"].(float64); int(count) != 0 {
		t.Errorf("expected graphResultCount=0, got %v", metadata["graphResultCount"])
	}
	if count, _ := metadata["textResultCount"].(float64); int(count) != 0 {
		t.Errorf("expected textResultCount=0, got %v", metadata["textResultCount"])
	}
	if response["debug"] != nil {
		t.Error("debug should be nil when not requested")
	}
	rl.Printf("unified search empty results verified")
}

// =============================================================================
// Test: Result Types Filter
// =============================================================================

func TestSearch_UnifiedSearch_ResultTypesGraph(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	uniqueKey := fmt.Sprintf("john-%d", time.Now().UnixNano())
	createGraphObjectForSearch(t, projectID, "Person", uniqueKey, map[string]any{"name": "John Doe"})
	createDocumentForSearch(t, projectID, fmt.Sprintf("test-%d.txt", time.Now().UnixNano()), "Some text content about other things")

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":       "John",
		"resultTypes": "graph",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if textCount, _ := metadata["textResultCount"].(float64); int(textCount) != 0 {
		t.Errorf("expected textResultCount=0, got %v", metadata["textResultCount"])
	}
	results, _ := response["results"].([]any)
	for _, item := range results {
		obj, _ := item.(map[string]any)
		if obj["type"] == "text" {
			t.Error("should not have text results when resultTypes=graph")
		}
	}
}

func TestSearch_UnifiedSearch_ResultTypesText(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	uniqueKey := fmt.Sprintf("john-text-%d", time.Now().UnixNano())
	createGraphObjectForSearch(t, projectID, "Person", uniqueKey, map[string]any{"name": "John Doe"})
	createDocumentForSearch(t, projectID, fmt.Sprintf("john-doc-%d.txt", time.Now().UnixNano()), "John Doe is mentioned in this text")

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":       "John",
		"resultTypes": "text",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if graphCount, _ := metadata["graphResultCount"].(float64); int(graphCount) != 0 {
		t.Errorf("expected graphResultCount=0, got %v", metadata["graphResultCount"])
	}
	results, _ := response["results"].([]any)
	for _, item := range results {
		obj, _ := item.(map[string]any)
		if obj["type"] == "graph" {
			t.Error("should not have graph results when resultTypes=text")
		}
	}
}

// =============================================================================
// Test: Fusion Strategies
// =============================================================================

func TestSearch_UnifiedSearch_FusionStrategyWeighted(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":          "test",
		"fusionStrategy": "weighted",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata["fusionStrategy"] != "weighted" {
		t.Errorf("expected fusionStrategy=weighted, got %v", metadata["fusionStrategy"])
	}
}

func TestSearch_UnifiedSearch_FusionStrategyRRF(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":          "test",
		"fusionStrategy": "rrf",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata["fusionStrategy"] != "rrf" {
		t.Errorf("expected fusionStrategy=rrf, got %v", metadata["fusionStrategy"])
	}
}

func TestSearch_UnifiedSearch_FusionStrategyInterleave(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":          "test",
		"fusionStrategy": "interleave",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata["fusionStrategy"] != "interleave" {
		t.Errorf("expected fusionStrategy=interleave, got %v", metadata["fusionStrategy"])
	}
}

func TestSearch_UnifiedSearch_FusionStrategyGraphFirst(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":          "test",
		"fusionStrategy": "graph_first",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata["fusionStrategy"] != "graph_first" {
		t.Errorf("expected fusionStrategy=graph_first, got %v", metadata["fusionStrategy"])
	}
}

func TestSearch_UnifiedSearch_FusionStrategyTextFirst(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":          "test",
		"fusionStrategy": "text_first",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata["fusionStrategy"] != "text_first" {
		t.Errorf("expected fusionStrategy=text_first, got %v", metadata["fusionStrategy"])
	}
}

// =============================================================================
// Test: Custom Weights
// =============================================================================

func TestSearch_UnifiedSearch_CustomWeights(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":          "test",
		"fusionStrategy": "weighted",
		"weights": map[string]any{
			"graphWeight": 0.7,
			"textWeight":  0.3,
		},
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata["fusionStrategy"] != "weighted" {
		t.Errorf("expected fusionStrategy=weighted, got %v", metadata["fusionStrategy"])
	}
}

// =============================================================================
// Test: Limit
// =============================================================================

func TestSearch_UnifiedSearch_WithLimit(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	for i := 0; i < 10; i++ {
		createGraphObjectForSearch(t, projectID, "Item", fmt.Sprintf("item-%d", i), map[string]any{
			"name": "Test Item",
		})
	}

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":       "Item",
		"limit":       5,
		"resultTypes": "graph",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	results, _ := response["results"].([]any)
	if len(results) > 5 {
		t.Errorf("expected at most 5 results, got %d", len(results))
	}
}

// =============================================================================
// Test: Debug Mode
// =============================================================================

func TestSearch_UnifiedSearch_DebugModeRequiresScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", "no-scope", projectID, jsonBody(map[string]any{
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

func TestSearch_UnifiedSearch_DebugModeRequiresScopeViaQueryParam(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified?debug=true", "no-scope", projectID, jsonBody(map[string]any{
		"query": "test",
	}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestSearch_UnifiedSearch_DebugModeViaBodyField(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":        "test debug",
		"includeDebug": true,
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["debug"] == nil {
		t.Error("debug should be present when includeDebug=true")
	}
	rl.Printf("debug mode returned debug info")
}

func TestSearch_UnifiedSearch_DebugModeViaQueryParam(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified?debug=true", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "query param debug",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["debug"] == nil {
		t.Error("debug should be present when ?debug=true")
	}
}

func TestSearch_UnifiedSearch_NoDebugWithoutFlag(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "no debug",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["debug"] != nil {
		t.Error("debug should NOT be present when debug is not requested")
	}
}

// =============================================================================
// Test: Execution Time Metadata
// =============================================================================

func TestSearch_UnifiedSearch_IncludesExecutionTime(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "test",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata == nil {
		t.Fatal("expected non-nil metadata")
	}
	execTime, _ := metadata["executionTime"].(map[string]any)
	if execTime == nil {
		t.Fatal("expected executionTime in metadata")
	}
	totalMs, _ := execTime["totalMs"].(float64)
	if totalMs < 0 {
		t.Errorf("expected totalMs >= 0, got %v", totalMs)
	}
	fusionMs, _ := execTime["fusionMs"].(float64)
	if fusionMs < 0 {
		t.Errorf("expected fusionMs >= 0, got %v", fusionMs)
	}
}

// =============================================================================
// Test: With Test Data
// =============================================================================

func TestSearch_UnifiedSearch_ReturnsGraphResults(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	createGraphObjectForSearch(t, projectID, "Requirement", "req-001", map[string]any{
		"title":       "User Authentication Requirement",
		"description": "The system shall support user authentication via OAuth2",
	})

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":       "authentication",
		"resultTypes": "graph",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["results"] == nil {
		t.Error("expected non-nil results")
	}
	rl.Printf("graph search returned results")
}

func TestSearch_UnifiedSearch_ReturnsTextResults(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	createDocumentForSearch(t, projectID, fmt.Sprintf("auth-doc-%d.txt", time.Now().UnixNano()), "This document discusses authentication requirements")

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":       "authentication",
		"resultTypes": "text",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["results"] == nil {
		t.Error("expected non-nil results")
	}
}

func TestSearch_UnifiedSearch_ReturnsBothResults(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	uniqueKey := fmt.Sprintf("req-security-%d", time.Now().UnixNano())
	createGraphObjectForSearch(t, projectID, "Requirement", uniqueKey, map[string]any{
		"title": "Security Requirement",
	})
	createDocumentForSearch(t, projectID, fmt.Sprintf("security-doc-%d.txt", time.Now().UnixNano()), "This document covers security requirements")

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query":       "security",
		"resultTypes": "both",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	if response["results"] == nil {
		t.Error("expected non-nil results")
	}
	if response["metadata"] == nil {
		t.Error("expected non-nil metadata")
	}
}

// =============================================================================
// Test: Default Fusion Strategy
// =============================================================================

func TestSearch_UnifiedSearch_DefaultFusionStrategy(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(map[string]any{
		"query": "test",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata["fusionStrategy"] != "weighted" {
		t.Errorf("expected default fusionStrategy=weighted, got %v", metadata["fusionStrategy"])
	}
	rl.Printf("default fusion strategy is weighted")
}
